package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 14,
		Up:      migrateSiteModelHourlyLookupIndex,
	})
}

func migrateSiteModelHourlyLookupIndex(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("stats_site_model_hourlies") {
		return nil
	}
	if db.Migrator().HasIndex("stats_site_model_hourlies", "idx_stats_site_model_account_hour") {
		return nil
	}
	return db.Exec("CREATE INDEX idx_stats_site_model_account_hour ON stats_site_model_hourlies(site_account_id, hour)").Error
}
