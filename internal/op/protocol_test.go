package op

import (
	"context"
	"encoding/json"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	transformermodel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestProtocolRequirementsFromRequestExtractsSemanticFeatures(t *testing.T) {
	previousID := "resp_previous"
	request := &transformermodel.InternalLLMRequest{
		RawAPIFormat:       transformermodel.APIFormatOpenAIResponse,
		PreviousResponseID: &previousID,
		ResponseFormat:     &transformermodel.ResponseFormat{Type: "json_schema"},
		ReasoningEffort:    "high",
		Modalities:         []string{"text", "audio"},
		Tools: []transformermodel.Tool{
			{Type: "function"},
			{Type: "image_generation", ImageGeneration: &transformermodel.ImageGeneration{}},
		},
		Messages: []transformermodel.Message{{
			Role: "user",
			Content: transformermodel.MessageContent{MultipleContent: []transformermodel.MessageContentPart{
				{Type: "image_url", ImageURL: &transformermodel.ImageURL{URL: "data:image/png;base64,AA=="}},
				{Type: "file", File: &transformermodel.File{FileID: "file_1"}},
				{Type: "document", Document: &transformermodel.DocumentSource{Type: "text", Text: "doc"}},
			}},
			CacheControl: &transformermodel.CacheControl{Type: transformermodel.CacheControlTypeEphemeral},
		}},
	}
	request.SetAnthropicExtensions(transformermodel.AnthropicExtension{
		MCPServers: json.RawMessage(`[{"url":"https://mcp.example"}]`),
		Container:  json.RawMessage(`{"type":"auto"}`),
	})

	requirements := ProtocolRequirementsFromRequest(request)
	assertProtocolFeatures(t, requirements.Features,
		dbmodel.ProtocolFeatureAnthropicContainer,
		dbmodel.ProtocolFeatureAnthropicMCP,
		dbmodel.ProtocolFeatureAudio,
		dbmodel.ProtocolFeatureBuiltInTools,
		dbmodel.ProtocolFeatureCacheControl,
		dbmodel.ProtocolFeatureContinuation,
		dbmodel.ProtocolFeatureDocuments,
		dbmodel.ProtocolFeatureFiles,
		dbmodel.ProtocolFeatureFunctionTools,
		dbmodel.ProtocolFeatureImages,
		dbmodel.ProtocolFeatureReasoning,
		dbmodel.ProtocolFeatureStructuredOutput,
	)
}

func TestAssessProtocolRouteStrictAndAllowLossy(t *testing.T) {
	requirements := dbmodel.ProtocolRouteRequirements{
		InboundProtocol: dbmodel.ProtocolAnthropic,
		Features:        []dbmodel.ProtocolFeature{dbmodel.ProtocolFeatureAnthropicServerTools},
	}

	strict := AssessProtocolRoute(
		requirements,
		dbmodel.ProtocolOpenAIChat,
		dbmodel.ProtocolPolicyTransformAllowed,
		false,
	)
	if strict.Included || strict.Compatibility != dbmodel.ProtocolCompatibilityLossy {
		t.Fatalf("strict route should exclude lossy server-tool conversion: %+v", strict)
	}

	lossy := AssessProtocolRoute(
		requirements,
		dbmodel.ProtocolOpenAIChat,
		dbmodel.ProtocolPolicyTransformAllowed,
		true,
	)
	if !lossy.Included || lossy.Compatibility != dbmodel.ProtocolCompatibilityLossy || len(lossy.Warnings) == 0 {
		t.Fatalf("allow-lossy route should include explicit warnings: %+v", lossy)
	}
}

func TestAssessProtocolRouteContinuationRequiresResponses(t *testing.T) {
	requirements := dbmodel.ProtocolRouteRequirements{
		InboundProtocol: dbmodel.ProtocolOpenAIResponses,
		Features:        []dbmodel.ProtocolFeature{dbmodel.ProtocolFeatureContinuation},
	}

	sameProtocol := AssessProtocolRoute(
		requirements,
		dbmodel.ProtocolOpenAIResponses,
		dbmodel.ProtocolPolicyAuto,
		false,
	)
	if !sameProtocol.Included || sameProtocol.Mode != dbmodel.ProtocolExecutionModeTransform {
		t.Fatalf("continuation without a channel-specific raw WS path should use transform execution: %+v", sameProtocol)
	}

	crossProtocol := AssessProtocolRoute(
		requirements,
		dbmodel.ProtocolAnthropic,
		dbmodel.ProtocolPolicyTransformAllowed,
		true,
	)
	if crossProtocol.Included || crossProtocol.Compatibility != dbmodel.ProtocolCompatibilityUnsupported {
		t.Fatalf("allow-lossy must not permit cross-protocol continuation: %+v", crossProtocol)
	}
}

func TestAssessProtocolRoutePassthroughOnlyRequiresRawPassthrough(t *testing.T) {
	tests := []struct {
		name         string
		requirements dbmodel.ProtocolRouteRequirements
		outbound     dbmodel.ProtocolName
		wantIncluded bool
		wantMode     dbmodel.ProtocolExecutionMode
	}{
		{
			name: "chat raw passthrough",
			requirements: dbmodel.ProtocolRouteRequirements{
				InboundProtocol: dbmodel.ProtocolOpenAIChat,
			},
			outbound:     dbmodel.ProtocolOpenAIChat,
			wantIncluded: true,
			wantMode:     dbmodel.ProtocolExecutionModePassthrough,
		},
		{
			name: "responses continuation requires channel capability",
			requirements: dbmodel.ProtocolRouteRequirements{
				InboundProtocol: dbmodel.ProtocolOpenAIResponses,
				Features:        []dbmodel.ProtocolFeature{dbmodel.ProtocolFeatureContinuation},
			},
			outbound:     dbmodel.ProtocolOpenAIResponses,
			wantIncluded: false,
			wantMode:     dbmodel.ProtocolExecutionModeTransform,
		},
		{
			name: "embedding raw passthrough",
			requirements: dbmodel.ProtocolRouteRequirements{
				InboundProtocol: dbmodel.ProtocolOpenAIEmbedding,
			},
			outbound:     dbmodel.ProtocolOpenAIEmbedding,
			wantIncluded: true,
			wantMode:     dbmodel.ProtocolExecutionModePassthrough,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := AssessProtocolRoute(
				test.requirements,
				test.outbound,
				dbmodel.ProtocolPolicyPassthroughOnly,
				false,
			)
			if assessment.Included != test.wantIncluded || assessment.Mode != test.wantMode {
				t.Fatalf("unexpected passthrough-only assessment: %+v", assessment)
			}
			if !test.wantIncluded && assessment.Reason != "passthrough-only policy" {
				t.Fatalf("transform path was rejected for the wrong reason: %+v", assessment)
			}
		})
	}
}

