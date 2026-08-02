package op

import (
	"context"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
)

// setupCatalogProvisionTest 在 setupBackupTestDB 之上补一步渠道缓存重置。
// channelCache 是包级缓存，setupBackupTestDB 只换数据库不清缓存，
// 而目录/供给的断言都依赖「当前到底有哪些渠道」，残留会让结果随用例执行顺序漂移。
func setupCatalogProvisionTest(t *testing.T) context.Context {
	t.Helper()
	ctx := setupBackupTestDB(t)
	resetChannelCache()
	t.Cleanup(resetChannelCache)
	return ctx
}

func resetChannelCache() {
	ids := make([]int, 0)
	for id := range channelCache.GetAll() {
		ids = append(ids, id)
	}
	channelCache.Del(ids...)
}

func TestCatalogSyncManualModeSkipsUnselectedModels(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{
		Name:    "manual-provisioning",
		Model:   "z-ai/glm-5.2,claude-opus-4-5",
		Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	// 第二个渠道提供同样的模型：跳过数按模型去重，不能被渠道数放大。
	sibling := model.Channel{
		Name:    "manual-provisioning-sibling",
		Model:   "z-ai/glm-5.2",
		Enabled: true,
	}
	if err := ChannelCreate(&sibling, ctx); err != nil {
		t.Fatalf("create sibling channel: %v", err)
	}

	result, err := CatalogSync(ctx)
	if err != nil {
		t.Fatalf("CatalogSync: %v", err)
	}
	if result.GroupsCreated != 0 || result.CanonicalCreated != 0 {
		t.Fatalf("manual mode must not create groups or canonicals: %+v", result)
	}
	if result.Skipped != 2 {
		t.Fatalf("expected 2 skipped models, got %d (%+v)", result.Skipped, result)
	}

	var groupCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.Group{}).Count(&groupCount).Error; err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 0 {
		t.Fatalf("expected no groups after manual sync, got %d", groupCount)
	}
}

func TestCatalogSyncManualModeStillServesExistingGroups(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{
		Name:    "manual-existing-group",
		Model:   "kept-model,dropped-model",
		Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group := &model.Group{Name: "kept-model", Mode: model.GroupModeRoundRobin}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}

	result, err := CatalogSync(ctx)
	if err != nil {
		t.Fatalf("CatalogSync: %v", err)
	}
	if result.CanonicalCreated != 1 {
		t.Fatalf("expected canonical for the user-created group, got %+v", result)
	}
	if result.GroupsCreated != 0 {
		t.Fatalf("manual mode must not create groups: %+v", result)
	}
	if result.Skipped != 1 {
		t.Fatalf("expected 1 skipped model, got %d (%+v)", result.Skipped, result)
	}

	reloaded, err := GroupGetEnabledMap("kept-model", ctx)
	if err != nil {
		t.Fatalf("load group: %v", err)
	}
	if len(reloaded.Items) != 1 || reloaded.Items[0].ModelName != "kept-model" {
		t.Fatalf("existing group did not receive its channel item: %+v", reloaded.Items)
	}
	findCatalogCandidate(t, channel.ID, "kept-model")
}

func TestCatalogSyncRepairsExistingCanonicalGroupCaseMismatch(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{Name: "case-repair", Model: "GLM-5.1", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := GroupCreate(&model.Group{
		Name: "glm-5.1",
		Mode: model.GroupModeRoundRobin,
	}, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.CanonicalModel{
		Name:            "GLM-5.1",
		NormalizedName:  "glm-5.1",
		RoutingStrategy: model.RoutingStrategyBalanced,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}).Error; err != nil {
		t.Fatalf("create mismatched canonical: %v", err)
	}

	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync: %v", err)
	}
	canonical, ok := CatalogResolveIdentity("GLM-5.1")
	if !ok || canonical.Name != "glm-5.1" {
		t.Fatalf("canonical mismatch was not repaired: ok=%v canonical=%+v", ok, canonical)
	}
	if _, err := GroupGetEnabledMap(canonical.Name, ctx); err != nil {
		t.Fatalf("repaired canonical name does not route: %v", err)
	}
}

