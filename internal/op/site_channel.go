package op

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

// siteChannelLogPageSize limits logs fetched for site channel history summaries.
const siteChannelLogPageSize = 500

func SiteChannelList(ctx context.Context) ([]model.SiteChannelCard, error) {
	sites, err := SiteList(ctx)
	if err != nil {
		return nil, err
	}
	logs, logsErr := RelayLogList(ctx, nil, nil, nil, 1, siteChannelLogPageSize)
	cards := make([]model.SiteChannelCard, 0, len(sites))
	for _, site := range sites {
		card, err := buildSiteChannelCard(ctx, site, logs, logsErr)
		if err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func SiteChannelGet(siteID int, ctx context.Context) (*model.SiteChannelCard, error) {
	site, err := SiteGet(siteID, ctx)
	if err != nil {
		return nil, err
	}
	logs, logsErr := RelayLogList(ctx, nil, nil, nil, 1, siteChannelLogPageSize)
	card, err := buildSiteChannelCard(ctx, *site, logs, logsErr)
	if err != nil {
		return nil, err
	}
	return &card, nil
}

func SiteChannelAccountGet(siteID int, accountID int, ctx context.Context) (*model.SiteChannelAccount, error) {
	site, err := SiteGet(siteID, ctx)
	if err != nil {
		return nil, err
	}
	var target *model.SiteAccount
	for i := range site.Accounts {
		if site.Accounts[i].ID == accountID {
			target = &site.Accounts[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("site account not found")
	}
	channelIDs := collectAccountChannelIDs(*target)
	logs, logsErr := RelayLogList(ctx, nil, nil, channelIDs, 1, siteChannelLogPageSize)
	var historyMap map[string]*model.SiteModelHistorySummary
	if logsErr == nil {
		historyMap, _ = siteChannelModelHistoryForAccount(*site, *target, logs)
	}
	view := model.SiteChannelAccount{
		SiteID:      site.ID,
		AccountID:   target.ID,
		AccountName: target.Name,
		Enabled:     target.Enabled,
		AutoSync:    target.AutoSync,
		Groups:      buildSiteChannelGroups(ctx, *site, *target, historyMap),
	}
	view.GroupCount = len(view.Groups)
	view.ModelCount = countSiteChannelModels(view.Groups)
	view.RouteSummaries = summarizeSiteRoutes(view.Groups)
	return &view, nil
}

func SiteChannelResetAccountRoutes(siteID int, accountID int, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []model.SiteModel
		if err := tx.Joins("JOIN site_accounts ON site_accounts.id = site_models.site_account_id").
			Where("site_accounts.site_id = ? AND site_models.site_account_id = ?", siteID, accountID).
			Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			routeType := model.InferSiteModelRouteType(row.ModelName)
			routeRawPayload := ""
			if metadata, ok := model.ParseSiteModelRouteMetadata(row.RouteRawPayload); ok {
				routeType = metadata.RouteType
				routeRawPayload = row.RouteRawPayload
			}
			if err := tx.Model(&model.SiteModel{}).Where("id = ?", row.ID).Updates(map[string]any{
				"route_type":        routeType,
				"route_source":      model.SiteModelRouteSourceSyncInferred,
				"manual_override":   false,
				"route_raw_payload": routeRawPayload,
				"route_updated_at":  gorm.Expr("CURRENT_TIMESTAMP"),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func SiteChannelModelHistory(siteID int, accountID int, ctx context.Context) (map[string]*model.SiteModelHistorySummary, error) {
	site, err := SiteGet(siteID, ctx)
	if err != nil {
		return nil, err
	}
	for _, account := range site.Accounts {
		if account.ID == accountID {
			channelIDs := collectAccountChannelIDs(account)
			logs, err := RelayLogList(ctx, nil, nil, channelIDs, 1, siteChannelLogPageSize)
			if err != nil {
				return nil, err
			}
			return siteChannelModelHistoryForAccount(*site, account, logs)
		}
	}
	return nil, fmt.Errorf("site account not found")
}

func siteChannelModelHistoryForAccount(site model.Site, account model.SiteAccount, logs []model.RelayLog) (map[string]*model.SiteModelHistorySummary, error) {
	bindingByChannel := make(map[int]model.SiteChannelBinding)
	for _, binding := range account.ChannelBindings {
		bindingByChannel[binding.ChannelID] = binding
	}
	modelRouteMap := make(map[string]model.SiteModelRouteType)
	for _, item := range account.Models {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		modelRouteMap[groupKey+"\x00"+strings.TrimSpace(item.ModelName)] = model.NormalizeSiteModelRouteType(item.RouteType)
	}
	result := make(map[string]*model.SiteModelHistorySummary)
	for _, entry := range logs {
		if len(entry.Attempts) == 0 {
			appendLegacySiteChannelHistory(result, bindingByChannel, modelRouteMap, entry)
			continue
		}

		for i := len(entry.Attempts) - 1; i >= 0; i-- {
			attempt := entry.Attempts[i]
			if attempt.Status != model.AttemptSuccess && attempt.Status != model.AttemptFailed {
				continue
			}
			appendAttemptSiteChannelHistory(result, bindingByChannel, modelRouteMap, entry, attempt)
		}
	}
	return result, nil
}

func appendLegacySiteChannelHistory(
	result map[string]*model.SiteModelHistorySummary,
	bindingByChannel map[int]model.SiteChannelBinding,
	modelRouteMap map[string]model.SiteModelRouteType,
	entry model.RelayLog,
) {
	channelID := entry.ChannelId
	if channelID == 0 {
		for i := len(entry.Attempts) - 1; i >= 0; i-- {
			if entry.Attempts[i].ChannelID != 0 {
				channelID = entry.Attempts[i].ChannelID
				break
			}
		}
	}
	binding, ok := bindingByChannel[channelID]
	if !ok {
		return
	}
	baseGroupKey, fallbackRouteType := model.ParseSiteChannelBindingKey(binding.GroupKey)
	actualModel := strings.TrimSpace(entry.ActualModelName)
	if actualModel == "" {
		actualModel = strings.TrimSpace(entry.RequestModelName)
	}
	if actualModel == "" {
		return
	}
	lookupKey := baseGroupKey + "\x00" + actualModel
	routeType := fallbackRouteType
	if value, ok := modelRouteMap[lookupKey]; ok {
		routeType = value
	}
	appendSiteChannelHistoryEntry(result, lookupKey, entry.Time, strings.TrimSpace(entry.Error) == "", routeType, channelID, entry.ChannelName, entry.RequestModelName, entry.ActualModelName, entry.Error)
}

func appendAttemptSiteChannelHistory(
	result map[string]*model.SiteModelHistorySummary,
	bindingByChannel map[int]model.SiteChannelBinding,
	modelRouteMap map[string]model.SiteModelRouteType,
	entry model.RelayLog,
	attempt model.ChannelAttempt,
) {
	if attempt.ChannelID == 0 {
		return
	}
	binding, ok := bindingByChannel[attempt.ChannelID]
	if !ok {
		return
	}
	baseGroupKey, fallbackRouteType := model.ParseSiteChannelBindingKey(binding.GroupKey)
	actualModel := strings.TrimSpace(attempt.ModelName)
	if actualModel == "" {
		actualModel = strings.TrimSpace(entry.ActualModelName)
	}
	if actualModel == "" {
		actualModel = strings.TrimSpace(entry.RequestModelName)
	}
	if actualModel == "" {
		return
	}
	lookupKey := baseGroupKey + "\x00" + actualModel
	routeType := fallbackRouteType
	if value, ok := modelRouteMap[lookupKey]; ok {
		routeType = value
	}
	appendSiteChannelHistoryEntry(result, lookupKey, entry.Time, attempt.Status == model.AttemptSuccess, routeType, attempt.ChannelID, attempt.ChannelName, entry.RequestModelName, actualModel, attempt.Msg)
}

func appendSiteChannelHistoryEntry(
	result map[string]*model.SiteModelHistorySummary,
	key string,
	timeValue int64,
	success bool,
	routeType model.SiteModelRouteType,
	channelID int,
	channelName string,
	requestModel string,
	actualModel string,
	errorMessage string,
) {
	summary := result[key]
	if summary == nil {
		summary = &model.SiteModelHistorySummary{Recent: make([]model.SiteModelHistoryEntry, 0, 5)}
		result[key] = summary
	}
	if success {
		summary.SuccessCount++
	} else {
		summary.FailureCount++
	}
	if summary.LastRequestAt == nil || timeValue > *summary.LastRequestAt {
		t := timeValue
		summary.LastRequestAt = &t
	}
	if len(summary.Recent) < 5 {
		summary.Recent = append(summary.Recent, model.SiteModelHistoryEntry{
			Time:         timeValue,
			Success:      success,
			RouteType:    routeType,
			ChannelID:    channelID,
			ChannelName:  channelName,
			RequestModel: requestModel,
			ActualModel:  actualModel,
			Error:        errorMessage,
		})
	}
}

func buildSiteChannelCard(ctx context.Context, site model.Site, logs []model.RelayLog, logsErr error) (model.SiteChannelCard, error) {
	card := model.SiteChannelCard{
		SiteID:       site.ID,
		SiteName:     site.Name,
		BaseURL:      site.BaseURL,
		Platform:     site.Platform,
		Enabled:      site.Enabled,
		AccountCount: len(site.Accounts),
		Accounts:     make([]model.SiteChannelAccount, 0, len(site.Accounts)),
	}
	historyByAccount := make(map[int]map[string]*model.SiteModelHistorySummary)
	for _, account := range site.Accounts {
		if logsErr == nil {
			history, err := siteChannelModelHistoryForAccount(site, account, logs)
			if err == nil {
				historyByAccount[account.ID] = history
			}
		} else {
			_ = ctx
		}
		view := model.SiteChannelAccount{
			SiteID:      site.ID,
			AccountID:   account.ID,
			AccountName: account.Name,
			Enabled:     account.Enabled,
			AutoSync:    account.AutoSync,
			Groups:      buildSiteChannelGroups(ctx, site, account, historyByAccount[account.ID]),
		}
		view.GroupCount = len(view.Groups)
		view.ModelCount = countSiteChannelModels(view.Groups)
		view.RouteSummaries = summarizeSiteRoutes(view.Groups)
		card.Accounts = append(card.Accounts, view)
	}
	return card, nil
}

func buildSiteChannelGroups(ctx context.Context, site model.Site, account model.SiteAccount, historyMap map[string]*model.SiteModelHistorySummary) []model.SiteChannelGroup {
	split := siteChannelShouldSplitByOutboundType(site)
	groups := make(map[string]*model.SiteChannelGroup)
	projectedChannels := make(map[int]*model.Channel)
	for _, group := range account.UserGroups {
		key := model.NormalizeSiteGroupKey(group.GroupKey)
		groups[key] = &model.SiteChannelGroup{GroupKey: key, GroupName: model.NormalizeSiteGroupName(key, group.Name), ProjectedChannelIDs: make([]int, 0), SourceKeys: make([]model.SiteSourceKey, 0), ProjectedKeys: make([]model.SiteProjectedKey, 0), Models: make([]model.SiteChannelModel, 0)}
	}
	for _, token := range account.Tokens {
		key := model.NormalizeSiteGroupKey(token.GroupKey)
		group := ensureSiteChannelGroup(groups, key, token.GroupName)
		group.KeyCount++
		if model.NormalizeSiteTokenValueStatus(token.ValueStatus, token.Token) == model.SiteTokenValueStatusMaskedPending {
			group.MaskedPendingKeyCount++
		}
		if token.Enabled && model.IsReadySiteToken(token) && !model.IsMaskedSiteTokenValue(token.Token) {
			group.EnabledKeyCount++
		}
		var lastSyncAt *int64
		if token.LastSyncAt != nil && !token.LastSyncAt.IsZero() {
			unix := token.LastSyncAt.UnixMilli()
			lastSyncAt = &unix
		}
		group.SourceKeys = append(group.SourceKeys, model.SiteSourceKey{
			ID:          token.ID,
			Enabled:     token.Enabled,
			Token:       token.Token,
			TokenMasked: maskProjectedChannelKey(token.Token),
			Name:        token.Name,
			GroupKey:    key,
			GroupName:   model.NormalizeSiteGroupName(key, token.GroupName),
			ValueStatus: model.NormalizeSiteTokenValueStatus(token.ValueStatus, token.Token),
			LastSyncAt:  lastSyncAt,
		})
	}
	for _, binding := range account.ChannelBindings {
		baseKey, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
		group := ensureSiteChannelGroup(groups, baseKey, baseKey)
		group.HasProjectedChannel = true
		group.ProjectedChannelIDs = append(group.ProjectedChannelIDs, binding.ChannelID)
		if _, ok := projectedChannels[binding.ChannelID]; ok {
			continue
		}
		channel, err := ChannelGet(binding.ChannelID, ctx)
		if err != nil {
			continue
		}
		projectedChannels[binding.ChannelID] = channel
		for _, key := range channel.Keys {
			group.ProjectedKeys = append(group.ProjectedKeys, model.SiteProjectedKey{
				ID:               key.ID,
				ChannelID:        channel.ID,
				ChannelName:      channel.Name,
				Enabled:          key.Enabled,
				ChannelKey:       key.ChannelKey,
				ChannelKeyMasked: maskProjectedChannelKey(key.ChannelKey),
				Remark:           key.Remark,
				StatusCode:       key.StatusCode,
				LastUseTimeStamp: key.LastUseTimeStamp,
				TotalCost:        key.TotalCost,
			})
		}
	}
	for _, item := range account.Models {
		key := model.NormalizeSiteGroupKey(item.GroupKey)
		if !siteModelBelongsToGroup(item, key) {
			continue
		}
		group := ensureSiteChannelGroup(groups, key, key)
		routeMetadata, _ := model.ParseSiteModelRouteMetadata(item.RouteRawPayload)
		channelID, hasChannel := findProjectedChannelID(account.ChannelBindings, key, item.RouteType, split)
		modelView := model.SiteChannelModel{
			ModelName:      item.ModelName,
			RouteType:      model.NormalizeSiteModelRouteType(item.RouteType),
			RouteSource:    model.NormalizeSiteModelRouteSource(item.RouteSource, item.ManualOverride),
			ManualOverride: item.ManualOverride,
			Disabled:       item.Disabled,
			RouteMetadata:  routeMetadata,
			History:        historyMap[key+"\x00"+item.ModelName],
		}
		if hasChannel {
			id := channelID
			modelView.ProjectedChannelID = &id
		}
		group.Models = append(group.Models, modelView)
	}
	result := make([]model.SiteChannelGroup, 0, len(groups))
	for _, item := range groups {
		item.HasKeys = item.KeyCount > 0
		sort.Slice(item.ProjectedChannelIDs, func(i, j int) bool { return item.ProjectedChannelIDs[i] < item.ProjectedChannelIDs[j] })
		sort.Slice(item.SourceKeys, func(i, j int) bool {
			if item.SourceKeys[i].Name == item.SourceKeys[j].Name {
				return item.SourceKeys[i].ID < item.SourceKeys[j].ID
			}
			return item.SourceKeys[i].Name < item.SourceKeys[j].Name
		})
		sort.Slice(item.ProjectedKeys, func(i, j int) bool {
			if item.ProjectedKeys[i].ChannelID == item.ProjectedKeys[j].ChannelID {
				return item.ProjectedKeys[i].ID < item.ProjectedKeys[j].ID
			}
			return item.ProjectedKeys[i].ChannelID < item.ProjectedKeys[j].ChannelID
		})
		sort.Slice(item.Models, func(i, j int) bool { return item.Models[i].ModelName < item.Models[j].ModelName })
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].GroupKey < result[j].GroupKey })
	return result
}

func siteModelBelongsToGroup(item model.SiteModel, groupKey string) bool {
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

func maskProjectedChannelKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return trimmed
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}

func ensureSiteChannelGroup(groups map[string]*model.SiteChannelGroup, groupKey string, groupName string) *model.SiteChannelGroup {
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	if item, ok := groups[groupKey]; ok {
		if strings.TrimSpace(item.GroupName) == "" {
			item.GroupName = model.NormalizeSiteGroupName(groupKey, groupName)
		}
		return item
	}
	item := &model.SiteChannelGroup{GroupKey: groupKey, GroupName: model.NormalizeSiteGroupName(groupKey, groupName), ProjectedChannelIDs: make([]int, 0), SourceKeys: make([]model.SiteSourceKey, 0), ProjectedKeys: make([]model.SiteProjectedKey, 0), Models: make([]model.SiteChannelModel, 0)}
	groups[groupKey] = item
	return item
}

func normalizeEditableSourceTokenValue(value string) (string, error) {
	normalized := model.NormalizeSiteSyncTokenValue(value)
	if normalized == "" {
		return "", fmt.Errorf("key 不能为空")
	}
	if model.IsMaskedSiteTokenValue(normalized) {
		return "", fmt.Errorf("必须填写完整 Key，不能保存脱敏值")
	}
	return normalized, nil
}

func UpdateSiteSourceKeys(siteID int, accountID int, req *model.SiteSourceKeyUpdateRequest, ctx context.Context) error {
	if req == nil {
		return fmt.Errorf("site source key update request is nil")
	}
	targetGroupKey := model.NormalizeSiteGroupKey(req.GroupKey)

	site, err := SiteGet(siteID, ctx)
	if err != nil {
		return err
	}

	var account *model.SiteAccount
	for i := range site.Accounts {
		if site.Accounts[i].ID == accountID {
			account = &site.Accounts[i]
			break
		}
	}
	if account == nil {
		return fmt.Errorf("site account not found")
	}

	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existingTokens []model.SiteToken
		if err := tx.Where("site_account_id = ? AND group_key = ?", accountID, targetGroupKey).Find(&existingTokens).Error; err != nil {
			return err
		}

		validIDs := make(map[int]model.SiteToken, len(existingTokens))
		for _, token := range existingTokens {
			validIDs[token.ID] = token
		}

		for _, item := range req.KeysToAdd {
			normalizedToken, err := normalizeEditableSourceTokenValue(item.Token)
			if err != nil {
				return err
			}
			row := model.SiteToken{
				SiteAccountID: accountID,
				Name:          strings.TrimSpace(item.Name),
				Token:         normalizedToken,
				GroupKey:      targetGroupKey,
				GroupName:     model.NormalizeSiteGroupName(targetGroupKey, targetGroupKey),
				Enabled:       item.Enabled,
				ValueStatus:   model.SiteTokenValueStatusReady,
				Source:        "manual",
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}

		for _, item := range req.KeysToUpdate {
			existing, ok := validIDs[item.ID]
			if !ok {
				continue
			}
			updates := map[string]any{}
			if item.Enabled != nil {
				updates["enabled"] = *item.Enabled
			}
			if item.Name != nil {
				updates["name"] = strings.TrimSpace(*item.Name)
			}
			if existing.Source != "manual" {
				updates["source"] = "manual"
			}
			if item.Token != nil {
				normalizedToken, err := normalizeEditableSourceTokenValue(*item.Token)
				if err != nil {
					return err
				}
				updates["token"] = normalizedToken
				updates["value_status"] = model.NormalizeSiteTokenValueStatus(existing.ValueStatus, normalizedToken)
			}
			if len(updates) == 0 {
				continue
			}
			if err := tx.Model(&model.SiteToken{}).Where("id = ? AND site_account_id = ? AND group_key = ?", item.ID, accountID, targetGroupKey).Updates(updates).Error; err != nil {
				return err
			}
		}

		if len(req.KeysToDelete) > 0 {
			deletableIDs := make([]int, 0, len(req.KeysToDelete))
			for _, id := range req.KeysToDelete {
				if _, ok := validIDs[id]; ok {
					deletableIDs = append(deletableIDs, id)
				}
			}
			if len(deletableIDs) > 0 {
				if err := tx.Where("id IN ? AND site_account_id = ? AND group_key = ?", deletableIDs, accountID, targetGroupKey).Delete(&model.SiteToken{}).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func countSiteChannelModels(groups []model.SiteChannelGroup) int {
	total := 0
	for _, group := range groups {
		total += len(group.Models)
	}
	return total
}

func summarizeSiteRoutes(groups []model.SiteChannelGroup) []model.SiteRouteSummary {
	counts := make(map[model.SiteModelRouteType]int)
	for _, group := range groups {
		for _, item := range group.Models {
			counts[item.RouteType]++
		}
	}
	result := make([]model.SiteRouteSummary, 0, len(counts))
	for routeType, count := range counts {
		result = append(result, model.SiteRouteSummary{RouteType: routeType, Count: count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RouteType < result[j].RouteType })
	return result
}

func siteChannelShouldSplitByOutboundType(site model.Site) bool {
	return model.ShouldSplitSiteChannelRoutes(site.Platform)
}

func siteChannelCompositeBindingKey(groupKey string, routeType model.SiteModelRouteType, split bool) string {
	return model.ComposeSiteChannelBindingKey(groupKey, routeType, split)
}

func collectAccountChannelIDs(account model.SiteAccount) []int {
	ids := make(map[int]struct{})
	for _, binding := range account.ChannelBindings {
		ids[binding.ChannelID] = struct{}{}
	}
	result := make([]int, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	return result
}

func findProjectedChannelID(bindings []model.SiteChannelBinding, groupKey string, routeType model.SiteModelRouteType, split bool) (int, bool) {
	if !model.IsProjectedSiteModelRouteType(routeType) {
		return 0, false
	}
	targetKey := siteChannelCompositeBindingKey(groupKey, routeType, split)
	for _, binding := range bindings {
		if model.NormalizeSiteGroupKey(binding.GroupKey) == targetKey {
			return binding.ChannelID, true
		}
	}
	if split {
		fallbackKey := model.NormalizeSiteGroupKey(groupKey)
		for _, binding := range bindings {
			if model.NormalizeSiteGroupKey(binding.GroupKey) == fallbackKey {
				return binding.ChannelID, true
			}
		}
	}
	return 0, false
}
