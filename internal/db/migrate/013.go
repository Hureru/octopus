package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

const relayLogSuccessBackfillBatchSize = 5000

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
	if err := backfillRelayLogSuccess(db); err != nil {
		return err
	}
	indexes := []struct {
		name string
		sql  string
	}{
		{name: "idx_relay_logs_time_id", sql: "CREATE INDEX idx_relay_logs_time_id ON relay_logs(time, id)"},
		{name: "idx_relay_logs_success_time_id", sql: "CREATE INDEX idx_relay_logs_success_time_id ON relay_logs(success, time, id)"},
		{name: "idx_relay_logs_channel_time_id", sql: "CREATE INDEX idx_relay_logs_channel_time_id ON relay_logs(channel_id, time, id)"},
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

func backfillRelayLogSuccess(db *gorm.DB) error {
	lastID := int64(0)
	for {
		var ids []int64
		if err := db.Model(&model.RelayLog{}).
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(relayLogSuccessBackfillBatchSize).
			Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("failed to scan relay_logs for success backfill: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.Model(&model.RelayLog{}).
			Where("id IN ?", ids).
			Update("success", gorm.Expr("CASE WHEN error = '' OR error IS NULL THEN ? ELSE ? END", true, false)).Error; err != nil {
			return fmt.Errorf("failed to backfill relay_logs success: %w", err)
		}
		lastID = ids[len(ids)-1]
		if len(ids) < relayLogSuccessBackfillBatchSize {
			return nil
		}
	}
}
