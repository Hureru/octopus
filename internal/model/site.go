package model

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

type SitePlatform string

const (
	SitePlatformNewAPI    SitePlatform = "new-api"
	SitePlatformAnyRouter SitePlatform = "anyrouter"
	SitePlatformOneAPI    SitePlatform = "one-api"
	SitePlatformOneHub    SitePlatform = "one-hub"
	SitePlatformDoneHub   SitePlatform = "done-hub"
	SitePlatformSub2API SitePlatform = "sub2api"
	SitePlatformAPI     SitePlatform = "api"
)

type SiteCredentialType string

const (
	SiteCredentialTypeUsernamePassword SiteCredentialType = "username_password"
	SiteCredentialTypeAccessToken      SiteCredentialType = "access_token"
	SiteCredentialTypeAPIKey           SiteCredentialType = "api_key"
)

type SiteExecutionStatus string

type SiteGroupModelSyncStatus string

const (
	SiteExecutionStatusIdle    SiteExecutionStatus = "idle"
	SiteExecutionStatusSuccess SiteExecutionStatus = "success"
	SiteExecutionStatusPartial SiteExecutionStatus = "partial"
	SiteExecutionStatusFailed  SiteExecutionStatus = "failed"
	SiteExecutionStatusSkipped SiteExecutionStatus = "skipped"
)

const (
	SiteGroupModelSyncStatusIdle       SiteGroupModelSyncStatus = "idle"
	SiteGroupModelSyncStatusSynced     SiteGroupModelSyncStatus = "synced"
	SiteGroupModelSyncStatusEmpty      SiteGroupModelSyncStatus = "empty"
	SiteGroupModelSyncStatusStale      SiteGroupModelSyncStatus = "stale"
	SiteGroupModelSyncStatusFailed     SiteGroupModelSyncStatus = "failed"
	SiteGroupModelSyncStatusUnresolved SiteGroupModelSyncStatus = "unresolved"
	SiteGroupModelSyncStatusMissingKey SiteGroupModelSyncStatus = "missing_key"
	SiteGroupModelSyncStatusRemoved    SiteGroupModelSyncStatus = "removed"
)

type SiteModelRouteType string

const (
	SiteModelRouteTypeOpenAIChat      SiteModelRouteType = "openai_chat"
	SiteModelRouteTypeOpenAIResponse  SiteModelRouteType = "openai_response"
	SiteModelRouteTypeAnthropic       SiteModelRouteType = "anthropic"
	SiteModelRouteTypeGemini          SiteModelRouteType = "gemini"
	SiteModelRouteTypeVolcengine      SiteModelRouteType = "volcengine"
	SiteModelRouteTypeOpenAIEmbedding SiteModelRouteType = "openai_embedding"
	SiteModelRouteTypeUnknown         SiteModelRouteType = "unknown"
)

type SiteModelRouteSource string

const (
	SiteModelRouteSourceSyncInferred    SiteModelRouteSource = "sync_inferred"
	SiteModelRouteSourceManualOverride  SiteModelRouteSource = "manual_override"
	SiteModelRouteSourceRuntimeLearned  SiteModelRouteSource = "runtime_learned"
	SiteModelRouteSourceDefaultAssigned SiteModelRouteSource = "default_assigned"
)

const (
	SiteDefaultGroupKey  = "default"
	SiteDefaultGroupName = "default"
)

// SiteRouteBaseURL overrides the projected channel base URL for a specific
// outbound route type. Some upstreams expose different protocols under
// different path prefixes (e.g. OpenAI responses at "<base>/v1" but Anthropic
// messages at "<base>/anthropic/v1"); a single site base URL cannot serve
// both, so each route type may carry its own full base URL here.
type SiteRouteBaseURL struct {
	RouteType SiteModelRouteType `json:"route_type"`
	BaseURL   string             `json:"base_url"`
}

// ResolveRouteBaseURL returns the per-route base URL override for routeType,
// trimmed of trailing slashes. The second return value reports whether a
// usable (non-empty) override exists.
func (s *Site) ResolveRouteBaseURL(routeType SiteModelRouteType) (string, bool) {
	if s == nil {
		return "", false
	}
	for _, item := range s.RouteBaseURLs {
		if item.RouteType != routeType {
			continue
		}
		trimmed := strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	}
	return "", false
}

