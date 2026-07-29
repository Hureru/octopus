package model

import "time"

type UsageValueSource string

const (
	UsageValueSourceReported  UsageValueSource = "reported"
	UsageValueSourceEstimated UsageValueSource = "estimated"
	UsageValueSourceUnknown   UsageValueSource = "unknown"
)

type UsageRequestFact struct {
	RelayLogID        int64            `json:"relay_log_id" gorm:"primaryKey;autoIncrement:false"`
	Time              int64            `json:"time" gorm:"not null;index:idx_usage_request_time;index:idx_usage_request_site_time,priority:2;index:idx_usage_request_account_time,priority:2;index:idx_usage_request_channel_time,priority:2;index:idx_usage_request_api_key_time,priority:2;index:idx_usage_request_model_time,priority:2"`
	SiteID            int              `json:"site_id" gorm:"index:idx_usage_request_site_time,priority:1"`
	SiteName          string           `json:"site_name" gorm:"size:191"`
	SiteAccountID     int              `json:"site_account_id" gorm:"index:idx_usage_request_account_time,priority:1"`
	SiteAccountName   string           `json:"site_account_name" gorm:"size:191"`
	ChannelID         int              `json:"channel_id" gorm:"index:idx_usage_request_channel_time,priority:1"`
	ChannelName       string           `json:"channel_name" gorm:"size:191"`
	APIKeyID          int              `json:"api_key_id" gorm:"index:idx_usage_request_api_key_time,priority:1"`
	APIKeyName        string           `json:"api_key_name" gorm:"size:191"`
	RouteCandidateID  int              `json:"route_candidate_id" gorm:"index"`
	RequestModel      string           `json:"request_model" gorm:"size:191;index:idx_usage_request_model_time,priority:1"`
	ActualModel       string           `json:"actual_model" gorm:"size:191;index"`
	CanonicalModel    string           `json:"canonical_model" gorm:"size:191;index"`
	Outcome           RequestOutcome   `json:"outcome" gorm:"type:varchar(24);not null;index"`
	InputTokens       int64            `json:"input_tokens" gorm:"bigint"`
	OutputTokens      int64            `json:"output_tokens" gorm:"bigint"`
	CacheReadTokens   int64            `json:"cache_read_tokens" gorm:"bigint"`
	CacheWriteTokens  int64            `json:"cache_write_tokens" gorm:"bigint"`
	CostUSD           float64          `json:"cost_usd" gorm:"type:real"`
	DurationMS        int64            `json:"duration_ms" gorm:"bigint"`
	FTUTMS            int64            `json:"ftut_ms" gorm:"column:ftut_ms;bigint"`
	FTUTKnown         bool             `json:"ftut_known" gorm:"column:ftut_known;not null;default:false"`
	TokenSource       UsageValueSource `json:"token_source" gorm:"type:varchar(16);not null;default:'unknown'"`
	PriceQuoteID      int              `json:"price_quote_id" gorm:"index"`
	PriceSource       PriceQuoteSource `json:"price_source" gorm:"type:varchar(32)"`
	PriceUnit         PriceUnit        `json:"price_unit" gorm:"type:varchar(32)"`
	PriceCurrency     string           `json:"price_currency" gorm:"size:16;index"`
	PriceInput        float64          `json:"price_input" gorm:"type:real"`
	PriceOutput       float64          `json:"price_output" gorm:"type:real"`
	PriceCacheRead    float64          `json:"price_cache_read" gorm:"type:real"`
	PriceCacheWrite   float64          `json:"price_cache_write" gorm:"type:real"`
	PricePerRequest   float64          `json:"price_per_request" gorm:"type:real"`
	PriceMultiplier   float64          `json:"price_multiplier" gorm:"type:real"`
	PriceRateToUSD    float64          `json:"price_rate_to_usd" gorm:"type:real"`
	PriceObservedAt   *time.Time       `json:"price_observed_at,omitempty"`
	PriceStale        bool             `json:"price_stale" gorm:"not null;default:false"`
	PriceConvertible  bool             `json:"price_convertible" gorm:"not null;default:false"`
	PriceOriginalCost float64          `json:"price_original_cost" gorm:"type:real"`
	PriceMatchReason  string           `json:"price_match_reason" gorm:"type:text"`
	InboundProtocol   ProtocolName     `json:"inbound_protocol" gorm:"type:varchar(32)"`
	OutboundProtocol  ProtocolName     `json:"outbound_protocol" gorm:"type:varchar(32)"`
	ProtocolMode      string           `json:"protocol_mode" gorm:"size:24"`
	AggregatedAt      *time.Time       `json:"aggregated_at,omitempty" gorm:"index"`
}

