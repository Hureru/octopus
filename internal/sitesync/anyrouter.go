package sitesync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/bestruirui/octopus/internal/model"
)

const anyRouterUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"

var (
	anyRouterArg1Pattern           = regexp.MustCompile(`var\s+arg1\s*=\s*['"]([0-9a-fA-F]+)['"]`)
	anyRouterMappingPattern        = regexp.MustCompile(`for\(var m=\[([^\]]+)\],p=L\(0x115\)`)
	anyRouterEncodedArrayPattern   = regexp.MustCompile(`var\s+N=\[(.*?)\];a0i=`)
	anyRouterArrayStringPattern    = regexp.MustCompile(`'([^']*)'`)
	anyRouterRotationTargetPattern = regexp.MustCompile(`\}\(a0i,\s*(0x[0-9a-fA-F]+|\d+)\)`)
	anyRouterBaseOffsetPattern     = regexp.MustCompile(`d=d-(0x[0-9a-fA-F]+|\d+);`)
	anyRouterNumericUnderscoreRE   = regexp.MustCompile(`_(\d{4,8})(?:\D|$)`)
	anyRouterNumericKeywordRE      = regexp.MustCompile(`(?:user(?:name)?|uid|id)[^\d]{0,16}(\d{4,8})(?:\D|$)`)
	anyRouterTitlePattern          = regexp.MustCompile(`(?is)<title>\s*([^<]+?)\s*</title>`)
	anyRouterHTMLCodePattern       = regexp.MustCompile(`(?is)<span[^>]*>\s*Error\s*</span>\s*<span[^>]*>\s*(\d{3,4})\s*</span>`)
	anyRouterLooseCodePattern      = regexp.MustCompile(`\bError\s*(\d{3,4})\b`)
)

func syncAnyRouter(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*syncSnapshot, error) {
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return syncWithDirectToken(ctx, siteRecord, account, resolveDirectToken(account), "manual")
	}

	accessToken, err := resolveAnyRouterManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return nil, err
	}

	userID, _ := anyRouterDiscoverUserID(ctx, siteRecord, account, accessToken)
	tokens, err := fetchAnyRouterManagementTokens(ctx, siteRecord, account, accessToken, userID)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 && strings.TrimSpace(account.APIKey) != "" {
		tokens = append(tokens, model.SiteToken{
			Name:      "default",
			Token:     strings.TrimSpace(account.APIKey),
			GroupKey:  model.SiteDefaultGroupKey,
			GroupName: model.SiteDefaultGroupName,
			Enabled:   true,
			Source:    "fallback",
			IsDefault: true,
		})
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no usable site token found")
	}

	groups, err := fetchAnyRouterManagementGroups(ctx, siteRecord, account, accessToken, userID)
	if err != nil {
		groups = nil
	}
	groups = mergeSiteGroups(groups, tokens)

	models, err := fetchModelsForSiteToken(ctx, siteRecord, account, pickModelToken(tokens))
	if err != nil || len(models) == 0 {
		fallbackModels, fallbackErr := fetchAnyRouterSessionModels(ctx, siteRecord, account, accessToken, userID)
		if fallbackErr == nil && len(fallbackModels) > 0 {
			models = fallbackModels
		} else if err != nil {
			return nil, err
		}
	}

	return &syncSnapshot{
		accessToken: accessToken,
		groups:      groups,
		tokens:      tokens,
		models:      buildGlobalSiteModels(models, groups, "sync"),
		message:     "site account synced",
	}, nil
}

func checkinAnyRouter(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*model.SiteCheckinResult, string, error) {
	accessToken, err := resolveAnyRouterManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return nil, accessToken, err
	}

	userID, _ := anyRouterDiscoverUserID(ctx, siteRecord, account, accessToken)
	if result, message, ok := anyRouterTryCheckinWithBearer(ctx, siteRecord, account, accessToken, userID); ok {
		return result, accessToken, nil
	} else if message != "" && !anyRouterShouldFallbackToCookieCheckin(message) {
		return &model.SiteCheckinResult{Status: model.SiteExecutionStatusFailed, Message: message}, accessToken, nil
	}

	result, message := anyRouterTryCheckinWithCookies(ctx, siteRecord, account, accessToken, userID)
	if result != nil {
		return result, accessToken, nil
	}

	alternateUserID, _ := anyRouterProbeAlternateUserIDByCookie(ctx, siteRecord, account, accessToken, userID)
	if alternateUserID > 0 {
		result, message = anyRouterTryCheckinWithCookies(ctx, siteRecord, account, accessToken, alternateUserID)
		if result != nil {
			return result, accessToken, nil
		}
	}

	return &model.SiteCheckinResult{
		Status:  model.SiteExecutionStatusFailed,
		Message: firstNonEmptyString(message, "checkin failed"),
	}, accessToken, nil
}

func resolveAnyRouterManagedAccessToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (string, error) {
	if account.CredentialType == model.SiteCredentialTypeAccessToken {
		token := strings.TrimSpace(account.AccessToken)
		if token == "" {
			return "", fmt.Errorf("access token is required")
		}
		return token, nil
	}
	if account.CredentialType != model.SiteCredentialTypeUsernamePassword {
		return "", fmt.Errorf("managed access token is not available for credential type %s", account.CredentialType)
	}

	payload, cookieHeader, err := anyRouterRequestJSONWithCookies(
		ctx,
		siteRecord,
		http.MethodPost,
		buildSiteURL(siteRecord.BaseURL, "/api/user/login"),
		map[string]any{"username": account.Username, "password": account.Password},
		map[string]string{"X-Requested-With": "XMLHttpRequest"},
		account,
	)
	if err != nil {
		return "", err
	}
	if payload == nil {
		return "", fmt.Errorf("shield challenge blocked login")
	}
	if !jsonBool(payload["success"]) {
		return "", fmt.Errorf("%s", firstNonEmptyString(anyRouterExtractResponseMessage(payload), "login failed"))
	}

	for _, candidate := range []string{
		jsonString(payload["data"]),
		jsonString(payload["token"]),
		jsonString(payload["access_token"]),
		jsonString(payload["accessToken"]),
		jsonString(nestedValue(payload, "data", "token")),
		jsonString(nestedValue(payload, "data", "access_token")),
		jsonString(nestedValue(payload, "data", "accessToken")),
	} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate), nil
		}
	}
	if anyRouterHasUsableSessionCookie(cookieHeader) {
		return cookieHeader, nil
	}
	return "", fmt.Errorf("login succeeded but no access token was returned")
}

func fetchAnyRouterManagementTokens(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) ([]model.SiteToken, error) {
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/token/?p=0&size=100")

	payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil, anyRouterAuthHeaders(accessToken, userID), account)
	if err != nil {
		return nil, err
	}
	if tokens := buildSiteTokensFromPayload(payload); len(tokens) > 0 {
		return tokens, nil
	}

	cookieTokens, cookieErr := fetchAnyRouterTokensByCookie(ctx, siteRecord, account, accessToken, userID)
	if len(cookieTokens) > 0 {
		return cookieTokens, nil
	}
	if cookieErr != nil {
		return nil, cookieErr
	}
	return nil, nil
}

func fetchAnyRouterManagementGroups(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) ([]model.SiteUserGroup, error) {
	endpoints := []string{"/api/user/self/groups", "/api/user_group_map"}
	seen := make(map[string]model.SiteUserGroup)
	var terminalErr error

	for _, endpoint := range endpoints {
		payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, buildSiteURL(siteRecord.BaseURL, endpoint), nil, anyRouterAuthHeaders(accessToken, userID), account)
		if err != nil {
			continue
		}
		if payload != nil && jsonBool(payload["success"]) == false {
			if message := anyRouterResolveGroupFetchErrorMessage(payload); message != "" {
				terminalErr = fmt.Errorf("%s", message)
			}
		}
		for _, group := range parseGroupItems(payload) {
			key := model.NormalizeSiteGroupKey(group.GroupKey)
			group.GroupKey = key
			group.Name = model.NormalizeSiteGroupName(key, group.Name)
			group.RawPayload = marshalRawPayload(payload)
			seen[key] = group
		}
	}
	if len(seen) > 0 {
		return anyRouterGroupMapToSlice(seen), nil
	}

	cookieGroups, cookieErr := fetchAnyRouterGroupsByCookie(ctx, siteRecord, account, accessToken, userID)
	if len(cookieGroups) > 0 {
		return cookieGroups, nil
	}
	if cookieErr != nil {
		return nil, cookieErr
	}
	if terminalErr != nil {
		return nil, terminalErr
	}
	return []model.SiteUserGroup{{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}}, nil
}

func fetchAnyRouterSessionModels(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) ([]string, error) {
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/user/models")

	payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil, anyRouterAuthHeaders(accessToken, userID), account)
	if err == nil {
		if models := anyRouterParseModelNames(payload); len(models) > 0 {
			return models, nil
		}
	}

	for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
		headers := map[string]string{"Cookie": cookie}
		anyRouterAddUserIDHeaders(headers, userID)
		payload, _, requestErr := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil, headers, account)
		if requestErr != nil {
			continue
		}
		if models := anyRouterParseModelNames(payload); len(models) > 0 {
			return models, nil
		}
	}
	return nil, err
}

