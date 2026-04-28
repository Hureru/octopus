package model

import (
	"encoding/json"
	"strings"
)

type ProviderExtensionNamespace string

const (
	ProviderExtensionNamespaceCommon     ProviderExtensionNamespace = "common"
	ProviderExtensionNamespaceAnthropic  ProviderExtensionNamespace = "anthropic"
	ProviderExtensionNamespaceGemini     ProviderExtensionNamespace = "gemini"
	ProviderExtensionNamespaceOpenAI     ProviderExtensionNamespace = "openai"
	ProviderExtensionNamespaceVolcengine ProviderExtensionNamespace = "volcengine"
)

type ProviderExtensions struct {
	Common     *CommonExtension     `json:"common,omitempty"`
	Anthropic  *AnthropicExtension  `json:"anthropic,omitempty"`
	Gemini     *GeminiExtension     `json:"gemini,omitempty"`
	OpenAI     *OpenAIExtension     `json:"openai,omitempty"`
	Volcengine *VolcengineExtension `json:"volcengine,omitempty"`
}

type CommonExtension struct {
	Raw json.RawMessage `json:"raw,omitempty"`
}

type AnthropicExtension struct {
	Beta         []string        `json:"beta,omitempty"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
	MCPServers   json.RawMessage `json:"mcp_servers,omitempty"`
	Container    json.RawMessage `json:"container,omitempty"`
	ServerTool   json.RawMessage `json:"server_tool,omitempty"`
}

type GeminiExtension struct {
	ThoughtSignature string          `json:"thought_signature,omitempty"`
	CachedContentRef *string         `json:"cached_content_ref,omitempty"`
	SpeechConfig     json.RawMessage `json:"speech_config,omitempty"`
}

type OpenAIExtension struct {
	ResponsesPassthroughRequired bool            `json:"responses_passthrough_required,omitempty"`
	ResponsesPassthroughReason   string          `json:"responses_passthrough_reason,omitempty"`
	RawResponseItems             json.RawMessage `json:"raw_response_items,omitempty"`
}

type OpenAIResponsesOptions struct {
	PreviousResponseID       *string         `json:"previous_response_id,omitempty"`
	Background               *bool           `json:"background,omitempty"`
	Prompt                   json.RawMessage `json:"prompt,omitempty"`
	PromptCacheKey           *string         `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention     *string         `json:"prompt_cache_retention,omitempty"`
	SafetyIdentifier         *string         `json:"safety_identifier,omitempty"`
	MaxToolCalls             *int64          `json:"max_tool_calls,omitempty"`
	Conversation             json.RawMessage `json:"conversation,omitempty"`
	ContextManagement        json.RawMessage `json:"context_management,omitempty"`
	StreamOptions            json.RawMessage `json:"stream_options,omitempty"`
	ReasoningSummary         *string         `json:"reasoning_summary,omitempty"`
	ReasoningGenerateSummary *string         `json:"reasoning_generate_summary,omitempty"`
	RawInputItems            json.RawMessage `json:"raw_input_items,omitempty"`
}

