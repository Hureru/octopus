package op

import (
	"context"
	"testing"
	"time"

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

// TestRelayLogEnsureIndexesAsyncCancelsImmediately 验证：异步入口在 ctx 已经
// 取消的情况下立刻返回，且 5s 的 warmup sleep 不会让它白等。
//
// 这条测试同时验证 shutdown 路径——容器停机时 ctx 被 cancel，goroutine 必须
// 在 100ms 量级退出，否则 db.Close() 会被卡住。
func TestRelayLogEnsureIndexesAsyncCancelsImmediately(t *testing.T) {
	_ = setupSiteOpTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 进入函数前已取消

	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		RelayLogEnsureIndexes(ctx)
	}()
	select {
	case <-done:
		// 必须远低于 relayLogIndexStartupDelay (5s)：sleepWithCtx 应该立刻看到 ctx.Done。
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("RelayLogEnsureIndexes took %s after canceled ctx; warmup sleep is not honoring cancellation", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RelayLogEnsureIndexes did not return within 2s after ctx cancel")
	}

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

// TestRelayLogEnsureIndexesAsyncCancelsDuringWarmup 验证：刚启动还在 warmup
// sleep 里就被 cancel，goroutine 同样要立刻退出（这是 docker stop 落在 server
// 刚起来 5s 内的真实场景）。
func TestRelayLogEnsureIndexesAsyncCancelsDuringWarmup(t *testing.T) {
	_ = setupSiteOpTestDB(t)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		RelayLogEnsureIndexes(ctx)
	}()

	// 进入 warmup sleep 后再取消，模拟 docker stop 落在启动后没几秒。
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("RelayLogEnsureIndexes took %s to exit after ctx cancel during warmup", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RelayLogEnsureIndexes blocked through warmup despite ctx cancel")
	}
}

