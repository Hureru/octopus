package cmd

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay"
	"github.com/bestruirui/octopus/internal/server"
	"github.com/bestruirui/octopus/internal/task"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/bestruirui/octopus/internal/utils/shutdown"
	"github.com/spf13/cobra"
)

var cfgFile string

// ensureIndexShutdownGrace 是 shutdown 时给 relay-log-ensure-indexes goroutine
// 自我退出留的最长时间。docker stop 默认 grace period 10s，给 SQLite 的
// sqlite3_interrupt 留出 ~3s 应对正在进行的 CREATE INDEX，其它时间留给后续
// shutdown hook（db.Close 等）。
const ensureIndexShutdownGrace = 3 * time.Second

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start " + conf.APP_NAME,
	PreRun: func(cmd *cobra.Command, args []string) {
		conf.PrintBanner()
		conf.Load(cfgFile)
		log.Configure(log.Config{
			Level:           conf.AppConfig.Log.Level,
			Format:          conf.AppConfig.Log.Format,
			Caller:          conf.AppConfig.Log.Caller,
			StacktraceLevel: conf.AppConfig.Log.StacktraceLevel,
		})
	},
	Run: func(cmd *cobra.Command, args []string) {
		shutdown.Init(log.Logger)
		defer shutdown.Listen()
		if err := db.InitDB(conf.AppConfig.Database.Type, conf.AppConfig.Database.Path, conf.IsDebug()); err != nil {
			log.Errorf("database init error: %v", err)
			return
		}
		shutdown.Register(db.Close)

		if err := op.InitCache(); err != nil {
			log.Errorf("cache init error: %v", err)
			return
		}
		relayLogWriterCtx, stopRelayLogWriter := context.WithCancel(context.Background())
		shutdown.Register(func() error {
			stopRelayLogWriter()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return op.RelayLogFlushPending(ctx)
		})
		shutdown.Register(op.SaveCache)

		if err := op.UserInit(); err != nil {
			log.Errorf("user init error: %v", err)
			return
		}

		if err := server.Start(); err != nil {
			log.Errorf("server start error: %v", err)
			return
		}
		shutdown.Register(server.Close)
		shutdown.Register(func() error {
			relay.CloseUpstreamWSPool()
			return nil
		})

		safe.Go("relay-log-writer", func() {
			op.RelayLogWriterRun(relayLogWriterCtx)
		})

		task.Init()
		safe.Go("task-runner", task.RUN)
		safe.Go("stats-site-model-backfill", func() {
			op.StatsSiteModelBackfill(cmd.Context())
		})

		// relay-log-ensure-indexes 是一个有限任务，但 CREATE INDEX 期间会持有
		// SQLite 唯一的写连接。容器在建索引时被 stop 必须做到：
		//   1. 立刻取消 warmup sleep / cooldown，让 goroutine 退出 wait。
		//   2. 进行中的 CREATE INDEX 通过 GORM/database/sql 的 ctx 链路传到
		//      modernc.org/sqlite 的 sqlite3_interrupt，SQLite 在下一个安全检查点
		//      放弃当前操作。
		//   3. shutdown.Register 等 goroutine 真正 return，再让 db.Close 关连接。
		//      最多等 ensureIndexShutdownGrace；超时只 warn 不挂，避免 docker stop 被卡。
		ensureIndexCtx, stopEnsureIndexes := context.WithCancel(context.Background())
		ensureIndexDone := make(chan struct{})
		shutdown.Register(func() error {
			stopEnsureIndexes()
			select {
			case <-ensureIndexDone:
				return nil
			case <-time.After(ensureIndexShutdownGrace):
				log.Warnf("relay-log-ensure-indexes did not exit within %s; continuing shutdown anyway", ensureIndexShutdownGrace)
				return nil
			}
		})
		safe.Go("relay-log-ensure-indexes", func() {
			defer close(ensureIndexDone)
			op.RelayLogEnsureIndexes(ensureIndexCtx)
		})
	},
}

func init() {
	startCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./data/config.json)")
	rootCmd.AddCommand(startCmd)
}
