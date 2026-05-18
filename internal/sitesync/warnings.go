package sitesync

import (
	"context"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func fetchPricingWithWarnings(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string, groups []model.SiteUserGroup) ([]model.SitePrice, []siteSyncWarning) {
	prices, err := fetchPricing(ctx, siteRecord, account, accessToken, groups)
	if err == nil {
		return prices, nil
	}
	lowered := strings.ToLower(err.Error())
	if strings.Contains(lowered, "pricing sync not supported") || strings.Contains(lowered, "http 404") || strings.Contains(lowered, "not found") {
		return nil, nil
	}
	return nil, []siteSyncWarning{{Reason: SiteBatchReasonPricingFetchFailed, Message: sanitizeSiteStatusMessage(err)}}
}

func applySyncWarnings(status model.SiteExecutionStatus, message string, warnings []siteSyncWarning) (model.SiteExecutionStatus, string) {
	if len(warnings) == 0 {
		return status, sanitizeSiteStatusText(message)
	}
	if status == model.SiteExecutionStatusSuccess {
		status = model.SiteExecutionStatusPartial
	}
	parts := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		switch warning.Reason {
		case SiteBatchReasonPricingFetchFailed:
			parts = append(parts, "价格信息更新失败")
		default:
			if warning.Message != "" {
				parts = append(parts, warning.Message)
			}
		}
	}
	warningMessage := strings.Join(parts, "，")
	message = sanitizeSiteStatusText(message)
	if warningMessage == "" {
		return status, message
	}
	if message == "" {
		return status, sanitizeSiteStatusText(warningMessage)
	}
	return status, sanitizeSiteStatusText(message + "；" + warningMessage)
}
