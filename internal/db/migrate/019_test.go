package migrate

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSitePriceIdentityBackfillsNullableScopes(t *testing.T) {
	conn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := conn.AutoMigrate(&model.SiteModelPriceQuote{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	now := time.Now()
	rows := []model.SiteModelPriceQuote{
		{SiteID: 1, ModelName: "gpt-4o", Source: model.PriceQuoteSourceSiteWide, ObservedAt: now},
		{SiteID: 2, ModelName: "gpt-4o", Source: model.PriceQuoteSourceSiteWide, ObservedAt: now},
		{SiteID: 1, ModelName: "gpt-4o", Source: model.PriceQuoteSourceSiteWide, ObservedAt: now.Add(-time.Hour)},
	}
	if err := conn.Create(&rows).Error; err != nil {
		t.Fatalf("seed quotes: %v", err)
	}

	if err := migrateSitePriceIdentity(conn); err != nil {
		t.Fatalf("migrateSitePriceIdentity: %v", err)
	}
	var got []model.SiteModelPriceQuote
	if err := conn.Order("id ASC").Find(&got).Error; err != nil {
		t.Fatalf("load migrated quotes: %v", err)
	}
	seen := make(map[string]struct{}, len(got))
	for _, item := range got {
		if item.IdentityKey == "" {
			t.Fatalf("quote %d has empty identity key", item.ID)
		}
		if _, duplicate := seen[item.IdentityKey]; duplicate {
			t.Fatalf("duplicate identity key after migration: %s", item.IdentityKey)
		}
		seen[item.IdentityKey] = struct{}{}
	}
	if !conn.Migrator().HasIndex(&model.SiteModelPriceQuote{}, sitePriceIdentityIndex) {
		t.Fatal("unique identity index was not created")
	}
	if err := migrateSitePriceIdentity(conn); err != nil {
		t.Fatalf("migration rerun failed: %v", err)
	}
}