func TestProtocolExecutionModeForResponsesChannel(t *testing.T) {
	ctx := setupBackupTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	if err := SettingSetString(dbmodel.SettingKeyResponsesWSDefaultMode, string(dbmodel.ChannelWSModePassthrough)); err != nil {
		t.Fatalf("set default WS mode: %v", err)
	}
	if err := SettingSetString(dbmodel.SettingKeyResponsesWSEnabled, "true"); err != nil {
		t.Fatalf("enable Responses WS: %v", err)
	}
	requirements := dbmodel.ProtocolRouteRequirements{
		InboundProtocol: dbmodel.ProtocolOpenAIResponses,
		Features:        []dbmodel.ProtocolFeature{dbmodel.ProtocolFeatureContinuation},
	}
	channel := dbmodel.Channel{
		Type:   outbound.OutboundTypeOpenAIResponse,
		WSMode: dbmodel.ChannelWSModePassthrough,
	}
	if got := protocolExecutionModeForChannel(requirements, channel); got != dbmodel.ProtocolExecutionModePassthrough {
		t.Fatalf("passthrough channel mode = %q", got)
	}
	channel.WSMode = dbmodel.ChannelWSModeTransform
	if got := protocolExecutionModeForChannel(requirements, channel); got != dbmodel.ProtocolExecutionModeTransform {
		t.Fatalf("transform channel mode = %q", got)
	}

	requirements.Features = append(requirements.Features, dbmodel.ProtocolFeatureWebSocket)
	channel.WSMode = dbmodel.ChannelWSModePassthrough
	if err := SettingSetString(dbmodel.SettingKeyResponsesWSEnabled, "false"); err != nil {
		t.Fatalf("disable Responses WS: %v", err)
	}
	if got := protocolExecutionModeForChannel(requirements, channel); got != dbmodel.ProtocolExecutionModeTransform {
		t.Fatalf("disabled client WS mode = %q", got)
	}
}

