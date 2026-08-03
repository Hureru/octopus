package op

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// useAutoCatalogProvisioning 让测试沿用「每个上游模型自动建组」的旧行为。
// 生产默认值已改为 manual，由「模型发现」界面挑选要建组的模型。
func useAutoCatalogProvisioning(t *testing.T) {
	t.Helper()
	settingCache.Set(
		model.SettingKeyCatalogGroupProvisioning,
		string(model.CatalogGroupProvisioningAuto),
	)
	t.Cleanup(func() {
		settingCache.Del(model.SettingKeyCatalogGroupProvisioning)
	})
}

func TestCatalogSyncPreservesCandidateForNonAuthoritativeManagedGroup(t *testing.T) {
	ctx := setupBackupTestDB(t)
	useAutoCatalogProvisioning(t)
	site, account, channel := createCatalogManagedFixture(t, ctx, true)

	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("initial CatalogSync failed: %v", err)
	}
	candidate := findCatalogCandidate(t, channel.ID, "catalog-model")

	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteUserGroup{}).
		Where("site_account_id = ? AND group_key = ?", account.ID, model.SiteDefaultGroupKey).
		Updates(map[string]any{
			"model_sync_authoritative": false,
			"model_sync_status":        model.SiteGroupModelSyncStatusFailed,
		}).Error; err != nil {
		t.Fatalf("mark group non-authoritative failed: %v", err)
	}
	empty := ""
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{
		ID:                 channel.ID,
		Model:              &empty,
		BypassManagedCheck: true,
	}, ctx); err != nil {
		t.Fatalf("clear managed channel model failed: %v", err)
	}

	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync after partial sync failed: %v", err)
	}
	var reloaded model.RouteCandidate
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, candidate.ID).Error; err != nil {
		t.Fatalf("reload candidate failed: %v", err)
	}
	if reloaded.Status != model.RouteCandidateActive || reloaded.UnavailableSince != nil {
		t.Fatalf("non-authoritative sync changed candidate lifecycle: %+v", reloaded)
	}

	var binding model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_id = ? AND site_account_id = ? AND channel_id = ?", site.ID, account.ID, channel.ID).
		First(&binding).Error; err != nil {
		t.Fatalf("managed binding disappeared: %v", err)
	}
}

func TestCatalogSyncArchivesAndRestoresUnseenCandidate(t *testing.T) {
	ctx := setupBackupTestDB(t)
	useAutoCatalogProvisioning(t)
	channel := model.Channel{Name: "catalog-lifecycle", Model: "lifecycle-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("initial CatalogSync failed: %v", err)
	}
	candidate := findCatalogCandidate(t, channel.ID, "lifecycle-model")

	empty := ""
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, Model: &empty}, ctx); err != nil {
		t.Fatalf("clear channel model failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("channel_id = ? AND model_name = ?", channel.ID, "lifecycle-model").
		Delete(&model.GroupItem{}).Error; err != nil {
		t.Fatalf("remove legacy group projection: %v", err)
	}
	if err := groupRefreshCache(ctx); err != nil {
		t.Fatalf("refresh group cache after removal: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync marking unavailable failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, candidate.ID).Error; err != nil {
		t.Fatalf("reload unavailable candidate failed: %v", err)
	}
	if candidate.Status != model.RouteCandidateUnavailable || candidate.UnavailableSince == nil {
		t.Fatalf("candidate was not marked unavailable: %+v", candidate)
	}

	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&candidate).Update("unavailable_since", old).Error; err != nil {
		t.Fatalf("age unavailable candidate failed: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync archiving candidate failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, candidate.ID).Error; err != nil {
		t.Fatalf("reload archived candidate failed: %v", err)
	}
	if candidate.Status != model.RouteCandidateArchived || candidate.ArchivedAt == nil {
		t.Fatalf("candidate was not archived after 30 days: %+v", candidate)
	}

	modelName := "lifecycle-model"
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, Model: &modelName}, ctx); err != nil {
		t.Fatalf("restore channel model failed: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync restoring candidate failed: %v", err)
	}
	var restored model.RouteCandidate
	if err := dbpkg.GetDB().WithContext(ctx).First(&restored, candidate.ID).Error; err != nil {
		t.Fatalf("reload restored candidate failed: %v", err)
	}
	if restored.Status != model.RouteCandidateActive ||
		restored.UnavailableSince != nil ||
		restored.ArchivedAt != nil {
		t.Fatalf("seen candidate did not recover from archive: %+v", restored)
	}
}

