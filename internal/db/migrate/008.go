package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 8,
		Up:      migrateSiteAccountsAddTodayIncome,
	})
}

// 008:
// - add site_accounts.today_income if missing
func migrateSiteAccountsAddTodayIncome(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("site_accounts") {
		return nil
	}

	if !db.Migrator().HasColumn("site_accounts", "today_income") {
		if err := db.Exec("ALTER TABLE site_accounts ADD COLUMN today_income REAL NOT NULL DEFAULT 0").Error; err != nil {
			return fmt.Errorf("failed to add site_accounts.today_income: %w", err)
		}
	}

	return nil
}
