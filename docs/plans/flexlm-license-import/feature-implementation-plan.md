# Feature Implementation Plan: FlexLM License Import

Replace CSV entitlement ingestion with direct FlexLM license-file parsing and dated, vendor-aware snapshot imports. The root debug-log parser remains unchanged. This is a clean cutover: new databases initialize at schema version 2, and version-1 databases fail with clear recreate instructions instead of being migrated.

## Outcome

Operators can inspect a supported FlexLM license file as one normalized JSON document. They can then import it as the authoritative capacity snapshot for a named pool. Reports match capacity to lmgrd usage by vendor and feature, distinguishing finite, uncounted, and missing capacity.

The work removes the CSV entitlement command and API. It retains source records for audit, rebuilds capacity atomically, and never exposes a partially imported snapshot after an error or cancellation.

## User Stories

- **US-1:** As an operator, I want to parse a FlexLM license file into a stable document, so that I can inspect the data that will drive capacity.
- **US-2:** As an operator, I want to import a dated license snapshot for a pool, so that historical usage reports reflect purchased capacity without maintaining CSV timelines.
- **US-3:** As a report consumer, I want capacity reported per vendor and feature, so that one vendor's licenses cannot hide another vendor's missing or exceeded capacity.

## Constraints and Boundaries

- **Recorded boundary:** The operator requested FlexLM `SERVER`, `VENDOR`, `FEATURE`, and `INCREMENT` parsing and direct mapping to lmgrd usage. The boundary replaces CSV, preserves the debug-log parser, and permits clean database initialization; see [scope-boundary.md](artifacts/scope-boundary.md).
- **Out of scope:** Do not retain a CSV compatibility path, migrate version-1 data, or infer relationships absent from the license file and existing usage records.
- **Watch after ship:** Representative production license fixtures and deployed version-1 databases were unavailable. They are nonblocking because the format and rejection behavior are pinned; the fixture triggers in Risks and Assumptions reopen the relevant decisions.

## Implementation Approach

### License document and parser

