package cmd

import (
	"os"
	"path/filepath"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/spf13/cobra"
)

// desktopDataDir 返回桌面模式的数据目录：
// Windows %APPDATA%\Octopus，其他平台 ~/.config/Octopus。
func desktopDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "Octopus")
}

// ensureDesktopConfig 在数据目录创建默认 config.json（不存在时），
// 使数据库与日志落在用户可写的目录（安装到 Program Files 后相对路径不可写）。
func ensureDesktopConfig(dir string) string {
	configPath := filepath.Join(dir, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		return configPath
	}
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0755); err != nil {
		return configPath
	}
	defaultConfig := `{
  "database": {
    "type": "sqlite",
    "path": "` + filepath.ToSlash(filepath.Join(dir, "data", "data.db")) + `"
  },
  "server": {
    "host": "127.0.0.1",
    "port": 8080
  },
  "log": {
    "level": "info",
    "format": "console",
    "caller": false,
    "stacktrace_level": "error"
  },
  "startup": {
    "cache_init_timeout_seconds": 120
  }
}
`
	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		log.Warnf("desktop: failed to write default config: %v", err)
	}
	return configPath
}

var desktopCmd = &cobra.Command{
	Use:   "desktop",
	Short: "Run in local desktop mode (no console, system tray, auto-open browser)",
	PreRun: func(cmd *cobra.Command, args []string) {
		dir := desktopDataDir()
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Errorf("desktop: failed to create data dir %s: %v", dir, err)
			os.Exit(1)
		}
		configPath := ensureDesktopConfig(dir)
		conf.Load(configPath)
		log.Configure(log.Config{
			Level:           conf.AppConfig.Log.Level,
			Format:          conf.AppConfig.Log.Format,
			Caller:          conf.AppConfig.Log.Caller,
			StacktraceLevel: conf.AppConfig.Log.StacktraceLevel,
			FilePath:        filepath.Join(dir, "logs", "octopus.log"),
		})
		log.Infof("desktop mode: data dir %s", dir)
	},
	Run: func(cmd *cobra.Command, args []string) {
		runService(cmd, onDesktopReady)
	},
}

// onDesktopReady 在 HTTP Server 启动成功后执行：自动打开浏览器 + 启动系统托盘。
func onDesktopReady(addr string) {
	openBrowser(addr)
	go runTray(addr)
}

func init() {
	rootCmd.AddCommand(desktopCmd)
}
