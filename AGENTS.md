# AGENTS.md

This file governs all work in this repository. A more specific `AGENTS.md` in
a subdirectory overrides it for files below that directory.

## Project purpose

`goflexlm` is a streaming Go library and command-line tool for parsing FlexNet
Publisher debug logs. Preserve the library API and command output as compatibility
surfaces. Prefer small, direct changes over new abstractions.

## Repository map

- `decoder.go` contains the streaming decoder, envelope parsing, activity parsing,
  and timestamp context.
- `types.go` defines the exported library API, event kinds, and diagnostic codes.
- `doc.go` provides the package documentation.
- `cmd/goflexlm` contains the JSON Lines command and its tests.
- `decoder_test.go` contains parser, recovery, reader, timestamp, and fuzz tests.
- `README.md` documents supported behavior and public usage.

## Go requirements

- Use Go 1.27 and preserve the standard-library-only dependency policy. Add a module
  dependency only when the task explicitly requires it and the standard library
  cannot solve the problem.
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

## Parser invariants

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

For parser changes, also run the fuzz target for a bounded smoke test:

```sh
go test -run '^$' -fuzz '^FuzzDecoder$' -fuzztime=10s .
```

## Documentation and change discipline

- Update `README.md` when supported input, public API behavior, JSON output, or CLI
  usage changes.
- Keep comments focused on why a rule exists or why an edge case is surprising. Do
  not restate the code.
- Preserve unrelated user changes. Inspect the working tree before editing and avoid
  destructive Git commands.
- Keep commits and patches focused. Do not combine formatting or refactoring outside
  the task with a behavioral change.
