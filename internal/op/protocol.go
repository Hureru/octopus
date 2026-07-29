package op

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	transformermodel "github.com/bestruirui/octopus/internal/transformer/model"
)

type protocolRouteAssessment struct {
	Included      bool
	Mode          dbmodel.ProtocolExecutionMode
	Compatibility dbmodel.ProtocolCompatibility
	Capabilities  []dbmodel.ProtocolFeatureDecision
	Warnings      []string
	Reason        string
}

func ProtocolRequirementsFromRequest(request *transformermodel.InternalLLMRequest) dbmodel.ProtocolRouteRequirements {
	requirements := dbmodel.ProtocolRouteRequirements{InboundProtocol: dbmodel.ProtocolUnknown}
	if request == nil {
		return requirements
	}
	requirements.InboundProtocol = protocolForAPIFormat(request.RawAPIFormat)

	features := make(map[dbmodel.ProtocolFeature]struct{})
	add := func(feature dbmodel.ProtocolFeature) {
		if feature != "" {
			features[feature] = struct{}{}
		}
	}

	if len(request.Tools) > 0 || request.ToolChoice != nil {
		add(dbmodel.ProtocolFeatureFunctionTools)
	}
	for _, tool := range request.Tools {
		if tool.Type != "" && tool.Type != "function" {
			add(dbmodel.ProtocolFeatureBuiltInTools)
		}
		if tool.Type == "image_generation" || tool.ImageGeneration != nil {
			add(dbmodel.ProtocolFeatureImages)
		}
		if len(tool.AnthropicServerSpec) > 0 {
			add(dbmodel.ProtocolFeatureAnthropicServerTools)
		}
		if tool.CacheControl != nil {
			add(dbmodel.ProtocolFeatureCacheControl)
		}
	}

	if request.ResponseFormat != nil && request.ResponseFormat.Type != "" && request.ResponseFormat.Type != "text" {
		add(dbmodel.ProtocolFeatureStructuredOutput)
	}
	if request.ReasoningEffort != "" || request.ReasoningBudget != nil || request.AdaptiveThinking ||
		request.ThinkingDisplay != "" || request.EnableThinking != nil || request.Thinking != nil ||
		request.ReasoningSummary != nil || request.ReasoningGenerateSummary != nil {
		add(dbmodel.ProtocolFeatureReasoning)
	}
	for _, modality := range request.Modalities {
		switch strings.ToLower(strings.TrimSpace(modality)) {
		case "audio":
			add(dbmodel.ProtocolFeatureAudio)
		case "image":
			add(dbmodel.ProtocolFeatureImages)
		}
	}
	if request.Audio != nil {
		add(dbmodel.ProtocolFeatureAudio)
	}
	if request.PromptCacheKey != nil || request.ResponsesPromptCacheKey != nil || request.PromptCacheRetention != nil {
		add(dbmodel.ProtocolFeatureCacheControl)
	}
	if len(request.Prediction) > 0 {
		add(dbmodel.ProtocolFeatureProviderExtensions)
	}
	if len(request.Metadata) > 0 {
		add(dbmodel.ProtocolFeatureProviderExtensions)
	}
	if len(request.WebSearchOptions) > 0 {
		add(dbmodel.ProtocolFeatureBuiltInTools)
	}
	if request.PreviousResponseID != nil && strings.TrimSpace(*request.PreviousResponseID) != "" {
		add(dbmodel.ProtocolFeatureContinuation)
	}
	if request.Background != nil || hasJSONValue(request.Prompt) || hasJSONValue(request.Conversation) ||
		hasJSONValue(request.ContextManagement) || hasJSONValue(request.ResponsesStreamOptions) ||
		len(request.Include) > 0 {
		add(dbmodel.ProtocolFeatureResponsesState)
	}
	if request.HasOpenAIResponsesPassthrough() {
		add(dbmodel.ProtocolFeatureProviderExtensions)
		if strings.Contains(request.OpenAIResponsesPassthroughReasonTextValue(), "tool:") {
			add(dbmodel.ProtocolFeatureBuiltInTools)
		}
	}

	for _, message := range request.Messages {
		if message.CacheControl != nil {
			add(dbmodel.ProtocolFeatureCacheControl)
		}
		if message.ReasoningContent != nil || message.Reasoning != nil || message.ReasoningSignature != nil ||
			len(message.ReasoningBlocks) > 0 || len(message.RedactedThinkingBlocks) > 0 {
			add(dbmodel.ProtocolFeatureReasoning)
		}
		for _, toolCall := range message.ToolCalls {
			add(dbmodel.ProtocolFeatureFunctionTools)
			if toolCall.CacheControl != nil {
				add(dbmodel.ProtocolFeatureCacheControl)
			}
			if toolCall.ThoughtSignature != "" {
				add(dbmodel.ProtocolFeatureGeminiExtensions)
			}
		}
		for _, part := range message.Content.MultipleContent {
			if part.CacheControl != nil {
				add(dbmodel.ProtocolFeatureCacheControl)
			}
			switch part.Type {
			case "image_url":
				add(dbmodel.ProtocolFeatureImages)
			case "input_audio":
				add(dbmodel.ProtocolFeatureAudio)
			case "file":
				add(dbmodel.ProtocolFeatureFiles)
			case "document":
				add(dbmodel.ProtocolFeatureDocuments)
			case "server_tool_use", "server_tool_result":
				add(dbmodel.ProtocolFeatureAnthropicServerTools)
			}
			if part.ProviderExtensions != nil {
				add(dbmodel.ProtocolFeatureProviderExtensions)
			}
		}
	}

	if extensions := request.ProviderExtensions; extensions != nil {
		if extensions.Common != nil || extensions.OpenAI != nil || extensions.Volcengine != nil {
			add(dbmodel.ProtocolFeatureProviderExtensions)
		}
		if anthropic := extensions.Anthropic; anthropic != nil {
			if len(anthropic.Beta) > 0 {
				add(dbmodel.ProtocolFeatureProviderExtensions)
			}
			if anthropic.CacheControl != nil {
				add(dbmodel.ProtocolFeatureCacheControl)
			}
			if hasJSONValue(anthropic.MCPServers) {
				add(dbmodel.ProtocolFeatureAnthropicMCP)
			}
			if hasJSONValue(anthropic.Container) {
				add(dbmodel.ProtocolFeatureAnthropicContainer)
			}
			if hasJSONValue(anthropic.ServerTool) {
				add(dbmodel.ProtocolFeatureAnthropicServerTools)
			}
		}
		if gemini := extensions.Gemini; gemini != nil {
			if gemini.ThoughtSignature != "" || gemini.CachedContentRef != nil || hasJSONValue(gemini.SpeechConfig) {
				add(dbmodel.ProtocolFeatureGeminiExtensions)
			}
			if gemini.CachedContentRef != nil {
				add(dbmodel.ProtocolFeatureCacheControl)
			}
			if hasJSONValue(gemini.SpeechConfig) {
				add(dbmodel.ProtocolFeatureAudio)
			}
		}
	}

	requirements.Features = make([]dbmodel.ProtocolFeature, 0, len(features))
	for feature := range features {
		requirements.Features = append(requirements.Features, feature)
	}
	sort.Slice(requirements.Features, func(i, j int) bool {
		return requirements.Features[i] < requirements.Features[j]
	})
	return requirements
}

