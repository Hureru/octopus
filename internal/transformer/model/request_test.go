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

func TestInternalLLMRequestProviderExtensionViews(t *testing.T) {
	cachedContent := "cachedContents/abc123"
	extCachedContent := "cachedContents/ext456"
	req := &InternalLLMRequest{
		GeminiCachedContentRef: &cachedContent,
		GeminiSpeechConfig:     json.RawMessage(`{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Zephyr"}}}`),
		AnthropicMCPServers:    json.RawMessage(`[{"type":"url","url":"https://example.test/mcp"}]`),
		AnthropicContainer:     json.RawMessage(`{"type":"auto"}`),
		RawInputItems:          json.RawMessage(`[{"type":"message","role":"user"}]`),
		ProviderExtensions: &ProviderExtensions{
			Gemini: &GeminiExtension{
				CachedContentRef: &extCachedContent,
				SpeechConfig:     json.RawMessage(`{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Puck"}}}`),
			},
			Anthropic: &AnthropicExtension{
				Container: json.RawMessage(`{"type":"custom"}`),
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
	if string(anthropic.MCPServers) == "" {
		t.Fatalf("expected Anthropic MCP servers extension")
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
	if !strings.Contains(string(openai.RawResponseItems), "computer_call") {
		t.Fatalf("expected extension raw response items, got %s", openai.RawResponseItems)
	}
}

func TestInternalLLMRequestIRViews(t *testing.T) {
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
		ProviderExtensions: &ProviderExtensions{OpenAI: &OpenAIExtension{
			RawResponseItems: json.RawMessage(`[{
				"type":"message"
			}]`),
		}},
		PreviousResponseID: &previousResponseID,
		RawInputItems: json.RawMessage(`[{
			"type":"message"
		}]`),
		GeminiCachedContentRef: &cachedContent,
		GeminiSpeechConfig:     json.RawMessage(`{"voiceConfig":{"voiceName":"Puck"}}`),
		AnthropicMCPServers:    json.RawMessage(`[{"type":"url"}]`),
		AnthropicContainer:     json.RawMessage(`{"type":"auto"}`),
	}
	req.MarkOpenAIResponsesPassthroughRequired("raw item")

	view := req.IRView()
	if view.Core.Model != req.Model || len(view.Core.Messages) != 1 || len(view.Core.Tools) != 1 {
		t.Fatalf("unexpected core view: %#v", view.Core)
	}
	if view.Core.Stream == nil || !*view.Core.Stream || view.Core.StreamOptions == nil || !view.Core.StreamOptions.IncludeUsage {
		t.Fatalf("unexpected stream core view: %#v", view.Core)
	}
	if view.Sampling.Temperature != &temperature || view.Sampling.TopK != &topK {
		t.Fatalf("unexpected sampling view: %#v", view.Sampling)
	}
	if view.Capabilities.ReasoningBudget != &reasoningBudget || !view.Capabilities.AdaptiveThinking || view.Capabilities.EnableThinking != &enableThinking {
		t.Fatalf("unexpected capability reasoning view: %#v", view.Capabilities)
	}
	if view.Capabilities.Verbosity != &verbosity || view.Capabilities.ServiceTier != &serviceTier || view.Capabilities.Truncation != &truncation {
		t.Fatalf("unexpected capability passthrough view: %#v", view.Capabilities)
	}
	if view.Provider.RawAPIFormat != APIFormatOpenAIResponse || view.Provider.Extensions != req.ProviderExtensions {
		t.Fatalf("unexpected provider view: %#v", view.Provider)
	}
	if view.Provider.OpenAIResponses.PreviousResponseID != &previousResponseID || len(view.Provider.OpenAIResponses.RawInputItems) == 0 {
		t.Fatalf("unexpected OpenAI Responses provider view: %#v", view.Provider.OpenAIResponses)
	}
	if view.Provider.Gemini.CachedContentRef != &cachedContent || !strings.Contains(string(view.Provider.Gemini.SpeechConfig), "Puck") {
		t.Fatalf("unexpected Gemini provider view: %#v", view.Provider.Gemini)
	}
	if len(view.Provider.Anthropic.MCPServers) == 0 || len(view.Provider.Anthropic.Container) == 0 {
		t.Fatalf("unexpected Anthropic provider view: %#v", view.Provider.Anthropic)
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
	if !req.OpenAIResponsesPassthroughRequired || !req.RequiresOpenAIResponsesPassthrough() {
		t.Fatalf("expected typed passthrough flag")
	}
	if req.OpenAIResponsesPassthroughReason != "tool:web_search,input:computer_call" {
		t.Fatalf("unexpected typed passthrough reason: %q", req.OpenAIResponsesPassthroughReason)
	}
	if req.TransformerMetadata[TransformerMetadataOpenAIResponsesPassthroughRequired] != "true" {
		t.Fatalf("expected metadata compatibility flag")
	}
	if ext := req.GetOpenAIExtensions(); !ext.ResponsesPassthroughRequired || ext.ResponsesPassthroughReason != req.OpenAIResponsesPassthroughReason {
		t.Fatalf("unexpected OpenAI extension view: %#v", ext)
	}

	legacy := &InternalLLMRequest{TransformerMetadata: map[string]string{
		TransformerMetadataOpenAIResponsesPassthroughRequired: "true",
		TransformerMetadataOpenAIResponsesPassthroughReason:   "legacy",
	}}
	if !legacy.RequiresOpenAIResponsesPassthrough() || legacy.OpenAIResponsesPassthroughReasonText() != "legacy" {
		t.Fatalf("expected metadata fallback, got %#v", legacy)
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