func anyRouterTryCheckinWithBearer(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (*model.SiteCheckinResult, string, bool) {
	payload, _, err := anyRouterRequestJSONWithCookies(
		ctx,
		siteRecord,
		http.MethodPost,
		buildSiteURL(siteRecord.BaseURL, "/api/user/checkin"),
		nil,
		anyRouterAuthHeaders(accessToken, userID),
		account,
	)
	if err != nil {
		return nil, err.Error(), false
	}
	if payload == nil {
		return nil, "", false
	}
	if result, ok := anyRouterBuildCheckinResult(payload); ok {
		return result, result.Message, true
	}
	return nil, anyRouterExtractResponseMessage(payload), false
}

func anyRouterTryCheckinWithCookies(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (*model.SiteCheckinResult, string) {
	firstFailure := ""
	for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
		signInPayload, _, signInErr := anyRouterRequestJSONWithCookies(
			ctx,
			siteRecord,
			http.MethodPost,
			buildSiteURL(siteRecord.BaseURL, "/api/user/sign_in"),
			map[string]any{},
			map[string]string{
				"Cookie":           cookie,
				"X-Requested-With": "XMLHttpRequest",
			},
			account,
		)
		if signInErr == nil && signInPayload != nil {
			if result, ok := anyRouterBuildCheckinResult(signInPayload); ok {
				return result, result.Message
			}
			if message := anyRouterExtractResponseMessage(signInPayload); message != "" && firstFailure == "" {
				firstFailure = message
			}
		} else if signInErr != nil && firstFailure == "" {
			firstFailure = signInErr.Error()
		}

		headers := map[string]string{"Cookie": cookie}
		anyRouterAddUserIDHeaders(headers, userID)
		payload, _, err := anyRouterRequestJSONWithCookies(
			ctx,
			siteRecord,
			http.MethodPost,
			buildSiteURL(siteRecord.BaseURL, "/api/user/checkin"),
			nil,
			headers,
			account,
		)
		if err == nil && payload != nil {
			if result, ok := anyRouterBuildCheckinResult(payload); ok {
				return result, result.Message
			}
			if message := anyRouterExtractResponseMessage(payload); message != "" {
				firstFailure = message
			}
			continue
		}
		if err != nil {
			firstFailure = err.Error()
		}
	}

	return nil, firstNonEmptyString(firstFailure, "checkin failed")
}

func anyRouterBuildCheckinResult(payload map[string]any) (*model.SiteCheckinResult, bool) {
	if payload == nil {
		return nil, false
	}
	message := firstNonEmptyString(anyRouterExtractResponseMessage(payload), "checkin success")
	lowered := strings.ToLower(message)
	if jsonBool(payload["success"]) || strings.Contains(lowered, "already") || strings.Contains(message, "已签到") {
		return &model.SiteCheckinResult{
			Status:  model.SiteExecutionStatusSuccess,
			Message: message,
			Reward:  jsonString(nestedValue(payload, "data", "reward")),
		}, true
	}
	return nil, false
}

func anyRouterShouldFallbackToCookieCheckin(message string) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return true
	}
	return strings.Contains(text, "unexpected token") ||
		strings.Contains(text, "not valid json") ||
		strings.Contains(text, "<html") ||
		strings.Contains(text, "new-api-user") ||
		strings.Contains(text, "access token") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "forbidden") ||
		strings.Contains(text, "not login") ||
		strings.Contains(text, "not logged") ||
		strings.Contains(text, "invalid url (post /api/user/checkin)") ||
		(strings.Contains(text, "http 404") && strings.Contains(text, "/api/user/checkin")) ||
		strings.Contains(text, "未登录") ||
		strings.Contains(text, "未提供")
}

func anyRouterDiscoverUserID(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) (int, error) {
	if jwtID := anyRouterTryDecodeJWTUserID(accessToken); jwtID > 0 {
		if ok, _ := anyRouterTestBearerUserID(ctx, siteRecord, account, accessToken, jwtID); ok {
			return jwtID, nil
		}
	}

	payload, _, err := anyRouterRequestJSONWithCookies(
		ctx,
		siteRecord,
		http.MethodGet,
		buildSiteURL(siteRecord.BaseURL, "/api/user/self"),
		nil,
		map[string]string{"Authorization": "Bearer " + strings.TrimSpace(accessToken)},
		account,
	)
	if err == nil {
		if userID := anyRouterExtractUserID(payload); userID > 0 {
			return userID, nil
		}
	}

	for _, userID := range anyRouterBuildUserIDProbeCandidates(accessToken) {
		if ok, _ := anyRouterTestBearerUserID(ctx, siteRecord, account, accessToken, userID); ok {
			return userID, nil
		}
	}

	if payload, _, cookieErr := anyRouterFetchUserSelfByCookie(ctx, siteRecord, account, accessToken, 0); cookieErr == nil {
		if userID := anyRouterExtractUserID(payload); userID > 0 {
			return userID, nil
		}
	}

	if userID, probeErr := anyRouterProbeUserIDByCookie(ctx, siteRecord, account, accessToken); userID > 0 || probeErr != nil {
		return userID, probeErr
	}

	return 0, nil
}

func anyRouterTestBearerUserID(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (bool, error) {
	payload, _, err := anyRouterRequestJSONWithCookies(
		ctx,
		siteRecord,
		http.MethodGet,
		buildSiteURL(siteRecord.BaseURL, "/api/user/self"),
		nil,
		anyRouterAuthHeaders(accessToken, userID),
		account,
	)
	if err != nil {
		return false, err
	}
	return payload != nil && jsonBool(payload["success"]) && anyRouterExtractUserID(payload) > 0, nil
}

func anyRouterFetchUserSelfByCookie(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (map[string]any, string, error) {
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/user/self")
	for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
		headers := map[string]string{"Cookie": cookie}
		anyRouterAddUserIDHeaders(headers, userID)
		payload, cookieHeader, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil, headers, account)
		if err != nil {
			continue
		}
		if payload != nil && jsonBool(payload["success"]) && anyRouterExtractUserID(payload) > 0 {
			return payload, cookieHeader, nil
		}
	}
	return nil, "", nil
}

func anyRouterProbeUserIDByCookie(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) (int, error) {
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/user/self")
	for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
		for _, userID := range anyRouterBuildUserIDProbeCandidates(accessToken) {
			headers := map[string]string{"Cookie": cookie}
			anyRouterAddUserIDHeaders(headers, userID)
			payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil, headers, account)
			if err != nil {
				continue
			}
			if payload != nil && jsonBool(payload["success"]) && anyRouterExtractUserID(payload) > 0 {
				return userID, nil
			}
		}
	}
	return 0, nil
}

