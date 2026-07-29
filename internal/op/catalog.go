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
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	normalized := NormalizeModelIdentity(name)
	if normalized == "" {
		return model.CanonicalModel{}, false
	}
	catalogCacheMu.RLock()
	defer catalogCacheMu.RUnlock()
	if canonical, ok := canonicalByNormalized[normalized]; ok && canonical.Enabled {
		return canonical, true
	}
	if canonicalID, ok := aliasToCanonical[normalized]; ok {
		canonical, exists := canonicalByID[canonicalID]
		return canonical, exists && canonical.Enabled
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

	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var canonicals []model.CanonicalModel
		if err := tx.Find(&canonicals).Error; err != nil {
			return err
		}
		canonicalByName := make(map[string]*model.CanonicalModel, len(canonicals))
		for i := range canonicals {
			canonicalByName[canonicals[i].NormalizedName] = &canonicals[i]
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
			channel model.Channel
			name    string
		}
		sources := make([]source, 0)
		channelIDs := make([]int, 0, len(channels))
		for _, channel := range channels {
			channelIDs = append(channelIDs, channel.ID)
			for _, name := range splitChannelModelNames(channel.Model, channel.CustomModel) {
				sources = append(sources, source{channel: channel, name: name})
			}
		}
		sort.Slice(sources, func(i, j int) bool {
			left := NormalizeModelIdentity(sources[i].name)
			right := NormalizeModelIdentity(sources[j].name)
			if left == right {
				return sources[i].channel.ID < sources[j].channel.ID
			}
			return left < right
		})

		seenCandidateIDs := make(map[int]struct{})
		for _, item := range sources {
			normalized := NormalizeModelIdentity(item.name)
			if normalized == "" {
				continue
			}

			canonical := canonicalByName[normalized]
			if canonical == nil {
				if canonicalID := aliasCanonical[normalized]; canonicalID > 0 {
					for i := range canonicals {
						if canonicals[i].ID == canonicalID {
							canonical = &canonicals[i]
							break
						}
					}
				}
			}
			if canonical == nil {
				displayName := strings.TrimSpace(item.name)
				if group := groupByNormalized[normalized]; group != nil {
					displayName = group.Name
				}
				created := model.CanonicalModel{
					Name:            displayName,
					NormalizedName:  normalized,
					RoutingStrategy: model.RoutingStrategyBalanced,
					ProtocolPolicy:  model.ProtocolPolicyAuto,
					Enabled:         true,
				}
				if err := tx.Create(&created).Error; err != nil {
					return err
				}
				canonicals = append(canonicals, created)
				canonical = &canonicals[len(canonicals)-1]
				canonicalByName[normalized] = canonical
				result.CanonicalCreated++
			}

			group := groupByNormalized[NormalizeModelIdentity(canonical.Name)]
			if group == nil {
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
				groups = append(groups, created)
				group = &groups[len(groups)-1]
				groupByNormalized[NormalizeModelIdentity(created.Name)] = group
				result.GroupsCreated++
			}

			hasGroupItem := false
			for _, groupItem := range group.Items {
				if groupItem.ChannelID == item.channel.ID && groupItem.ModelName == item.name {
					hasGroupItem = true
					break
				}
			}
			if !hasGroupItem {
				groupItem := model.GroupItem{
					GroupID:   group.ID,
					ChannelID: item.channel.ID,
					ModelName: item.name,
					Priority:  len(group.Items) + 1,
					Weight:    1,
				}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&groupItem).Error; err != nil {
					return err
				}
				group.Items = append(group.Items, groupItem)
				result.GroupItemsCreated++
			}

			status := model.RouteCandidateActive
			if !item.channel.Enabled {
				status = model.RouteCandidateDisabled
			}
			candidate := model.RouteCandidate{
				CanonicalModelID:  canonical.ID,
				ChannelID:         item.channel.ID,
				UpstreamModelName: item.name,
				Status:            status,
				Weight:            1,
				LastSeenAt:        now,
			}
			if binding, ok := bindingByChannel[item.channel.ID]; ok {
				siteID := binding.SiteID
				accountID := binding.SiteAccountID
				baseGroup, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
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
					"last_seen_at":      now,
					"unavailable_since": nil,
					"archived_at":       nil,
				}
				if !existing.Manual {
					updates["status"] = status
				}
				if err := tx.Model(&existing).Updates(updates).Error; err != nil {
					return err
				}
				seenCandidateIDs[existing.ID] = struct{}{}
				result.CandidatesUpdated++
			case err == gorm.ErrRecordNotFound:
				if err := tx.Create(&candidate).Error; err != nil {
					return err
				}
				seenCandidateIDs[candidate.ID] = struct{}{}
				result.CandidatesCreated++
			default:
				return err
			}
		}

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
	canonicalValue, ok := CatalogResolveRequest(requestedModel)
	if !ok {
		canonicalValue, ok = CatalogResolveRequest(group.Name)
	}
	var canonical *model.CanonicalModel
	strategy := model.RoutingStrategyManual
	if ok {
		canonical = &canonicalValue
		preview.CanonicalModel = canonical.Name
		strategy = canonical.RoutingStrategy.Normalize()
		preview.Strategy = strategy
	}

	var candidates []model.RouteCandidate
	if canonical != nil {
		if err := db.GetDB().WithContext(ctx).Where("canonical_model_id = ?", canonical.ID).Find(&candidates).Error; err != nil {
			return group, preview, canonical, err
		}
	}
	candidateByKey := make(map[string]model.RouteCandidate, len(candidates))
	for _, candidate := range candidates {
		candidateByKey[routeCandidateKey(candidate.ChannelID, candidate.UpstreamModelName)] = candidate
	}

	type scoredItem struct {
		item  model.GroupItem
		score float64
		rank  int
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
		assessment := AssessProtocolRoute(requirements, outboundProtocol, effectivePolicy, allowLossy)
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

		score := routeCandidateScore(ctx, strategy, channel, candidate)
		rank := protocolPreferenceRank(assessment)
		score += float64(rank) * 1e12
		decision.Included = true
		decision.Score = score
		preview.Decisions = append(preview.Decisions, decision)
		included = append(included, scoredItem{item: item, score: score, rank: rank})
	}

	if strategy == model.RoutingStrategyManual {
		sort.SliceStable(included, func(i, j int) bool {
			if included[i].rank == included[j].rank {
				return included[i].item.Priority < included[j].item.Priority
			}
			return included[i].rank > included[j].rank
		})
		filtered := make([]model.GroupItem, 0, len(included))
		for _, item := range included {
			filtered = append(filtered, item.item)
		}
		group.Items = filtered
		return group, preview, canonical, nil
	}
	sort.SliceStable(included, func(i, j int) bool {
		if included[i].score == included[j].score {
			return included[i].item.Priority < included[j].item.Priority
		}
		return included[i].score > included[j].score
	})
	group.Items = make([]model.GroupItem, 0, len(included))
	for index, item := range included {
		item.item.Priority = index
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
		policy = canonical.ProtocolPolicy.Normalize(model.ProtocolPolicyAuto)
		allowLossy = canonical.AllowLossy
	}
	if candidateExists {
		policy = candidate.ProtocolPolicy.Normalize(policy)
		if candidate.AllowLossy != nil {
			allowLossy = *candidate.AllowLossy
		}
	}
	return policy, allowLossy
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

