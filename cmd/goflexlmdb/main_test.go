package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportAndJSONReport(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	database := filepath.Join(directory, "usage.db")
	logPath := filepath.Join(directory, "license.log")
	contents := "2026-09-05T00:00:00Z (vendor) OUT: \"editor\" user@host\n" +
		"2026-09-05T01:00:00Z (vendor) IN: \"editor\" user@host\n"
	if err := os.WriteFile(logPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	status := run([]string{"import", "--db", database, "--pool", "p", "--stream", "s", logPath}, strings.NewReader(""), &output, &diagnostic)
	if status != 0 || !strings.Contains(output.String(), "2 activities") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, output.String(), diagnostic.String())
	}
	output.Reset()
	diagnostic.Reset()
	status = run([]string{"report", "capacity", "--db", database, "--pool", "p",
		"--from", "2026-09-05T00:00:00Z", "--to", "2026-09-06T00:00:00Z", "--feature", "editor", "--json"},
		strings.NewReader(""), &output, &diagnostic)
	if status != 0 || !strings.Contains(output.String(), `"buckets"`) || !strings.Contains(output.String(), `"unresolved_time":0`) {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, output.String(), diagnostic.String())
	}
}

func TestImportDiagnosticsCommitAndReturnOne(t *testing.T) {
	t.Parallel()
	database := filepath.Join(t.TempDir(), "usage.db")
	var output, diagnostic bytes.Buffer
	status := run([]string{"import", "--db", database, "--pool", "p", "--stream", "s", "-"},
		strings.NewReader("bad line\n2026-09-05T00:00:00Z (v) OUT: f u@h\n"), &output, &diagnostic)
	if status != 1 || !strings.Contains(diagnostic.String(), "line 1: invalid_envelope") || !strings.Contains(output.String(), "1 activities") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, output.String(), diagnostic.String())
	}

	output.Reset()
	diagnostic.Reset()
	status = run([]string{"report", "denials", "--db", database, "--pool", "p", "--from", "bad", "--to", "2026-09-06T00:00:00Z"}, strings.NewReader(""), &output, &diagnostic)
	if status != 2 {
		t.Fatalf("invalid argument status=%d", status)
	}
}

func TestReadEntitlementsRequiresExactHeader(t *testing.T) {
	t.Parallel()
	if _, err := readEntitlements(strings.NewReader("licenses,effective_from\n2,2026-01-01T00:00:00Z\n")); err == nil {
		t.Fatal("expected header error")
	}
	rows, err := readEntitlements(strings.NewReader("effective_from,licenses\n2026-01-01T00:00:00Z,2\n"))
	if err != nil || len(rows) != 1 || rows[0].Licenses != 2 {
		t.Fatalf("rows=%+v, error=%v", rows, err)
	}
}

func TestLicensesParseWritesLicenseDocumentFromStandardInput(t *testing.T) {
	var output, diagnostic bytes.Buffer
	status := run([]string{"licenses", "parse"}, strings.NewReader("FEATURE editor acme 1.0 permanent 2\n"), &output, &diagnostic)
	if status != 0 {
		t.Fatalf("status=%d stderr=%q", status, diagnostic.String())
	}
	want := "{\"servers\":[],\"vendors\":[],\"features\":[{\"line\":1,\"kind\":\"FEATURE\",\"name\":\"editor\",\"vendor\":\"acme\",\"version\":\"1.0\",\"expiration\":\"permanent\",\"licenses\":2,\"uncounted\":false}],\"use_server\":false}\n"
	if output.String() != want || diagnostic.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", output.String(), diagnostic.String())
	}
}
