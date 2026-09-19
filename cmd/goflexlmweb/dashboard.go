package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	store "github.com/danofsteel32/goflexlm/sqlite"
)

//go:embed dashboard.html dashboard.css
var assets embed.FS

type reports interface {
	Capacity(context.Context, store.AnalyticsQuery) (store.CapacityReport, error)
	Denials(context.Context, store.AnalyticsQuery) (store.DenialReport, error)
	Queueing(context.Context, store.AnalyticsQuery) (store.QueueReport, error)
}

type dashboard struct {
	store reports
	pool  string
	now   func() time.Time
	page  *template.Template
	css   []byte
}

type filters struct{ Pool, Feature, From, To, Timezone string }
type reportData struct {
	Capacity store.CapacityReport `json:"capacity"`
	Denials  store.DenialReport   `json:"denials"`
	Queues   store.QueueReport    `json:"queues"`
}
type featureSummary struct {
	Vendor, Feature, Capacity, Status, Tone string
	LowerPeak, UpperPeak                    int
	MinCapacity, MaxCapacity                int
	Finite, Missing, Uncounted              bool
	LowerHeadroom, UpperHeadroom            int
	LowerSaturated, UpperSaturated          float64
	PeakPercent                             float64
	Link                                    string
}
type pageData struct {
	Filters                           filters
	Error, Export                     string
	Loaded                            bool
	Reports                           reportData
	Features                          []featureSummary
	Pressure, Missing, Denied, Queued int
}

func newDashboard(db reports, pool string, now func() time.Time) (*dashboard, error) {
	page, err := template.New("dashboard.html").Funcs(template.FuncMap{
		"duration": func(v *time.Duration) string {
			if v == nil {
				return "—"
			}
			return v.Round(time.Second).String()
		},
		"hours": func(v int64) string { return fmt.Sprintf("%.2f h", float64(v)/float64(time.Hour)) },
		"count": func(v *int) string {
			if v == nil {
				return "Unknown"
			}
			return fmt.Sprint(*v)
		},
		"stamp": func(t time.Time) string { return t.UTC().Format("Jan 02, 15:04") },
	}).ParseFS(assets, "dashboard.html")
	if err != nil {
		return nil, fmt.Errorf("load dashboard: %w", err)
	}
	css, err := assets.ReadFile("dashboard.css")
	if err != nil {
		return nil, err
	}
	return &dashboard{store: db, pool: pool, now: now, page: page, css: css}, nil
}

