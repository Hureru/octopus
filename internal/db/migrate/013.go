package migrate

import (
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

// 分批大小经过保守取值：单批最多 500 行 × ~4 KB 主页面 ≈ 2 MB WAL 增长，
// 远低于 SQLite 默认 wal_autocheckpoint=1000 页 (~4 MB) 的触发阈值，
// 确保在 4 GB 容器内回填整张 relay_logs（哪怕表里有 GB 级 overflow 内容）
// 也不会把 WAL / page cache 顶满。
const relayLogSuccessBackfillBatchSize = 500

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 13,
		Up:      migrateRelayLogPerf,
	})
}

// migrateRelayLogPerf 给 relay_logs 加 success 列并回填历史数据。
// 出于内存安全考虑，此迁移刻意只做"能用窄 SQL 拍出来"的事情：
//   - relay_logs 不走 GORM AutoMigrate（见 internal/db/db.go 的注释），
//     避免 smart-migrate 触发 glebarez 的 recreateTable 全表拷贝。
//   - 三个性能索引由 op.RelayLogEnsureIndexes 在 server 启动后异步建，
//     使启动路径不被 GB 级表全表扫所阻塞。
func migrateRelayLogPerf(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("relay_logs") {
		return nil
	}

	start := time.Now()
	log.Infow("migration.relay_logs_perf.start")
	if err := ensureRelayLogSuccessColumn(db); err != nil {
		log.Errorw("migration.relay_logs_perf.failed", "duration", time.Since(start).String(), "step", "ensure_column", "error", err.Error())
		return err
	}
	if err := backfillRelayLogSuccess(db); err != nil {
		log.Errorw("migration.relay_logs_perf.failed", "duration", time.Since(start).String(), "step", "backfill", "error", err.Error())
		return err
	}
	log.Infow("migration.relay_logs_perf.done", "duration", time.Since(start).String())
	return nil
}

// ensureRelayLogSuccessColumn 用裸 SQL 加列，类型字面量刻意对齐 GORM SQLite
// dialector 给 bool 字段生成的 DDL（DataTypeOf=numeric + NOT NULL + DEFAULT false），
// 这样后续即便有别的地方再跑 db.AutoMigrate(&model.RelayLog{})，MigrateColumn
// 也不会把它判定成 schema drift 而触发 AlterColumn → recreateTable 全表拷贝。
func ensureRelayLogSuccessColumn(db *gorm.DB) error {
	if hasRelayLogColumn(db, "success") {
		return nil
	}
	log.Infow("migration.relay_logs_perf.add_column", "column", "success")
	return db.Exec("ALTER TABLE relay_logs ADD COLUMN success numeric NOT NULL DEFAULT false").Error
}

// backfillRelayLogSuccess 把历史日志里"error 为空"的行翻成 success=true。
// 全部新加的列默认就是 false，而 success=false 的失败行不需要再写一次，
// 所以子查询只命中"该翻而未翻"的行；这让重跑/重启完全等价于 no-op。
func backfillRelayLogSuccess(db *gorm.DB) error {
	total := int64(0)
	start := time.Now()
	for {
		// rowid 子查询 + LIMIT 把一次 UPDATE 的影响行数硬上限锁在 batchSize，
		// 避免一条 UPDATE 把整张表都纳入同一个写事务、把 WAL 一次性撑到 GB 级。
		result := db.Exec(`
UPDATE relay_logs
SET success = 1
WHERE rowid IN (
    SELECT rowid FROM relay_logs
    WHERE success = 0 AND (error = '' OR error IS NULL)
    LIMIT ?
)`, relayLogSuccessBackfillBatchSize)
		if result.Error != nil {
			return fmt.Errorf("failed to backfill relay_logs success: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			break
		}
		total += result.RowsAffected
		// 每 10k 行打一次进度，方便从外部观察迁移没卡死。
		if total%10000 < int64(relayLogSuccessBackfillBatchSize) {
			log.Infow("migration.relay_logs_perf.backfill_success.progress", "rows", total, "duration", time.Since(start).String())
		}
		if result.RowsAffected < int64(relayLogSuccessBackfillBatchSize) {
			break
		}
	}
	log.Infow("migration.relay_logs_perf.backfill_success.done", "rows", total, "duration", time.Since(start).String())
	return nil
}

// hasRelayLogColumn 不走 glebarez 的 HasColumn —— 后者用正则 LIKE 在 CREATE TABLE
// 文本里搜索列名，对名字短/常见的列容易撞误报。这里直接查 pragma_table_info。
// 非 sqlite 路径下回退到 GORM 的实现（其它驱动里 information_schema 查询是精确的）。
func hasRelayLogColumn(db *gorm.DB, column string) bool {
	if db == nil || strings.TrimSpace(column) == "" {
		return false
	}
	if db.Dialector != nil && db.Dialector.Name() == "sqlite" {
		var name string
		_ = db.Raw("SELECT name FROM pragma_table_info('relay_logs') WHERE name = ? LIMIT 1", column).Scan(&name).Error
		return name == column
	}
	var count int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_name = ? AND column_name = ?",
		"relay_logs", column,
	).Scan(&count).Error; err != nil {
		return false
	}
	return count > 0
}
