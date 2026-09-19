package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

type capacityPoint struct {
	at    int64
	value licenseCapacity
}

// Capacity returns exact vendor-and-feature usage and capacity segments.
func (s *Store) Capacity(ctx context.Context, query AnalyticsQuery) (CapacityReport, error) {
	report, err := s.capacityReport(ctx, query)
	if err != nil {
		return CapacityReport{}, fmt.Errorf("capacity report: %w", err)
	}
	return report, nil
}

func (s *Store) capacityReport(ctx context.Context, query AnalyticsQuery) (CapacityReport, error) {
	if _, err := checkedInstant(query.From); err != nil {
		return CapacityReport{}, err
	}
	if _, err := checkedInstant(query.To); err != nil {
		return CapacityReport{}, err
	}
	q, err := s.normalizeQuery(ctx, query)
	if err != nil {
		return CapacityReport{}, err
	}
	arithmetic := checkedCapacityMath{}
	arithmetic.sub(q.to.UnixNano(), q.from.UnixNano())
	if arithmetic.err != nil {
		return CapacityReport{}, arithmetic.err
	}
	report := CapacityReport{From: q.from, To: q.to, Buckets: []CapacityBucket{}}
	report.Quality, err = s.quality(ctx, q, "usage")
	if err != nil {
		return CapacityReport{}, err
	}
	identities, err := s.capacityIdentities(ctx, q)
	if err != nil {
		return CapacityReport{}, err
	}
	for _, key := range identities {
		if err := ctx.Err(); err != nil {
			return CapacityReport{}, err
		}
		intervals, err := s.vendorUsageIntervals(ctx, q, key)
		if err != nil {
			return CapacityReport{}, err
		}
		points, err := s.capacityPoints(ctx, q, key)
		if err != nil {
			return CapacityReport{}, err
		}
		for _, calendar := range reportSegments(q) {
			cuts := []int64{calendar[0]}
			for _, p := range points {
				if p.at > calendar[0] && p.at < calendar[1] {
					cuts = append(cuts, p.at)
				}
			}
			cuts = append(cuts, calendar[1])
			for i := 0; i+1 < len(cuts); i++ {
				at := cuts[i]
				index := sort.Search(len(points), func(i int) bool { return points[i].at > at })
				known := index > 0
				value := licenseCapacity{}
				if known {
					value = points[index-1].value
				}
				bucket, err := capacitySegment(key, at, cuts[i+1], value, known, intervals)
				if err != nil {
					return CapacityReport{}, fmt.Errorf("vendor %q feature %q: %w", key.vendor, key.feature, err)
				}
				if !known {
					report.Quality.MissingEntitlement++
					duration := arithmetic.sub(cuts[i+1], at)
					report.Quality.UncoveredNanoseconds = arithmetic.add(report.Quality.UncoveredNanoseconds, duration)
				}
				if known && !value.uncounted && bucket.UpperPeak > value.licenses {
					report.Quality.UsageAboveEntitlement = true
				}
				report.Buckets = append(report.Buckets, bucket)
			}
		}
	}
	if arithmetic.err != nil {
		return CapacityReport{}, arithmetic.err
	}
	return report, nil
}

func (s *Store) capacityIdentities(ctx context.Context, q normalizedQuery) ([]licenseIdentity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT vendor,feature FROM capacity_changes WHERE pool_id=? AND effective_ns<?
 UNION SELECT daemon,feature FROM sessions WHERE pool_id=? AND session_type='usage'
 AND ((start_ns<? AND (end_ns IS NULL OR end_ns>?)) OR (state='orphan' AND end_ns>=? AND end_ns<?))
 ORDER BY 1,2`, q.poolID, q.to.UnixNano(), q.poolID, q.to.UnixNano(), q.from.UnixNano(), q.from.UnixNano(), q.to.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []licenseIdentity
	for rows.Next() {
		var key licenseIdentity
		if err := rows.Scan(&key.vendor, &key.feature); err != nil {
			return nil, err
		}
		if q.feature == "" || key.feature == q.feature {
			result = append(result, key)
		}
	}
	return result, rows.Err()
}

func (s *Store) capacityPoints(ctx context.Context, q normalizedQuery, key licenseIdentity) ([]capacityPoint, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT effective_ns,licenses,uncounted FROM capacity_changes WHERE pool_id=? AND vendor=? AND feature=? AND effective_ns<? ORDER BY effective_ns", q.poolID, key.vendor, key.feature, q.to.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var points []capacityPoint
	for rows.Next() {
		var p capacityPoint
		var n sql.NullInt64
		if err := rows.Scan(&p.at, &n, &p.value.uncounted); err != nil {
			return nil, err
		}
		if n.Valid {
			if n.Int64 < 0 || uint64(n.Int64) > uint64(^uint(0)>>1) {
				return nil, fmt.Errorf("capacity count out of range")
			}
			p.value.licenses = int(n.Int64)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *Store) vendorUsageIntervals(ctx context.Context, q normalizedQuery, key licenseIdentity) ([]usageInterval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT start_ns,end_ns,quantity,state,ambiguous FROM sessions WHERE pool_id=? AND daemon=? AND feature=? AND session_type='usage' AND state<>'orphan'
 AND start_ns<? AND (end_ns IS NULL OR end_ns>?)`, q.poolID, key.vendor, key.feature, q.to.UnixNano(), q.from.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []usageInterval
	for rows.Next() {
		var start int64
		var end sql.NullInt64
		var quantity int
		var state string
		var ambiguous bool
		if err := rows.Scan(&start, &end, &quantity, &state, &ambiguous); err != nil {
			return nil, err
		}
		finish := q.to.UnixNano()
		if end.Valid {
			finish = end.Int64
		}
		result = append(result, usageInterval{start: start, end: finish, quantity: quantity, lower: state == "closed" && !ambiguous})
	}
	return result, rows.Err()
}

