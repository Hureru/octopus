package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 23,
		Up:      migrateCanonicalModelVendor,
	})
}

// migrateCanonicalModelVendor 为存量 Canonical Model 回填自动识别出的厂商，
// 让「模型发现」界面一上来就能按厂商筛选，而不用等下一次目录同步。
func migrateCanonicalModelVendor(conn *gorm.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	if !conn.Migrator().HasTable(&model.CanonicalModel{}) {
		return nil
	}
	if !conn.Migrator().HasColumn(&model.CanonicalModel{}, "Vendor") {
		return fmt.Errorf("canonical model vendor column is missing after schema migration")
	}

	var canonicals []model.CanonicalModel
	if err := conn.Where("vendor = '' OR vendor IS NULL").Find(&canonicals).Error; err != nil {
		return err
	}
	for _, canonical := range canonicals {
		vendor := modelvendor.Detect(canonical.Name)
		if vendor == "" {
			continue
		}
		if err := conn.Model(&model.CanonicalModel{}).
			Where("id = ?", canonical.ID).
			Update("vendor", vendor).Error; err != nil {
			return err
		}
	}
	return nil
}
