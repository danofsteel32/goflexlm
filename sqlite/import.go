package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"time"

	"github.com/danofsteel32/goflexlm"
)

type sourceCounter struct {
	hash        hash.Hash
	bytes       int64
	newlines    int
	hasBytes    bool
	lastNewline bool
}

func (c *sourceCounter) Write(p []byte) (int, error) {
	c.bytes += int64(len(p))
	for _, b := range p {
		if b == '\n' {
			c.newlines++
		}
	}
	if len(p) != 0 {
		c.hasBytes = true
		c.lastNewline = p[len(p)-1] == '\n'
	}
	return c.hash.Write(p)
}

func (c *sourceCounter) lines() int {
	if c.hasBytes && !c.lastNewline {
		return c.newlines + 1
	}
	return c.newlines
}

// Import streams and atomically stores one complete source file.
func (s *Store) Import(ctx context.Context, request ImportRequest) (ImportResult, error) {
	var result ImportResult
	result.DiagnosticCounts = make(map[string]int)
	if request.Reader == nil {
		return result, errors.New("import: reader is required")
	}
	if err := validateName("pool", request.Pool); err != nil {
		return result, fmt.Errorf("import: %w", err)
	}
	if err := validateName("stream", request.Stream); err != nil {
		return result, fmt.Errorf("import: %w", err)
	}
	if err := validateName("source name", request.SourceName); err != nil {
		return result, fmt.Errorf("import: %w", err)
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("import: begin transaction: %w", err)
	}
	defer tx.Rollback()

	pool, stream, err := ensurePoolStream(ctx, tx, request.Pool, request.Stream)
	if err != nil {
		return result, fmt.Errorf("import: %w", err)
	}
	var previousMaxTimestamp sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(timestamp_ns) FROM activity_events WHERE pool_id=?`, pool).Scan(&previousMaxTimestamp); err != nil {
		return result, fmt.Errorf("import: read existing chronology: %w", err)
	}
	placeholder := make([]byte, sha256.Size)
	if _, err := rand.Read(placeholder); err != nil {
		return result, fmt.Errorf("import: create provisional digest: %w", err)
	}
	created, err := tx.ExecContext(ctx, `INSERT INTO imports
          (stream_id, source_name, digest, byte_count, line_count, activity_count,
           omitted_message_count, diagnostic_count, diagnostic_json, unresolved_count, imported_at_ns)
          VALUES (?, ?, ?, 0, 0, 0, 0, 0, '{}', 0, ?)`,
		stream, request.SourceName, placeholder, time.Now().UTC().UnixNano())
	if err != nil {
		return result, fmt.Errorf("import: create import: %w", err)
	}
	importID, _ := created.LastInsertId()
	statement, err := tx.PrepareContext(ctx, insertActivitySQL)
	if err != nil {
		return result, fmt.Errorf("import: prepare activity insert: %w", err)
	}
	defer statement.Close()

	counter := &sourceCounter{hash: sha256.New()}
	decoder := goflexlm.NewDecoder(io.TeeReader(request.Reader, counter))
	var firstSessionTimestamp sql.NullInt64
	for decoder.Scan() {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("import: %w", err)
		}
		item := decoder.Result()
		if item.Diagnostic != nil {
			result.DiagnosticCounts[item.Diagnostic.Code]++
			if request.OnDiagnostic != nil {
				if err := request.OnDiagnostic(*item.Diagnostic); err != nil {
					return result, fmt.Errorf("import: diagnostic callback: %w", err)
				}
			}
			continue
		}
		if item.Event.Kind == goflexlm.KindMessage {
			result.OmittedMessageCount++
			continue
		}
		if item.Event.LogTime.Time != nil && (item.Event.Kind == goflexlm.KindCheckout || item.Event.Kind == goflexlm.KindCheckin || item.Event.Kind == goflexlm.KindQueued || item.Event.Kind == goflexlm.KindDequeued) {
			at := item.Event.LogTime.Time.UTC().UnixNano()
			if !firstSessionTimestamp.Valid || at < firstSessionTimestamp.Int64 {
				firstSessionTimestamp = sql.NullInt64{Int64: at, Valid: true}
			}
		}
		if err := insertActivity(ctx, statement, importID, stream, pool, request.SourceName, item.Event); err != nil {
			return result, fmt.Errorf("import: store line %d: %w", item.Event.Line, err)
		}
		result.ActivityCount++
		if item.Event.LogTime.Time == nil {
			result.UnresolvedTimestampCount++
		}
	}
	if err := decoder.Err(); err != nil {
		return result, fmt.Errorf("import: read source: %w", err)
	}
	result.ByteCount = counter.bytes
	result.LineCount = counter.lines()
	digest := counter.hash.Sum(nil)
	result.Digest = hex.EncodeToString(digest)
	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM imports WHERE stream_id = ? AND digest = ? AND id <> ?`, stream, digest, importID).Scan(&existing)
	if err == nil {
		result.Duplicate = true
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("import: check digest: %w", err)
	}
	diagnosticJSON, err := json.Marshal(result.DiagnosticCounts)
	if err != nil {
		return result, fmt.Errorf("import: encode diagnostic summary: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE imports SET digest = ?, byte_count = ?, line_count = ?,
          activity_count = ?, omitted_message_count = ?, diagnostic_count = ?, diagnostic_json = ?, unresolved_count = ?
		  WHERE id = ?`, digest, result.ByteCount, result.LineCount, result.ActivityCount, result.OmittedMessageCount,
		diagnosticTotal(result.DiagnosticCounts), string(diagnosticJSON), result.UnresolvedTimestampCount, importID); err != nil {
		return result, fmt.Errorf("import: finalize import: %w", err)
	}
	derive := rebuildTx
	if !firstSessionTimestamp.Valid || !previousMaxTimestamp.Valid || firstSessionTimestamp.Int64 >= previousMaxTimestamp.Int64 {
		derive = func(ctx context.Context, tx *sql.Tx, pool int64) error {
			return appendDerivationTx(ctx, tx, pool, importID)
		}
	}
	if err := derive(ctx, tx, pool); err != nil {
		return result, fmt.Errorf("import: derive sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("import: commit: %w", err)
	}
	if s.path != "" {
		secureSidecars(s.path)
	}
	return result, nil
}

func diagnosticTotal(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func ensurePoolStream(ctx context.Context, tx *sql.Tx, poolName, streamName string) (int64, int64, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO license_pools(name) VALUES (?) ON CONFLICT(name) DO NOTHING`, poolName); err != nil {
		return 0, 0, fmt.Errorf("ensure pool: %w", err)
	}
	pool, err := poolID(ctx, tx, poolName)
	if err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO log_streams(pool_id, name) VALUES (?, ?) ON CONFLICT(name) DO NOTHING`, pool, streamName); err != nil {
		return 0, 0, fmt.Errorf("ensure stream: %w", err)
	}
	var stream, assignedPool int64
	if err := tx.QueryRowContext(ctx, `SELECT id, pool_id FROM log_streams WHERE name = ?`, streamName).Scan(&stream, &assignedPool); err != nil {
		return 0, 0, fmt.Errorf("look up stream: %w", err)
	}
	if assignedPool != pool {
		return 0, 0, fmt.Errorf("stream %q is already assigned to another pool", streamName)
	}
	return pool, stream, nil
}

const insertActivitySQL = `INSERT INTO activity_events
  (import_id, stream_id, pool_id, source_name, source_line, timestamp_original, timestamp_ns,
   daemon, kind, feature, licenses, user_name, host_name, checkout_data, denial_reason, denial_error,
   verbose_version, verbose_user, verbose_ip, verbose_pid, verbose_handle, verbose_in_use,
   verbose_out_of, verbose_project, verbose_flex_version, verbose_flex_revision, verbose_date,
   verbose_time, verbose_duplicate_group, raw_line, message)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func insertActivity(ctx context.Context, statement *sql.Stmt, importID, streamID, poolID int64, source string, event *goflexlm.Event) error {
	a := event.Activity
	var timestamp any
	if event.LogTime.Time != nil {
		timestamp = event.LogTime.Time.UTC().UnixNano()
	}
	verbose := goflexlm.VerboseFields{}
	if a.Verbose != nil {
		verbose = *a.Verbose
	}
	_, err := statement.ExecContext(ctx, importID, streamID, poolID, source, event.Line,
		event.LogTime.Original, timestamp, event.Daemon, event.Kind, a.Feature, a.Licenses,
		a.User, a.Host, a.CheckoutData, a.DenialReason, a.DenialError, verbose.Version,
		verbose.User, verbose.IP, verbose.PID, verbose.Handle, verbose.InUse, verbose.OutOf,
		verbose.ProjectName, verbose.FlexVersion, verbose.FlexRevision, verbose.Date,
		verbose.Time, verbose.IsDuplicateGroup, event.Raw, event.Message)
	return err
}
