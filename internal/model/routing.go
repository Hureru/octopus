package model

import "time"

type ProtocolName string

const (
	ProtocolOpenAIChat      ProtocolName = "openai_chat"
	ProtocolOpenAIResponses ProtocolName = "openai_responses"
	ProtocolAnthropic       ProtocolName = "anthropic"
	ProtocolGemini          ProtocolName = "gemini"
	ProtocolVolcengine      ProtocolName = "volcengine"
	ProtocolOpenAIEmbedding ProtocolName = "openai_embedding"
	ProtocolUnknown         ProtocolName = "unknown"
)

type ProtocolPolicy string

const (
	ProtocolPolicyInherit          ProtocolPolicy = "inherit"
	ProtocolPolicyAuto             ProtocolPolicy = "auto"
	ProtocolPolicyPassthroughOnly  ProtocolPolicy = "passthrough-only"
	ProtocolPolicyTransformAllowed ProtocolPolicy = "transform-allowed"
)

func (p ProtocolPolicy) Normalize(defaultValue ProtocolPolicy) ProtocolPolicy {
	switch p {
	case ProtocolPolicyAuto, ProtocolPolicyPassthroughOnly, ProtocolPolicyTransformAllowed:
		return p
	case ProtocolPolicyInherit, "":
		return defaultValue
	default:
		return defaultValue
	}
}

type ProtocolFeature string

const (
	ProtocolFeatureFunctionTools        ProtocolFeature = "function_tools"
	ProtocolFeatureBuiltInTools         ProtocolFeature = "built_in_tools"
	ProtocolFeatureStructuredOutput     ProtocolFeature = "structured_output"
	ProtocolFeatureImages               ProtocolFeature = "images"
	ProtocolFeatureAudio                ProtocolFeature = "audio"
	ProtocolFeatureFiles                ProtocolFeature = "files"
	ProtocolFeatureDocuments            ProtocolFeature = "documents"
	ProtocolFeatureReasoning            ProtocolFeature = "reasoning"
	ProtocolFeatureCacheControl         ProtocolFeature = "cache_control"
	ProtocolFeatureContinuation         ProtocolFeature = "continuation"
	ProtocolFeatureResponsesState       ProtocolFeature = "responses_state"
	ProtocolFeatureProviderExtensions   ProtocolFeature = "provider_extensions"
	ProtocolFeatureAnthropicMCP         ProtocolFeature = "anthropic_mcp"
	ProtocolFeatureAnthropicContainer   ProtocolFeature = "anthropic_container"
	ProtocolFeatureAnthropicServerTools ProtocolFeature = "anthropic_server_tools"
	ProtocolFeatureGeminiExtensions     ProtocolFeature = "gemini_extensions"
	ProtocolFeatureWebSocket            ProtocolFeature = "websocket"
)

type FeatureCapability string

const (
	FeatureCapabilityNative      FeatureCapability = "native"
	FeatureCapabilityTransformed FeatureCapability = "transformed"
	FeatureCapabilityDegraded    FeatureCapability = "degraded"
	FeatureCapabilityUnsupported FeatureCapability = "unsupported"
)

type ProtocolExecutionMode string

const (
	ProtocolExecutionModePassthrough ProtocolExecutionMode = "passthrough"
	ProtocolExecutionModeTransform   ProtocolExecutionMode = "transform"
)

type ProtocolCompatibility string

const (
	ProtocolCompatibilityExact       ProtocolCompatibility = "exact"
	ProtocolCompatibilityLossy       ProtocolCompatibility = "lossy"
	ProtocolCompatibilityUnsupported ProtocolCompatibility = "unsupported"
)

type ProtocolFailureStage string

const (
	ProtocolFailureStageRoutePlanning             ProtocolFailureStage = "route_planning"
	ProtocolFailureStageRequestTransform          ProtocolFailureStage = "request_transform"
	ProtocolFailureStageOutboundStreamTransform   ProtocolFailureStage = "outbound_stream_transform"
	ProtocolFailureStageInboundStreamTransform    ProtocolFailureStage = "inbound_stream_transform"
	ProtocolFailureStageOutboundResponseTransform ProtocolFailureStage = "outbound_response_transform"
	ProtocolFailureStageInboundResponseTransform  ProtocolFailureStage = "inbound_response_transform"
)

type ProtocolRouteRequirements struct {
	InboundProtocol ProtocolName      `json:"inbound_protocol"`
	Features        []ProtocolFeature `json:"features,omitempty"`
}

type ProtocolFeatureDecision struct {
	Feature    ProtocolFeature   `json:"feature"`
	Capability FeatureCapability `json:"capability"`
	Reason     string            `json:"reason,omitempty"`
}

type ProtocolCapabilityDescriptor struct {
	InboundProtocol  ProtocolName              `json:"inbound_protocol"`
	OutboundProtocol ProtocolName              `json:"outbound_protocol"`
	Mode             ProtocolExecutionMode     `json:"mode"`
	Limited          bool                      `json:"limited,omitempty"`
	Features         []ProtocolFeatureDecision `json:"features"`
}

type RoutingStrategy string

const (
	RoutingStrategyBalanced      RoutingStrategy = "balanced"
	RoutingStrategyReliability   RoutingStrategy = "reliability"
	RoutingStrategyLowestCost    RoutingStrategy = "lowest-cost"
	RoutingStrategyLowestLatency RoutingStrategy = "lowest-latency"
	RoutingStrategyManual        RoutingStrategy = "manual"
)

func (s RoutingStrategy) Normalize() RoutingStrategy {
	switch s {
	case RoutingStrategyReliability, RoutingStrategyLowestCost, RoutingStrategyLowestLatency, RoutingStrategyManual:
		return s
	default:
		return RoutingStrategyBalanced
	}
}

