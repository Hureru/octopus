package op

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type rawImportObject map[string]any

type importedSiteInput struct {
	Name     string
	Platform model.SitePlatform
	BaseURL  string
}

type importedAccountInput struct {
	Site           importedSiteInput
	Name           string
	CredentialType model.SiteCredentialType
	Username       string
	Password       string
	AccessToken    string
	APIKey         string
	RefreshToken   string
	TokenExpiresAt int64
	PlatformUserID *int
	AccountProxy   *string
	Enabled        bool
	AutoSync       bool
	AutoCheckin    bool
	Balance        float64
	BalanceUsed    float64
}

type metAPIImportAccountData struct {
	Input          importedAccountInput
	OriginalID     int
	Tokens         []model.SiteToken
	Groups         []model.SiteUserGroup
	Models         []model.SiteModel
	DisabledModels []model.SiteModel
}

var supportedImportPlatforms = map[string]model.SitePlatform{
	"new-api":   model.SitePlatformNewAPI,
	"newapi":    model.SitePlatformNewAPI,
	"one-api":   model.SitePlatformOneAPI,
	"oneapi":    model.SitePlatformOneAPI,
	"anyrouter": model.SitePlatformAnyRouter,
	"one-hub":   model.SitePlatformOneHub,
	"onehub":    model.SitePlatformOneHub,
	"done-hub":  model.SitePlatformDoneHub,
	"donehub":   model.SitePlatformDoneHub,
	"sub2api":   model.SitePlatformSub2API,
	"openai":    model.SitePlatformOpenAI,
	"anthropic": model.SitePlatformClaude,
	"claude":    model.SitePlatformClaude,
	"google":    model.SitePlatformGemini,
	"gemini":    model.SitePlatformGemini,
}

var unsupportedImportHints = []string{
	"codex",
	"gemini-cli",
	"cliproxyapi",
	"veloera",
}

var directImportPlatforms = map[model.SitePlatform]struct{}{
	model.SitePlatformOpenAI: {},
	model.SitePlatformClaude: {},
	model.SitePlatformGemini: {},
}

func SiteImportAllAPIHub(ctx context.Context, body []byte) (*model.AllAPIHubImportResult, []int, error) {
	var payload rawImportObject
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, fmt.Errorf("site import invalid json")
	}
	if len(payload) == 0 {
		return nil, nil, fmt.Errorf("site import empty payload")
	}

	inputs, warnings, skipped, err := extractAllAPIHubAccounts(payload)
	if err != nil {
		return nil, nil, err
	}
	if len(inputs) == 0 {
		return nil, nil, fmt.Errorf("site import no importable all api hub site account data")
	}

	result := &model.AllAPIHubImportResult{
		SkippedAccounts: skipped,
		Warnings:        warnings,
	}
	createdSiteIDs := make(map[int]struct{})
	reusedSiteIDs := make(map[int]struct{})
	syncAccountIDs := make(map[int]struct{})

	if err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, input := range inputs {
			siteRecord, created, err := upsertImportedSite(tx, input.Site)
			if err != nil {
				return err
			}
			if created {
				createdSiteIDs[siteRecord.ID] = struct{}{}
			} else if _, ok := createdSiteIDs[siteRecord.ID]; !ok {
				reusedSiteIDs[siteRecord.ID] = struct{}{}
			}

			accountRecord, createdAccount, updatedAccount, err := upsertImportedAccount(tx, siteRecord, input)
			if err != nil {
				return err
			}
			if createdAccount {
				result.CreatedAccounts++
			}
			if updatedAccount {
				result.UpdatedAccounts++
			}
			if siteRecord.Enabled && accountRecord.Enabled && accountRecord.AutoSync {
				syncAccountIDs[accountRecord.ID] = struct{}{}
			}
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}

	result.CreatedSites = len(createdSiteIDs)
	result.ReusedSites = len(reusedSiteIDs)

	accountIDs := make([]int, 0, len(syncAccountIDs))
	for accountID := range syncAccountIDs {
		accountIDs = append(accountIDs, accountID)
	}
	slices.Sort(accountIDs)
	result.ScheduledSyncAccounts = len(accountIDs)

	return result, accountIDs, nil
}