func TestCatalogProvisionCreatesGroupPerSelectedModel(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{
		Name:    "provision-per-model",
		Model:   "gpt-5.1,claude-opus-4-5,ignored-model",
		Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	result, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models: []string{"gpt-5.1", "claude-opus-4-5"},
	})
	if err != nil {
		t.Fatalf("CatalogProvision: %v", err)
	}
	if result.GroupsCreated != 2 || result.CanonicalsCreated != 2 || result.GroupItemsCreated != 2 {
		t.Fatalf("unexpected provision result: %+v", result)
	}

	for _, name := range []string{"gpt-5.1", "claude-opus-4-5"} {
		group, err := GroupGetEnabledMap(name, ctx)
		if err != nil {
			t.Fatalf("load group %s: %v", name, err)
		}
		if len(group.Items) != 1 || group.Items[0].ChannelID != channel.ID {
			t.Fatalf("group %s missing channel item: %+v", name, group.Items)
		}
		findCatalogCandidate(t, channel.ID, name)
	}

	if _, err := GroupGetEnabledMap("ignored-model", ctx); err == nil {
		t.Fatal("unselected model must not get a group")
	}

	vendors := canonicalVendorIndex(t, ctx)
	if vendors["gpt-5.1"] != modelvendor.VendorOpenAI {
		t.Fatalf("gpt-5.1 vendor = %q, want %q", vendors["gpt-5.1"], modelvendor.VendorOpenAI)
	}
	if vendors["claude-opus-4-5"] != modelvendor.VendorAnthropic {
		t.Fatalf("claude vendor = %q, want %q", vendors["claude-opus-4-5"], modelvendor.VendorAnthropic)
	}
}

func TestCatalogProvisionAlignsCanonicalNameWithExistingGroupCase(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{
		Name:    "case-alignment",
		Model:   "GLM-5.1",
		Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := GroupCreate(&model.Group{
		Name: "glm-5.1",
		Mode: model.GroupModeRoundRobin,
	}, ctx); err != nil {
		t.Fatalf("create existing group: %v", err)
	}

	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models: []string{"GLM-5.1"},
	}); err != nil {
		t.Fatalf("CatalogProvision: %v", err)
	}

	canonical, ok := CatalogResolveIdentity("GLM-5.1")
	if !ok {
		t.Fatal("canonical identity was not created")
	}
	if canonical.Name != "glm-5.1" {
		t.Fatalf("canonical name = %q, want existing group spelling %q", canonical.Name, "glm-5.1")
	}
	group, err := GroupGetEnabledMap(canonical.Name, ctx)
	if err != nil {
		t.Fatalf("canonical name does not resolve to its group: %v", err)
	}
	if len(group.Items) != 1 || group.Items[0].ChannelID != channel.ID {
		t.Fatalf("existing group was not wired: %+v", group.Items)
	}
}

func TestCatalogProvisionRemapsUpstreamNameIntoTargetGroup(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	useAutoCatalogProvisioning(t)
	channel := model.Channel{
		Name:    "remap-source",
		Model:   "z-ai/glm-5.2",
		Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	// 先复现历史状态：自动建组把上游名建成了独立分组。
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync: %v", err)
	}
	if _, err := GroupGetEnabledMap("z-ai/glm-5.2", ctx); err != nil {
		t.Fatalf("auto-created source group missing: %v", err)
	}
	sourceCandidate := findCatalogCandidate(t, channel.ID, "z-ai/glm-5.2")

	result, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:                  []string{"z-ai/glm-5.2"},
		TargetName:              "glm-5.2",
		DeleteEmptySourceGroups: true,
	})
	if err != nil {
		t.Fatalf("CatalogProvision: %v", err)
	}
	if result.CanonicalsMerged != 1 || result.AliasesCreated != 1 || result.GroupsCreated != 1 {
		t.Fatalf("unexpected remap result: %+v", result)
	}
	if result.GroupsDeleted != 1 {
		t.Fatalf("redundant source group was not removed: %+v", result)
	}

	target, err := GroupGetEnabledMap("glm-5.2", ctx)
	if err != nil {
		t.Fatalf("load target group: %v", err)
	}
	if len(target.Items) != 1 || target.Items[0].ModelName != "z-ai/glm-5.2" {
		t.Fatalf("target group must keep the upstream model name: %+v", target.Items)
	}
	if _, err := GroupGetEnabledMap("z-ai/glm-5.2", ctx); err == nil {
		t.Fatal("source group should be gone after remap")
	}

	// 客户端用上游名请求时仍要能经别名折算到目标分组。
	canonical, ok := CatalogResolveIdentity("z-ai/glm-5.2")
	if !ok || canonical.Name != "glm-5.2" {
		t.Fatalf("upstream alias did not resolve to target: ok=%v canonical=%+v", ok, canonical)
	}
	if canonical.Vendor != modelvendor.VendorZhipuAI {
		t.Fatalf("target vendor = %q, want %q", canonical.Vendor, modelvendor.VendorZhipuAI)
	}

	var candidates []model.RouteCandidate
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("channel_id = ? AND upstream_model_name = ?", channel.ID, "z-ai/glm-5.2").
		Find(&candidates).Error; err != nil {
		t.Fatalf("load candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected exactly one migrated candidate, got %d", len(candidates))
	}
	if candidates[0].ID != sourceCandidate.ID {
		t.Fatalf("candidate was recreated instead of migrated: %d -> %d", sourceCandidate.ID, candidates[0].ID)
	}
	if candidates[0].CanonicalModelID != canonical.ID {
		t.Fatalf("candidate still points at the old canonical: %+v", candidates[0])
	}

	var staleCanonicalCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.CanonicalModel{}).
		Where("normalized_name = ?", "z-ai/glm-5.2").Count(&staleCanonicalCount).Error; err != nil {
		t.Fatalf("count stale canonical: %v", err)
	}
	if staleCanonicalCount != 0 {
		t.Fatalf("source canonical should be merged away, found %d", staleCanonicalCount)
	}
}

