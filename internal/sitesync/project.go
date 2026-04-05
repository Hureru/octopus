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
		groupMap[key] = model.SiteUserGroup{ID: item.ID, SiteAccountID: account.ID, GroupKey: key, Name: model.NormalizeSiteGroupName(key, item.Name), RawPayload: item.RawPayload}
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
	for groupKey := range groupMap {
		if len(tokenGroups[groupKey]) > 0 {
			desiredKeys = append(desiredKeys, groupKey)
		}
	}
	slices.Sort(desiredKeys)

	managedChannelIDs := make([]int, 0, len(desiredKeys))
	shouldSplit := shouldSplitByOutboundType(siteRecord)
	bindingChannelByKey := make(map[string]int)

	for _, groupKey := range desiredKeys {
		group := groupMap[groupKey]
		groupTokens := tokenGroups[groupKey]
		groupModels := modelsByGroup[groupKey]
		modelBuckets := partitionSiteModelsByRouteType(groupModels, shouldSplit, siteRecord.Platform)
		useProxy, proxyURL := resolveSiteAccountProxy(siteRecord, account)
		baseUrls := []model.BaseUrl{{URL: buildProjectedChannelBaseURL(siteRecord), Delay: 0}}
		enabled := siteRecord.Enabled && account.Enabled && len(groupTokens) > 0

		for routeType, bucketModels := range modelBuckets {
			if len(bucketModels) == 0 {
				continue
			}
			obType := routeType.ToOutboundType()
			modelNames := extractSiteModelNames(bucketModels)
			bindingKey := compositeBindingKey(groupKey, obType, shouldSplit)
			channelPayload := model.Channel{
				Name:         buildManagedChannelName(siteRecord, account, group, obType, shouldSplit),
				Type:         obType,
				Enabled:      enabled,
				BaseUrls:     baseUrls,
				Keys:         buildChannelKeys(groupTokens),
				Model:        strings.Join(modelNames, ","),
				CustomModel:  "",
				Proxy:        useProxy,
				AutoSync:     false,
				AutoGroup:    model.AutoGroupTypeNone,
				CustomHeader: siteRecord.CustomHeader,
				ChannelProxy: proxyURL,
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
				continue
			}

			updateReq := &model.ChannelUpdateRequest{ID: existingChannel.ID, Name: &channelPayload.Name, Type: &channelPayload.Type, Enabled: &channelPayload.Enabled, BaseUrls: &channelPayload.BaseUrls, Model: &channelPayload.Model, CustomModel: &channelPayload.CustomModel, Proxy: &channelPayload.Proxy, AutoSync: &channelPayload.AutoSync, AutoGroup: &channelPayload.AutoGroup, CustomHeader: &channelPayload.CustomHeader, ChannelProxy: channelPayload.ChannelProxy, BypassManagedCheck: true}
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
		}
	}

	desiredSet := make(map[string]struct{})
	for _, groupKey := range desiredKeys {
		modelBuckets := partitionSiteModelsByRouteType(modelsByGroup[groupKey], shouldSplit, siteRecord.Platform)
		for routeType, bucketModels := range modelBuckets {
			if len(bucketModels) == 0 {
				continue
			}
			obType := routeType.ToOutboundType()
			desiredSet[compositeBindingKey(groupKey, obType, shouldSplit)] = struct{}{}
		}
	}
	if err := rewriteManagedGroupItemsForAccount(ctx, account.ID, shouldSplit, account.Models, bindingChannelByKey); err != nil {
		return nil, err
	}
	for _, binding := range existingBindings {
		groupKey := model.NormalizeSiteGroupKey(binding.GroupKey)
		if _, ok := desiredSet[groupKey]; ok {
			continue
		}
		if err := op.ChannelDelManaged(binding.ChannelID, ctx); err != nil {
			log.Warnf("failed to delete stale managed channel %d: %v", binding.ChannelID, err)
		}
		if err := db.GetDB().WithContext(ctx).Delete(&binding).Error; err != nil {
			return nil, fmt.Errorf("failed to delete stale site channel binding: %w", err)
		}
	}

	return managedChannelIDs, nil
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

func buildManagedChannelName(siteRecord *model.Site, account *model.SiteAccount, group model.SiteUserGroup, obType outbound.OutboundType, split bool) string {
	base := fmt.Sprintf("[Site] %s / %s / %s (%s)", siteRecord.Name, account.Name, model.NormalizeSiteGroupName(group.GroupKey, group.Name), model.NormalizeSiteGroupKey(group.GroupKey))
	if !split {
		return base
	}
	if suffix := model.SiteModelRouteTypeName(model.SiteModelRouteTypeFromOutboundType(obType)); suffix != "" {
		return base + " [" + suffix + "]"
	}
	return base
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
func buildChannelKeys(tokens []model.SiteToken) []model.ChannelKey {
	keys := make([]model.ChannelKey, 0, len(tokens))
	for _, token := range tokens {
		if !model.IsReadySiteToken(token) || model.IsMaskedSiteTokenValue(token.Token) {
			continue
		}
		normalized := model.NormalizeSiteSyncTokenValue(token.Token)
		if normalized == "" {
			continue
		}
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

func platformOutboundType(platform model.SitePlatform) outbound.OutboundType {
	switch platform {
	case model.SitePlatformClaude:
		return outbound.OutboundTypeAnthropic
	case model.SitePlatformGemini:
		return outbound.OutboundTypeGemini
	default:
		return outbound.OutboundTypeOpenAIChat
	}
}

// shouldSplitByOutboundType 判断是否需要按模型端点格式拆分 Channel
func shouldSplitByOutboundType(site *model.Site) bool {
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
func partitionModelsByOutboundType(modelNames []string, split bool, platform model.SitePlatform) map[outbound.OutboundType][]string {
	if !split {
		// 不拆分时，所有模型放入平台默认的单一桶
		obType := platformOutboundType(platform)
		return map[outbound.OutboundType][]string{obType: modelNames}
	}
	buckets := make(map[outbound.OutboundType][]string)
	for _, name := range modelNames {
		obType := classifyModelOutboundType(name)
		buckets[obType] = append(buckets[obType], name)
	}
	return buckets
}

func partitionSiteModelsByRouteType(items []model.SiteModel, split bool, platform model.SitePlatform) map[model.SiteModelRouteType][]model.SiteModel {
	if !split {
		routeType := model.SiteModelRouteTypeFromOutboundType(platformOutboundType(platform))
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

func rewriteManagedGroupItemsForAccount(ctx context.Context, accountID int, split bool, accountModels []model.SiteModel, bindingChannelByKey map[string]int) error {
	if len(bindingChannelByKey) == 0 {
		return nil
	}
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
