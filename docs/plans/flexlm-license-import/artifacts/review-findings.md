# Review Findings: FlexLM License Import

This file records findings from the iterative review of [change-plan.md](../change-plan.md). Round history is in [review-iteration-history.md](review-iteration-history.md).

## Major findings

### F1: START accepted non-finite values with no activation instant

- **Agent:** junior-developer + edge-case-explorer
- **Category:** ambiguity
- **Finding:** `START` reused the expiration grammar, which includes `permanent` and zero-year forms that cannot resolve to an activation instant.
- **Evidence considered:** Provided plan, Target State date grammar and D-4.
- **Resolution:** Restricted `START` to finite dates and added validation coverage for non-finite forms.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F2: Empty and reversed contributor intervals were undefined

- **Agent:** junior-developer + edge-case-explorer
- **Category:** edge case
- **Finding:** The plan did not define records already expired at the snapshot or records whose `START` is at or after expiration.
- **Evidence considered:** Provided plan, Target State snapshot, START, and expiration rules.
- **Resolution:** Defined half-open contributor intervals; an empty interval contributes nothing and does not reject the import.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F3: Parser and store validity could diverge

- **Agent:** junior-developer + edge-case-explorer
- **Category:** unhandled failure mode
- **Finding:** Store validation rejected document states that the parser grammar did not clearly reject, despite the promise that every successful parse produces a valid document.
- **Evidence considered:** Provided plan, Target State validation and Unit 1 fuzz contract; D-9.
- **Resolution:** Required the parser to enforce every document-only import invariant and limited empty quoted strings to attribute values.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State

### F4: SERVER and VENDOR metadata acceptance was contradictory

- **Agent:** junior-developer + edge-case-explorer; self-review
- **Category:** ambiguity
- **Finding:** The model promised unknown metadata preservation, but SERVER referred to an absent allowlist and VENDOR had no generic trailing-attribute grammar.
- **Evidence considered:** Provided plan, public `Attributes` contract and compact grammar; official VENDOR syntax (web, publisher source).
- **Resolution:** Removed the metadata allowlist and allowed syntactically valid trailing SERVER and VENDOR attributes while retaining positional VENDOR normalization.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State

### F5: Inline hash behavior contradicted the verification promise

- **Agent:** junior-developer + edge-case-explorer
- **Category:** assumption refuted
- **Finding:** The grammar made inline `#` ordinary token content, while Unit 1 promised generic inline-comment rejection.
- **Evidence considered:** Provided plan, Target State and Unit 1.
- **Resolution:** Kept only full-line comments special and made inline `#` succeed or fail under the enclosing directive grammar.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F6: Count token and aggregate overflow had no contract

- **Agent:** junior-developer + edge-case-explorer; adversarial-validator + data-engineer; evidence-based-investigator
- **Category:** unhandled failure mode
- **Finding:** Positive decimals feed a public Go `int`, active counts are summed, and SQLite stores signed integers, but no range or checked-addition rule prevented silent overflow.
- **Evidence considered:** Codebase `sqlite/types.go`; provided plan public model, capacity projection, and schema.
- **Resolution:** Limited parsed counts to Go `int`, required checked aggregation within Go `int` and SQLite signed 64-bit bounds, and added rollback-focused overflow tests.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F7: Vendor-aware UncoveredNanoseconds semantics were unpinned

- **Agent:** junior-developer + edge-case-explorer; adversarial-validator + data-engineer; evidence-based-investigator
- **Category:** ambiguity
- **Finding:** The plan split capacity by vendor but did not say whether the public uncovered duration is wall-clock union time or missing identity-duration.
- **Evidence considered:** Codebase `sqlite/reports.go` additive feature behavior and `sqlite/types.go` public field; provided plan Target State.
- **Resolution:** Preserved additive accounting per missing vendor-plus-feature segment and stated that totals may exceed the report span.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F8: Parse close failure could occur after JSON output

- **Agent:** junior-developer + edge-case-explorer
- **Category:** unhandled failure mode
- **Finding:** The CLI contract covered close failures but did not order a parse file's close before stdout, allowing valid-looking JSON followed by status 1.
- **Evidence considered:** Codebase `cmd/goflexlmdb/main.go` deferred entitlement close pattern; provided plan CLI contract.
- **Resolution:** Required successful file close before parse output and kept stdin close as a no-op.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F9: USE_SERVER cardinality and preservation were ambiguous

- **Agent:** junior-developer + edge-case-explorer
- **Category:** ambiguity
- **Finding:** A boolean could not preserve duplicate directives, line numbers, or ordering despite a broader storage-preservation claim.
- **Evidence considered:** Provided plan public model and canonical storage wording.
- **Resolution:** Kept the smaller boolean contract, rejected duplicates and arguments, and narrowed preservation to semantic presence.
- **Resolved by:** re-reframing
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F10: Continued-record line attribution was undefined

