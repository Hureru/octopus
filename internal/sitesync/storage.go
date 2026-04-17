package sitesync

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

func loadSiteAccount(ctx context.Context, accountID int) (*model.Site, *model.SiteAccount, error) {
	account, err := op.SiteAccountGet(accountID, ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("site account not found")
	}
	siteRecord, err := op.SiteGet(account.SiteID, ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("site not found")
	}
	return siteRecord, account, nil
}

func listChannelBindingsByAccount(ctx context.Context, accountID int) ([]model.SiteChannelBinding, error) {
	var bindings []model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Where("site_account_id = ?", accountID).Order("id ASC").Find(&bindings).Error; err != nil {
		return nil, err
	}
	return bindings, nil
}

func deleteManagedChannelsByAccount(ctx context.Context, accountID int) error {
	bindings, err := listChannelBindingsByAccount(ctx, accountID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if err := op.ChannelDelManaged(binding.ChannelID, ctx); err != nil {
			log.Warnf("failed to delete managed channel %d: %v", binding.ChannelID, err)
		}
	}
	return db.GetDB().WithContext(ctx).Where("site_account_id = ?", accountID).Delete(&model.SiteChannelBinding{}).Error
}

func persistSyncSnapshot(ctx context.Context, accountID int, snapshot *syncSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("sync snapshot is nil")
	}
	now := time.Now()
	persistedPrices := preparePersistedSitePrices(accountID, snapshot.prices, now)
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteUserGroup{}).Error; err != nil {
			return err
		}

		var existingTokens []model.SiteToken
		if err := tx.Where("site_account_id = ?", accountID).Order("id ASC").Find(&existingTokens).Error; err != nil {
			return err
		}

		var existingModels []model.SiteModel
		if err := tx.Where("site_account_id = ?", accountID).Find(&existingModels).Error; err != nil {
			return err
		}
		existingModelMap := make(map[string]model.SiteModel, len(existingModels))
		for _, item := range existingModels {
			key := model.NormalizeSiteGroupKey(item.GroupKey) + "\x00" + strings.TrimSpace(item.ModelName)
			existingModelMap[key] = item
		}

		updatePayload := map[string]any{
			"last_sync_at":      &now,
			"last_sync_status":  snapshot.status,
			"last_sync_message": snapshot.message,
			"balance":           snapshot.balance,
			"balance_used":      snapshot.balanceUsed,
		}
		if strings.TrimSpace(snapshot.accessToken) != "" {
			updatePayload["access_token"] = strings.TrimSpace(snapshot.accessToken)
		}
		if err := tx.Model(&model.SiteAccount{}).Where("id = ?", accountID).Updates(updatePayload).Error; err != nil {
			return err
		}

		for i := range snapshot.groups {
			snapshot.groups[i].SiteAccountID = accountID
		}
		mergedTokens := mergePersistedSiteTokens(accountID, existingTokens, snapshot.tokens, now)
		incomingModels := preparePersistedSyncModels(accountID, snapshot.models, existingModelMap, now)
		finalModels := mergePersistedSiteModelsByGroup(existingModels, incomingModels, snapshot.groupResults)

		if len(snapshot.groups) > 0 {
			if err := tx.Create(&snapshot.groups).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteToken{}).Error; err != nil {
			return err
		}
		if len(mergedTokens) > 0 {
			if err := tx.Create(&mergedTokens).Error; err != nil {
				return err
			}
		}
		if len(finalModels) > 0 {
			if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteModel{}).Error; err != nil {
				return err
			}
			if err := tx.Create(&finalModels).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteModel{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SitePrice{}).Error; err != nil {
			return err
		}
		if len(persistedPrices) > 0 {
			if err := tx.Create(&persistedPrices).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	op.SitePriceCacheReplaceAccount(accountID, persistedPrices)
	return nil
}

// preparePersistedSitePrices 规范化并去重 SitePrice 行，准备批量写入。
func preparePersistedSitePrices(accountID int, prices []model.SitePrice, now time.Time) []model.SitePrice {
	if len(prices) == 0 {
		return nil
	}
	result := make([]model.SitePrice, 0, len(prices))
	seen := make(map[string]struct{}, len(prices))
	for _, p := range prices {
		p.ID = 0
		p.SiteAccountID = accountID
		p.GroupKey = model.NormalizeSiteGroupKey(p.GroupKey)
		p.ModelName = strings.TrimSpace(p.ModelName)
		if p.ModelName == "" {
			continue
		}
		if p.UpdatedAt.IsZero() {
			p.UpdatedAt = now
		}
		key := p.GroupKey + "\x00" + strings.ToLower(p.ModelName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, p)
	}
	return result
}

