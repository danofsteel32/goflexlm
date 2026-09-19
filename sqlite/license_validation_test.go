package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danofsteel32/goflexlm"
)

func TestLicenseSnapshotReplacementRollbackAndCancellation(t *testing.T) {
	s := licenseStore(t)
	r := licenseRequest(t, "FEATURE f v 1 permanent 2")
	if e := s.ImportLicenseFile(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	var before string
	s.db.QueryRow("SELECT document_json FROM license_imports").Scan(&before)
	if _, e := s.db.Exec("CREATE TRIGGER fail_projection BEFORE INSERT ON capacity_changes BEGIN SELECT RAISE(ABORT, 'injected'); END"); e != nil {
		t.Fatal(e)
	}
	changed := licenseRequest(t, "FEATURE f v 1 permanent 3")
	if e := s.ImportLicenseFile(context.Background(), changed); e == nil {
		t.Fatal("expected rollback")
	}
	var after string
	s.db.QueryRow("SELECT document_json FROM license_imports").Scan(&after)
	if after != before {
		t.Fatal("source changed on rollback")
	}
	var licenses int
	s.db.QueryRow("SELECT licenses FROM capacity_changes").Scan(&licenses)
	if licenses != 2 {
		t.Fatal("projection changed on rollback")
	}
	changed.Pool = "new"
	if e := s.ImportLicenseFile(context.Background(), changed); e == nil {
		t.Fatal("expected rollback")
	}
	var pools int
	s.db.QueryRow("SELECT COUNT(*) FROM license_pools").Scan(&pools)
	if pools != 1 {
		t.Fatal("pool created on rollback")
	}
	s.db.Exec("DROP TRIGGER fail_projection")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := s.ImportLicenseFile(ctx, changed); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancellation: %v", e)
	}
	// Err cancels at the second checkpoint, immediately after the lock.
	if e := s.ImportLicenseFile(&cancelAtLockContext{Context: context.Background()}, changed); !errors.Is(e, context.Canceled) {
		t.Fatalf("second checkpoint: %v", e)
	}
	s.db.QueryRow("SELECT COUNT(*) FROM license_pools").Scan(&pools)
	if pools != 1 {
		t.Fatal("pool created on cancellation")
	}
}

type cancelAtLockContext struct {
	context.Context
	checks int
}

func (c *cancelAtLockContext) Err() error {
	c.checks++
	if c.checks >= 2 {
		return context.Canceled
	}
	return nil
}

func TestLicenseSchemaRejectsV1WithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE schema_version(version INTEGER); INSERT INTO schema_version VALUES(1)"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if s, e := Open(context.Background(), path, OpenOptions{}); e == nil {
		s.Close()
		t.Fatal("accepted v1")
	} else if !strings.Contains(e.Error(), "recreated") {
		t.Fatal(e)
	}
	db, e = sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var n int
	if e = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&n); e != nil || n != 1 {
		t.Fatalf("changed v1 tables: %d %v", n, e)
	}
}

func TestLicenseSchemaEnforcesCapacityStates(t *testing.T) {
	s := licenseStore(t)
	r := licenseRequest(t, "")
	if e := s.ImportLicenseFile(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	for _, values := range []string{"-1,0", "NULL,0", "1,1", "NULL,2"} {
		_, e := s.db.Exec("INSERT INTO capacity_changes(pool_id,vendor,feature,effective_ns,licenses,uncounted,source_import_id) SELECT pool_id,'v','f',effective_ns," + values + ",id FROM license_imports")
		if e == nil {
			t.Fatalf("accepted invalid state %s", values)
		}
	}
}

func TestLicenseImportRejectsOverflowBeforeWriterLock(t *testing.T) {
	s := licenseStore(t)
	r := licenseRequest(t, fmt.Sprintf("FEATURE f v 1 permanent %d\nINCREMENT f v 1 permanent 1", int(^uint(0)>>1)))
	s.writer.Lock()
	defer s.writer.Unlock()
	// An already-cancelled context must not hide invalid capacity input.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.ImportLicenseFile(ctx, r)
	if err == nil || !strings.Contains(err.Error(), "overflow") || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error=%v", err)
	}
}