type UsageAttemptFact struct {
	RelayLogID        int64              `json:"relay_log_id" gorm:"primaryKey;autoIncrement:false"`
	AttemptNumber     int                `json:"attempt_number" gorm:"primaryKey;autoIncrement:false"`
	Time              int64              `json:"time" gorm:"not null;index:idx_usage_attempt_time;index:idx_usage_attempt_site_time,priority:2;index:idx_usage_attempt_account_time,priority:2;index:idx_usage_attempt_channel_time,priority:2;index:idx_usage_attempt_api_key_time,priority:2;index:idx_usage_attempt_model_time,priority:2"`
	SiteID            int                `json:"site_id" gorm:"index:idx_usage_attempt_site_time,priority:1"`
	SiteName          string             `json:"site_name" gorm:"size:191"`
	SiteAccountID     int                `json:"site_account_id" gorm:"index:idx_usage_attempt_account_time,priority:1"`
	SiteAccountName   string             `json:"site_account_name" gorm:"size:191"`
	ChannelID         int                `json:"channel_id" gorm:"index:idx_usage_attempt_channel_time,priority:1"`
	ChannelName       string             `json:"channel_name" gorm:"size:191"`
	APIKeyID          int                `json:"api_key_id" gorm:"index:idx_usage_attempt_api_key_time,priority:1"`
	APIKeyName        string             `json:"api_key_name" gorm:"size:191"`
	RouteCandidateID  int                `json:"route_candidate_id" gorm:"index"`
	RequestModel      string             `json:"request_model" gorm:"size:191;index:idx_usage_attempt_model_time,priority:1"`
	ActualModel       string             `json:"actual_model" gorm:"size:191;index"`
	CanonicalModel    string             `json:"canonical_model" gorm:"size:191;index"`
	UpstreamModel     string             `json:"upstream_model" gorm:"size:191"`
	Status            AttemptStatus      `json:"status" gorm:"type:varchar(24);not null"`
	StatusCode        int                `json:"status_code"`
	Outcome           RequestOutcome     `json:"outcome" gorm:"type:varchar(24);not null;index"`
	Attribution       AttemptAttribution `json:"attribution" gorm:"type:varchar(24)"`
	InputTokens       int64              `json:"input_tokens" gorm:"bigint"`
	OutputTokens      int64              `json:"output_tokens" gorm:"bigint"`
	CacheReadTokens   int64              `json:"cache_read_tokens" gorm:"bigint"`
	CacheWriteTokens  int64              `json:"cache_write_tokens" gorm:"bigint"`
	CostUSD           float64            `json:"cost_usd" gorm:"type:real"`
	DurationMS        int64              `json:"duration_ms" gorm:"bigint"`
	FTUTMS            int64              `json:"ftut_ms" gorm:"column:ftut_ms;bigint"`
	FTUTKnown         bool               `json:"ftut_known" gorm:"column:ftut_known;not null;default:false"`
	UsageAttributed   bool               `json:"usage_attributed" gorm:"not null;default:false"`
	TokenSource       UsageValueSource   `json:"token_source" gorm:"type:varchar(16);not null;default:'unknown'"`
	PriceQuoteID      int                `json:"price_quote_id" gorm:"index"`
	PriceSource       PriceQuoteSource   `json:"price_source" gorm:"type:varchar(32)"`
	PriceUnit         PriceUnit          `json:"price_unit" gorm:"type:varchar(32)"`
	PriceCurrency     string             `json:"price_currency" gorm:"size:16;index"`
	PriceInput        float64            `json:"price_input" gorm:"type:real"`
	PriceOutput       float64            `json:"price_output" gorm:"type:real"`
	PriceCacheRead    float64            `json:"price_cache_read" gorm:"type:real"`
	PriceCacheWrite   float64            `json:"price_cache_write" gorm:"type:real"`
	PricePerRequest   float64            `json:"price_per_request" gorm:"type:real"`
	PriceMultiplier   float64            `json:"price_multiplier" gorm:"type:real"`
	PriceRateToUSD    float64            `json:"price_rate_to_usd" gorm:"type:real"`
	PriceObservedAt   *time.Time         `json:"price_observed_at,omitempty"`
	PriceStale        bool               `json:"price_stale" gorm:"not null;default:false"`
	PriceConvertible  bool               `json:"price_convertible" gorm:"not null;default:false"`
	PriceOriginalCost float64            `json:"price_original_cost" gorm:"type:real"`
	PriceMatchReason  string             `json:"price_match_reason" gorm:"type:text"`
	InboundProtocol   ProtocolName       `json:"inbound_protocol" gorm:"type:varchar(32)"`
	OutboundProtocol  ProtocolName       `json:"outbound_protocol" gorm:"type:varchar(32)"`
	ProtocolMode      string             `json:"protocol_mode" gorm:"size:24"`
	AggregatedAt      *time.Time         `json:"aggregated_at,omitempty" gorm:"index"`
}

