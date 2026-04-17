package sitesync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

// NewAPI 系列以 model_ratio=1 约等 2 USD / 1M tokens 作为基准。
const sitePricingNewAPIBaseUSDPerMillion = 2.0

// defaultGroupRatio 确保返回的 groupRatio 至少包含 default。
const defaultGroupRatio = 1.0

// pricingCatalog 是 normalizer 统一输出：模型价格 + 分组倍率，
// 对齐 metapi PricingData 的语义。
type pricingCatalog struct {
	models     []pricingModel
	groupRatio map[string]float64
}

type pricingModel struct {
	modelName        string
	quotaType        int
	modelRatio       float64
	completionRatio  float64
	cacheRatio       float64
	cacheCreateRatio float64
	// flatInput/flatOutput 来自 model_price，仅 quotaType 为 1 或 OneHub 直出价场景使用。
	flatInput  float64
	flatOutput float64
	// hasFlatPrice 为 true 表示 modelPrice 字段存在且可直接作为绝对价使用（OneHub/DoneHub）。
	hasFlatPrice bool
	enableGroups []string
}

// fetchPricing 按平台分发抓取价格，返回可直接持久化的 SitePrice 行。
// groups 用于决定要展开到哪些分组；失败时返回空切片与 error，调用方按 best-effort 处理。
func fetchPricing(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	groups []model.SiteUserGroup,
) ([]model.SitePrice, error) {
	if siteRecord == nil || account == nil {
		return nil, fmt.Errorf("site or account is nil")
	}
	var (
		catalog *pricingCatalog
		err     error
	)
	switch siteRecord.Platform {
	case model.SitePlatformOneHub, model.SitePlatformDoneHub:
		catalog, err = fetchOneHubPricing(ctx, siteRecord, account, accessToken)
	case model.SitePlatformNewAPI, model.SitePlatformOneAPI, model.SitePlatformAnyRouter:
		catalog, err = fetchCommonPricing(ctx, siteRecord, account, accessToken)
	default:
		return nil, fmt.Errorf("pricing sync not supported for platform %s", siteRecord.Platform)
	}
	if err != nil {
		return nil, err
	}
	if catalog == nil || len(catalog.models) == 0 {
		return nil, nil
	}
	return buildSitePrices(account.ID, catalog, groups), nil
}

func fetchCommonPricing(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
) (*pricingCatalog, error) {
	// 已知的 pricing 端点对部分平台需要带上 Bearer token，对其它平台也允许匿名访问。
	headers := map[string]string{}
	if trimmed := strings.TrimSpace(accessToken); trimmed != "" {
		headers["Authorization"] = ensureBearer(trimmed)
	}
	payload, err := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/pricing"), nil, headers, account)
	if err != nil {
		return nil, err
	}
	return normalizeCommonPricingPayload(payload), nil
}

func fetchOneHubPricing(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
) (*pricingCatalog, error) {
	headers := map[string]string{}
	if trimmed := strings.TrimSpace(accessToken); trimmed != "" {
		headers["Authorization"] = ensureBearer(trimmed)
	}
	available, err := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/available_model"), nil, headers, account)
	if err != nil {
		return nil, err
	}
	groupMap, err := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/user_group_map"), nil, headers, account)
	if err != nil {
		// 分组映射可选；缺失时按默认倍率处理
		groupMap = nil
	}
	return normalizeOneHubPricingPayload(available, groupMap), nil
}

// normalizeCommonPricingPayload 解析 new-api 形态的 /api/pricing 响应。
// 载荷通常为 {"data": [...models], "group_ratio": {...}} 或直接数组。
func normalizeCommonPricingPayload(payload map[string]any) *pricingCatalog {
	rawModels := unwrapPricingArray(payload)
	if len(rawModels) == 0 {
		return nil
	}
	models := make([]pricingModel, 0, len(rawModels))
	for _, raw := range rawModels {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		modelName := strings.TrimSpace(jsonString(item["model_name"]))
		if modelName == "" {
			continue
		}
		pm := pricingModel{
			modelName:        modelName,
			quotaType:        int(jsonFloat(item["quota_type"])),
			modelRatio:       nonZeroFloat(jsonFloat(item["model_ratio"]), 1),
			completionRatio:  nonZeroFloat(jsonFloat(item["completion_ratio"]), 1),
			cacheRatio:       nonNegativeFloat(firstNonZeroAny(item["cache_ratio"], item["cacheRatio"]), 0),
			cacheCreateRatio: nonNegativeFloat(firstNonZeroAny(item["cache_creation_ratio"], item["cacheCreationRatio"], item["create_cache_ratio"], item["createCacheRatio"]), 0),
			enableGroups:     stringSliceFromAny(item["enable_groups"]),
		}
		if price := item["model_price"]; price != nil {
			input, output, flat := normalizePricingModelPrice(price)
			pm.flatInput = input
			pm.flatOutput = output
			pm.hasFlatPrice = flat
		}
		models = append(models, pm)
	}
	if len(models) == 0 {
		return nil
	}
	groupRatio := normalizeGroupRatioAny(payload["group_ratio"])
	return &pricingCatalog{models: models, groupRatio: groupRatio}
}

