package sitesync

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

const (
	siteBalanceQuotaPerUSD = 500000.0
	logFallbackPageSize    = 100
	logFallbackMaxPages    = 6
)

var (
	logIncomeTypes           = []int{1, 4}
	logIncomeContentNumberRE = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)
)

func fetchSiteAccountBalance(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (float64, float64, float64) {
	if siteRecord == nil || account == nil {
		return 0, 0, 0
	}
	switch siteRecord.Platform {
	case model.SitePlatformOneAPI,
		model.SitePlatformOneHub:
		return fetchManagementQuotaBalance(ctx, siteRecord, account, accessToken, userID, false)
	case model.SitePlatformNewAPI,
		model.SitePlatformAnyRouter,
		model.SitePlatformDoneHub:
		return fetchManagementQuotaBalance(ctx, siteRecord, account, accessToken, userID, true)
	case model.SitePlatformSub2API:
		balance, used := fetchSub2APIBalance(ctx, siteRecord, account, accessToken)
		return balance, used, 0
	default:
		return 0, 0, 0
	}
}

func fetchManagementQuotaBalance(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int, quotaIsRemaining bool) (float64, float64, float64) {
	if strings.TrimSpace(accessToken) == "" {
		return 0, 0, 0
	}
	knownUserID := userID > 0
	if !knownUserID {
		if discovered, _ := anyRouterDiscoverUserID(ctx, siteRecord, account, accessToken); discovered > 0 {
			userID = discovered
			rememberManagedPlatformUserID(userID, account)
		}
	}

	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/user/self")

	payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil,
		anyRouterAuthHeaders(accessToken, userID), account)

	// Attempt 2: cookie-based fallback with the same userID. AnyRouter often stores the access_token
	// as a raw session cookie value, so `Authorization: Bearer <cookie>` fails and only cookie auth works.
	// This runs even when knownUserID=true because the userID stays stable and cookie auth is how
	// shielded deployments expect requests to arrive.
	if !isValidUserSelfPayload(payload, err) {
		cookiePayload, _, cookieErr := anyRouterFetchUserSelfByCookie(ctx, siteRecord, account, accessToken, userID)
		if isValidUserSelfPayload(cookiePayload, cookieErr) {
			payload = cookiePayload
			err = nil
		} else if userID > 0 {
			// Attempt 3: probe for an alternate userID (e.g., the real gob-encoded user inside the
			// session cookie) when the passed-in userID doesn't match reality. Safe for multi-account:
			// anyRouterProbeAlternateUserIDByCookie returns 0 when the probed ID matches the current
			// one, and it only returns IDs that genuinely validate against the session.
			if alt, _ := anyRouterProbeAlternateUserIDByCookie(ctx, siteRecord, account, accessToken, userID); alt > 0 {
				altPayload, _, altErr := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil,
					anyRouterAuthHeaders(accessToken, alt), account)
				if isValidUserSelfPayload(altPayload, altErr) {
					payload = altPayload
					err = nil
					rememberManagedPlatformUserID(alt, account)
				} else {
					altCookiePayload, _, altCookieErr := anyRouterFetchUserSelfByCookie(ctx, siteRecord, account, accessToken, alt)
					if isValidUserSelfPayload(altCookiePayload, altCookieErr) {
						payload = altCookiePayload
						err = nil
						rememberManagedPlatformUserID(alt, account)
					}
				}
			}
		}
	}

	if err != nil || payload == nil {
		return 0, 0, 0
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = payload
	}
	quota := jsonFloat(data["quota"])
	used := jsonFloat(data["used_quota"])

	var balance, balanceUsed float64
	if quotaIsRemaining {
		balance = quota / siteBalanceQuotaPerUSD
		balanceUsed = used / siteBalanceQuotaPerUSD
	} else {
		remaining := quota - used
		if remaining < 0 {
			remaining = 0
		}
		balance = remaining / siteBalanceQuotaPerUSD
		balanceUsed = used / siteBalanceQuotaPerUSD
	}

	todayIncomeRaw, todayIncomeKnown := data["today_income"]
	todayIncome := 0.0
	if todayIncomeKnown {
		todayIncome = jsonFloat(todayIncomeRaw) / siteBalanceQuotaPerUSD
	}

	if !todayIncomeKnown && supportsTodayIncomeLogFallback(siteRecord.Platform) {
		if fallback, ok := fetchTodayIncomeFromLogs(ctx, siteRecord, account, accessToken, userID); ok {
			todayIncome = fallback
		}
	}

	return balance, balanceUsed, todayIncome
}

