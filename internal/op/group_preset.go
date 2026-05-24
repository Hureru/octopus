package op

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	name = strings.TrimSpace(name)
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

// GroupPresetCreateBlank 创建空白预设：使用默认 Mode + 空 items
// 用于"基于零起步设计一份新预设"的场景，不读取 Group 当前状态
func GroupPresetCreateBlank(groupID int, name string, ctx context.Context) (*model.GroupPreset, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("preset name required")
	}
	if _, ok := groupCache.Get(groupID); !ok {
		return nil, fmt.Errorf("group not found")
	}
	preset := model.GroupPreset{
		GroupID:    groupID,
		Name:       name,
		Mode:       model.GroupModeRoundRobin,
		MaxRetries: 3,
		Items:      []model.GroupPresetItem{},
	}
	if err := db.GetDB().WithContext(ctx).Create(&preset).Error; err != nil {
		return nil, fmt.Errorf("failed to create blank preset: %w", err)
	}
	return &preset, nil
}

// GroupPresetClone 克隆已有预设为副本，碰到同名冲突时自动追加 " 2"/" 3" 后缀
func GroupPresetClone(presetID int, newName string, ctx context.Context) (*model.GroupPreset, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, fmt.Errorf("preset name required")
	}
	var source model.GroupPreset
	if err := db.GetDB().WithContext(ctx).First(&source, presetID).Error; err != nil {
		return nil, fmt.Errorf("preset not found")
	}

	finalName := newName
	for suffix := 2; suffix < 1000; suffix++ {
		var count int64
		if err := db.GetDB().WithContext(ctx).
			Model(&model.GroupPreset{}).
			Where("group_id = ? AND name = ?", source.GroupID, finalName).
			Count(&count).Error; err != nil {
			return nil, fmt.Errorf("failed to check name conflict: %w", err)
		}
		if count == 0 {
			break
		}
		finalName = fmt.Sprintf("%s %d", newName, suffix)
	}

	items := make([]model.GroupPresetItem, len(source.Items))
	copy(items, source.Items)

	clone := model.GroupPreset{
		GroupID:           source.GroupID,
		Name:              finalName,
		Mode:              source.Mode,
		MatchRegex:        source.MatchRegex,
		FirstTokenTimeOut: source.FirstTokenTimeOut,
		SessionKeepTime:   source.SessionKeepTime,
		RetryEnabled:      source.RetryEnabled,
		MaxRetries:        source.MaxRetries,
		Items:             items,
	}
	if err := db.GetDB().WithContext(ctx).Create(&clone).Error; err != nil {
		return nil, fmt.Errorf("failed to clone preset: %w", err)
	}
	return &clone, nil
}

