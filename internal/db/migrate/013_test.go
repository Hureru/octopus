package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestMigrateRelayLogPerfBackfillsSuccess 验证迁移幂等地把"error 为空"的历史日志
// 翻成 success=true，且失败行保持 success=false。
func TestMigrateRelayLogPerfBackfillsSuccess(t *testing.T) {
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

	// 重跑必须是 no-op：第二次扫不到任何 success=0 且 error 空的行。
	if err := migrateRelayLogPerf(db); err != nil {
		t.Fatalf("migrateRelayLogPerf rerun failed: %v", err)
	}
}

// TestMigrateRelayLogPerfAddsMissingSuccessColumn 模拟 v0.8.25 → v0.8.27 升级路径：
// 老库的 relay_logs 没有 success 列；迁移要能用裸 ALTER TABLE 把列加上来。
// 这条路径之前完全没有测试覆盖，是这次止血的关键回归测试。
func TestMigrateRelayLogPerfAddsMissingSuccessColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 手工建一张不含 success 列的 relay_logs，模拟 v0.8.25 的 schema 残留。
	createSQL := `CREATE TABLE relay_logs (
		id INTEGER PRIMARY KEY,
		time INTEGER,
		request_model_name TEXT,
		request_api_key_name TEXT,
		channel_id INTEGER,
		channel_name TEXT,
		actual_model_name TEXT,
		input_tokens INTEGER,
		output_tokens INTEGER,
		ftut INTEGER,
		use_time INTEGER,
		cost REAL,
		request_content TEXT,
		response_content TEXT,
		error TEXT,
		attempts TEXT,
		total_attempts INTEGER,
		used_ws numeric DEFAULT false
	)`
	if err := db.Exec(createSQL).Error; err != nil {
		t.Fatalf("create legacy relay_logs: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO relay_logs(id, time, request_model_name, error) VALUES (1, 100, 'ok', ''), (2, 200, 'bad', 'boom')",
	).Error; err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}

	if err := migrateRelayLogPerf(db); err != nil {
		t.Fatalf("migrateRelayLogPerf failed: %v", err)
	}

	// 列存在；老数据被正确翻成 success。
	if !hasRelayLogColumn(db, "success") {
		t.Fatalf("success column not added")
	}
	type row struct {
		ID      int64
		Success bool
	}
	var got []row
	if err := db.Raw("SELECT id, success FROM relay_logs ORDER BY id ASC").Scan(&got).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got) != 2 || !got[0].Success || got[1].Success {
		t.Fatalf("unexpected backfill state: %+v", got)
	}

	// 关键：之后再跑一次 GORM AutoMigrate(&model.RelayLog{}) 必须不触发
	// glebarez 的 recreateTable —— 否则就把 GB 级表全表拷一遍重新踩坑。
	// 通过断言 sqlite_master.sql 里没有 relay_logs__temp、且重跑前后行数不变来验证。
	if err := db.AutoMigrate(&model.RelayLog{}); err != nil {
		t.Fatalf("post-migration AutoMigrate failed: %v", err)
	}
	var tempCount int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name LIKE 'relay_logs__%'",
	).Scan(&tempCount).Error; err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if tempCount != 0 {
		t.Fatalf("post-migration AutoMigrate left orphan rebuild tables (count=%d): smart-migrate triggered recreateTable", tempCount)
	}
	var afterCount int64
	if err := db.Table("relay_logs").Count(&afterCount).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if afterCount != 2 {
		t.Fatalf("row count changed after AutoMigrate: got %d, want 2", afterCount)
	}
}

// TestMigrateRelayLogPerfDoesNotCreateIndexes 守护契约：性能索引由
// op.RelayLogEnsureIndexes 异步建，启动路径上的 migration 013 不能再碰它们。
func TestMigrateRelayLogPerfDoesNotCreateIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.RelayLog{}); err != nil {
		t.Fatalf("AutoMigrate RelayLog: %v", err)
	}

	if err := migrateRelayLogPerf(db); err != nil {
		t.Fatalf("migrateRelayLogPerf failed: %v", err)
	}

	for _, name := range []string{
		"idx_relay_logs_time_id",
		"idx_relay_logs_success_time_id",
		"idx_relay_logs_channel_time_id",
	} {
		if db.Migrator().HasIndex("relay_logs", name) {
			t.Fatalf("migration 013 must not create index %s; that work belongs to op.RelayLogEnsureIndexes", name)
		}
	}
}
