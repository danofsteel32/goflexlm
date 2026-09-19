package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danofsteel32/goflexlm"
)

func TestImportDeriveAndReports(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "usage.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	input := strings.Join([]string{
		`09:00:00 (vendor) OUT: "editor" old@host`,
		`this is malformed`,
		`2026-09-05T00:00:00Z (vendor) OUT: "editor" user@host (2 licenses)`,
		`2026-09-05T00:30:00Z (vendor) QUEUED: "editor" user@host (2 licenses)`,
		`2026-09-05T01:00:00Z (vendor) IN: "editor" user@host`,
		`2026-09-05T01:00:00Z (vendor) DEQUEUED: "editor" user@host`,
		`2026-09-05T01:15:00Z (vendor) DENIED: 4 "editor" to user@host`,
		`2026-09-05T01:30:00Z (vendor) DEQUEUED: "editor" user@host`,
		`2026-09-05T02:00:00Z (vendor) IN: "editor" user@host`,
		`2026-09-05T02:30:00Z (vendor) status message`,
	}, "\r\n")
	var diagnostics int
	result, err := store.Import(ctx, ImportRequest{
		Reader: strings.NewReader(input), Pool: "engineering", Stream: "server-a", SourceName: "a.log",
		OnDiagnostic: func(diagnostic goflexlm.Diagnostic) error {
			diagnostics++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ActivityCount != 8 || result.OmittedMessageCount != 1 || result.UnresolvedTimestampCount != 1 || diagnostics != 1 {
		t.Fatalf("unexpected import result: %+v, callbacks=%d", result, diagnostics)
	}
	if result.LineCount != 10 || result.ByteCount != int64(len(input)) {
		t.Fatalf("source counts = %d lines, %d bytes", result.LineCount, result.ByteCount)
	}

	duplicate, err := store.Import(ctx, ImportRequest{Reader: strings.NewReader(input), Pool: "engineering", Stream: "server-a", SourceName: "copy.log"})
	if err != nil || !duplicate.Duplicate || duplicate.Digest != result.Digest {
		t.Fatalf("duplicate = %+v, %v", duplicate, err)
	}
	var imports int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM imports`).Scan(&imports); err != nil || imports != 1 {
		t.Fatalf("imports = %d, %v", imports, err)
	}

	from := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	to := from.Add(3 * time.Hour)
	request := licenseRequest(t, "FEATURE editor vendor 1 permanent 3")
	request.Pool = "engineering"
	request.EffectiveFrom = from
	if err := store.ImportLicenseFile(ctx, request); err != nil {
		t.Fatal(err)
	}
	capacity, err := store.Capacity(ctx, AnalyticsQuery{Pool: "engineering", Feature: "editor", From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if len(capacity.Buckets) != 1 {
		t.Fatalf("capacity buckets = %+v", capacity.Buckets)
	}
	bucket := capacity.Buckets[0]
	if bucket.LowerPeak != 2 || bucket.UpperPeak != 2 || bucket.LowerUsedSeatNanoseconds != int64(3*time.Hour) {
		t.Fatalf("capacity = %+v", bucket)
	}
	if capacity.Quality.UnresolvedTime != 1 || bucket.LowerUnusedSeatNanoseconds == nil || *bucket.LowerUnusedSeatNanoseconds != int64(6*time.Hour) {
		t.Fatalf("capacity quality = %+v, bucket = %+v", capacity.Quality, bucket)
	}

	denials, err := store.Denials(ctx, AnalyticsQuery{Pool: "engineering", Feature: "editor", From: from, To: to})
	if err != nil || len(denials.Buckets) != 1 || denials.Buckets[0].Events != 1 || denials.Buckets[0].Licenses != 4 {
		t.Fatalf("denials = %+v, %v", denials, err)
	}
	queues, err := store.Queueing(ctx, AnalyticsQuery{Pool: "engineering", Feature: "editor", From: from, To: to})
	if err != nil || len(queues.Buckets) != 1 {
		t.Fatalf("queues = %+v, %v", queues, err)
	}
	queue := queues.Buckets[0]
	if queue.QueuedEvents != 1 || queue.QueuedLicenses != 2 || queue.MaximumDepth != 2 || queue.CompletedWaits != 2 || queue.P50Wait == nil || *queue.P50Wait != 30*time.Minute || queue.P95Wait == nil || *queue.P95Wait != time.Hour {
		t.Fatalf("queue = %+v", queue)
	}
	otherStream, err := store.Import(ctx, ImportRequest{Reader: strings.NewReader(input), Pool: "engineering", Stream: "server-b", SourceName: "same-bytes.log"})
	if err != nil || otherStream.Duplicate {
		t.Fatalf("identical source in another stream = %+v, %v", otherStream, err)
	}
}

func TestChronologicalImportsContinueOpenAllocations(t *testing.T) {
	t.Parallel()
	store, err := Open(context.Background(), ":memory:", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	files := []string{
		`2026-09-05T00:00:00Z (v) OUT: f u@h (2 licenses)`,
		`2026-09-05T01:00:00Z (v) IN: f u@h`,
	}
	for index, contents := range files {
		_, err := store.Import(context.Background(), ImportRequest{
			Reader: strings.NewReader(contents), Pool: "p", Stream: "s", SourceName: string(rune('a' + index)),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var closed, open int
	if err := store.db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN state='closed' THEN quantity ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN state='open' THEN quantity ELSE 0 END),0)
		FROM sessions`).Scan(&closed, &open); err != nil {
		t.Fatal(err)
	}
	if closed != 1 || open != 1 {
		t.Fatalf("closed=%d open=%d", closed, open)
	}
}

func TestImportCallbackFailureRollsBack(t *testing.T) {
	t.Parallel()
	store, err := Open(context.Background(), ":memory:", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	want := errors.New("stop")
	_, err = store.Import(context.Background(), ImportRequest{
		Reader: strings.NewReader("bad line\n"), Pool: "p", Stream: "s", SourceName: "bad.log",
		OnDiagnostic: func(_ goflexlm.Diagnostic) error { return want },
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	var imports int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM imports`).Scan(&imports); err != nil || imports != 0 {
		t.Fatalf("imports = %d, %v", imports, err)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "newer.db")
	store, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE schema_version SET version=999`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path, OpenOptions{}); err == nil || !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("error = %v", err)
	}
}