func preparePersistedSyncModels(accountID int, incoming []model.SiteModel, existingModelMap map[string]model.SiteModel, now time.Time) []model.SiteModel {
	prepared := make([]model.SiteModel, 0, len(incoming))
	for i := range incoming {
		item := incoming[i]
		item.SiteAccountID = accountID
		item.GroupKey = model.NormalizeSiteGroupKey(item.GroupKey)
		key := item.GroupKey + "\x00" + strings.TrimSpace(item.ModelName)
		if existing, ok := existingModelMap[key]; ok {
			item.ID = existing.ID
			item.Disabled = existing.Disabled
			applyPersistedRouteState(&item, &existing, now)
		} else {
			applyPersistedRouteState(&item, nil, now)
		}
		prepared = append(prepared, item)
	}
	return compactPersistedSiteModels(prepared)
}

func mergePersistedSiteModelsByGroup(existing []model.SiteModel, incoming []model.SiteModel, results []siteGroupSyncResult) []model.SiteModel {
	replaceGroups := make(map[string]struct{})
	for _, result := range results {
		switch result.Status {
		case siteGroupSyncStatusSynced, siteGroupSyncStatusEmpty, siteGroupSyncStatusRemoved:
			replaceGroups[model.NormalizeSiteGroupKey(result.GroupKey)] = struct{}{}
		}
	}

	merged := make([]model.SiteModel, 0, len(existing)+len(incoming))
	for _, item := range existing {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		if _, ok := replaceGroups[groupKey]; ok {
			continue
		}
		item.GroupKey = groupKey
		item.ModelName = strings.TrimSpace(item.ModelName)
		if item.ModelName == "" {
			continue
		}
		merged = append(merged, item)
	}
	merged = append(merged, incoming...)
	return compactPersistedSiteModels(merged)
}

func mergePersistedSiteTokens(accountID int, existingTokens []model.SiteToken, incomingTokens []model.SiteToken, now time.Time) []model.SiteToken {
	preparedExisting := make([]model.SiteToken, 0, len(existingTokens))
	for _, token := range existingTokens {
		token.SiteAccountID = accountID
		token.GroupKey = model.NormalizeSiteGroupKey(token.GroupKey)
		token.GroupName = model.NormalizeSiteGroupName(token.GroupKey, token.GroupName)
		token.Token = strings.TrimSpace(token.Token)
		token.ValueStatus = model.NormalizeSiteTokenValueStatus(token.ValueStatus, token.Token)
		if token.ValueStatus == model.SiteTokenValueStatusMaskedPending {
			token.Enabled = false
			token.IsDefault = false
		}
		preparedExisting = append(preparedExisting, token)
	}

	readyCandidates := make([]model.SiteToken, 0)
	for _, token := range preparedExisting {
		if !model.IsReadySiteToken(token) || model.IsMaskedSiteTokenValue(token.Token) {
			continue
		}
		readyCandidates = append(readyCandidates, token)
	}

	result := make([]model.SiteToken, 0, len(incomingTokens)+len(preparedExisting))
	usedExistingIDs := make(map[int]struct{}, len(preparedExisting))

	for _, incoming := range incomingTokens {
		incoming.SiteAccountID = accountID
		incoming.GroupKey = model.NormalizeSiteGroupKey(incoming.GroupKey)
		incoming.GroupName = model.NormalizeSiteGroupName(incoming.GroupKey, incoming.GroupName)
		incoming.Token = strings.TrimSpace(incoming.Token)
		incoming.LastSyncAt = &now

		var merged model.SiteToken
		if model.IsMaskedSiteTokenValue(incoming.Token) {
			merged = mergeMaskedIncomingSiteToken(incoming, preparedExisting, readyCandidates, usedExistingIDs)
		} else {
			merged = mergeReadyIncomingSiteToken(incoming, preparedExisting, usedExistingIDs)
		}
		merged.SiteAccountID = accountID
		merged.LastSyncAt = &now
		merged.ValueStatus = model.NormalizeSiteTokenValueStatus(merged.ValueStatus, merged.Token)
		if merged.ValueStatus == model.SiteTokenValueStatusMaskedPending {
			merged.Enabled = false
			merged.IsDefault = false
		}
		result = append(result, merged)
	}

	for _, existing := range preparedExisting {
		if existing.ID != 0 {
			if _, used := usedExistingIDs[existing.ID]; used {
				continue
			}
		}
		if strings.TrimSpace(existing.Source) != "manual" {
			continue
		}
		existing.LastSyncAt = &now
		result = append(result, existing)
	}

	sort.SliceStable(result, func(i, j int) bool {
		if result[i].GroupKey == result[j].GroupKey {
			if result[i].Name == result[j].Name {
				return result[i].ID < result[j].ID
			}
			return result[i].Name < result[j].Name
		}
		return result[i].GroupKey < result[j].GroupKey
	})

	for i := range result {
		result[i].ID = 0
	}

	return result
}