func anyRouterProbeAlternateUserIDByCookie(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, currentUserID int) (int, error) {
	probed, err := anyRouterProbeUserIDByCookie(ctx, siteRecord, account, accessToken)
	if err != nil {
		return 0, err
	}
	if probed <= 0 || probed == currentUserID {
		return 0, nil
	}
	return probed, nil
}

func fetchAnyRouterTokensByCookie(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) ([]model.SiteToken, error) {
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/token/?p=0&size=100")
	tryUserIDs := []int{userID}
	if alternateUserID, _ := anyRouterProbeAlternateUserIDByCookie(ctx, siteRecord, account, accessToken, userID); alternateUserID > 0 {
		tryUserIDs = append(tryUserIDs, alternateUserID)
	}
	tryUserIDs = slices.Compact(tryUserIDs)

	for _, candidateUserID := range tryUserIDs {
		for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
			headers := map[string]string{"Cookie": cookie}
			anyRouterAddUserIDHeaders(headers, candidateUserID)
			payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil, headers, account)
			if err != nil {
				continue
			}
			if tokens := buildSiteTokensFromPayload(payload); len(tokens) > 0 {
				return tokens, nil
			}
		}
	}
	return nil, nil
}

func fetchAnyRouterGroupsByCookie(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) ([]model.SiteUserGroup, error) {
	endpoints := []string{"/api/user/self/groups", "/api/user_group_map"}
	tryUserIDs := []int{userID}
	if alternateUserID, _ := anyRouterProbeAlternateUserIDByCookie(ctx, siteRecord, account, accessToken, userID); alternateUserID > 0 {
		tryUserIDs = append(tryUserIDs, alternateUserID)
	}
	tryUserIDs = slices.Compact(tryUserIDs)

	seen := make(map[string]model.SiteUserGroup)
	var terminalErr error

	for _, candidateUserID := range tryUserIDs {
		for _, endpoint := range endpoints {
			requestURL := buildSiteURL(siteRecord.BaseURL, endpoint)
			for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
				headers := map[string]string{"Cookie": cookie}
				anyRouterAddUserIDHeaders(headers, candidateUserID)
				payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil, headers, account)
				if err != nil {
					continue
				}
				if payload != nil && jsonBool(payload["success"]) == false {
					if message := anyRouterResolveGroupFetchErrorMessage(payload); message != "" {
						terminalErr = fmt.Errorf("%s", message)
					}
				}
				for _, group := range parseGroupItems(payload) {
					key := model.NormalizeSiteGroupKey(group.GroupKey)
					group.GroupKey = key
					group.Name = model.NormalizeSiteGroupName(key, group.Name)
					group.RawPayload = marshalRawPayload(payload)
					seen[key] = group
				}
			}
		}
	}
	if len(seen) > 0 {
		return anyRouterGroupMapToSlice(seen), nil
	}
	return nil, terminalErr
}

func buildSiteTokensFromPayload(payload map[string]any) []model.SiteToken {
	items := parseTokenItems(payload)
	tokens := make([]model.SiteToken, 0, len(items))
	for index, item := range items {
		tokenValue := strings.TrimSpace(jsonString(item["key"]))
		if tokenValue == "" {
			continue
		}
		groupKey := model.NormalizeSiteGroupKey(firstNonEmptyString(
			jsonString(item["group"]),
			jsonString(item["token_group"]),
			jsonString(item["group_name"]),
		))
		groupName := model.NormalizeSiteGroupName(groupKey, firstNonEmptyString(
			jsonString(item["group_name"]),
			jsonString(item["group"]),
			jsonString(item["token_group"]),
		))
		tokens = append(tokens, model.SiteToken{
			Name:      firstNonEmptyString(strings.TrimSpace(jsonString(item["name"])), fmt.Sprintf("token-%d", index+1)),
			Token:     tokenValue,
			GroupKey:  groupKey,
			GroupName: groupName,
			Enabled:   parseEnabledFlag(item["status"]),
			Source:    "sync",
			IsDefault: index == 0,
		})
	}
	return tokens
}

func anyRouterParseModelNames(payload map[string]any) []string {
	if payload == nil {
		return nil
	}
	if values, ok := nestedValue(payload, "data").([]any); ok {
		names := make([]string, 0, len(values))
		for _, value := range values {
			if name := strings.TrimSpace(fmt.Sprint(value)); name != "" && name != "<nil>" {
				names = append(names, name)
			}
		}
		return normalizeModelNames(names)
	}
	if dataMap, ok := nestedValue(payload, "data").(map[string]any); ok {
		names := make([]string, 0, len(dataMap))
		for key := range dataMap {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				names = append(names, trimmed)
			}
		}
		return normalizeModelNames(names)
	}
	return nil
}

func anyRouterExtractUserID(payload map[string]any) int {
	if payload == nil {
		return 0
	}
	switch typed := nestedValue(payload, "data", "id").(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		value, _ := strconv.Atoi(strings.TrimSpace(typed))
		return value
	default:
		return 0
	}
}

func anyRouterAuthHeaders(accessToken string, userID int) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + strings.TrimSpace(accessToken),
	}
	anyRouterAddUserIDHeaders(headers, userID)
	return headers
}

