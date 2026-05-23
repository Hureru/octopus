package op

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

// GroupPresetList 列出某 Group 下的所有预设
func GroupPresetList(groupID int, ctx context.Context) ([]model.GroupPreset, error) {
	if _, ok := groupCache.Get(groupID); !ok {
		return nil, fmt.Errorf("group not found")
	}
	var presets []model.GroupPreset
	if err := db.GetDB().WithContext(ctx).
		Where("group_id = ?", groupID).
		Order("id ASC").
		Find(&presets).Error; err != nil {
		return nil, fmt.Errorf("failed to list presets: %w", err)
	}
	return presets, nil
}

// groupPresetSnapshotFromCache 从缓存中的 Group 取当前实时状态的快照
func groupPresetSnapshotFromCache(groupID int) (mode model.GroupMode, matchRegex string, firstTokenTimeOut, sessionKeepTime, maxRetries int, retryEnabled bool, items []model.GroupPresetItem, err error) {
	group, ok := groupCache.Get(groupID)
	if !ok {
		err = fmt.Errorf("group not found")
		return
	}
	mode = group.Mode
	matchRegex = group.MatchRegex
	firstTokenTimeOut = group.FirstTokenTimeOut
	sessionKeepTime = group.SessionKeepTime
	maxRetries = group.MaxRetries
	retryEnabled = group.RetryEnabled
	items = make([]model.GroupPresetItem, 0, len(group.Items))
	for _, it := range group.Items {
		items = append(items, model.GroupPresetItem{
			ChannelID: it.ChannelID,
			ModelName: it.ModelName,
			Priority:  it.Priority,
			Weight:    it.Weight,
		})
	}
	return
}

// GroupPresetCreate 抓取 Group 当前 Mode + Items + 其他路由参数快照成新预设
func GroupPresetCreate(groupID int, name string, ctx context.Context) (*model.GroupPreset, error) {
	if name == "" {
		return nil, fmt.Errorf("preset name required")
	}
	mode, matchRegex, fto, skt, mr, re, items, err := groupPresetSnapshotFromCache(groupID)
	if err != nil {
		return nil, err
	}
	preset := model.GroupPreset{
		GroupID:           groupID,
		Name:              name,
		Mode:              mode,
		MatchRegex:        matchRegex,
		FirstTokenTimeOut: fto,
		SessionKeepTime:   skt,
		RetryEnabled:      re,
		MaxRetries:        mr,
		Items:             items,
	}
	if err := db.GetDB().WithContext(ctx).Create(&preset).Error; err != nil {
		return nil, fmt.Errorf("failed to create preset: %w", err)
	}
	return &preset, nil
}

// GroupPresetOverwrite 用 Group 当前实时状态覆盖已有预设
func GroupPresetOverwrite(presetID int, ctx context.Context) (*model.GroupPreset, error) {
	var preset model.GroupPreset
	if err := db.GetDB().WithContext(ctx).First(&preset, presetID).Error; err != nil {
		return nil, fmt.Errorf("preset not found")
	}
	mode, matchRegex, fto, skt, mr, re, items, err := groupPresetSnapshotFromCache(preset.GroupID)
	if err != nil {
		return nil, err
	}
	preset.Mode = mode
	preset.MatchRegex = matchRegex
	preset.FirstTokenTimeOut = fto
	preset.SessionKeepTime = skt
	preset.RetryEnabled = re
	preset.MaxRetries = mr
	preset.Items = items
	if err := db.GetDB().WithContext(ctx).Save(&preset).Error; err != nil {
		return nil, fmt.Errorf("failed to overwrite preset: %w", err)
	}
	return &preset, nil
}

// GroupPresetUpdate 直接编辑预设内容（仅允许非活动预设；handler 层先做拦截）
// 不动 group_items、不刷 group 缓存、不重置熔断/粘性
func GroupPresetUpdate(presetID int, req *model.GroupPresetUpdateRequest, ctx context.Context) (*model.GroupPreset, error) {
	var preset model.GroupPreset
	if err := db.GetDB().WithContext(ctx).First(&preset, presetID).Error; err != nil {
		return nil, fmt.Errorf("preset not found")
	}
	if group, ok := groupCache.Get(preset.GroupID); ok {
		if group.ActivePresetID != nil && *group.ActivePresetID == presetID {
			return nil, fmt.Errorf("cannot edit active preset; switch to another preset first")
		}
	}
	if req.Name != nil {
		preset.Name = *req.Name
	}
	if req.Mode != nil {
		preset.Mode = *req.Mode
	}
	if req.MatchRegex != nil {
		preset.MatchRegex = *req.MatchRegex
	}
	if req.FirstTokenTimeOut != nil {
		preset.FirstTokenTimeOut = *req.FirstTokenTimeOut
	}
	if req.SessionKeepTime != nil {
		preset.SessionKeepTime = *req.SessionKeepTime
	}
	if req.RetryEnabled != nil {
		preset.RetryEnabled = *req.RetryEnabled
	}
	if req.MaxRetries != nil {
		v := *req.MaxRetries
		if v <= 0 {
			v = 3
		}
		preset.MaxRetries = v
	}
	if req.Items != nil {
		preset.Items = *req.Items
	}
	if err := db.GetDB().WithContext(ctx).Save(&preset).Error; err != nil {
		return nil, fmt.Errorf("failed to update preset: %w", err)
	}
	return &preset, nil
}

