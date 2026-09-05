package goflexlm

import (
	"bufio"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	classicEnvelope  = regexp.MustCompile(`^(\d{1,2}:\d{2}:\d{2}) \(([^()]+)\)(?: (.*))?$`)
	principalPattern = regexp.MustCompile(`^(?:"([^"]+)"|(\S+))\s+(\S+?)(?:\s+\[([^]]*)\])?(?:\s+\((\d+)(?:\s+licenses?)?\))?$`)
	deniedCount      = regexp.MustCompile(`^(\d+)\s+(?:"([^"]+)"|(\S+))\s+to\s+(\S+)$`)
	denialError      = regexp.MustCompile(`\((-?\d+)\s*,\s*(-?\d+)\)\s*$`)
)

// Decoder incrementally reads and parses a FlexNet Publisher debug log.
type Decoder struct {
	r       *bufio.Reader
	line    int
	result  Result
	err     error
	context *timeContext
}

type timeContext struct {
	date    time.Time
	zone    *time.Location
	lastTOD time.Duration
}

// NewDecoder returns a pull-based decoder reading from r.
func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: bufio.NewReader(r)} }

// Scan advances to the next non-blank input line.
func (d *Decoder) Scan() bool {
	d.result = Result{}
	if d.err != nil {
		return false
	}
	for {
		line, readErr := d.r.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			d.err = readErr
			return false
		}
		if len(line) == 0 {
			return false
		}
		d.line++
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			if readErr != nil {
				return false
			}
			continue
		}
		d.result = d.parseLine(line)
		return true
	}
}

// Result returns the item produced by the most recent successful Scan.
func (d *Decoder) Result() Result { return d.result }

// Err reports only failures while reading input, never malformed log content.
func (d *Decoder) Err() error { return d.err }

func (d *Decoder) parseLine(raw string) Result {
	original, daemon, message, tod, instant, code, err := parseEnvelope(raw)
	if err != nil {
		return diagnostic(d.line, raw, code, err.Error())
	}
	lt := LogTime{Original: original, TimeOfDay: tod}
	if instant != nil {
		t := *instant
		lt.Time = &t
		_, off := t.Zone()
		d.context = &timeContext{
			date:    time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()),
			zone:    time.FixedZone("", off),
			lastTOD: timeOfDayDuration(tod),
		}
	} else if d.context != nil {
		cur := timeOfDayDuration(tod)
		if cur < d.context.lastTOD {
			d.context.date = d.context.date.AddDate(0, 0, 1)
		}
		t := time.Date(d.context.date.Year(), d.context.date.Month(), d.context.date.Day(), tod.Hour(), tod.Minute(), tod.Second(), tod.Nanosecond(), d.context.zone)
		lt.Time = &t
		d.context.lastTOD = cur
	}

	kind, activity, recognized, err := parseActivity(message)
	if err != nil {
		return diagnostic(d.line, raw, DiagnosticInvalidActivity, err.Error())
	}
	if !recognized {
		kind = KindMessage
	}
	return Result{Event: &Event{Line: d.line, Raw: raw, Daemon: daemon, Message: message, Kind: kind, LogTime: lt, Activity: activity}}
}

func diagnostic(line int, raw, code, message string) Result {
	return Result{Diagnostic: &Diagnostic{Line: line, Raw: raw, Code: code, Message: message}}
}

func parseEnvelope(raw string) (string, string, string, time.Time, *time.Time, string, error) {
	first, rest, ok := strings.Cut(raw, " ")
	if !ok {
		return "", "", "", time.Time{}, nil, DiagnosticInvalidEnvelope, errors.New("expected timestamp, daemon, and message")
	}
	if strings.Contains(first, "T") || (len(first) >= 5 && first[4] == '-') {
		t, err := time.Parse(time.RFC3339Nano, first)
		if err != nil {
			return "", "", "", time.Time{}, nil, DiagnosticInvalidTimestamp, errors.New("invalid ISO 8601 timestamp")
		}
		daemon, message, ok := parseDaemonMessage(rest)
		if !ok {
			return "", "", "", time.Time{}, nil, DiagnosticInvalidEnvelope, errors.New("expected parenthesized daemon name")
		}
		tod := time.Date(0, 1, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
		return first, daemon, message, tod, &t, "", nil
	}

	m := classicEnvelope.FindStringSubmatch(raw)
	if m == nil {
		if strings.Contains(rest, "(") {
			return "", "", "", time.Time{}, nil, DiagnosticInvalidTimestamp, errors.New("invalid classic timestamp")
		}
		return "", "", "", time.Time{}, nil, DiagnosticInvalidEnvelope, errors.New("expected hh:mm:ss (daemon) message")
	}
	tod, err := time.Parse("15:04:05", padHour(m[1]))
	if err != nil {
		return "", "", "", time.Time{}, nil, DiagnosticInvalidTimestamp, errors.New("invalid classic timestamp")
	}
	return m[1], m[2], m[3], tod, nil, "", nil
}

func parseDaemonMessage(s string) (string, string, bool) {
	if !strings.HasPrefix(s, "(") {
		return "", "", false
	}
	end := strings.IndexByte(s, ')')
	if end < 2 || (len(s) > end+1 && s[end+1] != ' ') {
		return "", "", false
	}
	message := ""
	if len(s) > end+1 {
		message = s[end+2:]
	}
	return s[1:end], message, true
}

func padHour(s string) string {
	if len(s) == 7 {
		return "0" + s
	}
	return s
}

func timeOfDayDuration(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond())
}

