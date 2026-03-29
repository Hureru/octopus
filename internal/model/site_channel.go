package model

type SiteChannelCard struct {
	SiteID       int                  `json:"site_id"`
	SiteName     string               `json:"site_name"`
	Platform     SitePlatform         `json:"platform"`
	Enabled      bool                 `json:"enabled"`
	AccountCount int                  `json:"account_count"`
	Accounts     []SiteChannelAccount `json:"accounts"`
}

type SiteChannelAccount struct {
	SiteID         int                `json:"site_id"`
	AccountID      int                `json:"account_id"`
	AccountName    string             `json:"account_name"`
	Enabled        bool               `json:"enabled"`
	AutoSync       bool               `json:"auto_sync"`
	GroupCount     int                `json:"group_count"`
	ModelCount     int                `json:"model_count"`
	Groups         []SiteChannelGroup `json:"groups"`
	RouteSummaries []SiteRouteSummary `json:"route_summaries"`
}

type SiteRouteSummary struct {
	RouteType SiteModelRouteType `json:"route_type"`
	Count     int                `json:"count"`
}

type SiteChannelGroup struct {
	GroupKey            string             `json:"group_key"`
	GroupName           string             `json:"group_name"`
	KeyCount            int                `json:"key_count"`
	EnabledKeyCount     int                `json:"enabled_key_count"`
	HasKeys             bool               `json:"has_keys"`
	HasProjectedChannel bool               `json:"has_projected_channel"`
	ProjectedChannelIDs []int              `json:"projected_channel_ids"`
	Models              []SiteChannelModel `json:"models"`
}

type SiteChannelModel struct {
	ModelName          string                   `json:"model_name"`
	RouteType          SiteModelRouteType       `json:"route_type"`
	RouteSource        SiteModelRouteSource     `json:"route_source"`
	ManualOverride     bool                     `json:"manual_override"`
	Disabled           bool                     `json:"disabled"`
	ProjectedChannelID *int                     `json:"projected_channel_id,omitempty"`
	RouteMetadata      *SiteModelRouteMetadata  `json:"route_metadata,omitempty"`
	History            *SiteModelHistorySummary `json:"history,omitempty"`
}

type SiteModelHistorySummary struct {
	SuccessCount  int                     `json:"success_count"`
	FailureCount  int                     `json:"failure_count"`
	LastRequestAt *int64                  `json:"last_request_at,omitempty"`
	Recent        []SiteModelHistoryEntry `json:"recent"`
}

type SiteModelHistoryEntry struct {
	Time         int64              `json:"time"`
	Success      bool               `json:"success"`
	RouteType    SiteModelRouteType `json:"route_type"`
	ChannelID    int                `json:"channel_id"`
	ChannelName  string             `json:"channel_name"`
	RequestModel string             `json:"request_model"`
	ActualModel  string             `json:"actual_model"`
	Error        string             `json:"error,omitempty"`
}

type SiteModelRouteUpdateRequest struct {
	GroupKey        string             `json:"group_key" binding:"required"`
	ModelName       string             `json:"model_name" binding:"required"`
	RouteType       SiteModelRouteType `json:"route_type" binding:"required"`
	RouteRawPayload string             `json:"route_raw_payload,omitempty"`
}

type SiteModelDisableUpdateRequest struct {
	GroupKey  string `json:"group_key" binding:"required"`
	ModelName string `json:"model_name" binding:"required"`
	Disabled  bool   `json:"disabled"`
}

type SiteChannelKeyCreateRequest struct {
	GroupKey string `json:"group_key" binding:"required"`
	Name     string `json:"name,omitempty"`
}