// checkedCapacityMath keeps overflow propagation local to report arithmetic.
type checkedCapacityMath struct{ err error }

func (m *checkedCapacityMath) add(a, b int64) int64 {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		m.err = fmt.Errorf("capacity arithmetic overflow")
		return 0
	}
	return a + b
}
func (m *checkedCapacityMath) sub(a, b int64) int64 {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		m.err = fmt.Errorf("capacity arithmetic overflow")
		return 0
	}
	return a - b
}
func (m *checkedCapacityMath) mul(a, b int64) int64 {
	// Seat counts and elapsed durations are nonnegative.
	if a < 0 || b < 0 || (b != 0 && a > math.MaxInt64/b) {
		m.err = fmt.Errorf("capacity arithmetic overflow")
		return 0
	}
	return a * b
}

func capacitySegment(key licenseIdentity, from, to int64, value licenseCapacity, known bool, intervals []usageInterval) (CapacityBucket, error) {
	bucket := CapacityBucket{Vendor: key.vendor, Feature: key.feature, From: time.Unix(0, from).UTC(), To: time.Unix(0, to).UTC(), Uncounted: known && value.uncounted}
	finite := known && !value.uncounted
	if finite {
		p := value.licenses
		bucket.Purchased = &p
	}
	type delta struct{ lower, upper int64 }
	deltas := map[int64]delta{from: {}, to: {}}
	m := checkedCapacityMath{}
	for _, interval := range intervals {
		start, end := max64(interval.start, from), min64(interval.end, to)
		if start >= end {
			continue
		}
		for _, at := range []int64{start, end} {
			quantity := int64(interval.quantity)
			if at == end {
				quantity = -quantity
			}
			d := deltas[at]
			d.upper = m.add(d.upper, quantity)
			if interval.lower {
				d.lower = m.add(d.lower, quantity)
			}
			deltas[at] = d
		}
	}
	instants := make([]int64, 0, len(deltas))
	for at := range deltas {
		instants = append(instants, at)
	}
	sort.Slice(instants, func(i, j int) bool { return instants[i] < instants[j] })
	var lower, upper int64
	lowerMin, upperMin := int64(value.licenses), int64(value.licenses)
	for i, at := range instants {
		d := deltas[at]
		lower = m.add(lower, d.lower)
		upper = m.add(upper, d.upper)
		if i+1 == len(instants) {
			break
		}
		if lower < 0 || upper < 0 || uint64(upper) > uint64(^uint(0)>>1) || uint64(lower) > uint64(^uint(0)>>1) {
			return CapacityBucket{}, fmt.Errorf("usage count overflow")
		}
		if int(lower) > bucket.LowerPeak {
			bucket.LowerPeak = int(lower)
			t := time.Unix(0, at).UTC()
			bucket.LowerPeakAt = &t
		}
		if int(upper) > bucket.UpperPeak {
			bucket.UpperPeak = int(upper)
			t := time.Unix(0, at).UTC()
			bucket.UpperPeakAt = &t
		}
		duration := m.sub(instants[i+1], at)
		bucket.LowerUsedSeatNanoseconds = m.add(bucket.LowerUsedSeatNanoseconds, m.mul(lower, duration))
		bucket.UpperUsedSeatNanoseconds = m.add(bucket.UpperUsedSeatNanoseconds, m.mul(upper, duration))
		if finite {
			p := int64(value.licenses)
			if (p == 0 && lower > 0) || (p > 0 && lower >= p) {
				bucket.LowerSaturatedNanoseconds = m.add(bucket.LowerSaturatedNanoseconds, duration)
			}
			if (p == 0 && upper > 0) || (p > 0 && upper >= p) {
				bucket.UpperSaturatedNanoseconds = m.add(bucket.UpperSaturatedNanoseconds, duration)
			}
			lowerMin = min64(lowerMin, m.sub(p, lower))
			upperMin = min64(upperMin, m.sub(p, upper))
		}
	}
	if finite {
		duration := m.sub(to, from)
		available := m.mul(int64(value.licenses), duration)
		lowerNet := m.sub(available, bucket.LowerUsedSeatNanoseconds)
		upperNet := m.sub(available, bucket.UpperUsedSeatNanoseconds)
		lowerUnused, upperUnused := max64(0, lowerNet), max64(0, upperNet)
		lowerAverage, upperAverage := float64(lowerNet)/float64(duration), float64(upperNet)/float64(duration)
		low, high := int(lowerMin), int(upperMin)
		bucket.LowerUnusedSeatNanoseconds, bucket.UpperUnusedSeatNanoseconds = &lowerUnused, &upperUnused
		bucket.LowerMinimumHeadroom, bucket.UpperMinimumHeadroom = &low, &high
		bucket.LowerAverageHeadroom, bucket.UpperAverageHeadroom = &lowerAverage, &upperAverage
	}
	if m.err != nil {
		return CapacityBucket{}, m.err
	}
	return bucket, nil
}