func TestCatalogProjectionPreservesHealthOwnedStatuses(t *testing.T) {
	ctx := setupBackupTestDB(t)
	useAutoCatalogProvisioning(t)
	channel := model.Channel{Name: "catalog-health-status", Model: "health-status-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("initial CatalogSync failed: %v", err)
	}
	candidate := findCatalogCandidate(t, channel.ID, "health-status-model")

	if err := dbpkg.GetDB().WithContext(ctx).Model(&candidate).
		Update("status", model.RouteCandidateDegraded).Error; err != nil {
		t.Fatalf("mark candidate degraded: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync with degraded candidate failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, candidate.ID).Error; err != nil {
		t.Fatalf("reload degraded candidate: %v", err)
	}
	if candidate.Status != model.RouteCandidateDegraded {
		t.Fatalf("CatalogSync cleared health degradation: %+v", candidate)
	}

	if err := dbpkg.GetDB().WithContext(ctx).Model(&candidate).
		Update("status", model.RouteCandidateStale).Error; err != nil {
		t.Fatalf("mark candidate stale: %v", err)
	}
	group, err := GroupGetEnabledMap("health-status-model", ctx)
	if err != nil {
		t.Fatalf("load group: %v", err)
	}
	if len(group.Items) != 1 {
		t.Fatalf("unexpected group items: %+v", group.Items)
	}
	group.Items[0].Priority++
	if err := GroupItemUpdate(&group.Items[0], ctx); err != nil {
		t.Fatalf("update group item: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, candidate.ID).Error; err != nil {
		t.Fatalf("reload stale candidate: %v", err)
	}
	if candidate.Status != model.RouteCandidateStale {
		t.Fatalf("local group projection cleared stale status: %+v", candidate)
	}

	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("authoritative CatalogSync failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, candidate.ID).Error; err != nil {
		t.Fatalf("reload synchronized candidate: %v", err)
	}
	if candidate.Status != model.RouteCandidateActive {
		t.Fatalf("successful authoritative sync did not reactivate stale candidate: %+v", candidate)
	}
}

func TestChannelDeleteArchivesRouteCandidates(t *testing.T) {
	ctx := setupBackupTestDB(t)
	useAutoCatalogProvisioning(t)
	channel := model.Channel{Name: "catalog-delete", Model: "delete-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync failed: %v", err)
	}
	candidate := findCatalogCandidate(t, channel.ID, "delete-model")

	if err := ChannelDel(channel.ID, ctx); err != nil {
		t.Fatalf("ChannelDel failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, candidate.ID).Error; err != nil {
		t.Fatalf("reload candidate failed: %v", err)
	}
	if candidate.Status != model.RouteCandidateArchived ||
		candidate.ArchivedAt == nil ||
		candidate.UnavailableSince == nil {
		t.Fatalf("deleted channel candidate was not archived: %+v", candidate)
	}
}

func TestCatalogAliasUpsertRejectsCanonicalAndAliasConflicts(t *testing.T) {
	ctx := setupBackupTestDB(t)
	first := model.CanonicalModel{Name: "First Model", NormalizedName: "first-model", Enabled: true}
	second := model.CanonicalModel{Name: "Second Model", NormalizedName: "second-model", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&first).Error; err != nil {
		t.Fatalf("create first canonical failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&second).Error; err != nil {
		t.Fatalf("create second canonical failed: %v", err)
	}

	if _, err := CatalogAliasUpsert(ctx, first.ID, "first-model"); err == nil {
		t.Fatal("self alias should be rejected")
	}
	if _, err := CatalogAliasUpsert(ctx, first.ID, "second-model"); err == nil {
		t.Fatal("alias matching another canonical should be rejected")
	}
	if _, err := CatalogAliasUpsert(ctx, first.ID, "shared-alias"); err != nil {
		t.Fatalf("create alias failed: %v", err)
	}
	if _, err := CatalogAliasUpsert(ctx, first.ID, "shared-alias"); err != nil {
		t.Fatalf("idempotent alias update failed: %v", err)
	}
	if _, err := CatalogAliasUpsert(ctx, second.ID, "shared-alias"); err == nil {
		t.Fatal("alias reassignment conflict should be rejected")
	}
}

func TestCatalogCaseFoldMapsGLMAndPreservesUpstreamName(t *testing.T) {
	ctx := setupBackupTestDB(t)
	useAutoCatalogProvisioning(t)
	canonical := model.CanonicalModel{
		Name:            "glm-5.1",
		NormalizedName:  "glm-5.1",
		RoutingStrategy: model.RoutingStrategyBalanced,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&canonical).Error; err != nil {
		t.Fatalf("create GLM canonical: %v", err)
	}
	channel := model.Channel{Name: "GLM upstream", Model: "GLM-5.1", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create GLM channel: %v", err)
	}

	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("sync GLM catalog: %v", err)
	}
	resolved, ok := CatalogResolveRequest("GLM-5.1")
	if !ok || resolved.ID != canonical.ID || resolved.Name != "glm-5.1" {
		t.Fatalf("case-folded GLM request resolved incorrectly: ok=%v model=%+v", ok, resolved)
	}
	candidate := findCatalogCandidate(t, channel.ID, "GLM-5.1")
	if candidate.CanonicalModelID != canonical.ID ||
		candidate.UpstreamModelName != "GLM-5.1" {
		t.Fatalf("route candidate lost canonical or raw upstream identity: %+v", candidate)
	}
}

func TestCatalogCanonicalUpdateRejectsNameChanges(t *testing.T) {
	ctx := setupBackupTestDB(t)
	useAutoCatalogProvisioning(t)
	channel := model.Channel{Name: "catalog-stable-name", Model: "stable-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync failed: %v", err)
	}

	var canonical model.CanonicalModel
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("normalized_name = ?", "stable-model").
		First(&canonical).Error; err != nil {
		t.Fatalf("load canonical failed: %v", err)
	}
	originalStrategy := canonical.RoutingStrategy
	canonical.Name = "renamed-model"
	canonical.RoutingStrategy = model.RoutingStrategyReliability
	if _, err := CatalogCanonicalUpdate(ctx, canonical); err == nil {
		t.Fatal("canonical name change should be rejected")
	}

	var reloaded model.CanonicalModel
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, canonical.ID).Error; err != nil {
		t.Fatalf("reload canonical failed: %v", err)
	}
	if reloaded.Name != "stable-model" ||
		reloaded.NormalizedName != "stable-model" ||
		reloaded.RoutingStrategy != originalStrategy {
		t.Fatalf("rejected rename partially updated canonical: %+v", reloaded)
	}
	if _, ok := CatalogResolveRequest("stable-model"); !ok {
		t.Fatal("original canonical name stopped resolving after rejected rename")
	}
	if _, ok := CatalogResolveRequest("renamed-model"); ok {
		t.Fatal("rejected canonical name unexpectedly resolves")
	}

	reloaded.RoutingStrategy = model.RoutingStrategyReliability
	if _, err := CatalogCanonicalUpdate(ctx, reloaded); err != nil {
		t.Fatalf("policy update with unchanged canonical name failed: %v", err)
	}
}

