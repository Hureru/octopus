package relay

import (
	"encoding/json"
	"testing"
	"time"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestResponsesReplayStateKey(t *testing.T) {
	key1 := responsesReplayStateKey(1, 10, "gpt-4", "resp_abc123")
	key2 := responsesReplayStateKey(1, 10, "gpt-4", "resp_abc123")
	if key1 != key2 {
		t.Fatalf("expected same key for same inputs, got %q != %q", key1, key2)
	}

	key3 := responsesReplayStateKey(1, 10, "gpt-4", "resp_xyz789")
	if key1 == key3 {
		t.Fatalf("expected different keys for different response IDs")
	}

	key4 := responsesReplayStateKey(2, 10, "gpt-4", "resp_abc123")
	if key1 == key4 {
		t.Fatalf("expected different keys for different apiKeyIDs")
	}

	key5 := responsesReplayStateKey(1, 20, "gpt-4", "resp_abc123")
	if key1 == key5 {
		t.Fatalf("expected different keys for different groupIDs")
	}

	emptyKey := responsesReplayStateKey(1, 10, "", "resp_abc123")
	if emptyKey != "" {
		t.Fatalf("expected empty key for empty requestModel, got %q", emptyKey)
	}

	emptyKey2 := responsesReplayStateKey(1, 10, "gpt-4", "")
	if emptyKey2 != "" {
		t.Fatalf("expected empty key for empty responseID, got %q", emptyKey2)
	}
}

func TestStoreAndLoadResponsesReplayState(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	state := &wsConversationState{
		RequestModel:      "gpt-4",
		ChannelID:         5,
		ChannelKeyID:      7,
		LastResponseID:    "resp_saved",
		ReplayWindowItems: json.RawMessage(`[{"type":"message","role":"user"}]`),
	}

	storeResponsesReplayState(10, 100, "gpt-4", state, time.Minute)

	loaded := loadResponsesReplayState(10, 100, "gpt-4", "resp_saved")
	if loaded == nil {
		t.Fatal("expected loaded state, got nil")
	}
	if loaded.LastResponseID != "resp_saved" {
		t.Fatalf("expected LastResponseID=resp_saved, got %q", loaded.LastResponseID)
	}
	if loaded.ChannelID != 5 {
		t.Fatalf("expected ChannelID=5, got %d", loaded.ChannelID)
	}
	if loaded.ChannelKeyID != 7 {
		t.Fatalf("expected ChannelKeyID=7, got %d", loaded.ChannelKeyID)
	}
}

func TestLoadResponsesReplayStateNotFound(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	loaded := loadResponsesReplayState(10, 100, "gpt-4", "resp_nonexistent")
	if loaded != nil {
		t.Fatalf("expected nil for nonexistent key, got %+v", loaded)
	}
}

func TestResponsesReplayStateTTL(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	state := &wsConversationState{
		RequestModel:   "gpt-4",
		ChannelID:      5,
		LastResponseID: "resp_expire",
	}

	storeResponsesReplayState(10, 100, "gpt-4", state, 10*time.Millisecond)

	loaded := loadResponsesReplayState(10, 100, "gpt-4", "resp_expire")
	if loaded == nil {
		t.Fatal("expected state before expiration")
	}

	time.Sleep(20 * time.Millisecond)

	loaded = loadResponsesReplayState(10, 100, "gpt-4", "resp_expire")
	if loaded != nil {
		t.Fatalf("expected nil after TTL expiration, got %+v", loaded)
	}
}

func TestResolveResponsesReplayState(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	state := &wsConversationState{
		RequestModel:   "gpt-4",
		ChannelID:      8,
		ChannelKeyID:   12,
		LastResponseID: "resp_test",
	}
	storeResponsesReplayState(20, 200, "gpt-4", state, time.Minute)

	req := &transformerModel.InternalLLMRequest{
		PreviousResponseID: stringPtr("resp_test"),
	}

	resolved := resolveResponsesReplayState(20, 200, "gpt-4", req)
	if resolved == nil {
		t.Fatal("expected resolved state, got nil")
	}
	if resolved.LastResponseID != "resp_test" {
		t.Fatalf("expected LastResponseID=resp_test, got %q", resolved.LastResponseID)
	}
	if resolved.ChannelID != 8 {
		t.Fatalf("expected ChannelID=8, got %d", resolved.ChannelID)
	}
}

func TestResolveResponsesReplayStateNoPreviousID(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	req := &transformerModel.InternalLLMRequest{}

	resolved := resolveResponsesReplayState(20, 200, "gpt-4", req)
	if resolved != nil {
		t.Fatalf("expected nil when no previous_response_id, got %+v", resolved)
	}
}

func TestResponsesReplayStateToSticky(t *testing.T) {
	state := &wsConversationState{
		ChannelID:    15,
		ChannelKeyID: 25,
	}

	sticky := responsesReplayStateToSticky(state)
	if sticky == nil {
		t.Fatal("expected sticky entry, got nil")
	}
	if sticky.ChannelID != 15 {
		t.Fatalf("expected ChannelID=15, got %d", sticky.ChannelID)
	}
	if sticky.ChannelKeyID != 25 {
		t.Fatalf("expected ChannelKeyID=25, got %d", sticky.ChannelKeyID)
	}

	// Nil state
	sticky = responsesReplayStateToSticky(nil)
	if sticky != nil {
		t.Fatalf("expected nil sticky for nil state, got %+v", sticky)
	}

	// Invalid channel ID
	invalidState := &wsConversationState{ChannelID: 0}
	sticky = responsesReplayStateToSticky(invalidState)
	if sticky != nil {
		t.Fatalf("expected nil sticky for invalid channel ID, got %+v", sticky)
	}
}

func TestResponsesReplayStateCloning(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	state := &wsConversationState{
		RequestModel:      "gpt-4",
		ChannelID:         5,
		ChannelKeyID:      7,
		LastResponseID:    "resp_original",
		ReplayWindowItems: json.RawMessage(`[{"type":"message"}]`),
		Transcript: []transformerModel.Message{
			{Role: "user", Content: transformerModel.MessageContent{Content: stringPtr("hello")}},
		},
		ReplayAliases: []string{"alias1"},
		ReplayPending: false,
	}

	storeResponsesReplayState(30, 300, "gpt-4", state, time.Minute)

	// Mutate original
	state.LastResponseID = "resp_mutated"
	state.ChannelID = 999
	state.ReplayWindowItems = json.RawMessage(`[{"type":"mutated"}]`)
	state.Transcript[0].Role = "mutated"
	state.Transcript = append(state.Transcript, transformerModel.Message{Role: "extra"})
	state.ReplayAliases[0] = "mutated_alias"
	state.ReplayAliases = append(state.ReplayAliases, "extra_alias")

	// Load should have original values
	loaded := loadResponsesReplayState(30, 300, "gpt-4", "resp_original")
	if loaded == nil {
		t.Fatal("expected loaded state")
	}
	if loaded.LastResponseID != "resp_original" {
		t.Fatalf("expected cloned state with LastResponseID=resp_original, got %q", loaded.LastResponseID)
	}
	if loaded.ChannelID != 5 {
		t.Fatalf("expected cloned state with ChannelID=5, got %d", loaded.ChannelID)
	}
	if string(loaded.ReplayWindowItems) != `[{"type":"message"}]` {
		t.Fatalf("expected cloned ReplayWindowItems to be original, got %s", loaded.ReplayWindowItems)
	}
	if len(loaded.Transcript) != 1 || loaded.Transcript[0].Role != "user" {
		t.Fatalf("expected cloned Transcript to have 1 user message, got %d messages with role %q", len(loaded.Transcript), loaded.Transcript[0].Role)
	}
	if len(loaded.ReplayAliases) != 1 || loaded.ReplayAliases[0] != "alias1" {
		t.Fatalf("expected cloned ReplayAliases to be [alias1], got %v", loaded.ReplayAliases)
	}
}

func TestStoreResponsesReplayStateValidation(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	// Empty model
	storeResponsesReplayState(1, 10, "", &wsConversationState{LastResponseID: "resp_1"}, time.Minute)
	if loaded := loadResponsesReplayState(1, 10, "", "resp_1"); loaded != nil {
		t.Fatal("expected no store for empty requestModel")
	}

	// Nil state
	storeResponsesReplayState(1, 10, "gpt-4", nil, time.Minute)
	if loaded := loadResponsesReplayState(1, 10, "gpt-4", "resp_nil"); loaded != nil {
		t.Fatal("expected no store for nil state")
	}

	// Empty response ID
	storeResponsesReplayState(1, 10, "gpt-4", &wsConversationState{LastResponseID: ""}, time.Minute)
	if loaded := loadResponsesReplayState(1, 10, "gpt-4", ""); loaded != nil {
		t.Fatal("expected no store for empty LastResponseID")
	}
}

func TestResponsesReplayStateMultipleKeys(t *testing.T) {
	resetResponsesReplayStore()
	defer resetResponsesReplayStore()

	state1 := &wsConversationState{
		RequestModel:   "gpt-4",
		LastResponseID: "resp_a",
		ChannelID:      1,
	}
	state2 := &wsConversationState{
		RequestModel:   "gpt-4",
		LastResponseID: "resp_b",
		ChannelID:      2,
	}

	storeResponsesReplayState(1, 10, "gpt-4", state1, time.Minute)
	storeResponsesReplayState(1, 10, "gpt-4", state2, time.Minute)

	loaded1 := loadResponsesReplayState(1, 10, "gpt-4", "resp_a")
	loaded2 := loadResponsesReplayState(1, 10, "gpt-4", "resp_b")

	if loaded1 == nil || loaded1.LastResponseID != "resp_a" {
		t.Fatal("expected state1 to be stored independently")
	}
	if loaded2 == nil || loaded2.LastResponseID != "resp_b" {
		t.Fatal("expected state2 to be stored independently")
	}
}
