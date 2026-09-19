# Change Decision Log: FlexLM License Import

This file records every decision committed while planning FlexLM License Import. The plan lives in [../change-plan.md](../change-plan.md). Current-state evidence lives in [current-state-findings.md](current-state-findings.md).

## Trivial decisions

None. Each committed decision either rejects an alternative, changes a compatibility surface, depends on current-state evidence, or pins a cross-component contract.

## Full decisions

### D-1: License files replace CSV entitlements

- **Question:** Does the new license-file workflow coexist with or replace CSV entitlement input?
- **Decision:** Remove `goflexlmdb entitlements replace`, `readEntitlements`, `sqlite.Entitlement`, `Store.ReplaceEntitlements`, their CSV fixtures, and their documentation after the direct license parser/import path exists.
- **Rationale:** Two capacity sources would preserve the disliked structure and create conflicting authority. The operator explicitly chose replacement.
- **Evidence:** User input; [C-1](current-state-findings.md#c-1-the-exported-entitlement-model-is-a-derived-capacity-point); [C-2](current-state-findings.md#c-2-the-command-parses-strict-csv-at-single-feature-granularity).
- **Behavior impact:** Changing. The user's answer was: “Replace it in favor of just parsing FlexLM licenses directly. We can support parsing to intermediate formats like json.”
- **Rejected alternatives:**
  - Retain CSV beside license imports — rejected because it creates two authoritative capacity paths and contradicts the requested replacement.
  - Translate license files into CSV — rejected because it discards the requested common record structure and metadata.
- **Revisit criterion:** A concrete external consumer that cannot migrate to license-file input is identified before release.
- **Dissent (if any):** None.
- **Settles delta entry:** S-8, S-9, S-10.
- **Dependent decisions:** D-2, D-5, D-8.
- **Referenced in plan:** Why This Change; Surface Delta; Behavior Changes.

### D-2: Parser and storage boundary

- **Question:** Which package owns license syntax, and what crosses into SQLite?
- **Decision:** The root `goflexlm` package exports the concrete `LicenseFile`, `LicenseServer`, `LicenseVendor`, `LicenseFeature`, `LicenseFeatureKind`, and `LicenseAttribute` structs plus `ParseLicenseFile(io.Reader) (LicenseFile, error)`. SQLite accepts `goflexlm.LicenseFile` in `LicenseImportRequest.File`. No parser interface, visitor, or SQLite dependency is added to the root package.
- **Rationale:** Syntax parsing is independently useful for JSON and library callers, while storage owns operational mapping and dates. Concrete types are the smallest structure with one implementation.
- **Evidence:** [C-1](current-state-findings.md#c-1-the-exported-entitlement-model-is-a-derived-capacity-point); [C-5](current-state-findings.md#c-5-reports-consume-a-narrow-derived-capacity-timeline); YAGNI simpler-version test; software architect proposal.
- **Behavior impact:** Preserving. Existing log-parser contracts are untouched; this adds a separate API.
- **Rejected alternatives:**
  - Put parsing in `cmd/goflexlmdb` — rejected because the requested JSON/library intermediate would not be reusable.
  - Add parser and store interfaces — rejected because each has one implementation and no second caller.
  - Put report-ready capacity in the parser — rejected because effective snapshot time, timezone, and usage identity belong to storage/import policy rather than file syntax.
- **Revisit criterion:** A second parser or storage implementation creates three concrete uses for an interface.
- **Dissent (if any):** None.
- **Settles delta entry:** S-1, S-3.
- **Dependent decisions:** D-3, D-4, D-5, D-8.
- **Referenced in plan:** Target State; Surface Delta; Change Units.

### D-3: License parser grammar and failure contract

- **Question:** Which syntax is accepted and how does incomplete understanding fail?
- **Decision:** Accept the exact grammar and worked JSON example in `change-plan.md#target-state`: official SERVER, VENDOR, FEATURE, and INCREMENT positions; ordered/repeated bare or `NAME=value` attributes; quoted values; LF/CRLF/final lines; trailing-backslash continuations; blank lines; and full lines whose first non-whitespace byte is `#`. Do not recognize inline comments; `#` is an ordinary token elsewhere. Preserve syntactically valid SERVER attributes without an allowlist. Parse VENDOR with a deterministic positional phase followed by key/value-only attributes, as pinned in the plan; there are no generic bare VENDOR flags. SERVER ports are 1–65535; VENDOR `PORT` and `EPORT` values are 0–65535. Recognize at most one argument-free `USE_SERVER` as a document-presence flag. Normalize directive kinds to uppercase constants, match semantic tokens/START case-insensitively while preserving attribute/value spelling, and normalize numeric zero/`uncounted` to `Licenses == nil` and `Uncounted == true`. Accept finite `d[d]-mmm-yyyy` dates plus non-expiring expiration-year spellings `0`, `00`, `000`, `0000`, and `1900`; `START` accepts only finite dates. Pin record lines to the first physical line, token-specific errors to the token's physical line, and whole-record errors to the first line. Reject malformed recognized records, `PACKAGE`, `UPGRADE`, and unknown directives with `parse license file: line N: ...`; return no partial document on content or reader failure.
- **Rationale:** These rules cover the requested common model without silently producing incomplete capacity. A generic extension framework or partial-diagnostic API is unnecessary for an all-or-nothing snapshot.
- **Evidence:** User request; junior-developer reframing; official Revenera [SERVER](https://docs.revenera.com/fnp/2025r2/LicAdmin_Guide/Content/helplibrary/SERVER_Lines.htm), [VENDOR](https://docs.revenera.com/fnp/2024r2/LicAdmin_Guide/Content/helplibrary/VENDOR_Lines.htm), [FEATURE/INCREMENT](https://docs.revenera.com/fnp/2024r2/LicAdmin_Guide/Content/helplibrary/FEATURE_and_INCREMENT_Lines.htm), and [format overview](https://docs.revenera.com/fnp/2022r3/LicAdmin_Guide/Content/helplibrary/License_File_Format_Overview.htm); [C-3](current-state-findings.md#c-3-parse-failure-precedes-storage-and-replacement-is-atomic).
- **Behavior impact:** Preserving. This is a new parser; existing parsing behavior is unchanged.
- **Rejected alternatives:**
  - Ignore unsupported directives — rejected because capacity-bearing lines could be silently omitted.
  - Reject `USE_SERVER` — rejected because it is common, argument-free, and does not change capacity.
  - Preserve every unknown directive generically — rejected because successful import would imply semantics the capacity derivation does not understand.
  - Add recoverable diagnostics — rejected because a partial snapshot must never become authoritative.
- **Revisit criterion:** The scoped record set expands or a caller needs partial document inspection without storage.
- **Dissent (if any):** The initial software architect proposal rejected `USE_SERVER`; junior-developer review and official documentation established the smaller safe exception.
- **Settles delta entry:** S-1.
- **Dependent decisions:** D-4, D-6, D-8, D-9.
- **Referenced in plan:** Target State; Surface Delta; Risks; Deferred (YAGNI).

### D-4: Snapshot and date semantics

- **Question:** When does a license file and each dated grant affect historical capacity?
- **Decision:** `LicenseImportRequest` requires non-zero `EffectiveFrom` and a `Timezone string` that is exactly `UTC`, or contains `/` and is accepted by `time.LoadLocation`; blank, `Local`, fixed-zone abbreviations, and unknown names are rejected. The snapshot applies at `EffectiveFrom`. Finite-only `START` activates at local midnight but not before the snapshot. A finite expiration ends at local midnight at the start of the printed date; `permanent` and year spellings `0`, `00`, `000`, `0000`, and `1900` do not expire. Empty contributor intervals are valid and contribute nothing. Import persists resolved UTC boundaries. The CLI requires both RFC3339 `--effective-from` and explicit `--timezone`.
- **Rationale:** An explicit snapshot time makes backfills repeatable; import time and file timestamps do not. Date-only values need an explicit zone. Start-of-date expiration follows published FlexNet semantics rather than inventing an inclusive extra day.
- **Evidence:** Junior-developer reframing; [C-5](current-state-findings.md#c-5-reports-consume-a-narrow-derived-capacity-timeline); existing explicit `EffectiveFrom` in [C-1](current-state-findings.md#c-1-the-exported-entitlement-model-is-a-derived-capacity-point); official Revenera [expiration guidance](https://docs.revenera.com/fno2025r1_onprem/producer/Content/helplibrary/opsConfEnt.htm). The expiration rule is an explicit importer policy backed by a single publisher source; a contradictory representative fixture reopens it.
- **Behavior impact:** Preserving. This contract belongs to the new import path; existing log/report inputs retain their meanings.
- **Rejected alternatives:**
  - Use import time — rejected because re-importing the same historical file changes results.
  - Infer the snapshot from `START` or `ISSUED` — rejected because values can be missing or differ per record.
  - Expire after the printed day — rejected because Revenera documents start-of-date expiration; for example, `01-jan-2027` in `America/New_York` stops at `2027-01-01T05:00:00Z`.
- **Revisit criterion:** An authoritative source format supplies a distinct file-level effective instant.
- **Dissent (if any):** The initial software architect proposal used the following midnight; official evidence corrected it to the start of the printed date.
- **Settles delta entry:** S-3, S-7.
- **Dependent decisions:** D-5, D-6, D-8.
- **Referenced in plan:** Target State; Surface Delta; Risks.

### D-5: Clean schema and snapshot storage

- **Question:** How is the rich license source persisted, and what happens to schema-version-1 data?
- **Decision:** Initialize the exact schema version 2 layouts pinned in `change-plan.md#target-state`: `license_imports` is unique on `(pool_id, effective_ns)` and stores source, timezone, compact canonical validated `LicenseFile` JSON, and resolved boundary JSON; `capacity_changes` uses `(pool_id, vendor, feature, effective_ns)`, a checked nullable-count/uncounted pair, and a source-import index. Store no import wall-clock. Import creates a missing pool in its transaction. Replace a same-instant snapshot atomically, and treat a zero-feature document as an authoritative empty snapshot. Reject schema version 1 with delete-and-recreate instructions; implement no migration or legacy table.
- **Rationale:** Old CSV rows lack vendor, record, and source identity and cannot be converted faithfully. The operator confirmed no users or production data, so migration machinery and legacy retention have no current value.
- **Evidence:** User input; junior-developer reframing; [C-4](current-state-findings.md#c-4-entitlement-persistence-is-flattened-and-has-no-source-provenance); [C-9](current-state-findings.md#c-9-a-schema-change-requires-a-new-migration-path); [C-12](current-state-findings.md#c-12-the-scoped-analytics-implementation-is-uncommitted-working-tree-content).
- **Behavior impact:** Changing. The user's answer was: “There are no users yet and no production data. Wipe everything and start from a clean slate.”
- **Rejected alternatives:**
  - Migrate old rows — rejected because missing vendor/source fields would be invented.
  - Archive legacy rows — rejected because no user or production data exists and reports would not use them.
  - Silently delete rows during upgrade — rejected in favor of a clear recreate boundary.
  - Normalize SERVER, VENDOR, FEATURE, and attributes into child tables — rejected because no current query needs those joins; canonical document JSON plus derived capacity is strictly simpler.
- **Revisit criterion:** A deployed database or external consumer exists before the cutover.
- **Dissent (if any):** None after operator decision.
- **Settles delta entry:** S-4.
- **Dependent decisions:** D-6, D-8, D-10.
- **Referenced in plan:** Current State; Target State; Surface Delta; Behavior Changes; Change Units.

### D-6: Capacity projection and usage identity

- **Question:** How do parsed grants become the capacity timeline joined to lmgrd usage?
- **Decision:** Derive capacity per exact `(pool, vendor, feature)` identity. Feature slice order is authoritative and must have strictly increasing source lines. Use the first `FEATURE` per vendor-plus-feature identity and add every `INCREMENT`; sum active finite counts with checked addition, while any active uncounted contributor makes the interval uncounted. Apply snapshot, START, expiration, and next-snapshot boundaries. Build the identity universe across all snapshots, emit each identity's earliest known value, then emit later rows only when its value changes; zero carries through consecutive omissions. Report each vendor-plus-feature identity separately. Preserve version and pooling attributes in document JSON but do not split capacity by values absent from ordinary lmgrd activity.
- **Rationale:** Exact vendor/feature is the richest shared identity current data exposes. Aggregating hidden FlexNet pools is necessary because logs cannot reliably identify them, while alias tables and SERVER inference lack evidence.
- **Evidence:** [C-5](current-state-findings.md#c-5-reports-consume-a-narrow-derived-capacity-timeline); [C-6](current-state-findings.md#c-6-usage-identity-is-richer-than-capacity-identity); [C-7](current-state-findings.md#c-7-pools-and-streams-are-assigned-by-the-operator); official Revenera [FEATURE/INCREMENT semantics](https://docs.revenera.com/fnp/2024r2/LicAdmin_Guide/Content/helplibrary/FEATURE_and_INCREMENT_Lines.htm); software architect proposal with YAGNI simplification. The vendor documentation is a single-publisher web source; this is an explicit importer policy and a contradictory representative fixture reopens it.
- **Behavior impact:** Changing. Capacity is sourced from parsed licenses and matched to daemon/vendor as the operator requested.
- **Rejected alternatives:**
  - Match only feature — rejected because same-named features from different daemons can consume each other's capacity.
  - Add alias configuration — rejected because no current mismatch proves it is needed.
  - Infer pools from SERVER — rejected because current pools are explicit administrative groupings and SERVER identity is not a stable replacement.
  - Persist and report every FlexNet pool separately — rejected because ordinary log usage lacks a reliable pool key.
- **Revisit criterion:** Real license/log fixtures show exact vendor/feature mismatch or expose a reliable version/pool key in every usage event.
- **Dissent (if any):** The software architect proposed a pool-discriminating key for FEATURE processing; the plan simplifies to the observable vendor-plus-feature aggregate because pooling attributes do not change total capacity and cannot be joined to current logs.
- **Settles delta entry:** S-5.
- **Dependent decisions:** D-7, D-8, D-10, D-11.
- **Referenced in plan:** Target State; Surface Delta; Behavior Changes; Risks.

### D-7: Uncounted report contract

- **Question:** How do reports distinguish unlimited FlexLM capacity from missing entitlement data?
- **Decision:** Add `CapacityBucket.Uncounted bool \`json:"uncounted"\``. For an uncounted interval, set `Purchased` to nil and `Uncounted` to true, print `uncounted` in the table Purchased column, retain usage/peak/used-seat measures, set both saturation durations to zero, set all unused-seat/headroom fields to nil, do not increment missing-entitlement quality, and do not set `UsageAboveEntitlement`. Missing capacity remains `Purchased == nil` and `Uncounted == false`.
- **Rationale:** Mapping uncounted to zero creates false over-capacity alerts; mapping it to missing says a valid license was not found. One boolean is the smallest correct public distinction.
- **Evidence:** Official Revenera [counted-versus-uncounted semantics](https://docs.revenera.com/fnp/2023r1/LicAdmin_Guide/Content/helplibrary/Counted_vs__Uncounted_Licenses.htm); junior-developer reframing; user approval. User approval and the importer-state requirement mean the recommendation does not rest on the web source alone.
- **Behavior impact:** Changing. The user's answer to the proposed JSON/table/quality contract was: “Yeah that works.”
- **Rejected alternatives:**
  - Treat uncounted as missing — rejected because the license mapping is known.
  - Treat uncounted as zero — rejected because usage would always appear over capacity.
  - Reject uncounted files — rejected because it is common supported syntax.
  - Add a polymorphic capacity type or enum — rejected because a boolean beside the existing nullable purchased count is sufficient.
- **Revisit criterion:** Capacity gains more than the current three states: missing, counted, and uncounted.
- **Dissent (if any):** None after operator approval.
- **Settles delta entry:** S-6.
- **Dependent decisions:** D-8, D-11.
- **Referenced in plan:** Target State; Surface Delta; Behavior Changes; Change Units.

### D-8: License CLI contract

- **Question:** How do operators inspect and import license files after CSV removal?
- **Decision:** Add `goflexlmdb licenses parse [FILE|-]` and `goflexlmdb licenses import --db DB --pool POOL --effective-from RFC3339 --timezone AREA/LOCATION FILE`. Parse accepts zero or one path, defaults to standard input, and for file input completes parsing and a successful close before emitting normalized compact JSON plus one newline; stdin close is a no-op. Import requires exactly one path or `-`, requires explicit timezone, passes the path text exactly as supplied as `SourceName` (`-` for stdin), parses and closes input before opening the database, calls `Store.ImportLicenseFile`, and is silent on success. Usage errors print usage to stderr and return 2. Open/read/close, parse, output, and database failures are path-qualified where applicable, use the `goflexlmdb:` prefix, write only to stderr, and return 1. Missing/unknown `licenses` subcommands and extra paths are usage errors. Success returns 0.
- **Rationale:** Separate parse and import actions support the requested JSON intermediate without making JSON a second input grammar. The import command matches the document-scoped transaction.
- **Evidence:** User request; [C-2](current-state-findings.md#c-2-the-command-parses-strict-csv-at-single-feature-granularity); [C-3](current-state-findings.md#c-3-parse-failure-precedes-storage-and-replacement-is-atomic); [C-10](current-state-findings.md#c-10-json-is-not-yet-a-license-document-contract).
- **Behavior impact:** Changing. The new commands accept inputs previously rejected; the operator requested direct license import and JSON intermediate output. Existing log commands remain unchanged, while CSV removal is recorded in D-1.
- **Rejected alternatives:**
  - Import JSON — rejected because it creates a second authoritative validation path with no current caller.
  - Add flags to the root `goflexlm` JSONL command — rejected because that command's existing debug-log contract is protected.
  - Accept several license files per call — rejected because snapshot merge ordering and effective times would become ambiguous.
- **Revisit criterion:** A real workflow requires atomic import of several separately sourced license files at one effective instant.
- **Dissent (if any):** None.
- **Settles delta entry:** S-2, S-7, S-8.
- **Dependent decisions:** —
- **Referenced in plan:** Target State; Surface Delta; Behavior Changes; Change Units.

### D-9: Public document validation

- **Question:** What prevents a caller-constructed `LicenseFile` from bypassing parser invariants at the store boundary?
- **Decision:** Before locking or starting a transaction, `Store.ImportLicenseFile` normalizes nil top-level slices to empty arrays, then rejects a blank pool/source, zero effective time, a timezone outside D-4, non-positive record lines, feature lines not strictly increasing in slice order, blank required positional fields, invalid feature kinds, SERVER ports outside 1–65535, VENDOR `PORT`/`EPORT` outside 0–65535, blank attribute names, any bare VENDOR attribute, non-empty values on other bare attributes, invalid/repeated/non-finite case-insensitive START values, invalid expiration values, and feature counts not exactly `(Uncounted=true, Licenses=nil)` or `(Uncounted=false, Licenses>0)`. Parser success enforces the same document-only invariants. Effective and resolved times must preserve instant equality after UTC/nanosecond reconstruction, ignoring location and monotonic metadata. Count aggregation uses checked addition within Go `int` and SQLite signed-int64 bounds. Errors begin `import license file:`, include source lines for record dates and overflow, and validation never mutates storage.
- **Rationale:** Exported concrete structs are intentionally constructible by library callers; the store must enforce the same semantic boundary as the parser without introducing factories or interfaces.
- **Evidence:** Test-engineer review; [C-3](current-state-findings.md#c-3-parse-failure-precedes-storage-and-replacement-is-atomic); root AGENTS.md error and atomicity rules.
- **Behavior impact:** Preserving. It defines rejection on the new store API without changing existing callers.
- **Rejected alternatives:**
  - Trust only parser-produced values — rejected because the public structs can be built directly.
  - Make every field private and add constructors — rejected as a larger API that impedes JSON/library use.
  - Validate after mutation begins — rejected because invalid public input must not risk replacing a valid snapshot.
- **Revisit criterion:** The public model becomes construction-safe through a different concrete representation.
- **Dissent (if any):** None.
- **Settles delta entry:** S-3.
- **Dependent decisions:** D-10.
- **Referenced in plan:** Target State; Change Units; Review Findings.

### D-10: Atomic authoritative rebuild

- **Question:** How does snapshot replacement avoid stale or partially rebuilt capacity?
- **Decision:** Validate and resolve dates first; then under one writer lock and transaction ensure the pool, replace the snapshot, delete all capacity changes for its pool, read every snapshot in ascending time order, and compute the identity universe from all validated feature records. Rebuild each authoritative `[snapshot, next snapshot)` interval as a change-point timeline: at the earliest boundary emit every identity's value, treating absence as known zero; afterward emit boundary or internal rows only when the value changes. Empty or inactive records still establish identity. Coalesce changes at one instant and commit source plus projection together. Any error, overflow, or cancellation rolls back both. `source_import_id` names the snapshot that established the current value, while `license_imports` independently determines the authoritative source document. Known zero is distinct from missing capacity.
- **Rationale:** Point timelines carry the last value forward. Without explicit termination rows and one transaction, stale grants or half-updated source/projection data can survive.
- **Evidence:** Data-engineer and test-engineer reviews; [C-3](current-state-findings.md#c-3-parse-failure-precedes-storage-and-replacement-is-atomic); [C-5](current-state-findings.md#c-5-reports-consume-a-narrow-derived-capacity-timeline).
- **Behavior impact:** Changing. It defines the new authoritative snapshot semantics required by direct license import.
- **Rejected alternatives:**
  - Incrementally patch only the inserted snapshot — rejected because out-of-order insertion can change every later interval.
  - Leave absent identities unchanged — rejected because stale capacity would carry forward.
  - Store contributor-level or redundant per-boundary lineage — rejected because no current query needs it; value-establishing snapshot provenance is sufficient.
- **Revisit criterion:** Measured rebuild cost shows full-pool recomputation is unacceptable.
- **Dissent (if any):** None.
- **Settles delta entry:** S-4, S-5.
- **Dependent decisions:** —
- **Referenced in plan:** Target State; Change Units; Risks; Review Findings.

### D-11: Vendor-visible capacity output

- **Question:** Can feature-level report rows truthfully combine capacity from different vendors?
- **Decision:** Add `CapacityBucket.Vendor string \`json:"vendor"\`` and a leading VENDOR table column. Return separate buckets for each exact vendor-plus-feature identity, ordered ascending by `(vendor, feature, bucket start)` in JSON and tables. Evaluate missing/above-entitlement quality per vendor/feature/time segment: uncounted contributes neither signal, while missing or breached sibling vendors still contribute. Every report duration and seat-time calculation uses checked signed 64-bit subtraction, multiplication, and accumulation; overflow returns an operation-qualified report error. Keep `AnalyticsQuery.Feature` as the existing cross-vendor feature filter and add no vendor filter.
- **Rationale:** A combined row can let one vendor's uncounted or finite capacity hide another vendor's uncovered or over-capacity usage. Separate rows preserve the identity already used for correct joins and are simpler than mixed-state folding rules.
- **Evidence:** [C-6](current-state-findings.md#c-6-usage-identity-is-richer-than-capacity-identity); data-engineer, test-engineer, UX, and junior-developer reviews; user approval.
- **Behavior impact:** Changing. The user approved separate vendor-plus-feature rows in response to: “May capacity reports add a `vendor` field/column and return one row per vendor plus feature?” Answer: “Yes.”
- **Rejected alternatives:**
  - Keep feature-only rows with mixed-state truth tables — rejected because they erase the identity that determines which capacity can serve which usage.
  - Let uncounted dominate the combined row — rejected because it can hide a finite-vendor breach or missing mapping.
  - Add a vendor query filter now — rejected because no caller has requested it.
- **Revisit criterion:** A higher-level report explicitly needs a separately named, clearly qualified cross-vendor summary.
- **Dissent (if any):** None.
- **Settles delta entry:** S-11.
- **Dependent decisions:** —
- **Referenced in plan:** Target State; Surface Delta; Behavior Changes; Change Units; Review Findings.
