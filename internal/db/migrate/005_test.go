package migrate

import "testing"

func TestMigrateSiteModelsDropLegacyUniqueIndexAllowsSameModelAcrossGroups(t *testing.T) {
	db := openMigrationTestDB(t)

	statements := []string{
		"CREATE TABLE site_models (id INTEGER PRIMARY KEY, site_account_id INTEGER NOT NULL, group_key TEXT NOT NULL DEFAULT 'default', model_name TEXT NOT NULL, source TEXT)",
		"CREATE UNIQUE INDEX idx_site_account_model ON site_models(site_account_id, model_name)",
		"INSERT INTO site_models (id, site_account_id, group_key, model_name, source) VALUES (1, 10, 'default', 'gpt-4o-mini', 'sync')",
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed migration test db failed: %v", err)
		}
	}

	if err := migrateSiteModelsDropLegacyUniqueIndex(db); err != nil {
		t.Fatalf("migrateSiteModelsDropLegacyUniqueIndex returned error: %v", err)
	}

	if db.Migrator().HasIndex("site_models", "idx_site_account_model") {
		t.Fatalf("expected legacy unique index idx_site_account_model to be dropped")
	}
	if !db.Migrator().HasIndex("site_models", "idx_site_account_group_model") {
		t.Fatalf("expected scoped unique index idx_site_account_group_model to exist")
	}

	if err := db.Exec("INSERT INTO site_models (id, site_account_id, group_key, model_name, source) VALUES (2, 10, 'team-a', 'gpt-4o-mini', 'sync')").Error; err != nil {
		t.Fatalf("expected same model across different groups to be insertable after migration: %v", err)
	}
}
