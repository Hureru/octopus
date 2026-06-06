package sitesync

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

func MarkAccountProjectionStale(ctx context.Context, accountID int, reason string) error {
	message := sanitizeSiteStatusText(reason)
	if message == "" {
		message = "站点账号同步失败，已沿用历史投影"
	}
	now := time.Now()
	if err := ensureStaleAccountGroups(ctx, accountID, message, now); err != nil {
		return err
	}
	return db.GetDB().WithContext(ctx).Model(&model.SiteUserGroup{}).
		Where("site_account_id = ? AND projection_suspended = ?", accountID, false).
		Updates(map[string]any{
			"model_sync_status":        model.SiteGroupModelSyncStatusStale,
			"model_sync_message":       message,
			"model_sync_authoritative": false,
			"last_model_sync_at":       &now,
			"model_sync_failure_count": gorm.Expr("COALESCE(model_sync_failure_count, 0) + 1"),
		}).Error
}

func ensureStaleAccountGroups(ctx context.Context, accountID int, message string, now time.Time) error {
	return ensureAccountGroups(ctx, accountID, func(groupKey string, groupName string) model.SiteUserGroup {
		return model.SiteUserGroup{
			SiteAccountID:          accountID,
			GroupKey:               groupKey,
			Name:                   model.NormalizeSiteGroupName(groupKey, groupName),
			ModelSyncStatus:        model.SiteGroupModelSyncStatusStale,
			ModelSyncMessage:       message,
			ModelSyncAuthoritative: false,
			LastModelSyncAt:        &now,
		}
	})
}

func ensureAccountGroups(ctx context.Context, accountID int, buildRow func(groupKey string, groupName string) model.SiteUserGroup) error {
	account, err := op.SiteAccountGet(accountID, ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{})
	rows := make([]model.SiteUserGroup, 0)
	for _, group := range account.UserGroups {
		seen[model.NormalizeSiteGroupKey(group.GroupKey)] = struct{}{}
	}
	addGroup := func(groupKey string, groupName string) {
		groupKey = model.NormalizeSiteGroupKey(groupKey)
		if _, ok := seen[groupKey]; ok {
			return
		}
		seen[groupKey] = struct{}{}
		rows = append(rows, buildRow(groupKey, groupName))
	}
	for _, token := range account.Tokens {
		addGroup(token.GroupKey, token.GroupName)
	}
	for _, item := range account.Models {
		addGroup(item.GroupKey, "")
	}
	if len(seen) == 0 && len(rows) == 0 {
		addGroup(model.SiteDefaultGroupKey, model.SiteDefaultGroupName)
	}
	if len(rows) == 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).Create(&rows).Error
}
