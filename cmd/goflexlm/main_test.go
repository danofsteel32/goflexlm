package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStdinJSONLines(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(nil, strings.NewReader("01:02:03 (v) OUT: f u@h\n01:02:04 (v) TIMESTAMP\n"), &out, &errOut)
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
	code := run([]string{path}, strings.NewReader("ignored"), &out, &errOut)
	if code != 0 || errOut.String() != "goflexlm: skipped 1 unparseable line (invalid_envelope: 1)\n" || !strings.Contains(out.String(), `"line":2`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunConciseDiagnostics(t *testing.T) {
	input := strings.Repeat("bad\n", 10000) + "25:02:03 (v) message\n01:02:03 (v) OUT:\n01:02:04 (v) TIMESTAMP\n"
	var out, errOut bytes.Buffer
	code := run(nil, strings.NewReader(input), &out, &errOut)
	want := "goflexlm: skipped 10002 unparseable lines (invalid_activity: 1, invalid_envelope: 10000, invalid_timestamp: 1)\n"
	if code != 0 || errOut.String() != want || !strings.Contains(out.String(), `"line":10003`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunNoParseableEvents(t *testing.T) {
	for _, input := range []string{"", "\n \t\n", "bad\nalso bad\n"} {
		t.Run(input, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(nil, strings.NewReader(input), &out, &errOut)
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
	code := run(nil, input, &out, &errOut)
	if code != 1 || out.Len() == 0 || errOut.String() != "goflexlm: read input: read failed\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunDiagnosticOutputFailure(t *testing.T) {
	var out bytes.Buffer
	if code := run(nil, strings.NewReader("bad\n01:02:03 (v) TIMESTAMP\n"), &out, errorWriter{}); code != 1 {
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
	if code := run([]string{"-"}, strings.NewReader("01:02:03 (v) TIMESTAMP\n"), errorWriter{}, &errOut); code != 1 {
		t.Fatalf("code=%d", code)
	}
}
