package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026080302,
		Up:      migrateChannelIsReserveColumn,
	})
}

func migrateChannelIsReserveColumn(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.Channel{}); err != nil {
		return err
	}

	return db.Model(&model.Channel{}).
		Where("is_reserve IS NULL").
		Update("is_reserve", false).Error
}
