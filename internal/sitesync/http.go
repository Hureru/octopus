package sitesync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
)

func siteHTTPClient(siteRecord *model.Site, accounts ...*model.SiteAccount) (*http.Client, error) {
	if siteRecord == nil {
		return nil, fmt.Errorf("site is nil")
	}
	useProxy, proxyURL := resolveSiteAccountProxy(siteRecord, accounts...)
	if !useProxy {
		return client.GetHTTPClientSystemProxy(false)
	}
	if proxyURL == nil || strings.TrimSpace(*proxyURL) == "" {
		return client.GetHTTPClientSystemProxy(true)
	}
	return client.GetHTTPClientCustomProxy(strings.TrimSpace(*proxyURL))
}

func resolveSiteAccountProxy(siteRecord *model.Site, accounts ...*model.SiteAccount) (bool, *string) {
	if len(accounts) > 0 && accounts[0] != nil && accounts[0].AccountProxy != nil {
		trimmed := strings.TrimSpace(*accounts[0].AccountProxy)
		if trimmed != "" {
			return true, &trimmed
		}
	}
	if siteRecord == nil {
		return false, nil
	}
	if siteRecord.Proxy {
		if siteRecord.SiteProxy == nil {
			return true, nil
		}
		trimmed := strings.TrimSpace(*siteRecord.SiteProxy)
		if trimmed == "" {
			return true, nil
		}
		return true, &trimmed
	}
	if siteRecord.UseSystemProxy {
		return true, nil
	}
	return false, nil
}

func requestJSON(ctx context.Context, siteRecord *model.Site, method string, requestURL string, body any, headers map[string]string, accounts ...*model.SiteAccount) (map[string]any, error) {
	httpClient, err := siteHTTPClient(siteRecord, accounts...)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		payload, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, marshalErr
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return nil, err
	}
	applyDefaultSiteRequestHeaders(req, body != nil)
	for _, item := range siteRecord.CustomHeader {
		if strings.TrimSpace(item.HeaderKey) != "" {
			req.Header.Set(strings.TrimSpace(item.HeaderKey), item.HeaderValue)
		}
	}
	for key, value := range headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, formatSiteHTTPError(resp.StatusCode, resp.Header.Get("Content-Type"), bodyBytes)
	}
	if len(bodyBytes) == 0 {
		return map[string]any{}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, formatSiteDecodeError(resp.Header.Get("Content-Type"), bodyBytes, err)
	}
	return payload, nil
}

func applyDefaultSiteRequestHeaders(req *http.Request, hasJSONBody bool) {
	if req == nil {
		return
	}
	if hasJSONBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", anyRouterUserAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/plain, */*")
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	}
}

func formatSiteHTTPError(statusCode int, contentType string, bodyBytes []byte) error {
	if payload, ok := parseSiteJSONMap(bodyBytes); ok {
		if message := extractSiteResponseMessage(payload); message != "" {
			return fmt.Errorf("http %d: %s", statusCode, message)
		}
	}
	if summary := extractSiteHTMLResponseSummary(contentType, bodyBytes); summary != "" {
		return fmt.Errorf("http %d: %s", statusCode, summary)
	}
	return fmt.Errorf("http %d: %s", statusCode, strings.TrimSpace(string(bodyBytes)))
}

func formatSiteDecodeError(contentType string, bodyBytes []byte, err error) error {
	if summary := extractSiteHTMLResponseSummary(contentType, bodyBytes); summary != "" {
		return fmt.Errorf("decode response failed: %s", summary)
	}
	return fmt.Errorf("decode response failed: %w", err)
}

func parseSiteJSONMap(bodyBytes []byte) (map[string]any, bool) {
	if len(bodyBytes) == 0 {
		return map[string]any{}, true
	}
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func extractSiteResponseMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	return firstNonEmptyString(
		jsonString(payload["message"]),
		jsonString(nestedValue(payload, "error", "message")),
		jsonString(payload["msg"]),
	)
}

