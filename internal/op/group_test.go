package op

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func setupGroupOpTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "octopus-group-op-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}

func TestGroupAutoAddItemsRespectsExistingEndpointFamily(t *testing.T) {
	ctx := setupGroupOpTestDB(t)

	responseChannel := &model.Channel{
		Name:     "response-channel",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://response.example.com/v1"}},
		Model:    "gpt-5.4",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-response"}},
	}
	if err := ChannelCreate(responseChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate response failed: %v", err)
	}

	chatChannel := &model.Channel{
		Name:     "chat-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://chat.example.com/v1"}},
		Model:    "gpt-5.4",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-chat"}},
	}
	if err := ChannelCreate(chatChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate chat failed: %v", err)
	}

	anotherResponseChannel := &model.Channel{
		Name:     "response-channel-2",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://response-two.example.com/v1"}},
		Model:    "gpt-5.4",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-response-two"}},
	}
	if err := ChannelCreate(anotherResponseChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate second response failed: %v", err)
	}

	group := &model.Group{Name: "gpt-5.4", Mode: model.GroupModeFailover}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := GroupItemAdd(&model.GroupItem{
		GroupID:   group.ID,
		ChannelID: responseChannel.ID,
		ModelName: "gpt-5.4",
		Priority:  1,
		Weight:    1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd seed failed: %v", err)
	}

	result, err := GroupAutoAddItems(group.ID, ctx)
	if err != nil {
		t.Fatalf("GroupAutoAddItems failed: %v", err)
	}
	if result.MatchedCandidates != 2 {
		t.Fatalf("expected 2 matched response candidates, got %d", result.MatchedCandidates)
	}
	if result.AddedCandidates != 1 {
		t.Fatalf("expected 1 added candidate, got %d", result.AddedCandidates)
	}
	if result.SkippedCandidates != 1 {
		t.Fatalf("expected 1 skipped candidate, got %d", result.SkippedCandidates)
	}

	items, err := GroupItemList(group.ID, ctx)
	if err != nil {
		t.Fatalf("GroupItemList failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items after auto add, got %d", len(items))
	}
	for _, item := range items {
		if item.ChannelID == chatChannel.ID {
			t.Fatalf("unexpected chat channel %d added into response group", chatChannel.ID)
		}
	}

	secondResult, err := GroupAutoAddItems(group.ID, ctx)
	if err != nil {
		t.Fatalf("second GroupAutoAddItems failed: %v", err)
	}
	if secondResult.AddedCandidates != 0 {
		t.Fatalf("expected no new additions on second run, got %d", secondResult.AddedCandidates)
	}
	if secondResult.SkippedCandidates != 2 {
		t.Fatalf("expected 2 skipped candidates on second run, got %d", secondResult.SkippedCandidates)
	}
}

func TestGroupAutoAddItemsRequiresExactModelMatch(t *testing.T) {
	ctx := setupGroupOpTestDB(t)

	seedChannel := &model.Channel{
		Name:     "response-seed",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://seed.example.com/v1"}},
		Model:    "gpt-5.4",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-seed"}},
	}
	if err := ChannelCreate(seedChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate seed failed: %v", err)
	}

	exactChannel := &model.Channel{
		Name:     "response-exact",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://exact.example.com/v1"}},
		Model:    "gpt-5.4",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-exact"}},
	}
	if err := ChannelCreate(exactChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate exact failed: %v", err)
	}

	partialChannel := &model.Channel{
		Name:     "response-partial",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://partial.example.com/v1"}},
		Model:    "gpt-5.4-mini,gpt-5.4-openai-compact,gpt-5.4-2026-03-05",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-partial"}},
	}
	if err := ChannelCreate(partialChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate partial failed: %v", err)
	}

	group := &model.Group{Name: " gpt-5.4 ", Mode: model.GroupModeFailover}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := GroupItemAdd(&model.GroupItem{
		GroupID:   group.ID,
		ChannelID: seedChannel.ID,
		ModelName: "gpt-5.4",
		Priority:  1,
		Weight:    1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd seed failed: %v", err)
	}

	result, err := GroupAutoAddItems(group.ID, ctx)
	if err != nil {
		t.Fatalf("GroupAutoAddItems failed: %v", err)
	}
	if result.MatchedCandidates != 2 {
		t.Fatalf("expected only exact matches to count, got %d", result.MatchedCandidates)
	}
	if result.AddedCandidates != 1 {
		t.Fatalf("expected 1 exact candidate added, got %d", result.AddedCandidates)
	}

	items, err := GroupItemList(group.ID, ctx)
	if err != nil {
		t.Fatalf("GroupItemList failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected only exact matches in group, got %d items", len(items))
	}
	for _, item := range items {
		if item.ModelName != "gpt-5.4" {
			t.Fatalf("unexpected partial match added: %s", item.ModelName)
		}
	}
}

func TestGroupAutoAddItemsReturnsErrGroupNotFound(t *testing.T) {
	ctx := setupGroupOpTestDB(t)

	_, err := GroupAutoAddItems(9999, ctx)
	if !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("expected ErrGroupNotFound, got %v", err)
	}
}

func TestDeriveGroupAutoAddEndpointUsesDeterministicTieBreak(t *testing.T) {
	ctx := setupGroupOpTestDB(t)

	openAIChannel := &model.Channel{
		Name:     "openai-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://openai.example.com/v1"}},
		Model:    "claude-opus-4-6",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-openai"}},
	}
	if err := ChannelCreate(openAIChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate openai failed: %v", err)
	}

	responseChannel := &model.Channel{
		Name:     "response-channel",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://response.example.com/v1"}},
		Model:    "claude-opus-4-6",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-response"}},
	}
	if err := ChannelCreate(responseChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate response failed: %v", err)
	}

	selected := deriveGroupAutoAddEndpoint([]model.GroupItem{
		{ChannelID: openAIChannel.ID, ModelName: "claude-opus-4-6"},
		{ChannelID: responseChannel.ID, ModelName: "claude-opus-4-6"},
	})
	if selected == nil {
		t.Fatal("expected deterministic endpoint selection on tie")
	}
	if *selected != "openai" {
		t.Fatalf("expected lexicographically stable tie-break to choose openai, got %s", *selected)
	}
}
