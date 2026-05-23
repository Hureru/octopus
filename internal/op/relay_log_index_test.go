package op

import (
	"context"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
)

// TestRelayLogEnsureIndexesIdempotent 验证：
//  1. 头一次跑能把三个性能索引建出来；
//  2. 重复调用不会报错也不会重复建（幂等）；
//  3. 关键：迁移路径上不再建索引，依赖这条 op 函数作为唯一入口。
func TestRelayLogEnsureIndexesCreatesAndIsIdempotent(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	// 初始 InitDB 完成后，relay_logs 表已经存在（含 success 列，因为 migration 013
	// 在 InitDB 末尾跑过了），但启动期已不再同步建索引。
	for _, name := range []string{
		"idx_relay_logs_time_id",
		"idx_relay_logs_success_time_id",
		"idx_relay_logs_channel_time_id",
	} {
		if dbpkg.GetDB().Migrator().HasIndex("relay_logs", name) {
			t.Fatalf("startup path unexpectedly created index %s; that's the OOM regression we're trying to avoid", name)
		}
	}

	if err := RelayLogEnsureIndexesSync(ctx); err != nil {
		t.Fatalf("first RelayLogEnsureIndexesSync failed: %v", err)
	}

	for _, name := range []string{
		"idx_relay_logs_time_id",
		"idx_relay_logs_success_time_id",
		"idx_relay_logs_channel_time_id",
	} {
		if !dbpkg.GetDB().Migrator().HasIndex("relay_logs", name) {
			t.Fatalf("expected index %s to exist after RelayLogEnsureIndexesSync", name)
		}
	}

	// 第二次必须无副作用：HasIndex 命中后直接跳过，不会 CREATE INDEX 再触发一次全表扫。
	if err := RelayLogEnsureIndexesSync(ctx); err != nil {
		t.Fatalf("second RelayLogEnsureIndexesSync failed: %v", err)
	}
}

// TestRelayLogEnsureIndexesAsyncRespectsContext 验证异步入口在 ctx 取消时
// 能干净退出，不会在容器关停时仍然继续跑长尾的 CREATE INDEX。
func TestRelayLogEnsureIndexesAsyncRespectsContext(t *testing.T) {
	_ = setupSiteOpTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 提前取消

	// 不应 panic，也不应该阻塞——直接 return。
	RelayLogEnsureIndexes(ctx)

	// 取消的 ctx 下不应该建出索引。
	for _, name := range []string{
		"idx_relay_logs_time_id",
		"idx_relay_logs_success_time_id",
		"idx_relay_logs_channel_time_id",
	} {
		if dbpkg.GetDB().Migrator().HasIndex("relay_logs", name) {
			t.Fatalf("index %s should not be created when ctx is already canceled", name)
		}
	}
}
