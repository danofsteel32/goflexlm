# Investigation: Why Cadence license file parsing fails

The parser needs two small grammar extensions before it can read the supplied Cadence license file.

## Summary: approve two grammar extensions

- **Root Cause:** The parser does not recognize legacy `DAEMON` records, and its lexer rejects quoted values before following an in-quote continuation (E1, E2, E3).
- **Approval Decision:** Parse `DAEMON` through the existing `VENDOR` branch and carry quoted-token state across continued physical lines.
- **Why Correct:** The existing vendor model already represents `DAEMON`, and the original logical-line lexer accepted quoted continuations before commit `842cd59` changed line handling (E4, E5, E8).
- **Validation Outcome:** Two adversarial reviews confirmed both root causes and required tighter folding, error-line, delimiter, and regression-test rules (V1-V7).
- **Remaining Risks:** The selected fold matches the supplied Cadence file, but no authoritative source settles whether indentation can be meaningful in every quoted continuation (V3).

## Problem Statement: two valid forms fail at separate syntax gates

The supplied Cadence license file fails first on a `DAEMON` record with `unsupported directive "DAEMON"`. Changing that keyword to `VENDOR` exposes a second failure: `unterminated quote` on the first physical line of a continued `SIGN2` value.

`ParseLicenseFile` should accept the historical `DAEMON` spelling and FlexNet's backslash continuation inside a quoted attribute. It must still return a complete document or a zero document with a physical-line-qualified error.

The change must not alter exported types, JSON field names, SQLite schema, capacity calculations, or output for inputs already accepted.

## Root Cause Analysis: dispatch and lexing fail independently

### Two independent syntax gates reject the file

Directive dispatch recognizes `VENDOR` but not its legacy synonym `DAEMON`, so the first failure occurs before vendor fields are parsed (E1, E4). The existing `LicenseVendor` type already holds every field in the supplied record, so no model change is needed (E5).

After the directive spelling is changed, the lexer opens `SIGN2="...` and searches only the current physical line for its closing quote. It reports `unterminated quote` before the outer parser can consume the trailing backslash and read the next physical line (E2, E3).

Commit `842cd59` introduced this behavior. It replaced logical-line assembly with per-physical-line lexing to retain precise error lines (E8).

### The fix can preserve public and persisted contracts

Both directive spellings can normalize into the existing `Vendors` array. The continued `SIGN2` can remain an ordinary ordered `LicenseAttribute`.

SQLite validates and persists those existing shapes. It does not use vendor declaration records or signatures in capacity arithmetic (E6, E7). The intended compatibility change is that previously rejected valid input can now contribute its feature capacity.

## Planned Fix: reuse vendor parsing and retain quote state

### Accept both legacy directives and continued quoted values

Reuse the vendor parser for `DAEMON`. Then make the lexer retain an unfinished quoted token across physical lines while preserving current error attribution and memory bounds.

### Changes

#### `license_parser.go`

- **Change:** Add `DAEMON` to the existing `VENDOR` dispatch case. Carry quote, token, attribute, and source-line state across a continued physical line.
- **Evidence:** E1, E2, E3, E4, E5, E8.
- **Standards:** Keep the whole-document failure contract, physical-line errors, longest-logical-line memory bound, and existing exported model.
- **Details:** Recognize a terminal continuation marker while a quote is open. Preserve quoted bytes before the marker. Remove the marker, line ending, and leading horizontal indentation from the next physical line without inserting whitespace. Retain both the token's opening line and the current physical line so later record errors and immediate lexical errors keep their proper locations. Emit the token only after its closing quote. Then require whitespace or end-of-line before later attributes. An open quote without a terminal continuation remains an immediate `unterminated quote` error.

#### `license.go`

- **Change:** Update the exported `LicenseVendor` comment to cover `VENDOR` and legacy `DAEMON` records.
- **Evidence:** E4, E5, V2.
- **Standards:** Keep Go documentation aligned with expanded supported behavior.
- **Details:** Do not add a directive-kind field; source spelling remains intentionally normalized.

#### `license_test.go`

