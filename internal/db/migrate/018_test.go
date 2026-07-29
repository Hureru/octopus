package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateRelayLogOutcomeBackfillsWithoutReinterpretingCancellation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.RelayLog{}); err != nil {
		t.Fatalf("AutoMigrate RelayLog: %v", err)
	}

	rows := []model.RelayLog{
		{ID: 1, Time: 1, RequestModelName: "completed", Success: true},
		{ID: 2, Time: 2, RequestModelName: "canceled", Success: false, Error: "context canceled"},
		{ID: 3, Time: 3, RequestModelName: "known", Success: false, Outcome: model.RequestOutcomeClientCanceled},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("create relay logs: %v", err)
	}

	if err := migrateRelayLogOutcome(db); err != nil {
		t.Fatalf("migrateRelayLogOutcome failed: %v", err)
	}

	var got []model.RelayLog
	if err := db.Order("id ASC").Find(&got).Error; err != nil {
		t.Fatalf("query relay logs: %v", err)
	}
	if got[0].Outcome != model.RequestOutcomeSuccess {
		t.Fatalf("successful row outcome = %q", got[0].Outcome)
	}
	if got[1].Outcome != model.RequestOutcomeFailed || got[1].Error != "context canceled" {
		t.Fatalf("historical cancellation was reinterpreted: %+v", got[1])
	}
	if got[2].Outcome != model.RequestOutcomeClientCanceled {
		t.Fatalf("existing outcome was overwritten: %+v", got[2])
	}

	if err := migrateRelayLogOutcome(db); err != nil {
		t.Fatalf("migrateRelayLogOutcome rerun failed: %v", err)
	}
}
