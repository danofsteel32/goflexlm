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

func TestLicenseParseClosesBeforeWriting(t *testing.T) {
	for _, failClose := range []bool{false, true} {
		closed := false
		opener := func(string, io.Reader) (io.Reader, func() error, error) {
			return strings.NewReader("SERVER h id"), func() error {
				closed = true
				if failClose {
					return errors.New("close failed")
				}
				return nil
			}, nil
		}
		var stderr bytes.Buffer
		writes := 0
		out := licenseWriterFunc(func(p []byte) (int, error) {
			writes++
			if !closed {
				t.Fatal("output before close")
			}
			return len(p), nil
		})
		status := runLicenseParse("-", strings.NewReader(""), out, &stderr, opener)
		if failClose {
			if status != 1 || writes != 0 || !strings.Contains(stderr.String(), "close failed") {
				t.Fatalf("status=%d writes=%d stderr=%s", status, writes, &stderr)
			}
		} else if status != 0 || writes != 1 {
			t.Fatalf("status=%d writes=%d", status, writes)
		}
	}
}

type licenseWriterFunc func([]byte) (int, error)

func (f licenseWriterFunc) Write(p []byte) (int, error) { return f(p) }

type licenseReaderFunc func([]byte) (int, error)

func (f licenseReaderFunc) Read(p []byte) (int, error) { return f(p) }

func TestLicenseParseClosesAfterReadOrParseFailure(t *testing.T) {
	for _, reader := range []io.Reader{strings.NewReader("PACKAGE unsupported"), licenseReaderFunc(func([]byte) (int, error) { return 0, io.ErrClosedPipe })} {
		closed := false
		opener := func(string, io.Reader) (io.Reader, func() error, error) {
			return reader, func() error { closed = true; return nil }, nil
		}
		var out, stderr bytes.Buffer
		code := runLicenseParse("source.lic", nil, &out, &stderr, opener)
		if code != 1 || !closed || out.Len() != 0 || !strings.Contains(stderr.String(), "source.lic") {
			t.Fatalf("code=%d closed=%v output=%s stderr=%s", code, closed, &out, &stderr)
		}
	}
}

func TestLicenseParseFileAndFailureStatuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.lic")
	if e := os.WriteFile(path, []byte("SERVER h id"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"licenses"}, {"licenses", "unknown"}, {"licenses", "parse", "a", "b"}} {
		var out, err bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &err); code != 2 || out.Len() != 0 || !strings.Contains(err.String(), "usage:") {
			t.Fatalf("%v: %d %s", args, code, &err)
		}
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"licenses", "parse", path}, strings.NewReader(""), &out, &stderr); code != 0 || !strings.HasSuffix(out.String(), "\n") || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("status=%d output=%s err=%s", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	if code := run([]string{"licenses", "parse", path + ".missing"}, strings.NewReader(""), &out, &stderr); code != 1 || out.Len() != 0 || !strings.HasPrefix(stderr.String(), "goflexlmdb:") {
		t.Fatalf("status=%d", code)
	}
	for _, writer := range []io.Writer{
		licenseWriterFunc(func([]byte) (int, error) { return 0, io.ErrClosedPipe }),
		licenseWriterFunc(func(p []byte) (int, error) { return len(p) - 1, nil }),
	} {
		stderr.Reset()
		if code := run([]string{"licenses", "parse", "-"}, strings.NewReader("SERVER h id"), writer, &stderr); code != 1 {
			t.Fatalf("output failure status=%d", code)
		}
	}
}
