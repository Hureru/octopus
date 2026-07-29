package migrate

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 20,
		Up:      migrateHeaderPolicyMetadata,
	})
}

func migrateHeaderPolicyMetadata(conn *gorm.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	if !conn.Migrator().HasTable(&model.HeaderPolicy{}) {
		return nil
	}
	if !conn.Migrator().HasColumn(&model.HeaderPolicy{}, "Name") ||
		!conn.Migrator().HasColumn(&model.HeaderPolicy{}, "Version") {
		return fmt.Errorf("header policy metadata columns are missing after schema migration")
	}

	var policies []model.HeaderPolicy
	if err := conn.Find(&policies).Error; err != nil {
		return fmt.Errorf("load header policies: %w", err)
	}
	return conn.Transaction(func(tx *gorm.DB) error {
		for _, policy := range policies {
			updates := make(map[string]any, 2)
			if strings.TrimSpace(policy.Name) == "" {
				updates["name"] = model.HeaderPolicyDefaultName(policy.Scope, policy.ScopeID)
			}
			if policy.Version < 1 {
				updates["version"] = 1
			}
			if len(updates) == 0 {
				continue
			}
			if err := tx.Model(&model.HeaderPolicy{}).
				Where("id = ?", policy.ID).
				UpdateColumns(updates).Error; err != nil {
				return fmt.Errorf("backfill header policy %d metadata: %w", policy.ID, err)
			}
		}
		return nil
	})
}