// normalizeOneHubPricingPayload 解析 one-hub/done-hub 的 /api/available_model + /api/user_group_map 组合。
// available 载荷为 { "data": { modelName: { price: { input, output, type, ... }, groups: [...] } } }，价格即绝对值。
func normalizeOneHubPricingPayload(availablePayload map[string]any, groupPayload map[string]any) *pricingCatalog {
	dataMap := unwrapPricingObject(availablePayload)
	if len(dataMap) == 0 {
		return nil
	}
	models := make([]pricingModel, 0, len(dataMap))
	for modelName, raw := range dataMap {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(modelName)
		if name == "" {
			continue
		}
		priceMap, _ := item["price"].(map[string]any)
		var (
			input  = jsonFloat(priceMap["input"])
			output = jsonFloat(priceMap["output"])
		)
		if output == 0 {
			output = input
		}
		cacheRead := firstNonZeroAny(priceMap["input_cache_read"], priceMap["inputCacheRead"], priceMap["cache_read"], priceMap["cacheRead"])
		cacheWrite := firstNonZeroAny(priceMap["input_cache_write"], priceMap["inputCacheWrite"], priceMap["cache_write"], priceMap["cacheWrite"])
		isTokenType := strings.EqualFold(strings.TrimSpace(jsonString(priceMap["type"])), "tokens")
		quotaType := 1
		if isTokenType {
			quotaType = 0
		}

		var cacheRatio float64
		if input > 0 && cacheRead > 0 {
			cacheRatio = cacheRead / input
		}
		var cacheCreateRatio float64
		if input > 0 && cacheWrite > 0 {
			cacheCreateRatio = cacheWrite / input
		}
		var completionRatio float64 = 1
		if input > 0 {
			completionRatio = output / input
		}

		pm := pricingModel{
			modelName:        name,
			quotaType:        quotaType,
			modelRatio:       1,
			completionRatio:  completionRatio,
			cacheRatio:       cacheRatio,
			cacheCreateRatio: cacheCreateRatio,
			flatInput:        input,
			flatOutput:       output,
			hasFlatPrice:     true,
			enableGroups:     stringSliceFromAny(item["groups"]),
		}
		models = append(models, pm)
	}
	if len(models) == 0 {
		return nil
	}

	// one-hub group_map: { groupKey: { ratio, ... } }
	groupMap := unwrapPricingObject(groupPayload)
	groupRatio := make(map[string]float64)
	for key, value := range groupMap {
		if entry, ok := value.(map[string]any); ok {
			groupRatio[key] = nonZeroFloat(jsonFloat(entry["ratio"]), 1)
		}
	}
	groupRatio = finalizeGroupRatio(groupRatio)
	return &pricingCatalog{models: models, groupRatio: groupRatio}
}

// buildSitePrices 把 catalog 展开为 (account, group, model) 维度的绝对价格行。
// 仅对模型 enableGroups 中声明的分组生成价格；若模型未声明则对全部分组展开。
// 始终保证 default 分组至少有一份缓存。
func buildSitePrices(accountID int, catalog *pricingCatalog, siteGroups []model.SiteUserGroup) []model.SitePrice {
	if catalog == nil || len(catalog.models) == 0 {
		return nil
	}
	allGroupKeys := collectGroupKeys(catalog.groupRatio, siteGroups)
	if len(allGroupKeys) == 0 {
		allGroupKeys = []string{model.SiteDefaultGroupKey}
	}
	now := time.Now()
	result := make([]model.SitePrice, 0, len(catalog.models)*len(allGroupKeys))
	for _, pm := range catalog.models {
		eligible := pm.enableGroups
		if len(eligible) == 0 {
			eligible = allGroupKeys
		}
		for _, rawKey := range eligible {
			groupKey := model.NormalizeSiteGroupKey(rawKey)
			ratio := lookupGroupRatio(catalog.groupRatio, groupKey)
			inputPerMillion, outputPerMillion, cacheReadPerMillion, cacheWritePerMillion, flatPrice := computeAbsolutePricing(pm, ratio)
			result = append(result, model.SitePrice{
				SiteAccountID:   accountID,
				GroupKey:        groupKey,
				ModelName:       pm.modelName,
				QuotaType:       pm.quotaType,
				InputPrice:      inputPerMillion,
				OutputPrice:     outputPerMillion,
				CacheReadPrice:  cacheReadPerMillion,
				CacheWritePrice: cacheWritePerMillion,
				FlatPrice:       flatPrice,
				ModelRatio:      pm.modelRatio,
				CompletionRatio: pm.completionRatio,
				GroupRatio:      ratio,
				UpdatedAt:       now,
			})
		}
	}
	return result
}

