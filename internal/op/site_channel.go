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
			Groups:      buildSiteChannelGroups(site, account, historyByAccount[account.ID]),
		}
		view.GroupCount = len(view.Groups)
		view.ModelCount = countSiteChannelModels(view.Groups)
		view.RouteSummaries = summarizeSiteRoutes(view.Groups)
		card.Accounts = append(card.Accounts, view)
	}
	return card, nil
}

func buildSiteChannelGroups(site model.Site, account model.SiteAccount, historyMap map[string]*model.SiteModelHistorySummary) []model.SiteChannelGroup {
	split := siteChannelShouldSplitByOutboundType(site)
	groups := make(map[string]*model.SiteChannelGroup)
	for _, group := range account.UserGroups {
		key := model.NormalizeSiteGroupKey(group.GroupKey)
		groups[key] = &model.SiteChannelGroup{GroupKey: key, GroupName: model.NormalizeSiteGroupName(key, group.Name), ProjectedChannelIDs: make([]int, 0), Models: make([]model.SiteChannelModel, 0)}
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
	}
	for _, item := range account.Models {
		key := model.NormalizeSiteGroupKey(item.GroupKey)
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
		sort.Slice(item.Models, func(i, j int) bool { return item.Models[i].ModelName < item.Models[j].ModelName })
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].GroupKey < result[j].GroupKey })
	return result
}

func ensureSiteChannelGroup(groups map[string]*model.SiteChannelGroup, groupKey string, groupName string) *model.SiteChannelGroup {
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	if item, ok := groups[groupKey]; ok {
		if strings.TrimSpace(item.GroupName) == "" {
			item.GroupName = model.NormalizeSiteGroupName(groupKey, groupName)
		}
		return item
	}
	item := &model.SiteChannelGroup{GroupKey: groupKey, GroupName: model.NormalizeSiteGroupName(groupKey, groupName), ProjectedChannelIDs: make([]int, 0), Models: make([]model.SiteChannelModel, 0)}
	groups[groupKey] = item
	return item
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