func supportsTodayIncomeLogFallback(platform model.SitePlatform) bool {
	switch platform {
	case model.SitePlatformNewAPI,
		model.SitePlatformAnyRouter,
		model.SitePlatformOneAPI:
		return true
	default:
		return false
	}
}

func fetchTodayIncomeFromLogs(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (float64, bool) {
	if strings.TrimSpace(accessToken) == "" {
		return 0, false
	}

	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999_999_999, now.Location())
	startTs := startOfDay.Unix()
	endTs := endOfDay.Unix()

	baseURL := buildSiteURL(siteRecord.BaseURL, "/api/log/self")
	headers := anyRouterAuthHeaders(accessToken, userID)

	var total float64
	anyResponse := false

	for _, logType := range logIncomeTypes {
		for page := 1; page <= logFallbackMaxPages; page++ {
			requestURL := fmt.Sprintf("%s?p=%d&page_size=%d&type=%d&token_name=&model_name=&start_timestamp=%d&end_timestamp=%d&group=",
				baseURL, page, logFallbackPageSize, logType, startTs, endTs)

			payload, _, err := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil,
				headers, account)
			if err != nil || payload == nil {
				break
			}
			anyResponse = true

			items := extractLogItems(payload)
			for _, item := range items {
				quotaRaw := jsonFloat(item["quota"])
				if quotaRaw > 0 {
					total += quotaRaw / siteBalanceQuotaPerUSD
					continue
				}
				total += parseIncomeFromLogContent(jsonString(item["content"]))
			}

			if len(items) == 0 {
				break
			}
			if totalCount, ok := extractLogTotalCount(payload); ok && page*logFallbackPageSize >= totalCount {
				break
			}
		}
	}

	if !anyResponse {
		return 0, false
	}
	return math.Round(total*1_000_000) / 1_000_000, true
}

func extractLogItems(payload map[string]any) []map[string]any {
	if payload == nil {
		return nil
	}
	for _, candidate := range []any{
		nestedValue(payload, "data", "items"),
		payload["items"],
		payload["data"],
	} {
		arr, ok := candidate.([]any)
		if !ok {
			continue
		}
		items := make([]map[string]any, 0, len(arr))
		for _, entry := range arr {
			if m, ok := entry.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return items
	}
	return nil
}

func extractLogTotalCount(payload map[string]any) (int, bool) {
	if payload == nil {
		return 0, false
	}
	for _, candidate := range []any{
		nestedValue(payload, "data", "total"),
		payload["total"],
	} {
		switch v := candidate.(type) {
		case float64:
			if v >= 0 {
				return int(v), true
			}
		case int:
			if v >= 0 {
				return v, true
			}
		case int64:
			if v >= 0 {
				return int(v), true
			}
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				continue
			}
			parsed, err := strconv.Atoi(trimmed)
			if err == nil && parsed >= 0 {
				return parsed, true
			}
		}
	}
	return 0, false
}

func parseIncomeFromLogContent(content string) float64 {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return 0
	}
	normalized := strings.ReplaceAll(trimmed, ",", "")
	match := logIncomeContentNumberRE.FindString(normalized)
	if match == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}
	if parsed <= 0 {
		return 0
	}
	return parsed
}

func isValidUserSelfPayload(payload map[string]any, err error) bool {
	if err != nil || payload == nil {
		return false
	}
	if _, ok := payload["success"]; ok {
		if !jsonBool(payload["success"]) {
			return false
		}
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return false
	}
	if _, hasQuota := data["quota"]; hasQuota {
		return true
	}
	if _, hasUsed := data["used_quota"]; hasUsed {
		return true
	}
	return false
}

func fetchSub2APIBalance(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) (float64, float64) {
	token := stripBearerPrefix(accessToken)
	if token == "" {
		return 0, 0
	}
	payload, err := requestJSON(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/v1/auth/me"), nil, map[string]string{"Authorization": ensureBearer(token)}, account)
	if err != nil {
		return 0, 0
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = payload
	}
	return jsonFloat(data["balance"]), 0
}