// computeAbsolutePricing 统一把 ratio 形态与 flat 形态折算为 USD/百万 tokens。
func computeAbsolutePricing(pm pricingModel, groupRatio float64) (input, output, cacheRead, cacheWrite, flat float64) {
	if pm.quotaType == 1 {
		// 按次计费：记录单价，细则由调用方决定；token 单价填 0。
		return 0, 0, 0, 0, pm.flatInput * groupRatio
	}
	if pm.hasFlatPrice {
		// OneHub 体系的 model_price 已经是 USD/百万 tokens 的绝对值；再乘 groupRatio。
		input = pm.flatInput * groupRatio
		output = pm.flatOutput * groupRatio
	} else {
		input = pm.modelRatio * groupRatio * sitePricingNewAPIBaseUSDPerMillion
		output = input * pm.completionRatio
	}
	if pm.cacheRatio > 0 {
		cacheRead = input * pm.cacheRatio
	}
	if pm.cacheCreateRatio > 0 {
		cacheWrite = input * pm.cacheCreateRatio
	}
	return input, output, cacheRead, cacheWrite, 0
}

// collectGroupKeys 合并 pricing 侧 group_ratio、站点已知分组，至少返回 default。
func collectGroupKeys(groupRatio map[string]float64, siteGroups []model.SiteUserGroup) []string {
	seen := make(map[string]struct{})
	keys := make([]string, 0)
	for key := range groupRatio {
		normalized := model.NormalizeSiteGroupKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		keys = append(keys, normalized)
	}
	for _, group := range siteGroups {
		normalized := model.NormalizeSiteGroupKey(group.GroupKey)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		keys = append(keys, normalized)
	}
	if _, ok := seen[model.SiteDefaultGroupKey]; !ok {
		keys = append(keys, model.SiteDefaultGroupKey)
	}
	return keys
}

func lookupGroupRatio(groupRatio map[string]float64, groupKey string) float64 {
	if ratio, ok := groupRatio[groupKey]; ok && ratio > 0 {
		return ratio
	}
	if ratio, ok := groupRatio[model.SiteDefaultGroupKey]; ok && ratio > 0 {
		return ratio
	}
	return defaultGroupRatio
}

func finalizeGroupRatio(raw map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(raw)+1)
	for key, value := range raw {
		if value <= 0 {
			continue
		}
		result[model.NormalizeSiteGroupKey(key)] = value
	}
	if _, ok := result[model.SiteDefaultGroupKey]; !ok {
		result[model.SiteDefaultGroupKey] = defaultGroupRatio
	}
	return result
}

func normalizeGroupRatioAny(raw any) map[string]float64 {
	typed, ok := raw.(map[string]any)
	if !ok {
		return finalizeGroupRatio(nil)
	}
	flattened := make(map[string]float64, len(typed))
	for key, value := range typed {
		flattened[key] = jsonFloat(value)
	}
	return finalizeGroupRatio(flattened)
}

// normalizePricingModelPrice 支持 number、{input,output} 以及空值三种形态。
// 第三返回值表示是否拿到了可直接作为绝对价的结构（例如 OneHub 的 price 对象）。
func normalizePricingModelPrice(raw any) (input float64, output float64, flat bool) {
	switch typed := raw.(type) {
	case float64:
		return typed, typed, true
	case map[string]any:
		inVal := jsonFloat(typed["input"])
		outVal := jsonFloat(typed["output"])
		if inVal == 0 && outVal == 0 {
			return 0, 0, false
		}
		if outVal == 0 {
			outVal = inVal
		}
		return inVal, outVal, true
	}
	return 0, 0, false
}

func unwrapPricingArray(payload map[string]any) []any {
	if payload == nil {
		return nil
	}
	if arr, ok := payload["data"].([]any); ok {
		return arr
	}
	if arr, ok := payload["models"].([]any); ok {
		return arr
	}
	// 部分实现直接返回顶层数组 → 解析器已把它包成 map；再尝试 nested data.items。
	if nested := nestedValue(payload, "data", "items"); nested != nil {
		if arr, ok := nested.([]any); ok {
			return arr
		}
	}
	return nil
}

func unwrapPricingObject(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	if inner, ok := payload["data"].(map[string]any); ok {
		return inner
	}
	return payload
}

func stringSliceFromAny(raw any) []string {
	switch typed := raw.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := strings.TrimSpace(jsonString(item)); s != "" {
				result = append(result, s)
			}
		}
		return result
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := strings.TrimSpace(item); s != "" {
				result = append(result, s)
			}
		}
		return result
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		parts := strings.Split(trimmed, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			if s := strings.TrimSpace(part); s != "" {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

func firstNonZeroAny(values ...any) float64 {
	for _, value := range values {
		if value == nil {
			continue
		}
		if f := jsonFloat(value); f != 0 {
			return f
		}
	}
	return 0
}

func nonZeroFloat(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func nonNegativeFloat(value, fallback float64) float64 {
	if value >= 0 && value != 0 {
		return value
	}
	return fallback
}
