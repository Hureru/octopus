package sitesync

import (
	"context"
	"fmt"
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
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteUserGroup{}).Error; err != nil {
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
			"last_sync_status":  model.SiteExecutionStatusSuccess,
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
		for i := range snapshot.tokens {
			snapshot.tokens[i].SiteAccountID = accountID
			snapshot.tokens[i].LastSyncAt = &now
		}
		for i := range snapshot.models {
			snapshot.models[i].SiteAccountID = accountID
			snapshot.models[i].GroupKey = model.NormalizeSiteGroupKey(snapshot.models[i].GroupKey)
			key := snapshot.models[i].GroupKey + "\x00" + strings.TrimSpace(snapshot.models[i].ModelName)
			if existing, ok := existingModelMap[key]; ok {
				snapshot.models[i].ID = existing.ID
				snapshot.models[i].Disabled = existing.Disabled
				applyPersistedRouteState(&snapshot.models[i], &existing, now)
				continue
			}
			applyPersistedRouteState(&snapshot.models[i], nil, now)
		}
		snapshot.models = compactPersistedSiteModels(snapshot.models)

		if len(snapshot.groups) > 0 {
			if err := tx.Create(&snapshot.groups).Error; err != nil {
				return err
			}
		}
		if len(snapshot.tokens) > 0 {
			if err := tx.Create(&snapshot.tokens).Error; err != nil {
				return err
			}
		}
		if len(snapshot.models) > 0 {
			if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteModel{}).Error; err != nil {
				return err
			}
			if err := tx.Create(&snapshot.models).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteModel{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
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
