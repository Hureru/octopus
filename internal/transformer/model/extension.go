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

type OctopusExtension struct {
	ProviderExtensions *ProviderExtensions `json:"provider_extensions,omitempty"`
}

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

type VolcengineExtension struct {
	Raw json.RawMessage `json:"raw,omitempty"`
}

func GeminiThoughtSignatureExtension(signature string) *OctopusExtension {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return nil
	}
	return &OctopusExtension{
		ProviderExtensions: &ProviderExtensions{
			Gemini: &GeminiExtension{ThoughtSignature: signature},
		},
	}
}

func GeminiThoughtSignatureFromExtension(ext *OctopusExtension) string {
	if ext == nil || ext.ProviderExtensions == nil || ext.ProviderExtensions.Gemini == nil {
		return ""
	}
	return strings.TrimSpace(ext.ProviderExtensions.Gemini.ThoughtSignature)
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

func (r *InternalLLMRequest) GetOpenAIExtensions() OpenAIExtension {
	if r == nil {
		return OpenAIExtension{}
	}
	ext := OpenAIExtension{
		RawResponseItems: r.RawInputItems,
	}
	if r.RequiresOpenAIResponsesPassthrough() {
		ext.ResponsesPassthroughRequired = true
		ext.ResponsesPassthroughReason = r.OpenAIResponsesPassthroughReasonText()
	}
	if r.ProviderExtensions != nil && r.ProviderExtensions.OpenAI != nil {
		mergeOpenAIExtension(&ext, r.ProviderExtensions.OpenAI)
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
		dst.RawResponseItems = src.RawResponseItems
	}
}
