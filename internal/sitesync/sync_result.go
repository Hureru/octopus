package sitesync

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

type siteGroupSyncStatus string

const (
	siteGroupSyncStatusSynced     siteGroupSyncStatus = "synced"
	siteGroupSyncStatusEmpty      siteGroupSyncStatus = "empty"
	siteGroupSyncStatusMissingKey siteGroupSyncStatus = "missing_key"
	siteGroupSyncStatusUnresolved siteGroupSyncStatus = "unresolved"
	siteGroupSyncStatusFailed     siteGroupSyncStatus = "failed"
	siteGroupSyncStatusRemoved    siteGroupSyncStatus = "removed"
)

type siteGroupSyncResult struct {
	GroupKey      string
	GroupName     string
	HasKey        bool
	Status        siteGroupSyncStatus
	Authoritative bool
	ModelCount    int
	Message       string
}

func hasAuthoritativeGroupSyncResult(results []siteGroupSyncResult) bool {
	for _, item := range results {
		if item.Authoritative {
			return true
		}
	}
	return false
}

func hasGroupSyncFailure(results []siteGroupSyncResult) bool {
	for _, item := range results {
		switch item.Status {
		case siteGroupSyncStatusFailed, siteGroupSyncStatusUnresolved, siteGroupSyncStatusMissingKey:
			return true
		}
	}
	return false
}

func buildSyncSnapshotStatus(results []siteGroupSyncResult) model.SiteExecutionStatus {
	if !hasAuthoritativeGroupSyncResult(results) {
		return model.SiteExecutionStatusFailed
	}
	if hasGroupSyncFailure(results) {
		return model.SiteExecutionStatusPartial
	}
	return model.SiteExecutionStatusSuccess
}

func buildSyncSnapshotMessage(results []siteGroupSyncResult) string {
	counts := make(map[siteGroupSyncStatus]int)
	for _, item := range results {
		counts[item.Status]++
	}

	parts := make([]string, 0, 5)
	if counts[siteGroupSyncStatusSynced] > 0 {
		parts = append(parts, fmt.Sprintf("更新 %d 个分组", counts[siteGroupSyncStatusSynced]))
	}
	if counts[siteGroupSyncStatusEmpty] > 0 {
		parts = append(parts, fmt.Sprintf("清空 %d 个分组", counts[siteGroupSyncStatusEmpty]))
	}
	if counts[siteGroupSyncStatusRemoved] > 0 {
		parts = append(parts, fmt.Sprintf("移除 %d 个分组", counts[siteGroupSyncStatusRemoved]))
	}
	if counts[siteGroupSyncStatusUnresolved] > 0 || counts[siteGroupSyncStatusFailed] > 0 {
		parts = append(parts, fmt.Sprintf("保留 %d 个分组的历史模型", counts[siteGroupSyncStatusUnresolved]+counts[siteGroupSyncStatusFailed]))
	}
	if counts[siteGroupSyncStatusMissingKey] > 0 {
		parts = append(parts, fmt.Sprintf("%d 个分组缺少可用 Key", counts[siteGroupSyncStatusMissingKey]))
	}

	message := strings.Join(parts, "，")
	if message == "" {
		message = "没有可写入的分组变更"
	}

	switch buildSyncSnapshotStatus(results) {
	case model.SiteExecutionStatusPartial:
		return "部分分组同步完成：" + message
	case model.SiteExecutionStatusSuccess:
		if counts[siteGroupSyncStatusSynced] == 0 && counts[siteGroupSyncStatusEmpty] > 0 && counts[siteGroupSyncStatusRemoved] == 0 {
			return "上游当前无可用模型，已清空历史模型：" + message
		}
		if counts[siteGroupSyncStatusSynced] == 0 && counts[siteGroupSyncStatusEmpty] > 0 && counts[siteGroupSyncStatusRemoved] > 0 {
			return "上游当前无可用模型或分组已被移除，已清理历史数据：" + message
		}
		return "同步完成：" + message
	default:
		return "同步失败：所有分组都未能确认模型，已保留历史模型"
	}
}

func buildSyncSnapshotFailure(results []siteGroupSyncResult) error {
	if len(results) == 0 {
		return fmt.Errorf("站点账号同步失败：没有可用的分组同步结果")
	}

	parts := make([]string, 0, len(results))
	for _, item := range results {
		switch item.Status {
		case siteGroupSyncStatusFailed, siteGroupSyncStatusUnresolved, siteGroupSyncStatusMissingKey:
			message := strings.TrimSpace(item.Message)
			if message == "" {
				message = "本次未能确认模型"
			}
			parts = append(parts, fmt.Sprintf("%s（%s）", item.GroupKey, message))
		}
	}
	if len(parts) == 0 {
		return fmt.Errorf("站点账号同步失败：所有分组都未能确认模型，已保留历史模型")
	}
	return fmt.Errorf("站点账号同步失败：所有分组都未能确认模型，已保留历史模型。%s", strings.Join(parts, "；"))
}

