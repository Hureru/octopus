package op

import (
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type catalogRemapResult struct {
	sourceGroupID      int
	affectedChannelIDs []int
}

// remapModelToCanonicalTx 把上游模型名挂到目标 Canonical Model 上。
// 该名字若已被自动建成独立的 Canonical Model，则连同路由候选一并合入目标；
// 已存在的别名则只迁移该上游名对应的分组条目与候选。
func remapModelToCanonicalTx(
	tx *gorm.DB,
	name string,
	target *model.CanonicalModel,
	targetGroup *model.Group,
	result *model.CatalogProvisionResult,
) (catalogRemapResult, error) {
	remapped := catalogRemapResult{}
	normalized := NormalizeModelIdentity(name)

	var source model.CanonicalModel
	switch err := tx.Where("normalized_name = ?", normalized).First(&source).Error; {
	case err == nil:
		group, found, err := findGroupByNameTx(tx, source.Name)
		if err != nil {
			return remapped, err
		}
		if found && group.ID != targetGroup.ID {
			remapped.sourceGroupID = group.ID
			channelIDs, err := moveGroupItemsTx(tx, group.ID, targetGroup, normalized)
			if err != nil {
				return remapped, err
			}
			remapped.affectedChannelIDs = append(remapped.affectedChannelIDs, channelIDs...)
		}
		channelIDs, err := mergeCanonicalTx(tx, source, *target)
		if err != nil {
			return remapped, err
		}
		remapped.affectedChannelIDs = append(remapped.affectedChannelIDs, channelIDs...)
		result.CanonicalsMerged++
	case err != gorm.ErrRecordNotFound:
		return remapped, err
	default:
		moved, err := moveExistingAliasWiringTx(tx, normalized, target, targetGroup)
		if err != nil {
			return remapped, err
		}
		remapped = moved
	}

	created, err := ensureAliasTx(tx, name, target.ID)
	if err != nil {
		return remapped, err
	}
	if created {
		result.AliasesCreated++
	}
	remapped.affectedChannelIDs = uniqueInts(remapped.affectedChannelIDs)
	return remapped, nil
}

func moveExistingAliasWiringTx(
	tx *gorm.DB,
	normalizedAlias string,
	target *model.CanonicalModel,
	targetGroup *model.Group,
) (catalogRemapResult, error) {
	remapped := catalogRemapResult{}
	var alias model.ModelAlias
	switch err := tx.Where("normalized_alias = ?", normalizedAlias).First(&alias).Error; {
	case err == gorm.ErrRecordNotFound:
		return remapped, nil
	case err != nil:
		return remapped, err
	case alias.CanonicalModelID == target.ID:
		return remapped, nil
	}

	var source model.CanonicalModel
	if err := tx.First(&source, alias.CanonicalModelID).Error; err != nil {
		return remapped, err
	}
	group, found, err := findGroupByNameTx(tx, source.Name)
	if err != nil {
		return remapped, err
	}
	if found && group.ID != targetGroup.ID {
		remapped.sourceGroupID = group.ID
		channelIDs, err := moveGroupItemsTx(tx, group.ID, targetGroup, normalizedAlias)
		if err != nil {
			return remapped, err
		}
		remapped.affectedChannelIDs = append(remapped.affectedChannelIDs, channelIDs...)
	}
	channelIDs, err := moveRouteCandidatesByUpstreamTx(
		tx,
		source.ID,
		target.ID,
		normalizedAlias,
	)
	if err != nil {
		return remapped, err
	}
	remapped.affectedChannelIDs = append(remapped.affectedChannelIDs, channelIDs...)
	return remapped, nil
}

// mergeCanonicalTx 把 source 的路由候选与别名并入 target，然后删除 source。
// 目标下已存在同「渠道 + 上游模型名」的候选时保留目标候选，并迁移依赖引用。
func mergeCanonicalTx(
	tx *gorm.DB,
	source model.CanonicalModel,
	target model.CanonicalModel,
) ([]int, error) {
	if source.ID == target.ID {
		return nil, nil
	}

	var candidates []model.RouteCandidate
	if err := tx.Where("canonical_model_id = ?", source.ID).Find(&candidates).Error; err != nil {
		return nil, err
	}
	channelIDs := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		if err := moveRouteCandidateTx(tx, candidate, target.ID); err != nil {
			return nil, err
		}
		channelIDs = append(channelIDs, candidate.ChannelID)
	}

	if err := tx.Model(&model.ModelAlias{}).Where("canonical_model_id = ?", source.ID).
		Update("canonical_model_id", target.ID).Error; err != nil {
		return nil, err
	}
	if err := moveCanonicalReferencesTx(tx, source.ID, target.ID); err != nil {
		return nil, err
	}
	if err := tx.Delete(&model.CanonicalModel{}, source.ID).Error; err != nil {
		return nil, err
	}
	return uniqueInts(channelIDs), nil
}

