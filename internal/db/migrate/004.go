package migrate

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 4,
		Up:      migrateSiteModelRoutesAndDropDisabledModels,
	})
}

func migrateSiteModelRoutesAndDropDisabledModels(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if db.Migrator().HasTable("site_models") {
		if !db.Migrator().HasColumn("site_models", "route_type") {
			if err := db.Exec("ALTER TABLE site_models ADD COLUMN route_type TEXT NOT NULL DEFAULT 'openai_chat'").Error; err != nil {
				return fmt.Errorf("failed to add site_models.route_type: %w", err)
			}
		}
		if !db.Migrator().HasColumn("site_models", "route_source") {
			if err := db.Exec("ALTER TABLE site_models ADD COLUMN route_source TEXT NOT NULL DEFAULT 'sync_inferred'").Error; err != nil {
				return fmt.Errorf("failed to add site_models.route_source: %w", err)
			}
		}
		if !db.Migrator().HasColumn("site_models", "manual_override") {
			if err := db.Exec("ALTER TABLE site_models ADD COLUMN manual_override BOOLEAN NOT NULL DEFAULT FALSE").Error; err != nil {
				return fmt.Errorf("failed to add site_models.manual_override: %w", err)
			}
		}
		if !db.Migrator().HasColumn("site_models", "route_raw_payload") {
			if err := db.Exec("ALTER TABLE site_models ADD COLUMN route_raw_payload TEXT NOT NULL DEFAULT ''").Error; err != nil {
				return fmt.Errorf("failed to add site_models.route_raw_payload: %w", err)
			}
		}
		if !db.Migrator().HasColumn("site_models", "route_updated_at") {
			if err := db.Exec("ALTER TABLE site_models ADD COLUMN route_updated_at DATETIME").Error; err != nil {
				return fmt.Errorf("failed to add site_models.route_updated_at: %w", err)
			}
		}
		if !db.Migrator().HasColumn("site_models", "disabled") {
			if err := db.Exec("ALTER TABLE site_models ADD COLUMN disabled BOOLEAN NOT NULL DEFAULT FALSE").Error; err != nil {
				return fmt.Errorf("failed to add site_models.disabled: %w", err)
			}
		}

		var rows []struct {
			ID        int
			ModelName string
		}
		if err := db.Table("site_models").Select("id", "model_name").Find(&rows).Error; err != nil {
			return fmt.Errorf("failed to query site_models for route backfill: %w", err)
		}
		for _, row := range rows {
			routeType := inferRouteTypeFromModelName(row.ModelName)
			if err := db.Exec(
				"UPDATE site_models SET route_type = ?, route_source = CASE WHEN TRIM(COALESCE(route_source, '')) = '' THEN 'sync_inferred' ELSE route_source END, manual_override = COALESCE(manual_override, FALSE), disabled = COALESCE(disabled, FALSE), route_updated_at = COALESCE(route_updated_at, CURRENT_TIMESTAMP) WHERE id = ?",
				routeType,
				row.ID,
			).Error; err != nil {
				return fmt.Errorf("failed to backfill site_models row %d: %w", row.ID, err)
			}
		}
	}

	if db.Migrator().HasTable("site_disabled_models") {
		if err := backfillLegacySiteDisabledModels(db); err != nil {
			return err
		}
		if err := db.Migrator().DropTable("site_disabled_models"); err != nil {
			return fmt.Errorf("failed to drop site_disabled_models: %w", err)
		}
	}

	return nil
}

func inferRouteTypeFromModelName(modelName string) string {
	return string(model.InferSiteModelRouteType(modelName))
}

func backfillLegacySiteDisabledModels(db *gorm.DB) error {
	type legacyDisabledModel struct {
		SiteID    int
		ModelName string
	}

	var rows []legacyDisabledModel
	if err := db.Table("site_disabled_models").Select("site_id", "model_name").Find(&rows).Error; err != nil {
		return fmt.Errorf("failed to query legacy site_disabled_models: %w", err)
	}

	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		modelName := strings.TrimSpace(row.ModelName)
		if row.SiteID == 0 || modelName == "" {
			continue
		}
		key := fmt.Sprintf("%d\x00%s", row.SiteID, modelName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		subQuery := db.Table("site_accounts").Select("id").Where("site_id = ?", row.SiteID)
		if err := db.Model(&model.SiteModel{}).
			Where("model_name = ? AND site_account_id IN (?)", modelName, subQuery).
			Update("disabled", true).Error; err != nil {
			return fmt.Errorf("failed to backfill legacy disabled model %q for site %d: %w", modelName, row.SiteID, err)
		}
	}

	return nil
}
