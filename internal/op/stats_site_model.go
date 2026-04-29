package op

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// channelSiteBinding 是 channelID → 站点账号绑定的简化形式。
type channelSiteBinding struct {
	SiteAccountID int
	BaseGroupKey  string
	Found         bool
}

// 站点渠道绑定缓存，懒加载，正向命中永久持有；负向命中也缓存，避免每次请求都查 DB。
// 由于 SiteChannelBinding 的 channel_id 是 uniqueIndex，迁移场景下绑定基本不会重映射，
// 在站点账号删除时会调用 invalidateSiteBindingCache 清理。
var siteBindingByChannelCache = cache.New[int, channelSiteBinding](16)

// 桶级缓存：以小时桶为粒度累加，由后台任务批量持久化。
type siteModelHourlyKey struct {
	Hour          int
	SiteAccountID int
	GroupKey      string
	ModelName     string
}

var siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
var siteModelHourlyCacheLock sync.Mutex

// StatsSiteModelHourlyUpdate 记录一次站点渠道请求到对应小时桶。
// 非站点渠道（无绑定）会被静默忽略。
func StatsSiteModelHourlyUpdate(channelID int, actualModel string, metrics model.StatsMetrics) {
	actualModel = strings.TrimSpace(actualModel)
	if channelID == 0 || actualModel == "" {
		return
	}

	binding, err := lookupChannelSiteBinding(channelID)
	if err != nil || !binding.Found {
		return
	}

	now := time.Now()
	hour := int(now.Unix() / 3600)
	date := now.Format("20060102")

	key := siteModelHourlyKey{
		Hour:          hour,
		SiteAccountID: binding.SiteAccountID,
		GroupKey:      binding.BaseGroupKey,
		ModelName:     actualModel,
	}

	siteModelHourlyCacheLock.Lock()
	defer siteModelHourlyCacheLock.Unlock()
	entry, ok := siteModelHourlyCache[key]
	if !ok {
		entry = &model.StatsSiteModelHourly{
			Hour:          hour,
			SiteAccountID: binding.SiteAccountID,
			GroupKey:      binding.BaseGroupKey,
			ModelName:     actualModel,
			Date:          date,
		}
		siteModelHourlyCache[key] = entry
	}
	entry.StatsMetrics.Add(metrics)
}

// StatsSiteModelHourlyRecordAttempts 把一次 relay 中所有 success/failed attempts
// 按 (channel, attempt.modelName) 维度记录到小时桶。仅累加 request_success/request_failed，
// 与现有 site_channel 历史计数语义一致；token/cost 等不在此处累加（已由全局 stats 处理）。
func StatsSiteModelHourlyRecordAttempts(attempts []model.ChannelAttempt, fallbackModel string) {
	for _, attempt := range attempts {
		if attempt.ChannelID == 0 {
			continue
		}
		if attempt.Status != model.AttemptSuccess && attempt.Status != model.AttemptFailed {
			continue
		}
		modelName := strings.TrimSpace(attempt.ModelName)
		if modelName == "" {
			modelName = strings.TrimSpace(fallbackModel)
		}
		if modelName == "" {
			continue
		}
		var metrics model.StatsMetrics
		if attempt.Status == model.AttemptSuccess {
			metrics.RequestSuccess = 1
		} else {
			metrics.RequestFailed = 1
		}
		StatsSiteModelHourlyUpdate(attempt.ChannelID, modelName, metrics)
	}
}

// StatsSiteModelHourlySaveDB 把内存桶批量 upsert 入库。
// 由 stats 后台任务调用。
func StatsSiteModelHourlySaveDB(ctx context.Context) error {
	siteModelHourlyCacheLock.Lock()
	if len(siteModelHourlyCache) == 0 {
		siteModelHourlyCacheLock.Unlock()
		return nil
	}
	rows := make([]model.StatsSiteModelHourly, 0, len(siteModelHourlyCache))
	for _, entry := range siteModelHourlyCache {
		rows = append(rows, *entry)
	}
	siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
	siteModelHourlyCacheLock.Unlock()

	dbConn := db.GetDB().WithContext(ctx)
	return dbConn.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "hour"}, {Name: "site_account_id"}, {Name: "group_key"}, {Name: "model_name"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"date":            clause.Column{Name: "date"},
			"input_token":     gorm.Expr("stats_site_model_hourlies.input_token + EXCLUDED.input_token"),
			"output_token":    gorm.Expr("stats_site_model_hourlies.output_token + EXCLUDED.output_token"),
			"input_cost":      gorm.Expr("stats_site_model_hourlies.input_cost + EXCLUDED.input_cost"),
			"output_cost":     gorm.Expr("stats_site_model_hourlies.output_cost + EXCLUDED.output_cost"),
			"wait_time":       gorm.Expr("stats_site_model_hourlies.wait_time + EXCLUDED.wait_time"),
			"request_success": gorm.Expr("stats_site_model_hourlies.request_success + EXCLUDED.request_success"),
			"request_failed":  gorm.Expr("stats_site_model_hourlies.request_failed + EXCLUDED.request_failed"),
		}),
	}).Create(&rows).Error
}