// presetIsActiveTx 在事务内检查指定预设是否被任何 Group 当作活动预设引用
// 用 DB 而非 groupCache，避免缓存延迟造成的竞态绕过
// 仅 GroupPresetDelete 使用——拦截"删除 active 预设"
func presetIsActiveTx(tx *gorm.DB, presetID int) (bool, error) {
	var count int64
	if err := tx.Model(&model.Group{}).
		Where("active_preset_id = ?", presetID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// mirrorPresetToActiveGroupTx 若该预设是某 Group 的 active，把预设字段+items 写回 Group
// 返回受影响的 channel ID 列表（旧 items 的 + 新 items 的），供事务外做熔断/粘性重置
// 未绑定时返回 0, nil, nil
func mirrorPresetToActiveGroupTx(tx *gorm.DB, preset *model.GroupPreset) (groupID int, channelIDs []int, err error) {
	var group model.Group
	err = tx.Where("active_preset_id = ?", preset.ID).First(&group).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("failed to find owning group: %w", err)
	}

	// 收集旧 items 的 channel IDs
	var oldItems []model.GroupItem
	if err = tx.Where("group_id = ?", group.ID).Find(&oldItems).Error; err != nil {
		return group.ID, nil, fmt.Errorf("failed to load old items: %w", err)
	}
	ids := make([]int, 0, len(oldItems)+len(preset.Items))
	for _, it := range oldItems {
		ids = append(ids, it.ChannelID)
	}
	for _, it := range preset.Items {
		ids = append(ids, it.ChannelID)
	}

	// 镜像字段
	maxRetries := preset.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if err = tx.Model(&model.Group{}).
		Where("id = ?", group.ID).
		Updates(map[string]interface{}{
			"mode":                 preset.Mode,
			"match_regex":          preset.MatchRegex,
			"first_token_time_out": preset.FirstTokenTimeOut,
			"session_keep_time":    preset.SessionKeepTime,
			"retry_enabled":        preset.RetryEnabled,
			"max_retries":          maxRetries,
		}).Error; err != nil {
		return group.ID, ids, fmt.Errorf("failed to mirror preset to group: %w", err)
	}

	// 替换 items：清空再插入
	if err = tx.Where("group_id = ?", group.ID).Delete(&model.GroupItem{}).Error; err != nil {
		return group.ID, ids, fmt.Errorf("failed to clear old items: %w", err)
	}
	if len(preset.Items) > 0 {
		newItems := make([]model.GroupItem, 0, len(preset.Items))
		for _, it := range preset.Items {
			newItems = append(newItems, model.GroupItem{
				GroupID:   group.ID,
				ChannelID: it.ChannelID,
				ModelName: it.ModelName,
				Priority:  it.Priority,
				Weight:    it.Weight,
			})
		}
		if err = tx.Create(&newItems).Error; err != nil {
			return group.ID, ids, fmt.Errorf("failed to insert new items: %w", err)
		}
	}

	return group.ID, ids, nil
}

// syncActivePresetTx 用 Group 当前实时状态（tx 内读取）回写 active preset
// 在 GroupUpdate 的 tx.Commit() 之前调用，确保 active preset 永远等于运行配置
// 若 active_preset_id 指向已删除行，自愈置为 NULL
func syncActivePresetTx(tx *gorm.DB, groupID int) error {
	var group model.Group
	if err := tx.First(&group, groupID).Error; err != nil {
		return fmt.Errorf("failed to load group for preset sync: %w", err)
	}
	if group.ActivePresetID == nil {
		return nil
	}
	presetID := *group.ActivePresetID

	var preset model.GroupPreset
	if err := tx.Where("id = ? AND group_id = ?", presetID, groupID).First(&preset).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// active_preset_id 指向不存在或不属于本 group 的预设：清空，仅在仍指向我们认为的 presetID 时
			return tx.Model(&model.Group{}).
				Where("id = ? AND active_preset_id = ?", groupID, presetID).
				Update("active_preset_id", gorm.Expr("NULL")).Error
		}
		return fmt.Errorf("failed to load active preset: %w", err)
	}

	var items []model.GroupItem
	if err := tx.Where("group_id = ?", groupID).Order("priority ASC").Find(&items).Error; err != nil {
		return fmt.Errorf("failed to load group items: %w", err)
	}
	presetItems := make([]model.GroupPresetItem, 0, len(items))
	for _, it := range items {
		presetItems = append(presetItems, model.GroupPresetItem{
			ChannelID: it.ChannelID,
			ModelName: it.ModelName,
			Priority:  it.Priority,
			Weight:    it.Weight,
		})
	}

	preset.Mode = group.Mode
	preset.MatchRegex = group.MatchRegex
	preset.FirstTokenTimeOut = group.FirstTokenTimeOut
	preset.SessionKeepTime = group.SessionKeepTime
	preset.RetryEnabled = group.RetryEnabled
	preset.MaxRetries = group.MaxRetries
	preset.Items = presetItems

	if err := tx.Save(&preset).Error; err != nil {
		return fmt.Errorf("failed to sync active preset: %w", err)
	}
	return nil
}

// GroupPresetUpdate 直接编辑预设内容
// 若该预设是某 Group 的 active，同事务内把改动镜像到该 Group 的实时配置 + items
func GroupPresetUpdate(presetID int, req *model.GroupPresetUpdateRequest, ctx context.Context) (*model.GroupPreset, error) {
	var preset model.GroupPreset
	var mirrorGroupID int
	var affectedChannels []int

	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&preset, presetID).Error; err != nil {
			return fmt.Errorf("preset not found")
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
		if err := tx.Save(&preset).Error; err != nil {
			return fmt.Errorf("failed to update preset: %w", err)
		}

		gID, chIDs, err := mirrorPresetToActiveGroupTx(tx, &preset)
		if err != nil {
			return err
		}
		mirrorGroupID = gID
		affectedChannels = chIDs
		return nil
	})
	if err != nil {
		return nil, err
	}

	if mirrorGroupID > 0 {
		if err := groupRefreshCacheByID(mirrorGroupID, ctx); err != nil {
			return nil, fmt.Errorf("failed to refresh cache: %w", err)
		}
		resetBalancerStateForChannels(affectedChannels...)
	}
	return &preset, nil
}

// GroupPresetDelete 删除预设。若是当前 active 预设则拒绝——用户必须先激活其他预设再删
func GroupPresetDelete(presetID int, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var preset model.GroupPreset
		if err := tx.First(&preset, presetID).Error; err != nil {
			return fmt.Errorf("preset not found")
		}
		active, err := presetIsActiveTx(tx, presetID)
		if err != nil {
			return fmt.Errorf("failed to check active preset: %w", err)
		}
		if active {
			return fmt.Errorf("cannot delete active preset; activate another preset first")
		}
		if err := tx.Delete(&model.GroupPreset{}, presetID).Error; err != nil {
			return fmt.Errorf("failed to delete preset: %w", err)
		}
		return nil
	})
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
