package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danofsteel32/goflexlm"
)

func TestCapacityBucketJSONIncludesVendorAndUncounted(t *testing.T) {
	data, err := json.Marshal(CapacityBucket{Vendor: "acme", Feature: "editor", Uncounted: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"vendor":"acme"`) || !strings.Contains(string(data), `"uncounted":true`) {
		t.Fatalf("JSON = %s", data)
	}
}

func TestImportLicenseFileStoresValidatedSnapshot(t *testing.T) {
	document, err := goflexlm.ParseLicenseFile(strings.NewReader("FEATURE editor acme 1.0 permanent 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), ":memory:", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	err = store.ImportLicenseFile(context.Background(), LicenseImportRequest{
		File:          document,
		Pool:          "engineering",
		SourceName:    "licenses.lic",
		EffectiveFrom: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Timezone:      "UTC",
	})
	if err != nil {
		t.Fatalf("ImportLicenseFile() error = %v", err)
	}

	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM license_imports").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("license import count = %d, want 1", count)
	}
	var vendor, feature string
	var effective int64
	var licenses int
	if err := store.db.QueryRow("SELECT vendor, feature, effective_ns, licenses FROM capacity_changes").Scan(&vendor, &feature, &effective, &licenses); err != nil {
		t.Fatal(err)
	}
	if vendor != "acme" || feature != "editor" || effective != time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).UnixNano() || licenses != 2 {
		t.Fatalf("capacity change = vendor=%q feature=%q effective=%d licenses=%d", vendor, feature, effective, licenses)
	}
}

func TestImportLicenseFileCoalescesFeatureAndIncrementCapacity(t *testing.T) {
	document, err := goflexlm.ParseLicenseFile(strings.NewReader("FEATURE editor acme 1.0 permanent 2\nINCREMENT editor acme 1.0 permanent 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), ":memory:", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.ImportLicenseFile(context.Background(), LicenseImportRequest{File: document, Pool: "engineering", SourceName: "licenses.lic", EffectiveFrom: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Timezone: "UTC"})
	if err != nil {
		t.Fatalf("ImportLicenseFile() error = %v", err)
	}
	var licenses int
	if err := store.db.QueryRow("SELECT licenses FROM capacity_changes WHERE vendor='acme' AND feature='editor'").Scan(&licenses); err != nil {
		t.Fatal(err)
	}
	if licenses != 5 {
		t.Fatalf("licenses = %d, want 5", licenses)
	}
}

func TestCapacityReportsVendorCapacityWithoutUsage(t *testing.T) {
	document, err := goflexlm.ParseLicenseFile(strings.NewReader("FEATURE editor acme 1.0 permanent 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), ":memory:", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := store.ImportLicenseFile(context.Background(), LicenseImportRequest{File: document, Pool: "engineering", SourceName: "licenses.lic", EffectiveFrom: start, Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	report, err := store.Capacity(context.Background(), AnalyticsQuery{Pool: "engineering", From: start, To: start.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Buckets) != 1 || report.Buckets[0].Vendor != "acme" || report.Buckets[0].Feature != "editor" || report.Buckets[0].Purchased == nil || *report.Buckets[0].Purchased != 2 {
		t.Fatalf("buckets = %+v", report.Buckets)
	}
}