// SiteChannelModelHourlyForAccount 读取指定 site account 下所有 (group, model) 的小时聚合，
// 合并未刷盘的内存桶后，按自适应桶宽生成 SiteModelHistorySummary。
// key 与 site_channel.go 保持一致：baseGroupKey + "\x00" + modelName。
func SiteChannelModelHourlyForAccount(ctx context.Context, siteAccountID int) (map[string]*model.SiteModelHistorySummary, error) {
	var rows []model.StatsSiteModelHourly
	if err := db.GetDB().WithContext(ctx).
		Where("site_account_id = ?", siteAccountID).
		Order("hour ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	// 合并尚未刷盘的内存桶。
	siteModelHourlyCacheLock.Lock()
	pending := make([]model.StatsSiteModelHourly, 0, len(siteModelHourlyCache))
	for k, entry := range siteModelHourlyCache {
		if k.SiteAccountID == siteAccountID {
			pending = append(pending, *entry)
		}
	}
	siteModelHourlyCacheLock.Unlock()

	type compositeKey struct {
		Hour      int
		GroupKey  string
		ModelName string
	}
	merged := make(map[compositeKey]*model.StatsSiteModelHourly, len(rows)+len(pending))
	add := func(r model.StatsSiteModelHourly) {
		k := compositeKey{Hour: r.Hour, GroupKey: r.GroupKey, ModelName: r.ModelName}
		if existing, ok := merged[k]; ok {
			existing.StatsMetrics.Add(r.StatsMetrics)
			return
		}
		copyRow := r
		merged[k] = &copyRow
	}
	for _, r := range rows {
		add(r)
	}
	for _, r := range pending {
		add(r)
	}

	// 按 (group_key, model_name) 分组。
	type groupedSeries struct {
		GroupKey  string
		ModelName string
		Hours     []model.StatsSiteModelHourly
	}
	grouped := make(map[string]*groupedSeries)
	for _, entry := range merged {
		key := entry.GroupKey + "\x00" + entry.ModelName
		series, ok := grouped[key]
		if !ok {
			series = &groupedSeries{GroupKey: entry.GroupKey, ModelName: entry.ModelName}
			grouped[key] = series
		}
		series.Hours = append(series.Hours, *entry)
	}

	result := make(map[string]*model.SiteModelHistorySummary, len(grouped))
	for key, series := range grouped {
		sort.Slice(series.Hours, func(i, j int) bool {
			return series.Hours[i].Hour < series.Hours[j].Hour
		})
		result[key] = buildSiteModelSummary(series.Hours)
	}
	return result, nil
}

// buildSiteModelSummary 把按时间排序的小时记录聚合为 SiteModelHistorySummary，
// 自适应选择桶宽。
func buildSiteModelSummary(hours []model.StatsSiteModelHourly) *model.SiteModelHistorySummary {
	summary := &model.SiteModelHistorySummary{}
	if len(hours) == 0 {
		return summary
	}

	for i := range hours {
		summary.SuccessCount += int(hours[i].RequestSuccess)
		summary.FailureCount += int(hours[i].RequestFailed)
	}

	earliestHour := hours[0].Hour
	latestHour := hours[len(hours)-1].Hour
	latestSec := int64(latestHour+1)*3600 - 1
	summary.LastRequestAt = &latestSec
	spanSeconds := int64((latestHour - earliestHour + 1) * 3600)

	bucketSpan := chooseBucketSpan(spanSeconds)
	summary.BucketSpan = bucketSpan

	bucketMap := make(map[int64]*model.SiteModelHistoryBucket)
	for _, h := range hours {
		hourStart := int64(h.Hour) * 3600
		bucketStart := hourStart - hourStart%int64(bucketSpan)
		bucket, ok := bucketMap[bucketStart]
		if !ok {
			bucket = &model.SiteModelHistoryBucket{Time: bucketStart}
			bucketMap[bucketStart] = bucket
		}
		bucket.Success += int(h.RequestSuccess)
		bucket.Failure += int(h.RequestFailed)
	}

	buckets := make([]model.SiteModelHistoryBucket, 0, len(bucketMap))
	for _, b := range bucketMap {
		buckets = append(buckets, *b)
	}
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Time < buckets[j].Time
	})
	summary.Buckets = buckets
	return summary
}

func chooseBucketSpan(spanSeconds int64) int {
	const (
		hour = int64(3600)
		day  = 24 * hour
		week = 7 * day
	)
	switch {
	case spanSeconds <= 24*hour:
		return int(hour)
	case spanSeconds <= 7*day:
		return int(6 * hour)
	case spanSeconds <= 30*day:
		return int(day)
	default:
		return int(week)
	}
}

// lookupChannelSiteBinding 查询并缓存 channelID → 站点绑定信息。
func lookupChannelSiteBinding(channelID int) (channelSiteBinding, error) {
	if cached, ok := siteBindingByChannelCache.Get(channelID); ok {
		return cached, nil
	}
	var binding model.SiteChannelBinding
	err := db.GetDB().Where("channel_id = ?", channelID).First(&binding).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result := channelSiteBinding{Found: false}
			siteBindingByChannelCache.Set(channelID, result)
			return result, nil
		}
		return channelSiteBinding{}, err
	}
	baseGroupKey, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
	result := channelSiteBinding{
		SiteAccountID: binding.SiteAccountID,
		BaseGroupKey:  baseGroupKey,
		Found:         true,
	}
	siteBindingByChannelCache.Set(channelID, result)
	return result, nil
}

// invalidateSiteBindingCache 在站点账号变更时清理映射缓存。
func invalidateSiteBindingCache() {
	siteBindingByChannelCache.Clear()
}
