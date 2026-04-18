package sitesync

import (
	"context"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

const (
	siteBalanceQuotaPerUSD = 500000.0
)

func fetchSiteAccountBalance(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int) (float64, float64) {
	if siteRecord == nil || account == nil {
		return 0, 0
	}
	switch siteRecord.Platform {
	case model.SitePlatformNewAPI,
		model.SitePlatformAnyRouter,
		model.SitePlatformOneAPI,
		model.SitePlatformOneHub:
		return fetchManagementQuotaBalance(ctx, siteRecord, account, accessToken, userID, false)
	case model.SitePlatformDoneHub:
		return fetchManagementQuotaBalance(ctx, siteRecord, account, accessToken, userID, true)
	case model.SitePlatformSub2API:
		return fetchSub2APIBalance(ctx, siteRecord, account, accessToken)
	default:
		return 0, 0
	}
}

func fetchManagementQuotaBalance(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, userID int, quotaIsRemaining bool) (float64, float64) {
	if strings.TrimSpace(accessToken) == "" {
		return 0, 0
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

	// When the caller passed in a trusted userID, trust attempt 1. Only probe if we had to discover userID ourselves.
	if !knownUserID && !isValidUserSelfPayload(payload, err) {
		cookiePayload, _, cookieErr := anyRouterFetchUserSelfByCookie(ctx, siteRecord, account, accessToken, userID)
		if isValidUserSelfPayload(cookiePayload, cookieErr) {
			payload = cookiePayload
			err = nil
		} else if userID > 0 {
			if alt, _ := anyRouterProbeAlternateUserIDByCookie(ctx, siteRecord, account, accessToken, userID); alt > 0 {
				altPayload, _, altErr := anyRouterRequestJSONWithCookies(ctx, siteRecord, http.MethodGet, requestURL, nil,
					anyRouterAuthHeaders(accessToken, alt), account)
				if isValidUserSelfPayload(altPayload, altErr) {
					payload = altPayload
					err = nil
					rememberManagedPlatformUserID(alt, account)
				}
			}
		}
	}

	if err != nil || payload == nil {
		return 0, 0
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = payload
	}
	quota := jsonFloat(data["quota"])
	used := jsonFloat(data["used_quota"])

	if quotaIsRemaining {
		return quota / siteBalanceQuotaPerUSD, used / siteBalanceQuotaPerUSD
	}
	remaining := quota - used
	if remaining < 0 {
		remaining = 0
	}
	return remaining / siteBalanceQuotaPerUSD, used / siteBalanceQuotaPerUSD
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
