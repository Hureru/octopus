package model

import "time"

// DBDump is a full-database JSON export format for Octopus.
// Import uses incremental semantics (insert new rows, and upsert on certain key-based tables).
type DBDump struct {
	Version      int       `json:"version"`
	ExportedAt   time.Time `json:"exported_at"`
	IncludeLogs  bool      `json:"include_logs"`
	IncludeStats bool      `json:"include_stats"`

	Channels             []Channel               `json:"channels,omitempty"`
	ChannelKeys          []ChannelKey            `json:"channel_keys,omitempty"`
	ProxyConfigurations  []ProxyConfiguration    `json:"proxy_configurations,omitempty"`
	Sites                []Site                  `json:"sites,omitempty"`
	SiteAccounts         []SiteAccount           `json:"site_accounts,omitempty"`
	SiteTokens           []SiteToken             `json:"site_tokens,omitempty"`
	SiteUserGroups       []SiteUserGroup         `json:"site_user_groups,omitempty"`
	SiteModels           []SiteModel             `json:"site_models,omitempty"`
	SiteChannelBindings  []SiteChannelBinding    `json:"site_channel_bindings,omitempty"`
	Groups               []Group                 `json:"groups,omitempty"`
	GroupItems           []GroupItem             `json:"group_items,omitempty"`
	LLMInfos             []LLMInfo               `json:"llm_infos,omitempty"`
	APIKeys              []APIKey                `json:"api_keys,omitempty"`
	Settings             []Setting               `json:"settings,omitempty"`
	CanonicalModels      []CanonicalModel        `json:"canonical_models,omitempty"`
	ModelAliases         []ModelAlias            `json:"model_aliases,omitempty"`
	RouteCandidates      []RouteCandidate        `json:"route_candidates,omitempty"`
	HeaderPolicies       []HeaderPolicy          `json:"header_policies,omitempty"`
	UserAgentProfiles    []UserAgentProfile      `json:"user_agent_profiles,omitempty"`
	SiteModelPriceQuotes []SiteModelPriceQuote   `json:"site_model_price_quotes,omitempty"`
	CurrencyRates        []CurrencyRate          `json:"currency_rates,omitempty"`
	ClashControllers     []ClashControllerBackup `json:"clash_controllers,omitempty"`
	SiteProxyPreferences []SiteProxyPreference   `json:"site_proxy_preferences,omitempty"`

	StatsTotal           []StatsTotal           `json:"stats_total,omitempty"`
	StatsDaily           []StatsDaily           `json:"stats_daily,omitempty"`
	StatsHourly          []StatsHourly          `json:"stats_hourly,omitempty"`
	StatsModel           []StatsModel           `json:"stats_model,omitempty"`
	StatsChannel         []StatsChannel         `json:"stats_channel,omitempty"`
	StatsAPIKey          []StatsAPIKey          `json:"stats_api_key,omitempty"`
	StatsSiteModelHourly []StatsSiteModelHourly `json:"stats_site_model_hourly,omitempty"`
	UsageRequestFacts    []UsageRequestFact     `json:"usage_request_facts,omitempty"`
	UsageAttemptFacts    []UsageAttemptFact     `json:"usage_attempt_facts,omitempty"`
	UsageAggregates      []UsageAggregate       `json:"usage_aggregates,omitempty"`

	RelayLogs             []RelayLog             `json:"relay_logs,omitempty"`
	RelayLogRepairAudits  []RelayLogRepairAudit  `json:"relay_log_repair_audits,omitempty"`
	SiteOperationAttempts []SiteOperationAttempt `json:"site_operation_attempts,omitempty"`
}

// ClashControllerBackup is the portable controller configuration. New
// ordinary exports intentionally omit the encrypted secret; legacy imports
// may still carry it and are rewrapped by the import security gate.
type ClashControllerBackup struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	APIURL          string    `json:"api_url"`
	ProxyURL        string    `json:"proxy_url"`
	GroupName       string    `json:"group_name"`
	SecretEncrypted string    `json:"secret_encrypted,omitempty"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type DBImportResult struct {
	// RowsAffected contains the rows affected for each table operation (insert/upsert depending on table).
	RowsAffected map[string]int64  `json:"rows_affected"`
	Warnings     []DBImportWarning `json:"warnings,omitempty"`
}

type DBImportWarning struct {
	Code         string `json:"code"`
	ResourceType string `json:"resource_type"`
	ResourceName string `json:"resource_name"`
	Message      string `json:"message"`
}
