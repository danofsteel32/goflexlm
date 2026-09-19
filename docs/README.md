# Project documentation

`goflexlm` turns FlexNet Publisher debug logs into structured events and reads
supported FlexLM license documents. Its optional SQLite package combines activity
and dated license snapshots to report usage and capacity.

| Guide | Read it to |
| --- | --- |
| [Debug-log parser](log-parser.md) | Decode events in Go and understand timestamps, diagnostics, and JSON Lines |
| [License files](license-files.md) | Parse supported directives and understand capacity snapshots |
| [Command-line reference](cli.md) | Build and run the commands, import data, and request reports |
| [Web interface](web-interface.md) | Monitor usage and capacity, review purchasing signals, and export reports |
| [SQLite analytics](sqlite.md) | Use the store API and interpret matching, reports, and quality fields |
| [Development](development.md) | Find implementation code, run checks, and maintain compatibility |

Start with the [project README](../README.md) for a short introduction or the
[synthetic SQLite demo](../testdata/sqlite-demo/README.md) for a complete workflow
with expected results. All shell commands in these guides assume the repository
root as the working directory.
