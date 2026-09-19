# Current State Findings: FlexLM License Import

## Provenance

These findings were produced by this run's discovery round. A structural analyst inspected module boundaries, coupling, dependency direction, and abstractions. A behavioral analyst inspected data flow, validation, error propagation, persistence, and observable command behavior. The run also inspected repository conventions, documentation, recent Git history, and the working tree. The scoped area was the current entitlement import, storage, reporting, CLI, tests, and documentation named in `artifacts/scope-boundary.md`.

## Project Context

- **Stack:** Go 1.27. The root parser is a pull-based streaming Go library and command. The in-progress analytics area adds `database/sql` with `modernc.org/sqlite` and a second command at `cmd/goflexlmdb`.
- **Conventions source:** Root `AGENTS.md`; no `CLAUDE.md` or `project-discovery.md` was found.
- **ADRs found:** None under `docs/adr/`; no ADR directory exists.
- **Coding standards found:** Root `AGENTS.md` is the only coding-standard source found.
- **Recent churn:** Git history in the last 90 days names only `README.md` in the scoped paths. The SQLite analytics and entitlement implementation is present as uncommitted working-tree content rather than committed precedent.

## Gaps

- No existing FlexLM license-file parser, grammar, license document type, sample license files, or parser fixtures were found.
- No repository evidence settles continuation syntax, quoting, vendor-specific extensions, permanent expirations, FEATURE versus INCREMENT aggregation, or the meaning of issue/effective dates.
- No mapping table or alias mechanism connects license-file vendor/feature identities to lmgrd daemon/feature identities.
- No license-file import provenance or JSON license-document command exists.
- No ADR records the intended entitlement model, mapping key, database migration policy, or command compatibility decision.
- Production license files and deployed version-1 databases were not available to inspect. Findings that depend on their exact shapes remain unverified and are listed below.

## Findings

### C-1: The exported entitlement model is a derived capacity point

- **Claim:** The current public model contains only an effective time and license count; it cannot preserve SERVER, VENDOR, FEATURE, INCREMENT, version, expiration, or metadata.
- **Location:** `sqlite/types.go`, `Entitlement`; `sqlite/entitlements.go`, `Store.ReplaceEntitlements`.
- **Evidence:**

  ```go
  // Entitlement is a capacity change effective at EffectiveFrom.
  type Entitlement struct {
      EffectiveFrom time.Time `json:"effective_from"`
      Licenses      int       `json:"licenses"`
  }
  ```

  ```go
  func (s *Store) ReplaceEntitlements(ctx context.Context, pool, feature string, entitlements []Entitlement) error
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified.
- **Bears on:** S-1, S-9; D-1, D-2.

### C-2: The command parses strict CSV at single-feature granularity

- **Claim:** `goflexlmdb entitlements replace` requires the operator to name one pool and one feature, then accepts exactly two CSV columns; a multi-record FlexLM license file does not fit this command boundary.
- **Location:** `cmd/goflexlmdb/main.go`, `usage`, `runEntitlements`, and `readEntitlements`.
- **Evidence:**

  ```go
  fmt.Fprintln(w, "       goflexlmdb entitlements replace --db DB --pool POOL --feature FEATURE FILE.csv")
  ```

  ```go
  if len(header) != 2 || header[0] != "effective_from" || header[1] != "licenses" {
      return nil, errors.New("entitlement CSV must have exact columns effective_from,licenses")
  }
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified.
- **Bears on:** S-2, S-7, S-8; D-1, D-8.

### C-3: Parse failure precedes storage and replacement is atomic

- **Claim:** The command finishes parsing before opening the database, and the store replaces a feature timeline under one writer lock and transaction. Invalid input cannot partially replace the existing timeline.
- **Location:** `cmd/goflexlmdb/main.go`, `runEntitlements`; `sqlite/entitlements.go`, `Store.ReplaceEntitlements`.
- **Evidence:**

  ```go
  entitlements, err := readEntitlements(reader)
  if err != nil {
      failure(stderr, err)
      return 1
  }
  db, err := store.Open(context.Background(), *dbPath, store.OpenOptions{})
  ```

  ```go
  s.writer.Lock()
  defer s.writer.Unlock()
  tx, err := s.db.BeginTx(ctx, nil)
  ```
- **Raised by:** Behavioral analyst.
- **Confidence:** Verified.
- **Bears on:** S-3, S-10; D-9, D-10.

