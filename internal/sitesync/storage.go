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
		if err := op.ChannelDel(binding.ChannelID, ctx); err != nil {
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
		if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteToken{}).Error; err != nil {
			return err
		}
		if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteUserGroup{}).Error; err != nil {
			return err
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
		}

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
			if err := tx.Create(&snapshot.models).Error; err != nil {
				return err
			}
		}
		return nil
	})
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
		"last_checkin_status":  status,
		"last_checkin_message": message,
	}
	if success {
		updatePayload["last_checkin_at"] = &now
	}
	if success {
		account.LastCheckinAt = &now
		account.LastCheckinStatus = status
	}
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