func AssessProtocolRoute(
	requirements dbmodel.ProtocolRouteRequirements,
	outboundProtocol dbmodel.ProtocolName,
	policy dbmodel.ProtocolPolicy,
	allowLossy bool,
) protocolRouteAssessment {
	return assessProtocolRouteWithMode(
		requirements,
		outboundProtocol,
		policy,
		allowLossy,
		protocolExecutionMode(requirements, outboundProtocol),
	)
}

func assessProtocolRouteWithMode(
	requirements dbmodel.ProtocolRouteRequirements,
	outboundProtocol dbmodel.ProtocolName,
	policy dbmodel.ProtocolPolicy,
	allowLossy bool,
	mode dbmodel.ProtocolExecutionMode,
) protocolRouteAssessment {
	policy = policy.Normalize(dbmodel.ProtocolPolicyAuto)
	assessment := protocolRouteAssessment{
		Mode:          mode,
		Compatibility: dbmodel.ProtocolCompatibilityExact,
		Capabilities:  make([]dbmodel.ProtocolFeatureDecision, 0, len(requirements.Features)),
	}

	if !ProtocolTransformSupported(requirements.InboundProtocol, outboundProtocol) {
		assessment.Compatibility = dbmodel.ProtocolCompatibilityUnsupported
		assessment.Reason = "protocol conversion unsupported"
		return assessment
	}
	if policy == dbmodel.ProtocolPolicyPassthroughOnly &&
		assessment.Mode != dbmodel.ProtocolExecutionModePassthrough {
		assessment.Compatibility = dbmodel.ProtocolCompatibilityUnsupported
		assessment.Reason = "passthrough-only policy"
		return assessment
	}

	for _, feature := range normalizeProtocolFeatures(requirements.Features) {
		capability, reason := protocolFeatureCapability(requirements.InboundProtocol, outboundProtocol, feature)
		assessment.Capabilities = append(assessment.Capabilities, dbmodel.ProtocolFeatureDecision{
			Feature:    feature,
			Capability: capability,
			Reason:     reason,
		})
		switch capability {
		case dbmodel.FeatureCapabilityUnsupported:
			assessment.Compatibility = dbmodel.ProtocolCompatibilityUnsupported
			assessment.Reason = fmt.Sprintf("%s unsupported: %s", feature, reason)
		case dbmodel.FeatureCapabilityDegraded:
			if assessment.Compatibility != dbmodel.ProtocolCompatibilityUnsupported {
				assessment.Compatibility = dbmodel.ProtocolCompatibilityLossy
			}
			assessment.Warnings = append(assessment.Warnings, fmt.Sprintf("%s: %s", feature, reason))
		}
	}

	if assessment.Compatibility == dbmodel.ProtocolCompatibilityUnsupported {
		return assessment
	}
	if assessment.Compatibility == dbmodel.ProtocolCompatibilityLossy && !allowLossy {
		assessment.Reason = "lossy features require allow-lossy"
		return assessment
	}

	assessment.Included = true
	if assessment.Compatibility == dbmodel.ProtocolCompatibilityLossy {
		assessment.Reason = "compatible lossy transform"
	} else if assessment.Mode == dbmodel.ProtocolExecutionModePassthrough {
		assessment.Reason = "native passthrough preferred"
	} else {
		assessment.Reason = "compatible transform"
	}
	return assessment
}

