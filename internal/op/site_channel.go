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

func SiteChannelList(ctx context.Context) ([]model.SiteChannelCard, error) {
	sites, err := SiteList(ctx)
	if err != nil {
		return nil, err
	}
	cards := make([]model.SiteChannelCard, 0, len(sites))
	for _, site := range sites {
		card, err := buildSiteChannelCard(ctx, site)
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
	card, err := buildSiteChannelCard(ctx, *site)
	if err != nil {
		return nil, err
	}
	return &card, nil
}

func SiteChannelAccountGet(siteID int, accountID int, ctx context.Context) (*model.SiteChannelAccount, error) {
	card, err := SiteChannelGet(siteID, ctx)
	if err != nil {
		return nil, err
	}
	for _, account := range card.Accounts {
		if account.AccountID == accountID {
			copy := account
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("site account not found")
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
			return siteChannelModelHistoryForAccount(ctx, *site, account)
		}
	}
	return nil, fmt.Errorf("site account not found")
}

func siteChannelModelHistoryForAccount(ctx context.Context, site model.Site, account model.SiteAccount) (map[string]*model.SiteModelHistorySummary, error) {
	bindingByChannel := make(map[int]model.SiteChannelBinding)
	for _, binding := range account.ChannelBindings {
		bindingByChannel[binding.ChannelID] = binding
	}
	modelRouteMap := make(map[string]model.SiteModelRouteType)
	for _, item := range account.Models {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		modelRouteMap[groupKey+"\x00"+strings.TrimSpace(item.ModelName)] = model.NormalizeSiteModelRouteType(item.RouteType)
	}
	logs, err := RelayLogList(ctx, nil, nil, 1, 500)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*model.SiteModelHistorySummary)
	for _, entry := range logs {
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
			continue
		}
		baseGroupKey, fallbackRouteType := model.ParseSiteChannelBindingKey(binding.GroupKey)
		lookupKey := baseGroupKey + "\x00" + strings.TrimSpace(entry.ActualModelName)
		if strings.TrimSpace(entry.ActualModelName) == "" {
			lookupKey = baseGroupKey + "\x00" + strings.TrimSpace(entry.RequestModelName)
		}
		routeType, ok := modelRouteMap[lookupKey]
		if !ok {
			routeType = fallbackRouteType
		}
		key := lookupKey
		if strings.TrimSpace(entry.ActualModelName) == "" && strings.TrimSpace(entry.RequestModelName) == "" {
			continue
		}
		summary := result[key]
		if summary == nil {
			summary = &model.SiteModelHistorySummary{Recent: make([]model.SiteModelHistoryEntry, 0, 5)}
			result[key] = summary
		}
		success := strings.TrimSpace(entry.Error) == ""
		if success {
			summary.SuccessCount++
		} else {
			summary.FailureCount++
		}
		if summary.LastRequestAt == nil || entry.Time > *summary.LastRequestAt {
			t := entry.Time
			summary.LastRequestAt = &t
		}
		if len(summary.Recent) < 5 {
			summary.Recent = append(summary.Recent, model.SiteModelHistoryEntry{
				Time:         entry.Time,
				Success:      success,
				RouteType:    routeType,
				ChannelID:    channelID,
				ChannelName:  entry.ChannelName,
				RequestModel: entry.RequestModelName,
				ActualModel:  entry.ActualModelName,
				Error:        entry.Error,
			})
		}
	}
	return result, nil
}

