# goflexlm

[![CI](https://github.com/danofsteel32/goflexlm/actions/workflows/ci.yml/badge.svg)](https://github.com/danofsteel32/goflexlm/actions/workflows/ci.yml)

`goflexlm` parses FlexNet Publisher debug logs and supported FlexLM license
files using only the Go standard library. The streaming log parser understands
classic and `-datestamp` envelopes and the compact and verbose forms of `OUT`, `IN`, `DENIED`, `QUEUED`, and
`DEQUEUED`. Other valid log messages are retained as generic events.

See [the documentation](docs/README.md) for the
[log parser](docs/log-parser.md), [license-file format](docs/license-files.md),
[command reference](docs/cli.md), and
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
offset. Dated records include RFC3339 envelopes and native FlexLM
`Server's System Date and Time` messages; later `TIMESTAMP M/D/YYYY` messages
reset the date while retaining the known offset. Right-aligned one-digit hours
such as ` 1:02:03` are accepted. Midnight rollover is then tracked within that
input stream.

Administrative `IN:` check-ins ending in `(USER_REMOVED)` or `(SHUTDOWN)` are
parsed as check-in events; the original reason remains in the event's message
and raw line.

## Command line

```console
$ go run ./cmd/goflexlm license.log
{"line":1,"raw":"09:10:11 (vendor) OUT: \"editor\" alex@workstation","time":"09:10:11","daemon":"vendor","kind":"checkout","message":"OUT: \"editor\" alex@workstation","activity":{"feature":"editor","user":"alex","host":"workstation","licenses":1}}
$ cat license.log | go run ./cmd/goflexlm -
```

The command accepts zero or one argument. No argument and `-` read standard
input. Valid events are written as JSON Lines to standard output. Unparseable
lines are skipped, with one summary of their counts by diagnostic code on standard
error. Exit status is 0 when at least one event was parsed, 1 when no events were
parsed (including empty input) or an I/O failure occurred, and 2 for invalid
arguments.

## License files

`ParseLicenseFile(io.Reader)` returns one complete document containing SERVER,
VENDOR or legacy DAEMON, FEATURE, INCREMENT, and USE_SERVER records. It preserves
record line numbers and ordered attributes, including repeated metadata and bare
attributes. Quoted values, backslash continuations, CRLF, and final lines without
a newline are supported without a fixed line-length limit.

```go
file, err := goflexlm.ParseLicenseFile(reader)
if err != nil {
    return err
}
for _, feature := range file.Features {
    fmt.Println(feature.Vendor, feature.Name, feature.Uncounted)
}
```

Malformed records, unsupported directives, and reader failures reject the whole
document with a line-qualified error. Zero and `uncounted` counts normalize to
uncounted licenses. See the [license-file guide](docs/license-files.md) for syntax,
dates, and the returned document format.

## License

This project is available under the [MIT License](LICENSE).