func anyRouterAddUserIDHeaders(headers map[string]string, userID int) {
	if headers == nil || userID <= 0 {
		return
	}
	value := strconv.Itoa(userID)
	headers["New-API-User"] = value
	headers["Veloera-User"] = value
	headers["voapi-user"] = value
	headers["User-id"] = value
	headers["Rix-Api-User"] = value
	headers["neo-api-user"] = value
}

func anyRouterBuildCookieCandidates(token string) []string {
	raw := strings.TrimSpace(token)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	seen := make(map[string]struct{}, 3)
	candidates := make([]string, 0, 3)
	appendCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}
	if strings.Contains(raw, "=") {
		appendCandidate(raw)
	}
	appendCandidate("session=" + raw)
	appendCandidate("token=" + raw)
	return candidates
}

func anyRouterTryDecodeJWTUserID(token string) int {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return 0
	}
	payloadBytes, ok := anyRouterDecodeBase64String(parts[1])
	if !ok {
		return 0
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadBytes), &payload); err != nil {
		return 0
	}
	if id := anyRouterParseInt(payload["id"]); id > 0 {
		return id
	}
	return anyRouterParseInt(payload["sub"])
}

func anyRouterBuildUserIDProbeCandidates(token string) []int {
	seen := make(map[int]struct{})
	candidates := make([]int, 0, 18)
	appendCandidate := func(value int) {
		if value <= 0 || value > 10_000_000 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}
	appendCandidate(anyRouterTryDecodeJWTUserID(token))
	for _, value := range anyRouterExtractLikelyUserIDs(token) {
		appendCandidate(value)
	}
	for _, value := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15, 20, 50, 100, 8899, 11494} {
		appendCandidate(value)
	}
	return candidates
}

func anyRouterParseInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		return n
	default:
		return 0
	}
}

func anyRouterRequestJSONWithCookies(ctx context.Context, siteRecord *model.Site, method string, requestURL string, body any, headers map[string]string, accounts ...*model.SiteAccount) (map[string]any, string, error) {
	httpClient, err := siteHTTPClient(siteRecord, accounts...)
	if err != nil {
		return nil, "", err
	}

	var payloadBytes []byte
	if body != nil {
		payloadBytes, err = json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
	}

	mergedHeaders := map[string]string{
		"User-Agent": anyRouterUserAgent,
	}
	if len(payloadBytes) > 0 {
		mergedHeaders["Content-Type"] = "application/json"
	}
	for _, item := range siteRecord.CustomHeader {
		if strings.TrimSpace(item.HeaderKey) != "" {
			mergedHeaders[strings.TrimSpace(item.HeaderKey)] = item.HeaderValue
		}
	}
	for key, value := range headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			mergedHeaders[key] = value
		}
	}

	cookieHeader := firstNonEmptyString(mergedHeaders["Cookie"], mergedHeaders["cookie"])
	delete(mergedHeaders, "cookie")
	if cookieHeader != "" {
		mergedHeaders["Cookie"] = cookieHeader
	}

	for attempt := 0; attempt < 3; attempt++ {
		var bodyReader io.Reader
		if len(payloadBytes) > 0 {
			bodyReader = bytes.NewReader(payloadBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
		if err != nil {
			return nil, cookieHeader, err
		}
		for key, value := range mergedHeaders {
			req.Header.Set(key, value)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, cookieHeader, err
		}

		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, cookieHeader, readErr
		}

		cookieHeader = anyRouterMergeSetCookiePairs(cookieHeader, resp.Header.Values("Set-Cookie"))
		if cookieHeader != "" {
			mergedHeaders["Cookie"] = cookieHeader
		}

		if payload, ok := anyRouterParseJSONObject(bodyBytes); ok {
			return payload, cookieHeader, nil
		}

		text := strings.TrimSpace(string(bodyBytes))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if anyRouterIsShieldChallenge(resp.Header.Get("Content-Type"), text) && cookieHeader != "" {
				acwScV2 := anyRouterSolveAcwScV2(text)
				if acwScV2 != "" {
					cookieHeader = anyRouterUpsertCookie(cookieHeader, "acw_sc__v2", acwScV2)
					mergedHeaders["Cookie"] = cookieHeader
					continue
				}
			}
			return nil, cookieHeader, nil
		}

		return nil, cookieHeader, anyRouterFormatHTTPError(resp.StatusCode, text)
	}

	return nil, cookieHeader, nil
}

