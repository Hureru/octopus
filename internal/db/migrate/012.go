package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 12,
		Up:      migrateChannelWSMode,
	})
}

func migrateChannelWSMode(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.Channel{}); err != nil {
		return err
	}
	return db.Model(&model.Channel{}).
		Where("ws_mode IS NULL OR ws_mode = ''").
		Update("ws_mode", string(model.ChannelWSModeInherit)).Error
}
