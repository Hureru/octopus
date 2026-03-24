package sitesync

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func syncAccountState(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*syncSnapshot, error) {
	if siteRecord == nil || account == nil {
		return nil, fmt.Errorf("site or account is nil")
	}
	switch siteRecord.Platform {
	case model.SitePlatformAnyRouter:
		return syncAnyRouter(ctx, siteRecord, account)
	case model.SitePlatformNewAPI, model.SitePlatformOneAPI, model.SitePlatformOneHub, model.SitePlatformDoneHub:
		return syncManagementPlatform(ctx, siteRecord, account)
	case model.SitePlatformSub2API:
		return syncSub2API(ctx, siteRecord, account)
	case model.SitePlatformOpenAI, model.SitePlatformClaude, model.SitePlatformGemini:
		return syncOfficialPlatform(ctx, siteRecord, account)
	default:
		return nil, fmt.Errorf("unsupported site platform: %s", siteRecord.Platform)
	}
}

func checkinAccountState(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*model.SiteCheckinResult, string, error) {
	if siteRecord == nil || account == nil {
		return nil, "", fmt.Errorf("site or account is nil")
	}
	switch siteRecord.Platform {
	case model.SitePlatformDoneHub, model.SitePlatformSub2API, model.SitePlatformOpenAI, model.SitePlatformClaude, model.SitePlatformGemini:
		return &model.SiteCheckinResult{Status: model.SiteExecutionStatusSkipped, Message: "checkin is not supported by this platform"}, "", nil
	case model.SitePlatformAnyRouter:
		return checkinAnyRouter(ctx, siteRecord, account)
	case model.SitePlatformNewAPI, model.SitePlatformOneAPI, model.SitePlatformOneHub:
		accessToken, err := resolveManagedAccessToken(ctx, siteRecord, account)
		if err != nil {
			return nil, accessToken, err
		}
		payload, err := requestJSON(ctx, siteRecord, http.MethodPost, buildSiteURL(siteRecord.BaseURL, "/api/user/checkin"), nil, map[string]string{"Authorization": "Bearer " + accessToken})
		if err != nil {
			lowered := strings.ToLower(err.Error())
			if strings.Contains(lowered, "404") || strings.Contains(lowered, "not found") {
				return &model.SiteCheckinResult{Status: model.SiteExecutionStatusSkipped, Message: "checkin is not supported by this platform"}, accessToken, nil
			}
			return nil, accessToken, err
		}
		success := jsonBool(payload["success"])
		message := firstNonEmptyString(jsonString(payload["message"]), "checkin success")
		lowered := strings.ToLower(message)
		if success || strings.Contains(lowered, "already") || strings.Contains(message, "已签到") {
			return &model.SiteCheckinResult{Status: model.SiteExecutionStatusSuccess, Message: message, Reward: jsonString(nestedValue(payload, "data", "reward"))}, accessToken, nil
		}
		return &model.SiteCheckinResult{Status: model.SiteExecutionStatusFailed, Message: message}, accessToken, nil
	default:
		return nil, "", fmt.Errorf("unsupported site platform: %s", siteRecord.Platform)
	}
}

func syncManagementPlatform(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*syncSnapshot, error) {
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return syncWithDirectToken(ctx, siteRecord, account, resolveDirectToken(account), "manual")
	}

	accessToken, err := resolveManagedAccessToken(ctx, siteRecord, account)
	if err != nil {
		return nil, err
	}

	tokens, err := fetchManagementTokens(ctx, siteRecord, accessToken)
	if err != nil {
		return nil, err
	}
	groups, err := fetchManagementGroups(ctx, siteRecord, accessToken)
	if err != nil {
		groups = nil
	}
	if len(tokens) == 0 && strings.TrimSpace(account.APIKey) != "" {
		tokens = append(tokens, model.SiteToken{Name: "default", Token: strings.TrimSpace(account.APIKey), GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true, Source: "fallback", IsDefault: true})
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no usable site token found")
	}

	groups = mergeSiteGroups(groups, tokens)
	models, err := fetchModelsForSiteToken(ctx, siteRecord, pickModelToken(tokens))
	if err != nil {
		return nil, err
	}
	return &syncSnapshot{accessToken: accessToken, groups: groups, tokens: tokens, models: buildSiteModels(models, "sync"), message: "site account synced"}, nil
}

