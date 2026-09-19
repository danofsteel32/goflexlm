# AGENTS.md

This file governs all work in this repository. A more specific `AGENTS.md` in
a subdirectory overrides it for files below that directory.

## Project purpose

`goflexlm` parses FlexNet Publisher debug logs and supported FlexLM license files.
The optional SQLite package imports activity and license snapshots and reports
usage, capacity, denials, and queueing. Preserve the library APIs, command output,
and persisted database contracts. Prefer small, direct changes over new abstractions.

## Repository map

- `decoder.go` contains the streaming decoder, envelope parsing, activity parsing,
  and timestamp context.
- `types.go` defines the exported library API, event kinds, and diagnostic codes.
- `doc.go` provides the package documentation.
- `cmd/goflexlm` contains the JSON Lines command and its tests.
- `decoder_test.go` contains parser, recovery, reader, timestamp, and fuzz tests.
- `license.go` and `license_parser.go` define and parse complete license documents;
  `license_test.go` and `license_conformance_test.go` cover grammar and fuzzing.
- `internal/licenserules` shares license syntax rules between parsing and validation.
- `sqlite` contains storage, imports, session derivation, capacity projection,
  reports, and their tests. `schema.go` defines the persisted schema.
- `cmd/goflexlmdb` contains the database and license-file commands and their tests.
- `testdata/licenses` contains the license conformance corpus;
  `testdata/sqlite-demo` contains synthetic inputs and a runnable analytics demo.
- `README.md` is the project introduction; `docs/README.md` indexes the detailed
  parser, license-file, CLI, analytics, and development guides.
- `.github/workflows/ci.yml` checks formatting, vet, and race-enabled tests.

## Go requirements

- Use Go 1.27 as declared in `go.mod`. Keep the root parser package, its internal
  syntax helpers, and `cmd/goflexlm` standard-library-only. The optional `sqlite`
  package uses the existing pure-Go `modernc.org/sqlite` dependency;
  `cmd/goflexlmdb` depends on that package. Add further dependencies only when the
  task explicitly requires them and the standard library cannot solve the problem.
- Write idiomatic Go. Run `gofmt` on every changed Go file and keep imports grouped
  by `gofmt`.
- Keep package names short and lowercase. Use exported names only for supported
  public behavior. Add Go doc comments to new or changed exported declarations;
  one comment may document a cohesive constant or variable group.
- Return errors with useful operation context. Do not log inside the library, panic
  for malformed input, or use sentinel errors where a diagnostic code is the public
  contract.
- Accept interfaces at I/O boundaries. Keep concrete types for internal state unless
  an interface has more than one real implementation or creates a test seam.
- Pass `context.Context` as the first parameter only for work that can be canceled or
  crosses a process or network boundary. Do not store contexts in structs.
- Avoid mutable package-level state. Compiled regular expressions may remain package
  variables because they are immutable and safe for concurrent use.
- Keep functions focused. Extract helpers when they clarify a parsing rule, not only
  to reduce line count.

## Debug-log parser invariants

- `Decoder` remains pull-based: callers advance with `Scan`, inspect one `Result`,
  and check `Err` after scanning stops.
- Memory use must remain proportional to the longest line being processed. Do not
  read the full input into memory or introduce an arbitrary scanner token limit.
- Skip blank lines, accept LF and CRLF, and process a final line without a newline.
  Preserve each non-blank source line exactly in `Event.Raw` or `Diagnostic.Raw`,
  excluding the line-ending bytes.
- A successful `Scan` produces exactly one event or one diagnostic. Content failures
  use stable diagnostic codes and do not stop later lines from being parsed. Reserve
  `Decoder.Err` for reader failures.
- Treat malformed recognized activities as diagnostics. Preserve valid unsupported
  messages as `KindMessage`; do not reject an envelope because its message type is
  unknown.
- An omitted license count means one. Preserve vendor-provided text such as checkout
  data, denial details, and verbose placeholders unless the public type intentionally
  normalizes it.
- A classic time remains unresolved until a dated line establishes a date and UTC
  offset. Preserve midnight rollover and allow each later dated line to reset that
  context.
- Keep parser state on each `Decoder`. Separate decoders must never share timestamp
  context or other mutable state.

## License-file parser invariants

- `ParseLicenseFile(io.Reader)` returns a complete document or a zero document and
  a line-qualified error. Do not apply the debug decoder's recover-and-continue
  behavior to license files.
- Preserve record line numbers and ordered attributes, including repeated metadata
  and the distinction between bare attributes and values. Support quoted values,
  physical-line continuations, LF/CRLF, and final lines without a newline.
- Keep working memory beyond the returned document proportional to the longest
  logical line; do not introduce an arbitrary token limit.
- Reject unsupported directives and malformed records. Keep parser and store
  validation aligned through `internal/licenserules`, including for caller-built
  documents that never passed through the text parser.
- Normalize zero and `uncounted` license counts to uncounted capacity. Keep START
  finite and preserve non-expiring expiration conventions.

