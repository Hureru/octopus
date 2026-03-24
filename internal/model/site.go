package model

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type SitePlatform string

const (
	SitePlatformNewAPI    SitePlatform = "new-api"
	SitePlatformAnyRouter SitePlatform = "anyrouter"
	SitePlatformOneAPI    SitePlatform = "one-api"
	SitePlatformOneHub    SitePlatform = "one-hub"
	SitePlatformDoneHub   SitePlatform = "done-hub"
	SitePlatformSub2API   SitePlatform = "sub2api"
	SitePlatformOpenAI    SitePlatform = "openai"
	SitePlatformClaude    SitePlatform = "claude"
	SitePlatformGemini    SitePlatform = "gemini"
)

type SiteCredentialType string

const (
	SiteCredentialTypeUsernamePassword SiteCredentialType = "username_password"
	SiteCredentialTypeAccessToken      SiteCredentialType = "access_token"
	SiteCredentialTypeAPIKey           SiteCredentialType = "api_key"
)

type SiteExecutionStatus string

const (
	SiteExecutionStatusIdle    SiteExecutionStatus = "idle"
	SiteExecutionStatusSuccess SiteExecutionStatus = "success"
	SiteExecutionStatusFailed  SiteExecutionStatus = "failed"
	SiteExecutionStatusSkipped SiteExecutionStatus = "skipped"
)

const (
	SiteDefaultGroupKey  = "default"
	SiteDefaultGroupName = "default"
)

type Site struct {
	ID           int            `json:"id" gorm:"primaryKey"`
	Name         string         `json:"name" gorm:"unique;not null"`
	Platform     SitePlatform   `json:"platform" gorm:"type:varchar(32);not null"`
	BaseURL      string         `json:"base_url" gorm:"not null"`
	Enabled      bool           `json:"enabled" gorm:"default:true"`
	Proxy        bool           `json:"proxy" gorm:"default:false"`
	SiteProxy    *string        `json:"site_proxy"`
	CustomHeader []CustomHeader `json:"custom_header" gorm:"serializer:json"`
	Accounts     []SiteAccount  `json:"accounts,omitempty" gorm:"foreignKey:SiteID"`
}

type SiteAccount struct {
	ID                         int                  `json:"id" gorm:"primaryKey"`
	SiteID                     int                  `json:"site_id" gorm:"index;not null"`
	Name                       string               `json:"name" gorm:"not null"`
	CredentialType             SiteCredentialType   `json:"credential_type" gorm:"type:varchar(32);not null"`
	Username                   string               `json:"username"`
	Password                   string               `json:"password"`
	AccessToken                string               `json:"access_token"`
	APIKey                     string               `json:"api_key"`
	Enabled                    bool                 `json:"enabled" gorm:"default:true"`
	AutoSync                   bool                 `json:"auto_sync" gorm:"default:true"`
	AutoCheckin                bool                 `json:"auto_checkin" gorm:"default:true"`
	RandomCheckin              bool                 `json:"random_checkin" gorm:"default:false"`
	CheckinIntervalHours       int                  `json:"checkin_interval_hours" gorm:"default:24"`
	CheckinRandomWindowMinutes int                  `json:"checkin_random_window_minutes" gorm:"default:120"`
	NextAutoCheckinAt          *time.Time           `json:"next_auto_checkin_at"`
	LastSyncAt                 *time.Time           `json:"last_sync_at"`
	LastCheckinAt              *time.Time           `json:"last_checkin_at"`
	LastSyncStatus             SiteExecutionStatus  `json:"last_sync_status" gorm:"type:varchar(16);default:'idle'"`
	LastCheckinStatus          SiteExecutionStatus  `json:"last_checkin_status" gorm:"type:varchar(16);default:'idle'"`
	LastSyncMessage            string               `json:"last_sync_message"`
	LastCheckinMessage         string               `json:"last_checkin_message"`
	Tokens                     []SiteToken          `json:"tokens,omitempty" gorm:"foreignKey:SiteAccountID"`
	UserGroups                 []SiteUserGroup      `json:"user_groups,omitempty" gorm:"foreignKey:SiteAccountID"`
	Models                     []SiteModel          `json:"models,omitempty" gorm:"foreignKey:SiteAccountID"`
	ChannelBindings            []SiteChannelBinding `json:"channel_bindings,omitempty" gorm:"foreignKey:SiteAccountID"`
}