func parseActivity(message string) (Kind, *Activity, bool, error) {
	kinds := []struct {
		prefix string
		kind   Kind
	}{
		{"OUT:", KindCheckout}, {"IN:", KindCheckin}, {"DENIED:", KindDenied},
		{"QUEUED:", KindQueued}, {"DEQUEUED:", KindDequeued},
	}
	for _, candidate := range kinds {
		if !strings.HasPrefix(message, candidate.prefix) {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(message, candidate.prefix))
		if body == "" {
			return candidate.kind, nil, true, errors.New("activity has no fields")
		}
		a, err := parseActivityBody(candidate.kind, body)
		return candidate.kind, a, true, err
	}
	return KindMessage, nil, false, nil
}

func parseActivityBody(kind Kind, body string) (*Activity, error) {
	if strings.Contains(body, "^^^") {
		return parseVerbose(kind, body)
	}
	if kind == KindDenied {
		return parseDenied(body)
	}
	return parsePrincipal(body, false)
}

func parsePrincipal(body string, verbose bool) (*Activity, error) {
	m := principalPattern.FindStringSubmatch(body)
	if m == nil {
		return nil, errors.New("activity does not match a documented compact format")
	}
	feature := m[1]
	if feature == "" {
		feature = m[2]
	}
	principal := m[3]
	a := &Activity{Feature: feature, Licenses: 1, CheckoutData: m[4]}
	if m[5] != "" {
		n, err := strconv.Atoi(m[5])
		if err != nil || n < 1 {
			return nil, errors.New("license count must be a positive integer")
		}
		a.Licenses = n
	}
	if strings.HasPrefix(principal, "@") {
		a.Host = strings.TrimPrefix(principal, "@")
	} else if at := strings.LastIndexByte(principal, '@'); at >= 0 {
		a.User, a.Host = principal[:at], principal[at+1:]
	} else if !verbose {
		a.User = principal
	} else {
		return nil, errors.New("verbose activity requires @host")
	}
	if a.Host == "" && strings.Contains(principal, "@") {
		return nil, errors.New("activity has an empty host")
	}
	return a, nil
}

func parseDenied(body string) (*Activity, error) {
	if m := deniedCount.FindStringSubmatch(body); m != nil {
		feature := m[2]
		if feature == "" {
			feature = m[3]
		}
		n, _ := strconv.Atoi(m[1])
		a := &Activity{Feature: feature, Licenses: n}
		setPrincipal(a, m[4])
		if n < 1 {
			return nil, errors.New("license count must be a positive integer")
		}
		return a, nil
	}

	feature, rest, ok := takeFeature(body)
	if !ok {
		return nil, errors.New("denial has no feature")
	}
	principal, detail := rest, ""
	if i := strings.Index(rest, " ("); i >= 0 && strings.HasSuffix(rest, ")") {
		principal, detail = rest[:i], rest[i+2:len(rest)-1]
	}
	if strings.TrimSpace(principal) == "" {
		return nil, errors.New("denial has no user or host")
	}
	a := &Activity{Feature: feature, Licenses: 1, DenialReason: detail}
	setPrincipal(a, principal)
	if m := denialError.FindStringSubmatch(detail); m != nil {
		a.DenialError = m[1] + "," + m[2]
		a.DenialReason = strings.TrimSpace(detail[:len(detail)-len(m[0])])
	}
	return a, nil
}

func parseVerbose(kind Kind, body string) (*Activity, error) {
	parts := strings.Split(body, "^^^")
	if len(parts) != 3 || strings.TrimSpace(parts[2]) != "" {
		return nil, errors.New("verbose activity must have one matching ^^^ delimiter pair")
	}
	fields := strings.Fields(parts[1])
	if len(fields) < 13 {
		return nil, errors.New("verbose activity must contain all 13 metadata fields")
	}
	last := len(fields) - 5
	v := &VerboseFields{
		Version: fields[0], User: fields[1], IP: fields[2], PID: fields[3], Handle: fields[4],
		InUse: fields[5], OutOf: fields[6], ProjectName: strings.Join(fields[7:last], " "), FlexVersion: fields[last],
		FlexRevision: fields[last+1], Date: fields[last+2], Time: fields[last+3], IsDuplicateGroup: fields[last+4],
	}

	head := strings.TrimSpace(parts[0])
	var a *Activity
	var err error
	if kind == KindDenied {
		a, err = parseDenied(head)
	} else {
		a, err = parsePrincipal(head, true)
	}
	if err != nil {
		return nil, err
	}
	a.User = v.User
	a.Verbose = v
	return a, nil
}

func takeFeature(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		end := strings.Index(s[1:], `"`)
		if end < 0 {
			return "", "", false
		}
		end++
		return s[1:end], strings.TrimSpace(s[end+1:]), true
	}
	feature, rest, ok := strings.Cut(s, " ")
	return feature, strings.TrimSpace(rest), ok && feature != ""
}

func setPrincipal(a *Activity, principal string) {
	principal = strings.TrimSpace(principal)
	if strings.HasPrefix(principal, "@") {
		a.Host = principal[1:]
	} else if at := strings.LastIndexByte(principal, '@'); at >= 0 {
		a.User, a.Host = principal[:at], principal[at+1:]
	} else {
		a.User = principal
	}
}