- **Change:** Add exported-API regression cases for `DAEMON` and lowercase `daemon`, plus synthetic Cadence-shaped multiline `SIGN2` and `VENDOR_STRING` values.
- **Evidence:** E1, E3, E5, E9.
- **Standards:** Test successful parsing and nearby malformed input through `ParseLicenseFile`.
- **Details:** Assert vendor normalization, feature start line, attribute order, the exact folded quoted values, and the valued `V7.1_LK` attribute following the `SIGN2` closing quote. Preserve the existing `VENDOR` test rather than duplicating it.

#### `license_conformance_test.go`

- **Change:** Add LF and CRLF quoted-continuation cases, malformed boundary cases, reader-failure and long-token cases, and a quoted-continuation fuzz seed.
- **Evidence:** E3, E9.
- **Standards:** Preserve deterministic fuzzing, zero-document failures, final-line handling, and physical-line error checks.
- **Details:** Cover an uncontinued open quote, end of input after a continuation marker, several continued lines without a closing quote, reader failure while a quote is open, a long multiline quoted token, and successful closure on a final line without a newline. Pin the current physical line for lexical errors. Assert that whitespace after a closing quote permits another token. Adjacent text and an adjacent continuation marker must remain invalid.

#### `README.md`

- **Change:** Name `DAEMON` as a supported legacy alias and clarify that quoted values may span backslash continuations.
- **Evidence:** E4, E10.
- **Standards:** Update the project introduction when supported input changes.

#### `docs/license-files.md`

- **Change:** Add `DAEMON` to the syntax table and replace the same-physical-line quote restriction with the continued-quote rule.
- **Evidence:** E4, E9, E10.
- **Standards:** Keep the detailed parser guide aligned with accepted grammar.

## Evidence Summary: code, history, and source-format support

### E1: Directive dispatch omits `DAEMON`

- **Trust class:** Codebase.
- **Source:** `license_parser.go:150`, `license_parser.go:179`, `license_parser.go:260`
- **Finding:**

  ```go
  switch licenserules.Upper(t[0].text) {
  // ...
  case "VENDOR":
  // ...
  default:
      return licenseError(start, "unsupported directive %q", t[0].text)
  }
  ```

- **Relevance:** `DAEMON` always reaches the unsupported-directive branch.

### E2: Continuation is recognized only outside quoted text

- **Trust class:** Codebase.
- **Source:** `license_parser.go:84`, `license_parser.go:90`, `license_parser.go:96`
- **Finding:**

  ```go
  if s[i] == '\\' && strings.TrimSpace(s[i+1:]) == "" {
      return tokens, true, nil
  }
  // ...
  if s[i] == '"' {
      // scans only for another quote in s
  }
  ```

- **Relevance:** A backslash inside an open quoted value cannot request the next physical line.

### E3: The quote error fires before another physical line is read

- **Trust class:** Codebase.
- **Source:** `license_parser.go:30`, `license_parser.go:45`, `license_parser.go:50`, `license_parser.go:105`
- **Finding:**

  ```go
  part, continued, lexErr := lexLicenseLine(raw, physical)
  if lexErr != nil {
      return LicenseFile{}, lexErr
  }
  // the next line is read only when continued is true
  ```

  Reproduction after changing only `DAEMON` to `VENDOR`:

  ```text
  goflexlmdb: parse -: parse license file: line 8: unterminated quote
  exit status 1
  ```

- **Relevance:** The `SIGN2` token never reaches its second physical line.

### E4: `DAEMON` is the legacy spelling of `VENDOR`

