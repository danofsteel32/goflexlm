# License files and snapshots

`goflexlm.ParseLicenseFile(io.Reader)` reads a complete supported license document.
It returns `(LicenseFile, error)`. Any malformed record, unsupported directive,
or reader failure returns a zero document and a line-qualified error. It does
not recover record by record like the debug-log decoder.

```go
file, err := goflexlm.ParseLicenseFile(reader)
if err != nil {
	return err
}
for _, feature := range file.Features {
	fmt.Println(feature.Vendor, feature.Name, feature.Uncounted)
}
```

Here `reader` is an `io.Reader`; the snippet belongs inside a function returning
an error. The returned document holds all parsed records. Additional working
memory scales with the longest logical line.

## Supported syntax

```text
SERVER licenses.example 001122aabbcc 27000
VENDOR acme /opt/acme PORT=27001
USE_SERVER
FEATURE editor acme 2026.0 permanent 25
INCREMENT render acme 2026.0 30-sep-2026 uncounted \
    START=01-sep-2026 NOTICE="Synthetic example"
```

Directive recognition is ASCII case-insensitive. FEATURE and INCREMENT kinds
normalize to uppercase. Names and ordinary metadata retain their supplied text.

| Directive | Required fields after the directive | Optional data |
| --- | --- | --- |
| `SERVER` | Host, host ID | Port, ordered attributes |
| `VENDOR` | Vendor name | Daemon path, positional options path and port, key/value attributes |
| `FEATURE`, `INCREMENT` | Feature, vendor, version, expiration, count | Ordered attributes, including `START` |
| `USE_SERVER` | None | None; duplicate directives are rejected |

SERVER ports range from 1 through 65535. Vendor `PORT` and `EPORT` values allow
0 through 65535. Positional vendor options and port normalize to `OPTIONS` and
`PORT` attributes. Unknown directives, including unimplemented FlexLM directives,
reject the document rather than being silently ignored.

Blank lines and whole-line `#` comments are skipped. LF, CRLF, and final lines
without a newline are accepted. A trailing backslash outside quotes continues
the logical record. Double quotes may surround a whole token or attribute value;
they must close on that physical line. This is not a shell quoting language:
escaped quotes and inline comment stripping are not provided.

Record `Line` values identify the first physical line of a logical record.
Attributes retain order and duplicates except where a recognized rule rejects
them, such as repeated `START`. `HasValue` distinguishes a bare attribute from
one written with `=`. These are parsed records, not a byte-for-byte copy of the
source file; retain original files separately when exact source bytes matter.

## Counts and dates

A positive unsigned decimal count sets `Licenses` to that count. `0` and
`uncounted` set `Uncounted` to true and leave `Licenses` nil. In JSON this is
`"licenses": null, "uncounted": true`. Zero in a license record therefore does
not mean a finite capacity of zero.

Finite dates use `d-mmm-yyyy` or `dd-mmm-yyyy`, with an English month abbreviation.
Expiration also accepts `permanent` or a date whose year is `0`, `00`, `000`,
`0000`, or `1900` as non-expiring. For example, `01-jan-0000` does not expire;
a bare `0000` is not a date. `START` must contain a finite date.

Successful empty input produces empty `servers`, `vendors`, and `features`
arrays with `use_server: false`. The [canonical corpus](../testdata/licenses/conforming.lic)
and its [expected JSON](../testdata/licenses/conforming.json) show the full shape.
Run `goflexlmdb licenses parse [FILE|-]` to inspect it without opening a database.

## Import an authoritative capacity snapshot

`Store.ImportLicenseFile(ctx, sqlite.LicenseImportRequest{...})` accepts the parsed
`File`, a `Pool`, `SourceName`, `EffectiveFrom`, and an explicit `Timezone`.
The timezone must be loadable as `UTC` or an IANA location such as
`America/New_York`. The store validates caller-built documents as well as parsed
documents before acquiring its writer lock or changing storage.

Every file describes the entire pool from its effective instant until the next
snapshot. Importing at the same instant atomically replaces the existing
snapshot. Omitting a previously known vendor/feature sets its capacity to known
zero. Empty, comment-only, and SERVER/VENDOR-only snapshots end known capacity.

Within a snapshot, the first FEATURE for each vendor/feature and all INCREMENT
records contribute capacity. Versions and other pooling attributes remain
metadata; they do not create separate capacity groups. Any active uncounted
contributor makes the combined capacity uncounted.

START activates at local midnight, never earlier than the snapshot's effective
instant. A finite expiration stops capacity at the start of the printed local
date, not at its end. The store persists the resolved UTC boundaries so later
rebuilds retain the original interpretation.

Capacity matching requires exact license-vendor and log-daemon spelling. See
[SQLite analytics](sqlite.md) for the distinction between zero, uncounted, and
missing capacity, and the [CLI guide](cli.md) for import commands.
