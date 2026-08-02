package op

import (
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

// remapModelToCanonicalTx 把上游模型名挂到目标 Canonical Model 上。
// 该名字若已被自动建成独立的 Canonical Model，则连同路由候选一并合入目标；
// 返回可能因此变得冗余的原分组 ID（0 表示没有）。
func remapModelToCanonicalTx(
	tx *gorm.DB,
	name string,
	target *model.CanonicalModel,
	result *model.CatalogProvisionResult,
) (int, error) {
	normalized := NormalizeModelIdentity(name)
	sourceGroupID := 0

	var source model.CanonicalModel
	switch err := tx.Where("normalized_name = ?", normalized).First(&source).Error; {
	case err == nil:
		group, found, groupErr := findGroupByNameTx(tx, normalized)
		if groupErr != nil {
			return 0, groupErr
		}
		if found {
			sourceGroupID = group.ID
		}
		if err := mergeCanonicalTx(tx, source, *target); err != nil {
			return 0, err
		}
		result.CanonicalsMerged++
	case err != gorm.ErrRecordNotFound:
		return 0, err
	}

	created, err := ensureAliasTx(tx, name, target.ID)
	if err != nil {
		return 0, err
	}
	if created {
		result.AliasesCreated++
	}
	return sourceGroupID, nil
}

// mergeCanonicalTx 把 source 的路由候选与别名并入 target，然后删除 source。
// 目标下已存在同「渠道 + 上游模型名」的候选时丢弃源候选，避免撞上唯一索引。
func mergeCanonicalTx(tx *gorm.DB, source, target model.CanonicalModel) error {
	if source.ID == target.ID {
		return nil
	}

	var candidates []model.RouteCandidate
	if err := tx.Where("canonical_model_id = ?", source.ID).Find(&candidates).Error; err != nil {
		return err
	}
	for _, candidate := range candidates {
		var conflict model.RouteCandidate
		err := tx.Where(
			"canonical_model_id = ? AND channel_id = ? AND upstream_model_name = ?",
			target.ID,
			candidate.ChannelID,
			candidate.UpstreamModelName,
		).First(&conflict).Error
		switch {
		case err == nil:
			if err := tx.Delete(&model.RouteCandidate{}, candidate.ID).Error; err != nil {
				return err
			}
		case err == gorm.ErrRecordNotFound:
			if err := tx.Model(&model.RouteCandidate{}).Where("id = ?", candidate.ID).
				Update("canonical_model_id", target.ID).Error; err != nil {
				return err
			}
		default:
			return err
		}
	}

	if err := tx.Model(&model.ModelAlias{}).Where("canonical_model_id = ?", source.ID).
		Update("canonical_model_id", target.ID).Error; err != nil {
		return err
	}
	return tx.Delete(&model.CanonicalModel{}, source.ID).Error
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
