package op

import (
	"context"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func SiteList(ctx context.Context) ([]model.Site, error) {
	var sites []model.Site
	if err := db.GetDB().WithContext(ctx).
		Preload("Accounts").
		Preload("Accounts.Tokens").
		Preload("Accounts.UserGroups").
		Preload("Accounts.Models").
		Preload("Accounts.ChannelBindings").
		Order("id ASC").
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

	var selectFields []string
	updates := model.Site{ID: req.ID}

	if req.Name != nil {
		selectFields = append(selectFields, "name")
		updates.Name = strings.TrimSpace(*req.Name)
	}
	if req.Platform != nil {
		if err := req.Platform.Validate(); err != nil {
			return nil, err
		}
		selectFields = append(selectFields, "platform")
		updates.Platform = *req.Platform
	}
	if req.BaseURL != nil {
		baseURL := strings.TrimRight(strings.TrimSpace(*req.BaseURL), "/")
		selectFields = append(selectFields, "base_url")
		updates.BaseURL = baseURL
	}
	if req.Enabled != nil {
		selectFields = append(selectFields, "enabled")
		updates.Enabled = *req.Enabled
	}
	if req.Proxy != nil {
		selectFields = append(selectFields, "proxy")
		updates.Proxy = *req.Proxy
	}
	if req.SiteProxy != nil {
		selectFields = append(selectFields, "site_proxy")
		trimmed := strings.TrimSpace(*req.SiteProxy)
		if trimmed != "" {
			updates.SiteProxy = &trimmed
		}
	}
	if req.CustomHeader != nil {
		selectFields = append(selectFields, "custom_header")
		updates.CustomHeader = *req.CustomHeader
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
	return db.GetDB().WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).Update("enabled", enabled).Error
}

func SiteDel(id int, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Delete(&model.Site{}, id).Error
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

	var selectFields []string
	updates := model.SiteAccount{ID: req.ID}

	if req.Name != nil {
		selectFields = append(selectFields, "name")
		updates.Name = strings.TrimSpace(*req.Name)
	}
	if req.CredentialType != nil {
		if err := req.CredentialType.Validate(); err != nil {
			return nil, err
		}
		selectFields = append(selectFields, "credential_type")
		updates.CredentialType = *req.CredentialType
	}
	if req.Username != nil {
		selectFields = append(selectFields, "username")
		updates.Username = strings.TrimSpace(*req.Username)
	}
	if req.Password != nil {
		selectFields = append(selectFields, "password")
		updates.Password = strings.TrimSpace(*req.Password)
	}
	if req.AccessToken != nil {
		selectFields = append(selectFields, "access_token")
		updates.AccessToken = strings.TrimSpace(*req.AccessToken)
	}
	if req.APIKey != nil {
		selectFields = append(selectFields, "api_key")
		updates.APIKey = strings.TrimSpace(*req.APIKey)
	}
	if req.Enabled != nil {
		selectFields = append(selectFields, "enabled")
		updates.Enabled = *req.Enabled
	}
	if req.AutoSync != nil {
		selectFields = append(selectFields, "auto_sync")
		updates.AutoSync = *req.AutoSync
	}
	if req.AutoCheckin != nil {
		selectFields = append(selectFields, "auto_checkin")
		updates.AutoCheckin = *req.AutoCheckin
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
	return db.GetDB().WithContext(ctx).Delete(&model.SiteAccount{}, id).Error
}