func mergeReadyIncomingSiteToken(incoming model.SiteToken, existingTokens []model.SiteToken, usedExistingIDs map[int]struct{}) model.SiteToken {
	incoming.ValueStatus = model.SiteTokenValueStatusReady
	for _, existing := range existingTokens {
		if existing.ID != 0 {
			if _, used := usedExistingIDs[existing.ID]; used {
				continue
			}
		}
		if !sameComparableSiteTokenValue(existing.Token, incoming.Token) {
			continue
		}
		if model.NormalizeSiteGroupKey(existing.GroupKey) != incoming.GroupKey {
			continue
		}
		incoming.ID = existing.ID
		incomingsToken := strings.TrimSpace(incoming.Token)
		existingToken := strings.TrimSpace(existing.Token)
		if existingToken != "" && existingToken != incomingsToken {
			incoming.Token = existingToken
		}
		if existing.ID != 0 {
			usedExistingIDs[existing.ID] = struct{}{}
		}
		return incoming
	}
	for _, existing := range existingTokens {
		if existing.ID != 0 {
			if _, used := usedExistingIDs[existing.ID]; used {
				continue
			}
		}
		if strings.TrimSpace(existing.Source) == "manual" {
			continue
		}
		if normalizeSiteTokenName(existing.Name) != normalizeSiteTokenName(incoming.Name) {
			continue
		}
		if model.NormalizeSiteGroupKey(existing.GroupKey) != incoming.GroupKey {
			continue
		}
		incoming.ID = existing.ID
		if existing.ID != 0 {
			usedExistingIDs[existing.ID] = struct{}{}
		}
		return incoming
	}
	return incoming
}

func mergeMaskedIncomingSiteToken(incoming model.SiteToken, existingTokens []model.SiteToken, readyCandidates []model.SiteToken, usedExistingIDs map[int]struct{}) model.SiteToken {
	incoming.ValueStatus = model.SiteTokenValueStatusMaskedPending

	for _, existing := range existingTokens {
		if existing.ID != 0 {
			if _, used := usedExistingIDs[existing.ID]; used {
				continue
			}
		}
		if normalizeSiteTokenName(existing.Name) != normalizeSiteTokenName(incoming.Name) {
			continue
		}
		if model.NormalizeSiteGroupKey(existing.GroupKey) != incoming.GroupKey {
			continue
		}
		if model.IsReadySiteToken(existing) && !model.IsMaskedSiteTokenValue(existing.Token) && siteMaskedTokenMatches(existing.Token, incoming.Token) {
			incoming.ID = existing.ID
			incoming.Token = existing.Token
			incoming.ValueStatus = model.SiteTokenValueStatusReady
			incoming.Enabled = incoming.Enabled && existing.Enabled
			usedExistingIDs[existing.ID] = struct{}{}
			return incoming
		}
	}

	matches := make([]model.SiteToken, 0, 2)
	for _, existing := range readyCandidates {
		if existing.ID != 0 {
			if _, used := usedExistingIDs[existing.ID]; used {
				continue
			}
		}
		if model.NormalizeSiteGroupKey(existing.GroupKey) != incoming.GroupKey {
			continue
		}
		if normalizeSiteTokenName(incoming.Name) != "" && normalizeSiteTokenName(existing.Name) != normalizeSiteTokenName(incoming.Name) {
			continue
		}
		if !siteMaskedTokenMatches(existing.Token, incoming.Token) {
			continue
		}
		matches = append(matches, existing)
		if len(matches) > 1 {
			break
		}
	}
	if len(matches) == 1 {
		incoming.ID = matches[0].ID
		incoming.Token = matches[0].Token
		incoming.ValueStatus = model.SiteTokenValueStatusReady
		incoming.Enabled = incoming.Enabled && matches[0].Enabled
		usedExistingIDs[matches[0].ID] = struct{}{}
		return incoming
	}

	for _, existing := range existingTokens {
		if existing.ID != 0 {
			if _, used := usedExistingIDs[existing.ID]; used {
				continue
			}
		}
		if normalizeSiteTokenName(existing.Name) != normalizeSiteTokenName(incoming.Name) {
			continue
		}
		if model.NormalizeSiteGroupKey(existing.GroupKey) != incoming.GroupKey {
			continue
		}
		if model.IsReadySiteToken(existing) && !model.IsMaskedSiteTokenValue(existing.Token) {
			incoming.ID = existing.ID
			incoming.Token = existing.Token
			incoming.ValueStatus = model.SiteTokenValueStatusReady
			incoming.Enabled = incoming.Enabled && existing.Enabled
			if existing.ID != 0 {
				usedExistingIDs[existing.ID] = struct{}{}
			}
			return incoming
		}
		incoming.ID = existing.ID
		incomingsToken := strings.TrimSpace(incoming.Token)
		existingToken := strings.TrimSpace(existing.Token)
		if existingToken != "" && existingToken != incomingsToken {
			incoming.Token = existingToken
		}
		incoming.Enabled = false
		incoming.IsDefault = false
		if existing.ID != 0 {
			usedExistingIDs[existing.ID] = struct{}{}
		}
		return incoming
	}

	incoming.Enabled = false
	incoming.IsDefault = false
	return incoming
}