- **Agent:** junior-developer + edge-case-explorer
- **Category:** ambiguity
- **Finding:** Exported line numbers and line-qualified failures did not specify first-line versus detection-line behavior across continuations.
- **Evidence considered:** Codebase `decoder.go` physical-line convention; provided plan continuation and error contracts.
- **Resolution:** Pinned exported `Line` to the first physical line, detection errors to their physical line, and EOF-after-continuation to the logical record's first line.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F11: Consecutive omissions broke authoritative snapshot provenance

- **Agent:** evidence-based-investigator
- **Category:** internal contradiction
- **Finding:** An adjacent-snapshot identity union leaves the first zero row carrying old `source_import_id` through a second omission, contradicting authoritative-snapshot provenance.
- **Evidence considered:** Provided plan Target State and D-10 lifecycle.
- **Resolution:** Required a boundary row for every historically established pool identity at every later snapshot, including repeated zero rows.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F12: Zero-feature snapshots had an unspoken destructive meaning

- **Agent:** adversarial-validator + data-engineer
- **Category:** edge case
- **Finding:** Blank, comment-only, or metadata-only documents could terminate all capacity, but the authoritative snapshot contract did not state that result.
- **Evidence considered:** Provided plan parser acceptance and omission-to-zero behavior.
- **Resolution:** Explicitly made zero-feature documents authoritative empty snapshots and added direct coverage.
- **Resolved by:** re-reframing
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F13: Caller-built feature order could disagree with source lines

- **Agent:** adversarial-validator + data-engineer
- **Category:** ambiguity
- **Finding:** First-FEATURE behavior depended on slice order, but caller-built values could supply duplicate or decreasing feature lines.
- **Evidence considered:** Codebase `sqlite/entitlements.go` validates behavior-bearing order; provided plan public model and folding rule.
- **Resolution:** Declared feature slice order authoritative and required strictly increasing feature line numbers.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F14: First license import did not define pool creation

- **Agent:** adversarial-validator + data-engineer
- **Category:** overlap
- **Finding:** The new lifecycle omitted missing-pool behavior even though import may be the first operation on a clean database.
- **Evidence considered:** Codebase `sqlite/entitlements.go` transactional ensure-pool pattern and `sqlite/reports.go` existing-pool lookup.
- **Resolution:** Reused transactional pool creation and added an empty-database test.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F15: Uncounted finite-capacity metrics were unspecified

- **Agent:** adversarial-validator + data-engineer
- **Category:** ambiguity
- **Finding:** The plan pinned purchased and quality state but not saturation, unused-seat, or headroom fields for uncounted buckets.
- **Evidence considered:** Codebase public `CapacityBucket` fields and current report computation; provided plan S-6.
- **Resolution:** Retained usage, peaks, and used-seat time; set saturation durations to zero and finite-capacity-derived unused/headroom fields to nil.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F16: FlexNet semantic claims overstated their evidence quality

- **Agent:** adversarial-validator + data-engineer; evidence-based-investigator
- **Category:** standards conflict
- **Finding:** Expiration, FEATURE/INCREMENT folding, and zero-as-uncounted rules were described as verified although repository evidence cannot settle them and their web evidence came from one publisher.
- **Evidence considered:** Codebase discovery C-8 and its unauditable-findings section; provided decision log; official Revenera documentation (web, single publisher).
- **Resolution:** Added exact citations, reframed the rules as explicit importer policies, identified the single-publisher limitation, and named a contradictory representative fixture as the reopening trigger. User approval independently supports the uncounted public state.
- **Resolved by:** re-reframing
- **Raised in round:** R1
- **Changed in plan:** Target State; Risks; Open Items

### F17: VENDOR port zero was incorrectly rejected

- **Agent:** self-review
- **Category:** assumption refuted
- **Finding:** The plan applied SERVER's 1–65535 range to VENDOR ports, but the VENDOR contract permits zero for an ephemeral port.
- **Evidence considered:** Official Revenera VENDOR documentation for 2024 R2 and 2026 R1 (web, same publisher); official SERVER documentation distinguishes its 1–65535 range.
- **Resolution:** Split the ranges: SERVER remains 1–65535, while VENDOR `PORT` and `EPORT` accept 0–65535.
- **Resolved by:** evidence
- **Raised in round:** R1
- **Changed in plan:** Target State; Change Units

### F18: Repeated zero rows were unnecessary provenance machinery

- **Agent:** evidence-based-investigator
- **Category:** YAGNI candidate
- **Finding:** Repeating unchanged zero at every later snapshot served only per-boundary provenance; no current report or public contract consumes that lineage.
- **Evidence considered:** Codebase `sqlite/reports.go` reads value change points; provided scope and D-10 reject unused contributor lineage.
- **Resolution:** Replaced repeated boundary rows with the simpler change-point model. `source_import_id` now identifies the snapshot that established the current value, while `license_imports` identifies the authoritative document.
- **Resolved by:** re-reframing
- **Raised in round:** R2
- **Changed in plan:** Target State; Change Units

### F19: VENDOR positional and generic attribute parsing was ambiguous

