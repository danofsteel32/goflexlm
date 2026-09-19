package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

type normalizedQuery struct {
	poolID  int64
	pool    string
	feature string
	from    time.Time
	to      time.Time
	bucket  CalendarBucket
	zone    *time.Location
}

func (s *Store) normalizeQuery(ctx context.Context, query AnalyticsQuery) (normalizedQuery, error) {
	if err := validateName("pool", query.Pool); err != nil {
		return normalizedQuery{}, err
	}
	if query.From.IsZero() || query.To.IsZero() || !query.From.Before(query.To) {
		return normalizedQuery{}, errors.New("from and to must define a non-empty half-open range")
	}
	if query.Bucket != "" && query.Bucket != BucketHour && query.Bucket != BucketDay && query.Bucket != BucketWeek && query.Bucket != BucketMonth {
		return normalizedQuery{}, fmt.Errorf("unsupported bucket %q", query.Bucket)
	}
	zone := query.Timezone
	if zone == nil {
		zone = time.UTC
	}
	id, err := poolID(ctx, s.db, query.Pool)
	if err != nil {
		return normalizedQuery{}, err
	}
	return normalizedQuery{poolID: id, pool: query.Pool, feature: query.Feature,
		from: query.From.UTC(), to: query.To.UTC(), bucket: query.Bucket, zone: zone}, nil
}

func featureClause(feature string) (string, []any) {
	if feature == "" {
		return "", nil
	}
	return " AND feature = ?", []any{feature}
}

