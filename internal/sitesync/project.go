package sitesync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

func ProjectAccount(ctx context.Context, accountID int) ([]int, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	if !siteRecord.Enabled || !account.Enabled {
		bindings, err := listChannelBindingsByAccount(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		channelIDs := make([]int, 0, len(bindings))
		for _, binding := range bindings {
			channelIDs = append(channelIDs, binding.ChannelID)
			if err := op.ChannelEnabledManaged(binding.ChannelID, false, ctx); err != nil {
				log.Warnf("failed to disable managed channel %d: %v", binding.ChannelID, err)
			}
		}
		return channelIDs, nil
	}

	groupMap := make(map[string]model.SiteUserGroup)
	for _, item := range account.UserGroups {
		key := model.NormalizeSiteGroupKey(item.GroupKey)
		item.GroupKey = key
		item.Name = model.NormalizeSiteGroupName(key, item.Name)
		groupMap[key] = item
	}
	if len(groupMap) == 0 {
		groupMap[model.SiteDefaultGroupKey] = model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}
	}

	tokenGroups := make(map[string][]model.SiteToken)
	for _, token := range account.Tokens {
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		token.GroupKey = groupKey
		token.GroupName = model.NormalizeSiteGroupName(groupKey, token.GroupName)
		tokenGroups[groupKey] = append(tokenGroups[groupKey], token)
		if _, ok := groupMap[groupKey]; !ok {
			groupMap[groupKey] = model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: groupKey, Name: model.NormalizeSiteGroupName(groupKey, token.GroupName)}
		}
	}

	modelsByGroup := make(map[string][]model.SiteModel)
	for _, item := range account.Models {
		name := strings.TrimSpace(item.ModelName)
		if name == "" {
			continue
		}
		if item.Disabled {
			continue
		}
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		group, ok := groupMap[groupKey]
		if !ok || !isSiteGroupProjectionActive(siteRecord, account, group, tokenGroups[groupKey]) {
			continue
		}
		item.GroupKey = groupKey
		item.ModelName = name
		if !siteModelBelongsToProjectedGroup(item, groupKey) {
			continue
		}
		if strings.TrimSpace(string(item.RouteType)) == "" {
			item.RouteType = model.InferSiteModelRouteType(item.ModelName)
		} else {
			item.RouteType = model.NormalizeSiteModelRouteType(item.RouteType)
		}
		modelsByGroup[groupKey] = append(modelsByGroup[groupKey], item)
	}
	for groupKey, items := range modelsByGroup {
		modelsByGroup[groupKey] = compactSiteModels(items)
	}
	if err := syncProjectedModelPrices(ctx, modelsByGroup); err != nil {
		log.Warnf("failed to sync projected model prices (account=%d): %v", account.ID, err)
	}

	existingBindings, err := listChannelBindingsByAccount(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	bindingMap := make(map[string]model.SiteChannelBinding, len(existingBindings))
	for _, binding := range existingBindings {
		bindingMap[model.NormalizeSiteGroupKey(binding.GroupKey)] = binding
	}

	desiredKeys := make([]string, 0, len(groupMap))
	for groupKey, group := range groupMap {
		if isSiteGroupProjectionActive(siteRecord, account, group, tokenGroups[groupKey]) {
			desiredKeys = append(desiredKeys, groupKey)
		}
	}
	slices.Sort(desiredKeys)

	managedChannelIDs := make([]int, 0, len(desiredKeys))
	shouldSplit := shouldSplitForAccount(account, siteRecord)
	bindingChannelByKey := make(map[string]int)

	for _, groupKey := range desiredKeys {
		group := groupMap[groupKey]
		groupTokens := tokenGroups[groupKey]
		groupModels := modelsByGroup[groupKey]
		modelBuckets := partitionSiteModelsByRouteType(groupModels, shouldSplit, siteRecord)
		proxyMode, proxyConfigID := resolveSiteAccountProxy(siteRecord, account)
		enabled := siteRecord.Enabled && account.Enabled && hasUsableToken(groupTokens)
		for routeType, bucketModels := range modelBuckets {
			if len(bucketModels) == 0 {
				continue
			}
			obType := routeType.ToOutboundType()
			baseUrls := []model.BaseUrl{{URL: resolveProjectedChannelBaseURL(siteRecord, routeType), Delay: 0}}
			modelNames := extractSiteModelNames(bucketModels)
			bindingKey := compositeBindingKey(groupKey, obType, shouldSplit)
			channelPayload := model.Channel{
				Name:          buildManagedChannelName(siteRecord, account, group, obType),
				Type:          obType,
				Enabled:       enabled,
				BaseUrls:      baseUrls,
				Keys:          buildChannelKeys(groupTokens, siteRecord.Platform),
				Model:         strings.Join(modelNames, ","),
				CustomModel:   "",
				ProxyMode:     proxyMode,
				ProxyConfigID: proxyConfigID,
				Proxy:         proxyMode != model.ProxyUsageModeDirect,
				AutoSync:      false,
				AutoGroup:     model.AutoGroupTypeNone,
				CustomHeader:  siteRecord.CustomHeader,
			}

			binding, exists := bindingMap[bindingKey]
			if !exists {
				reusedBinding, reused, err := reuseManagedChannelByName(ctx, siteRecord, account, group, bindingKey, channelPayload)
				if err != nil {
					return nil, err
				}
				if reused {
					binding = *reusedBinding
					bindingMap[bindingKey] = binding
					exists = true
				}
			}
			if !exists {
				if err := op.ChannelCreate(&channelPayload, ctx); err != nil {
					return nil, fmt.Errorf("failed to create managed channel: %w", err)
				}
				binding = model.SiteChannelBinding{SiteID: siteRecord.ID, SiteAccountID: account.ID, GroupKey: bindingKey, ChannelID: channelPayload.ID}
				if group.ID != 0 {
					binding.SiteUserGroupID = &group.ID
				}
				if err := db.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
					return nil, fmt.Errorf("failed to create site channel binding: %w", err)
				}
				bindingMap[bindingKey] = binding
				bindingChannelByKey[bindingKey] = channelPayload.ID
				managedChannelIDs = append(managedChannelIDs, channelPayload.ID)
				if effective := op.EffectiveProjectedChannelAutoGroup(channelPayload); effective != model.AutoGroupTypeNone {
					op.ChannelAutoGroupWithMode(&channelPayload, effective, ctx)
				}
				continue
			}

			existingChannel, err := op.ChannelGet(binding.ChannelID, ctx)
			if err != nil {
				if err := db.GetDB().WithContext(ctx).Delete(&binding).Error; err != nil {
					return nil, fmt.Errorf("failed to delete broken site channel binding: %w", err)
				}
				if err := op.ChannelCreate(&channelPayload, ctx); err != nil {
					return nil, fmt.Errorf("failed to recreate managed channel: %w", err)
				}
				binding.ChannelID = channelPayload.ID
				if group.ID != 0 {
					binding.SiteUserGroupID = &group.ID
				} else {
					binding.SiteUserGroupID = nil
				}
				if err := db.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
					return nil, fmt.Errorf("failed to recreate site channel binding: %w", err)
				}
				bindingChannelByKey[bindingKey] = channelPayload.ID
				managedChannelIDs = append(managedChannelIDs, channelPayload.ID)
				if effective := op.EffectiveProjectedChannelAutoGroup(channelPayload); effective != model.AutoGroupTypeNone {
					op.ChannelAutoGroupWithMode(&channelPayload, effective, ctx)
				}
				continue
			}

			updateReq := &model.ChannelUpdateRequest{ID: existingChannel.ID, Name: &channelPayload.Name, Type: &channelPayload.Type, Enabled: &channelPayload.Enabled, BaseUrls: &channelPayload.BaseUrls, Model: &channelPayload.Model, CustomModel: &channelPayload.CustomModel, ProxyMode: &channelPayload.ProxyMode, ProxyConfigID: channelPayload.ProxyConfigID, AutoSync: &channelPayload.AutoSync, CustomHeader: &channelPayload.CustomHeader, BypassManagedCheck: true}
			updateReq.KeysToAdd, updateReq.KeysToUpdate, updateReq.KeysToDelete = diffManagedChannelKeys(existingChannel.Keys, channelPayload.Keys)
			if _, err := op.ChannelUpdate(updateReq, ctx); err != nil {
				return nil, fmt.Errorf("failed to update managed channel: %w", err)
			}
			updateBinding := map[string]any{"group_key": bindingKey}
			if group.ID != 0 {
				updateBinding["site_user_group_id"] = group.ID
			} else {
				updateBinding["site_user_group_id"] = nil
			}
			if err := db.GetDB().WithContext(ctx).Model(&model.SiteChannelBinding{}).Where("id = ?", binding.ID).Updates(updateBinding).Error; err != nil {
				return nil, fmt.Errorf("failed to update site channel binding: %w", err)
			}
			bindingChannelByKey[bindingKey] = existingChannel.ID
			managedChannelIDs = append(managedChannelIDs, existingChannel.ID)
			updatedChannel, err := op.ChannelGet(existingChannel.ID, ctx)
			if err != nil {
				return nil, err
			}
			if effective := op.EffectiveProjectedChannelAutoGroup(*updatedChannel); effective != model.AutoGroupTypeNone {
				op.ChannelAutoGroupWithMode(updatedChannel, effective, ctx)
			}
		}
	}

	desiredSet := make(map[string]struct{})
	for _, groupKey := range desiredKeys {
		modelBuckets := partitionSiteModelsByRouteType(modelsByGroup[groupKey], shouldSplit, siteRecord)
		for routeType, bucketModels := range modelBuckets {
			if len(bucketModels) == 0 {
				continue
			}
			obType := routeType.ToOutboundType()
			desiredSet[compositeBindingKey(groupKey, obType, shouldSplit)] = struct{}{}
		}
	}
	if err := rewriteManagedGroupItemsForAccount(ctx, siteRecord, account, shouldSplit, groupMap, tokenGroups, account.Models, bindingChannelByKey); err != nil {
		return nil, err
	}
	for _, binding := range existingBindings {
		bindingKey := model.NormalizeSiteGroupKey(binding.GroupKey)
		if _, ok := desiredSet[bindingKey]; ok {
			continue
		}
		baseGroupKey, _ := parseCompositeBindingKey(bindingKey)
		if group, ok := groupMap[baseGroupKey]; ok && isSiteGroupProjectionSystemPaused(group) {
			if err := updateSiteChannelBindingGroup(ctx, binding.ID, group); err != nil {
				return nil, err
			}
			if err := op.ChannelEnabledManaged(binding.ChannelID, false, ctx); err != nil {
				log.Warnf("failed to disable system-paused managed channel %d: %v", binding.ChannelID, err)
			}
			continue
		}
		if err := op.ChannelDelManaged(binding.ChannelID, ctx); err != nil {
			log.Warnf("failed to delete stale managed channel %d: %v", binding.ChannelID, err)
		}
		if err := db.GetDB().WithContext(ctx).Delete(&binding).Error; err != nil {
			return nil, fmt.Errorf("failed to delete stale site channel binding: %w", err)
		}
	}

	// POR 覆盖：本次同步可能把数据面已退役的渠道重新 enabled，按退役状态压回 disabled。
	// 仅作用于 managedChannelIDs（本轮实际投影/启用的渠道），不影响上面已清理删除的过时绑定。
	for _, channelID := range managedChannelIDs {
		retired, err := op.SiteChannelOutlierIsRetired(channelID, ctx)
		if err != nil {
			log.Warnf("POR retired check failed for channel %d: %v", channelID, err)
			continue
		}
		if retired {
			if err := op.ChannelEnabledManaged(channelID, false, ctx); err != nil {
				log.Warnf("POR keep retired channel %d disabled failed: %v", channelID, err)
			}
		}
	}

	return managedChannelIDs, nil
}