func licenseRequest(t *testing.T, input string) LicenseImportRequest {
	t.Helper()
	f, e := goflexlm.ParseLicenseFile(strings.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	return LicenseImportRequest{File: f, Pool: "p", SourceName: "test.lic", EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Timezone: "UTC"}
}
func licenseStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(context.Background(), ":memory:", OpenOptions{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestLicenseImportRejectsInvalidConstructedDocuments(t *testing.T) {
	for name, mutate := range map[string]func(*LicenseImportRequest){
		"line":                func(r *LicenseImportRequest) { r.File.Features[0].Line = 0 },
		"order":               func(r *LicenseImportRequest) { r.File.Features = append(r.File.Features, r.File.Features[0]) },
		"kind":                func(r *LicenseImportRequest) { r.File.Features[0].Kind = "bad" },
		"name":                func(r *LicenseImportRequest) { r.File.Features[0].Name = " " },
		"count nil":           func(r *LicenseImportRequest) { r.File.Features[0].Licenses = nil },
		"count zero":          func(r *LicenseImportRequest) { n := 0; r.File.Features[0].Licenses = &n },
		"count contradictory": func(r *LicenseImportRequest) { r.File.Features[0].Uncounted = true },
		"expiration":          func(r *LicenseImportRequest) { r.File.Features[0].Expiration = "31-feb-2026" },
		"start": func(r *LicenseImportRequest) {
			r.File.Features[0].Attributes = []goflexlm.LicenseAttribute{{Name: "START", Value: "permanent", HasValue: true}}
		},
		"attribute": func(r *LicenseImportRequest) {
			r.File.Features[0].Attributes = []goflexlm.LicenseAttribute{{Name: "A", Value: "not bare"}}
		},
		"server": func(r *LicenseImportRequest) {
			n := 0
			r.File.Servers = []goflexlm.LicenseServer{{Line: 1, Host: "h", HostID: "id", Port: &n}}
		},
		"vendor": func(r *LicenseImportRequest) {
			r.File.Vendors = []goflexlm.LicenseVendor{{Line: 1, Name: "v", Attributes: []goflexlm.LicenseAttribute{{Name: "FLAG"}}}}
		},
		"vendor port": func(r *LicenseImportRequest) {
			r.File.Vendors = []goflexlm.LicenseVendor{{Line: 1, Name: "v", Attributes: []goflexlm.LicenseAttribute{{Name: "PORT", Value: "-1", HasValue: true}}}}
		},
		"effective range":   func(r *LicenseImportRequest) { r.EffectiveFrom = time.Date(2500, 1, 1, 0, 0, 0, 0, time.UTC) },
		"date range":        func(r *LicenseImportRequest) { r.File.Features[0].Expiration = "1-jan-2500" },
		"zone local":        func(r *LicenseImportRequest) { r.Timezone = "Local" },
		"zone empty":        func(r *LicenseImportRequest) { r.Timezone = "" },
		"zone abbreviation": func(r *LicenseImportRequest) { r.Timezone = "EST" },
		"zone unknown":      func(r *LicenseImportRequest) { r.Timezone = "No/SuchZone" },
		"pool":              func(r *LicenseImportRequest) { r.Pool = " " },
		"source":            func(r *LicenseImportRequest) { r.SourceName = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := licenseStore(t)
			r := licenseRequest(t, "FEATURE f v 1 permanent 2")
			mutate(&r)
			err := s.ImportLicenseFile(context.Background(), r)
			if err == nil || !strings.HasPrefix(err.Error(), "import license file:") {
				t.Fatalf("error=%v", err)
			}
			var n int
			if e := s.db.QueryRow("SELECT COUNT(*) FROM license_pools").Scan(&n); e != nil || n != 0 {
				t.Fatalf("mutation: %d %v", n, e)
			}
		})
	}
}

func TestLicenseImportPersistsResolvedDatesAndCanonicalCorpus(t *testing.T) {
	s := licenseStore(t)
	data, e := os.ReadFile("../testdata/licenses/conforming.lic")
	if e != nil {
		t.Fatal(e)
	}
	r := licenseRequest(t, string(data))
	r.Timezone = "America/New_York"
	if e = s.ImportLicenseFile(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	var raw, dates string
	if e = s.db.QueryRow("SELECT document_json,resolved_dates_json FROM license_imports").Scan(&raw, &dates); e != nil {
		t.Fatal(e)
	}
	expected, _ := json.Marshal(r.File)
	if raw != string(expected) {
		t.Fatalf("document=%s", raw)
	}
	var resolved []struct {
		Start      *int64 `json:"start_ns"`
		Expiration *int64 `json:"expiration_ns"`
	}
	if e = json.Unmarshal([]byte(dates), &resolved); e != nil {
		t.Fatal(e)
	}
	want := time.Date(2026, 12, 31, 5, 0, 0, 0, time.UTC).UnixNano()
	if len(resolved) != 1 || resolved[0].Start != nil || resolved[0].Expiration == nil || *resolved[0].Expiration != want {
		t.Fatalf("dates=%s", dates)
	}
}

func TestLicenseImportNormalizesAndReplacesAtSameInstant(t *testing.T) {
	s := licenseStore(t)
	ctx := context.Background()
	r := licenseRequest(t, "FEATURE f v 1 1-apr-2026 1 START=8-mar-2026")
	r.Timezone = "America/New_York"
	if e := s.ImportLicenseFile(ctx, r); e != nil {
		t.Fatal(e)
	}
	var dates string
	if e := s.db.QueryRow("SELECT resolved_dates_json FROM license_imports").Scan(&dates); e != nil {
		t.Fatal(e)
	}
	want := fmt.Sprintf("[{\"start_ns\":%d,\"expiration_ns\":%d}]", time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC).UnixNano(), time.Date(2026, 4, 1, 4, 0, 0, 0, time.UTC).UnixNano())
	if dates != want {
		t.Fatalf("resolved dates=%s want=%s", dates, want)
	}
	r.File.Servers = nil
	r.File.Vendors = nil
	r.File.Features = nil
	r.SourceName = "replacement"
	if e := s.ImportLicenseFile(ctx, r); e != nil {
		t.Fatal(e)
	}
	var n int
	var name, document string
	if e := s.db.QueryRow("SELECT COUNT(*),source_name,document_json,resolved_dates_json FROM license_imports").Scan(&n, &name, &document, &dates); e != nil {
		t.Fatal(e)
	}
	if n != 1 || name != "replacement" || document != `{"servers":[],"vendors":[],"features":[],"use_server":false}` || dates != "[]" {
		t.Fatalf("replacement=%d %s %s %s", n, name, document, dates)
	}
}
