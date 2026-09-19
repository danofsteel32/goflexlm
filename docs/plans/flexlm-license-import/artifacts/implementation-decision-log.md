# Implementation Decision Log: FlexLM License Import

This log records implementation decisions for the plan in [feature-implementation-plan.md](../feature-implementation-plan.md). Source behavior and its rationale remain in [change-plan.md](../change-plan.md); round evidence is in [implementation-iteration-history.md](implementation-iteration-history.md).

## Trivial decisions

None.

## Full decisions

### D-1: Clean v2 initialization and v1 rejection

- **Question:** How should the store open a new or legacy database during the CSV-to-license cutover?
- **Decision:** Use one `Open` sequence: under initialization, create only schema-version-2 tables for a new database; when the recorded version is 1, return an actionable error directing the operator to delete and recreate the database, without running legacy DDL or a migration.
- **Rationale:** The boundary authorizes a clean break, and v1 rows cannot faithfully supply vendor, source-document, or resolved-date data.
- **Evidence:** [scope boundary](scope-boundary.md); [change plan D-5](change-decision-log.md#d-5-clean-schema-and-snapshot-storage); R1 J1 and DE2; `sqlite/schema.go` and `sqlite/store.go`.
- **Rejected alternatives:**
  - Migrate or retain v1 data — rejected because required source fields would be invented and no deployed database is in scope.
  - Run old DDL before rejecting v1 — rejected because failure must not alter a legacy database.
- **Specialist owner:** Store implementer.
- **Revisit criterion:** A deployed v1 database or external consumer requiring preserved data is supplied.
- **Dissent (if any):** None.
- **Driven by rounds:** R1.
- **Dependent decisions:** D-3, D-6, D-7.
- **Referenced in plan:** Implementation Approach; Work Units and Sequencing; Definition of Done.

### D-2: Parser and store conformance boundary

- **Question:** How can parser output and the public store boundary agree without assuming all callers use the parser?
- **Decision:** Maintain one corpus of license documents that parser acceptance and store document validation both accept. Test invalid caller-constructed `LicenseFile` values directly against `Store.ImportLicenseFile`; validation occurs before the writer lock and transaction.
- **Rationale:** Exported concrete structs are constructible by callers, while duplicated uncorrelated tests can let parser and store rules diverge.
- **Evidence:** [change plan D-3](change-decision-log.md#d-3-license-parser-grammar-and-failure-contract) and [D-9](change-decision-log.md#d-9-public-document-validation); R1 J3 and TE1; root `AGENTS.md` atomicity rules.
- **Rejected alternatives:**
  - Trust parser-only values — rejected because callers can construct public structs.
  - Make the document opaque or introduce factories — rejected because it expands the API and obstructs JSON/library use.
- **Specialist owner:** Parser and store implementer.
- **Revisit criterion:** The public document representation becomes construction-safe by design.
- **Dissent (if any):** None.
- **Driven by rounds:** R1.
- **Dependent decisions:** D-3, D-6.
- **Referenced in plan:** Implementation Approach; Work Units and Sequencing; Definition of Done.

### D-3: Projection-first delivery order

- **Question:** In what order should storage and reporting change so projection semantics are independently proved?
- **Decision:** Build and test the full-pool projection with a three-snapshot corpus before wiring reports to it. The corpus must establish known zero from omissions, late-introduced identities, expiration, source provenance, and same-instant replacement.
- **Rationale:** Report behavior cannot establish whether a source/projection bug or lookup bug caused a missing result. The projection is the foundation for report semantics.
- **Evidence:** [change plan D-10](change-decision-log.md#d-10-atomic-authoritative-rebuild); R1 J4; `sqlite/reports.go` consumes capacity change points.
- **Rejected alternatives:**
  - Rewire reports while deriving projection — rejected because failures become cross-surface and ambiguous.
  - Test only one snapshot — rejected because it cannot prove authoritative replacement or zero carry-forward.
- **Specialist owner:** Store implementer.
- **Revisit criterion:** A stable projection contract already exists independently of reports.
- **Dissent (if any):** None.
- **Driven by rounds:** R1.
- **Dependent decisions:** D-4, D-6.
- **Referenced in plan:** Implementation Approach; Work Units and Sequencing; Definition of Done.

### D-4: Report identity union and exact-pair lookup

- **Question:** Which identities must a report represent when capacity and usage differ?
- **Decision:** For the requested range, form the union of usage `(daemon, feature)` and capacity `(vendor, feature)` identities, then look up capacity only by the exact vendor/daemon-and-feature pair. Emit capacity-only rows and usage-only missing rows; never let a sibling vendor's finite or uncounted capacity mask another identity.
- **Rationale:** Vendor is part of the shared observable identity. A feature-only row would merge independent capacity states and misstate coverage.
- **Evidence:** [change plan D-6](change-decision-log.md#d-6-capacity-projection-and-usage-identity), [D-7](change-decision-log.md#d-7-uncounted-report-contract), and [D-11](change-decision-log.md#d-11-vendor-visible-capacity-output); R1 DE1; `sqlite/reports.go`.
- **Rejected alternatives:**
  - Feature-only capacity lookup — rejected because same-named vendor features can cover neither other's usage.
  - Let uncounted dominate a combined feature row — rejected because it hides sibling missing or finite breaches.
- **Specialist owner:** Report implementer.
- **Revisit criterion:** A higher-level cross-vendor summary has a separately specified, truthful aggregation contract.
- **Dissent (if any):** None.
- **Driven by rounds:** R1.
- **Dependent decisions:** D-6.
- **Referenced in plan:** Implementation Approach; Work Units and Sequencing; Definition of Done.

### D-5: Package-local CLI workflow seam

- **Question:** How will command tests prove close-before-output and parse-before-database-open behavior?
- **Decision:** Add a narrow package-local command workflow seam that tests can use to provide a failing closer and observe database opening. It must not become an exported interface, a mutable global, or a general production abstraction.
- **Rationale:** These ordering failures are otherwise hard to force deterministically, but the command has no second production implementation that justifies a broad interface.
- **Evidence:** [change plan D-8](change-decision-log.md#d-8-license-cli-contract); R1 J5 and TE2; `cmd/goflexlmdb/main.go`.
- **Rejected alternatives:**
  - Test only successful command paths — rejected because it cannot prove close failures do not leak JSON or open the database.
  - Add a public command dependency interface — rejected because it creates unsupported API solely for a test seam.
- **Specialist owner:** CLI implementer.
- **Revisit criterion:** A second real command-host implementation needs the same workflow boundary.
- **Dissent (if any):** None.
- **Driven by rounds:** R1.
- **Dependent decisions:** —.
- **Referenced in plan:** Implementation Approach; Work Units and Sequencing; Testing Strategy.

### D-6: State-matrix and atomic rollback tests

- **Question:** Which tests must protect the interaction between vendor coverage and atomic imports?
- **Decision:** Use a table-driven vendor/coverage state matrix covering finite, uncounted, missing, capacity-only, and usage-only identities. Assert that any error or cancellation rolls back the canonical snapshot, its entire projection, and pool creation/replacement together.
- **Rationale:** The most damaging regressions cross multiple derived states or leave data half-mutated; isolated happy paths do not establish those guarantees.
- **Evidence:** [change plan D-7](change-decision-log.md#d-7-uncounted-report-contract) and [D-10](change-decision-log.md#d-10-atomic-authoritative-rebuild); R1 TE3 and TE4.
- **Rejected alternatives:**
  - Test report states individually — rejected because sibling vendors can mask each other only in combinations.
  - Roll back only the projection — rejected because a surviving canonical snapshot or pool changes later behavior.
- **Specialist owner:** Test and store implementer.
- **Revisit criterion:** The import lifecycle is split into independently committed operations.
- **Dissent (if any):** None.
- **Driven by rounds:** R1.
- **Dependent decisions:** —.
- **Referenced in plan:** Work Units and Sequencing; Definition of Done; Testing Strategy.

### D-7: Cancellation checkpoints with existing writer lock

- **Question:** How should import honor cancellation while serializing snapshot rebuilds?
- **Decision:** Check `ctx.Err()` immediately before taking `Store.writer` and immediately after acquiring it, then perform the transaction using existing context-aware database operations. Do not make the mutex context-aware.
- **Rationale:** The two checks make cancellation observable around the only unavoidable non-context-aware wait without adding a new locking primitive or unsafe cancellation behavior.
- **Evidence:** [change plan D-10](change-decision-log.md#d-10-atomic-authoritative-rebuild); R1 DE3; `sqlite/store.go` / `Store.writer`.
- **Rejected alternatives:**
  - Add a context-aware mutex — rejected because no evidence justifies a new concurrency abstraction.
  - Check context only before locking — rejected because cancellation can happen while waiting for the writer.
- **Specialist owner:** Store implementer.
- **Revisit criterion:** Measured lock contention materially delays cancelled imports and a safe replacement protocol is designed.
- **Dissent (if any):** None.
- **Driven by rounds:** R1.
- **Dependent decisions:** —.
- **Referenced in plan:** Implementation Approach; Deferred (YAGNI).
