package sitesync

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
)

const (
	siteRecoveryMaxPaths = 3
	siteRecoveryBudget   = 60 * time.Second
)

type siteRecoveryPath struct {
	proxyMode         model.ProxyUsageMode
	proxyConfigID     *int
	clashControllerID *int
	clashNode         string
	label             string
	diagnostic        string
}

type checkinRecoveryValue struct {
	result      *model.SiteCheckinResult
	accessToken string
}

func syncAccountStateWithRecovery(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
) (*syncSnapshot, error) {
	return runSiteOperationWithRecovery(
		ctx,
		siteRecord,
		account,
		model.SiteOperationSync,
		func(runCtx context.Context, siteCopy *model.Site, accountCopy *model.SiteAccount) (*syncSnapshot, error) {
			return syncAccountState(runCtx, siteCopy, accountCopy)
		},
	)
}

func checkinAccountStateWithRecovery(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
) (*model.SiteCheckinResult, string, error) {
	value, err := runSiteOperationWithRecovery(
		ctx,
		siteRecord,
		account,
		model.SiteOperationCheckin,
		func(runCtx context.Context, siteCopy *model.Site, accountCopy *model.SiteAccount) (*checkinRecoveryValue, error) {
			result, token, err := checkinAccountState(runCtx, siteCopy, accountCopy)
			return &checkinRecoveryValue{result: result, accessToken: token}, err
		},
	)
	if value == nil {
		return nil, "", err
	}
	return value.result, value.accessToken, err
}

