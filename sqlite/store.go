package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// Store is a SQLite-backed activity and analytics store.
type Store struct {
	db     *sql.DB
	path   string
	writer sync.Mutex
}

// Open opens path, applies forward-only migrations, and configures SQLite.
func Open(ctx context.Context, path string, options OpenOptions) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("open sqlite: database path is required")
	}
	dsn, diskPath, err := databaseDSN(path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if diskPath != "" {
		if err := createDatabaseFile(diskPath); err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	max := options.MaxOpenConns
	if max == 0 {
		max = 4
	}
	if max < 1 {
		db.Close()
		return nil, errors.New("open sqlite: MaxOpenConns must be positive")
	}
	db.SetMaxOpenConns(max)
	db.SetMaxIdleConns(max)
	store := &Store{db: db, path: diskPath}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.rebuildStale(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if diskPath != "" {
		secureSidecars(diskPath)
	}
	return store, nil
}

func (s *Store) rebuildStale(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT e.pool_id FROM activity_events e
        LEFT JOIN derivation_state d ON d.pool_id=e.pool_id
        WHERE d.pool_id IS NULL OR d.version<>?`, derivationVersion)
	if err != nil {
		return fmt.Errorf("refresh session derivations: list pools: %w", err)
	}
	var pools []int64
	for rows.Next() {
		var pool int64
		if err := rows.Scan(&pool); err != nil {
			rows.Close()
			return fmt.Errorf("refresh session derivations: scan pool: %w", err)
		}
		pools = append(pools, pool)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("refresh session derivations: %w", err)
	}
	for _, pool := range pools {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("refresh session derivations: begin: %w", err)
		}
		if err := rebuildTx(ctx, tx, pool); err != nil {
			tx.Rollback()
			return fmt.Errorf("refresh session derivations: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("refresh session derivations: commit: %w", err)
		}
	}
	return nil
}

func databaseDSN(path string) (string, string, error) {
	values := url.Values{}
	values.Add("_pragma", "foreign_keys(1)")
	values.Add("_pragma", "journal_mode(WAL)")
	values.Add("_pragma", "synchronous(NORMAL)")
	values.Add("_pragma", "busy_timeout(5000)")
	if path == ":memory:" {
		var token [12]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", "", fmt.Errorf("create in-memory database name: %w", err)
		}
		values.Set("mode", "memory")
		values.Set("cache", "shared")
		return "file:goflexlm-" + fmt.Sprintf("%x", token[:]) + "?" + values.Encode(), "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve database path: %w", err)
	}
	return (&url.URL{Scheme: "file", Path: abs, RawQuery: values.Encode()}).String(), abs, nil
}

func createDatabaseFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}
	return file.Close()
}

func secureSidecars(path string) {
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err == nil {
			_ = os.Chmod(path+suffix, 0o600)
		}
	}
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate sqlite: begin: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("migrate sqlite: create version table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM schema_version)`); err != nil {
		return fmt.Errorf("migrate sqlite: initialize version: %w", err)
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&version); err != nil {
		return fmt.Errorf("migrate sqlite: read version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("migrate sqlite: database schema version %d is newer than supported version %d", version, schemaVersion)
	}
	if version < schemaVersion {
		if version != 0 {
			return fmt.Errorf("migrate sqlite: database schema version %d must be deleted and recreated", version)
		}
		if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
			return fmt.Errorf("migrate sqlite: apply version 2: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate sqlite: commit: %w", err)
	}
	return nil
}

// Close closes all database resources.
func (s *Store) Close() error {
	err := s.db.Close()
	if s.path != "" {
		secureSidecars(s.path)
	}
	if err != nil {
		return fmt.Errorf("close sqlite: %w", err)
	}
	return nil
}

func validateName(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", kind)
	}
	return nil
}

func poolID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, pool string) (int64, error) {
	var id int64
	if err := q.QueryRowContext(ctx, `SELECT id FROM license_pools WHERE name = ?`, pool).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("pool %q does not exist", pool)
		}
		return 0, fmt.Errorf("look up pool %q: %w", pool, err)
	}
	return id, nil
}
