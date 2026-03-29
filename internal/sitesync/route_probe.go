package sitesync

import (
	"context"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

type siteModelRouteDetection struct {
	RouteType       model.SiteModelRouteType
	RouteRawPayload string
	ApplyRouteType  bool
}

func applyDetectedRoutesToSiteModels(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	modelToken model.SiteToken,
	platformUserID int,
	items []model.SiteModel,
) []model.SiteModel {
	if siteRecord == nil || len(items) == 0 {
		return items
	}

	detections := detectSiteModelRoutes(ctx, siteRecord, account, accessToken, modelToken, platformUserID, items)
	if len(detections) == 0 {
		return items
	}

	for i := range items {
		modelName := strings.ToLower(strings.TrimSpace(items[i].ModelName))
		detection, ok := detections[modelName]
		if !ok {
			continue
		}
		if detection.ApplyRouteType {
			items[i].RouteType = detection.RouteType
		}
		items[i].RouteSource = model.SiteModelRouteSourceSyncInferred
		items[i].RouteRawPayload = detection.RouteRawPayload
	}
	return items
}

func detectSiteModelRoutes(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	modelToken model.SiteToken,
	platformUserID int,
	items []model.SiteModel,
) map[string]siteModelRouteDetection {
	modelFilter := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.ModelName))
		if name == "" {
			continue
		}
		modelFilter[name] = struct{}{}
	}
	if len(modelFilter) == 0 {
		return nil
	}

	switch siteRecord.Platform {
	case model.SitePlatformNewAPI, model.SitePlatformOneAPI:
		return detectManagedPricingRoutes(ctx, siteRecord, account, accessToken, modelToken, modelFilter)
	case model.SitePlatformOneHub, model.SitePlatformDoneHub:
		detections := detectManagedPricingRoutes(ctx, siteRecord, account, accessToken, modelToken, modelFilter)
		detections = mergeSiteModelRouteDetections(
			detections,
			detectManagedAvailableModelRoutes(ctx, siteRecord, account, accessToken, modelToken, modelFilter),
		)
		return detections
	case model.SitePlatformAnyRouter:
		return detectAnyRouterPricingRoutes(ctx, siteRecord, account, accessToken, platformUserID, modelFilter)
	default:
		return nil
	}
}

func detectManagedPricingRoutes(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	modelToken model.SiteToken,
	modelFilter map[string]struct{},
) map[string]siteModelRouteDetection {
	return detectManagedRoutesFromPath(
		ctx,
		siteRecord,
		account,
		accessToken,
		modelToken,
		"/api/pricing",
		modelFilter,
		collectPricingRouteDetections,
	)
}

func detectManagedAvailableModelRoutes(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	modelToken model.SiteToken,
	modelFilter map[string]struct{},
) map[string]siteModelRouteDetection {
	return detectManagedRoutesFromPath(
		ctx,
		siteRecord,
		account,
		accessToken,
		modelToken,
		"/api/available_model",
		modelFilter,
		collectAvailableModelRouteDetections,
	)
}

func detectManagedRoutesFromPath(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	modelToken model.SiteToken,
	path string,
	modelFilter map[string]struct{},
	collector func(map[string]any, string, map[string]struct{}) map[string]siteModelRouteDetection,
) map[string]siteModelRouteDetection {
	var detections map[string]siteModelRouteDetection

	tryCollect := func(payload map[string]any, source string) {
		detections = mergeSiteModelRouteDetections(detections, collector(payload, source, modelFilter))
	}

	if strings.TrimSpace(accessToken) != "" {
		if payload, err := requestJSONWithManagedAccessToken(
			ctx,
			siteRecord,
			"GET",
			buildSiteURL(siteRecord.BaseURL, path),
			nil,
			accessToken,
			account,
		); err == nil {
			tryCollect(payload, path)
		}
	}

	tokenValue := strings.TrimSpace(modelToken.Token)
	if tokenValue != "" {
		if payload, err := requestJSON(
			ctx,
			siteRecord,
			"GET",
			buildSiteURL(siteRecord.BaseURL, path),
			nil,
			map[string]string{"Authorization": ensureBearer(tokenValue)},
			account,
		); err == nil {
			tryCollect(payload, path)
		}
	}

	if payload, err := requestJSON(
		ctx,
		siteRecord,
		"GET",
		buildSiteURL(siteRecord.BaseURL, path),
		nil,
		nil,
		account,
	); err == nil {
		tryCollect(payload, path)
	}

	if len(detections) == 0 {
		return nil
	}
	return detections
}

