package migrate

import (
	"fmt"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Migration struct {
	Version int
	Up      func(db *gorm.DB) error
}

type MigrationRecordStatus int

const (
	MigrationRecordStatusSuccess MigrationRecordStatus = 1
	MigrationRecordStatusFailed
)

type MigrationRecord struct {
	Version int `gorm:"primaryKey"`
	Status  MigrationRecordStatus
}

var beforeAutoMigrations = make([]Migration, 0)
var afterAutoMigrations = make([]Migration, 0)

func RegisterBeforeAutoMigration(m Migration) {
	beforeAutoMigrations = append(beforeAutoMigrations, m)
}

func RegisterAfterAutoMigration(m Migration) {
	afterAutoMigrations = append(afterAutoMigrations, m)
}

func BeforeAutoMigrate(db *gorm.DB) error {
	return runMigrationsWithRecord(db, beforeAutoMigrations)
}

func AfterAutoMigrate(db *gorm.DB) error {
	return runMigrationsWithRecord(db, afterAutoMigrations)
}

func runMigrationsWithRecord(db *gorm.DB, migrations []Migration) error {
	if len(migrations) == 0 {
		return nil
	}
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := ensureMigrationRecordTable(db); err != nil {
		return err
	}

	// 排序
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	// check duplicated versions
	seen := make(map[int]struct{}, len(migrations))
	versions := make([]int, 0, len(migrations))
	for _, m := range migrations {
		if _, ok := seen[m.Version]; ok {
			return fmt.Errorf("duplicated migration version: %d", m.Version)
		}
		seen[m.Version] = struct{}{}
		versions = append(versions, m.Version)
	}

	// load records in batch to avoid N queries
	existing := make([]MigrationRecord, 0)
	if err := db.Where("version IN ?", versions).Find(&existing).Error; err != nil {
		return fmt.Errorf("failed to query migration records: %w", err)
	}
	statusByVersion := make(map[int]MigrationRecordStatus, len(existing))
	for _, r := range existing {
		statusByVersion[r.Version] = r.Status
	}

	for _, m := range migrations {
		if m.Up == nil {
			return fmt.Errorf("migration %d has nil Up", m.Version)
		}

		// 已成功则跳过
		if st, ok := statusByVersion[m.Version]; ok && st == MigrationRecordStatusSuccess {
			continue
		}

		// 执行迁移
		if err := m.Up(db); err != nil {
			upsertMigrationRecord(db, m.Version, MigrationRecordStatusFailed)
			statusByVersion[m.Version] = MigrationRecordStatusFailed
			return fmt.Errorf("failed to run migration %d: %w", m.Version, err)
		}

		// 记录成功
		if err := upsertMigrationRecord(db, m.Version, MigrationRecordStatusSuccess); err != nil {
			return fmt.Errorf("failed to set migration %d success: %w", m.Version, err)
		}
		statusByVersion[m.Version] = MigrationRecordStatusSuccess
	}
	return nil
}

func ensureMigrationRecordTable(db *gorm.DB) error {
	if db.Migrator().HasTable(&MigrationRecord{}) {
		return nil
	}
	// For BeforeAutoMigrate: the record table may not exist yet.
	if err := db.AutoMigrate(&MigrationRecord{}); err != nil {
		return fmt.Errorf("failed to auto migrate MigrationRecord: %w", err)
	}
	return nil
}

func upsertMigrationRecord(db *gorm.DB, version int, status MigrationRecordStatus) error {
	rec := MigrationRecord{Version: version, Status: status}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "version"}},
		DoUpdates: clause.AssignmentColumns([]string{"status"}),
	}).Create(&rec).Error
}