func runSiteOperationWithRecovery[T any](
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	operation model.SiteOperationType,
	run func(context.Context, *model.Site, *model.SiteAccount) (T, error),
) (T, error) {
	var zero T
	if siteRecord == nil || account == nil {
		return zero, fmt.Errorf("site or account is nil")
	}
	paths, err := buildSiteRecoveryPaths(ctx, siteRecord, account)
	if err != nil {
		return zero, err
	}
	if len(paths) == 0 {
		paths = []siteRecoveryPath{{proxyMode: model.ProxyUsageModeDirect, label: "direct"}}
	}
	if len(paths) > siteRecoveryMaxPaths {
		paths = paths[:siteRecoveryMaxPaths]
	}

	recoveryCtx, cancel := context.WithTimeout(ctx, siteRecoveryBudget)
	defer cancel()
	operationID := strconv.FormatInt(snowflake.GenerateID(), 10)
	var lastErr error
	var lastValue T
	for index, path := range paths {
		if recoveryCtx.Err() != nil {
			reportSiteRecoveryWriteError(
				siteRecord,
				account,
				operationID,
				"mark budget stop reason",
				markSiteOperationStopReason(
					context.WithoutCancel(ctx),
					operationID,
					"budget_exhausted",
				),
			)
			if lastErr != nil {
				return lastValue, lastErr
			}
			return lastValue, recoveryCtx.Err()
		}
		startedAt := time.Now()
		if path.clashControllerID != nil && path.clashNode != "" {
			if err := op.ClashSwitchNode(recoveryCtx, *path.clashControllerID, path.clashNode); err != nil {
				stopReason := ""
				if index == len(paths)-1 {
					stopReason = "paths_exhausted"
				}
				reportSiteRecoveryWriteError(siteRecord, account, operationID, "record clash switch attempt", recordSiteOperationAttempt(
					context.WithoutCancel(ctx),
					siteRecord,
					account,
					operation,
					operationID,
					index+1,
					path,
					startedAt,
					err,
					stopReason,
				))
				reportSiteRecoveryWriteError(
					siteRecord,
					account,
					operationID,
					"record clash path failure",
					recordSiteRecoveryPathFailure(
						context.WithoutCancel(ctx),
						siteRecord,
						account,
						path,
						startedAt,
						err,
					),
				)
				lastErr = err
				continue
			}
		}

		siteCopy := *siteRecord
		accountCopy := *account
		accountCopy.ProxyMode = path.proxyMode
		accountCopy.ProxyConfigID = cloneInt(path.proxyConfigID)
		accountCopy.PreferredClashNode = path.clashNode
		value, runErr := run(recoveryCtx, &siteCopy, &accountCopy)
		lastValue = value
		if runErr == nil {
			reportSiteRecoveryWriteError(siteRecord, account, operationID, "record successful attempt", recordSiteOperationAttempt(
				context.WithoutCancel(ctx),
				siteRecord,
				account,
				operation,
				operationID,
				index+1,
				path,
				startedAt,
				nil,
				"",
			))
			reportSiteRecoveryWriteError(
				siteRecord,
				account,
				operationID,
				"record path success",
				recordSiteRecoveryPathSuccess(
					context.WithoutCancel(ctx),
					siteRecord,
					account,
					path,
					startedAt,
				),
			)
			reportSiteRecoveryWriteError(
				siteRecord,
				account,
				operationID,
				"learn successful path",
				learnSiteRecoveryPath(recoveryCtx, siteRecord, account, path),
			)
			return value, nil
		}
		lastErr = runErr
		stopReason := ""
		if IsCloudflareProtectionError(runErr) {
			stopReason = "verification_required"
		} else if !siteRecoveryErrorRetryable(runErr) {
			stopReason = "non_retryable"
		} else if index == len(paths)-1 {
			stopReason = "paths_exhausted"
		}
		reportSiteRecoveryWriteError(siteRecord, account, operationID, "record failed attempt", recordSiteOperationAttempt(
			context.WithoutCancel(ctx),
			siteRecord,
			account,
			operation,
			operationID,
			index+1,
			path,
			startedAt,
			runErr,
			stopReason,
		))
		reportSiteRecoveryWriteError(
			siteRecord,
			account,
			operationID,
			"record path failure",
			recordSiteRecoveryPathFailure(
				context.WithoutCancel(ctx),
				siteRecord,
				account,
				path,
				startedAt,
				runErr,
			),
		)
		if IsCloudflareProtectionError(runErr) {
			_, ensureErr := op.VerificationSessionEnsure(context.WithoutCancel(ctx), op.VerificationSessionCreateRequest{
				SiteAccountID: account.ID,
				ProxyConfigID: cloneInt(path.proxyConfigID),
				ClashNode:     path.clashNode,
				Operation:     operation,
			})
			if ensureErr != nil {
				reportSiteRecoveryWriteError(
					siteRecord,
					account,
					operationID,
					"mark verification unavailable",
					markSiteOperationStopReason(
						context.WithoutCancel(ctx),
						operationID,
						"verification_unavailable",
					),
				)
				return value, fmt.Errorf("%w: create verification session: %v", runErr, ensureErr)
			}
			return value, fmt.Errorf("%w: verification session required", runErr)
		}
		if !siteRecoveryErrorRetryable(runErr) {
			return value, runErr
		}
	}
	return lastValue, lastErr
}

