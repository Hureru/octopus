package op

import (
	"context"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm/clause"
)

// StatsSiteModelBackfill 一次性从最近的 relay_logs 回填 StatsSiteModelHourly 聚合表，
// 让首次启用此功能的实例也能立即看到历史折线图。已回填则跳过。
// 回填窗口：默认 30 天。
func StatsSiteModelBackfill(ctx context.Context) {
	done, err := SettingGetBool(model.SettingKeyStatsSiteModelBackfilled)
	if err == nil && done {
		return
	}

	startTime := time.Now()
	cutoff := startTime.Add(-30 * 24 * time.Hour).Unix()

	// 取出所有站点绑定，构造 channelID → (siteAccountID, baseGroupKey)
	var bindings []model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Find(&bindings).Error; err != nil {
		log.Warnf("stats site model backfill: failed to load bindings: %v", err)
		return
	}
	if len(bindings) == 0 {
		_ = SettingSetString(model.SettingKeyStatsSiteModelBackfilled, "true")
		return
	}
	bindingByChannel := make(map[int]channelSiteBinding, len(bindings))
	for _, b := range bindings {
		baseGroupKey, _ := model.ParseSiteChannelBindingKey(b.GroupKey)
		bindingByChannel[b.ChannelID] = channelSiteBinding{
			SiteAccountID: b.SiteAccountID,
			BaseGroupKey:  baseGroupKey,
			Found:         true,
		}
	}

	// 分页扫描 relay_logs。RequestContent/ResponseContent 单条可达 MB 级别，
	// 这里通过专用的 backfillLogRow 结构体限定字段，让 GORM 只 SELECT
	// 聚合真正需要的列，避免把全量内容字段拉进 Go 堆 —— 在 LLM 长上下文
	// 加上容器内存上限的组合下，旧实现足以直接 OOM。
	const pageSize = 200
	type aggKey struct {
		Hour          int
		SiteAccountID int
		GroupKey      string
		ModelName     string
	}
	aggregated := make(map[aggKey]*model.StatsSiteModelHourly)

	processAttempt := func(ts int64, channelID int, modelName string, success bool) {
		binding, ok := bindingByChannel[channelID]
		if !ok {
			return
		}
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			return
		}
		hour := int(ts / 3600)
		k := aggKey{Hour: hour, SiteAccountID: binding.SiteAccountID, GroupKey: binding.BaseGroupKey, ModelName: modelName}
		entry, ok := aggregated[k]
		if !ok {
			entry = &model.StatsSiteModelHourly{
				Hour:          hour,
				SiteAccountID: binding.SiteAccountID,
				GroupKey:      binding.BaseGroupKey,
				ModelName:     modelName,
				Date:          time.Unix(ts, 0).Format("20060102"),
			}
			aggregated[k] = entry
		}
		if success {
			entry.RequestSuccess++
		} else {
			entry.RequestFailed++
		}
		if ts > entry.LastRequestAt {
			entry.LastRequestAt = ts
		}
	}

	var lastID int64
	for {
		var batch []backfillLogRow
		if err := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Where("time >= ? AND id > ?", cutoff, lastID).
			Order("id ASC").
			Limit(pageSize).
			Find(&batch).Error; err != nil {
			log.Warnf("stats site model backfill: scan logs failed: %v", err)
			return
		}
		if len(batch) == 0 {
			break
		}
		for _, entry := range batch {
			lastID = entry.ID
			if len(entry.Attempts) == 0 {
				success := strings.TrimSpace(entry.Error) == ""
				modelName := entry.ActualModelName
				if modelName == "" {
					modelName = entry.RequestModelName
				}
				processAttempt(entry.Time, entry.ChannelId, modelName, success)
				continue
			}
			for _, attempt := range entry.Attempts {
				if attempt.Status != model.AttemptSuccess && attempt.Status != model.AttemptFailed {
					continue
				}
				modelName := attempt.ModelName
				if modelName == "" {
					modelName = entry.ActualModelName
				}
				if modelName == "" {
					modelName = entry.RequestModelName
				}
				processAttempt(entry.Time, attempt.ChannelID, modelName, attempt.Status == model.AttemptSuccess)
			}
		}
		if len(batch) < pageSize {
			break
		}
	}

	if len(aggregated) == 0 {
		_ = SettingSetString(model.SettingKeyStatsSiteModelBackfilled, "true")
		log.Infof("stats site model backfill: no data, marked complete (took %s)", time.Since(startTime))
		return
	}

	rows := make([]model.StatsSiteModelHourly, 0, len(aggregated))
	for _, v := range aggregated {
		rows = append(rows, *v)
	}

	const upsertChunk = 500
	dbConn := db.GetDB().WithContext(ctx)
	for i := 0; i < len(rows); i += upsertChunk {
		end := i + upsertChunk
		if end > len(rows) {
			end = len(rows)
		}
		if err := dbConn.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "hour"}, {Name: "site_account_id"}, {Name: "group_key"}, {Name: "model_name"},
			},
			DoNothing: true,
		}).Create(rows[i:end]).Error; err != nil {
			log.Warnf("stats site model backfill: insert chunk failed: %v", err)
			return
		}
	}

	if err := SettingSetString(model.SettingKeyStatsSiteModelBackfilled, "true"); err != nil {
		log.Warnf("stats site model backfill: failed to mark complete: %v", err)
		return
	}
	log.Infof("stats site model backfill done: %d aggregated rows from %d-day window in %s", len(rows), 30, time.Since(startTime))
}

// backfillLogRow 是一次性回填扫描专用的精简行结构。字段集合刻意比
// model.RelayLog 小，GORM 会按目的结构的字段裁剪 SELECT 列表，
// 把 request_content / response_content 等大字段留在数据库里。
// 不要给它增加内容字段，否则会重新引入 OOM 风险。
type backfillLogRow struct {
	ID               int64
	Time             int64
	ChannelId        int
	ActualModelName  string
	RequestModelName string
	Error            string
	Attempts         []model.ChannelAttempt `gorm:"serializer:json"`
}
