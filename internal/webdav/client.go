package webdav

import (
	"fmt"
	"net/http"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
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

func NewClient(cfg *Config) *gowebdav.Client {
	c := gowebdav.NewClient(cfg.URL, cfg.Username, cfg.Password)

	httpClient := getHTTPClient()
	if httpClient != nil {
		c.SetTransport(httpClient.Transport)
	}

	return c
}

func getHTTPClient() *http.Client {
	proxyURL, _ := op.SettingGetString(model.SettingKeyProxyURL)
	if proxyURL != "" {
		httpClient, err := client.GetHTTPClientSystemProxy(true)
		if err == nil {
			return httpClient
		}
	}
	httpClient, err := client.GetHTTPClientSystemProxy(false)
	if err != nil {
		return nil
	}
	return httpClient
}

func TestConnection(cfg *Config) error {
	c := NewClient(cfg)
	_, err := c.ReadDir(cfg.BackupPath)
	if err != nil {
		if err := c.MkdirAll(cfg.BackupPath, 0755); err != nil {
			return fmt.Errorf("cannot access or create backup directory: %w", err)
		}
	}
	return nil
}
