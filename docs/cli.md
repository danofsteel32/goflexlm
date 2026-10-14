# Command-line reference

Build the command with the Go 1.27 toolchain declared in `go.mod`:

```sh
go build -o /tmp/goflexlm ./cmd/goflexlm
```

The examples below use `go run` from the repository root. Substitute a built
binary when scripting: exit statuses documented here are the command's own
statuses; `go run` may report them through its wrapper.

## Select a subcommand

```text
goflexlm lmgrd [options] [FILE|-]
goflexlm license [options] [FILE|-]
```

Use `lmgrd` for debug logs and `license` for license files. A subcommand is
required. Both read standard input when the file is omitted or is `-`.
Each subcommand has its own options; put them after the subcommand and before
the filename. Use `goflexlm --help` to list commands and
`goflexlm <command> --help` for command help. Both forms return status 0.
Use `--` after the subcommand and before a filename that starts with a dash.

## Convert a debug log to JSON Lines

```sh
go run ./cmd/goflexlm lmgrd debug.log
go run ./cmd/goflexlm lmgrd - < lmgrd.log
printf '%s\n' '09:10:11 (acme) OUT: "editor" alex@workstation' | go run ./cmd/goflexlm lmgrd
```

Log mode writes one JSON object per valid event to standard output. Unparseable
lines are skipped and summarized once on standard error, with counts grouped by
diagnostic code:

```text
goflexlm: skipped 6 unparseable lines (invalid_envelope: 6)
```

Content errors allow later lines to be processed. The command succeeds when at
least one event was parsed. If none were parsed, it reports
`goflexlm: no parseable events`; this includes empty or blank-only input.
Reader or output failures stop processing and return status 1 even if some
events were already written.

| Status | Meaning |
| --- | --- |
| 0 | At least one parsed event and no I/O failure |
| 1 | No parsed events (including empty input), or I/O failure |
| 2 | Missing or unknown subcommand, invalid options, or more than one file |

See [the parser guide](log-parser.md) for the event JSON fields.

## Convert a license file to JSON

```sh
go run ./cmd/goflexlm license server.lic
go run ./cmd/goflexlm license - < server.lic
```

License mode writes one JSON document with `servers`, `vendors`, `features`, and
`use_server` fields to standard output. It uses the same schema as the
[`ParseLicenseFile(io.Reader)` library API](license-files.md).

Unlike log mode, license mode rejects the whole document if any record is
malformed or unsupported. Parsing errors include a line number on standard error,
and no JSON is written. Empty or comment-only input reports
`goflexlm: no parseable license records` and returns status 1. The library API
continues to accept empty documents.

License mode returns 0 for a nonempty valid document, 1 for parsing or I/O
failures, and 2 for invalid arguments. `--help` returns 0.
