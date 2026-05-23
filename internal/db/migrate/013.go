package migrate

import (
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

// 分批大小经过保守取值：单批最多 500 行 × ~4 KB 主页面 ≈ 2 MB WAL 增长，
// 远低于 SQLite 默认 wal_autocheckpoint=1000 页 (~4 MB) 的触发阈值。
// 仅用于 SQLite 路径；MySQL/Postgres 在单条 UPDATE 里走原生 row-level lock，
// 不存在 WAL 堆积问题，所以那两个 dialect 直接一条 UPDATE 完事。
const relayLogSuccessBackfillBatchSize = 500

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 13,
		Up:      migrateRelayLogPerf,
	})
}

// migrateRelayLogPerf 给 relay_logs 加 success 列并回填历史数据。
// 出于内存安全考虑，此迁移在不同 dialect 上策略不同：
//   - SQLite：用裸 ALTER TABLE 加列、按 id 分批 UPDATE 回填，避免触发 glebarez
//     的 AlterColumn → recreateTable 全表拷贝（GB 级表会直接 OOM）。
//   - MySQL/Postgres：smart-migrate 在这两个 dialect 上是安全的（用原生
//     ALTER COLUMN），所以直接 db.AutoMigrate(&model.RelayLog{}) 加列，
//     然后一条 UPDATE 把 error 空的历史行翻成 success=true。
//
// 性能索引的创建由 op.RelayLogEnsureIndexes 在 server 启动后异步完成，
// 使启动路径不被 GB 级表全表扫所阻塞。
func migrateRelayLogPerf(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("relay_logs") {
		return nil
	}

	start := time.Now()
	log.Infow("migration.relay_logs_perf.start", "dialect", db.Dialector.Name())
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

// ensureRelayLogSuccessColumn 按 dialect 分别处理 success 列的存在性。
//
//   - SQLite：用裸 SQL，列类型字面量刻意对齐 GORM SQLite dialector 给
//     bool not null;default:false 字段生成的 "numeric NOT NULL DEFAULT false"。
//     这样后续即便有别的地方再跑 db.AutoMigrate(&model.RelayLog{})，
//     MigrateColumn 也不会判定为 schema drift 而触发 AlterColumn →
//     recreateTable 把整张 GB 级表拷贝一遍。
//   - MySQL/Postgres：用 GORM 的 AutoMigrate 让它按本 dialect 的 bool 表示
//     建列（MySQL → tinyint(1)，Postgres → boolean），保证后续 WHERE success = ?
//     的隐式类型匹配按各 dialect 的正常路径走。这两个 dialect 的 migrator
//     用原生 ALTER COLUMN，不会做全表拷贝，所以是安全的。
func ensureRelayLogSuccessColumn(db *gorm.DB) error {
	if hasRelayLogColumn(db, "success") {
		return nil
	}
	dialect := db.Dialector.Name()
	log.Infow("migration.relay_logs_perf.add_column", "column", "success", "dialect", dialect)
	if dialect == "sqlite" {
		return db.Exec("ALTER TABLE relay_logs ADD COLUMN success numeric NOT NULL DEFAULT false").Error
	}
	// MySQL / Postgres：交给 GORM AutoMigrate，它会按 dialect 生成正确的 ADD COLUMN。
	return db.AutoMigrate(&model.RelayLog{})
}

// backfillRelayLogSuccess 把历史日志里 "error 为空" 的行翻成 success=true。
// 全部新加的列默认就是 false，而 success=false 的失败行不需要再写一次，
// 所以子查询只命中 "该翻而未翻" 的行；这让重跑/重启完全等价于 no-op。
func backfillRelayLogSuccess(db *gorm.DB) error {
	if db.Dialector.Name() == "sqlite" {
		return backfillRelayLogSuccessSQLite(db)
	}
	return backfillRelayLogSuccessOther(db)
}

// backfillRelayLogSuccessSQLite 用 id (PK，所有 dialect 通用) 分批 UPDATE，
// 把每批写入对 WAL 的影响硬上限锁在 batchSize × 主页面大小。避免一条 UPDATE
// 把整张表都纳入同一个写事务、把 WAL 一次性撑到 GB 级。
func backfillRelayLogSuccessSQLite(db *gorm.DB) error {
	total := int64(0)
	start := time.Now()
	for {
		// 用 id 而不是 rowid：id 是显式声明的 PRIMARY KEY，在所有 dialect 上等价；
		// rowid 是 SQLite 专属，在 MySQL/Postgres 没有，迁移要可移植就只能走 PK。
		result := db.Exec(`
UPDATE relay_logs
SET success = 1
WHERE id IN (
    SELECT id FROM relay_logs
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
		// 每 10k 行打一次进度，便于从外部观察迁移没卡死。
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

// backfillRelayLogSuccessOther 给 MySQL/Postgres 用：一条 UPDATE 一次到位。
// 这两个 dialect 的存储引擎走原生 row-level lock，没有 WAL 堆积问题，单条
// UPDATE 即便扫几十万行也只是一次正常的 DML；分批反而引入多次 round-trip。
//
// 注意：success = false / true 的字面量在两个 dialect 上都直接被认作 bool。
func backfillRelayLogSuccessOther(db *gorm.DB) error {
	start := time.Now()
	result := db.Exec("UPDATE relay_logs SET success = true WHERE success = false AND (error = '' OR error IS NULL)")
	if result.Error != nil {
		return fmt.Errorf("failed to backfill relay_logs success: %w", result.Error)
	}
	log.Infow("migration.relay_logs_perf.backfill_success.done", "rows", result.RowsAffected, "duration", time.Since(start).String())
	return nil
}

// hasRelayLogColumn 按 dialect 选择最稳的列存在性查询：
//   - SQLite：glebarez 的 HasColumn 用正则 LIKE 扫 CREATE TABLE 文本，
//     遇到短/常见列名会误报；这里直接查 pragma_table_info 精确匹配。
//   - MySQL/Postgres：委托给 GORM 的 dialect-aware Migrator().HasColumn，
//     它会用各自驱动里正确的 information_schema 查询并按 DATABASE() /
//     current_schema() 过滤当前 schema —— 直接拼 information_schema.columns
//     而不限定 schema 会跨库/跨 schema 误判，比如同名 relay_logs 在
//     另一个数据库里也存在时会被算进来。
func hasRelayLogColumn(db *gorm.DB, column string) bool {
	if db == nil || strings.TrimSpace(column) == "" {
		return false
	}
	if db.Dialector != nil && db.Dialector.Name() == "sqlite" {
		var name string
		_ = db.Raw("SELECT name FROM pragma_table_info('relay_logs') WHERE name = ? LIMIT 1", column).Scan(&name).Error
		return name == column
	}
	return db.Migrator().HasColumn(&model.RelayLog{}, column)
}