func TestCatalogProvisionRemapsExistingAliasWithoutLeavingOldRoute(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{Name: "alias-remap", Model: "upstream-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:     []string{"upstream-model"},
		TargetName: "group-A",
	}); err != nil {
		t.Fatalf("provision group A: %v", err)
	}
	sourceCandidate := findCatalogCandidate(t, channel.ID, "upstream-model")

	result, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:                  []string{"upstream-model"},
		TargetName:              "group-B",
		DeleteEmptySourceGroups: true,
	})
	if err != nil {
		t.Fatalf("remap to group B: %v", err)
	}
	if result.GroupsDeleted != 1 {
		t.Fatalf("old alias group was not deleted: %+v", result)
	}
	if _, err := GroupGetEnabledMap("group-A", ctx); err == nil {
		t.Fatal("old alias group still exists")
	}
	target, err := GroupGetEnabledMap("group-B", ctx)
	if err != nil {
		t.Fatalf("load target group: %v", err)
	}
	if len(target.Items) != 1 || target.Items[0].ModelName != "upstream-model" {
		t.Fatalf("target group wiring is wrong: %+v", target.Items)
	}

	var candidates []model.RouteCandidate
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("channel_id = ? AND LOWER(upstream_model_name) = ?", channel.ID, "upstream-model").
		Find(&candidates).Error; err != nil {
		t.Fatalf("load remapped candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != sourceCandidate.ID {
		t.Fatalf("candidate was duplicated instead of moved: %+v", candidates)
	}
	canonical, ok := CatalogResolveIdentity("upstream-model")
	if !ok || canonical.Name != "group-B" || candidates[0].CanonicalModelID != canonical.ID {
		t.Fatalf("alias or candidate still points at old target: canonical=%+v candidates=%+v", canonical, candidates)
	}
}

