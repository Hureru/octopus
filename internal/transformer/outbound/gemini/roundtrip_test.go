package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	"github.com/bestruirui/octopus/internal/transformer/model"
)

// TestGeminiThoughtSignatureRoundTrip verifies that thought signatures survive
// a full round-trip: Gemini response → Internal → Anthropic format → Internal → Gemini request.
// This test covers the bug where non-streaming responses didn't set ProviderExtensions,
// causing signatures to be lost in multi-turn conversations.
func TestGeminiThoughtSignatureRoundTrip(t *testing.T) {
	// Step 1: Simulate Gemini response with thought signature
	geminiResp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{
			{
				Index: 0,
				Content: &model.GeminiContent{
					Role: "model",
					Parts: []*model.GeminiPart{
						{
							FunctionCall: &model.GeminiFunctionCall{
								Name: "search",
								Args: map[string]interface{}{"query": "test"},
							},
							ThoughtSignature: "sig-abc-123",
						},
					},
				},
			},
		},
	}

	// Step 2: Convert Gemini response to internal format (non-streaming)
	internalResp := convertGeminiToLLMResponse(geminiResp, false, nil)
	if len(internalResp.Choices) != 1 || internalResp.Choices[0].Message == nil {
		t.Fatalf("unexpected internal response: %+v", internalResp)
	}

	toolCalls := internalResp.Choices[0].Message.ToolCalls
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	// Verify ToolCall has both ThoughtSignature and ProviderExtensions
	tc := toolCalls[0]
	if tc.ThoughtSignature != "sig-abc-123" {
		t.Errorf("ToolCall.ThoughtSignature = %q, want sig-abc-123", tc.ThoughtSignature)
	}
	if tc.ProviderExtensions == nil || tc.ProviderExtensions.Gemini == nil {
		t.Fatal("ToolCall.ProviderExtensions.Gemini is nil")
	}
	if tc.ProviderExtensions.Gemini.ThoughtSignature != "sig-abc-123" {
		t.Errorf("ProviderExtensions.Gemini.ThoughtSignature = %q, want sig-abc-123",
			tc.ProviderExtensions.Gemini.ThoughtSignature)
	}

	// Step 3: Convert to Anthropic format
	anthInbound := &anthropic.MessagesInbound{}
	anthBytes, err := anthInbound.TransformResponse(context.Background(), internalResp)
	if err != nil {
		t.Fatalf("failed to transform to Anthropic format: %v", err)
	}

	// Verify Anthropic response carries the signature only through the standard thinking shim.
	var anthResp anthropic.Message
	if err := json.Unmarshal(anthBytes, &anthResp); err != nil {
		t.Fatalf("failed to unmarshal Anthropic response: %v", err)
	}

	if len(anthResp.Content) != 2 {
		t.Fatalf("expected thinking shim and tool_use block, got %d", len(anthResp.Content))
	}

	shimBlock := anthResp.Content[0]
	if shimBlock.Type != "thinking" || shimBlock.Thinking == nil || *shimBlock.Thinking != "" || shimBlock.Signature == nil || *shimBlock.Signature != "sig-abc-123" {
		t.Fatalf("unexpected thinking shim block: %+v", shimBlock)
	}

	toolUseBlock := anthResp.Content[1]
	if toolUseBlock.Type != "tool_use" {
		t.Fatalf("expected tool_use block, got %s", toolUseBlock.Type)
	}

	// Step 4: Simulate multi-turn request with history
	// Claude Code sends back the assistant message with tool_use in history
	multiTurnReq := &anthropic.MessageRequest{
		Model:     "gemini-2.0-flash-exp",
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			{
				Role: "user",
				Content: anthropic.MessageContent{
					Content: ptrString("First question"),
				},
			},
			{
				Role: "assistant",
				Content: anthropic.MessageContent{
					MultipleContent: []anthropic.MessageContentBlock{
						{
							Type:      shimBlock.Type,
							Thinking:  shimBlock.Thinking,
							Signature: shimBlock.Signature,
						},
						{
							Type:  "tool_use",
							ID:    toolUseBlock.ID,
							Name:  toolUseBlock.Name,
							Input: toolUseBlock.Input,
						},
					},
				},
			},
			{
				Role: "user",
				Content: anthropic.MessageContent{
					MultipleContent: []anthropic.MessageContentBlock{
						{
							Type:      "tool_result",
							ToolUseID: &toolUseBlock.ID,
							Content: &anthropic.MessageContent{
								Content: ptrString("search result"),
							},
						},
					},
				},
			},
			{
				Role: "user",
				Content: anthropic.MessageContent{
					Content: ptrString("Follow-up question"),
				},
			},
		},
	}

	reqBytes, _ := json.Marshal(multiTurnReq)

	// Step 5: Convert Anthropic request to internal format
	internalReq, err := anthInbound.TransformRequest(context.Background(), reqBytes)
	if err != nil {
		t.Fatalf("failed to transform Anthropic request: %v", err)
	}

	// Find the assistant message with tool calls
	var assistantMsg *model.Message
	for i := range internalReq.Messages {
		if internalReq.Messages[i].Role == "assistant" && len(internalReq.Messages[i].ToolCalls) > 0 {
			assistantMsg = &internalReq.Messages[i]
			break
		}
	}
	if assistantMsg == nil {
		t.Fatal("assistant message with tool calls not found")
	}

	// Verify signature was preserved
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call in history, got %d", len(assistantMsg.ToolCalls))
	}
	historyTC := assistantMsg.ToolCalls[0]
	if historyTC.ThoughtSignature != "sig-abc-123" {
		t.Errorf("history ToolCall.ThoughtSignature = %q, want sig-abc-123", historyTC.ThoughtSignature)
	}

	// Step 6: Convert to Gemini request
	geminiReq := convertLLMToGeminiRequest(internalReq)

	// Find the model (assistant) content with function call
	var modelContent *model.GeminiContent
	for _, content := range geminiReq.Contents {
		if content.Role == "model" {
			for _, part := range content.Parts {
				if part.FunctionCall != nil {
					modelContent = content
					break
				}
			}
		}
		if modelContent != nil {
			break
		}
	}

	if modelContent == nil {
		t.Fatal("model content with function call not found in Gemini request")
	}

	// Verify the function call has the thought signature
	var foundSignature bool
	for _, part := range modelContent.Parts {
		if part.FunctionCall != nil && part.FunctionCall.Name == "search" {
			if part.ThoughtSignature != "sig-abc-123" {
				t.Errorf("Gemini request FunctionCall.ThoughtSignature = %q, want sig-abc-123",
					part.ThoughtSignature)
			} else {
				foundSignature = true
			}
			break
		}
	}

	if !foundSignature {
		t.Error("thought signature not found in Gemini request")
	}
}

