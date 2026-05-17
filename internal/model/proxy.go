package model

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type ProxyUsageMode string

const (
	ProxyUsageModeDirect  ProxyUsageMode = "direct"
	ProxyUsageModeSystem  ProxyUsageMode = "system"
	ProxyUsageModePool    ProxyUsageMode = "pool"
	ProxyUsageModeInherit ProxyUsageMode = "inherit"
)

type ProxyConfiguration struct {
	ID             int       `json:"id" gorm:"primaryKey"`
	Name           string    `json:"name" gorm:"unique;not null"`
	URL            string    `json:"url" gorm:"unique;not null"`
	Enabled        bool      `json:"enabled" gorm:"default:true"`
	Remark         string    `json:"remark"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ReferenceCount int       `json:"reference_count" gorm:"-"`
}

type ProxyConfigurationUpdateRequest struct {
	ID      int     `json:"id" binding:"required"`
	Name    *string `json:"name,omitempty"`
	URL     *string `json:"url,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
	Remark  *string `json:"remark,omitempty"`
}

type ProxyTestRequest struct {
	ProxyConfigID *int   `json:"proxy_config_id,omitempty"`
	ProxyURL      string `json:"proxy_url,omitempty"`
	URL           string `json:"url,omitempty"`
}

type ProxyTestResult struct {
	Success    bool   `json:"success"`
	StatusCode int    `json:"status_code"`
	DurationMS int64  `json:"duration_ms"`
	Message    string `json:"message"`
}

type ProxyConfigurationReferenceType string

const (
	ProxyConfigurationReferenceTypeSite           ProxyConfigurationReferenceType = "site"
	ProxyConfigurationReferenceTypeSiteAccount    ProxyConfigurationReferenceType = "site_account"
	ProxyConfigurationReferenceTypeChannel        ProxyConfigurationReferenceType = "channel"
	ProxyConfigurationReferenceTypeManagedChannel ProxyConfigurationReferenceType = "managed_channel"
)

type ProxyConfigurationReference struct {
	Type            ProxyConfigurationReferenceType `json:"type"`
	SiteID          int                             `json:"site_id,omitempty"`
	SiteName        string                          `json:"site_name,omitempty"`
	SiteArchived    bool                            `json:"site_archived,omitempty"`
	SiteAccountID   int                             `json:"site_account_id,omitempty"`
	SiteAccountName string                          `json:"site_account_name,omitempty"`
	ChannelID       int                             `json:"channel_id,omitempty"`
	ChannelName     string                          `json:"channel_name,omitempty"`
	Managed         bool                            `json:"managed,omitempty"`
	ManagedSource   *ManagedChannelSource           `json:"managed_source,omitempty"`
}

func (m ProxyUsageMode) Validate(allowInherit bool) error {
	switch m {
	case ProxyUsageModeDirect, ProxyUsageModeSystem, ProxyUsageModePool:
		return nil
	case ProxyUsageModeInherit:
		if allowInherit {
			return nil
		}
	}
	return fmt.Errorf("unsupported proxy mode: %s", m)
}

func NormalizeProxyURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("proxy url is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid proxy url: %w", err)
	}
	parsed.Scheme = strings.ToLower(strings.TrimSpace(parsed.Scheme))
	parsed.Host = strings.ToLower(strings.TrimSpace(parsed.Host))
	switch parsed.Scheme {
	case "http", "https", "socks", "socks5":
	default:
		return "", fmt.Errorf("unsupported proxy scheme: %s", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("proxy url must have a host")
	}
	return parsed.String(), nil
}

func (p *ProxyConfiguration) Normalize() error {
	if p == nil {
		return fmt.Errorf("proxy configuration is nil")
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Remark = strings.TrimSpace(p.Remark)
	if p.Name == "" {
		return fmt.Errorf("proxy name is required")
	}
	normalizedURL, err := NormalizeProxyURL(p.URL)
	if err != nil {
		return err
	}
	p.URL = normalizedURL
	return nil
}

func (p *ProxyConfiguration) Validate() error {
	return p.Normalize()
}