// NormalizeSiteRouteBaseURLs trims values, drops entries with an empty base
// URL or route type, and keeps the first entry per route type.
func NormalizeSiteRouteBaseURLs(items []SiteRouteBaseURL) []SiteRouteBaseURL {
	if len(items) == 0 {
		return items
	}
	seen := make(map[SiteModelRouteType]struct{}, len(items))
	result := make([]SiteRouteBaseURL, 0, len(items))
	for _, item := range items {
		routeType := SiteModelRouteType(strings.TrimSpace(string(item.RouteType)))
		baseURL := strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
		if routeType == "" || baseURL == "" {
			continue
		}
		if _, ok := seen[routeType]; ok {
			continue
		}
		seen[routeType] = struct{}{}
		result = append(result, SiteRouteBaseURL{RouteType: routeType, BaseURL: baseURL})
	}
	return result
}

// ValidateSiteRouteBaseURLs rejects overrides whose route type is not a
// projectable outbound route or whose base URL is not a valid http/https URL.
// It mirrors the validation applied to Site.BaseURL so malformed overrides are
// surfaced to the caller instead of silently breaking projection.
func ValidateSiteRouteBaseURLs(items []SiteRouteBaseURL) error {
	for _, item := range items {
		if !IsProjectedSiteModelRouteType(item.RouteType) {
			return fmt.Errorf("route base url has unsupported route type: %s", item.RouteType)
		}
		parsed, err := url.Parse(item.BaseURL)
		if err != nil {
			return fmt.Errorf("route base url for %s is invalid: %w", item.RouteType, err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("route base url for %s must use http or https", item.RouteType)
		}
		if parsed.Host == "" {
			return fmt.Errorf("route base url for %s must have a host", item.RouteType)
		}
	}
	return nil
}

type Site struct {
	ID                 int                `json:"id" gorm:"primaryKey"`
	Name               string             `json:"name" gorm:"unique;not null"`
	Platform           SitePlatform       `json:"platform" gorm:"type:varchar(32);not null"`
	BaseURL            string             `json:"base_url" gorm:"not null"`
	Enabled            bool               `json:"enabled" gorm:"default:true"`
	EnabledSet         bool               `json:"-" gorm:"-"`
	ProxyMode          ProxyUsageMode     `json:"proxy_mode" gorm:"type:varchar(16);not null;default:'direct'"`
	ProxyConfigID      *int               `json:"proxy_config_id"`
	Proxy              bool               `json:"-" gorm:"default:false"`
	SiteProxy          *string            `json:"-" gorm:"column:site_proxy"`
	UseSystemProxy     bool               `json:"-" gorm:"default:false"`
	ExternalCheckinURL *string            `json:"external_checkin_url"`
	IsPinned           bool               `json:"is_pinned" gorm:"default:false"`
	SortOrder          int                `json:"sort_order" gorm:"default:0"`
	GlobalWeight       float64            `json:"global_weight" gorm:"default:1"`
	CustomHeader       []CustomHeader     `json:"custom_header" gorm:"serializer:json"`
	RouteBaseURLs      []SiteRouteBaseURL `json:"route_base_urls" gorm:"serializer:json"`
	DefaultRouteType   SiteModelRouteType `json:"default_route_type" gorm:"type:varchar(32);not null;default:''"`
	Tags               []string           `json:"tags" gorm:"serializer:json"`
	Archived           bool               `json:"archived" gorm:"default:false;index"`
	ArchivedAt         *time.Time         `json:"archived_at"`
	Accounts           []SiteAccount      `json:"accounts,omitempty" gorm:"foreignKey:SiteID"`
}

func (s *Site) UnmarshalJSON(data []byte) error {
	type alias Site
	aux := struct {
		*alias
		Proxy          *bool   `json:"proxy"`
		SiteProxy      *string `json:"site_proxy"`
		UseSystemProxy *bool   `json:"use_system_proxy"`
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, s.EnabledSet = raw["enabled"]
	if aux.Proxy != nil {
		s.Proxy = *aux.Proxy
	}
	if aux.SiteProxy != nil {
		s.SiteProxy = aux.SiteProxy
	}
	if aux.UseSystemProxy != nil {
		s.UseSystemProxy = *aux.UseSystemProxy
	}
	return nil
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
	RefreshToken               string               `json:"refresh_token"`
	TokenExpiresAt             int64                `json:"token_expires_at" gorm:"default:0"`
	PlatformUserID             *int                 `json:"platform_user_id"`
	ProxyMode                  ProxyUsageMode       `json:"proxy_mode" gorm:"type:varchar(16);not null;default:'inherit'"`
	ProxyConfigID              *int                 `json:"proxy_config_id"`
	AccountProxy               *string              `json:"-" gorm:"column:account_proxy"`
	Enabled                    bool                 `json:"enabled" gorm:"default:true"`
	EnabledSet                 bool                 `json:"-" gorm:"-"`
	AutoSync                   bool                 `json:"auto_sync" gorm:"default:true"`
	AutoSyncSet                bool                 `json:"-" gorm:"-"`
	AutoCheckin                bool                 `json:"auto_checkin" gorm:"default:true"`
	AutoCheckinSet             bool                 `json:"-" gorm:"-"`
	RandomCheckin              bool                 `json:"random_checkin" gorm:"default:false"`
	CheckinIntervalHours       int                  `json:"checkin_interval_hours" gorm:"default:24"`
	CheckinRandomWindowMinutes int                  `json:"checkin_random_window_minutes" gorm:"default:120"`
	Balance                    float64              `json:"balance" gorm:"default:0"`
	BalanceUsed                float64              `json:"balance_used" gorm:"default:0"`
	TodayIncome                float64              `json:"today_income" gorm:"default:0"`
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

func (a *SiteAccount) UnmarshalJSON(data []byte) error {
	type alias SiteAccount
	aux := struct {
		*alias
		AccountProxy *string `json:"account_proxy"`
	}{alias: (*alias)(a)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, a.EnabledSet = raw["enabled"]
	_, a.AutoSyncSet = raw["auto_sync"]
	_, a.AutoCheckinSet = raw["auto_checkin"]
	if aux.AccountProxy != nil {
		a.AccountProxy = aux.AccountProxy
	}
	return nil
}

type SiteTokenValueStatus string

const (
	SiteTokenValueStatusReady         SiteTokenValueStatus = "ready"
	SiteTokenValueStatusMaskedPending SiteTokenValueStatus = "masked_pending"
)

type SiteToken struct {
	ID            int                  `json:"id" gorm:"primaryKey"`
	SiteAccountID int                  `json:"site_account_id" gorm:"index;not null"`
	Name          string               `json:"name"`
	Token         string               `json:"token" gorm:"not null"`
	ValueStatus   SiteTokenValueStatus `json:"value_status" gorm:"type:varchar(32);not null;default:'ready'"`
	GroupKey      string               `json:"group_key" gorm:"size:128;index"`
	GroupName     string               `json:"group_name"`
	Enabled       bool                 `json:"enabled" gorm:"default:true"`
	Source        string               `json:"source"`
	IsDefault     bool                 `json:"is_default" gorm:"default:false"`
	LastSyncAt    *time.Time           `json:"last_sync_at"`
}

type SiteUserGroup struct {
	ID                      int                      `json:"id" gorm:"primaryKey"`
	SiteAccountID           int                      `json:"site_account_id" gorm:"uniqueIndex:idx_site_account_group;not null"`
	GroupKey                string                   `json:"group_key" gorm:"size:128;uniqueIndex:idx_site_account_group;not null"`
	Name                    string                   `json:"name"`
	RawPayload              string                   `json:"raw_payload"`
	ProjectionDisabled      bool                     `json:"projection_disabled" gorm:"default:false"`
	ProjectionSuspended     bool                     `json:"projection_suspended" gorm:"default:false;index"`
	ProjectionSuspendReason string                   `json:"projection_suspend_reason"`
	ProjectionSuspendedAt   *time.Time               `json:"projection_suspended_at"`
	ModelSyncStatus         SiteGroupModelSyncStatus `json:"model_sync_status" gorm:"type:varchar(32);not null;default:'idle';index"`
	ModelSyncMessage        string                   `json:"model_sync_message"`
	ModelSyncAuthoritative  bool                     `json:"model_sync_authoritative" gorm:"default:false"`
	ModelSyncModelCount     int                      `json:"model_sync_model_count" gorm:"default:0"`
	LastModelSyncAt         *time.Time               `json:"last_model_sync_at"`
	LastModelSyncSuccessAt  *time.Time               `json:"last_model_sync_success_at"`
	ModelSyncFailureCount   int                      `json:"model_sync_failure_count" gorm:"default:0"`
}

type SiteModel struct {
	ID              int                  `json:"id" gorm:"primaryKey"`
	SiteAccountID   int                  `json:"site_account_id" gorm:"uniqueIndex:idx_site_account_group_model;not null"`
	GroupKey        string               `json:"group_key" gorm:"size:128;uniqueIndex:idx_site_account_group_model;not null;default:'default'"`
	ModelName       string               `json:"model_name" gorm:"size:191;uniqueIndex:idx_site_account_group_model;not null"`
	Source          string               `json:"source"`
	RouteType       SiteModelRouteType   `json:"route_type" gorm:"type:varchar(32);not null;default:'openai_chat';index"`
	RouteSource     SiteModelRouteSource `json:"route_source" gorm:"type:varchar(32);not null;default:'sync_inferred'"`
	ManualOverride  bool                 `json:"manual_override" gorm:"default:false"`
	RouteRawPayload string               `json:"route_raw_payload"`
	RouteUpdatedAt  *time.Time           `json:"route_updated_at"`
	Disabled        bool                 `json:"disabled" gorm:"default:false;index"`
}

type SiteChannelBinding struct {
	ID              int    `json:"id" gorm:"primaryKey"`
	SiteID          int    `json:"site_id" gorm:"index;not null"`
	SiteAccountID   int    `json:"site_account_id" gorm:"uniqueIndex:idx_site_account_channel_group;not null"`
	SiteUserGroupID *int   `json:"site_user_group_id"`
	GroupKey        string `json:"group_key" gorm:"size:128;uniqueIndex:idx_site_account_channel_group;not null"`
	ChannelID       int    `json:"channel_id" gorm:"uniqueIndex;not null"`
}

type SiteUpdateRequest struct {
	ID                 int                 `json:"id" binding:"required"`
	Name               *string             `json:"name,omitempty"`
	Platform           *SitePlatform       `json:"platform,omitempty"`
	BaseURL            *string             `json:"base_url,omitempty"`
	Enabled            *bool               `json:"enabled,omitempty"`
	ProxyMode          *ProxyUsageMode     `json:"proxy_mode,omitempty"`
	ProxyConfigID      *int                `json:"proxy_config_id,omitempty"`
	ProxyConfigIDSet   bool                `json:"-"`
	Proxy              *bool               `json:"-"`
	SiteProxy          *string             `json:"-"`
	UseSystemProxy     *bool               `json:"-"`
	ExternalCheckinURL *string             `json:"external_checkin_url,omitempty"`
	ExternalCheckinSet bool                `json:"-"`
	IsPinned           *bool               `json:"is_pinned,omitempty"`
	SortOrder          *int                `json:"sort_order,omitempty"`
	GlobalWeight       *float64            `json:"global_weight,omitempty"`
	CustomHeader       *[]CustomHeader     `json:"custom_header,omitempty"`
	RouteBaseURLs      *[]SiteRouteBaseURL `json:"route_base_urls,omitempty"`
	Tags               *[]string           `json:"tags,omitempty"`
}

func (r *SiteUpdateRequest) UnmarshalJSON(data []byte) error {
	type alias SiteUpdateRequest
	var aux alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*r = SiteUpdateRequest(aux)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, r.ProxyConfigIDSet = raw["proxy_config_id"]
	_, r.ExternalCheckinSet = raw["external_checkin_url"]
	return nil
}

type SiteAccountUpdateRequest struct {
	ID                         int                 `json:"id" binding:"required"`
	Name                       *string             `json:"name,omitempty"`
	CredentialType             *SiteCredentialType `json:"credential_type,omitempty"`
	Username                   *string             `json:"username,omitempty"`
	Password                   *string             `json:"password,omitempty"`
	AccessToken                *string             `json:"access_token,omitempty"`
	APIKey                     *string             `json:"api_key,omitempty"`
	RefreshToken               *string             `json:"refresh_token,omitempty"`
	TokenExpiresAt             *int64              `json:"token_expires_at,omitempty"`
	PlatformUserID             *int                `json:"platform_user_id,omitempty"`
	PlatformUserIDSet          bool                `json:"-"`
	ProxyMode                  *ProxyUsageMode     `json:"proxy_mode,omitempty"`
	ProxyConfigID              *int                `json:"proxy_config_id,omitempty"`
	ProxyConfigIDSet           bool                `json:"-"`
	AccountProxy               *string             `json:"-"`
	Enabled                    *bool               `json:"enabled,omitempty"`
	AutoSync                   *bool               `json:"auto_sync,omitempty"`
	AutoCheckin                *bool               `json:"auto_checkin,omitempty"`
	RandomCheckin              *bool               `json:"random_checkin,omitempty"`
	CheckinIntervalHours       *int                `json:"checkin_interval_hours,omitempty"`
	CheckinRandomWindowMinutes *int                `json:"checkin_random_window_minutes,omitempty"`
}

func (r *SiteAccountUpdateRequest) UnmarshalJSON(data []byte) error {
	type alias SiteAccountUpdateRequest
	var aux alias
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*r = SiteAccountUpdateRequest(aux)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, r.PlatformUserIDSet = raw["platform_user_id"]
	_, r.ProxyConfigIDSet = raw["proxy_config_id"]
	return nil
}

type SiteSyncResult struct {
	AccountID       int                   `json:"account_id"`
	SiteID          int                   `json:"site_id"`
	Status          SiteExecutionStatus   `json:"status"`
	ChannelCount    int                   `json:"channel_count"`
	GroupCount      int                   `json:"group_count"`
	TokenCount      int                   `json:"token_count"`
	ModelCount      int                   `json:"model_count"`
	ManagedChannels []int                 `json:"managed_channels,omitempty"`
	Models          []string              `json:"models,omitempty"`
	GroupResults    []SiteSyncGroupResult `json:"group_results,omitempty"`
	Message         string                `json:"message"`
}

type SiteSyncGroupResult struct {
	GroupKey                string `json:"group_key"`
	GroupName               string `json:"group_name"`
	HasKey                  bool   `json:"has_key"`
	Status                  string `json:"status"`
	Authoritative           bool   `json:"authoritative"`
	ModelCount              int    `json:"model_count"`
	Message                 string `json:"message,omitempty"`
	ProjectionSuspended     bool   `json:"projection_suspended"`
	ProjectionSuspendReason string `json:"projection_suspend_reason,omitempty"`
}

type SiteCheckinResult struct {
	AccountID int                 `json:"account_id"`
	SiteID    int                 `json:"site_id"`
	Status    SiteExecutionStatus `json:"status"`
	Message   string              `json:"message"`
	Reward    string              `json:"reward,omitempty"`
}

type SiteBatchRequest struct {
	IDs    []int  `json:"ids" binding:"required"`
	Action string `json:"action" binding:"required"`
}

type SiteBatchResult struct {
	SuccessIDs  []int              `json:"success_ids"`
	FailedItems []SiteBatchFailure `json:"failed_items"`
}

type SiteBatchFailure struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

// SiteBatchEditRequest 批量编辑：对一批站点统一应用标签与 custom_header 修改补丁。
// 标签先添加后移除（同名时移除优先）。
// Upserts 按 header key 大小写不敏感 upsert（命中改值并保留已存的原始 key 大小写）；
// DeleteKeys 按 key 大小写不敏感删除；同一 key 同时出现时 DeleteKeys 优先（最终删除）。
type SiteBatchEditRequest struct {
	IDs        []int          `json:"ids" binding:"required"`
	AddTags    []string       `json:"add_tags"`
	RemoveTags []string       `json:"remove_tags"`
	Upserts    []CustomHeader `json:"upserts"`
	DeleteKeys []string       `json:"delete_keys"`
}

func NormalizeSiteGroupKey(value string) string {
	key := strings.TrimSpace(value)
	if key == "" {
		return SiteDefaultGroupKey
	}
	return key
}

const (
	SiteTagMaxLength = 32
	SiteTagsMaxCount = 20
)

func NormalizeSiteTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func ValidateSiteTags(tags []string) error {
	if len(tags) > SiteTagsMaxCount {
		return fmt.Errorf("site tags must not exceed %d", SiteTagsMaxCount)
	}
	for _, tag := range tags {
		if utf8.RuneCountInString(tag) > SiteTagMaxLength {
			return fmt.Errorf("site tag %q must not exceed %d characters", tag, SiteTagMaxLength)
		}
	}
	return nil
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

func NormalizeSiteSyncTokenValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "sk-") {
		return trimmed
	}
	return "sk-" + trimmed
}

// usesSyncTokenSkPrefix reports whether the platform follows the new-api family
// convention where tokens are surfaced without the "sk-" prefix but must carry
// it on upstream requests. Direct provider platforms (OpenAI/Claude/Gemini) use
// their keys verbatim, so they must never have a prefix forced on them.
func (p SitePlatform) usesSyncTokenSkPrefix() bool {
	switch p {
	case SitePlatformAPI:
		return false
	default:
		return true
	}
}

// NormalizeSiteSyncTokenValueForPlatform normalizes a sync token for upstream
// use according to the platform convention: new-api family platforms get the
// "sk-" prefix added when missing, while direct provider platforms keep the
// value verbatim (trimmed only).
func NormalizeSiteSyncTokenValueForPlatform(platform SitePlatform, value string) string {
	if platform.usesSyncTokenSkPrefix() {
		return NormalizeSiteSyncTokenValue(value)
	}
	return strings.TrimSpace(value)
}

func NormalizeComparableSiteTokenValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 3 && strings.EqualFold(trimmed[:3], "sk-") {
		return strings.TrimSpace(trimmed[3:])
	}
	return trimmed
}

func IsMaskedSiteTokenValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "*") || strings.Contains(trimmed, "•")
}

