package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type identity struct {
	handle   string
	checkout string
	pid      string
	ip       string
	user     string
	host     string
}

type derivationEvent struct {
	id       int64
	stream   int64
	daemon   string
	feature  string
	kind     string
	ts       int64
	quantity int
	identity identity
}

type openAllocation struct {
	eventID  int64
	start    int64
	quantity int
	identity identity
}

type partition struct {
	poolID      int64
	streamID    int64
	daemon      string
	feature     string
	sessionType string
	open        []*openAllocation
}

// Rebuild reconstructs all disposable sessions for pool from immutable events.
func (s *Store) Rebuild(ctx context.Context, pool string) error {
	if err := validateName("pool", pool); err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rebuild: begin transaction: %w", err)
	}
	defer tx.Rollback()
	id, err := poolID(ctx, tx, pool)
	if err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	if err := rebuildTx(ctx, tx, id); err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rebuild: commit: %w", err)
	}
	return nil
}

func rebuildTx(ctx context.Context, tx *sql.Tx, pool int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE pool_id = ?`, pool); err != nil {
		return fmt.Errorf("clear sessions: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, stream_id, daemon, feature, kind, timestamp_ns,
          licenses, verbose_handle, checkout_data, verbose_pid, verbose_ip, user_name, host_name
        FROM activity_events
        WHERE pool_id = ? AND timestamp_ns IS NOT NULL AND kind IN ('checkout','checkin','queued','dequeued')
        ORDER BY timestamp_ns, import_id, source_line, id`, pool)
	if err != nil {
		return fmt.Errorf("read activities: %w", err)
	}
	defer rows.Close()
	partitions := make(map[string]*partition)
	insert, err := tx.PrepareContext(ctx, `INSERT INTO sessions
          (pool_id, stream_id, daemon, feature, session_type, start_ns, end_ns, quantity,
           state, match_tier, ambiguous, opening_event_id, closing_event_id)
          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare session insert: %w", err)
	}
	defer insert.Close()
	for rows.Next() {
		var event derivationEvent
		if err := rows.Scan(&event.id, &event.stream, &event.daemon, &event.feature, &event.kind,
			&event.ts, &event.quantity, &event.identity.handle, &event.identity.checkout,
			&event.identity.pid, &event.identity.ip, &event.identity.user, &event.identity.host); err != nil {
			return fmt.Errorf("scan activity: %w", err)
		}
		typeName, opening := eventSessionType(event.kind)
		key := fmt.Sprintf("%d\x00%s\x00%s\x00%s", event.stream, event.daemon, event.feature, typeName)
		p := partitions[key]
		if p == nil {
			p = &partition{poolID: pool, streamID: event.stream, daemon: event.daemon, feature: event.feature, sessionType: typeName}
			partitions[key] = p
		}
		if opening {
			p.open = append(p.open, &openAllocation{eventID: event.id, start: event.ts, quantity: event.quantity, identity: event.identity})
			continue
		}
		if err := closeAllocations(ctx, insert, p, event); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate activities: %w", err)
	}
	for _, p := range partitions {
		for _, allocation := range p.open {
			if allocation.quantity == 0 {
				continue
			}
			if err := insertSession(ctx, insert, p, allocation.start, nil, allocation.quantity, "open", 0, false, allocation.eventID, nil); err != nil {
				return err
			}
		}
	}
	return updateDerivationState(ctx, tx, pool)
}

func appendDerivationTx(ctx context.Context, tx *sql.Tx, pool, importID int64) error {
	partitions := make(map[string]*partition)
	rows, err := tx.QueryContext(ctx, `SELECT s.stream_id, s.daemon, s.feature, s.session_type,
		s.opening_event_id, s.start_ns, s.quantity, e.verbose_handle, e.checkout_data,
		e.verbose_pid, e.verbose_ip, e.user_name, e.host_name
		FROM sessions s JOIN activity_events e ON e.id=s.opening_event_id
		WHERE s.pool_id=? AND s.state='open' ORDER BY s.start_ns, s.id`, pool)
	if err != nil {
		return fmt.Errorf("read open sessions: %w", err)
	}
	for rows.Next() {
		var p partition
		var allocation openAllocation
		if err := rows.Scan(&p.streamID, &p.daemon, &p.feature, &p.sessionType,
			&allocation.eventID, &allocation.start, &allocation.quantity, &allocation.identity.handle,
			&allocation.identity.checkout, &allocation.identity.pid, &allocation.identity.ip,
			&allocation.identity.user, &allocation.identity.host); err != nil {
			rows.Close()
			return fmt.Errorf("scan open session: %w", err)
		}
		p.poolID = pool
		key := fmt.Sprintf("%d\x00%s\x00%s\x00%s", p.streamID, p.daemon, p.feature, p.sessionType)
		existing := partitions[key]
		if existing == nil {
			existing = &p
			partitions[key] = existing
		}
		existing.open = append(existing.open, &allocation)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("read open sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE pool_id=? AND state='open'`, pool); err != nil {
		return fmt.Errorf("replace open sessions: %w", err)
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO sessions
		(pool_id, stream_id, daemon, feature, session_type, start_ns, end_ns, quantity,
		 state, match_tier, ambiguous, opening_event_id, closing_event_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare session insert: %w", err)
	}
	defer insert.Close()
	events, err := tx.QueryContext(ctx, `SELECT id, stream_id, daemon, feature, kind, timestamp_ns,
		licenses, verbose_handle, checkout_data, verbose_pid, verbose_ip, user_name, host_name
		FROM activity_events WHERE import_id=? AND timestamp_ns IS NOT NULL
		  AND kind IN ('checkout','checkin','queued','dequeued')
		ORDER BY timestamp_ns, source_line, id`, importID)
	if err != nil {
		return fmt.Errorf("read appended activities: %w", err)
	}
	for events.Next() {
		var event derivationEvent
		if err := events.Scan(&event.id, &event.stream, &event.daemon, &event.feature, &event.kind,
			&event.ts, &event.quantity, &event.identity.handle, &event.identity.checkout,
			&event.identity.pid, &event.identity.ip, &event.identity.user, &event.identity.host); err != nil {
			events.Close()
			return fmt.Errorf("scan appended activity: %w", err)
		}
		typeName, opening := eventSessionType(event.kind)
		key := fmt.Sprintf("%d\x00%s\x00%s\x00%s", event.stream, event.daemon, event.feature, typeName)
		p := partitions[key]
		if p == nil {
			p = &partition{poolID: pool, streamID: event.stream, daemon: event.daemon, feature: event.feature, sessionType: typeName}
			partitions[key] = p
		}
		if opening {
			p.open = append(p.open, &openAllocation{eventID: event.id, start: event.ts, quantity: event.quantity, identity: event.identity})
		} else if err := closeAllocations(ctx, insert, p, event); err != nil {
			events.Close()
			return err
		}
	}
	if err := events.Close(); err != nil {
		return fmt.Errorf("read appended activities: %w", err)
	}
	for _, p := range partitions {
		for _, allocation := range p.open {
			if allocation.quantity > 0 {
				if err := insertSession(ctx, insert, p, allocation.start, nil, allocation.quantity, "open", 0, false, allocation.eventID, nil); err != nil {
					return err
				}
			}
		}
	}
	return updateDerivationState(ctx, tx, pool)
}