func (d *dashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/dashboard.css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		if r.Method != http.MethodHead {
			_, _ = w.Write(d.css)
		}
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	now := d.now().UTC()
	values := r.URL.Query()
	f := filters{Pool: d.pool, From: now.AddDate(0, 0, -29).Format(time.DateOnly), To: now.AddDate(0, 0, 1).Format(time.DateOnly), Timezone: "UTC"}
	if len(values) > 0 {
		f = filters{Pool: values.Get("pool"), Feature: values.Get("feature"), From: values.Get("from"), To: values.Get("to"), Timezone: values.Get("timezone")}
	}
	data := pageData{Filters: f}
	q, err := parseFilters(f)
	status := http.StatusOK
	if err != nil {
		data.Error = err.Error()
		status = http.StatusBadRequest
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		data.Reports, err = d.load(ctx, q)
		if err != nil {
			data.Error = "Could not load reports. Check the pool name and database availability. " + err.Error()
			status = http.StatusInternalServerError
		} else {
			data.Loaded = true
			data.Features = summarize(data.Reports.Capacity.Buckets, f)
			for _, feature := range data.Features {
				if feature.UpperSaturated > 0 {
					data.Pressure++
				}
				if feature.Missing {
					data.Missing++
				}
			}
			for _, b := range data.Reports.Denials.Buckets {
				data.Denied += b.Events
			}
			for _, b := range data.Reports.Queues.Buckets {
				data.Queued += b.QueuedEvents
			}
			params := filterValues(f)
			params.Set("format", "json")
			data.Export = "/?" + params.Encode()
		}
	}
	if values.Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		if data.Loaded {
			w.Header().Set("Content-Disposition", `attachment; filename="license-reports.json"`)
		}
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			if data.Loaded {
				_ = json.NewEncoder(w).Encode(data.Reports)
			} else {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": data.Error})
			}
		}
		return
	}
	var body bytes.Buffer
	if err := d.page.Execute(&body, data); err != nil {
		http.Error(w, "Could not render dashboard", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

func parseFilters(f filters) (store.AnalyticsQuery, error) {
	if strings.TrimSpace(f.Pool) == "" {
		return store.AnalyticsQuery{}, fmt.Errorf("Enter a license pool.")
	}
	if f.Timezone == "" {
		return store.AnalyticsQuery{}, fmt.Errorf("Enter UTC or an IANA timezone.")
	}
	zone, err := time.LoadLocation(f.Timezone)
	if err != nil {
		return store.AnalyticsQuery{}, fmt.Errorf("Timezone must be UTC or an IANA name such as America/New_York.")
	}
	from, e1 := time.ParseInLocation(time.DateOnly, f.From, zone)
	to, e2 := time.ParseInLocation(time.DateOnly, f.To, zone)
	if e1 != nil || e2 != nil || !from.Before(to) {
		return store.AnalyticsQuery{}, fmt.Errorf("Choose valid dates with the end after the start. The end date is excluded.")
	}
	if to.After(from.AddDate(0, 0, 366)) || from.Year() < 1700 || to.Year() > 2200 {
		return store.AnalyticsQuery{}, fmt.Errorf("Choose at most 366 days between the years 1700 and 2200.")
	}
	return store.AnalyticsQuery{Pool: f.Pool, Feature: f.Feature, From: from, To: to, Timezone: zone}, nil
}

func (d *dashboard) load(ctx context.Context, q store.AnalyticsQuery) (reportData, error) {
	var result reportData
	var err error
	// Capacity retains daily and entitlement boundaries. Queue percentiles are
	// computed over the whole range; averaging daily percentiles is misleading.
	capacityQuery := q
	capacityQuery.Bucket = store.BucketDay
	if result.Capacity, err = d.store.Capacity(ctx, capacityQuery); err != nil {
		return reportData{}, err
	}
	if result.Denials, err = d.store.Denials(ctx, q); err != nil {
		return reportData{}, err
	}
	if result.Queues, err = d.store.Queueing(ctx, q); err != nil {
		return reportData{}, err
	}
	return result, nil
}

func filterValues(f filters) url.Values {
	return url.Values{"pool": {f.Pool}, "feature": {f.Feature}, "from": {f.From}, "to": {f.To}, "timezone": {f.Timezone}}
}

func summarize(buckets []store.CapacityBucket, f filters) []featureSummary {
	type identity struct{ vendor, feature string }
	groups := map[identity]*featureSummary{}
	for _, b := range buckets {
		key := identity{b.Vendor, b.Feature}
		s := groups[key]
		if s == nil {
			params := filterValues(f)
			params.Set("feature", b.Feature)
			s = &featureSummary{Vendor: b.Vendor, Feature: b.Feature, Link: "/?" + params.Encode() + "#capacity"}
			groups[key] = s
		}
		s.LowerPeak = max(s.LowerPeak, b.LowerPeak)
		s.UpperPeak = max(s.UpperPeak, b.UpperPeak)
		s.LowerSaturated += float64(b.LowerSaturatedNanoseconds) / float64(time.Hour)
		s.UpperSaturated += float64(b.UpperSaturatedNanoseconds) / float64(time.Hour)
		if b.Uncounted {
			s.Uncounted = true
		} else if b.Purchased == nil {
			s.Missing = true
		} else {
			if !s.Finite {
				s.MinCapacity, s.MaxCapacity = *b.Purchased, *b.Purchased
				s.LowerHeadroom, s.UpperHeadroom = *b.LowerMinimumHeadroom, *b.UpperMinimumHeadroom
			}
			s.Finite = true
			s.MinCapacity = min(s.MinCapacity, *b.Purchased)
			s.MaxCapacity = max(s.MaxCapacity, *b.Purchased)
			s.LowerHeadroom = min(s.LowerHeadroom, *b.LowerMinimumHeadroom)
			s.UpperHeadroom = min(s.UpperHeadroom, *b.UpperMinimumHeadroom)
			if *b.Purchased > 0 {
				s.PeakPercent = max(s.PeakPercent, 100*float64(b.UpperPeak)/float64(*b.Purchased))
			}
		}
	}
	result := make([]featureSummary, 0, len(groups))
	for _, s := range groups {
		s.Capacity = "Unknown"
		if s.Finite {
			s.Capacity = fmt.Sprint(s.MinCapacity)
			if s.MinCapacity != s.MaxCapacity {
				s.Capacity += "–" + fmt.Sprint(s.MaxCapacity)
			}
		}
		if s.Uncounted {
			if s.Finite {
				s.Capacity += " / uncounted"
			} else {
				s.Capacity = "Uncounted"
			}
		}
		if s.Missing && (s.Finite || s.Uncounted) {
			s.Capacity += " / unknown"
		}
		s.Status, s.Tone = "Headroom observed", "calm"
		if s.Uncounted {
			s.Status, s.Tone = "Uncounted capacity", "neutral"
		}
		if s.UpperSaturated > 0 {
			s.Status, s.Tone = "Capacity pressure", "warning"
		}
		if s.Missing {
			s.Status, s.Tone = "Check license coverage", "warning"
		}
		result = append(result, *s)
	}
	sort.Slice(result, func(i, j int) bool {
		if (result[i].Tone == "warning") != (result[j].Tone == "warning") {
			return result[i].Tone == "warning"
		}
		if result[i].Vendor != result[j].Vendor {
			return result[i].Vendor < result[j].Vendor
		}
		return result[i].Feature < result[j].Feature
	})
	return result
}