func TestProtocolSettingsKeepTheStrictestLayer(t *testing.T) {
	channel := dbmodel.Channel{
		ProtocolPolicy: dbmodel.ProtocolPolicyPassthroughOnly,
		AllowLossy:     true,
	}
	canonical := &dbmodel.CanonicalModel{
		Manual:         true,
		ProtocolPolicy: dbmodel.ProtocolPolicyAuto,
		AllowLossy:     false,
	}

	policy, allowLossy := effectiveProtocolSettings(canonical, channel, dbmodel.RouteCandidate{}, false)
	if policy != dbmodel.ProtocolPolicyPassthroughOnly || allowLossy {
		t.Fatalf("canonical settings weakened the channel boundary: policy=%q allow_lossy=%v", policy, allowLossy)
	}

	allow := true
	candidate := dbmodel.RouteCandidate{
		ProtocolPolicy: dbmodel.ProtocolPolicyTransformAllowed,
		AllowLossy:     &allow,
	}
	policy, allowLossy = effectiveProtocolSettings(canonical, channel, candidate, true)
	if policy != dbmodel.ProtocolPolicyPassthroughOnly || allowLossy {
		t.Fatalf("candidate settings weakened a stricter parent: policy=%q allow_lossy=%v", policy, allowLossy)
	}
}

func TestAssessProtocolRouteMarksCrossProtocolStructuredOutputLossy(t *testing.T) {
	requirements := dbmodel.ProtocolRouteRequirements{
		InboundProtocol: dbmodel.ProtocolOpenAIResponses,
		Features:        []dbmodel.ProtocolFeature{dbmodel.ProtocolFeatureStructuredOutput},
	}
	strict := AssessProtocolRoute(
		requirements,
		dbmodel.ProtocolAnthropic,
		dbmodel.ProtocolPolicyTransformAllowed,
		false,
	)
	if strict.Included || strict.Compatibility != dbmodel.ProtocolCompatibilityLossy {
		t.Fatalf("strict structured-output conversion should be excluded: %+v", strict)
	}
	lossy := AssessProtocolRoute(
		requirements,
		dbmodel.ProtocolAnthropic,
		dbmodel.ProtocolPolicyTransformAllowed,
		true,
	)
	if !lossy.Included || len(lossy.Warnings) == 0 {
		t.Fatalf("explicit lossy conversion should include a warning: %+v", lossy)
	}
}

func TestVolcengineProtocolRequiresLossyOptInForMetadata(t *testing.T) {
	request := &transformermodel.InternalLLMRequest{
		RawAPIFormat: transformermodel.APIFormatOpenAIResponse,
		Metadata:     map[string]string{"trace": "value"},
	}
	requirements := ProtocolRequirementsFromRequest(request)
	assertProtocolFeatures(t, requirements.Features, dbmodel.ProtocolFeatureProviderExtensions)

	strict := AssessProtocolRoute(
		requirements,
		dbmodel.ProtocolVolcengine,
		dbmodel.ProtocolPolicyTransformAllowed,
		false,
	)
	if strict.Included || strict.Compatibility != dbmodel.ProtocolCompatibilityLossy {
		t.Fatalf("Volcengine metadata loss should require opt-in: %+v", strict)
	}
	if got := ProtocolForOutboundType(outbound.OutboundTypeVolcengine); got != dbmodel.ProtocolVolcengine {
		t.Fatalf("Volcengine outbound protocol = %q", got)
	}
}

