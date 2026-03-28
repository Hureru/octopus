package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 3,
		Up:      migrateSiteModelsToGroupScoped,
	})
}

// 003:
// - add site_models.group_key if missing
// - backfill empty values to default
func migrateSiteModelsToGroupScoped(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if !db.Migrator().HasTable("site_models") {
		return nil
	}

	if !db.Migrator().HasColumn("site_models", "group_key") {
		if err := db.Exec("ALTER TABLE site_models ADD COLUMN group_key TEXT NOT NULL DEFAULT 'default'").Error; err != nil {
			return fmt.Errorf("failed to add site_models.group_key: %w", err)
		}
	}

	if err := db.Exec("UPDATE site_models SET group_key = 'default' WHERE TRIM(COALESCE(group_key, '')) = ''").Error; err != nil {
		return fmt.Errorf("failed to backfill site_models.group_key: %w", err)
	}

	return nil
}
