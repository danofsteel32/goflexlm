package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "github.com/danofsteel32/goflexlm/sqlite"
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

func TestLicensesParseRejectsMalformedInputWithoutJSON(t *testing.T) {
	var output, diagnostic bytes.Buffer
	status := run([]string{"licenses", "parse"}, strings.NewReader("PACKAGE unsupported\n"), &output, &diagnostic)
	if status != 1 || output.Len() != 0 || !strings.HasPrefix(diagnostic.String(), "goflexlmdb:") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, output.String(), diagnostic.String())
	}
}

func TestWriteCapacityTableLeadsWithVendor(t *testing.T) {
	var output bytes.Buffer
	err := writeTable(&output, store.CapacityReport{Buckets: []store.CapacityBucket{{Vendor: "acme", Feature: "editor"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "VENDOR") || !strings.Contains(output.String(), "acme") {
		t.Fatalf("table = %q", output.String())
	}
}

func TestCapacityTableLabelsUncounted(t *testing.T) {
	var out bytes.Buffer
	if e := writeTable(&out, store.CapacityReport{Buckets: []store.CapacityBucket{{Vendor: "v", Feature: "f", Uncounted: true}}}); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "uncounted") || strings.Contains(out.String(), "unknown") {
		t.Fatalf("table=%s", out.String())
	}
}