func anyRouterParseJSONObject(body []byte) (map[string]any, bool) {
	if len(body) == 0 {
		return map[string]any{}, true
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func anyRouterFormatHTTPError(statusCode int, body string) error {
	if payload, ok := anyRouterParseJSONObject([]byte(body)); ok {
		if message := anyRouterExtractResponseMessage(payload); message != "" {
			return fmt.Errorf("http %d: %s", statusCode, message)
		}
	}
	if summary := anyRouterExtractHTMLErrorSummary(body); summary != "" {
		return fmt.Errorf("http %d: %s", statusCode, summary)
	}
	return fmt.Errorf("http %d: %s", statusCode, strings.TrimSpace(body))
}

func anyRouterExtractResponseMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	return firstNonEmptyString(
		jsonString(payload["message"]),
		jsonString(nestedValue(payload, "error", "message")),
		jsonString(payload["msg"]),
	)
}

func anyRouterResolveGroupFetchErrorMessage(payload map[string]any) string {
	message := strings.TrimSpace(anyRouterExtractResponseMessage(payload))
	lowered := strings.ToLower(message)
	if strings.Contains(lowered, "expired") ||
		strings.Contains(lowered, "invalid token") ||
		strings.Contains(lowered, "access token") ||
		strings.Contains(lowered, "unauthorized") ||
		strings.Contains(lowered, "forbidden") ||
		strings.Contains(lowered, "未登录") ||
		strings.Contains(lowered, "登录") ||
		strings.Contains(lowered, "过期") {
		return "账号会话可能已过期，请重新登录后再拉取分组"
	}
	return message
}

func anyRouterHasUsableSessionCookie(cookieHeader string) bool {
	if cookieHeader == "" {
		return false
	}
	ignored := map[string]struct{}{
		"acw_tc":     {},
		"acw_sc__v2": {},
		"cdn_sec_tc": {},
	}
	for _, part := range strings.Split(cookieHeader, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := strings.SplitN(part, "=", 2)
		if len(segments) != 2 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(segments[0]))
		if _, ok := ignored[name]; ok {
			continue
		}
		if name == "session" || name == "token" || name == "auth_token" || name == "access_token" || name == "jwt" || name == "jwt_token" || strings.Contains(name, "session") || strings.Contains(name, "token") || strings.Contains(name, "auth") {
			return true
		}
	}
	return false
}

func anyRouterMergeSetCookiePairs(cookieHeader string, setCookieHeaders []string) string {
	merged := strings.TrimSpace(cookieHeader)
	for _, raw := range setCookieHeaders {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		firstPair := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
		parts := strings.SplitN(firstPair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		merged = anyRouterUpsertCookie(merged, strings.TrimSpace(parts[0]), parts[1])
	}
	return merged
}

func anyRouterUpsertCookie(cookieHeader string, name string, value string) string {
	parts := strings.Split(cookieHeader, ";")
	next := make([]string, 0, len(parts)+1)
	replaced := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := strings.SplitN(part, "=", 2)
		if len(segments) != 2 {
			next = append(next, part)
			continue
		}
		if strings.TrimSpace(segments[0]) == name {
			next = append(next, name+"="+value)
			replaced = true
			continue
		}
		next = append(next, part)
	}
	if !replaced {
		next = append(next, name+"="+value)
	}
	return strings.Join(next, "; ")
}

func anyRouterIsShieldChallenge(contentType string, text string) bool {
	lowered := strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(lowered, "text/html") && (strings.Contains(text, "var arg1=") || strings.Contains(text, "acw_sc__v2") || strings.Contains(text, "cdn_sec_tc") || strings.Contains(strings.ToLower(text), "<script")) {
		return true
	}
	return strings.Contains(text, "var arg1=")
}

func anyRouterExtractHTMLErrorSummary(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	lowered := strings.ToLower(trimmed)
	if !strings.Contains(lowered, "<html") && !strings.Contains(lowered, "<!doctype") {
		return ""
	}

	title := ""
	if match := anyRouterTitlePattern.FindStringSubmatch(trimmed); len(match) >= 2 {
		title = strings.TrimSpace(match[1])
		if pipe := strings.Index(title, "|"); pipe >= 0 {
			title = strings.TrimSpace(title[:pipe])
		}
	}
	if title == "" && strings.Contains(lowered, "cloudflare tunnel error") {
		title = "Cloudflare Tunnel error"
	}
	if title == "" {
		return ""
	}

	code := ""
	if match := anyRouterHTMLCodePattern.FindStringSubmatch(trimmed); len(match) >= 2 {
		code = strings.TrimSpace(match[1])
	} else if match := anyRouterLooseCodePattern.FindStringSubmatch(trimmed); len(match) >= 2 {
		code = strings.TrimSpace(match[1])
	}
	if code != "" {
		return fmt.Sprintf("%s (Error %s)", title, code)
	}
	return title
}

