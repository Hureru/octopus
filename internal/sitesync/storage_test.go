package sitesync

import (
	"context"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestSiteMaskedTokenMatchesIgnoresOptionalSKPrefix(t *testing.T) {
	tests := []struct {
		name      string
		fullToken string
		masked    string
	}{
		{name: "full has sk prefix", fullToken: "sk-yzFyREALREALOTkb", masked: "yzFy**********OTkb"},
		{name: "masked has sk prefix", fullToken: "yzFyREALREALOTkb", masked: "sk-yzFy**********OTkb"},
		{name: "both have sk prefix", fullToken: "sk-yzFyREALREALOTkb", masked: "sk-yzFy**********OTkb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !siteMaskedTokenMatches(tt.fullToken, tt.masked) {
				t.Fatalf("expected %q to match %q", tt.fullToken, tt.masked)
			}
		})
	}
}

func TestMergePersistedSiteTokensPreservesManualFullTokenWhenIncomingIsMasked(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            41,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-yzFyREALREALOTkb",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:        "primary",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "sk-yzFyREALREALOTkb" {
		t.Fatalf("expected merged token to keep full manual value, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected merged token to remain ready, got %q", merged[0].ValueStatus)
	}
	if !merged[0].Enabled {
		t.Fatalf("expected merged token to remain enabled")
	}
}

func TestMergePersistedSiteTokensTreatsOptionalSKPrefixAsSameReadyToken(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            7,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-abc123",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:      "primary",
		Token:     "abc123",
		GroupKey:  model.SiteDefaultGroupKey,
		GroupName: model.SiteDefaultGroupName,
		Enabled:   true,
		Source:    "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "sk-abc123" {
		t.Fatalf("expected merged token to preserve stored full token format, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected merged token to remain ready, got %q", merged[0].ValueStatus)
	}
}

func TestMergePersistedSiteTokensKeepsMaskedPendingWhenMatchIsAmbiguous(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{
		{
			ID:            1,
			SiteAccountID: 9,
			Name:          "alpha",
			Token:         "sk-yzFyONEOTkb",
			GroupKey:      model.SiteDefaultGroupKey,
			GroupName:     model.SiteDefaultGroupName,
			Enabled:       true,
			ValueStatus:   model.SiteTokenValueStatusReady,
			Source:        "manual",
		},
		{
			ID:            2,
			SiteAccountID: 9,
			Name:          "beta",
			Token:         "sk-yzFyTWOOTkb",
			GroupKey:      model.SiteDefaultGroupKey,
			GroupName:     model.SiteDefaultGroupName,
			Enabled:       true,
			ValueStatus:   model.SiteTokenValueStatusReady,
			Source:        "manual",
		},
	}
	incoming := []model.SiteToken{{
		Name:        "",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 3 {
		t.Fatalf("expected masked pending token plus two preserved manual tokens, got %+v", merged)
	}
	maskedCount := 0
	for _, item := range merged {
		if item.Token == "yzFy**********OTkb" {
			maskedCount++
			if item.ValueStatus != model.SiteTokenValueStatusMaskedPending {
				t.Fatalf("expected ambiguous incoming token to remain masked_pending, got %+v", item)
			}
			if item.Enabled {
				t.Fatalf("expected ambiguous masked_pending token to stay disabled")
			}
		}
	}
	if maskedCount != 1 {
		t.Fatalf("expected exactly one preserved masked_pending token, got %+v", merged)
	}
}

func TestMergePersistedSiteTokensDoesNotOverwriteReadyTokenOnNameOnlyMaskedFallback(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            5,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-different-full-token",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:        "primary",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "sk-different-full-token" {
		t.Fatalf("expected ready token to be preserved on name-only fallback, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected ready token to stay ready, got %q", merged[0].ValueStatus)
	}
}

func TestPersistSyncSnapshotReplacesOnlyAuthoritativeGroups(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	vipGroup := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: "vip", Name: "VIP"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&vipGroup).Error; err != nil {
		t.Fatalf("create vip group failed: %v", err)
	}
	vipToken := model.SiteToken{SiteAccountID: account.ID, Name: "vip", Token: "key-vip", GroupKey: "vip", GroupName: "VIP", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&vipToken).Error; err != nil {
		t.Fatalf("create vip token failed: %v", err)
	}
	vipModel := model.SiteModel{SiteAccountID: account.ID, GroupKey: "vip", ModelName: "gpt-4o-vip-old", Source: "sync", RouteType: model.SiteModelRouteTypeOpenAIChat, RouteSource: model.SiteModelRouteSourceSyncInferred}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&vipModel).Error; err != nil {
		t.Fatalf("create vip model failed: %v", err)
	}

	snapshot := &syncSnapshot{
		accessToken: account.AccessToken,
		groups: []model.SiteUserGroup{
			{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName},
			{GroupKey: "vip", Name: "VIP"},
		},
		tokens: []model.SiteToken{
			{Name: "primary", Token: "key-primary-new", GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true, Source: "sync"},
			{Name: "vip", Token: "key-vip-new", GroupKey: "vip", GroupName: "VIP", Enabled: true, Source: "sync"},
		},
		models: []model.SiteModel{
			{GroupKey: model.SiteDefaultGroupKey, ModelName: "gpt-4.1", Source: "sync", RouteType: model.SiteModelRouteTypeOpenAIChat, RouteSource: model.SiteModelRouteSourceSyncInferred},
		},
		groupResults: []siteGroupSyncResult{
			{GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, HasKey: true, Status: siteGroupSyncStatusSynced, Authoritative: true, ModelCount: 1, Message: "同步到 1 个模型"},
			{GroupKey: "vip", GroupName: "VIP", HasKey: true, Status: siteGroupSyncStatusFailed, Authoritative: false, Message: "unauthorized"},
		},
		status:  model.SiteExecutionStatusPartial,
		message: "部分分组同步完成：更新 1 个分组，保留 1 个分组的历史模型",
	}

	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		t.Fatalf("persistSyncSnapshot returned error: %v", err)
	}

	var models []model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Order("group_key ASC, model_name ASC").Find(&models).Error; err != nil {
		t.Fatalf("query models failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected one refreshed default model and one preserved vip model, got %+v", models)
	}
	modelsByGroup := make(map[string][]string)
	for _, item := range models {
		modelsByGroup[item.GroupKey] = append(modelsByGroup[item.GroupKey], item.ModelName)
	}
	if len(modelsByGroup[model.SiteDefaultGroupKey]) != 1 || modelsByGroup[model.SiteDefaultGroupKey][0] != "gpt-4.1" {
		t.Fatalf("expected default group to be fully replaced, got %+v", modelsByGroup)
	}
	if len(modelsByGroup["vip"]) != 1 || modelsByGroup["vip"][0] != "gpt-4o-vip-old" {
		t.Fatalf("expected vip group to keep historical model, got %+v", modelsByGroup)
	}

	reloaded, err := op.SiteAccountGet(account.ID, context.Background())
	if err != nil {
		t.Fatalf("SiteAccountGet failed: %v", err)
	}
	if reloaded.LastSyncStatus != model.SiteExecutionStatusPartial {
		t.Fatalf("expected partial last_sync_status, got %q", reloaded.LastSyncStatus)
	}
	if reloaded.LastSyncMessage != snapshot.message {
		t.Fatalf("expected last_sync_message %q, got %q", snapshot.message, reloaded.LastSyncMessage)
	}
}
