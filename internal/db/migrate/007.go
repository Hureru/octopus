package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 7,
		Up:      migrateSiteTokensAddValueStatus,
	})
}

// 007:
// - add site_tokens.value_status if missing
// - backfill existing masked token values to masked_pending
func migrateSiteTokensAddValueStatus(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("site_tokens") {
		return nil
	}

	if !db.Migrator().HasColumn("site_tokens", "value_status") {
		if err := db.Exec("ALTER TABLE site_tokens ADD COLUMN value_status TEXT NOT NULL DEFAULT 'ready'").Error; err != nil {
			return fmt.Errorf("failed to add site_tokens.value_status: %w", err)
		}
	}

	if err := db.Exec("UPDATE site_tokens SET value_status = 'masked_pending' WHERE token LIKE '%*%' OR token LIKE '%•%'").Error; err != nil {
		return fmt.Errorf("failed to backfill masked site_tokens.value_status: %w", err)
	}
	if err := db.Exec("UPDATE site_tokens SET value_status = 'ready' WHERE value_status IS NULL OR value_status = ''").Error; err != nil {
		return fmt.Errorf("failed to backfill empty site_tokens.value_status: %w", err)
	}

	return nil
}