func anyRouterExtractLikelyUserIDs(token string) []int {
	sessionValues := make([]string, 0)
	seenSessions := make(map[string]struct{})
	sessionCookiePattern := regexp.MustCompile(`(?:^|;\s*)session=([^;]+)`)
	for _, candidate := range anyRouterBuildCookieCandidates(token) {
		match := sessionCookiePattern.FindStringSubmatch(candidate)
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value == "" {
			continue
		}
		if _, ok := seenSessions[value]; ok {
			continue
		}
		seenSessions[value] = struct{}{}
		sessionValues = append(sessionValues, value)
	}
	raw := strings.TrimSpace(token)
	if raw != "" && !strings.Contains(raw, "=") {
		raw = strings.TrimPrefix(raw, "Bearer ")
		raw = strings.TrimPrefix(raw, "bearer ")
		if _, ok := seenSessions[raw]; !ok {
			seenSessions[raw] = struct{}{}
			sessionValues = append(sessionValues, raw)
		}
	}

	ids := make([]int, 0)
	seen := make(map[int]struct{})
	appendID := func(value int) {
		if value <= 0 || value > 10_000_000 {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}

	for _, sessionValue := range sessionValues {
		decodedBuffer, ok := anyRouterDecodeBase64Buffer(sessionValue)
		if !ok {
			continue
		}

		payloadTexts := []string{string(decodedBuffer)}
		payloadBuffers := [][]byte{decodedBuffer}
		parts := strings.Split(string(decodedBuffer), "|")
		if len(parts) >= 2 {
			if middleBuffer, ok := anyRouterDecodeBase64Buffer(parts[1]); ok {
				payloadTexts = append(payloadTexts, string(middleBuffer))
				payloadBuffers = append(payloadBuffers, middleBuffer)
			}
		}

		for _, payload := range payloadTexts {
			for _, match := range anyRouterNumericUnderscoreRE.FindAllStringSubmatch(payload, -1) {
				if len(match) >= 2 {
					appendID(anyRouterParseInt(match[1]))
				}
			}
			for _, match := range anyRouterNumericKeywordRE.FindAllStringSubmatch(strings.ToLower(payload), -1) {
				if len(match) >= 2 {
					appendID(anyRouterParseInt(match[1]))
				}
			}
		}

		for _, payload := range payloadBuffers {
			for _, value := range anyRouterExtractGobFieldInts(payload, "id") {
				appendID(value)
			}
		}
	}

	return ids
}

func anyRouterExtractGobFieldInts(payload []byte, fieldName string) []int {
	marker := append(append([]byte(fieldName), 0x03), append([]byte("int"), 0x04)...)
	values := make([]int, 0)
	seen := make(map[int]struct{})

	for start := 0; start < len(payload); {
		position := bytes.Index(payload[start:], marker)
		if position < 0 {
			break
		}
		position += start
		if position+len(marker)+1 >= len(payload) {
			break
		}
		encodedLength := int(payload[position+len(marker)])
		delimiter := payload[position+len(marker)+1]
		start = position + len(marker)
		if delimiter != 0x00 {
			continue
		}
		byteLength := encodedLength - 1
		valueStart := position + len(marker) + 2
		valueEnd := valueStart + byteLength
		if byteLength <= 0 || valueEnd > len(payload) {
			continue
		}
		if value := anyRouterDecodeGobSignedInt(payload[valueStart:valueEnd]); value > 0 {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}
	}

	return values
}

func anyRouterDecodeGobSignedInt(encoded []byte) int {
	if len(encoded) == 0 || len(encoded) > 8 {
		return 0
	}

	var unsigned uint64
	if encoded[0] < 0x80 {
		unsigned = uint64(encoded[0])
	} else {
		width := int(0x100 - uint16(encoded[0]))
		if width <= 0 || len(encoded) != width+1 || width > 8 {
			return 0
		}
		for i := 1; i < len(encoded); i++ {
			unsigned = (unsigned << 8) | uint64(encoded[i])
		}
	}

	var signed int64
	if unsigned&1 == 0 {
		signed = int64(unsigned >> 1)
	} else {
		signed = -int64((unsigned >> 1) + 1)
	}
	if signed <= 0 || signed > 10_000_000 {
		return 0
	}
	return int(signed)
}

func anyRouterSolveAcwScV2(html string) string {
	arg1Match := anyRouterArg1Pattern.FindStringSubmatch(html)
	if len(arg1Match) < 2 {
		return ""
	}
	arg1 := strings.ToUpper(strings.TrimSpace(arg1Match[1]))
	if arg1 == "" {
		return ""
	}

	mappingMatch := anyRouterMappingPattern.FindStringSubmatch(html)
	if len(mappingMatch) < 2 {
		return ""
	}
	mappingParts := strings.Split(mappingMatch[1], ",")
	mapping := make([]int, 0, len(mappingParts))
	for _, part := range mappingParts {
		value, ok := anyRouterParseIntegerLiteral(strings.TrimSpace(part))
		if !ok {
			return ""
		}
		mapping = append(mapping, value)
	}

	arrayMatch := anyRouterEncodedArrayPattern.FindStringSubmatch(html)
	if len(arrayMatch) < 2 {
		return ""
	}
	rawMatches := anyRouterArrayStringPattern.FindAllStringSubmatch(arrayMatch[1], -1)
	if len(rawMatches) == 0 {
		return ""
	}
	encodedItems := make([]string, 0, len(rawMatches))
	for _, match := range rawMatches {
		if len(match) >= 2 {
			encodedItems = append(encodedItems, match[1])
		}
	}

	target := 0x760bf
	if targetMatch := anyRouterRotationTargetPattern.FindStringSubmatch(html); len(targetMatch) >= 2 {
		if parsed, ok := anyRouterParseIntegerLiteral(strings.TrimSpace(targetMatch[1])); ok {
			target = parsed
		}
	}

	baseOffset := 0xfb
	if baseMatch := anyRouterBaseOffsetPattern.FindStringSubmatch(html); len(baseMatch) >= 2 {
		if parsed, ok := anyRouterParseIntegerLiteral(strings.TrimSpace(baseMatch[1])); ok {
			baseOffset = parsed
		}
	}

	xorSeed := anyRouterResolveChallengeXorSeed(encodedItems, baseOffset, target)
	if xorSeed == "" {
		return ""
	}

	reordered := make([]byte, len(mapping))
	for index, ch := range arg1 {
		for mappedIndex, value := range mapping {
			if value == index+1 {
				reordered[mappedIndex] = byte(ch)
			}
		}
	}

	var builder strings.Builder
	for i := 0; i+1 < len(reordered) && i+1 < len(xorSeed); i += 2 {
		left, err := strconv.ParseUint(string(reordered[i:i+2]), 16, 8)
		if err != nil {
			return ""
		}
		right, err := strconv.ParseUint(xorSeed[i:i+2], 16, 8)
		if err != nil {
			return ""
		}
		builder.WriteString(fmt.Sprintf("%02x", byte(left)^byte(right)))
	}
	return builder.String()
}

func anyRouterResolveChallengeXorSeed(encodedItems []string, baseOffset int, target int) string {
	if len(encodedItems) == 0 {
		return ""
	}
	decodeAt := func(rotation int, index int) string {
		offset := index - baseOffset
		if offset < 0 || len(encodedItems) == 0 {
			return ""
		}
		raw := encodedItems[(offset+rotation)%len(encodedItems)]
		decoded, ok := anyRouterDecodeObfuscatedBase64String(raw)
		if !ok {
			return ""
		}
		return decoded
	}

	evaluate := func(rotation int) (int, bool) {
		parseAt := func(index int) (int, bool) {
			return anyRouterJSParseIntPrefix(decodeAt(rotation, index))
		}

		a, ok := parseAt(0x117)
		if !ok {
			return 0, false
		}
		b, ok := parseAt(0x111)
		if !ok {
			return 0, false
		}
		c, ok := parseAt(0xfb)
		if !ok {
			return 0, false
		}
		d, ok := parseAt(0x10e)
		if !ok {
			return 0, false
		}
		e, ok := parseAt(0x101)
		if !ok {
			return 0, false
		}
		f, ok := parseAt(0xfd)
		if !ok {
			return 0, false
		}
		g, ok := parseAt(0x102)
		if !ok {
			return 0, false
		}
		h, ok := parseAt(0x122)
		if !ok {
			return 0, false
		}
		i, ok := parseAt(0x112)
		if !ok {
			return 0, false
		}
		j, ok := parseAt(0x11d)
		if !ok {
			return 0, false
		}
		k, ok := parseAt(0x11c)
		if !ok {
			return 0, false
		}
		l, ok := parseAt(0x114)
		if !ok {
			return 0, false
		}

		value := -a/1*(b/2) + -c/3*(d/4) + -e/5*(-f/6) + -g/7*(h/8) + i/9 + j/10*(k/11) + l/12
		return value, true
	}

	for rotation := 0; rotation < len(encodedItems); rotation++ {
		value, ok := evaluate(rotation)
		if !ok || value != target {
			continue
		}
		seed := decodeAt(rotation, 0x115)
		if seed != "" {
			return strings.TrimSpace(seed)
		}
	}
	return ""
}

func anyRouterJSParseIntPrefix(value string) (int, bool) {
	trimmed := strings.TrimLeftFunc(strings.TrimSpace(value), unicode.IsSpace)
	if trimmed == "" {
		return 0, false
	}

	sign := 1
	switch trimmed[0] {
	case '+':
		trimmed = trimmed[1:]
	case '-':
		sign = -1
		trimmed = trimmed[1:]
	}

	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return 0, false
	}

	number, err := strconv.Atoi(trimmed[:digits])
	if err != nil {
		return 0, false
	}
	return sign * number, true
}