// SiteMaskedTokenMatches reports whether fullToken can plausibly produce
// maskedToken under the upstream masking scheme. Both sides are normalised so
// that an optional "sk-" prefix on either value does not break the comparison.
func SiteMaskedTokenMatches(fullToken string, maskedToken string) bool {
	normalizedFull := NormalizeComparableSiteTokenValue(fullToken)
	normalizedMasked := NormalizeComparableSiteTokenValue(maskedToken)
	if normalizedFull == "" || normalizedMasked == "" {
		return false
	}
	if !IsMaskedSiteTokenValue(normalizedMasked) {
		return normalizedFull == normalizedMasked
	}
	firstMask := strings.IndexAny(normalizedMasked, "*•")
	if firstMask < 0 {
		return normalizedFull == normalizedMasked
	}
	lastMask := strings.LastIndexAny(normalizedMasked, "*•")
	if lastMask < firstMask {
		return normalizedFull == normalizedMasked
	}
	maskedRunes := []rune(normalizedMasked)
	runeFirst := len([]rune(normalizedMasked[:firstMask]))
	runeLast := len([]rune(normalizedMasked[:lastMask])) + 1
	prefix := string(maskedRunes[:runeFirst])
	suffix := string(maskedRunes[runeLast:])
	if prefix == "" && suffix == "" {
		return false
	}
	if len(normalizedFull) < len(prefix)+len(suffix)+1 {
		return false
	}
	if prefix != "" && !strings.HasPrefix(normalizedFull, prefix) {
		return false
	}
	if suffix != "" && !strings.HasSuffix(normalizedFull, suffix) {
		return false
	}
	return true
}