func normalizeSiteTokenName(name string) string {
	return strings.TrimSpace(name)
}

func siteMaskedTokenMatches(fullToken string, maskedToken string) bool {
	normalizedFull := model.NormalizeComparableSiteTokenValue(fullToken)
	normalizedMasked := model.NormalizeComparableSiteTokenValue(maskedToken)
	if normalizedFull == "" || normalizedMasked == "" {
		return false
	}
	if !model.IsMaskedSiteTokenValue(normalizedMasked) {
		return normalizedFull == normalizedMasked
	}
	firstMask := strings.IndexAny(normalizedMasked, "*•")
	if firstMask < 0 {
		return normalizedFull == normalizedMasked
	}
	lastMask := strings.LastIndexAny(normalizedMasked, "*•")
	if lastMask < firstMask {
		return normalizedFull == normalizedMasked
	}
	prefix := normalizedMasked[:firstMask]
	suffix := normalizedMasked[lastMask+1:]
	if prefix == "" && suffix == "" {
		return false
	}
	if len(normalizedFull) < len(prefix)+len(suffix) {
		return false
	}
	if prefix != "" && !strings.HasPrefix(normalizedFull, prefix) {
		return false
	}
	if suffix != "" && !strings.HasSuffix(normalizedFull, suffix) {
		return false
	}
	return true
}

func sameComparableSiteTokenValue(left string, right string) bool {
	normalizedLeft := model.NormalizeComparableSiteTokenValue(left)
	normalizedRight := model.NormalizeComparableSiteTokenValue(right)
	if normalizedLeft == "" || normalizedRight == "" {
		return false
	}
	return normalizedLeft == normalizedRight
}

func compactPersistedSiteModels(items []model.SiteModel) []model.SiteModel {
	if len(items) <= 1 {
		return items
	}
	seen := make(map[string]int, len(items))
	result := make([]model.SiteModel, 0, len(items))
	for _, item := range items {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		modelName := strings.TrimSpace(item.ModelName)
		if modelName == "" {
			continue
		}
		item.GroupKey = groupKey
		item.ModelName = modelName
		key := groupKey + "\x00" + modelName
		if index, ok := seen[key]; ok {
			// Keep the row with stronger persisted state if duplicates slip through.
			if result[index].ManualOverride || result[index].RouteSource == model.SiteModelRouteSourceRuntimeLearned {
				continue
			}
			if item.ManualOverride || item.RouteSource == model.SiteModelRouteSourceRuntimeLearned {
				result[index] = item
			}
			continue
		}
		seen[key] = len(result)
		result = append(result, item)
	}
	return result
}

func inferSiteModelRouteType(item model.SiteModel) model.SiteModelRouteType {
	return model.InferSiteModelRouteType(item.ModelName)
}

