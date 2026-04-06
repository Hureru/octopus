package sitesync

import (
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func platformUsesSKPrefixedSiteToken(platform model.SitePlatform) bool {
	switch platform {
	case model.SitePlatformNewAPI,
		model.SitePlatformAnyRouter,
		model.SitePlatformOneAPI,
		model.SitePlatformOneHub,
		model.SitePlatformDoneHub,
		model.SitePlatformSub2API,
		model.SitePlatformOpenAI:
		return true
	default:
		return false
	}
}

func shouldNormalizeSiteTokenValue(siteRecord *model.Site, token model.SiteToken) bool {
	if siteRecord == nil || !platformUsesSKPrefixedSiteToken(siteRecord.Platform) {
		return false
	}
	if model.IsMaskedSiteTokenValue(token.Token) {
		return false
	}
	return strings.TrimSpace(token.Source) != "access_token_fallback"
}

func normalizeSiteTokenForPlatform(siteRecord *model.Site, token model.SiteToken) model.SiteToken {
	token.Token = normalizeSiteTokenValueForPlatform(siteRecord, token)
	return token
}

func normalizeSiteTokenValueForPlatform(siteRecord *model.Site, token model.SiteToken) string {
	trimmed := strings.TrimSpace(token.Token)
	if trimmed == "" {
		return ""
	}
	if shouldNormalizeSiteTokenValue(siteRecord, token) {
		return model.NormalizeSiteSyncTokenValue(trimmed)
	}
	return trimmed
}

func normalizeSiteTokensForPlatform(siteRecord *model.Site, tokens []model.SiteToken) []model.SiteToken {
	if len(tokens) == 0 {
		return tokens
	}
	for i := range tokens {
		tokens[i] = normalizeSiteTokenForPlatform(siteRecord, tokens[i])
	}
	return tokens
}

func hasSKPrefix(value string) bool {
	trimmed := strings.TrimSpace(value)
	return len(trimmed) >= 3 && strings.EqualFold(trimmed[:3], "sk-")
}

func preferComparableSiteTokenValue(existingToken string, incomingToken string) string {
	existingTrimmed := strings.TrimSpace(existingToken)
	incomingTrimmed := strings.TrimSpace(incomingToken)
	switch {
	case hasSKPrefix(existingTrimmed) && !hasSKPrefix(incomingTrimmed):
		return existingTrimmed
	case hasSKPrefix(incomingTrimmed) && !hasSKPrefix(existingTrimmed):
		return incomingTrimmed
	case existingTrimmed != "":
		return existingTrimmed
	default:
		return incomingTrimmed
	}
}