### C-4: Entitlement persistence is flattened and has no source provenance

- **Claim:** The database stores only pool, feature, time, and count. It cannot audit a source license file, preserve raw records and metadata, distinguish FEATURE from INCREMENT, or deduplicate a license source.
- **Location:** `sqlite/schema.go`, `entitlements` table.
- **Evidence:**

  ```sql
  CREATE TABLE IF NOT EXISTS entitlements (
    id INTEGER PRIMARY KEY,
    pool_id INTEGER NOT NULL REFERENCES license_pools(id),
    feature TEXT NOT NULL,
    effective_ns INTEGER NOT NULL,
    licenses INTEGER NOT NULL CHECK(licenses >= 0),
    UNIQUE(pool_id, feature, effective_ns)
  );
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified.
- **Bears on:** S-4; D-5.

### C-5: Reports consume a narrow derived capacity timeline

- **Claim:** Capacity math is isolated from entitlement input syntax. It needs sorted effective-time/count points and can remain stable if richer license records derive that projection.
- **Location:** `sqlite/reports.go`, `Store.Capacity`, `entitlementPoints`, `splitAtEntitlements`, and `entitlementAt`.
- **Evidence:**

  ```go
  points, err := s.entitlementPoints(ctx, q.poolID, feature, q.to.UnixNano())
  segments = splitAtEntitlements(segments, points)
  purchased, covered := entitlementAt(points, segment[0])
  ```

  ```sql
  SELECT effective_ns, licenses FROM entitlements
          WHERE pool_id=? AND feature=? AND effective_ns < ? ORDER BY effective_ns
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified.
- **Bears on:** S-5; D-6, D-10.

### C-6: Usage identity is richer than capacity identity

- **Claim:** Session derivation distinguishes stream, daemon, and feature, but capacity joins only by pool and feature. The repository does not define how a license-file VENDOR maps to a log daemon or how same-named features from different vendors remain separate.
- **Location:** `sqlite/derive.go`, session partition key; `sqlite/reports.go`, `usageIntervals`; `sqlite/schema.go`, `entitlements`.
- **Evidence:**

  ```go
  key := fmt.Sprintf("%d\x00%s\x00%s\x00%s", event.stream, event.daemon, event.feature, typeName)
  ```

  ```sql
  FROM sessions WHERE pool_id=? AND feature=? AND session_type='usage'
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified for current behavior; the intended target mapping is unverified.
- **Bears on:** S-5, S-11; D-6, D-11.

### C-7: Pools and streams are assigned by the operator

- **Claim:** Log import connects activity to a pool through the caller's `Pool` and `Stream` values. SERVER records currently have no role in pool or stream identity.
- **Location:** `sqlite/types.go`, `ImportRequest`; `sqlite/import.go`, `Store.Import` and `ensurePoolStream`.
- **Evidence:**

  ```go
  type ImportRequest struct {
      Reader       io.Reader
      Pool         string
      Stream       string
      SourceName   string
      OnDiagnostic func(goflexlm.Diagnostic) error
  }
  ```

  ```go
  pool, stream, err := ensurePoolStream(ctx, tx, request.Pool, request.Stream)
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified.
- **Bears on:** D-6.

### C-8: Replacement scope and license aggregation rules are not modeled

