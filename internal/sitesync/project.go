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
	for _, groupKey := range desiredKeys {
		group := groupMap[groupKey]
		groupTokens := tokenGroups[groupKey]
		useProxy, proxyURL := resolveSiteAccountProxy(siteRecord, account)
		channelPayload := model.Channel{
			Name:         buildManagedChannelName(siteRecord, account, group),
			Type:         platformOutboundType(siteRecord.Platform),
			Enabled:      siteRecord.Enabled && account.Enabled && len(groupTokens) > 0,
			BaseUrls:     []model.BaseUrl{{URL: buildProjectedChannelBaseURL(siteRecord), Delay: 0}},
			Keys:         buildChannelKeys(groupTokens),
			Model:        strings.Join(modelNames, ","),
			CustomModel:  "",
			Proxy:        useProxy,
			AutoSync:     false,
			AutoGroup:    model.AutoGroupTypeNone,
			CustomHeader: siteRecord.CustomHeader,
			ChannelProxy: proxyURL,
		}

		binding, exists := bindingMap[groupKey]
		if !exists {
			if err := op.ChannelCreate(&channelPayload, ctx); err != nil {
				return nil, fmt.Errorf("failed to create managed channel: %w", err)
			}
			binding = model.SiteChannelBinding{SiteID: siteRecord.ID, SiteAccountID: account.ID, GroupKey: groupKey, ChannelID: channelPayload.ID}
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

		updateReq := &model.ChannelUpdateRequest{ID: existingChannel.ID, Name: &channelPayload.Name, Type: &channelPayload.Type, Enabled: &channelPayload.Enabled, BaseUrls: &channelPayload.BaseUrls, Model: &channelPayload.Model, CustomModel: &channelPayload.CustomModel, Proxy: &channelPayload.Proxy, AutoSync: &channelPayload.AutoSync, AutoGroup: &channelPayload.AutoGroup, CustomHeader: &channelPayload.CustomHeader, ChannelProxy: channelPayload.ChannelProxy}
		for _, key := range existingChannel.Keys {
			updateReq.KeysToDelete = append(updateReq.KeysToDelete, key.ID)
		}
		for _, key := range channelPayload.Keys {
			updateReq.KeysToAdd = append(updateReq.KeysToAdd, model.ChannelKeyAddRequest{Enabled: key.Enabled, ChannelKey: key.ChannelKey, Remark: key.Remark})
		}
		if _, err := op.ChannelUpdate(updateReq, ctx); err != nil {
			return nil, fmt.Errorf("failed to update managed channel: %w", err)
		}
		updateBinding := map[string]any{"group_key": groupKey}
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

	desiredSet := make(map[string]struct{}, len(desiredKeys))
	for _, groupKey := range desiredKeys {
		desiredSet[groupKey] = struct{}{}
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

func buildManagedChannelName(siteRecord *model.Site, account *model.SiteAccount, group model.SiteUserGroup) string {
	return fmt.Sprintf("[Site] %s / %s / %s (%s)", siteRecord.Name, account.Name, model.NormalizeSiteGroupName(group.GroupKey, group.Name), model.NormalizeSiteGroupKey(group.GroupKey))
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
