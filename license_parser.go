package goflexlm

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/danofsteel32/goflexlm/internal/licenserules"
)

type licenseToken struct {
	text      string
	line      int
	attribute bool
}

type licenseLexer struct {
	tokens    []licenseToken
	token     strings.Builder
	tokenLine int
	attribute bool
	quoted    bool
}

// ParseLicenseFile reads a complete supported FlexLM license document.
// Any malformed record or reader failure returns a zero document and a line error.
// Working memory beyond the returned document is proportional to the longest logical line.
func ParseLicenseFile(r io.Reader) (LicenseFile, error) {
	file := LicenseFile{Servers: []LicenseServer{}, Vendors: []LicenseVendor{}, Features: []LicenseFeature{}}
	reader := bufio.NewReader(r)
	physical := 0
	for {
		start := physical + 1
		var lexer licenseLexer
		for {
			raw, err := reader.ReadString('\n')
			if err != nil && err != io.EOF {
				return LicenseFile{}, fmt.Errorf("parse license file: line %d: read: %w", physical+1, err)
			}
			if len(raw) == 0 && err == io.EOF {
				if physical >= start {
					return LicenseFile{}, licenseError(start, "unfinished continuation")
				}
				return file, nil
			}
			physical++
			raw = strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
			if physical == start && (strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#")) {
				break
			}
			continued, lexErr := lexer.scanLine(raw, physical)
			if lexErr != nil {
				return LicenseFile{}, lexErr
			}
			if !continued {
				break
			}
			if err == io.EOF {
				return LicenseFile{}, licenseError(start, "unfinished continuation")
			}
		}
		if len(lexer.tokens) == 0 {
			continue
		}
		if err := parseLicenseRecord(&file, lexer.tokens, start); err != nil {
			return LicenseFile{}, err
		}
	}
}

func licenseError(line int, format string, args ...any) error {
	return fmt.Errorf("parse license file: line %d: %s", line, fmt.Sprintf(format, args...))
}

func licenseSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f' }

func (l *licenseLexer) scanLine(s string, line int) (bool, error) {
	if !utf8.ValidString(s) {
		return false, licenseError(line, "invalid UTF-8")
	}
	// Find the only possible continuation marker once. Checking the suffix at
	// every backslash repeatedly scans trailing whitespace on long values.
	continuation := len(strings.TrimRightFunc(s, unicode.IsSpace)) - 1
	if continuation >= 0 && s[continuation] != '\\' {
		continuation = -1
	}
	i := 0
	if l.quoted {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		continued, err := l.scanQuoted(s, &i, line, continuation)
		if err != nil || continued {
			return continued, err
		}
	}
	for i < len(s) {
		for i < len(s) && licenseSpace(s[i]) {
			i++
		}
		if i == len(s) {
			break
		}
		if i == continuation {
			return true, nil
		}
		l.tokenLine = line
		for i < len(s) && !licenseSpace(s[i]) {
			if i == continuation {
				if l.tokenLine != 0 {
					l.emitToken()
				}
				return true, nil
			}
			if s[i] == '"' {
				if l.token.Len() != 0 && !strings.HasSuffix(l.token.String(), "=") {
					return false, licenseError(line, "quote must start a token or attribute value")
				}
				i++
				l.quoted = true
				continued, err := l.scanQuoted(s, &i, line, continuation)
				if err != nil || continued {
					return continued, err
				}
				break
			}
			if s[i] == '=' {
				l.attribute = true
			}
			l.token.WriteByte(s[i])
			i++
		}
		if l.tokenLine != 0 {
			l.emitToken()
		}
	}
	return false, nil
}

func (l *licenseLexer) scanQuoted(s string, i *int, line, continuation int) (bool, error) {
	begin := *i
	for *i < len(s) {
		if s[*i] == '"' {
			l.token.WriteString(s[begin:*i])
			(*i)++
			l.quoted = false
			if *i < len(s) && !licenseSpace(s[*i]) {
				return false, licenseError(line, "text after closing quote")
			}
			l.emitToken()
			return false, nil
		}
		if *i == continuation {
			l.token.WriteString(s[begin:*i])
			return true, nil
		}
		(*i)++
	}
	return false, licenseError(line, "unterminated quote")
}

func (l *licenseLexer) emitToken() {
	l.tokens = append(l.tokens, licenseToken{l.token.String(), l.tokenLine, l.attribute})
	l.token.Reset()
	l.tokenLine = 0
	l.attribute = false
}

