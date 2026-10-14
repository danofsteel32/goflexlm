package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunStdinJSONLines(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"lmgrd"}, strings.NewReader("01:02:03 (v) OUT: f u@h\n01:02:04 (v) TIMESTAMP\n"), &out, &errOut)
	if code != 0 || errOut.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"kind":"checkout"`) || !strings.Contains(lines[1], `"kind":"message"`) {
		t.Fatalf("stdout=%s", out.String())
	}
}

func TestRunFileAndDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	if err := os.WriteFile(path, []byte("bad\n01:02:03 (v) TIMESTAMP\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"lmgrd", path}, strings.NewReader("ignored"), &out, &errOut)
	if code != 0 || errOut.String() != "goflexlm: skipped 1 unparseable line (invalid_envelope: 1)\n" || !strings.Contains(out.String(), `"line":2`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunConciseDiagnostics(t *testing.T) {
	input := strings.Repeat("bad\n", 10000) + "25:02:03 (v) message\n01:02:03 (v) OUT:\n01:02:04 (v) TIMESTAMP\n"
	var out, errOut bytes.Buffer
	code := run([]string{"lmgrd"}, strings.NewReader(input), &out, &errOut)
	want := "goflexlm: skipped 10002 unparseable lines (invalid_activity: 1, invalid_envelope: 10000, invalid_timestamp: 1)\n"
	if code != 0 || errOut.String() != want || !strings.Contains(out.String(), `"line":10003`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunNoParseableEvents(t *testing.T) {
	for _, input := range []string{"", "\n \t\n", "bad\nalso bad\n"} {
		t.Run(input, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run([]string{"lmgrd"}, strings.NewReader(input), &out, &errOut)
			if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), "goflexlm: no parseable events\n") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestRunInputFailureAfterEvent(t *testing.T) {
	input := io.MultiReader(strings.NewReader("01:02:03 (v) TIMESTAMP\n"), errorReader{})
	var out, errOut bytes.Buffer
	code := run([]string{"lmgrd"}, input, &out, &errOut)
	if code != 1 || out.Len() == 0 || errOut.String() != "goflexlm: read input: read failed\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunDiagnosticOutputFailure(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"lmgrd"}, strings.NewReader("bad\n01:02:03 (v) TIMESTAMP\n"), &out, errorWriter{}); code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunInvalidArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"one", "two"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRunOutputFailure(t *testing.T) {
	var errOut bytes.Buffer
	if code := run([]string{"lmgrd", "-"}, strings.NewReader("01:02:03 (v) TIMESTAMP\n"), errorWriter{}, &errOut); code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunLmgrdExplicitStdin(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"lmgrd", "-"}, strings.NewReader("01:02:03 (v) TIMESTAMP\n"), &out, &errOut)
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), `"kind":"message"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunLicenseJSON(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "licenses", "conforming.lic")
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := os.ReadFile(filepath.Join("..", "..", "testdata", "licenses", "conforming.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want any
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"license", path},
		{"license"},
		{"license", "-"},
		{"license", "--", path},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(args, bytes.NewReader(input), &out, &errOut)
			if code != 0 || errOut.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			var got any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("stdout=%s, want %s", out.String(), wantJSON)
			}
		})
	}
}

func TestRunLicenseFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input io.Reader
		want  string
	}{
		{"empty", strings.NewReader(""), "no parseable license records"},
		{"comments", strings.NewReader("# comment\n\n"), "no parseable license records"},
		{"malformed", strings.NewReader("SERVER host id\nFEATURE incomplete\n"), "line 2: incomplete FEATURE record"},
		{"log", strings.NewReader("01:02:03 (v) TIMESTAMP\n"), "unsupported directive"},
		{"reader", io.MultiReader(strings.NewReader("SERVER host id\n"), errorReader{}), "read failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run([]string{"license"}, tc.input, &out, &errOut)
			if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestRunLicenseOutputFailure(t *testing.T) {
	var errOut bytes.Buffer
	code := run([]string{"license"}, strings.NewReader("USE_SERVER\n"), errorWriter{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "write output: write failed") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestRunFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 2},
		{[]string{"--help"}, 0},
		{[]string{"-h"}, 0},
		{[]string{"lmgrd", "--help"}, 0},
		{[]string{"lmgrd", "-h"}, 0},
		{[]string{"license", "--help"}, 0},
		{[]string{"license", "-h"}, 0},
		{[]string{"debug.log"}, 2},
		{[]string{"lmgrd", "one", "two"}, 2},
		{[]string{"lmgrd", "--unknown"}, 2},
		{[]string{"license", "--unknown"}, 2},
		{[]string{"license", "--summary"}, 2},
		{[]string{"lmgrd", "--summary=invalid"}, 2},
		{[]string{"--format", "unknown"}, 2},
		{[]string{"--format"}, 2},
		{[]string{"--unknown"}, 2},
		{[]string{"license", "one", "two"}, 2},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tc.args, strings.NewReader(""), &out, &errOut)
			if code != tc.code || out.Len() != 0 || errOut.Len() == 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestRunLogSummaryCounts(t *testing.T) {
	input := "2026-10-14T10:00:00Z (v) OUT: f u@h (2 licenses)\n" +
		"10:00:01 (v) IN: f u@h\n" +
		"10:00:02 (v) DENIED: f u@h\n" +
		"10:00:03 (v) QUEUED: f u@h\n" +
		"10:00:04 (v) DEQUEUED: f u@h\n" +
		"bad\n10:00:05 (v) OUT: invalid\n" +
		"10:00:06 (v) OUT: f u@h\n" +
		"10:00:07 (v) UNSUPPORTED: f u@h (No such feature exists.)\n" +
		"10:00:08 (v) UNSUPPORTED: other u@h\n" +
		"10:00:09 (v) message mentioning UNSUPPORTED: f\n" +
		"10:00:10 (v) UNSUPPORTED_FEATURE: f\n" +
		"10:00:11 (v) TIMESTAMP\n"
	path := filepath.Join(t.TempDir(), "debug.log")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	want := "Time range: 2026-10-14T10:00:00Z -> 2026-10-14T10:00:11Z\n" +
		"OUT: 2\nIN: 1\nDENIED: 1\nUNSUPPORTED: 2\n"
	for _, args := range [][]string{
		{"lmgrd", "--summary"},
		{"lmgrd", "--summary", "-"},
		{"lmgrd", "--summary", path},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(args, strings.NewReader(input), &out, &errOut)
			if code != 0 || out.String() != want || errOut.String() != "goflexlm: skipped 2 unparseable lines (invalid_activity: 1, invalid_envelope: 1)\n" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestRunLogSummaryTimeRange(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"clock only", " 1:02:03 (v) hello\n02:03:04 (v) bye\n", "01:02:03 (date/offset unknown) -> 02:03:04 (date/offset unknown)"},
		{"single event", "2026-10-14T01:02:03.123-04:00 (v) hello\n", "2026-10-14T01:02:03.123-04:00 -> 2026-10-14T01:02:03.123-04:00"},
		{"midnight", "2026-10-14T23:59:59Z (v) hello\n00:00:01 (v) bye\n", "2026-10-14T23:59:59Z -> 2026-10-15T00:00:01Z"},
		{"native date", "13:46:51 (lmgrd) Server's System Date and Time: Fri Aug 29 2025 13:46:51 UTC\n13:46:52 (v) hello\n", "2025-08-29T13:46:51Z -> 2025-08-29T13:46:52Z"},
		{"partly dated", "01:02:03 (v) hello\n2026-10-14T01:02:04Z (v) bye\n", "2026-10-14T01:02:03Z -> 2026-10-14T01:02:04Z"},
		{"anchor after midnight", "14:00:57 (v) hello\n2026-06-10T11:35:30Z (v) bye\n", "2026-06-09T14:00:57Z -> 2026-06-10T11:35:30Z"},
		{"multiple midnights before anchor", "23:59:59 (v) hello\n00:00:01 (v) hello\n23:59:59 (v) hello\n2026-01-01T00:00:01-04:00 (v) bye\n", "2025-12-30T23:59:59-04:00 -> 2026-01-01T00:00:01-04:00"},
		{"native date backfill", "14:00:57 (v) hello\n00:00:01 (v) hello\n13:46:51 (lmgrd) Server's System Date and Time: Fri Aug 29 2025 13:46:51 UTC\n", "2025-08-28T14:00:57Z -> 2025-08-29T13:46:51Z"},
		{"later anchor preserves start", "01:02:03 (v) hello\n2026-10-14T01:02:04Z (v) hello\n2026-10-20T01:02:05Z (v) bye\n", "2026-10-14T01:02:03Z -> 2026-10-20T01:02:05Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run([]string{"lmgrd", "--summary"}, strings.NewReader(tc.input), &out, &errOut)
			if code != 0 || errOut.Len() != 0 || out.String() != "Time range: "+tc.want+"\nOUT: 0\nIN: 0\nDENIED: 0\nUNSUPPORTED: 0\n" {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestRunLogSummaryFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input io.Reader
		want  string
	}{
		{"empty", strings.NewReader(""), "no parseable events"},
		{"invalid", strings.NewReader("bad\n"), "no parseable events"},
		{"read failure", io.MultiReader(strings.NewReader("01:02:03 (v) hello\n"), errorReader{}), "read input: read failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run([]string{"lmgrd", "--summary"}, tc.input, &out, &errOut)
			if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
	var errOut bytes.Buffer
	code := run([]string{"lmgrd", "--summary"}, strings.NewReader("01:02:03 (v) hello\n"), errorWriter{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "write output: write failed") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestRunLogSummaryDisabled(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"lmgrd", "--summary=false"}, strings.NewReader("01:02:03 (v) hello\n"), &out, &errOut)
	if code != 0 || errOut.Len() != 0 || !json.Valid(out.Bytes()) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunCommandHelp(t *testing.T) {
	for _, command := range []string{"lmgrd", "license"} {
		t.Run(command, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run([]string{command, "--help"}, errorReader{}, &out, &errOut)
			if code != 0 || out.Len() != 0 || !strings.Contains(errOut.String(), "usage: goflexlm "+command+" [options] [FILE|-]") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}

func TestRunMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	for _, command := range []string{"lmgrd", "license"} {
		t.Run(command, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run([]string{command, path}, strings.NewReader("USE_SERVER\n"), &out, &errOut)
			if code != 1 || out.Len() != 0 || !strings.Contains(errOut.String(), path) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
}
