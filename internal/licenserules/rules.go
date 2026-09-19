// Package licenserules contains syntax rules shared by license parsing and validation.
package licenserules

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Upper performs ASCII-only case normalization.
func Upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// Decimal accepts unsigned decimal values representable as int.
func Decimal(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty decimal")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid decimal %q", s)
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("decimal out of range: %q", s)
	}
	return n, nil
}

// Date returns a calendar date or nil for a non-expiring expiration.
func Date(s string, finite bool) (*time.Time, error) {
	if Upper(s) == "PERMANENT" && !finite {
		return nil, nil
	}
	p := strings.Split(s, "-")
	if len(p) != 3 || len(p[0]) < 1 || len(p[0]) > 2 || len(p[1]) != 3 || len(p[2]) < 1 || len(p[2]) > 4 {
		return nil, fmt.Errorf("invalid date %q", s)
	}
	day, e1 := Decimal(p[0])
	year, e2 := Decimal(p[2])
	month := 0
	for i, m := range []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"} {
		if Upper(p[1]) == m {
			month = i + 1
		}
	}
	if e1 != nil || e2 != nil || month == 0 || day < 1 || day > 31 {
		return nil, fmt.Errorf("invalid date %q", s)
	}
	if year == 0 || year == 1900 {
		if finite {
			return nil, fmt.Errorf("START must be finite: %q", s)
		}
		return nil, nil
	}
	if len(p[2]) != 4 {
		return nil, fmt.Errorf("invalid year %q", p[2])
	}
	d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if d.Day() != day || int(d.Month()) != month {
		return nil, fmt.Errorf("invalid date %q", s)
	}
	return &d, nil
}

// Port validates the range of a vendor port.
func Port(s string) error {
	n, err := Decimal(s)
	if err != nil || n > 65535 {
		return fmt.Errorf("invalid vendor port %q", s)
	}
	return nil
}