func NormalizeSiteTokenValueStatus(value SiteTokenValueStatus, token string) SiteTokenValueStatus {
	if IsMaskedSiteTokenValue(token) {
		return SiteTokenValueStatusMaskedPending
	}
	_ = value
	return SiteTokenValueStatusReady
}

func IsReadySiteToken(token SiteToken) bool {
	return NormalizeSiteTokenValueStatus(token.ValueStatus, token.Token) == SiteTokenValueStatusReady
}

func NormalizeSiteModelRouteType(routeType SiteModelRouteType) SiteModelRouteType {
	switch routeType {
	case SiteModelRouteTypeOpenAIChat,
		SiteModelRouteTypeOpenAIResponse,
		SiteModelRouteTypeAnthropic,
		SiteModelRouteTypeGemini,
		SiteModelRouteTypeVolcengine,
		SiteModelRouteTypeOpenAIEmbedding,
		SiteModelRouteTypeUnknown:
		return routeType
	default:
		return SiteModelRouteTypeOpenAIChat
	}
}

func IsProjectedSiteModelRouteType(routeType SiteModelRouteType) bool {
	switch routeType {
	case SiteModelRouteTypeOpenAIChat,
		SiteModelRouteTypeOpenAIResponse,
		SiteModelRouteTypeAnthropic,
		SiteModelRouteTypeGemini,
		SiteModelRouteTypeVolcengine,
		SiteModelRouteTypeOpenAIEmbedding:
		return true
	default:
		return false
	}
}

