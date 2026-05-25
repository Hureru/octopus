package sitesync

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const maxSiteBatchLogGroups = 5

type SiteBatchPhase string

const (
	SiteBatchPhaseSync    SiteBatchPhase = "sync"
	SiteBatchPhaseCheckin SiteBatchPhase = "checkin"
)

type SiteBatchTrigger string

const (
	SiteBatchTriggerScheduled SiteBatchTrigger = "scheduled"
	SiteBatchTriggerManual    SiteBatchTrigger = "manual"
	SiteBatchTriggerImport    SiteBatchTrigger = "import"
	SiteBatchTriggerAuto      SiteBatchTrigger = "auto"
)

type SiteBatchReason string

const (
	SiteBatchReasonCloudflareProtection    SiteBatchReason = "cloudflare_protection"
	SiteBatchReasonUnauthorized            SiteBatchReason = "unauthorized"
	SiteBatchReasonLoginFailed             SiteBatchReason = "login_failed"
	SiteBatchReasonAccessTokenRequired     SiteBatchReason = "access_token_required"
	SiteBatchReasonDirectTokenRequired     SiteBatchReason = "direct_token_required"
	SiteBatchReasonUnsupportedPlatform     SiteBatchReason = "unsupported_platform"
	SiteBatchReasonUnsupportedCheckin      SiteBatchReason = "unsupported_checkin"
	SiteBatchReasonMissingGroupKey         SiteBatchReason = "missing_group_key"
	SiteBatchReasonUpstreamHTTPError       SiteBatchReason = "upstream_http_error"
	SiteBatchReasonUpstreamDecodeFailed    SiteBatchReason = "upstream_decode_failed"
	SiteBatchReasonUpstreamHTMLResponse    SiteBatchReason = "upstream_html_response"
	SiteBatchReasonScheduledLater          SiteBatchReason = "scheduled_later"
	SiteBatchReasonBatchCanceled           SiteBatchReason = "batch_canceled"
	SiteBatchReasonTimeout                 SiteBatchReason = "timeout"
	SiteBatchReasonContextCanceled         SiteBatchReason = "context_canceled"
	SiteBatchReasonContextDeadlineExceeded SiteBatchReason = "context_deadline_exceeded"
	SiteBatchReasonDatabaseError           SiteBatchReason = "database_error"
	SiteBatchReasonInternalError           SiteBatchReason = "internal_error"
	SiteBatchReasonUnknown                 SiteBatchReason = "unknown"
)

type SiteBatchOptions struct {
	Trigger SiteBatchTrigger
}

type SiteBatchSummary struct {
	Phase        SiteBatchPhase
	Trigger      SiteBatchTrigger
	Total        int
	Attempted    int
	Success      int
	Partial      int
	Failed       int
	Skipped      int
	Warnings     int
	Canceled     bool
	CancelReason SiteBatchReason
	Duration     time.Duration

	failureGroups map[siteBatchGroupKey]*SiteBatchOutcomeGroup
	warningGroups map[siteBatchGroupKey]*SiteBatchOutcomeGroup
	skipGroups    map[siteBatchGroupKey]*SiteBatchOutcomeGroup
	Samples       []SiteBatchFailureSample
	startedAt     time.Time
}

type SiteBatchOutcomeGroup struct {
	SiteID   int
	Platform model.SitePlatform
	Reason   SiteBatchReason
	Count    int
	Failed   int
	Skipped  int
	Warnings int
}

type SiteBatchFailureSample struct {
	SiteID    int
	Platform  model.SitePlatform
	AccountID int
	Reason    SiteBatchReason
	Message   string
}

type siteBatchGroupKey struct {
	SiteID   int
	Platform model.SitePlatform
	Reason   SiteBatchReason
}

func normalizedSiteBatchTrigger(trigger SiteBatchTrigger) SiteBatchTrigger {
	if trigger == "" {
		return SiteBatchTriggerScheduled
	}
	return trigger
}

func newSiteBatchSummary(phase SiteBatchPhase, opts SiteBatchOptions, total int) *SiteBatchSummary {
	trigger := normalizedSiteBatchTrigger(opts.Trigger)
	return &SiteBatchSummary{
		Phase:         phase,
		Trigger:       trigger,
		Total:         total,
		failureGroups: make(map[siteBatchGroupKey]*SiteBatchOutcomeGroup),
		warningGroups: make(map[siteBatchGroupKey]*SiteBatchOutcomeGroup),
		skipGroups:    make(map[siteBatchGroupKey]*SiteBatchOutcomeGroup),
		startedAt:     time.Now(),
	}
}

func (s *SiteBatchSummary) finish() {
	if s.startedAt.IsZero() {
		return
	}
	s.Duration = time.Since(s.startedAt)
}