func SiteImportMetAPI(ctx context.Context, body []byte) (*model.MetAPIImportResult, error) {
	var payload rawImportObject
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("site import invalid json")
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("site import empty payload")
	}

	inputs, warnings, skipped, err := extractMetAPIAccounts(payload)
	if err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("site import no importable metapi site account data")
	}

	result := &model.MetAPIImportResult{
		SkippedAccounts: skipped,
		Warnings:        warnings,
	}
	createdSiteIDs := make(map[int]struct{})
	reusedSiteIDs := make(map[int]struct{})

	if err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, input := range inputs {
			siteRecord, created, err := upsertImportedSite(tx, input.Input.Site)
			if err != nil {
				return err
			}
			if created {
				createdSiteIDs[siteRecord.ID] = struct{}{}
			} else if _, ok := createdSiteIDs[siteRecord.ID]; !ok {
				reusedSiteIDs[siteRecord.ID] = struct{}{}
			}

			accountRecord, createdAccount, updatedAccount, err := upsertImportedAccount(tx, siteRecord, input.Input)
			if err != nil {
				return err
			}
			if createdAccount {
				result.CreatedAccounts++
			}
			if updatedAccount {
				result.UpdatedAccounts++
			}

			tokens, groups, models, disabledModels, err := replaceMetAPIAccountData(tx, accountRecord.ID, input)
			if err != nil {
				return err
			}
			result.ImportedTokens += tokens
			result.ImportedGroups += groups
			result.ImportedModels += models
			result.DisabledModels += disabledModels
		}
		return nil
	}); err != nil {
		return nil, err
	}

	result.CreatedSites = len(createdSiteIDs)
	result.ReusedSites = len(reusedSiteIDs)
	return result, nil
}

func extractAllAPIHubAccounts(payload rawImportObject) ([]importedAccountInput, []string, int, error) {
	var warnings []string
	inputs := make([]importedAccountInput, 0)
	skipped := 0

	accountsContainer := asObject(payload["accounts"])
	rows := asObjectSlice(accountsContainer["accounts"])
	for _, row := range rows {
		input, warning, ok := parseAllAPIHubAccountRow(row)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if !ok {
			skipped++
			continue
		}
		inputs = append(inputs, input)
	}

	profilesContainer := asObject(payload["apiCredentialProfiles"])
	profiles := asObjectSlice(profilesContainer["profiles"])
	for _, profile := range profiles {
		input, warning, ok := parseAllAPIHubProfile(profile)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if !ok {
			skipped++
			continue
		}
		inputs = append(inputs, input)
	}

	if len(rows) == 0 && len(profiles) == 0 {
		return nil, warnings, skipped, fmt.Errorf("site import no recognizable all api hub payload sections")
	}

	return inputs, warnings, skipped, nil
}

func extractMetAPIAccounts(payload rawImportObject) ([]metAPIImportAccountData, []string, int, error) {
	section := detectMetAPIAccountsSection(payload)
	if section == nil {
		return nil, nil, 0, fmt.Errorf("site import no recognizable metapi accounts section")
	}

	siteRows := asObjectSlice(section["sites"])
	accountRows := asObjectSlice(section["accounts"])
	if len(siteRows) == 0 || len(accountRows) == 0 {
		return nil, nil, 0, fmt.Errorf("metapi accounts section must include sites and accounts")
	}

	tokenRows := asObjectSlice(section["accountTokens"])
	manualModelRows := asObjectSlice(section["manualModels"])
	disabledModelRows := asObjectSlice(section["siteDisabledModels"])
	routeRows := asObjectSlice(section["tokenRoutes"])
	routeChannelRows := asObjectSlice(section["routeChannels"])
	downstreamKeyRows := asObjectSlice(section["downstreamApiKeys"])

	warnings := make([]string, 0)
	if len(routeRows) > 0 || len(routeChannelRows) > 0 {
		warnings = append(warnings, "已跳过 metapi 路由策略和路由通道，导入后由 Octopus 重新同步/投影生成")
	}
	if len(downstreamKeyRows) > 0 {
		warnings = append(warnings, "已跳过 metapi 下游 Key，Octopus API Key 需要单独配置")
	}

	siteByID := make(map[int]importedSiteInput, len(siteRows))
	for _, row := range siteRows {
		id := asInt(row["id"])
		siteURL := normalizeImportBaseURL(firstNonEmptyString(asString(row["url"]), asString(row["baseUrl"]), asString(row["base_url"])))
		siteName := firstNonEmptyString(asString(row["name"]), siteURL)
		if id <= 0 || siteURL == "" {
			continue
		}
		platform, ok := resolveImportedPlatform(firstNonEmptyString(asString(row["platform"]), asString(row["site_type"])), siteURL)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("跳过 metapi 站点 %s：站点平台不受支持", firstNonEmptyString(siteName, fmt.Sprintf("%d", id))))
			continue
		}
		siteByID[id] = importedSiteInput{
			Name:     siteName,
			Platform: platform,
			BaseURL:  siteURL,
		}
	}

	tokensByAccountID := make(map[int][]model.SiteToken)
	for _, row := range tokenRows {
		accountID := asInt(row["accountId"])
		token := strings.TrimSpace(asString(row["token"]))
		if accountID <= 0 || token == "" {
			continue
		}
		groupKey := model.NormalizeSiteGroupKey(firstNonEmptyString(asString(row["tokenGroup"]), asString(row["groupKey"]), asString(row["group_key"])))
		tokensByAccountID[accountID] = append(tokensByAccountID[accountID], model.SiteToken{
			Name:        firstNonEmptyString(asString(row["name"]), groupKey),
			Token:       token,
			ValueStatus: model.NormalizeSiteTokenValueStatus(model.SiteTokenValueStatus(asString(row["valueStatus"])), token),
			GroupKey:    groupKey,
			GroupName:   model.NormalizeSiteGroupName(groupKey, asString(row["tokenGroup"])),
			Enabled:     asBool(row["enabled"], true),
			Source:      firstNonEmptyString(asString(row["source"]), "metapi"),
			IsDefault:   asBool(row["isDefault"], false),
		})
	}

	manualModelsByAccountID := make(map[int][]model.SiteModel)
	for _, row := range manualModelRows {
		accountID := asInt(row["accountId"])
		modelName := strings.TrimSpace(asString(row["modelName"]))
		if accountID <= 0 || modelName == "" {
			continue
		}
		manualModelsByAccountID[accountID] = append(manualModelsByAccountID[accountID], model.SiteModel{
			GroupKey:       model.SiteDefaultGroupKey,
			ModelName:      modelName,
			Source:         "metapi",
			RouteType:      model.InferSiteModelRouteType(modelName),
			RouteSource:    model.SiteModelRouteSourceSyncInferred,
			ManualOverride: false,
			Disabled:       false,
		})
	}

	disabledModelsBySiteID := make(map[int][]string)
	for _, row := range disabledModelRows {
		siteID := asInt(row["siteId"])
		modelName := strings.TrimSpace(asString(row["modelName"]))
		if siteID <= 0 || modelName == "" {
			continue
		}
		disabledModelsBySiteID[siteID] = append(disabledModelsBySiteID[siteID], modelName)
	}

	inputs := make([]metAPIImportAccountData, 0, len(accountRows))
	skipped := 0
	for _, row := range accountRows {
		originalID := asInt(row["id"])
		input, warning, ok := parseMetAPIAccountRow(row, siteByID, tokensByAccountID[originalID])
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if !ok {
			skipped++
			continue
		}
		inputs = append(inputs, metAPIImportAccountData{
			Input:          input,
			OriginalID:     originalID,
			Tokens:         tokensByAccountID[originalID],
			Groups:         buildMetAPIGroups(tokensByAccountID[originalID]),
			Models:         manualModelsByAccountID[originalID],
			DisabledModels: buildMetAPIDisabledModels(disabledModelsBySiteID[asInt(row["siteId"])]),
		})
	}

	return inputs, warnings, skipped, nil
}

