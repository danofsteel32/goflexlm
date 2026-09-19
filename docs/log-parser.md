# Debug-log parser

The root package decodes debug logs one non-blank physical line at a time. It uses
only the Go standard library. Import it as
`github.com/danofsteel32/goflexlm`.

## Decode a stream

This complete example reads standard input, prints event kinds, and reports
malformed content without discarding later events:

```go
package main

import (
	"fmt"
	"os"

	"github.com/danofsteel32/goflexlm"
)

func main() {
	decoder := goflexlm.NewDecoder(os.Stdin)
	for decoder.Scan() {
		result := decoder.Result()
		if d := result.Diagnostic; d != nil {
			fmt.Fprintf(os.Stderr, "line %d: %s: %s\n", d.Line, d.Code, d.Message)
			continue
		}
		fmt.Println(result.Event.Kind, result.Event.Message)
	}
	if err := decoder.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Each successful `Scan` produces exactly one `Event` or `Diagnostic` in `Result`.
Always check `Err` after scanning stops: it reports reader failures only. The
example handles diagnostics separately; applications choose whether those make
the overall operation unsuccessful. The supplied CLI returns status 1 for them.

The decoder accepts LF, CRLF, and a final line without a newline. Physical line
numbers include skipped blank lines. `Raw` preserves the source line without its
line ending. Memory use scales with the longest line being processed; there is no
fixed scanner token limit. Each decoder owns its timestamp context.

## Supported envelopes and activities

```text
09:10:11 (acme) OUT: "editor" alex@workstation
2026-09-01T09:10:11-04:00 (acme) IN: "editor" alex@workstation
```

Classic envelopes use `h:mm:ss` or `hh:mm:ss`; dated envelopes use RFC3339
timestamps, including fractional seconds and an explicit UTC offset or `Z`.
The parenthesized name becomes `Event.Daemon` and the following text becomes
`Event.Message`.

| Message prefix | Event kind |
| --- | --- |
| `OUT:` | `checkout` |
| `IN:` | `checkin` |
| `DENIED:` | `denied` |
| `QUEUED:` | `queued` |
| `DEQUEUED:` | `dequeued` |
| Other valid message | `message` |

Recognized prefixes are case-sensitive. Compact activities support quoted or
unquoted feature names, user/host information, optional bracketed checkout data,
and a trailing positive license count. An omitted count means one. For example:

```text
09:10:11 (acme) OUT: "editor" alex@workstation [checkout-42] (2 licenses)
```

Verbose activities use `^^^` delimiters. Their version, user, IP, PID, handle,
in-use count, total count, project, Flex version/revision, date, time, and duplicate
group fields remain text in `Activity.Verbose`, preserving placeholders such as
`N/A`. Denial reason and error details are available on `Activity`. See the
[parser tests](../decoder_test.go) for accepted compact and verbose variants.

Unknown messages with valid envelopes remain `KindMessage` events with no
activity. Malformed recognized activities produce diagnostics instead.

## Timestamp resolution

`Event.LogTime.Original` is the source timestamp text. `TimeOfDay` describes the
clock component; `Time` points to a resolved instant when one is available.

A classic line before the first dated envelope has `Time == nil`. A dated envelope
establishes a calendar date and a fixed UTC offset for subsequent classic lines.
Whenever a subsequent classic clock moves backward, the decoder advances its
date by one day. A later dated envelope resets the context. Earlier unresolved
events are not retroactively updated.

This rule assumes chronological input: an out-of-order classic line can look like
midnight rollover. It also carries a fixed offset, not an IANA timezone with
daylight-saving rules. Each separate input decoder starts without a date.

## Diagnostics

| Stable code | Meaning |
| --- | --- |
| `invalid_envelope` | The line cannot be split into the expected envelope |
| `invalid_timestamp` | A classic or dated timestamp is malformed |
| `invalid_activity` | A recognized activity has malformed fields |

Diagnostics contain `Line`, `Raw`, `Code`, and a human-readable `Message`.
Use `Code` for programmatic decisions. Content diagnostics do not stop scanning.

## JSON Lines output

The `goflexlm` command uses a dedicated JSON representation; directly marshaling
the Go `Event` struct is not the same output contract.

| Field | Meaning |
| --- | --- |
| `line`, `raw` | Physical line number and original line without ending bytes |
| `time` | Original timestamp text |
| `timestamp` | Resolved RFC3339Nano instant; omitted when unresolved |
| `daemon`, `kind`, `message` | Parsed envelope and event classification |
| `activity` | Activity fields; omitted for generic messages |

Activity always includes `feature` and `licenses`. Empty optional user, host,
checkout-data, and denial fields are omitted. When present, `verbose` contains
the text fields declared in [types.go](../types.go). Diagnostics go to standard
error, never into the JSON Lines event stream. See [CLI usage](cli.md).