func buildSiteRecoveryPaths(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
) ([]siteRecoveryPath, error) {
	enabled := siteRecord.AutoProxyRecovery
	if account.AutoProxyRecovery != nil {
		enabled = *account.AutoProxyRecovery
	}
	currentMode, currentProxyID := resolveSiteAccountProxy(siteRecord, account)
	current := siteRecoveryPath{
		proxyMode:     currentMode,
		proxyConfigID: cloneInt(currentProxyID),
		label:         string(currentMode),
	}
	if !enabled {
		return []siteRecoveryPath{current}, nil
	}

	preferredID := account.PreferredProxyConfigID
	preferredNode := account.PreferredClashNode
	if preferredID == nil {
		preferredID = siteRecord.PreferredProxyConfigID
		preferredNode = siteRecord.PreferredClashNode
	}

	proxies, err := op.ProxyConfigurationList(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]model.ProxyConfiguration, len(proxies))
	for _, proxy := range proxies {
		if proxy.Enabled {
			byID[proxy.ID] = proxy
		}
	}
	preferences, err := op.SiteProxyPreferenceList(ctx, siteRecord.ID, account.ID)
	if err != nil {
		return nil, err
	}
	preferenceByKey := make(map[string]model.SiteProxyPreference, len(preferences))
	for _, preference := range preferences {
		preferenceByKey[preference.IdentityKey] = preference
	}

	paths := make([]siteRecoveryPath, 0, siteRecoveryMaxPaths)
	seen := make(map[string]struct{})
	appendPath := func(path siteRecoveryPath) {
		key := recoveryPathKey(path)
		if _, ok := seen[key]; ok || len(paths) >= siteRecoveryMaxPaths {
			return
		}
		seen[key] = struct{}{}
		paths = append(paths, path)
	}

	if preferredID != nil {
		if proxy, ok := byID[*preferredID]; ok {
			preferredPaths := recoveryPathsForProxy(ctx, proxy, preferredNode)
			for _, path := range preferredPaths {
				if recoveryPathUsable(siteRecord.ID, account.ID, path, preferenceByKey) {
					appendPath(path)
					break
				}
			}
		}
	}
	appendPath(siteRecoveryPath{proxyMode: model.ProxyUsageModeDirect, label: "direct"})
	otherPaths := make([]siteRecoveryPath, 0, len(proxies)*2+1)
	if current.proxyMode != model.ProxyUsageModeDirect {
		otherPaths = append(otherPaths, current)
	}
	for _, proxy := range proxies {
		if !proxy.Enabled {
			continue
		}
		for _, path := range recoveryPathsForProxy(ctx, proxy, "") {
			if recoveryPathUsable(siteRecord.ID, account.ID, path, preferenceByKey) {
				otherPaths = append(otherPaths, path)
			}
		}
	}
	sort.SliceStable(otherPaths, func(i, j int) bool {
		left := recoveryPathScore(siteRecord.ID, account.ID, otherPaths[i], preferenceByKey)
		right := recoveryPathScore(siteRecord.ID, account.ID, otherPaths[j], preferenceByKey)
		if left == right {
			return otherPaths[i].label < otherPaths[j].label
		}
		return left > right
	})
	for _, path := range otherPaths {
		appendPath(path)
	}
	return paths, nil
}

func recoveryPathsForProxy(ctx context.Context, proxy model.ProxyConfiguration, preferredNode string) []siteRecoveryPath {
	proxyID := proxy.ID
	base := siteRecoveryPath{
		proxyMode:     model.ProxyUsageModePool,
		proxyConfigID: &proxyID,
		label:         proxy.Name,
	}
	if proxy.ClashControllerID == nil {
		return []siteRecoveryPath{base}
	}
	controllerID := *proxy.ClashControllerID
	base.clashControllerID = &controllerID
	state, err := op.ClashControllerState(ctx, controllerID)
	if err != nil {
		base.diagnostic = sanitizeSiteStatusText(
			"controller unavailable; using proxy endpoint: " + sanitizeSiteStatusMessage(err),
		)
		return []siteRecoveryPath{base}
	}
	nodes := make([]string, 0, len(state.All))
	appendNode := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range nodes {
			if existing == value {
				return
			}
		}
		nodes = append(nodes, value)
	}
	appendNode(preferredNode)
	appendNode(state.Now)
	for _, node := range state.All {
		appendNode(node)
	}
	result := make([]siteRecoveryPath, 0, len(nodes))
	for _, node := range nodes {
		path := base
		path.clashControllerID = &controllerID
		path.clashNode = node
		path.label = proxy.Name + "/" + node
		result = append(result, path)
	}
	return result
}

func recoveryPathKey(path siteRecoveryPath) string {
	proxyID := 0
	if path.proxyConfigID != nil {
		proxyID = *path.proxyConfigID
	}
	return fmt.Sprintf("%s:%d:%s", path.proxyMode, proxyID, path.clashNode)
}

func recoveryPathDescriptor(
	siteID int,
	accountID int,
	path siteRecoveryPath,
) op.SiteProxyPathDescriptor {
	proxyID := 0
	if path.proxyConfigID != nil {
		proxyID = *path.proxyConfigID
	}
	controllerID := 0
	if path.clashControllerID != nil {
		controllerID = *path.clashControllerID
	}
	return op.SiteProxyPathDescriptor{
		SiteID:            siteID,
		SiteAccountID:     accountID,
		ProxyMode:         path.proxyMode,
		ProxyConfigID:     proxyID,
		ClashControllerID: controllerID,
		ClashNode:         path.clashNode,
	}
}

