package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCapacityVendorStateMatrixAndBoundaries(t *testing.T) {
	s := licenseStore(t)
	ctx := context.Background()
	r := licenseRequest(t, "FEATURE editor acme 1 permanent 1\nFEATURE editor unlimited 1 permanent uncounted\nFEATURE editor zero 1 1-jan-2026 3\nFEATURE other capacityonly 1 permanent 5")
	if e := s.ImportLicenseFile(ctx, r); e != nil {
		t.Fatal(e)
	}
	var log strings.Builder
	for _, vendor := range []string{"missing", "unlimited", "zero", "acme"} {
		fmt.Fprintf(&log, "2026-01-01T00:00:00Z (%s) OUT: editor u@h (2 licenses)\n2026-01-01T01:00:00Z (%s) IN: editor u@h (2 licenses)\n", vendor, vendor)
	}
	if _, e := s.Import(ctx, ImportRequest{Reader: strings.NewReader(log.String()), Pool: "p", Stream: "s", SourceName: "usage"}); e != nil {
		t.Fatal(e)
	}
	q := AnalyticsQuery{Pool: "p", From: r.EffectiveFrom, To: r.EffectiveFrom.Add(2 * time.Hour), Bucket: BucketHour}
	report, e := s.Capacity(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	if len(report.Buckets) != 10 {
		t.Fatalf("buckets=%+v", report.Buckets)
	}
	wantVendors := []string{"acme", "capacityonly", "missing", "unlimited", "zero"}
	for i, vendor := range wantVendors {
		b := report.Buckets[2*i]
		next := report.Buckets[2*i+1]
		if b.Vendor != vendor || next.Vendor != vendor || !b.From.Before(next.From) {
			t.Fatalf("order: %+v", report.Buckets)
		}
		switch vendor {
		case "missing":
			if b.Purchased != nil || b.Uncounted || b.UpperPeak != 2 {
				t.Fatalf("missing=%+v", b)
			}
		case "unlimited":
			if !b.Uncounted || b.Purchased != nil || b.UpperPeak != 2 || b.UpperUsedSeatNanoseconds != int64(2*time.Hour) || b.UpperMinimumHeadroom != nil || b.UpperUnusedSeatNanoseconds != nil || b.UpperAverageHeadroom != nil || b.UpperSaturatedNanoseconds != 0 {
				t.Fatalf("unlimited=%+v", b)
			}
		case "zero":
			if b.Purchased == nil || *b.Purchased != 0 {
				t.Fatalf("zero=%+v", b)
			}
		}
	}
	if report.Quality.MissingEntitlement != 2 || report.Quality.UncoveredNanoseconds != int64(2*time.Hour) || !report.Quality.UsageAboveEntitlement {
		t.Fatalf("quality=%+v", report.Quality)
	}
	q.Feature = "editor"
	filtered, e := s.Capacity(ctx, q)
	if e != nil || len(filtered.Buckets) != 8 {
		t.Fatalf("filtered=%+v err=%v", filtered, e)
	}
}

func TestCapacitySplitsAtChangesInsideRange(t *testing.T) {
	s := licenseStore(t)
	r := licenseRequest(t, "FEATURE f v 1 permanent 2 START=2-jan-2026")
	if e := s.ImportLicenseFile(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	report, e := s.Capacity(context.Background(), AnalyticsQuery{Pool: "p", From: r.EffectiveFrom.Add(-time.Hour), To: r.EffectiveFrom.Add(48 * time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if len(report.Buckets) != 3 || report.Buckets[0].Purchased != nil || report.Buckets[1].Purchased == nil || *report.Buckets[1].Purchased != 0 || report.Buckets[2].Purchased == nil || *report.Buckets[2].Purchased != 2 {
		t.Fatalf("buckets=%+v", report.Buckets)
	}
}

func TestCapacityRejectsSeatTimeOverflow(t *testing.T) {
	s := licenseStore(t)
	r := licenseRequest(t, fmt.Sprintf("FEATURE f v 1 permanent %d", int(^uint(0)>>1)))
	if e := s.ImportLicenseFile(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	_, e := s.Capacity(context.Background(), AnalyticsQuery{Pool: "p", From: r.EffectiveFrom, To: r.EffectiveFrom.Add(time.Second)})
	if e == nil || !strings.Contains(e.Error(), "capacity report:") || !strings.Contains(e.Error(), "overflow") {
		t.Fatalf("error=%v", e)
	}
}

func TestCapacityRejectsOverlappingMissingCoverageOverflow(t *testing.T) {
	s := licenseStore(t)
	ctx := context.Background()
	log := "1900-01-01T00:00:00Z (a) OUT: f u@h\n1900-01-01T00:00:01Z (a) IN: f u@h\n1900-01-01T00:00:00Z (b) OUT: f u@h\n1900-01-01T00:00:01Z (b) IN: f u@h"
	if _, e := s.Import(ctx, ImportRequest{Reader: strings.NewReader(log), Pool: "p", Stream: "s", SourceName: "log"}); e != nil {
		t.Fatal(e)
	}
	_, e := s.Capacity(ctx, AnalyticsQuery{Pool: "p", From: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)})
	if e == nil || !strings.Contains(e.Error(), "overflow") {
		t.Fatalf("error=%v", e)
	}
}

func TestCapacityWithSingleConnectionAndUsageOnlyIdentity(t *testing.T) {
	s, e := Open(context.Background(), ":memory:", OpenOptions{MaxOpenConns: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	log := "2026-01-01T00:00:00Z (v) OUT: f u@h\n2026-01-01T01:00:00Z (v) IN: f u@h"
	if _, e := s.Import(ctx, ImportRequest{Reader: strings.NewReader(log), Pool: "p", Stream: "s", SourceName: "log"}); e != nil {
		t.Fatal(e)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, e := s.Capacity(ctx, AnalyticsQuery{Pool: "p", From: start, To: start.Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Buckets) != 1 || r.Buckets[0].Vendor != "v" || r.Buckets[0].Purchased != nil || r.Quality.MissingEntitlement != 1 || r.Quality.UncoveredNanoseconds != int64(time.Hour) {
		t.Fatalf("report=%+v", r)
	}
}
