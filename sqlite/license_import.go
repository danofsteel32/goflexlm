package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/danofsteel32/goflexlm"
)

// ImportLicenseFile stores one validated FlexLM license snapshot.
func (s *Store) ImportLicenseFile(ctx context.Context, request LicenseImportRequest) error {
	if err := validateLicenseImportRequest(request); err != nil {
		return fmt.Errorf("import license file: %w", err)
	}
	request.File = normalizeLicenseFile(request.File)
	document, err := json.Marshal(request.File)
	if err != nil {
		return fmt.Errorf("import license file: encode document: %w", err)
	}
	resolved, err := json.Marshal(make([]any, len(request.File.Features)))
	if err != nil {
		return fmt.Errorf("import license file: encode resolved dates: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("import license file: %w", err)
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("import license file: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("import license file: begin transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO license_pools(name) VALUES (?) ON CONFLICT(name) DO NOTHING", request.Pool); err != nil {
		return fmt.Errorf("import license file: ensure pool: %w", err)
	}
	pool, err := poolID(ctx, tx, request.Pool)
	if err != nil {
		return fmt.Errorf("import license file: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO license_imports(pool_id, source_name, effective_ns, timezone, document_json, resolved_dates_json)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(pool_id, effective_ns) DO UPDATE SET source_name=excluded.source_name, timezone=excluded.timezone, document_json=excluded.document_json, resolved_dates_json=excluded.resolved_dates_json`,
		pool, request.SourceName, request.EffectiveFrom.UTC().UnixNano(), request.Timezone, string(document), string(resolved)); err != nil {
		return fmt.Errorf("import license file: store snapshot: %w", err)
	}
	var importID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM license_imports WHERE pool_id=? AND effective_ns=?", pool, request.EffectiveFrom.UTC().UnixNano()).Scan(&importID); err != nil {
		return fmt.Errorf("import license file: find snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM capacity_changes WHERE pool_id=?", pool); err != nil {
		return fmt.Errorf("import license file: clear capacity projection: %w", err)
	}
	type capacityKey struct{ vendor, feature string }
	capacities := make(map[capacityKey]int)
	for _, feature := range request.File.Features {
		if feature.Licenses == nil {
			continue
		}
		key := capacityKey{vendor: feature.Vendor, feature: feature.Name}
		capacities[key] += *feature.Licenses
	}
	for key, licenses := range capacities {
		if _, err := tx.ExecContext(ctx, `INSERT INTO capacity_changes(pool_id, vendor, feature, effective_ns, licenses, uncounted, source_import_id)
            VALUES (?, ?, ?, ?, ?, 0, ?)`, pool, key.vendor, key.feature, request.EffectiveFrom.UTC().UnixNano(), licenses, importID); err != nil {
			return fmt.Errorf("import license file: store capacity projection: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("import license file: commit: %w", err)
	}
	return nil
}

func validateLicenseImportRequest(request LicenseImportRequest) error {
	if strings.TrimSpace(request.Pool) == "" || strings.TrimSpace(request.SourceName) == "" {
		return fmt.Errorf("pool and source name are required")
	}
	if request.EffectiveFrom.IsZero() {
		return fmt.Errorf("effective time is required")
	}
	if request.Timezone != "UTC" && !strings.Contains(request.Timezone, "/") {
		return fmt.Errorf("timezone must be UTC or an IANA location")
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	return nil
}

func normalizeLicenseFile(file goflexlm.LicenseFile) goflexlm.LicenseFile {
	if file.Servers == nil {
		file.Servers = []goflexlm.LicenseServer{}
	}
	if file.Vendors == nil {
		file.Vendors = []goflexlm.LicenseVendor{}
	}
	if file.Features == nil {
		file.Features = []goflexlm.LicenseFeature{}
	}
	return file
}
