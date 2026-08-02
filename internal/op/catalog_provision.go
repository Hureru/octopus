package op

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

// CatalogProvision 把选中的上游模型纳入分组。
//
// TargetName 为空时每个模型各自建立同名分组；非空时全部映射到该分组，
// 例如把 z-ai/glm-5.2 映射到 glm-5.2 —— 上游名保留在 group item 上，
// 客户端用任一名字请求都会落到同一个分组（见 relay 的 CatalogResolveIdentity）。
func CatalogProvision(
	ctx context.Context,
	request model.CatalogProvisionRequest,
) (model.CatalogProvisionResult, error) {
	result := model.CatalogProvisionResult{}
	models := normalizeModelNameList(request.Models)
	if len(models) == 0 {
		return result, newCatalogProvisionBadRequestError("at least one model is required")
	}
	targetName := strings.TrimSpace(request.TargetName)
	if strings.Contains(targetName, ",") {
		return result, newCatalogProvisionBadRequestError("target name must be a single group name")
	}

	sourcesByModel := channelSourcesByModel(models)
	bindingByChannel, err := provisionBindingIndex(ctx, sourcesByModel)
	if err != nil {
		return result, err
	}

	now := time.Now()
	deletableGroupIDs := make([]int, 0)
	affectedChannelIDs := make([]int, 0)
	var deletedGroups []model.Group

	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, name := range models {
			target := targetName
			if target == "" {
				target = name
			}
			canonical, err := ensureCanonicalTx(tx, target, &result)
			if err != nil {
				return err
			}
			group, err := ensureGroupTx(tx, canonical.Name, &result)
			if err != nil {
				return err
			}

			if NormalizeModelIdentity(name) != canonical.NormalizedName {
				sourceGroupID, err := remapModelToCanonicalTx(tx, name, canonical, &result)
				if err != nil {
					return err
				}
				if sourceGroupID > 0 && sourceGroupID != group.ID && request.DeleteEmptySourceGroups {
					deletableGroupIDs = append(deletableGroupIDs, sourceGroupID)
				}
			}

			for _, source := range sourcesByModel[NormalizeModelIdentity(name)] {
				wiring := catalogWiring{
					canonical:    canonical,
					group:        group,
					channel:      source.channel,
					upstreamName: source.upstreamName,
					now:          now,
				}
				if binding, ok := bindingByChannel[source.channel.ID]; ok {
					wiring.binding = &binding
				}
				wired, err := catalogEnsureWiring(tx, wiring)
				if err != nil {
					return err
				}
				if wired.groupItemCreated {
					result.GroupItemsCreated++
				}
				affectedChannelIDs = append(affectedChannelIDs, source.channel.ID)
			}
		}

		removed, err := deleteRedundantGroupsTx(tx, deletableGroupIDs, models)
		if err != nil {
			return err
		}
		deletedGroups = removed
		return nil
	})
	if err != nil {
		return model.CatalogProvisionResult{}, err
	}

	result.GroupsDeleted = len(deletedGroups)
	evictGroupCache(deletedGroups)
	if err := refreshCatalogCaches(ctx); err != nil {
		return result, err
	}
	resetBalancerStateForChannels(affectedChannelIDs...)
	return result, nil
}

// provisionBindingIndex 一次性取出涉及渠道的站点绑定，避免在事务里逐条查询。
func provisionBindingIndex(
	ctx context.Context,
	sourcesByModel map[string][]provisionSource,
) (map[int]model.SiteChannelBinding, error) {
	channelIDs := make([]int, 0)
	for _, sources := range sourcesByModel {
		for _, source := range sources {
			channelIDs = append(channelIDs, source.channel.ID)
		}
	}
	return SiteChannelBindingMapByChannelIDs(uniqueInts(channelIDs), ctx)
}

func evictGroupCache(groups []model.Group) {
	for _, group := range groups {
		groupCache.Del(group.ID)
		groupMap.Del(group.Name)
	}
}

