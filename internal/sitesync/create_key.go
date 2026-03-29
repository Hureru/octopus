package sitesync

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func CreateAccountToken(ctx context.Context, accountID int, req model.SiteChannelKeyCreateRequest) (*model.SiteSyncResult, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if siteRecord == nil || account == nil {
		return nil, fmt.Errorf("site account not found")
	}

	groupKey := model.NormalizeSiteGroupKey(req.GroupKey)
	name := strings.TrimSpace(req.Name)

	switch siteRecord.Platform {
	case model.SitePlatformAnyRouter:
		if err := createAnyRouterToken(ctx, siteRecord, account, groupKey, name); err != nil {
			return nil, err
		}
	case model.SitePlatformNewAPI, model.SitePlatformOneAPI, model.SitePlatformOneHub, model.SitePlatformDoneHub:
		if err := createManagementPlatformToken(ctx, siteRecord, account, groupKey, name); err != nil {
			return nil, err
		}
	case model.SitePlatformSub2API:
		if err := createSub2APIToken(ctx, siteRecord, account, groupKey, name); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("site platform %s does not support quick key creation", siteRecord.Platform)
	}

	return SyncAccount(ctx, accountID)
}

func createManagementPlatformToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, groupKey string, name string) error {
	if account == nil {
		return fmt.Errorf("site account is nil")
	}
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return fmt.Errorf("API key credential account does not support quick site key creation")
	}

	accessToken, err := resolveManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return err
	}

	payload, err := requestJSONWithManagedAccessToken(
		ctx,
		siteRecord,
		http.MethodPost,
		buildSiteURL(siteRecord.BaseURL, "/api/token/"),
		buildManagedTokenCreatePayload(account, groupKey, name),
		accessToken,
		account,
	)
	if err != nil {
		return err
	}
	if !siteTokenCreateSucceeded(payload) {
		return fmt.Errorf("%s", firstNonEmptyString(extractSiteResponseMessage(payload), "site token creation failed"))
	}
	return nil
}

func createAnyRouterToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, groupKey string, name string) error {
	if account == nil {
		return fmt.Errorf("site account is nil")
	}
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return fmt.Errorf("API key credential account does not support quick site key creation")
	}

	accessToken, err := resolveAnyRouterManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return err
	}

	payloadBody := buildManagedTokenCreatePayload(account, groupKey, name)
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/token/")

	userID, _ := anyRouterDiscoverUserID(ctx, siteRecord, account, accessToken)
	payload, _, err := anyRouterRequestJSONWithCookies(
		ctx,
		siteRecord,
		http.MethodPost,
		requestURL,
		payloadBody,
		anyRouterAuthHeaders(accessToken, userID),
		account,
	)
	if err == nil && siteTokenCreateSucceeded(payload) {
		return nil
	}

	tryUserIDs := []int{userID}
	if alternateUserID, probeErr := anyRouterProbeAlternateUserIDByCookie(ctx, siteRecord, account, accessToken, userID); probeErr == nil && alternateUserID > 0 {
		tryUserIDs = append(tryUserIDs, alternateUserID)
	}
	if userID <= 0 {
		if probedUserID, probeErr := anyRouterProbeUserIDByCookie(ctx, siteRecord, account, accessToken); probeErr == nil && probedUserID > 0 {
			tryUserIDs = append(tryUserIDs, probedUserID)
		}
	}
	tryUserIDs = slicesCompactInts(tryUserIDs)

	for _, candidateUserID := range tryUserIDs {
		for _, cookie := range anyRouterBuildCookieCandidates(accessToken) {
			headers := map[string]string{"Cookie": cookie}
			anyRouterAddUserIDHeaders(headers, candidateUserID)
			payload, _, requestErr := anyRouterRequestJSONWithCookies(
				ctx,
				siteRecord,
				http.MethodPost,
				requestURL,
				payloadBody,
				headers,
				account,
			)
			if requestErr != nil {
				if err == nil {
					err = requestErr
				}
				continue
			}
			if siteTokenCreateSucceeded(payload) {
				return nil
			}
			if message := strings.TrimSpace(extractSiteResponseMessage(payload)); message != "" {
				err = fmt.Errorf("%s", message)
			}
		}
	}

	if err != nil {
		return err
	}
	return fmt.Errorf("site token creation failed")
}

