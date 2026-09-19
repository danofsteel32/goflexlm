# Change Plan: FlexLM License Import

## Why This Change

This change replaces the manually authored CSV entitlement time series with FlexLM license files as the source of truth. This replacement addresses the reported friction with the current workflow. The operator asked for a parser and import structure based on `SERVER`, `VENDOR`, `FEATURE`, and `INCREMENT` records so purchased licenses can be mapped to lmgrd usage. The existing CSV workflow is replaced rather than retained ([D-1](artifacts/change-decision-log.md#d-1-license-files-replace-csv-entitlements)).

## What Changes, In One Paragraph

The root library gains a concrete FlexLM license-file document model and parser. SQLite imports a parsed license file as a dated pool snapshot, preserves its supported records and metadata, and derives vendor-plus-feature capacity changes for reports. The database command parses license files to JSON and imports them directly; the CSV entitlement command and API disappear. Existing log parsing stays unchanged, while capacity reports add explicit vendor and uncounted fields and return separate vendor-plus-feature rows.

## Current State

The exported entitlement model is only an effective timestamp and count, and the store replaces one caller-named pool/feature timeline at a time ([C-1](artifacts/current-state-findings.md#c-1-the-exported-entitlement-model-is-a-derived-capacity-point)). The database command owns strict two-column CSV parsing and exposes that one-feature granularity in its flags ([C-2](artifacts/current-state-findings.md#c-2-the-command-parses-strict-csv-at-single-feature-granularity)). SQLite flattens capacity to pool, feature, time, and count without license-source provenance ([C-4](artifacts/current-state-findings.md#c-4-entitlement-persistence-is-flattened-and-has-no-source-provenance)).

Reports already consume a narrow effective-time capacity projection, so their interval math need not understand license syntax ([C-5](artifacts/current-state-findings.md#c-5-reports-consume-a-narrow-derived-capacity-timeline)). Session derivation retains daemon identity, but capacity lookup currently drops it and joins only by pool and feature ([C-6](artifacts/current-state-findings.md#c-6-usage-identity-is-richer-than-capacity-identity)). The analytics implementation is uncommitted working-tree content, and there are no users or production databases to migrate ([C-12](artifacts/current-state-findings.md#c-12-the-scoped-analytics-implementation-is-uncommitted-working-tree-content)).

## Target State

The root `goflexlm` package owns syntax only. `license.go` defines `LicenseFile`, `LicenseServer`, `LicenseVendor`, `LicenseFeature`, `LicenseFeatureKind`, and `LicenseAttribute`; `license_parser.go` provides `ParseLicenseFile(io.Reader) (LicenseFile, error)`. No parser type depends on SQLite or report concepts, and no single-implementation parser interface is introduced ([D-2](artifacts/change-decision-log.md#d-2-parser-and-storage-boundary)).

The public document contract is:

```go
type LicenseFile struct {
    Servers   []LicenseServer  `json:"servers"`
    Vendors   []LicenseVendor  `json:"vendors"`
    Features  []LicenseFeature `json:"features"`
    UseServer bool             `json:"use_server"`
}

type LicenseServer struct {
    Line       int                `json:"line"`
    Host       string             `json:"host"`
    HostID     string             `json:"host_id"`
    Port       *int               `json:"port,omitempty"`
    Attributes []LicenseAttribute `json:"attributes,omitempty"`
}

type LicenseVendor struct {
    Line       int                `json:"line"`
    Name       string             `json:"name"`
    DaemonPath string             `json:"daemon_path,omitempty"`
    Attributes []LicenseAttribute `json:"attributes,omitempty"`
}

type LicenseFeatureKind string

const (
    LicenseFeatureLine   LicenseFeatureKind = "FEATURE"
    LicenseIncrementLine LicenseFeatureKind = "INCREMENT"
)

type LicenseFeature struct {
    Line       int                `json:"line"`
    Kind       LicenseFeatureKind `json:"kind"`
    Name       string             `json:"name"`
    Vendor     string             `json:"vendor"`
    Version    string             `json:"version"`
    Expiration string             `json:"expiration"`
    Licenses   *int               `json:"licenses"`
    Uncounted  bool               `json:"uncounted"`
    Attributes []LicenseAttribute `json:"attributes,omitempty"`
}

type LicenseAttribute struct {
    Name     string `json:"name"`
    Value    string `json:"value,omitempty"`
    HasValue bool   `json:"has_value"`
}

func ParseLicenseFile(r io.Reader) (LicenseFile, error)
```

`Attributes` is ordered and permits repeated names, except that a feature record may contain at most one case-insensitive `START`. This keeps unknown metadata and bare flags intact through JSON conversion. A numeric zero and the token `uncounted` both produce `Licenses == nil` and `Uncounted == true`; positive counts representable by Go `int` produce a non-nil `Licenses` and `Uncounted == false`. A larger decimal is a line error rather than an architecture-dependent wrap.

The parser accepts LF, CRLF, a final line without a newline, double-quoted values, and an unquoted trailing backslash continuation without an arbitrary line limit. Directive names and `permanent`/`uncounted` are ASCII case-insensitive. `LicenseFeature.Kind` is normalized to the uppercase public constants. Attribute names and all vendor-provided values retain their spelling, while semantic lookup of `START` is ASCII case-insensitive.

Blank lines and full lines whose first non-whitespace byte is `#` are ignored. Inline comments are not recognized: `#` is ordinary token content on every logical line, continued or not, and succeeds or fails only according to the surrounding directive grammar. One `USE_SERVER` sets `UseServer`; a duplicate or an argument on that directive is a line error. The boolean preserves semantic presence, not the directive's source line or position. `PACKAGE`, `UPGRADE`, and unknown directives fail the document because silently ignoring them could understate capacity. Malformed input or a reader error returns no partial document and an operation-qualified line error ([D-3](artifacts/change-decision-log.md#d-3-license-parser-grammar-and-failure-contract)).

The compact grammar is:

```text
logical-line  = physical-line { unquoted-trailing-"\\" physical-line }
token         = bare-token | '"' quoted-value '"'
attribute     = name | name "=" (bare-value | '"' quoted-value '"')
SERVER        = "SERVER" host hostid [decimal-port] {attribute}
VENDOR        = "VENDOR" name [daemon-path] [["OPTIONS="] options-path] [["PORT="] decimal-port] {vendor-attribute}
vendor-attribute = name "=" (bare-value | '"' quoted-value '"')
FEATURE       = ("FEATURE" | "INCREMENT") name vendor version expiration count {attribute}
USE_SERVER    = "USE_SERVER"
expiration    = "permanent" | d[d]-3-letter-month-yyyy | d[d]-3-letter-month-zero-year
count         = positive-decimal | "0" | "uncounted"
```

Whitespace separates tokens only outside double quotes. Quotes do not nest and contain no escape syntax. Continuation removes the trailing backslash and joins the next physical line with one separating space. A record's exported `Line` is the first physical line of its logical record. A token-specific lexical or semantic error reports that token's physical line. A missing-token or whole-record arity error reports the logical record's first line, as does EOF after a continuation.

SERVER preserves every syntactically valid trailing bare or key/value attribute; there is no metadata allowlist. FEATURE/INCREMENT attributes use bare `NAME`, `NAME=value`, or `NAME="value with spaces"` forms.

VENDOR parsing has one positional phase followed by an attribute phase. Before the first `NAME=value` token, the first bare token is the daemon path, the second is the options path, and the third must be the decimal port. Once a key/value token appears, every remaining token must be a key/value attribute. VENDOR does not accept generic bare flags. Omitting a positional slot requires `OPTIONS=` or `PORT=` for the later value. Positional options and port values become ordered `OPTIONS` and `PORT` attributes with `HasValue == true`; unknown `NAME=value` metadata is preserved.

SERVER ports are 1–65535. VENDOR `PORT` and `EPORT` values are 0–65535, where zero requests an ephemeral port. Counts are decimal and nonnegative before the uncounted normalization.

A finite date accepts a one- or two-digit day, an ASCII case-insensitive three-letter English month, and a four-digit year other than 1900. Year spellings `0`, `00`, `000`, `0000`, and `1900` are accepted as non-expiring forms. The original `Expiration` text is preserved. Parsed semantic dates are normalized only when resolved by import.

A conforming parse and JSON projection is:

```text
SERVER lic01 001122aabbcc 27000 PRIMARY_IS_MASTER
VENDOR acme /opt/acme OPTIONS=/etc/acme.opt PORT=27001
USE_SERVER
INCREMENT editor acme 2026.0 31-dec-2026 12 \
  VENDOR_STRING="team a" SIGN=0123ABCD
```

```json
{"servers":[{"line":1,"host":"lic01","host_id":"001122aabbcc","port":27000,"attributes":[{"name":"PRIMARY_IS_MASTER","has_value":false}]}],"vendors":[{"line":2,"name":"acme","daemon_path":"/opt/acme","attributes":[{"name":"OPTIONS","value":"/etc/acme.opt","has_value":true},{"name":"PORT","value":"27001","has_value":true}]}],"features":[{"line":4,"kind":"INCREMENT","name":"editor","vendor":"acme","version":"2026.0","expiration":"31-dec-2026","licenses":12,"uncounted":false,"attributes":[{"name":"VENDOR_STRING","value":"team a","has_value":true},{"name":"SIGN","value":"0123ABCD","has_value":true}]}],"use_server":true}
```

The database layer owns operational meaning. Its public import contract is:

```go
type LicenseImportRequest struct {
    File          goflexlm.LicenseFile
    Pool          string
    SourceName    string
    EffectiveFrom time.Time
    Timezone      string
}

func (s *Store) ImportLicenseFile(ctx context.Context, request LicenseImportRequest) error
```

The caller parses first, so syntax failure occurs before storage. `EffectiveFrom` is required. It gives the instant when the whole file became the authoritative snapshot for the pool.

`Timezone` must be exactly `UTC` or contain `/` and be accepted by `time.LoadLocation`. `Local`, blank, fixed-zone abbreviations, and unknown names are rejected. `START` accepts only a finite date and activates a record at local midnight, but never before the snapshot; `permanent` and every zero-year spelling are invalid for `START`. A finite expiration stops the record at local midnight at the start of the printed expiration date. `permanent` and the pinned zero-year forms do not expire.

Each contributor is active on `[max(snapshot start, START), min(next snapshot, expiration))`, omitting either optional bound when it does not exist. An empty interval contributes nothing and is not an import error, including a record already expired at the snapshot or one whose `START` is equal to or after its expiration.

Import resolves and persists the resulting UTC start/expiration instants. Later rebuilds therefore do not depend on changed timezone data ([D-4](artifacts/change-decision-log.md#d-4-snapshot-and-date-semantics)).

`ImportLicenseFile` normalizes nil top-level slices to non-nil empty slices. It then validates the complete public value before taking the writer lock or opening a transaction.

It rejects these request-level states:

- A blank pool or source.
- A zero effective time.
- A timezone outside the executable rule above.

It rejects these document states:

- Non-positive source lines or feature `Line` values that are not strictly increasing in slice order.
- Blank required positional fields or an invalid `LicenseFeatureKind`.
- SERVER ports outside 1–65535, or VENDOR `PORT` and `EPORT` values outside 0–65535.
- Blank attribute names, a bare attribute with a non-empty `Value`, or any bare VENDOR attribute. Bare attributes remain valid on SERVER and FEATURE/INCREMENT records.
- An invalid, repeated, or non-finite case-insensitive `START`, or an invalid expiration.
A feature count must be exactly one of these states:

- `Uncounted == true` with `Licenses == nil`.
- `Uncounted == false` with a positive `Licenses` value.

`ParseLicenseFile` enforces every document invariant above that does not depend on request fields. Required positional fields and attribute names must be nonblank; `""` is valid only as an attribute value. The parse command therefore never emits a document that import would reject for document content.

`EffectiveFrom` and every resolved date must preserve the same instant after conversion through `time.Unix(0, value.UnixNano()).UTC()`. Comparison uses instant equality after UTC normalization. It ignores the original location and monotonic-clock metadata. Out-of-range instants are rejected rather than wrapped or clamped.

Capacity aggregation uses checked addition. It rejects a total that exceeds Go `int` or SQLite's signed 64-bit integer range before database mutation. The error identifies the vendor-plus-feature identity and source line that caused the overflow.

Errors are prefixed `import license file:` and include source lines for record dates. No validation error mutates the database ([D-9](artifacts/change-decision-log.md#d-9-public-document-validation)).

Schema version 2 is a clean initialization schema, not a migration. Opening a version-1 database returns an actionable error telling the operator to delete and recreate it.

`license_imports` stores one canonical `LicenseFile` JSON document per `(pool_id, effective_ns)`. That document preserves SERVER, VENDOR, FEATURE, and INCREMENT records, their source lines and ordered attributes, plus the semantic presence of `USE_SERVER`. Resolved date boundaries are stored beside the canonical document so historical rebuilds are deterministic.

Importing another document at the same pool and effective instant atomically replaces that snapshot. Import creates the named pool inside the same transaction when it does not exist, so it can be the first operation on a clean database. Inserting an earlier or later snapshot rebuilds derived capacity for the pool ([D-5](artifacts/change-decision-log.md#d-5-clean-schema-and-snapshot-storage)).

A document with no feature records is a valid authoritative empty snapshot: it ends every previously known identity at its effective instant. This destructive meaning is explicit; blank, comment-only, and SERVER/VENDOR-only documents all have the same import effect.

The source-storage contract is:

```sql
CREATE TABLE license_imports (
  id INTEGER PRIMARY KEY,
  pool_id INTEGER NOT NULL REFERENCES license_pools(id),
  source_name TEXT NOT NULL,
  effective_ns INTEGER NOT NULL,
  timezone TEXT NOT NULL,
  document_json TEXT NOT NULL,
  resolved_dates_json TEXT NOT NULL,
  UNIQUE(pool_id, effective_ns)
);
```

`document_json` is compact `encoding/json.Marshal` output for the normalized, validated public struct, with standard escaping and no trailing newline. Its top-level `servers`, `vendors`, and `features` values are always arrays, including when empty; empty `attributes` fields are omitted. `licenses parse` writes those same bytes plus one newline. `resolved_dates_json` is a compact JSON array aligned by feature ordinal with `{"start_ns": integer|null, "expiration_ns": integer|null}` entries. It is internal rebuild data, while `document_json` remains the auditable source model.

The persisted capacity projection is:

```sql
CREATE TABLE capacity_changes (
  pool_id INTEGER NOT NULL REFERENCES license_pools(id),
  vendor TEXT NOT NULL,
  feature TEXT NOT NULL,
  effective_ns INTEGER NOT NULL,
  licenses INTEGER,
  uncounted INTEGER NOT NULL CHECK(uncounted IN (0,1)),
  source_import_id INTEGER NOT NULL REFERENCES license_imports(id) ON DELETE CASCADE,
  PRIMARY KEY(pool_id, vendor, feature, effective_ns),
  CHECK((uncounted=1 AND licenses IS NULL) OR
        (uncounted=0 AND licenses IS NOT NULL AND licenses>=0))
);

CREATE INDEX capacity_changes_source_import_idx
  ON capacity_changes(source_import_id);
```

Within each snapshot, feature slice order is authoritative. The first counted or uncounted `FEATURE` for a vendor-plus-feature identity contributes capacity. Later `FEATURE` records for that identity do not. Every `INCREMENT` contributes. Active positive counts sum with checked addition. If any active contributing record is uncounted, the projection is uncounted until that record expires. These are the importer's explicit folding rules; they follow the cited vendor documentation but do not claim to emulate every hidden FlexNet pool rule.

Pooling attributes and versions remain preserved metadata. They do not split report capacity because current lmgrd activity does not carry a reliable license-pool or version key. Exact `(pool, daemon/vendor, feature)` equality maps sessions to capacity. No alias configuration or SERVER-to-pool inference is added ([D-6](artifacts/change-decision-log.md#d-6-capacity-projection-and-usage-identity)).

Snapshot import and projection use one lifecycle:

1. Validate and resolve dates.
2. Take the store writer lock.
3. Begin one transaction.
4. Replace the `(pool, effective_ns)` canonical snapshot row.
5. Delete that pool's `capacity_changes`.
6. Read all its snapshots in time order.
7. Rebuild the full projection before committing.

Each snapshot is authoritative on `[effective_ns, next effective_ns)`. Rebuild first computes the identity universe from every validated feature record in every stored snapshot for the pool, whether or not that record ever has a non-empty active interval. At each snapshot boundary it evaluates every identity in that universe; absence is known zero. This emits zero at an earlier authoritative empty snapshot even when the identity first appears in a later snapshot, while activity-only identities absent from all license snapshots remain missing.

The projection is a change-point timeline. It emits the first value for every identity at the pool's earliest snapshot, then emits a later boundary row only when the value changes. A zero at the first omitting snapshot therefore carries through consecutive omissions without redundant rows. `source_import_id` names the snapshot that established the current value, not necessarily the latest authoritative document; the document authoritative at any instant is found independently from `license_imports`.

Within a snapshot interval, the rebuild writes zero at expiration when no contributor remains. It coalesces all changes at one instant and suppresses redundant non-boundary rows. Any error or cancellation rolls the source row and projection back together. Zero is known finite capacity, not missing capacity. `source_import_id` is snapshot provenance, not contributor-level lineage ([D-10](artifacts/change-decision-log.md#d-10-atomic-authoritative-rebuild)).

Capacity reports use one row/bucket per exact vendor-plus-feature identity. Both JSON and table output order them by `(vendor, feature, bucket start)`. `CapacityBucket` adds `Vendor string \`json:"vendor"\`` and `Uncounted bool \`json:"uncounted"\``. Table output adds a leading VENDOR column.

An uncounted segment has `Purchased == nil` and `Uncounted == true`. It retains usage, peak, and used-seat-time measures; both saturation durations are zero, and every unused-seat and headroom field is nil because those measures require a finite capacity. It contributes neither missing nor above-entitlement quality.

A missing sibling vendor remains missing and is not masked by an uncounted row. It increments `MissingEntitlement` for each affected vendor/feature/time segment. `UncoveredNanoseconds` adds each missing vendor-plus-feature segment, so overlapping identities can produce a total longer than the report's wall-clock span.

All report duration and seat-time subtraction, multiplication, and accumulation use checked signed 64-bit arithmetic. Overflow returns an operation-qualified capacity-report error rather than wrapping or saturating.

A finite sibling breach still sets the report-level `UsageAboveEntitlement` OR-flag. `AnalyticsQuery.Feature` continues to filter the feature across all vendors; no vendor filter is added ([D-7](artifacts/change-decision-log.md#d-7-uncounted-report-contract)) ([D-11](artifacts/change-decision-log.md#d-11-vendor-visible-capacity-output)).

The command contract becomes:

```text
goflexlmdb licenses parse [FILE|-]
goflexlmdb licenses import --db DB --pool POOL --effective-from RFC3339 --timezone AREA/LOCATION FILE
```

`licenses parse` defaults to standard input when no path is supplied. For a file path, parsing and a successful input close both finish before any output is written; a close failure returns status 1 without emitting JSON. Standard input has a no-op close. The command then writes one `LicenseFile` JSON object plus a newline. `licenses import` accepts exactly one file, with `-` for standard input, and requires an explicit timezone, including explicit `UTC`.

The CLI passes the path argument exactly as supplied as `SourceName`, including `-` for standard input. No source-name flag is added. Successful import is silent, and the store method returns only `error`.

Usage and flag errors return 2 and print usage to standard error. Input open/read/close, parse, output, or database failures return 1. They write only to standard error with the `goflexlmdb:` prefix and wrap parser line errors with the source path. Success returns 0.

Import parsing and input close complete before the database is opened. The top-level usage lists both `licenses` subcommands. A missing/unknown subcommand, more than one parse path, or anything other than one import path is a usage error. The existing `goflexlm` command and debug-log `Decoder` remain unchanged ([D-8](artifacts/change-decision-log.md#d-8-license-cli-contract)).

## Surface Delta

### S-1: License document types and `ParseLicenseFile` — Added

**Target state.** The root `goflexlm` package exports the concrete license document types and `ParseLicenseFile(io.Reader) (LicenseFile, error)` contract shown in Target State. It parses the supported FlexLM syntax without depending on SQLite.

**Behavior.** Preserving. Existing log-parser inputs and callers are unchanged; this adds a separate public entry point requested by the operator.

**Why.** The current model cannot represent the required license records or metadata ([C-1](artifacts/current-state-findings.md#c-1-the-exported-entitlement-model-is-a-derived-capacity-point)).

**Decision.** [D-2](artifacts/change-decision-log.md#d-2-parser-and-storage-boundary)

### S-2: `goflexlmdb licenses parse` — Added

**Target state.** `goflexlmdb licenses parse [FILE|-]` writes the pinned `LicenseFile` JSON object and follows the pinned status contract.

**Behavior.** Changing. A previously unknown command now accepts license input and emits the requested JSON intermediate. The operator explicitly requested JSON as an intermediate format.

**Why.** JSON does not exist as a license-document contract today ([C-10](artifacts/current-state-findings.md#c-10-json-is-not-yet-a-license-document-contract)).

**Depends on.** S-1.

**Decision.** [D-8](artifacts/change-decision-log.md#d-8-license-cli-contract)

### S-3: `LicenseImportRequest` and `Store.ImportLicenseFile` — Added

**Target state.** The `sqlite` package exposes the concrete parsed-document import contract shown in Target State and atomically replaces one dated pool snapshot.

**Behavior.** Preserving. Existing log import remains unchanged; this adds the requested direct license-file storage path.

**Why.** Multi-feature license documents require a document-scoped transaction rather than repeated feature replacements ([C-3](artifacts/current-state-findings.md#c-3-parse-failure-precedes-storage-and-replacement-is-atomic)).

**Depends on.** S-1.

**Decision.** [D-4](artifacts/change-decision-log.md#d-4-snapshot-and-date-semantics) and [D-9](artifacts/change-decision-log.md#d-9-public-document-validation)

### S-4: SQLite license and capacity schema — Re-scoped

**Target state.** Schema version 2 stores each dated validated `LicenseFile` as canonical JSON, stores resolved date-boundary instants, and stores vendor-aware derived capacity changes. Version-1 databases are rejected with recreate instructions; no legacy entitlement table or migration exists.

**Behavior.** Changing. Existing in-progress database contents are discarded rather than migrated. The operator decided: “There are no users yet and no production data. Wipe everything and start from a clean slate.”

**Why.** The current schema cannot preserve license sources or vendor identity, and faithful conversion of CSV rows is impossible ([C-4](artifacts/current-state-findings.md#c-4-entitlement-persistence-is-flattened-and-has-no-source-provenance)).

**Depends on.** S-3.

**Decision.** [D-5](artifacts/change-decision-log.md#d-5-clean-schema-and-snapshot-storage) and [D-10](artifacts/change-decision-log.md#d-10-atomic-authoritative-rebuild)

### S-5: Capacity derivation and report lookup — Re-scoped

**Target state.** SQLite derives time-ordered capacity changes from FEATURE and INCREMENT records, joins sessions by exact pool-plus-daemon/vendor-plus-feature identity, and emits capacity buckets separately for each vendor-plus-feature identity.

**Behavior.** Changing. Capacity comes from parsed FlexLM records and respects vendor identity rather than a manually supplied pool/feature series. The operator requested direct mapping from license files to lmgrd usage.

**Why.** Current sessions retain daemon identity but entitlement lookup drops it ([C-6](artifacts/current-state-findings.md#c-6-usage-identity-is-richer-than-capacity-identity)).

**Depends on.** S-4.

**Decision.** [D-6](artifacts/change-decision-log.md#d-6-capacity-projection-and-usage-identity) and [D-10](artifacts/change-decision-log.md#d-10-atomic-authoritative-rebuild)

### S-6: `CapacityBucket.Uncounted` — Added

**Target state.** `CapacityBucket` exposes `Uncounted bool \`json:"uncounted"\``. `true` means capacity is known but has no finite seat limit; `false` with `Purchased == nil` means no matching license was found.

**Behavior.** Changing. JSON gains `uncounted`, table output may show `uncounted`, and uncounted usage is no longer eligible for over-capacity warnings. The operator approved: “Yeah that works.”

**Why.** FlexLM's `0` and `uncounted` values cannot truthfully map to zero purchased seats or missing entitlement.

**Depends on.** S-5.

**Decision.** [D-7](artifacts/change-decision-log.md#d-7-uncounted-report-contract)

### S-11: `CapacityBucket.Vendor` — Added

**Target state.** `CapacityBucket` exposes `Vendor string \`json:"vendor"\`` and table output adds a leading VENDOR column. Reports return separate rows for same-named features served by different vendors; `AnalyticsQuery.Feature` still filters across vendors.

**Behavior.** Changing. JSON and tables gain vendor identity, and one feature row may become several. The operator approved separate vendor-plus-feature report rows: “Yes.”

**Why.** Combining vendor states can hide uncovered usage or imply that one vendor's seats satisfy another vendor's checkouts ([C-6](artifacts/current-state-findings.md#c-6-usage-identity-is-richer-than-capacity-identity)).

**Depends on.** S-5.

**Decision.** [D-11](artifacts/change-decision-log.md#d-11-vendor-visible-capacity-output)

### S-7: `goflexlmdb licenses import` — Added

**Target state.** `goflexlmdb licenses import` requires an explicit effective time and IANA timezone, parses and closes one license file, then opens the database and atomically stores the dated pool snapshot through `Store.ImportLicenseFile`; success is silent.

**Behavior.** Changing. A previously unknown command now imports license files. The operator explicitly requested direct license-file import; existing debug-log import remains unchanged.

**Why.** The command needs a multi-feature document boundary instead of a caller-named single feature ([C-2](artifacts/current-state-findings.md#c-2-the-command-parses-strict-csv-at-single-feature-granularity)).

**Depends on.** S-1, S-3, S-4, and S-5.

**Decision.** [D-8](artifacts/change-decision-log.md#d-8-license-cli-contract)

### S-8: `goflexlmdb entitlements replace` — Removed

**Target state.** The CSV entitlement command does not exist. Direct license-file import through `goflexlmdb licenses import` owns capacity ingestion.

**Behavior.** Changing. Existing CSV command invocations fail as usage errors. The operator directed: “Replace it in favor of just parsing FlexLM licenses directly.”

**Why.** Retaining both inputs would preserve the structure this change replaces and create two competing capacity sources.

**Depends on.** S-7.

**Migration.** Delete the schema-version-1 development database, recreate it by importing retained logs, then replace `goflexlmdb entitlements replace --db usage.db --pool engineering --feature editor entitlements.csv` with `goflexlmdb licenses import --db usage.db --pool engineering --effective-from 2026-01-01T00:00:00Z --timezone UTC licenses.lic` for each complete dated snapshot.

**Decision.** [D-1](artifacts/change-decision-log.md#d-1-license-files-replace-csv-entitlements)

### S-9: `Entitlement` — Removed

**Target state.** The exported `sqlite.Entitlement` type does not exist. `goflexlm.LicenseFile` is the source model, and SQLite's capacity-change representation is internal.

**Behavior.** Changing. Go callers using `Entitlement` stop compiling. The operator directed replacement of the CSV entitlement workflow.

**Why.** The type is a derived point that cannot represent the required license document ([C-1](artifacts/current-state-findings.md#c-1-the-exported-entitlement-model-is-a-derived-capacity-point)).

**Depends on.** S-3 and S-5.

**Migration.** Parse a license with `goflexlm.ParseLicenseFile` and pass it in `sqlite.LicenseImportRequest.File`.

**Decision.** [D-1](artifacts/change-decision-log.md#d-1-license-files-replace-csv-entitlements)

### S-10: `Store.ReplaceEntitlements` — Removed

**Target state.** `Store.ReplaceEntitlements` does not exist. `Store.ImportLicenseFile` owns atomic dated snapshot replacement and capacity derivation.

**Behavior.** Changing. Go callers using `ReplaceEntitlements` stop compiling. The operator directed replacement of the CSV entitlement workflow.

**Why.** Repeating a single-feature method cannot atomically import a multi-feature license document ([C-3](artifacts/current-state-findings.md#c-3-parse-failure-precedes-storage-and-replacement-is-atomic)).

**Depends on.** S-3 and S-5.

**Migration.** Call `Store.ImportLicenseFile` once with the complete parsed license snapshot.

**Decision.** [D-1](artifacts/change-decision-log.md#d-1-license-files-replace-csv-entitlements)

## Behavior Changes

- **Existing development databases are not upgraded.** Opening schema version 1 fails with instructions to delete and recreate it. The operator approved wiping all data because there are no users or production data (S-4).
- **Capacity is derived from vendor-aware FlexLM records.** Reports use exact daemon/vendor and feature matches rather than manual CSV points. This is the direct license-to-lmgrd mapping the operator requested (S-5).
- **Uncounted capacity is visible.** JSON adds `uncounted`; table output shows `uncounted`; usage does not trigger over-capacity warnings. The operator explicitly approved this contract (S-6).
- **Vendor identity is visible in capacity reports.** JSON and tables add `vendor`, and same-named features from different vendors use separate rows. The operator explicitly approved this contract (S-11).
- **Two new license commands are accepted.** `licenses parse` emits the requested JSON intermediate and `licenses import` stores a dated snapshot; these are additions requested by the operator (S-2 and S-7).
- **The CSV CLI and exported entitlement API disappear.** Callers use the license parser and snapshot import instead. The operator explicitly chose replacement rather than coexistence (S-8 through S-10).

## Change Units

### Unit 1: Add the license document parser

**What it does.** Add the public model, tokenizer, continuation handling, supported record parsers, JSON tags, fatal error contract, and parser tests without changing existing log behavior.

**Delta entries.** S-1 and the `LicenseFile` JSON-shape prerequisite for S-2; command wiring lands in Unit 2.

**Justification.** The request explicitly asks for a FlexLM license-file parser supporting the named record types and metadata.

**How you know it worked.** Table tests cover the pinned grammar and worked JSON example, including:

- All four records, `USE_SERVER`, full-line comments, and directive/token case normalization.
- Ordered/repeated attributes, case-insensitive START lookup with preserved spelling, and VENDOR positional normalization.
- Numeric-zero/`uncounted`, every non-expiring year spelling, one/two-digit finite dates, quoted values, and continuations.
- LF/CRLF/final lines and normalized empty arrays.
- Malformed quotes/continuations/ports/counts/dates, inline `#` under ordinary token rules, duplicate `USE_SERVER`, unsupported directives, and reader failures with physical-line attribution.
- Long physical/logical lines and many records.

`FuzzParseLicenseFile` must terminate and never panic. It must return either a fully valid document or an error and produce deterministic JSON for successful inputs. Parser working memory beyond the returned document remains proportional to the longest logical line. All existing decoder tests remain green.

### Unit 2: Replace entitlement storage, reports, commands, and guidance

**What it does.** Land the clean version-2 schema, validated snapshot import, atomic capacity rebuild, vendor-aware reports, uncounted/vendor output fields, both `licenses` commands, CSV/API removal, README changes, and demo fixture changes together. This avoids an intermediate repository whose schema, reports, command, and documentation disagree.

**Delta entries.** S-2 through S-11.

**Ordering constraint.** Unit 1 must land first because the store accepts `goflexlm.LicenseFile`.

**Justification.** Persisting parsed license data, mapping it to usage, removing the replaced CSV workflow, and documenting the new public contract are inseparable parts of the requested cutover.

**How you know it worked.** Verification covers these areas:

- Validation tables cover every rejected caller-constructed state, including a bare VENDOR attribute, strictly increasing feature lines, nil-slice normalization, finite-only `START`, first/last signed-int64 nanosecond instants and one outside each side, non-UTC and monotonic-bearing `time.Time` values, plus one valid manual document before mutation.
- Store tests cover first-import pool creation; same-instant replacement; unchanged values and consecutive omissions under change-point provenance; earlier/later out-of-order snapshots; removal zeros; authoritative empty snapshots; empty/reversed contributor intervals and zero-valued first appearances; coalesced START/expiration/snapshot boundaries; permanent, counted, and uncounted records; first-FEATURE/additive-INCREMENT behavior; checked two- and three-contributor overflow; canonical JSON and resolved-date round-trip; exact file/`-` source provenance; reopen/rebuild across a DST-sensitive zone; context cancellation; and transaction rollback after snapshot replacement and during projection insertion using test-only SQLite triggers.
- A three-snapshot oracle proves empty-to-feature known zero, replacement-to-empty behavior, consecutive omissions, and older-snapshot insertion under the pool-wide identity universe.
- CLI tests cover file/stdin rules, usage status 2, failure status 1, path-qualified errors, close-before-output and close-before-database ordering, output/database failures, silent success, parse-before-open with no database file creation, deterministic `(vendor, feature, bucket start)` ordering, finite+missing/uncounted+finite-breach/uncounted+missing quality, checked report overflow for extreme ranges and large quantities, exact uncounted derived metrics, and old-command rejection.
- Golden tests preserve unaffected log import, rebuild, denial, queue, and root `goflexlm` output/status behavior.
- Every README/demo command runs against a synthetic license fixture, and no CSV reference remains.

## Risks

- **Capacity can be overstated if the same feature name spans versions that cannot share licenses.** Debug logs do not reliably expose the license-pool/version identity, so the plan aggregates only at the observable vendor-plus-feature key. Synthetic multi-version tests make this limitation explicit.
- **Date-only boundaries can shift reports by a day if timezone handling drifts.** Require an explicit canonical timezone, persist resolved UTC instants, and test DST and month/year transitions. An expiration of `01-jan-2027` in `America/New_York` stops capacity at `2027-01-01T05:00:00Z`; the license is not covered during January 1. This importer policy follows Revenera's published start-of-date rule. The evidence is a single publisher source, so a representative vendor fixture that behaves differently reopens D-4.
- **A partially understood directive could silently undercount capacity.** Imports fail on `PACKAGE`, `UPGRADE`, and unknown directives; the failure is detectable before database mutation.
- **Vendor-name spelling can leave valid usage uncovered.** Exact matching is deliberate and detectable through existing missing-entitlement quality; aliases remain deferred until real data proves they are needed.
- **Cutover touches public API, CLI, JSON, schema, and documentation together.** Unit 2 keeps these coupled so the repository never reports from a half-switched data source or advertises a removed command.

## Deferred (YAGNI)

### JSON as an import source
**Why deferred:** The request asks for JSON as an intermediate output, and no second authoritative input path is required.
**Reopen when:** A concrete caller needs to persist and later re-import parsed JSON without the original license file.
**Source:** Software architect proposal review.

### Vendor and feature aliases
**Why deferred:** No current fixture or user report shows a license vendor/feature spelling that differs from lmgrd output; exact matching is simpler and missing mappings are already visible.
**Reopen when:** A real license/log pair fails exact matching.
**Source:** C-6 and software architect proposal.

### PACKAGE and UPGRADE semantics
**Why deferred:** The recorded request names SERVER, VENDOR, FEATURE, and INCREMENT. Silently ignoring other capacity-bearing directives is unsafe, but implementing their semantics has no current example or requirement.
**Reopen when:** A target license file contains PACKAGE or UPGRADE and its capacity must be reported.
**Source:** Official FlexNet grammar review and junior-developer reframing.

### Signature validation and server discovery
**Why deferred:** Capacity mapping needs parsed metadata, not cryptographic verification, daemon executable inspection, DNS, or license-server network access.
**Reopen when:** The application must establish authenticity or discover live server topology rather than analyze supplied files.
**Source:** YAGNI sweep of the software architect proposal.

### Import wall-clock storage
**Why deferred:** Effective time, source name, canonical document JSON, and source-import linkage satisfy the current provenance and rebuild needs; no view or retention rule consumes ingestion time.
**Reopen when:** An audit view or retention policy needs the ingestion instant separately from the snapshot effective time.
**Source:** Junior-developer YAGNI review.

## Cut for Scope

### AI-generated parser programs

This would add a prompt-driven subsystem that writes parser programs. It was cut because the operator asked for a FlexLM parser and import architecture, while `DESIGN.md` is an uncommitted aspiration rather than the work item's scope authority ([C-13](artifacts/current-state-findings.md#c-13-the-design-note-does-not-define-the-requested-mapping)). The operator can reinstate it, and that direction would become its justification.

## Open Items

None block implementation. The parser, public data shapes, CLI, storage schema, matching key, date rules, failure behavior, replacement behavior, and report representation are pinned. FEATURE/INCREMENT folding, zero-as-uncounted normalization, and start-of-date expiration remain explicit importer policies backed by the cited publisher documentation; a representative license/tool fixture that contradicts any policy reopens the corresponding decision.

## Review Findings

The review team was `junior-developer`, `data-engineer`, `test-engineer`, `user-experience-designer`, and `risk-analyst`. Three rounds reached the large-plan cap. Their findings changed the plan in these material ways:

- Replaced ambiguous feature-only folding with operator-approved vendor-plus-feature report rows ([D-11](artifacts/change-decision-log.md#d-11-vendor-visible-capacity-output)).
- Replaced normalized SERVER/VENDOR/FEATURE tables with canonical document JSON plus the derived capacity table, removing schema no current query needed ([D-5](artifacts/change-decision-log.md#d-5-clean-schema-and-snapshot-storage)).
- Merged storage, report, CLI, removal, and documentation cutover into one working unit rather than leaving a broken intermediate schema.
- Pinned caller-built document validation, canonical JSON, timezone/range checks, full-line comments, exact date/case rules, and path provenance ([D-3](artifacts/change-decision-log.md#d-3-license-parser-grammar-and-failure-contract)) ([D-9](artifacts/change-decision-log.md#d-9-public-document-validation)).
- Pinned authoritative boundary rows, termination zeros, transaction order, rollback tests, and snapshot provenance ([D-10](artifacts/change-decision-log.md#d-10-atomic-authoritative-rebuild)).
- Expanded verification to cover fault-injected rollback, out-of-order snapshots, deterministic report ordering/quality, compatibility goldens, and explicit fuzz invariants.

No review finding remains open. The earlier risk review treated official Revenera documentation as closing expiration inclusivity. This review narrows that claim: the behavior is pinned and does not block implementation, but it remains an explicit single-publisher-backed importer policy with the reopening trigger recorded under Open Items.

## Review History

- **Review mode:** Team review for a large, cross-cutting plan.
- **Rounds completed:** 3 — see [review-iteration-history.md](artifacts/review-iteration-history.md).
- **Team composition:** Junior developer for hidden prerequisites and plain-language buildability; adversarial validator for falsification; evidence-based investigator for repository claims and cross-artifact consistency; edge-case explorer for parser and failure boundaries; data engineer for schema, snapshot, and report semantics.
- **Findings raised:** 27 — see [review-findings.md](artifacts/review-findings.md). Twenty-two were resolved by evidence, four by re-reframing, and one was a minor evidence-wording correction. None required user input or deferral.
- **YAGNI candidates:** 1 — repeated zero boundary rows were replaced with the simpler change-point projection.
- **Assumptions challenged across all passes:** Date activation, parser/store parity, line attribution, authoritative empty snapshots, identity establishment, provenance, time/count bounds, report arithmetic, and external FlexNet semantics.
- **Consolidations made:** Snapshot projection now uses one pool-wide identity universe and a change-point timeline instead of adjacent unions and redundant boundary rows.
- **Ambiguities resolved:** The review pinned VENDOR token precedence, record-specific attributes, contributor intervals, empty snapshots, time equality, close ordering, uncounted metrics, and additive report-quality behavior.
- **Open items remaining:** 0 blocking. Contradictory representative FlexNet fixtures reopen the explicitly labeled importer policies under Open Items.
- **Cross-reference verification:** Could not verify. The required checker exited with status 141 and produced no diagnostic output; contract-pinning verification passed.