func extractSiteHTMLResponseSummary(contentType string, bodyBytes []byte) string {
	body := strings.TrimSpace(string(bodyBytes))
	if body == "" {
		return ""
	}
	if summary := anyRouterExtractHTMLErrorSummary(body); summary != "" {
		return summary
	}
	lowered := strings.ToLower(contentType + "\n" + body)
	if strings.Contains(lowered, "just a moment") {
		return "Just a moment..."
	}
	if strings.Contains(lowered, "cloudflare") {
		return "Cloudflare challenge"
	}
	return ""
}
func buildSiteURL(baseURL string, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

func parseTokenItems(payload map[string]any) []map[string]any {
	for _, candidate := range []any{payload["data"], nestedValue(payload, "data", "items"), nestedValue(payload, "data", "data"), payload["items"], payload["list"], nestedValue(payload, "data", "list")} {
		if items := normalizeItemSlice(candidate); len(items) > 0 {
			return items
		}
	}
	return nil
}

func parseGroupItems(payload map[string]any) []model.SiteUserGroup {
	items := make([]model.SiteUserGroup, 0)
	for _, candidate := range []any{payload["data"], nestedValue(payload, "data", "groups"), payload["groups"], payload} {
		switch value := candidate.(type) {
		case map[string]any:
			for key := range value {
				lowered := strings.ToLower(strings.TrimSpace(key))
				if lowered == "" || lowered == "success" || lowered == "message" || lowered == "data" || lowered == "code" || lowered == "error" {
					continue
				}
				items = append(items, model.SiteUserGroup{GroupKey: key, Name: key})
			}
		case []any:
			for _, raw := range value {
				switch item := raw.(type) {
				case string:
					if strings.TrimSpace(item) != "" {
						items = append(items, model.SiteUserGroup{GroupKey: strings.TrimSpace(item), Name: strings.TrimSpace(item)})
					}
				case map[string]any:
					groupKey := firstNonEmptyString(jsonString(item["group_id"]), jsonString(item["groupId"]), jsonString(item["id"]), jsonString(item["value"]), jsonString(item["name"]), jsonString(item["group_name"]), jsonString(item["groupName"]), jsonString(item["title"]))
					groupName := firstNonEmptyString(jsonString(item["name"]), jsonString(item["group_name"]), jsonString(item["groupName"]), jsonString(item["title"]), groupKey)
					if strings.TrimSpace(groupKey) != "" {
						items = append(items, model.SiteUserGroup{GroupKey: strings.TrimSpace(groupKey), Name: strings.TrimSpace(groupName)})
					}
				}
			}
		}
		if len(items) > 0 {
			break
		}
	}
	deduped := make(map[string]model.SiteUserGroup)
	for _, item := range items {
		key := model.NormalizeSiteGroupKey(item.GroupKey)
		item.GroupKey = key
		item.Name = model.NormalizeSiteGroupName(key, item.Name)
		deduped[key] = item
	}
	keys := make([]string, 0, len(deduped))
	for key := range deduped {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]model.SiteUserGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, deduped[key])
	}
	return result
}

func normalizeItemSlice(value any) []map[string]any {
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item, ok := raw.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

func normalizeModelNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
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
	slices.Sort(result)
	return result
}

func parseEnabledFlag(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return int(typed) != 0
	case int:
		return typed != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "", "enabled", "active", "1", "true", "on":
			return true
		case "disabled", "inactive", "0", "false", "off":
			return false
		default:
			return true
		}
	default:
		return true
	}
}

func ensureBearer(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}

func requestJSONWithManagedAccessToken(ctx context.Context, siteRecord *model.Site, method string, requestURL string, body any, accessToken string, accounts ...*model.SiteAccount) (map[string]any, error) {
	initialHeaders := managedUserIDHeaders(firstManagedPlatformUserID(accounts...))
	payload, err := requestJSONWithManagedHeaders(ctx, siteRecord, method, requestURL, body, accessToken, initialHeaders, accounts...)
	if err == nil || !siteRequiresManagedUserIDHeader(siteRecord) || !shouldRetryManagedRequestWithUserID(err) {
		return payload, err
	}

	userID, discoverErr := discoverManagedUserID(ctx, siteRecord, accessToken, accounts...)
	if discoverErr != nil {
		return nil, discoverErr
	}
	if userID <= 0 {
		return nil, err
	}
	rememberManagedPlatformUserID(userID, accounts...)

	userHeaders := managedUserIDHeaders(userID)
	return requestJSONWithManagedHeaders(ctx, siteRecord, method, requestURL, body, accessToken, userHeaders, accounts...)
}

