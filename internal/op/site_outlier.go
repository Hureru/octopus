package op

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm/clause"
)

// SiteChannelOutlierGet 读取单个渠道的退役状态记录（不存在返回 error）。
func SiteChannelOutlierGet(channelID int, ctx context.Context) (*model.SiteChannelOutlierState, error) {
	var state model.SiteChannelOutlierState
	if err := db.GetDB().WithContext(ctx).Where("channel_id = ?", channelID).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

// SiteChannelOutlierListRetired 列出所有处于 retired 状态的渠道。
func SiteChannelOutlierListRetired(ctx context.Context) ([]model.SiteChannelOutlierState, error) {
	var states []model.SiteChannelOutlierState
	err := db.GetDB().WithContext(ctx).
		Where("status = ?", model.OutlierStatusRetired).
		Find(&states).Error
	return states, err
}

// SiteChannelOutlierIsRetired 供 sitesync 投影钩子判断某渠道是否已被退役。
func SiteChannelOutlierIsRetired(channelID int, ctx context.Context) (bool, error) {
	var count int64
	err := db.GetDB().WithContext(ctx).
		Model(&model.SiteChannelOutlierState{}).
		Where("channel_id = ? AND status = ?", channelID, model.OutlierStatusRetired).
		Count(&count).Error
	return count > 0, err
}

// SiteChannelOutlierRetire 退役一个渠道（upsert，按 channel_id 去重）。
func SiteChannelOutlierRetire(channelID, siteAccountID int, reason string, cloudflare bool, snap model.OutlierSnapshot, now time.Time, ctx context.Context) error {
	state := model.SiteChannelOutlierState{
		ChannelID:         channelID,
		SiteAccountID:     siteAccountID,
		Status:            model.OutlierStatusRetired,
		Reason:            reason,
		CloudflareBlocked: cloudflare,
		RetiredAt:         &now,
		RecoverStreak:     0,
		Snapshot:          snap,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"site_account_id", "status", "reason", "cloudflare_blocked",
			"retired_at", "recover_streak", "snapshot", "updated_at",
		}),
	}).Create(&state).Error
}

// SiteChannelOutlierMarkProbe 记录一次恢复探活结果。
// 成功累加 RecoverStreak，失败清零；返回 recovered=是否达到恢复阈值。
func SiteChannelOutlierMarkProbe(channelID int, success bool, recoverThreshold int, now time.Time, ctx context.Context) (bool, error) {
	state, err := SiteChannelOutlierGet(channelID, ctx)
	if err != nil {
		return false, err
	}
	if success {
		state.RecoverStreak++
	} else {
		state.RecoverStreak = 0
	}
	updates := map[string]any{
		"recover_streak": state.RecoverStreak,
		"last_probe_at":  now,
		"updated_at":     now,
	}
	if err := db.GetDB().WithContext(ctx).
		Model(&model.SiteChannelOutlierState{}).
		Where("channel_id = ?", channelID).
		Updates(updates).Error; err != nil {
		return false, err
	}
	return success && state.RecoverStreak >= recoverThreshold, nil
}

// SiteChannelOutlierClear 删除退役记录（探活恢复后调用）。
func SiteChannelOutlierClear(channelID int, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).
		Where("channel_id = ?", channelID).
		Delete(&model.SiteChannelOutlierState{}).Error
}
