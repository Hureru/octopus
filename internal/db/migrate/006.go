package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 6,
		Up:      migrateSiteAccountsAddSub2APIRefreshFields,
	})
}

// 006:
// - add site_accounts.refresh_token if missing
// - add site_accounts.token_expires_at if missing
func migrateSiteAccountsAddSub2APIRefreshFields(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("site_accounts") {
		return nil
	}

	if !db.Migrator().HasColumn("site_accounts", "refresh_token") {
		if err := db.Exec("ALTER TABLE site_accounts ADD COLUMN refresh_token TEXT NOT NULL DEFAULT ''").Error; err != nil {
			return fmt.Errorf("failed to add site_accounts.refresh_token: %w", err)
		}
	}
	if !db.Migrator().HasColumn("site_accounts", "token_expires_at") {
		if err := db.Exec("ALTER TABLE site_accounts ADD COLUMN token_expires_at INTEGER NOT NULL DEFAULT 0").Error; err != nil {
			return fmt.Errorf("failed to add site_accounts.token_expires_at: %w", err)
		}
	}

	return nil
}
