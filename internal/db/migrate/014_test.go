package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSiteModelHourlyLookupIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.StatsSiteModelHourly{}); err != nil {
		t.Fatalf("AutoMigrate StatsSiteModelHourly: %v", err)
	}
	if err := migrateSiteModelHourlyLookupIndex(db); err != nil {
		t.Fatalf("migrateSiteModelHourlyLookupIndex failed: %v", err)
	}
	if !db.Migrator().HasIndex("stats_site_model_hourlies", "idx_stats_site_model_account_hour") {
		t.Fatalf("expected lookup index to exist")
	}
}
