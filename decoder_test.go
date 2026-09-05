package goflexlm

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestEnvelopesAndGenericMessages(t *testing.T) {
	tests := []struct {
		line, original, daemon, message string
		resolved                        bool
	}{
		{"8:01:02 (lmgrd) TIMESTAMP", "8:01:02", "lmgrd", "TIMESTAMP", false},
		{"12:13:14 (vendor_7) Server started", "12:13:14", "vendor_7", "Server started", false},
		{"2026-09-05T12:13:14-04:00 (vendor) Shutting down", "2026-09-05T12:13:14-04:00", "vendor", "Shutting down", true},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			d := NewDecoder(strings.NewReader(tt.line))
			if !d.Scan() {
				t.Fatalf("Scan=false, err=%v", d.Err())
			}
			r := d.Result()
			if r.Event == nil || r.Diagnostic != nil {
				t.Fatalf("unexpected result: %+v", r)
			}
			e := r.Event
			if e.Raw != tt.line || e.Daemon != tt.daemon || e.Message != tt.message || e.Kind != KindMessage {
				t.Fatalf("event = %+v", e)
			}
			if e.LogTime.Original != tt.original || (e.LogTime.Time != nil) != tt.resolved {
				t.Fatalf("time = %+v", e.LogTime)
			}
		})
	}
}

func TestUnsupportedActivitiesAndLifecycleMessagesAreGeneric(t *testing.T) {
	messages := []string{
		"Started vendor demo",
		"Shutting down demo",
		"TIMESTAMP 9/5/2026",
		`UPGRADE: "f1" user@host (1->2 licenses)`,
	}
	for _, message := range messages {
		d := NewDecoder(strings.NewReader("01:02:03 (lmgrd) " + message))
		if !d.Scan() || d.Result().Event == nil {
			t.Fatalf("%q did not produce an event", message)
		}
		if got := d.Result().Event; got.Kind != KindMessage || got.Message != message || got.Activity != nil {
			t.Fatalf("event = %+v", got)
		}
	}
}

func TestCompactActivities(t *testing.T) {
	tests := []struct {
		message                                      string
		kind                                         Kind
		feature, user, host, data, reason, denialErr string
		licenses                                     int
	}{
		{`OUT: "f1" alice@pc [job-42] (3 licenses)`, KindCheckout, "f1", "alice", "pc", "job-42", "", "", 3},
		{`IN: f2 bob (2 licenses)`, KindCheckin, "f2", "bob", "", "", "", "", 2},
		{`OUT: f3 carol@host`, KindCheckout, "f3", "carol", "host", "", "", "", 1},
		{`DENIED: 4 f4 to dave@desk`, KindDenied, "f4", "dave", "desk", "", "", "", 4},
		{`DENIED: "f5" erin@node (Licensed number reached. (-4,342))`, KindDenied, "f5", "erin", "node", "", "Licensed number reached.", "-4,342", 1},
		{`QUEUED: "f6" frank@machine`, KindQueued, "f6", "frank", "machine", "", "", "", 1},
		{`DEQUEUED: "f7" grace@machine`, KindDequeued, "f7", "grace", "machine", "", "", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			d := NewDecoder(strings.NewReader("01:02:03 (vd) " + tt.message))
			if !d.Scan() {
				t.Fatal("no result")
			}
			r := d.Result()
			if r.Diagnostic != nil {
				t.Fatalf("diagnostic: %+v", r.Diagnostic)
			}
			a := r.Event.Activity
			if r.Event.Kind != tt.kind || a == nil {
				t.Fatalf("event: %+v", r.Event)
			}
			if a.Feature != tt.feature || a.User != tt.user || a.Host != tt.host || a.Licenses != tt.licenses || a.CheckoutData != tt.data || a.DenialReason != tt.reason || a.DenialError != tt.denialErr {
				t.Fatalf("activity = %+v", a)
			}
		})
	}
}

func TestVerboseActivities(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		kind   Kind
	}{
		{`OUT: "f1" @host (2)`, KindCheckout},
		{`IN: "f1" @host (2)`, KindCheckin},
		{`QUEUED: "f1" @host`, KindQueued},
		{`DEQUEUED: "f1" @host`, KindDequeued},
		{`DENIED: "f1" @host (No licenses (-4,342))`, KindDenied},
	} {
		message := tc.prefix + ` ^^^ 1.0 alice 127.0.0.1 29865 0x12CE 1 3 proj 11 19 8-8-2023 8:41:56 YES ^^^`
		d := NewDecoder(strings.NewReader("08:41:54 (demo) " + message))
		if !d.Scan() {
			t.Fatal("no result")
		}
		r := d.Result()
		if r.Diagnostic != nil {
			t.Fatalf("%s: %+v", tc.prefix, r.Diagnostic)
		}
		a := r.Event.Activity
		if r.Event.Kind != tc.kind || a.User != "alice" || a.Host != "host" || a.Verbose == nil {
			t.Fatalf("event = %+v", r.Event)
		}
		if a.Verbose.ProjectName != "proj" || a.Verbose.IsDuplicateGroup != "YES" {
			t.Fatalf("verbose = %+v", a.Verbose)
		}
	}
}