func buildSiteChannelCard(ctx context.Context, site model.Site) (model.SiteChannelCard, error) {
	card := model.SiteChannelCard{
		SiteID:       site.ID,
		SiteName:     site.Name,
		Platform:     site.Platform,
		Enabled:      site.Enabled,
		AccountCount: len(site.Accounts),
		Accounts:     make([]model.SiteChannelAccount, 0, len(site.Accounts)),
	}
	historyByAccount := make(map[int]map[string]*model.SiteModelHistorySummary)
	for _, account := range site.Accounts {
		history, err := siteChannelModelHistoryForAccount(ctx, site, account)
		if err == nil {
			historyByAccount[account.ID] = history
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
		groups[key] = &model.SiteChannelGroup{GroupKey: key, GroupName: model.NormalizeSiteGroupName(key, group.Name), ProjectedChannelIDs: make([]int, 0), ProjectedKeys: make([]model.SiteProjectedKey, 0), Models: make([]model.SiteChannelModel, 0)}
	}
	for _, token := range account.Tokens {
		key := model.NormalizeSiteGroupKey(token.GroupKey)
		group := ensureSiteChannelGroup(groups, key, token.GroupName)
		group.KeyCount++
		if token.Enabled {
			group.EnabledKeyCount++
		}
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
	item := &model.SiteChannelGroup{GroupKey: groupKey, GroupName: model.NormalizeSiteGroupName(groupKey, groupName), ProjectedChannelIDs: make([]int, 0), ProjectedKeys: make([]model.SiteProjectedKey, 0), Models: make([]model.SiteChannelModel, 0)}
	groups[groupKey] = item
	return item
}

func UpdateSiteProjectedKeys(siteID int, accountID int, req *model.SiteProjectedKeyUpdateRequest, ctx context.Context) error {
	if req == nil {
		return fmt.Errorf("site projected key update request is nil")
	}
	if strings.TrimSpace(req.GroupKey) == "" {
		return fmt.Errorf("group key is required")
	}

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

	targetGroupKey := model.NormalizeSiteGroupKey(req.GroupKey)
	split := siteChannelShouldSplitByOutboundType(*site)
	channelIDs := make([]int, 0)
	for _, binding := range account.ChannelBindings {
		baseKey, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
		if model.NormalizeSiteGroupKey(baseKey) != targetGroupKey {
			continue
		}
		channelIDs = append(channelIDs, binding.ChannelID)
	}
	if len(channelIDs) == 0 && !split {
		return fmt.Errorf("projected channel not found for group %s", targetGroupKey)
	}
	if len(channelIDs) == 0 {
		return fmt.Errorf("projected channels not found for group %s", targetGroupKey)
	}

	for _, channelID := range channelIDs {
		channel, getErr := ChannelGet(channelID, ctx)
		if getErr != nil {
			return getErr
		}

		validKeyIDs := make(map[int]struct{}, len(channel.Keys))
		for _, key := range channel.Keys {
			validKeyIDs[key.ID] = struct{}{}
		}

		updateReq := &model.ChannelUpdateRequest{ID: channelID, BypassManagedCheck: true}
		if len(req.KeysToAdd) > 0 {
			updateReq.KeysToAdd = make([]model.ChannelKeyAddRequest, 0, len(req.KeysToAdd))
			for _, item := range req.KeysToAdd {
				normalizedKey := model.NormalizeSiteSyncTokenValue(item.ChannelKey)
				if normalizedKey == "" {
					continue
				}
				updateReq.KeysToAdd = append(updateReq.KeysToAdd, model.ChannelKeyAddRequest{
					Enabled:    item.Enabled,
					ChannelKey: normalizedKey,
					Remark:     strings.TrimSpace(item.Remark),
				})
			}
		}
		if len(req.KeysToUpdate) > 0 {
			updateReq.KeysToUpdate = make([]model.ChannelKeyUpdateRequest, 0, len(req.KeysToUpdate))
			for _, item := range req.KeysToUpdate {
				if _, ok := validKeyIDs[item.ID]; !ok {
					continue
				}
				entry := model.ChannelKeyUpdateRequest{ID: item.ID}
				if item.Enabled != nil {
					entry.Enabled = item.Enabled
				}
				if item.ChannelKey != nil {
					normalized := model.NormalizeSiteSyncTokenValue(*item.ChannelKey)
					entry.ChannelKey = &normalized
				}
				if item.Remark != nil {
					trimmed := strings.TrimSpace(*item.Remark)
					entry.Remark = &trimmed
				}
				if entry.Enabled != nil || entry.ChannelKey != nil || entry.Remark != nil {
					updateReq.KeysToUpdate = append(updateReq.KeysToUpdate, entry)
				}
			}
		}
		if len(req.KeysToDelete) > 0 {
			updateReq.KeysToDelete = make([]int, 0, len(req.KeysToDelete))
			for _, id := range req.KeysToDelete {
				if _, ok := validKeyIDs[id]; ok {
					updateReq.KeysToDelete = append(updateReq.KeysToDelete, id)
				}
			}
		}
		if len(updateReq.KeysToAdd) == 0 && len(updateReq.KeysToUpdate) == 0 && len(updateReq.KeysToDelete) == 0 {
			continue
		}
		if _, updateErr := ChannelUpdate(updateReq, ctx); updateErr != nil {
			return updateErr
		}
	}

	return nil
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