func recoveryPathPreference(
	siteID int,
	accountID int,
	path siteRecoveryPath,
	preferences map[string]model.SiteProxyPreference,
) (model.SiteProxyPreference, bool) {
	accountDescriptor := recoveryPathDescriptor(siteID, accountID, path)
	if preference, ok := preferences[accountDescriptor.IdentityKey()]; ok {
		return preference, true
	}
	siteDescriptor := recoveryPathDescriptor(siteID, 0, path)
	preference, ok := preferences[siteDescriptor.IdentityKey()]
	return preference, ok
}

func recoveryPathUsable(
	siteID int,
	accountID int,
	path siteRecoveryPath,
	preferences map[string]model.SiteProxyPreference,
) bool {
	preference, ok := recoveryPathPreference(siteID, accountID, path, preferences)
	return !ok || op.SiteProxyPreferenceUsable(preference, time.Now())
}

func recoveryPathScore(
	siteID int,
	accountID int,
	path siteRecoveryPath,
	preferences map[string]model.SiteProxyPreference,
) float64 {
	preference, ok := recoveryPathPreference(siteID, accountID, path, preferences)
	if !ok {
		return 0
	}
	total := preference.SuccessCount + preference.FailureCount
	successRate := float64(0)
	if total > 0 {
		successRate = float64(preference.SuccessCount) / float64(total)
	}
	score := successRate*1000 - preference.AverageLatencyMS/1000
	if preference.SiteAccountID == accountID && accountID > 0 {
		score += 100
	}
	if preference.LastSuccessAt != nil {
		ageHours := time.Since(*preference.LastSuccessAt).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		score += max(0, 50-ageHours)
	}
	return score
}

func recordSiteOperationAttempt(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	operation model.SiteOperationType,
	operationID string,
	attemptNumber int,
	path siteRecoveryPath,
	startedAt time.Time,
	err error,
	stopReason string,
) error {
	entry := model.SiteOperationAttempt{
		SiteID:            siteRecord.ID,
		SiteAccountID:     account.ID,
		Operation:         operation,
		AttemptNumber:     attemptNumber,
		ProxyMode:         path.proxyMode,
		ProxyConfigID:     cloneInt(path.proxyConfigID),
		ClashControllerID: cloneInt(path.clashControllerID),
		ClashNode:         path.clashNode,
		StartedAt:         startedAt,
		DurationMS:        time.Since(startedAt).Milliseconds(),
		Success:           err == nil,
		OperationID:       operationID,
		PathLabel:         path.label,
		StopReason:        stopReason,
		Message:           path.diagnostic,
	}
	if err != nil {
		entry.FailureClass = classifySiteRecoveryError(err)
		failureMessage := sanitizeSiteStatusMessage(err)
		if entry.Message == "" {
			entry.Message = failureMessage
		} else {
			entry.Message = sanitizeSiteStatusText(entry.Message + "; operation failed: " + failureMessage)
		}
	}
	return db.GetDB().WithContext(ctx).Create(&entry).Error
}

func markSiteOperationStopReason(ctx context.Context, operationID, stopReason string) error {
	if operationID == "" || stopReason == "" {
		return nil
	}
	var item model.SiteOperationAttempt
	if err := db.GetDB().WithContext(ctx).Where("operation_id = ?", operationID).
		Order("attempt_number DESC, id DESC").
		First(&item).Error; err != nil {
		return err
	}
	return db.GetDB().WithContext(ctx).Model(&item).
		Update("stop_reason", stopReason).Error
}

func recordSiteRecoveryPathSuccess(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	path siteRecoveryPath,
	startedAt time.Time,
) error {
	scopeAccountID := recoveryPreferenceScopeAccountID(account)
	return op.SiteProxyPreferenceRecordSuccess(
		ctx,
		recoveryPathDescriptor(siteRecord.ID, scopeAccountID, path),
		time.Since(startedAt),
	)
}

