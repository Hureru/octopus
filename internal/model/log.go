package model

import "time"

// AttemptStatus 尝试状态
type AttemptStatus string

const (
	AttemptSuccess      AttemptStatus = "success" // 转发成功
	AttemptFailed       AttemptStatus = "failed"  // 转发失败
	AttemptCanceled     AttemptStatus = "client_canceled"
	AttemptCircuitBreak AttemptStatus = "circuit_break" // 熔断跳过
	AttemptSkipped      AttemptStatus = "skipped"       // 其他原因跳过（禁用、无Key、类型不兼容等）
)

type RequestOutcome string

const (
	RequestOutcomeSuccess        RequestOutcome = "success"
	RequestOutcomeFailed         RequestOutcome = "failed"
	RequestOutcomeClientCanceled RequestOutcome = "client_canceled"
	RequestOutcomeIndeterminate  RequestOutcome = "indeterminate"
)

func (o RequestOutcome) IsSuccess() bool {
	return o == RequestOutcomeSuccess
}

type TransportTermination string

const (
	TransportTerminationUnknown                       TransportTermination = "unknown"
	TransportTerminationHTTPCompleted                 TransportTermination = "http_completed"
	TransportTerminationUpstreamEOF                   TransportTermination = "upstream_eof"
	TransportTerminationProtocolTerminal              TransportTermination = "protocol_terminal"
	TransportTerminationClientCanceled                TransportTermination = "client_canceled"
	TransportTerminationClientDisconnectedAfterFinish TransportTermination = "client_disconnected_after_finish"
	TransportTerminationFirstTokenTimeout             TransportTermination = "first_token_timeout"
	TransportTerminationReadError                     TransportTermination = "read_error"
	TransportTerminationWriteError                    TransportTermination = "write_error"
	TransportTerminationTransformError                TransportTermination = "transform_error"
	TransportTerminationUpstreamError                 TransportTermination = "upstream_error"
)

type CompletionEvidence string

const (
	CompletionEvidenceNone             CompletionEvidence = ""
	CompletionEvidenceHTTP2xx          CompletionEvidence = "http_2xx"
	CompletionEvidenceProtocolTerminal CompletionEvidence = "protocol_terminal"
	CompletionEvidenceUpstreamEOF      CompletionEvidence = "upstream_eof"
	CompletionEvidenceHistoricalRule   CompletionEvidence = "historical_repair_v1"
)

type AttemptAttribution string

const (
	AttemptAttributionNone     AttemptAttribution = ""
	AttemptAttributionUpstream AttemptAttribution = "upstream"
	AttemptAttributionClient   AttemptAttribution = "client"
	AttemptAttributionRelay    AttemptAttribution = "relay"
	AttemptAttributionPolicy   AttemptAttribution = "policy"
)

