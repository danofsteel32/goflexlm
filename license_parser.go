package goflexlm

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseLicenseFile parses a supported FlexLM license document.
func ParseLicenseFile(r io.Reader) (LicenseFile, error) {
	file := LicenseFile{Servers: []LicenseServer{}, Vendors: []LicenseVendor{}, Features: []LicenseFeature{}}
	reader := bufio.NewReader(r)
	line := 0
	for {
		text, startLine, err := readLicenseLogicalLine(reader, &line)
		if err == io.EOF {
			return file, nil
		}
		if err != nil {
			return LicenseFile{}, fmt.Errorf("parse license file line %d: read: %w", startLine, err)
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			fields, lexErr := licenseFields(trimmed)
			if lexErr != nil {
				return LicenseFile{}, fmt.Errorf("parse license file line %d: %w", startLine, lexErr)
			}
			switch {
			case len(fields) > 0 && strings.EqualFold(fields[0], "SERVER"):
				server, parseErr := parseLicenseServer(startLine, fields)
				if parseErr != nil {
					return LicenseFile{}, parseErr
				}
				file.Servers = append(file.Servers, server)
			case len(fields) > 0 && strings.EqualFold(fields[0], "VENDOR"):
				vendor, parseErr := parseLicenseVendor(startLine, fields)
				if parseErr != nil {
					return LicenseFile{}, parseErr
				}
				file.Vendors = append(file.Vendors, vendor)
			case len(fields) > 0 && (strings.EqualFold(fields[0], "FEATURE") || strings.EqualFold(fields[0], "INCREMENT")):
				feature, parseErr := parseLicenseFeature(startLine, fields)
				if parseErr != nil {
					return LicenseFile{}, parseErr
				}
				file.Features = append(file.Features, feature)
			case len(fields) > 0 && strings.EqualFold(fields[0], "USE_SERVER"):
				if len(fields) != 1 || file.UseServer {
					return LicenseFile{}, fmt.Errorf("parse license file line %d: invalid USE_SERVER directive", startLine)
				}
				file.UseServer = true
			default:
				return LicenseFile{}, fmt.Errorf("parse license file line %d: unsupported directive", startLine)
			}
		}
	}
}

func readLicenseLogicalLine(reader *bufio.Reader, line *int) (string, int, error) {
	var parts []string
	startLine := *line + 1
	for {
		text, err := reader.ReadString('\n')
		if err == io.EOF && len(text) == 0 {
			if len(parts) == 0 {
				return "", 0, io.EOF
			}
			return "", startLine, io.ErrUnexpectedEOF
		}
		*line++
		text = strings.TrimSuffix(strings.TrimSuffix(text, "\n"), "\r")
		continued := strings.HasSuffix(text, "\\")
		if continued {
			text = strings.TrimSuffix(text, "\\")
		}
		parts = append(parts, text)
		if err != nil && err != io.EOF {
			return "", startLine, err
		}
		if !continued {
			return strings.Join(parts, " "), startLine, nil
		}
		if err == io.EOF {
			return "", startLine, io.ErrUnexpectedEOF
		}
	}
}

func licenseFields(text string) ([]string, error) {
	var fields []string
	var field strings.Builder
	inQuote := false
	flush := func() {
		if field.Len() > 0 {
			fields = append(fields, field.String())
			field.Reset()
		}
	}
	for _, character := range text {
		switch {
		case character == '"':
			inQuote = !inQuote
		case (character == ' ' || character == '\t') && !inQuote:
			flush()
		default:
			field.WriteRune(character)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return fields, nil
}

func parseLicenseVendor(line int, fields []string) (LicenseVendor, error) {
	if len(fields) < 2 {
		return LicenseVendor{}, fmt.Errorf("parse license file line %d: VENDOR requires a name", line)
	}
	vendor := LicenseVendor{Line: line, Name: fields[1]}
	position := 0
	for _, field := range fields[2:] {
		if !strings.Contains(field, "=") {
			switch position {
			case 0:
				vendor.DaemonPath = field
			case 1:
				vendor.Attributes = append(vendor.Attributes, LicenseAttribute{Name: "OPTIONS", Value: field, HasValue: true})
			case 2:
				port, err := strconv.Atoi(field)
				if err != nil || port < 0 || port > 65535 {
					return LicenseVendor{}, fmt.Errorf("parse license file line %d: invalid VENDOR port", line)
				}
				vendor.Attributes = append(vendor.Attributes, LicenseAttribute{Name: "PORT", Value: field, HasValue: true})
			default:
				return LicenseVendor{}, fmt.Errorf("parse license file line %d: invalid VENDOR attribute", line)
			}
			position++
			continue
		}
		attribute, hasValue, err := parseLicenseAttribute(field)
		if err != nil {
			return LicenseVendor{}, fmt.Errorf("parse license file line %d: invalid VENDOR attribute", line)
		}
		if !hasValue {
			return LicenseVendor{}, fmt.Errorf("parse license file line %d: invalid VENDOR attribute", line)
		}
		vendor.Attributes = append(vendor.Attributes, attribute)
	}
	return vendor, nil
}

func parseLicenseFeature(line int, fields []string) (LicenseFeature, error) {
	if len(fields) < 6 {
		return LicenseFeature{}, fmt.Errorf("parse license file line %d: feature record requires five fields", line)
	}
	licenses, uncounted, err := parseLicenseCount(fields[5])
	if err != nil {
		return LicenseFeature{}, fmt.Errorf("parse license file line %d: invalid license count", line)
	}
	kind := LicenseFeatureLine
	if strings.EqualFold(fields[0], "INCREMENT") {
		kind = LicenseIncrementLine
	}
	feature := LicenseFeature{Line: line, Kind: kind, Name: fields[1], Vendor: fields[2], Version: fields[3], Expiration: fields[4], Licenses: licenses, Uncounted: uncounted}
	for _, field := range fields[6:] {
		attribute, _, err := parseLicenseAttribute(field)
		if err != nil {
			return LicenseFeature{}, fmt.Errorf("parse license file line %d: invalid feature attribute", line)
		}
		feature.Attributes = append(feature.Attributes, attribute)
	}
	return feature, nil
}

func parseLicenseAttribute(field string) (LicenseAttribute, bool, error) {
	name, value, hasValue := strings.Cut(field, "=")
	if name == "" {
		return LicenseAttribute{}, false, fmt.Errorf("empty attribute name")
	}
	if !hasValue {
		return LicenseAttribute{Name: name}, false, nil
	}
	return LicenseAttribute{Name: name, Value: value, HasValue: true}, true, nil
}

func parseLicenseCount(text string) (*int, bool, error) {
	if strings.EqualFold(text, "uncounted") || text == "0" {
		return nil, true, nil
	}
	count, err := strconv.Atoi(text)
	if err != nil || count <= 0 {
		return nil, false, fmt.Errorf("invalid count")
	}
	return &count, false, nil
}

func parseLicenseServer(line int, fields []string) (LicenseServer, error) {
	if len(fields) != 4 {
		return LicenseServer{}, fmt.Errorf("parse license file line %d: SERVER requires host, host ID, and port", line)
	}
	port, err := strconv.Atoi(fields[3])
	if err != nil || port < 1 || port > 65535 {
		return LicenseServer{}, fmt.Errorf("parse license file line %d: invalid SERVER port", line)
	}
	return LicenseServer{Line: line, Host: fields[1], HostID: fields[2], Port: &port}, nil
}
