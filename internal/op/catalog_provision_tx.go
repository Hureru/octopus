package op

import (
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
	"gorm.io/gorm"
)

// provisionSource 是某个渠道上出现的一个上游模型名，upstreamName 保留渠道侧的原始拼写。
type provisionSource struct {
	channel      model.Channel
	upstreamName string
}

// channelSourcesByModel 按归一化模型名收集所有提供该模型的渠道，结果按渠道 ID 排序以保证优先级稳定。
func channelSourcesByModel(models []string) map[string][]provisionSource {
	wanted := make(map[string]struct{}, len(models))
	for _, name := range models {
		wanted[NormalizeModelIdentity(name)] = struct{}{}
	}

	sources := make(map[string][]provisionSource, len(models))
	for _, channel := range channelCache.GetAll() {
		for _, name := range splitChannelModelNames(channel.Model, channel.CustomModel) {
			normalized := NormalizeModelIdentity(name)
			if _, ok := wanted[normalized]; !ok {
				continue
			}
			sources[normalized] = append(sources[normalized], provisionSource{
				channel:      channel,
				upstreamName: name,
			})
		}
	}
	for normalized := range sources {
		items := sources[normalized]
		sort.Slice(items, func(i, j int) bool {
			if items[i].channel.ID == items[j].channel.ID {
				return items[i].upstreamName < items[j].upstreamName
			}
			return items[i].channel.ID < items[j].channel.ID
		})
	}
	return sources
}

// ensureCanonicalTx 取得或创建目标 Canonical Model。
// 目标名本身若已是别的模型的别名，直接复用它指向的 Canonical Model，避免制造分叉。
func ensureCanonicalTx(
	tx *gorm.DB,
	name string,
	result *model.CatalogProvisionResult,
) (*model.CanonicalModel, error) {
	displayName := strings.TrimSpace(name)
	normalized := NormalizeModelIdentity(displayName)
	if normalized == "" {
		return nil, newCatalogProvisionBadRequestError("model name is required")
	}

	var canonical model.CanonicalModel
	switch err := tx.Where("normalized_name = ?", normalized).First(&canonical).Error; {
	case err == nil:
		return &canonical, nil
	case err != gorm.ErrRecordNotFound:
		return nil, err
	}

	var alias model.ModelAlias
	switch err := tx.Where("normalized_alias = ?", normalized).First(&alias).Error; {
	case err == nil:
		if err := tx.First(&canonical, alias.CanonicalModelID).Error; err != nil {
			return nil, err
		}
		return &canonical, nil
	case err != gorm.ErrRecordNotFound:
		return nil, err
	}

	canonical = model.CanonicalModel{
		Name:            displayName,
		NormalizedName:  normalized,
		Vendor:          modelvendor.Detect(displayName),
		RoutingStrategy: model.RoutingStrategyBalanced,
		ProtocolPolicy:  model.ProtocolPolicyAuto,
		Enabled:         true,
	}
	if err := tx.Create(&canonical).Error; err != nil {
		return nil, err
	}
	result.CanonicalsCreated++
	return &canonical, nil
}

// ensureGroupTx 取得或创建目标分组，默认值与 CatalogSync 自动建组保持一致。
func ensureGroupTx(
	tx *gorm.DB,
	name string,
	result *model.CatalogProvisionResult,
) (*model.Group, error) {
	group, found, err := findGroupByNameTx(tx, name)
	if err != nil {
		return nil, err
	}
	if found {
		return group, nil
	}

	created := model.Group{
		Name:              strings.TrimSpace(name),
		Mode:              model.GroupModeRoundRobin,
		FirstTokenTimeOut: 0,
		SessionKeepTime:   0,
		MaxRetries:        3,
	}
	if err := tx.Create(&created).Error; err != nil {
		return nil, err
	}
	result.GroupsCreated++
	return &created, nil
}

// alignCanonicalNameToGroupTx 让 Canonical Model 使用分组的实际拼写。
// 路由缓存按分组名精确查找，因此仅大小写不同也必须统一到 Group.Name。
func alignCanonicalNameToGroupTx(
	tx *gorm.DB,
	canonical *model.CanonicalModel,
	group *model.Group,
) error {
	if canonical == nil || group == nil || canonical.Name == group.Name {
		return nil
	}
	if canonical.NormalizedName != NormalizeModelIdentity(group.Name) {
		return nil
	}
	if err := tx.Model(&model.CanonicalModel{}).Where("id = ?", canonical.ID).
		Update("name", group.Name).Error; err != nil {
		return err
	}
	canonical.Name = group.Name
	return nil
}

// findGroupByNameTx 按归一化名字查分组：分组名大小写由用户决定，而目录侧一律按小写比较。
func findGroupByNameTx(tx *gorm.DB, name string) (*model.Group, bool, error) {
	normalized := NormalizeModelIdentity(name)
	if normalized == "" {
		return nil, false, nil
	}
	var group model.Group
	switch err := tx.Preload("Items").Where("LOWER(name) = ?", normalized).First(&group).Error; {
	case err == nil:
		return &group, true, nil
	case err == gorm.ErrRecordNotFound:
		return nil, false, nil
	default:
		return nil, false, err
	}
}

// deleteRedundantGroupsTx 删除内容已被重映射完全带走的原分组（自动建组的产物），
// 仍含有其它模型的分组一律保留。
//
// 这里刻意不走 GroupDel：那条路径会调用 ensureRouteCandidatesForGroupTx 把该分组名解析出的
// Canonical Model 下所有候选标记为 unavailable，而重映射场景下这些候选刚被移交给目标模型，
// 退役它们等于把刚接好的路由拆掉。
func deleteRedundantGroupsTx(
	tx *gorm.DB,
	groupIDs []int,
	remappedModels []string,
) ([]model.Group, error) {
	deleted := make([]model.Group, 0, len(groupIDs))
	if len(groupIDs) == 0 {
		return deleted, nil
	}

	remapped := make(map[string]struct{}, len(remappedModels))
	for _, name := range remappedModels {
		remapped[NormalizeModelIdentity(name)] = struct{}{}
	}

	for _, groupID := range uniqueInts(groupIDs) {
		var group model.Group
		switch err := tx.Preload("Items").First(&group, groupID).Error; {
		case err == gorm.ErrRecordNotFound:
			continue
		case err != nil:
			return deleted, err
		}

		redundant := true
		for _, item := range group.Items {
			if _, ok := remapped[NormalizeModelIdentity(item.ModelName)]; !ok {
				redundant = false
				break
			}
		}
		if !redundant {
			continue
		}

		if err := tx.Where("group_id = ?", groupID).Delete(&model.GroupItem{}).Error; err != nil {
			return deleted, err
		}
		if err := tx.Where("group_id = ?", groupID).Delete(&model.GroupPreset{}).Error; err != nil {
			return deleted, err
		}
		if err := tx.Delete(&model.Group{}, groupID).Error; err != nil {
			return deleted, err
		}
		deleted = append(deleted, group)
	}
	return deleted, nil
}

func uniqueInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
