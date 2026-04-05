package sitesync

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
)

const (
	siteModelSourceSync         = "sync"
	siteModelSourceSyncFallback = "sync_fallback"
)

type siteModelFetchResult struct {
	names      []string
	source     string
	detections map[string]siteModelRouteDetection
}

func fetchManagementTokens(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) ([]model.SiteToken, error) {
	payload, err := requestJSONWithManagedAccessToken(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/token/?p=0&size=100"), nil, accessToken, account)
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
		payload, err := requestJSONWithManagedAccessToken(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, endpoint), nil, accessToken, account)
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
	inferredGroups := make([]model.SiteUserGroup, 0)
	seen := make(map[string]struct{})
	for _, token := range tokens {
		key := model.NormalizeSiteGroupKey(token.GroupKey)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		inferredGroups = append(inferredGroups, model.SiteUserGroup{GroupKey: key, Name: model.NormalizeSiteGroupName(key, token.GroupName)})
	}

	endpoints := []string{
		"/api/v1/groups/available",
		"/api/v1/groups?page=1&page_size=100",
		"/api/v1/groups",
		"/api/v1/group?page=1&page_size=100",
		"/api/v1/group",
	}
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
	if len(inferredGroups) > 0 {
		return inferredGroups, nil
	}
	return []model.SiteUserGroup{{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}}, nil
}

func stripBearerPrefix(token string) string {
	trimmed := strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(trimmed), "bearer ") {
		return strings.TrimSpace(trimmed[7:])
	}
	return trimmed
}

func fetchModelsForSiteToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, token model.SiteToken) ([]string, error) {
	useProxy, proxyURL := resolveSiteAccountProxy(siteRecord, account)
	var (
		firstErr error
		models   []string
	)

	for _, baseURL := range buildModelFetchBaseURLs(siteRecord) {
		channel := model.Channel{Type: platformOutboundType(siteRecord.Platform), BaseUrls: []model.BaseUrl{{URL: baseURL, Delay: 0}}, Keys: []model.ChannelKey{{Enabled: true, ChannelKey: token.Token}}, Proxy: useProxy, CustomHeader: siteRecord.CustomHeader, ChannelProxy: proxyURL}
		fetched, err := helper.FetchModels(ctx, channel)
		if err == nil && len(fetched) > 0 {
			return normalizeModelNames(fetched), nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if len(fetched) > 0 {
			models = fetched
		}
	}
	if siteRecord.Platform != model.SitePlatformOneHub && siteRecord.Platform != model.SitePlatformDoneHub {
		if firstErr != nil {
			return nil, firstErr
		}
		return normalizeModelNames(models), nil
	}

	payload, fallbackErr := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/available_model"), nil, map[string]string{"Authorization": "Bearer " + token.Token}, account)
	if fallbackErr != nil {
		if firstErr != nil {
			return nil, firstErr
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
		if firstErr != nil {
			return nil, firstErr
		}
		return normalizeModelNames(models), nil
	}
	names := make([]string, 0, len(modelSet))
	for name := range modelSet {
		names = append(names, name)
	}
	return normalizeModelNames(names), nil
}

func fetchManagementModels(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	token model.SiteToken,
	sessionFallbackFetcher func(token model.SiteToken) (siteModelFetchResult, error),
) (siteModelFetchResult, error) {
	models, err := fetchModelsForSiteToken(ctx, siteRecord, account, token)
	if len(models) > 0 || siteRecord.Platform != model.SitePlatformNewAPI {
		return siteModelFetchResult{names: models, source: siteModelSourceSync}, err
	}

	if sessionFallbackFetcher == nil {
		return siteModelFetchResult{}, err
	}

	fallbackResult, fallbackErr := sessionFallbackFetcher(token)
	if len(fallbackResult.names) > 0 {
		if strings.TrimSpace(fallbackResult.source) == "" {
			fallbackResult.source = siteModelSourceSyncFallback
		}
		return fallbackResult, nil
	}
	if err != nil {
		return siteModelFetchResult{}, err
	}
	return siteModelFetchResult{}, fallbackErr
}

func fetchManagedSessionModels(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) ([]string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, nil
	}
	payload, err := requestJSONWithManagedAccessToken(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/user/models"), nil, accessToken, account)
	if err != nil {
		return nil, err
	}
	return anyRouterParseModelNames(payload), nil
}

func buildModelFetchBaseURLs(siteRecord *model.Site) []string {
	if siteRecord == nil {
		return nil
	}

	baseURL := strings.TrimRight(strings.TrimSpace(siteRecord.BaseURL), "/")
	if baseURL == "" {
		return nil
	}

	candidates := []string{baseURL}
	if sitePlatformUsesV1ModelEndpoint(siteRecord.Platform) && !strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
		candidates = append(candidates, baseURL+"/v1")
	}
	return candidates
}

func filterSessionFallbackModelsByGroup(
	names []string,
	groupKey string,
	detections map[string]siteModelRouteDetection,
) (siteModelFetchResult, error) {
	normalizedGroupKey := model.NormalizeSiteGroupKey(groupKey)
	if normalizedGroupKey == "" {
		normalizedGroupKey = model.SiteDefaultGroupKey
	}
	if len(detections) == 0 {
		return siteModelFetchResult{}, fmt.Errorf("site sync could not resolve models for group %q; create a key for that group on the site and sync again", normalizedGroupKey)
	}

	filteredNames := make([]string, 0, len(names))
	filteredDetections := make(map[string]siteModelRouteDetection)
	for _, name := range normalizeModelNames(names) {
		lookupKey := strings.ToLower(strings.TrimSpace(name))
		detection, ok := detections[lookupKey]
		if !ok {
			continue
		}
		metadata, ok := model.ParseSiteModelRouteMetadata(detection.RouteRawPayload)
		if !ok || len(metadata.EnableGroups) == 0 {
			continue
		}
		if !stringSliceContainsFold(metadata.EnableGroups, normalizedGroupKey) {
			continue
		}
		filteredNames = append(filteredNames, name)
		filteredDetections[lookupKey] = detection
	}
	if len(filteredNames) == 0 {
		return siteModelFetchResult{}, fmt.Errorf("site sync could not resolve models for group %q; create a key for that group on the site and sync again", normalizedGroupKey)
	}

	return siteModelFetchResult{
		names:      filteredNames,
		source:     siteModelSourceSyncFallback,
		detections: filteredDetections,
	}, nil
}

func stringSliceContainsFold(values []string, target string) bool {
	normalizedTarget := strings.ToLower(strings.TrimSpace(target))
	if normalizedTarget == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), normalizedTarget) {
			return true
		}
	}
	return false
}

