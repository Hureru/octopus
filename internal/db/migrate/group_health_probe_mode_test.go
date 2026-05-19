package migrate

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type legacyGroupHealthSnapshot struct {
	ID           int       `gorm:"primaryKey"`
	GroupID      int       `gorm:"index:idx_group_health_group_started"`
	GroupName    string    `gorm:"type:varchar(255);not null"`
	GroupMode    int       `gorm:"not null"`
	RequestModel string    `gorm:"type:varchar(255);not null"`
	Status       string    `gorm:"type:varchar(16);index:idx_group_health_status_started;not null"`
	StartedAt    time.Time `gorm:"index:idx_group_health_group_started;not null"`
	FinishedAt   *time.Time
	DurationMS   int64 `gorm:"not null;default:0"`
	Message      string
}

func (legacyGroupHealthSnapshot) TableName() string {
	return "group_health_snapshots"
}

func TestMigrateGroupHealthProbeModeColumnBackfillsLegacyRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}

	if err := db.AutoMigrate(&legacyGroupHealthSnapshot{}); err != nil {
		t.Fatalf("legacy AutoMigrate failed: %v", err)
	}

	legacy := &legacyGroupHealthSnapshot{
		GroupID:      1,
		GroupName:    "legacy-group",
		GroupMode:    int(model.GroupModeFailover),
		RequestModel: "legacy-group",
		Status:       string(model.GroupHealthStatusSuccess),
		StartedAt:    time.Now(),
	}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatalf("insert legacy snapshot failed: %v", err)
	}

	if err := migrateGroupHealthProbeModeColumn(db); err != nil {
		t.Fatalf("migrateGroupHealthProbeModeColumn failed: %v", err)
	}

	if !db.Migrator().HasColumn(&model.GroupHealthSnapshot{}, "probe_mode") {
		t.Fatal("expected probe_mode column after migration")
	}

	var stored model.GroupHealthSnapshot
	if err := db.First(&stored, legacy.ID).Error; err != nil {
		t.Fatalf("load migrated snapshot failed: %v", err)
	}
	if stored.ProbeMode != model.GroupHealthProbeModeStandard {
		t.Fatalf("expected migrated probe mode %q, got %q", model.GroupHealthProbeModeStandard, stored.ProbeMode)
	}
}

func TestMigrateGroupHealthProbeModeColumnKeepsExplicitMode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}

	if err := migrateGroupHealthProbeModeColumn(db); err != nil {
		t.Fatalf("migrateGroupHealthProbeModeColumn failed: %v", err)
	}

	snapshot := &model.GroupHealthSnapshot{
		GroupID:      1,
		GroupName:    "full-probe-group",
		GroupMode:    model.GroupModeFailover,
		ProbeMode:    model.GroupHealthProbeModeFull,
		RequestModel: "full-probe-group",
		Status:       model.GroupHealthStatusSuccess,
		StartedAt:    time.Now(),
	}
	if err := db.Create(snapshot).Error; err != nil {
		t.Fatalf("insert snapshot failed: %v", err)
	}

	if err := migrateGroupHealthProbeModeColumn(db); err != nil {
		t.Fatalf("rerun migrateGroupHealthProbeModeColumn failed: %v", err)
	}

	var stored model.GroupHealthSnapshot
	if err := db.First(&stored, snapshot.ID).Error; err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	if stored.ProbeMode != model.GroupHealthProbeModeFull {
		t.Fatalf("expected explicit probe mode %q, got %q", model.GroupHealthProbeModeFull, stored.ProbeMode)
	}
}