func TestVerboseProjectNameMayContainSpaces(t *testing.T) {
	line := `08:41:54 (demo) OUT: "f1" @host ^^^ 1.0 alice 127.0.0.1 1 h 1 3 a project name 11 19 8-8-2023 8:41:56 NO ^^^`
	d := NewDecoder(strings.NewReader(line))
	if !d.Scan() || d.Result().Event == nil {
		t.Fatalf("result=%+v", d.Result())
	}
	if got := d.Result().Event.Activity.Verbose.ProjectName; got != "a project name" {
		t.Fatalf("project name=%q", got)
	}
}

func TestTimestampContextAndMidnight(t *testing.T) {
	input := strings.Join([]string{
		"23:59:57 (v) startup",
		"2026-09-05T23:59:58-04:00 (v) dated",
		"23:59:59 (v) before midnight",
		"00:00:01 (v) after midnight",
		"2026-10-10T12:00:00+02:30 (v) reset",
		"12:00:01 (v) inherited",
	}, "\n")
	d := NewDecoder(strings.NewReader(input))
	var events []*Event
	for d.Scan() {
		events = append(events, d.Result().Event)
	}
	if err := d.Err(); err != nil {
		t.Fatal(err)
	}
	if events[0].LogTime.Time != nil {
		t.Fatal("classic time resolved without context")
	}
	want := []string{"2026-09-05T23:59:58-04:00", "2026-09-05T23:59:59-04:00", "2026-09-06T00:00:01-04:00", "2026-10-10T12:00:00+02:30", "2026-10-10T12:00:01+02:30"}
	for i, value := range want {
		if got := events[i+1].LogTime.Time.Format(time.RFC3339); got != value {
			t.Errorf("event %d timestamp=%s want %s", i+1, got, value)
		}
	}
}

func TestDiagnosticsRecoverAndPreserveRaw(t *testing.T) {
	input := "not a log line\n25:00:00 (v) bad time\n01:00:00 (v) OUT: nonsense\r\n01:00:01 (v) TIMESTAMP"
	d := NewDecoder(strings.NewReader(input))
	var codes []string
	var event *Event
	for d.Scan() {
		r := d.Result()
		if (r.Event == nil) == (r.Diagnostic == nil) {
			t.Fatalf("result does not contain exactly one value: %+v", r)
		}
		if r.Diagnostic != nil {
			codes = append(codes, r.Diagnostic.Code)
		} else {
			event = r.Event
		}
	}
	if got := strings.Join(codes, ","); got != "invalid_envelope,invalid_timestamp,invalid_activity" {
		t.Fatalf("codes=%s", got)
	}
	if event == nil || event.Line != 4 || event.Raw != "01:00:01 (v) TIMESTAMP" {
		t.Fatalf("event=%+v", event)
	}
}

func TestBlankCRLFLongAndFinalLine(t *testing.T) {
	long := strings.Repeat("x", 200_000)
	d := NewDecoder(strings.NewReader("\r\n01:02:03 (v) " + long + "\r\n01:02:04 (v) final"))
	var got []*Event
	for d.Scan() {
		got = append(got, d.Result().Event)
	}
	if d.Err() != nil || len(got) != 2 {
		t.Fatalf("events=%d err=%v", len(got), d.Err())
	}
	if got[0].Line != 2 || got[0].Message != long || got[1].Raw != "01:02:04 (v) final" {
		t.Fatal("input was not preserved")
	}
}

type failingReader struct{ sent bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, errors.New("injected read failure")
	}
	r.sent = true
	return copy(p, "01:02:03 (v) first\n"), nil
}

func TestReaderFailure(t *testing.T) {
	d := NewDecoder(&failingReader{})
	if !d.Scan() || d.Result().Event == nil {
		t.Fatal("first event missing")
	}
	if d.Scan() {
		t.Fatal("unexpected result after failure")
	}
	if d.Err() == nil || d.Err().Error() != "injected read failure" {
		t.Fatalf("Err=%v", d.Err())
	}
}

func FuzzDecoder(f *testing.F) {
	f.Add([]byte("01:02:03 (v) OUT: f u@h\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		d := NewDecoder(strings.NewReader(string(data)))
		count := 0
		for d.Scan() {
			count++
			if count > len(data)+1 {
				t.Fatal("decoder failed to consume input")
			}
		}
		_ = d.Err()
	})
}

var _ io.Reader = (*failingReader)(nil)
