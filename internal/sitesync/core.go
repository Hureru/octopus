package sitesync

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

type syncSnapshot struct {
	accessToken  string
	groups       []model.SiteUserGroup
	tokens       []model.SiteToken
	models       []model.SiteModel
	groupResults []siteGroupSyncResult
	status       model.SiteExecutionStatus
	balance      float64
	balanceUsed  float64
	todayIncome  float64
	message      string
}

type siteBatchAccount struct {
	site    *model.Site
	account *model.SiteAccount
}

func SyncAccount(ctx context.Context, accountID int) (*model.SiteSyncResult, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}

	snapshot, syncErr := syncAccountState(ctx, siteRecord, account)
	if snapshot == nil && syncErr != nil {
		message := sanitizeSiteStatusMessage(syncErr)
		updateErr := updateAccountSyncState(ctx, account.ID, model.SiteExecutionStatusFailed, message, "")
		if updateErr != nil {
			log.Warnf("failed to update site account sync state (account=%d): %v", account.ID, updateErr)
		}
		if staleErr := MarkAccountProjectionStale(ctx, account.ID, message); staleErr != nil {
			log.Warnf("failed to mark site account projection stale (account=%d): %v", account.ID, staleErr)
		}
		return nil, sanitizeSiteError(syncErr)
	}

	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		return nil, sanitizeSiteError(err)
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}

	modelNames := make([]string, 0, len(snapshot.models))
	for _, item := range snapshot.models {
		modelNames = append(modelNames, item.ModelName)
	}
	slices.Sort(modelNames)

	result := &model.SiteSyncResult{
		AccountID:       account.ID,
		SiteID:          siteRecord.ID,
		Status:          snapshot.status,
		ChannelCount:    len(channelIDs),
		GroupCount:      len(snapshot.groups),
		TokenCount:      len(snapshot.tokens),
		ModelCount:      len(snapshot.models),
		ManagedChannels: channelIDs,
		Models:          modelNames,
		GroupResults:    exportSiteSyncGroupResults(snapshot.groupResults),
		Message:         sanitizeSiteStatusText(snapshot.message),
	}
	if syncErr != nil {
		return result, sanitizeSiteError(syncErr)
	}
	return result, nil
}

func CheckinAccount(ctx context.Context, accountID int) (*model.SiteCheckinResult, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}

	result, resolvedAccessToken, err := checkinAccountState(ctx, siteRecord, account)
	if err != nil {
		status := model.SiteExecutionStatusFailed
		lowered := strings.ToLower(err.Error())
		if strings.Contains(lowered, "not supported") || strings.Contains(lowered, "not found") {
			status = model.SiteExecutionStatusSkipped
		}
		message := sanitizeSiteStatusMessage(err)
		updateErr := updateAccountCheckinState(ctx, account, status, message, false, resolvedAccessToken)
		if updateErr != nil {
			return nil, sanitizeSiteError(updateErr)
		}
		return &model.SiteCheckinResult{AccountID: account.ID, SiteID: siteRecord.ID, Status: status, Message: message}, nil
	}

	result.AccountID = account.ID
	result.SiteID = siteRecord.ID
	result.Message = sanitizeSiteStatusText(result.Message)
	if err := updateAccountCheckinState(ctx, account, result.Status, result.Message, result.Status == model.SiteExecutionStatusSuccess, resolvedAccessToken); err != nil {
		return nil, sanitizeSiteError(err)
	}
	return result, nil
}

// 全量同步/签到的上次执行时间（含定时与手动触发），仅内存记录，重启后清零
var (
	lastBatchTimeMu    sync.RWMutex
	lastSyncAllTime    time.Time
	lastCheckinAllTime time.Time
)

func markLastSyncAllTime() {
	lastBatchTimeMu.Lock()
	lastSyncAllTime = time.Now()
	lastBatchTimeMu.Unlock()
}

func markLastCheckinAllTime() {
	lastBatchTimeMu.Lock()
	lastCheckinAllTime = time.Now()
	lastBatchTimeMu.Unlock()
}

func LastSyncAllTime() time.Time {
	lastBatchTimeMu.RLock()
	defer lastBatchTimeMu.RUnlock()
	return lastSyncAllTime
}

func LastCheckinAllTime() time.Time {
	lastBatchTimeMu.RLock()
	defer lastBatchTimeMu.RUnlock()
	return lastCheckinAllTime
}