func (s *SiteBatchSummary) recordResult(siteID int, platform model.SitePlatform, accountID int, status model.SiteExecutionStatus, message string) {
	s.Attempted++
	safeMessage := sanitizeSiteStatusText(message)
	switch status {
	case model.SiteExecutionStatusSuccess:
		s.Success++
	case model.SiteExecutionStatusPartial:
		s.Partial++
	case model.SiteExecutionStatusSkipped:
		s.Skipped++
		s.addGroup(s.skipGroups, siteID, platform, SiteBatchReasonUnsupportedCheckin, func(g *SiteBatchOutcomeGroup) { g.Skipped++ })
	case model.SiteExecutionStatusFailed:
		s.Failed++
		s.addFailure(siteID, platform, accountID, SiteBatchReasonUnknown, safeMessage)
	default:
		s.Success++
	}
}

func (s *SiteBatchSummary) recordFailure(siteID int, platform model.SitePlatform, accountID int, err error) {
	s.Attempted++
	s.Failed++
	s.addFailure(siteID, platform, accountID, siteBatchReason(err), sanitizeSiteStatusMessage(err))
}

func (s *SiteBatchSummary) recordSkip(siteID int, platform model.SitePlatform, reason SiteBatchReason, count int) {
	if count <= 0 {
		return
	}
	s.Skipped += count
	s.addGroupN(s.skipGroups, siteID, platform, reason, count, func(g *SiteBatchOutcomeGroup, n int) { g.Skipped += n })
}

func (s *SiteBatchSummary) markCanceled(ctxErr error) {
	s.Canceled = true
	s.CancelReason = contextCancelReason(ctxErr)
}

func (s *SiteBatchSummary) addFailure(siteID int, platform model.SitePlatform, accountID int, reason SiteBatchReason, message string) {
	s.addGroup(s.failureGroups, siteID, platform, reason, func(g *SiteBatchOutcomeGroup) { g.Failed++ })
	s.addSample(siteID, platform, accountID, reason, message)
}

func (s *SiteBatchSummary) addWarning(siteID int, platform model.SitePlatform, accountID int, reason SiteBatchReason, message string) {
	if reason == "" {
		reason = SiteBatchReasonUnknown
	}
	s.Warnings++
	s.addGroup(s.warningGroups, siteID, platform, reason, func(g *SiteBatchOutcomeGroup) { g.Warnings++ })
	s.addSample(siteID, platform, accountID, reason, sanitizeSiteStatusText(message))
}

func (s *SiteBatchSummary) addGroup(groups map[siteBatchGroupKey]*SiteBatchOutcomeGroup, siteID int, platform model.SitePlatform, reason SiteBatchReason, update func(*SiteBatchOutcomeGroup)) {
	s.addGroupN(groups, siteID, platform, reason, 1, func(g *SiteBatchOutcomeGroup, _ int) { update(g) })
}

func (s *SiteBatchSummary) addGroupN(groups map[siteBatchGroupKey]*SiteBatchOutcomeGroup, siteID int, platform model.SitePlatform, reason SiteBatchReason, n int, update func(*SiteBatchOutcomeGroup, int)) {
	if reason == "" {
		reason = SiteBatchReasonUnknown
	}
	key := siteBatchGroupKey{SiteID: siteID, Platform: platform, Reason: reason}
	group := groups[key]
	if group == nil {
		group = &SiteBatchOutcomeGroup{SiteID: siteID, Platform: platform, Reason: reason}
		groups[key] = group
	}
	group.Count += n
	update(group, n)
}

func (s *SiteBatchSummary) addSample(siteID int, platform model.SitePlatform, accountID int, reason SiteBatchReason, message string) {
	if len(s.Samples) >= maxSiteBatchLogGroups || message == "" {
		return
	}
	for _, sample := range s.Samples {
		if sample.SiteID == siteID && sample.Platform == platform && sample.Reason == reason {
			return
		}
	}
	s.Samples = append(s.Samples, SiteBatchFailureSample{SiteID: siteID, Platform: platform, AccountID: accountID, Reason: reason, Message: message})
}

func (s *SiteBatchSummary) emitLog() {
	s.finish()
	failureGroups := sortedSiteBatchGroups(s.failureGroups)
	warningGroups := sortedSiteBatchGroups(s.warningGroups)
	skipGroups := sortedSiteBatchGroups(s.skipGroups)
	if s.Failed == 0 && s.Warnings == 0 && !hasExceptionalSkips(skipGroups) && !s.Canceled {
		log.Debugw(siteBatchEventName(s, false), s.logFields(failureGroups, warningGroups, skipGroups)...)
		return
	}
	log.Warnw(siteBatchEventName(s, s.Failed == 0 && s.Warnings > 0 && !s.Canceled), s.logFields(failureGroups, warningGroups, skipGroups)...)
	if len(s.Samples) > 0 {
		log.Debugw(string("sitesync."+string(s.Phase)+".failure_samples"), "trigger", string(s.Trigger), "samples", formatSiteBatchSamples(s.Samples))
	}
}

