package sitesync

import (
	"sync"
	"time"
)

// SiteBatchSummaryView 是最近一次同步/签到批次的可公开视图，
// 供站点总览页做失败原因分组展示。
type SiteBatchSummaryView struct {
	Phase         SiteBatchPhase            `json:"phase"`
	Trigger       SiteBatchTrigger          `json:"trigger"`
	Total         int                       `json:"total"`
	Attempted     int                       `json:"attempted"`
	Success       int                       `json:"success"`
	Partial       int                       `json:"partial"`
	Failed        int                       `json:"failed"`
	Skipped       int                       `json:"skipped"`
	Warnings      int                       `json:"warnings"`
	Canceled      bool                      `json:"canceled"`
	CancelReason  SiteBatchReason           `json:"cancel_reason,omitempty"`
	DurationMS    int64                     `json:"duration_ms"`
	FinishedAt    time.Time                 `json:"finished_at"`
	FailureGroups []SiteBatchOutcomeGroup   `json:"failure_groups"`
	WarningGroups []SiteBatchOutcomeGroup   `json:"warning_groups"`
	Samples       []SiteBatchFailureSample  `json:"samples,omitempty"`
}

const maxLatestSamples = 12

var latestSummaries = struct {
	sync.RWMutex
	syncBatch  *SiteBatchSummaryView
	checkin    *SiteBatchSummaryView
}{}

// recordLatestSummary 在批次结束（emitLog）时保存最近一次视图。
func recordLatestSummary(s *SiteBatchSummary) {
	if s == nil {
		return
	}
	view := &SiteBatchSummaryView{
		Phase:        s.Phase,
		Trigger:      s.Trigger,
		Total:        s.Total,
		Attempted:    s.Attempted,
		Success:      s.Success,
		Partial:      s.Partial,
		Failed:       s.Failed,
		Skipped:      s.Skipped,
		Warnings:     s.Warnings,
		Canceled:     s.Canceled,
		CancelReason: s.CancelReason,
		DurationMS:   s.Duration.Milliseconds(),
		FinishedAt:   time.Now(),
		FailureGroups: sortedSiteBatchGroups(s.failureGroups),
		WarningGroups: sortedSiteBatchGroups(s.warningGroups),
	}
	if len(s.Samples) > maxLatestSamples {
		view.Samples = s.Samples[:maxLatestSamples]
	} else {
		view.Samples = s.Samples
	}
	latestSummaries.Lock()
	defer latestSummaries.Unlock()
	if s.Phase == SiteBatchPhaseCheckin {
		latestSummaries.checkin = view
	} else {
		latestSummaries.syncBatch = view
	}
}

// LatestBatchSummary 返回最近一次批次视图；未执行过返回 nil。
func LatestBatchSummary(phase SiteBatchPhase) *SiteBatchSummaryView {
	latestSummaries.RLock()
	defer latestSummaries.RUnlock()
	if phase == SiteBatchPhaseCheckin {
		return latestSummaries.checkin
	}
	return latestSummaries.syncBatch
}

// BatchSummaryReasons 前端失败分组的完整原因枚举。
func BatchSummaryReasons() []SiteBatchReason {
	return []SiteBatchReason{
		SiteBatchReasonUnauthorized,
		SiteBatchReasonLoginFailed,
		SiteBatchReasonAccessTokenRequired,
		SiteBatchReasonDirectTokenRequired,
		SiteBatchReasonCloudflareProtection,
		SiteBatchReasonMissingGroupKey,
		SiteBatchReasonUpstreamHTTPError,
		SiteBatchReasonUpstreamDecodeFailed,
		SiteBatchReasonUpstreamHTMLResponse,
		SiteBatchReasonUnsupportedPlatform,
		SiteBatchReasonUnsupportedCheckin,
		SiteBatchReasonScheduledLater,
		SiteBatchReasonBatchCanceled,
		SiteBatchReasonTimeout,
		SiteBatchReasonContextCanceled,
		SiteBatchReasonContextDeadlineExceeded,
		SiteBatchReasonDatabaseError,
		SiteBatchReasonInternalError,
		SiteBatchReasonUnknown,
	}
}
