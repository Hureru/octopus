package relay

import (
	"encoding/json"
	"strings"
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
		RawResponsesOutputItems: json.RawMessage(`[{"type":"function_call","call_id":"call_abc","name":"get_weather","arguments":"{}"}]`),
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

// TestHTTPReplayMultiTurnChain tests continuous multi-turn replay (resp1 -> replay resp2 -> replay resp3)
func TestHTTPReplayMultiTurnChain(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	apiKeyID := 500
	groupID := 150
	requestModel := "gpt-4"
	channelID := 60
	channelKeyID := 30

	// === Turn 1: Initial request ===
	turn1Req := &transformerModel.InternalLLMRequest{
		Model: requestModel,
		Messages: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("Hello")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	turn1Resp := &transformerModel.InternalLLMResponse{
		ID:                      "resp_turn1",
		Model:                   requestModel,
		RawResponsesOutputItems: json.RawMessage(`[{"type":"message","role":"assistant","content":[{"type":"text","text":"Hi there!"}]}]`),
		Choices: []transformerModel.Choice{
			{Message: &transformerModel.Message{Role: "assistant", Content: transformerModel.MessageContent{Content: stringPtr("Hi there!")}}},
		},
	}

	state1 := &wsConversationState{
		RequestModel: requestModel,
		ChannelID:    channelID,
		ChannelKeyID: channelKeyID,
	}
	state1.ApplySuccessfulTurn(turn1Req, turn1Resp)
	storeResponsesReplayState(apiKeyID, groupID, requestModel, state1, time.Minute)

	// === Turn 2: Replay continuation ===
	turn2Req := &transformerModel.InternalLLMRequest{
		Model:              requestModel,
		PreviousResponseID: stringPtr("resp_turn1"),
		Messages: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("How are you?")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	loadedState2 := resolveResponsesReplayState(apiKeyID, groupID, requestModel, turn2Req)
	if loadedState2 == nil {
		t.Fatal("turn 2: expected to load replay state")
	}
	if loadedState2.LastResponseID != "resp_turn1" {
		t.Fatalf("turn 2: expected LastResponseID=resp_turn1, got %q", loadedState2.LastResponseID)
	}

	replayedReq2 := loadedState2.BuildReplayRequest(turn2Req)
	if replayedReq2 == nil {
		t.Fatal("turn 2: BuildReplayRequest failed")
	}
	if !replayedReq2.IsOpenAIExactReplayRequest() {
		t.Fatal("turn 2: expected exact replay request")
	}

	// Simulate turn 2 response
	turn2Resp := &transformerModel.InternalLLMResponse{
		ID:                      "resp_turn2",
		Model:                   requestModel,
		RawResponsesOutputItems: json.RawMessage(`[{"type":"message","role":"assistant","content":[{"type":"text","text":"I'm good!"}]}]`),
		Choices: []transformerModel.Choice{
			{Message: &transformerModel.Message{Role: "assistant", Content: transformerModel.MessageContent{Content: stringPtr("I'm good!")}}},
		},
	}

	// Save turn 2 state (based on existing state)
	state2 := cloneWSConversationState(loadedState2)
	state2.ChannelID = channelID
	state2.ChannelKeyID = channelKeyID
	state2.ApplySuccessfulTurn(replayedReq2, turn2Resp)
	storeResponsesReplayState(apiKeyID, groupID, requestModel, state2, time.Minute)

	// === Turn 3: Continue replay from turn 2 ===
	turn3Req := &transformerModel.InternalLLMRequest{
		Model:              requestModel,
		PreviousResponseID: stringPtr("resp_turn2"),
		Messages: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("Great!")}},
		},
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}

	loadedState3 := resolveResponsesReplayState(apiKeyID, groupID, requestModel, turn3Req)
	if loadedState3 == nil {
		t.Fatal("turn 3: expected to load replay state from turn 2")
	}
	if loadedState3.LastResponseID != "resp_turn2" {
		t.Fatalf("turn 3: expected LastResponseID=resp_turn2, got %q", loadedState3.LastResponseID)
	}

	// Verify transcript accumulated across turns
	// Turn 1: user + assistant (2 messages)
	// Turn 2: replayedReq2 only has instruction messages, but ApplySuccessfulTurn appends current request messages + response
	// So state2 should have: turn1 user + turn1 assistant + turn2 user + turn2 assistant = 4 messages
	// But BuildReplayRequest retains only instruction messages, so replayedReq2 has 0 user messages
	// ApplySuccessfulTurn uses req.Messages which is empty after BuildReplayRequest
	// This is expected behavior - we need to check the actual accumulated state
	if len(loadedState3.Transcript) < 2 {
		t.Fatalf("turn 3: expected at least 2 messages in transcript, got %d", len(loadedState3.Transcript))
	}

	replayedReq3 := loadedState3.BuildReplayRequest(turn3Req)
	if replayedReq3 == nil {
		t.Fatal("turn 3: BuildReplayRequest failed")
	}
	if !replayedReq3.IsOpenAIExactReplayRequest() {
		t.Fatal("turn 3: expected exact replay request")
	}

	// Verify turn 2 history survives into turn 3's replayed input items
	rawItems3 := replayedReq3.OpenAIRawInputItems()
	if len(rawItems3) == 0 {
		t.Fatal("turn 3: expected non-empty RawInputItems")
	}
	rawItems3Str := string(rawItems3)
	if !strings.Contains(rawItems3Str, "How are you?") {
		t.Fatalf("turn 3: expected RawInputItems to contain turn 2 user message 'How are you?', got %s", rawItems3Str)
	}
	if !strings.Contains(rawItems3Str, "I'm good!") {
		t.Fatalf("turn 3: expected RawInputItems to contain turn 2 assistant message 'I'm good!', got %s", rawItems3Str)
	}
	if !strings.Contains(rawItems3Str, "Hello") {
		t.Fatalf("turn 3: expected RawInputItems to contain turn 1 user message 'Hello', got %s", rawItems3Str)
	}
	if !strings.Contains(rawItems3Str, "Great!") {
		t.Fatalf("turn 3: expected RawInputItems to contain turn 3 user message 'Great!', got %s", rawItems3Str)
	}
}

