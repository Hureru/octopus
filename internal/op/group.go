package op

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	model2 "github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var groupCache = cache.New[int, model.Group](16)
var groupMap = cache.New[string, model.Group](16)

var ErrGroupNotFound = errors.New("group not found")

func GroupList(ctx context.Context) ([]model.Group, error) {
	groups := make([]model.Group, 0, groupCache.Len())
	for _, group := range groupCache.GetAll() {
		groups = append(groups, group)
	}
	return groups, nil
}

func GroupListModel(ctx context.Context) ([]string, error) {
	models := []string{}
	for _, group := range groupCache.GetAll() {
		models = append(models, group.Name)
	}
	return models, nil
}

func GroupGet(id int, ctx context.Context) (*model.Group, error) {
	group, ok := groupCache.Get(id)
	if !ok {
		return nil, ErrGroupNotFound
	}
	return &group, nil
}

func GroupGetEnabledMap(name string, ctx context.Context) (model.Group, error) {
	group, ok := groupMap.Get(name)
	if !ok {
		return model.Group{}, ErrGroupNotFound
	}
	if len(group.Items) == 0 {
		group.Items = nil
		return group, nil
	}

	enabledItems := make([]model.GroupItem, 0, len(group.Items))
	for _, item := range group.Items {
		channel, ok := channelCache.Get(item.ChannelID)
		if !ok || !channel.Enabled {
			continue
		}
		enabledItems = append(enabledItems, item)
	}
	group.Items = enabledItems
	return group, nil
}

func GroupCreate(group *model.Group, ctx context.Context) error {
	if err := db.GetDB().WithContext(ctx).Create(group).Error; err != nil {
		return err
	}
	groupCache.Set(group.ID, *group)
	groupMap.Set(group.Name, *group)
	return nil
}

func GroupUpdate(req *model.GroupUpdateRequest, ctx context.Context) (*model.Group, error) {
	oldGroup, ok := groupCache.Get(req.ID)
	if !ok {
		return nil, ErrGroupNotFound
	}
	oldName := oldGroup.Name
	affectedChannelIDs := groupUpdateAffectedChannelIDs(oldGroup, req)

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var selectFields []string
	updates := model.Group{ID: req.ID}

	if req.Name != nil {
		selectFields = append(selectFields, "name")
		updates.Name = *req.Name
	}
	if req.Mode != nil {
		selectFields = append(selectFields, "mode")
		updates.Mode = *req.Mode
	}
	if req.MatchRegex != nil {
		selectFields = append(selectFields, "match_regex")
		updates.MatchRegex = *req.MatchRegex
	}
	if req.FirstTokenTimeOut != nil {
		selectFields = append(selectFields, "first_token_time_out")
		updates.FirstTokenTimeOut = *req.FirstTokenTimeOut
	}
	if req.SessionKeepTime != nil {
		selectFields = append(selectFields, "session_keep_time")
		updates.SessionKeepTime = *req.SessionKeepTime
	}
	if req.RetryEnabled != nil {
		selectFields = append(selectFields, "retry_enabled")
		updates.RetryEnabled = *req.RetryEnabled
	}
	if req.MaxRetries != nil {
		v := *req.MaxRetries
		if v <= 0 {
			v = 3
		}
		selectFields = append(selectFields, "max_retries")
		updates.MaxRetries = v
	}

	if len(selectFields) > 0 {
		if err := tx.Model(&model.Group{}).Where("id = ?", req.ID).Select(selectFields).Updates(&updates).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to update group: %w", err)
		}
	}

	// 删除 items
	if len(req.ItemsToDelete) > 0 {
		if err := tx.Where("id IN ? AND group_id = ?", req.ItemsToDelete, req.ID).Delete(&model.GroupItem{}).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to delete items: %w", err)
		}
	}

	// 批量更新 items
	if len(req.ItemsToUpdate) > 0 {
		ids := make([]int, len(req.ItemsToUpdate))
		priorityCase := "CASE id"
		weightCase := "CASE id"
		for i, item := range req.ItemsToUpdate {
			ids[i] = item.ID
			priorityCase += fmt.Sprintf(" WHEN %d THEN %d", item.ID, item.Priority)
			weightCase += fmt.Sprintf(" WHEN %d THEN %d", item.ID, item.Weight)
		}
		priorityCase += " END"
		weightCase += " END"

		if err := tx.Model(&model.GroupItem{}).
			Where("id IN ? AND group_id = ?", ids, req.ID).
			Updates(map[string]interface{}{
				"priority": gorm.Expr(priorityCase),
				"weight":   gorm.Expr(weightCase),
			}).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to update items: %w", err)
		}
	}

	// 批量新增 items
	if len(req.ItemsToAdd) > 0 {
		newItems := make([]model.GroupItem, len(req.ItemsToAdd))
		for i, item := range req.ItemsToAdd {
			newItems[i] = model.GroupItem{
				GroupID:   req.ID,
				ChannelID: item.ChannelID,
				ModelName: item.ModelName,
				Priority:  item.Priority,
				Weight:    item.Weight,
			}
		}
		if err := tx.Create(&newItems).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create items: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 刷新缓存并返回最新数据
	if err := groupRefreshCacheByID(req.ID, ctx); err != nil {
		return nil, err
	}

	group, _ := groupCache.Get(req.ID)
	if oldName != "" && oldName != group.Name {
		groupMap.Del(oldName)
	}
	resetBalancerStateForChannels(affectedChannelIDs...)
	return &group, nil
}

