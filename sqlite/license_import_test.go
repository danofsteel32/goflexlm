package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/danofsteel32/goflexlm"
)

func TestImportLicenseFileStoresValidatedSnapshot(t *testing.T) {
	document, err := goflexlm.ParseLicenseFile(strings.NewReader("FEATURE editor acme 1.0 permanent 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), ":memory:", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	err = store.ImportLicenseFile(context.Background(), LicenseImportRequest{
		File:          document,
		Pool:          "engineering",
		SourceName:    "licenses.lic",
		EffectiveFrom: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Timezone:      "UTC",
	})
	if err != nil {
		t.Fatalf("ImportLicenseFile() error = %v", err)
	}

	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM license_imports").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("license import count = %d, want 1", count)
	}
}
