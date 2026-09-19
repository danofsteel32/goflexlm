package sqlite

import (
	"context"
	"fmt"
)

// ReplaceEntitlements atomically replaces all capacity changes for a pool and feature.
func (s *Store) ReplaceEntitlements(ctx context.Context, pool, feature string, entitlements []Entitlement) error {
	if err := validateName("pool", pool); err != nil {
		return fmt.Errorf("replace entitlements: %w", err)
	}
	if err := validateName("feature", feature); err != nil {
		return fmt.Errorf("replace entitlements: %w", err)
	}
	for index, entitlement := range entitlements {
		if entitlement.EffectiveFrom.IsZero() {
			return fmt.Errorf("replace entitlements: row %d has a zero effective time", index+1)
		}
		if entitlement.Licenses < 0 {
			return fmt.Errorf("replace entitlements: row %d has negative licenses", index+1)
		}
		if index > 0 && !entitlement.EffectiveFrom.After(entitlements[index-1].EffectiveFrom) {
			return fmt.Errorf("replace entitlements: rows must be strictly chronological")
		}
	}
	s.writer.Lock()
	defer s.writer.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace entitlements: begin transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO license_pools(name) VALUES (?) ON CONFLICT(name) DO NOTHING`, pool); err != nil {
		return fmt.Errorf("replace entitlements: ensure pool: %w", err)
	}
	poolID, err := poolID(ctx, tx, pool)
	if err != nil {
		return fmt.Errorf("replace entitlements: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM entitlements WHERE pool_id = ? AND feature = ?`, poolID, feature); err != nil {
		return fmt.Errorf("replace entitlements: clear existing rows: %w", err)
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO entitlements(pool_id, feature, effective_ns, licenses) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("replace entitlements: prepare insert: %w", err)
	}
	defer statement.Close()
	for index, entitlement := range entitlements {
		if _, err := statement.ExecContext(ctx, poolID, feature, entitlement.EffectiveFrom.UTC().UnixNano(), entitlement.Licenses); err != nil {
			return fmt.Errorf("replace entitlements: insert row %d: %w", index+1, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("replace entitlements: commit: %w", err)
	}
	return nil
}
