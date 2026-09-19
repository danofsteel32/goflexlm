# SQLite demo data

This dataset exercises the common paths in `goflexlmdb` without being too
large to inspect by hand. It contains two logical log streams in one license
pool, two features, an entitlement change, partial quantities, verbose
identity fields, denials, completed and open queues, open usage, a zero-length
session, an unresolved classic timestamp, generic daemon messages, and a
separate parser-diagnostic recovery example.

All commands below run from the repository root. Start with a fresh database:

```sh
rm -f /tmp/goflexlm-demo.db /tmp/goflexlm-demo.db-wal /tmp/goflexlm-demo.db-shm

go run ./cmd/goflexlmdb entitlements replace \
  --db /tmp/goflexlm-demo.db --pool studio --feature editor \
  testdata/sqlite-demo/entitlements-editor.csv

go run ./cmd/goflexlmdb entitlements replace \
  --db /tmp/goflexlm-demo.db --pool studio --feature render \
  testdata/sqlite-demo/entitlements-render.csv

go run ./cmd/goflexlmdb import \
  --db /tmp/goflexlm-demo.db --pool studio --stream server-a \
  testdata/sqlite-demo/server-a-2026-09-01.log \
  testdata/sqlite-demo/server-a-2026-09-02.log

go run ./cmd/goflexlmdb import \
  --db /tmp/goflexlm-demo.db --pool studio --stream server-b \
  testdata/sqlite-demo/server-b-2026-09.log
```

The three source files should import 13, 8, and 5 activity records. Generic
messages are omitted. The second server-a file also reports one unresolved
timestamp.

## Capacity checks

```sh
go run ./cmd/goflexlmdb report capacity \
  --db /tmp/goflexlm-demo.db --pool studio --feature editor \
  --from 2026-09-01T00:00:00Z --to 2026-09-03T00:00:00Z \
  --bucket day --timezone UTC
```

Expected peaks:

| Day | Purchased | Lower peak | Upper peak |
| --- | ---: | ---: | ---: |
| 2026-09-01 | 4 | 4 | 4 |
| 2026-09-02 | 6 | 3 | 4 |

The quality object in JSON mode should report three open editor licenses and
one unresolved timestamp. The first day has 45 minutes of saturation, from
08:15 through 09:00 UTC. The second day is not saturated.

## Denial checks

```sh
go run ./cmd/goflexlmdb report denials \
  --db /tmp/goflexlm-demo.db --pool studio \
  --from 2026-09-01T00:00:00Z --to 2026-09-03T00:00:00Z --json
```

Expected groups are:

- `editor`, empty reason and error, one event requesting three licenses;
- `editor`, `All licenses are in use.`, error `-4,342`, one event requesting
  one license;
- `render`, `Licensed number reached.`, error `-4,342`, one event requesting
  one license.

## Queue checks

```sh
go run ./cmd/goflexlmdb report queues \
  --db /tmp/goflexlm-demo.db --pool studio \
  --from 2026-09-01T00:00:00Z --to 2026-09-03T00:00:00Z --json
```

For `editor`, expect two queued licenses, maximum depth two, two completed
waits, p50 of 10 minutes, and p95/max of 40 minutes. For `render`, expect one
completed 45-minute wait plus one open queued license whose age is measured at
the report end.

## Deduplication and diagnostic recovery

Re-run either import command unchanged. Each file should be reported as a
duplicate and no activity should be added.

The diagnostic fixture deliberately returns status 1 while committing the two
valid activities around its malformed line:

```sh
go run ./cmd/goflexlmdb import \
  --db /tmp/goflexlm-demo.db --pool studio --stream diagnostic-demo \
  testdata/sqlite-demo/diagnostic-recovery.log
```

The output should report two imported activities and one diagnostic. A later
capacity report covering September 3 should contain Nina's one-hour session,
confirming that parsing recovered after the malformed line.