func syncSub2API(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*syncSnapshot, error) {
	if account.CredentialType == model.SiteCredentialTypeUsernamePassword {
		return nil, fmt.Errorf("sub2api does not support username/password login")
	}
	if account.CredentialType == model.SiteCredentialTypeAPIKey {
		return syncWithDirectToken(ctx, siteRecord, account, resolveDirectToken(account), "manual")
	}

	accessToken := strings.TrimSpace(account.AccessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	tokens, err := fetchSub2APITokens(ctx, siteRecord, accessToken)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 && strings.TrimSpace(account.APIKey) != "" {
		tokens = append(tokens, model.SiteToken{Name: "default", Token: strings.TrimSpace(account.APIKey), GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true, Source: "fallback", IsDefault: true})
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no usable site token found")
	}

	groups, err := fetchSub2APIGroups(ctx, siteRecord, accessToken, tokens)
	if err != nil {
		groups = nil
	}
	groups = mergeSiteGroups(groups, tokens)
	models, err := fetchModelsForSiteToken(ctx, siteRecord, pickModelToken(tokens))
	if err != nil {
		return nil, err
	}
	return &syncSnapshot{accessToken: accessToken, groups: groups, tokens: tokens, models: buildSiteModels(models, "sync"), message: "site account synced"}, nil
}

func syncOfficialPlatform(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*syncSnapshot, error) {
	return syncWithDirectToken(ctx, siteRecord, account, resolveDirectToken(account), "manual")
}

func syncWithDirectToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, token string, source string) (*syncSnapshot, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("direct token is required")
	}
	models, err := fetchModelsForSiteToken(ctx, siteRecord, model.SiteToken{Token: token, GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true})
	if err != nil {
		return nil, err
	}
	return &syncSnapshot{
		accessToken: strings.TrimSpace(account.AccessToken),
		groups:      []model.SiteUserGroup{{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}},
		tokens:      []model.SiteToken{{Name: "default", Token: token, GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true, Source: source, IsDefault: true}},
		models:      buildSiteModels(models, source),
		message:     "site account synced",
	}, nil
}

func resolveManagedAccessToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (string, error) {
	if account.CredentialType == model.SiteCredentialTypeAccessToken {
		if strings.TrimSpace(account.AccessToken) == "" {
			return "", fmt.Errorf("access token is required")
		}
		return strings.TrimSpace(account.AccessToken), nil
	}
	if account.CredentialType != model.SiteCredentialTypeUsernamePassword {
		return "", fmt.Errorf("managed access token is not available for credential type %s", account.CredentialType)
	}

	payload, err := requestJSON(ctx, siteRecord, http.MethodPost, buildSiteURL(siteRecord.BaseURL, "/api/user/login"), map[string]any{"username": account.Username, "password": account.Password}, nil)
	if err != nil {
		return "", err
	}
	if !jsonBool(payload["success"]) {
		return "", fmt.Errorf("%s", firstNonEmptyString(jsonString(payload["message"]), "login failed"))
	}

	token := jsonString(payload["data"])
	if token == "" {
		if dataMap, ok := payload["data"].(map[string]any); ok {
			token = firstNonEmptyString(jsonString(dataMap["token"]), jsonString(dataMap["access_token"]), jsonString(dataMap["accessToken"]))
		}
	}
	if token == "" {
		token = firstNonEmptyString(jsonString(payload["token"]), jsonString(payload["access_token"]), jsonString(payload["accessToken"]))
	}
	if token == "" {
		return "", fmt.Errorf("login succeeded but no access token was returned")
	}
	return token, nil
}

func resolveDirectToken(account *model.SiteAccount) string {
	if account == nil {
		return ""
	}
	if strings.TrimSpace(account.APIKey) != "" {
		return strings.TrimSpace(account.APIKey)
	}
	return strings.TrimSpace(account.AccessToken)
}