func moveRouteCandidatesByUpstreamTx(
	tx *gorm.DB,
	sourceCanonicalID int,
	targetCanonicalID int,
	normalizedUpstream string,
) ([]int, error) {
	var candidates []model.RouteCandidate
	if err := tx.Where(
		"canonical_model_id = ? AND LOWER(upstream_model_name) = ?",
		sourceCanonicalID,
		normalizedUpstream,
	).Find(&candidates).Error; err != nil {
		return nil, err
	}
	channelIDs := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		if err := moveRouteCandidateTx(tx, candidate, targetCanonicalID); err != nil {
			return nil, err
		}
		channelIDs = append(channelIDs, candidate.ChannelID)
	}
	return uniqueInts(channelIDs), nil
}

func moveRouteCandidateTx(
	tx *gorm.DB,
	candidate model.RouteCandidate,
	targetCanonicalID int,
) error {
	var conflict model.RouteCandidate
	err := tx.Where(
		"canonical_model_id = ? AND channel_id = ? AND LOWER(upstream_model_name) = ?",
		targetCanonicalID,
		candidate.ChannelID,
		NormalizeModelIdentity(candidate.UpstreamModelName),
	).First(&conflict).Error
	switch {
	case err == nil:
		if err := moveRouteCandidateReferencesTx(tx, candidate.ID, conflict.ID); err != nil {
			return err
		}
		return tx.Delete(&model.RouteCandidate{}, candidate.ID).Error
	case err == gorm.ErrRecordNotFound:
		return tx.Model(&model.RouteCandidate{}).Where("id = ?", candidate.ID).
			Update("canonical_model_id", targetCanonicalID).Error
	default:
		return err
	}
}

func moveGroupItemsTx(
	tx *gorm.DB,
	sourceGroupID int,
	targetGroup *model.Group,
	normalizedModel string,
) ([]int, error) {
	if sourceGroupID <= 0 || targetGroup == nil || sourceGroupID == targetGroup.ID {
		return nil, nil
	}
	var items []model.GroupItem
	query := tx.Where("group_id = ?", sourceGroupID)
	if normalizedModel != "" {
		query = query.Where("LOWER(model_name) = ?", normalizedModel)
	}
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}

	channelIDs := make([]int, 0, len(items))
	for _, item := range items {
		var conflict model.GroupItem
		err := tx.Where(
			"group_id = ? AND channel_id = ? AND LOWER(model_name) = ?",
			targetGroup.ID,
			item.ChannelID,
			NormalizeModelIdentity(item.ModelName),
		).First(&conflict).Error
		switch {
		case err == nil:
			if err := tx.Delete(&model.GroupItem{}, item.ID).Error; err != nil {
				return nil, err
			}
			appendGroupItemIfMissing(targetGroup, conflict)
		case err == gorm.ErrRecordNotFound:
			if err := tx.Model(&model.GroupItem{}).Where("id = ?", item.ID).
				Update("group_id", targetGroup.ID).Error; err != nil {
				return nil, err
			}
			item.GroupID = targetGroup.ID
			appendGroupItemIfMissing(targetGroup, item)
		default:
			return nil, err
		}
		channelIDs = append(channelIDs, item.ChannelID)
	}
	return uniqueInts(channelIDs), nil
}

func appendGroupItemIfMissing(group *model.Group, item model.GroupItem) {
	for _, existing := range group.Items {
		if existing.ID == item.ID {
			return
		}
	}
	group.Items = append(group.Items, item)
}

// ensureAliasTx 建立「上游名 → 目标 Canonical Model」的别名；已指向别处时改指向，保证重映射可重复执行。
func ensureAliasTx(tx *gorm.DB, name string, canonicalModelID int) (bool, error) {
	normalized := NormalizeModelIdentity(name)
	var existing model.ModelAlias
	switch err := tx.Where("normalized_alias = ?", normalized).First(&existing).Error; {
	case err == nil:
		if existing.CanonicalModelID == canonicalModelID {
			return false, nil
		}
		return false, tx.Model(&model.ModelAlias{}).Where("id = ?", existing.ID).
			Updates(map[string]any{
				"canonical_model_id": canonicalModelID,
				"manual":             true,
			}).Error
	case err != gorm.ErrRecordNotFound:
		return false, err
	}

	alias := model.ModelAlias{
		CanonicalModelID: canonicalModelID,
		Alias:            strings.TrimSpace(name),
		NormalizedAlias:  normalized,
		Manual:           true,
	}
	if err := tx.Create(&alias).Error; err != nil {
		return false, err
	}
	return true, nil
}