func TestGeminiStreamToAnthropicPreservesThoughtSignature(t *testing.T) {
	geminiChunk := model.GeminiGenerateContentResponse{
		ResponseId:   "resp_stream",
		ModelVersion: "gemini-2.0-flash-exp",
		Candidates: []*model.GeminiCandidate{{
			Index: 0,
			Content: &model.GeminiContent{
				Role: "model",
				Parts: []*model.GeminiPart{{
					FunctionCall: &model.GeminiFunctionCall{
						Name: "search",
						Args: map[string]interface{}{"query": "octopus"},
					},
					ThoughtSignature: "sig-stream-123",
				}},
			},
			FinishReason: ptrString("STOP"),
		}},
	}
	chunkBytes, err := json.Marshal(geminiChunk)
	if err != nil {
		t.Fatalf("marshal gemini chunk failed: %v", err)
	}

	outbound := &MessagesOutbound{}
	events, err := outbound.TransformStreamEvent(context.Background(), chunkBytes)
	if err != nil {
		t.Fatalf("TransformStreamEvent failed: %v", err)
	}
	internal := model.InternalResponseFromStreamEvents(events)
	if internal == nil {
		t.Fatalf("expected internal stream response to be rebuilt")
	}

	anthInbound := &anthropic.MessagesInbound{}
	streamBytes, err := anthInbound.TransformStream(context.Background(), internal)
	if err != nil {
		t.Fatalf("TransformStream failed: %v", err)
	}
	anthropicEvents := parseAnthropicStreamEvents(t, streamBytes)
	var foundSignatureDelta, foundToolUse bool
	for _, event := range anthropicEvents {
		if event.Type == "content_block_delta" &&
			event.Delta != nil &&
			event.Delta.Type != nil &&
			*event.Delta.Type == "signature_delta" &&
			event.Delta.Signature != nil &&
			*event.Delta.Signature == "sig-stream-123" {
			foundSignatureDelta = true
		}
		if event.Type == "content_block_start" &&
			event.ContentBlock != nil &&
			event.ContentBlock.Type == "tool_use" {
			foundToolUse = true
		}
	}
	if !foundSignatureDelta {
		t.Fatalf("expected Anthropic stream event signature_delta with Gemini thought signature, got %+v", anthropicEvents)
	}
	if !foundToolUse {
		t.Fatalf("expected Anthropic stream event tool_use content block, got %+v", anthropicEvents)
	}
}

func ptrString(s string) *string {
	return &s
}

func parseAnthropicStreamEvents(t *testing.T, raw []byte) []anthropic.StreamEvent {
	t.Helper()

	var events []anthropic.StreamEvent
	for _, line := range bytes.Split(raw, []byte("\n")) {
		payload, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		payload = bytes.TrimPrefix(payload, []byte(" "))
		var event anthropic.StreamEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("failed to decode Anthropic stream event %q: %v", string(payload), err)
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		t.Fatalf("expected Anthropic stream events, got %s", raw)
	}
	return events
}
