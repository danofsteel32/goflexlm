package sqlite

const (
	schemaVersion     = 2
	derivationVersion = 1
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version(version) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM schema_version);

CREATE TABLE IF NOT EXISTS license_pools (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS log_streams (
  id INTEGER PRIMARY KEY,
  pool_id INTEGER NOT NULL REFERENCES license_pools(id),
  name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS imports (
  id INTEGER PRIMARY KEY,
  stream_id INTEGER NOT NULL REFERENCES log_streams(id),
  source_name TEXT NOT NULL,
  digest BLOB NOT NULL,
  byte_count INTEGER NOT NULL,
  line_count INTEGER NOT NULL,
  activity_count INTEGER NOT NULL,
  omitted_message_count INTEGER NOT NULL,
  diagnostic_count INTEGER NOT NULL,
  diagnostic_json TEXT NOT NULL,
  unresolved_count INTEGER NOT NULL,
  imported_at_ns INTEGER NOT NULL,
  UNIQUE(stream_id, digest)
);
CREATE TABLE IF NOT EXISTS activity_events (
  id INTEGER PRIMARY KEY,
  import_id INTEGER NOT NULL REFERENCES imports(id) ON DELETE CASCADE,
  stream_id INTEGER NOT NULL REFERENCES log_streams(id),
  pool_id INTEGER NOT NULL REFERENCES license_pools(id),
  source_name TEXT NOT NULL,
  source_line INTEGER NOT NULL,
  timestamp_original TEXT NOT NULL,
  timestamp_ns INTEGER,
  daemon TEXT NOT NULL,
  kind TEXT NOT NULL,
  feature TEXT NOT NULL,
  licenses INTEGER NOT NULL CHECK(licenses > 0),
  user_name TEXT NOT NULL,
  host_name TEXT NOT NULL,
  checkout_data TEXT NOT NULL,
  denial_reason TEXT NOT NULL,
  denial_error TEXT NOT NULL,
  verbose_version TEXT NOT NULL,
  verbose_user TEXT NOT NULL,
  verbose_ip TEXT NOT NULL,
  verbose_pid TEXT NOT NULL,
  verbose_handle TEXT NOT NULL,
  verbose_in_use TEXT NOT NULL,
  verbose_out_of TEXT NOT NULL,
  verbose_project TEXT NOT NULL,
  verbose_flex_version TEXT NOT NULL,
  verbose_flex_revision TEXT NOT NULL,
  verbose_date TEXT NOT NULL,
  verbose_time TEXT NOT NULL,
  verbose_duplicate_group TEXT NOT NULL,
  raw_line TEXT NOT NULL,
  message TEXT NOT NULL,
  UNIQUE(import_id, source_line)
);
CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY,
  pool_id INTEGER NOT NULL REFERENCES license_pools(id),
  stream_id INTEGER NOT NULL REFERENCES log_streams(id),
  daemon TEXT NOT NULL,
  feature TEXT NOT NULL,
  session_type TEXT NOT NULL CHECK(session_type IN ('usage','queue')),
  start_ns INTEGER,
  end_ns INTEGER,
  quantity INTEGER NOT NULL CHECK(quantity > 0),
  state TEXT NOT NULL CHECK(state IN ('closed','open','orphan')),
  match_tier INTEGER NOT NULL,
  ambiguous INTEGER NOT NULL CHECK(ambiguous IN (0,1)),
  opening_event_id INTEGER REFERENCES activity_events(id),
  closing_event_id INTEGER REFERENCES activity_events(id)
);
CREATE TABLE IF NOT EXISTS entitlements (
  id INTEGER PRIMARY KEY,
  pool_id INTEGER NOT NULL REFERENCES license_pools(id),
  feature TEXT NOT NULL,
  effective_ns INTEGER NOT NULL,
  licenses INTEGER NOT NULL CHECK(licenses >= 0),
  UNIQUE(pool_id, feature, effective_ns)
);
CREATE TABLE IF NOT EXISTS license_imports (
  id INTEGER PRIMARY KEY,
  pool_id INTEGER NOT NULL REFERENCES license_pools(id),
  source_name TEXT NOT NULL,
  effective_ns INTEGER NOT NULL,
  timezone TEXT NOT NULL,
  document_json TEXT NOT NULL,
  resolved_dates_json TEXT NOT NULL,
  UNIQUE(pool_id, effective_ns)
);
CREATE TABLE IF NOT EXISTS capacity_changes (
  pool_id INTEGER NOT NULL REFERENCES license_pools(id), vendor TEXT NOT NULL, feature TEXT NOT NULL,
  effective_ns INTEGER NOT NULL, licenses INTEGER, uncounted INTEGER NOT NULL CHECK(uncounted IN (0,1)),
  source_import_id INTEGER NOT NULL REFERENCES license_imports(id) ON DELETE CASCADE,
  PRIMARY KEY(pool_id, vendor, feature, effective_ns),
  CHECK((uncounted=1 AND licenses IS NULL) OR
        (uncounted=0 AND licenses IS NOT NULL AND licenses>=0))
);
CREATE INDEX IF NOT EXISTS capacity_changes_source_import_idx ON capacity_changes(source_import_id);
CREATE TABLE IF NOT EXISTS derivation_state (
  pool_id INTEGER PRIMARY KEY REFERENCES license_pools(id),
  version INTEGER NOT NULL,
  processed_event_id INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS activity_chronology ON activity_events(stream_id, daemon, feature, timestamp_ns, id);
CREATE INDEX IF NOT EXISTS activity_reports ON activity_events(pool_id, kind, timestamp_ns, feature);
CREATE INDEX IF NOT EXISTS session_intervals ON sessions(pool_id, feature, start_ns, end_ns);
UPDATE schema_version SET version = 2;
`