Keep FlexLM syntax in the root package and SQLite operational meaning in `sqlite`. Export the concrete `LicenseFile`, server, vendor, feature, and ordered-attribute model plus `ParseLicenseFile(io.Reader)`. Do not add a parser interface. The parser accepts the pinned logical-line grammar, quoted values, continuations, case rules, and supported directives. It rejects unsupported directives and returns no partial document on failure ([D-1](artifacts/implementation-decision-log.md#d-1-clean-v2-initialization-and-v1-rejection)).

The parser/store contract is concrete. Parser-produced documents satisfy all document-only import validation. A caller-built document is independently validated by `Store.ImportLicenseFile` before locking or mutation. A shared corpus covers conforming documents, while invalid constructed values remain store-boundary tests ([D-2](artifacts/implementation-decision-log.md#d-2-parser-and-store-conformance-boundary)).

### Snapshot storage and capacity projection

Import one parsed `LicenseFile` using `LicenseImportRequest{File, Pool, SourceName, EffectiveFrom, Timezone}`. Require a nonzero effective instant and `UTC` or an IANA-style loadable timezone. Store one canonical document and its resolved date boundaries per `(pool, effective_ns)`. Replace a same-instant snapshot atomically and rebuild the pool-wide change-point projection in the same transaction. A new database creates only the version-2 schema. Opening version 1 returns the pinned delete-and-recreate error without applying legacy DDL ([D-1](artifacts/implementation-decision-log.md#d-1-clean-v2-initialization-and-v1-rejection)).

Build and prove the three-snapshot projection before reports consume it. Its identity universe includes every vendor/feature record in every stored snapshot. Absence at an authoritative snapshot is known zero, while an identity present only in usage remains missing ([D-3](artifacts/implementation-decision-log.md#d-3-projection-first-delivery-order)).

### Vendor-aware reports

Build the report identity set from the usage `(daemon, feature)` and capacity `(vendor, feature)` identities that affect the requested range. Then perform exact-pair capacity lookup. Emit separate vendor/feature buckets, including capacity-only rows and usage-only missing rows. `CapacityBucket` therefore carries `Vendor` and `Uncounted`. Uncounted is known unlimited capacity, not missing capacity ([D-4](artifacts/implementation-decision-log.md#d-4-report-identity-union-and-exact-pair-lookup)).

### Command workflow

Add `goflexlmdb licenses parse [FILE|-]` and `goflexlmdb licenses import --db DB --pool POOL --effective-from RFC3339 --timezone AREA/LOCATION FILE`. Parsing and a successful file close complete before parse output. Import parses and closes input before opening the database. Success is silent for import and is one compact JSON object plus newline for parse. Usage errors return 2; operational failures return 1. Use only a narrow package-local test seam around the command workflow to observe close and database-open ordering ([D-5](artifacts/implementation-decision-log.md#d-5-package-local-cli-workflow-seam)).

### Cancellation and atomicity

Check context immediately before and after acquiring the existing writer lock. Then use the transaction for all snapshot, projection, and pool changes. Do not introduce a context-aware mutex. Cancellation cannot safely interrupt a mutex wait without a broader concurrency design ([D-7](artifacts/implementation-decision-log.md#d-7-cancellation-checkpoints-with-existing-writer-lock)).

## Work Units and Sequencing

| # | Work Unit | Story | Delivers | Justification | Depends On | Verification |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Add the license document parser and its shared conformance corpus | US-1 | The public FlexLM document, parser, normalized JSON behavior, and parser/store-valid document fixtures. | The recorded request explicitly names these license records and their metadata; a shared corpus prevents two independently public boundaries from drifting. ([D-2](artifacts/implementation-decision-log.md#d-2-parser-and-store-conformance-boundary)) | — | Parser grammar, line attribution, JSON projection, reader failure, and corpus tests pass. |
| 2 | Replace entitlement storage with validated snapshots and a projection | US-2 | Clean v2 initialization, v1 rejection, canonical snapshot storage, date resolution, and an atomic vendor/feature capacity change-point projection. | Direct license-file import requires one document transaction; the boundary permits recreation instead of migration. ([D-1](artifacts/implementation-decision-log.md#d-1-clean-v2-initialization-and-v1-rejection)) | 1 | Three-snapshot projection and snapshot/projection/pool rollback tests pass. |
| 3 | Rewire reports to the vendor-aware projection | US-3 | Exact vendor/feature report rows, uncounted behavior, missing coverage, and checked report arithmetic. | Existing usage retains daemon identity, so feature-only matching would contradict the requested license-to-usage mapping. ([D-4](artifacts/implementation-decision-log.md#d-4-report-identity-union-and-exact-pair-lookup)) | 2 | Vendor/coverage state-matrix tests cover finite, uncounted, missing, capacity-only, and usage-only cases. |
| 4 | Replace the CSV command and guidance with license commands | US-1, US-2 | `licenses parse`, `licenses import`, removed CSV surface, and updated README/demo material. | The boundary explicitly replaces the CSV workflow; ordering tests protect close-before-output and parse-before-database-open behavior. ([D-5](artifacts/implementation-decision-log.md#d-5-package-local-cli-workflow-seam)) | 1, 2, 3 | Command exit, stderr, close failure, database-open ordering, documentation, and demo tests pass. |

## Definition of Done

- [ ] A supported license file parses into the pinned public document and JSON projection, while malformed or unsupported records return a line-qualified error without a partial result.
- [ ] A clean database initializes at v2, a v1 database returns recreate guidance, and invalid constructed documents do not mutate storage. ([D-1](artifacts/implementation-decision-log.md#d-1-clean-v2-initialization-and-v1-rejection), [D-2](artifacts/implementation-decision-log.md#d-2-parser-and-store-conformance-boundary))
- [ ] Three-snapshot tests prove authoritative empty snapshots, expiration, replacement, known zero, and atomic rollback for the snapshot, projection, and newly created pool. ([D-3](artifacts/implementation-decision-log.md#d-3-projection-first-delivery-order), [D-6](artifacts/implementation-decision-log.md#d-6-state-matrix-and-atomic-rollback-tests))
- [ ] Reports emit exact vendor/feature buckets and correctly preserve finite, uncounted, and missing states without arithmetic overflow. ([D-4](artifacts/implementation-decision-log.md#d-4-report-identity-union-and-exact-pair-lookup))
- [ ] License commands meet their output, close, error-status, and database-open ordering contracts; the CSV command and public entitlement API are absent from supported documentation and builds.
- [ ] Nothing in Deferred (YAGNI) is built unless its stated reopening trigger is recorded.

## Testing Strategy

- **Observable behaviors to test:** Parse normalized JSON; import an authoritative snapshot; report separate vendors; reject v1; replace a snapshot; and run both license commands with their documented statuses.
- **Edge cases requiring coverage:** LF/CRLF/final lines, quoted and continued records, duplicate `START`, zero/uncounted capacity, empty snapshots, out-of-order snapshots, expired contributors, capacity/report overflow, cancellation at lock boundaries, and input-close/output failure ordering.
- **Test doubles posture and levels:** Use table-driven parser and store tests plus SQLite integration tests. Keep the command seam package-local and test-only; use it to inject close/open observations rather than adding mutable globals or a production abstraction. ([D-5](artifacts/implementation-decision-log.md#d-5-package-local-cli-workflow-seam))

## Risks and Assumptions

### Risks

| ID | Risk | Impact | Mitigation | Owner |
| --- | --- | --- | --- | --- |
| R1 | Real vendor files use unsupported package, upgrade, alias, or signature behavior. | Import rejects a file rather than silently understating capacity. | Keep unsupported syntax explicit; add a representative fixture before broadening semantics. | Maintainer |
| R2 | A deployed v1 database is encountered. | It cannot open until recreated. | Return actionable recreate guidance; revisit migration only if deployment evidence exists. | Operator |
| R3 | Full-pool projection rebuild becomes expensive. | Imports may slow as snapshot history grows. | Start with the correct atomic rebuild and measure before adding incremental complexity. | Maintainer |

### Assumptions

| ID | Assumption | What Changes If Wrong | Status |
| --- | --- | --- | --- |
| A1 | Exact daemon/vendor plus feature is the richest shared usage/capacity key. | Add a new key only when representative logs and licenses supply it consistently. | Verified |
| A2 | No production license files were available during planning. | Add a fixture and revisit the affected parser/import policy when a contradictory real file arrives. | Runtime-only |
| A3 | No deployed v1 database exists. | Reconsider clean rejection and migration only when a deployed database is supplied. | Runtime-only |

## Cut for Scope

This is work the work item excludes, rather than work deferred for lack of evidence. Nothing here carries a reopening trigger because the recorded boundary already settled it.

### AI-generated parser programs

- **Why cut:** The boundary asks for FlexLM parsing and import, not a prompt-driven parser-program subsystem. See [scope-boundary.md](artifacts/scope-boundary.md).
- **Source:** Source change plan / Cut for Scope.

## Deferred (YAGNI)

This is work with no supporting evidence yet, rather than work the work item excludes. Every entry carries the trigger that would justify revisiting it.

### JSON as an import source

- **Why deferred:** JSON is an inspection projection, and accepting it would create a second authoritative validation path.
- **Reopen when:** A real caller needs to submit stored normalized documents without access to the original license file.
- **Source:** Source change plan / D-8.

### Vendor and feature aliases

- **Why deferred:** Exact identity matching is supported by current usage data; no mismatch demonstrates alias need.
- **Reopen when:** A representative license/log pair proves a stable name mismatch.
- **Source:** Source change plan / D-6.

### PACKAGE and UPGRADE semantics

- **Why deferred:** They can understate capacity if ignored, and no fixture defines safe folding rules.
- **Reopen when:** A representative required license file contains either directive and its intended capacity result is established.
- **Source:** Source change plan / D-3.

### Signature validation and server discovery

- **Why deferred:** Import needs parsed source and capacity, not online license-server verification.
- **Reopen when:** An operator requires authenticity checks or inventory derived from reachable servers.
- **Source:** Source change plan / D-3.

### Import wall-clock storage

- **Why deferred:** Effective time and source identity answer current audit and rebuild needs.
- **Reopen when:** An audit requirement needs the time the local import command ran.
- **Source:** Source change plan / D-5.

### Context-aware lock

- **Why deferred:** Existing lock checks before and after acquisition preserve cancellation semantics without a new concurrency abstraction.
- **Reopen when:** Measurements show lock contention materially delays cancelled imports and a safe lock redesign is specified.
- **Source:** R1 / DE3.

### Static removed-symbol test

- **Why deferred:** Normal builds, command rejection, and documentation coverage prove the supported cutover without testing source-text absence.
- **Reopen when:** A downstream compatibility suite needs to enforce removal across separately maintained consumers.
- **Source:** R1 / TE5.

## Sources and Plan Records

- **Feature specification:** No source specification file — inputs were the complete [change plan](change-plan.md) and its supporting decision, current-state, and review artifacts.
- **Specification decisions inherited / open items to respect:** change-plan D-1 through D-11 / none.
- **Decision rationale and rejected alternatives:** [implementation decision log](artifacts/implementation-decision-log.md)
- **Team composition and round-by-round history:** [implementation iteration history](artifacts/implementation-iteration-history.md)

## Recommendation

Ship as planned. The sole implementation review round resolved all questions with existing decisions and code evidence. Production fixtures and deployed v1 databases remain explicitly nonblocking watch items.