func detectMetAPIAccountsSection(payload rawImportObject) rawImportObject {
	if section := coerceMetAPIAccountsSection(payload); section != nil {
		return section
	}
	if section := coerceMetAPIAccountsSection(asObject(payload["accounts"])); section != nil {
		return section
	}
	if data := asObject(payload["data"]); data != nil {
		if section := coerceMetAPIAccountsSection(asObject(data["accounts"])); section != nil {
			return section
		}
	}
	return nil
}

func coerceMetAPIAccountsSection(value rawImportObject) rawImportObject {
	if value == nil {
		return nil
	}
	if len(asObjectSlice(value["sites"])) == 0 || len(asObjectSlice(value["accounts"])) == 0 {
		return nil
	}
	if value["accountTokens"] == nil || value["tokenRoutes"] == nil || value["routeChannels"] == nil {
		return nil
	}
	return value
}

func parseMetAPIAccountRow(row rawImportObject, sites map[int]importedSiteInput, tokens []model.SiteToken) (importedAccountInput, string, bool) {
	accountID := asInt(row["id"])
	siteID := asInt(row["siteId"])
	siteInput, ok := sites[siteID]
	rowID := firstNonEmptyString(asString(row["username"]), fmt.Sprintf("%d", accountID))
	if !ok {
		return importedAccountInput{}, fmt.Sprintf("跳过 metapi 账号 %s：关联站点缺失或不支持", rowID), false
	}

	accessToken := asString(row["accessToken"])
	apiToken := asString(row["apiToken"])
	if apiToken == "" {
		apiToken = firstReadyMetAPITokenValue(tokens)
	}
	extraConfig := asObjectFromJSONString(asString(row["extraConfig"]))
	credentialMode := strings.ToLower(strings.TrimSpace(asString(extraConfig["credentialMode"])))

	input := importedAccountInput{
		Site:           siteInput,
		Name:           firstNonEmptyString(asString(row["username"]), fmt.Sprintf("metapi-account-%d", accountID)),
		Username:       asString(row["username"]),
		Enabled:        metAPIAccountEnabled(row["status"]),
		AutoSync:       true,
		AutoCheckin:    asBool(row["checkinEnabled"], true) && platformSupportsCheckin(siteInput.Platform),
		Balance:        asFloat64(row["balance"]),
		BalanceUsed:    asFloat64(row["balanceUsed"]),
		PlatformUserID: asIntPointer(extraConfig["platformUserId"]),
		AccountProxy:   asStringPointer(extraConfig["proxyUrl"]),
	}

	if isDirectImportPlatform(siteInput.Platform) || credentialMode == "apikey" || accessToken == "" {
		if apiToken == "" {
			return importedAccountInput{}, fmt.Sprintf("跳过 metapi 账号 %s：apiToken 缺失", rowID), false
		}
		input.CredentialType = model.SiteCredentialTypeAPIKey
		input.APIKey = apiToken
		input.AutoCheckin = false
		return input, "", true
	}

	input.CredentialType = model.SiteCredentialTypeAccessToken
	input.AccessToken = accessToken
	input.APIKey = apiToken
	if auth := asObject(extraConfig["sub2apiAuth"]); auth != nil {
		input.RefreshToken = asString(auth["refreshToken"])
		input.TokenExpiresAt = asInt64(auth["tokenExpiresAt"])
	}
	return input, "", true
}

