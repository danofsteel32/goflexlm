package goflexlm

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestLicenseQuotedContinuationWhitespace(t *testing.T) {
	for _, prefix := range []string{"", " \t", "\v", "\f", "\r", "\u00a0"} {
		t.Run(fmt.Sprintf("%q", prefix), func(t *testing.T) {
			input := "FEATURE f v 1 permanent 1 DATA=\"first\\ \t\u00a0\n" + prefix + "last\" NEXT=value"
			doc, err := ParseLicenseFile(strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			want := []LicenseAttribute{
				{Name: "DATA", Value: "first" + strings.TrimLeft(prefix, " \t") + "last", HasValue: true},
				{Name: "NEXT", Value: "value", HasValue: true},
			}
			if got := doc.Features[0].Attributes; !reflect.DeepEqual(got, want) {
				t.Fatalf("attributes = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLicenseMultilineTokenErrorLocations(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"FEATURE f v 1 permanent 1 A=\"first\\\nlast\"junk", "line 2: text after closing quote"},
		{"FEATURE f v 1 permanent 1 A=\"first\\\nlast\"\\", "line 2: text after closing quote"},
		{"FEATURE f v 1 permanent 1 A=\"first\\\n\xff\"", "line 2: invalid UTF-8"},
		{"FEATURE f v 1 permanent 1 \\\nSTART=\"bad\\\ndate\"", "line 2: invalid date"},
		{"SERVER h id\nFEATURE f v 1 permanent 1 A=\"first\\\nlast\\\n", "line 2: unfinished continuation"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			doc, err := ParseLicenseFile(strings.NewReader(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.want) || !reflect.DeepEqual(doc, LicenseFile{}) {
				t.Fatalf("document=%+v error=%v, want %q", doc, err, tc.want)
			}
		})
	}
}

func BenchmarkLicenseBackslashContinuation(b *testing.B) {
	for _, quoted := range []bool{false, true} {
		for _, size := range []int{1024, 16384} {
			b.Run(fmt.Sprintf("quoted=%t/bytes=%d", quoted, size), func(b *testing.B) {
				value := strings.Repeat("\\", size)
				input := "FEATURE f v 1 permanent 1 DATA="
				if quoted {
					input += "\""
				}
				input += value + "\\" + strings.Repeat(" ", size) + "\n"
				if quoted {
					input += "\" "
				}
				input += "NEXT=value"
				b.SetBytes(int64(len(input)))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					doc, err := ParseLicenseFile(strings.NewReader(input))
					if err != nil {
						b.Fatal(err)
					}
					if got := doc.Features[0].Attributes[0].Value; got != value {
						b.Fatalf("value length = %d, want %d", len(got), len(value))
					}
				}
			})
		}
	}
}
