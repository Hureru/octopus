package db

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

// sqlCaptureLogger 记下 GORM 发出的所有 SQL，用于断言 schema 变更路径上没有
// 触发 glebarez 的 recreateTable（即 CREATE TABLE relay_logs__temp 那条特征）。
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

// TestEnsureRelayLogColumnsSQLite_AddsMissingWithoutRecreate 模拟 v0.8.25
// 留下的 relay_logs（25 列、无 success）。ensureRelayLogColumnsSQLite 必须：
//  1. 把缺失的 success 列加上来；
//  2. 全程不发出任何 recreateTable 特征 SQL。
func TestEnsureRelayLogColumnsSQLite_AddsMissingWithoutRecreate(t *testing.T) {
	capture := &sqlCaptureLogger{}
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: capture})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// v0.8.25 列集合：除了 success，其它全有。
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
	if err := gormDB.Exec(createSQL).Error; err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	capture.reset()
	if err := ensureRelayLogColumnsSQLite(gormDB); err != nil {
		t.Fatalf("ensureRelayLogColumnsSQLite failed: %v", err)
	}

	// success 列被加上。
	var name string
	if err := gormDB.Raw(
		"SELECT name FROM pragma_table_info('relay_logs') WHERE name = ? LIMIT 1",
		"success",
	).Scan(&name).Error; err != nil {
		t.Fatalf("read columns: %v", err)
	}
	if name != "success" {
		t.Fatalf("success column not added by ensureRelayLogColumnsSQLite")
	}

	// 没发出过 recreateTable 特征 SQL。
	for _, sql := range capture.snapshot() {
		if strings.Contains(strings.ToLower(sql), "relay_logs__temp") {
			t.Fatalf("ensureRelayLogColumnsSQLite triggered recreateTable: %s", sql)
		}
	}
}

// TestEnsureRelayLogColumnsSQLite_NoopOnCurrentSchema 验证幂等：
// 在 model 已声明的完整 schema 上重复跑不会发出任何 ALTER TABLE。
func TestEnsureRelayLogColumnsSQLite_NoopOnCurrentSchema(t *testing.T) {
	capture := &sqlCaptureLogger{}
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: capture})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gormDB.AutoMigrate(&model.RelayLog{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	capture.reset()
	if err := ensureRelayLogColumnsSQLite(gormDB); err != nil {
		t.Fatalf("ensureRelayLogColumnsSQLite failed: %v", err)
	}
	for _, sql := range capture.snapshot() {
		upper := strings.ToUpper(sql)
		if strings.Contains(upper, "ALTER TABLE") || strings.Contains(upper, "RELAY_LOGS__TEMP") {
			t.Fatalf("ensureRelayLogColumnsSQLite must be a no-op on current schema, but emitted: %s", sql)
		}
	}
}

// 升级路径：旧库的统计表没有缓存列，AutoMigrate 补列后旧行必须读得出来（新列为 0 而不是 NULL）。
func TestAutoMigrateAddsStatsCacheColumnsToExistingRows(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, sql := range []string{
		"CREATE TABLE stats_totals (id integer PRIMARY KEY, input_token bigint, output_token bigint, input_cost real, output_cost real, wait_time bigint, request_success bigint, request_failed bigint)",
		"INSERT INTO stats_totals (id, input_token, output_token) VALUES (1, 100, 20)",
	} {
		if err := gormDB.Exec(sql).Error; err != nil {
			t.Fatalf("seed legacy table: %v", err)
		}
	}

	if err := gormDB.AutoMigrate(&model.StatsTotal{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	var got model.StatsTotal
	if err := gormDB.First(&got, 1).Error; err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if got.InputToken != 100 || got.OutputToken != 20 || got.CacheReadToken != 0 || got.CacheWriteToken != 0 {
		t.Fatalf("unexpected legacy row after migrate: %+v", got.StatsMetrics)
	}
}
