# Command-line reference

Build the commands with the Go 1.27 toolchain declared in `go.mod`:

```sh
go build -o /tmp/goflexlm ./cmd/goflexlm
go build -o /tmp/goflexlmdb ./cmd/goflexlmdb
go build -o /tmp/goflexlmweb ./cmd/goflexlmweb
```

The examples below use `go run` from the repository root. Substitute a built
binary when scripting: exit statuses documented here are the command's own
statuses; `go run` may report them through its wrapper.

## Convert a debug log to JSON Lines

```sh
go run ./cmd/goflexlm testdata/sqlite-demo/server-a-2026-09-01.log
go run ./cmd/goflexlm - < testdata/sqlite-demo/server-a-2026-09-01.log
```

`goflexlm [FILE|-]` reads standard input with no argument or with `-`. It writes
one JSON object per valid event to standard output. Diagnostics use
`line N: CODE: MESSAGE` on standard error. Content errors allow later lines to
be processed; reader or output failures stop processing.

| Status | Meaning |
| --- | --- |
| 0 | Clean parse |
| 1 | Content diagnostic or I/O failure |
| 2 | More than one argument |

See [the parser guide](log-parser.md) for the event JSON fields.

## Database command syntax

```text
goflexlmdb import --db DB --pool POOL --stream STREAM FILE...
goflexlmdb licenses parse [FILE|-]
goflexlmdb licenses import --db DB --pool POOL --effective-from RFC3339 --timezone AREA/LOCATION FILE
goflexlmdb rebuild --db DB --pool POOL
goflexlmdb report capacity|denials|queues --db DB --pool POOL --from RFC3339 --to RFC3339 [--feature FEATURE] [--bucket hour|day|week|month] [--timezone AREA/LOCATION] [--json]
```

Place flags before positional file arguments. `-` can be used as an input file
for standard input. Database commands create a database when needed. Pools group
licenses and usage; streams identify independent logical log histories within a
pool. Use the same stream for rotated files from the same history.

## Inspect or import license files

```sh
go run ./cmd/goflexlmdb licenses parse testdata/sqlite-demo/licenses-2026-09-01.lic

docs_demo_dir=$(mktemp -d /tmp/goflexlm-docs-demo.XXXXXX)
go run ./cmd/goflexlmdb licenses import --db "$docs_demo_dir/usage.db" --pool studio \
  --effective-from 2026-09-01T00:00:00Z --timezone UTC \
  testdata/sqlite-demo/licenses-2026-09-01.lic
```

`licenses parse` defaults to standard input and writes one compact JSON document
followed by a newline only after successful parsing and input closure. It does
not open a database.

`licenses import` requires exactly one file, a nonzero RFC3339 effective instant,
and an explicit loadable timezone: `UTC` or an IANA location. It parses and closes
the input before opening the database, preserves the supplied path as the source
name, and prints nothing on success. Each file replaces the whole pool snapshot
from that instant; see [snapshot semantics](license-files.md).

## Import logs and rebuild sessions

Continue using the temporary database created above:

```sh
go run ./cmd/goflexlmdb import --db "$docs_demo_dir/usage.db" --pool studio --stream server-a \
  testdata/sqlite-demo/server-a-2026-09-01.log
go run ./cmd/goflexlmdb rebuild --db "$docs_demo_dir/usage.db" --pool studio
```

`import` requires one or more files and prints a summary for each: source path,
`imported` or `duplicate`, activity and diagnostic counts, and SHA-256 digest.
Exact duplicate bytes within the same stream do not add activity. Each file is
a separate transaction; a failure in a later file does not undo earlier imports.

Malformed log content is printed as `PATH:line N: CODE: MESSAGE`. Valid activities
surrounding those lines still commit, and the command finishes with status 1.
Reader, callback, or database failures roll back the affected import. An output
or input-close error can occur after an import has committed.

Session derivation runs during import. `rebuild` explicitly reconstructs a pool's
usage and queue sessions from stored facts and is silent on success. It does not
supply missing dates to unresolved records or reread the original files.

## Request reports

```sh
go run ./cmd/goflexlmdb report capacity --db "$docs_demo_dir/usage.db" --pool studio \
  --from 2026-09-01T00:00:00Z --to 2026-09-02T00:00:00Z \
  --feature editor --bucket day --timezone UTC --json
go run ./cmd/goflexlmdb report denials --db "$docs_demo_dir/usage.db" --pool studio \
  --from 2026-09-01T00:00:00Z --to 2026-09-02T00:00:00Z --json
go run ./cmd/goflexlmdb report queues --db "$docs_demo_dir/usage.db" --pool studio \
  --from 2026-09-01T00:00:00Z --to 2026-09-02T00:00:00Z --json
```

`--from` and `--to` accept RFC3339 timestamps, including fractional seconds;
`--to` must be later. Ranges include the start and exclude the end. `--feature`
filters by exact feature name across vendors. Without it, reports include all
matching features in the pool.

`--bucket` optionally groups by calendar hour, day, week, or month. `--timezone`
defaults to `UTC` and controls calendar boundaries, not the interpretation of
already imported activity. Capacity rows also split when capacity changes.

The default output is a readable table. `--json` writes one report object,
including quality fields and metrics not shown in the tables. Queue durations
and seat-time metrics use nanoseconds in JSON. See [report interpretation](sqlite.md).

For `goflexlmdb`, successful commands exit 0, usage errors exit 2, and operational
errors exit 1 with a `goflexlmdb:` message on standard error. Log diagnostics also
produce status 1 as described above. Version-1 databases require recreation and
reimport; there is no CSV entitlement command or automatic schema migration.

The [full demo](../testdata/sqlite-demo/README.md) adds a second snapshot and stream,
duplicate imports, missing vendor coverage, and diagnostic recovery.

## Local web reports

```sh
go run ./cmd/goflexlmweb --db usage.db --pool engineering
```

`goflexlmweb --db DB --pool POOL [--listen 127.0.0.1:8080]` opens an existing
database and serves a local report dashboard. The listen address must use a
loopback IP. Usage errors exit 2, operational failures exit 1, and a clean
shutdown exits 0. See the [web interface guide](web-interface.md) for filters,
report interpretation, and export behavior.