func detectAnyRouterPricingRoutes(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	accessToken string,
	platformUserID int,
	modelFilter map[string]struct{},
) map[string]siteModelRouteDetection {
	token := strings.TrimSpace(accessToken)
	if token == "" || siteRecord == nil || account == nil {
		return nil
	}

	userID := platformUserID
	if userID <= 0 {
		if discovered, err := anyRouterDiscoverUserID(ctx, siteRecord, account, token); err == nil {
			userID = discovered
		}
	}

	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/pricing")
	var detections map[string]siteModelRouteDetection

	if payload, _, err := anyRouterRequestJSONWithCookies(
		ctx,
		siteRecord,
		"GET",
		requestURL,
		nil,
		anyRouterAuthHeaders(token, userID),
		account,
	); err == nil {
		detections = mergeSiteModelRouteDetections(detections, collectPricingRouteDetections(payload, "/api/pricing", modelFilter))
	}

	for _, cookie := range anyRouterBuildCookieCandidates(token) {
		headers := map[string]string{"Cookie": cookie}
		anyRouterAddUserIDHeaders(headers, userID)
		payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, "GET", requestURL, nil, headers, account)
		if err != nil {
			continue
		}
		detections = mergeSiteModelRouteDetections(detections, collectPricingRouteDetections(payload, "/api/pricing", modelFilter))
	}

	if len(detections) == 0 {
		return nil
	}
	return detections
}

