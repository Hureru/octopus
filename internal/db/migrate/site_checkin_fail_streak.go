package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026080304,
		Up:      migrateSiteAccountCheckinFailStreak,
	})
}

func migrateSiteAccountCheckinFailStreak(db *gorm.DB) error {
	return db.AutoMigrate(&model.SiteAccount{})
}
