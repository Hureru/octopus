package op

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	model2 "github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var channelCache = cache.New[int, model.Channel](16)
var channelKeyCache = cache.New[int, model.ChannelKey](16)
var channelKeyCacheNeedUpdate = make(map[int]struct{})
var channelKeyCacheNeedUpdateLock sync.Mutex

func ChannelList(ctx context.Context) ([]model.Channel, error) {
	channels := make([]model.Channel, 0, channelCache.Len())
	for _, channel := range channelCache.GetAll() {
		normalizeChannelProxyFields(&channel)
		channels = append(channels, channel)
	}
	return channels, nil
}

func normalizeChannelProxyFields(channel *model.Channel) {
	if channel == nil {
		return
	}
	if channel.ProxyMode == "" {
		channel.ProxyMode = model.ProxyUsageModeDirect
	}
	if channel.ProxyMode != model.ProxyUsageModePool {
		channel.ProxyConfigID = nil
	}
	channel.Proxy = channel.ProxyMode != model.ProxyUsageModeDirect
	channel.ChannelProxy = nil
}

func ChannelCreate(channel *model.Channel, ctx context.Context) error {
	if channel == nil {
		return fmt.Errorf("channel is nil")
	}
	if channel.ProxyMode == "" {
		channel.ProxyMode = model.ProxyUsageModeDirect
	}
	if err := channel.ProxyMode.Validate(false); err != nil {
		return err
	}
	if channel.ProxyMode == model.ProxyUsageModePool {
		if channel.ProxyConfigID == nil || *channel.ProxyConfigID <= 0 {
			return fmt.Errorf("proxy config id is required when proxy mode is pool")
		}
		if _, err := ProxyURLForConfig(*channel.ProxyConfigID, ctx); err != nil {
			return err
		}
	} else {
		channel.ProxyConfigID = nil
	}
	if err := db.GetDB().WithContext(ctx).Create(channel).Error; err != nil {
		return err
	}
	normalizeChannelProxyFields(channel)
	channelCache.Set(channel.ID, *channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}
	return nil
}

// ChannelKeyUpdate 仅更新 ChannelKey 的内存缓存（不落库），并标记为需要在 SaveCache 时写入数据库。
func ChannelKeyUpdate(key model.ChannelKey) error {
	if key.ID == 0 || key.ChannelID == 0 {
		return fmt.Errorf("invalid channel key")
	}
	ch, ok := channelCache.Get(key.ChannelID)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	if len(ch.Keys) > 0 {
		keys := make([]model.ChannelKey, len(ch.Keys))
		copy(keys, ch.Keys)
		for i := range keys {
			if keys[i].ID == key.ID {
				keys[i] = key
				break
			}
		}
		ch.Keys = keys
	}
	channelCache.Set(key.ChannelID, ch)
	channelKeyCache.Set(key.ID, key)
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate[key.ID] = struct{}{}
	channelKeyCacheNeedUpdateLock.Unlock()
	return nil
}
func ChannelBaseUrlUpdate(channelID int, baseUrl []model.BaseUrl) error {
	ch, ok := channelCache.Get(channelID)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	// Copy to decouple callers from internal cache storage.
	if baseUrl == nil {
		ch.BaseUrls = nil
	} else {
		cp := make([]model.BaseUrl, len(baseUrl))
		copy(cp, baseUrl)
		ch.BaseUrls = cp
	}
	channelCache.Set(channelID, ch)
	return nil
}