// AttemptUsageSnapshot captures usage and price evidence observed for one
// upstream attempt. It lives inside RelayLog.Attempts so no mutable channel or
// pricing state is consulted when analytics facts are written later.
type AttemptUsageSnapshot struct {
	InputTokens       int64            `json:"input_tokens"`
	OutputTokens      int64            `json:"output_tokens"`
	CacheReadTokens   int64            `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens  int64            `json:"cache_write_tokens,omitempty"`
	CostUSD           float64          `json:"cost_usd,omitempty"`
	FTUTMS            int64            `json:"ftut_ms,omitempty"`
	FTUTKnown         bool             `json:"ftut_known,omitempty"`
	TokenSource       UsageValueSource `json:"token_source,omitempty"`
	PriceQuoteID      int              `json:"price_quote_id,omitempty"`
	PriceSource       PriceQuoteSource `json:"price_source,omitempty"`
	PriceUnit         PriceUnit        `json:"price_unit,omitempty"`
	PriceCurrency     string           `json:"price_currency,omitempty"`
	PriceInput        float64          `json:"price_input,omitempty"`
	PriceOutput       float64          `json:"price_output,omitempty"`
	PriceCacheRead    float64          `json:"price_cache_read,omitempty"`
	PriceCacheWrite   float64          `json:"price_cache_write,omitempty"`
	PricePerRequest   float64          `json:"price_per_request,omitempty"`
	PriceMultiplier   float64          `json:"price_multiplier,omitempty"`
	PriceRateToUSD    float64          `json:"price_rate_to_usd,omitempty"`
	PriceObservedAt   *time.Time       `json:"price_observed_at,omitempty"`
	PriceStale        bool             `json:"price_stale,omitempty"`
	PriceConvertible  bool             `json:"price_convertible,omitempty"`
	PriceOriginalCost float64          `json:"price_original_cost,omitempty"`
	PriceMatchReason  string           `json:"price_match_reason,omitempty"`
}

// ChannelAttempt 记录单次渠道尝试的决策和结果
type ChannelAttempt struct {
	ChannelID          int                   `json:"channel_id"`
	ChannelKeyID       int                   `json:"channel_key_id,omitempty"`
	ChannelName        string                `json:"channel_name"`
	ModelName          string                `json:"model_name"`
	RouteCandidateID   int                   `json:"route_candidate_id,omitempty"`
	AttemptNum         int                   `json:"attempt_num"`
	Status             AttemptStatus         `json:"status"`
	StatusCode         int                   `json:"status_code,omitempty"`
	Outcome            RequestOutcome        `json:"outcome,omitempty"`
	Attribution        AttemptAttribution    `json:"attribution,omitempty"`
	CompletionEvidence CompletionEvidence    `json:"completion_evidence,omitempty"`
	Duration           int                   `json:"duration"`
	Sticky             bool                  `json:"sticky,omitempty"`
	Msg                string                `json:"msg,omitempty"`
	Usage              *AttemptUsageSnapshot `json:"usage,omitempty"`
}

// RelayLogWSMode 表示本次上游 WebSocket 的会话/恢复模式。
type RelayLogWSMode string

const (
	RelayLogWSModeFresh        RelayLogWSMode = "fresh"        // 新建 WS 会话
	RelayLogWSModeContinuation RelayLogWSMode = "continuation" // 直接续传上游会话
	RelayLogWSModeReplay       RelayLogWSMode = "replay"       // 续传失败后回放上下文
)

// RelayLogWSExecMode 表示本次上游 WebSocket 的事件处理方式。
type RelayLogWSExecMode string

const (
	RelayLogWSExecModePassthrough RelayLogWSExecMode = "passthrough" // 原生 WS 事件直通
	RelayLogWSExecModeTransform   RelayLogWSExecMode = "transform"   // 经内部 transformer 管线转换
)

// RelayLogWSRecovery 表示本次会话在执行过程中触发的恢复动作。
type RelayLogWSRecovery string

const (
	RelayLogWSRecoveryReconnect RelayLogWSRecovery = "reconnect" // 续传链路失效后，原链路强制重连成功
	RelayLogWSRecoveryReplay    RelayLogWSRecovery = "replay"    // 续传失败后回放上下文成功
	RelayLogWSRecoveryDowngrade RelayLogWSRecovery = "downgrade" // WebSocket 不可用后降级到 HTTP
)

type RelayLog struct {
	ID                   int64                `json:"id" gorm:"primaryKey;autoIncrement:false"` // Snowflake ID
	Time                 int64                `json:"time"`                                     // 时间戳（秒）
	RequestModelName     string               `json:"request_model_name"`                       // 请求模型名称
	RequestAPIKeyID      int                  `json:"request_api_key_id,omitempty" gorm:"index"`
	RequestAPIKeyName    string               `json:"request_api_key_name"` // 请求使用的 API Key 名称
	ChannelId            int                  `json:"channel" gorm:"index"` // 实际使用的渠道ID
	ChannelName          string               `json:"channel_name"`         // 渠道名称
	ActualModelName      string               `json:"actual_model_name"`    // 实际使用模型名称
	CanonicalModelName   string               `json:"canonical_model_name,omitempty" gorm:"size:191;index"`
	RouteCandidateID     int                  `json:"route_candidate_id,omitempty" gorm:"index"`
	InboundProtocol      ProtocolName         `json:"inbound_protocol,omitempty" gorm:"type:varchar(32)"`
	OutboundProtocol     ProtocolName         `json:"outbound_protocol,omitempty" gorm:"type:varchar(32)"`
	ProtocolMode         string               `json:"protocol_mode,omitempty" gorm:"type:varchar(24)"`
	ProtocolPolicy       ProtocolPolicy       `json:"protocol_policy,omitempty" gorm:"type:varchar(32)"`
	ProtocolAllowLossy   bool                 `json:"protocol_allow_lossy" gorm:"not null;default:false"`
	ProtocolWarnings     []string             `json:"protocol_warnings,omitempty" gorm:"serializer:json"`
	ProtocolFailureStage ProtocolFailureStage `json:"protocol_failure_stage,omitempty" gorm:"type:varchar(48)"`
	InputTokens          int                  `json:"input_tokens"`                     // 输入Token
	TransportInputTokens *int                 `json:"transport_input_tokens,omitempty"` // 实际发送到上游请求体的 Token 估算
	BillInputTokens      *int                 `json:"bill_input_tokens,omitempty"`      // 按常规输入价格计费的 Token
	CacheReadTokens      *int                 `json:"cache_read_tokens,omitempty"`      // 从缓存读取的 Token
	CacheWriteTokens     *int                 `json:"cache_write_tokens,omitempty"`     // 写入缓存的 Token
	OutputTokens         int                  `json:"output_tokens"`                    // 输出 Token
	Ftut                 int                  `json:"ftut"`                             // 首字时间(毫秒)
	UseTime              int                  `json:"use_time"`                         // 总用时(毫秒)
	Cost                 float64              `json:"cost"`                             // 消耗费用
	PriceQuoteID         int                  `json:"price_quote_id,omitempty" gorm:"index"`
	PriceSource          PriceQuoteSource     `json:"price_source,omitempty" gorm:"type:varchar(32)"`
	PriceUnit            PriceUnit            `json:"price_unit,omitempty" gorm:"type:varchar(32)"`
	PriceCurrency        string               `json:"price_currency,omitempty" gorm:"size:16"`
	PriceInput           float64              `json:"price_input,omitempty"`
	PriceOutput          float64              `json:"price_output,omitempty"`
	PriceCacheRead       float64              `json:"price_cache_read,omitempty"`
	PriceCacheWrite      float64              `json:"price_cache_write,omitempty"`
	PricePerRequest      float64              `json:"price_per_request,omitempty"`
	PriceGroupMultiplier float64              `json:"price_group_multiplier,omitempty"`
	PriceExchangeRateUSD float64              `json:"price_exchange_rate_usd,omitempty"`
	PriceObservedAt      *time.Time           `json:"price_observed_at,omitempty"`
	PriceStale           bool                 `json:"price_stale" gorm:"not null;default:false"`
	PriceConvertible     bool                 `json:"price_convertible" gorm:"not null;default:false"`
	PriceOriginalCost    float64              `json:"price_original_cost,omitempty"`
	PriceMatchReason     string               `json:"price_match_reason,omitempty" gorm:"type:text"`
	TokenSource          UsageValueSource     `json:"token_source,omitempty" gorm:"type:varchar(16);not null;default:'unknown'"`
	RequestContent       string               `json:"request_content"`                       // 请求内容
	ResponseContent      string               `json:"response_content"`                      // 响应内容
	Error                string               `json:"error"`                                 // 错误信息
	Success              bool                 `json:"success" gorm:"not null;default:false"` // 是否成功，便于状态筛选索引
	Outcome              RequestOutcome       `json:"outcome" gorm:"type:varchar(24);not null;default:'';index"`
	TransportTermination TransportTermination `json:"transport_termination" gorm:"type:varchar(48);not null;default:''"`
	CompletionEvidence   CompletionEvidence   `json:"completion_evidence" gorm:"type:varchar(48);not null;default:''"`
	TerminalEvent        string               `json:"terminal_event,omitempty" gorm:"type:varchar(64)"`
	HeaderPolicyTrace    string               `json:"header_policy_trace,omitempty" gorm:"type:text"`
	Attempts             []ChannelAttempt     `json:"attempts" gorm:"serializer:json"` // 所有尝试记录
	TotalAttempts        int                  `json:"total_attempts"`                  // 总尝试次数
	UsedWS               bool                 `json:"used_ws" gorm:"default:false"`    // 是否使用了上游WebSocket
	WSMode               *RelayLogWSMode      `json:"ws_mode,omitempty"`               // 上游 WebSocket 会话模式
	WSExecMode           *RelayLogWSExecMode  `json:"ws_exec_mode,omitempty"`          // 上游 WebSocket 事件处理方式
	WSRecovery           *RelayLogWSRecovery  `json:"ws_recovery,omitempty"`           // 本次请求触发的恢复动作
	OriginalOutcome      RequestOutcome       `json:"original_outcome,omitempty" gorm:"type:varchar(24)"`
	OriginalError        string               `json:"original_error,omitempty"`
	RepairBatchID        string               `json:"repair_batch_id,omitempty" gorm:"type:varchar(64);index"`
	RepairRuleVersion    string               `json:"repair_rule_version,omitempty" gorm:"type:varchar(32)"`
	RepairedAt           *time.Time           `json:"repaired_at,omitempty"`
}

type RelayLogRepairAudit struct {
	ID          int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	BatchID     string    `json:"batch_id" gorm:"type:varchar(64);not null;index"`
	RuleVersion string    `json:"rule_version" gorm:"type:varchar(32);not null"`
	DryRun      bool      `json:"dry_run" gorm:"not null;default:false"`
	Matched     int       `json:"matched"`
	Updated     int       `json:"updated"`
	Excluded    int       `json:"excluded"`
	RequestedAt time.Time `json:"requested_at"`
	CompletedAt time.Time `json:"completed_at"`
}
