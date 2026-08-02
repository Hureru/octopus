package op

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"gorm.io/gorm"
)

const routeCandidateArchiveAfter = 30 * 24 * time.Hour

var (
	catalogCacheMu        sync.RWMutex
	canonicalByNormalized = map[string]model.CanonicalModel{}
	aliasToCanonical      = map[string]int{}
	canonicalByID         = map[int]model.CanonicalModel{}
)

func NormalizeModelIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func catalogRefreshCache(ctx context.Context) error {
	var canonicals []model.CanonicalModel
	if err := db.GetDB().WithContext(ctx).Find(&canonicals).Error; err != nil {
		return err
	}
	var aliases []model.ModelAlias
	if err := db.GetDB().WithContext(ctx).Find(&aliases).Error; err != nil {
		return err
	}

	byNormalized := make(map[string]model.CanonicalModel, len(canonicals))
	byID := make(map[int]model.CanonicalModel, len(canonicals))
	for _, item := range canonicals {
		byNormalized[item.NormalizedName] = item
		byID[item.ID] = item
	}
	aliasMap := make(map[string]int, len(aliases))
	for _, item := range aliases {
		aliasMap[item.NormalizedAlias] = item.CanonicalModelID
	}

	catalogCacheMu.Lock()
	canonicalByNormalized = byNormalized
	canonicalByID = byID
	aliasToCanonical = aliasMap
	catalogCacheMu.Unlock()
	return nil
}

func CatalogResolveRequest(name string) (model.CanonicalModel, bool) {
	canonical, ok := CatalogResolveIdentity(name)
	return canonical, ok && canonical.Enabled
}

func CatalogResolveIdentity(name string) (model.CanonicalModel, bool) {
	normalized := NormalizeModelIdentity(name)
	if normalized == "" {
		return model.CanonicalModel{}, false
	}
	catalogCacheMu.RLock()
	defer catalogCacheMu.RUnlock()
	if canonical, ok := canonicalByNormalized[normalized]; ok {
		return canonical, true
	}
	if canonicalID, ok := aliasToCanonical[normalized]; ok {
		canonical, exists := canonicalByID[canonicalID]
		return canonical, exists
	}
	return model.CanonicalModel{}, false
}