// GroupPresetRename 仅改名
func GroupPresetRename(presetID int, name string, ctx context.Context) error {
	if name == "" {
		return fmt.Errorf("preset name required")
	}
	if err := db.GetDB().WithContext(ctx).
		Model(&model.GroupPreset{}).
		Where("id = ?", presetID).
		Update("name", name).Error; err != nil {
		return fmt.Errorf("failed to rename preset: %w", err)
	}
	return nil
}

// GroupPresetDelete 删除预设。若是当前活动预设则拒绝
func GroupPresetDelete(presetID int, ctx context.Context) error {
	var preset model.GroupPreset
	if err := db.GetDB().WithContext(ctx).First(&preset, presetID).Error; err != nil {
		return fmt.Errorf("preset not found")
	}
	if group, ok := groupCache.Get(preset.GroupID); ok {
		if group.ActivePresetID != nil && *group.ActivePresetID == presetID {
			return fmt.Errorf("cannot delete active preset; switch to another preset first")
		}
	}
	if err := db.GetDB().WithContext(ctx).Delete(&model.GroupPreset{}, presetID).Error; err != nil {
		return fmt.Errorf("failed to delete preset: %w", err)
	}
	return nil
}

// GroupPresetActivate 用预设覆盖 Group 的实时 Mode + Items + 路由参数；写 ActivePresetID
// 校验预设引用的渠道全部存在，否则拒绝
func GroupPresetActivate(presetID int, ctx context.Context) error {
	var preset model.GroupPreset
	if err := db.GetDB().WithContext(ctx).First(&preset, presetID).Error; err != nil {
		return fmt.Errorf("preset not found")
	}
	oldGroup, ok := groupCache.Get(preset.GroupID)
	if !ok {
		return fmt.Errorf("group not found")
	}

	// 校验渠道存在
	missing := make([]int, 0)
	seen := make(map[int]struct{})
	for _, it := range preset.Items {
		if _, dup := seen[it.ChannelID]; dup {
			continue
		}
		seen[it.ChannelID] = struct{}{}
		if _, exists := channelCache.Get(it.ChannelID); !exists {
			missing = append(missing, it.ChannelID)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("preset references missing channels: %v", missing)
	}

	// 收集新旧 channel IDs（供熔断/粘性重置）
	channelIDs := make([]int, 0, len(oldGroup.Items)+len(preset.Items))
	for _, it := range oldGroup.Items {
		channelIDs = append(channelIDs, it.ChannelID)
	}
	for _, it := range preset.Items {
		channelIDs = append(channelIDs, it.ChannelID)
	}

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 清空旧 items
	if err := tx.Where("group_id = ?", preset.GroupID).Delete(&model.GroupItem{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to clear old items: %w", err)
	}

	// 写入预设 items
	if len(preset.Items) > 0 {
		newItems := make([]model.GroupItem, 0, len(preset.Items))
		for _, it := range preset.Items {
			newItems = append(newItems, model.GroupItem{
				GroupID:   preset.GroupID,
				ChannelID: it.ChannelID,
				ModelName: it.ModelName,
				Priority:  it.Priority,
				Weight:    it.Weight,
			})
		}
		if err := tx.Create(&newItems).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert preset items: %w", err)
		}
	}

	// 写回 Group 的实时字段 + active_preset_id
	maxRetries := preset.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if err := tx.Model(&model.Group{}).
		Where("id = ?", preset.GroupID).
		Updates(map[string]interface{}{
			"mode":                 preset.Mode,
			"match_regex":          preset.MatchRegex,
			"first_token_time_out": preset.FirstTokenTimeOut,
			"session_keep_time":    preset.SessionKeepTime,
			"retry_enabled":        preset.RetryEnabled,
			"max_retries":          maxRetries,
			"active_preset_id":     preset.ID,
		}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update group: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	if err := groupRefreshCacheByID(preset.GroupID, ctx); err != nil {
		return fmt.Errorf("failed to refresh cache: %w", err)
	}
	resetBalancerStateForChannels(channelIDs...)
	return nil
}

// GroupSetPinned 设置置顶状态。pinned=true 时写入 PinnedAt=now；false 时清空
func GroupSetPinned(groupID int, pinned bool, ctx context.Context) error {
	if _, ok := groupCache.Get(groupID); !ok {
		return fmt.Errorf("group not found")
	}
	updates := map[string]interface{}{
		"pinned": pinned,
	}
	if pinned {
		updates["pinned_at"] = time.Now()
	} else {
		updates["pinned_at"] = gorm.Expr("NULL")
	}
	if err := db.GetDB().WithContext(ctx).
		Model(&model.Group{}).
		Where("id = ?", groupID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update pin state: %w", err)
	}
	return groupRefreshCacheByID(groupID, ctx)
}
