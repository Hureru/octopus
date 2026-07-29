package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateHeaderPolicyMetadataBackfillsLegacyRows(t *testing.T) {
	conn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := conn.AutoMigrate(&model.HeaderPolicy{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	policies := []model.HeaderPolicy{
		{Scope: model.HeaderPolicyScopeGlobal, Enabled: true},
		{
			Name: "Site policy", Version: 3,
			Scope: model.HeaderPolicyScopeSite, ScopeID: 7, Enabled: true,
		},
	}
	if err := conn.Create(&policies).Error; err != nil {
		t.Fatalf("seed header policies: %v", err)
	}
	if err := conn.Model(&model.HeaderPolicy{}).
		Where("id = ?", policies[0].ID).
		UpdateColumn("version", 0).Error; err != nil {
		t.Fatalf("force legacy version: %v", err)
	}

	if err := migrateHeaderPolicyMetadata(conn); err != nil {
		t.Fatalf("migrateHeaderPolicyMetadata: %v", err)
	}
	var got []model.HeaderPolicy
	if err := conn.Order("id ASC").Find(&got).Error; err != nil {
		t.Fatalf("load migrated policies: %v", err)
	}
	if got[0].Name != model.HeaderPolicyDefaultName(model.HeaderPolicyScopeGlobal, 0) ||
		got[0].Version != 1 {
		t.Fatalf("legacy metadata was not backfilled: %+v", got[0])
	}
	if got[1].Name != "Site policy" || got[1].Version != 3 {
		t.Fatalf("valid metadata was overwritten: %+v", got[1])
	}
	if err := migrateHeaderPolicyMetadata(conn); err != nil {
		t.Fatalf("migration rerun failed: %v", err)
	}
}
