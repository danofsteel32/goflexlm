# Development and architecture

Use Go 1.27, matching [go.mod](../go.mod). The root parser and `cmd/goflexlm` use
only the standard library. The optional SQLite package uses `modernc.org/sqlite`;
`cmd/goflexlmdb` and `cmd/goflexlmweb` build on that package. Repository contribution rules live in
[AGENTS.md](../AGENTS.md).

## Code map and data flow

| Area | Files | Responsibility |
| --- | --- | --- |
| Debug logs | [decoder.go](../decoder.go), [types.go](../types.go) | Streaming envelopes, activities, timestamps, diagnostics |
| License files | [license_parser.go](../license_parser.go), [license.go](../license.go) | Whole-document parsing and exported record types |
| Shared syntax | [internal/licenserules/rules.go](../internal/licenserules/rules.go) | Case, decimal, port, and license-date validation |
| Log CLI | [cmd/goflexlm/main.go](../cmd/goflexlm/main.go) | JSON Lines conversion and exit behavior |
| Database CLI | [cmd/goflexlmdb/main.go](../cmd/goflexlmdb/main.go) | License parsing, imports, rebuilds, tables, and JSON reports |
| Web reports | [cmd/goflexlmweb](../cmd/goflexlmweb) | Local HTTP dashboard, embedded templates and CSS, report filters and export |
| Store setup | [sqlite/store.go](../sqlite/store.go), [sqlite/schema.go](../sqlite/schema.go) | Connections, permissions, schema, version checks |
| Activity imports | [sqlite/import.go](../sqlite/import.go), [sqlite/derive.go](../sqlite/derive.go) | Transactional facts, digest deduplication, session matching |
| License imports | [sqlite/license_import.go](../sqlite/license_import.go), [sqlite/license_validation.go](../sqlite/license_validation.go), [sqlite/license_capacity.go](../sqlite/license_capacity.go) | Validated snapshots and capacity timelines |
| Reports | [sqlite/reports.go](../sqlite/reports.go), [sqlite/capacity_report.go](../sqlite/capacity_report.go) | Calendar grouping, quality, usage, and capacity arithmetic |

Log bytes flow through `Decoder` into one event or diagnostic per non-blank line.
The log CLI converts events into its JSON representation. The store importer
instead hashes the entire source, retains activity facts, and derives sessions
within the import transaction.

License bytes flow through `ParseLicenseFile` into a complete document. The store
validates that document again, resolves local calendar dates to UTC, saves the
snapshot, and rebuilds its pool's capacity timeline in one transaction. Sharing
syntax helpers keeps text input and caller-constructed documents consistent.

## Persisted data and projections

| Tables | Role |
| --- | --- |
| `license_pools`, `log_streams` | Pool and source-history identity |
| `imports`, `activity_events` | Log provenance, diagnostics summary, activity facts |
| `sessions`, `derivation_state` | Derived quantity-bearing sessions and derivation version |
| `license_imports` | Authoritative documents, effective instants, and resolved dates |
| `capacity_changes` | Derived vendor/feature capacity timeline |
| `schema_version` | Database format version |

Session projections can be rebuilt from activity facts. Capacity projections are
recomputed when license snapshots change. Schema versioning and session-derivation
versioning serve different purposes; an automatic derivation refresh is not a
database migration. Current schema version 2 rejects version 1 without migrating
its data.

## Verification

Run from the repository root before handing off a change:

```sh
# Format only Go files you changed, for example:
# gofmt -w decoder.go decoder_test.go
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

If the default build cache is not writable, set a task-local cache first:

```sh
export GOCACHE=$(mktemp -d /tmp/goflexlm-build-cache.XXXXXX)
```

For parser changes, run the affected fuzz target, or both if shared rules change:

```sh
go test -run '^$' -fuzz '^FuzzDecoder$' -fuzztime=10s .
go test -run '^$' -fuzz '^FuzzParseLicenseFile$' -fuzztime=10s .
```

[CI](../.github/workflows/ci.yml) runs formatting, vet, and race-enabled tests.
Parser tests cover grammar, recovery, long and final lines, reader failures, and
timestamp rollover. License tests add conformance fixtures and whole-document
failure behavior. SQLite tests cover transactions, matching, snapshot timelines,
capacity state distinctions, and report arithmetic. CLI tests exercise the command
runners with local readers and writers, including failure paths.

## Making changes

Keep fixes small and add regression tests alongside parser fixes. Exercise public
APIs where possible, with both valid input and nearby malformed variants. For
debug logs, include a valid line after malformed content to prove recovery.

Exported types, JSON names, enum strings, diagnostic codes, command statuses, and
persisted data are compatibility surfaces. Changing them requires a deliberate
decision and tests. Preserve the separation between log diagnostics and terminal
reader errors, and between incremental logs and all-or-nothing license documents.

Use synthetic fixtures, temporary databases, and deterministic tests. Keep build
artifacts and caches out of commits. Update the relevant guide and README when
supported behavior changes. The [demo](../testdata/sqlite-demo/README.md) provides
an end-to-end workflow with expected analytics results.
