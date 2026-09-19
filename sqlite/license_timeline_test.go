package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLicenseProjectionRebuildsAuthoritativeTimeline(t *testing.T) {
	s := licenseStore(t)
	ctx := context.Background()
	// Import out of order. The earliest empty file must know later identities as zero.
	third := licenseRequest(t, "FEATURE editor v 1 permanent uncounted")
	third.EffectiveFrom = third.EffectiveFrom.Add(4 * 24 * time.Hour)
	first := licenseRequest(t, "")
	second := licenseRequest(t, "FEATURE editor v 1 4-jan-2026 2\nFEATURE editor v 1 permanent 999\nINCREMENT editor v 1 3-jan-2026 3")
	second.EffectiveFrom = second.EffectiveFrom.Add(24 * time.Hour)
	for _, r := range []LicenseImportRequest{third, first, second} {
		if e := s.ImportLicenseFile(ctx, r); e != nil {
			t.Fatal(e)
		}
	}
	assertTimeline(t, s, []string{"01:0", "02:5", "03:2", "04:0", "05:uncounted"})
	// Replacing the middle snapshot removes its old expiration events.
	second.File = first.File
	if e := s.ImportLicenseFile(ctx, second); e != nil {
		t.Fatal(e)
	}
	assertTimeline(t, s, []string{"01:0", "05:uncounted"})
}

func assertTimeline(t *testing.T, s *Store, want []string) {
	t.Helper()
	rows, e := s.db.Query("SELECT effective_ns,licenses,uncounted FROM capacity_changes ORDER BY effective_ns")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var at int64
		var count sql.NullInt64
		var uncounted bool
		if e := rows.Scan(&at, &count, &uncounted); e != nil {
			t.Fatal(e)
		}
		value := "uncounted"
		if !uncounted {
			value = fmt.Sprint(count.Int64)
		}
		got = append(got, time.Unix(0, at).UTC().Format("02")+":"+value)
	}
	if e := rows.Err(); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("timeline=%v want=%v", got, want)
	}
}

func TestLicenseProjectionCoalescesAndPreservesProvenance(t *testing.T) {
	s := licenseStore(t)
	ctx := context.Background()
	first := licenseRequest(t, "FEATURE f v 1 2-jan-2026 2\nINCREMENT f v 1 permanent 2 START=2-jan-2026\nINCREMENT f v 1 3-jan-2026 uncounted START=2-jan-2026")
	if e := s.ImportLicenseFile(ctx, first); e != nil {
		t.Fatal(e)
	}
	second := licenseRequest(t, "FEATURE f v 1 permanent 2")
	second.EffectiveFrom = second.EffectiveFrom.Add(3 * 24 * time.Hour)
	if e := s.ImportLicenseFile(ctx, second); e != nil {
		t.Fatal(e)
	}
	assertTimeline(t, s, []string{"01:2", "02:uncounted", "03:2"})
	var sources int
	if e := s.db.QueryRow("SELECT COUNT(DISTINCT source_import_id) FROM capacity_changes").Scan(&sources); e != nil || sources != 1 {
		t.Fatalf("provenance=%d err=%v", sources, e)
	}
}

func TestLicenseProjectionOverflowLeavesSourceAndProjectionUnchanged(t *testing.T) {
	s := licenseStore(t)
	ctx := context.Background()
	first := licenseRequest(t, "FEATURE f v 1 permanent 2")
	if e := s.ImportLicenseFile(ctx, first); e != nil {
		t.Fatal(e)
	}
	tooLarge := licenseRequest(t, fmt.Sprintf("FEATURE f v 1 permanent %d\nINCREMENT f v 1 permanent 1", int(^uint(0)>>1)))
	if e := s.ImportLicenseFile(ctx, tooLarge); e == nil {
		t.Fatal("expected overflow")
	}
	assertTimeline(t, s, []string{"01:2"})
	var raw string
	s.db.QueryRow("SELECT document_json FROM license_imports").Scan(&raw)
	if !strings.Contains(raw, `"licenses":2`) {
		t.Fatalf("source=%s", raw)
	}
}
