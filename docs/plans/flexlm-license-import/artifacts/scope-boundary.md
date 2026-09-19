# Scope Boundary: FlexLM License Import

## Work Item

No separate ticket, issue, pull request, or written work item exists. The operator's request in this conversation is the only boundary this run has.

## Stated Scope

> I do not like the way license entitlements work right now. The structure of import should be based on the common model of the FlexLM license file syntax. SERVER, VENDOR, FEATURE, INCREMENT, lines and their options/metadata should be supported. We will have a FlexLM license file parser that can parse the data we need to map licenses to license usage in lmgrd logs. Let's come up with a plan to make that happen.

## Stated Exclusions

None stated.

## Operator-Stated Scope

> The structure of import should be based on the common model of the FlexLM license file syntax. SERVER, VENDOR, FEATURE, INCREMENT, lines and their options/metadata should be supported.

> We will have a FlexLM license file parser that can parse the data we need to map licenses to license usage in lmgrd logs.

## Direction of Travel

The existing CSV entitlement import is replaced by direct FlexLM license-file parsing. Intermediate representations such as JSON may be supported, but they do not preserve CSV as an alternative entitlement source.

There are no users or production data. The CSV-era database contents may be wiped and replaced with a clean schema; no data migration or legacy compatibility path is required.

## Visual Material Received

None received.

## Record Provenance

Established by `han-planning:plan-a-change` from the operator's request in this conversation.
