package relay

import (
	"encoding/json"
	"testing"
	"time"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

// TestHTTPReplayIntegration tests the complete HTTP replay flow:
// 1. Initial request succeeds and saves replay state
// 2. Follow-up request with previous_response_id loads state and transforms request
func TestHTTPReplayIntegration(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	apiKeyID := 100
	groupID := 50
	requestModel := "gpt-4"
	channelID := 10
	channelKeyID := 5

	// === Simulate first successful request ===
	firstReq := &transformerModel.InternalLLMRequest{
		Model: requestModel,
		Messages: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("What is AI?")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	firstResp := &transformerModel.InternalLLMResponse{
		ID:    "resp_first123",
		Model: requestModel,
		Choices: []transformerModel.Choice{
			{
				Message: &transformerModel.Message{
					Role:    "assistant",
					Content: transformerModel.MessageContent{Content: stringPtr("AI is...")},
				},
			},
		},
		RawResponsesOutputItems: json.RawMessage(`[{"type":"message","role":"assistant","content":[{"type":"text","text":"AI is..."}]}]`),
	}

	// Save state after first turn
	state := &wsConversationState{
		RequestModel: requestModel,
		ChannelID:    channelID,
		ChannelKeyID: channelKeyID,
	}
	state.ApplySuccessfulTurn(firstReq, firstResp)

	if state.LastResponseID != "resp_first123" {
		t.Fatalf("expected LastResponseID=resp_first123, got %q", state.LastResponseID)
	}
	if len(state.Transcript) != 2 {
		t.Fatalf("expected 2 messages in transcript, got %d", len(state.Transcript))
	}
	if len(state.ReplayWindowItems) == 0 {
		t.Fatal("expected ReplayWindowItems to be populated")
	}

	storeResponsesReplayState(apiKeyID, groupID, requestModel, state, time.Minute)

	// === Simulate second request with previous_response_id ===
	secondReq := &transformerModel.InternalLLMRequest{
		Model:              requestModel,
		PreviousResponseID: stringPtr("resp_first123"),
		Messages: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("Tell me more")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	// Resolve replay state
	loadedState := resolveResponsesReplayState(apiKeyID, groupID, requestModel, secondReq)
	if loadedState == nil {
		t.Fatal("expected to load replay state for second request")
	}
	if loadedState.ChannelID != channelID {
		t.Fatalf("expected ChannelID=%d, got %d", channelID, loadedState.ChannelID)
	}
	if loadedState.ChannelKeyID != channelKeyID {
		t.Fatalf("expected ChannelKeyID=%d, got %d", channelKeyID, loadedState.ChannelKeyID)
	}

	// Transform request using replay state
	replayedReq := loadedState.BuildReplayRequest(secondReq)
	if replayedReq == nil {
		t.Fatal("expected BuildReplayRequest to succeed")
	}

	// Verify transformation
	if replayedReq.OpenAIPreviousResponseID() != "" {
		t.Fatalf("expected previous_response_id to be removed, got %q", replayedReq.OpenAIPreviousResponseID())
	}
	if !replayedReq.IsOpenAIExactReplayRequest() {
		t.Fatal("expected replayed request to be marked as exact replay")
	}
	if len(replayedReq.OpenAIRawInputItems()) == 0 {
		t.Fatal("expected RawInputItems to be merged with history")
	}

	// Verify sticky routing
	stickyEntry := responsesReplayStateToSticky(loadedState)
	if stickyEntry == nil {
		t.Fatal("expected sticky entry for replay state")
	}
	if stickyEntry.ChannelID != channelID {
		t.Fatalf("expected sticky ChannelID=%d, got %d", channelID, stickyEntry.ChannelID)
	}
	if stickyEntry.ChannelKeyID != channelKeyID {
		t.Fatalf("expected sticky ChannelKeyID=%d, got %d", channelKeyID, stickyEntry.ChannelKeyID)
	}
}

// TestHTTPReplayStreamingRequest tests replay with streaming requests
func TestHTTPReplayStreamingRequest(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	apiKeyID := 200
	groupID := 75
	requestModel := "gpt-4"

	stream := true
	firstReq := &transformerModel.InternalLLMRequest{
		Model:  requestModel,
		Stream: &stream,
		Messages: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("Stream test")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	firstResp := &transformerModel.InternalLLMResponse{
		ID:                      "resp_stream456",
		Model:                   requestModel,
		RawResponsesOutputItems: json.RawMessage(`[{"type":"message","role":"assistant","content":[{"type":"text","text":"Streaming..."}]}]`),
		Choices: []transformerModel.Choice{
			{Message: &transformerModel.Message{Role: "assistant", Content: transformerModel.MessageContent{Content: stringPtr("Streaming...")}}},
		},
	}

	state := &wsConversationState{
		RequestModel: requestModel,
		ChannelID:    20,
		ChannelKeyID: 10,
	}
	state.ApplySuccessfulTurn(firstReq, firstResp)
	storeResponsesReplayState(apiKeyID, groupID, requestModel, state, time.Minute)

	// Second streaming request with previous_response_id
	secondReq := &transformerModel.InternalLLMRequest{
		Model:              requestModel,
		Stream:             &stream,
		PreviousResponseID: stringPtr("resp_stream456"),
		Messages: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("Continue")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	loadedState := resolveResponsesReplayState(apiKeyID, groupID, requestModel, secondReq)
	if loadedState == nil {
		t.Fatal("expected to load replay state for streaming request")
	}

	replayedReq := loadedState.BuildReplayRequest(secondReq)
	if replayedReq == nil {
		t.Fatal("expected BuildReplayRequest to succeed for streaming")
	}
	if replayedReq.Stream == nil || !*replayedReq.Stream {
		t.Fatal("expected Stream to be preserved")
	}
}

// TestHTTPReplayDifferentGroups tests isolation between different groups
func TestHTTPReplayDifferentGroups(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	apiKeyID := 300
	requestModel := "gpt-4"

	// Store state for group 1
	state1 := &wsConversationState{
		RequestModel:   requestModel,
		ChannelID:      30,
		ChannelKeyID:   15,
		LastResponseID: "resp_group1",
	}
	storeResponsesReplayState(apiKeyID, 1, requestModel, state1, time.Minute)

	// Store state for group 2 with same response ID (different group should isolate)
	state2 := &wsConversationState{
		RequestModel:   requestModel,
		ChannelID:      40,
		ChannelKeyID:   20,
		LastResponseID: "resp_group1", // Same response ID
	}
	storeResponsesReplayState(apiKeyID, 2, requestModel, state2, time.Minute)

	// Load from group 1
	loaded1 := loadResponsesReplayState(apiKeyID, 1, requestModel, "resp_group1")
	if loaded1 == nil || loaded1.ChannelID != 30 {
		t.Fatal("expected to load state for group 1 with ChannelID=30")
	}

	// Load from group 2
	loaded2 := loadResponsesReplayState(apiKeyID, 2, requestModel, "resp_group1")
	if loaded2 == nil || loaded2.ChannelID != 40 {
		t.Fatal("expected to load state for group 2 with ChannelID=40")
	}
}

// TestHTTPReplayWithToolCalls tests replay with tool calls
func TestHTTPReplayWithToolCalls(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	apiKeyID := 400
	groupID := 100
	requestModel := "gpt-4"

	firstReq := &transformerModel.InternalLLMRequest{
		Model: requestModel,
		Messages: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("What's the weather?")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	firstResp := &transformerModel.InternalLLMResponse{
		ID:    "resp_tool789",
		Model: requestModel,
		Choices: []transformerModel.Choice{
			{
				Message: &transformerModel.Message{
					Role: "assistant",
					ToolCalls: []transformerModel.ToolCall{
						{ID: "call_abc", Function: transformerModel.FunctionCall{Name: "get_weather"}},
					},
				},
			},
		},
		RawResponsesOutputItems: json.RawMessage(`[{"type":"message","role":"assistant","content":[{"type":"function_call_output","call_id":"call_abc"}]}]`),
	}

	state := &wsConversationState{
		RequestModel: requestModel,
		ChannelID:    50,
		ChannelKeyID: 25,
	}
	state.ApplySuccessfulTurn(firstReq, firstResp)
	storeResponsesReplayState(apiKeyID, groupID, requestModel, state, time.Minute)

	// Second request with tool output
	secondReq := &transformerModel.InternalLLMRequest{
		Model:              requestModel,
		PreviousResponseID: stringPtr("resp_tool789"),
		Messages: []transformerModel.Message{
			{Role: "tool", ToolCallID: stringPtr("call_abc"), Content: transformerModel.MessageContent{Content: stringPtr("Sunny, 25°C")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	loadedState := resolveResponsesReplayState(apiKeyID, groupID, requestModel, secondReq)
	if loadedState == nil {
		t.Fatal("expected to load replay state for tool call response")
	}

	replayedReq := loadedState.BuildReplayRequest(secondReq)
	if replayedReq == nil {
		t.Fatal("expected BuildReplayRequest to succeed with tool calls")
	}

	// Verify history includes assistant message with tool call
	if len(loadedState.Transcript) < 2 {
		t.Fatalf("expected at least 2 messages in transcript (user + assistant with tool call), got %d", len(loadedState.Transcript))
	}
}