## SQLite invariants

- Keep each source-file import transactional, including session derivation.
  Deduplicate exact source bytes by SHA-256 within a pool's logical stream.
  Reader, callback, cancellation, and database failures must not commit partial
  imports. Content diagnostics may coexist with committed valid activity.
- Retain activity source provenance and unresolved timestamps. Omit generic log
  messages from stored activity; exclude unresolved times from session derivation
  and time-based measures while surfacing them in report quality.
- Match sessions within pool, stream, daemon, feature, and session type. Preserve
  quantities, partial closes, match strength, ambiguity, open sessions, and orphans.
- Treat each license import as an authoritative pool snapshot. Replace snapshots
  at the same effective instant atomically. The first FEATURE per vendor/feature
  and every INCREMENT contribute; preserve other pooling metadata without inventing
  grouping behavior.
- Resolve license calendar dates in the explicit import timezone and persist UTC
  boundaries. START cannot activate before the snapshot; expiration ends capacity
  at the start of its printed date. Rebuilds must retain those resolved boundaries.
- Use exact daemon/vendor and feature spelling for capacity matching. Distinguish
  known finite zero, missing capacity, and uncounted capacity in storage and reports.
- Preserve half-open report intervals, calendar timezone boundaries, quality
  counters, and checked capacity arithmetic. Never silently wrap overflow.
- Schema version 2 is current; version 1 requires recreation and reimport, not an
  implicit migration. Schema and derivation changes need an explicit versioning
  decision and tests. Do not delete a user's database as part of an upgrade.

## Public compatibility

- Treat exported types, constants, constructors, methods, event-kind strings,
  diagnostic-code strings, JSON field names, and CLI exit statuses as public API.
- Preserve the command contract `goflexlm [FILE|-]`. No argument and `-` read standard
  input; more than one argument exits with status 2.
- Write one JSON object per valid event to standard output. Write line-numbered
  diagnostics to standard error and finish parsing before returning status 1 for
  content diagnostics.
- Stop on input or output failures. Exit with status 0 only after a clean parse and
  status 1 for content or I/O failures.
- Do not add incidental fields to JSON output. New fields and enum values require an
  explicit compatibility decision and tests.
- For `goflexlmdb`, preserve the `import`, `licenses parse`, `licenses import`,
  `rebuild`, and `report capacity|denials|queues` command contracts documented in
  `docs/cli.md`. Usage errors exit 2 and operational errors exit 1.
- `licenses parse` writes one complete JSON document only after successful parsing
  and input closure. `licenses import` validates and closes input before opening
  the database and is silent on success.
- Log-import diagnostics may produce exit 1 after committing surrounding valid
  activity. Keep that distinct from an import failure that rolls back the file.

## Testing expectations

- Add a regression test before or with every parser bug fix. Prefer table-driven
  tests and subtests for grammar variants.
- Test behavior through exported APIs when possible, including from same-package
  tests. Reach into unexported helpers only when the public API cannot isolate the
  parsing rule.
- Cover both successful parsing and nearby malformed input. Verify recovery by placing
  a valid line after malformed content.
- Include boundary cases for line endings, final lines, long lines, optional fields,
  timestamp rollover, reader failures, and output failures when those paths change.
- Keep fuzz targets deterministic and free of timing assumptions. Their core invariant
  is that arbitrary bytes never panic or cause scanning without input consumption.
- Avoid sleeps, external processes, network access, global environment mutation, and
  shared temporary paths in unit tests. Use `t.TempDir`, `t.Setenv`, and test-local
  readers or writers where needed.
- For storage changes, cover rollback, deduplication, snapshot replacement,
  chronological and out-of-order imports, rebuild consistency, and report quality
  where affected. Use synthetic inputs and temporary databases.

## Verification

Run these commands from the repository root before handing off a change:

```sh
gofmt -w path/to/changed.go
test -z "$(gofmt -l .)"
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

If the environment does not permit writes to the default Go build cache, set
`GOCACHE` to a task-specific directory under `/tmp`. Do not commit build artifacts or
cache contents.

For parser changes, also run the affected fuzz target for a bounded smoke test
(both when shared parsing rules change):

```sh
go test -run '^$' -fuzz '^FuzzDecoder$' -fuzztime=10s .
go test -run '^$' -fuzz '^FuzzParseLicenseFile$' -fuzztime=10s .
```

## Documentation and change discipline

- Update `README.md` when supported input, public API behavior, JSON output, or CLI
  usage changes, and update the corresponding guide in `docs`. Keep the docs index
  current. Verify examples against code and synthetic fixtures; distinguish the
  streaming log API from the whole-document license API.
- Keep comments focused on why a rule exists or why an edge case is surprising. Do
  not restate the code.
- Preserve unrelated user changes. Inspect the working tree before editing and avoid
  destructive Git commands.
- Keep commits and patches focused. Do not combine formatting or refactoring outside
  the task with a behavioral change.