- **Claim:** The store deletes one pool/feature timeline, then inserts strictly chronological nonnegative points. It has no semantics for several files in one pool, overlapping records, expirations, permanent licenses, or additive FEATURE and INCREMENT records.
- **Location:** `sqlite/entitlements.go`, `Store.ReplaceEntitlements`.
- **Evidence:**

  ```go
  if index > 0 && !entitlement.EffectiveFrom.After(entitlements[index-1].EffectiveFrom) {
      return fmt.Errorf("replace entitlements: rows must be strictly chronological")
  }
  ```

  ```sql
  DELETE FROM entitlements WHERE pool_id = ? AND feature = ?
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified for current behavior; FlexLM aggregation semantics are unverified.
- **Bears on:** D-4, D-6, D-10.

### C-9: A schema change requires a new migration path

- **Claim:** The store can initialize schema version 1 but cannot upgrade an existing nonzero schema. Rich license imports require a versioned 1-to-2 migration and an explicit policy for old CSV-derived rows.
- **Location:** `sqlite/schema.go`, `schemaVersion`; `sqlite/store.go`, `Store.migrate`.
- **Evidence:**

  ```go
  const (
      schemaVersion = 1
  )
  ```

  ```go
  if version < schemaVersion {
      if version != 0 {
          return fmt.Errorf("migrate sqlite: no migration path from version %d", version)
      }
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified.
- **Bears on:** S-4; D-5.

### C-10: JSON is not yet a license-document contract

- **Claim:** JSON support exists for reports, and `Entitlement` happens to carry JSON tags, but there is no parsed license-document JSON shape or license JSON command.
- **Location:** `cmd/goflexlmdb/main.go`, `runReport`; `sqlite/types.go`, `Entitlement`.
- **Evidence:**

  ```go
  if *jsonOutput {
      encoder := json.NewEncoder(stdout)
      encoder.SetEscapeHTML(false)
      if err := encoder.Encode(report); err != nil {
  ```
- **Raised by:** Structural analyst and behavioral analyst.
- **Confidence:** Verified.
- **Bears on:** S-2; D-8.

### C-11: Tests and documentation encode the CSV compatibility surface

- **Claim:** Tests directly exercise strict CSV parsing and exported `ReplaceEntitlements`; README and demo instructions present CSV replacement as supported behavior. No license-file parser fixtures or syntax tests exist.
- **Location:** `cmd/goflexlmdb/main_test.go`, `TestReadEntitlementsRequiresExactHeader`; `sqlite/store_test.go`, `TestImportDeriveAndReports`; `README.md`; `testdata/sqlite-demo/README.md`.
- **Evidence:**

  ```go
  func TestReadEntitlementsRequiresExactHeader(t *testing.T) {
  ```

  ```go
  if err := store.ReplaceEntitlements(ctx, "engineering", "editor", []Entitlement{{EffectiveFrom: from, Licenses: 3}}); err != nil {
  ```

  ```console
  $ goflexlmdb entitlements replace --db usage.db --pool engineering --feature editor entitlements.csv
  ```
- **Raised by:** Structural analyst, behavioral analyst, and this run's documentation sweep.
- **Confidence:** Verified.
- **Bears on:** S-8, S-9, S-10; Change Units.

### C-12: The scoped analytics implementation is uncommitted working-tree content

- **Claim:** The plan is based on user-owned, in-progress files: `sqlite/`, `cmd/goflexlmdb/`, `DESIGN.md`, and `testdata/` are untracked, while `README.md` and `go.mod` are modified relative to `HEAD`.
- **Location:** Repository working tree reported by `git status --short`.
- **Evidence:**

  ```text
   M README.md
   M go.mod
  ?? DESIGN.md
  ?? cmd/goflexlmdb/
  ?? go.sum
  ?? sqlite/
  ?? testdata/
  ```
- **Raised by:** This run's repository sweep.
- **Confidence:** Verified.
- **Bears on:** S-4; D-5.

### C-13: The design note does not define the requested mapping

- **Claim:** `DESIGN.md` expresses an aspiration to parse license files into a Product/Feature mapping and mentions AI-generated parsers, but it defines neither the Product model nor mapping rules and supplies no evidence that an AI subsystem is required for this change.
- **Location:** `DESIGN.md`.
- **Evidence:**

  ```text
  Write a program that uses structured outputs, examples, tests, and AI to write programs for parsing flexlm license files into the Product/Feature mapping. Build the prompt into the program.
  ```
- **Raised by:** Structural analyst and this run's documentation sweep.
- **Confidence:** Verified.
- **Bears on:** Cut for Scope.

## Findings No Agent Could Audit

- Exact vendor-specific extensions could not be audited because the repository contains no representative license files. The plan limits support to its pinned common grammar and fails on unknown directives; real fixtures can expand that boundary later.
- FEATURE versus INCREMENT aggregation, version separation, expiration semantics, and issue/effective-date semantics could not be audited from current code. Planning closed these questions through D-3, D-4, and D-6, using official Revenera documentation where it supplied the rule.
- Whether deployed schema-version-1 databases contain entitlement data worth preserving could not be audited. The operator closed this question by confirming that there are no users or production data and approving a clean rebuild in D-5.
- Whether SERVER identity should validate or automatically bind log streams could not be audited because current behavior uses only operator-provided pool and stream names. D-6 keeps that behavior and defers server inference.