func NormalizeSiteModelRouteSource(routeSource SiteModelRouteSource, manualOverride bool) SiteModelRouteSource {
	switch routeSource {
	case SiteModelRouteSourceSyncInferred,
		SiteModelRouteSourceManualOverride,
		SiteModelRouteSourceRuntimeLearned,
		SiteModelRouteSourceDefaultAssigned:
		return routeSource
	default:
		if manualOverride {
			return SiteModelRouteSourceManualOverride
		}
		return SiteModelRouteSourceSyncInferred
	}
}

func InferSiteModelRouteType(modelName string) SiteModelRouteType {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case strings.HasPrefix(lower, "claude"):
		return SiteModelRouteTypeAnthropic
	case strings.HasPrefix(lower, "gemini"):
		return SiteModelRouteTypeGemini
	case strings.Contains(lower, "embedding"):
		return SiteModelRouteTypeOpenAIEmbedding
	default:
		return SiteModelRouteTypeOpenAIChat
	}
}

func SiteModelRouteTypeSuffix(routeType SiteModelRouteType) string {
	switch NormalizeSiteModelRouteType(routeType) {
	case SiteModelRouteTypeOpenAIResponse:
		return "openai-response"
	case SiteModelRouteTypeAnthropic:
		return "anthropic"
	case SiteModelRouteTypeGemini:
		return "gemini"
	case SiteModelRouteTypeVolcengine:
		return "volcengine"
	case SiteModelRouteTypeOpenAIEmbedding:
		return "openai-embedding"
	default:
		return ""
	}
}