func (s *SiteBatchSummary) logFields(failureGroups, warningGroups, skipGroups []SiteBatchOutcomeGroup) []interface{} {
	fields := []interface{}{
		"trigger", string(s.Trigger),
		"total", s.Total,
		"attempted", s.Attempted,
		"success", s.Success,
		"partial", s.Partial,
		"failed", s.Failed,
		"skipped", s.Skipped,
		"warnings", s.Warnings,
		"failure_groups", len(failureGroups),
		"warning_groups", len(warningGroups),
		"skip_groups", len(skipGroups),
		"duration", s.Duration.String(),
	}
	fields = appendTopGroupFields(fields, "failure", failureGroups)
	fields = appendTopGroupFields(fields, "warning", warningGroups)
	fields = appendTopGroupFields(fields, "skip", skipGroups)
	if s.Canceled {
		fields = append(fields, "canceled", true, "cancel_reason", string(s.CancelReason))
	}
	return fields
}

func siteBatchEventName(s *SiteBatchSummary, warningOnly bool) string {
	if warningOnly {
		return "sitesync." + string(s.Phase) + ".warning_summary"
	}
	if s.Failed == 0 && s.Warnings == 0 && !s.Canceled {
		return "sitesync." + string(s.Phase) + ".done"
	}
	return "sitesync." + string(s.Phase) + ".summary"
}

func appendTopGroupFields(fields []interface{}, prefix string, groups []SiteBatchOutcomeGroup) []interface{} {
	if len(groups) == 0 {
		return fields
	}
	top, omitted := splitTopSiteBatchGroups(groups)
	fields = append(fields, "top_"+prefix+"_groups", formatSiteBatchGroups(top))
	if omitted.groups > 0 {
		fields = append(fields,
			"omitted_"+prefix+"_groups", omitted.groups,
			"omitted_"+prefix+"_failures", omitted.failures,
			"omitted_"+prefix+"_skips", omitted.skips,
			"omitted_"+prefix+"_warnings", omitted.warnings,
		)
	}
	return fields
}

type omittedSiteBatchGroups struct {
	groups   int
	failures int
	skips    int
	warnings int
}

func splitTopSiteBatchGroups(groups []SiteBatchOutcomeGroup) ([]SiteBatchOutcomeGroup, omittedSiteBatchGroups) {
	if len(groups) <= maxSiteBatchLogGroups {
		return groups, omittedSiteBatchGroups{}
	}
	omitted := omittedSiteBatchGroups{groups: len(groups) - maxSiteBatchLogGroups}
	for _, group := range groups[maxSiteBatchLogGroups:] {
		omitted.failures += group.Failed
		omitted.skips += group.Skipped
		omitted.warnings += group.Warnings
	}
	return groups[:maxSiteBatchLogGroups], omitted
}

func sortedSiteBatchGroups(groups map[siteBatchGroupKey]*SiteBatchOutcomeGroup) []SiteBatchOutcomeGroup {
	result := make([]SiteBatchOutcomeGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		if result[i].SiteID != result[j].SiteID {
			return result[i].SiteID < result[j].SiteID
		}
		if result[i].Platform != result[j].Platform {
			return result[i].Platform < result[j].Platform
		}
		return result[i].Reason < result[j].Reason
	})
	return result
}

func formatSiteBatchGroups(groups []SiteBatchOutcomeGroup) string {
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		items := []string{fmt.Sprintf("site=%d", group.SiteID)}
		if group.Platform != "" {
			items = append(items, "platform="+string(group.Platform))
		}
		items = append(items, "reason="+string(group.Reason))
		if group.Failed > 0 {
			items = append(items, fmt.Sprintf("failed=%d", group.Failed))
		}
		if group.Skipped > 0 {
			items = append(items, fmt.Sprintf("skipped=%d", group.Skipped))
		}
		if group.Warnings > 0 {
			items = append(items, fmt.Sprintf("warnings=%d", group.Warnings))
		}
		parts = append(parts, strings.Join(items, " "))
	}
	return strings.Join(parts, "; ")
}

func formatSiteBatchSamples(samples []SiteBatchFailureSample) string {
	parts := make([]string, 0, len(samples))
	for _, sample := range samples {
		items := []string{fmt.Sprintf("site=%d", sample.SiteID), fmt.Sprintf("account=%d", sample.AccountID)}
		if sample.Platform != "" {
			items = append(items, "platform="+string(sample.Platform))
		}
		items = append(items, "reason="+string(sample.Reason), "message="+sample.Message)
		parts = append(parts, strings.Join(items, " "))
	}
	return strings.Join(parts, "; ")
}

func hasExceptionalSkips(groups []SiteBatchOutcomeGroup) bool {
	for _, group := range groups {
		switch group.Reason {
		case SiteBatchReasonCloudflareProtection, SiteBatchReasonBatchCanceled:
			return true
		}
	}
	return false
}

func contextCancelReason(err error) SiteBatchReason {
	switch err {
	case context.DeadlineExceeded:
		return SiteBatchReasonContextDeadlineExceeded
	case context.Canceled:
		return SiteBatchReasonContextCanceled
	default:
		return SiteBatchReasonBatchCanceled
	}
}