- **Agent:** junior-developer + edge-case-explorer
- **Category:** ambiguity
- **Finding:** Optional daemon, options, and port positions combined with generic bare attributes gave several lines more than one valid parse.
- **Evidence considered:** Provided plan compact grammar and F4 resolution.
- **Resolution:** Pinned a left-to-right positional phase followed by key/value-only attributes; bare VENDOR flags are not accepted, and skipped positions require prefixes.
- **Resolved by:** evidence
- **Raised in round:** R2
- **Changed in plan:** Target State

### F20: Continued-record semantic error lines were still undefined

- **Agent:** junior-developer + edge-case-explorer
- **Category:** ambiguity
- **Finding:** The first review pinned lexical errors but not invalid dates, counts, ports, attributes, or arity discovered across continued physical lines.
- **Evidence considered:** Codebase `decoder.go` physical-line convention; provided plan line-error contract.
- **Resolution:** Token-specific lexical and semantic failures report the token's physical line; missing-token and whole-record arity failures report the logical record's first line.
- **Resolved by:** evidence
- **Raised in round:** R2
- **Changed in plan:** Target State; Change Units

### F21: Identity establishment and empty-snapshot zero were incomplete

- **Agent:** junior-developer + edge-case-explorer; adversarial-validator + data-engineer
- **Category:** edge case
- **Finding:** Earlier authoritative empty snapshots could not establish zero for identities introduced later, and feature records with empty active intervals had no defined identity effect.
- **Evidence considered:** Codebase reports distinguish absent capacity from known zero; provided plan empty snapshot and interval rules.
- **Resolution:** Build the pool identity universe from every feature record in every snapshot, regardless of active interval, and evaluate every identity from the earliest snapshot boundary. Activity-only identities remain missing.
- **Resolved by:** evidence
- **Raised in round:** R2
- **Changed in plan:** Target State; Change Units

### F22: Additive UncoveredNanoseconds could overflow

- **Agent:** adversarial-validator + data-engineer
- **Category:** unhandled failure mode
- **Finding:** Summing missing identity-duration can exceed the report span and eventually overflow the public signed 64-bit field.
- **Evidence considered:** Codebase `sqlite/types.go` and unchecked accumulation in `sqlite/reports.go`; provided plan additive semantics.
- **Resolution:** Required checked signed 64-bit accumulation and an operation-qualified capacity-report error on overflow.
- **Resolved by:** evidence
- **Raised in round:** R2
- **Changed in plan:** Target State; Change Units

### F23: Exact time round-trip did not define equality

- **Agent:** adversarial-validator + data-engineer
- **Category:** ambiguity
- **Finding:** Struct equality could reject representable non-UTC or monotonic-bearing `time.Time` values even when their instants survive Unix-nanosecond conversion.
- **Evidence considered:** Codebase normalizes entitlement instants through UTC Unix nanoseconds; provided plan validation rule.
- **Resolution:** Defined representability by instant equality after UTC reconstruction, ignoring location and monotonic metadata, and added both cases to validation coverage.
- **Resolved by:** evidence
- **Raised in round:** R2
- **Changed in plan:** Target State; Change Units

### F24: Store validation allowed parser-forbidden bare VENDOR attributes

- **Agent:** junior-developer + edge-case-explorer; evidence-based-investigator
- **Category:** internal contradiction
- **Finding:** The VENDOR parser accepts only key/value attributes, but record-agnostic store validation still allowed `HasValue == false` with an empty value.
- **Evidence considered:** Provided plan grammar, validation contract, and D-9 parity commitment.
- **Resolution:** Required every VENDOR attribute to have a value and added caller-built rejection coverage; SERVER and FEATURE/INCREMENT retain bare attributes.
- **Resolved by:** evidence
- **Raised in round:** R3
- **Changed in plan:** Target State; Change Units

### F25: Finite capacity report arithmetic could still overflow

- **Agent:** adversarial-validator + data-engineer; evidence-based-investigator
- **Category:** unhandled failure mode
- **Finding:** Valid extreme time ranges and large quantities can overflow signed 64-bit duration, multiplication, and seat-time accumulation beyond the previously covered uncovered-duration counter.
- **Evidence considered:** Codebase `sqlite/reports.go` unchecked arithmetic and `sqlite/types.go` signed 64-bit public fields; provided plan accepted bounds.
- **Resolution:** Required checked signed 64-bit arithmetic for every report duration and seat-time calculation with operation-qualified errors and focused tests.
- **Resolved by:** evidence
- **Raised in round:** R3
- **Changed in plan:** Target State; Change Units

### F26: D-6 retained the repeated-zero model removed by F18

- **Agent:** evidence-based-investigator
- **Category:** standards conflict
- **Finding:** D-6 still required repeated zero boundary rows while the plan and D-10 used the simplified change-point model.
- **Evidence considered:** Provided plan Target State, D-6, D-10, and F18 resolution.
- **Resolution:** Updated D-6 to compute the pool-wide identity universe and emit only initial values and later changes.
- **Resolved by:** evidence
- **Raised in round:** R3
- **Changed in plan:** Target State; artifacts/change-decision-log.md D-6

## Minor edits

- F27: Reconciled the old review summary and D-4 evidence wording with the single-publisher importer-policy characterization — evidence-based-investigator — Review Findings; artifacts/change-decision-log.md D-4
