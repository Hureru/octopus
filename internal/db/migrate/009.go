package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 9,
		Up:      migrateSiteModelHourlyAddLastRequestAt,
	})
}

// 009:
// - add stats_site_model_hourlies.last_request_at if missing
//   该列承载该小时桶内最后一次请求的精确 unix 秒，用于站点渠道页"最近请求时间"的秒级展示。
//   老数据保持 0，由读取路径在 LastRequestAt=0 时回退到 (latestHour+1)*3600-1 的旧近似值。
func migrateSiteModelHourlyAddLastRequestAt(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("stats_site_model_hourlies") {
		return nil
	}

	if !db.Migrator().HasColumn("stats_site_model_hourlies", "last_request_at") {
		if err := db.Exec("ALTER TABLE stats_site_model_hourlies ADD COLUMN last_request_at INTEGER NOT NULL DEFAULT 0").Error; err != nil {
			return fmt.Errorf("failed to add stats_site_model_hourlies.last_request_at: %w", err)
		}
	}

	return nil
}
