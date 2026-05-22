package op

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"gorm.io/gorm"
)

const (
	relayLogBatchSize        = 200
	relayLogFlushInterval    = time.Second
	relayLogQueueSize        = 5000
	relayLogQueueBytes       = 64 << 20
	relayLogRecentMaxSize    = 100 // 最近日志缓存，用于实时查询/不落库模式
	relayLogCleanupBatchSize = 1000
	relayLogCleanupBatchWait = 30 * time.Millisecond
	relayLogWriterMaxBatches = 25
)

var relayLogPending = make([]model.RelayLog, 0, relayLogBatchSize)
var relayLogPendingBytes int64
var relayLogPendingLock sync.Mutex

var relayLogRecent = make([]model.RelayLog, 0, relayLogRecentMaxSize)
var relayLogRecentLock sync.Mutex

var relayLogFlushLock sync.Mutex
var relayLogFlushSignal = make(chan struct{}, 1)
var relayLogDroppedTotal atomic.Uint64
var relayLogLastDropWarn atomic.Int64

var relayLogSubscribers = make(map[chan model.RelayLog]struct{})
var relayLogSubscribersLock sync.RWMutex

var relayLogStreamTokens = make(map[string]struct{})
var relayLogStreamTokensLock sync.RWMutex

func RelayLogStreamTokenCreate() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens[token] = struct{}{}
	relayLogStreamTokensLock.Unlock()

	return token, nil
}

func RelayLogStreamTokenVerify(token string) bool {
	relayLogStreamTokensLock.RLock()
	_, ok := relayLogStreamTokens[token]
	relayLogStreamTokensLock.RUnlock()
	return ok
}

func RelayLogStreamTokenRevoke(token string) {
	relayLogStreamTokensLock.Lock()
	delete(relayLogStreamTokens, token)
	relayLogStreamTokensLock.Unlock()
}

func RelayLogSubscribe() chan model.RelayLog {
	ch := make(chan model.RelayLog, 10)
	relayLogSubscribersLock.Lock()
	relayLogSubscribers[ch] = struct{}{}
	relayLogSubscribersLock.Unlock()
	return ch
}

func RelayLogUnsubscribe(ch chan model.RelayLog) {
	relayLogSubscribersLock.Lock()
	delete(relayLogSubscribers, ch)
	relayLogSubscribersLock.Unlock()
	close(ch)
}

func notifySubscribers(relayLog model.RelayLog) {
	relayLogSubscribersLock.RLock()
	defer relayLogSubscribersLock.RUnlock()

	for ch := range relayLogSubscribers {
		select {
		case ch <- relayLog:
		default:
		}
	}
}

// RelayLogWriterRun flushes persisted relay logs from the in-memory queue in
// the background. It wakes either when the queue reaches relayLogBatchSize or
// on relayLogFlushInterval; request goroutines never write relay_logs directly.
func RelayLogWriterRun(ctx context.Context) {
	ticker := time.NewTicker(relayLogFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = RelayLogFlushPending(flushCtx)
			cancel()
			return
		case <-relayLogFlushSignal:
			if err := relayLogDrainPending(ctx, relayLogWriterMaxBatches); err != nil {
				log.Warnw("relay_log.flush_failed", "batch_size", relayLogBatchSize, "queue_length", RelayLogPendingLen(), "error", err.Error())
			}
		case <-ticker.C:
			if err := relayLogDrainPending(ctx, relayLogWriterMaxBatches); err != nil {
				log.Warnw("relay_log.flush_failed", "batch_size", relayLogBatchSize, "queue_length", RelayLogPendingLen(), "error", err.Error())
			}
		}
	}
}

func signalRelayLogFlush() {
	select {
	case relayLogFlushSignal <- struct{}{}:
	default:
	}
}

func appendRelayLogRecent(relayLog model.RelayLog) {
	relayLogRecentLock.Lock()
	relayLogRecent = append(relayLogRecent, relayLog)
	if len(relayLogRecent) > relayLogRecentMaxSize {
		keep := relayLogRecentMaxSize / 2
		relayLogRecent = append([]model.RelayLog(nil), relayLogRecent[len(relayLogRecent)-keep:]...)
	}
	relayLogRecentLock.Unlock()
}

