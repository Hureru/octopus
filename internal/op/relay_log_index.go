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
var relayLogPerfIndexes = []struct {
	name string
	sql  string
}{
	{name: "idx_relay_logs_time_id", sql: "CREATE INDEX idx_relay_logs_time_id ON relay_logs(time, id)"},
	{name: "idx_relay_logs_success_time_id", sql: "CREATE INDEX idx_relay_logs_success_time_id ON relay_logs(success, time, id) WHERE success = 1"},
	{name: "idx_relay_logs_channel_time_id", sql: "CREATE INDEX idx_relay_logs_channel_time_id ON relay_logs(channel_id, time, id)"},
}

// RelayLogEnsureIndexes 确保 relay_logs 的性能索引存在。幂等：已存在的索引会跳过。
// 调用方应在 server.Start 之后 safe.Go 调用，避免阻塞启动路径。
func RelayLogEnsureIndexes(ctx context.Context) {
	if db.GetDB() == nil {
		return
	}
	start := time.Now()
	log.Infow("relay_log.ensure_indexes.start")

	created := 0
	for _, index := range relayLogPerfIndexes {
		select {
		case <-ctx.Done():
			log.Warnw("relay_log.ensure_indexes.canceled", "duration", time.Since(start).String(), "created", created)
			return
		default:
		}
		dbConn := db.GetDB().WithContext(ctx)
		if dbConn.Migrator().HasIndex("relay_logs", index.name) {
			continue
		}
		idxStart := time.Now()
		log.Infow("relay_log.ensure_indexes.create.start", "index", index.name)
		if err := dbConn.Exec(index.sql).Error; err != nil {
			// 不致命：缺失的索引意味着相关查询走全表扫，慢但不会出错。
			// 上层不需要 fatal exit；下次启动会再次尝试。
			log.Errorw("relay_log.ensure_indexes.create.failed", "index", index.name, "duration", time.Since(idxStart).String(), "error", err.Error())
			return
		}
		created++
		log.Infow("relay_log.ensure_indexes.create.done", "index", index.name, "duration", time.Since(idxStart).String())
	}
	log.Infow("relay_log.ensure_indexes.done", "duration", time.Since(start).String(), "created", created)
}

// RelayLogEnsureIndexesSync 同步版本，主要给测试使用。
// 与 RelayLogEnsureIndexes 行为相同但返回第一个出错信息，方便断言。
func RelayLogEnsureIndexesSync(ctx context.Context) error {
	if db.GetDB() == nil {
		return fmt.Errorf("db not initialized")
	}
	for _, index := range relayLogPerfIndexes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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
