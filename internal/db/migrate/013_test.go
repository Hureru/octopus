package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateRelayLogPerfBackfillsSuccessAndCreatesIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.RelayLog{}); err != nil {
		t.Fatalf("AutoMigrate RelayLog: %v", err)
	}
	rows := []model.RelayLog{
		{ID: 1, Time: 1, RequestModelName: "ok", Error: ""},
		{ID: 2, Time: 2, RequestModelName: "bad", Error: "failed"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("create relay logs: %v", err)
	}

	if err := migrateRelayLogPerf(db); err != nil {
		t.Fatalf("migrateRelayLogPerf failed: %v", err)
	}

	var reloaded []model.RelayLog
	if err := db.Order("id ASC").Find(&reloaded).Error; err != nil {
		t.Fatalf("query relay logs: %v", err)
	}
	if len(reloaded) != 2 || !reloaded[0].Success || reloaded[1].Success {
		t.Fatalf("unexpected success backfill: %+v", reloaded)
	}
	for _, name := range []string{
		"idx_relay_logs_time_id",
		"idx_relay_logs_success_time_id",
		"idx_relay_logs_channel_time_id",
	} {
		if !db.Migrator().HasIndex("relay_logs", name) {
			t.Fatalf("expected index %s to exist", name)
		}
	}
}