func SyncAll(ctx context.Context) {
	SyncAllWithOptions(ctx, SiteBatchOptions{Trigger: SiteBatchTriggerScheduled})
}

func SyncAllWithOptions(ctx context.Context, opts SiteBatchOptions) SiteBatchSummary {
	trigger := normalizedSiteBatchTrigger(opts.Trigger)
	sites, err := op.SiteList(ctx)
	if err != nil {
		log.Warnw("sitesync.sync.list_failed", "trigger", string(trigger), "reason", string(siteBatchReason(err)), "message", sanitizeSiteStatusMessage(err))
		return SiteBatchSummary{Phase: SiteBatchPhaseSync, Trigger: trigger}
	}
	defer markLastSyncAllTime()
	return syncBatchAccounts(ctx, eligibleSyncAccounts(sites), opts)
}

func SyncAccountsWithOptions(ctx context.Context, accountIDs []int, opts SiteBatchOptions) SiteBatchSummary {
	items := make([]siteBatchAccount, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		siteRecord, account, err := loadSiteAccount(ctx, accountID)
		if err != nil {
			log.Debugf("site import sync account load failed (account=%d): %v", accountID, sanitizeSiteStatusMessage(err))
			continue
		}
		if siteRecord == nil || account == nil || !siteRecord.Enabled || !account.Enabled {
			continue
		}
		items = append(items, siteBatchAccount{site: siteRecord, account: account})
	}
	return syncBatchAccounts(ctx, items, opts)
}

func syncBatchAccounts(ctx context.Context, items []siteBatchAccount, opts SiteBatchOptions) SiteBatchSummary {
	// Site batch logging intentionally aggregates account-level business failures.
	// Individual account messages are stored on the account status; console logs
	// stay aggregated to avoid leaking upstream HTML and overwhelming operators.
	summary := newSiteBatchSummary(SiteBatchPhaseSync, opts, len(items))
	defer summary.emitLog()
	for i := 0; i < len(items); i++ {
		item := items[i]
		if !waitSiteBatchInterval(ctx, 500*time.Millisecond) {
			summary.markCanceled(ctx.Err())
			recordBatchCanceledSkips(summary, items[i:])
			return *summary
		}
		result, err := SyncAccount(ctx, item.account.ID)
		if err != nil {
			summary.recordFailure(item.site.ID, item.site.Platform, item.account.ID, err)
			if IsCloudflareProtectionError(err) || siteBatchReason(err) == SiteBatchReasonCloudflareProtection {
				i = recordCloudflareSkipsAndWait(ctx, summary, items, i, CloudflareRetryAfter(err))
			}
			continue
		}
		summary.recordResult(item.site.ID, item.site.Platform, item.account.ID, result.Status, result.Message)
	}
	return *summary
}

func CheckinAll(ctx context.Context) {
	CheckinAllWithOptions(ctx, SiteBatchOptions{Trigger: SiteBatchTriggerScheduled})
}

func CheckinAllWithOptions(ctx context.Context, opts SiteBatchOptions) SiteBatchSummary {
	trigger := normalizedSiteBatchTrigger(opts.Trigger)
	sites, err := op.SiteList(ctx)
	if err != nil {
		log.Warnw("sitesync.checkin.list_failed", "trigger", string(trigger), "reason", string(siteBatchReason(err)), "message", sanitizeSiteStatusMessage(err))
		return SiteBatchSummary{Phase: SiteBatchPhaseCheckin, Trigger: trigger}
	}
	defer markLastCheckinAllTime()
	items := eligibleCheckinAccounts(sites)
	summary := newSiteBatchSummary(SiteBatchPhaseCheckin, opts, len(items))
	defer summary.emitLog()
	now := time.Now()
	for i := 0; i < len(items); i++ {
		item := items[i]
		if item.account.RandomCheckin {
			nextAt, scheduleErr := ensureRandomCheckinSchedule(ctx, item.account, now)
			if scheduleErr != nil {
				summary.recordFailure(item.site.ID, item.site.Platform, item.account.ID, sanitizeSiteError(scheduleErr))
				continue
			}
			if nextAt != nil && now.Before(*nextAt) {
				summary.recordSkip(item.site.ID, item.site.Platform, SiteBatchReasonScheduledLater, 1)
				continue
			}
		}
		if !waitSiteBatchInterval(ctx, 500*time.Millisecond) {
			summary.markCanceled(ctx.Err())
			recordBatchCanceledSkips(summary, items[i:])
			return *summary
		}
		result, err := CheckinAccount(ctx, item.account.ID)
		if err != nil {
			summary.recordFailure(item.site.ID, item.site.Platform, item.account.ID, err)
			if IsCloudflareProtectionError(err) || siteBatchReason(err) == SiteBatchReasonCloudflareProtection {
				i = recordCloudflareSkipsAndWait(ctx, summary, items, i, CloudflareRetryAfter(err))
			}
			continue
		}
		if result.Status == model.SiteExecutionStatusSkipped {
			summary.recordSkip(item.site.ID, item.site.Platform, SiteBatchReasonUnsupportedCheckin, 1)
			continue
		}
		summary.recordResult(item.site.ID, item.site.Platform, item.account.ID, result.Status, result.Message)
	}
	return *summary
}

