# SQLite license-import demo

All files are synthetic. Run these commands from the repository root.
Use a fresh directory; version-1 databases require deletion and recreation,
followed by reimport of retained inputs.

```sh
demo_dir=$(mktemp -d /tmp/goflexlm-demo.XXXXXX)

go run ./cmd/goflexlmdb licenses parse testdata/sqlite-demo/licenses-2026-09-01.lic
go run ./cmd/goflexlmdb licenses import --db "$demo_dir/usage.db" --pool studio \
  --effective-from 2026-09-01T00:00:00Z --timezone UTC \
  testdata/sqlite-demo/licenses-2026-09-01.lic
go run ./cmd/goflexlmdb licenses import --db "$demo_dir/usage.db" --pool studio \
  --effective-from 2026-09-02T00:00:00Z --timezone UTC \
  testdata/sqlite-demo/licenses-2026-09-02.lic

go run ./cmd/goflexlmdb import --db "$demo_dir/usage.db" --pool studio --stream server-a \
  testdata/sqlite-demo/server-a-2026-09-01.log \
  testdata/sqlite-demo/server-a-2026-09-02.log
go run ./cmd/goflexlmdb import --db "$demo_dir/usage.db" --pool studio --stream server-b \
  testdata/sqlite-demo/server-b-2026-09.log
go run ./cmd/goflexlmdb report capacity --db "$demo_dir/usage.db" --pool studio \
  --from 2026-09-01T00:00:00Z --to 2026-09-03T00:00:00Z --bucket day --timezone UTC
```

License imports are silent on success. Each file replaces the whole pool
snapshot from its effective instant; the second file includes render because
omitting it would set render capacity to known zero. Importing another file
at the same instant replaces that snapshot. An empty file ends all known
capacity, so use it only when that is intended.

The log files import 13, 8, and 5 activities. Generic messages are omitted;
the second server-a file retains one unresolved timestamp.

Expected finite editor rows:

| Vendor | Day | Purchased | Lower peak | Upper peak |
| --- | --- | ---: | ---: | ---: |
| acme | 2026-09-01 | 4 | 4 | 4 |
| acme | 2026-09-02 | 6 | 3 | 4 |

The first day has 45 minutes of saturation; the second is not saturated.
The preview feature has known zero capacity on September 1 and uncounted
capacity on September 2. Its uncounted row shows no finite purchased count
or headroom; JSON exposes `uncounted: true`.

To see missing capacity from a different vendor:

```sh
go run ./cmd/goflexlmdb import --db "$demo_dir/usage.db" --pool studio --stream unmatched \
  testdata/sqlite-demo/unmatched-vendor.log
go run ./cmd/goflexlmdb report capacity --db "$demo_dir/usage.db" --pool studio \
  --from 2026-09-02T00:00:00Z --to 2026-09-03T00:00:00Z --feature editor --json
```

The beta/editor row has `purchased: null`, `uncounted: false`, and one hour
of used seat time. It contributes one day of missing coverage. The separate
acme/editor row retains its six purchased licenses. Feature filtering applies
across vendors; capacity matching requires an exact vendor/daemon match.

Other commands continue to use the same database:

```sh
go run ./cmd/goflexlmdb rebuild --db "$demo_dir/usage.db" --pool studio
go run ./cmd/goflexlmdb report denials --db "$demo_dir/usage.db" --pool studio \
  --from 2026-09-01T00:00:00Z --to 2026-09-03T00:00:00Z --json
go run ./cmd/goflexlmdb report queues --db "$demo_dir/usage.db" --pool studio \
  --from 2026-09-01T00:00:00Z --to 2026-09-03T00:00:00Z --json
```

Re-running a log import reports duplicates without adding activity. The
diagnostic-recovery.log fixture returns status 1 while committing the two
valid activities around its malformed line. The resulting September 3
capacity report contains Nina's one-hour session.
