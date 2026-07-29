package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const sitePriceIdentityIndex = "ux_site_model_price_identity_key"

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 19,
		Up:      migrateSitePriceIdentity,
	})
}

func migrateSitePriceIdentity(conn *gorm.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	if !conn.Migrator().HasTable(&model.SiteModelPriceQuote{}) {
		return nil
	}
	if !conn.Migrator().HasColumn(&model.SiteModelPriceQuote{}, "IdentityKey") {
		return fmt.Errorf("site_model_price_quotes identity_key column is missing after schema migration")
	}

	var rows []model.SiteModelPriceQuote
	if err := conn.Order("observed_at DESC, id DESC").Find(&rows).Error; err != nil {
		return fmt.Errorf("load site price quotes: %w", err)
	}
	desiredKeys := make(map[int]string, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		rows[i].RefreshIdentityKey()
		key := rows[i].IdentityKey
		if _, duplicate := seen[key]; duplicate {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s|legacy=%d", key, rows[i].ID)))
			key = hex.EncodeToString(sum[:])
		}
		seen[key] = struct{}{}
		desiredKeys[rows[i].ID] = key
	}

	if err := conn.Transaction(func(tx *gorm.DB) error {
		for i := range rows {
			sum := sha256.Sum256([]byte(fmt.Sprintf("migration-019-temp|id=%d", rows[i].ID)))
			if err := tx.Model(&model.SiteModelPriceQuote{}).
				Where("id = ?", rows[i].ID).
				UpdateColumn("identity_key", hex.EncodeToString(sum[:])).Error; err != nil {
				return fmt.Errorf("stage site price quote %d identity: %w", rows[i].ID, err)
			}
		}
		for i := range rows {
			updates := map[string]any{"identity_key": desiredKeys[rows[i].ID]}
			if rows[i].Status == "" {
				updates["status"] = model.PriceQuoteStatusValid
			}
			if err := tx.Model(&model.SiteModelPriceQuote{}).
				Where("id = ?", rows[i].ID).
				UpdateColumns(updates).Error; err != nil {
				return fmt.Errorf("backfill site price quote %d identity: %w", rows[i].ID, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if conn.Migrator().HasIndex(&model.SiteModelPriceQuote{}, "idx_site_price_identity") {
		if err := conn.Migrator().DropIndex(&model.SiteModelPriceQuote{}, "idx_site_price_identity"); err != nil {
			return fmt.Errorf("drop nullable site price identity index: %w", err)
		}
	}
	if conn.Migrator().HasIndex(&model.SiteModelPriceQuote{}, sitePriceIdentityIndex) {
		return nil
	}
	if err := conn.Exec(
		"CREATE UNIQUE INDEX " + sitePriceIdentityIndex + " ON site_model_price_quotes(identity_key)",
	).Error; err != nil {
		return fmt.Errorf("create site price identity index: %w", err)
	}
	return nil
}
