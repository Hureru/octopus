package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 15,
		Up:      migrateSiteUserGroupProjectionDisabled,
	})
}

func migrateSiteUserGroupProjectionDisabled(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteUserGroup{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.SiteUserGroup{}, "projection_disabled") {
		if err := db.Migrator().AddColumn(&model.SiteUserGroup{}, "ProjectionDisabled"); err != nil {
			return err
		}
	}
	return db.Model(&model.SiteUserGroup{}).
		Where("projection_disabled IS NULL").
		Update("projection_disabled", false).Error
}
