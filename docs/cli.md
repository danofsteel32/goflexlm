# Command-line reference

Build the command with the Go 1.27 toolchain declared in `go.mod`:

```sh
go build -o /tmp/goflexlm ./cmd/goflexlm
```

The examples below use `go run` from the repository root. Substitute a built
binary when scripting: exit statuses documented here are the command's own
statuses; `go run` may report them through its wrapper.

## Convert a debug log to JSON Lines

```sh
go run ./cmd/goflexlm lmgrd.log
go run ./cmd/goflexlm - < lmgrd.log
printf '%s\n' '09:10:11 (acme) OUT: "editor" alex@workstation' | go run ./cmd/goflexlm
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

License-file parsing is available through the
[`ParseLicenseFile(io.Reader)` library API](license-files.md).