func TestCatalogPlanGroupExcludesItemsWithoutCanonicalRouteCandidate(t *testing.T) {
	ctx := setupBackupTestDB(t)
	channel := model.Channel{Name: "catalog-missing-candidate", Model: "upstream-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	canonical := model.CanonicalModel{
		Name:            "stable-model",
		NormalizedName:  "stable-model",
		RoutingStrategy: model.RoutingStrategyManual,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&canonical).Error; err != nil {
		t.Fatalf("create canonical failed: %v", err)
	}
	governedCandidate := model.RouteCandidate{
		CanonicalModelID:  canonical.ID,
		ChannelID:         channel.ID,
		UpstreamModelName: "different-upstream-model",
		Status:            model.RouteCandidateActive,
		Weight:            1,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&governedCandidate).Error; err != nil {
		t.Fatalf("create governed route candidate failed: %v", err)
	}
	if err := catalogRefreshCache(ctx); err != nil {
		t.Fatalf("refresh catalog cache failed: %v", err)
	}

	group := model.Group{
		Name: canonical.Name,
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{{
			ChannelID: channel.ID,
			ModelName: "upstream-model",
			Priority:  1,
			Weight:    1,
		}},
	}
	planned, preview, resolved, err := CatalogPlanGroup(
		ctx,
		canonical.Name,
		model.ProtocolRouteRequirements{InboundProtocol: model.ProtocolOpenAIChat},
		group,
	)
	if err != nil {
		t.Fatalf("CatalogPlanGroup failed: %v", err)
	}
	if resolved == nil || resolved.ID != canonical.ID {
		t.Fatalf("canonical model was not resolved: %+v", resolved)
	}
	if len(planned.Items) != 0 {
		t.Fatalf("group item without route candidate should be excluded: %+v", planned.Items)
	}
	if len(preview.Decisions) != 1 ||
		preview.Decisions[0].Included ||
		preview.Decisions[0].Reason != "route candidate missing" {
		t.Fatalf("missing candidate should be explicit in preview: %+v", preview.Decisions)
	}

	if err := dbpkg.GetDB().WithContext(ctx).Delete(&governedCandidate).Error; err != nil {
		t.Fatalf("delete governed candidate failed: %v", err)
	}
	legacy, legacyPreview, _, err := CatalogPlanGroup(
		ctx,
		canonical.Name,
		model.ProtocolRouteRequirements{InboundProtocol: model.ProtocolOpenAIChat},
		group,
	)
	if err != nil {
		t.Fatalf("legacy CatalogPlanGroup failed: %v", err)
	}
	if len(legacy.Items) != 1 ||
		len(legacyPreview.Decisions) != 1 ||
		!legacyPreview.Decisions[0].Included {
		t.Fatalf("zero-candidate legacy group fallback was lost: %+v", legacyPreview.Decisions)
	}
}

