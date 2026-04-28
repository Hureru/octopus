package model

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func requestTestStringPtr(value string) *string {
	return &value
}

func TestInternalLLMRequestValidateFillsStableToolCallIDs(t *testing.T) {
	makeRequest := func(prefix bool) *InternalLLMRequest {
		messages := []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				Type: "function",
				Function: FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"octopus","limit":1}`,
				},
			}},
		}}
		if prefix {
			messages = append([]Message{{Role: "user", Content: MessageContent{Content: requestTestStringPtr("prefix")}}}, messages...)
		}
		return &InternalLLMRequest{Model: "gpt-4o", Messages: messages}
	}

	first := makeRequest(false)
	second := makeRequest(true)
	if err := first.Validate(); err != nil {
		t.Fatalf("validate first: %v", err)
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("validate second: %v", err)
	}
	firstID := first.Messages[0].ToolCalls[0].ID
	secondID := second.Messages[1].ToolCalls[0].ID
	if firstID == "" || secondID == "" {
		t.Fatalf("expected generated IDs, got %q and %q", firstID, secondID)
	}
	if firstID != secondID {
		t.Fatalf("expected ID independent of message index, got %q and %q", firstID, secondID)
	}
}

func TestInternalLLMRequestValidatePreservesExistingToolCallIDs(t *testing.T) {
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_existing",
				Type: "function",
				Function: FunctionCall{
					Name:      "lookup",
					Arguments: `{}`,
				},
			}},
		}},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := req.Messages[0].ToolCalls[0].ID; got != "call_existing" {
		t.Fatalf("expected existing ID preserved, got %q", got)
	}
}

func TestInternalLLMRequestValidateDisambiguatesDuplicateGeneratedToolCallIDs(t *testing.T) {
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{Type: "function", Function: FunctionCall{Name: "lookup", Arguments: `{"q":"octopus"}`}},
				{Type: "function", Function: FunctionCall{Name: "lookup", Arguments: `{"q":"octopus"}`}},
			},
		}},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	first := req.Messages[0].ToolCalls[0].ID
	second := req.Messages[0].ToolCalls[1].ID
	if first == "" || second == "" || first == second {
		t.Fatalf("expected unique generated IDs, got %q and %q", first, second)
	}
}

func TestInternalLLMRequestValidateAllowsResponsesRawInputItems(t *testing.T) {
	req := &InternalLLMRequest{
		Model:        "gpt-4o",
		RawAPIFormat: APIFormatOpenAIResponse,
		RawInputItems: json.RawMessage(`[
			{"type":"computer_call","id":"call_1"}
		]`),
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected raw responses input items to satisfy validation, got %v", err)
	}
	if !req.IsChatRequest() {
		t.Fatalf("expected raw responses input items to be treated as chat request")
	}
}

func TestInternalLLMRequestValidateRejectsRawInputItemsWithoutResponsesFormat(t *testing.T) {
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		RawInputItems: json.RawMessage(`[
			{"type":"input_text","text":"hello"}
		]`),
	}

	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "raw_input_items require OpenAI Responses api format") {
		t.Fatalf("expected raw_input_items format validation error, got %v", err)
	}
}

func TestInternalLLMRequestValidateRejectsInvalidRawInputItems(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{
			name: "object",
			raw:  json.RawMessage(`{"type":"input_text","text":"hello"}`),
		},
		{
			name: "null",
			raw:  json.RawMessage(`null`),
		},
		{
			name: "empty array",
			raw:  json.RawMessage(`[]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &InternalLLMRequest{
				Model:         "gpt-4o",
				RawAPIFormat:  APIFormatOpenAIResponse,
				RawInputItems: tt.raw,
			}

			if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "raw_input_items must be a valid JSON array") {
				t.Fatalf("expected raw_input_items json array validation error, got %v", err)
			}
		})
	}
}

func TestInternalLLMRequestValidateRejectsPreviousResponseIDWithoutResponsesFormat(t *testing.T) {
	previousResponseID := "resp_prev"
	req := &InternalLLMRequest{
		Model:              "gpt-4o",
		PreviousResponseID: &previousResponseID,
		Messages:           []Message{{Role: "user", Content: MessageContent{Content: requestTestStringPtr("hello")}}},
	}

	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "previous_response_id requires OpenAI Responses api format") {
		t.Fatalf("expected previous_response_id format validation error, got %v", err)
	}
}