func TestCatalogPlanGroupPrefersSameProtocolAndHonorsCanonicalPolicy(t *testing.T) {
	ctx := setupBackupTestDB(t)
	chatChannel := createProtocolTestChannel(t, ctx, "protocol-chat", outbound.OutboundTypeOpenAIChat)
	responsesChannel := createProtocolTestChannel(t, ctx, "protocol-responses", outbound.OutboundTypeOpenAIResponse)
	group := dbmodel.Group{
		Name: "protocol-model",
		Mode: dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ChannelID: chatChannel.ID, ModelName: "upstream-chat", Priority: 1, Weight: 1},
			{ChannelID: responsesChannel.ID, ModelName: "upstream-responses", Priority: 2, Weight: 1},
		},
	}
	canonical := dbmodel.CanonicalModel{
		Name:            group.Name,
		NormalizedName:  NormalizeModelIdentity(group.Name),
		RoutingStrategy: dbmodel.RoutingStrategyManual,
		ProtocolPolicy:  dbmodel.ProtocolPolicyAuto,
		Enabled:         true,
		Manual:          true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&canonical).Error; err != nil {
		t.Fatalf("create canonical model: %v", err)
	}
	candidates := []dbmodel.RouteCandidate{
		{
			CanonicalModelID:  canonical.ID,
			ChannelID:         chatChannel.ID,
			UpstreamModelName: "upstream-chat",
			Status:            dbmodel.RouteCandidateActive,
			Weight:            1,
		},
		{
			CanonicalModelID:  canonical.ID,
			ChannelID:         responsesChannel.ID,
			UpstreamModelName: "upstream-responses",
			Status:            dbmodel.RouteCandidateActive,
			Weight:            1,
		},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&candidates).Error; err != nil {
		t.Fatalf("create route candidates: %v", err)
	}
	if err := catalogRefreshCache(ctx); err != nil {
		t.Fatalf("refresh catalog cache: %v", err)
	}

	planned, preview, _, err := CatalogPlanGroup(ctx, group.Name, dbmodel.ProtocolRouteRequirements{
		InboundProtocol: dbmodel.ProtocolOpenAIResponses,
	}, group)
	if err != nil {
		t.Fatalf("CatalogPlanGroup failed: %v", err)
	}
	if len(planned.Items) != 2 || planned.Items[0].ChannelID != responsesChannel.ID {
		t.Fatalf("same-protocol route should be ordered first: %+v", planned.Items)
	}
	if decisionForProtocolTest(preview, responsesChannel.ID).ProtocolMode != dbmodel.ProtocolExecutionModePassthrough {
		t.Fatalf("responses route should preview passthrough: %+v", preview.Decisions)
	}

	canonical.ProtocolPolicy = dbmodel.ProtocolPolicyPassthroughOnly
	if _, err := CatalogCanonicalUpdate(ctx, canonical); err != nil {
		t.Fatalf("update canonical protocol policy: %v", err)
	}
	planned, _, _, err = CatalogPlanGroup(ctx, group.Name, dbmodel.ProtocolRouteRequirements{
		InboundProtocol: dbmodel.ProtocolOpenAIResponses,
	}, group)
	if err != nil {
		t.Fatalf("CatalogPlanGroup with passthrough-only failed: %v", err)
	}
	if len(planned.Items) != 1 || planned.Items[0].ChannelID != responsesChannel.ID {
		t.Fatalf("canonical passthrough-only policy should override channel defaults: %+v", planned.Items)
	}
}

func createProtocolTestChannel(
	t *testing.T,
	ctx context.Context,
	name string,
	channelType outbound.OutboundType,
) dbmodel.Channel {
	t.Helper()
	channel := dbmodel.Channel{
		Name:           name,
		Type:           channelType,
		Enabled:        true,
		Model:          name + "-model",
		ProtocolPolicy: dbmodel.ProtocolPolicyTransformAllowed,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel %s: %v", name, err)
	}
	return channel
}

func decisionForProtocolTest(preview dbmodel.RoutePreview, channelID int) dbmodel.RouteDecisionReason {
	for _, decision := range preview.Decisions {
		if decision.ChannelID == channelID {
			return decision
		}
	}
	return dbmodel.RouteDecisionReason{}
}

func assertProtocolFeatures(t *testing.T, got []dbmodel.ProtocolFeature, want ...dbmodel.ProtocolFeature) {
	t.Helper()
	gotSet := make(map[dbmodel.ProtocolFeature]struct{}, len(got))
	for _, feature := range got {
		gotSet[feature] = struct{}{}
	}
	for _, feature := range want {
		if _, ok := gotSet[feature]; !ok {
			t.Fatalf("missing protocol feature %q in %v", feature, got)
		}
	}
}
