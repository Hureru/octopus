package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 13,
		Up:      migrateRelayLogPerf,
	})
}

func migrateRelayLogPerf(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("relay_logs") {
		return nil
	}
	if err := db.AutoMigrate(&model.RelayLog{}); err != nil {
		return err
	}
	if err := db.Exec("UPDATE relay_logs SET success = CASE WHEN error = '' OR error IS NULL THEN ? ELSE ? END", true, false).Error; err != nil {
		return fmt.Errorf("failed to backfill relay_logs success: %w", err)
	}
	indexes := []struct {
		name string
		sql  string
	}{
		{name: "idx_relay_logs_time_id", sql: "CREATE INDEX idx_relay_logs_time_id ON relay_logs(time, id)"},
		{name: "idx_relay_logs_success_time_id", sql: "CREATE INDEX idx_relay_logs_success_time_id ON relay_logs(success, time, id)"},
		{name: "idx_relay_logs_channel_time_id", sql: "CREATE INDEX idx_relay_logs_channel_time_id ON relay_logs(channel_id, time, id)"},
		{name: "idx_relay_logs_request_model_time_id", sql: "CREATE INDEX idx_relay_logs_request_model_time_id ON relay_logs(request_model_name, time, id)"},
	}
	for _, index := range indexes {
		if db.Migrator().HasIndex("relay_logs", index.name) {
			continue
		}
		if err := db.Exec(index.sql).Error; err != nil {
			return fmt.Errorf("failed to create relay_logs index %s: %w", index.name, err)
		}
	}
	return nil
}