func TestCatalogProvisionCandidateConflictMovesScopedReferences(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{Name: "candidate-conflict", Model: "shared-upstream", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:     []string{"shared-upstream"},
		TargetName: "source-group",
	}); err != nil {
		t.Fatalf("provision source group: %v", err)
	}
	sourceCandidate := findCatalogCandidate(t, channel.ID, "shared-upstream")

	targetCanonical := model.CanonicalModel{
		Name:            "target-group",
		NormalizedName:  "target-group",
		RoutingStrategy: model.RoutingStrategyBalanced,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&targetCanonical).Error; err != nil {
		t.Fatalf("create target canonical: %v", err)
	}
	if err := GroupCreate(&model.Group{
		Name: "target-group",
		Mode: model.GroupModeRoundRobin,
	}, ctx); err != nil {
		t.Fatalf("create target group: %v", err)
	}
	targetCandidate := model.RouteCandidate{
		CanonicalModelID:  targetCanonical.ID,
		ChannelID:         channel.ID,
		UpstreamModelName: "shared-upstream",
		Status:            model.RouteCandidateActive,
		Weight:            1,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&targetCandidate).Error; err != nil {
		t.Fatalf("create target candidate: %v", err)
	}
	mustCreateCatalogHeaderPolicy(
		t,
		ctx,
		model.HeaderPolicyScopeRouteCandidate,
		sourceCandidate.ID,
		"source candidate policy",
	)
	sourceQuote := mustCreateCatalogCandidateQuote(t, ctx, sourceCandidate.ID, 41)
	targetQuote := mustCreateCatalogCandidateQuote(t, ctx, targetCandidate.ID, 41)
	movedQuote := mustCreateCatalogCandidateQuote(t, ctx, sourceCandidate.ID, 42)

	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:                  []string{"shared-upstream"},
		TargetName:              "target-group",
		DeleteEmptySourceGroups: true,
	}); err != nil {
		t.Fatalf("remap into conflicting target: %v", err)
	}

	var sourceCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.RouteCandidate{}).
		Where("id = ?", sourceCandidate.ID).Count(&sourceCount).Error; err != nil {
		t.Fatalf("count source candidate: %v", err)
	}
	if sourceCount != 0 {
		t.Fatal("conflicting source candidate was not removed")
	}
	var policy model.HeaderPolicy
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("scope = ? AND scope_id = ?", model.HeaderPolicyScopeRouteCandidate, targetCandidate.ID).
		First(&policy).Error; err != nil {
		t.Fatalf("candidate policy was not moved: %v", err)
	}
	var sourcePolicyCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.HeaderPolicy{}).
		Where("scope = ? AND scope_id = ?", model.HeaderPolicyScopeRouteCandidate, sourceCandidate.ID).
		Count(&sourcePolicyCount).Error; err != nil {
		t.Fatalf("count source policy: %v", err)
	}
	if sourcePolicyCount != 0 {
		t.Fatal("source candidate policy became orphaned")
	}
	var quotes []model.SiteModelPriceQuote
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("route_candidate_id IN ?", []int{sourceCandidate.ID, targetCandidate.ID}).
		Find(&quotes).Error; err != nil {
		t.Fatalf("load candidate quotes: %v", err)
	}
	if len(quotes) != 2 {
		t.Fatalf("target quote must win identity collision: %+v", quotes)
	}
	quoteByID := make(map[int]model.SiteModelPriceQuote, len(quotes))
	for _, quote := range quotes {
		quoteByID[quote.ID] = quote
	}
	if _, found := quoteByID[sourceQuote.ID]; found {
		t.Fatal("conflicting source quote was not removed")
	}
	if _, found := quoteByID[targetQuote.ID]; !found {
		t.Fatal("target quote was removed during conflict resolution")
	}
	reloadedMovedQuote, found := quoteByID[movedQuote.ID]
	if !found || reloadedMovedQuote.RouteCandidateID == nil ||
		*reloadedMovedQuote.RouteCandidateID != targetCandidate.ID {
		t.Fatalf("non-conflicting source quote was not moved: %+v", reloadedMovedQuote)
	}
	expectedMovedQuote := movedQuote
	expectedMovedQuote.RouteCandidateID = &targetCandidate.ID
	expectedMovedQuote.RefreshIdentityKey()
	if reloadedMovedQuote.IdentityKey != expectedMovedQuote.IdentityKey {
		t.Fatalf(
			"moved quote identity = %q, want %q",
			reloadedMovedQuote.IdentityKey,
			expectedMovedQuote.IdentityKey,
		)
	}
}

