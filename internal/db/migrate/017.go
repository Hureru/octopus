package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 17,
		Up:      migrateUnifyAPIPlatform,
	})
}

func migrateUnifyAPIPlatform(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Site{}) {
		return nil
	}

	if !db.Migrator().HasColumn(&model.Site{}, "DefaultRouteType") {
		if err := db.Migrator().AddColumn(&model.Site{}, "DefaultRouteType"); err != nil {
			return err
		}
	}

	if err := db.Model(&model.Site{}).Where("platform = ?", "openai").Updates(map[string]any{
		"platform":           "api",
		"default_route_type": model.SiteModelRouteTypeOpenAIChat,
	}).Error; err != nil {
		return err
	}
	if err := db.Model(&model.Site{}).Where("platform = ?", "claude").Updates(map[string]any{
		"platform":           "api",
		"default_route_type": model.SiteModelRouteTypeAnthropic,
	}).Error; err != nil {
		return err
	}
	if err := db.Model(&model.Site{}).Where("platform = ?", "gemini").Updates(map[string]any{
		"platform":           "api",
		"default_route_type": model.SiteModelRouteTypeGemini,
	}).Error; err != nil {
		return err
	}
	return nil
}
