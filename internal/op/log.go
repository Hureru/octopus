package op

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"gorm.io/gorm"
)

const relayLogMaxSize = 20
const relayLogMaxSizeNoDB = 100 // 当不保存到数据库时，允许更大的缓存用于实时查询

var relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
var relayLogCacheLock sync.Mutex

var relayLogFlushLock sync.Mutex

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

func relayLogFlushToDB(ctx context.Context) error {
	relayLogFlushLock.Lock()
	defer relayLogFlushLock.Unlock()

	relayLogCacheLock.Lock()
	if len(relayLogCache) == 0 {
		relayLogCacheLock.Unlock()
		return nil
	}
	batch := make([]model.RelayLog, len(relayLogCache))
	copy(batch, relayLogCache)
	flushedUpto := len(batch)
	relayLogCacheLock.Unlock()

	result := db.GetDB().WithContext(ctx).Create(&batch)
	if result.Error != nil {
		return result.Error
	}

	relayLogCacheLock.Lock()
	if len(relayLogCache) >= flushedUpto {
		relayLogCache = relayLogCache[flushedUpto:]
	} else {
		relayLogCache = relayLogCache[:0]
	}
	if len(relayLogCache) == 0 {
		relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
	}
	relayLogCacheLock.Unlock()

	return nil
}

func RelayLogAdd(ctx context.Context, relayLog model.RelayLog) error {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return err
	}
	maxSize := relayLogMaxSize
	if !enabled {
		maxSize = relayLogMaxSizeNoDB
	}
	relayLog.ID = snowflake.GenerateID()
	go notifySubscribers(relayLog)

	relayLogCacheLock.Lock()
	relayLogCache = append(relayLogCache, relayLog)
	if len(relayLogCache) >= maxSize {
		if enabled {
			relayLogCacheLock.Unlock()
			return relayLogFlushToDB(ctx)
		}
		// 如果未启用日志保存，移除最旧的日志，保留最新的日志用于实时查询
		keepSize := maxSize / 2
		if len(relayLogCache) > keepSize {
			relayLogCache = relayLogCache[len(relayLogCache)-keepSize:]
		}
	}
	relayLogCacheLock.Unlock()
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
		if err := relayLogFlushToDB(ctx); err != nil {
			return err
		}
		return relayLogCleanup(ctx)
	}

	// 如果未启用日志保存，检查缓存大小，如果超过限制则清理旧日志
	relayLogCacheLock.Lock()
	if len(relayLogCache) > relayLogMaxSizeNoDB {
		keepSize := relayLogMaxSizeNoDB / 2
		relayLogCache = relayLogCache[len(relayLogCache)-keepSize:]
	}
	relayLogCacheLock.Unlock()

	return nil
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
	return db.GetDB().WithContext(ctx).Where("time < ?", cutoffTime).Delete(&model.RelayLog{}).Error
}

type RelayLogStatusFilter string

const (
	RelayLogStatusAll     RelayLogStatusFilter = ""
	RelayLogStatusSuccess RelayLogStatusFilter = "success"
	RelayLogStatusError   RelayLogStatusFilter = "error"
)

type RelayLogListFilter struct {
	StartTime  *int
	EndTime    *int
	ChannelIDs []int
	Status     RelayLogStatusFilter
	Keyword    string
	Page       int
	PageSize   int
}

// RelayLogList 查询日志列表，支持可选的时间范围和渠道ID过滤
// startTime 和 endTime 为 nil 时表示不限制时间范围
// channelIDs 为 nil 或空时表示不限制渠道
func RelayLogList(ctx context.Context, startTime, endTime *int, channelIDs []int, page, pageSize int) ([]model.RelayLog, error) {
	logs, _, err := RelayLogListWithFilter(ctx, RelayLogListFilter{
		StartTime:  startTime,
		EndTime:    endTime,
		ChannelIDs: channelIDs,
		Page:       page,
		PageSize:   pageSize,
	})
	return logs, err
}