func eligibleSyncAccounts(sites []model.Site) []siteBatchAccount {
	items := make([]siteBatchAccount, 0)
	for siteIndex := range sites {
		siteRecord := &sites[siteIndex]
		if !siteRecord.Enabled {
			continue
		}
		for accountIndex := range siteRecord.Accounts {
			account := &siteRecord.Accounts[accountIndex]
			if !account.Enabled || !account.AutoSync {
				continue
			}
			items = append(items, siteBatchAccount{site: siteRecord, account: account})
		}
	}
	return items
}

func eligibleCheckinAccounts(sites []model.Site) []siteBatchAccount {
	items := make([]siteBatchAccount, 0)
	for siteIndex := range sites {
		siteRecord := &sites[siteIndex]
		if !siteRecord.Enabled {
			continue
		}
		for accountIndex := range siteRecord.Accounts {
			account := &siteRecord.Accounts[accountIndex]
			if !account.Enabled || !account.AutoCheckin {
				continue
			}
			items = append(items, siteBatchAccount{site: siteRecord, account: account})
		}
	}
	return items
}

func recordCloudflareSkipsAndWait(ctx context.Context, summary *SiteBatchSummary, items []siteBatchAccount, currentIndex int, retryAfter time.Duration) int {
	current := items[currentIndex]
	lastSkipped := currentIndex
	for j := currentIndex + 1; j < len(items); j++ {
		if items[j].site.ID != current.site.ID {
			break
		}
		summary.recordSkip(items[j].site.ID, items[j].site.Platform, SiteBatchReasonCloudflareProtection, 1)
		lastSkipped = j
	}
	waitSiteCloudflareRetryAfter(ctx, retryAfter)
	return lastSkipped
}

func recordBatchCanceledSkips(summary *SiteBatchSummary, items []siteBatchAccount) {
	for _, item := range items {
		summary.recordSkip(item.site.ID, item.site.Platform, SiteBatchReasonBatchCanceled, 1)
	}
}

func waitSiteBatchInterval(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func waitSiteCloudflareRetryAfter(ctx context.Context, retryAfter time.Duration) {
	waitSiteBatchInterval(ctx, retryAfter)
}

func DeleteSite(ctx context.Context, siteID int) error {
	siteRecord, err := op.SiteGet(siteID, ctx)
	if err != nil {
		return err
	}
	for _, account := range siteRecord.Accounts {
		if err := deleteManagedChannelsByAccount(ctx, account.ID); err != nil {
			return err
		}
	}
	return op.SiteDel(siteID, ctx)
}

func ArchiveSite(ctx context.Context, siteID int) error {
	siteRecord, err := op.SiteGet(siteID, ctx)
	if err != nil {
		return err
	}
	for _, account := range siteRecord.Accounts {
		if err := deleteManagedChannelsByAccount(ctx, account.ID); err != nil {
			return err
		}
	}
	return op.SiteArchive(siteID, ctx)
}

func RestoreSite(ctx context.Context, siteID int) error {
	return op.SiteRestore(siteID, ctx)
}

func ListArchivedSites(ctx context.Context) ([]model.Site, error) {
	return op.SiteListArchived(ctx)
}

func DeleteSiteAccount(ctx context.Context, accountID int) error {
	if err := deleteManagedChannelsByAccount(ctx, accountID); err != nil {
		return err
	}
	return op.SiteAccountDel(accountID, ctx)
}
