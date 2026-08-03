package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026080301,
		Up:      migrateSiteIsReserveColumn,
	})
}

func migrateSiteIsReserveColumn(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.Site{}); err != nil {
		return err
	}

	return db.Model(&model.Site{}).
		Where("is_reserve IS NULL").
		Update("is_reserve", false).Error
}
