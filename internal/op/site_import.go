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
	PlatformUserID *int
	Enabled        bool
	AutoSync       bool
	AutoCheckin    bool
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
		return nil, nil, fmt.Errorf("invalid all api hub payload: %w", err)
	}
	if len(payload) == 0 {
		return nil, nil, fmt.Errorf("empty all api hub payload")
	}

	inputs, warnings, skipped, err := extractAllAPIHubAccounts(payload)
	if err != nil {
		return nil, nil, err
	}
	if len(inputs) == 0 {
		return nil, nil, fmt.Errorf("no importable all api hub site account data found")
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
		return nil, warnings, skipped, fmt.Errorf("no recognizable all api hub payload sections found")
	}

	return inputs, warnings, skipped, nil
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
	cookieSession := asString(cookieAuth["sessionCookie"])
	platformUserID := asIntPointer(accountInfo["id"])

	input := importedAccountInput{
		Site: importedSiteInput{
			Name:     siteName,
			Platform: platform,
			BaseURL:  siteURL,
		},
		Name:        username,
		Enabled:     !asBool(row["disabled"], false),
		AutoSync:    true,
		AutoCheckin: asBool(checkin["autoCheckInEnabled"], true) && platformSupportsCheckin(platform),
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
			PlatformUserID:             input.PlatformUserID,
			Enabled:                    input.Enabled,
			AutoSync:                   input.AutoSync,
			AutoCheckin:                input.AutoCheckin,
			RandomCheckin:              false,
			CheckinIntervalHours:       24,
			CheckinRandomWindowMinutes: 120,
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
			"platform_user_id":              created.PlatformUserID,
			"enabled":                       created.Enabled,
			"auto_sync":                     created.AutoSync,
			"auto_checkin":                  created.AutoCheckin,
			"random_checkin":                created.RandomCheckin,
			"checkin_interval_hours":        created.CheckinIntervalHours,
			"checkin_random_window_minutes": created.CheckinRandomWindowMinutes,
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
	merged.PlatformUserID = input.PlatformUserID
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
		"platform_user_id": merged.PlatformUserID,
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
	accountRecord.PlatformUserID = merged.PlatformUserID
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