func TestInternalLLMRequestValidateRejectsReplayExactWithPreviousResponseID(t *testing.T) {
	previousResponseID := "resp_prev"
	req := &InternalLLMRequest{
		Model:              "gpt-4o",
		RawAPIFormat:       APIFormatOpenAIResponse,
		PreviousResponseID: &previousResponseID,
		RawInputItems:      json.RawMessage(`[{"type":"input_text","text":"hello"}]`),
	}
	req.MarkOpenAIExactReplayRequest()

	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "replay_exact request must not include previous_response_id") {
		t.Fatalf("expected replay_exact previous_response_id validation error, got %v", err)
	}
}

func TestInternalLLMRequestValidateRejectsReplayExactWithoutRawInputItems(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "missing"},
		{name: "null", raw: json.RawMessage(`null`)},
		{name: "empty array", raw: json.RawMessage(`[]`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &InternalLLMRequest{
				Model:         "gpt-4o",
				RawAPIFormat:  APIFormatOpenAIResponse,
				RawInputItems: tt.raw,
			}
			req.MarkOpenAIExactReplayRequest()

			if err := req.Validate(); err == nil {
				t.Fatalf("expected replay_exact raw_input_items validation error")
			}
		})
	}
}

func TestInternalLLMRequestMarkOpenAIExactReplayRequest(t *testing.T) {
	req := &InternalLLMRequest{}
	req.MarkOpenAIExactReplayRequest()

	if !req.IsOpenAIExactReplayRequest() {
		t.Fatalf("expected marked request to be detected as exact replay")
	}
	if req.TransformerMetadata[TransformerMetadataWSExecutionMode] != TransformerMetadataWSExecutionModeReplayExact {
		t.Fatalf("unexpected ws execution mode metadata: %#v", req.TransformerMetadata)
	}
}

func TestInternalLLMRequestProviderExtensionViewsUseProviderExtensionsWithoutOverridingOpenAIRawInputTruth(t *testing.T) {
	extCachedContent := "cachedContents/ext456"
	req := &InternalLLMRequest{
		RawInputItems: json.RawMessage(`[{"type":"message","role":"user"}]`),
		ProviderExtensions: &ProviderExtensions{
			Gemini: &GeminiExtension{
				CachedContentRef: &extCachedContent,
				SpeechConfig:     json.RawMessage(`{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Puck"}}}`),
			},
			Anthropic: &AnthropicExtension{
				MCPServers: json.RawMessage(`[{"type":"url","url":"https://example.test/mcp"}]`),
				Container:  json.RawMessage(`{"type":"custom"}`),
			},
			OpenAI: &OpenAIExtension{
				RawResponseItems: json.RawMessage(`[{"type":"computer_call","id":"call_1"}]`),
			},
		},
	}
	req.MarkOpenAIResponsesPassthroughRequired("computer_use item")

	gemini := req.GetGeminiExtensions()
	if gemini.CachedContentRef == nil || *gemini.CachedContentRef != extCachedContent {
		t.Fatalf("unexpected Gemini cached content ref: %#v", gemini.CachedContentRef)
	}
	if !strings.Contains(string(gemini.SpeechConfig), "Puck") {
		t.Fatalf("expected extension Gemini speech config, got %s", gemini.SpeechConfig)
	}

	anthropic := req.GetAnthropicExtensions()
	if !strings.Contains(string(anthropic.MCPServers), "example.test/mcp") {
		t.Fatalf("expected Anthropic MCP servers extension, got %s", anthropic.MCPServers)
	}
	if !strings.Contains(string(anthropic.Container), "custom") {
		t.Fatalf("expected Anthropic extension container, got %s", anthropic.Container)
	}

	openai := req.GetOpenAIExtensions()
	if !openai.ResponsesPassthroughRequired {
		t.Fatalf("expected OpenAI Responses passthrough requirement")
	}
	if openai.ResponsesPassthroughReason != "computer_use item" {
		t.Fatalf("unexpected OpenAI Responses passthrough reason: %q", openai.ResponsesPassthroughReason)
	}
	if !strings.Contains(string(openai.RawResponseItems), "\"role\":\"user\"") {
		t.Fatalf("expected RawInputItems to remain authoritative for OpenAI raw response items, got %s", openai.RawResponseItems)
	}
	if strings.Contains(string(openai.RawResponseItems), "computer_call") {
		t.Fatalf("expected stale OpenAI extension payload to stay out of the view, got %s", openai.RawResponseItems)
	}
}

