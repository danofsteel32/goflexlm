# License purchasing and usage dashboard

`goflexlmweb` serves reports from an existing goflexlm SQLite database. It uses
server-rendered HTML and embedded CSS; no JavaScript, frontend build, additional
dependency, or external service is required.

## Start the interface

Import logs and license snapshots with `goflexlmdb`, then run:

```sh
go run ./cmd/goflexlmweb --db usage.db --pool engineering
```

Open `http://127.0.0.1:8080`. To choose another local port, add
`--listen 127.0.0.1:8081`. Only loopback IP addresses are accepted. The interface
has no authentication and is intended for the person running it locally.
Ctrl+C or SIGTERM shuts down the server. Startup prints the URL; usage errors
exit 2, operational failures exit 1, and a clean shutdown exits 0.

The database file must already exist. The interface exposes only reports, with
no import, edit, or delete actions. Opening the store still performs its normal
schema checks and refreshes stale session derivations; this is not a SQLite
read-only connection. Schema version 2 and all existing database and report
contracts are unchanged.

For synthetic data, follow the [SQLite demo](../testdata/sqlite-demo/README.md),
then run `go run ./cmd/goflexlmweb --db "$demo_dir/usage.db" --pool studio`.
Select September 1, 2026 through September 3, 2026 in UTC. The editor row shows
purchased capacity of 4–6, peak usage of 4–4, and 0.75–0.75 hours at capacity.

## Select the period

The form accepts the pool, an optional exact feature name, start and end dates,
and a timezone such as `UTC` or `America/New_York`. The end date is excluded.
Dates mean midnight in the selected timezone, including daylight-saving changes.
The default is the current UTC date and preceding 29 days, with tomorrow as the
exclusive end. Requests are limited to 366 calendar days between 1700 and 2200.
Use several periods to investigate a longer history.

All filters are included in the page URL, so you can bookmark a report. Clicking
a feature filters all three reports to that feature, across vendors. Clear the
feature field to show all features. Apply filters again after imports to refresh;
there is no live polling. Reports run sequentially, so finish imports before
refreshing when comparing all three reports at the same point in time.

## Read the reports

- **Summary:** vendor/feature pairs, pairs with possible saturation, denial events,
  and queue events. These are not unique users or interchangeable license totals.
- **Usage and capacity:** exact vendor/feature rows, purchased capacity ranges,
  confirmed-to-possible peak usage, minimum spare seats, and time at capacity.
  Capacity pressure means possible saturation occurred, not a recommendation to
  buy a particular quantity. The peak meter is the largest possible peak divided
  by its contemporaneous finite capacity; it is not average utilization.
- **Daily detail:** expand the capacity table to see calendar and license-change
  segments, purchased capacity, peaks, and used seat-hours. Rows are grouped in
  the selected timezone; printed segment boundaries are UTC.
- **Denied requests:** event counts, requested-license quantities, vendor reasons,
  and error codes. Denials are grouped by feature and reason, not by vendor.
- **Queue waits:** queued events and quantities, median, P95 and maximum completed
  wait, maximum depth, open queued licenses, and oldest open age. Percentiles are
  calculated over the whole selected period, never averaged from daily values.
  A dash means no completed wait or no open age, rather than a zero-duration wait.
- **Data quality:** open, ambiguous, orphan, weakly matched and unresolved data;
  missing capacity segments and uncovered duration; possible usage above capacity.
  Usage and denial quality share the same underlying session selection, so usage
  quality is displayed once. Unresolved timestamp counts cover all imported
  history matching the pool and feature, not only the selected dates.

Confirmed usage includes closed, unambiguous sessions; possible usage also
includes open and ambiguous sessions. Minimum spare-seat ranges run from the
possible-usage result to the confirmed-usage result. Negative spare seats indicate
usage above known capacity. Headroom and saturation summaries apply only to
segments with known finite capacity.

Known zero, uncounted, and unknown capacity remain distinct. If capacity changes
state during the period, the purchased column lists the states together; expand
daily detail to see when. The interface does not infer savings, prices, renewal
dates, unique-user counts, or a recommended purchase quantity from the logs.
Observed spare capacity is a starting point for review, not proof that licenses
can be removed.

## Export

**Export reports** downloads one JSON object with `capacity`, `denials`, and
`queues` properties containing the existing report JSON structures. Capacity uses
daily buckets with additional license-change boundaries. Denial and queue reports
cover the full period without calendar bucketing. The export retains exact
nanosecond fields and quality counters; the web tables round durations for display.
Existing `goflexlmdb report --json` output is unaffected.