type RouteCandidateStatus string

const (
	RouteCandidateActive      RouteCandidateStatus = "active"
	RouteCandidateDegraded    RouteCandidateStatus = "degraded"
	RouteCandidateStale       RouteCandidateStatus = "stale"
	RouteCandidateUnavailable RouteCandidateStatus = "unavailable"
	RouteCandidateDisabled    RouteCandidateStatus = "disabled"
	RouteCandidateArchived    RouteCandidateStatus = "archived"
)

type CanonicalModel struct {
	ID              int              `json:"id" gorm:"primaryKey"`
	Name            string           `json:"name" gorm:"size:191;not null"`
	NormalizedName  string           `json:"normalized_name" gorm:"size:191;uniqueIndex;not null"`
	Vendor          string           `json:"vendor" gorm:"size:64;index"`                 // 厂商 ID，自动识别或人工指定，空表示未知
	VendorManual    bool             `json:"vendor_manual" gorm:"not null;default:false"` // 人工指定后不再被自动识别覆盖
	RoutingStrategy RoutingStrategy  `json:"routing_strategy" gorm:"type:varchar(32);not null;default:'balanced'"`
	ProtocolPolicy  ProtocolPolicy   `json:"protocol_policy" gorm:"type:varchar(32);not null;default:'auto'"`
	AllowLossy      bool             `json:"allow_lossy" gorm:"not null;default:false"`
	Enabled         bool             `json:"enabled" gorm:"not null;default:true;index"`
	Manual          bool             `json:"manual" gorm:"not null;default:false"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	Aliases         []ModelAlias     `json:"aliases,omitempty" gorm:"foreignKey:CanonicalModelID"`
	RouteCandidates []RouteCandidate `json:"route_candidates,omitempty" gorm:"foreignKey:CanonicalModelID"`
}

type ModelAlias struct {
	ID               int       `json:"id" gorm:"primaryKey"`
	CanonicalModelID int       `json:"canonical_model_id" gorm:"not null;index"`
	Alias            string    `json:"alias" gorm:"size:191;not null"`
	NormalizedAlias  string    `json:"normalized_alias" gorm:"size:191;uniqueIndex;not null"`
	Manual           bool      `json:"manual" gorm:"not null;default:true"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type RouteCandidate struct {
	ID                int                  `json:"id" gorm:"primaryKey"`
	CanonicalModelID  int                  `json:"canonical_model_id" gorm:"not null;index;uniqueIndex:idx_route_candidate_identity"`
	ChannelID         int                  `json:"channel_id" gorm:"not null;index;uniqueIndex:idx_route_candidate_identity"`
	UpstreamModelName string               `json:"upstream_model_name" gorm:"size:191;not null;uniqueIndex:idx_route_candidate_identity"`
	SiteID            *int                 `json:"site_id,omitempty" gorm:"index"`
	SiteAccountID     *int                 `json:"site_account_id,omitempty" gorm:"index"`
	SiteGroupKey      string               `json:"site_group_key,omitempty" gorm:"size:128;index"`
	Status            RouteCandidateStatus `json:"status" gorm:"type:varchar(24);not null;default:'active';index"`
	Priority          int                  `json:"priority" gorm:"not null;default:0"`
	Weight            int                  `json:"weight" gorm:"not null;default:1"`
	ProtocolPolicy    ProtocolPolicy       `json:"protocol_policy" gorm:"type:varchar(32);not null;default:'inherit'"`
	AllowLossy        *bool                `json:"allow_lossy,omitempty"`
	Manual            bool                 `json:"manual" gorm:"not null;default:false"`
	LastSeenAt        time.Time            `json:"last_seen_at" gorm:"index"`
	UnavailableSince  *time.Time           `json:"unavailable_since,omitempty"`
	ArchivedAt        *time.Time           `json:"archived_at,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

type CatalogSyncResult struct {
	CanonicalCreated  int `json:"canonical_created"`
	AliasesCreated    int `json:"aliases_created"`
	CandidatesCreated int `json:"candidates_created"`
	CandidatesUpdated int `json:"candidates_updated"`
	MarkedUnavailable int `json:"marked_unavailable"`
	Archived          int `json:"archived"`
	GroupsCreated     int `json:"groups_created"`
	GroupItemsCreated int `json:"group_items_created"`
	Skipped           int `json:"skipped"` // 手动供给模式下跳过的未选中模型数
}

type RouteDecisionReason struct {
	RouteCandidateID int                       `json:"route_candidate_id"`
	ChannelID        int                       `json:"channel_id"`
	UpstreamModel    string                    `json:"upstream_model"`
	Status           RouteCandidateStatus      `json:"status"`
	OutboundProtocol ProtocolName              `json:"outbound_protocol"`
	ProtocolMode     ProtocolExecutionMode     `json:"protocol_mode,omitempty"`
	ProtocolPolicy   ProtocolPolicy            `json:"protocol_policy,omitempty"`
	AllowLossy       bool                      `json:"allow_lossy"`
	Compatibility    ProtocolCompatibility     `json:"compatibility,omitempty"`
	Capabilities     []ProtocolFeatureDecision `json:"capabilities,omitempty"`
	Warnings         []string                  `json:"warnings,omitempty"`
	Included         bool                      `json:"included"`
	Reason           string                    `json:"reason"`
	Score            float64                   `json:"score"`
}

type RoutePreview struct {
	RequestedModel  string                `json:"requested_model"`
	CanonicalModel  string                `json:"canonical_model"`
	InboundProtocol ProtocolName          `json:"inbound_protocol"`
	Features        []ProtocolFeature     `json:"features,omitempty"`
	Strategy        RoutingStrategy       `json:"strategy"`
	Decisions       []RouteDecisionReason `json:"decisions"`
}
