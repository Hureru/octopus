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
		// SQLite 唯一的写连接。如果容器在建索引时被 stop，必须让 goroutine
		// 看到取消信号、立刻让出连接，否则 db.Close() 会在写锁上阻塞，
		// 拖到 docker stop 的 grace timeout 后被 SIGKILL，下次启动还得做 WAL recovery。
		ensureIndexCtx, stopEnsureIndexes := context.WithCancel(context.Background())
		shutdown.Register(func() error {
			stopEnsureIndexes()
			return nil
		})
		safe.Go("relay-log-ensure-indexes", func() {
			op.RelayLogEnsureIndexes(ensureIndexCtx)
		})
	},
}

func init() {
	startCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./data/config.json)")
	rootCmd.AddCommand(startCmd)
}