func CatalogSync(ctx context.Context) (model.CatalogSyncResult, error) {
	now := time.Now()
	result := model.CatalogSyncResult{}
	channels := channelCache.GetAll()
	if len(channels) == 0 {
		if err := catalogRefreshCache(ctx); err != nil {
			return result, err
		}
		return result, nil
	}

	var bindings []model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Find(&bindings).Error; err != nil {
		return result, err
	}
	bindingByChannel := make(map[int]model.SiteChannelBinding, len(bindings))
	groupIDs := make([]int, 0, len(bindings))
	for _, binding := range bindings {
		bindingByChannel[binding.ChannelID] = binding
		if binding.SiteUserGroupID != nil {
			groupIDs = append(groupIDs, *binding.SiteUserGroupID)
		}
	}
	nonAuthoritativeChannelIDs := make(map[int]struct{})
	if len(groupIDs) > 0 {
		var groups []model.SiteUserGroup
		if err := db.GetDB().WithContext(ctx).Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
			return result, err
		}
		groupByID := make(map[int]model.SiteUserGroup, len(groups))
		for _, group := range groups {
			groupByID[group.ID] = group
		}
		for _, binding := range bindings {
			if binding.SiteUserGroupID == nil {
				continue
			}
			group, ok := groupByID[*binding.SiteUserGroupID]
			if !ok || !group.ModelSyncAuthoritative {
				nonAuthoritativeChannelIDs[binding.ChannelID] = struct{}{}
			}
		}
	}

	provisioning := CatalogGroupProvisioningMode()
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var canonicals []model.CanonicalModel
		if err := tx.Find(&canonicals).Error; err != nil {
			return err
		}
		canonicalByName := make(map[string]*model.CanonicalModel, len(canonicals))
		canonicalByRecordID := make(map[int]*model.CanonicalModel, len(canonicals))
		for i := range canonicals {
			canonicalByName[canonicals[i].NormalizedName] = &canonicals[i]
			canonicalByRecordID[canonicals[i].ID] = &canonicals[i]
		}

		var groups []model.Group
		if err := tx.Preload("Items").Find(&groups).Error; err != nil {
			return err
		}
		groupByNormalized := make(map[string]*model.Group, len(groups))
		for i := range groups {
			groupByNormalized[NormalizeModelIdentity(groups[i].Name)] = &groups[i]
		}

		var aliases []model.ModelAlias
		if err := tx.Find(&aliases).Error; err != nil {
			return err
		}
		aliasCanonical := make(map[string]int, len(aliases))
		for _, alias := range aliases {
			aliasCanonical[alias.NormalizedAlias] = alias.CanonicalModelID
		}

		type source struct {
			channel       model.Channel
			canonicalName string
			upstreamName  string
			priority      int
			weight        int
		}
		sourceByKey := make(map[string]source)
		addSource := func(item source) {
			canonicalName := NormalizeModelIdentity(item.canonicalName)
			upstreamName := strings.TrimSpace(item.upstreamName)
			if canonicalName == "" || upstreamName == "" {
				return
			}
			if item.weight <= 0 {
				item.weight = 1
			}
			key := fmt.Sprintf(
				"%s\x00%d\x00%s",
				canonicalName,
				item.channel.ID,
				upstreamName,
			)
			sourceByKey[key] = item
		}
		channelIDs := make([]int, 0, len(channels))
		for _, channel := range channels {
			channelIDs = append(channelIDs, channel.ID)
			for _, name := range splitChannelModelNames(channel.Model, channel.CustomModel) {
				addSource(source{
					channel:       channel,
					canonicalName: name,
					upstreamName:  name,
					weight:        1,
				})
			}
		}
		for _, group := range groups {
			for _, item := range group.Items {
				channel, ok := channelCache.Get(item.ChannelID)
				if !ok {
					continue
				}
				addSource(source{
					channel:       channel,
					canonicalName: group.Name,
					upstreamName:  item.ModelName,
					priority:      item.Priority,
					weight:        item.Weight,
				})
			}
		}
		sources := make([]source, 0, len(sourceByKey))
		for _, item := range sourceByKey {
			sources = append(sources, item)
		}
		sort.Slice(sources, func(i, j int) bool {
			left := NormalizeModelIdentity(sources[i].canonicalName)
			right := NormalizeModelIdentity(sources[j].canonicalName)
			if left == right {
				if sources[i].channel.ID == sources[j].channel.ID {
					return sources[i].upstreamName < sources[j].upstreamName
				}
				return sources[i].channel.ID < sources[j].channel.ID
			}
			return left < right
		})

		seenCandidateIDs := make(map[int]struct{})
		// 同一个模型可能来自多个渠道，跳过数按模型去重，与界面「N 个模型未选中」的口径一致。
		skippedModels := make(map[string]struct{})
		for _, item := range sources {
			normalized := NormalizeModelIdentity(item.canonicalName)
			if normalized == "" {
				continue
			}

			canonical := canonicalByName[normalized]
			if canonical == nil {
				if canonicalID := aliasCanonical[normalized]; canonicalID > 0 {
					canonical = canonicalByRecordID[canonicalID]
				}
			}
			namedGroup := groupByNormalized[normalized]
			if canonical == nil {
				// 手动供给模式下只接纳用户已经建好分组的模型，其余留给「模型发现」界面挑选，
				// 否则接一个聚合站就会凭空冒出几百个分组。
				if provisioning == model.CatalogGroupProvisioningManual && namedGroup == nil {
					skippedModels[normalized] = struct{}{}
					continue
				}
				displayName := strings.TrimSpace(item.canonicalName)
				if namedGroup != nil {
					displayName = namedGroup.Name
				}
				created := model.CanonicalModel{
					Name:            displayName,
					NormalizedName:  normalized,
					Vendor:          modelvendor.Detect(displayName),
					RoutingStrategy: model.RoutingStrategyBalanced,
					ProtocolPolicy:  model.ProtocolPolicyAuto,
					Enabled:         true,
				}
				if err := tx.Create(&created).Error; err != nil {
					return err
				}
				canonical = &created
				canonicalByName[normalized] = canonical
				canonicalByRecordID[created.ID] = canonical
				result.CanonicalCreated++
			} else if canonical.Vendor == "" && !canonical.VendorManual {
				if vendor := modelvendor.Detect(canonical.Name); vendor != "" {
					if err := tx.Model(&model.CanonicalModel{}).Where("id = ?", canonical.ID).
						Update("vendor", vendor).Error; err != nil {
						return err
					}
					canonical.Vendor = vendor
				}
			}

			group := groupByNormalized[NormalizeModelIdentity(canonical.Name)]
			if group == nil {
				if provisioning == model.CatalogGroupProvisioningManual {
					skippedModels[normalized] = struct{}{}
					continue
				}
				created := model.Group{
					Name:              canonical.Name,
					Mode:              model.GroupModeRoundRobin,
					FirstTokenTimeOut: 0,
					SessionKeepTime:   0,
					MaxRetries:        3,
				}
				if err := tx.Create(&created).Error; err != nil {
					return err
				}
				group = &created
				groupByNormalized[NormalizeModelIdentity(created.Name)] = group
				result.GroupsCreated++
			}

			wiring := catalogWiring{
				canonical:    canonical,
				group:        group,
				channel:      item.channel,
				upstreamName: item.upstreamName,
				priority:     item.priority,
				weight:       item.weight,
				now:          now,
			}
			if binding, ok := bindingByChannel[item.channel.ID]; ok {
				wiring.binding = &binding
			}
			wired, err := catalogEnsureWiring(tx, wiring)
			if err != nil {
				return err
			}
			if wired.groupItemCreated {
				result.GroupItemsCreated++
			}
			if wired.candidateCreated {
				result.CandidatesCreated++
			}
			if wired.candidateUpdated {
				result.CandidatesUpdated++
			}
			if wired.candidateID > 0 {
				seenCandidateIDs[wired.candidateID] = struct{}{}
			}
		}

		result.Skipped = len(skippedModels)

		var existingCandidates []model.RouteCandidate
		query := tx.Where("manual = ?", false)
		if len(channelIDs) > 0 {
			query = query.Where("channel_id IN ?", channelIDs)
		}
		if err := query.Find(&existingCandidates).Error; err != nil {
			return err
		}
		for _, candidate := range existingCandidates {
			if _, ok := seenCandidateIDs[candidate.ID]; ok {
				continue
			}
			if _, nonAuthoritative := nonAuthoritativeChannelIDs[candidate.ChannelID]; nonAuthoritative {
				continue
			}
			if candidate.Status == model.RouteCandidateArchived {
				continue
			}
			unavailableSince := candidate.UnavailableSince
			if unavailableSince == nil {
				value := now
				unavailableSince = &value
				result.MarkedUnavailable++
			}
			updates := map[string]any{
				"status":            model.RouteCandidateUnavailable,
				"unavailable_since": unavailableSince,
			}
			if now.Sub(*unavailableSince) >= routeCandidateArchiveAfter {
				value := now
				updates["status"] = model.RouteCandidateArchived
				updates["archived_at"] = &value
				result.Archived++
			}
			if err := tx.Model(&candidate).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if err := groupRefreshCache(ctx); err != nil {
		return result, err
	}
	if err := catalogRefreshCache(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func ensureRouteCandidatesForGroupTx(tx *gorm.DB, groupID int) error {
	if tx == nil || groupID <= 0 {
		return nil
	}
	var group model.Group
	if err := tx.Preload("Items").First(&group, groupID).Error; err != nil {
		return err
	}
	normalized := NormalizeModelIdentity(group.Name)
	if normalized == "" {
		return nil
	}
	var canonical model.CanonicalModel
	err := tx.Where("normalized_name = ?", normalized).First(&canonical).Error
	if err == gorm.ErrRecordNotFound {
		var alias model.ModelAlias
		if aliasErr := tx.Where("normalized_alias = ?", normalized).First(&alias).Error; aliasErr != nil {
			if aliasErr == gorm.ErrRecordNotFound {
				return nil
			}
			return aliasErr
		}
		err = tx.First(&canonical, alias.CanonicalModelID).Error
	}
	if err != nil {
		return err
	}

	now := time.Now()
	seenCandidateIDs := make(map[int]struct{}, len(group.Items))
	for _, item := range group.Items {
		channel, ok := channelCache.Get(item.ChannelID)
		if !ok {
			continue
		}
		status := model.RouteCandidateActive
		if !channel.Enabled {
			status = model.RouteCandidateDisabled
		}
		candidate := model.RouteCandidate{
			CanonicalModelID:  canonical.ID,
			ChannelID:         item.ChannelID,
			UpstreamModelName: item.ModelName,
			Status:            status,
			Priority:          item.Priority,
			Weight:            max(item.Weight, 1),
			LastSeenAt:        now,
		}
		var binding model.SiteChannelBinding
		if bindingErr := tx.Where("channel_id = ?", item.ChannelID).First(&binding).Error; bindingErr == nil {
			siteID := binding.SiteID
			accountID := binding.SiteAccountID
			candidate.SiteID = &siteID
			candidate.SiteAccountID = &accountID
			candidate.SiteGroupKey, _ = model.ParseSiteChannelBindingKey(binding.GroupKey)
		} else if bindingErr != gorm.ErrRecordNotFound {
			return bindingErr
		}

		var existing model.RouteCandidate
		err := tx.Where(
			"canonical_model_id = ? AND channel_id = ? AND upstream_model_name = ?",
			candidate.CanonicalModelID,
			candidate.ChannelID,
			candidate.UpstreamModelName,
		).First(&existing).Error
		switch {
		case err == gorm.ErrRecordNotFound:
			if err := tx.Create(&candidate).Error; err != nil {
				return err
			}
			seenCandidateIDs[candidate.ID] = struct{}{}
		case err != nil:
			return err
		default:
			seenCandidateIDs[existing.ID] = struct{}{}
			if !existing.Manual {
				if err := tx.Model(&existing).Updates(map[string]any{
					"site_id":           candidate.SiteID,
					"site_account_id":   candidate.SiteAccountID,
					"site_group_key":    candidate.SiteGroupKey,
					"status":            projectedRouteCandidateStatus(existing.Status, candidate.Status, true),
					"priority":          candidate.Priority,
					"weight":            candidate.Weight,
					"last_seen_at":      candidate.LastSeenAt,
					"unavailable_since": nil,
					"archived_at":       nil,
				}).Error; err != nil {
					return err
				}
			}
		}
	}

	var projected []model.RouteCandidate
	if err := tx.Where(
		"canonical_model_id = ? AND manual = ?",
		canonical.ID,
		false,
	).Find(&projected).Error; err != nil {
		return err
	}
	for _, candidate := range projected {
		if _, ok := seenCandidateIDs[candidate.ID]; ok {
			continue
		}
		if candidate.Status == model.RouteCandidateArchived {
			continue
		}
		unavailableSince := candidate.UnavailableSince
		if unavailableSince == nil {
			value := now
			unavailableSince = &value
		}
		if err := tx.Model(&candidate).Updates(map[string]any{
			"status":            model.RouteCandidateUnavailable,
			"unavailable_since": unavailableSince,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func projectedRouteCandidateStatus(
	existing model.RouteCandidateStatus,
	projected model.RouteCandidateStatus,
	preserveStale bool,
) model.RouteCandidateStatus {
	if projected != model.RouteCandidateActive {
		return projected
	}
	if existing == model.RouteCandidateDegraded {
		return existing
	}
	if preserveStale && existing == model.RouteCandidateStale {
		return existing
	}
	return projected
}

func CatalogList(ctx context.Context) ([]model.CanonicalModel, error) {
	var items []model.CanonicalModel
	err := db.GetDB().WithContext(ctx).
		Preload("Aliases").
		Preload("RouteCandidates", func(query *gorm.DB) *gorm.DB {
			return query.Order("status ASC, priority ASC, id ASC")
		}).
		Order("normalized_name ASC").
		Find(&items).Error
	return items, err
}

func CatalogAliasUpsert(ctx context.Context, canonicalModelID int, alias string) (*model.ModelAlias, error) {
	normalized := NormalizeModelIdentity(alias)
	if canonicalModelID <= 0 || normalized == "" {
		return nil, fmt.Errorf("canonical model and alias are required")
	}
	var saved model.ModelAlias
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target model.CanonicalModel
		if err := tx.First(&target, canonicalModelID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("canonical model not found")
			}
			return err
		}

		var canonicalConflict model.CanonicalModel
		if err := tx.Where("normalized_name = ?", normalized).First(&canonicalConflict).Error; err == nil {
			return fmt.Errorf("alias conflicts with canonical model %q", canonicalConflict.Name)
		} else if err != gorm.ErrRecordNotFound {
			return err
		}

		var existing model.ModelAlias
		if err := tx.Where("normalized_alias = ?", normalized).First(&existing).Error; err == nil {
			if existing.CanonicalModelID != canonicalModelID {
				return fmt.Errorf("alias is already assigned to another canonical model")
			}
			if err := tx.Model(&existing).Updates(map[string]any{
				"alias":      strings.TrimSpace(alias),
				"manual":     true,
				"updated_at": time.Now(),
			}).Error; err != nil {
				return err
			}
			saved = existing
			saved.Alias = strings.TrimSpace(alias)
			saved.Manual = true
			return nil
		} else if err != gorm.ErrRecordNotFound {
			return err
		}

		saved = model.ModelAlias{
			CanonicalModelID: canonicalModelID,
			Alias:            strings.TrimSpace(alias),
			NormalizedAlias:  normalized,
			Manual:           true,
		}
		return tx.Create(&saved).Error
	})
	if err != nil {
		return nil, err
	}
	if err := catalogRefreshCache(ctx); err != nil {
		return nil, err
	}
	if err := db.GetDB().WithContext(ctx).Where("normalized_alias = ?", normalized).First(&saved).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func CatalogAliasDelete(ctx context.Context, id int) error {
	if id <= 0 {
		return fmt.Errorf("alias id is required")
	}
	if err := db.GetDB().WithContext(ctx).Delete(&model.ModelAlias{}, id).Error; err != nil {
		return err
	}
	return catalogRefreshCache(ctx)
}

func CatalogCanonicalUpdate(ctx context.Context, request model.CanonicalModel) (*model.CanonicalModel, error) {
	if request.ID <= 0 {
		return nil, fmt.Errorf("canonical model id is required")
	}
	var existing model.CanonicalModel
	if err := db.GetDB().WithContext(ctx).First(&existing, request.ID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("canonical model not found")
		}
		return nil, err
	}
	if name := strings.TrimSpace(request.Name); name != "" && name != existing.Name {
		return nil, fmt.Errorf("canonical model name is immutable; add an alias instead")
	}
	updates := map[string]any{
		"routing_strategy": request.RoutingStrategy.Normalize(),
		"protocol_policy":  request.ProtocolPolicy.Normalize(model.ProtocolPolicyAuto),
		"allow_lossy":      request.AllowLossy,
		"enabled":          request.Enabled,
		"manual":           true,
	}
	// 厂商留空表示不修改，交回自动识别；显式填写则锁定为人工维护。
	if vendor := strings.TrimSpace(request.Vendor); vendor != "" {
		updates["vendor"] = strings.ToLower(vendor)
		updates["vendor_manual"] = true
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.CanonicalModel{}).Where("id = ?", request.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := catalogRefreshCache(ctx); err != nil {
		return nil, err
	}
	var saved model.CanonicalModel
	if err := db.GetDB().WithContext(ctx).Preload("Aliases").Preload("RouteCandidates").First(&saved, request.ID).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func CatalogRouteCandidateUpdate(ctx context.Context, request model.RouteCandidate) (*model.RouteCandidate, error) {
	if request.ID <= 0 {
		return nil, fmt.Errorf("route candidate id is required")
	}
	switch request.Status {
	case model.RouteCandidateActive, model.RouteCandidateDegraded, model.RouteCandidateStale,
		model.RouteCandidateUnavailable, model.RouteCandidateDisabled, model.RouteCandidateArchived:
	default:
		return nil, fmt.Errorf("invalid route candidate status")
	}
	updates := map[string]any{
		"status":          request.Status,
		"priority":        request.Priority,
		"weight":          request.Weight,
		"protocol_policy": request.ProtocolPolicy,
		"allow_lossy":     request.AllowLossy,
		"manual":          true,
	}
	if request.Status != model.RouteCandidateArchived {
		updates["archived_at"] = nil
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.RouteCandidate{}).Where("id = ?", request.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	var saved model.RouteCandidate
	if err := db.GetDB().WithContext(ctx).First(&saved, request.ID).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func CatalogCandidateFor(ctx context.Context, canonicalModelID, channelID int, upstreamModel string) (*model.RouteCandidate, error) {
	if canonicalModelID <= 0 || channelID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var item model.RouteCandidate
	err := db.GetDB().WithContext(ctx).Where(
		"canonical_model_id = ? AND channel_id = ? AND upstream_model_name = ?",
		canonicalModelID,
		channelID,
		upstreamModel,
	).First(&item).Error
	return &item, err
}

func CatalogPlanGroup(
	ctx context.Context,
	requestedModel string,
	requirements model.ProtocolRouteRequirements,
	group model.Group,
) (model.Group, model.RoutePreview, *model.CanonicalModel, error) {
	requirements.Features = normalizeProtocolFeatures(requirements.Features)
	preview := model.RoutePreview{
		RequestedModel:  requestedModel,
		CanonicalModel:  group.Name,
		InboundProtocol: requirements.InboundProtocol,
		Features:        requirements.Features,
		Strategy:        model.RoutingStrategyManual,
		Decisions:       make([]model.RouteDecisionReason, 0, len(group.Items)),
	}
	canonicalValue, ok := CatalogResolveIdentity(requestedModel)
	if !ok {
		canonicalValue, ok = CatalogResolveIdentity(group.Name)
	}
	var canonical *model.CanonicalModel
	strategy := model.RoutingStrategyManual
	if ok {
		canonical = &canonicalValue
		preview.CanonicalModel = canonical.Name
		strategy = canonical.RoutingStrategy.Normalize()
		preview.Strategy = strategy
		if !canonical.Enabled {
			for _, item := range group.Items {
				preview.Decisions = append(preview.Decisions, model.RouteDecisionReason{
					ChannelID:     item.ChannelID,
					UpstreamModel: item.ModelName,
					Reason:        "canonical disabled",
				})
			}
			group.Items = nil
			return group, preview, canonical, nil
		}
	}

	var candidates []model.RouteCandidate
	if canonical != nil {
		if err := db.GetDB().WithContext(ctx).Where("canonical_model_id = ?", canonical.ID).Find(&candidates).Error; err != nil {
			return group, preview, canonical, err
		}
	}
	candidateByKey := make(map[string]model.RouteCandidate, len(candidates))
	candidateIDs := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		candidateByKey[routeCandidateKey(candidate.ChannelID, candidate.UpstreamModelName)] = candidate
		candidateIDs = append(candidateIDs, candidate.ID)
	}
	candidatePerformance, err := routeCandidatePerformanceMap(ctx, candidateIDs, time.Now())
	if err != nil {
		return group, preview, canonical, err
	}

	type scoredItem struct {
		item              model.GroupItem
		score             float64
		scoreKnown        bool
		rank              int
		candidatePriority int
		candidateWeight   int
	}
	included := make([]scoredItem, 0, len(group.Items))
	for _, item := range group.Items {
		decision := model.RouteDecisionReason{
			ChannelID:     item.ChannelID,
			UpstreamModel: item.ModelName,
		}
		candidate, exists := candidateByKey[routeCandidateKey(item.ChannelID, item.ModelName)]
		if exists {
			decision.RouteCandidateID = candidate.ID
			decision.Status = candidate.Status
		}
		channel, channelExists := channelCache.Get(item.ChannelID)
		if !channelExists || !channel.Enabled {
			decision.Reason = "channel disabled"
			preview.Decisions = append(preview.Decisions, decision)
			continue
		}
		if canonical != nil && len(candidates) > 0 && !exists {
			decision.Reason = "route candidate missing"
			preview.Decisions = append(preview.Decisions, decision)
			continue
		}
		if exists {
			switch candidate.Status {
			case model.RouteCandidateActive, model.RouteCandidateDegraded, model.RouteCandidateStale:
			default:
				decision.Reason = "candidate " + string(candidate.Status)
				preview.Decisions = append(preview.Decisions, decision)
				continue
			}
		}

		outboundProtocol := ProtocolForOutboundType(channel.Type)
		effectivePolicy, allowLossy := effectiveProtocolSettings(canonical, channel, candidate, exists)
		assessment := assessProtocolRouteWithMode(
			requirements,
			outboundProtocol,
			effectivePolicy,
			allowLossy,
			protocolExecutionModeForChannel(requirements, channel),
		)
		decision.OutboundProtocol = outboundProtocol
		decision.ProtocolMode = assessment.Mode
		decision.ProtocolPolicy = effectivePolicy
		decision.AllowLossy = allowLossy
		decision.Compatibility = assessment.Compatibility
		decision.Capabilities = assessment.Capabilities
		decision.Warnings = assessment.Warnings
		decision.Reason = assessment.Reason
		if !assessment.Included {
			preview.Decisions = append(preview.Decisions, decision)
			continue
		}

		candidatePriority := item.Priority
		candidateWeight := item.Weight
		if exists {
			if candidate.Manual || candidate.Priority != 0 {
				candidatePriority = candidate.Priority
			}
			if candidate.Manual || candidate.Weight != 1 {
				candidateWeight = candidate.Weight
			}
		}
		if candidateWeight <= 0 {
			candidateWeight = 1
		}
		score, scoreKnown := routeCandidateScore(
			ctx,
			strategy,
			candidate,
			candidatePerformance[candidate.ID],
			candidateWeight,
		)
		rank := protocolPreferenceRank(assessment)
		decision.Included = true
		if scoreKnown {
			decision.Score = score
		}
		preview.Decisions = append(preview.Decisions, decision)
		included = append(included, scoredItem{
			item:              item,
			score:             score,
			scoreKnown:        scoreKnown,
			rank:              rank,
			candidatePriority: candidatePriority,
			candidateWeight:   candidateWeight,
		})
	}

	if canonical == nil || len(candidates) == 0 {
		filtered := make([]model.GroupItem, 0, len(included))
		for _, item := range included {
			filtered = append(filtered, item.item)
		}
		group.Items = filtered
		return group, preview, canonical, nil
	}

	sort.SliceStable(included, func(i, j int) bool {
		left := included[i]
		right := included[j]
		if left.rank != right.rank {
			return left.rank > right.rank
		}
		if strategy == model.RoutingStrategyManual {
			if left.candidatePriority != right.candidatePriority {
				return left.candidatePriority < right.candidatePriority
			}
		} else {
			if left.scoreKnown != right.scoreKnown {
				return left.scoreKnown
			}
			if left.scoreKnown && left.score != right.score {
				return left.score > right.score
			}
		}
		if left.item.Priority != right.item.Priority {
			return left.item.Priority < right.item.Priority
		}
		if left.item.ChannelID != right.item.ChannelID {
			return left.item.ChannelID < right.item.ChannelID
		}
		return left.item.ModelName < right.item.ModelName
	})
	group.Items = make([]model.GroupItem, 0, len(included))
	for index, item := range included {
		item.item.Priority = index
		item.item.Weight = item.candidateWeight
		group.Items = append(group.Items, item.item)
	}
	group.Mode = model.GroupModeFailover
	return group, preview, canonical, nil
}

func effectiveProtocolSettings(
	canonical *model.CanonicalModel,
	channel model.Channel,
	candidate model.RouteCandidate,
	candidateExists bool,
) (model.ProtocolPolicy, bool) {
	policy := channel.ProtocolPolicy.Normalize(model.ProtocolPolicyAuto)
	allowLossy := channel.AllowLossy
	if canonical != nil &&
		(canonical.Manual || canonical.ProtocolPolicy != model.ProtocolPolicyAuto || canonical.AllowLossy) {
		policy = stricterProtocolPolicy(
			policy,
			canonical.ProtocolPolicy.Normalize(model.ProtocolPolicyAuto),
		)
		allowLossy = allowLossy && canonical.AllowLossy
	}
	if candidateExists {
		policy = stricterProtocolPolicy(policy, candidate.ProtocolPolicy.Normalize(policy))
		if candidate.AllowLossy != nil {
			allowLossy = allowLossy && *candidate.AllowLossy
		}
	}
	return policy, allowLossy
}

func stricterProtocolPolicy(left, right model.ProtocolPolicy) model.ProtocolPolicy {
	left = left.Normalize(model.ProtocolPolicyAuto)
	right = right.Normalize(model.ProtocolPolicyAuto)
	if left == model.ProtocolPolicyPassthroughOnly || right == model.ProtocolPolicyPassthroughOnly {
		return model.ProtocolPolicyPassthroughOnly
	}
	if left == model.ProtocolPolicyAuto || right == model.ProtocolPolicyAuto {
		return model.ProtocolPolicyAuto
	}
	return model.ProtocolPolicyTransformAllowed
}

func protocolPreferenceRank(assessment protocolRouteAssessment) int {
	if assessment.Mode == model.ProtocolExecutionModePassthrough {
		return 3
	}
	if assessment.Compatibility == model.ProtocolCompatibilityExact {
		return 2
	}
	if assessment.Compatibility == model.ProtocolCompatibilityLossy {
		return 1
	}
	return 0
}

func routeCandidateScore(
	ctx context.Context,
	strategy model.RoutingStrategy,
	candidate model.RouteCandidate,
	performance routeCandidatePerformance,
	weight int,
) (float64, bool) {
	total := performance.SuccessCount + performance.FailureCount
	reliability := 0.5
	latency := float64(60_000)
	if total > 0 {
		reliability = float64(performance.SuccessCount) / float64(total)
		if performance.SuccessCount > 0 {
			latency = float64(performance.SuccessDurationMS) / float64(performance.SuccessCount)
		}
	}
	priceValue := math.Inf(1)
	if candidate.ID > 0 {
		if effective, err := EffectivePriceForCandidate(
			ctx,
			candidate.ID,
			candidate.UpstreamModelName,
		); err == nil &&
			effective.Source != model.PriceQuoteSourceUnknown &&
			effective.Unit != model.PriceUnitPerRequest &&
			effective.PerRequest == 0 &&
			effective.Convertible &&
			effective.ExchangeRateToUSD > 0 {
			priceValue = (effective.Input + effective.Output) * effective.ExchangeRateToUSD
		}
	}
	switch strategy {
	case model.RoutingStrategyReliability:
		if total == 0 {
			return 0, false
		}
		return reliability * 10_000, true
	case model.RoutingStrategyLowestCost:
		if math.IsInf(priceValue, 1) {
			return 0, false
		}
		return -priceValue, true
	case model.RoutingStrategyLowestLatency:
		if performance.SuccessCount == 0 {
			return 0, false
		}
		return -latency, true
	default:
		costPenalty := 0.0
		if !math.IsInf(priceValue, 1) {
			costPenalty = math.Log1p(priceValue) * 20
		}
		weightBonus := math.Log1p(float64(max(weight, 1))) * 10
		statusPenalty := 0.0
		switch candidate.Status {
		case model.RouteCandidateDegraded:
			statusPenalty = 250
		case model.RouteCandidateStale:
			statusPenalty = 100
		}
		return reliability*1000 - latency/100 - costPenalty + weightBonus - statusPenalty, true
	}
}

func routeCandidateKey(channelID int, modelName string) string {
	return fmt.Sprintf("%d\x00%s", channelID, modelName)
}

func ProtocolForOutboundType(value outbound.OutboundType) model.ProtocolName {
	switch value {
	case outbound.OutboundTypeOpenAIResponse:
		return model.ProtocolOpenAIResponses
	case outbound.OutboundTypeAnthropic:
		return model.ProtocolAnthropic
	case outbound.OutboundTypeGemini:
		return model.ProtocolGemini
	case outbound.OutboundTypeVolcengine:
		return model.ProtocolVolcengine
	case outbound.OutboundTypeOpenAIEmbedding:
		return model.ProtocolOpenAIEmbedding
	default:
		return model.ProtocolOpenAIChat
	}
}

func protocolExecutionModeForChannel(
	requirements model.ProtocolRouteRequirements,
	channel model.Channel,
) model.ProtocolExecutionMode {
	outboundProtocol := ProtocolForOutboundType(channel.Type)
	if requirements.InboundProtocol != outboundProtocol {
		return model.ProtocolExecutionModeTransform
	}
	if outboundProtocol != model.ProtocolOpenAIResponses ||
		(!hasProtocolFeature(requirements.Features, model.ProtocolFeatureContinuation) &&
			!hasProtocolFeature(requirements.Features, model.ProtocolFeatureWebSocket)) {
		return protocolExecutionMode(requirements, outboundProtocol)
	}

	mode := channel.WSMode.Normalize()
	if mode == model.ChannelWSModeInherit {
		value, _ := SettingGetString(model.SettingKeyResponsesWSDefaultMode)
		mode = model.ChannelWSMode(strings.TrimSpace(value)).Normalize()
	}
	if mode != model.ChannelWSModePassthrough {
		return model.ProtocolExecutionModeTransform
	}
	if hasProtocolFeature(requirements.Features, model.ProtocolFeatureWebSocket) {
		enabled, _ := SettingGetBool(model.SettingKeyResponsesWSEnabled)
		if !enabled {
			return model.ProtocolExecutionModeTransform
		}
	}
	return model.ProtocolExecutionModePassthrough
}

func hasProtocolFeature(features []model.ProtocolFeature, target model.ProtocolFeature) bool {
	for _, feature := range features {
		if feature == target {
			return true
		}
	}
	return false
}

func ProtocolTransformSupported(inboundProtocol, outboundProtocol model.ProtocolName) bool {
	if inboundProtocol == outboundProtocol {
		return true
	}
	switch inboundProtocol {
	case model.ProtocolOpenAIChat, model.ProtocolOpenAIResponses, model.ProtocolAnthropic:
		switch outboundProtocol {
		case model.ProtocolOpenAIChat, model.ProtocolOpenAIResponses, model.ProtocolAnthropic,
			model.ProtocolGemini, model.ProtocolVolcengine:
			return true
		}
	case model.ProtocolOpenAIEmbedding:
		return outboundProtocol == model.ProtocolOpenAIEmbedding
	}
	return false
}
