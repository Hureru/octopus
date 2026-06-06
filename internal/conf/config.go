package conf

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/spf13/viper"
)

type Server struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type Log struct {
	Level           string `mapstructure:"level"`
	Format          string `mapstructure:"format"`
	Caller          bool   `mapstructure:"caller"`
	StacktraceLevel string `mapstructure:"stacktrace_level"`
	Access          struct {
		Enabled         bool `mapstructure:"enabled"`
		SlowThresholdMS int  `mapstructure:"slow_threshold_ms"`
	} `mapstructure:"access"`
	Relay struct {
		Summary bool `mapstructure:"summary"`
	} `mapstructure:"relay"`
}

type Database struct {
	Type string `mapstructure:"type"`
	Path string `mapstructure:"path"`
}

type Startup struct {
	CacheInitTimeoutSeconds int `mapstructure:"cache_init_timeout_seconds"`
}

type Config struct {
	Server   Server   `mapstructure:"server"`
	Log      Log      `mapstructure:"log"`
	Database Database `mapstructure:"database"`
	Startup  Startup  `mapstructure:"startup"`
}

var AppConfig Config

func CacheInitTimeout() time.Duration {
	seconds := AppConfig.Startup.CacheInitTimeoutSeconds
	if seconds <= 0 {
		seconds = 120
	}
	return time.Duration(seconds) * time.Second
}

func Load(path string) error {
	if path != "" {
		viper.SetConfigFile(path)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("json")
		viper.AddConfigPath("data")
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix(APP_NAME)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults()

	if err := viper.ReadInConfig(); err == nil {
		log.Infof("Using config file: %s", viper.ConfigFileUsed())
	} else {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Infof("Config file not found, creating default config")
			if err := os.MkdirAll("data", 0755); err != nil {
				log.Errorf("Failed to create data directory: %v", err)
			}
			if err := viper.SafeWriteConfigAs("data/config.json"); err != nil {
				log.Errorf("Failed to create default config: %v", err)
			}
		} else {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	if err := viper.Unmarshal(&AppConfig); err != nil {
		return fmt.Errorf("unable to decode config into struct: %w", err)
	}
	return nil
}

func setDefaults() {
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("database.type", "sqlite")
	viper.SetDefault("database.path", "data/data.db")
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "console")
	viper.SetDefault("log.caller", false)
	viper.SetDefault("log.stacktrace_level", "error")
	viper.SetDefault("log.access.enabled", false)
	viper.SetDefault("log.access.slow_threshold_ms", 3000)
	viper.SetDefault("log.relay.summary", true)
	viper.SetDefault("startup.cache_init_timeout_seconds", 120)
}