func TestInternalLLMRequestProviderExtensionViewsFallbackToCompatibilityMirrors(t *testing.T) {
	cachedContent := "cachedContents/abc123"
	req := &InternalLLMRequest{
		GeminiCachedContentRef: &cachedContent,
		GeminiSpeechConfig:     json.RawMessage(`{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Zephyr"}}}`),
		AnthropicMCPServers:    json.RawMessage(`[{"type":"url","url":"https://example.test/mcp"}]`),
		AnthropicContainer:     json.RawMessage(`{"type":"auto"}`),
	}

	gemini := req.GetGeminiExtensions()
	if gemini.CachedContentRef == nil || *gemini.CachedContentRef != cachedContent {
		t.Fatalf("expected top-level Gemini cached content compatibility field, got %#v", gemini.CachedContentRef)
	}
	if !strings.Contains(string(gemini.SpeechConfig), "Zephyr") {
		t.Fatalf("expected top-level Gemini speech config compatibility field, got %s", gemini.SpeechConfig)
	}

	anthropic := req.GetAnthropicExtensions()
	if !strings.Contains(string(anthropic.MCPServers), "example.test/mcp") {
		t.Fatalf("expected top-level Anthropic mcp_servers compatibility field, got %s", anthropic.MCPServers)
	}
	if !strings.Contains(string(anthropic.Container), "auto") {
		t.Fatalf("expected top-level Anthropic container compatibility field, got %s", anthropic.Container)
	}
}

func TestInternalLLMRequestSetProviderExtensionsSynchronizesCompatibilityMirrors(t *testing.T) {
	cachedContent := "cachedContents/sync"
	req := &InternalLLMRequest{}
	req.SetGeminiExtensions(GeminiExtension{
		CachedContentRef: &cachedContent,
		SpeechConfig:     json.RawMessage(`{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Nova"}}}`),
	})
	req.SetAnthropicExtensions(AnthropicExtension{
		MCPServers: json.RawMessage(`[{"type":"url","url":"https://example.test/mcp"}]`),
		Container:  json.RawMessage(`{"type":"auto"}`),
	})
	req.SetOpenAIRawInputItems(json.RawMessage(`[{"type":"computer_call","call_id":"call_1"}]`))
	req.MarkOpenAIResponsesPassthroughRequired("tool:computer_use")

	if req.ProviderExtensions == nil || req.ProviderExtensions.OpenAI == nil || req.ProviderExtensions.Gemini == nil || req.ProviderExtensions.Anthropic == nil {
		t.Fatalf("expected provider extensions to be fully populated, got %#v", req.ProviderExtensions)
	}
	if req.GeminiCachedContentRef == nil || *req.GeminiCachedContentRef != cachedContent {
		t.Fatalf("expected Gemini compatibility mirror to sync, got %#v", req.GeminiCachedContentRef)
	}
	if !strings.Contains(string(req.GeminiSpeechConfig), "Nova") {
		t.Fatalf("expected Gemini speech config mirror to sync, got %s", req.GeminiSpeechConfig)
	}
	if !strings.Contains(string(req.AnthropicMCPServers), "example.test/mcp") || !strings.Contains(string(req.AnthropicContainer), "auto") {
		t.Fatalf("expected Anthropic mirrors to sync, got mcp=%s container=%s", req.AnthropicMCPServers, req.AnthropicContainer)
	}
	if !strings.Contains(string(req.RawInputItems), "computer_call") {
		t.Fatalf("expected OpenAI raw input items mirror to sync, got %s", req.RawInputItems)
	}
	if !req.HasOpenAIResponsesPassthrough() || req.OpenAIResponsesPassthroughReasonTextValue() != "tool:computer_use" {
		t.Fatalf("expected OpenAI passthrough mirrors to sync, got required=%t reason=%q", req.HasOpenAIResponsesPassthrough(), req.OpenAIResponsesPassthroughReasonTextValue())
	}
}

