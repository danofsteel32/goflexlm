package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danofsteel32/goflexlm"
	store "github.com/danofsteel32/goflexlm/sqlite"
)

func demoStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "usage.db"), store.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, day := range []string{"2026-09-01", "2026-09-02"} {
		raw, err := os.ReadFile("../../testdata/sqlite-demo/licenses-" + day + ".lic")
		if err != nil {
			t.Fatal(err)
		}
		doc, err := goflexlm.ParseLicenseFile(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		at, _ := time.Parse(time.DateOnly, day)
		if err := db.ImportLicenseFile(ctx, store.LicenseImportRequest{File: doc, Pool: "studio", SourceName: day, EffectiveFrom: at, Timezone: "UTC"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range []struct{ name, stream string }{
		{"server-a-2026-09-01.log", "a"}, {"server-a-2026-09-02.log", "a"}, {"server-b-2026-09.log", "b"}, {"unmatched-vendor.log", "unmatched"},
	} {
		raw, err := os.ReadFile("../../testdata/sqlite-demo/" + fixture.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Import(ctx, store.ImportRequest{Reader: strings.NewReader(string(raw)), Pool: "studio", Stream: fixture.stream, SourceName: fixture.name}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func fixedNow() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) }
func demoFilters() filters {
	return filters{Pool: "studio", From: "2026-09-01", To: "2026-09-03", Timezone: "UTC"}
}

func TestDashboardReports(t *testing.T) {
	d, err := newDashboard(demoStore(t), "studio", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	f := demoFilters()
	target := "/?" + filterValues(f).Encode()
	response := httptest.NewRecorder()
	d.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body)
	}
	for _, text := range []string{"License overview", "Capacity pressure", "Check license coverage", "Uncounted", "4–6", "Denied requests", "Queue waits", "Data quality", "Licensed number reached", "10m0s / 40m0s"} {
		if !strings.Contains(response.Body.String(), text) {
			t.Errorf("missing %q", text)
		}
	}
	exported := httptest.NewRecorder()
	d.ServeHTTP(exported, httptest.NewRequest(http.MethodGet, target+"&format=json", nil))
	if exported.Code != 200 {
		t.Fatalf("export: %s", exported.Body)
	}
	var data reportData
	if err := json.Unmarshal(exported.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Capacity.Quality.UnresolvedTime != 1 || data.Capacity.Quality.MissingEntitlement == 0 {
		t.Fatalf("quality: %+v", data.Capacity.Quality)
	}
	summaries := summarize(data.Capacity.Buckets, f)
	for _, summary := range summaries {
		if summary.Vendor == "acme" && summary.Feature == "editor" {
			if summary.Capacity != "4–6" || summary.LowerPeak != 4 || summary.UpperPeak != 4 || summary.LowerSaturated != .75 || summary.UpperSaturated != .75 {
				t.Errorf("incorrect editor summary: %+v", summary)
			}
		}
	}
	// Filtering includes all vendors, preserving exact vendor/feature capacity identity.
	f.Feature = "editor"
	selected := httptest.NewRecorder()
	d.ServeHTTP(selected, httptest.NewRequest(http.MethodGet, "/?"+filterValues(f).Encode()+"&format=json", nil))
	if err := json.Unmarshal(selected.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	vendors := map[string]bool{}
	for _, b := range data.Capacity.Buckets {
		if b.Feature != "editor" {
			t.Fatal("feature filter not applied")
		}
		vendors[b.Vendor] = true
	}
	if !vendors["acme"] || !vendors["beta"] {
		t.Fatalf("vendors: %v", vendors)
	}
	for _, b := range data.Denials.Buckets {
		if b.Feature != "editor" {
			t.Fatal("denial filter not applied")
		}
	}
	for _, b := range data.Queues.Buckets {
		if b.Feature != "editor" {
			t.Fatal("queue filter not applied")
		}
	}
}

func TestParseFilters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*filters)
	}{
		{"pool", func(f *filters) { f.Pool = " " }},
		{"invalid date", func(f *filters) { f.From = "2026-02-30" }},
		{"reversed", func(f *filters) { f.To = f.From }},
		{"timezone", func(f *filters) { f.Timezone = "invalid/zone" }},
		{"empty timezone", func(f *filters) { f.Timezone = "" }},
		{"too long", func(f *filters) { f.To = "2028-09-01" }},
		{"overflow", func(f *filters) { f.From = "2300-09-01"; f.To = "2300-09-02" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := demoFilters()
			tc.change(&f)
			if _, err := parseFilters(f); err == nil {
				t.Fatal("accepted invalid filters")
			}
		})
	}
	f := filters{Pool: "studio", From: "2026-03-08", To: "2026-03-09", Timezone: "America/New_York"}
	q, err := parseFilters(f)
	if err != nil {
		t.Fatal(err)
	}
	if q.To.Sub(q.From) != 23*time.Hour {
		t.Fatalf("DST day: %v", q.To.Sub(q.From))
	}
}

type failingReports struct{ err error }

func (f failingReports) Capacity(context.Context, store.AnalyticsQuery) (store.CapacityReport, error) {
	return store.CapacityReport{}, f.err
}
func (f failingReports) Denials(context.Context, store.AnalyticsQuery) (store.DenialReport, error) {
	return store.DenialReport{}, f.err
}
func (f failingReports) Queueing(context.Context, store.AnalyticsQuery) (store.QueueReport, error) {
	return store.QueueReport{}, f.err
}

func TestDashboardFailuresAndHTTP(t *testing.T) {
	d, err := newDashboard(failingReports{errors.New("database unavailable <script>alert(1)</script>")}, "studio", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, target string
		status         int
	}{
		{"GET", "/", 500}, {"POST", "/", 405}, {"GET", "/missing", 404},
		{"GET", "/?pool=studio", 400}, {"GET", "/dashboard.css", 200}, {"HEAD", "/dashboard.css", 200},
	} {
		t.Run(tc.method+tc.target, func(t *testing.T) {
			response := httptest.NewRecorder()
			d.ServeHTTP(response, httptest.NewRequest(tc.method, tc.target, nil))
			if response.Code != tc.status {
				t.Fatalf("status %d", response.Code)
			}
			if strings.Contains(response.Body.String(), "<script>") {
				t.Fatal("unescaped database error")
			}
			if tc.method == "HEAD" && response.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
			if response.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("missing CSP")
			}
		})
	}
	response := httptest.NewRecorder()
	d.ServeHTTP(response, httptest.NewRequest("GET", "/?format=json", nil))
	if response.Code != 400 || !json.Valid(response.Body.Bytes()) {
		t.Fatal("invalid JSON error response")
	}
}

func TestEmptyAndEscapedFilters(t *testing.T) {
	db := demoStore(t)
	d, err := newDashboard(db, "studio", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	f := demoFilters()
	f.Feature = `<script>alert("x")</script>`
	response := httptest.NewRecorder()
	d.ServeHTTP(response, httptest.NewRequest("GET", "/?"+filterValues(f).Encode(), nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "No usage or capacity to show") {
		t.Fatalf("%d: %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), f.Feature) {
		t.Fatal("unescaped feature")
	}
	if !strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatal("filter not preserved")
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		status int
	}{
		{nil, 2}, {[]string{"--db", "missing", "--pool", "studio", "extra"}, 2},
		{[]string{"--db", "missing", "--pool", "studio", "--listen", "0.0.0.0:8080"}, 2},
		{[]string{"--db", filepath.Join(t.TempDir(), "missing.db"), "--pool", "studio"}, 1},
	} {
		if got := run(context.Background(), tc.args, io.Discard, io.Discard); got != tc.status {
			t.Errorf("%v: status %d, want %d", tc.args, got, tc.status)
		}
	}
}

func TestFeatureLinksEscapeValues(t *testing.T) {
	from := fixedNow()
	to := from.Add(time.Hour)
	n := 0
	buckets := []store.CapacityBucket{{Vendor: "vendor/a", Feature: "feature&pool=other", From: from, To: to, Purchased: &n, LowerMinimumHeadroom: &n, UpperMinimumHeadroom: &n}}
	summary := summarize(buckets, demoFilters())[0]
	link, err := url.Parse(summary.Link)
	if err != nil {
		t.Fatal(err)
	}
	if link.Query().Get("pool") != "studio" || link.Query().Get("feature") != buckets[0].Feature || summary.Capacity != "0" {
		t.Fatalf("summary: %+v", summary)
	}
}
