package op

import (
	"context"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
)

// discoveryAccumulator 按归一化模型名聚合同一个上游模型在多个渠道上的出现。
type discoveryAccumulator struct {
	item          model.DiscoveredModel
	channelIDs    map[int]struct{}
	siteNames     map[string]struct{}
	endpointTypes map[string]struct{}
}

// CatalogDiscoveredModels 汇总所有渠道上报的上游模型及其分组归属，供「模型发现」界面挑选。
// 同一个模型名在多个渠道出现时合并为一行，渠道数量与站点来源用于判断该模型是否值得单独建组。
func CatalogDiscoveredModels(ctx context.Context) ([]model.DiscoveredModel, error) {
	channels := channelCache.GetAll()
	if len(channels) == 0 {
		return []model.DiscoveredModel{}, nil
	}

	channelIDs := make([]int, 0, len(channels))
	for id := range channels {
		channelIDs = append(channelIDs, id)
	}
	bindingMap, err := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	if err != nil {
		return nil, err
	}
	siteNameByID, err := siteNameIndex(ctx)
	if err != nil {
		return nil, err
	}

	groupByNormalized := make(map[string]model.Group, groupCache.Len())
	for _, group := range groupCache.GetAll() {
		groupByNormalized[NormalizeModelIdentity(group.Name)] = group
	}

	accumulators := make(map[string]*discoveryAccumulator)
	for _, channel := range channels {
		endpointType := channelEndpointType(channel.Type)
		siteName := ""
		if binding, ok := bindingMap[channel.ID]; ok {
			siteName = siteNameByID[binding.SiteID]
		}
		for _, name := range splitChannelModelNames(channel.Model, channel.CustomModel) {
			normalized := NormalizeModelIdentity(name)
			if normalized == "" {
				continue
			}
			accumulator, ok := accumulators[normalized]
			if !ok {
				accumulator = &discoveryAccumulator{
					item:          newDiscoveredModel(name, normalized, groupByNormalized),
					channelIDs:    make(map[int]struct{}),
					siteNames:     make(map[string]struct{}),
					endpointTypes: make(map[string]struct{}),
				}
				accumulators[normalized] = accumulator
			}
			accumulator.channelIDs[channel.ID] = struct{}{}
			if siteName != "" {
				accumulator.siteNames[siteName] = struct{}{}
			}
			if endpointType != "" {
				accumulator.endpointTypes[endpointType] = struct{}{}
			}
		}
	}

	items := make([]model.DiscoveredModel, 0, len(accumulators))
	for _, accumulator := range accumulators {
		item := accumulator.item
		item.ChannelIDs = sortedInts(accumulator.channelIDs)
		item.ChannelCount = len(item.ChannelIDs)
		item.SiteNames = sortedStrings(accumulator.siteNames)
		item.EndpointTypes = sortedStrings(accumulator.endpointTypes)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Vendor != items[j].Vendor {
			// 未知厂商排在最后，方便优先处理能确定归属的模型。
			if items[i].Vendor == "" || items[j].Vendor == "" {
				return items[j].Vendor == ""
			}
			return items[i].Vendor < items[j].Vendor
		}
		return items[i].NormalizedName < items[j].NormalizedName
	})
	return items, nil
}

// newDiscoveredModel 解析单个上游模型名的目录归属：自身建组、映射到别的分组、或尚未纳入。
func newDiscoveredModel(
	name string,
	normalized string,
	groupByNormalized map[string]model.Group,
) model.DiscoveredModel {
	item := model.DiscoveredModel{
		Name:           strings.TrimSpace(name),
		NormalizedName: normalized,
		Status:         model.DiscoveredModelUngrouped,
		Vendor:         modelvendor.Detect(name),
	}

	canonical, resolved := CatalogResolveIdentity(name)
	if !resolved {
		return item
	}

	item.CanonicalModelID = canonical.ID
	item.CanonicalName = canonical.Name
	if canonical.Vendor != "" {
		item.Vendor = canonical.Vendor
		item.VendorManual = canonical.VendorManual
	}
	group, hasGroup := groupByNormalized[NormalizeModelIdentity(canonical.Name)]
	if hasGroup {
		item.GroupID = group.ID
		item.GroupName = group.Name
	}

	switch {
	case canonical.NormalizedName != normalized:
		// 通过别名落到了别的 Canonical Model 上，即用户做过重映射。
		item.Status = model.DiscoveredModelMapped
	case hasGroup:
		item.Status = model.DiscoveredModelGrouped
	}
	return item
}

// siteNameIndex 只取站点 id/name，避免 SiteList 的全量 Preload 开销。
func siteNameIndex(ctx context.Context) (map[int]string, error) {
	var sites []model.Site
	if err := db.GetDB().WithContext(ctx).Select("id", "name").Find(&sites).Error; err != nil {
		return nil, err
	}
	index := make(map[int]string, len(sites))
	for _, site := range sites {
		index[site.ID] = site.Name
	}
	return index, nil
}

func sortedInts(values map[int]struct{}) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func sortedStrings(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