func enqueueRelayLogPending(relayLog model.RelayLog) bool {
	estimatedBytes := relayLogApproxBytes(relayLog)
	relayLogPendingLock.Lock()
	defer relayLogPendingLock.Unlock()
	if len(relayLogPending) >= relayLogQueueSize || relayLogPendingBytes+estimatedBytes > relayLogQueueBytes {
		dropped := relayLogDroppedTotal.Add(1)
		warnRelayLogDropped(dropped)
		return false
	}
	relayLogPending = append(relayLogPending, relayLog)
	relayLogPendingBytes += estimatedBytes
	if len(relayLogPending) >= relayLogBatchSize {
		signalRelayLogFlush()
	}
	return true
}

func relayLogApproxBytes(relayLog model.RelayLog) int64 {
	size := 256
	size += len(relayLog.RequestModelName) + len(relayLog.RequestAPIKeyName) + len(relayLog.ChannelName) + len(relayLog.ActualModelName)
	size += len(relayLog.RequestContent) + len(relayLog.ResponseContent) + len(relayLog.Error)
	for _, attempt := range relayLog.Attempts {
		size += 96 + len(attempt.ChannelName) + len(attempt.ModelName) + len(attempt.Msg)
	}
	return int64(size)
}

func warnRelayLogDropped(dropped uint64) {
	now := time.Now().Unix()
	last := relayLogLastDropWarn.Load()
	if now-last < 60 {
		return
	}
	if relayLogLastDropWarn.CompareAndSwap(last, now) {
		log.Warnw("relay_log.queue_full", "dropped_total", dropped, "queue_size", relayLogQueueSize, "queue_bytes", relayLogQueueBytes)
	}
}

func RelayLogPendingLen() int {
	relayLogPendingLock.Lock()
	defer relayLogPendingLock.Unlock()
	return len(relayLogPending)
}

func RelayLogDroppedTotal() uint64 {
	return relayLogDroppedTotal.Load()
}

func relayLogDrainPending(ctx context.Context, maxBatches int) error {
	if maxBatches <= 0 {
		maxBatches = 1
	}
	for i := 0; i < maxBatches; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if RelayLogPendingLen() == 0 {
			return nil
		}
		if err := relayLogFlushPendingBatch(ctx, relayLogBatchSize); err != nil {
			return err
		}
	}
	if RelayLogPendingLen() > 0 {
		signalRelayLogFlush()
	}
	return nil
}

func relayLogFlushPendingBatch(ctx context.Context, batchSize int) error {
	if batchSize <= 0 {
		batchSize = relayLogBatchSize
	}
	relayLogFlushLock.Lock()
	defer relayLogFlushLock.Unlock()

	relayLogPendingLock.Lock()
	if len(relayLogPending) == 0 {
		relayLogPendingLock.Unlock()
		return nil
	}
	if batchSize > len(relayLogPending) {
		batchSize = len(relayLogPending)
	}
	batch := make([]model.RelayLog, batchSize)
	copy(batch, relayLogPending[:batchSize])
	batchBytes := relayLogBatchApproxBytes(batch)
	relayLogPendingLock.Unlock()

	start := time.Now()
	result := db.GetDB().WithContext(ctx).CreateInBatches(&batch, relayLogBatchSize)
	if result.Error != nil {
		return result.Error
	}
	duration := time.Since(start)
	log.Debugw("relay_log.flush", "batch_size", len(batch), "duration", duration.String(), "queue_length", RelayLogPendingLen())

	relayLogPendingLock.Lock()
	if len(relayLogPending) >= batchSize && relayLogPending[0].ID == batch[0].ID && relayLogPending[batchSize-1].ID == batch[batchSize-1].ID {
		relayLogPending = relayLogPending[batchSize:]
		relayLogPendingBytes -= batchBytes
	} else {
		flushed := make(map[int64]struct{}, len(batch))
		for _, item := range batch {
			flushed[item.ID] = struct{}{}
		}
		kept := relayLogPending[:0]
		keptBytes := int64(0)
		for _, item := range relayLogPending {
			if _, ok := flushed[item.ID]; !ok {
				kept = append(kept, item)
				keptBytes += relayLogApproxBytes(item)
			}
		}
		relayLogPending = kept
		relayLogPendingBytes = keptBytes
	}
	if relayLogPendingBytes < 0 {
		relayLogPendingBytes = 0
	}
	if len(relayLogPending) == 0 {
		relayLogPending = make([]model.RelayLog, 0, relayLogBatchSize)
		relayLogPendingBytes = 0
	}
	relayLogPendingLock.Unlock()

	return nil
}