func SiteModelRouteTypeName(routeType SiteModelRouteType) string {
	switch NormalizeSiteModelRouteType(routeType) {
	case SiteModelRouteTypeOpenAIResponse:
		return "OpenAI Response"
	case SiteModelRouteTypeAnthropic:
		return "Anthropic"
	case SiteModelRouteTypeGemini:
		return "Gemini"
	case SiteModelRouteTypeVolcengine:
		return "Volcengine"
	case SiteModelRouteTypeOpenAIEmbedding:
		return "OpenAI Embedding"
	case SiteModelRouteTypeUnknown:
		return "Unsupported"
	default:
		return ""
	}
}

func CompactSiteModelRouteTypeName(routeType SiteModelRouteType) string {
	switch NormalizeSiteModelRouteType(routeType) {
	case SiteModelRouteTypeOpenAIChat:
		return "Chat"
	case SiteModelRouteTypeOpenAIResponse:
		return "Response"
	case SiteModelRouteTypeAnthropic:
		return "Anthropic"
	case SiteModelRouteTypeGemini:
		return "Gemini"
	case SiteModelRouteTypeVolcengine:
		return "Volcengine"
	case SiteModelRouteTypeOpenAIEmbedding:
		return "Embedding"
	case SiteModelRouteTypeUnknown:
		return "Unsupported"
	default:
		return "Chat"
	}
}