func metAPIAccountEnabled(raw any) bool {
	status := strings.ToLower(strings.TrimSpace(asString(raw)))
	return status == "" || status == "active"
}

func firstReadyMetAPITokenValue(tokens []model.SiteToken) string {
	for _, token := range tokens {
		tokenValue := strings.TrimSpace(token.Token)
		if tokenValue == "" || model.IsMaskedSiteTokenValue(tokenValue) {
			continue
		}
		if !token.Enabled {
			continue
		}
		return tokenValue
	}
	return ""
}

func buildMetAPIGroups(tokens []model.SiteToken) []model.SiteUserGroup {
	seen := make(map[string]model.SiteUserGroup)
	for _, token := range tokens {
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		seen[groupKey] = model.SiteUserGroup{
			GroupKey: groupKey,
			Name:     model.NormalizeSiteGroupName(groupKey, token.GroupName),
		}
	}
	if len(seen) == 0 {
		return nil
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]model.SiteUserGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

func buildMetAPIDisabledModels(modelNames []string) []model.SiteModel {
	result := make([]model.SiteModel, 0, len(modelNames))
	seen := make(map[string]struct{}, len(modelNames))
	for _, name := range modelNames {
		modelName := strings.TrimSpace(name)
		if modelName == "" {
			continue
		}
		key := strings.ToLower(modelName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, model.SiteModel{
			GroupKey:    model.SiteDefaultGroupKey,
			ModelName:   modelName,
			Source:      "metapi",
			RouteType:   model.InferSiteModelRouteType(modelName),
			RouteSource: model.SiteModelRouteSourceSyncInferred,
			Disabled:    true,
		})
	}
	return result
}

func parseAllAPIHubAccountRow(row rawImportObject) (importedAccountInput, string, bool) {
	siteURL := normalizeImportBaseURL(asString(row["site_url"]))
	siteName := firstNonEmptyString(asString(row["site_name"]), siteURL)
	rowID := firstNonEmptyString(asString(row["id"]), asString(row["username"]), siteName, "unknown")
	if siteURL == "" {
		return importedAccountInput{}, fmt.Sprintf("跳过 ALL-API-Hub 账号 %s：site_url 无效", rowID), false
	}

	platform, ok := resolveImportedPlatform(row["site_type"], siteURL)
	if !ok {
		return importedAccountInput{}, fmt.Sprintf("跳过 ALL-API-Hub 账号 %s：站点平台不受支持", rowID), false
	}

	accountInfo := asObject(row["account_info"])
	cookieAuth := asObject(row["cookieAuth"])
	checkin := asObject(row["checkIn"])
	authType := strings.ToLower(strings.TrimSpace(asString(row["authType"])))
	username := firstNonEmptyString(asString(accountInfo["username"]), asString(row["username"]), rowID)
	accessTokenCandidate := firstNonEmptyString(asString(accountInfo["access_token"]), asString(row["access_token"]))
	refreshTokenCandidate := firstNonEmptyString(asString(accountInfo["refresh_token"]), asString(row["refresh_token"]))
	tokenExpiresAt := asInt64(accountInfo["token_expires_at"])
	cookieSession := asString(cookieAuth["sessionCookie"])
	platformUserID := asIntPointer(accountInfo["id"])

	input := importedAccountInput{
		Site: importedSiteInput{
			Name:     siteName,
			Platform: platform,
			BaseURL:  siteURL,
		},
		Name:           username,
		Enabled:        !asBool(row["disabled"], false),
		AutoSync:       true,
		AutoCheckin:    asBool(checkin["autoCheckInEnabled"], true) && platformSupportsCheckin(platform),
		RefreshToken:   refreshTokenCandidate,
		TokenExpiresAt: tokenExpiresAt,
	}
	if platformUserID != nil {
		input.PlatformUserID = platformUserID
	}

	switch authType {
	case "cookie":
		if cookieSession == "" {
			return importedAccountInput{}, fmt.Sprintf("跳过 ALL-API-Hub 账号 %s：cookieAuth.sessionCookie 缺失", rowID), false
		}
		input.CredentialType = model.SiteCredentialTypeAccessToken
		input.AccessToken = cookieSession
	case "access_token", "session":
		if accessTokenCandidate == "" {
			return importedAccountInput{}, fmt.Sprintf("跳过 ALL-API-Hub 账号 %s：access_token 缺失", rowID), false
		}
		if isDirectImportPlatform(platform) {
			input.CredentialType = model.SiteCredentialTypeAPIKey
			input.APIKey = accessTokenCandidate
			input.AutoCheckin = false
		} else {
			input.CredentialType = model.SiteCredentialTypeAccessToken
			input.AccessToken = accessTokenCandidate
		}
	case "api_key":
		if accessTokenCandidate == "" {
			return importedAccountInput{}, fmt.Sprintf("跳过 ALL-API-Hub 账号 %s：api_key 缺失", rowID), false
		}
		input.CredentialType = model.SiteCredentialTypeAPIKey
		input.APIKey = accessTokenCandidate
		input.AutoCheckin = false
	default:
		return importedAccountInput{}, fmt.Sprintf("跳过 ALL-API-Hub 账号 %s：authType=%s 不支持离线导入", rowID, firstNonEmptyString(authType, "unknown")), false
	}

	return input, "", true
}

func parseAllAPIHubProfile(profile rawImportObject) (importedAccountInput, string, bool) {
	baseURL := normalizeImportBaseURL(asString(profile["baseUrl"]))
	apiKey := asString(profile["apiKey"])
	profileID := firstNonEmptyString(asString(profile["id"]), asString(profile["name"]), baseURL, "unknown")
	if baseURL == "" || apiKey == "" {
		return importedAccountInput{}, fmt.Sprintf("跳过 ALL-API-Hub API 凭据 %s：baseUrl 或 apiKey 缺失", profileID), false
	}

	platform, ok := resolveImportedProfilePlatform(profile["apiType"], baseURL)
	if !ok {
		return importedAccountInput{}, fmt.Sprintf("跳过 ALL-API-Hub API 凭据 %s：站点平台不受支持", profileID), false
	}

	return importedAccountInput{
		Site: importedSiteInput{
			Name:     baseURL,
			Platform: platform,
			BaseURL:  baseURL,
		},
		Name:           firstNonEmptyString(asString(profile["name"]), profileID, baseURL),
		CredentialType: model.SiteCredentialTypeAPIKey,
		APIKey:         apiKey,
		Enabled:        true,
		AutoSync:       true,
		AutoCheckin:    false,
	}, "", true
}

func upsertImportedSite(tx *gorm.DB, input importedSiteInput) (*model.Site, bool, error) {
	normalizedBaseURL := normalizeImportBaseURL(input.BaseURL)
	var siteRecord model.Site
	err := tx.Where("platform = ? AND base_url = ?", input.Platform, normalizedBaseURL).First(&siteRecord).Error
	if err == nil {
		return &siteRecord, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, fmt.Errorf("query site failed: %w", err)
	}

	siteRecord = model.Site{
		Name:     uniqueSiteName(tx, firstNonEmptyString(input.Name, normalizedBaseURL)),
		Platform: input.Platform,
		BaseURL:  normalizedBaseURL,
		Enabled:  true,
	}
	if err := siteRecord.Validate(); err != nil {
		return nil, false, err
	}
	if err := tx.Create(&siteRecord).Error; err != nil {
		return nil, false, fmt.Errorf("create site failed: %w", err)
	}
	return &siteRecord, true, nil
}

func replaceMetAPIAccountData(tx *gorm.DB, accountID int, data metAPIImportAccountData) (int, int, int, int, error) {
	groups := prepareMetAPIImportedGroups(accountID, data.Groups)
	tokens := prepareMetAPIImportedTokens(accountID, data.Tokens)
	models := prepareMetAPIImportedModels(accountID, append(data.Models, data.DisabledModels...))

	if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteUserGroup{}).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	if len(groups) > 0 {
		if err := tx.Create(&groups).Error; err != nil {
			return 0, 0, 0, 0, err
		}
	}

	if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteToken{}).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	if len(tokens) > 0 {
		if err := tx.Create(&tokens).Error; err != nil {
			return 0, 0, 0, 0, err
		}
	}

	if err := tx.Where("site_account_id = ?", accountID).Delete(&model.SiteModel{}).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	if len(models) > 0 {
		if err := tx.Create(&models).Error; err != nil {
			return 0, 0, 0, 0, err
		}
	}

	disabled := 0
	for _, item := range models {
		if item.Disabled {
			disabled++
		}
	}
	return len(tokens), len(groups), len(models), disabled, nil
}

