package op

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func SiteList(ctx context.Context) ([]model.Site, error) {
	var sites []model.Site
	if err := db.GetDB().WithContext(ctx).
		Preload("Accounts").
		Preload("Accounts.Tokens").
		Preload("Accounts.UserGroups").
		Preload("Accounts.Models").
		Preload("Accounts.ChannelBindings").
		Order("is_pinned DESC, sort_order ASC, id ASC").
		Find(&sites).Error; err != nil {
		return nil, err
	}
	return sites, nil
}

func SiteGet(id int, ctx context.Context) (*model.Site, error) {
	var site model.Site
	if err := db.GetDB().WithContext(ctx).
		Preload("Accounts").
		Preload("Accounts.Tokens").
		Preload("Accounts.UserGroups").
		Preload("Accounts.Models").
		Preload("Accounts.ChannelBindings").
		First(&site, id).Error; err != nil {
		return nil, err
	}
	return &site, nil
}

func SiteCreate(site *model.Site, ctx context.Context) error {
	if site == nil {
		return fmt.Errorf("site is nil")
	}
	if err := site.Validate(); err != nil {
		return err
	}
	return db.GetDB().WithContext(ctx).Create(site).Error
}

func SiteUpdate(req *model.SiteUpdateRequest, ctx context.Context) (*model.Site, error) {
	if req == nil {
		return nil, fmt.Errorf("site update request is nil")
	}
	var site model.Site
	if err := db.GetDB().WithContext(ctx).First(&site, req.ID).Error; err != nil {
		return nil, fmt.Errorf("site not found")
	}

	merged := site
	var selectFields []string
	updates := model.Site{ID: req.ID}

	if req.Name != nil {
		merged.Name = *req.Name
		selectFields = append(selectFields, "name")
	}
	if req.Platform != nil {
		merged.Platform = *req.Platform
		selectFields = append(selectFields, "platform")
	}
	if req.BaseURL != nil {
		merged.BaseURL = *req.BaseURL
		selectFields = append(selectFields, "base_url")
	}
	if req.Enabled != nil {
		merged.Enabled = *req.Enabled
		selectFields = append(selectFields, "enabled")
	}
	if req.Proxy != nil {
		merged.Proxy = *req.Proxy
		selectFields = append(selectFields, "proxy")
	}
	if req.SiteProxy != nil {
		merged.SiteProxy = req.SiteProxy
		selectFields = append(selectFields, "site_proxy")
	}
	if req.UseSystemProxy != nil {
		merged.UseSystemProxy = *req.UseSystemProxy
		selectFields = append(selectFields, "use_system_proxy")
	}
	if req.ExternalCheckinURL != nil {
		merged.ExternalCheckinURL = req.ExternalCheckinURL
		selectFields = append(selectFields, "external_checkin_url")
	}
	if req.IsPinned != nil {
		merged.IsPinned = *req.IsPinned
		selectFields = append(selectFields, "is_pinned")
	}
	if req.SortOrder != nil {
		merged.SortOrder = *req.SortOrder
		selectFields = append(selectFields, "sort_order")
	}
	if req.GlobalWeight != nil {
		merged.GlobalWeight = *req.GlobalWeight
		selectFields = append(selectFields, "global_weight")
	}
	if req.CustomHeader != nil {
		merged.CustomHeader = *req.CustomHeader
		selectFields = append(selectFields, "custom_header")
	}
	if len(selectFields) > 0 {
		if err := merged.Validate(); err != nil {
			return nil, err
		}
	}
	if req.Name != nil {
		updates.Name = merged.Name
	}
	if req.Platform != nil {
		updates.Platform = merged.Platform
	}
	if req.BaseURL != nil {
		updates.BaseURL = merged.BaseURL
	}
	if req.Enabled != nil {
		updates.Enabled = merged.Enabled
	}
	if req.Proxy != nil {
		updates.Proxy = merged.Proxy
	}
	if req.SiteProxy != nil {
		updates.SiteProxy = merged.SiteProxy
	}
	if req.UseSystemProxy != nil {
		updates.UseSystemProxy = merged.UseSystemProxy
	}
	if req.ExternalCheckinURL != nil {
		updates.ExternalCheckinURL = merged.ExternalCheckinURL
	}
	if req.IsPinned != nil {
		updates.IsPinned = merged.IsPinned
	}
	if req.SortOrder != nil {
		updates.SortOrder = merged.SortOrder
	}
	if req.GlobalWeight != nil {
		updates.GlobalWeight = merged.GlobalWeight
	}
	if req.CustomHeader != nil {
		updates.CustomHeader = merged.CustomHeader
	}
	if len(selectFields) > 0 {
		if err := db.GetDB().WithContext(ctx).
			Model(&model.Site{}).
			Where("id = ?", req.ID).
			Select(selectFields).
			Updates(&updates).Error; err != nil {
			return nil, fmt.Errorf("failed to update site: %w", err)
		}
	}
	return SiteGet(req.ID, ctx)
}

