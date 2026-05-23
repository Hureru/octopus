package op

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// relayLogPerfIndexes 列出 relay_logs 的性能索引。
// 这些索引此前在 migration 013 同步创建，但每个 CREATE INDEX 都要全表扫描；
// 当 relay_logs 含 GB 级 request/response_content 时，三次全表扫加上 page cache
// 会把容器内存顶满，直接 OOMKill。改为 server 起来后异步建：
//   - 启动路径不再被建索引阻塞，迁移状态可以正常落库（不会再陷入 OOM 重启循环）。
//   - 建索引期间，相关查询走全表扫但仍能返回，只是慢一点；建完即恢复。
//
// 索引不带 partial WHERE 谓词，三个 dialect 通用，"success=true / =false"
// 两个方向的过滤都能命中 idx_relay_logs_success_time_id。
var relayLogPerfIndexes = []struct {
	name string
	sql  string
}{
	{name: "idx_relay_logs_time_id", sql: "CREATE INDEX idx_relay_logs_time_id ON relay_logs(time, id)"},
	{name: "idx_relay_logs_success_time_id", sql: "CREATE INDEX idx_relay_logs_success_time_id ON relay_logs(success, time, id)"},
	{name: "idx_relay_logs_channel_time_id", sql: "CREATE INDEX idx_relay_logs_channel_time_id ON relay_logs(channel_id, time, id)"},
}

const (
	// relayLogIndexStartupDelay 让 server 完成 InitCache、handler 注册、连接池预热
	// 之后再开始建索引。SQLite 上 MaxOpenConns=1，CREATE INDEX 期间任何写请求都会排队，
	// 让出几秒给冷启动后的第一波流量减少抖动。
	relayLogIndexStartupDelay = 5 * time.Second

	// relayLogIndexCooldown 单个 CREATE INDEX 之间留出的让步窗口。
	// SQLite 上整个连接池只有 1 个写连接（见 internal/db/db.go），CREATE INDEX
	// 期间会独占这个连接；relay log writer / stats save / 管理端 API 都会排队。
	// 在两个索引之间 sleep 让出片刻，避免连续三次 CREATE INDEX 把请求路径整体卡死。
	relayLogIndexCooldown = 500 * time.Millisecond
)

// RelayLogEnsureIndexes 确保 relay_logs 的性能索引存在。幂等：已存在的索引会跳过。
// 调用方应在 server.Start 之后 safe.Go 调用，避免阻塞启动路径。
//
// ctx 应该绑定 shutdown 的取消链路：
//   - 起步前的 warmup sleep 立刻退出。
//   - 进行中的 CREATE INDEX 会通过 GORM/database/sql 的 ctx 链路向下传到
//     modernc.org/sqlite，最终走 sqlite3_interrupt 让 SQLite 在下一个安全检查点
//     放弃当前操作（实测对 CREATE INDEX 通常在 100ms 内响应）。
//
// 任何一个 CREATE INDEX 失败都不致命：缺失的索引意味着相关查询走全表扫，
// 慢但不会报错；下次启动会再次尝试缺失的那些。
func RelayLogEnsureIndexes(ctx context.Context) {
	if db.GetDB() == nil {
		return
	}
	// 启动前 warmup：让出 server 起步阶段的连接，让 InitCache 完成、handler 接住
	// 第一波请求之后再开始抢 SQLite 写连接。
	if !sleepWithCtx(ctx, relayLogIndexStartupDelay) {
		log.Infow("relay_log.ensure_indexes.canceled_before_start")
		return
	}

	start := time.Now()
	log.Infow("relay_log.ensure_indexes.start")

	created := 0
	for i, index := range relayLogPerfIndexes {
		if ctx.Err() != nil {
			log.Warnw("relay_log.ensure_indexes.canceled", "duration", time.Since(start).String(), "created", created)
			return
		}
		dbConn := db.GetDB().WithContext(ctx)
		if dbConn.Migrator().HasIndex("relay_logs", index.name) {
			continue
		}
		// 第二个及以后的实际建索引前先让出 SQLite 写连接，给请求路径喘息时间。
		if i > 0 {
			if !sleepWithCtx(ctx, relayLogIndexCooldown) {
				log.Warnw("relay_log.ensure_indexes.canceled", "duration", time.Since(start).String(), "created", created)
				return
			}
		}
		idxStart := time.Now()
		log.Infow("relay_log.ensure_indexes.create.start", "index", index.name)
		// 这里依然用绑定了外层 ctx 的 dbConn，shutdown 时能透传到 sqlite3_interrupt。
		if err := dbConn.Exec(index.sql).Error; err != nil {
			if ctx.Err() != nil {
				log.Infow("relay_log.ensure_indexes.create.interrupted", "index", index.name, "duration", time.Since(idxStart).String())
				return
			}
			log.Errorw("relay_log.ensure_indexes.create.failed", "index", index.name, "duration", time.Since(idxStart).String(), "error", err.Error())
			return
		}
		created++
		log.Infow("relay_log.ensure_indexes.create.done", "index", index.name, "duration", time.Since(idxStart).String())
	}
	log.Infow("relay_log.ensure_indexes.done", "duration", time.Since(start).String(), "created", created)
}

// RelayLogEnsureIndexesSync 同步版本，主要给测试使用。
// 与 RelayLogEnsureIndexes 行为相同但跳过 warmup/cooldown，不返回 nil 而是把
// 第一个出错信息往上传，方便断言。
func RelayLogEnsureIndexesSync(ctx context.Context) error {
	if db.GetDB() == nil {
		return fmt.Errorf("db not initialized")
	}
	for _, index := range relayLogPerfIndexes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		dbConn := db.GetDB().WithContext(ctx)
		if dbConn.Migrator().HasIndex("relay_logs", index.name) {
			continue
		}
		if err := dbConn.Exec(index.sql).Error; err != nil {
			return fmt.Errorf("create relay_logs index %s: %w", index.name, err)
		}
	}
	return nil
}

// sleepWithCtx 在 ctx 取消时立刻返回 false，避免 shutdown 路径上还得等 sleep 跑完。
func sleepWithCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