func TestInternalLLMRequestSetProviderExtensionsDefensivelyCopiesRawMessages(t *testing.T) {
	req := &InternalLLMRequest{}
	geminiRaw := json.RawMessage(`{"voice":"A"}`)
	anthropicRaw := json.RawMessage(`[{"url":"https://example.test/a"}]`)
	openAIRaw := json.RawMessage(`[{"type":"message","id":"msg_1"}]`)

	req.SetGeminiExtensions(GeminiExtension{SpeechConfig: geminiRaw})
	req.SetAnthropicExtensions(AnthropicExtension{MCPServers: anthropicRaw})
	req.SetOpenAIExtensions(OpenAIExtension{RawResponseItems: openAIRaw})

	geminiRaw[0] = '['
	anthropicRaw[0] = '{'
	openAIRaw[0] = '{'

	if string(req.ProviderExtensions.Gemini.SpeechConfig) != `{"voice":"A"}` {
		t.Fatalf("expected Gemini speech config to be defensively copied, got %s", req.ProviderExtensions.Gemini.SpeechConfig)
	}
	if string(req.ProviderExtensions.Anthropic.MCPServers) != `[{"url":"https://example.test/a"}]` {
		t.Fatalf("expected Anthropic MCP servers to be defensively copied, got %s", req.ProviderExtensions.Anthropic.MCPServers)
	}
	if string(req.ProviderExtensions.OpenAI.RawResponseItems) != `[{"type":"message","id":"msg_1"}]` {
		t.Fatalf("expected OpenAI raw response items to be defensively copied, got %s", req.ProviderExtensions.OpenAI.RawResponseItems)
	}
}

func TestCloneProviderExtensionsDeepCopiesPointerFields(t *testing.T) {
	cachedContent := "cachedContents/original"
	ext := &ProviderExtensions{
		Anthropic: &AnthropicExtension{
			CacheControl: &CacheControl{Type: CacheControlTypeEphemeral, TTL: CacheTTL5m},
		},
		Gemini: &GeminiExtension{
			CachedContentRef: &cachedContent,
		},
	}

	cloned := CloneProviderExtensions(ext)
	if cloned == nil || cloned.Anthropic == nil || cloned.Gemini == nil {
		t.Fatalf("expected provider extensions to clone, got %#v", cloned)
	}

	ext.Anthropic.CacheControl.TTL = CacheTTL1h
	cachedContent = "cachedContents/mutated"

	if cloned.Anthropic.CacheControl == ext.Anthropic.CacheControl || cloned.Anthropic.CacheControl.TTL != CacheTTL5m {
		t.Fatalf("expected cache control to be deep-copied, got %#v", cloned.Anthropic.CacheControl)
	}
	if cloned.Gemini.CachedContentRef == ext.Gemini.CachedContentRef || *cloned.Gemini.CachedContentRef != "cachedContents/original" {
		t.Fatalf("expected cached content ref to be deep-copied, got %#v", cloned.Gemini.CachedContentRef)
	}
}

func TestInternalLLMRequestGetOpenAIExtensionsPrefersRawInputItems(t *testing.T) {
	req := &InternalLLMRequest{
		RawInputItems: json.RawMessage(`[{"type":"function_call_output","output":"fresh"}]`),
		ProviderExtensions: &ProviderExtensions{
			OpenAI: &OpenAIExtension{
				RawResponseItems: json.RawMessage(`[{"type":"input_text","text":"stale"}]`),
			},
		},
	}

	openai := req.GetOpenAIExtensions()
	if !strings.Contains(string(openai.RawResponseItems), "fresh") {
		t.Fatalf("expected RawInputItems to remain authoritative, got %s", openai.RawResponseItems)
	}
	if strings.Contains(string(openai.RawResponseItems), "stale") {
		t.Fatalf("expected stale extension payload to be ignored when RawInputItems exist, got %s", openai.RawResponseItems)
	}
}