func requiredLicenseTokens(t []licenseToken, n, start int) error {
	if len(t) < n {
		return licenseError(start, "incomplete %s record", t[0].text)
	}
	for _, v := range t[:n] {
		if strings.TrimSpace(v.text) == "" {
			return licenseError(v.line, "required field is blank")
		}
	}
	return nil
}

func licenseAttribute(t licenseToken) (LicenseAttribute, error) {
	a := LicenseAttribute{Name: t.text}
	if t.attribute {
		a.Name, a.Value, _ = strings.Cut(t.text, "=")
		a.HasValue = true
	}
	if strings.TrimSpace(a.Name) == "" {
		return a, licenseError(t.line, "attribute name is blank")
	}
	return a, nil
}

func parseLicenseRecord(file *LicenseFile, t []licenseToken, start int) error {
	switch licenserules.Upper(t[0].text) {
	case "USE_SERVER":
		if len(t) != 1 || file.UseServer {
			return licenseError(start, "invalid or duplicate USE_SERVER")
		}
		file.UseServer = true
	case "SERVER":
		if err := requiredLicenseTokens(t, 3, start); err != nil {
			return err
		}
		s := LicenseServer{Line: start, Host: t[1].text, HostID: t[2].text}
		rest := t[3:]
		if len(rest) > 0 && len(rest[0].text) > 0 && !rest[0].attribute && ((rest[0].text[0] >= '0' && rest[0].text[0] <= '9') || rest[0].text[0] == '-' || rest[0].text[0] == '+') {
			port, err := licenserules.Decimal(rest[0].text)
			if err != nil || port < 1 || port > 65535 {
				return licenseError(rest[0].line, "invalid SERVER port")
			}
			s.Port = &port
			rest = rest[1:]
		}
		for _, v := range rest {
			a, err := licenseAttribute(v)
			if err != nil {
				return err
			}
			s.Attributes = append(s.Attributes, a)
		}
		file.Servers = append(file.Servers, s)
	case "VENDOR", "DAEMON":
		if err := requiredLicenseTokens(t, 2, start); err != nil {
			return err
		}
		v := LicenseVendor{Line: start, Name: t[1].text}
		position := 0
		attributes := false
		for _, token := range t[2:] {
			a, err := licenseAttribute(token)
			if err != nil {
				return err
			}
			if !a.HasValue {
				if attributes || position > 2 {
					return licenseError(token.line, "VENDOR requires key=value after positional fields")
				}
				if strings.TrimSpace(token.text) == "" {
					return licenseError(token.line, "blank VENDOR positional field")
				}
				switch position {
				case 0:
					v.DaemonPath = token.text
				case 1:
					a = LicenseAttribute{Name: "OPTIONS", Value: token.text, HasValue: true}
				case 2:
					a = LicenseAttribute{Name: "PORT", Value: token.text, HasValue: true}
				}
				position++
				if position == 1 {
					continue
				}
			} else {
				attributes = true
			}
			if licenserules.Upper(a.Name) == "PORT" || licenserules.Upper(a.Name) == "EPORT" {
				if err := licenserules.Port(a.Value); err != nil {
					return licenseError(token.line, "%v", err)
				}
			}
			v.Attributes = append(v.Attributes, a)
		}
		file.Vendors = append(file.Vendors, v)
	case "FEATURE", "INCREMENT":
		if err := requiredLicenseTokens(t, 6, start); err != nil {
			return err
		}
		f := LicenseFeature{Line: start, Kind: LicenseFeatureKind(licenserules.Upper(t[0].text)), Name: t[1].text, Vendor: t[2].text, Version: t[3].text, Expiration: t[4].text}
		if _, err := licenserules.Date(f.Expiration, false); err != nil {
			return licenseError(t[4].line, "%v", err)
		}
		if licenserules.Upper(t[5].text) == "UNCOUNTED" {
			f.Uncounted = true
		} else {
			n, err := licenserules.Decimal(t[5].text)
			if err != nil {
				return licenseError(t[5].line, "%v", err)
			}
			if n == 0 {
				f.Uncounted = true
			} else {
				f.Licenses = &n
			}
		}
		hasStart := false
		for _, token := range t[6:] {
			a, err := licenseAttribute(token)
			if err != nil {
				return err
			}
			if licenserules.Upper(a.Name) == "START" {
				if hasStart || !a.HasValue {
					return licenseError(token.line, "invalid or repeated START")
				}
				if _, err := licenserules.Date(a.Value, true); err != nil {
					return licenseError(token.line, "%v", err)
				}
				hasStart = true
			}
			f.Attributes = append(f.Attributes, a)
		}
		file.Features = append(file.Features, f)
	default:
		return licenseError(start, "unsupported directive %q", t[0].text)
	}
	return nil
}
