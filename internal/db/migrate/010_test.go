package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateGroupHealthTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}

	if err := BeforeAutoMigrate(db); err != nil {
		t.Fatalf("BeforeAutoMigrate failed: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.Channel{},
		&model.ChannelKey{},
		&model.Site{},
		&model.SiteAccount{},
		&model.SiteToken{},
		&model.SiteUserGroup{},
		&model.SiteModel{},
		&model.SiteChannelBinding{},
		&model.Group{},
		&model.GroupItem{},
		&model.LLMInfo{},
		&model.APIKey{},
		&model.Setting{},
		&model.StatsTotal{},
		&model.StatsDaily{},
		&model.StatsHourly{},
		&model.StatsModel{},
		&model.StatsChannel{},
		&model.StatsAPIKey{},
		&model.StatsSiteModelHourly{},
		&model.GroupHealthSnapshot{},
		&model.GroupHealthAttempt{},
		&model.RelayLog{},
		&MigrationRecord{},
	); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	if err := AfterAutoMigrate(db); err != nil {
		t.Fatalf("AfterAutoMigrate failed: %v", err)
	}

	if !db.Migrator().HasTable("group_health_snapshots") {
		t.Fatal("expected group_health_snapshots table")
	}
	if !db.Migrator().HasTable("group_health_attempts") {
		t.Fatal("expected group_health_attempts table")
	}
}