func createSub2APIToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, groupKey string, name string) error {
	if account == nil {
		return fmt.Errorf("site account is nil")
	}
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return fmt.Errorf("API key credential account does not support quick site key creation")
	}

	accessToken := strings.TrimSpace(account.AccessToken)
	accessToken, err := ensureFreshSub2APIAccessToken(ctx, siteRecord, account, false)
	if err != nil {
		return err
	}

	requestBody := buildSub2APITokenCreatePayload(account, groupKey, name)
	headers := map[string]string{"Authorization": ensureBearer(accessToken)}
	endpoints := []string{"/api/v1/keys", "/api/v1/api-keys"}
	var firstErr error

	for _, endpoint := range endpoints {
		payload, err := requestJSON(
			ctx,
			siteRecord,
			http.MethodPost,
			buildSiteURL(siteRecord.BaseURL, endpoint),
			requestBody,
			headers,
			account,
		)
		if err != nil {
			if shouldRetrySub2APIAfterRefresh(err, account) {
				refreshedToken, refreshErr := ensureFreshSub2APIAccessToken(ctx, siteRecord, account, true)
				if refreshErr == nil && stripBearerPrefix(refreshedToken) != stripBearerPrefix(accessToken) {
					headers = map[string]string{"Authorization": ensureBearer(refreshedToken)}
					payload, err = requestJSON(
						ctx,
						siteRecord,
						http.MethodPost,
						buildSiteURL(siteRecord.BaseURL, endpoint),
						requestBody,
						headers,
						account,
					)
					if err == nil && siteTokenCreateSucceeded(payload) {
						return nil
					}
				}
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if siteTokenCreateSucceeded(payload) {
			return nil
		}
		return fmt.Errorf("%s", firstNonEmptyString(extractSiteResponseMessage(payload), "site token creation failed"))
	}

	if firstErr != nil {
		return firstErr
	}
	return fmt.Errorf("site token creation failed")
}

func buildManagedTokenCreatePayload(account *model.SiteAccount, groupKey string, name string) map[string]any {
	return map[string]any{
		"name":                 defaultSiteTokenCreateName(account, groupKey, name),
		"unlimited_quota":      true,
		"expired_time":         -1,
		"remain_quota":         0,
		"allow_ips":            "",
		"model_limits_enabled": false,
		"model_limits":         "",
		"group":                model.NormalizeSiteGroupKey(groupKey),
	}
}

func buildSub2APITokenCreatePayload(account *model.SiteAccount, groupKey string, name string) map[string]any {
	payload := map[string]any{
		"name": defaultSiteTokenCreateName(account, groupKey, name),
	}
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	if groupID, err := strconv.Atoi(groupKey); err == nil && groupID > 0 {
		payload["group_id"] = groupID
	}
	return payload
}

func defaultSiteTokenCreateName(account *model.SiteAccount, groupKey string, name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}

	groupPart := strings.TrimSpace(groupKey)
	groupPart = strings.NewReplacer("/", "-", "\\", "-", " ", "-", "\t", "-", "\n", "-").Replace(groupPart)
	groupPart = strings.Trim(groupPart, "-")
	if groupPart == "" {
		groupPart = model.SiteDefaultGroupKey
	}
	return fmt.Sprintf("octopus-%s-%d", groupPart, time.Now().Unix())
}

func siteTokenCreateSucceeded(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if raw, ok := payload["success"]; ok {
		switch typed := raw.(type) {
		case bool:
			return typed
		case float64:
			return typed != 0
		case int:
			return typed != 0
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "1", "true", "ok", "success":
				return true
			case "0", "false", "fail", "failed", "error":
				return false
			}
		}
		return false
	}
	return true
}

func slicesCompactInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value < 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
