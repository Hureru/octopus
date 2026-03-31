package model

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/transformer/outbound"
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

type Site struct {
	ID                 int            `json:"id" gorm:"primaryKey"`
	Name               string         `json:"name" gorm:"unique;not null"`
	Platform           SitePlatform   `json:"platform" gorm:"type:varchar(32);not null"`
	BaseURL            string         `json:"base_url" gorm:"not null"`
	Enabled            bool           `json:"enabled" gorm:"default:true"`
	Proxy              bool           `json:"proxy" gorm:"default:false"`
	SiteProxy          *string        `json:"site_proxy"`
	UseSystemProxy     bool           `json:"use_system_proxy" gorm:"default:false"`
	ExternalCheckinURL *string        `json:"external_checkin_url"`
	IsPinned           bool           `json:"is_pinned" gorm:"default:false"`
	SortOrder          int            `json:"sort_order" gorm:"default:0"`
	GlobalWeight       float64        `json:"global_weight" gorm:"default:1"`
	CustomHeader       []CustomHeader `json:"custom_header" gorm:"serializer:json"`
	Accounts           []SiteAccount  `json:"accounts,omitempty" gorm:"foreignKey:SiteID"`
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
	AccountProxy               *string              `json:"account_proxy"`
	Enabled                    bool                 `json:"enabled" gorm:"default:true"`
	AutoSync                   bool                 `json:"auto_sync" gorm:"default:true"`
	AutoCheckin                bool                 `json:"auto_checkin" gorm:"default:true"`
	RandomCheckin              bool                 `json:"random_checkin" gorm:"default:false"`
	CheckinIntervalHours       int                  `json:"checkin_interval_hours" gorm:"default:24"`
	CheckinRandomWindowMinutes int                  `json:"checkin_random_window_minutes" gorm:"default:120"`
	Balance                    float64              `json:"balance" gorm:"default:0"`
	BalanceUsed                float64              `json:"balance_used" gorm:"default:0"`
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
	GroupKey      string               `json:"group_key" gorm:"index"`
	GroupName     string               `json:"group_name"`
	Enabled       bool                 `json:"enabled" gorm:"default:true"`
	Source        string               `json:"source"`
	IsDefault     bool                 `json:"is_default" gorm:"default:false"`
	LastSyncAt    *time.Time           `json:"last_sync_at"`
}

type SiteUserGroup struct {
	ID            int    `json:"id" gorm:"primaryKey"`
	SiteAccountID int    `json:"site_account_id" gorm:"uniqueIndex:idx_site_account_group;not null"`
	GroupKey      string `json:"group_key" gorm:"uniqueIndex:idx_site_account_group;not null"`
	Name          string `json:"name"`
	RawPayload    string `json:"raw_payload"`
}

type SiteModel struct {
	ID              int                  `json:"id" gorm:"primaryKey"`
	SiteAccountID   int                  `json:"site_account_id" gorm:"uniqueIndex:idx_site_account_group_model;not null"`
	GroupKey        string               `json:"group_key" gorm:"uniqueIndex:idx_site_account_group_model;not null;default:'default'"`
	ModelName       string               `json:"model_name" gorm:"uniqueIndex:idx_site_account_group_model;not null"`
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
	GroupKey        string `json:"group_key" gorm:"uniqueIndex:idx_site_account_channel_group;not null"`
	ChannelID       int    `json:"channel_id" gorm:"uniqueIndex;not null"`
}

type SiteUpdateRequest struct {
	ID                 int             `json:"id" binding:"required"`
	Name               *string         `json:"name,omitempty"`
	Platform           *SitePlatform   `json:"platform,omitempty"`
	BaseURL            *string         `json:"base_url,omitempty"`
	Enabled            *bool           `json:"enabled,omitempty"`
	Proxy              *bool           `json:"proxy,omitempty"`
	SiteProxy          *string         `json:"site_proxy,omitempty"`
	UseSystemProxy     *bool           `json:"use_system_proxy,omitempty"`
	ExternalCheckinURL *string         `json:"external_checkin_url,omitempty"`
	IsPinned           *bool           `json:"is_pinned,omitempty"`
	SortOrder          *int            `json:"sort_order,omitempty"`
	GlobalWeight       *float64        `json:"global_weight,omitempty"`
	CustomHeader       *[]CustomHeader `json:"custom_header,omitempty"`
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
	AccountProxy               *string             `json:"account_proxy,omitempty"`
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

func IsMaskedSiteTokenValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "*") || strings.Contains(trimmed, "•")
}

func NormalizeSiteTokenValueStatus(value SiteTokenValueStatus, token string) SiteTokenValueStatus {
	if IsMaskedSiteTokenValue(token) {
		return SiteTokenValueStatusMaskedPending
	}
	if value == SiteTokenValueStatusMaskedPending {
		return SiteTokenValueStatusMaskedPending
	}
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
	case SitePlatformClaude, SitePlatformGemini, SitePlatformOpenAI:
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
	if s.ExternalCheckinURL != nil {
		trimmed := strings.TrimRight(strings.TrimSpace(*s.ExternalCheckinURL), "/")
		if trimmed == "" {
			s.ExternalCheckinURL = nil
		} else {
			s.ExternalCheckinURL = &trimmed
		}
	}
	if s.GlobalWeight <= 0 {
		s.GlobalWeight = 1
	}
	if s.SortOrder < 0 {
		s.SortOrder = 0
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
