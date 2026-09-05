package main

import (
	"bytes"
	"errors"
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
	if code != 1 || !strings.Contains(errOut.String(), "line 1: invalid_envelope") || !strings.Contains(out.String(), `"line":2`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
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