func updateDerivationState(ctx context.Context, tx *sql.Tx, pool int64) error {
	var maxEventID int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM activity_events WHERE pool_id=?`, pool).Scan(&maxEventID); err != nil {
		return fmt.Errorf("read derivation position: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO derivation_state(pool_id, version, processed_event_id)
		VALUES (?, ?, ?) ON CONFLICT(pool_id) DO UPDATE SET version=excluded.version,
		processed_event_id=excluded.processed_event_id`, pool, derivationVersion, maxEventID); err != nil {
		return fmt.Errorf("update derivation state: %w", err)
	}
	return nil
}

func eventSessionType(kind string) (string, bool) {
	switch kind {
	case "checkout":
		return "usage", true
	case "checkin":
		return "usage", false
	case "queued":
		return "queue", true
	default:
		return "queue", false
	}
}

func closeAllocations(ctx context.Context, insert *sql.Stmt, p *partition, close derivationEvent) error {
	remaining := close.quantity
	ambiguousTier := 0
	for remaining > 0 {
		bestTier := 0
		var candidates []int
		for index, allocation := range p.open {
			if allocation.quantity == 0 || !compatible(allocation.identity, close.identity) {
				continue
			}
			tier := matchTier(allocation.identity, close.identity)
			if tier == 0 {
				continue
			}
			if tier > bestTier {
				bestTier = tier
				candidates = candidates[:0]
			}
			if tier == bestTier {
				candidates = append(candidates, index)
			}
		}
		if len(candidates) == 0 {
			end := close.ts
			closing := close.id
			if err := insertSession(ctx, insert, p, nil, &end, remaining, "orphan", 0, false, nil, &closing); err != nil {
				return err
			}
			return nil
		}
		if len(candidates) > 1 {
			ambiguousTier = bestTier
		}
		allocation := p.open[candidates[0]]
		quantity := remaining
		if quantity > allocation.quantity {
			quantity = allocation.quantity
		}
		end, opening, closing := close.ts, allocation.eventID, close.id
		if err := insertSession(ctx, insert, p, allocation.start, &end, quantity, "closed", bestTier,
			ambiguousTier == bestTier, opening, &closing); err != nil {
			return err
		}
		allocation.quantity -= quantity
		remaining -= quantity
	}
	return nil
}

func insertSession(ctx context.Context, statement *sql.Stmt, p *partition, start any, end *int64,
	quantity int, state string, tier int, ambiguous bool, opening any, closing *int64) error {
	if _, err := statement.ExecContext(ctx, p.poolID, p.streamID, p.daemon, p.feature,
		p.sessionType, start, end, quantity, state, tier, ambiguous, opening, closing); err != nil {
		return fmt.Errorf("insert %s session: %w", p.sessionType, err)
	}
	return nil
}

func compatible(left, right identity) bool {
	pairs := [][2]string{{left.handle, right.handle}, {left.checkout, right.checkout},
		{left.pid, right.pid}, {left.ip, right.ip}, {left.user, right.user}, {left.host, right.host}}
	for _, pair := range pairs {
		if usableIdentity(pair[0]) && usableIdentity(pair[1]) && pair[0] != pair[1] {
			return false
		}
	}
	return true
}

func matchTier(left, right identity) int {
	if sameIdentity(left.handle, right.handle) {
		return 6
	}
	if sameIdentity(left.checkout, right.checkout) {
		return 5
	}
	if sameIdentity(left.pid, right.pid) && sameIdentity(left.ip, right.ip) {
		return 4
	}
	if sameIdentity(left.user, right.user) && sameIdentity(left.host, right.host) {
		return 3
	}
	if sameIdentity(left.host, right.host) {
		return 2
	}
	if sameIdentity(left.user, right.user) {
		return 1
	}
	return 0
}

func sameIdentity(left, right string) bool {
	return usableIdentity(left) && usableIdentity(right) && left == right
}

func usableIdentity(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "-", "N/A", "NA", "NONE", "UNKNOWN":
		return false
	default:
		return true
	}
}
