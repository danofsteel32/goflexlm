package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/danofsteel32/goflexlm/sqlite"
)

func TestLicenseDemoWorkflow(t *testing.T) {
	db := filepath.Join(t.TempDir(), "demo.db")
	invoke := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &stderr); code != 0 {
			t.Fatalf("%v: code=%d stderr=%s", args, code, &stderr)
		}
		return out.String()
	}
	for _, date := range []string{"2026-09-01", "2026-09-02"} {
		path := "../../testdata/sqlite-demo/licenses-" + date + ".lic"
		invoke("licenses", "parse", path)
		if out := invoke("licenses", "import", "--db", db, "--pool", "studio", "--effective-from", date+"T00:00:00Z", "--timezone", "UTC", path); out != "" {
			t.Fatalf("import output=%q", out)
		}
	}
	invoke("import", "--db", db, "--pool", "studio", "--stream", "server-a", "../../testdata/sqlite-demo/server-a-2026-09-01.log", "../../testdata/sqlite-demo/server-a-2026-09-02.log")
	invoke("import", "--db", db, "--pool", "studio", "--stream", "server-b", "../../testdata/sqlite-demo/server-b-2026-09.log")
	raw := invoke("report", "capacity", "--db", db, "--pool", "studio", "--from", "2026-09-01T00:00:00Z", "--to", "2026-09-03T00:00:00Z", "--bucket", "day", "--timezone", "UTC", "--feature", "editor", "--json")
	var report store.CapacityReport
	if e := json.Unmarshal([]byte(raw), &report); e != nil {
		t.Fatal(e)
	}
	if len(report.Buckets) != 2 {
		t.Fatalf("report=%s", raw)
	}
	for i, want := range []struct{ purchased, lower, upper int }{{4, 4, 4}, {6, 3, 4}} {
		b := report.Buckets[i]
		if b.Vendor != "acme" || b.Purchased == nil || *b.Purchased != want.purchased || b.LowerPeak != want.lower || b.UpperPeak != want.upper {
			t.Fatalf("bucket=%+v want=%+v", b, want)
		}
	}
	invoke("import", "--db", db, "--pool", "studio", "--stream", "unmatched", "../../testdata/sqlite-demo/unmatched-vendor.log")
	invoke("rebuild", "--db", db, "--pool", "studio")
	for _, kind := range []string{"denials", "queues"} {
		invoke("report", kind, "--db", db, "--pool", "studio", "--from", "2026-09-01T00:00:00Z", "--to", "2026-09-03T00:00:00Z", "--json")
	}
}

func TestLicenseImportRejectsInvalidInputBeforeDatabaseOpen(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		closeErr    error
	}{
		{"parse", "PACKAGE bad", nil}, {"close", "SERVER h id", errors.New("close failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			closed, opened := false, false
			opener := func(string, io.Reader) (io.Reader, func() error, error) {
				return strings.NewReader(tc.input), func() error { closed = true; return tc.closeErr }, nil
			}
			openDB := func(context.Context, string, store.OpenOptions) (*store.Store, error) {
				opened = true
				return nil, errors.New("must not open")
			}
			var stderr bytes.Buffer
			code := runLicenseImport([]string{"--db", "unused", "--pool", "p", "--effective-from", "2026-01-01T00:00:00Z", "--timezone", "UTC", "source"}, nil, &stderr, opener, openDB)
			if code != 1 || !closed || opened || !strings.HasPrefix(stderr.String(), "goflexlmdb:") {
				t.Fatalf("status=%d closed=%v opened=%v stderr=%s", code, closed, opened, &stderr)
			}
		})
	}
	closed := false
	opener := func(string, io.Reader) (io.Reader, func() error, error) {
		return strings.NewReader("SERVER h id"), func() error { closed = true; return nil }, nil
	}
	openDB := func(context.Context, string, store.OpenOptions) (*store.Store, error) {
		if !closed {
			t.Fatal("opened before close")
		}
		return nil, errors.New("database failure")
	}
	var stderr bytes.Buffer
	if code := runLicenseImport([]string{"--db", "unused", "--pool", "p", "--effective-from", "2026-01-01T00:00:00Z", "--timezone", "UTC", "source"}, nil, &stderr, opener, openDB); code != 1 || !strings.Contains(stderr.String(), "database failure") {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
}