func sitePlatformUsesV1ModelEndpoint(platform model.SitePlatform) bool {
	switch platform {
	case model.SitePlatformClaude, model.SitePlatformGemini:
		return false
	default:
		return true
	}
}

func buildSiteModels(names []string, groupKey string, source string) []model.SiteModel {
	names = normalizeModelNames(names)
	models := make([]model.SiteModel, 0, len(names))
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	for _, name := range names {
		models = append(models, model.SiteModel{GroupKey: groupKey, ModelName: name, Source: source})
	}
	return models
}

func buildGlobalSiteModels(names []string, groups []model.SiteUserGroup, source string) []model.SiteModel {
	if len(groups) == 0 {
		return buildSiteModels(names, model.SiteDefaultGroupKey, source)
	}
	seen := make(map[string]struct{})
	models := make([]model.SiteModel, 0, len(names)*len(groups))
	for _, group := range groups {
		groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
		for _, item := range buildSiteModels(names, groupKey, source) {
			key := groupKey + "\x00" + item.ModelName
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			models = append(models, item)
		}
	}
	return models
}

func pickModelTokensByGroup(tokens []model.SiteToken) []model.SiteToken {
	if len(tokens) == 0 {
		return nil
	}

	order := make([]string, 0, len(tokens))
	selected := make(map[string]model.SiteToken, len(tokens))
	for _, token := range tokens {
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		token.GroupKey = groupKey
		token.GroupName = model.NormalizeSiteGroupName(groupKey, token.GroupName)
		if _, ok := selected[groupKey]; !ok {
			order = append(order, groupKey)
			selected[groupKey] = token
			continue
		}
		if shouldPreferGroupModelToken(token, selected[groupKey]) {
			selected[groupKey] = token
		}
	}

	result := make([]model.SiteToken, 0, len(order))
	for _, groupKey := range order {
		token := selected[groupKey]
		if strings.TrimSpace(token.Token) == "" {
			continue
		}
		result = append(result, token)
	}
	return result
}

func shouldPreferGroupModelToken(candidate model.SiteToken, current model.SiteToken) bool {
	candidateToken := strings.TrimSpace(candidate.Token)
	currentToken := strings.TrimSpace(current.Token)
	if candidateToken == "" {
		return false
	}
	if currentToken == "" {
		return true
	}
	if candidate.Enabled != current.Enabled {
		return candidate.Enabled
	}
	return candidate.IsDefault && !current.IsDefault
}