func prepareMetAPIImportedGroups(accountID int, groups []model.SiteUserGroup) []model.SiteUserGroup {
	seen := make(map[string]model.SiteUserGroup, len(groups))
	for _, group := range groups {
		groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
		seen[groupKey] = model.SiteUserGroup{
			SiteAccountID: accountID,
			GroupKey:      groupKey,
			Name:          model.NormalizeSiteGroupName(groupKey, group.Name),
			RawPayload:    strings.TrimSpace(group.RawPayload),
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]model.SiteUserGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

func prepareMetAPIImportedTokens(accountID int, tokens []model.SiteToken) []model.SiteToken {
	seen := make(map[string]model.SiteToken, len(tokens))
	for _, token := range tokens {
		tokenValue := strings.TrimSpace(token.Token)
		if tokenValue == "" {
			continue
		}
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		valueStatus := model.NormalizeSiteTokenValueStatus(token.ValueStatus, tokenValue)
		key := groupKey + "\x00" + model.NormalizeComparableSiteTokenValue(tokenValue)
		if key == groupKey+"\x00" {
			key = groupKey + "\x00" + tokenValue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = model.SiteToken{
			SiteAccountID: accountID,
			Name:          firstNonEmptyString(token.Name, groupKey),
			Token:         tokenValue,
			ValueStatus:   valueStatus,
			GroupKey:      groupKey,
			GroupName:     model.NormalizeSiteGroupName(groupKey, token.GroupName),
			Enabled:       token.Enabled && valueStatus == model.SiteTokenValueStatusReady,
			Source:        firstNonEmptyString(token.Source, "metapi"),
			IsDefault:     token.IsDefault && valueStatus == model.SiteTokenValueStatusReady,
			LastSyncAt:    token.LastSyncAt,
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]model.SiteToken, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

func prepareMetAPIImportedModels(accountID int, models []model.SiteModel) []model.SiteModel {
	seen := make(map[string]model.SiteModel, len(models))
	for _, item := range models {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		modelName := strings.TrimSpace(item.ModelName)
		if modelName == "" {
			continue
		}
		key := groupKey + "\x00" + modelName
		current, exists := seen[key]
		if exists && current.Disabled && !item.Disabled {
			continue
		}
		item.SiteAccountID = accountID
		item.GroupKey = groupKey
		item.ModelName = modelName
		item.Source = firstNonEmptyString(item.Source, "metapi")
		if strings.TrimSpace(string(item.RouteType)) == "" {
			item.RouteType = model.InferSiteModelRouteType(modelName)
		} else {
			item.RouteType = model.NormalizeSiteModelRouteType(item.RouteType)
		}
		item.RouteSource = model.NormalizeSiteModelRouteSource(item.RouteSource, item.ManualOverride)
		seen[key] = item
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]model.SiteModel, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

func uniqueSiteName(tx *gorm.DB, baseName string) string {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		baseName = "imported-site"
	}
	candidate := baseName
	index := 2
	for {
		var count int64
		if err := tx.Model(&model.Site{}).Where("name = ?", candidate).Count(&count).Error; err != nil {
			return candidate
		}
		if count == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s (%d)", baseName, index)
		index++
	}
}

func upsertImportedAccount(tx *gorm.DB, siteRecord *model.Site, input importedAccountInput) (*model.SiteAccount, bool, bool, error) {
	accountRecord, err := findImportedAccount(tx, siteRecord.ID, input)
	if err != nil {
		return nil, false, false, err
	}

	if accountRecord == nil {
		created := model.SiteAccount{
			SiteID:                     siteRecord.ID,
			Name:                       strings.TrimSpace(input.Name),
			CredentialType:             input.CredentialType,
			Username:                   strings.TrimSpace(input.Username),
			Password:                   strings.TrimSpace(input.Password),
			AccessToken:                strings.TrimSpace(input.AccessToken),
			APIKey:                     strings.TrimSpace(input.APIKey),
			RefreshToken:               strings.TrimSpace(input.RefreshToken),
			TokenExpiresAt:             input.TokenExpiresAt,
			PlatformUserID:             input.PlatformUserID,
			AccountProxy:               input.AccountProxy,
			Enabled:                    input.Enabled,
			AutoSync:                   input.AutoSync,
			AutoCheckin:                input.AutoCheckin,
			RandomCheckin:              false,
			CheckinIntervalHours:       24,
			CheckinRandomWindowMinutes: 120,
			Balance:                    input.Balance,
			BalanceUsed:                input.BalanceUsed,
		}
		if err := created.Validate(); err != nil {
			return nil, false, false, err
		}
		if err := tx.Model(&model.SiteAccount{}).Create(map[string]any{
			"site_id":                       created.SiteID,
			"name":                          created.Name,
			"credential_type":               created.CredentialType,
			"username":                      created.Username,
			"password":                      created.Password,
			"access_token":                  created.AccessToken,
			"api_key":                       created.APIKey,
			"refresh_token":                 created.RefreshToken,
			"token_expires_at":              created.TokenExpiresAt,
			"platform_user_id":              created.PlatformUserID,
			"account_proxy":                 created.AccountProxy,
			"enabled":                       created.Enabled,
			"auto_sync":                     created.AutoSync,
			"auto_checkin":                  created.AutoCheckin,
			"random_checkin":                created.RandomCheckin,
			"checkin_interval_hours":        created.CheckinIntervalHours,
			"checkin_random_window_minutes": created.CheckinRandomWindowMinutes,
			"balance":                       created.Balance,
			"balance_used":                  created.BalanceUsed,
			"last_sync_status":              model.SiteExecutionStatusIdle,
			"last_checkin_status":           model.SiteExecutionStatusIdle,
		}).Error; err != nil {
			return nil, false, false, fmt.Errorf("create site account failed: %w", err)
		}
		accountRecord, err = findImportedAccount(tx, siteRecord.ID, input)
		if err != nil {
			return nil, false, false, err
		}
		if accountRecord == nil {
			return nil, false, false, fmt.Errorf("created site account could not be reloaded")
		}
		return accountRecord, true, false, nil
	}

	merged := *accountRecord
	merged.Name = strings.TrimSpace(input.Name)
	merged.CredentialType = input.CredentialType
	merged.Username = strings.TrimSpace(input.Username)
	merged.Password = strings.TrimSpace(input.Password)
	merged.AccessToken = strings.TrimSpace(input.AccessToken)
	merged.APIKey = strings.TrimSpace(input.APIKey)
	merged.RefreshToken = strings.TrimSpace(input.RefreshToken)
	merged.TokenExpiresAt = input.TokenExpiresAt
	merged.PlatformUserID = input.PlatformUserID
	merged.AccountProxy = input.AccountProxy
	merged.AutoCheckin = input.AutoCheckin
	if err := merged.Validate(); err != nil {
		return nil, false, false, err
	}

	updates := map[string]any{
		"name":             merged.Name,
		"credential_type":  merged.CredentialType,
		"username":         merged.Username,
		"password":         merged.Password,
		"access_token":     merged.AccessToken,
		"api_key":          merged.APIKey,
		"refresh_token":    merged.RefreshToken,
		"token_expires_at": merged.TokenExpiresAt,
		"platform_user_id": merged.PlatformUserID,
		"account_proxy":    merged.AccountProxy,
		"auto_checkin":     merged.AutoCheckin,
	}
	if err := tx.Model(&model.SiteAccount{}).Where("id = ?", accountRecord.ID).Updates(updates).Error; err != nil {
		return nil, false, false, fmt.Errorf("update site account failed: %w", err)
	}
	accountRecord.Name = merged.Name
	accountRecord.CredentialType = merged.CredentialType
	accountRecord.Username = merged.Username
	accountRecord.Password = merged.Password
	accountRecord.AccessToken = merged.AccessToken
	accountRecord.APIKey = merged.APIKey
	accountRecord.RefreshToken = merged.RefreshToken
	accountRecord.TokenExpiresAt = merged.TokenExpiresAt
	accountRecord.PlatformUserID = merged.PlatformUserID
	accountRecord.AccountProxy = merged.AccountProxy
	accountRecord.AutoCheckin = merged.AutoCheckin
	return accountRecord, false, true, nil
}

func findImportedAccount(tx *gorm.DB, siteID int, input importedAccountInput) (*model.SiteAccount, error) {
	findByQuery := func(query string, args ...any) (*model.SiteAccount, error) {
		var accountRecord model.SiteAccount
		err := tx.Where(query, args...).First(&accountRecord).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &accountRecord, nil
	}

	switch input.CredentialType {
	case model.SiteCredentialTypeUsernamePassword:
		if input.Username != "" {
			record, err := findByQuery("site_id = ? AND credential_type = ? AND username = ?", siteID, input.CredentialType, strings.TrimSpace(input.Username))
			if record != nil || err != nil {
				return record, err
			}
		}
	case model.SiteCredentialTypeAccessToken:
		if input.AccessToken != "" {
			record, err := findByQuery("site_id = ? AND credential_type = ? AND access_token = ?", siteID, input.CredentialType, strings.TrimSpace(input.AccessToken))
			if record != nil || err != nil {
				return record, err
			}
		}
	case model.SiteCredentialTypeAPIKey:
		if input.APIKey != "" {
			record, err := findByQuery("site_id = ? AND credential_type = ? AND api_key = ?", siteID, input.CredentialType, strings.TrimSpace(input.APIKey))
			if record != nil || err != nil {
				return record, err
			}
		}
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, nil
	}

	var matches []model.SiteAccount
	if err := tx.Where("site_id = ? AND name = ?", siteID, name).Find(&matches).Error; err != nil {
		return nil, fmt.Errorf("query site account by name failed: %w", err)
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	return nil, nil
}

func normalizeImportBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/")
	}
	return strings.TrimRight(trimmed, "/")
}

func resolveImportedPlatform(rawPlatform any, rawURL string) (model.SitePlatform, bool) {
	if hinted, unsupported := detectSupportedPlatform(rawPlatform, rawURL); unsupported {
		return "", false
	} else if hinted != "" {
		return hinted, true
	}

	value := strings.ToLower(strings.TrimSpace(asString(rawPlatform)))
	if platform, ok := supportedImportPlatforms[value]; ok {
		return platform, true
	}
	if strings.Contains(value, "wong") {
		return model.SitePlatformNewAPI, true
	}
	if strings.Contains(value, "done") {
		return model.SitePlatformDoneHub, true
	}
	if strings.Contains(value, "anyrouter") {
		return model.SitePlatformAnyRouter, true
	}
	if value == "" {
		return model.SitePlatformNewAPI, true
	}
	return model.SitePlatformNewAPI, true
}

func resolveImportedProfilePlatform(rawType any, baseURL string) (model.SitePlatform, bool) {
	if hinted, unsupported := detectSupportedPlatform(rawType, baseURL); unsupported {
		return "", false
	} else if hinted != "" {
		return hinted, true
	}

	switch strings.ToLower(strings.TrimSpace(asString(rawType))) {
	case "openai":
		return model.SitePlatformOpenAI, true
	case "anthropic":
		return model.SitePlatformClaude, true
	case "google":
		return model.SitePlatformGemini, true
	case "openai-compatible", "":
		return model.SitePlatformOpenAI, true
	default:
		return model.SitePlatformOpenAI, true
	}
}

func detectSupportedPlatform(values ...any) (model.SitePlatform, bool) {
	joined := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.ToLower(strings.TrimSpace(asString(value)))
		if text != "" {
			joined = append(joined, text)
		}
	}
	combined := strings.Join(joined, " ")
	for _, hint := range unsupportedImportHints {
		if strings.Contains(combined, hint) {
			return "", true
		}
	}

	switch {
	case strings.Contains(combined, "api.openai.com"):
		return model.SitePlatformOpenAI, false
	case strings.Contains(combined, "api.anthropic.com"), strings.Contains(combined, "anthropic.com/v1"):
		return model.SitePlatformClaude, false
	case strings.Contains(combined, "generativelanguage.googleapis.com"),
		strings.Contains(combined, "googleapis.com/v1beta/openai"),
		strings.Contains(combined, "gemini.google.com"):
		return model.SitePlatformGemini, false
	case strings.Contains(combined, "anyrouter"):
		return model.SitePlatformAnyRouter, false
	case strings.Contains(combined, "donehub"), strings.Contains(combined, "done-hub"):
		return model.SitePlatformDoneHub, false
	case strings.Contains(combined, "onehub"), strings.Contains(combined, "one-hub"):
		return model.SitePlatformOneHub, false
	case strings.Contains(combined, "oneapi"), strings.Contains(combined, "one-api"):
		return model.SitePlatformOneAPI, false
	case strings.Contains(combined, "sub2api"):
		return model.SitePlatformSub2API, false
	}
	return "", false
}

func isDirectImportPlatform(platform model.SitePlatform) bool {
	_, ok := directImportPlatforms[platform]
	return ok
}

func platformSupportsCheckin(platform model.SitePlatform) bool {
	switch platform {
	case model.SitePlatformDoneHub, model.SitePlatformSub2API, model.SitePlatformOpenAI, model.SitePlatformClaude, model.SitePlatformGemini:
		return false
	default:
		return true
	}
}

func asObject(value any) rawImportObject {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}

func asObjectFromJSONString(value string) rawImportObject {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var result rawImportObject
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil
	}
	return result
}

func asObjectSlice(value any) []rawImportObject {
	typed, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]rawImportObject, 0, len(typed))
	for _, item := range typed {
		if row := asObject(item); row != nil {
			result = append(result, row)
		}
	}
	return result
}