func TestLicenseImportUsageAndBadInputDoNotCreateDatabase(t *testing.T) {
	for _, zone := range []string{"", "Local", "EST", "No/SuchZone"} {
		db := filepath.Join(t.TempDir(), "not-created.db")
		args := []string{"licenses", "import", "--db", db, "--pool", "p", "--effective-from", "2026-01-01T00:00:00Z", "--timezone", zone, "-"}
		var out, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &out, &stderr); code != 2 || !strings.Contains(stderr.String(), "usage:") {
			t.Fatalf("zone=%s code=%d", zone, code)
		}
		if _, e := os.Stat(db); !errors.Is(e, os.ErrNotExist) {
			t.Fatalf("database created: %v", e)
		}
	}
	for _, input := range []string{"PACKAGE bad", "FEATURE f v 1 permanent -1"} {
		db := filepath.Join(t.TempDir(), "not-created.db")
		var out, stderr bytes.Buffer
		if code := run([]string{"licenses", "import", "--db", db, "--pool", "p", "--effective-from", "2026-01-01T00:00:00Z", "--timezone", "UTC", "-"}, strings.NewReader(input), &out, &stderr); code != 1 || out.Len() != 0 {
			t.Fatalf("code=%d output=%s", code, &out)
		}
		if _, e := os.Stat(db); !errors.Is(e, os.ErrNotExist) {
			t.Fatalf("database created: %v", e)
		}
	}
}

func TestLicenseImportRejectsBadFlagsAndPathCounts(t *testing.T) {
	base := []string{"licenses", "import", "--db", filepath.Join(t.TempDir(), "unused"), "--pool", "p", "--effective-from", "2026-01-01T00:00:00Z", "--timezone", "UTC"}
	for _, args := range [][]string{
		base, append(append([]string{}, base...), "one", "two"),
		{"licenses", "import", "--db", "x", "--pool", "p", "--effective-from", "invalid", "--timezone", "UTC", "-"},
		{"licenses", "import", "--db", "x", "--pool", "p", "--effective-from", "2026-01-01T00:00:00Z", "--timezone", "UTC", "--unknown", "-"},
	} {
		var out, stderr bytes.Buffer
		code := run(args, strings.NewReader(""), &out, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "usage:") || out.Len() != 0 {
			t.Fatalf("%v: code=%d stderr=%s", args, code, &stderr)
		}
	}
}

func TestLegacyEntitlementCommandIsRejected(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run([]string{"entitlements", "replace", "--db", filepath.Join(t.TempDir(), "unused"), "--pool", "p", "--feature", "f", "-"}, strings.NewReader("effective_from,licenses\n2026-01-01T00:00:00Z,2"), &out, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
}

func TestLicenseImportCommandPersistsSourceAndIsSilent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "licenses.lic")
	if e := os.WriteFile(path, []byte("FEATURE editor acme 1 permanent 2"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, source := range []string{path, "-"} {
		dbPath := filepath.Join(t.TempDir(), "usage.db")
		var out, stderr bytes.Buffer
		args := []string{"licenses", "import", "--db", dbPath, "--pool", "p", "--effective-from", "2026-01-01T00:00:00Z", "--timezone", "UTC", source}
		code := run(args, strings.NewReader("FEATURE editor acme 1 permanent 2"), &out, &stderr)
		if code != 0 || out.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("status=%d stdout=%s stderr=%s", code, &out, &stderr)
		}
		db, e := sql.Open("sqlite", dbPath)
		if e != nil {
			t.Fatal(e)
		}
		var stored string
		e = db.QueryRow("SELECT source_name FROM license_imports").Scan(&stored)
		db.Close()
		if e != nil || stored != source {
			t.Fatalf("source=%q error=%v", stored, e)
		}
	}
}

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
