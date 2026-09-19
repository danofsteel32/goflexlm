package goflexlm

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLicenseCorpus(t *testing.T) {
	input, err := os.ReadFile("testdata/licenses/conforming.lic")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/licenses/conforming.json")
	if err != nil {
		t.Fatal(err)
	}
	file, err := ParseLicenseFile(strings.NewReader(string(input)))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != strings.TrimSpace(string(expected)) {
		t.Fatalf("JSON = %s", actual)
	}
}

func TestLicenseErrorsPreservePhysicalLinesAndNoDocument(t *testing.T) {
	for _, tc := range []struct{ input, part string }{
		{"SERVER h id\nFEATURE f v 1 permanent \\\n -1", "line 3"},
		{"FEATURE f v \\\n 1 permanent", "line 1"},
		{"FEATURE f v 1 permanent 1 \\\n A=\"broken", "line 2"},
		{"FEATURE f v 1 permanent 1 \\", "line 1"},
	} {
		doc, err := ParseLicenseFile(strings.NewReader(tc.input))
		if err == nil || !strings.Contains(err.Error(), tc.part) || !reflect.DeepEqual(doc, LicenseFile{}) {
			t.Fatalf("%q: %+v, %v", tc.input, doc, err)
		}
	}
}

type licenseFailReader struct{}

func (licenseFailReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestLicenseReaderFailure(t *testing.T) {
	doc, err := ParseLicenseFile(io.MultiReader(strings.NewReader("SERVER h id\n"), licenseFailReader{}))
	if !errors.Is(err, io.ErrClosedPipe) || !reflect.DeepEqual(doc, LicenseFile{}) {
		t.Fatalf("document=%+v error=%v", doc, err)
	}
}

func TestLicenseLongLineAndFinalCRLF(t *testing.T) {
	value := strings.Repeat("x", 200000)
	for _, ending := range []string{"", "\n", "\r\n"} {
		doc, err := ParseLicenseFile(strings.NewReader("FEATURE f v 1 permanent 1 DATA=" + value + ending))
		if err != nil || doc.Features[0].Attributes[0].Value != value {
			t.Fatalf("long line: %v", err)
		}
	}
}

func FuzzParseLicenseFile(f *testing.F) {
	f.Add("SERVER h id\nFEATURE f v 1 permanent 1")
	f.Add("INCREMENT f v 1 1-jan-2026 0 \\\n SIGN=\"hello\"")
	f.Fuzz(func(t *testing.T, input string) {
		first, err := ParseLicenseFile(strings.NewReader(input))
		second, again := ParseLicenseFile(strings.NewReader(input))
		if (err == nil) != (again == nil) || !reflect.DeepEqual(first, second) {
			t.Fatal("nondeterministic parse")
		}
		if err != nil {
			if !reflect.DeepEqual(first, LicenseFile{}) {
				t.Fatal("partial document")
			}
			return
		}
		a, e := json.Marshal(first)
		if e != nil {
			t.Fatal(e)
		}
		b, e := json.Marshal(second)
		if e != nil || string(a) != string(b) {
			t.Fatal("nondeterministic JSON")
		}
	})
}

func TestLicenseGrammarConformance(t *testing.T) {
	for _, input := range []string{
		"SERVER host id FLAG X=one X=two\n", "SERVER host id\r\nVENDOR v OPTIONS=\"a b\" PORT=0\n",
		"feature f v 1 29-Feb-2024 00 EMPTY=\"\" FLAG", "INCREMENT f v 1 1-jan-000 2 START=1-jan-2026",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseLicenseFile(strings.NewReader(input)); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, input := range []string{
		"FEATURE f v 1 31-feb-2026 2", "FEATURE f v 1 permanent +2", "FEATURE f v 1 permanent 2 START=permanent",
		"FEATURE f v 1 permanent 2 START=1-jan-2026 start=2-jan-2026", "VENDOR v PORT=1 bare", "VENDOR v PORT=65536",
		"SERVER h id 0", "FEATURE \"\" v 1 permanent 2", "FEATURE f v 1 permanent 2 A=\"x\"junk",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseLicenseFile(strings.NewReader(input)); err == nil {
				t.Fatal("expected invalid document")
			}
		})
	}
}
