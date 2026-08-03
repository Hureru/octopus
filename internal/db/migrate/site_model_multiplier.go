package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026080303,
		Up:      migrateSiteModelPriceModelMultiplier,
	})
}

func migrateSiteModelPriceModelMultiplier(db *gorm.DB) error {
	return db.AutoMigrate(&model.SiteModelPriceQuote{})
}
