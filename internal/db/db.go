package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db/migrate"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func InitDB(dbType, dsn string, debug bool) error {
	var err error
	gormConfig := gorm.Config{Logger: logger.Discard}
	if debug {
		gormConfig.Logger = logger.Default.LogMode(logger.Info)
	}

	switch dbType {
	case "sqlite":
		db, err = initSQLite(dsn, &gormConfig)
	case "mysql":
		db, err = initMySQL(dsn, &gormConfig)
	case "postgres", "postgresql":
		db, err = initPostgres(dsn, &gormConfig)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	switch dbType {
	case "sqlite":
		// SQLite 单写模型：限制为单连接，避免连接池内自相竞争 SQLITE_BUSY；
		// WAL 模式下读连接由驱动内部处理，不会被该限制阻塞。
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		sqlDB.SetConnMaxLifetime(0)
		sqlDB.SetConnMaxIdleTime(0)
	default:
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
		sqlDB.SetConnMaxLifetime(time.Hour)
		sqlDB.SetConnMaxIdleTime(10 * time.Minute)
	}

	if err := migrate.BeforeAutoMigrate(db); err != nil {
		return err
	}
	// relay_logs 是表里行最大、最容易踩 OOM 的表。但触发 OOM 的根因是
	// glebarez (SQLite) 的 AlterColumn 走 recreateTable 全表拷贝；
	// MySQL/Postgres 的 migrator 用原生 ALTER COLUMN，没有这个问题。
	// 所以这里只在 SQLite 上把 RelayLog 拿出来交给 migrate/013.go 显式处理，
	// MySQL/Postgres 仍然走正常的 smart-migrate，未来加列也能自动跟上。
	models := []interface{}{
		&model.User{},
		&model.Channel{},
		&model.ChannelKey{},
		&model.ProxyConfiguration{},
		&model.Site{},
		&model.SiteAccount{},
		&model.SiteToken{},
		&model.SiteUserGroup{},
		&model.SiteModel{},
		&model.SiteChannelBinding{},
		&model.SitePrice{},
		&model.Group{},
		&model.GroupItem{},
		&model.GroupPreset{},
		&model.LLMInfo{},
		&model.APIKey{},
		&model.Setting{},
		&model.StatsTotal{},
		&model.StatsDaily{},
		&model.StatsHourly{},
		&model.StatsModel{},
		&model.StatsChannel{},
		&model.StatsAPIKey{},
		&model.StatsSiteModelHourly{},
		&model.GroupHealthSnapshot{},
		&model.GroupHealthAttempt{},
		&model.WSResponseAffinity{},
		&migrate.MigrationRecord{},
	}
	if dbType == "sqlite" {
		// SQLite：表不存在时单独 CreateTable（首次安装路径，零行无 OOM 风险）；
		// 表已存在时，用 AddColumn-only 的安全路径补齐缺失字段——这避开了
		// MigrateColumn → AlterColumn → recreateTable 的全表拷贝链路，
		// 同时保留 "未来加新字段不需要手写迁移" 的开发体验。
		// 已有列的类型/默认值漂移由显式 migrate/01x.go 等显式 SQL 处理。
		// success 列的回填以及历史 schema 的一次性整理放在 migrate/013.go。
		// 索引由 op.RelayLogEnsureIndexes 在 server 起来后异步建。
		if !db.Migrator().HasTable(&model.RelayLog{}) {
			if err := db.Migrator().CreateTable(&model.RelayLog{}); err != nil {
				return err
			}
		} else {
			if err := ensureRelayLogColumnsSQLite(db); err != nil {
				return err
			}
		}
	} else {
		// MySQL/Postgres：放心交给 AutoMigrate，原生 ALTER 不会全表拷贝。
		models = append(models, &model.RelayLog{})
	}
	if err := db.AutoMigrate(models...); err != nil {
		return err
	}
	if err := migrate.AfterAutoMigrate(db); err != nil {
		return err
	}
	// Postgres: schema changes during migrations can invalidate cached prepared plans
	// (e.g. "cached plan must not change result type"). Clear them.
	if db.Dialector != nil && db.Dialector.Name() == "postgres" {
		db.Exec("DEALLOCATE ALL")
		db.Exec("DISCARD ALL")
	}
	return nil
}

func initSQLite(path string, config *gorm.Config) (*gorm.DB, error) {
	// glebarez/sqlite (modernc.org/sqlite) 只识别 _pragma=NAME(VALUE) 形式参数，
	// 旧的下划线参数会被静默忽略（导致 WAL/busy_timeout 实际未生效）。
	params := []string{
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(ON)",
		"_pragma=cache_size(-10000)",
		"_pragma=mmap_size(268435456)",
		"_pragma=temp_store(MEMORY)",
	}
	return gorm.Open(sqlite.Open(path+"?"+strings.Join(params, "&")), config)
}

func initMySQL(dsn string, config *gorm.Config) (*gorm.DB, error) {
	// DSN 格式: user:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local
	if !strings.Contains(dsn, "?") {
		dsn += "?charset=utf8mb4&parseTime=True&loc=Local"
	}
	return gorm.Open(mysql.Open(dsn), config)
}

func initPostgres(dsn string, config *gorm.Config) (*gorm.DB, error) {
	// DSN 格式: host=localhost user=postgres password=xxx dbname=octopus port=5432 sslmode=disable
	return gorm.Open(postgres.Open(dsn), config)
}

func Close() error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func GetDB() *gorm.DB {
	return db
}

// ensureRelayLogColumnsSQLite 用 GORM 的 AddColumn 给 relay_logs 补齐 model 里
// 已声明、但 DB 里还没有的字段。**只**做 ADD COLUMN，不走 smart-migrate 的
// MigrateColumn/AlterColumn 路径——后者在 glebarez/sqlite 上会触发
// recreateTable 全表拷贝，对 GB 级的 relay_logs 表会直接 OOM。
//
// 这条路径让 "未来给 model.RelayLog 加字段不需要手写迁移" 这件事在 SQLite 上
// 也能成立（MySQL/Postgres 走正常的 AutoMigrate）；已有列的类型 / 默认值漂移
// 不会被自动 ALTER，需要显式迁移脚本，但这正是我们想要的——把 "可能引发
// 全表拷贝" 的决定权交给人类。
func ensureRelayLogColumnsSQLite(db *gorm.DB) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&model.RelayLog{}); err != nil {
		return fmt.Errorf("parse relay_logs schema: %w", err)
	}
	for _, field := range stmt.Schema.Fields {
		if field.IgnoreMigration {
			continue
		}
		// 跳过仅在 Go 侧持有的关联字段（没有 DBName）
		if field.DBName == "" {
			continue
		}
		var name string
		if err := db.Raw(
			"SELECT name FROM pragma_table_info('relay_logs') WHERE name = ? LIMIT 1",
			field.DBName,
		).Scan(&name).Error; err != nil {
			return fmt.Errorf("inspect relay_logs column %s: %w", field.DBName, err)
		}
		if name == field.DBName {
			continue
		}
		// AddColumn 在 GORM 里就是 "ALTER TABLE ? ADD ? ?"，
		// 类型由 FullDataTypeOf(field) 生成 —— 直通 ALTER TABLE ADD COLUMN，
		// 绝不会走 recreateTable。
		if err := db.Migrator().AddColumn(&model.RelayLog{}, field.Name); err != nil {
			return fmt.Errorf("add relay_logs column %s: %w", field.DBName, err)
		}
	}
	return nil
}