func SiteEnabled(id int, enabled bool, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Site{}).Where("id = ?", id).Update("enabled", enabled).Error; err != nil {
			return err
		}
		return tx.Model(&model.SiteAccount{}).Where("site_id = ?", id).Update("enabled", enabled).Error
	})
}

func SiteDel(id int, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var accountIDs []int
		if err := tx.Model(&model.SiteAccount{}).Where("site_id = ?", id).Pluck("id", &accountIDs).Error; err != nil {
			return err
		}
		if len(accountIDs) > 0 {
			if err := tx.Where("site_account_id IN ?", accountIDs).Delete(&model.SiteToken{}).Error; err != nil {
				return err
			}
			if err := tx.Where("site_account_id IN ?", accountIDs).Delete(&model.SiteUserGroup{}).Error; err != nil {
				return err
			}
			if err := tx.Where("site_account_id IN ?", accountIDs).Delete(&model.SiteModel{}).Error; err != nil {
				return err
			}
			if err := tx.Where("site_account_id IN ?", accountIDs).Delete(&model.SiteChannelBinding{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", accountIDs).Delete(&model.SiteAccount{}).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&model.Site{}, id).Error
	})
}

func SiteAccountGet(id int, ctx context.Context) (*model.SiteAccount, error) {
	var account model.SiteAccount
	if err := db.GetDB().WithContext(ctx).
		Preload("Tokens").
		Preload("UserGroups").
		Preload("Models").
		Preload("ChannelBindings").
		First(&account, id).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func SiteAccountCreate(account *model.SiteAccount, ctx context.Context) error {
	if account == nil {
		return fmt.Errorf("site account is nil")
	}
	if err := account.Validate(); err != nil {
		return err
	}
	return db.GetDB().WithContext(ctx).Create(account).Error
}

func SiteAccountUpdate(req *model.SiteAccountUpdateRequest, ctx context.Context) (*model.SiteAccount, error) {
	if req == nil {
		return nil, fmt.Errorf("site account update request is nil")
	}

	var account model.SiteAccount
	if err := db.GetDB().WithContext(ctx).First(&account, req.ID).Error; err != nil {
		return nil, fmt.Errorf("site account not found")
	}

	merged := account
	var selectFields []string
	updates := model.SiteAccount{ID: req.ID}

	if req.Name != nil {
		merged.Name = *req.Name
		selectFields = append(selectFields, "name")
	}
	if req.CredentialType != nil {
		merged.CredentialType = *req.CredentialType
		selectFields = append(selectFields, "credential_type")
	}
	if req.Username != nil {
		merged.Username = *req.Username
		selectFields = append(selectFields, "username")
	}
	if req.Password != nil {
		merged.Password = *req.Password
		selectFields = append(selectFields, "password")
	}
	if req.AccessToken != nil {
		merged.AccessToken = *req.AccessToken
		selectFields = append(selectFields, "access_token")
	}
	if req.APIKey != nil {
		merged.APIKey = *req.APIKey
		selectFields = append(selectFields, "api_key")
	}
	if req.RefreshToken != nil {
		merged.RefreshToken = *req.RefreshToken
		selectFields = append(selectFields, "refresh_token")
	}
	if req.TokenExpiresAt != nil {
		merged.TokenExpiresAt = *req.TokenExpiresAt
		selectFields = append(selectFields, "token_expires_at")
	}
	if req.PlatformUserID != nil {
		merged.PlatformUserID = req.PlatformUserID
		selectFields = append(selectFields, "platform_user_id")
	}
	if req.AccountProxy != nil {
		merged.AccountProxy = req.AccountProxy
		selectFields = append(selectFields, "account_proxy")
	}
	if req.Enabled != nil {
		merged.Enabled = *req.Enabled
		selectFields = append(selectFields, "enabled")
	}
	if req.AutoSync != nil {
		merged.AutoSync = *req.AutoSync
		selectFields = append(selectFields, "auto_sync")
	}
	if req.AutoCheckin != nil {
		merged.AutoCheckin = *req.AutoCheckin
		selectFields = append(selectFields, "auto_checkin")
	}
	if req.RandomCheckin != nil {
		merged.RandomCheckin = *req.RandomCheckin
		selectFields = append(selectFields, "random_checkin")
	}
	if req.CheckinIntervalHours != nil {
		merged.CheckinIntervalHours = *req.CheckinIntervalHours
		selectFields = append(selectFields, "checkin_interval_hours")
	}
	if req.CheckinRandomWindowMinutes != nil {
		merged.CheckinRandomWindowMinutes = *req.CheckinRandomWindowMinutes
		selectFields = append(selectFields, "checkin_random_window_minutes")
	}

	if len(selectFields) > 0 {
		if err := merged.Validate(); err != nil {
			return nil, err
		}
	}
	if req.Name != nil {
		updates.Name = merged.Name
	}
	if req.CredentialType != nil {
		updates.CredentialType = merged.CredentialType
	}
	if req.Username != nil {
		updates.Username = merged.Username
	}
	if req.Password != nil {
		updates.Password = merged.Password
	}
	if req.AccessToken != nil {
		updates.AccessToken = merged.AccessToken
	}
	if req.APIKey != nil {
		updates.APIKey = merged.APIKey
	}
	if req.RefreshToken != nil {
		updates.RefreshToken = merged.RefreshToken
	}
	if req.TokenExpiresAt != nil {
		updates.TokenExpiresAt = merged.TokenExpiresAt
	}
	if req.PlatformUserID != nil {
		updates.PlatformUserID = merged.PlatformUserID
	}
	if req.AccountProxy != nil {
		updates.AccountProxy = merged.AccountProxy
	}
	if req.Enabled != nil {
		updates.Enabled = merged.Enabled
	}
	if req.AutoSync != nil {
		updates.AutoSync = merged.AutoSync
	}
	if req.AutoCheckin != nil {
		updates.AutoCheckin = merged.AutoCheckin
	}
	if req.RandomCheckin != nil {
		updates.RandomCheckin = merged.RandomCheckin
	}
	if req.CheckinIntervalHours != nil {
		updates.CheckinIntervalHours = merged.CheckinIntervalHours
	}
	if req.CheckinRandomWindowMinutes != nil {
		updates.CheckinRandomWindowMinutes = merged.CheckinRandomWindowMinutes
	}

	if len(selectFields) > 0 {
		if err := db.GetDB().WithContext(ctx).
			Model(&model.SiteAccount{}).
			Where("id = ?", req.ID).
			Select(selectFields).
			Updates(&updates).Error; err != nil {
			return nil, fmt.Errorf("failed to update site account: %w", err)
		}
	}
	return SiteAccountGet(req.ID, ctx)
}

func SiteAccountEnabled(id int, enabled bool, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", id).Update("enabled", enabled).Error
}

func SiteAccountDel(id int, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("site_account_id = ?", id).Delete(&model.SiteToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("site_account_id = ?", id).Delete(&model.SiteUserGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Where("site_account_id = ?", id).Delete(&model.SiteModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("site_account_id = ?", id).Delete(&model.SiteChannelBinding{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.SiteAccount{}, id).Error
	})
}

func SiteAvailableModels(siteID int, ctx context.Context) ([]string, error) {
	var rows []model.SiteModel
	if err := db.GetDB().WithContext(ctx).
		Joins("JOIN site_accounts ON site_accounts.id = site_models.site_account_id").
		Where("site_accounts.site_id = ? AND site_models.disabled = ?", siteID, false).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	models := make([]string, 0, len(rows))
	for _, row := range rows {
		trimmed := strings.TrimSpace(row.ModelName)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		models = append(models, trimmed)
	}
	sort.Strings(models)
	return models, nil
}

func SiteModelRouteUpdate(accountID int, groupKey string, modelName string, routeType model.SiteModelRouteType, source model.SiteModelRouteSource, manualOverride bool, routeRawPayload string, ctx context.Context) error {
	now := time.Now()
	updates := map[string]any{
		"route_type":        model.NormalizeSiteModelRouteType(routeType),
		"route_source":      model.NormalizeSiteModelRouteSource(source, manualOverride),
		"manual_override":   manualOverride,
		"route_raw_payload": strings.TrimSpace(routeRawPayload),
		"route_updated_at":  &now,
	}
	return db.GetDB().WithContext(ctx).
		Model(&model.SiteModel{}).
		Where("site_account_id = ? AND group_key = ? AND model_name = ?", accountID, model.NormalizeSiteGroupKey(groupKey), strings.TrimSpace(modelName)).
		Updates(updates).Error
}

func SiteModelRouteUpdateIfNotManual(accountID int, groupKey string, modelName string, routeType model.SiteModelRouteType, source model.SiteModelRouteSource, routeRawPayload string, ctx context.Context) (bool, error) {
	now := time.Now()
	updates := map[string]any{
		"route_type":        model.NormalizeSiteModelRouteType(routeType),
		"route_source":      model.NormalizeSiteModelRouteSource(source, false),
		"manual_override":   false,
		"route_raw_payload": strings.TrimSpace(routeRawPayload),
		"route_updated_at":  &now,
	}
	result := db.GetDB().WithContext(ctx).
		Model(&model.SiteModel{}).
		Where("site_account_id = ? AND group_key = ? AND model_name = ? AND manual_override = ?", accountID, model.NormalizeSiteGroupKey(groupKey), strings.TrimSpace(modelName), false).
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func SiteModelDisabledUpdate(accountID int, groupKey string, modelName string, disabled bool, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).
		Model(&model.SiteModel{}).
		Where("site_account_id = ? AND group_key = ? AND model_name = ?", accountID, model.NormalizeSiteGroupKey(groupKey), strings.TrimSpace(modelName)).
		Update("disabled", disabled).Error
}

func SiteUpdateSystemProxy(id int, useSystemProxy bool, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).Update("use_system_proxy", useSystemProxy).Error
}
