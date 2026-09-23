package goflexlm

import (
	"strings"
	"testing"
)

func TestParseLicenseFileReturnsEmptyDocumentForBlankInput(t *testing.T) {
	file, err := ParseLicenseFile(strings.NewReader("\n\r\n  # comment\n"))
	if err != nil {
		t.Fatalf("ParseLicenseFile() error = %v", err)
	}
	if file.Servers == nil || file.Vendors == nil || file.Features == nil || file.UseServer {
		t.Fatalf("ParseLicenseFile() = %+v", file)
	}
}

func TestParseLicenseFileReturnsServerRecord(t *testing.T) {
	file, err := ParseLicenseFile(strings.NewReader("SERVER lic01 001122aabbcc 27000\n"))
	if err != nil {
		t.Fatalf("ParseLicenseFile() error = %v", err)
	}
	if len(file.Servers) != 1 {
		t.Fatalf("server count = %d, want 1", len(file.Servers))
	}
	server := file.Servers[0]
	if server.Line != 1 || server.Host != "lic01" || server.HostID != "001122aabbcc" || server.Port == nil || *server.Port != 27000 {
		t.Fatalf("server = %+v", server)
	}
}

func TestParseLicenseFileReturnsVendorAndFeatureRecords(t *testing.T) {
	input := "VENDOR acme /opt/acme OPTIONS=/etc/acme.opt\nUSE_SERVER\nFEATURE editor acme 2026.0 31-dec-2026 12\n"
	file, err := ParseLicenseFile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseLicenseFile() error = %v", err)
	}
	if !file.UseServer || len(file.Vendors) != 1 || len(file.Features) != 1 {
		t.Fatalf("ParseLicenseFile() = %+v", file)
	}
	vendor := file.Vendors[0]
	if vendor.Line != 1 || vendor.Name != "acme" || vendor.DaemonPath != "/opt/acme" || len(vendor.Attributes) != 1 || vendor.Attributes[0] != (LicenseAttribute{Name: "OPTIONS", Value: "/etc/acme.opt", HasValue: true}) {
		t.Fatalf("vendor = %+v", vendor)
	}
	feature := file.Features[0]
	if feature.Line != 3 || feature.Kind != LicenseFeatureLine || feature.Name != "editor" || feature.Vendor != "acme" || feature.Licenses == nil || *feature.Licenses != 12 || feature.Uncounted {
		t.Fatalf("feature = %+v", feature)
	}
}

func TestParseLicenseFilePreservesQuotedContinuedAttributes(t *testing.T) {
	input := "INCREMENT editor acme 2026.0 permanent 0 \\\n  VENDOR_STRING=\"team a\" SIGN=0123ABCD\n"
	file, err := ParseLicenseFile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseLicenseFile() error = %v", err)
	}
	if len(file.Features) != 1 {
		t.Fatalf("feature count = %d, want 1", len(file.Features))
	}
	feature := file.Features[0]
	if feature.Line != 1 || feature.Kind != LicenseIncrementLine || !feature.Uncounted || feature.Licenses != nil {
		t.Fatalf("feature = %+v", feature)
	}
	want := []LicenseAttribute{{Name: "VENDOR_STRING", Value: "team a", HasValue: true}, {Name: "SIGN", Value: "0123ABCD", HasValue: true}}
	if len(feature.Attributes) != len(want) {
		t.Fatalf("attributes = %+v", feature.Attributes)
	}
	for index := range want {
		if feature.Attributes[index] != want[index] {
			t.Fatalf("attribute %d = %+v, want %+v", index, feature.Attributes[index], want[index])
		}
	}
}

func TestParseLicenseFileNormalizesVendorPositionalOptions(t *testing.T) {
	file, err := ParseLicenseFile(strings.NewReader("VENDOR acme /opt/acme /etc/acme.opt 27001\n"))
	if err != nil {
		t.Fatalf("ParseLicenseFile() error = %v", err)
	}
	if len(file.Vendors) != 1 {
		t.Fatalf("vendor count = %d", len(file.Vendors))
	}
	want := []LicenseAttribute{{Name: "OPTIONS", Value: "/etc/acme.opt", HasValue: true}, {Name: "PORT", Value: "27001", HasValue: true}}
	if got := file.Vendors[0].Attributes; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("attributes = %+v, want %+v", got, want)
	}
}

func TestParseLicenseFileAcceptsLegacyDaemonDirective(t *testing.T) {
	for _, directive := range []string{"DAEMON", "daemon"} {
		t.Run(directive, func(t *testing.T) {
			input := directive + " cdslmd /opt/cadence/bin/cdslmd /opt/cadence/cdslmd.opt port=27001\n"
			file, err := ParseLicenseFile(strings.NewReader(input))
			if err != nil {
				t.Fatalf("ParseLicenseFile() error = %v", err)
			}
			if len(file.Vendors) != 1 {
				t.Fatalf("vendor count = %d, want 1", len(file.Vendors))
			}
			vendor := file.Vendors[0]
			want := []LicenseAttribute{
				{Name: "OPTIONS", Value: "/opt/cadence/cdslmd.opt", HasValue: true},
				{Name: "port", Value: "27001", HasValue: true},
			}
			if vendor.Line != 1 || vendor.Name != "cdslmd" || vendor.DaemonPath != "/opt/cadence/bin/cdslmd" {
				t.Fatalf("vendor = %+v", vendor)
			}
			if len(vendor.Attributes) != len(want) || vendor.Attributes[0] != want[0] || vendor.Attributes[1] != want[1] {
				t.Fatalf("attributes = %+v, want %+v", vendor.Attributes, want)
			}
		})
	}
}

func TestParseLicenseFileContinuesQuotedAttributes(t *testing.T) {
	input := "FEATURE LicFileVersion cdslmd 2018.0 permanent 1 CHECKSUM \\\n" +
		"\tVENDOR_STRING=\"site one \\\n" +
		"\tcontinued\" DUP_GROUP=SITE \\\n" +
		"\tSIGN2=\"195E 5CA5 \\\n" +
		"\tB357 1DD3 \\\n" +
		"\tC0B1\" V7.1_LK=5F4C98A1"
	file, err := ParseLicenseFile(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseLicenseFile() error = %v", err)
	}
	if len(file.Features) != 1 {
		t.Fatalf("feature count = %d, want 1", len(file.Features))
	}
	feature := file.Features[0]
	want := []LicenseAttribute{
		{Name: "CHECKSUM"},
		{Name: "VENDOR_STRING", Value: "site one continued", HasValue: true},
		{Name: "DUP_GROUP", Value: "SITE", HasValue: true},
		{Name: "SIGN2", Value: "195E 5CA5 B357 1DD3 C0B1", HasValue: true},
		{Name: "V7.1_LK", Value: "5F4C98A1", HasValue: true},
	}
	if feature.Line != 1 || len(feature.Attributes) != len(want) {
		t.Fatalf("feature = %+v", feature)
	}
	for index := range want {
		if feature.Attributes[index] != want[index] {
			t.Fatalf("attribute %d = %+v, want %+v", index, feature.Attributes[index], want[index])
		}
	}
}