func (s *Store) quality(ctx context.Context, q normalizedQuery, sessionType string) (Quality, error) {
	var quality Quality
	clause, args := featureClause(q.feature)
	base := []any{q.poolID, sessionType, q.to.UnixNano(), q.from.UnixNano(), q.from.UnixNano(), q.to.UnixNano()}
	base = append(base, args...)
	row := s.db.QueryRowContext(ctx, `SELECT
          COALESCE(SUM(CASE WHEN state='open' THEN quantity ELSE 0 END),0),
          COALESCE(SUM(CASE WHEN ambiguous=1 THEN quantity ELSE 0 END),0),
          COALESCE(SUM(CASE WHEN state='orphan' THEN quantity ELSE 0 END),0),
          COALESCE(SUM(CASE WHEN state='closed' AND match_tier IN (1,2) THEN quantity ELSE 0 END),0)
		FROM sessions WHERE pool_id=? AND session_type=?
		  AND ((start_ns < ? AND (end_ns IS NULL OR end_ns > ?))
		    OR (state='orphan' AND end_ns>=? AND end_ns<?))`+clause, base...)
	if err := row.Scan(&quality.Open, &quality.Ambiguous, &quality.Orphan, &quality.WeakMatch); err != nil {
		return quality, fmt.Errorf("read session quality: %w", err)
	}
	activityArgs := []any{q.poolID}
	activityArgs = append(activityArgs, args...)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_events
        WHERE pool_id=? AND timestamp_ns IS NULL`+clause, activityArgs...).Scan(&quality.UnresolvedTime); err != nil {
		return quality, fmt.Errorf("read unresolved timestamp count: %w", err)
	}
	return quality, nil
}

type usageInterval struct {
	start, end int64
	quantity   int
	lower      bool
}

type entitlementPoint struct {
	at       int64
	licenses int
}

// Capacity returns exact interval-based usage and capacity measures.
func (s *Store) Capacity(ctx context.Context, query AnalyticsQuery) (CapacityReport, error) {
	q, err := s.normalizeQuery(ctx, query)
	if err != nil {
		return CapacityReport{}, fmt.Errorf("capacity report: %w", err)
	}
	report := CapacityReport{From: q.from, To: q.to, Buckets: []CapacityBucket{}}
	report.Quality, err = s.quality(ctx, q, "usage")
	if err != nil {
		return CapacityReport{}, fmt.Errorf("capacity report: %w", err)
	}
	features, err := s.reportFeatures(ctx, q, true)
	if err != nil {
		return CapacityReport{}, fmt.Errorf("capacity report: %w", err)
	}
	for _, feature := range features {
		intervals, err := s.usageIntervals(ctx, q, feature)
		if err != nil {
			return CapacityReport{}, fmt.Errorf("capacity report: %w", err)
		}
		points, err := s.entitlementPoints(ctx, q.poolID, feature, q.to.UnixNano())
		if err != nil {
			return CapacityReport{}, fmt.Errorf("capacity report: %w", err)
		}
		segments := reportSegments(q)
		segments = splitAtEntitlements(segments, points)
		for _, segment := range segments {
			purchased, covered := entitlementAt(points, segment[0])
			bucket := capacitySegment(feature, segment[0], segment[1], purchased, covered, intervals)
			if !covered {
				report.Quality.MissingEntitlement++
				report.Quality.UncoveredNanoseconds += segment[1] - segment[0]
			}
			if covered && bucket.UpperPeak > purchased {
				report.Quality.UsageAboveEntitlement = true
			}
			report.Buckets = append(report.Buckets, bucket)
		}
	}
	return report, nil
}

func (s *Store) reportFeatures(ctx context.Context, q normalizedQuery, includeEntitlements bool) ([]string, error) {
	if q.feature != "" {
		return []string{q.feature}, nil
	}
	statement := `SELECT DISTINCT feature FROM activity_events WHERE pool_id=?`
	if includeEntitlements {
		statement = `SELECT feature FROM activity_events WHERE pool_id=? UNION SELECT feature FROM entitlements WHERE pool_id=? ORDER BY feature`
	}
	args := []any{q.poolID}
	if includeEntitlements {
		args = append(args, q.poolID)
	}
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list features: %w", err)
	}
	defer rows.Close()
	var features []string
	for rows.Next() {
		var feature string
		if err := rows.Scan(&feature); err != nil {
			return nil, fmt.Errorf("scan feature: %w", err)
		}
		features = append(features, feature)
	}
	return features, rows.Err()
}

func (s *Store) usageIntervals(ctx context.Context, q normalizedQuery, feature string) ([]usageInterval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT start_ns, end_ns, quantity, state, ambiguous
        FROM sessions WHERE pool_id=? AND feature=? AND session_type='usage' AND start_ns < ?
          AND (end_ns IS NULL OR end_ns > ?) AND state <> 'orphan'`,
		q.poolID, feature, q.to.UnixNano(), q.from.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("read usage sessions: %w", err)
	}
	defer rows.Close()
	var intervals []usageInterval
	for rows.Next() {
		var start int64
		var end sql.NullInt64
		var quantity int
		var state string
		var ambiguous bool
		if err := rows.Scan(&start, &end, &quantity, &state, &ambiguous); err != nil {
			return nil, fmt.Errorf("scan usage session: %w", err)
		}
		finish := q.to.UnixNano()
		if end.Valid {
			finish = end.Int64
		}
		intervals = append(intervals, usageInterval{start: start, end: finish, quantity: quantity, lower: state == "closed" && !ambiguous})
	}
	return intervals, rows.Err()
}