func ComposeSiteChannelBindingKey(groupKey string, routeType SiteModelRouteType, split bool) string {
	groupKey = NormalizeSiteGroupKey(groupKey)
	if !split {
		return groupKey
	}
	if suffix := SiteModelRouteTypeSuffix(routeType); suffix != "" {
		return groupKey + "::" + suffix
	}
	return groupKey
}

func ParseSiteChannelBindingKey(groupKey string) (string, SiteModelRouteType) {
	baseKey, suffix, found := strings.Cut(NormalizeSiteGroupKey(groupKey), "::")
	if !found {
		return baseKey, SiteModelRouteTypeOpenAIChat
	}
	switch suffix {
	case "openai-response":
		return baseKey, SiteModelRouteTypeOpenAIResponse
	case "anthropic":
		return baseKey, SiteModelRouteTypeAnthropic
	case "gemini":
		return baseKey, SiteModelRouteTypeGemini
	case "volcengine":
		return baseKey, SiteModelRouteTypeVolcengine
	case "openai-embedding":
		return baseKey, SiteModelRouteTypeOpenAIEmbedding
	default:
		return baseKey, SiteModelRouteTypeOpenAIChat
	}
}

func ShouldSplitSiteChannelRoutes(platform SitePlatform) bool {
	switch platform {
	case SitePlatformAPI:
		return false
	default:
		return true
	}
}

func (t SiteModelRouteType) ToOutboundType() outbound.OutboundType {
	switch NormalizeSiteModelRouteType(t) {
	case SiteModelRouteTypeOpenAIResponse:
		return outbound.OutboundTypeOpenAIResponse
	case SiteModelRouteTypeAnthropic:
		return outbound.OutboundTypeAnthropic
	case SiteModelRouteTypeGemini:
		return outbound.OutboundTypeGemini
	case SiteModelRouteTypeVolcengine:
		return outbound.OutboundTypeVolcengine
	case SiteModelRouteTypeOpenAIEmbedding:
		return outbound.OutboundTypeOpenAIEmbedding
	default:
		return outbound.OutboundTypeOpenAIChat
	}
}

func SiteModelRouteTypeFromOutboundType(t outbound.OutboundType) SiteModelRouteType {
	switch t {
	case outbound.OutboundTypeOpenAIResponse:
		return SiteModelRouteTypeOpenAIResponse
	case outbound.OutboundTypeAnthropic:
		return SiteModelRouteTypeAnthropic
	case outbound.OutboundTypeGemini:
		return SiteModelRouteTypeGemini
	case outbound.OutboundTypeVolcengine:
		return SiteModelRouteTypeVolcengine
	case outbound.OutboundTypeOpenAIEmbedding:
		return SiteModelRouteTypeOpenAIEmbedding
	default:
		return SiteModelRouteTypeOpenAIChat
	}
}