func relayLogBatchApproxBytes(batch []model.RelayLog) int64 {
	var total int64
	for _, item := range batch {
		total += relayLogApproxBytes(item)
	}
	return total
}

func RelayLogFlushPending(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if RelayLogPendingLen() == 0 {
			return nil
		}
		if err := relayLogFlushPendingBatch(ctx, relayLogBatchSize); err != nil {
			return err
		}
	}
}

func RelayLogAdd(ctx context.Context, relayLog model.RelayLog) error {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return err
	}
	relayLog.ID = snowflake.GenerateID()
	notifySubscribers(relayLog)
	appendRelayLogRecent(relayLog)

	if !enabled {
		return nil
	}
	enqueueRelayLogPending(relayLog)
	_ = ctx // kept for API compatibility; DB writes are handled by the background writer.
	return nil
}

func RelayLogSaveDBTask(ctx context.Context) error {
	log.Debugf("relay log save db task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("relay log save db task finished, save time: %s", time.Since(startTime))
	}()
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return err
	}

	if enabled {
		if err := RelayLogFlushPending(ctx); err != nil {
			return err
		}
		return relayLogCleanup(ctx)
	}

	trimRelayLogRecent()
	return nil
}

func trimRelayLogRecent() {
	relayLogRecentLock.Lock()
	if len(relayLogRecent) > relayLogRecentMaxSize {
		keepSize := relayLogRecentMaxSize / 2
		relayLogRecent = append([]model.RelayLog(nil), relayLogRecent[len(relayLogRecent)-keepSize:]...)
	}
	relayLogRecentLock.Unlock()
}