func TestGroupItemWritesCreateRouteCandidatesImmediately(t *testing.T) {
	ctx := setupBackupTestDB(t)
	useAutoCatalogProvisioning(t)
	seed := model.Channel{Name: "catalog-seed", Model: "governed-model", Enabled: true}
	if err := ChannelCreate(&seed, ctx); err != nil {
		t.Fatalf("create seed channel: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("initial CatalogSync: %v", err)
	}
	group, err := GroupGetEnabledMap("governed-model", ctx)
	if err != nil {
		t.Fatalf("load governed group: %v", err)
	}
	var canonical model.CanonicalModel
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("normalized_name = ?", "governed-model").
		First(&canonical).Error; err != nil {
		t.Fatalf("load canonical: %v", err)
	}

	addChannel := model.Channel{Name: "catalog-add", Enabled: true}
	batchChannel := model.Channel{Name: "catalog-batch", Enabled: true}
	updateChannel := model.Channel{Name: "catalog-update", Enabled: true}
	groupUpdateChannel := model.Channel{Name: "catalog-group-update", Enabled: true}
	manualChannel := model.Channel{Name: "catalog-manual", Enabled: true}
	for _, channel := range []*model.Channel{
		&addChannel,
		&batchChannel,
		&updateChannel,
		&groupUpdateChannel,
		&manualChannel,
	} {
		if err := ChannelCreate(channel, ctx); err != nil {
			t.Fatalf("create channel %s: %v", channel.Name, err)
		}
	}

	added := model.GroupItem{
		GroupID: group.ID, ChannelID: addChannel.ID,
		ModelName: "add-upstream", Priority: 7, Weight: 3,
	}
	if err := GroupItemAdd(&added, ctx); err != nil {
		t.Fatalf("GroupItemAdd: %v", err)
	}
	assertRouteCandidateProjection(t, canonical.ID, added)

	if err := GroupItemBatchAdd(group.ID, []model.GroupIDAndLLMName{{
		ChannelID: batchChannel.ID,
		ModelName: "batch-upstream",
	}}, ctx); err != nil {
		t.Fatalf("GroupItemBatchAdd: %v", err)
	}
	var batchItem model.GroupItem
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("group_id = ? AND channel_id = ?", group.ID, batchChannel.ID).
		First(&batchItem).Error; err != nil {
		t.Fatalf("load batch item: %v", err)
	}
	assertRouteCandidateProjection(t, canonical.ID, batchItem)

	updateItem := model.GroupItem{
		GroupID: group.ID, ChannelID: updateChannel.ID,
		ModelName: "before-update", Priority: 8, Weight: 2,
	}
	if err := GroupItemAdd(&updateItem, ctx); err != nil {
		t.Fatalf("seed update item: %v", err)
	}
	updateItem.ModelName = "after-update"
	updateItem.Priority = 2
	updateItem.Weight = 9
	if err := GroupItemUpdate(&updateItem, ctx); err != nil {
		t.Fatalf("GroupItemUpdate: %v", err)
	}
	assertRouteCandidateProjection(t, canonical.ID, updateItem)
	assertProjectedCandidateUnavailable(t, canonical.ID, updateChannel.ID, "before-update")

	if _, err := GroupUpdate(&model.GroupUpdateRequest{
		ID: group.ID,
		ItemsToAdd: []model.GroupItemAddRequest{{
			ChannelID: groupUpdateChannel.ID,
			ModelName: "group-update-upstream",
			Priority:  4,
			Weight:    6,
		}},
	}, ctx); err != nil {
		t.Fatalf("GroupUpdate: %v", err)
	}
	assertRouteCandidateProjection(t, canonical.ID, model.GroupItem{
		ChannelID: groupUpdateChannel.ID,
		ModelName: "group-update-upstream",
		Priority:  4,
		Weight:    6,
	})

	if err := GroupItemDel(added.ID, ctx); err != nil {
		t.Fatalf("GroupItemDel: %v", err)
	}
	assertProjectedCandidateUnavailable(t, canonical.ID, addChannel.ID, "add-upstream")

	manualItem := model.GroupItem{
		GroupID: group.ID, ChannelID: manualChannel.ID,
		ModelName: "manual-upstream", Priority: 12, Weight: 2,
	}
	if err := GroupItemAdd(&manualItem, ctx); err != nil {
		t.Fatalf("add manual item: %v", err)
	}
	manualCandidate := findCatalogCandidate(t, manualChannel.ID, "manual-upstream")
	if err := dbpkg.GetDB().WithContext(ctx).Model(&manualCandidate).Updates(map[string]any{
		"manual": true,
		"status": model.RouteCandidateDegraded,
	}).Error; err != nil {
		t.Fatalf("make candidate manual: %v", err)
	}
	if err := GroupItemDel(manualItem.ID, ctx); err != nil {
		t.Fatalf("delete manual candidate item: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&manualCandidate, manualCandidate.ID).Error; err != nil {
		t.Fatalf("reload manual candidate: %v", err)
	}
	if !manualCandidate.Manual || manualCandidate.Status != model.RouteCandidateDegraded {
		t.Fatalf("group item deletion overwrote manual candidate lifecycle: %+v", manualCandidate)
	}

	if err := GroupItemBatchDelByChannelAndModels([]model.GroupIDAndLLMName{{
		ChannelID: batchChannel.ID,
		ModelName: "batch-upstream",
	}}, ctx); err != nil {
		t.Fatalf("GroupItemBatchDelByChannelAndModels: %v", err)
	}
	assertProjectedCandidateUnavailable(t, canonical.ID, batchChannel.ID, "batch-upstream")
}

func TestCatalogSyncDiscoversGroupOnlyUpstreamMapping(t *testing.T) {
	ctx := setupBackupTestDB(t)
	channel := model.Channel{Name: "group-only-channel", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group := model.Group{
		Name: "client-facing-model",
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{{
			ChannelID: channel.ID,
			ModelName: "provider-specific-model",
			Priority:  3,
			Weight:    5,
		}},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
		t.Fatalf("create legacy group: %v", err)
	}
	if err := groupRefreshCache(ctx); err != nil {
		t.Fatalf("refresh group cache: %v", err)
	}

	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync: %v", err)
	}
	var canonical model.CanonicalModel
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("normalized_name = ?", "client-facing-model").
		First(&canonical).Error; err != nil {
		t.Fatalf("load canonical: %v", err)
	}
	assertRouteCandidateProjection(t, canonical.ID, group.Items[0])
}

func TestCatalogPlanMaterializesGovernedOrderForBalancer(t *testing.T) {
	ctx := setupBackupTestDB(t)
	first := model.Channel{Name: "candidate-priority-first", Enabled: true}
	second := model.Channel{Name: "candidate-priority-second", Enabled: true}
	if err := ChannelCreate(&first, ctx); err != nil {
		t.Fatalf("create first channel: %v", err)
	}
	if err := ChannelCreate(&second, ctx); err != nil {
		t.Fatalf("create second channel: %v", err)
	}
	canonical := model.CanonicalModel{
		Name:            "priority-model",
		NormalizedName:  "priority-model",
		RoutingStrategy: model.RoutingStrategyManual,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&canonical).Error; err != nil {
		t.Fatalf("create canonical: %v", err)
	}
	candidates := []model.RouteCandidate{
		{
			CanonicalModelID: canonical.ID, ChannelID: first.ID,
			UpstreamModelName: "first-upstream", Status: model.RouteCandidateActive,
			Priority: 1, Weight: 7, Manual: true,
		},
		{
			CanonicalModelID: canonical.ID, ChannelID: second.ID,
			UpstreamModelName: "second-upstream", Status: model.RouteCandidateActive,
			Priority: 9, Weight: 1, Manual: true,
		},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&candidates).Error; err != nil {
		t.Fatalf("create candidates: %v", err)
	}
	if err := catalogRefreshCache(ctx); err != nil {
		t.Fatalf("refresh catalog cache: %v", err)
	}
	group := model.Group{
		Name: "priority-model",
		Mode: model.GroupModeRandom,
		Items: []model.GroupItem{
			{ChannelID: second.ID, ModelName: "second-upstream", Priority: 1, Weight: 1},
			{ChannelID: first.ID, ModelName: "first-upstream", Priority: 9, Weight: 1},
		},
	}
	planned, _, _, err := CatalogPlanGroup(ctx, group.Name, model.ProtocolRouteRequirements{
		InboundProtocol: model.ProtocolOpenAIChat,
	}, group)
	if err != nil {
		t.Fatalf("CatalogPlanGroup: %v", err)
	}
	if planned.Mode != model.GroupModeFailover {
		t.Fatalf("governed order was not materialized as failover: mode=%d", planned.Mode)
	}
	if len(planned.Items) != 2 ||
		planned.Items[0].ChannelID != first.ID ||
		planned.Items[0].Priority != 0 ||
		planned.Items[0].Weight != 7 {
		t.Fatalf("planner did not materialize candidate priority/weight: %+v", planned.Items)
	}
}

func TestDisabledCanonicalDoesNotUseLegacyGroupFallback(t *testing.T) {
	ctx := setupBackupTestDB(t)
	channel := model.Channel{Name: "disabled-canonical-channel", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	canonical := model.CanonicalModel{
		Name:            "disabled-model",
		NormalizedName:  "disabled-model",
		RoutingStrategy: model.RoutingStrategyManual,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         false,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&canonical).Error; err != nil {
		t.Fatalf("create disabled canonical: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&canonical).
		UpdateColumn("enabled", false).Error; err != nil {
		t.Fatalf("disable canonical: %v", err)
	}
	canonical.Enabled = false
	alias := model.ModelAlias{
		CanonicalModelID: canonical.ID,
		Alias:            "disabled-alias",
		NormalizedAlias:  "disabled-alias",
		Manual:           true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&alias).Error; err != nil {
		t.Fatalf("create alias: %v", err)
	}
	if err := catalogRefreshCache(ctx); err != nil {
		t.Fatalf("refresh catalog cache: %v", err)
	}
	if _, ok := CatalogResolveRequest(canonical.Name); ok {
		t.Fatal("disabled canonical unexpectedly resolved as routable")
	}
	if resolved, ok := CatalogResolveIdentity(alias.Alias); !ok || resolved.ID != canonical.ID {
		t.Fatalf("disabled alias identity was lost: ok=%v resolved=%+v", ok, resolved)
	}
	group := model.Group{
		Name: canonical.Name,
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{{
			ChannelID: channel.ID,
			ModelName: "legacy-upstream",
			Priority:  1,
			Weight:    1,
		}},
	}
	planned, preview, resolved, err := CatalogPlanGroup(
		ctx,
		alias.Alias,
		model.ProtocolRouteRequirements{InboundProtocol: model.ProtocolOpenAIChat},
		group,
	)
	if err != nil {
		t.Fatalf("CatalogPlanGroup: %v", err)
	}
	if resolved == nil || resolved.ID != canonical.ID || len(planned.Items) != 0 {
		t.Fatalf("disabled canonical fell back to legacy group: resolved=%+v items=%+v", resolved, planned.Items)
	}
	if len(preview.Decisions) != 1 || preview.Decisions[0].Reason != "canonical disabled" {
		t.Fatalf("disabled decision was not explicit: %+v", preview.Decisions)
	}
}

func TestLowestCostUsesConvertibleTokenPriceAndRejectsPerRequestShortcut(t *testing.T) {
	ctx := setupBackupTestDB(t)
	site := model.Site{
		Name: "lowest-cost-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://lowest-cost.example.com", Enabled: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "lowest-cost-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	canonical := model.CanonicalModel{
		Name:            "lowest-cost-model",
		NormalizedName:  "lowest-cost-model",
		RoutingStrategy: model.RoutingStrategyLowestCost,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&canonical).Error; err != nil {
		t.Fatalf("create canonical: %v", err)
	}
	channels := []model.Channel{
		{Name: "per-request-channel", Enabled: true},
		{Name: "usd-channel", Enabled: true},
		{Name: "converted-channel", Enabled: true},
	}
	for index := range channels {
		if err := ChannelCreate(&channels[index], ctx); err != nil {
			t.Fatalf("create channel %d: %v", index, err)
		}
	}
	candidates := make([]model.RouteCandidate, len(channels))
	for index := range channels {
		candidates[index] = model.RouteCandidate{
			CanonicalModelID: canonical.ID,
			ChannelID:        channels[index].ID,
			UpstreamModelName: []string{
				"per-request-upstream",
				"usd-upstream",
				"converted-upstream",
			}[index],
			SiteID:        &site.ID,
			SiteAccountID: &account.ID,
			SiteGroupKey:  model.SiteDefaultGroupKey,
			Status:        model.RouteCandidateActive,
			Priority:      []int{1, 2, 9}[index],
			Weight:        1,
			Manual:        true,
			LastSeenAt:    time.Now(),
		}
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&candidates).Error; err != nil {
		t.Fatalf("create candidates: %v", err)
	}
	quotes := []model.SiteModelPriceQuote{
		{
			RouteCandidateID: &candidates[0].ID,
			SiteID:           site.ID, SiteAccountID: &account.ID,
			GroupKey: model.SiteDefaultGroupKey, ModelName: candidates[0].UpstreamModelName,
			Source: model.PriceQuoteSourceSiteExact, Unit: model.PriceUnitPerRequest,
			Currency: "USD", PerRequest: 0.001, GroupMultiplier: 1,
			ExchangeRateToUSD: 1, ObservedAt: time.Now(),
		},
		{
			RouteCandidateID: &candidates[1].ID,
			SiteID:           site.ID, SiteAccountID: &account.ID,
			GroupKey: model.SiteDefaultGroupKey, ModelName: candidates[1].UpstreamModelName,
			Source: model.PriceQuoteSourceSiteExact, Unit: model.PriceUnitPerMillionTokens,
			Currency: "USD", Input: 1, Output: 1, GroupMultiplier: 1,
			ExchangeRateToUSD: 1, ObservedAt: time.Now(),
		},
		{
			RouteCandidateID: &candidates[2].ID,
			SiteID:           site.ID, SiteAccountID: &account.ID,
			GroupKey: model.SiteDefaultGroupKey, ModelName: candidates[2].UpstreamModelName,
			Source: model.PriceQuoteSourceSiteExact, Unit: model.PriceUnitSiteCredit,
			Currency: "CREDIT", Input: 5, Output: 5, GroupMultiplier: 1,
			ExchangeRateToUSD: 0.1, ObservedAt: time.Now(),
		},
	}
	if err := SiteModelPriceQuotesUpsert(ctx, quotes); err != nil {
		t.Fatalf("create quotes: %v", err)
	}
	if err := catalogRefreshCache(ctx); err != nil {
		t.Fatalf("refresh catalog cache: %v", err)
	}
	group := model.Group{
		Name: "lowest-cost-model",
		Mode: model.GroupModeRandom,
		Items: []model.GroupItem{
			{ChannelID: channels[0].ID, ModelName: candidates[0].UpstreamModelName, Priority: 1, Weight: 1},
			{ChannelID: channels[1].ID, ModelName: candidates[1].UpstreamModelName, Priority: 1, Weight: 1},
			{ChannelID: channels[2].ID, ModelName: candidates[2].UpstreamModelName, Priority: 1, Weight: 1},
		},
	}
	planned, _, _, err := CatalogPlanGroup(ctx, group.Name, model.ProtocolRouteRequirements{
		InboundProtocol: model.ProtocolOpenAIChat,
	}, group)
	if err != nil {
		t.Fatalf("CatalogPlanGroup: %v", err)
	}
	if len(planned.Items) != 3 || planned.Items[0].ChannelID != channels[2].ID {
		t.Fatalf("lowest-cost order ignored FX or selected per-request shortcut: %+v", planned.Items)
	}
}

func TestCatalogPlanUsesRouteCandidateScopedHealth(t *testing.T) {
	ctx := setupBackupTestDB(t)
	channel := model.Channel{Name: "candidate-health-shared-channel", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create shared channel: %v", err)
	}
	canonical := model.CanonicalModel{
		Name: "candidate-health-model", NormalizedName: "candidate-health-model",
		RoutingStrategy: model.RoutingStrategyReliability,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	mustCreatePricingRow(t, ctx, &canonical)
	candidates := []model.RouteCandidate{
		{
			CanonicalModelID: canonical.ID, ChannelID: channel.ID,
			UpstreamModelName: "unhealthy-upstream", Status: model.RouteCandidateActive,
			Priority: 1, Weight: 1, LastSeenAt: time.Now(),
		},
		{
			CanonicalModelID: canonical.ID, ChannelID: channel.ID,
			UpstreamModelName: "healthy-upstream", Status: model.RouteCandidateActive,
			Priority: 9, Weight: 1, LastSeenAt: time.Now(),
		},
	}
	mustCreatePricingRow(t, ctx, &candidates)
	now := time.Now()
	facts := make([]model.UsageAttemptFact, 0, 10)
	for i := 0; i < 5; i++ {
		facts = append(facts,
			model.UsageAttemptFact{
				RelayLogID: int64(100 + i), AttemptNumber: 1, Time: now.Unix(),
				RouteCandidateID: candidates[0].ID, Status: model.AttemptFailed,
				Outcome: model.RequestOutcomeFailed, Attribution: model.AttemptAttributionUpstream,
				DurationMS: 900, TokenSource: model.UsageValueSourceUnknown,
			},
			model.UsageAttemptFact{
				RelayLogID: int64(200 + i), AttemptNumber: 1, Time: now.Unix(),
				RouteCandidateID: candidates[1].ID, Status: model.AttemptSuccess,
				Outcome: model.RequestOutcomeSuccess, DurationMS: 100,
				TokenSource: model.UsageValueSourceUnknown,
			},
		)
	}
	mustCreatePricingRow(t, ctx, &facts)
	if err := catalogRefreshCache(ctx); err != nil {
		t.Fatalf("refresh catalog cache: %v", err)
	}
	group := model.Group{
		Name: canonical.Name,
		Items: []model.GroupItem{
			{ChannelID: channel.ID, ModelName: candidates[0].UpstreamModelName, Priority: 1, Weight: 1},
			{ChannelID: channel.ID, ModelName: candidates[1].UpstreamModelName, Priority: 9, Weight: 1},
		},
	}
	planned, _, _, err := CatalogPlanGroup(
		ctx,
		canonical.Name,
		model.ProtocolRouteRequirements{InboundProtocol: model.ProtocolOpenAIChat},
		group,
	)
	if err != nil {
		t.Fatalf("plan candidate health routing: %v", err)
	}
	if len(planned.Items) != 2 || planned.Items[0].ModelName != "healthy-upstream" {
		t.Fatalf("candidate-scoped health did not override shared channel priority: %+v", planned.Items)
	}
}

func TestRouteCandidateHealthRefreshDegradesAndRecoversAutomaticCandidates(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "health-refresh")
	fixture.candidate.Manual = false
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.RouteCandidate{}).
		Where("id = ?", fixture.candidate.ID).
		UpdateColumn("manual", false).Error; err != nil {
		t.Fatalf("make candidate automatic: %v", err)
	}
	now := time.Now()
	createCandidateHealthFacts(t, ctx, fixture.candidate.ID, now, model.AttemptFailed)
	if updated, err := RouteCandidateHealthRefresh(ctx, now, 5); err != nil || updated != 1 {
		t.Fatalf("degrade candidate: updated=%d err=%v", updated, err)
	}
	var candidate model.RouteCandidate
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, fixture.candidate.ID).Error; err != nil {
		t.Fatalf("reload degraded candidate: %v", err)
	}
	if candidate.Status != model.RouteCandidateDegraded {
		t.Fatalf("candidate was not degraded: %+v", candidate)
	}

	if err := dbpkg.GetDB().WithContext(ctx).
		Where("route_candidate_id = ?", fixture.candidate.ID).
		Delete(&model.UsageAttemptFact{}).Error; err != nil {
		t.Fatalf("clear failed health facts: %v", err)
	}
	createCandidateHealthFacts(t, ctx, fixture.candidate.ID, now, model.AttemptSuccess)
	if updated, err := RouteCandidateHealthRefresh(ctx, now, 5); err != nil || updated != 1 {
		t.Fatalf("recover candidate: updated=%d err=%v", updated, err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, fixture.candidate.ID).Error; err != nil {
		t.Fatalf("reload recovered candidate: %v", err)
	}
	if candidate.Status != model.RouteCandidateActive {
		t.Fatalf("candidate was not recovered: %+v", candidate)
	}
}

func TestGroupDeleteRetiresAutomaticCandidatesAndPreservesManualCandidates(t *testing.T) {
	ctx := setupBackupTestDB(t)
	useAutoCatalogProvisioning(t)
	channel := model.Channel{Name: "catalog-group-delete", Model: "group-delete-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		t.Fatalf("CatalogSync: %v", err)
	}
	group, err := GroupGetEnabledMap("group-delete-model", ctx)
	if err != nil {
		t.Fatalf("load group: %v", err)
	}
	automatic := findCatalogCandidate(t, channel.ID, "group-delete-model")
	manualChannel := model.Channel{Name: "catalog-group-delete-manual", Enabled: true}
	if err := ChannelCreate(&manualChannel, ctx); err != nil {
		t.Fatalf("create manual channel: %v", err)
	}
	manual := model.RouteCandidate{
		CanonicalModelID:  automatic.CanonicalModelID,
		ChannelID:         manualChannel.ID,
		UpstreamModelName: "manual-route",
		Status:            model.RouteCandidateDegraded,
		Priority:          1,
		Weight:            1,
		Manual:            true,
		LastSeenAt:        time.Now(),
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&manual).Error; err != nil {
		t.Fatalf("create manual candidate: %v", err)
	}

	if err := GroupDel(group.ID, ctx); err != nil {
		t.Fatalf("GroupDel: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&automatic, automatic.ID).Error; err != nil {
		t.Fatalf("reload automatic candidate: %v", err)
	}
	if automatic.Status != model.RouteCandidateUnavailable || automatic.UnavailableSince == nil {
		t.Fatalf("automatic candidate was not retired with group: %+v", automatic)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&manual, manual.ID).Error; err != nil {
		t.Fatalf("reload manual candidate: %v", err)
	}
	if manual.Status != model.RouteCandidateDegraded || !manual.Manual {
		t.Fatalf("group deletion overwrote manual candidate: %+v", manual)
	}
}

func createCandidateHealthFacts(
	t *testing.T,
	ctx context.Context,
	candidateID int,
	now time.Time,
	status model.AttemptStatus,
) {
	t.Helper()
	facts := make([]model.UsageAttemptFact, 5)
	for i := range facts {
		outcome := model.RequestOutcomeSuccess
		attribution := model.AttemptAttributionNone
		if status == model.AttemptFailed {
			outcome = model.RequestOutcomeFailed
			attribution = model.AttemptAttributionUpstream
		}
		facts[i] = model.UsageAttemptFact{
			RelayLogID: int64(1000 + candidateID*10 + i), AttemptNumber: 1,
			Time: now.Unix(), RouteCandidateID: candidateID,
			Status: status, Outcome: outcome, Attribution: attribution,
			DurationMS: 100, TokenSource: model.UsageValueSourceUnknown,
		}
	}
	mustCreatePricingRow(t, ctx, &facts)
}

func assertRouteCandidateProjection(
	t *testing.T,
	canonicalID int,
	item model.GroupItem,
) {
	t.Helper()
	var candidate model.RouteCandidate
	if err := dbpkg.GetDB().
		Where(
			"canonical_model_id = ? AND channel_id = ? AND upstream_model_name = ?",
			canonicalID,
			item.ChannelID,
			item.ModelName,
		).
		First(&candidate).Error; err != nil {
		t.Fatalf("route candidate was not projected: %v", err)
	}
	if candidate.Priority != item.Priority || candidate.Weight != max(item.Weight, 1) {
		t.Fatalf("candidate lost priority/weight: candidate=%+v item=%+v", candidate, item)
	}
}

func assertProjectedCandidateUnavailable(
	t *testing.T,
	canonicalID int,
	channelID int,
	upstreamModel string,
) {
	t.Helper()
	var candidate model.RouteCandidate
	if err := dbpkg.GetDB().
		Where(
			"canonical_model_id = ? AND channel_id = ? AND upstream_model_name = ?",
			canonicalID,
			channelID,
			upstreamModel,
		).
		First(&candidate).Error; err != nil {
		t.Fatalf("load retired route candidate: %v", err)
	}
	if candidate.Manual ||
		candidate.Status != model.RouteCandidateUnavailable ||
		candidate.UnavailableSince == nil {
		t.Fatalf("projected route candidate was not marked unavailable: %+v", candidate)
	}
}

func createCatalogManagedFixture(
	t *testing.T,
	ctx context.Context,
	authoritative bool,
) (model.Site, model.SiteAccount, model.Channel) {
	t.Helper()
	site := model.Site{
		Name: "catalog-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://catalog.example.com", Enabled: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site failed: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "catalog-account",
		CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "catalog-key", Enabled: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account failed: %v", err)
	}
	group := model.SiteUserGroup{
		SiteAccountID:          account.ID,
		GroupKey:               model.SiteDefaultGroupKey,
		Name:                   model.SiteDefaultGroupName,
		ModelSyncStatus:        model.SiteGroupModelSyncStatusSynced,
		ModelSyncAuthoritative: authoritative,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
		t.Fatalf("create site group failed: %v", err)
	}
	channel := model.Channel{Name: "catalog-managed", Model: "catalog-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}
	binding := model.SiteChannelBinding{
		SiteID: site.ID, SiteAccountID: account.ID, SiteUserGroupID: &group.ID,
		GroupKey: model.SiteDefaultGroupKey, ChannelID: channel.ID,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}
	return site, account, channel
}

func findCatalogCandidate(t *testing.T, channelID int, upstreamModel string) model.RouteCandidate {
	t.Helper()
	var candidate model.RouteCandidate
	if err := dbpkg.GetDB().
		Where("channel_id = ? AND upstream_model_name = ?", channelID, upstreamModel).
		First(&candidate).Error; err != nil {
		t.Fatalf("find route candidate failed: %v", err)
	}
	return candidate
}

func TestCatalogPlanGroupSortsByReserveBalanceAndRate(t *testing.T) {
	ctx := setupBackupTestDB(t)
	site := model.Site{
		Name: "sort-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://sort.example.com", Enabled: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site failed: %v", err)
	}
	accountBalances := []float64{50, 10, 30, 5}
	accounts := make([]model.SiteAccount, len(accountBalances))
	for index, balance := range accountBalances {
		accounts[index] = model.SiteAccount{
			SiteID: site.ID, Name: fmt.Sprintf("sort-account-%d", index),
			CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: fmt.Sprintf("sort-token-%d", index),
			Enabled: true, Balance: balance,
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&accounts[index]).Error; err != nil {
			t.Fatalf("create account %d failed: %v", index, err)
		}
	}
	canonical := model.CanonicalModel{
		Name:            "sort-model",
		NormalizedName:  "sort-model",
		RoutingStrategy: model.RoutingStrategyManual,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&canonical).Error; err != nil {
		t.Fatalf("create canonical failed: %v", err)
	}
	channelIsReserve := []bool{false, false, true, true}
	upstreamNames := []string{"sort-upstream-a", "sort-upstream-b", "sort-upstream-c", "sort-upstream-d"}
	ratePerQuote := []float64{2, 1, 3, 0.5}
	channels := make([]model.Channel, 4)
	candidates := make([]model.RouteCandidate, 4)
	for index := range channels {
		channels[index] = model.Channel{
			Name: fmt.Sprintf("sort-channel-%d", index), Model: upstreamNames[index],
			Enabled: true, IsReserve: channelIsReserve[index],
		}
		if err := ChannelCreate(&channels[index], ctx); err != nil {
			t.Fatalf("create channel %d failed: %v", index, err)
		}
		binding := model.SiteChannelBinding{
			SiteID: site.ID, SiteAccountID: accounts[index].ID,
			GroupKey: model.SiteDefaultGroupKey, ChannelID: channels[index].ID,
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
			t.Fatalf("create binding %d failed: %v", index, err)
		}
		candidates[index] = model.RouteCandidate{
			CanonicalModelID: canonical.ID, ChannelID: channels[index].ID,
			UpstreamModelName: upstreamNames[index],
			SiteID:            &site.ID, SiteAccountID: &accounts[index].ID,
			SiteGroupKey: model.SiteDefaultGroupKey, Status: model.RouteCandidateActive,
			Priority: 1, Weight: 1, LastSeenAt: time.Now(),
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&candidates[index]).Error; err != nil {
			t.Fatalf("create candidate %d failed: %v", index, err)
		}
		quotes := []model.SiteModelPriceQuote{{
			RouteCandidateID: &candidates[index].ID,
			SiteID:           site.ID, SiteAccountID: &accounts[index].ID,
			GroupKey: model.SiteDefaultGroupKey, ModelName: upstreamNames[index],
			Source: model.PriceQuoteSourceSiteExact, Unit: model.PriceUnitPerMillionTokens,
			Currency: "USD", Input: ratePerQuote[index] / 2, Output: ratePerQuote[index] / 2,
			GroupMultiplier: 1, ExchangeRateToUSD: 1, ObservedAt: time.Now(),
		}}
		if err := SiteModelPriceQuotesUpsert(ctx, quotes); err != nil {
			t.Fatalf("create quote %d failed: %v", index, err)
		}
	}
	if err := catalogRefreshCache(ctx); err != nil {
		t.Fatalf("refresh catalog cache failed: %v", err)
	}
	group := model.Group{
		Name: "sort-model",
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: channels[3].ID, ModelName: upstreamNames[3], Priority: 1, Weight: 1},
			{ChannelID: channels[0].ID, ModelName: upstreamNames[0], Priority: 1, Weight: 1},
			{ChannelID: channels[2].ID, ModelName: upstreamNames[2], Priority: 1, Weight: 1},
			{ChannelID: channels[1].ID, ModelName: upstreamNames[1], Priority: 1, Weight: 1},
		},
	}
	planned, _, _, err := CatalogPlanGroup(ctx, group.Name, model.ProtocolRouteRequirements{
		InboundProtocol: model.ProtocolOpenAIChat,
	}, group)
	if err != nil {
		t.Fatalf("CatalogPlanGroup failed: %v", err)
	}
	got := make([]int, 0, len(planned.Items))
	for _, item := range planned.Items {
		got = append(got, item.ChannelID)
	}
	want := []int{channels[0].ID, channels[1].ID, channels[3].ID, channels[2].ID}
	if !slices.Equal(got, want) {
		t.Fatalf("sort order mismatch: got %v, want %v", got, want)
	}
}

func TestCatalogPlanGroupTierFallsBackToSiteReserve(t *testing.T) {
	ctx := setupBackupTestDB(t)
	site := model.Site{
		Name: "reserve-fallback-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://reserve-fallback.example.com", Enabled: true, IsReserve: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site failed: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "reserve-fallback-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "reserve-fallback-token",
		Enabled: true, Balance: 100,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account failed: %v", err)
	}
	canonical := model.CanonicalModel{
		Name:            "reserve-fallback-model",
		NormalizedName:  "reserve-fallback-model",
		RoutingStrategy: model.RoutingStrategyManual,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&canonical).Error; err != nil {
		t.Fatalf("create canonical failed: %v", err)
	}
	normal := model.Channel{Name: "reserve-fallback-normal", Model: "fallback-normal-upstream", Enabled: true}
	if err := ChannelCreate(&normal, ctx); err != nil {
		t.Fatalf("create normal channel failed: %v", err)
	}
	relay := model.Channel{Name: "reserve-fallback-relay", Model: "fallback-relay-upstream", Enabled: true}
	if err := ChannelCreate(&relay, ctx); err != nil {
		t.Fatalf("create relay channel failed: %v", err)
	}
	candidates := []model.RouteCandidate{
		{
			CanonicalModelID: canonical.ID, ChannelID: normal.ID,
			UpstreamModelName: "fallback-normal-upstream", Status: model.RouteCandidateActive,
			Priority: 1, Weight: 1, LastSeenAt: time.Now(),
		},
		{
			CanonicalModelID: canonical.ID, ChannelID: relay.ID,
			UpstreamModelName: "fallback-relay-upstream", Status: model.RouteCandidateActive,
			SiteID: &site.ID, SiteAccountID: &account.ID, SiteGroupKey: model.SiteDefaultGroupKey,
			Priority: 1, Weight: 1, LastSeenAt: time.Now(),
		},
	}
	for index := range candidates {
		if err := dbpkg.GetDB().WithContext(ctx).Create(&candidates[index]).Error; err != nil {
			t.Fatalf("create candidate %d failed: %v", index, err)
		}
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.SiteChannelBinding{
		SiteID: site.ID, SiteAccountID: account.ID,
		GroupKey: model.SiteDefaultGroupKey, ChannelID: relay.ID,
	}).Error; err != nil {
		t.Fatalf("create binding failed: %v", err)
	}
	if err := catalogRefreshCache(ctx); err != nil {
		t.Fatalf("refresh catalog cache failed: %v", err)
	}
	group := model.Group{
		Name: "reserve-fallback-model",
		Mode: model.GroupModeFailover,
		Items: []model.GroupItem{
			{ChannelID: relay.ID, ModelName: "fallback-relay-upstream", Priority: 1, Weight: 1},
			{ChannelID: normal.ID, ModelName: "fallback-normal-upstream", Priority: 1, Weight: 1},
		},
	}
	planned, _, _, err := CatalogPlanGroup(ctx, group.Name, model.ProtocolRouteRequirements{
		InboundProtocol: model.ProtocolOpenAIChat,
	}, group)
	if err != nil {
		t.Fatalf("CatalogPlanGroup failed: %v", err)
	}
	if len(planned.Items) != 2 || planned.Items[0].ChannelID != normal.ID || planned.Items[1].ChannelID != relay.ID {
		t.Fatalf("fallback reserve tier not applied: got %+v", planned.Items)
	}
}
