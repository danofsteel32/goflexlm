// Package sqlite stores parsed FlexNet activity and derives auditable usage
// and queue sessions in SQLite.
package sqlite

import (
	"io"
	"time"

	"github.com/danofsteel32/goflexlm"
)

// OpenOptions controls database connection behavior.
type OpenOptions struct {
	// MaxOpenConns is the maximum number of concurrent SQLite connections.
	// Zero uses a conservative default of four.
	MaxOpenConns int
}

// ImportRequest describes one complete source file.
type ImportRequest struct {
	Reader       io.Reader
	Pool         string
	Stream       string
	SourceName   string
	OnDiagnostic func(goflexlm.Diagnostic) error
}

// ImportResult summarizes a committed import.
type ImportResult struct {
	Digest                   string         `json:"digest"`
	Duplicate                bool           `json:"duplicate"`
	ByteCount                int64          `json:"byte_count"`
	LineCount                int            `json:"line_count"`
	ActivityCount            int            `json:"activity_count"`
	OmittedMessageCount      int            `json:"omitted_message_count"`
	DiagnosticCounts         map[string]int `json:"diagnostic_counts"`
	UnresolvedTimestampCount int            `json:"unresolved_timestamp_count"`
}

// LicenseImportRequest describes one parsed FlexLM license snapshot.
type LicenseImportRequest struct {
	File          goflexlm.LicenseFile
	Pool          string
	SourceName    string
	EffectiveFrom time.Time
	Timezone      string
}

// CalendarBucket controls report grouping.
type CalendarBucket string

// Supported calendar bucket sizes.
const (
	BucketHour  CalendarBucket = "hour"
	BucketDay   CalendarBucket = "day"
	BucketWeek  CalendarBucket = "week"
	BucketMonth CalendarBucket = "month"
)

// AnalyticsQuery selects a pool, optional feature, and half-open time range.
type AnalyticsQuery struct {
	Pool     string
	Feature  string
	From     time.Time
	To       time.Time
	Bucket   CalendarBucket
	Timezone *time.Location
}

// Quality summarizes data that limits report confidence.
type Quality struct {
	Open                  int   `json:"open"`
	Ambiguous             int   `json:"ambiguous"`
	Orphan                int   `json:"orphan"`
	WeakMatch             int   `json:"weak_match"`
	UnresolvedTime        int   `json:"unresolved_time"`
	MissingEntitlement    int   `json:"missing_entitlement"`
	UncoveredNanoseconds  int64 `json:"uncovered_nanoseconds"`
	UsageAboveEntitlement bool  `json:"usage_above_entitlement"`
}

// CapacityBucket reports capacity and usage for one vendor/feature time segment.
// Uncounted capacity is known unlimited capacity; Purchased and finite headroom
// fields are nil. Purchased nil with Uncounted false means missing capacity.
type CapacityBucket struct {
	Vendor                     string     `json:"vendor"`
	Feature                    string     `json:"feature"`
	Uncounted                  bool       `json:"uncounted"`
	From                       time.Time  `json:"from"`
	To                         time.Time  `json:"to"`
	Purchased                  *int       `json:"purchased"`
	LowerPeak                  int        `json:"lower_peak"`
	LowerPeakAt                *time.Time `json:"lower_peak_at,omitempty"`
	UpperPeak                  int        `json:"upper_peak"`
	UpperPeakAt                *time.Time `json:"upper_peak_at,omitempty"`
	LowerSaturatedNanoseconds  int64      `json:"lower_saturated_nanoseconds"`
	UpperSaturatedNanoseconds  int64      `json:"upper_saturated_nanoseconds"`
	LowerUsedSeatNanoseconds   int64      `json:"lower_used_seat_nanoseconds"`
	UpperUsedSeatNanoseconds   int64      `json:"upper_used_seat_nanoseconds"`
	LowerUnusedSeatNanoseconds *int64     `json:"lower_unused_seat_nanoseconds"`
	UpperUnusedSeatNanoseconds *int64     `json:"upper_unused_seat_nanoseconds"`
	LowerMinimumHeadroom       *int       `json:"lower_minimum_headroom"`
	UpperMinimumHeadroom       *int       `json:"upper_minimum_headroom"`
	LowerAverageHeadroom       *float64   `json:"lower_average_headroom"`
	UpperAverageHeadroom       *float64   `json:"upper_average_headroom"`
}

// CapacityReport contains capacity segments and quality counters.
type CapacityReport struct {
	From    time.Time        `json:"from"`
	To      time.Time        `json:"to"`
	Buckets []CapacityBucket `json:"buckets"`
	Quality Quality          `json:"quality"`
}

// DenialBucket groups denied activity.
type DenialBucket struct {
	Feature   string     `json:"feature"`
	Reason    string     `json:"reason"`
	ErrorCode string     `json:"error_code"`
	From      *time.Time `json:"from,omitempty"`
	To        *time.Time `json:"to,omitempty"`
	Events    int        `json:"events"`
	Licenses  int        `json:"licenses"`
}

// DenialReport contains grouped denied activity and quality counters.
type DenialReport struct {
	From    time.Time      `json:"from"`
	To      time.Time      `json:"to"`
	Buckets []DenialBucket `json:"buckets"`
	Quality Quality        `json:"quality"`
}

// QueueBucket summarizes queue activity for a feature and calendar bucket.
type QueueBucket struct {
	Feature           string         `json:"feature"`
	From              *time.Time     `json:"from,omitempty"`
	To                *time.Time     `json:"to,omitempty"`
	QueuedEvents      int            `json:"queued_events"`
	QueuedLicenses    int            `json:"queued_licenses"`
	MaximumDepth      int            `json:"maximum_depth"`
	CompletedWaits    int            `json:"completed_waits"`
	P50Wait           *time.Duration `json:"p50_wait,omitempty"`
	P95Wait           *time.Duration `json:"p95_wait,omitempty"`
	MaximumWait       *time.Duration `json:"maximum_wait,omitempty"`
	OpenQueueLicenses int            `json:"open_queue_licenses"`
	OldestOpenAge     *time.Duration `json:"oldest_open_age,omitempty"`
	Ambiguous         int            `json:"ambiguous"`
	Orphan            int            `json:"orphan"`
}

// QueueReport contains queue summaries and quality counters.
type QueueReport struct {
	From    time.Time     `json:"from"`
	To      time.Time     `json:"to"`
	Buckets []QueueBucket `json:"buckets"`
	Quality Quality       `json:"quality"`
}
