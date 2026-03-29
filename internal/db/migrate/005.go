package migrate

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 5,
		Up:      migrateSiteModelsDropLegacyUniqueIndex,
	})
}

// 005:
// - drop legacy unique index on (site_account_id, model_name)
// - ensure the scoped unique index on (site_account_id, group_key, model_name) exists
func migrateSiteModelsDropLegacyUniqueIndex(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("site_models") {
		return nil
	}

	type sqliteIndexRow struct {
		Name   string `gorm:"column:name"`
		Unique int    `gorm:"column:unique"`
	}
	type sqliteIndexColumn struct {
		Name string `gorm:"column:name"`
	}

	switch db.Dialector.Name() {
	case "sqlite":
		var indexes []sqliteIndexRow
		if err := db.Raw("PRAGMA index_list('site_models')").Scan(&indexes).Error; err != nil {
			return fmt.Errorf("failed to inspect site_models indexes: %w", err)
		}

		for _, index := range indexes {
			if index.Unique == 0 || strings.TrimSpace(index.Name) == "" {
				continue
			}
			var columns []sqliteIndexColumn
			if err := db.Raw(fmt.Sprintf("PRAGMA index_info(%q)", index.Name)).Scan(&columns).Error; err != nil {
				return fmt.Errorf("failed to inspect site_models index %s: %w", index.Name, err)
			}
			if len(columns) != 2 {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(columns[0].Name), "site_account_id") &&
				strings.EqualFold(strings.TrimSpace(columns[1].Name), "model_name") {
				if err := db.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %q", index.Name)).Error; err != nil {
					return fmt.Errorf("failed to drop legacy site_models index %s: %w", index.Name, err)
				}
			}
		}
	default:
		legacyCandidates := []string{
			"idx_site_account_model",
			"idx_site_models_site_account_id_model_name",
		}
		for _, name := range legacyCandidates {
			if db.Migrator().HasIndex("site_models", name) {
				if err := db.Migrator().DropIndex("site_models", name); err != nil {
					return fmt.Errorf("failed to drop legacy site_models index %s: %w", name, err)
				}
			}
		}
	}

	if !db.Migrator().HasIndex("site_models", "idx_site_account_group_model") {
		if err := db.Exec("CREATE UNIQUE INDEX idx_site_account_group_model ON site_models(site_account_id, group_key, model_name)").Error; err != nil {
			return fmt.Errorf("failed to create site_models scoped unique index: %w", err)
		}
	}

	return nil
}