func (p SitePlatform) Validate() error {
	switch p {
	case SitePlatformNewAPI, SitePlatformAnyRouter, SitePlatformOneAPI, SitePlatformOneHub, SitePlatformDoneHub,
		SitePlatformSub2API, SitePlatformAPI:
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
	if s.ExternalCheckinURL != nil {
		trimmed := strings.TrimRight(strings.TrimSpace(*s.ExternalCheckinURL), "/")
		if trimmed == "" {
			s.ExternalCheckinURL = nil
		} else {
			s.ExternalCheckinURL = &trimmed
		}
	}
	if strings.TrimSpace(string(s.ProxyMode)) == "" {
		s.ProxyMode = ProxyUsageModeDirect
	}
	if s.ProxyMode != ProxyUsageModePool {
		s.ProxyConfigID = nil
	}
	if s.GlobalWeight <= 0 {
		s.GlobalWeight = 1
	}
	if s.SortOrder < 0 {
		s.SortOrder = 0
	}
	s.Tags = NormalizeSiteTags(s.Tags)
	s.RouteBaseURLs = NormalizeSiteRouteBaseURLs(s.RouteBaseURLs)
	s.normalizeLegacyAPIPlatform()
}

func (s *Site) normalizeLegacyAPIPlatform() {
	switch s.Platform {
	case "openai":
		s.Platform = SitePlatformAPI
		if s.DefaultRouteType == "" {
			s.DefaultRouteType = SiteModelRouteTypeOpenAIChat
		}
	case "claude":
		s.Platform = SitePlatformAPI
		if s.DefaultRouteType == "" {
			s.DefaultRouteType = SiteModelRouteTypeAnthropic
		}
	case "gemini":
		s.Platform = SitePlatformAPI
		if s.DefaultRouteType == "" {
			s.DefaultRouteType = SiteModelRouteTypeGemini
		}
	}
}

func (s *Site) ResolveDefaultRouteType() SiteModelRouteType {
	if s.DefaultRouteType != "" {
		return s.DefaultRouteType
	}
	return SiteModelRouteTypeOpenAIChat
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
	if err := s.ProxyMode.Validate(false); err != nil {
		return err
	}
	if s.ProxyMode == ProxyUsageModePool && (s.ProxyConfigID == nil || *s.ProxyConfigID <= 0) {
		return fmt.Errorf("proxy config id is required when proxy mode is pool")
	}
	if err := ValidateSiteTags(s.Tags); err != nil {
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
	if err := ValidateSiteRouteBaseURLs(s.RouteBaseURLs); err != nil {
		return err
	}
	if s.ExternalCheckinURL != nil {
		checkinParsed, err := url.Parse(*s.ExternalCheckinURL)
		if err != nil {
			return fmt.Errorf("external checkin url is invalid: %w", err)
		}
		if checkinParsed.Scheme != "http" && checkinParsed.Scheme != "https" {
			return fmt.Errorf("external checkin url must use http or https")
		}
		if checkinParsed.Host == "" {
			return fmt.Errorf("external checkin url must have a host")
		}
	}
	return nil
}

func (a *SiteAccount) Normalize() {
	a.Name = strings.TrimSpace(a.Name)
	a.Username = strings.TrimSpace(a.Username)
	a.Password = strings.TrimSpace(a.Password)
	a.AccessToken = strings.TrimSpace(a.AccessToken)
	a.APIKey = strings.TrimSpace(a.APIKey)
	a.RefreshToken = strings.TrimSpace(a.RefreshToken)
	if a.TokenExpiresAt < 0 {
		a.TokenExpiresAt = 0
	}
	if a.TokenExpiresAt > 0 && a.TokenExpiresAt < 1_000_000_000_000 {
		a.TokenExpiresAt *= 1000
	}
	if a.PlatformUserID != nil && *a.PlatformUserID <= 0 {
		a.PlatformUserID = nil
	}
	if a.AccountProxy != nil {
		trimmed := strings.TrimSpace(*a.AccountProxy)
		if trimmed == "" {
			a.AccountProxy = nil
		} else {
			a.AccountProxy = &trimmed
		}
	}
	if strings.TrimSpace(string(a.ProxyMode)) == "" {
		a.ProxyMode = ProxyUsageModeInherit
	}
	if a.ProxyMode != ProxyUsageModePool {
		a.ProxyConfigID = nil
	}
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
	if err := a.ProxyMode.Validate(true); err != nil {
		return err
	}
	if a.ProxyMode == ProxyUsageModePool && (a.ProxyConfigID == nil || *a.ProxyConfigID <= 0) {
		return fmt.Errorf("proxy config id is required when proxy mode is pool")
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
	if a.PlatformUserID != nil && *a.PlatformUserID <= 0 {
		return fmt.Errorf("platform user id must be greater than 0")
	}
	if a.TokenExpiresAt < 0 {
		return fmt.Errorf("token expires at must be greater than or equal to 0")
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
