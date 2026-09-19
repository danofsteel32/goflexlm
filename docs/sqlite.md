# SQLite storage and analytics

The optional `github.com/danofsteel32/goflexlm/sqlite` package stores debug-log
activity, derives usage and queue sessions, and compares usage with dated license
snapshots. It uses the pure-Go `modernc.org/sqlite` driver. Importing the root
parser package does not require importing the SQLite package.

## Use the store in Go

This function imports a caller-owned reader into a database. The caller provides
the context and remains responsible for closing the reader:

```go
package example

import (
	"context"
	"io"

	"github.com/danofsteel32/goflexlm/sqlite"
)

func importLog(ctx context.Context, path string, input io.Reader) (sqlite.ImportResult, error) {
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return sqlite.ImportResult{}, err
	}
	result, importErr := db.Import(ctx, sqlite.ImportRequest{
		Reader: input, Pool: "engineering", Stream: "server-a", SourceName: "lmgrd.log",
	})
	closeErr := db.Close()
	if importErr != nil {
		return result, importErr
	}
	return result, closeErr
}
```

`OpenOptions.MaxOpenConns` defaults to four; a negative value is invalid. Use a
filesystem path for persistence or `:memory:` for a store-local in-memory database.
`Open` initializes schema version 2 and refreshes stale session derivations.

| Store method | Purpose |
| --- | --- |
| `Import(ctx, ImportRequest)` | Atomically import one log source and derive sessions |
| `ImportLicenseFile(ctx, LicenseImportRequest)` | Validate and replace an authoritative capacity snapshot |
| `Rebuild(ctx, pool)` | Reconstruct usage and queue sessions |
| `Capacity(ctx, AnalyticsQuery)` | Report vendor/feature usage and capacity segments |
| `Denials(ctx, AnalyticsQuery)` | Group denials by feature, reason, and error code |
| `Queueing(ctx, AnalyticsQuery)` | Report queue depth, volume, and waits |
| `Close()` | Release database resources |

`AnalyticsQuery` requires a pool and increasing `From`/`To` instants. `Feature`
is optional. `Bucket` accepts the exported hour/day/week/month constants or the
empty value; `Timezone == nil` means UTC. See [types.go](../sqlite/types.go) for
the complete request and report field definitions.

## Imports and provenance

A pool is a purchaser-defined grouping of usage and licenses. A stream represents
a logical log history within that pool. Activity stores its source name, line,
raw text, parsed fields, and resolved timestamp when available. Generic messages
are omitted from activity storage and counted in the import summary.

The SHA-256 digest covers exact source bytes, including blank lines and line
endings. Deduplication is per stream, not by filename or individual event. Different
files with overlapping log content can therefore add overlapping activity.

Each import creates its own decoder. A classic line at the start of a rotated
file remains unresolved until a dated envelope in that file establishes context;
a preceding file does not supply it. Unresolved activity is retained for audit
but excluded from sessions and time-based measures.

`ImportResult` reports the digest, duplicate flag, byte and physical-line counts,
activity count, omitted messages, diagnostic counts by code, and unresolved
timestamp count. Content diagnostics do not fail the transaction. An optional
`OnDiagnostic` callback receives each diagnostic; returning an error aborts the
import. Reader, database, and cancellation failures also prevent a partial commit.

## Session matching

Usage sessions pair OUT with IN; queue sessions pair QUEUED with DEQUEUED. Matching
stays within pool, stream, daemon, feature, and session type. Resolved events are
processed chronologically with deterministic tie ordering. Chronological imports
can continue open allocations; older imports cause a full session rebuild.

Candidates must not conflict on any usable identity field. Matching prefers,
in order: handle, checkout data, PID plus IP, user plus host, host alone, then
user alone. Placeholder values such as `N/A`, `UNKNOWN`, and `-` are not usable
identity evidence. Host-only and user-only matches are marked weak.

Quantities are preserved: an IN for two licenses can consume part of an OUT for
three, leaving one open. Multiple best candidates mark the result ambiguous.
Unmatched closing quantities become orphans. Unclosed opening quantities remain
open. Generic daemon messages do not automatically close outstanding sessions.

## Interpret reports

All report ranges are half-open: `From <= timestamp < To`. Calendar buckets use
the query timezone; weeks start on Monday. UTC instants remain the storage basis.
Capacity uses exact daemon/vendor and feature spelling and sorts rows by vendor,
feature, and segment start. Denial and queue reports group by feature across vendors.

### Capacity

Rows split at calendar boundaries and capacity changes. They include identities
with capacity but no usage, as well as identities with usage but no capacity.

| `purchased` | `uncounted` | Interpretation |
| --- | --- | --- |
| Positive integer | false | Known finite capacity |
| 0 | false | Known finite zero |
| null | false | Missing capacity history |
| null | true | Known unlimited capacity |

The lower usage estimate includes closed, unambiguous sessions. The upper estimate
also includes ambiguous and open sessions, clipping open usage at the report end.
Orphans supply no usage interval. These are estimates from the available log
evidence, not a reconstruction of events missing from the logs.

Each estimate includes peak concurrent licenses and used seat-nanoseconds.
For finite capacity, the report also calculates saturation duration, unused
seat-nanoseconds, minimum headroom, and average headroom. The `lower_*` and
`upper_*` prefixes identify the usage estimate used, including for headroom:
the upper usage estimate can produce less headroom.

One license used for one second is 1,000,000,000 seat-nanoseconds. Saturation
means usage meets or exceeds positive capacity, or usage is positive while
capacity is zero. Missing and uncounted rows retain usage measures; their finite
headroom and unused-seat fields are null and saturation counters remain zero
because finite saturation is not applicable. Checked capacity arithmetic rejects
overflow instead of returning wrapped values.

### Denials and queues

Denials group event and license counts by feature, reason, and error code, with
optional calendar boundaries. Counts of events and quantities are distinct.

Queue reports include queued events and licenses, maximum depth, completed waits,
wait percentiles, maximum wait, open license quantities, and oldest open age.
Waits are counted per closed session allocation whose opening falls in the
bucket, even when it closes later. Percentiles use those allocation durations,
not license-weighted samples. JSON durations are integer nanoseconds. Optional
wait metrics are omitted when no qualifying sample exists.

### Quality fields

Always inspect `quality` alongside report metrics. `open`, `ambiguous`, `orphan`,
and `weak_match` summarize session quantities, not simply row counts.
`unresolved_time` counts undated activity for the selected pool and feature across
all imported time, since those records cannot be assigned to the requested range.

Capacity additionally reports missing entitlement segments, uncovered nanoseconds,
and whether usage exceeds finite entitlement. Uncovered time is accumulated per
vendor/feature, so it can exceed the wall-clock duration of the query. Missing
coverage is not treated as zero purchased capacity.

## Storage and recovery

Connections enable foreign keys, WAL, a five-second busy timeout, and
`synchronous=NORMAL`. Each store serializes its writers; WAL permits concurrent
readers. NORMAL preserves database consistency but the latest commit can be lost
after power failure. Retain original logs and license files as recovery inputs.

New database files use mode 0600; WAL and shared-memory sidecars are secured when
available. Existing database file permissions are not rewritten. Stored activity
and license metadata can contain users, hosts, and vendor-provided text.

Schema version 2 is current. Version-1 databases are rejected and must be recreated
from retained inputs; newer unsupported versions are rejected as well. `Rebuild`
reconstructs derived sessions, not source data or schema versions. See
[the architecture guide](development.md) for the source and projection tables.
