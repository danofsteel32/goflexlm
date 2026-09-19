# Implementation Iteration History: FlexLM License Import

Round-by-round review for the implementation plan. The primary plan is [../feature-implementation-plan.md](../feature-implementation-plan.md) and committed implementation decisions are in [implementation-decision-log.md](implementation-decision-log.md).

## R1: Cross-surface implementation review

- **Specialists engaged:** `junior-developer`, `data-engineer`, and `test-engineer`.
- **New input provided:** The source [change plan](../change-plan.md), its decision/current-state artifacts, [scope boundary](scope-boundary.md), and [discovery notes](.discovery-notes.md). No visual material exists. Specialists read the discovery notes first.
- **Claim ledger:**
  - **Evidenced — J1, DE2:** The clean version-2 initialization and version-1 rejection must be one `Open` path: a new database receives only version-2 tables in its initialization transaction; version 1 returns the pinned delete-and-recreate error without applying legacy DDL. Evidence: `change-plan.md` / Target State / “Schema version 2”; D-5 / Decision; `sqlite/schema.go`; `sqlite/store.go` / `migrate`.
  - **Evidenced — J3, TE1:** Parser acceptance and store document validation need a shared conformance corpus, while caller-constructed invalid documents remain store tests. Evidence: `change-plan.md` / Target State / “ParseLicenseFile enforces”; D-9 / Decision; Change Units / Unit 1 and Unit 2.
  - **Evidenced — J4:** Develop and verify the pool-wide, three-snapshot projection before rewiring capacity reports, so absent-but-known-zero capacity and source provenance cannot be mistaken for report behavior. Evidence: D-10 / Decision; Change Units / Unit 2.
  - **Evidenced — DE1:** Capacity reports require the union of usage `(daemon, feature)` and capacity `(vendor, feature)` identities that can affect the range, followed by exact-pair lookups. This makes capacity-only identities visible and usage-only identities missing as the source behavior requires. Evidence: Target State / “Capacity reports”; D-6 / Decision; D-11 / Decision; `sqlite/reports.go`.
  - **Evidenced — J5, TE2:** The license command handlers need a narrow package-local workflow seam to prove explicit close-before-output/database behavior without mutable globals or a new production abstraction. Evidence: Target State / “The command contract becomes”; D-8 / Decision; `cmd/goflexlmdb/main.go`.
  - **Evidenced — TE3, TE4:** Tests need concrete expectations for the vendor/coverage state matrix and atomic rollback of canonical snapshots, projections, and pool state. Evidence: D-7 / Decision; D-10 / Decision; Change Units / Unit 2.
  - **Evidenced — DE3:** Cancellation should be checked immediately before and after the writer lock; a context-aware mutex is not justified. Evidence: D-10 / Decision; `sqlite/store.go` / `Store.writer`. The context-aware-lock alternative is deferred as YAGNI.
  - **Evidenced — TE5:** A bespoke static test for the absence of removed Go symbols is not needed; command rejection, documentation/demo coverage, and the normal build prove the supported cutover. Evidence: S-8 through S-10; D-1 / Decision. A future downstream compatibility suite reopens it.
- **Open Questions raised:** None. The report-identity, initialization, cancellation, and test-seam details were settled by source decisions plus current code evidence during aggregation.
- **Spec-maturity tags:** `plan-level`: 8 settled; `spec-level`: 0; `T#-contradiction`: not applicable because no technical-notes artifact exists. The spec-maturity gate did not trip.
- **Contract pinning:** Y. The source plan concretely pins the license grammar and JSON projection, import request, persisted tables, capacity state, report identity/outcome, and CLI/status behavior. This run adds the report identity-union and initialization sequence as implementation decisions, not new behavior.
- **Resolution source:** All R1 implementation details were settled by evidence: the source plan’s Target State and D-1 through D-11, corroborated by the current SQLite and command code. No junior-developer reframing or user input was needed.
- **Decisions produced:** D-1 through D-7; see [implementation-decision-log.md](implementation-decision-log.md).
- **Changed in plan:** Outcome; Constraints and Boundaries; Implementation Approach; Work Units and Sequencing; Definition of Done; Testing Strategy; Risks and Assumptions; Cut for Scope; Deferred (YAGNI); Sources and Plan Records; Recommendation.
- **Next-step recommendation:** Go to synthesis.

## Unaudited evidence classes

- Production license files and deployed version-1 databases were unavailable to every specialist. They do not block the build because the source plan pins deterministic importer behavior and names representative-fixture triggers that reopen it.