func isSiteGroupProjectionActive(siteRecord *model.Site, account *model.SiteAccount, group model.SiteUserGroup, tokens []model.SiteToken) bool {
	if siteRecord == nil || account == nil {
		return false
	}
	if !siteRecord.Enabled || !account.Enabled {
		return false
	}
	if !hasUsableToken(tokens) {
		return false
	}
	if group.ProjectionDisabled || group.ProjectionSuspended {
		return false
	}
	switch group.ModelSyncStatus {
	case "", model.SiteGroupModelSyncStatusIdle,
		model.SiteGroupModelSyncStatusSynced,
		model.SiteGroupModelSyncStatusStale,
		model.SiteGroupModelSyncStatusFailed,
		model.SiteGroupModelSyncStatusUnresolved:
		return true
	default:
		return false
	}
}

func isSiteGroupProjectionSystemPaused(group model.SiteUserGroup) bool {
	if group.ProjectionDisabled {
		return false
	}
	if group.ProjectionSuspended {
		return true
	}
	switch group.ModelSyncStatus {
	case model.SiteGroupModelSyncStatusEmpty, model.SiteGroupModelSyncStatusMissingKey:
		return true
	default:
		return false
	}
}

func updateSiteChannelBindingGroup(ctx context.Context, bindingID int, group model.SiteUserGroup) error {
	updates := map[string]any{}
	if group.ID != 0 {
		updates["site_user_group_id"] = group.ID
	} else {
		updates["site_user_group_id"] = nil
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.SiteChannelBinding{}).Where("id = ?", bindingID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update paused site channel binding: %w", err)
	}
	return nil
}

