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
		t.Fatalf("same-protocol continuation should use stateful transform execution: %+v", sameProtocol)
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