func groupUpdateAffectedChannelIDs(oldGroup model.Group, req *model.GroupUpdateRequest) []int {
	itemChannels := make(map[int]int, len(oldGroup.Items))
	for _, item := range oldGroup.Items {
		itemChannels[item.ID] = item.ChannelID
	}

	ids := make([]int, 0, len(oldGroup.Items)+len(req.ItemsToAdd))
	if req.Mode != nil || req.SessionKeepTime != nil {
		for _, item := range oldGroup.Items {
			ids = append(ids, item.ChannelID)
		}
	}
	if req.RetryEnabled != nil || req.MaxRetries != nil {
		for _, item := range oldGroup.Items {
			ids = append(ids, item.ChannelID)
		}
	}
	for _, itemID := range req.ItemsToDelete {
		ids = append(ids, itemChannels[itemID])
	}
	for _, item := range req.ItemsToUpdate {
		ids = append(ids, itemChannels[item.ID])
	}
	for _, item := range req.ItemsToAdd {
		ids = append(ids, item.ChannelID)
	}
	return ids
}

func GroupDel(id int, ctx context.Context) error {
	group, ok := groupCache.Get(id)
	if !ok {
		return ErrGroupNotFound
	}

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Where("group_id = ?", id).Delete(&model.GroupItem{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	if err := tx.Delete(&model.Group{}, id).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete group: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	groupCache.Del(id)
	groupMap.Del(group.Name)
	for _, item := range group.Items {
		resetBalancerStateForChannel(item.ChannelID)
	}
	return nil
}

func GroupItemAdd(item *model.GroupItem, ctx context.Context) error {
	if _, ok := groupCache.Get(item.GroupID); !ok {
		return ErrGroupNotFound
	}

	if err := db.GetDB().WithContext(ctx).Create(item).Error; err != nil {
		return err
	}

	if err := groupRefreshCacheByID(item.GroupID, ctx); err != nil {
		return err
	}
	resetBalancerStateForChannel(item.ChannelID)
	return nil
}

func GroupItemBatchAdd(groupID int, items []model.GroupIDAndLLMName, ctx context.Context) error {
	if len(items) == 0 {
		return nil
	}

	group, ok := groupCache.Get(groupID)
	if !ok {
		return ErrGroupNotFound
	}

	seen := make(map[string]struct{}, len(items))
	uniq := make([]model.GroupIDAndLLMName, 0, len(items))
	for _, it := range items {
		if it.ChannelID == 0 || it.ModelName == "" {
			continue
		}
		k := fmt.Sprintf("%d|%s", it.ChannelID, it.ModelName)
		if _, exists := seen[k]; exists {
			continue
		}
		seen[k] = struct{}{}
		uniq = append(uniq, it)
	}
	if len(uniq) == 0 {
		return nil
	}

	nextPriority := 1
	for _, gi := range group.Items {
		if gi.Priority >= nextPriority {
			nextPriority = gi.Priority + 1
		}
	}

	newItems := make([]model.GroupItem, 0, len(uniq))
	for _, it := range uniq {
		newItems = append(newItems, model.GroupItem{
			GroupID:   groupID,
			ChannelID: it.ChannelID,
			ModelName: it.ModelName,
			Priority:  nextPriority,
			Weight:    1,
		})
		nextPriority++
	}

	if err := db.GetDB().WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "group_id"}, {Name: "channel_id"}, {Name: "model_name"}},
			DoNothing: true,
		}).
		Create(&newItems).Error; err != nil {
		return fmt.Errorf("failed to create group items: %w", err)
	}

	if err := groupRefreshCacheByID(groupID, ctx); err != nil {
		return err
	}
	channelIDs := make([]int, 0, len(uniq))
	for _, item := range uniq {
		channelIDs = append(channelIDs, item.ChannelID)
	}
	resetBalancerStateForChannels(channelIDs...)
	return nil
}

