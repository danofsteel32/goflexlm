# Development and architecture

Use Go 1.27, matching [go.mod](../go.mod). All packages use only the standard
library.

## Code map and data flow

| Area | Files | Responsibility |
| --- | --- | --- |
| Debug logs | [decoder.go](../decoder.go), [types.go](../types.go) | Streaming envelopes, activities, timestamps, diagnostics |
| License files | [license_parser.go](../license_parser.go), [license.go](../license.go) | Whole-document parsing and exported record types |
| License syntax | [internal/licenserules/rules.go](../internal/licenserules/rules.go) | Case, decimal, port, and license-date validation |
| Log CLI | [cmd/goflexlm/main.go](../cmd/goflexlm/main.go) | JSON Lines conversion and exit behavior |

Log bytes flow through `Decoder` into one event or diagnostic per non-blank line.
The log CLI converts events into its JSON representation. The decoder keeps its
own timestamp context and working memory proportional to the longest line.

License bytes flow through `ParseLicenseFile` into a complete document. Any
malformed record or reader failure rejects the document. Working memory beyond
the returned document scales with the longest logical line. License syntax
helpers validate counts, ports, and calendar dates.

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
failure behavior. CLI tests exercise the command runner with local readers and
writers, including failure paths.

## Making changes

Keep fixes small and add regression tests alongside parser fixes. Exercise public
APIs where possible, with both valid input and nearby malformed variants. For
debug logs, include a valid line after malformed content to prove recovery.

Exported types, JSON names, enum strings, diagnostic codes, and command statuses
are compatibility surfaces. Changing them requires a deliberate decision and
tests. Preserve the separation between log diagnostics and terminal
reader errors, and between incremental logs and all-or-nothing license documents.

Use synthetic fixtures and deterministic tests. Keep build artifacts and caches
out of commits. Update the relevant guide and README when
supported behavior changes.