// ChannelKeySaveDB 将运行时更新过的 ChannelKey 缓存写入数据库。
func ChannelKeySaveDB(ctx context.Context) error {
	channelKeyCacheNeedUpdateLock.Lock()
	keyIDs := make([]int, 0, len(channelKeyCacheNeedUpdate))
	for id := range channelKeyCacheNeedUpdate {
		keyIDs = append(keyIDs, id)
	}
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()

	if len(keyIDs) == 0 {
		return nil
	}

	rows := make([]model.ChannelKey, 0, len(keyIDs))
	for _, id := range keyIDs {
		k, ok := channelKeyCache.Get(id)
		if ok {
			rows = append(rows, k)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if err := db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).CreateInBatches(&rows, 100).Error; err != nil {
		channelKeyCacheNeedUpdateLock.Lock()
		for _, id := range keyIDs {
			channelKeyCacheNeedUpdate[id] = struct{}{}
		}
		channelKeyCacheNeedUpdateLock.Unlock()
		return err
	}
	return nil
}

func ChannelUpdate(req *model.ChannelUpdateRequest, ctx context.Context) (*model.Channel, error) {
	existingChannel, ok := channelCache.Get(req.ID)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	normalizeChannelProxyFields(&existingChannel)
	if !req.BypassManagedCheck {
		if _, managed, err := ChannelManagedBinding(req.ID, ctx); err != nil {
			return nil, err
		} else if managed {
			return nil, fmt.Errorf("managed site channel is read-only; please edit it from the site account")
		}
	}

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var selectFields []string
	updates := model.Channel{ID: req.ID}

	if req.Name != nil {
		selectFields = append(selectFields, "name")
		updates.Name = *req.Name
	}
	if req.Type != nil {
		selectFields = append(selectFields, "type")
		updates.Type = *req.Type
	}
	if req.Enabled != nil {
		selectFields = append(selectFields, "enabled")
		updates.Enabled = *req.Enabled
	}
	if req.BaseUrls != nil {
		selectFields = append(selectFields, "base_urls")
		updates.BaseUrls = *req.BaseUrls
	}
	if req.Model != nil {
		selectFields = append(selectFields, "model")
		updates.Model = *req.Model
	}
	if req.CustomModel != nil {
		selectFields = append(selectFields, "custom_model")
		updates.CustomModel = *req.CustomModel
	}
	effectiveProxyMode := existingChannel.ProxyMode
	effectiveProxyConfigID := existingChannel.ProxyConfigID
	proxyTouched := false
	if req.ProxyMode != nil {
		proxyTouched = true
		effectiveProxyMode = *req.ProxyMode
		selectFields = append(selectFields, "proxy_mode")
		updates.ProxyMode = *req.ProxyMode
	}
	if req.ProxyConfigID != nil || req.ProxyMode != nil {
		proxyTouched = true
		if effectiveProxyMode == model.ProxyUsageModePool {
			if req.ProxyConfigID != nil {
				selectFields = append(selectFields, "proxy_config_id")
				effectiveProxyConfigID = req.ProxyConfigID
				updates.ProxyConfigID = req.ProxyConfigID
			}
		} else {
			selectFields = append(selectFields, "proxy_config_id")
			effectiveProxyConfigID = nil
			updates.ProxyConfigID = nil
		}
	}
	if proxyTouched {
		if effectiveProxyMode == "" {
			effectiveProxyMode = model.ProxyUsageModeDirect
		}
		if err := effectiveProxyMode.Validate(false); err != nil {
			tx.Rollback()
			return nil, err
		}
		if effectiveProxyMode == model.ProxyUsageModePool {
			if effectiveProxyConfigID == nil || *effectiveProxyConfigID <= 0 {
				tx.Rollback()
				return nil, fmt.Errorf("proxy config id is required when proxy mode is pool")
			}
			if _, err := ProxyURLForConfig(*effectiveProxyConfigID, ctx); err != nil {
				tx.Rollback()
				return nil, err
			}
		}
	}
	if req.AutoSync != nil {
		selectFields = append(selectFields, "auto_sync")
		updates.AutoSync = *req.AutoSync
	}
	if req.AutoGroup != nil {
		selectFields = append(selectFields, "auto_group")
		updates.AutoGroup = *req.AutoGroup
	}
	if req.CustomHeader != nil {
		selectFields = append(selectFields, "custom_header")
		updates.CustomHeader = *req.CustomHeader
	}
	if req.WSMode != nil {
		selectFields = append(selectFields, "ws_mode")
		updates.WSMode = req.WSMode.Normalize()
	}
	if req.ParamOverride != nil {
		selectFields = append(selectFields, "param_override")
		updates.ParamOverride = req.ParamOverride
	}
	if req.MatchRegex != nil {
		selectFields = append(selectFields, "match_regex")
		updates.MatchRegex = req.MatchRegex
	}

	// 只有当有字段需要更新时才执行 UPDATE
	if len(selectFields) > 0 {
		if err := tx.Model(&model.Channel{}).Where("id = ?", req.ID).Select(selectFields).Updates(&updates).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to update channel: %w", err)
		}
	}

	// 删除 keys
	if len(req.KeysToDelete) > 0 {
		if err := tx.Where("id IN ? AND channel_id = ?", req.KeysToDelete, req.ID).Delete(&model.ChannelKey{}).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to delete channel keys: %w", err)
		}
	}

	// 更新 keys（逐条，只更新提供的字段）
	if len(req.KeysToUpdate) > 0 {
		for _, ku := range req.KeysToUpdate {
			updates := map[string]interface{}{}
			if ku.Enabled != nil {
				updates["enabled"] = *ku.Enabled
			}
			if ku.ChannelKey != nil {
				updates["channel_key"] = *ku.ChannelKey
			}
			if ku.Remark != nil {
				updates["remark"] = *ku.Remark
			}
			if len(updates) == 0 {
				continue
			}
			if err := tx.Model(&model.ChannelKey{}).
				Where("id = ? AND channel_id = ?", ku.ID, req.ID).
				Updates(updates).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to update channel key %d: %w", ku.ID, err)
			}
		}
	}

	// 新增 keys
	if len(req.KeysToAdd) > 0 {
		newKeys := make([]model.ChannelKey, 0, len(req.KeysToAdd))
		for _, ka := range req.KeysToAdd {
			newKeys = append(newKeys, model.ChannelKey{
				ChannelID:  req.ID,
				Enabled:    ka.Enabled,
				ChannelKey: ka.ChannelKey,
				Remark:     ka.Remark,
			})
		}
		if err := tx.Create(&newKeys).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create channel keys: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 刷新缓存并返回最新数据
	if err := channelRefreshCacheByID(req.ID, ctx); err != nil {
		return nil, err
	}

	channel, _ := channelCache.Get(req.ID)
	normalizeChannelProxyFields(&channel)
	channelCache.Set(req.ID, channel)
	resetBalancerStateForChannel(req.ID)
	return &channel, nil
}