func collectPricingRouteDetections(
	payload map[string]any,
	source string,
	modelFilter map[string]struct{},
) map[string]siteModelRouteDetection {
	items := normalizeItemSlice(payload["data"])
	if len(items) == 0 {
		return nil
	}

	result := make(map[string]siteModelRouteDetection)
	for _, item := range items {
		modelName := firstNonEmptyString(
			jsonString(item["model_name"]),
			jsonString(item["model"]),
			jsonString(item["name"]),
			jsonString(item["id"]),
		)
		detection, ok := buildSiteModelRouteDetection(
			modelName,
			normalizeStringList(item["enable_groups"]),
			normalizeStringList(item["supported_endpoint_types"]),
			source,
			modelFilter,
		)
		if !ok {
			continue
		}
		result[strings.ToLower(strings.TrimSpace(modelName))] = detection
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func collectAvailableModelRouteDetections(
	payload map[string]any,
	source string,
	modelFilter map[string]struct{},
) map[string]siteModelRouteDetection {
	dataMap, ok := nestedValue(payload, "data").(map[string]any)
	if !ok || len(dataMap) == 0 {
		return nil
	}

	result := make(map[string]siteModelRouteDetection)
	for modelName, raw := range dataMap {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		detection, ok := buildSiteModelRouteDetection(
			modelName,
			normalizeStringList(item["enable_groups"]),
			normalizeStringList(item["supported_endpoint_types"]),
			source,
			modelFilter,
		)
		if !ok {
			continue
		}
		result[strings.ToLower(strings.TrimSpace(modelName))] = detection
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func buildSiteModelRouteDetection(
	modelName string,
	enableGroups []string,
	supportedEndpointTypes []string,
	source string,
	modelFilter map[string]struct{},
) (siteModelRouteDetection, bool) {
	normalizedName := strings.ToLower(strings.TrimSpace(modelName))
	if normalizedName == "" {
		return siteModelRouteDetection{}, false
	}
	if len(modelFilter) > 0 {
		if _, ok := modelFilter[normalizedName]; !ok {
			return siteModelRouteDetection{}, false
		}
	}

	supportedEndpointTypes = dedupeStringsPreserveOrder(supportedEndpointTypes)
	enableGroups = model.NormalizeSiteModelRouteMetadataGroupKeys(enableGroups)
	heuristicEndpointTypes := inferHeuristicEndpointTypes(modelName, supportedEndpointTypes)
	if len(supportedEndpointTypes) == 0 && len(enableGroups) == 0 && len(heuristicEndpointTypes) == 0 {
		return siteModelRouteDetection{}, false
	}

	knownRouteTypes := normalizeSupportedRouteTypes(append(append([]string{}, supportedEndpointTypes...), heuristicEndpointTypes...))
	metadata := model.SiteModelRouteMetadata{
		Source:                  strings.TrimSpace(source),
		RouteSupported:          len(knownRouteTypes) > 0,
		EnableGroups:            enableGroups,
		SupportedEndpointTypes:  supportedEndpointTypes,
		HeuristicEndpointTypes:  heuristicEndpointTypes,
		NormalizedEndpointTypes: routeTypesToStrings(knownRouteTypes),
	}
	if metadata.RouteSupported {
		metadata.RouteType = pickPreferredDetectedRouteType(modelName, knownRouteTypes)
	} else if len(supportedEndpointTypes) > 0 {
		metadata.RouteType = model.SiteModelRouteTypeUnknown
		metadata.UnsupportedReason = "site reports endpoint types outside current supported route buckets"
	}

	return siteModelRouteDetection{
		RouteType:       metadata.RouteType,
		RouteRawPayload: metadata.Marshal(),
		ApplyRouteType:  len(supportedEndpointTypes) > 0 || len(heuristicEndpointTypes) > 0,
	}, true
}

func inferHeuristicEndpointTypes(modelName string, supportedEndpointTypes []string) []string {
	if !shouldHeuristicallyAddOpenAIResponse(modelName) {
		return nil
	}
	if explicitSupportsResponse(supportedEndpointTypes) {
		return nil
	}
	return []string{"/v1/responses"}
}

func shouldHeuristicallyAddOpenAIResponse(modelName string) bool {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	return strings.HasPrefix(lower, "gpt-5")
}

func explicitSupportsResponse(supportedEndpointTypes []string) bool {
	for _, endpointType := range supportedEndpointTypes {
		if routeType, ok := mapSupportedEndpointType(endpointType); ok && routeType == model.SiteModelRouteTypeOpenAIResponse {
			return true
		}
	}
	return false
}

func normalizeSupportedRouteTypes(values []string) []model.SiteModelRouteType {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[model.SiteModelRouteType]struct{})
	result := make([]model.SiteModelRouteType, 0, len(values))
	for _, value := range values {
		routeType, ok := mapSupportedEndpointType(value)
		if !ok {
			continue
		}
		if _, exists := seen[routeType]; exists {
			continue
		}
		seen[routeType] = struct{}{}
		result = append(result, routeType)
	}
	sort.Slice(result, func(i, j int) bool {
		return detectedRouteTypePriority(result[i]) < detectedRouteTypePriority(result[j])
	})
	return result
}

func routeTypesToStrings(values []model.SiteModelRouteType) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func pickPreferredDetectedRouteType(modelName string, values []model.SiteModelRouteType) model.SiteModelRouteType {
	if len(values) == 0 {
		return model.SiteModelRouteTypeUnknown
	}

	nativeRouteType := model.InferSiteModelRouteType(modelName)
	switch nativeRouteType {
	case model.SiteModelRouteTypeAnthropic,
		model.SiteModelRouteTypeGemini,
		model.SiteModelRouteTypeVolcengine,
		model.SiteModelRouteTypeOpenAIEmbedding:
		for _, value := range values {
			if value == nativeRouteType {
				return value
			}
		}
	}

	fallbackOrder := []model.SiteModelRouteType{
		model.SiteModelRouteTypeAnthropic,
		model.SiteModelRouteTypeOpenAIResponse,
		model.SiteModelRouteTypeOpenAIChat,
		model.SiteModelRouteTypeGemini,
		model.SiteModelRouteTypeVolcengine,
		model.SiteModelRouteTypeOpenAIEmbedding,
	}
	for _, preferred := range fallbackOrder {
		for _, value := range values {
			if value == preferred {
				return value
			}
		}
	}

	return values[0]
}

func detectedRouteTypePriority(routeType model.SiteModelRouteType) int {
	switch routeType {
	case model.SiteModelRouteTypeOpenAIEmbedding:
		return 0
	case model.SiteModelRouteTypeOpenAIResponse:
		return 1
	case model.SiteModelRouteTypeAnthropic:
		return 2
	case model.SiteModelRouteTypeGemini:
		return 3
	case model.SiteModelRouteTypeVolcengine:
		return 4
	case model.SiteModelRouteTypeOpenAIChat:
		return 5
	default:
		return 99
	}
}

func mapSupportedEndpointType(value string) (model.SiteModelRouteType, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case normalized == "",
		normalized == "none":
		return "", false
	case normalized == "embedding",
		normalized == "embeddings",
		normalized == "openai_embedding",
		normalized == "openai/embeddings",
		strings.Contains(normalized, "/v1/embeddings"):
		return model.SiteModelRouteTypeOpenAIEmbedding, true
	case normalized == "responses",
		normalized == "response",
		normalized == "openai/responses",
		strings.Contains(normalized, "/v1/responses"):
		return model.SiteModelRouteTypeOpenAIResponse, true
	case normalized == "messages",
		normalized == "anthropic",
		normalized == "anthropic/messages",
		strings.Contains(normalized, "/v1/messages"):
		return model.SiteModelRouteTypeAnthropic, true
	case normalized == "gemini",
		normalized == "generatecontent",
		normalized == "streamgeneratecontent",
		normalized == "counttokens",
		strings.Contains(normalized, ":generatecontent"),
		strings.Contains(normalized, ":streamgeneratecontent"),
		strings.Contains(normalized, ":counttokens"):
		return model.SiteModelRouteTypeGemini, true
	case normalized == "volcengine",
		normalized == "ark",
		strings.Contains(normalized, "volcengine"):
		return model.SiteModelRouteTypeVolcengine, true
	case normalized == "chat",
		normalized == "chat_completions",
		normalized == "chat/completions",
		normalized == "completions",
		normalized == "openai",
		normalized == "openai/chat_completions",
		strings.Contains(normalized, "/v1/chat/completions"):
		return model.SiteModelRouteTypeOpenAIChat, true
	default:
		return "", false
	}
}

func normalizeStringList(value any) []string {
	switch typed := value.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(jsonString(item))
			if text == "" {
				if str, ok := item.(string); ok {
					text = strings.TrimSpace(str)
				}
			}
			if text != "" {
				result = append(result, text)
			}
		}
		return dedupeStringsPreserveOrder(result)
	case []string:
		return dedupeStringsPreserveOrder(typed)
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parts := strings.Split(typed, ",")
		result := make([]string, 0, len(parts))
		for _, item := range parts {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return dedupeStringsPreserveOrder(result)
	default:
		return nil
	}
}

func dedupeStringsPreserveOrder(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func mergeSiteModelRouteDetections(
	dst map[string]siteModelRouteDetection,
	src map[string]siteModelRouteDetection,
) map[string]siteModelRouteDetection {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]siteModelRouteDetection, len(src))
	}
	for key, value := range src {
		existing, ok := dst[key]
		if !ok || shouldReplaceSiteModelRouteDetection(existing, value) {
			dst[key] = value
		}
	}
	return dst
}

func shouldReplaceSiteModelRouteDetection(
	existing siteModelRouteDetection,
	next siteModelRouteDetection,
) bool {
	existingMetadata, existingOK := model.ParseSiteModelRouteMetadata(existing.RouteRawPayload)
	nextMetadata, nextOK := model.ParseSiteModelRouteMetadata(next.RouteRawPayload)
	if !existingOK {
		return nextOK
	}
	if !nextOK {
		return false
	}
	if nextMetadata.RouteSupported && !existingMetadata.RouteSupported {
		return true
	}
	return false
}