func TestCatalogProvisionCanonicalMergeMovesCanonicalPolicy(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	useAutoCatalogProvisioning(t)
	channel := model.Channel{Name: "canonical-policy", Model: "source-canonical", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync: %v", err)
	}
	sourceCanonical, ok := CatalogResolveIdentity("source-canonical")
	if !ok {
		t.Fatal("source canonical missing")
	}
	mustCreateCatalogHeaderPolicy(
		t,
		ctx,
		model.HeaderPolicyScopeCanonicalModel,
		sourceCanonical.ID,
		"source canonical policy",
	)

	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:                  []string{"source-canonical"},
		TargetName:              "target-canonical",
		DeleteEmptySourceGroups: true,
	}); err != nil {
		t.Fatalf("merge canonical: %v", err)
	}
	targetCanonical, ok := CatalogResolveIdentity("target-canonical")
	if !ok {
		t.Fatal("target canonical missing")
	}
	var policy model.HeaderPolicy
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("scope = ? AND scope_id = ?", model.HeaderPolicyScopeCanonicalModel, targetCanonical.ID).
		First(&policy).Error; err != nil {
		t.Fatalf("canonical policy was not moved: %v", err)
	}
	var sourcePolicyCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.HeaderPolicy{}).
		Where("scope = ? AND scope_id = ?", model.HeaderPolicyScopeCanonicalModel, sourceCanonical.ID).
		Count(&sourcePolicyCount).Error; err != nil {
		t.Fatalf("count source canonical policy: %v", err)
	}
	if sourcePolicyCount != 0 {
		t.Fatal("source canonical policy became orphaned")
	}
}

func TestCatalogProvisionKeepsSourceGroupWithOtherModels(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	useAutoCatalogProvisioning(t)
	channel := model.Channel{Name: "remap-shared", Model: "shared-alias", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync: %v", err)
	}
	source, err := GroupGetEnabledMap("shared-alias", ctx)
	if err != nil {
		t.Fatalf("load source group: %v", err)
	}
	extra := model.GroupItem{GroupID: source.ID, ChannelID: channel.ID, ModelName: "another-model"}
	if err := GroupItemAdd(&extra, ctx); err != nil {
		t.Fatalf("add unrelated group item: %v", err)
	}

	result, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:                  []string{"shared-alias"},
		TargetName:              "shared-target",
		DeleteEmptySourceGroups: true,
	})
	if err != nil {
		t.Fatalf("CatalogProvision: %v", err)
	}
	if result.GroupsDeleted != 0 {
		t.Fatalf("source group holding other models must be kept: %+v", result)
	}
	if _, err := GroupGetEnabledMap("shared-alias", ctx); err != nil {
		t.Fatalf("source group disappeared: %v", err)
	}
}

func TestCatalogUnprovisionRemovesGroupAndAlias(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{Name: "unprovision", Model: "drop-me,z-ai/keep-alias", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models: []string{"drop-me"},
	}); err != nil {
		t.Fatalf("provision drop-me: %v", err)
	}
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:     []string{"z-ai/keep-alias"},
		TargetName: "keep-alias",
	}); err != nil {
		t.Fatalf("provision alias: %v", err)
	}

	result, err := CatalogUnprovision(ctx, model.CatalogUnprovisionRequest{
		Models:      []string{"drop-me", "z-ai/keep-alias"},
		DeleteGroup: true,
	})
	if err != nil {
		t.Fatalf("CatalogUnprovision: %v", err)
	}
	if result.CanonicalsRemoved != 1 || result.AliasesRemoved != 1 || result.GroupsDeleted != 1 {
		t.Fatalf("unexpected unprovision result: %+v", result)
	}
	if _, err := GroupGetEnabledMap("drop-me", ctx); err == nil {
		t.Fatal("drop-me group should be deleted")
	}
	if _, ok := CatalogResolveIdentity("z-ai/keep-alias"); ok {
		t.Fatal("alias should no longer resolve")
	}

	// 目标分组本身保留，但被取消的上游模型不能再挂在它下面继续路由。
	target, err := GroupGetEnabledMap("keep-alias", ctx)
	if err != nil {
		t.Fatalf("alias target group must survive: %v", err)
	}
	if len(target.Items) != 0 {
		t.Fatalf("unprovisioned upstream model is still routed: %+v", target.Items)
	}
	if result.GroupItemsRemoved != 2 {
		t.Fatalf("expected 2 removed group items, got %d (%+v)", result.GroupItemsRemoved, result)
	}

	var candidateCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.RouteCandidate{}).
		Where("LOWER(upstream_model_name) IN ?", []string{"drop-me", "z-ai/keep-alias"}).
		Count(&candidateCount).Error; err != nil {
		t.Fatalf("count candidates: %v", err)
	}
	if candidateCount != 0 {
		t.Fatalf("route candidates for unprovisioned models remain: %d", candidateCount)
	}
}