func ChannelEnabled(id int, enabled bool, ctx context.Context) error {
	oldChannel, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	if _, managed, err := ChannelManagedBinding(id, ctx); err != nil {
		return err
	} else if managed {
		return fmt.Errorf("managed site channel is read-only; please enable or disable it from the site account")
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Update("enabled", enabled).Error; err != nil {
		return err
	}
	oldChannel.Enabled = enabled
	normalizeChannelProxyFields(&oldChannel)
	channelCache.Set(id, oldChannel)
	resetBalancerStateForChannel(id)
	return nil
}

func ChannelEnabledManaged(id int, enabled bool, ctx context.Context) error {
	oldChannel, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.Channel{}).Where("id = ?", id).Update("enabled", enabled).Error; err != nil {
		return err
	}
	oldChannel.Enabled = enabled
	normalizeChannelProxyFields(&oldChannel)
	channelCache.Set(id, oldChannel)
	resetBalancerStateForChannel(id)
	return nil
}

func ChannelDel(id int, ctx context.Context) error {
	return channelDel(id, ctx, false)
}

func ChannelDelManaged(id int, ctx context.Context) error {
	if _, managed, err := ChannelManagedBinding(id, ctx); err != nil {
		return err
	} else if !managed {
		return fmt.Errorf("channel is not a managed site channel")
	}
	return channelDel(id, ctx, true)
}