type SiteToken struct {
	ID            int        `json:"id" gorm:"primaryKey"`
	SiteAccountID int        `json:"site_account_id" gorm:"index;not null"`
	Name          string     `json:"name"`
	Token         string     `json:"token" gorm:"not null"`
	GroupKey      string     `json:"group_key" gorm:"index"`
	GroupName     string     `json:"group_name"`
	Enabled       bool       `json:"enabled" gorm:"default:true"`
	Source        string     `json:"source"`
	IsDefault     bool       `json:"is_default" gorm:"default:false"`
	LastSyncAt    *time.Time `json:"last_sync_at"`
}

type SiteUserGroup struct {
	ID            int    `json:"id" gorm:"primaryKey"`
	SiteAccountID int    `json:"site_account_id" gorm:"uniqueIndex:idx_site_account_group;not null"`
	GroupKey      string `json:"group_key" gorm:"uniqueIndex:idx_site_account_group;not null"`
	Name          string `json:"name"`
	RawPayload    string `json:"raw_payload"`
}

type SiteModel struct {
	ID            int    `json:"id" gorm:"primaryKey"`
	SiteAccountID int    `json:"site_account_id" gorm:"uniqueIndex:idx_site_account_model;not null"`
	ModelName     string `json:"model_name" gorm:"uniqueIndex:idx_site_account_model;not null"`
	Source        string `json:"source"`
}

type SiteChannelBinding struct {
	ID              int    `json:"id" gorm:"primaryKey"`
	SiteID          int    `json:"site_id" gorm:"index;not null"`
	SiteAccountID   int    `json:"site_account_id" gorm:"uniqueIndex:idx_site_account_channel_group;not null"`
	SiteUserGroupID *int   `json:"site_user_group_id"`
	GroupKey        string `json:"group_key" gorm:"uniqueIndex:idx_site_account_channel_group;not null"`
	ChannelID       int    `json:"channel_id" gorm:"uniqueIndex;not null"`
}

type SiteUpdateRequest struct {
	ID           int             `json:"id" binding:"required"`
	Name         *string         `json:"name,omitempty"`
	Platform     *SitePlatform   `json:"platform,omitempty"`
	BaseURL      *string         `json:"base_url,omitempty"`
	Enabled      *bool           `json:"enabled,omitempty"`
	Proxy        *bool           `json:"proxy,omitempty"`
	SiteProxy    *string         `json:"site_proxy,omitempty"`
	CustomHeader *[]CustomHeader `json:"custom_header,omitempty"`
}

type SiteAccountUpdateRequest struct {
	ID                         int                 `json:"id" binding:"required"`
	Name                       *string             `json:"name,omitempty"`
	CredentialType             *SiteCredentialType `json:"credential_type,omitempty"`
	Username                   *string             `json:"username,omitempty"`
	Password                   *string             `json:"password,omitempty"`
	AccessToken                *string             `json:"access_token,omitempty"`
	APIKey                     *string             `json:"api_key,omitempty"`
	Enabled                    *bool               `json:"enabled,omitempty"`
	AutoSync                   *bool               `json:"auto_sync,omitempty"`
	AutoCheckin                *bool               `json:"auto_checkin,omitempty"`
	RandomCheckin              *bool               `json:"random_checkin,omitempty"`
	CheckinIntervalHours       *int                `json:"checkin_interval_hours,omitempty"`
	CheckinRandomWindowMinutes *int                `json:"checkin_random_window_minutes,omitempty"`
}

type SiteSyncResult struct {
	AccountID       int      `json:"account_id"`
	SiteID          int      `json:"site_id"`
	ChannelCount    int      `json:"channel_count"`
	GroupCount      int      `json:"group_count"`
	TokenCount      int      `json:"token_count"`
	ModelCount      int      `json:"model_count"`
	ManagedChannels []int    `json:"managed_channels,omitempty"`
	Models          []string `json:"models,omitempty"`
	Message         string   `json:"message"`
}

