package goflexlm

import "time"

// Kind identifies a parsed log event.
type Kind string

const (
	KindCheckout Kind = "checkout"
	KindCheckin  Kind = "checkin"
	KindDenied   Kind = "denied"
	KindQueued   Kind = "queued"
	KindDequeued Kind = "dequeued"
	KindMessage  Kind = "message"
)

// LogTime is the timestamp attached to a log record. Time is nil for an
// undated classic record until a dated record has established temporal context.
type LogTime struct {
	Original  string
	TimeOfDay time.Time
	Time      *time.Time
}

// Activity contains fields shared by license activity messages.
type Activity struct {
	Feature      string         `json:"feature"`
	User         string         `json:"user,omitempty"`
	Host         string         `json:"host,omitempty"`
	Licenses     int            `json:"licenses"`
	CheckoutData string         `json:"checkout_data,omitempty"`
	DenialReason string         `json:"denial_reason,omitempty"`
	DenialError  string         `json:"denial_error,omitempty"`
	Verbose      *VerboseFields `json:"verbose,omitempty"`
}

// VerboseFields contains the documented fields between verbose-message ^^^
// delimiters. Values are preserved as text because vendor daemons sometimes
// use placeholders such as N/A in otherwise numeric fields.
type VerboseFields struct {
	Version          string `json:"version"`
	User             string `json:"user"`
	IP               string `json:"ip"`
	PID              string `json:"pid"`
	Handle           string `json:"handle"`
	InUse            string `json:"in_use"`
	OutOf            string `json:"out_of"`
	ProjectName      string `json:"project_name"`
	FlexVersion      string `json:"flex_version"`
	FlexRevision     string `json:"flex_revision"`
	Date             string `json:"date"`
	Time             string `json:"time"`
	IsDuplicateGroup string `json:"is_dup_group"`
}

// Event is one valid FlexNet debug-log line.
type Event struct {
	Line     int
	Raw      string
	Daemon   string
	Message  string
	Kind     Kind
	LogTime  LogTime
	Activity *Activity
}

// Diagnostic describes invalid content. It does not stop decoding.
type Diagnostic struct {
	Line    int
	Raw     string
	Code    string
	Message string
}

// Stable diagnostic codes returned for malformed log content.
const (
	DiagnosticInvalidEnvelope  = "invalid_envelope"
	DiagnosticInvalidTimestamp = "invalid_timestamp"
	DiagnosticInvalidActivity  = "invalid_activity"
)

// Result contains exactly one Event or Diagnostic.
type Result struct {
	Event      *Event
	Diagnostic *Diagnostic
}
