package sitesync

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
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
			if err := op.ChannelEnabled(binding.ChannelID, false, ctx); err != nil {
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

	modelNames := make([]string, 0, len(account.Models))
	for _, item := range account.Models {
		if strings.TrimSpace(item.ModelName) != "" {
			modelNames = append(modelNames, strings.TrimSpace(item.ModelName))
		}
	}
	slices.Sort(modelNames)
	modelNames = slices.Compact(modelNames)

	// Filter out disabled models for this site
	disabledModels, _ := op.SiteDisabledModelList(siteRecord.ID, ctx)
	if len(disabledModels) > 0 {
		disabledSet := make(map[string]struct{}, len(disabledModels))
		for _, name := range disabledModels {
			disabledSet[strings.TrimSpace(name)] = struct{}{}
		}
		filtered := make([]string, 0, len(modelNames))
		for _, name := range modelNames {
			if _, disabled := disabledSet[name]; !disabled {
				filtered = append(filtered, name)
			}
		}
		modelNames = filtered
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
	modelBuckets := partitionModelsByOutboundType(modelNames, shouldSplit, siteRecord.Platform)

	for _, groupKey := range desiredKeys {
		group := groupMap[groupKey]
		groupTokens := tokenGroups[groupKey]
		useProxy, proxyURL := resolveSiteAccountProxy(siteRecord, account)
		baseUrls := []model.BaseUrl{{URL: buildProjectedChannelBaseURL(siteRecord), Delay: 0}}
		enabled := siteRecord.Enabled && account.Enabled && len(groupTokens) > 0

		for obType, bucketModels := range modelBuckets {
			if len(bucketModels) == 0 {
				continue
			}
			bindingKey := compositeBindingKey(groupKey, obType, shouldSplit)
			channelPayload := model.Channel{
				Name:         buildManagedChannelName(siteRecord, account, group, obType, shouldSplit),
				Type:         obType,
				Enabled:      enabled,
				BaseUrls:     baseUrls,
				Keys:         buildChannelKeys(groupTokens),
				Model:        strings.Join(bucketModels, ","),
				CustomModel:  "",
				Proxy:        useProxy,
				AutoSync:     false,
				AutoGroup:    model.AutoGroupTypeNone,
				CustomHeader: siteRecord.CustomHeader,
				ChannelProxy: proxyURL,
			}

			binding, exists := bindingMap[bindingKey]
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
				managedChannelIDs = append(managedChannelIDs, channelPayload.ID)
				continue
			}

			updateReq := &model.ChannelUpdateRequest{ID: existingChannel.ID, Name: &channelPayload.Name, Type: &channelPayload.Type, Enabled: &channelPayload.Enabled, BaseUrls: &channelPayload.BaseUrls, Model: &channelPayload.Model, CustomModel: &channelPayload.CustomModel, Proxy: &channelPayload.Proxy, AutoSync: &channelPayload.AutoSync, AutoGroup: &channelPayload.AutoGroup, CustomHeader: &channelPayload.CustomHeader, ChannelProxy: channelPayload.ChannelProxy, BypassManagedCheck: true}
			for _, key := range existingChannel.Keys {
				updateReq.KeysToDelete = append(updateReq.KeysToDelete, key.ID)
			}
			for _, key := range channelPayload.Keys {
				updateReq.KeysToAdd = append(updateReq.KeysToAdd, model.ChannelKeyAddRequest{Enabled: key.Enabled, ChannelKey: key.ChannelKey, Remark: key.Remark})
			}
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
			managedChannelIDs = append(managedChannelIDs, existingChannel.ID)
		}
	}

	desiredSet := make(map[string]struct{})
	for _, groupKey := range desiredKeys {
		for obType, bucketModels := range modelBuckets {
			if len(bucketModels) == 0 {
				continue
			}
			desiredSet[compositeBindingKey(groupKey, obType, shouldSplit)] = struct{}{}
		}
	}
	for _, binding := range existingBindings {
		groupKey := model.NormalizeSiteGroupKey(binding.GroupKey)
		if _, ok := desiredSet[groupKey]; ok {
			continue
		}
		if err := op.ChannelDel(binding.ChannelID, ctx); err != nil {
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
	if suffix := outboundTypeName(obType); suffix != "" {
		return base + " [" + suffix + "]"
	}
	return base
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
		if strings.TrimSpace(token.Token) == "" {
			continue
		}
		keys = append(keys, model.ChannelKey{Enabled: token.Enabled, ChannelKey: strings.TrimSpace(token.Token), Remark: model.NormalizeSiteGroupName(token.GroupKey, token.GroupName)})
	}
	return keys
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
	switch site.OutboundFormatMode {
	case model.OutboundFormatModeOpenAI:
		return false
	case model.OutboundFormatModeAuto:
		return true
	default:
		// 空值：单一供应商平台不拆分，多模型平台自动拆分
		switch site.Platform {
		case model.SitePlatformClaude, model.SitePlatformGemini, model.SitePlatformOpenAI:
			return false
		default:
			return true
		}
	}
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

// compositeBindingKey 生成复合绑定 key，用于区分同一 tokenGroup 的不同端点格式 Channel
func compositeBindingKey(groupKey string, obType outbound.OutboundType, split bool) string {
	if !split {
		return groupKey
	}
	suffix := outboundTypeSuffix(obType)
	if suffix == "" {
		return groupKey
	}
	return groupKey + "::" + suffix
}

func outboundTypeSuffix(t outbound.OutboundType) string {
	switch t {
	case outbound.OutboundTypeAnthropic:
		return "anthropic"
	case outbound.OutboundTypeGemini:
		return "gemini"
	default:
		return ""
	}
}

func outboundTypeName(t outbound.OutboundType) string {
	switch t {
	case outbound.OutboundTypeAnthropic:
		return "Anthropic"
	case outbound.OutboundTypeGemini:
		return "Gemini"
	default:
		return ""
	}
}
