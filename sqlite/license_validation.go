package sqlite

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danofsteel32/goflexlm"
	"github.com/danofsteel32/goflexlm/internal/licenserules"
)

type resolvedLicenseDate struct {
	Start      *int64 `json:"start_ns"`
	Expiration *int64 `json:"expiration_ns"`
}

func checkedInstant(t time.Time) (int64, error) {
	n := t.UnixNano()
	if !time.Unix(0, n).UTC().Equal(t.UTC()) {
		return 0, fmt.Errorf("instant outside nanosecond range: %s", t)
	}
	return n, nil
}

func validateLicenseDocument(file goflexlm.LicenseFile, zone *time.Location) ([]resolvedLicenseDate, error) {
	field := func(line int, values ...string) error {
		if line <= 0 {
			return fmt.Errorf("source line must be positive")
		}
		for _, s := range values {
			if strings.TrimSpace(s) == "" || !utf8.ValidString(s) {
				return fmt.Errorf("line %d: required field is blank or invalid UTF-8", line)
			}
		}
		return nil
	}
	attrs := func(line int, values []goflexlm.LicenseAttribute, vendor bool) error {
		for _, a := range values {
			if err := field(line, a.Name); err != nil {
				return err
			}
			if !utf8.ValidString(a.Value) || (!a.HasValue && (a.Value != "" || vendor)) {
				return fmt.Errorf("line %d: invalid attribute %q", line, a.Name)
			}
			if vendor && (licenserules.Upper(a.Name) == "PORT" || licenserules.Upper(a.Name) == "EPORT") {
				if err := licenserules.Port(a.Value); err != nil {
					return fmt.Errorf("line %d: %w", line, err)
				}
			}
		}
		return nil
	}
	for _, s := range file.Servers {
		if err := field(s.Line, s.Host, s.HostID); err != nil {
			return nil, err
		}
		if s.Port != nil && (*s.Port < 1 || *s.Port > 65535) {
			return nil, fmt.Errorf("line %d: invalid SERVER port", s.Line)
		}
		if err := attrs(s.Line, s.Attributes, false); err != nil {
			return nil, err
		}
	}
	for _, v := range file.Vendors {
		if err := field(v.Line, v.Name); err != nil {
			return nil, err
		}
		if !utf8.ValidString(v.DaemonPath) {
			return nil, fmt.Errorf("line %d: invalid daemon path", v.Line)
		}
		if err := attrs(v.Line, v.Attributes, true); err != nil {
			return nil, err
		}
	}
	dates := make([]resolvedLicenseDate, len(file.Features))
	for i, f := range file.Features {
		if err := field(f.Line, f.Name, f.Vendor, f.Version, f.Expiration); err != nil {
			return nil, err
		}
		if i > 0 && f.Line <= file.Features[i-1].Line {
			return nil, fmt.Errorf("line %d: feature lines must increase", f.Line)
		}
		if f.Kind != goflexlm.LicenseFeatureLine && f.Kind != goflexlm.LicenseIncrementLine {
			return nil, fmt.Errorf("line %d: invalid feature kind", f.Line)
		}
		if (f.Uncounted && f.Licenses != nil) || (!f.Uncounted && (f.Licenses == nil || *f.Licenses <= 0)) {
			return nil, fmt.Errorf("line %d: invalid count state", f.Line)
		}
		if err := attrs(f.Line, f.Attributes, false); err != nil {
			return nil, err
		}
		resolve := func(value string, finite bool) (*int64, error) {
			date, err := licenserules.Date(value, finite)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", f.Line, err)
			}
			if date == nil {
				return nil, nil
			}
			n, err := checkedInstant(time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, zone))
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", f.Line, err)
			}
			return &n, nil
		}
		exp, err := resolve(f.Expiration, false)
		if err != nil {
			return nil, err
		}
		dates[i].Expiration = exp
		seen := false
		for _, a := range f.Attributes {
			if licenserules.Upper(a.Name) != "START" {
				continue
			}
			if seen || !a.HasValue {
				return nil, fmt.Errorf("line %d: invalid or repeated START", f.Line)
			}
			seen = true
			start, err := resolve(a.Value, true)
			if err != nil {
				return nil, err
			}
			dates[i].Start = start
		}
	}
	return dates, nil
}