// CatalogUnprovision 把选中的上游模型移出分组体系：映射过的删别名，自建组的按需连分组一起删。
func CatalogUnprovision(
	ctx context.Context,
	request model.CatalogUnprovisionRequest,
) (model.CatalogUnprovisionResult, error) {
	result := model.CatalogUnprovisionResult{}
	models := normalizeModelNameList(request.Models)
	if len(models) == 0 {
		return result, newCatalogProvisionBadRequestError("at least one model is required")
	}

	groupIDsToDelete := make([]int, 0)
	affectedChannelIDs := make([]int, 0)
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, name := range models {
			normalized := NormalizeModelIdentity(name)

			// 分组条目与路由候选按上游名清理：只删别名的话，该模型仍会被目标分组继续路由。
			removedItems, channelIDs, err := removeGroupItemsByModelTx(tx, normalized)
			if err != nil {
				return err
			}
			result.GroupItemsRemoved += removedItems
			affectedChannelIDs = append(affectedChannelIDs, channelIDs...)
			if err := tx.Where("LOWER(upstream_model_name) = ?", normalized).
				Delete(&model.RouteCandidate{}).Error; err != nil {
				return err
			}

			var alias model.ModelAlias
			switch err := tx.Where("normalized_alias = ?", normalized).First(&alias).Error; {
			case err == nil:
				if err := tx.Delete(&model.ModelAlias{}, alias.ID).Error; err != nil {
					return err
				}
				result.AliasesRemoved++
				continue
			case err != gorm.ErrRecordNotFound:
				return err
			}

			var canonical model.CanonicalModel
			switch err := tx.Where("normalized_name = ?", normalized).First(&canonical).Error; {
			case err == gorm.ErrRecordNotFound:
				continue
			case err != nil:
				return err
			}

			// canonical 是「目标分组」时，指向它的别名上游（如 z-ai/glm-5.2 → glm-5.2）
			// 也引用同一个分组条目，必须一并清理，否则 canonical 删除后分组里会残留死条目。
			var aliases []model.ModelAlias
			if err := tx.Where("canonical_model_id = ?", canonical.ID).Find(&aliases).Error; err != nil {
				return err
			}
			for _, item := range aliases {
				if item.NormalizedAlias == normalized {
					continue
				}
				removedItems, channelIDs, err := removeGroupItemsByModelTx(tx, item.NormalizedAlias)
				if err != nil {
					return err
				}
				result.GroupItemsRemoved += removedItems
				affectedChannelIDs = append(affectedChannelIDs, channelIDs...)
			}

			if request.DeleteGroup {
				group, found, err := findGroupByNameTx(tx, canonical.Name)
				if err != nil {
					return err
				}
				if found {
					groupIDsToDelete = append(groupIDsToDelete, group.ID)
				}
			}

			if err := tx.Where("canonical_model_id = ?", canonical.ID).
				Delete(&model.RouteCandidate{}).Error; err != nil {
				return err
			}
			if err := tx.Where("canonical_model_id = ?", canonical.ID).
				Delete(&model.ModelAlias{}).Error; err != nil {
				return err
			}
			if err := tx.Delete(&model.CanonicalModel{}, canonical.ID).Error; err != nil {
				return err
			}
			result.CanonicalsRemoved++
		}
		return nil
	})
	if err != nil {
		return model.CatalogUnprovisionResult{}, err
	}

	// 分组删除走 GroupDel，让它负责 group items / presets / 负载均衡状态的连带清理。
	for _, groupID := range groupIDsToDelete {
		if err := GroupDel(groupID, ctx); err != nil {
			return result, fmt.Errorf("delete group %d failed: %w", groupID, err)
		}
		result.GroupsDeleted++
	}
	if err := refreshCatalogCaches(ctx); err != nil {
		return result, err
	}
	resetBalancerStateForChannels(affectedChannelIDs...)
	return result, nil
}

// removeGroupItemsByModelTx 删除所有引用该上游模型名的分组条目，返回删除条数与受影响渠道。
func removeGroupItemsByModelTx(tx *gorm.DB, normalizedModel string) (int, []int, error) {
	if normalizedModel == "" {
		return 0, nil, nil
	}
	var items []model.GroupItem
	if err := tx.Where("LOWER(model_name) = ?", normalizedModel).Find(&items).Error; err != nil {
		return 0, nil, err
	}
	if len(items) == 0 {
		return 0, nil, nil
	}
	channelIDs := make([]int, 0, len(items))
	for _, item := range items {
		channelIDs = append(channelIDs, item.ChannelID)
	}
	if err := tx.Where("LOWER(model_name) = ?", normalizedModel).
		Delete(&model.GroupItem{}).Error; err != nil {
		return 0, nil, err
	}
	return len(items), channelIDs, nil
}

func refreshCatalogCaches(ctx context.Context) error {
	if err := groupRefreshCache(ctx); err != nil {
		return err
	}
	return catalogRefreshCache(ctx)
}

// normalizeModelNameList 去重并保序，归一化后为空的条目直接丢弃。
func normalizeModelNameList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		normalized := NormalizeModelIdentity(name)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, name)
	}
	return result
}
