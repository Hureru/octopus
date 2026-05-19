package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026051901,
		Up:      migrateGroupHealthProbeModeColumn,
	})
}

func migrateGroupHealthProbeModeColumn(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.GroupHealthSnapshot{}); err != nil {
		return err
	}

	return db.Model(&model.GroupHealthSnapshot{}).
		Where("probe_mode IS NULL OR probe_mode = ''").
		Update("probe_mode", string(model.GroupHealthProbeModeStandard)).Error
}