func requestJSONWithManagedHeaders(ctx context.Context, siteRecord *model.Site, method string, requestURL string, body any, accessToken string, extraHeaders map[string]string, accounts ...*model.SiteAccount) (map[string]any, error) {
	var firstErr error
	for _, headers := range buildManagedAuthHeaders(accessToken) {
		payload, err := requestJSON(ctx, siteRecord, method, requestURL, body, mergeHeaders(headers, extraHeaders), accounts...)
		if err == nil {
			return payload, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if !shouldTryAlternativeManagedAuth(err) {
			return nil, err
		}
	}
	return nil, firstErr
}

func buildManagedAuthHeaders(accessToken string) []map[string]string {
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return []map[string]string{{}}
	}

	candidates := make([]map[string]string, 0, 2)
	if looksLikeCookieToken(token) {
		candidates = append(candidates, map[string]string{"Cookie": token})
	}
	candidates = append(candidates, map[string]string{"Authorization": ensureBearer(token)})
	return candidates
}

func looksLikeCookieToken(token string) bool {
	trimmed := strings.TrimSpace(token)
	lowered := strings.ToLower(trimmed)
	if trimmed == "" || strings.HasPrefix(lowered, "bearer ") {
		return false
	}
	if strings.Contains(trimmed, ";") {
		return true
	}
	return strings.Contains(trimmed, "=") && !strings.Contains(trimmed, " ")
}

func shouldTryAlternativeManagedAuth(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "http 400") || strings.Contains(message, "http 401") || strings.Contains(message, "http 403")
}

func siteRequiresManagedUserIDHeader(siteRecord *model.Site) bool {
	return siteRecord != nil && siteRecord.Platform == model.SitePlatformNewAPI
}

func shouldRetryManagedRequestWithUserID(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "new-api-user") ||
		strings.Contains(message, "missing user id") ||
		strings.Contains(message, "requires user id") ||
		strings.Contains(message, "invalid user id") ||
		strings.Contains(message, "wrong user id") ||
		strings.Contains(message, "未提供")
}

func discoverManagedUserID(ctx context.Context, siteRecord *model.Site, accessToken string, accounts ...*model.SiteAccount) (int, error) {
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/user/self")

	payload, err := requestJSONWithManagedHeaders(ctx, siteRecord, http.MethodGet, requestURL, nil, accessToken, nil, accounts...)
	if err == nil {
		if userID := anyRouterExtractUserID(payload); userID > 0 {
			return userID, nil
		}
	}

	var firstErr error
	if err != nil {
		firstErr = err
	}

	for _, userID := range anyRouterBuildUserIDProbeCandidates(accessToken) {
		userHeaders := map[string]string{}
		anyRouterAddUserIDHeaders(userHeaders, userID)
		payload, probeErr := requestJSONWithManagedHeaders(ctx, siteRecord, http.MethodGet, requestURL, nil, accessToken, userHeaders, accounts...)
		if probeErr != nil {
			if firstErr == nil {
				firstErr = probeErr
			}
			continue
		}
		if anyRouterExtractUserID(payload) > 0 {
			return userID, nil
		}
	}

	return 0, firstErr
}

func firstManagedPlatformUserID(accounts ...*model.SiteAccount) int {
	for _, account := range accounts {
		if account != nil && account.PlatformUserID != nil && *account.PlatformUserID > 0 {
			return *account.PlatformUserID
		}
	}
	return 0
}

func rememberManagedPlatformUserID(userID int, accounts ...*model.SiteAccount) {
	if userID <= 0 {
		return
	}
	for _, account := range accounts {
		if account == nil {
			continue
		}
		if account.PlatformUserID == nil || *account.PlatformUserID != userID {
			resolvedUserID := userID
			account.PlatformUserID = &resolvedUserID
		}
	}
}

func managedUserIDHeaders(userID int) map[string]string {
	if userID <= 0 {
		return nil
	}
	headers := map[string]string{}
	anyRouterAddUserIDHeaders(headers, userID)
	return headers
}

func mergeHeaders(base map[string]string, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func jsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	case int:
		return strings.TrimSpace(fmt.Sprintf("%d", typed))
	default:
		return ""
	}
}

func jsonBool(value any) bool {
	typed, ok := value.(bool)
	if ok {
		return typed
	}
	return false
}

func nestedValue(payload map[string]any, keys ...string) any {
	var current any = payload
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[key]
	}
	return current
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func marshalRawPayload(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(payload)
}