func relayLogCleanup(ctx context.Context) error {
	keepPeriod, err := SettingGetInt(model.SettingKeyRelayLogKeepPeriod)
	if err != nil {
		return err
	}

	if keepPeriod <= 0 {
		return nil
	}

	cutoffTime := time.Now().Add(-time.Duration(keepPeriod) * 24 * time.Hour).Unix()
	start := time.Now()
	deletedRows := int64(0)
	batchCount := 0
	dbConn := db.GetDB().WithContext(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var ids []int64
		if err := dbConn.Model(&model.RelayLog{}).
			Where("time < ?", cutoffTime).
			Order("time ASC").
			Order("id ASC").
			Limit(relayLogCleanupBatchSize).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		result := dbConn.Where("id IN ?", ids).Unscoped().Delete(&model.RelayLog{})
		if result.Error != nil {
			return result.Error
		}
		deletedRows += result.RowsAffected
		batchCount++
		if len(ids) < relayLogCleanupBatchSize {
			break
		}
		if relayLogCleanupBatchWait > 0 {
			timer := time.NewTimer(relayLogCleanupBatchWait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if deletedRows > 0 {
		log.Debugw("relay_log.cleanup", "deleted_rows", deletedRows, "batch_count", batchCount, "duration", time.Since(start).String())
	}
	return nil
}

type RelayLogStatusFilter string

const (
	RelayLogStatusAll     RelayLogStatusFilter = ""
	RelayLogStatusSuccess RelayLogStatusFilter = "success"
	RelayLogStatusError   RelayLogStatusFilter = "error"
)

type RelayLogKeywordScope string

const (
	RelayLogKeywordScopeDefault RelayLogKeywordScope = ""
	RelayLogKeywordScopeContent RelayLogKeywordScope = "content"
)

type RelayLogCursor struct {
	Time int64 `json:"time"`
	ID   int64 `json:"id"`
}

type RelayLogKeywordMode string

const (
	RelayLogKeywordModeDefault  RelayLogKeywordMode = ""
	RelayLogKeywordModePrefix   RelayLogKeywordMode = "prefix"
	RelayLogKeywordModeExact    RelayLogKeywordMode = "exact"
	RelayLogKeywordModeContains RelayLogKeywordMode = "contains"
)

type RelayLogListFilter struct {
	StartTime      *int
	EndTime        *int
	ChannelIDs     []int
	Status         RelayLogStatusFilter
	Keyword        string
	KeywordScope   RelayLogKeywordScope
	KeywordMode    RelayLogKeywordMode
	Page           int
	PageSize       int
	IncludeContent bool
	WithTotal      bool
	Limit          int
	BeforeTime     *int64
	BeforeID       *int64
	// Pagination forces cursor or page mode. Empty defers to cursor when
	// limit/cursor fields are set, otherwise page mode.
	Pagination string
}

type RelayLogListResult struct {
	Logs       []model.RelayLog `json:"logs"`
	Total      int              `json:"total"`
	HasMore    bool             `json:"has_more"`
	NextCursor *RelayLogCursor  `json:"next_cursor,omitempty"`
	SearchMode string           `json:"search_mode,omitempty"`
	Warning    string           `json:"warning,omitempty"`
}

const (
	relayLogKeywordContainsMinLen     = 3
	relayLogKeywordContainsMaxWindow  = int64(7 * 24 * 60 * 60)
	relayLogKeywordContainsDefaultWin = int64(24 * 60 * 60)
)

// ErrRelayLogContainsKeywordTooShort signals that a contains-mode keyword does
// not meet the minimum length requirement enforced by the backend.
var (
	ErrRelayLogContainsKeywordTooShort = &RelayLogFilterError{Code: "keyword_too_short", Message: "contains search requires keyword of at least 3 characters"}
	ErrRelayLogContainsWindowMissing   = &RelayLogFilterError{Code: "time_window_required", Message: "contains search requires an explicit time range"}
	ErrRelayLogContainsWindowTooWide   = &RelayLogFilterError{Code: "time_window_too_wide", Message: "contains search time window must be at most 7 days"}
)

type RelayLogFilterError struct {
	Code    string
	Message string
}

func (e *RelayLogFilterError) Error() string { return e.Message }

// RelayLogList 查询日志列表，支持可选的时间范围和渠道ID过滤
// startTime 和 endTime 为 nil 时表示不限制时间范围
// channelIDs 为 nil 或空时表示不限制渠道
func RelayLogList(ctx context.Context, startTime, endTime *int, channelIDs []int, page, pageSize int) ([]model.RelayLog, error) {
	result, err := RelayLogListWithFilter(ctx, RelayLogListFilter{
		StartTime:      startTime,
		EndTime:        endTime,
		ChannelIDs:     channelIDs,
		Page:           page,
		PageSize:       pageSize,
		IncludeContent: true,
		WithTotal:      true,
	})
	return result.Logs, err
}

// RelayLogListWithFilter 查询日志列表，支持时间、渠道、状态、关键字和 cursor 过滤。
func RelayLogListWithFilter(ctx context.Context, filter RelayLogListFilter) (RelayLogListResult, error) {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return RelayLogListResult{}, err
	}

	cursorMode := filter.BeforeTime != nil || filter.BeforeID != nil || filter.Limit > 0
	switch filter.Pagination {
	case "cursor":
		cursorMode = true
	case "page":
		cursorMode = false
	}
	if filter.Limit < 0 || filter.Limit > 100 {
		filter.Limit = 20
	}
	if filter.Limit == 0 {
		filter.Limit = filter.PageSize
	}
	if filter.Limit < 1 {
		filter.Limit = 20
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	filter.Keyword = strings.TrimSpace(filter.Keyword)

	// Resolve effective keyword mode and apply guardrails for slow contains
	// search before any DB work.
	resolvedMode, warning, err := resolveRelayLogKeywordMode(&filter)
	if err != nil {
		return RelayLogListResult{}, err
	}
	filter.KeywordMode = resolvedMode

	hasChannelFilter := len(filter.ChannelIDs) > 0
	var channelSet map[int]struct{}
	if hasChannelFilter {
		channelSet = make(map[int]struct{}, len(filter.ChannelIDs))
		for _, id := range filter.ChannelIDs {
			channelSet[id] = struct{}{}
		}
	}

	keyword := strings.ToLower(filter.Keyword)

	cachedLogs := relayLogCachedMatches(filter, channelSet, keyword, enabled, !filter.IncludeContent)

	cacheCount := len(cachedLogs)
	offset := (filter.Page - 1) * filter.PageSize

	if cursorMode {
		result, err := relayLogListCursor(ctx, filter, cachedLogs, enabled)
		if err != nil {
			return RelayLogListResult{}, err
		}
		result.SearchMode = relayLogSearchMode(filter)
		result.Warning = warning
		return result, nil
	}

	var logs []model.RelayLog
	total := 0
	if filter.WithTotal {
		total = cacheCount
	}

	// 先从未落库 pending 中取（pending 是最新日志）；已落库日志从 DB 取，避免重复分页。
	if offset < cacheCount {
		cacheEnd := offset + filter.PageSize
		if cacheEnd > cacheCount {
			cacheEnd = cacheCount
		}
		logs = append(logs, cachedLogs[offset:cacheEnd]...)
	}

	// 如果启用了日志保存，从数据库读取剩余条目；仅按需统计总数。
	if enabled {
		if filter.WithTotal {
			var dbCount int64
			countQuery := db.GetDB().WithContext(ctx).Model(&model.RelayLog{})
			countQuery = applyRelayLogDBFilters(countQuery, filter)
			if err := countQuery.Count(&dbCount).Error; err != nil {
				return RelayLogListResult{}, err
			}
			total += int(dbCount)
		}

		remaining := filter.PageSize - len(logs)
		if remaining > 0 {
			dbOffset := 0
			if offset > cacheCount {
				dbOffset = offset - cacheCount
			}

			query := db.GetDB().WithContext(ctx)
			query = applyRelayLogDBFilters(query, filter)
			query = selectRelayLogListFields(query, filter.IncludeContent)

			var dbLogs []model.RelayLog
			if err := query.Order("time DESC").Order("id DESC").Offset(dbOffset).Limit(remaining).Find(&dbLogs).Error; err != nil {
				return RelayLogListResult{}, err
			}
			logs = appendDedupedByID(logs, cachedLogs, dbLogs)
		}
	}

	return RelayLogListResult{Logs: logs, Total: total, SearchMode: relayLogSearchMode(filter), Warning: warning}, nil
}

func relayLogListCursor(ctx context.Context, filter RelayLogListFilter, cachedLogs []model.RelayLog, enabled bool) (RelayLogListResult, error) {
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	logs := make([]model.RelayLog, 0, limit)
	for _, entry := range cachedLogs {
		if !relayLogBeforeCursor(entry, filter.BeforeTime, filter.BeforeID) {
			continue
		}
		logs = append(logs, entry)
		if len(logs) >= limit+1 {
			break
		}
	}

	if enabled && len(logs) < limit+1 {
		remaining := limit + 1 - len(logs)
		query := db.GetDB().WithContext(ctx)
		query = applyRelayLogDBFilters(query, filter)
		query = applyRelayLogCursor(query, filter.BeforeTime, filter.BeforeID)
		query = selectRelayLogListFields(query, filter.IncludeContent)

		var dbLogs []model.RelayLog
		if err := query.Order("time DESC").Order("id DESC").Limit(remaining).Find(&dbLogs).Error; err != nil {
			return RelayLogListResult{}, err
		}
		logs = appendDedupedByID(logs, cachedLogs, dbLogs)
	}

	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}
	var nextCursor *RelayLogCursor
	if hasMore && len(logs) > 0 {
		last := logs[len(logs)-1]
		nextCursor = &RelayLogCursor{Time: last.Time, ID: last.ID}
	}
	return RelayLogListResult{Logs: logs, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func relayLogBeforeCursor(entry model.RelayLog, beforeTime *int64, beforeID *int64) bool {
	if beforeTime == nil && beforeID == nil {
		return true
	}
	if beforeTime != nil && beforeID != nil {
		return entry.Time < *beforeTime || (entry.Time == *beforeTime && entry.ID < *beforeID)
	}
	if beforeTime != nil {
		return entry.Time < *beforeTime
	}
	return beforeID == nil || entry.ID < *beforeID
}

func applyRelayLogCursor(query *gorm.DB, beforeTime *int64, beforeID *int64) *gorm.DB {
	if beforeTime != nil && beforeID != nil {
		return query.Where("time < ? OR (time = ? AND id < ?)", *beforeTime, *beforeTime, *beforeID)
	}
	if beforeTime != nil {
		return query.Where("time < ?", *beforeTime)
	}
	if beforeID != nil {
		return query.Where("id < ?", *beforeID)
	}
	return query
}

func selectRelayLogListFields(query *gorm.DB, includeContent bool) *gorm.DB {
	if includeContent {
		return query
	}
	return query.Select(
		"id",
		"time",
		"request_model_name",
		"request_api_key_name",
		"channel_id",
		"channel_name",
		"actual_model_name",
		"input_tokens",
		"transport_input_tokens",
		"bill_input_tokens",
		"cache_read_tokens",
		"cache_write_tokens",
		"output_tokens",
		"ftut",
		"use_time",
		"cost",
		"error",
		"success",
		"attempts",
		"total_attempts",
		"used_ws",
		"ws_mode",
		"ws_exec_mode",
		"ws_recovery",
	)
}

// appendDedupedByID appends dbLogs to logs, skipping any entry whose ID is
// already present in cachedSource. The cache snapshot and DB read are not
// transactionally consistent — a batch flushed between the two reads could
// otherwise surface in both, producing duplicate rows by ID.
func appendDedupedByID(logs []model.RelayLog, cachedSource []model.RelayLog, dbLogs []model.RelayLog) []model.RelayLog {
	if len(dbLogs) == 0 {
		return logs
	}
	if len(cachedSource) == 0 {
		return append(logs, dbLogs...)
	}
	seen := make(map[int64]struct{}, len(cachedSource))
	for _, entry := range cachedSource {
		seen[entry.ID] = struct{}{}
	}
	for _, entry := range dbLogs {
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		logs = append(logs, entry)
	}
	return logs
}

func RelayLogGet(ctx context.Context, id int64) (*model.RelayLog, error) {
	if item, ok := relayLogFindPending(id); ok {
		return &item, nil
	}
	if item, ok := relayLogFindRecent(id); ok {
		return &item, nil
	}
	var entry model.RelayLog
	if err := db.GetDB().WithContext(ctx).First(&entry, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func relayLogCachedMatches(filter RelayLogListFilter, channelSet map[int]struct{}, keyword string, enabled bool, light bool) []model.RelayLog {
	if enabled {
		return relayLogCollectPending(filter, channelSet, keyword, light)
	}
	return relayLogCollectRecent(filter, channelSet, keyword, light)
}

func relayLogCollectPending(filter RelayLogListFilter, channelSet map[int]struct{}, keyword string, light bool) []model.RelayLog {
	relayLogPendingLock.Lock()
	defer relayLogPendingLock.Unlock()
	result := make([]model.RelayLog, 0, min(len(relayLogPending), filter.PageSize+filter.Limit+1))
	for i := len(relayLogPending) - 1; i >= 0; i-- {
		entry := relayLogPending[i]
		if !relayLogMatchesFilter(entry, filter, channelSet, keyword) {
			continue
		}
		if light {
			entry = relayLogLightCopy(entry)
		}
		result = append(result, entry)
	}
	return result
}

func relayLogCollectRecent(filter RelayLogListFilter, channelSet map[int]struct{}, keyword string, light bool) []model.RelayLog {
	relayLogRecentLock.Lock()
	defer relayLogRecentLock.Unlock()
	result := make([]model.RelayLog, 0, min(len(relayLogRecent), filter.PageSize+filter.Limit+1))
	for i := len(relayLogRecent) - 1; i >= 0; i-- {
		entry := relayLogRecent[i]
		if !relayLogMatchesFilter(entry, filter, channelSet, keyword) {
			continue
		}
		if light {
			entry = relayLogLightCopy(entry)
		}
		result = append(result, entry)
	}
	return result
}

func relayLogFindPending(id int64) (model.RelayLog, bool) {
	relayLogPendingLock.Lock()
	defer relayLogPendingLock.Unlock()
	for i := len(relayLogPending) - 1; i >= 0; i-- {
		if relayLogPending[i].ID == id {
			return relayLogPending[i], true
		}
	}
	return model.RelayLog{}, false
}

func relayLogFindRecent(id int64) (model.RelayLog, bool) {
	relayLogRecentLock.Lock()
	defer relayLogRecentLock.Unlock()
	for i := len(relayLogRecent) - 1; i >= 0; i-- {
		if relayLogRecent[i].ID == id {
			return relayLogRecent[i], true
		}
	}
	return model.RelayLog{}, false
}

func relayLogLightCopy(entry model.RelayLog) model.RelayLog {
	entry.RequestContent = ""
	entry.ResponseContent = ""
	return entry
}

func relayLogMatchesFilter(relayLog model.RelayLog, filter RelayLogListFilter, channelSet map[int]struct{}, keyword string) bool {
	if filter.StartTime != nil && relayLog.Time < int64(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && relayLog.Time > int64(*filter.EndTime) {
		return false
	}
	if len(channelSet) > 0 && !logMatchesChannels(relayLog, channelSet) {
		return false
	}
	if filter.Status == RelayLogStatusSuccess && !relayLog.Success {
		return false
	}
	if filter.Status == RelayLogStatusError && relayLog.Success {
		return false
	}
	if keyword != "" && !logMatchesKeyword(relayLog, keyword, filter.KeywordScope, filter.KeywordMode) {
		return false
	}
	return true
}

func applyRelayLogDBFilters(query *gorm.DB, filter RelayLogListFilter) *gorm.DB {
	if filter.StartTime != nil {
		query = query.Where("time >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("time <= ?", *filter.EndTime)
	}
	if len(filter.ChannelIDs) > 0 {
		query = query.Where("channel_id IN ?", filter.ChannelIDs)
	}
	if filter.Status == RelayLogStatusSuccess {
		query = query.Where("success = ?", true)
	} else if filter.Status == RelayLogStatusError {
		query = query.Where("success = ?", false)
	}
	keyword := strings.ToLower(strings.TrimSpace(filter.Keyword))
	if keyword == "" {
		return query
	}
	switch filter.KeywordMode {
	case RelayLogKeywordModeExact:
		query = query.Where(
			"LOWER(request_model_name) = ? OR LOWER(actual_model_name) = ? OR LOWER(request_api_key_name) = ? OR LOWER(channel_name) = ?",
			keyword, keyword, keyword, keyword,
		)
	case RelayLogKeywordModeContains:
		escaped := escapeLikeKeyword(keyword)
		like := "%" + escaped + "%"
		if filter.KeywordScope == RelayLogKeywordScopeContent {
			query = query.Where(
				"LOWER(request_model_name) LIKE ? ESCAPE '#' OR LOWER(actual_model_name) LIKE ? ESCAPE '#' OR LOWER(request_api_key_name) LIKE ? ESCAPE '#' OR LOWER(channel_name) LIKE ? ESCAPE '#' OR LOWER(request_content) LIKE ? ESCAPE '#' OR LOWER(response_content) LIKE ? ESCAPE '#' OR LOWER(error) LIKE ? ESCAPE '#'",
				like, like, like, like, like, like, like,
			)
		} else {
			query = query.Where(
				"LOWER(request_model_name) LIKE ? ESCAPE '#' OR LOWER(actual_model_name) LIKE ? ESCAPE '#' OR LOWER(request_api_key_name) LIKE ? ESCAPE '#' OR LOWER(channel_name) LIKE ? ESCAPE '#' OR LOWER(error) LIKE ? ESCAPE '#'",
				like, like, like, like, like,
			)
		}
	default:
		// prefix is the default fast path: anchored LIKE 'kw%' can leverage
		// indexes where available, and avoids the worst leading-wildcard scans.
		like := escapeLikeKeyword(keyword) + "%"
		query = query.Where(
			"LOWER(request_model_name) LIKE ? ESCAPE '#' OR LOWER(actual_model_name) LIKE ? ESCAPE '#' OR LOWER(request_api_key_name) LIKE ? ESCAPE '#' OR LOWER(channel_name) LIKE ? ESCAPE '#'",
			like, like, like, like,
		)
	}
	return query
}

// escapeLikeKeyword escapes SQL LIKE wildcards (and the escape char itself) so
// callers can match user input literally. Pair with `ESCAPE '#'` in the LIKE
// clause. A non-special ASCII char is used so the same SQL parses identically
// across SQLite, MySQL, and PostgreSQL string literals.
func escapeLikeKeyword(s string) string {
	s = strings.ReplaceAll(s, "#", "##")
	s = strings.ReplaceAll(s, "%", "#%")
	s = strings.ReplaceAll(s, "_", "#_")
	return s
}

// logMatchesChannels 检查日志是否属于指定的渠道集合。
// 仅匹配顶层 ChannelId，保持与 DB 查询 channel_id IN ? 一致，
// 避免缓存与 DB 分页/计数语义偏差。
func logMatchesChannels(log model.RelayLog, channelSet map[int]struct{}) bool {
	_, ok := channelSet[log.ChannelId]
	return ok
}

func logMatchesKeyword(relayLog model.RelayLog, keyword string, scope RelayLogKeywordScope, mode RelayLogKeywordMode) bool {
	fields := []string{
		relayLog.RequestModelName,
		relayLog.ActualModelName,
		relayLog.RequestAPIKeyName,
		relayLog.ChannelName,
	}
	if mode == RelayLogKeywordModeContains {
		fields = append(fields, relayLog.Error)
		if scope == RelayLogKeywordScopeContent {
			fields = append(fields, relayLog.RequestContent, relayLog.ResponseContent)
		}
	}
	for _, field := range fields {
		lower := strings.ToLower(field)
		switch mode {
		case RelayLogKeywordModeExact:
			if lower == keyword {
				return true
			}
		case RelayLogKeywordModeContains:
			if strings.Contains(lower, keyword) {
				return true
			}
		default:
			if strings.HasPrefix(lower, keyword) {
				return true
			}
		}
	}
	return false
}

// resolveRelayLogKeywordMode validates contains-mode constraints and returns
// the effective mode. Empty keyword always resolves to prefix to keep behavior
// stable for callers that don't care about mode.
func resolveRelayLogKeywordMode(filter *RelayLogListFilter) (RelayLogKeywordMode, string, error) {
	if filter.Keyword == "" {
		return RelayLogKeywordModeDefault, "", nil
	}
	mode := filter.KeywordMode
	if filter.KeywordScope == RelayLogKeywordScopeContent {
		// Content scope only makes sense with contains semantics.
		mode = RelayLogKeywordModeContains
	}
	switch mode {
	case RelayLogKeywordModePrefix, RelayLogKeywordModeExact, RelayLogKeywordModeDefault:
		if mode == RelayLogKeywordModeDefault {
			mode = RelayLogKeywordModePrefix
		}
		return mode, "", nil
	case RelayLogKeywordModeContains:
		if len([]rune(filter.Keyword)) < relayLogKeywordContainsMinLen {
			return mode, "", ErrRelayLogContainsKeywordTooShort
		}
		now := time.Now().Unix()
		warning := ""
		if filter.StartTime == nil && filter.EndTime == nil {
			// Apply a default 24h window rather than reject outright; surface
			// a warning so the UI can show it.
			start := int(now - relayLogKeywordContainsDefaultWin)
			filter.StartTime = &start
			warning = "applied default 24h time window for contains search"
		} else {
			end := now
			if filter.EndTime != nil {
				end = int64(*filter.EndTime)
			}
			var start int64
			if filter.StartTime != nil {
				start = int64(*filter.StartTime)
			} else {
				// EndTime set but StartTime not: anchor the window to EndTime
				// so end-only queries stay within the contains-search budget.
				start = end - relayLogKeywordContainsMaxWindow
				if start < 0 {
					start = 0
				}
				startInt := int(start)
				filter.StartTime = &startInt
			}
			if end-start > relayLogKeywordContainsMaxWindow {
				return mode, "", ErrRelayLogContainsWindowTooWide
			}
		}
		return mode, warning, nil
	default:
		return RelayLogKeywordModePrefix, "", nil
	}
}

func relayLogSearchMode(filter RelayLogListFilter) string {
	if filter.Keyword == "" {
		return ""
	}
	if filter.KeywordMode == RelayLogKeywordModeContains {
		return "slow"
	}
	return "fast"
}

func RelayLogClear(ctx context.Context) error {
	relayLogFlushLock.Lock()
	defer relayLogFlushLock.Unlock()

	relayLogPendingLock.Lock()
	relayLogPending = make([]model.RelayLog, 0, relayLogBatchSize)
	relayLogPendingBytes = 0
	relayLogPendingLock.Unlock()

	relayLogRecentLock.Lock()
	relayLogRecent = make([]model.RelayLog, 0, relayLogRecentMaxSize)
	relayLogRecentLock.Unlock()

	start := time.Now()
	deletedRows := int64(0)
	batchCount := 0
	dbConn := db.GetDB().WithContext(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var ids []int64
		if err := dbConn.Model(&model.RelayLog{}).
			Order("time ASC").
			Order("id ASC").
			Limit(relayLogCleanupBatchSize).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		result := dbConn.Where("id IN ?", ids).Unscoped().Delete(&model.RelayLog{})
		if result.Error != nil {
			return result.Error
		}
		deletedRows += result.RowsAffected
		batchCount++
		if len(ids) < relayLogCleanupBatchSize {
			break
		}
		if relayLogCleanupBatchWait > 0 {
			timer := time.NewTimer(relayLogCleanupBatchWait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if deletedRows > 0 {
		log.Debugw("relay_log.clear", "deleted_rows", deletedRows, "batch_count", batchCount, "duration", time.Since(start).String())
	}
	return nil
}