func GroupItemUpdate(item *model.GroupItem, ctx context.Context) error {
	if err := db.GetDB().WithContext(ctx).Model(item).
		Select("ModelName", "Priority", "Weight").
		Updates(item).Error; err != nil {
		return err
	}

	if err := groupRefreshCacheByID(item.GroupID, ctx); err != nil {
		return err
	}
	resetBalancerStateForChannel(item.ChannelID)
	return nil
}

func GroupItemDel(id int, ctx context.Context) error {
	var item model.GroupItem
	if err := db.GetDB().WithContext(ctx).First(&item, id).Error; err != nil {
		return fmt.Errorf("group item not found")
	}

	if err := db.GetDB().WithContext(ctx).Delete(&item).Error; err != nil {
		return err
	}

	if err := groupRefreshCacheByID(item.GroupID, ctx); err != nil {
		return err
	}
	resetBalancerStateForChannel(item.ChannelID)
	return nil
}

// GroupItemBatchDelByChannelAndModels 根据渠道ID和模型名称批量删除分组项
func GroupItemBatchDelByChannelAndModels(keys []model.GroupIDAndLLMName, ctx context.Context) error {
	if len(keys) == 0 {
		return nil
	}

	conditions := make([][]interface{}, len(keys))
	for i, key := range keys {
		conditions[i] = []interface{}{key.ChannelID, key.ModelName}
	}

	var groupIDs []int
	if err := db.GetDB().WithContext(ctx).
		Model(&model.GroupItem{}).
		Distinct("group_id").
		Where("(channel_id, model_name) IN ?", conditions).
		Pluck("group_id", &groupIDs).Error; err != nil {
		return fmt.Errorf("failed to find group ids: %w", err)
	}

	if len(groupIDs) == 0 {
		return nil
	}

	if err := db.GetDB().WithContext(ctx).
		Where("(channel_id, model_name) IN ?", conditions).
		Delete(&model.GroupItem{}).Error; err != nil {
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	if err := groupRefreshCacheByIDs(groupIDs, ctx); err != nil {
		return fmt.Errorf("failed to refresh group cache: %w", err)
	}

	channelIDs := make([]int, 0, len(keys))
	for _, key := range keys {
		channelIDs = append(channelIDs, key.ChannelID)
	}
	resetBalancerStateForChannels(channelIDs...)
	return nil
}

func GroupItemList(groupID int, ctx context.Context) ([]model.GroupItem, error) {
	var items []model.GroupItem
	if err := db.GetDB().WithContext(ctx).
		Where("group_id = ?", groupID).
		Order("priority ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func groupRefreshCache(ctx context.Context) error {
	groups := []model.Group{}
	if err := db.GetDB().WithContext(ctx).
		Preload("Items").
		Find(&groups).Error; err != nil {
		return err
	}
	for _, group := range groups {
		groupCache.Set(group.ID, group)
		groupMap.Set(group.Name, group)
	}
	return nil
}

func groupRefreshCacheByID(id int, ctx context.Context) error {
	var group model.Group
	if err := db.GetDB().WithContext(ctx).
		Preload("Items").
		First(&group, id).Error; err != nil {
		return err
	}
	groupCache.Set(group.ID, group)
	groupMap.Set(group.Name, group)
	return nil
}

func groupRefreshCacheByIDs(ids []int, ctx context.Context) error {
	if len(ids) == 0 {
		return nil
	}
	var groups []model.Group
	if err := db.GetDB().WithContext(ctx).
		Preload("Items").
		Where("id IN ?", ids).
		Find(&groups).Error; err != nil {
		return err
	}
	for _, group := range groups {
		groupCache.Set(group.ID, group)
		groupMap.Set(group.Name, group)
	}
	return nil
}

func GroupRefreshCacheByIDs(ids []int, ctx context.Context) error {
	return groupRefreshCacheByIDs(ids, ctx)
}

type GroupAutoAddResult struct {
	GroupID           int `json:"group_id"`
	MatchedCandidates int `json:"matched_candidates"`
	AddedCandidates   int `json:"added_candidates"`
	SkippedCandidates int `json:"skipped_candidates"`
}

func GroupAutoAddItems(groupID int, ctx context.Context) (*GroupAutoAddResult, error) {
	group, ok := groupCache.Get(groupID)
	if !ok {
		return nil, ErrGroupNotFound
	}

	modelChannels, err := ChannelLLMList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list channel models: %w", err)
	}

	existingKeys := make(map[string]struct{}, len(group.Items))
	endpointPreference := deriveGroupAutoAddEndpoint(group.Items)
	for _, item := range group.Items {
		existingKeys[groupAutoAddKey(item.ChannelID, item.ModelName)] = struct{}{}
	}

	matched := make([]model.GroupIDAndLLMName, 0)
	matchedCount := 0
	addedCount := 0
	skippedCount := 0
	seen := make(map[string]struct{})

	for _, candidate := range modelChannels {
		if !candidate.Enabled {
			continue
		}
		if !groupAutoAddModelMatchesGroup(candidate.Name, group) {
			continue
		}
		if endpointPreference != nil && !groupAutoAddEndpointMatches(candidate.EndpointType, *endpointPreference) {
			continue
		}

		matchedCount++
		key := groupAutoAddKey(candidate.ChannelID, candidate.Name)
		if _, exists := existingKeys[key]; exists {
			skippedCount++
			continue
		}
		if _, exists := seen[key]; exists {
			skippedCount++
			continue
		}
		seen[key] = struct{}{}
		matched = append(matched, model.GroupIDAndLLMName{
			ChannelID: candidate.ChannelID,
			ModelName: candidate.Name,
		})
		addedCount++
	}

	if len(matched) > 0 {
		if err := GroupItemBatchAdd(groupID, matched, ctx); err != nil {
			return nil, err
		}
	}

	return &GroupAutoAddResult{
		GroupID:           groupID,
		MatchedCandidates: matchedCount,
		AddedCandidates:   addedCount,
		SkippedCandidates: skippedCount,
	}, nil
}

func groupAutoAddKey(channelID int, modelName string) string {
	return fmt.Sprintf("%d|%s", channelID, strings.TrimSpace(strings.ToLower(modelName)))
}

func groupAutoAddModelMatchesGroup(modelName string, group model.Group) bool {
	name := strings.TrimSpace(modelName)
	if name == "" {
		return false
	}

	groupName := strings.TrimSpace(group.Name)
	if groupName == "" {
		return false
	}

	return strings.EqualFold(name, groupName)
}

func deriveGroupAutoAddEndpoint(items []model.GroupItem) *string {
	if len(items) == 0 {
		return nil
	}

	counts := make(map[string]int)
	for _, item := range items {
		channel, ok := channelCache.Get(item.ChannelID)
		if !ok {
			continue
		}
		endpoint := groupAutoAddEndpointName(channel.Type)
		if endpoint == "" {
			continue
		}
		counts[endpoint]++
	}

	if len(counts) == 0 {
		return nil
	}

	var selected string
	var maxCount int
	for endpoint, count := range counts {
		if count > maxCount || (count == maxCount && (selected == "" || endpoint < selected)) {
			selected = endpoint
			maxCount = count
		}
	}
	if selected == "" {
		return nil
	}

	return &selected
}

func groupAutoAddEndpointName(channelType model2.OutboundType) string {
	switch channelType {
	case model2.OutboundTypeOpenAIChat:
		return "openai"
	case model2.OutboundTypeOpenAIResponse:
		return "response"
	case model2.OutboundTypeAnthropic:
		return "anthropic"
	case model2.OutboundTypeGemini:
		return "gemini"
	default:
		return ""
	}
}

func groupAutoAddEndpointMatches(candidateEndpoint string, expected string) bool {
	normalizedCandidate := strings.TrimSpace(strings.ToLower(candidateEndpoint))
	normalizedExpected := strings.TrimSpace(strings.ToLower(expected))
	if normalizedExpected == "" {
		return true
	}

	return normalizedCandidate == normalizedExpected
}