// RelayLogListWithFilter 查询日志列表，支持时间、渠道、状态和关键字过滤；返回当前页和总匹配数。
func RelayLogListWithFilter(ctx context.Context, filter RelayLogListFilter) ([]model.RelayLog, int, error) {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, 0, err
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	filter.Keyword = strings.TrimSpace(filter.Keyword)

	hasChannelFilter := len(filter.ChannelIDs) > 0
	var channelSet map[int]struct{}
	if hasChannelFilter {
		channelSet = make(map[int]struct{}, len(filter.ChannelIDs))
		for _, id := range filter.ChannelIDs {
			channelSet[id] = struct{}{}
		}
	}

	keyword := strings.ToLower(filter.Keyword)

	// 获取缓存中符合条件的日志
	relayLogCacheLock.Lock()
	var cachedLogs []model.RelayLog
	for _, relayLog := range relayLogCache {
		if !relayLogMatchesFilter(relayLog, filter, channelSet, keyword) {
			continue
		}
		cachedLogs = append(cachedLogs, relayLog)
	}
	relayLogCacheLock.Unlock()

	// 反转缓存日志顺序（原本新的在末尾，反转后新的在前面，方便分页）
	for i, j := 0, len(cachedLogs)-1; i < j; i, j = i+1, j-1 {
		cachedLogs[i], cachedLogs[j] = cachedLogs[j], cachedLogs[i]
	}

	cacheCount := len(cachedLogs)
	offset := (filter.Page - 1) * filter.PageSize

	var result []model.RelayLog
	total := cacheCount

	// 先从缓存中取（缓存是最新的日志）
	if offset < cacheCount {
		cacheEnd := offset + filter.PageSize
		if cacheEnd > cacheCount {
			cacheEnd = cacheCount
		}
		result = append(result, cachedLogs[offset:cacheEnd]...)
	}

	// 如果启用了日志保存，从数据库读取剩余条目并统计总数
	if enabled {
		var dbCount int64
		countQuery := db.GetDB().WithContext(ctx).Model(&model.RelayLog{})
		countQuery = applyRelayLogDBFilters(countQuery, filter)
		if err := countQuery.Count(&dbCount).Error; err != nil {
			return nil, 0, err
		}
		total += int(dbCount)

		remaining := filter.PageSize - len(result)
		if remaining > 0 {
			dbOffset := 0
			if offset > cacheCount {
				dbOffset = offset - cacheCount
			}

			query := db.GetDB().WithContext(ctx)
			query = applyRelayLogDBFilters(query, filter)

			var dbLogs []model.RelayLog
			if err := query.Order("time DESC").Order("id DESC").Offset(dbOffset).Limit(remaining).Find(&dbLogs).Error; err != nil {
				return nil, 0, err
			}
			result = append(result, dbLogs...)
		}
	}

	return result, total, nil
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
	if filter.Status == RelayLogStatusSuccess && relayLog.Error != "" {
		return false
	}
	if filter.Status == RelayLogStatusError && relayLog.Error == "" {
		return false
	}
	if keyword != "" && !logMatchesKeyword(relayLog, keyword) {
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
		query = query.Where("error = ''")
	} else if filter.Status == RelayLogStatusError {
		query = query.Where("error <> ''")
	}
	keyword := strings.ToLower(strings.TrimSpace(filter.Keyword))
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(
			"LOWER(request_model_name) LIKE ? OR LOWER(actual_model_name) LIKE ? OR LOWER(request_api_key_name) LIKE ? OR LOWER(request_content) LIKE ? OR LOWER(response_content) LIKE ? OR LOWER(error) LIKE ?",
			like, like, like, like, like, like,
		)
	}
	return query
}

// logMatchesChannels 检查日志是否属于指定的渠道集合。
// 仅匹配顶层 ChannelId，保持与 DB 查询 channel_id IN ? 一致，
// 避免缓存与 DB 分页/计数语义偏差。
func logMatchesChannels(log model.RelayLog, channelSet map[int]struct{}) bool {
	_, ok := channelSet[log.ChannelId]
	return ok
}

func logMatchesKeyword(relayLog model.RelayLog, keyword string) bool {
	fields := []string{
		relayLog.RequestModelName,
		relayLog.ActualModelName,
		relayLog.RequestAPIKeyName,
		relayLog.RequestContent,
		relayLog.ResponseContent,
		relayLog.Error,
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), keyword) {
			return true
		}
	}
	return false
}

func RelayLogClear(ctx context.Context) error {
	relayLogCacheLock.Lock()
	relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
	relayLogCacheLock.Unlock()
	return db.GetDB().WithContext(ctx).Where("1 = 1").Delete(&model.RelayLog{}).Error
}
