package migrate

import "testing"

func TestMigrateSiteAccountsAddSub2APIRefreshFields(t *testing.T) {
	db := openMigrationTestDB(t)

	if err := db.Exec("CREATE TABLE site_accounts (id INTEGER PRIMARY KEY, access_token TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("seed migration test db failed: %v", err)
	}

	if err := migrateSiteAccountsAddSub2APIRefreshFields(db); err != nil {
		t.Fatalf("migrateSiteAccountsAddSub2APIRefreshFields returned error: %v", err)
	}

	if !db.Migrator().HasColumn("site_accounts", "refresh_token") {
		t.Fatalf("expected refresh_token column to exist after migration")
	}
	if !db.Migrator().HasColumn("site_accounts", "token_expires_at") {
		t.Fatalf("expected token_expires_at column to exist after migration")
	}
}