func ProjectSite(ctx context.Context, siteID int) error {
	siteRecord, err := op.SiteGet(siteID, ctx)
	if err != nil {
		return err
	}
	for _, account := range siteRecord.Accounts {
		if _, err := ProjectAccount(ctx, account.ID); err != nil {
			return err
		}
	}
	return nil
}

func buildManagedChannelName(siteRecord *model.Site, account *model.SiteAccount, group model.SiteUserGroup, obType outbound.OutboundType) string {
	groupName := model.NormalizeSiteGroupName(group.GroupKey, group.Name)
	formatName := model.CompactSiteModelRouteTypeName(model.SiteModelRouteTypeFromOutboundType(obType))
	return fmt.Sprintf("%s/%s/%s-%s", siteRecord.Name, account.Name, groupName, formatName)
}

func reuseManagedChannelByName(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, group model.SiteUserGroup, bindingKey string, channelPayload model.Channel) (*model.SiteChannelBinding, bool, error) {
	existingChannel, err := op.ChannelGetByName(channelPayload.Name, ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to lookup managed channel by name: %w", err)
	}

	binding, managed, err := op.ChannelManagedBinding(existingChannel.ID, ctx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to inspect existing managed channel binding: %w", err)
	}
	if managed {
		if binding.SiteID != siteRecord.ID || binding.SiteAccountID != account.ID {
			return nil, false, fmt.Errorf("managed channel name %q is already bound to another site account", channelPayload.Name)
		}
		return binding, true, nil
	}

	reusedBinding := model.SiteChannelBinding{
		SiteID:        siteRecord.ID,
		SiteAccountID: account.ID,
		GroupKey:      bindingKey,
		ChannelID:     existingChannel.ID,
	}
	if group.ID != 0 {
		reusedBinding.SiteUserGroupID = &group.ID
	}
	if err := db.GetDB().WithContext(ctx).Create(&reusedBinding).Error; err != nil {
		return nil, false, fmt.Errorf("failed to bind existing channel %q as managed channel: %w", channelPayload.Name, err)
	}
	return &reusedBinding, true, nil
}

