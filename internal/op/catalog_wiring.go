package op

import (
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// catalogWiring 描述「把某个渠道上的上游模型挂到目标分组与 Canonical Model 之下」所需的全部输入。
// CatalogSync 与 CatalogProvision 共用，保证两条路径产出的 group item / route candidate 完全一致。
type catalogWiring struct {
	canonical    *model.CanonicalModel
	group        *model.Group
	channel      model.Channel
	upstreamName string
	priority     int
	weight       int
	binding      *model.SiteChannelBinding
	now          time.Time
}

type catalogWiringResult struct {
	groupItemCreated bool
	candidateCreated bool
	candidateUpdated bool
	candidateID      int
}

// catalogEnsureWiring 幂等地补齐分组条目与路由候选，并把新建的条目回写到 wiring.group.Items，
// 便于同一批次内的后续调用看到最新的优先级基线。
func catalogEnsureWiring(tx *gorm.DB, wiring catalogWiring) (catalogWiringResult, error) {
	result := catalogWiringResult{}
	if tx == nil || wiring.canonical == nil || wiring.group == nil || wiring.upstreamName == "" {
		return result, nil
	}

	priority := wiring.priority
	weight := wiring.weight
	if weight <= 0 {
		weight = 1
	}

	hasGroupItem := false
	for _, groupItem := range wiring.group.Items {
		if groupItem.ChannelID == wiring.channel.ID && groupItem.ModelName == wiring.upstreamName {
			hasGroupItem = true
			// 渠道条目已存在时以它为准：CatalogProvision 不携带 priority，
			// 若直接落 0 会在重复供给时把路由候选的优先级覆盖成 0（见 review 问题 #2）。
			if priority <= 0 {
				priority = groupItem.Priority
			}
			if wiring.weight <= 0 {
				weight = groupItem.Weight
			}
			break
		}
	}
	if !hasGroupItem {
		groupItem := model.GroupItem{
			GroupID:   wiring.group.ID,
			ChannelID: wiring.channel.ID,
			ModelName: wiring.upstreamName,
			Priority:  len(wiring.group.Items) + 1,
			Weight:    1,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&groupItem).Error; err != nil {
			return result, err
		}
		wiring.group.Items = append(wiring.group.Items, groupItem)
		priority = groupItem.Priority
		weight = groupItem.Weight
		result.groupItemCreated = true
	}

	status := model.RouteCandidateActive
	if !wiring.channel.Enabled {
		status = model.RouteCandidateDisabled
	}
	candidate := model.RouteCandidate{
		CanonicalModelID:  wiring.canonical.ID,
		ChannelID:         wiring.channel.ID,
		UpstreamModelName: wiring.upstreamName,
		Status:            status,
		Priority:          priority,
		Weight:            weight,
		LastSeenAt:        wiring.now,
	}
	if wiring.binding != nil {
		siteID := wiring.binding.SiteID
		accountID := wiring.binding.SiteAccountID
		baseGroup, _ := model.ParseSiteChannelBindingKey(wiring.binding.GroupKey)
		candidate.SiteID = &siteID
		candidate.SiteAccountID = &accountID
		candidate.SiteGroupKey = baseGroup
	}

	var existing model.RouteCandidate
	err := tx.Where(
		"canonical_model_id = ? AND channel_id = ? AND upstream_model_name = ?",
		candidate.CanonicalModelID,
		candidate.ChannelID,
		candidate.UpstreamModelName,
	).First(&existing).Error
	switch {
	case err == nil:
		updates := map[string]any{
			"site_id":           candidate.SiteID,
			"site_account_id":   candidate.SiteAccountID,
			"site_group_key":    candidate.SiteGroupKey,
			"last_seen_at":      wiring.now,
			"unavailable_since": nil,
			"archived_at":       nil,
		}
		if !existing.Manual {
			updates["status"] = projectedRouteCandidateStatus(existing.Status, status, false)
			updates["priority"] = candidate.Priority
			updates["weight"] = candidate.Weight
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return result, err
		}
		result.candidateUpdated = true
		result.candidateID = existing.ID
	case err == gorm.ErrRecordNotFound:
		if err := tx.Create(&candidate).Error; err != nil {
			return result, err
		}
		result.candidateCreated = true
		result.candidateID = candidate.ID
	default:
		return result, err
	}
	return result, nil
}
