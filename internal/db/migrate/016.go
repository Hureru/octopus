package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 16,
		Up:      migrateSiteUserGroupProjectionSuspension,
	})
}

func migrateSiteUserGroupProjectionSuspension(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteUserGroup{}) {
		return nil
	}

	columns := []string{
		"ProjectionSuspended",
		"ProjectionSuspendReason",
		"ProjectionSuspendedAt",
		"ModelSyncStatus",
		"ModelSyncMessage",
		"ModelSyncAuthoritative",
		"ModelSyncModelCount",
		"LastModelSyncAt",
		"LastModelSyncSuccessAt",
		"ModelSyncFailureCount",
	}
	for _, column := range columns {
		if !db.Migrator().HasColumn(&model.SiteUserGroup{}, column) {
			if err := db.Migrator().AddColumn(&model.SiteUserGroup{}, column); err != nil {
				return err
			}
		}
	}

	return db.Model(&model.SiteUserGroup{}).Where("model_sync_status = '' OR model_sync_status IS NULL").Updates(map[string]any{
		"model_sync_status":        model.SiteGroupModelSyncStatusIdle,
		"projection_suspended":     false,
		"model_sync_authoritative": false,
		"model_sync_model_count":   0,
		"model_sync_failure_count": 0,
	}).Error
}