func (s *Store) entitlementPoints(ctx context.Context, pool int64, feature string, before int64) ([]entitlementPoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT effective_ns, licenses FROM entitlements
        WHERE pool_id=? AND feature=? AND effective_ns < ? ORDER BY effective_ns`, pool, feature, before)
	if err != nil {
		return nil, fmt.Errorf("read entitlements: %w", err)
	}
	defer rows.Close()
	var points []entitlementPoint
	for rows.Next() {
		var point entitlementPoint
		if err := rows.Scan(&point.at, &point.licenses); err != nil {
			return nil, fmt.Errorf("scan entitlement: %w", err)
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

func reportSegments(q normalizedQuery) [][2]int64 {
	from, to := q.from.UnixNano(), q.to.UnixNano()
	if q.bucket == "" {
		return [][2]int64{{from, to}}
	}
	var segments [][2]int64
	start := q.from
	for start.Before(q.to) {
		next := nextBucketBoundary(start, q.bucket, q.zone)
		if next.After(q.to) {
			next = q.to
		}
		segments = append(segments, [2]int64{start.UnixNano(), next.UnixNano()})
		start = next
	}
	return segments
}

func nextBucketBoundary(instant time.Time, bucket CalendarBucket, zone *time.Location) time.Time {
	local := instant.In(zone)
	var boundary time.Time
	switch bucket {
	case BucketHour:
		withinHour := time.Duration(local.Minute())*time.Minute + time.Duration(local.Second())*time.Second + time.Duration(local.Nanosecond())
		boundary = instant.Add(time.Hour - withinHour)
	case BucketDay:
		boundary = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, zone)
	case BucketWeek:
		days := (8 - int(local.Weekday())) % 7
		if days == 0 {
			days = 7
		}
		boundary = time.Date(local.Year(), local.Month(), local.Day()+days, 0, 0, 0, 0, zone)
	case BucketMonth:
		boundary = time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, zone)
	}
	if !boundary.After(instant) {
		return instant.Add(time.Nanosecond)
	}
	return boundary.UTC()
}

func splitAtEntitlements(segments [][2]int64, points []entitlementPoint) [][2]int64 {
	var result [][2]int64
	for _, segment := range segments {
		cuts := []int64{segment[0]}
		for _, point := range points {
			if point.at > segment[0] && point.at < segment[1] {
				cuts = append(cuts, point.at)
			}
		}
		cuts = append(cuts, segment[1])
		for index := 0; index+1 < len(cuts); index++ {
			result = append(result, [2]int64{cuts[index], cuts[index+1]})
		}
	}
	return result
}

func entitlementAt(points []entitlementPoint, at int64) (int, bool) {
	index := sort.Search(len(points), func(index int) bool { return points[index].at > at })
	if index == 0 {
		return 0, false
	}
	return points[index-1].licenses, true
}

type usageDelta struct{ lower, upper int }

func capacitySegment(feature string, from, to int64, purchased int, covered bool, intervals []usageInterval) CapacityBucket {
	bucket := CapacityBucket{Feature: feature, From: time.Unix(0, from).UTC(), To: time.Unix(0, to).UTC()}
	if covered {
		value := purchased
		bucket.Purchased = &value
	}
	deltas := map[int64]usageDelta{from: {}}
	for _, interval := range intervals {
		start, end := max64(interval.start, from), min64(interval.end, to)
		if start >= end {
			continue
		}
		delta := deltas[start]
		delta.upper += interval.quantity
		if interval.lower {
			delta.lower += interval.quantity
		}
		deltas[start] = delta
		delta = deltas[end]
		delta.upper -= interval.quantity
		if interval.lower {
			delta.lower -= interval.quantity
		}
		deltas[end] = delta
	}
	times := make([]int64, 0, len(deltas)+1)
	for at := range deltas {
		times = append(times, at)
	}
	if _, ok := deltas[to]; !ok {
		times = append(times, to)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	lower, upper := 0, 0
	lowerMin, upperMin := purchased, purchased
	for index, at := range times {
		delta := deltas[at]
		lower += delta.lower
		upper += delta.upper
		if lower > bucket.LowerPeak {
			bucket.LowerPeak = lower
			t := time.Unix(0, at).UTC()
			bucket.LowerPeakAt = &t
		}
		if upper > bucket.UpperPeak {
			bucket.UpperPeak = upper
			t := time.Unix(0, at).UTC()
			bucket.UpperPeakAt = &t
		}
		if index+1 == len(times) {
			break
		}
		duration := times[index+1] - at
		bucket.LowerUsedSeatNanoseconds += int64(lower) * duration
		bucket.UpperUsedSeatNanoseconds += int64(upper) * duration
		if covered {
			if (purchased == 0 && lower > 0) || (purchased > 0 && lower >= purchased) {
				bucket.LowerSaturatedNanoseconds += duration
			}
			if (purchased == 0 && upper > 0) || (purchased > 0 && upper >= purchased) {
				bucket.UpperSaturatedNanoseconds += duration
			}
			lowerMin = min(lowerMin, purchased-lower)
			upperMin = min(upperMin, purchased-upper)
		}
	}
	if covered {
		duration := to - from
		lowerUnused := max64(0, int64(purchased)*duration-bucket.LowerUsedSeatNanoseconds)
		upperUnused := max64(0, int64(purchased)*duration-bucket.UpperUsedSeatNanoseconds)
		lowerAverage := float64(int64(purchased)*duration-bucket.LowerUsedSeatNanoseconds) / float64(duration)
		upperAverage := float64(int64(purchased)*duration-bucket.UpperUsedSeatNanoseconds) / float64(duration)
		bucket.LowerUnusedSeatNanoseconds, bucket.UpperUnusedSeatNanoseconds = &lowerUnused, &upperUnused
		bucket.LowerMinimumHeadroom, bucket.UpperMinimumHeadroom = &lowerMin, &upperMin
		bucket.LowerAverageHeadroom, bucket.UpperAverageHeadroom = &lowerAverage, &upperAverage
	}
	return bucket
}

// Denials returns denied event and requested-license quantities.
func (s *Store) Denials(ctx context.Context, query AnalyticsQuery) (DenialReport, error) {
	q, err := s.normalizeQuery(ctx, query)
	if err != nil {
		return DenialReport{}, fmt.Errorf("denial report: %w", err)
	}
	report := DenialReport{From: q.from, To: q.to, Buckets: []DenialBucket{}}
	report.Quality, err = s.quality(ctx, q, "usage")
	if err != nil {
		return DenialReport{}, fmt.Errorf("denial report: %w", err)
	}
	clause, args := featureClause(q.feature)
	queryArgs := []any{q.poolID, q.from.UnixNano(), q.to.UnixNano()}
	queryArgs = append(queryArgs, args...)
	rows, err := s.db.QueryContext(ctx, `SELECT timestamp_ns, feature, denial_reason, denial_error, licenses
        FROM activity_events WHERE pool_id=? AND kind='denied' AND timestamp_ns>=? AND timestamp_ns<?`+clause+
		` ORDER BY timestamp_ns, feature, denial_reason, denial_error`, queryArgs...)
	if err != nil {
		return DenialReport{}, fmt.Errorf("denial report: query: %w", err)
	}
	defer rows.Close()
	type key struct {
		feature, reason, code string
		from, to              int64
	}
	groups := make(map[key]*DenialBucket)
	var order []key
	for rows.Next() {
		var at int64
		var feature, reason, code string
		var licenses int
		if err := rows.Scan(&at, &feature, &reason, &code, &licenses); err != nil {
			return DenialReport{}, fmt.Errorf("denial report: scan: %w", err)
		}
		var from, to int64
		if q.bucket != "" {
			from, to = containingSegment(at, q)
		}
		groupKey := key{feature, reason, code, from, to}
		group := groups[groupKey]
		if group == nil {
			group = &DenialBucket{Feature: feature, Reason: reason, ErrorCode: code}
			if q.bucket != "" {
				f, t := time.Unix(0, from).UTC(), time.Unix(0, to).UTC()
				group.From, group.To = &f, &t
			}
			groups[groupKey] = group
			order = append(order, groupKey)
		}
		group.Events++
		group.Licenses += licenses
	}
	if err := rows.Err(); err != nil {
		return DenialReport{}, fmt.Errorf("denial report: %w", err)
	}
	for _, groupKey := range order {
		report.Buckets = append(report.Buckets, *groups[groupKey])
	}
	return report, nil
}

func containingSegment(at int64, q normalizedQuery) (int64, int64) {
	for _, segment := range reportSegments(q) {
		if at >= segment[0] && at < segment[1] {
			return segment[0], segment[1]
		}
	}
	return q.from.UnixNano(), q.to.UnixNano()
}

type waitValue struct{ duration int64 }

// Queueing returns queue volume, depth, and completed wait durations.
func (s *Store) Queueing(ctx context.Context, query AnalyticsQuery) (QueueReport, error) {
	q, err := s.normalizeQuery(ctx, query)
	if err != nil {
		return QueueReport{}, fmt.Errorf("queue report: %w", err)
	}
	report := QueueReport{From: q.from, To: q.to, Buckets: []QueueBucket{}}
	report.Quality, err = s.quality(ctx, q, "queue")
	if err != nil {
		return QueueReport{}, fmt.Errorf("queue report: %w", err)
	}
	features, err := s.queueFeatures(ctx, q)
	if err != nil {
		return QueueReport{}, fmt.Errorf("queue report: %w", err)
	}
	segments := reportSegments(q)
	for _, feature := range features {
		for _, segment := range segments {
			bucket, err := s.queueSegment(ctx, q, feature, segment[0], segment[1])
			if err != nil {
				return QueueReport{}, fmt.Errorf("queue report: %w", err)
			}
			report.Buckets = append(report.Buckets, bucket)
		}
	}
	return report, nil
}

func (s *Store) queueFeatures(ctx context.Context, q normalizedQuery) ([]string, error) {
	if q.feature != "" {
		return []string{q.feature}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT feature FROM activity_events
		WHERE pool_id=? AND kind IN ('queued','dequeued') ORDER BY feature`, q.poolID)
	if err != nil {
		return nil, fmt.Errorf("list queue features: %w", err)
	}
	defer rows.Close()
	var features []string
	for rows.Next() {
		var feature string
		if err := rows.Scan(&feature); err != nil {
			return nil, fmt.Errorf("scan queue feature: %w", err)
		}
		features = append(features, feature)
	}
	return features, rows.Err()
}