func TestInternalLLMRequestSetOpenAIExtensionsDoesNotOverwriteRawInputItems(t *testing.T) {
	req := &InternalLLMRequest{
		RawInputItems: json.RawMessage(`[{"type":"function_call_output","output":"fresh"}]`),
	}

	req.SetOpenAIExtensions(OpenAIExtension{
		RawResponseItems:           json.RawMessage(`[{"type":"input_text","text":"stale"}]`),
		ResponsesPassthroughReason: "tool:test",
	})

	if !strings.Contains(string(req.RawInputItems), "fresh") {
		t.Fatalf("expected RawInputItems to keep authoritative payload, got %s", req.RawInputItems)
	}
	if strings.Contains(string(req.RawInputItems), "stale") {
		t.Fatalf("expected SetOpenAIExtensions to avoid overwriting RawInputItems, got %s", req.RawInputItems)
	}
}

func TestInternalLLMRequestSetOpenAIRawInputItemsCanClearMirror(t *testing.T) {
	req := &InternalLLMRequest{}
	req.SetOpenAIRawInputItems(json.RawMessage(`[{"type":"input_text","text":"hello"}]`))
	req.SetOpenAIRawInputItems(nil)

	if len(req.RawInputItems) != 0 {
		t.Fatalf("expected RawInputItems to clear, got %s", req.RawInputItems)
	}
	if req.ProviderExtensions == nil || req.ProviderExtensions.OpenAI == nil {
		t.Fatalf("expected OpenAI provider extension mirror to exist")
	}
	if len(req.ProviderExtensions.OpenAI.RawResponseItems) != 0 {
		t.Fatalf("expected OpenAI provider extension mirror to clear, got %s", req.ProviderExtensions.OpenAI.RawResponseItems)
	}
}

func TestInternalLLMRequestOpenAIResponsesOptionsRoundTripAndClone(t *testing.T) {
	previousID := "resp_prev"
	background := true
	cacheKey := "cache-key"
	retention := "24h"
	safetyID := "safe-user"
	maxToolCalls := int64(8)
	summary := "auto"
	generateSummary := "concise"
	req := &InternalLLMRequest{}

	req.SetOpenAIResponsesOptions(OpenAIResponsesOptions{
		PreviousResponseID:       &previousID,
		Background:               &background,
		Prompt:                   json.RawMessage(`{"id":"prompt_1"}`),
		PromptCacheKey:           &cacheKey,
		PromptCacheRetention:     &retention,
		SafetyIdentifier:         &safetyID,
		MaxToolCalls:             &maxToolCalls,
		Conversation:             json.RawMessage(`{"id":"conv_1"}`),
		ContextManagement:        json.RawMessage(`{"strategy":"auto"}`),
		StreamOptions:            json.RawMessage(`{"include_usage":true}`),
		ReasoningSummary:         &summary,
		ReasoningGenerateSummary: &generateSummary,
		RawInputItems:            json.RawMessage(`[{"type":"input_text","text":"hello"}]`),
	})

	options := req.GetOpenAIResponsesOptions()
	if options.PreviousResponseID == nil || *options.PreviousResponseID != previousID {
		t.Fatalf("expected previous response id to round-trip, got %#v", options.PreviousResponseID)
	}
	if !strings.Contains(string(options.Prompt), "prompt_1") || !strings.Contains(string(options.RawInputItems), "hello") {
		t.Fatalf("expected raw responses options to round-trip, got prompt=%s raw=%s", options.Prompt, options.RawInputItems)
	}
	if req.ProviderExtensions == nil || req.ProviderExtensions.OpenAI == nil ||
		string(req.ProviderExtensions.OpenAI.RawResponseItems) != string(req.RawInputItems) {
		t.Fatalf("expected raw input mirror to stay synchronized")
	}

	*options.PreviousResponseID = "mutated"
	options.RawInputItems[0] = 'x'
	if req.OpenAIPreviousResponseID() != previousID {
		t.Fatalf("expected previous response id getter to be clone-safe, got %q", req.OpenAIPreviousResponseID())
	}
	if !strings.Contains(string(req.RawInputItems), "hello") {
		t.Fatalf("expected raw input items to be clone-safe, got %s", req.RawInputItems)
	}
}