func asStringPointer(value any) *string {
	trimmed := asString(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	case int:
		return strings.TrimSpace(fmt.Sprintf("%d", typed))
	case int64:
		return strings.TrimSpace(fmt.Sprintf("%d", typed))
	default:
		return ""
	}
}

func asBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	case float64:
		return typed != 0
	case int:
		return typed != 0
	}
	return fallback
}

func asIntPointer(value any) *int {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return &typed
		}
	case int64:
		if typed > 0 {
			result := int(typed)
			return &result
		}
	case float64:
		if typed > 0 {
			result := int(typed)
			return &result
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			result := int(parsed)
			return &result
		}
	case string:
		if parsed, err := json.Number(strings.TrimSpace(typed)).Int64(); err == nil && parsed > 0 {
			result := int(parsed)
			return &result
		}
	}
	return nil
}

func asInt(value any) int {
	if parsed := asInt64(value); parsed > 0 {
		return int(parsed)
	}
	return 0
}

func asInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return int64(typed)
		}
	case int64:
		if typed > 0 {
			return typed
		}
	case float64:
		if typed > 0 {
			return int64(typed)
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return parsed
		}
	case string:
		if parsed, err := json.Number(strings.TrimSpace(typed)).Int64(); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func asFloat64(value any) float64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return typed
		}
	case float32:
		if typed > 0 {
			return float64(typed)
		}
	case int:
		if typed > 0 {
			return float64(typed)
		}
	case int64:
		if typed > 0 {
			return float64(typed)
		}
	case json.Number:
		if parsed, err := typed.Float64(); err == nil && parsed > 0 {
			return parsed
		}
	case string:
		if parsed, err := json.Number(strings.TrimSpace(typed)).Float64(); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
