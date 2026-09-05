# goflexlm

[![CI](https://github.com/danofsteel32/goflexlm/actions/workflows/ci.yml/badge.svg)](https://github.com/danofsteel32/goflexlm/actions/workflows/ci.yml)

`goflexlm` is a streaming, standard-library-only Go parser for FlexNet
Publisher debug logs. It understands classic and `-datestamp` envelopes and
the compact and verbose forms of `OUT`, `IN`, `DENIED`, `QUEUED`, and
`DEQUEUED`. Other valid log messages are retained as generic events.

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
