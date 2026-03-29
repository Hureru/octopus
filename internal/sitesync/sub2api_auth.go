package sitesync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

const sub2APIAccessTokenRefreshLead = 5 * time.Minute

type sub2APIRefreshedCredentials struct {
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt int64
}

func ensureFreshSub2APIAccessToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, forceRefresh bool) (string, error) {
	if account == nil {
		return "", fmt.Errorf("site account is nil")
	}

	accessToken := stripBearerPrefix(account.AccessToken)
	if accessToken == "" {
		return "", fmt.Errorf("access token is required")
	}
	if !forceRefresh && !shouldProactivelyRefreshSub2API(account) {
		return accessToken, nil
	}
	if strings.TrimSpace(account.RefreshToken) == "" {
		return accessToken, nil
	}

	refreshed, err := refreshSub2APIManagedSession(ctx, siteRecord, account, accessToken)
	if err != nil {
		if forceRefresh {
			return "", err
		}
		return accessToken, nil
	}
	return refreshed, nil
}

func shouldProactivelyRefreshSub2API(account *model.SiteAccount) bool {
	if account == nil {
		return false
	}
	if strings.TrimSpace(account.RefreshToken) == "" {
		return false
	}
	if account.TokenExpiresAt <= 0 {
		return false
	}
	return time.Until(time.UnixMilli(account.TokenExpiresAt)) <= sub2APIAccessTokenRefreshLead
}

func shouldRetrySub2APIAfterRefresh(err error, account *model.SiteAccount) bool {
	if err == nil || account == nil || strings.TrimSpace(account.RefreshToken) == "" {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "" {
		return false
	}
	return strings.Contains(text, "http 401") ||
		strings.Contains(text, "http 403") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "forbidden") ||
		strings.Contains(text, "expired") ||
		strings.Contains(text, "invalid token") ||
		strings.Contains(text, "access token")
}

func refreshSub2APIManagedSession(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, currentAccessToken string) (string, error) {
	if siteRecord == nil || account == nil {
		return "", fmt.Errorf("site or account is nil")
	}
	refreshToken := strings.TrimSpace(account.RefreshToken)
	if refreshToken == "" {
		return "", fmt.Errorf("sub2api managed refresh token missing")
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if currentAccessToken = stripBearerPrefix(currentAccessToken); currentAccessToken != "" {
		headers["Authorization"] = ensureBearer(currentAccessToken)
	}

	payload, err := requestJSON(
		ctx,
		siteRecord,
		"POST",
		buildSiteURL(siteRecord.BaseURL, "/api/v1/auth/refresh"),
		map[string]any{"refresh_token": refreshToken},
		headers,
		account,
	)
	if err != nil {
		return "", fmt.Errorf("sub2api token refresh request failed: %w", err)
	}

	refreshed, ok := parseSub2APIRefreshPayload(payload)
	if !ok {
		return "", fmt.Errorf("sub2api token refresh failed")
	}

	account.AccessToken = refreshed.AccessToken
	account.RefreshToken = refreshed.RefreshToken
	account.TokenExpiresAt = refreshed.TokenExpiresAt

	if account.ID > 0 {
		if err := db.GetDB().WithContext(ctx).
			Model(&model.SiteAccount{}).
			Where("id = ?", account.ID).
			Updates(map[string]any{
				"access_token":     refreshed.AccessToken,
				"refresh_token":    refreshed.RefreshToken,
				"token_expires_at": refreshed.TokenExpiresAt,
			}).Error; err != nil {
			return "", fmt.Errorf("failed to persist sub2api refreshed session: %w", err)
		}
	}

	return refreshed.AccessToken, nil
}

func parseSub2APIRefreshPayload(payload map[string]any) (sub2APIRefreshedCredentials, bool) {
	if payload == nil {
		return sub2APIRefreshedCredentials{}, false
	}

	code := anyToInt64(payload["code"])
	if code != 0 {
		return sub2APIRefreshedCredentials{}, false
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		return sub2APIRefreshedCredentials{}, false
	}

	accessToken := stripBearerPrefix(jsonString(data["access_token"]))
	refreshToken := strings.TrimSpace(jsonString(data["refresh_token"]))
	expiresInSeconds := anyToInt64(data["expires_in"])
	if accessToken == "" || refreshToken == "" || expiresInSeconds <= 0 {
		return sub2APIRefreshedCredentials{}, false
	}

	return sub2APIRefreshedCredentials{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		TokenExpiresAt: time.Now().Add(time.Duration(expiresInSeconds) * time.Second).UnixMilli(),
	}, true
}

func anyToInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		var parsed int64
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}
