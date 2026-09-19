# Work Items — FlexLM License Import

These work items implement the [feature implementation plan](feature-implementation-plan.md). Work items are numbered `W-N` for cross-reference only. `Depends on` lines refer to other work items in this file.

## Shared reference artifacts

- **Recorded scope** — [scope boundary](artifacts/scope-boundary.md) records direct FlexLM license-file import, CSV replacement, clean-schema recreation, and the unchanged debug-log parser.
- **Pinned contracts** — [change plan target state](change-plan.md#target-state) defines the public license document, import, storage, projection, report, and command forms that these work items implement.
- **Project constraints** — [AGENTS.md](../../../AGENTS.md) defines the public compatibility, parser, Go, and verification rules that apply throughout this change.

## W-1 — Parse FlexLM license documents

**Summary.** Operators need one faithful view of a supported license file. This item adds that view to the root library. It keeps source metadata intact for later import and audit. Invalid files fail as a whole.

**Work to be done.**

- Add the public license document and parser that represent the supported server, vendor, and capacity records.
  - Keep the syntax boundary in the root `goflexlm` package; use the pinned public types and `ParseLicenseFile(io.Reader)` contract in the shared change plan.
- Parse the supported logical-line grammar without imposing a scanner-sized line limit.
  - Preserve ordered attributes, quoted values, continuations, source lines, case rules, and the semantic `USE_SERVER` flag; reject unsupported directives and malformed records with an operation-qualified line error.
- Prove the parser contract with a shared corpus and focused boundary tests.
  - Cover normalized JSON, line attribution, final lines and CRLF, reader failure, malformed neighbors, and deterministic fuzz behavior.

**Justification.** This descends from Work Unit 1, which delivers the requested FlexLM document parser and its shared conformance corpus.

**References.**

- **Shared artifacts** — [pinned contracts](#shared-reference-artifacts) define the document layout, grammar, normalization, and error contract.
- **Plan work unit** — [Work Unit 1](feature-implementation-plan.md#work-units-and-sequencing) adds the license document parser and shared conformance corpus.

**Acceptance criteria.**

- [ ] `ParseLicenseFile(io.Reader)` returns the pinned `LicenseFile` JSON shape: `servers`, `vendors`, and `features` arrays plus `use_server`; its records retain source line numbers and ordered attributes.
- [ ] The parser accepts the pinned SERVER, VENDOR, FEATURE, INCREMENT, and USE_SERVER grammar, including LF, CRLF, final lines, quotes, and continuations; unsupported or malformed directives return a line-qualified error and no partial document.
- [ ] Counts normalize as pinned: a positive count is finite, while `0` and `uncounted` become uncounted with no finite license count.
- [ ] Automated tests cover conforming and malformed grammar variants, reader failure, JSON projection, and a bounded fuzz target that never panics.

**Depends on.** None.

## W-2 — Store validated license snapshots

**Summary.** A parsed license file must become an auditable pool snapshot. This item adds that safe storage boundary. It starts new databases in the replacement schema. Older databases give clear recreate guidance.

**Work to be done.**

- Add the public snapshot-import request and validate caller-built documents before they can change storage.
  - In `sqlite`, introduce the pinned `LicenseImportRequest` and `Store.ImportLicenseFile` contract; validate request fields, document invariants, timezone rules, dates, and capacity arithmetic before locking or mutation.
- Replace the entitlement schema with the clean version-2 source and projection schema.
  - Persist one compact normalized license document and resolved date data per pool and effective instant; define the vendor-and-feature capacity-change rows consumed by later projection and report work.
- Make snapshot replacement transactional and safe to cancel at the existing writer-lock boundaries.
  - A same-instant import replaces the document atomically, creates a new pool in the transaction when needed, and rejects version 1 with delete-and-recreate instructions rather than a migration.
- Add store-boundary tests for invalid constructed values and persistence behavior.
  - Exercise nil slice normalization, timezone and date failures, version rejection, replacement, cancellation checkpoints, and rollback before the projection builder is added.

**Justification.** This descends from Work Unit 2, which requires validated dated license snapshots and a clean version-2 initialization.

**References.**

- **Shared artifacts** — [pinned contracts](#shared-reference-artifacts) define `LicenseImportRequest`, canonical source storage, resolved dates, and the capacity-change field layout.
- **Plan work unit** — [Work Unit 2](feature-implementation-plan.md#work-units-and-sequencing) replaces entitlement storage with validated snapshots and a projection.

**Acceptance criteria.**

- [ ] `Store.ImportLicenseFile(ctx, LicenseImportRequest{File, Pool, SourceName, EffectiveFrom, Timezone})` validates the complete public value before taking the writer lock or mutating storage, and errors begin with `import license file:`.
- [ ] A new database creates schema version 2 with `license_imports(pool_id, source_name, effective_ns, timezone, document_json, resolved_dates_json)` and vendor-aware `capacity_changes(pool_id, vendor, feature, effective_ns, licenses, uncounted, source_import_id)`; version 1 instead returns actionable recreate guidance.
- [ ] Import stores compact normalized document JSON and resolved UTC date boundaries, replaces a same-pool same-instant snapshot atomically, and can create the named pool as part of the transaction.
- [ ] Invalid request or document values, date-range failures, and cancellation at either lock checkpoint leave no new pool, source snapshot, or capacity change behind.
- [ ] Automated store tests cover validation, schema initialization and v1 rejection, replacement, date resolution, rollback, and cancellation.

**Depends on.** W-1.

## W-3 — Rebuild vendor-aware capacity changes

**Summary.** Stored snapshots need a capacity history that reports can trust. This item derives that history from each complete pool timeline. It keeps vendor identity with each feature. A failed rebuild leaves the prior state intact.

**Work to be done.**

- Build the pool-wide capacity projection from all persisted snapshots inside the import transaction.
  - Use the capacity-change schema introduced in W-2, rebuild after source replacement, and retain the source snapshot that established each change.
- Apply the pinned contributor and identity rules to produce a minimal change-point timeline.
  - Match capacity by pool, vendor, and feature; fold FEATURE and INCREMENT records as specified; honor START, expiration, authoritative empty snapshots, known zeros, and uncounted capacity.
- Protect the rebuild's atomicity and arithmetic boundaries.
  - Coalesce concurrent changes, suppress redundant rows, use checked addition, and check context before and after the existing writer lock as well as through transactional work.
- Prove the projection using multiple dated snapshots.
  - Cover out-of-order and replacement imports, expirations, empty authoritative snapshots, zero versus missing capacity, overflow, and rollback of the source row and projection together.

**Justification.** This descends from Work Unit 2, which requires an atomic vendor-and-feature capacity change-point projection before reports consume it.

**References.**

- **Shared artifacts** — [pinned contracts](#shared-reference-artifacts) define contributor intervals, identity matching, feature folding, and change-point behavior.
- **Plan work unit** — [Work Unit 2](feature-implementation-plan.md#work-units-and-sequencing) requires a three-snapshot projection with atomic rollback.

**Acceptance criteria.**

- [ ] Each import rebuilds a pool's `capacity_changes` inside the same transaction as source replacement, so an error or cancellation leaves both the snapshot and projection unchanged.
- [ ] Projection rows use exact `(pool, vendor, feature)` identity and the pinned finite, uncounted, and known-zero states; usage-only identities remain absent rather than being invented as zero.
- [ ] A snapshot is authoritative until the next snapshot, including an empty snapshot that ends every known identity at its effective instant; expiration creates a zero only when no active contributor remains.
- [ ] Multiple records changing at one instant are coalesced, redundant non-boundary rows are omitted, and checked arithmetic rejects overflow with the responsible identity and source line.
- [ ] Automated three-snapshot tests cover empty snapshots, expiration, replacement, zero, uncounted capacity, out-of-order imports, overflow, and transactional rollback.

**Depends on.** W-2.

## W-4 — Report capacity by vendor and feature

**Summary.** Reports must show which vendor supplies each feature. This item makes capacity lookup match that identity. It separates unlimited capacity from missing capacity. It preserves the existing report measures for finite capacity.

**Work to be done.**

- Build report rows from the union of relevant usage and capacity identities.
  - Match lmgrd daemon to license vendor only when pool, vendor, and feature all agree; retain capacity-only and usage-only identities in the requested range.
- Add vendor and uncounted capacity to the public report output.
  - Extend `CapacityBucket` and table rendering with the pinned fields; order JSON and table output by vendor, feature, and bucket start.
- Apply the distinct finite, uncounted, and missing states to report quality and arithmetic.
  - Keep finite peak and headroom calculations checked; uncounted is known unlimited, while each missing vendor-and-feature segment contributes to the existing coverage quality measures.
- Add a report state matrix.
  - Test same-named features from separate vendors, finite and uncounted siblings, capacity-only and usage-only rows, deterministic order, and checked overflow.

**Justification.** This descends from Work Unit 3, which requires capacity reports to match lmgrd usage by vendor and feature.

**References.**

- **Shared artifacts** — [pinned contracts](#shared-reference-artifacts) define exact-pair lookup and the vendor-aware report representation.
- **Plan work unit** — [Work Unit 3](feature-implementation-plan.md#work-units-and-sequencing) rewires reports to the vendor-aware projection.

**Acceptance criteria.**

- [ ] `CapacityBucket` exposes `Vendor string` as JSON `vendor` and `Uncounted bool` as JSON `uncounted`; table output has a leading VENDOR column.
- [ ] Report rows are distinct for each exact vendor-and-feature identity, including capacity-only and usage-only identities; an `AnalyticsQuery.Feature` filter still applies across all vendors.
- [ ] An uncounted row has no finite purchased value, no finite headroom or saturation measures, and does not create missing-capacity or over-capacity quality; a missing sibling remains missing.
- [ ] Known zero at an authoritative snapshot is reported as finite zero rather than missing capacity, and finite capacity retains checked report arithmetic.
- [ ] Automated state-matrix tests cover finite, uncounted, missing, capacity-only, usage-only, mixed-vendor, ordering, quality counters, and overflow.

**Depends on.** W-3.

## W-5 — Add the license parse command

**Summary.** Operators need to inspect a license file before import. This item exposes that inspection through the database command. It produces one stable JSON document on success. It never writes that document after a failed input close.

**Work to be done.**

- Add the `licenses parse` command path and its usage entry.
  - Support `goflexlmdb licenses parse [FILE|-]`; no path and `-` use standard input, while more than one path is a usage error.
- Complete input work before writing output.
  - Parse with W-1, close file input successfully, then write the parser's compact JSON form plus exactly one newline; standard input has a no-op close.
- Preserve the command error contract with a narrow package-local workflow seam.
  - Report usage errors as status 2 and open, parse, close, or output failures as status 1 with the existing command prefix; keep the seam test-only and focused on close/output order.
- Add focused command tests.
  - Cover standard input, file input, exact JSON output, bad arguments, source failures, close failure, and output failure.

**Justification.** This descends from Work Unit 4, which delivers the requested `licenses parse` inspection command.

**References.**

- **Shared artifacts** — [pinned contracts](#shared-reference-artifacts) define the parse command, JSON bytes, status behavior, and input-close ordering.
- **Plan work unit** — [Work Unit 4](feature-implementation-plan.md#work-units-and-sequencing) replaces CSV command guidance with license commands.

**Acceptance criteria.**

- [ ] `goflexlmdb licenses parse [FILE|-]` accepts no path, `-`, or one file path and writes the compact W-1 `LicenseFile` JSON bytes followed by one newline only after parsing and closing succeed.
- [ ] A parse, open, close, or output failure writes an operation-qualified `goflexlmdb:` error to standard error, produces no successful JSON, and returns status 1.
- [ ] A missing or unknown licenses subcommand and more than one parse path print usage to standard error and return status 2.
- [ ] Automated command tests cover standard input, file input, exact output, usage errors, and close and output failure ordering.

**Depends on.** W-1.

## W-6 — Import licenses and retire CSV guidance

**Summary.** Operators need one command that replaces the CSV workflow. This item imports one dated license snapshot for a pool. It keeps malformed input away from the database. It also makes the new workflow the only documented capacity import path.

**Work to be done.**

- Add the `licenses import` command and validate its caller-facing inputs.
  - Support `goflexlmdb licenses import --db DB --pool POOL --effective-from RFC3339 --timezone AREA/LOCATION FILE`; require exactly one path and an explicit valid timezone, including `UTC`.
- Parse and close the complete input before database work begins.
  - Pass the supplied path unchanged as the snapshot source name, then call W-2 with the parsed document only after input success; successful import writes nothing.
- Remove the replaced CSV command and exported entitlement API.
  - Retire `entitlements replace`, `Entitlement`, and `ReplaceEntitlements` along with CSV parsing and entitlement persistence, while preserving log import, rebuild, denial, and queue commands.
- Update operator documentation and runnable examples.
  - Replace CSV examples with synthetic license-file parse and import examples, explain recreate behavior for version 1, and show vendor-aware finite, uncounted, and missing report outcomes.
- Test the completed command cutover.
  - Keep the package-local test seam narrow and use it to prove parse-and-close-before-database-open behavior, exit status handling, and the absence of the old command path.

**Justification.** This descends from Work Unit 4 and the recorded boundary, both of which replace CSV entitlement ingestion with direct license-file import.

**References.**

- **Shared artifacts** — [recorded scope](#shared-reference-artifacts) requires direct import, CSV removal, and no version-1 migration.
- **Shared artifacts** — [pinned contracts](#shared-reference-artifacts) define the import command, store request, status rules, and vendor-aware report behavior.
- **Plan work unit** — [Work Unit 4](feature-implementation-plan.md#work-units-and-sequencing) delivers license import, removes the CSV surface, and updates guidance.

**Acceptance criteria.**

- [ ] `goflexlmdb licenses import --db DB --pool POOL --effective-from RFC3339 --timezone AREA/LOCATION FILE` accepts exactly one input, requires an explicit loadable timezone, passes the unmodified path as `SourceName`, and is silent on success.
- [ ] Import parses and closes input before opening the database; invalid input or close failure neither opens nor creates a database, while storage and other operational failures return status 1.
- [ ] Usage and flag failures print usage to standard error and return status 2; all operational failures use the `goflexlmdb:` error form and return status 1.
- [ ] `goflexlmdb entitlements replace`, CSV parsing, `sqlite.Entitlement`, and `Store.ReplaceEntitlements` are absent; the old command is rejected as a usage error.
- [ ] README and demo material use synthetic license files, document version-1 recreation and vendor-aware capacity states, and contain no CSV entitlement import workflow.
- [ ] Automated command tests cover source names, input-close and database-open ordering, silent success, statuses, old-command rejection, and unaffected log import, rebuild, denial, and queue commands.

**Depends on.** W-2, W-4, W-5.

## Cut for Scope

This is work the work items exclude, not work deferred for lack of evidence. There is no trigger that reopens an entry here; the recorded boundary already settled it.

- **Keep CSV capacity import or migrate version-1 data.** This would preserve the retired entitlement workflow and its data, but the [recorded scope](artifacts/scope-boundary.md) requires replacement and permits clean recreation.
- **Build a prompt-driven parser-program subsystem.** This would add a separate way to generate parsers, but the parent plan's [Cut for Scope](feature-implementation-plan.md#cut-for-scope) asks only for direct FlexLM parsing and import.