func ProtocolCapabilityMatrix() []dbmodel.ProtocolCapabilityDescriptor {
	features := []dbmodel.ProtocolFeature{
		dbmodel.ProtocolFeatureFunctionTools,
		dbmodel.ProtocolFeatureBuiltInTools,
		dbmodel.ProtocolFeatureStructuredOutput,
		dbmodel.ProtocolFeatureImages,
		dbmodel.ProtocolFeatureAudio,
		dbmodel.ProtocolFeatureFiles,
		dbmodel.ProtocolFeatureDocuments,
		dbmodel.ProtocolFeatureReasoning,
		dbmodel.ProtocolFeatureCacheControl,
		dbmodel.ProtocolFeatureContinuation,
		dbmodel.ProtocolFeatureResponsesState,
		dbmodel.ProtocolFeatureProviderExtensions,
		dbmodel.ProtocolFeatureAnthropicMCP,
		dbmodel.ProtocolFeatureAnthropicContainer,
		dbmodel.ProtocolFeatureAnthropicServerTools,
		dbmodel.ProtocolFeatureGeminiExtensions,
		dbmodel.ProtocolFeatureWebSocket,
	}
	inboundProtocols := []dbmodel.ProtocolName{
		dbmodel.ProtocolOpenAIChat,
		dbmodel.ProtocolOpenAIResponses,
		dbmodel.ProtocolAnthropic,
		dbmodel.ProtocolOpenAIEmbedding,
	}
	outboundProtocols := []dbmodel.ProtocolName{
		dbmodel.ProtocolOpenAIChat,
		dbmodel.ProtocolOpenAIResponses,
		dbmodel.ProtocolAnthropic,
		dbmodel.ProtocolGemini,
		dbmodel.ProtocolVolcengine,
		dbmodel.ProtocolOpenAIEmbedding,
	}

	matrix := make([]dbmodel.ProtocolCapabilityDescriptor, 0, len(inboundProtocols)*len(outboundProtocols))
	for _, inboundProtocol := range inboundProtocols {
		for _, outboundProtocol := range outboundProtocols {
			if !ProtocolTransformSupported(inboundProtocol, outboundProtocol) {
				continue
			}
			entry := dbmodel.ProtocolCapabilityDescriptor{
				InboundProtocol:  inboundProtocol,
				OutboundProtocol: outboundProtocol,
				Mode: protocolExecutionMode(dbmodel.ProtocolRouteRequirements{
					InboundProtocol: inboundProtocol,
				}, outboundProtocol),
				Limited: outboundProtocol == dbmodel.ProtocolGemini ||
					outboundProtocol == dbmodel.ProtocolVolcengine,
				Features: make([]dbmodel.ProtocolFeatureDecision, 0, len(features)),
			}
			for _, feature := range features {
				capability, reason := protocolFeatureCapability(inboundProtocol, outboundProtocol, feature)
				entry.Features = append(entry.Features, dbmodel.ProtocolFeatureDecision{
					Feature:    feature,
					Capability: capability,
					Reason:     reason,
				})
			}
			matrix = append(matrix, entry)
		}
	}
	return matrix
}