func TestInternalLLMRequestProviderFields(t *testing.T) {
	content := "hello"
	temperature := 0.7
	topK := int64(40)
	stream := true
	reasoningBudget := int64(1024)
	enableThinking := true
	verbosity := "high"
	serviceTier := "flex"
	truncation := "auto"
	promptCacheKey := "cache-key"
	previousResponseID := "resp_123"
	cachedContent := "cachedContents/abc123"
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []Message{{
			Role:    "user",
			Content: MessageContent{Content: &content},
		}},
		Temperature:    &temperature,
		TopK:           &topK,
		Stream:         &stream,
		StreamOptions:  &StreamOptions{IncludeUsage: true},
		Tools:          []Tool{{Type: "function", Function: Function{Name: "lookup"}}},
		ResponseFormat: &ResponseFormat{Type: "json_object"},
		Modalities:     []string{"text", "audio"},
		Audio: &struct {
			Format string `json:"format,omitempty"`
			Voice  string `json:"voice,omitempty"`
		}{Format: "mp3", Voice: "alloy"},
		ReasoningEffort:  "high",
		ReasoningBudget:  &reasoningBudget,
		AdaptiveThinking: true,
		ThinkingDisplay:  "summarized",
		EnableThinking:   &enableThinking,
		Verbosity:        &verbosity,
		Include:          []string{"reasoning.encrypted_content"},
		ServiceTier:      &serviceTier,
		Truncation:       &truncation,
		PromptCacheKey:   &promptCacheKey,
		RawRequest:       []byte(`{"model":"gpt-4o"}`),
		RawAPIFormat:     APIFormatOpenAIResponse,
		ExtraBody:        json.RawMessage(`{"provider":"extra"}`),
		Query:            url.Values{"timeout": []string{"30"}},
		ProviderExtensions: &ProviderExtensions{
			OpenAI: &OpenAIExtension{
				RawResponseItems: json.RawMessage(`[{
					"type":"message"
				}]`),
			},
			Gemini: &GeminiExtension{
				CachedContentRef: &cachedContent,
				SpeechConfig:     json.RawMessage(`{"voiceConfig":{"voiceName":"Puck"}}`),
			},
			Anthropic: &AnthropicExtension{
				MCPServers: json.RawMessage(`[{"type":"url"}]`),
				Container:  json.RawMessage(`{"type":"auto"}`),
			},
		},
		PreviousResponseID: &previousResponseID,
		RawInputItems: json.RawMessage(`[{
			"type":"message"
		}]`),
	}
	req.MarkOpenAIResponsesPassthroughRequired("raw item")

	if req.Model != "gpt-4o" || len(req.Messages) != 1 || len(req.Tools) != 1 {
		t.Fatalf("unexpected core request fields: %#v", req)
	}
	if req.Stream == nil || !*req.Stream || req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
		t.Fatalf("unexpected stream request fields: %#v", req.StreamOptions)
	}
	if req.Temperature != &temperature || req.TopK != &topK {
		t.Fatalf("unexpected sampling fields: temperature=%v topK=%v", req.Temperature, req.TopK)
	}
	if req.ReasoningBudget != &reasoningBudget || !req.AdaptiveThinking || req.EnableThinking != &enableThinking {
		t.Fatalf("unexpected reasoning capability fields: %#v", req)
	}
	if req.Verbosity != &verbosity || req.ServiceTier != &serviceTier || req.Truncation != &truncation {
		t.Fatalf("unexpected passthrough capability fields: verbosity=%v serviceTier=%v truncation=%v", req.Verbosity, req.ServiceTier, req.Truncation)
	}
	if req.RawAPIFormat != APIFormatOpenAIResponse || req.ProviderExtensions == nil || req.ProviderExtensions.OpenAI == nil {
		t.Fatalf("unexpected provider fields: %#v", req.ProviderExtensions)
	}
	if req.PreviousResponseID != &previousResponseID || len(req.RawInputItems) == 0 {
		t.Fatalf("unexpected OpenAI Responses fields: previous=%v raw=%s", req.PreviousResponseID, req.RawInputItems)
	}
	geminiExt := req.GetGeminiExtensions()
	if geminiExt.CachedContentRef == nil || *geminiExt.CachedContentRef != cachedContent || !strings.Contains(string(geminiExt.SpeechConfig), "Puck") {
		t.Fatalf("unexpected Gemini fields: cached=%v speech=%s", geminiExt.CachedContentRef, geminiExt.SpeechConfig)
	}
	anthropicExt := req.GetAnthropicExtensions()
	if len(anthropicExt.MCPServers) == 0 || len(anthropicExt.Container) == 0 {
		t.Fatalf("unexpected Anthropic fields: mcp=%s container=%s", anthropicExt.MCPServers, anthropicExt.Container)
	}
}
func TestStreamAggregatorMergesChatChunks(t *testing.T) {
	text1 := "hel"
	text2 := "lo"
	reasoning1 := "think "
	reasoning2 := "more"
	finish := FinishReasonToolCalls.String()
	aggregator := &StreamAggregator{}
	aggregator.Add(&InternalLLMResponse{
		ID:                "chunk-1",
		Object:            "chat.completion.chunk",
		Model:             "gpt-4o-mini",
		SystemFingerprint: "fp_1",
		ServiceTier:       "default",
		Choices: []Choice{{
			Index: 0,
			Delta: &Message{
				Role:             "assistant",
				Content:          MessageContent{Content: &text1},
				ReasoningContent: &reasoning1,
				Audio: &struct {
					Data       string `json:"data,omitempty"`
					ExpiresAt  int64  `json:"expires_at,omitempty"`
					ID         string `json:"id,omitempty"`
					Transcript string `json:"transcript,omitempty"`
				}{ID: "aud_1", Data: "AAA", Transcript: "hi", ExpiresAt: 123},
				ToolCalls: []ToolCall{{
					ID:    "call_1",
					Type:  "function",
					Index: 0,
					Function: FunctionCall{
						Name:      "look",
						Arguments: `{"q":`,
					},
				}},
			},
		}},
		Usage: &Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	})
	aggregator.Add(&InternalLLMResponse{
		ID:    "chunk-2",
		Model: "gpt-4o",
		Choices: []Choice{{
			Index: 0,
			Delta: &Message{
				Content:          MessageContent{Content: &text2},
				ReasoningContent: &reasoning2,
				Audio: &struct {
					Data       string `json:"data,omitempty"`
					ExpiresAt  int64  `json:"expires_at,omitempty"`
					ID         string `json:"id,omitempty"`
					Transcript string `json:"transcript,omitempty"`
				}{Data: "BBB", Transcript: " there"},
				ToolCalls: []ToolCall{{
					Index: 0,
					Function: FunctionCall{
						Name:      "up",
						Arguments: `"octopus"}`,
					},
				}},
			},
			FinishReason: &finish,
		}},
		Usage: &Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5},
	})

	response := aggregator.BuildAndReset()
	if response == nil || response.ID != "chunk-2" || response.Model != "gpt-4o" || response.Object != "chat.completion" {
		t.Fatalf("unexpected aggregated response: %#v", response)
	}
	if response.Usage == nil || response.Usage.TotalTokens != 5 {
		t.Fatalf("expected last usage, got %#v", response.Usage)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message == nil {
		t.Fatalf("expected one message choice, got %#v", response.Choices)
	}
	message := response.Choices[0].Message
	if message.Role != "assistant" || message.Content.Content == nil || *message.Content.Content != "hello" {
		t.Fatalf("unexpected message content: %#v", message)
	}
	if message.ReasoningContent == nil || *message.ReasoningContent != "think more" {
		t.Fatalf("unexpected reasoning content: %#v", message.ReasoningContent)
	}
	if message.Audio == nil || message.Audio.ID != "aud_1" || message.Audio.Data != "AAABBB" || message.Audio.Transcript != "hi there" || message.Audio.ExpiresAt != 123 {
		t.Fatalf("unexpected audio aggregation: %#v", message.Audio)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "lookup" || message.ToolCalls[0].Function.Arguments != `{"q":"octopus"}` {
		t.Fatalf("unexpected tool call aggregation: %#v", message.ToolCalls)
	}
	if response.Choices[0].FinishReason == nil || *response.Choices[0].FinishReason != finish {
		t.Fatalf("unexpected finish reason: %#v", response.Choices[0].FinishReason)
	}
	if aggregator.Response() != nil {
		t.Fatalf("expected aggregator reset after build")
	}
}

func TestOpenAIResponsesPassthroughTypedFieldsAndMetadataFallback(t *testing.T) {
	req := &InternalLLMRequest{}
	req.MarkOpenAIResponsesPassthroughRequired("tool:web_search")
	req.MarkOpenAIResponsesPassthroughRequired("input:computer_call")
	if !req.OpenAIResponsesPassthroughRequired || !req.HasOpenAIResponsesPassthrough() {
		t.Fatalf("expected typed passthrough flag")
	}
	if req.OpenAIResponsesPassthroughReason != "tool:web_search,input:computer_call" {
		t.Fatalf("unexpected typed passthrough reason: %q", req.OpenAIResponsesPassthroughReason)
	}
	if req.TransformerMetadata[TransformerMetadataOpenAIResponsesPassthroughRequired] != "true" {
		t.Fatalf("expected metadata compatibility flag")
	}
	if req.OpenAIResponsesPassthroughReasonTextValue() != req.OpenAIResponsesPassthroughReason {
		t.Fatalf("expected new passthrough reason accessor to prefer typed field")
	}
	if ext := req.GetOpenAIExtensions(); !ext.ResponsesPassthroughRequired || ext.ResponsesPassthroughReason != req.OpenAIResponsesPassthroughReason {
		t.Fatalf("unexpected OpenAI extension view: %#v", ext)
	}

	legacy := &InternalLLMRequest{TransformerMetadata: map[string]string{
		TransformerMetadataOpenAIResponsesPassthroughRequired: "true",
		TransformerMetadataOpenAIResponsesPassthroughReason:   "legacy",
	}}
	if !legacy.HasOpenAIResponsesPassthrough() || legacy.OpenAIResponsesPassthroughReasonTextValue() != "legacy" {
		t.Fatalf("expected metadata fallback on new accessors, got %#v", legacy)
	}
}

func TestStreamEventsRoundTripInternalResponse(t *testing.T) {
	text := "hello"
	finish := FinishReasonStop.String()
	chunk := &InternalLLMResponse{
		ID:     "chatcmpl_1",
		Object: "chat.completion.chunk",
		Model:  "gpt-4o",
		Choices: []Choice{{
			Index: 0,
			Delta: &Message{
				Role:    "assistant",
				Content: MessageContent{Content: &text},
				ToolCalls: []ToolCall{{
					ID:    "call_1",
					Type:  "function",
					Index: 0,
					Function: FunctionCall{
						Name:      "lookup",
						Arguments: `{"q":"octopus"}`,
					},
				}},
			},
			FinishReason: &finish,
		}},
		Usage: &Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}

	events := StreamEventsFromInternalResponse(chunk)
	if len(events) == 0 {
		t.Fatalf("expected stream events")
	}
	rebuilt := InternalResponseFromStreamEvents(events)
	if rebuilt == nil || rebuilt.Object != "chat.completion.chunk" {
		t.Fatalf("unexpected rebuilt response: %#v", rebuilt)
	}
	if rebuilt.ID != chunk.ID || rebuilt.Model != chunk.Model {
		t.Fatalf("rebuilt metadata mismatch: %#v", rebuilt)
	}
	if len(rebuilt.Choices) != 1 || rebuilt.Choices[0].Delta == nil {
		t.Fatalf("expected one rebuilt delta choice: %#v", rebuilt.Choices)
	}
	if got := rebuilt.Choices[0].Delta.Content.Content; got == nil || *got != text {
		t.Fatalf("unexpected rebuilt text: %#v", got)
	}
	if len(rebuilt.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected rebuilt tool call: %#v", rebuilt.Choices[0].Delta.ToolCalls)
	}
	if rebuilt.Usage == nil || rebuilt.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected rebuilt usage: %#v", rebuilt.Usage)
	}
}

func TestStreamEventsDoneRoundTrip(t *testing.T) {
	events := StreamEventsFromInternalResponse(&InternalLLMResponse{Object: "[DONE]"})
	if len(events) != 1 || events[0].Kind != StreamEventKindDone {
		t.Fatalf("unexpected done events: %#v", events)
	}
	rebuilt := InternalResponseFromStreamEvents(events)
	if rebuilt == nil || rebuilt.Object != "[DONE]" {
		t.Fatalf("unexpected done response: %#v", rebuilt)
	}
}