type SiteCheckinResult struct {
	AccountID int                 `json:"account_id"`
	SiteID    int                 `json:"site_id"`
	Status    SiteExecutionStatus `json:"status"`
	Message   string              `json:"message"`
	Reward    string              `json:"reward,omitempty"`
}

func NormalizeSiteGroupKey(value string) string {
	key := strings.TrimSpace(value)
	if key == "" {
		return SiteDefaultGroupKey
	}
	return key
}

func NormalizeSiteGroupName(groupKey string, name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(groupKey); trimmed != "" {
		return trimmed
	}
	return SiteDefaultGroupName
}

func (p SitePlatform) Validate() error {
	switch p {
	case SitePlatformNewAPI, SitePlatformAnyRouter, SitePlatformOneAPI, SitePlatformOneHub, SitePlatformDoneHub,
		SitePlatformSub2API, SitePlatformOpenAI, SitePlatformClaude, SitePlatformGemini:
		return nil
	default:
		return fmt.Errorf("unsupported site platform: %s", p)
	}
}

func (t SiteCredentialType) Validate() error {
	switch t {
	case SiteCredentialTypeUsernamePassword, SiteCredentialTypeAccessToken, SiteCredentialTypeAPIKey:
		return nil
	default:
		return fmt.Errorf("unsupported site credential type: %s", t)
	}
}

func (s *Site) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	if s.SiteProxy != nil {
		trimmed := strings.TrimSpace(*s.SiteProxy)
		if trimmed == "" {
			s.SiteProxy = nil
		} else {
			s.SiteProxy = &trimmed
		}
	}
}

func (s *Site) Validate() error {
	if s == nil {
		return fmt.Errorf("site is nil")
	}
	s.Normalize()
	if s.Name == "" {
		return fmt.Errorf("site name is required")
	}
	if err := s.Platform.Validate(); err != nil {
		return err
	}
	parsed, err := url.Parse(s.BaseURL)
	if err != nil {
		return fmt.Errorf("site base url is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("site base url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("site base url must have a host")
	}
	return nil
}

func (a *SiteAccount) Normalize() {
	a.Name = strings.TrimSpace(a.Name)
	a.Username = strings.TrimSpace(a.Username)
	a.Password = strings.TrimSpace(a.Password)
	a.AccessToken = strings.TrimSpace(a.AccessToken)
	a.APIKey = strings.TrimSpace(a.APIKey)
	if a.CheckinIntervalHours <= 0 {
		a.CheckinIntervalHours = 24
	}
	if a.CheckinRandomWindowMinutes < 0 {
		a.CheckinRandomWindowMinutes = 0
	}
}

func (a *SiteAccount) Validate() error {
	if a == nil {
		return fmt.Errorf("site account is nil")
	}
	a.Normalize()
	if a.SiteID == 0 {
		return fmt.Errorf("site id is required")
	}
	if a.Name == "" {
		return fmt.Errorf("site account name is required")
	}
	if err := a.CredentialType.Validate(); err != nil {
		return err
	}
	if a.CheckinIntervalHours <= 0 {
		return fmt.Errorf("checkin interval hours must be greater than 0")
	}
	if a.CheckinIntervalHours > 720 {
		return fmt.Errorf("checkin interval hours must be less than or equal to 720")
	}
	if a.CheckinRandomWindowMinutes < 0 {
		return fmt.Errorf("checkin random window minutes must be greater than or equal to 0")
	}
	if a.CheckinRandomWindowMinutes > 1440 {
		return fmt.Errorf("checkin random window minutes must be less than or equal to 1440")
	}
	switch a.CredentialType {
	case SiteCredentialTypeUsernamePassword:
		if a.Username == "" || a.Password == "" {
			return fmt.Errorf("username and password are required")
		}
	case SiteCredentialTypeAccessToken:
		if a.AccessToken == "" {
			return fmt.Errorf("access token is required")
		}
	case SiteCredentialTypeAPIKey:
		if a.APIKey == "" {
			return fmt.Errorf("api key is required")
		}
	}
	return nil
}
