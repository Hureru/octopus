package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 11,
		Up:      migrateWSResponseAffinity,
	})
}

func migrateWSResponseAffinity(db *gorm.DB) error {
	return db.AutoMigrate(&model.WSResponseAffinity{})
}
