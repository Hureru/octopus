package sitesync

import (
	"context"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
)

func fetchManagementTokens(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) ([]model.SiteToken, error) {
	payload, err := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/token/?p=0&size=100"), nil, map[string]string{"Authorization": "Bearer " + accessToken}, account)
	if err != nil {
		return nil, err
	}
	items := parseTokenItems(payload)
	tokens := make([]model.SiteToken, 0, len(items))
	for index, item := range items {
		tokenValue := strings.TrimSpace(jsonString(item["key"]))
		if tokenValue == "" {
			continue
		}
		groupKey := model.NormalizeSiteGroupKey(firstNonEmptyString(jsonString(item["group"]), jsonString(item["token_group"]), jsonString(item["group_name"])))
		groupName := model.NormalizeSiteGroupName(groupKey, firstNonEmptyString(jsonString(item["group_name"]), jsonString(item["group"]), jsonString(item["token_group"])))
		tokens = append(tokens, model.SiteToken{Name: firstNonEmptyString(strings.TrimSpace(jsonString(item["name"])), fmt.Sprintf("token-%d", index+1)), Token: tokenValue, GroupKey: groupKey, GroupName: groupName, Enabled: parseEnabledFlag(item["status"]), Source: "sync", IsDefault: index == 0})
	}
	return tokens, nil
}

func fetchManagementGroups(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) ([]model.SiteUserGroup, error) {
	endpoints := []string{"/api/user/self/groups", "/api/user_group_map"}
	seen := make(map[string]model.SiteUserGroup)
	for _, endpoint := range endpoints {
		payload, err := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, endpoint), nil, map[string]string{"Authorization": "Bearer " + accessToken}, account)
		if err != nil {
			continue
		}
		for _, group := range parseGroupItems(payload) {
			key := model.NormalizeSiteGroupKey(group.GroupKey)
			group.GroupKey = key
			group.Name = model.NormalizeSiteGroupName(key, group.Name)
			group.RawPayload = marshalRawPayload(payload)
			seen[key] = group
		}
	}
	if len(seen) == 0 {
		return []model.SiteUserGroup{{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}}, nil
	}
	groups := make([]model.SiteUserGroup, 0, len(seen))
	for _, group := range seen {
		groups = append(groups, group)
	}
	return groups, nil
}

func fetchSub2APITokens(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) ([]model.SiteToken, error) {
	endpoints := []string{"/api/v1/keys?page=1&page_size=100", "/api/v1/api-keys?page=1&page_size=100", "/api/v1/keys", "/api/v1/api-keys"}
	for _, endpoint := range endpoints {
		payload, err := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, endpoint), nil, map[string]string{"Authorization": ensureBearer(accessToken)}, account)
		if err != nil {
			continue
		}
		items := parseTokenItems(payload)
		tokens := make([]model.SiteToken, 0, len(items))
		for index, item := range items {
			tokenValue := strings.TrimSpace(jsonString(item["key"]))
			if tokenValue == "" {
				continue
			}
			groupKey := model.NormalizeSiteGroupKey(firstNonEmptyString(jsonString(item["group_id"]), jsonString(item["groupId"]), jsonString(item["group_name"]), jsonString(item["group"])))
			groupName := model.NormalizeSiteGroupName(groupKey, firstNonEmptyString(jsonString(item["group_name"]), jsonString(item["group"]), jsonString(item["groupId"])))
			tokens = append(tokens, model.SiteToken{Name: firstNonEmptyString(strings.TrimSpace(jsonString(item["name"])), fmt.Sprintf("token-%d", index+1)), Token: tokenValue, GroupKey: groupKey, GroupName: groupName, Enabled: parseEnabledFlag(item["status"]), Source: "sync", IsDefault: index == 0})
		}
		if len(tokens) > 0 {
			return tokens, nil
		}
	}
	return nil, nil
}

