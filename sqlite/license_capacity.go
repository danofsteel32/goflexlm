package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/danofsteel32/goflexlm"
)

func rebuildLicenseCapacity(ctx context.Context, tx *sql.Tx, pool int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT id,effective_ns,document_json,resolved_dates_json FROM license_imports WHERE pool_id=? ORDER BY effective_ns", pool)
	if err != nil {
		return err
	}
	type snapshot struct {
		id, at       int64
		contributors []licenseContributor
	}
	var snapshots []snapshot
	universe := map[licenseIdentity]bool{}
	for rows.Next() {
		var s snapshot
		var raw, datesRaw string
		if err := rows.Scan(&s.id, &s.at, &raw, &datesRaw); err != nil {
			rows.Close()
			return err
		}
		var file goflexlm.LicenseFile
		var dates []resolvedLicenseDate
		if err := json.Unmarshal([]byte(raw), &file); err != nil {
			rows.Close()
			return fmt.Errorf("decode snapshot: %w", err)
		}
		if err := json.Unmarshal([]byte(datesRaw), &dates); err != nil {
			rows.Close()
			return fmt.Errorf("decode resolved dates: %w", err)
		}
		if len(dates) != len(file.Features) {
			rows.Close()
			return fmt.Errorf("snapshot %d has inconsistent resolved dates", s.id)
		}
		for _, f := range file.Features {
			universe[licenseIdentity{f.Vendor, f.Name}] = true
		}
		s.contributors = licenseContributors(file, dates, s.at)
		snapshots = append(snapshots, s)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM capacity_changes WHERE pool_id=?", pool); err != nil {
		return err
	}
	keys := make([]licenseIdentity, 0, len(universe))
	for k := range universe {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].vendor != keys[j].vendor {
			return keys[i].vendor < keys[j].vendor
		}
		return keys[i].feature < keys[j].feature
	})
	previous := map[licenseIdentity]licenseCapacity{}
	for i, s := range snapshots {
		for _, at := range capacityInstants(s.contributors, s.at) {
			if i+1 < len(snapshots) && at >= snapshots[i+1].at {
				break
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			values, err := capacityAt(s.contributors, at)
			if err != nil {
				return err
			}
			for _, key := range keys {
				value := values[key]
				if before, ok := previous[key]; ok && before == value {
					continue
				}
				var licenses any = value.licenses
				if value.uncounted {
					licenses = nil
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO capacity_changes(pool_id,vendor,feature,effective_ns,licenses,uncounted,source_import_id) VALUES(?,?,?,?,?,?,?)`, pool, key.vendor, key.feature, at, licenses, value.uncounted, s.id); err != nil {
					return err
				}
				previous[key] = value
			}
		}
	}
	return nil
}

type licenseIdentity struct{ vendor, feature string }
type licenseCapacity struct {
	licenses  int
	uncounted bool
}
type licenseContributor struct {
	identity   licenseIdentity
	line       int
	start      int64
	expiration *int64
	capacity   licenseCapacity
}

func licenseContributors(file goflexlm.LicenseFile, dates []resolvedLicenseDate, effective int64) []licenseContributor {
	seen := map[licenseIdentity]bool{}
	var result []licenseContributor
	for i, f := range file.Features {
		key := licenseIdentity{f.Vendor, f.Name}
		if f.Kind == goflexlm.LicenseFeatureLine {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		start := effective
		if dates[i].Start != nil && *dates[i].Start > start {
			start = *dates[i].Start
		}
		if dates[i].Expiration != nil && start >= *dates[i].Expiration {
			continue
		}
		c := licenseContributor{identity: key, line: f.Line, start: start, expiration: dates[i].Expiration, capacity: licenseCapacity{uncounted: f.Uncounted}}
		if f.Licenses != nil {
			c.capacity.licenses = *f.Licenses
		}
		result = append(result, c)
	}
	return result
}

func capacityAt(contributors []licenseContributor, at int64) (map[licenseIdentity]licenseCapacity, error) {
	values := map[licenseIdentity]licenseCapacity{}
	for _, c := range contributors {
		if at < c.start || (c.expiration != nil && at >= *c.expiration) {
			continue
		}
		v := values[c.identity]
		if c.capacity.licenses > int(^uint(0)>>1)-v.licenses {
			return nil, fmt.Errorf("line %d: capacity overflow for vendor %q feature %q", c.line, c.identity.vendor, c.identity.feature)
		}
		v.licenses += c.capacity.licenses
		v.uncounted = v.uncounted || c.capacity.uncounted
		values[c.identity] = v
	}
	for key, v := range values {
		if v.uncounted {
			v.licenses = 0
			values[key] = v
		}
	}
	return values, nil
}

func capacityInstants(contributors []licenseContributor, effective int64) []int64 {
	seen := map[int64]bool{effective: true}
	for _, c := range contributors {
		seen[c.start] = true
		if c.expiration != nil {
			seen[*c.expiration] = true
		}
	}
	result := make([]int64, 0, len(seen))
	for at := range seen {
		result = append(result, at)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func validateLicenseCapacity(file goflexlm.LicenseFile, dates []resolvedLicenseDate, effective int64) error {
	contributors := licenseContributors(file, dates, effective)
	for _, at := range capacityInstants(contributors, effective) {
		if _, err := capacityAt(contributors, at); err != nil {
			return err
		}
	}
	return nil
}