func buildProjectedChannelBaseURL(siteRecord *model.Site) string {
	if siteRecord == nil {
		return ""
	}

	baseURL := strings.TrimRight(strings.TrimSpace(siteRecord.BaseURL), "/")
	if baseURL == "" {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

// resolveProjectedChannelBaseURL returns the base URL for a projected channel
// of the given route type. A per-route override on the site (RouteBaseURLs)
// wins and is used verbatim; otherwise the default site base URL handling
// (with the "/v1" convention) applies. This lets one upstream expose different
// protocols under different path prefixes.
func resolveProjectedChannelBaseURL(siteRecord *model.Site, routeType model.SiteModelRouteType) string {
	if override, ok := siteRecord.ResolveRouteBaseURL(routeType); ok {
		return override
	}
	return buildProjectedChannelBaseURL(siteRecord)
}

// isUsableSiteToken reports whether a token can produce a projected channel
// key: it must be ready, unmasked, and carry a non-empty normalized value.
func isUsableSiteToken(token model.SiteToken) bool {
	if !model.IsReadySiteToken(token) || model.IsMaskedSiteTokenValue(token.Token) {
		return false
	}
	return strings.TrimSpace(token.Token) != ""
}

// hasUsableToken reports whether at least one token would yield a channel key,
// keeping projection activation aligned with buildChannelKeys.
func hasUsableToken(tokens []model.SiteToken) bool {
	for _, token := range tokens {
		if isUsableSiteToken(token) {
			return true
		}
	}
	return false
}

func buildChannelKeys(tokens []model.SiteToken, platform model.SitePlatform) []model.ChannelKey {
	keys := make([]model.ChannelKey, 0, len(tokens))
	for _, token := range tokens {
		if !isUsableSiteToken(token) {
			continue
		}
		normalized := model.NormalizeSiteSyncTokenValueForPlatform(platform, token.Token)
		keys = append(keys, model.ChannelKey{Enabled: token.Enabled, ChannelKey: normalized, Remark: model.NormalizeSiteGroupName(token.GroupKey, token.GroupName)})
	}
	return keys
}

func diffManagedChannelKeys(existingKeys []model.ChannelKey, desiredKeys []model.ChannelKey) ([]model.ChannelKeyAddRequest, []model.ChannelKeyUpdateRequest, []int) {
	used := make(map[int]struct{}, len(existingKeys))
	adds := make([]model.ChannelKeyAddRequest, 0)
	updates := make([]model.ChannelKeyUpdateRequest, 0)

	for _, desired := range desiredKeys {
		matchedIndex := -1
		for i, existing := range existingKeys {
			if existing.ChannelKey != desired.ChannelKey {
				continue
			}
			if _, ok := used[existing.ID]; ok {
				continue
			}
			matchedIndex = i
			break
		}
		if matchedIndex == -1 {
			adds = append(adds, model.ChannelKeyAddRequest{
				Enabled:    desired.Enabled,
				ChannelKey: desired.ChannelKey,
				Remark:     desired.Remark,
			})
			continue
		}

		existing := existingKeys[matchedIndex]
		used[existing.ID] = struct{}{}
		update := model.ChannelKeyUpdateRequest{ID: existing.ID}
		if existing.Enabled != desired.Enabled {
			enabled := desired.Enabled
			update.Enabled = &enabled
		}
		if existing.Remark != desired.Remark {
			remark := desired.Remark
			update.Remark = &remark
		}
		if update.Enabled != nil || update.Remark != nil {
			updates = append(updates, update)
		}
	}

	deletes := make([]int, 0)
	for _, existing := range existingKeys {
		if _, ok := used[existing.ID]; ok {
			continue
		}
		deletes = append(deletes, existing.ID)
	}
	return adds, updates, deletes
}

func syncProjectedModelPrices(ctx context.Context, modelsByGroup map[string][]model.SiteModel) error {
	modelNames := make([]string, 0)
	seen := make(map[string]struct{})
	for _, groupModels := range modelsByGroup {
		for _, item := range groupModels {
			modelName := strings.TrimSpace(item.ModelName)
			if modelName == "" {
				continue
			}
			if _, ok := seen[modelName]; ok {
				continue
			}
			seen[modelName] = struct{}{}
			modelNames = append(modelNames, modelName)
		}
	}
	if len(modelNames) == 0 {
		return nil
	}
	return helper.LLMPriceAddToDB(modelNames, ctx)
}

func platformOutboundType(site *model.Site) outbound.OutboundType {
	if site.Platform == model.SitePlatformAPI {
		switch site.ResolveDefaultRouteType() {
		case model.SiteModelRouteTypeAnthropic:
			return outbound.OutboundTypeAnthropic
		case model.SiteModelRouteTypeGemini:
			return outbound.OutboundTypeGemini
		default:
			return outbound.OutboundTypeOpenAIChat
		}
	}
	return outbound.OutboundTypeOpenAIChat
}

// shouldSplitByOutboundType 判断是否需要按模型端点格式拆分 Channel
// 当站点配置了协议路径覆盖时，强制启用拆分以确保每个协议使用正确的 base URL
func shouldSplitByOutboundType(site *model.Site) bool {
	if site != nil && len(site.RouteBaseURLs) > 0 {
		return true
	}
	return model.ShouldSplitSiteChannelRoutes(site.Platform)
}

// classifyModelOutboundType 根据模型名称判断应使用的端点格式
func classifyModelOutboundType(modelName string) outbound.OutboundType {
	lower := strings.ToLower(modelName)
	if strings.HasPrefix(lower, "claude") {
		return outbound.OutboundTypeAnthropic
	}
	if strings.HasPrefix(lower, "gemini") {
		return outbound.OutboundTypeGemini
	}
	return outbound.OutboundTypeOpenAIChat
}

func classifyModelRouteType(modelName string) model.SiteModelRouteType {
	return model.InferSiteModelRouteType(modelName)
}

// partitionModelsByOutboundType 将模型列表按端点格式分桶
func partitionModelsByOutboundType(modelNames []string, split bool, site *model.Site) map[outbound.OutboundType][]string {
	if !split {
		obType := platformOutboundType(site)
		return map[outbound.OutboundType][]string{obType: modelNames}
	}
	buckets := make(map[outbound.OutboundType][]string)
	for _, name := range modelNames {
		obType := classifyModelOutboundType(name)
		buckets[obType] = append(buckets[obType], name)
	}
	return buckets
}

func partitionSiteModelsByRouteType(items []model.SiteModel, split bool, site *model.Site) map[model.SiteModelRouteType][]model.SiteModel {
	if !split {
		routeType := model.SiteModelRouteTypeFromOutboundType(platformOutboundType(site))
		if len(items) == 0 {
			return map[model.SiteModelRouteType][]model.SiteModel{}
		}
		return map[model.SiteModelRouteType][]model.SiteModel{routeType: items}
	}
	buckets := make(map[model.SiteModelRouteType][]model.SiteModel)
	for _, item := range items {
		routeType := model.NormalizeSiteModelRouteType(item.RouteType)
		if !model.IsProjectedSiteModelRouteType(routeType) {
			continue
		}
		buckets[routeType] = append(buckets[routeType], item)
	}
	return buckets
}

func compactSiteModels(items []model.SiteModel) []model.SiteModel {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]model.SiteModel, 0, len(items))
	for _, item := range items {
		key := model.NormalizeSiteGroupKey(item.GroupKey) + "\x00" + strings.TrimSpace(item.ModelName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	slices.SortFunc(result, func(a, b model.SiteModel) int {
		return strings.Compare(a.ModelName, b.ModelName)
	})
	return result
}

func extractSiteModelNames(items []model.SiteModel) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.ModelName)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func siteModelBelongsToProjectedGroup(item model.SiteModel, groupKey string) bool {
	metadata, ok := model.ParseSiteModelRouteMetadata(item.RouteRawPayload)
	if !ok || len(metadata.EnableGroups) == 0 {
		return true
	}
	targetGroupKey := model.NormalizeSiteGroupKey(groupKey)
	for _, explicitGroupKey := range metadata.EnableGroups {
		if model.NormalizeSiteGroupKey(explicitGroupKey) == targetGroupKey {
			return true
		}
	}
	return false
}

// compositeBindingKey 生成复合绑定 key，用于区分同一 tokenGroup 的不同端点格式 Channel
func compositeBindingKey(groupKey string, obType outbound.OutboundType, split bool) string {
	return model.ComposeSiteChannelBindingKey(groupKey, model.SiteModelRouteTypeFromOutboundType(obType), split)
}

func parseCompositeBindingKey(groupKey string) (string, model.SiteModelRouteType) {
	return model.ParseSiteChannelBindingKey(groupKey)
}

func rewriteManagedGroupItemsForAccount(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, split bool, groupMap map[string]model.SiteUserGroup, tokenGroups map[string][]model.SiteToken, accountModels []model.SiteModel, bindingChannelByKey map[string]int) error {
	if account == nil {
		return nil
	}
	accountID := account.ID
	var bindings []model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Where("site_account_id = ?", accountID).Find(&bindings).Error; err != nil {
		return fmt.Errorf("failed to list bindings for group rewrite: %w", err)
	}
	if len(bindings) == 0 {
		return nil
	}
	channelIDs := make([]int, 0, len(bindings))
	for _, binding := range bindings {
		channelIDs = append(channelIDs, binding.ChannelID)
	}
	var items []model.GroupItem
	if err := db.GetDB().WithContext(ctx).Where("channel_id IN ?", channelIDs).Find(&items).Error; err != nil {
		return fmt.Errorf("failed to list group items for rewrite: %w", err)
	}
	if len(items) == 0 {
		return nil
	}
	modelRouteMap := make(map[string]model.SiteModelRouteType)
	activeModelKeys := make(map[string]struct{})
	for _, item := range accountModels {
		if item.Disabled {
			continue
		}
		baseGroupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		group, ok := groupMap[baseGroupKey]
		if !ok || !isSiteGroupProjectionActive(siteRecord, account, group, tokenGroups[baseGroupKey]) {
			continue
		}
		if !siteModelBelongsToProjectedGroup(item, baseGroupKey) {
			continue
		}
		item.GroupKey = baseGroupKey
		item.ModelName = strings.TrimSpace(item.ModelName)
		if item.ModelName == "" {
			continue
		}
		key := model.NormalizeSiteGroupKey(item.GroupKey) + "\x00" + strings.TrimSpace(item.ModelName)
		activeModelKeys[key] = struct{}{}
		routeType := model.NormalizeSiteModelRouteType(item.RouteType)
		if !split || model.IsProjectedSiteModelRouteType(routeType) {
			modelRouteMap[key] = routeType
		}
	}
	affectedGroupIDs := make(map[int]struct{})
	deleteItemIDs := make([]int, 0)
	for _, item := range items {
		var binding *model.SiteChannelBinding
		for i := range bindings {
			if bindings[i].ChannelID == item.ChannelID {
				binding = &bindings[i]
				break
			}
		}
		if binding == nil {
			continue
		}
		baseGroupKey, _ := parseCompositeBindingKey(binding.GroupKey)
		if group, ok := groupMap[baseGroupKey]; ok && isSiteGroupProjectionSystemPaused(group) {
			continue
		}
		modelKey := baseGroupKey + "\x00" + strings.TrimSpace(item.ModelName)
		if _, ok := activeModelKeys[modelKey]; !ok {
			deleteItemIDs = append(deleteItemIDs, item.ID)
			affectedGroupIDs[item.GroupID] = struct{}{}
			continue
		}
		routeType, ok := modelRouteMap[modelKey]
		if !ok {
			deleteItemIDs = append(deleteItemIDs, item.ID)
			affectedGroupIDs[item.GroupID] = struct{}{}
			continue
		}
		targetBindingKey := compositeBindingKey(baseGroupKey, routeType.ToOutboundType(), split)
		targetChannelID, ok := bindingChannelByKey[targetBindingKey]
		if !ok {
			deleteItemIDs = append(deleteItemIDs, item.ID)
			affectedGroupIDs[item.GroupID] = struct{}{}
			continue
		}
		if targetChannelID == item.ChannelID {
			continue
		}
		if err := db.GetDB().WithContext(ctx).Model(&model.GroupItem{}).Where("id = ?", item.ID).Update("channel_id", targetChannelID).Error; err != nil {
			return fmt.Errorf("failed to rewrite group item %d: %w", item.ID, err)
		}
		affectedGroupIDs[item.GroupID] = struct{}{}
	}
	if len(deleteItemIDs) > 0 {
		if err := db.GetDB().WithContext(ctx).Where("id IN ?", deleteItemIDs).Delete(&model.GroupItem{}).Error; err != nil {
			return fmt.Errorf("failed to delete stale group items: %w", err)
		}
	}
	if len(affectedGroupIDs) == 0 {
		return nil
	}
	groupIDs := make([]int, 0, len(affectedGroupIDs))
	for id := range affectedGroupIDs {
		groupIDs = append(groupIDs, id)
	}
	if err := op.GroupRefreshCacheByIDs(groupIDs, ctx); err != nil {
		return fmt.Errorf("failed to refresh group cache after rewrite: %w", err)
	}
	return nil
}

// shouldSplitForAccount 决定是否为账号启用渠道拆分。
// 当检测到账号内有多种手动覆盖的 RouteType 时，自动启用拆分，
// 使得不同端点格式的模型可以分配到不同的投影渠道。
func shouldSplitForAccount(account *model.SiteAccount, site *model.Site) bool {
	// 防御性检查：确保不会因 nil 输入而 panic
	if site == nil || account == nil {
		return false
	}

	// 优先级 1: 站点配置了协议路径覆盖，强制拆分
	if len(site.RouteBaseURLs) > 0 {
		return true
	}

	// 优先级 2: 平台默认策略
	if model.ShouldSplitSiteChannelRoutes(site.Platform) {
		return true
	}

	// 优先级 3: 检测手动覆盖是否与平台默认类型不同
	// 只要有任何手动覆盖与默认不同，或有多种手动覆盖类型，就启用拆分
	siteDefaultRoute := model.SiteModelRouteTypeFromOutboundType(platformOutboundType(site))
	routeTypes := make(map[model.SiteModelRouteType]struct{})
	for _, m := range account.Models {
		if m.Disabled {
			continue // 跳过禁用的模型
		}
		if !m.ManualOverride {
			continue // 跳过自动推断的模型
		}
		rt := model.NormalizeSiteModelRouteType(m.RouteType)
		if !model.IsProjectedSiteModelRouteType(rt) {
			continue // 跳过非投影类型
		}
		// 如果手动覆盖与平台默认不同，需要拆分
		if rt != siteDefaultRoute {
			return true
		}
		routeTypes[rt] = struct{}{}
		if len(routeTypes) > 1 {
			return true // 检测到混合类型，提前返回
		}
	}

	// 所有手动覆盖都与平台默认相同，不需要拆分
	return false
}