// TestHTTPReplayFailedMergeKeepsOriginalRequest tests that failed history merge returns nil
func TestHTTPReplayFailedMergeKeepsOriginalRequest(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	apiKeyID := 600
	groupID := 200
	requestModel := "gpt-4"

	// Store state with empty replay window (will cause merge failure)
	state := &wsConversationState{
		RequestModel:      requestModel,
		ChannelID:         70,
		ChannelKeyID:      35,
		LastResponseID:    "resp_empty",
		ReplayWindowItems: nil, // Empty
		Transcript:        nil,
	}
	storeResponsesReplayState(apiKeyID, groupID, requestModel, state, time.Minute)

	// Request with previous_response_id but NO current messages or raw input items
	// This will cause buildRequestInputItems to fail and return ok=false
	req := &transformerModel.InternalLLMRequest{
		Model:              requestModel,
		PreviousResponseID: stringPtr("resp_empty"),
		Messages:           nil, // Empty messages
		RawInputItems:      nil, // Empty raw items
		RawAPIFormat:       transformerModel.APIFormatOpenAIResponse,
	}

	originalPrevID := req.OpenAIPreviousResponseID()
	if originalPrevID != "resp_empty" {
		t.Fatalf("setup error: expected previous_response_id=resp_empty, got %q", originalPrevID)
	}

	loadedState := resolveResponsesReplayState(apiKeyID, groupID, requestModel, req)
	if loadedState == nil {
		t.Fatal("expected to load state")
	}

	// BuildReplayRequest now returns nil on merge failure
	replayedReq := loadedState.BuildReplayRequest(req)
	if replayedReq != nil {
		t.Fatal("expected BuildReplayRequest to return nil on merge failure, but got non-nil")
	}

	// The caller (relay.go) should detect nil and keep the original request
	// This ensures previous_response_id is preserved and can fall back to native continuation
}