type UsageAggregateGranularity string

const (
	UsageAggregateHourly UsageAggregateGranularity = "hour"
	UsageAggregateDaily  UsageAggregateGranularity = "day"
)

type UsageAggregate struct {
	AggregateKey       string                    `json:"aggregate_key" gorm:"size:64;primaryKey"`
	Granularity        UsageAggregateGranularity `json:"granularity" gorm:"type:varchar(8);not null;index:idx_usage_aggregate_window,priority:1"`
	MetricScope        string                    `json:"metric_scope" gorm:"type:varchar(16);not null;index:idx_usage_aggregate_window,priority:2"`
	BucketStart        int64                     `json:"bucket_start" gorm:"not null;index:idx_usage_aggregate_window,priority:3"`
	SiteID             int                       `json:"site_id" gorm:"index"`
	SiteName           string                    `json:"site_name" gorm:"size:191"`
	SiteAccountID      int                       `json:"site_account_id" gorm:"index"`
	SiteAccountName    string                    `json:"site_account_name" gorm:"size:191"`
	ChannelID          int                       `json:"channel_id" gorm:"index"`
	ChannelName        string                    `json:"channel_name" gorm:"size:191"`
	APIKeyID           int                       `json:"api_key_id" gorm:"index"`
	APIKeyName         string                    `json:"api_key_name" gorm:"size:191"`
	RequestModel       string                    `json:"request_model" gorm:"size:191;index"`
	ActualModel        string                    `json:"actual_model" gorm:"size:191;index"`
	CanonicalModel     string                    `json:"canonical_model" gorm:"size:191;index"`
	MetricCount        int64                     `json:"metric_count"`
	SuccessCount       int64                     `json:"success_count"`
	FailedCount        int64                     `json:"failed_count"`
	CanceledCount      int64                     `json:"canceled_count"`
	IndeterminateCount int64                     `json:"indeterminate_count"`
	InputTokens        int64                     `json:"input_tokens"`
	OutputTokens       int64                     `json:"output_tokens"`
	CacheReadTokens    int64                     `json:"cache_read_tokens"`
	CacheWriteTokens   int64                     `json:"cache_write_tokens"`
	CostUSD            float64                   `json:"cost_usd" gorm:"type:real"`
	DurationSumMS      int64                     `json:"duration_sum_ms"`
	DurationMaxMS      int64                     `json:"duration_max_ms"`
	DurationHistogram  []uint64                  `json:"duration_histogram" gorm:"serializer:json"`
	FTUTSumMS          int64                     `json:"ftut_sum_ms" gorm:"column:ftut_sum_ms"`
	FTUTMaxMS          int64                     `json:"ftut_max_ms" gorm:"column:ftut_max_ms"`
	FTUTSamples        int64                     `json:"ftut_samples" gorm:"column:ftut_samples"`
	FTUTHistogram      []uint64                  `json:"ftut_histogram" gorm:"column:ftut_histogram;serializer:json"`
	ReportedCount      int64                     `json:"reported_count"`
	EstimatedCount     int64                     `json:"estimated_count"`
	UnknownTokenCount  int64                     `json:"unknown_token_count"`
	UnknownPriceCount  int64                     `json:"unknown_price_count"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
}