func routeCandidateScore(ctx context.Context, strategy model.RoutingStrategy, channel model.Channel, candidate model.RouteCandidate) float64 {
	stats := StatsChannelGet(channel.ID).StatsMetrics
	total := stats.RequestSuccess + stats.RequestFailed
	reliability := 0.5
	latency := float64(60_000)
	if total > 0 {
		reliability = float64(stats.RequestSuccess) / float64(total)
		latency = float64(stats.WaitTime) / float64(total)
	}
	priceValue := math.Inf(1)
	if candidate.ID > 0 {
		if effective, err := EffectivePriceForCandidate(ctx, candidate.ID, candidate.UpstreamModelName); err == nil && effective.Source != model.PriceQuoteSourceUnknown {
			priceValue = effective.Input + effective.Output
		}
	}
	switch strategy {
	case model.RoutingStrategyReliability:
		return reliability * 10_000
	case model.RoutingStrategyLowestCost:
		if math.IsInf(priceValue, 1) {
			return -1e12
		}
		return -priceValue
	case model.RoutingStrategyLowestLatency:
		return -latency
	default:
		costPenalty := 0.0
		if !math.IsInf(priceValue, 1) {
			costPenalty = math.Log1p(priceValue) * 20
		}
		return reliability*1000 - latency/100 - costPenalty
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
	case outbound.OutboundTypeOpenAIEmbedding:
		return model.ProtocolOpenAIEmbedding
	default:
		return model.ProtocolOpenAIChat
	}
}

func ProtocolTransformSupported(inboundProtocol, outboundProtocol model.ProtocolName) bool {
	if inboundProtocol == outboundProtocol {
		return true
	}
	switch inboundProtocol {
	case model.ProtocolOpenAIChat, model.ProtocolOpenAIResponses, model.ProtocolAnthropic:
		switch outboundProtocol {
		case model.ProtocolOpenAIChat, model.ProtocolOpenAIResponses, model.ProtocolAnthropic, model.ProtocolGemini:
			return true
		}
	case model.ProtocolOpenAIEmbedding:
		return outboundProtocol == model.ProtocolOpenAIEmbedding
	}
	return false
}
