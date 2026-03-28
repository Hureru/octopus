package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrateSiteModelRoutesAndDropDisabledModelsBackfillsLegacyDisabledRows(t *testing.T) {
	db := openMigrationTestDB(t)

	statements := []string{
		"CREATE TABLE site_accounts (id INTEGER PRIMARY KEY, site_id INTEGER NOT NULL)",
		"CREATE TABLE site_models (id INTEGER PRIMARY KEY, site_account_id INTEGER NOT NULL, group_key TEXT NOT NULL DEFAULT 'default', model_name TEXT NOT NULL, source TEXT)",
		"CREATE TABLE site_disabled_models (id INTEGER PRIMARY KEY, site_id INTEGER NOT NULL, model_name TEXT NOT NULL)",
		"INSERT INTO site_accounts (id, site_id) VALUES (1, 10), (2, 10), (3, 20)",
		"INSERT INTO site_models (id, site_account_id, group_key, model_name, source) VALUES " +
			"(1, 1, 'default', 'shared-model', 'sync'), " +
			"(2, 2, 'team-a', 'shared-model', 'sync'), " +
			"(3, 1, 'default', 'other-model', 'sync'), " +
			"(4, 3, 'default', 'site-20-model', 'sync')",
		"INSERT INTO site_disabled_models (site_id, model_name) VALUES (10, 'shared-model'), (20, 'site-20-model')",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed migration test db failed: %v", err)
		}
	}

	if err := migrateSiteModelRoutesAndDropDisabledModels(db); err != nil {
		t.Fatalf("migrateSiteModelRoutesAndDropDisabledModels returned error: %v", err)
	}

	if db.Migrator().HasTable("site_disabled_models") {
		t.Fatalf("expected legacy site_disabled_models table to be dropped after backfill")
	}

	var rows []struct {
		ID        int
		ModelName string
		RouteType string
		Disabled  bool
	}
	if err := db.Table("site_models").
		Select("id", "model_name", "route_type", "disabled").
		Order("id ASC").
		Find(&rows).Error; err != nil {
		t.Fatalf("query migrated site_models failed: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 site_models rows after migration, got %d", len(rows))
	}

	if !rows[0].Disabled || rows[0].RouteType != "openai_chat" {
		t.Fatalf("expected first shared-model row to be disabled with inferred route, got %#v", rows[0])
	}
	if !rows[1].Disabled || rows[1].RouteType != "openai_chat" {
		t.Fatalf("expected second shared-model row to be disabled with inferred route, got %#v", rows[1])
	}
	if rows[2].Disabled {
		t.Fatalf("expected unrelated model to remain enabled, got %#v", rows[2])
	}
	if !rows[3].Disabled {
		t.Fatalf("expected site-specific disabled model to be backfilled, got %#v", rows[3])
	}
}

func openMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "migration-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return db
}
