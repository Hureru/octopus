package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestMigrateSiteTokensAddValueStatus(t *testing.T) {
	db := openMigrationTestDB(t)

	if err := db.Exec("CREATE TABLE site_tokens (id INTEGER PRIMARY KEY, token TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("seed migration test db failed: %v", err)
	}
	if err := db.Exec("INSERT INTO site_tokens (id, token) VALUES (1, 'sk-real-token'), (2, 'sk-ab***xyz')").Error; err != nil {
		t.Fatalf("seed site_tokens rows failed: %v", err)
	}

	if err := migrateSiteTokensAddValueStatus(db); err != nil {
		t.Fatalf("migrateSiteTokensAddValueStatus returned error: %v", err)
	}

	if !db.Migrator().HasColumn("site_tokens", "value_status") {
		t.Fatalf("expected value_status column to exist after migration")
	}

	type tokenRow struct {
		ID          int
		ValueStatus string
	}
	var rows []tokenRow
	if err := db.Table("site_tokens").Select("id, value_status").Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("query migrated site_tokens failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ValueStatus != string(model.SiteTokenValueStatusReady) {
		t.Fatalf("expected ready value status for row 1, got %q", rows[0].ValueStatus)
	}
	if rows[1].ValueStatus != string(model.SiteTokenValueStatusMaskedPending) {
		t.Fatalf("expected masked_pending value status for row 2, got %q", rows[1].ValueStatus)
	}
}