func TestCatalogUnprovisionAliasDeletesAliasNamedGroup(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{Name: "alias-delete-group", Model: "alias-leaf", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:     []string{"alias-leaf"},
		TargetName: "alias-target",
	}); err != nil {
		t.Fatalf("provision alias: %v", err)
	}
	if err := GroupCreate(&model.Group{
		Name: "alias-leaf",
		Mode: model.GroupModeRoundRobin,
	}, ctx); err != nil {
		t.Fatalf("create alias-named group: %v", err)
	}

	result, err := CatalogUnprovision(ctx, model.CatalogUnprovisionRequest{
		Models:      []string{"alias-leaf"},
		DeleteGroup: true,
	})
	if err != nil {
		t.Fatalf("CatalogUnprovision: %v", err)
	}
	if result.GroupsDeleted != 1 {
		t.Fatalf("alias-named group was not deleted: %+v", result)
	}
	if _, err := GroupGetEnabledMap("alias-leaf", ctx); err == nil {
		t.Fatal("alias-named group still exists")
	}
}

func TestCatalogUnprovisionDeletesScopedReferences(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{Name: "reference-cleanup", Model: "drop-references", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models: []string{"drop-references"},
	}); err != nil {
		t.Fatalf("provision model: %v", err)
	}
	canonical, ok := CatalogResolveIdentity("drop-references")
	if !ok {
		t.Fatal("canonical missing")
	}
	candidate := findCatalogCandidate(t, channel.ID, "drop-references")
	mustCreateCatalogHeaderPolicy(
		t,
		ctx,
		model.HeaderPolicyScopeCanonicalModel,
		canonical.ID,
		"canonical cleanup policy",
	)
	mustCreateCatalogHeaderPolicy(
		t,
		ctx,
		model.HeaderPolicyScopeRouteCandidate,
		candidate.ID,
		"candidate cleanup policy",
	)
	mustCreateCatalogCandidateQuote(t, ctx, candidate.ID, 73)

	if _, err := CatalogUnprovision(ctx, model.CatalogUnprovisionRequest{
		Models: []string{"drop-references"},
	}); err != nil {
		t.Fatalf("CatalogUnprovision: %v", err)
	}

	var policyCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.HeaderPolicy{}).
		Where(
			"(scope = ? AND scope_id = ?) OR (scope = ? AND scope_id = ?)",
			model.HeaderPolicyScopeCanonicalModel,
			canonical.ID,
			model.HeaderPolicyScopeRouteCandidate,
			candidate.ID,
		).
		Count(&policyCount).Error; err != nil {
		t.Fatalf("count scoped policies: %v", err)
	}
	if policyCount != 0 {
		t.Fatalf("unprovision left %d scoped policies", policyCount)
	}
	var quoteCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteModelPriceQuote{}).
		Where("route_candidate_id = ?", candidate.ID).Count(&quoteCount).Error; err != nil {
		t.Fatalf("count scoped quotes: %v", err)
	}
	if quoteCount != 0 {
		t.Fatalf("unprovision left %d candidate quotes", quoteCount)
	}
}

func TestCatalogDiscoveredModelsReportsVendorAndStatus(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	channel := model.Channel{
		Name:    "discovery",
		Model:   "z-ai/glm-5.2,gpt-5.1,mystery-model",
		Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models: []string{"gpt-5.1"},
	}); err != nil {
		t.Fatalf("provision gpt-5.1: %v", err)
	}
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:     []string{"z-ai/glm-5.2"},
		TargetName: "glm-5.2",
	}); err != nil {
		t.Fatalf("remap glm: %v", err)
	}

	items, err := CatalogDiscoveredModels(ctx)
	if err != nil {
		t.Fatalf("CatalogDiscoveredModels: %v", err)
	}
	byName := make(map[string]model.DiscoveredModel, len(items))
	for _, item := range items {
		byName[item.NormalizedName] = item
	}
	if len(byName) != 3 {
		t.Fatalf("expected 3 discovered models, got %d (%+v)", len(byName), items)
	}

	grouped := byName["gpt-5.1"]
	if grouped.Status != model.DiscoveredModelGrouped || grouped.GroupName != "gpt-5.1" {
		t.Fatalf("gpt-5.1 discovery row wrong: %+v", grouped)
	}
	if grouped.Vendor != modelvendor.VendorOpenAI || grouped.ChannelCount != 1 {
		t.Fatalf("gpt-5.1 vendor/channel wrong: %+v", grouped)
	}

	mapped := byName["z-ai/glm-5.2"]
	if mapped.Status != model.DiscoveredModelMapped || mapped.CanonicalName != "glm-5.2" {
		t.Fatalf("glm discovery row wrong: %+v", mapped)
	}
	if mapped.GroupName != "glm-5.2" || mapped.Vendor != modelvendor.VendorZhipuAI {
		t.Fatalf("glm discovery target wrong: %+v", mapped)
	}

	unknown := byName["mystery-model"]
	if unknown.Status != model.DiscoveredModelUngrouped || unknown.Vendor != "" {
		t.Fatalf("unknown model row wrong: %+v", unknown)
	}
}

