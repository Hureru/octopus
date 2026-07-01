package webdav

import (
	"fmt"
	"net/http"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/studio-b12/gowebdav"
)

type Config struct {
	URL            string
	Username       string
	Password       string
	BackupPath     string
	RetentionCount int
	IncludeStats   bool
}

func LoadConfig() (*Config, error) {
	url, err := op.SettingGetString(model.SettingKeyWebDAVURL)
	if err != nil {
		return nil, fmt.Errorf("webdav_url not configured: %w", err)
	}
	if url == "" {
		return nil, fmt.Errorf("webdav_url is empty")
	}

	username, _ := op.SettingGetString(model.SettingKeyWebDAVUsername)
	password, _ := op.SettingGetString(model.SettingKeyWebDAVPassword)

	backupPath, err := op.SettingGetString(model.SettingKeyWebDAVBackupPath)
	if err != nil || backupPath == "" {
		backupPath = "/octopus-backups"
	}

	retentionCount, err := op.SettingGetInt(model.SettingKeyWebDAVRetentionCount)
	if err != nil || retentionCount < 1 {
		retentionCount = 10
	}

	includeStatsStr, _ := op.SettingGetString(model.SettingKeyWebDAVIncludeStats)
	includeStats := includeStatsStr != "false"

	return &Config{
		URL:            url,
		Username:       username,
		Password:       password,
		BackupPath:     backupPath,
		RetentionCount: retentionCount,
		IncludeStats:   includeStats,
	}, nil
}

func NewClient(cfg *Config) (*gowebdav.Client, error) {
	c := gowebdav.NewClient(cfg.URL, cfg.Username, cfg.Password)
	c.SetTimeout(30 * time.Second)

	httpClient, err := getHTTPClient()
	if err != nil {
		return nil, err
	}
	if httpClient != nil {
		c.SetTransport(httpClient.Transport)
	}

	return c, nil
}

func getHTTPClient() (*http.Client, error) {
	proxyURL, _ := op.SettingGetString(model.SettingKeyProxyURL)
	useProxy := proxyURL != ""
	httpClient, err := client.GetHTTPClientSystemProxy(useProxy)
	if err != nil {
		if useProxy {
			return nil, fmt.Errorf("failed to create proxied HTTP client: %w", err)
		}
		log.Warnf("failed to create HTTP client: %v", err)
		return nil, nil
	}
	return httpClient, nil
}

func TestConnection(cfg *Config) error {
	c, err := NewClient(cfg)
	if err != nil {
		return err
	}
	_, err = c.ReadDir(cfg.BackupPath)
	if err != nil {
		if !gowebdav.IsErrNotFound(err) {
			return fmt.Errorf("cannot access backup directory: %w", err)
		}
		if err := c.MkdirAll(cfg.BackupPath, 0755); err != nil {
			return fmt.Errorf("cannot create backup directory: %w", err)
		}
	}
	return nil
}