func recordSiteRecoveryPathFailure(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	path siteRecoveryPath,
	startedAt time.Time,
	err error,
) error {
	failureClass := classifySiteRecoveryError(err)
	if !recoveryPathFailureAffectsHealth(failureClass) {
		return nil
	}
	scopeAccountID := recoveryPreferenceScopeAccountID(account)
	return op.SiteProxyPreferenceRecordFailure(
		ctx,
		recoveryPathDescriptor(siteRecord.ID, scopeAccountID, path),
		failureClass,
		time.Since(startedAt),
	)
}

func recoveryPathFailureAffectsHealth(failureClass string) bool {
	switch failureClass {
	case "network", "timeout", "cloudflare":
		return true
	default:
		return false
	}
}

func recoveryPreferenceScopeAccountID(account *model.SiteAccount) int {
	if account != nil && (account.PreferredProxyConfigID != nil || strings.TrimSpace(account.PreferredClashNode) != "") {
		return account.ID
	}
	return 0
}

func learnSiteRecoveryPath(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
	path siteRecoveryPath,
) error {
	updates := map[string]any{
		"preferred_proxy_config_id": nil,
		"preferred_clash_node":      "",
	}
	if path.proxyMode == model.ProxyUsageModePool && path.proxyConfigID != nil {
		updates["preferred_proxy_config_id"] = *path.proxyConfigID
		updates["preferred_clash_node"] = path.clashNode
	} else if path.proxyMode != model.ProxyUsageModeDirect &&
		path.proxyMode != model.ProxyUsageModeSystem {
		return nil
	}
	if recoveryPreferenceScopeAccountID(account) > 0 {
		if err := db.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
			Where("id = ?", account.ID).Updates(updates).Error; err != nil {
			return err
		}
		account.PreferredProxyConfigID = cloneInt(path.proxyConfigID)
		if path.proxyMode != model.ProxyUsageModePool {
			account.PreferredProxyConfigID = nil
		}
		account.PreferredClashNode = updates["preferred_clash_node"].(string)
		return nil
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.Site{}).
		Where("id = ?", siteRecord.ID).Updates(updates).Error; err != nil {
		return err
	}
	siteRecord.PreferredProxyConfigID = cloneInt(path.proxyConfigID)
	if path.proxyMode != model.ProxyUsageModePool {
		siteRecord.PreferredProxyConfigID = nil
	}
	siteRecord.PreferredClashNode = updates["preferred_clash_node"].(string)
	return nil
}

func reportSiteRecoveryWriteError(
	siteRecord *model.Site,
	account *model.SiteAccount,
	operationID string,
	action string,
	err error,
) {
	if err == nil {
		return
	}
	siteID := 0
	accountID := 0
	if siteRecord != nil {
		siteID = siteRecord.ID
	}
	if account != nil {
		accountID = account.ID
	}
	log.Warnw(
		"site recovery persistence failed",
		"action", action,
		"site_id", siteID,
		"site_account_id", accountID,
		"operation_id", operationID,
		"error", err,
	)
}

func siteRecoveryErrorRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if IsCloudflareProtectionError(err) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	lowered := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection refused",
		"connection reset",
		"timeout",
		"tls handshake",
		"no such host",
		"network is unreachable",
		"unexpected eof",
		"cloudflare",
	} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	for _, status := range []int{408, 409, 425, 429, 500, 502, 503, 504, 520, 521, 522, 523, 524} {
		if strings.Contains(lowered, strconv.Itoa(status)) {
			return true
		}
	}
	return false
}

func classifySiteRecoveryError(err error) string {
	if err == nil {
		return ""
	}
	if IsCloudflareProtectionError(err) {
		return "cloudflare"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout"
		}
		return "network"
	}
	lowered := strings.ToLower(err.Error())
	if strings.Contains(lowered, "401") || strings.Contains(lowered, "unauthorized") {
		return "authentication"
	}
	if strings.Contains(lowered, "429") {
		return "rate_limit"
	}
	if strings.Contains(lowered, "decode") || strings.Contains(lowered, "html") {
		return "response_format"
	}
	return "upstream"
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func SiteOperationAttemptList(ctx context.Context, accountID int, limit int) ([]model.SiteOperationAttempt, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []model.SiteOperationAttempt
	err := db.GetDB().WithContext(ctx).
		Where("site_account_id = ?", accountID).
		Order("started_at DESC, id DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}