- **Trust class:** Web, independently corroborated.
- **Sources:** [IBM-hosted FLEXlm FAQ](https://public.dhe.ibm.com/software/rational/tools/flexlm/flexlm.7.0f.docs/flexfaq/chap5.htm), [Intel licensing guide](https://docs.altera.com/r/docs/683472/25.3/altera-fpga-software-installation-and-licensing/server-vendor-and-use_server-lines), [Flexera community answer](https://community.revenera.com/s/question/0D5PL00000NwtMi0AJ/what-is-the-difference-between-vendor-and-daemon-lines-in-a-license-file)
- **Finding:** Older FlexLM files use `DAEMON`; later versions permit the equivalent `VENDOR` spelling.
- **Relevance:** Accepting both words follows the source format rather than inventing a Cadence-only exception.

### E5: The existing vendor model fits the legacy record

- **Trust class:** Codebase.
- **Source:** `license.go:20`, `license_parser.go:183`
- **Finding:**

  ```go
  type LicenseVendor struct {
      Line       int
      Name       string
      DaemonPath string
      Attributes []LicenseAttribute
  }
  ```

  With `VENDOR`, the supplied fields normalize without loss to the daemon name, daemon path, `OPTIONS`, and `port=27001`.
- **Relevance:** No new exported type or JSON field is needed for `DAEMON`.

### E6: SQLite accepts and preserves the existing shapes

- **Trust class:** Codebase.
- **Source:** `sqlite/license_validation.go:38`, `sqlite/license_validation.go:65`, `sqlite/license_import.go:27`
- **Finding:** Vendor records and ordinary feature attributes are validated as existing values, then the document is stored as JSON.
- **Relevance:** Normalizing the alias and retaining `SIGN2` do not require a schema change.

### E7: Capacity ignores vendor records and signature metadata

- **Trust class:** Codebase.
- **Source:** `sqlite/license_capacity.go:117`
- **Finding:** Capacity derives from feature vendor, feature name, count, start, and expiration. It does not read vendor declarations or general signature attributes.
- **Relevance:** The proposed parser change cannot alter capacity arithmetic for the supplied license.

### E8: Physical-line lexing introduced the quoted-continuation regression

- **Trust class:** Codebase history.
- **Source:** commit `842cd59`, prior implementation in commit `7f1ed26`
- **Finding:** The original parser joined continued physical fragments before tokenizing. Commit `842cd59` lexed each physical line separately to preserve precise error attribution.
- **Relevance:** The fix must recover logical quoted continuations without losing the newer physical-line errors.

### E9: Boundary whitespace has no current contract

- **Trust class:** Provided evidence plus an unresolved general case.
- **Source:** Supplied Cadence input, `docs/license-files.md:48`, `license_test.go:51`
- **Finding:** The supplied file places semantic spaces before continuation markers and uses tabs only to indent following physical lines. Current docs ban the form, and tests cover continuation only between complete tokens. No authoritative source establishes whether leading indentation can ever be meaningful quoted data.
- **Relevance:** The implementation will preserve bytes before the marker and remove continuation syntax plus indentation without inserting bytes. This is a new parser contract. Revisit it if a real license or FlexNet tool demonstrates meaningful leading whitespace on a continued fragment.

### E10: Current documentation excludes both forms

- **Trust class:** Codebase.
- **Source:** `README.md:54`, `docs/license-files.md:36`, `docs/license-files.md:48`
- **Finding:** The public syntax lists `VENDOR` but not `DAEMON`, and the guide requires quotes to close on the same physical line.
- **Relevance:** Both documents must change with the accepted grammar.

### E11: FlexNet permits continuation inside a quoted value

- **Trust class:** Web and provided, independently corroborated.
- **Sources:** Historical FLEXlm End User Guide quoted `NOTICE` example; supplied Cadence `SIGN2` and `VENDOR_STRING` records.
- **Finding:** The guide shows a quoted `NOTICE` split with a terminal backslash before its closing quote, matching the construction used by the supplied file.
- **Relevance:** Continued quote state is part of the source grammar, not a Cadence-only exception.

## Validation Results: two reviews confirmed the fix and tightened its boundaries

### Counter-Evidence Investigated

#### V1: The two claimed failure paths match the code and reproductions

- **Hypothesis:** `DAEMON` dispatch and in-quote continuation are the independent root causes.
- **Investigation:** Both validators traced directive dispatch, quote scanning, and the outer read loop. They reproduced the first failure. They also reproduced the second after changing only the directive spelling.
- **Result:** Confirmed.
- **Impact:** Keep the root-cause analysis and the two-part fix.

#### V2: Alias normalization does not require a new public or stored shape

- **Hypothesis:** `DAEMON` can use `LicenseVendor` without API, JSON, schema, validation, or capacity-code changes.
- **Investigation:** The validators traced the existing vendor fields through validation, JSON persistence, and capacity projection.
- **Result:** Confirmed, with a documentation correction.
- **Impact:** Add `license.go` to the plan and document intentional loss of the original directive spelling.

#### V3: Historical behavior does not prove a single-space fold

- **Hypothesis:** The pre-regression parser establishes the exact desired whitespace.
- **Investigation:** The old parser removed the backslash, added one separator, and retained surrounding spaces and indentation. The supplied file and an official quoted-continuation example support preserving pre-marker spaces while dropping formatting indentation. They do not settle every possible value.
- **Result:** Partially refuted.
- **Impact:** Specify a byte-level rule, describe it as a new contract, and record the remaining general-case risk.

#### V4: One line number is insufficient for multiline tokens

- **Hypothesis:** Retaining only a token source line preserves all current error attribution.
- **Investigation:** Completed-token validation uses the token's opening line, while immediate lexer failures use the physical line currently being scanned.
- **Result:** Refuted.
- **Impact:** Retain both token-opening and current physical lines, then pin malformed multiline errors in tests.

#### V5: Closing-quote delimiter handling can regress

- **Hypothesis:** Emitting a token on its closing quote preserves the existing boundary grammar automatically.
- **Investigation:** Current code rejects adjacent text and an adjacent backslash after a closing quote; a loose state transition could accept them.
- **Result:** Refuted.
- **Impact:** Preserve the whitespace-or-end delimiter rule and test success and failure neighbors.

#### V6: The original test plan mislabeled and under-covered the fixture

- **Hypothesis:** `V7.1_LK=...` is bare and the proposed boundary suite is sufficient.
- **Investigation:** The lexer marks `V7.1_LK=...` as a valued attribute. Reviewers also found several uncovered paths: final-no-newline, reader-error, long-token, and multiline `VENDOR_STRING`.
- **Result:** Refuted.
- **Impact:** Correct the expected attribute shape and add the missing focused cases. Do not add redundant CLI or SQLite tests.

#### V7: Unrelated working-tree edits can be preserved

- **Hypothesis:** Planned documentation changes conflict with the user's existing dashboard work.
- **Investigation:** The existing `README.md` edit is in a later dashboard section. The parser documentation lines are separate. Other modified and untracked files are outside the plan.
- **Result:** Confirmed as non-blocking.
- **Impact:** Make a narrow README edit and leave all dashboard, web-documentation, and database files untouched.

### Adjustments Made After Review

- Added `license.go` after V2 found its exported comment incomplete.
- Replaced the unsupported history-based single-space claim with an explicit byte-level fold after V3.
- Required separate token-opening and current error lines after V4.
- Pinned post-quote delimiter behavior after V5.
- Corrected `V7.1_LK` to a valued attribute and expanded focused boundary coverage after V6.
- Removed any need for CLI, SQLite, schema, or capacity tests after both reviews found existing downstream coverage sufficient.

### Confidence Assessment: root causes are high confidence; folding has one open risk

- **Confidence:** High for both root causes and the `DAEMON` normalization; medium-high for the selected quoted-continuation fold.
- **Remaining Risks:** A quoted continuation with intentionally meaningful leading indentation may need a different fold. No supplied file, repository test, or authoritative example demonstrates that case.

## Coding Standards Reference: contracts the fix must preserve

| Standard | Source | Applies To |
| --- | --- | --- |
| Preserve complete-document parsing, physical line numbers, and logical-line memory bounds | `AGENTS.md`, License-file parser invariants | Lexer and parser changes |
| Preserve exported API, JSON, CLI status, and SQLite contracts | `AGENTS.md`, Public compatibility and SQLite invariants | Alias normalization and downstream behavior |
| Add regression coverage for parser bugs and nearby malformed input | `AGENTS.md`, Testing expectations | Parser and conformance tests |
| Update README and detailed guide when supported input changes | `AGENTS.md`, Documentation and change discipline | Documentation changes |