func (s *Store) queueSegment(ctx context.Context, q normalizedQuery, feature string, from, to int64) (QueueBucket, error) {
	f, t := time.Unix(0, from).UTC(), time.Unix(0, to).UTC()
	bucket := QueueBucket{Feature: feature}
	if q.bucket != "" {
		bucket.From, bucket.To = &f, &t
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(licenses),0) FROM activity_events
        WHERE pool_id=? AND feature=? AND kind='queued' AND timestamp_ns>=? AND timestamp_ns<?`,
		q.poolID, feature, from, to).Scan(&bucket.QueuedEvents, &bucket.QueuedLicenses); err != nil {
		return bucket, fmt.Errorf("read queue volume: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT start_ns, end_ns, quantity, state, ambiguous
        FROM sessions WHERE pool_id=? AND feature=? AND session_type='queue'
          AND ((start_ns < ? AND (end_ns IS NULL OR end_ns > ?))
            OR (state='orphan' AND end_ns>=? AND end_ns<?))`, q.poolID, feature, to, from, from, to)
	if err != nil {
		return bucket, fmt.Errorf("read queue sessions: %w", err)
	}
	defer rows.Close()
	deltas := map[int64]int{}
	var waits []waitValue
	oldestOpen := int64(0)
	for rows.Next() {
		var start, end sql.NullInt64
		var quantity int
		var state string
		var ambiguous bool
		if err := rows.Scan(&start, &end, &quantity, &state, &ambiguous); err != nil {
			return bucket, fmt.Errorf("scan queue session: %w", err)
		}
		if ambiguous {
			bucket.Ambiguous += quantity
		}
		if state == "orphan" {
			bucket.Orphan += quantity
			continue
		}
		activeStart := max64(start.Int64, from)
		activeEnd := to
		if end.Valid {
			activeEnd = min64(end.Int64, to)
		}
		if activeStart < activeEnd {
			deltas[activeStart] += quantity
			deltas[activeEnd] -= quantity
		}
		if state == "closed" && start.Int64 >= from && start.Int64 < to {
			waits = append(waits, waitValue{duration: end.Int64 - start.Int64})
			bucket.CompletedWaits++
		}
		if state == "open" && start.Int64 < to {
			bucket.OpenQueueLicenses += quantity
			if oldestOpen == 0 || start.Int64 < oldestOpen {
				oldestOpen = start.Int64
			}
		}
	}
	if err := rows.Err(); err != nil {
		return bucket, err
	}
	var depth int
	times := make([]int64, 0, len(deltas))
	for at := range deltas {
		times = append(times, at)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	for _, at := range times {
		depth += deltas[at]
		bucket.MaximumDepth = max(bucket.MaximumDepth, depth)
	}
	if len(waits) > 0 {
		sort.Slice(waits, func(i, j int) bool { return waits[i].duration < waits[j].duration })
		p50, p95 := percentile(waits, 0.50), percentile(waits, 0.95)
		maximum := time.Duration(waits[len(waits)-1].duration)
		bucket.P50Wait, bucket.P95Wait, bucket.MaximumWait = &p50, &p95, &maximum
	}
	if oldestOpen != 0 {
		age := time.Duration(to - oldestOpen)
		bucket.OldestOpenAge = &age
	}
	return bucket, nil
}

func percentile(values []waitValue, fraction float64) time.Duration {
	target := int(float64(len(values))*fraction+0.999999999) - 1
	return time.Duration(values[target].duration)
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func max64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func min64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