func TestCatalogUnprovisionCanonicalHeadCleansAliasGroupItems(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	// 渠道只上报别名上游名；映射到 glm-5.2 目标分组后，
	// 分组条目的 ModelName 是 z-ai/glm-5.2（走别名计入目标组）。
	channel := model.Channel{Name: "unprovision-head", Model: "z-ai/glm-5.2", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	// 把别名叶子上报的上游名映射到目标分组：z-ai/glm-5.2 → glm-5.2。
	if _, err := CatalogProvision(ctx, model.CatalogProvisionRequest{
		Models:     []string{"z-ai/glm-5.2"},
		TargetName: "glm-5.2",
	}); err != nil {
		t.Fatalf("provision alias leaf: %v", err)
	}

	// 只移除 canonical 头 glm-5.2（别名叶子并未选中）；DeleteGroup=false 时分组必须保留。
	result, err := CatalogUnprovision(ctx, model.CatalogUnprovisionRequest{
		Models: []string{"glm-5.2"},
	})
	if err != nil {
		t.Fatalf("CatalogUnprovision: %v", err)
	}
	if result.CanonicalsRemoved != 1 {
		t.Fatalf("expected canonical head removed, got %+v", result)
	}

	group, err := GroupGetEnabledMap("glm-5.2", ctx)
	if err != nil {
		t.Fatalf("load target group: %v", err)
	}
	if len(group.Items) != 0 {
		t.Fatalf("alias group item survived unprovision of canonical head: %+v", group.Items)
	}
	var candidateCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.RouteCandidate{}).
		Where("LOWER(upstream_model_name) = ?", "z-ai/glm-5.2").Count(&candidateCount).Error; err != nil {
		t.Fatalf("count candidates: %v", err)
	}
	if candidateCount != 0 {
		t.Fatalf("alias route candidate survived canonical head unprovision: %d", candidateCount)
	}
}

func canonicalVendorIndex(t *testing.T, ctx context.Context) map[string]string {
	t.Helper()
	var canonicals []model.CanonicalModel
	if err := dbpkg.GetDB().WithContext(ctx).Find(&canonicals).Error; err != nil {
		t.Fatalf("load canonicals: %v", err)
	}
	index := make(map[string]string, len(canonicals))
	for _, canonical := range canonicals {
		index[canonical.NormalizedName] = canonical.Vendor
	}
	return index
}

func mustCreateCatalogHeaderPolicy(
	t *testing.T,
	ctx context.Context,
	scope model.HeaderPolicyScope,
	scopeID int,
	name string,
) model.HeaderPolicy {
	t.Helper()
	policy, err := HeaderPolicyUpsert(ctx, model.HeaderPolicy{
		Name:    name,
		Scope:   scope,
		ScopeID: scopeID,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create header policy: %v", err)
	}
	return *policy
}

func mustCreateCatalogCandidateQuote(
	t *testing.T,
	ctx context.Context,
	candidateID int,
	siteID int,
) model.SiteModelPriceQuote {
	t.Helper()
	quote := model.SiteModelPriceQuote{
		RouteCandidateID:  &candidateID,
		SiteID:            siteID,
		ModelName:         "shared-upstream",
		Source:            model.PriceQuoteSourceManualOverride,
		Unit:              model.PriceUnitPerMillionTokens,
		Currency:          "USD",
		GroupMultiplier:   1,
		ExchangeRateToUSD: 1,
		ManualOverride:    true,
		Status:            model.PriceQuoteStatusValid,
	}
	quote.RefreshIdentityKey()
	if err := dbpkg.GetDB().WithContext(ctx).Create(&quote).Error; err != nil {
		t.Fatalf("create candidate quote: %v", err)
	}
	return quote
}