func protocolFeatureCapability(
	inboundProtocol, outboundProtocol dbmodel.ProtocolName,
	feature dbmodel.ProtocolFeature,
) (dbmodel.FeatureCapability, string) {
	if inboundProtocol == outboundProtocol {
		switch feature {
		case dbmodel.ProtocolFeatureContinuation, dbmodel.ProtocolFeatureWebSocket:
			if inboundProtocol == dbmodel.ProtocolOpenAIResponses {
				if protocolExecutionMode(dbmodel.ProtocolRouteRequirements{
					InboundProtocol: inboundProtocol,
					Features:        []dbmodel.ProtocolFeature{feature},
				}, outboundProtocol) == dbmodel.ProtocolExecutionModeTransform {
					return dbmodel.FeatureCapabilityTransformed, "stateful Responses execution requires a verified raw WebSocket path"
				}
				return dbmodel.FeatureCapabilityNative, "stateful Responses execution preserves the same wire protocol"
			}
			return dbmodel.FeatureCapabilityUnsupported, "stateful execution is only verified for OpenAI Responses"
		}
		if protocolExecutionMode(dbmodel.ProtocolRouteRequirements{
			InboundProtocol: inboundProtocol,
			Features:        []dbmodel.ProtocolFeature{feature},
		}, outboundProtocol) == dbmodel.ProtocolExecutionModeTransform {
			return dbmodel.FeatureCapabilityTransformed, "same-protocol request uses the registered transformer"
		}
		return dbmodel.FeatureCapabilityNative, "preserved by same-protocol passthrough"
	}

	switch feature {
	case dbmodel.ProtocolFeatureFunctionTools:
		return dbmodel.FeatureCapabilityTransformed, "function definitions and calls map through the shared request model"
	case dbmodel.ProtocolFeatureBuiltInTools:
		return dbmodel.FeatureCapabilityDegraded, "provider-native tools have no guaranteed cross-provider equivalent"
	case dbmodel.ProtocolFeatureStructuredOutput:
		return dbmodel.FeatureCapabilityDegraded, "cross-protocol schema conversion cannot preserve every structured-output constraint"
	case dbmodel.ProtocolFeatureImages:
		if outboundProtocol == dbmodel.ProtocolGemini {
			return dbmodel.FeatureCapabilityDegraded, "Gemini conversion only preserves supported inline image representations"
		}
		return dbmodel.FeatureCapabilityTransformed, "image content is mapped to the target protocol"
	case dbmodel.ProtocolFeatureAudio:
		if outboundProtocol == dbmodel.ProtocolAnthropic {
			return dbmodel.FeatureCapabilityDegraded, "Anthropic Messages has no equivalent input-audio block"
		}
		return dbmodel.FeatureCapabilityTransformed, "audio content is mapped to the target protocol"
	case dbmodel.ProtocolFeatureFiles:
		switch outboundProtocol {
		case dbmodel.ProtocolAnthropic:
			return dbmodel.FeatureCapabilityDegraded, "file handles cannot be converted to Anthropic document sources without fetching content"
		case dbmodel.ProtocolGemini:
			return dbmodel.FeatureCapabilityDegraded, "Gemini conversion only preserves supported inline file payloads"
		default:
			return dbmodel.FeatureCapabilityTransformed, "file content is mapped to the target protocol"
		}
	case dbmodel.ProtocolFeatureDocuments:
		switch outboundProtocol {
		case dbmodel.ProtocolAnthropic:
			return dbmodel.FeatureCapabilityTransformed, "document blocks are mapped to Anthropic document sources"
		case dbmodel.ProtocolGemini:
			return dbmodel.FeatureCapabilityDegraded, "document URLs and oversized inline documents may fall back to hints or be omitted"
		default:
			return dbmodel.FeatureCapabilityDegraded, "document metadata is flattened to text on this protocol"
		}
	case dbmodel.ProtocolFeatureReasoning:
		if outboundProtocol == dbmodel.ProtocolVolcengine {
			return dbmodel.FeatureCapabilityDegraded, "Volcengine reasoning controls are model-dependent and unsupported fields are omitted"
		}
		return dbmodel.FeatureCapabilityTransformed, "reasoning controls and blocks use the shared reasoning model"
	case dbmodel.ProtocolFeatureCacheControl:
		if outboundProtocol == dbmodel.ProtocolAnthropic {
			return dbmodel.FeatureCapabilityTransformed, "cache breakpoints are mapped to Anthropic cache_control"
		}
		return dbmodel.FeatureCapabilityDegraded, "provider cache semantics cannot be preserved exactly"
	case dbmodel.ProtocolFeatureContinuation:
		return dbmodel.FeatureCapabilityUnsupported, "continuation requires a verified same-protocol Responses route"
	case dbmodel.ProtocolFeatureResponsesState:
		return dbmodel.FeatureCapabilityDegraded, "Responses-only state fields are not represented by the target protocol"
	case dbmodel.ProtocolFeatureProviderExtensions:
		return dbmodel.FeatureCapabilityDegraded, "provider extension fields are only guaranteed on same-protocol passthrough"
	case dbmodel.ProtocolFeatureAnthropicMCP:
		if outboundProtocol == dbmodel.ProtocolAnthropic {
			return dbmodel.FeatureCapabilityTransformed, "Anthropic MCP connector payload is preserved"
		}
		return dbmodel.FeatureCapabilityDegraded, "target protocol has no Anthropic MCP connector equivalent"
	case dbmodel.ProtocolFeatureAnthropicContainer:
		if outboundProtocol == dbmodel.ProtocolAnthropic {
			return dbmodel.FeatureCapabilityTransformed, "Anthropic container payload is preserved"
		}
		return dbmodel.FeatureCapabilityDegraded, "target protocol has no Anthropic container equivalent"
	case dbmodel.ProtocolFeatureAnthropicServerTools:
		if outboundProtocol == dbmodel.ProtocolAnthropic {
			return dbmodel.FeatureCapabilityTransformed, "Anthropic server-tool payload is preserved"
		}
		return dbmodel.FeatureCapabilityDegraded, "server-tool definitions or result blocks are omitted"
	case dbmodel.ProtocolFeatureGeminiExtensions:
		if outboundProtocol == dbmodel.ProtocolGemini {
			return dbmodel.FeatureCapabilityTransformed, "Gemini extension fields are preserved"
		}
		return dbmodel.FeatureCapabilityDegraded, "Gemini-specific extension fields are omitted"
	case dbmodel.ProtocolFeatureWebSocket:
		return dbmodel.FeatureCapabilityUnsupported, "WebSocket execution is only verified for same-protocol Responses routes"
	default:
		return dbmodel.FeatureCapabilityUnsupported, "feature is not registered in the capability matrix"
	}
}