func applyPersistedRouteState(item *model.SiteModel, existing *model.SiteModel, now time.Time) {
	if item == nil {
		return
	}

	if existing != nil && (existing.ManualOverride || existing.RouteSource == model.SiteModelRouteSourceRuntimeLearned) {
		item.RouteType = model.NormalizeSiteModelRouteType(existing.RouteType)
		item.RouteSource = model.NormalizeSiteModelRouteSource(existing.RouteSource, existing.ManualOverride)
		item.ManualOverride = existing.ManualOverride
		item.RouteRawPayload = existing.RouteRawPayload
		item.RouteUpdatedAt = existing.RouteUpdatedAt
		return
	}

	if routeType, routeRawPayload, explicit := resolveExplicitSyncRoute(item, existing); explicit {
		item.RouteType = routeType
		item.RouteSource = model.SiteModelRouteSourceSyncInferred
		item.ManualOverride = false
		item.RouteRawPayload = routeRawPayload
		if existing != nil &&
			model.NormalizeSiteModelRouteType(existing.RouteType) == routeType &&
			strings.TrimSpace(existing.RouteRawPayload) == strings.TrimSpace(routeRawPayload) &&
			!existing.ManualOverride &&
			existing.RouteSource == model.SiteModelRouteSourceSyncInferred {
			item.RouteUpdatedAt = existing.RouteUpdatedAt
			return
		}
		item.RouteUpdatedAt = &now
		return
	}

	item.RouteType = inferSiteModelRouteType(*item)
	item.RouteSource = model.SiteModelRouteSourceSyncInferred
	item.ManualOverride = false
	item.RouteRawPayload = ""
	if existing != nil &&
		model.NormalizeSiteModelRouteType(existing.RouteType) == item.RouteType &&
		strings.TrimSpace(existing.RouteRawPayload) == "" &&
		!existing.ManualOverride &&
		existing.RouteSource == model.SiteModelRouteSourceSyncInferred {
		item.RouteUpdatedAt = existing.RouteUpdatedAt
		return
	}
	item.RouteUpdatedAt = &now
}

func resolveExplicitSyncRoute(item *model.SiteModel, existing *model.SiteModel) (model.SiteModelRouteType, string, bool) {
	if item != nil {
		if metadata, ok := model.ParseSiteModelRouteMetadata(item.RouteRawPayload); ok {
			return metadata.RouteType, item.RouteRawPayload, true
		}
		if strings.TrimSpace(string(item.RouteType)) != "" {
			routeType := model.NormalizeSiteModelRouteType(item.RouteType)
			return routeType, strings.TrimSpace(item.RouteRawPayload), true
		}
	}
	if existing != nil {
		if metadata, ok := model.ParseSiteModelRouteMetadata(existing.RouteRawPayload); ok {
			return metadata.RouteType, existing.RouteRawPayload, true
		}
	}
	return "", "", false
}

func updateAccountSyncState(ctx context.Context, accountID int, status model.SiteExecutionStatus, message string, accessToken string) error {
	now := time.Now()
	updatePayload := map[string]any{
		"last_sync_at":      &now,
		"last_sync_status":  status,
		"last_sync_message": message,
	}
	if strings.TrimSpace(accessToken) != "" {
		updatePayload["access_token"] = strings.TrimSpace(accessToken)
	}
	return db.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", accountID).Updates(updatePayload).Error
}

func updateAccountCheckinState(ctx context.Context, account *model.SiteAccount, status model.SiteExecutionStatus, message string, success bool, accessToken string) error {
	if account == nil {
		return fmt.Errorf("site account is nil")
	}
	now := time.Now()
	updatePayload := map[string]any{
		"last_checkin_at":      &now,
		"last_checkin_status":  status,
		"last_checkin_message": message,
	}
	account.LastCheckinAt = &now
	account.LastCheckinStatus = status
	if success {
		nextAt := buildNextRandomCheckinAt(account, now)
		account.NextAutoCheckinAt = nextAt
		updatePayload["next_auto_checkin_at"] = nextAt
	} else if !account.Enabled || !account.AutoCheckin || !account.RandomCheckin {
		account.NextAutoCheckinAt = nil
		updatePayload["next_auto_checkin_at"] = nil
	}
	if strings.TrimSpace(accessToken) != "" {
		updatePayload["access_token"] = strings.TrimSpace(accessToken)
	}
	return db.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(updatePayload).Error
}
