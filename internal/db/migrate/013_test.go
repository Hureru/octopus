package migrate

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sqlCaptureLogger 记下所有经过 GORM 的 SQL，用来断言迁移路径上没有发起
// recreateTable 那一组 SQL（CREATE TABLE relay_logs__temp / INSERT INTO ... SELECT *）。
type sqlCaptureLogger struct {
	mu         sync.Mutex
	statements []string
}

func (l *sqlCaptureLogger) LogMode(logger.LogLevel) logger.Interface { return l }
func (l *sqlCaptureLogger) Info(context.Context, string, ...interface{})  {}
func (l *sqlCaptureLogger) Warn(context.Context, string, ...interface{})  {}
func (l *sqlCaptureLogger) Error(context.Context, string, ...interface{}) {}
func (l *sqlCaptureLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statements = append(l.statements, sql)
}
func (l *sqlCaptureLogger) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.statements))
	copy(out, l.statements)
	return out
}
func (l *sqlCaptureLogger) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statements = l.statements[:0]
}

// containsRecreate 匹配 glebarez recreateTable 会发出的两条特征 SQL：
//   - CREATE TABLE `relay_logs__temp` ...
//   - INSERT INTO `relay_logs__temp`(...) SELECT ... FROM `relay_logs`
//
// 这两条只要出现一条，就说明触发了全表拷贝路径——这正是本次止血要避免的。
func containsRecreate(statements []string) (bool, string) {
	for _, s := range statements {
		lower := strings.ToLower(s)
		if strings.Contains(lower, "relay_logs__temp") {
			return true, s
		}
	}
	return false, ""
}

// TestMigrateRelayLogPerfBackfillsSuccess 验证迁移幂等地把 "error 为空" 的历史日志
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
// 关键回归保护：迁移过程不能触发 glebarez 的 recreateTable 全表拷贝。
func TestMigrateRelayLogPerfAddsMissingSuccessColumn(t *testing.T) {
	capture := &sqlCaptureLogger{}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: capture})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// 手工建一张 v0.8.25 样式的 relay_logs：包含 v0.8.25 模型已有的所有列
	// （transport_input_tokens / bill_input_tokens / cache_read_tokens /
	// cache_write_tokens / ws_mode / ws_exec_mode / ws_recovery 都在 v0.8.25 时
	// 已经加进来了），只缺一个 v0.8.27 新加的 success 列。
	// 列类型字面量刻意对齐 glebarez/sqlite dialector 给各字段生成的实际 DDL，
	// 否则 MigrateColumn 会按 schema drift 触发 AlterColumn → recreateTable。
	createSQL := `CREATE TABLE relay_logs (
		id integer,
		time integer,
		request_model_name text,
		request_api_key_name text,
		channel_id integer,
		channel_name text,
		actual_model_name text,
		input_tokens integer,
		transport_input_tokens integer,
		bill_input_tokens integer,
		cache_read_tokens integer,
		cache_write_tokens integer,
		output_tokens integer,
		ftut integer,
		use_time integer,
		cost real,
		request_content text,
		response_content text,
		error text,
		attempts text,
		total_attempts integer,
		used_ws numeric DEFAULT false,
		ws_mode text,
		ws_exec_mode text,
		ws_recovery text,
		PRIMARY KEY (id)
	)`
	if err := db.Exec(createSQL).Error; err != nil {
		t.Fatalf("create legacy relay_logs: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO relay_logs(id, time, request_model_name, error) VALUES (1, 100, 'ok', ''), (2, 200, 'bad', 'boom')",
	).Error; err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}

	// 只观察 migrateRelayLogPerf 自己发的 SQL，不要把建表/插数据混进来。
	capture.reset()
	if err := migrateRelayLogPerf(db); err != nil {
		t.Fatalf("migrateRelayLogPerf failed: %v", err)
	}

	// 关键不变量 #1：迁移过程没有触发 recreateTable。
	if found, sql := containsRecreate(capture.snapshot()); found {
		t.Fatalf("migrateRelayLogPerf unexpectedly triggered recreateTable: %s", sql)
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

	// 关键不变量 #2：迁移落定后再跑一次 GORM AutoMigrate(&model.RelayLog{}) 也不应
	// 触发 recreateTable。如果 ALTER TABLE 加的 success 列类型字面量与 GORM 的期望
	// 对不齐，MigrateColumn 会判定为 schema drift → AlterColumn →
	// glebarez recreateTable，直接把整张 GB 级表拷一遍——这就是本次止血要回归保护的。
	// 即使生产代码不会主动调（SQLite 路径上我们已经把 RelayLog 从 AutoMigrate
	// 列表里拿出来了），这个测试守护了 "success 列定义本身不会引发漂移" 的属性，
	// 防止后续重构不小心把它再加回 AutoMigrate 时立刻引爆。
	capture.reset()
	if err := db.AutoMigrate(&model.RelayLog{}); err != nil {
		t.Fatalf("post-migration AutoMigrate failed: %v", err)
	}
	if found, sql := containsRecreate(capture.snapshot()); found {
		t.Fatalf("post-migration AutoMigrate triggered recreateTable (smart-migrate misread schema): %s", sql)
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