func protocolExecutionMode(
	requirements dbmodel.ProtocolRouteRequirements,
	outboundProtocol dbmodel.ProtocolName,
) dbmodel.ProtocolExecutionMode {
	if requirements.InboundProtocol != outboundProtocol {
		return dbmodel.ProtocolExecutionModeTransform
	}
	for _, feature := range requirements.Features {
		if feature == dbmodel.ProtocolFeatureContinuation || feature == dbmodel.ProtocolFeatureWebSocket {
			return dbmodel.ProtocolExecutionModeTransform
		}
	}
	switch outboundProtocol {
	case dbmodel.ProtocolOpenAIChat, dbmodel.ProtocolOpenAIResponses,
		dbmodel.ProtocolAnthropic, dbmodel.ProtocolOpenAIEmbedding:
		return dbmodel.ProtocolExecutionModePassthrough
	default:
		return dbmodel.ProtocolExecutionModeTransform
	}
}

func normalizeProtocolFeatures(features []dbmodel.ProtocolFeature) []dbmodel.ProtocolFeature {
	if len(features) == 0 {
		return nil
	}
	seen := make(map[dbmodel.ProtocolFeature]struct{}, len(features))
	result := make([]dbmodel.ProtocolFeature, 0, len(features))
	for _, feature := range features {
		if feature == "" {
			continue
		}
		if _, ok := seen[feature]; ok {
			continue
		}
		seen[feature] = struct{}{}
		result = append(result, feature)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})
	return result
}

func protocolForAPIFormat(format transformermodel.APIFormat) dbmodel.ProtocolName {
	switch format {
	case transformermodel.APIFormatOpenAIResponse:
		return dbmodel.ProtocolOpenAIResponses
	case transformermodel.APIFormatAnthropicMessage:
		return dbmodel.ProtocolAnthropic
	case transformermodel.APIFormatOpenAIEmbedding:
		return dbmodel.ProtocolOpenAIEmbedding
	case transformermodel.APIFormatOpenAIChatCompletion:
		return dbmodel.ProtocolOpenAIChat
	default:
		return dbmodel.ProtocolUnknown
	}
}

func hasJSONValue(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) ||
		bytes.Equal(trimmed, []byte("{}")) || bytes.Equal(trimmed, []byte("[]")) {
		return false
	}
	return true
}
