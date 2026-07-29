package op

import (
	"context"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestCatalogSyncPreservesCandidateForNonAuthoritativeManagedGroup(t *testing.T) {
	ctx := setupBackupTestDB(t)
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

func TestChannelDeleteArchivesRouteCandidates(t *testing.T) {
	ctx := setupBackupTestDB(t)
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