type VolcengineExtension struct {
	Raw json.RawMessage `json:"raw,omitempty"`
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneCacheControl(value *CacheControl) *CacheControl {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func CloneProviderExtensions(ext *ProviderExtensions) *ProviderExtensions {
	if ext == nil {
		return nil
	}
	cloned := &ProviderExtensions{}
	if ext.Common != nil {
		cloned.Common = &CommonExtension{Raw: cloneRawMessage(ext.Common.Raw)}
	}
	if ext.Anthropic != nil {
		cloned.Anthropic = &AnthropicExtension{
			Beta:         append([]string(nil), ext.Anthropic.Beta...),
			CacheControl: cloneCacheControl(ext.Anthropic.CacheControl),
			MCPServers:   cloneRawMessage(ext.Anthropic.MCPServers),
			Container:    cloneRawMessage(ext.Anthropic.Container),
			ServerTool:   cloneRawMessage(ext.Anthropic.ServerTool),
		}
	}
	if ext.Gemini != nil {
		cloned.Gemini = &GeminiExtension{
			ThoughtSignature: ext.Gemini.ThoughtSignature,
			CachedContentRef: cloneStringPtr(ext.Gemini.CachedContentRef),
			SpeechConfig:     cloneRawMessage(ext.Gemini.SpeechConfig),
		}
	}
	if ext.OpenAI != nil {
		cloned.OpenAI = &OpenAIExtension{
			ResponsesPassthroughRequired: ext.OpenAI.ResponsesPassthroughRequired,
			ResponsesPassthroughReason:   ext.OpenAI.ResponsesPassthroughReason,
			RawResponseItems:             cloneRawMessage(ext.OpenAI.RawResponseItems),
		}
	}
	if ext.Volcengine != nil {
		cloned.Volcengine = &VolcengineExtension{Raw: cloneRawMessage(ext.Volcengine.Raw)}
	}
	return cloned
}

func (r *InternalLLMRequest) ensureProviderExtensions() *ProviderExtensions {
	if r == nil {
		return nil
	}
	if r.ProviderExtensions == nil {
		r.ProviderExtensions = &ProviderExtensions{}
	}
	return r.ProviderExtensions
}

func (r *InternalLLMRequest) SetGeminiExtensions(ext GeminiExtension) {
	if r == nil {
		return
	}
	providerExtensions := r.ensureProviderExtensions()
	if providerExtensions.Gemini == nil {
		providerExtensions.Gemini = &GeminiExtension{}
	}
	extCopy := GeminiExtension{
		ThoughtSignature: ext.ThoughtSignature,
		CachedContentRef: cloneStringPtr(ext.CachedContentRef),
		SpeechConfig:     cloneRawMessage(ext.SpeechConfig),
	}
	mergeGeminiExtension(providerExtensions.Gemini, &extCopy)
	r.GeminiCachedContentRef = providerExtensions.Gemini.CachedContentRef
	r.GeminiSpeechConfig = cloneRawMessage(providerExtensions.Gemini.SpeechConfig)
}

func (r *InternalLLMRequest) SetAnthropicExtensions(ext AnthropicExtension) {
	if r == nil {
		return
	}
	providerExtensions := r.ensureProviderExtensions()
	if providerExtensions.Anthropic == nil {
		providerExtensions.Anthropic = &AnthropicExtension{}
	}
	extCopy := AnthropicExtension{
		Beta:         append([]string(nil), ext.Beta...),
		CacheControl: cloneCacheControl(ext.CacheControl),
		MCPServers:   cloneRawMessage(ext.MCPServers),
		Container:    cloneRawMessage(ext.Container),
		ServerTool:   cloneRawMessage(ext.ServerTool),
	}
	mergeAnthropicExtension(providerExtensions.Anthropic, &extCopy)
	r.AnthropicMCPServers = cloneRawMessage(providerExtensions.Anthropic.MCPServers)
	r.AnthropicContainer = cloneRawMessage(providerExtensions.Anthropic.Container)
}

func (r *InternalLLMRequest) SetOpenAIExtensions(ext OpenAIExtension) {
	if r == nil {
		return
	}
	providerExtensions := r.ensureProviderExtensions()
	if providerExtensions.OpenAI == nil {
		providerExtensions.OpenAI = &OpenAIExtension{}
	}
	extCopy := OpenAIExtension{
		ResponsesPassthroughRequired: ext.ResponsesPassthroughRequired,
		ResponsesPassthroughReason:   ext.ResponsesPassthroughReason,
		RawResponseItems:             cloneRawMessage(ext.RawResponseItems),
	}
	mergeOpenAIExtension(providerExtensions.OpenAI, &extCopy)
	r.OpenAIResponsesPassthroughRequired = providerExtensions.OpenAI.ResponsesPassthroughRequired
	r.OpenAIResponsesPassthroughReason = strings.TrimSpace(providerExtensions.OpenAI.ResponsesPassthroughReason)
	if r.OpenAIResponsesPassthroughRequired {
		r.SetTransformerMetadataValue(TransformerMetadataOpenAIResponsesPassthroughRequired, "true")
	}
	if r.OpenAIResponsesPassthroughReason != "" {
		r.SetTransformerMetadataValue(TransformerMetadataOpenAIResponsesPassthroughReason, r.OpenAIResponsesPassthroughReason)
	}
}

func (r *InternalLLMRequest) SetOpenAIRawInputItems(raw json.RawMessage) {
	if r == nil {
		return
	}
	providerExtensions := r.ensureProviderExtensions()
	if providerExtensions.OpenAI == nil {
		providerExtensions.OpenAI = &OpenAIExtension{}
	}
	r.RawInputItems = cloneRawMessage(raw)
	providerExtensions.OpenAI.RawResponseItems = cloneRawMessage(raw)
}

func (r *InternalLLMRequest) SetOpenAIResponsesOptions(options OpenAIResponsesOptions) {
	if r == nil {
		return
	}
	r.PreviousResponseID = cloneStringPtr(options.PreviousResponseID)
	r.Background = cloneBoolPtr(options.Background)
	r.Prompt = cloneRawMessage(options.Prompt)
	r.ResponsesPromptCacheKey = cloneStringPtr(options.PromptCacheKey)
	r.PromptCacheRetention = cloneStringPtr(options.PromptCacheRetention)
	r.SafetyIdentifier = cloneStringPtr(options.SafetyIdentifier)
	r.MaxToolCalls = cloneInt64Ptr(options.MaxToolCalls)
	r.Conversation = cloneRawMessage(options.Conversation)
	r.ContextManagement = cloneRawMessage(options.ContextManagement)
	r.ResponsesStreamOptions = cloneRawMessage(options.StreamOptions)
	r.ReasoningSummary = cloneStringPtr(options.ReasoningSummary)
	r.ReasoningGenerateSummary = cloneStringPtr(options.ReasoningGenerateSummary)
	r.SetOpenAIRawInputItems(options.RawInputItems)
}

func (r *InternalLLMRequest) GetOpenAIResponsesOptions() OpenAIResponsesOptions {
	if r == nil {
		return OpenAIResponsesOptions{}
	}
	return OpenAIResponsesOptions{
		PreviousResponseID:       cloneStringPtr(r.PreviousResponseID),
		Background:               cloneBoolPtr(r.Background),
		Prompt:                   cloneRawMessage(r.Prompt),
		PromptCacheKey:           cloneStringPtr(r.ResponsesPromptCacheKey),
		PromptCacheRetention:     cloneStringPtr(r.PromptCacheRetention),
		SafetyIdentifier:         cloneStringPtr(r.SafetyIdentifier),
		MaxToolCalls:             cloneInt64Ptr(r.MaxToolCalls),
		Conversation:             cloneRawMessage(r.Conversation),
		ContextManagement:        cloneRawMessage(r.ContextManagement),
		StreamOptions:            cloneRawMessage(r.ResponsesStreamOptions),
		ReasoningSummary:         cloneStringPtr(r.ReasoningSummary),
		ReasoningGenerateSummary: cloneStringPtr(r.ReasoningGenerateSummary),
		RawInputItems:            cloneRawMessage(r.RawInputItems),
	}
}

func (r *InternalLLMRequest) OpenAIRawInputItems() json.RawMessage {
	if r == nil {
		return nil
	}
	return cloneRawMessage(r.RawInputItems)
}

func (r *InternalLLMRequest) OpenAIPreviousResponseID() string {
	if r == nil || r.PreviousResponseID == nil {
		return ""
	}
	return strings.TrimSpace(*r.PreviousResponseID)
}

func (r *InternalLLMRequest) GetGeminiExtensions() GeminiExtension {
	if r == nil {
		return GeminiExtension{}
	}
	ext := GeminiExtension{
		CachedContentRef: r.GeminiCachedContentRef,
		SpeechConfig:     r.GeminiSpeechConfig,
	}
	if r.ProviderExtensions != nil && r.ProviderExtensions.Gemini != nil {
		mergeGeminiExtension(&ext, r.ProviderExtensions.Gemini)
	}
	return ext
}

func (r *InternalLLMRequest) GetAnthropicExtensions() AnthropicExtension {
	if r == nil {
		return AnthropicExtension{}
	}
	ext := AnthropicExtension{
		MCPServers: r.AnthropicMCPServers,
		Container:  r.AnthropicContainer,
	}
	if r.ProviderExtensions != nil && r.ProviderExtensions.Anthropic != nil {
		mergeAnthropicExtension(&ext, r.ProviderExtensions.Anthropic)
	}
	return ext
}

func (r *InternalLLMRequest) HasOpenAIResponsesPassthrough() bool {
	if r == nil {
		return false
	}
	if r.OpenAIResponsesPassthroughRequired {
		return true
	}
	if r.ProviderExtensions != nil && r.ProviderExtensions.OpenAI != nil && r.ProviderExtensions.OpenAI.ResponsesPassthroughRequired {
		return true
	}
	return r.TransformerMetadataBool(TransformerMetadataOpenAIResponsesPassthroughRequired)
}

func (r *InternalLLMRequest) OpenAIResponsesPassthroughReasonTextValue() string {
	if r == nil {
		return ""
	}
	if reason := strings.TrimSpace(r.OpenAIResponsesPassthroughReason); reason != "" {
		return reason
	}
	if r.ProviderExtensions != nil && r.ProviderExtensions.OpenAI != nil {
		if reason := strings.TrimSpace(r.ProviderExtensions.OpenAI.ResponsesPassthroughReason); reason != "" {
			return reason
		}
	}
	return r.TransformerMetadataValue(TransformerMetadataOpenAIResponsesPassthroughReason)
}

func (r *InternalLLMRequest) GetOpenAIExtensions() OpenAIExtension {
	if r == nil {
		return OpenAIExtension{}
	}
	ext := OpenAIExtension{}
	if r.ProviderExtensions != nil && r.ProviderExtensions.OpenAI != nil {
		mergeOpenAIExtension(&ext, r.ProviderExtensions.OpenAI)
	}
	if r.HasOpenAIResponsesPassthrough() {
		ext.ResponsesPassthroughRequired = true
		ext.ResponsesPassthroughReason = r.OpenAIResponsesPassthroughReasonTextValue()
	}
	if len(r.RawInputItems) > 0 {
		ext.RawResponseItems = cloneRawMessage(r.RawInputItems)
	}
	return ext
}

func (m *Message) GetAnthropicExtensions() AnthropicExtension {
	if m == nil || m.ProviderExtensions == nil || m.ProviderExtensions.Anthropic == nil {
		return AnthropicExtension{}
	}
	return *m.ProviderExtensions.Anthropic
}

func (p *MessageContentPart) GetAnthropicExtensions() AnthropicExtension {
	if p == nil || p.ProviderExtensions == nil || p.ProviderExtensions.Anthropic == nil {
		return AnthropicExtension{}
	}
	return *p.ProviderExtensions.Anthropic
}

func (tc *ToolCall) GetGeminiExtensions() GeminiExtension {
	ext := GeminiExtension{}
	if tc == nil {
		return ext
	}
	if tc.ThoughtSignature != "" {
		ext.ThoughtSignature = tc.ThoughtSignature
	}
	if tc.ProviderExtensions != nil && tc.ProviderExtensions.Gemini != nil {
		mergeGeminiExtension(&ext, tc.ProviderExtensions.Gemini)
	}
	return ext
}

func (tc *ToolCall) GetAnthropicExtensions() AnthropicExtension {
	if tc == nil || tc.ProviderExtensions == nil || tc.ProviderExtensions.Anthropic == nil {
		return AnthropicExtension{}
	}
	return *tc.ProviderExtensions.Anthropic
}

func mergeGeminiExtension(dst *GeminiExtension, src *GeminiExtension) {
	if dst == nil || src == nil {
		return
	}
	if sig := strings.TrimSpace(src.ThoughtSignature); sig != "" {
		dst.ThoughtSignature = sig
	}
	if src.CachedContentRef != nil {
		dst.CachedContentRef = src.CachedContentRef
	}
	if len(src.SpeechConfig) > 0 {
		dst.SpeechConfig = src.SpeechConfig
	}
}

func mergeAnthropicExtension(dst *AnthropicExtension, src *AnthropicExtension) {
	if dst == nil || src == nil {
		return
	}
	if len(src.Beta) > 0 {
		dst.Beta = append(dst.Beta[:0], src.Beta...)
	}
	if src.CacheControl != nil {
		dst.CacheControl = src.CacheControl
	}
	if len(src.MCPServers) > 0 {
		dst.MCPServers = src.MCPServers
	}
	if len(src.Container) > 0 {
		dst.Container = src.Container
	}
	if len(src.ServerTool) > 0 {
		dst.ServerTool = src.ServerTool
	}
}

func mergeOpenAIExtension(dst *OpenAIExtension, src *OpenAIExtension) {
	if dst == nil || src == nil {
		return
	}
	if src.ResponsesPassthroughRequired {
		dst.ResponsesPassthroughRequired = true
	}
	if reason := strings.TrimSpace(src.ResponsesPassthroughReason); reason != "" {
		dst.ResponsesPassthroughReason = reason
	}
	if len(src.RawResponseItems) > 0 {
		dst.RawResponseItems = cloneRawMessage(src.RawResponseItems)
	}
}