func syncSiteModelsByGroup(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	groupTokens []model.SiteToken,
	platformUserID int,
	source string,
	fetcher func(token model.SiteToken, allowGlobalFallback bool) (siteModelFetchResult, error),
) ([]model.SiteModel, error) {
	if len(groupTokens) == 0 {
		return nil, nil
	}

	allowGlobalFallback := len(groupTokens) == 1
	models := make([]model.SiteModel, 0)
	seen := make(map[string]struct{})
	var firstErr error

	for _, token := range groupTokens {
		result, err := fetcher(token, allowGlobalFallback)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if len(result.names) == 0 {
			continue
		}

		groupSource := strings.TrimSpace(result.source)
		if groupSource == "" {
			groupSource = source
		}
		groupModels := buildSiteModels(result.names, token.GroupKey, groupSource)
		if len(result.detections) > 0 {
			groupModels = applyKnownRouteDetectionsToSiteModels(groupModels, result.detections)
		} else {
			groupModels = applyDetectedRoutesToSiteModels(ctx, siteRecord, account, accessToken, token, platformUserID, groupModels)
		}
		for _, item := range groupModels {
			key := model.NormalizeSiteGroupKey(item.GroupKey) + "\x00" + strings.TrimSpace(item.ModelName)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			models = append(models, item)
		}
	}

	if len(models) == 0 {
		return nil, firstErr
	}

	sort.Slice(models, func(i, j int) bool {
		leftGroup := model.NormalizeSiteGroupKey(models[i].GroupKey)
		rightGroup := model.NormalizeSiteGroupKey(models[j].GroupKey)
		if leftGroup == rightGroup {
			return models[i].ModelName < models[j].ModelName
		}
		return leftGroup < rightGroup
	})
	return models, nil
}

func expandExplicitGroupModelsToGroups(
	items []model.SiteModel,
	groups []model.SiteUserGroup,
	tokens []model.SiteToken,
) []model.SiteModel {
	if len(items) == 0 || len(groups) == 0 {
		return items
	}

	groupKeys := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
		groupKeys[groupKey] = struct{}{}
	}

	groupKeysWithTokens := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		groupKeysWithTokens[groupKey] = struct{}{}
	}

	groupsWithoutTokens := make(map[string]struct{})
	for groupKey := range groupKeys {
		if _, ok := groupKeysWithTokens[groupKey]; ok {
			continue
		}
		groupsWithoutTokens[groupKey] = struct{}{}
	}
	if len(groupsWithoutTokens) == 0 {
		return items
	}

	expanded := make([]model.SiteModel, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		modelName := strings.TrimSpace(item.ModelName)
		key := groupKey + "\x00" + modelName
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			expanded = append(expanded, item)
		}

		metadata, ok := model.ParseSiteModelRouteMetadata(item.RouteRawPayload)
		if !ok || len(metadata.EnableGroups) == 0 {
			continue
		}
		for _, explicitGroupKey := range metadata.EnableGroups {
			targetGroupKey := model.NormalizeSiteGroupKey(explicitGroupKey)
			if _, ok := groupsWithoutTokens[targetGroupKey]; !ok {
				continue
			}
			targetKey := targetGroupKey + "\x00" + modelName
			if _, ok := seen[targetKey]; ok {
				continue
			}
			copy := item
			copy.ID = 0
			copy.SiteAccountID = 0
			copy.GroupKey = targetGroupKey
			expanded = append(expanded, copy)
			seen[targetKey] = struct{}{}
		}
	}
	return expanded
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

func fetchAccountBalance(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) (float64, float64) {
	if accessToken == "" {
		return 0, 0
	}
	payload, err := requestJSONWithManagedAccessToken(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/user/self"), nil, accessToken, account)
	if err != nil {
		return 0, 0
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = payload
	}
	balance := jsonFloat(data["quota"])
	balanceUsed := jsonFloat(data["used_quota"])
	// Some platforms use different field names
	if balance == 0 {
		balance = jsonFloat(data["balance"])
	}
	if balanceUsed == 0 {
		balanceUsed = jsonFloat(data["used_balance"])
	}
	return balance, balanceUsed
}

func jsonFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		var f float64
		if _, err := fmt.Sscanf(trimmed, "%f", &f); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
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