func channelDel(id int, ctx context.Context, bypassManagedCheck bool) error {
	ch, ok := channelCache.Get(id)
	if !ok {
		return fmt.Errorf("channel not found")
	}
	if !bypassManagedCheck {
		if _, managed, err := ChannelManagedBinding(id, ctx); err != nil {
			return err
		} else if managed {
			return fmt.Errorf("managed site channel cannot be deleted directly; delete the site account or site binding instead")
		}
	}

	// 开启事务
	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取所有受影响的 GroupID，用于刷新缓存
	var affectedGroupIDs []int
	if err := tx.Model(&model.GroupItem{}).
		Where("channel_id = ?", id).
		Pluck("group_id", &affectedGroupIDs).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to get affected groups: %w", err)
	}

	// 删除所有引用该渠道的 GroupItem
	if err := tx.Where("channel_id = ?", id).Delete(&model.GroupItem{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	// 删除渠道 keys
	if err := tx.Where("channel_id = ?", id).Delete(&model.ChannelKey{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel keys: %w", err)
	}

	// 删除统计数据
	if err := tx.Where("channel_id = ?", id).Delete(&model.StatsChannel{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel stats: %w", err)
	}

	// 删除渠道
	if err := tx.Delete(&model.Channel{}, id).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete channel: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 删除缓存
	channelCache.Del(id)
	for _, k := range ch.Keys {
		if k.ID != 0 {
			channelKeyCache.Del(k.ID)
		}
	}
	StatsChannelDel(id)
	resetBalancerStateForChannel(id)

	// 刷新受影响的分组缓存
	for _, groupID := range affectedGroupIDs {
		if err := groupRefreshCacheByID(groupID, ctx); err != nil {
			log.Warnf("failed to refresh group cache for group %d: %v", groupID, err)
		}
	}

	return nil
}

func ChannelLLMList(ctx context.Context) ([]model.LLMChannel, error) {
	channelsByID := channelCache.GetAll()
	channelIDs := make([]int, 0, len(channelsByID))
	for channelID := range channelsByID {
		channelIDs = append(channelIDs, channelID)
	}
	bindingMap, err := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	if err != nil {
		return nil, err
	}
	siteCache := make(map[int]*model.Site)
	accountCache := make(map[int]*model.SiteAccount)

	models := []model.LLMChannel{}
	for _, channel := range channelsByID {
		var binding *model.SiteChannelBinding
		if item, ok := bindingMap[channel.ID]; ok {
			copy := item
			binding = &copy
		}
		siteName := ""
		siteAccountName := ""
		siteGroupKey := ""
		siteGroupName := ""
		endpointType := "openai"
		var siteID *int
		var siteAccountID *int
		if binding != nil {
			siteID = &binding.SiteID
			siteAccountID = &binding.SiteAccountID
			siteGroupKey = model.NormalizeSiteGroupKey(binding.GroupKey)
			if site, ok := siteCache[binding.SiteID]; ok {
				siteName = site.Name
			} else if site, getErr := SiteGet(binding.SiteID, ctx); getErr == nil {
				siteCache[binding.SiteID] = site
				siteName = site.Name
			}
			if account, ok := accountCache[binding.SiteAccountID]; ok {
				siteAccountName = account.Name
			} else if account, getErr := SiteAccountGet(binding.SiteAccountID, ctx); getErr == nil {
				accountCache[binding.SiteAccountID] = account
				siteAccountName = account.Name
			}
			siteGroupName = siteGroupKey
			if binding.SiteUserGroupID != nil && *binding.SiteUserGroupID > 0 {
				if account := accountCache[binding.SiteAccountID]; account != nil {
					for _, group := range account.UserGroups {
						if group.ID == *binding.SiteUserGroupID {
							siteGroupName = model.NormalizeSiteGroupName(group.GroupKey, group.Name)
							siteGroupKey = model.NormalizeSiteGroupKey(group.GroupKey)
							break
						}
					}
				}
			}
			if siteGroupName == "" {
				siteGroupName = model.NormalizeSiteGroupName(siteGroupKey, "")
			}
			switch channel.Type {
			case model2.OutboundTypeAnthropic:
				endpointType = "anthropic"
			case model2.OutboundTypeGemini:
				endpointType = "gemini"
			default:
				endpointType = "openai"
			}
		}
		modelNames := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
		for _, modelName := range modelNames {
			if modelName == "" {
				continue
			}
			models = append(models, model.LLMChannel{
				Name:            modelName,
				Enabled:         channel.Enabled,
				ChannelID:       channel.ID,
				ChannelName:     channel.Name,
				SiteID:          siteID,
				SiteAccountID:   siteAccountID,
				SiteGroupKey:    siteGroupKey,
				SiteGroupName:   siteGroupName,
				SiteName:        siteName,
				SiteAccountName: siteAccountName,
				EndpointType:    endpointType,
			})
		}
	}
	return models, nil
}

func ChannelGet(id int, ctx context.Context) (*model.Channel, error) {
	channel, ok := channelCache.Get(id)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	normalizeChannelProxyFields(&channel)
	return &channel, nil
}

func ChannelGetByName(name string, ctx context.Context) (*model.Channel, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, fmt.Errorf("channel name is empty")
	}

	var channel model.Channel
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		Where("name = ?", trimmed).
		First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			for id, cached := range channelCache.GetAll() {
				if cached.Name != trimmed {
					continue
				}
				channelCache.Del(id)
				for _, key := range cached.Keys {
					if key.ID != 0 {
						channelKeyCache.Del(key.ID)
					}
				}
			}
		}
		return nil, err
	}

	normalizeChannelProxyFields(&channel)
	channelCache.Set(channel.ID, channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}

	return &channel, nil
}

func channelRefreshCache(ctx context.Context) error {
	channels := []model.Channel{}
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		Find(&channels).Error; err != nil {
		log.Warnf("failed to get channels: %v", err)
		return err
	}
	channelKeyCache.Clear()
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()
	for _, channel := range channels {
		normalizeChannelProxyFields(&channel)
		channelCache.Set(channel.ID, channel)
		for _, k := range channel.Keys {
			if k.ID != 0 {
				channelKeyCache.Set(k.ID, k)
			}
		}
	}
	return nil
}

func channelRefreshCacheByID(id int, ctx context.Context) error {
	if old, ok := channelCache.Get(id); ok {
		for _, k := range old.Keys {
			if k.ID != 0 {
				channelKeyCache.Del(k.ID)
			}
		}
	}
	var channel model.Channel
	if err := db.GetDB().WithContext(ctx).
		Preload("Keys").
		Preload("Stats").
		First(&channel, id).Error; err != nil {
		return err
	}
	normalizeChannelProxyFields(&channel)
	channelCache.Set(channel.ID, channel)
	for _, k := range channel.Keys {
		if k.ID != 0 {
			channelKeyCache.Set(k.ID, k)
		}
	}
	return nil
}