func fetchSub2APIGroups(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, tokens []model.SiteToken) ([]model.SiteUserGroup, error) {
	groups := make([]model.SiteUserGroup, 0)
	seen := make(map[string]struct{})
	for _, token := range tokens {
		key := model.NormalizeSiteGroupKey(token.GroupKey)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		groups = append(groups, model.SiteUserGroup{GroupKey: key, Name: model.NormalizeSiteGroupName(key, token.GroupName)})
	}
	if len(groups) > 0 {
		return groups, nil
	}

	endpoints := []string{"/api/v1/groups/available", "/api/v1/groups?page=1&page_size=100", "/api/v1/groups"}
	for _, endpoint := range endpoints {
		payload, err := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, endpoint), nil, map[string]string{"Authorization": ensureBearer(accessToken)}, account)
		if err != nil {
			continue
		}
		items := parseGroupItems(payload)
		if len(items) > 0 {
			return items, nil
		}
	}
	return []model.SiteUserGroup{{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}}, nil
}

func fetchModelsForSiteToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, token model.SiteToken) ([]string, error) {
	useProxy, proxyURL := resolveSiteAccountProxy(siteRecord, account)
	channel := model.Channel{Type: platformOutboundType(siteRecord.Platform), BaseUrls: []model.BaseUrl{{URL: siteRecord.BaseURL, Delay: 0}}, Keys: []model.ChannelKey{{Enabled: true, ChannelKey: token.Token}}, Proxy: useProxy, CustomHeader: siteRecord.CustomHeader, ChannelProxy: proxyURL}
	models, err := helper.FetchModels(ctx, channel)
	if err == nil && len(models) > 0 {
		return normalizeModelNames(models), nil
	}
	if siteRecord.Platform != model.SitePlatformOneHub && siteRecord.Platform != model.SitePlatformDoneHub {
		if err != nil {
			return nil, err
		}
		return normalizeModelNames(models), nil
	}

	payload, fallbackErr := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/available_model"), nil, map[string]string{"Authorization": "Bearer " + token.Token}, account)
	if fallbackErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, fallbackErr
	}

	modelSet := make(map[string]struct{})
	if dataMap, ok := nestedValue(payload, "data").(map[string]any); ok {
		for key := range dataMap {
			trimmed := strings.TrimSpace(key)
			if trimmed != "" {
				modelSet[trimmed] = struct{}{}
			}
		}
	}
	if len(modelSet) == 0 {
		if err != nil {
			return nil, err
		}
		return normalizeModelNames(models), nil
	}
	names := make([]string, 0, len(modelSet))
	for name := range modelSet {
		names = append(names, name)
	}
	return normalizeModelNames(names), nil
}

func buildSiteModels(names []string, source string) []model.SiteModel {
	names = normalizeModelNames(names)
	models := make([]model.SiteModel, 0, len(names))
	for _, name := range names {
		models = append(models, model.SiteModel{ModelName: name, Source: source})
	}
	return models
}

func mergeSiteGroups(groups []model.SiteUserGroup, tokens []model.SiteToken) []model.SiteUserGroup {
	merged := make(map[string]model.SiteUserGroup)
	for _, item := range groups {
		key := model.NormalizeSiteGroupKey(item.GroupKey)
		item.GroupKey = key
		item.Name = model.NormalizeSiteGroupName(key, item.Name)
		merged[key] = item
	}
	for _, token := range tokens {
		key := model.NormalizeSiteGroupKey(token.GroupKey)
		if _, ok := merged[key]; ok {
			continue
		}
		merged[key] = model.SiteUserGroup{GroupKey: key, Name: model.NormalizeSiteGroupName(key, token.GroupName)}
	}
	if len(merged) == 0 {
		merged[model.SiteDefaultGroupKey] = model.SiteUserGroup{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}
	}
	result := make([]model.SiteUserGroup, 0, len(merged))
	for _, group := range merged {
		result = append(result, group)
	}
	return result
}

func pickModelToken(tokens []model.SiteToken) model.SiteToken {
	for _, token := range tokens {
		if token.Enabled && strings.TrimSpace(token.Token) != "" {
			return token
		}
	}
	for _, token := range tokens {
		if strings.TrimSpace(token.Token) != "" {
			return token
		}
	}
	return model.SiteToken{}
}
