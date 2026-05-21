package op

import (
	"context"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

type LogSiteActionTarget struct {
	SiteID          int    `json:"site_id"`
	SiteName        string `json:"site_name"`
	AccountID       int    `json:"account_id"`
	AccountName     string `json:"account_name"`
	GroupKey        string `json:"group_key"`
	GroupName       string `json:"group_name"`
	ModelName       string `json:"model_name"`
	ModelDisabled   bool   `json:"model_disabled"`
	CanDisableModel bool   `json:"can_disable_model"`
	ChannelID       int    `json:"channel_id"`
	ChannelName     string `json:"channel_name"`
}

type LogSiteActionTargets struct {
	AttemptTargets    []*LogSiteActionTarget `json:"attempt_targets"`
	LegacyErrorTarget *LogSiteActionTarget   `json:"legacy_error_target,omitempty"`
}

type logActionBindingRowsByChannel map[int][]logActionBindingRow

type logActionBindingRow struct {
	ChannelID     int
	ChannelName   string
	SiteID        int
	SiteName      string
	AccountID     int
	AccountName   string
	GroupKey      string
	GroupName     string
	ModelName     string
	ModelDisabled bool
}

func RelayLogSiteActionTargets(ctx context.Context, ids []int64) (map[int64]LogSiteActionTargets, error) {
	result := make(map[int64]LogSiteActionTargets)
	if len(ids) == 0 {
		return result, nil
	}

	logs := make([]model.RelayLog, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		item, err := RelayLogGet(ctx, id)
		if err != nil || item == nil {
			continue
		}
		logs = append(logs, *item)
	}
	if len(logs) == 0 {
		return result, nil
	}

	channelIDs := make(map[int]struct{})
	for _, item := range logs {
		if item.ChannelId > 0 {
			channelIDs[item.ChannelId] = struct{}{}
		}
		for _, attempt := range item.Attempts {
			if attempt.ChannelID > 0 {
				channelIDs[attempt.ChannelID] = struct{}{}
			}
		}
	}
	bindingRows, err := logActionBindingRows(ctx, channelIDs)
	if err != nil {
		return nil, err
	}

	for _, item := range logs {
		view := LogSiteActionTargets{AttemptTargets: make([]*LogSiteActionTarget, len(item.Attempts))}
		fallbackModelName := firstNonEmptyLogActionString(strings.TrimSpace(item.ActualModelName), strings.TrimSpace(item.RequestModelName))
		for index, attempt := range item.Attempts {
			modelName := strings.TrimSpace(attempt.ModelName)
			if modelName == "" {
				modelName = fallbackModelName
			}
			view.AttemptTargets[index] = resolveLogActionTarget(bindingRows[attempt.ChannelID], modelName)
		}
		if item.Error != "" {
			view.LegacyErrorTarget = resolveLogActionTarget(bindingRows[item.ChannelId], fallbackModelName)
		}
		result[item.ID] = view
	}
	return result, nil
}

func logActionBindingRows(ctx context.Context, channelIDs map[int]struct{}) (logActionBindingRowsByChannel, error) {
	rowsByChannel := make(logActionBindingRowsByChannel, len(channelIDs))
	for channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		channel, err := ChannelGet(channelID, ctx)
		if err != nil || channel == nil {
			continue
		}
		binding, ok, err := ChannelManagedBinding(channelID, ctx)
		if err != nil {
			return nil, err
		}
		if !ok || binding == nil {
			continue
		}
		site, err := SiteGet(binding.SiteID, ctx)
		if err != nil || site == nil {
			continue
		}
		account := findSiteAccount(*site, binding.SiteAccountID)
		if account == nil {
			continue
		}
		baseGroupKey, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
		groupName := siteAccountGroupName(*account, baseGroupKey)
		for _, siteModel := range account.Models {
			if model.NormalizeSiteGroupKey(siteModel.GroupKey) != baseGroupKey {
				continue
			}
			rowsByChannel[channelID] = append(rowsByChannel[channelID], logActionBindingRow{
				ChannelID:     channelID,
				ChannelName:   channel.Name,
				SiteID:        site.ID,
				SiteName:      site.Name,
				AccountID:     account.ID,
				AccountName:   account.Name,
				GroupKey:      baseGroupKey,
				GroupName:     groupName,
				ModelName:     siteModel.ModelName,
				ModelDisabled: siteModel.Disabled,
			})
		}
	}
	return rowsByChannel, nil
}

func resolveLogActionTarget(rows []logActionBindingRow, modelName string) *LogSiteActionTarget {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" || len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		if row.ModelName != modelName {
			continue
		}
		return &LogSiteActionTarget{
			SiteID:          row.SiteID,
			SiteName:        row.SiteName,
			AccountID:       row.AccountID,
			AccountName:     row.AccountName,
			GroupKey:        row.GroupKey,
			GroupName:       row.GroupName,
			ModelName:       modelName,
			ModelDisabled:   row.ModelDisabled,
			CanDisableModel: true,
			ChannelID:       row.ChannelID,
			ChannelName:     row.ChannelName,
		}
	}
	return nil
}

func firstNonEmptyLogActionString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func findSiteAccount(site model.Site, accountID int) *model.SiteAccount {
	for i := range site.Accounts {
		if site.Accounts[i].ID == accountID {
			return &site.Accounts[i]
		}
	}
	return nil
}

func siteAccountGroupName(account model.SiteAccount, groupKey string) string {
	for _, group := range account.UserGroups {
		if model.NormalizeSiteGroupKey(group.GroupKey) == groupKey {
			return model.NormalizeSiteGroupName(groupKey, group.Name)
		}
	}
	return model.NormalizeSiteGroupName(groupKey, groupKey)
}