func anyRouterParseIntegerLiteral(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		base = 16
		value = value[2:]
	}
	parsed, err := strconv.ParseInt(value, base, 64)
	if err != nil {
		return 0, false
	}
	return int(parsed), true
}

func anyRouterDecodeBase64String(value string) (string, bool) {
	buf, ok := anyRouterDecodeBase64Buffer(value)
	if !ok {
		return "", false
	}
	return string(buf), true
}

func anyRouterDecodeObfuscatedBase64String(value string) (string, bool) {
	translated, ok := anyRouterTranslateObfuscatedBase64(value)
	if !ok {
		return "", false
	}
	buf, ok := anyRouterDecodeBase64Buffer(translated)
	if !ok {
		return "", false
	}
	return string(buf), true
}

func anyRouterTranslateObfuscatedBase64(value string) (string, bool) {
	const obfuscatedAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/="
	const standardAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="

	var builder strings.Builder
	builder.Grow(len(value))
	for _, ch := range value {
		index := strings.IndexRune(obfuscatedAlphabet, ch)
		if index < 0 {
			return "", false
		}
		builder.WriteByte(standardAlphabet[index])
	}
	return builder.String(), true
}

func anyRouterDecodeBase64Buffer(value string) ([]byte, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}

	candidates := []string{
		value,
		value + strings.Repeat("=", (4-len(value)%4)%4),
		strings.ReplaceAll(strings.ReplaceAll(value, "-", "+"), "_", "/"),
	}
	candidates = append(candidates, candidates[2]+strings.Repeat("=", (4-len(candidates[2])%4)%4))

	for _, candidate := range candidates {
		for _, encoding := range []*base64.Encoding{
			base64.StdEncoding,
			base64.RawStdEncoding,
			base64.URLEncoding,
			base64.RawURLEncoding,
		} {
			buf, err := encoding.DecodeString(candidate)
			if err == nil {
				return buf, true
			}
		}
	}
	return nil, false
}

func anyRouterGroupMapToSlice(items map[string]model.SiteUserGroup) []model.SiteUserGroup {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]model.SiteUserGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, items[key])
	}
	return result
}
