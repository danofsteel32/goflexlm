# goflexlm

[![CI](https://github.com/danofsteel32/goflexlm/actions/workflows/ci.yml/badge.svg)](https://github.com/danofsteel32/goflexlm/actions/workflows/ci.yml)

`goflexlm` is a streaming Go parser for FlexNet Publisher debug logs. The core
parser and JSON Lines command use only the standard library; the optional
`sqlite` package uses the pure-Go `modernc.org/sqlite` driver. The parser
understands classic and `-datestamp` envelopes and
the compact and verbose forms of `OUT`, `IN`, `DENIED`, `QUEUED`, and
`DEQUEUED`. Other valid log messages are retained as generic events.

See [the documentation](docs/README.md) for the
[log parser](docs/log-parser.md), [license-file format](docs/license-files.md),
[command reference](docs/cli.md), [SQLite analytics](docs/sqlite.md), and
[development guide](docs/development.md).

## Library

```go
decoder := goflexlm.NewDecoder(reader)
for decoder.Scan() {
    result := decoder.Result()
    if result.Diagnostic != nil {
        log.Printf("line %d: %s", result.Diagnostic.Line, result.Diagnostic.Message)
        continue
    }
    fmt.Printf("%s: %s\n", result.Event.Kind, result.Event.Message)
}
if err := decoder.Err(); err != nil {
    return err
}
```

Blank lines are skipped. Invalid content produces a `Diagnostic` and decoding
continues; `Decoder.Err` is reserved for reader failures. A classic timestamp
is resolved to an instant only after a dated record establishes a date and UTC
offset. Midnight rollover is then tracked within that input stream.

## Command line

```console
$ go run ./cmd/goflexlm license.log
{"line":1,"raw":"09:10:11 (vendor) OUT: \"editor\" alex@workstation","time":"09:10:11","daemon":"vendor","kind":"checkout","message":"OUT: \"editor\" alex@workstation","activity":{"feature":"editor","user":"alex","host":"workstation","licenses":1}}
$ cat license.log | go run ./cmd/goflexlm -
```

The command accepts zero or one argument. No argument and `-` read standard
input. Valid events are written as JSON Lines to standard output and
line-numbered diagnostics to standard error. Exit status is 0 for a clean
parse, 1 for content or I/O failures, and 2 for invalid arguments.

## SQLite usage analytics

License files can also be read with `ParseLicenseFile(io.Reader)`. It returns
one complete document containing SERVER, VENDOR, FEATURE, INCREMENT, and
USE_SERVER records, preserving ordered metadata and physical source lines.
It supports quoted values, continuations, CRLF, and unlimited logical line
lengths. Unsupported directives or malformed records reject the entire
document with a line-qualified error. Zero and `uncounted` counts normalize
to uncounted capacity.

The additive `sqlite` package imports activity facts into a purchaser-defined
license pool and derives quantity-bearing usage and queue sessions. Imports
are transactional and deduplicated by the exact source-file SHA-256 digest
within a logical stream. Parser diagnostics do not discard surrounding valid
activity. Generic daemon messages are omitted, while unresolved timestamps
and original activity lines remain available for audit.

```go
db, err := sqlite.Open(ctx, "usage.db", sqlite.OpenOptions{})
if err != nil {
    return err
}
defer db.Close()

result, err := db.Import(ctx, sqlite.ImportRequest{
    Reader: input,
    Pool:   "engineering",
    Stream: "license-server-a",
    SourceName: "lmgrd.log",
})
```

The database command imports logs and dated license snapshots, rebuilds
usage sessions, and produces capacity, denial, and queue reports:

```console
$ goflexlmdb import --db usage.db --pool engineering --stream server-a lmgrd.log
$ goflexlmdb licenses parse licenses.lic
$ goflexlmdb licenses import --db usage.db --pool engineering \
    --effective-from 2026-01-01T00:00:00Z --timezone America/New_York licenses.lic
$ goflexlmdb report capacity --db usage.db --pool engineering \
    --from 2026-08-01T00:00:00Z --to 2026-09-01T00:00:00Z \
    --feature editor --bucket day --timezone America/New_York --json
```

For example, this synthetic license file supplies finite editor capacity and
uncounted render capacity:

```text
SERVER licenses.example 001122aabbcc 27000
VENDOR acme /opt/acme PORT=27001
USE_SERVER
FEATURE editor acme 2026.0 permanent 25
INCREMENT render acme 2026.0 permanent uncounted
```

`licenses parse [FILE|-]` defaults to standard input and emits one compact
JSON document after input closes successfully. `licenses import` requires
exactly one file (or `-`), an effective RFC3339 instant, and an explicit
loadable timezone (`UTC` or an IANA location such as `America/New_York`).
It parses and closes input before opening the database, records the path
unchanged as the source name, and prints nothing on success. Usage errors
exit 2; operational errors exit 1 with a `goflexlmdb:` error on stderr.

Library callers can pass the parsed document in
`sqlite.LicenseImportRequest{File, Pool, SourceName, EffectiveFrom, Timezone}`
to `Store.ImportLicenseFile(ctx, request)`. The store validates caller-built
documents too, before locking or changing storage.

Each file is an authoritative pool snapshot until the next imported snapshot.
Importing at the same effective instant replaces that snapshot atomically.
An empty, comment-only, or SERVER/VENDOR-only file ends all known capacity.
Within a snapshot the first FEATURE per vendor/feature and every INCREMENT
contribute; versions and pooling attributes remain metadata. START activates
at local midnight, never before the snapshot. Finite expiration ends capacity
at the start of the printed date. `permanent` and zero-year expirations
(`0`, `00`, `000`, `0000`, or `1900`) do not expire. START must be finite.
Resolved UTC boundaries are stored so rebuilds retain the original meaning.

New databases use schema version 2. Version-1 databases cannot be migrated:
delete and recreate them, then reimport retained logs and license files.
The former CSV command and entitlement API have been removed.
See [the runnable demo](testdata/sqlite-demo/README.md) for complete examples.

Report ranges are half-open (`from <= timestamp < to`). Missing entitlement
history is reported as uncovered time, not zero capacity. Unresolved classic
timestamps are excluded from sessions and time-based measures and surfaced in
each report's quality fields.

Capacity reports match log daemon names to license vendors by exact spelling.
Rows are ordered by vendor, feature, and segment start. A finite zero is known
capacity; missing capacity has a null purchased value and contributes to
coverage quality. Uncounted capacity is known unlimited capacity: it preserves
usage measures but has no finite purchased, saturation, or headroom measures.
Missing coverage is added separately for each vendor/feature, so its duration
may exceed the wall-clock report range. Report arithmetic rejects overflow.

SQLite connections use foreign keys, WAL mode, a five-second busy timeout,
and `synchronous=NORMAL`. WAL permits readers during the serialized writer.
`NORMAL` keeps the database consistent but the newest commit can be lost after
a power failure; retained source logs are the recovery source.

## License

This project is available under the [MIT License](LICENSE).