func finalizeSiteGroupSyncResults(
	account *model.SiteAccount,
	groups []model.SiteUserGroup,
	tokens []model.SiteToken,
	models []model.SiteModel,
	tokenResults []siteGroupSyncResult,
) []siteGroupSyncResult {
	resultMap := make(map[string]siteGroupSyncResult)
	groupNames := collectCurrentGroupNames(account, groups, tokens)
	hasKeyMap := make(map[string]bool, len(tokens))
	currentKeys := make(map[string]struct{})
	modelCounts := make(map[string]int)

	for _, token := range tokens {
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		hasKeyMap[groupKey] = true
		currentKeys[groupKey] = struct{}{}
	}
	for _, group := range groups {
		groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
		currentKeys[groupKey] = struct{}{}
	}
	for _, item := range models {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		if strings.TrimSpace(item.ModelName) == "" {
			continue
		}
		modelCounts[groupKey]++
		currentKeys[groupKey] = struct{}{}
	}

	for _, item := range tokenResults {
		item.GroupKey = model.NormalizeSiteGroupKey(item.GroupKey)
		item.GroupName = resolveGroupName(item.GroupKey, item.GroupName, groupNames)
		item.HasKey = true
		if item.ModelCount == 0 {
			item.ModelCount = modelCounts[item.GroupKey]
		}
		resultMap[item.GroupKey] = item
	}

	keys := make([]string, 0, len(currentKeys))
	for groupKey := range currentKeys {
		keys = append(keys, groupKey)
	}
	for _, groupKey := range keys {
		if _, ok := resultMap[groupKey]; ok {
			continue
		}
		count := modelCounts[groupKey]
		result := siteGroupSyncResult{
			GroupKey:   groupKey,
			GroupName:  resolveGroupName(groupKey, "", groupNames),
			HasKey:     hasKeyMap[groupKey],
			ModelCount: count,
		}
		if count > 0 {
			result.Status = siteGroupSyncStatusSynced
			result.Authoritative = true
			result.Message = fmt.Sprintf("通过显式分组元数据同步到 %d 个模型", count)
		} else if hasKeyMap[groupKey] {
			result.Status = siteGroupSyncStatusUnresolved
			result.Message = "本次未能确认该分组模型，已保留历史模型"
		} else {
			result.Status = siteGroupSyncStatusMissingKey
			result.Message = "该分组没有可用 Key，本次保留历史模型"
		}
		resultMap[groupKey] = result
	}

	for groupKey, groupName := range collectExistingGroupNames(account) {
		if _, ok := currentKeys[groupKey]; ok {
			continue
		}
		resultMap[groupKey] = siteGroupSyncResult{
			GroupKey:      groupKey,
			GroupName:     groupName,
			HasKey:        false,
			Status:        siteGroupSyncStatusRemoved,
			Authoritative: true,
			Message:       "上游已移除此分组，已清理历史模型",
		}
	}

	results := make([]siteGroupSyncResult, 0, len(resultMap))
	for _, item := range resultMap {
		results = append(results, item)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].GroupKey < results[j].GroupKey
	})
	return results
}

func collectCurrentGroupNames(account *model.SiteAccount, groups []model.SiteUserGroup, tokens []model.SiteToken) map[string]string {
	result := collectExistingGroupNames(account)
	for _, group := range groups {
		groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
		result[groupKey] = model.NormalizeSiteGroupName(groupKey, group.Name)
	}
	for _, token := range tokens {
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		if _, ok := result[groupKey]; ok {
			continue
		}
		result[groupKey] = model.NormalizeSiteGroupName(groupKey, token.GroupName)
	}
	return result
}

func collectExistingGroupNames(account *model.SiteAccount) map[string]string {
	result := make(map[string]string)
	if account == nil {
		return result
	}
	for _, group := range account.UserGroups {
		groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
		result[groupKey] = model.NormalizeSiteGroupName(groupKey, group.Name)
	}
	for _, token := range account.Tokens {
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		if _, ok := result[groupKey]; ok {
			continue
		}
		result[groupKey] = model.NormalizeSiteGroupName(groupKey, token.GroupName)
	}
	for _, item := range account.Models {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		if _, ok := result[groupKey]; ok {
			continue
		}
		result[groupKey] = model.NormalizeSiteGroupName(groupKey, "")
	}
	return result
}

func resolveGroupName(groupKey string, preferred string, names map[string]string) string {
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	if trimmed := strings.TrimSpace(preferred); trimmed != "" {
		return model.NormalizeSiteGroupName(groupKey, trimmed)
	}
	if name, ok := names[groupKey]; ok {
		return model.NormalizeSiteGroupName(groupKey, name)
	}
	return model.NormalizeSiteGroupName(groupKey, "")
}

func exportSiteSyncGroupResults(results []siteGroupSyncResult) []model.SiteSyncGroupResult {
	if len(results) == 0 {
		return nil
	}
	exported := make([]model.SiteSyncGroupResult, 0, len(results))
	for _, item := range results {
		exported = append(exported, model.SiteSyncGroupResult{
			GroupKey:      item.GroupKey,
			GroupName:     item.GroupName,
			HasKey:        item.HasKey,
			Status:        string(item.Status),
			Authoritative: item.Authoritative,
			ModelCount:    item.ModelCount,
			Message:       item.Message,
		})
	}
	return exported
}
