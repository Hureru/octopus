package sitesync

import (
	"context"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

const (
	siteBalanceQuotaPerUSD = 500000.0
)

func fetchSiteAccountBalance(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) (float64, float64) {
	if siteRecord == nil || account == nil {
		return 0, 0
	}
	switch siteRecord.Platform {
	case model.SitePlatformNewAPI,
		model.SitePlatformAnyRouter,
		model.SitePlatformOneAPI,
		model.SitePlatformOneHub:
		return fetchManagementQuotaBalance(ctx, siteRecord, account, accessToken, false)
	case model.SitePlatformDoneHub:
		return fetchManagementQuotaBalance(ctx, siteRecord, account, accessToken, true)
	case model.SitePlatformSub2API:
		return fetchSub2APIBalance(ctx, siteRecord, account, accessToken)
	default:
		return 0, 0
	}
}

func fetchManagementQuotaBalance(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, quotaIsRemaining bool) (float64, float64) {
	if strings.TrimSpace(accessToken) == "" {
		return 0, 0
	}
	payload, err := requestJSONWithManagedAccessToken(ctx, siteRecord, "GET", buildSiteURL(siteRecord.BaseURL, "/api/user/self"), nil, accessToken, account)
	if err != nil {
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
