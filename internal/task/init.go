package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const (
	TaskPriceUpdate  = "price_update"
	TaskStatsSave    = "stats_save"
	TaskRelayLogSave = "relay_log_save"
	TaskSyncLLM      = "sync_llm"
	TaskCleanLLM     = "clean_llm"
	TaskBaseUrlDelay = "base_url_delay"
	TaskSiteSync     = "site_sync"
	TaskSiteCheckin  = "site_checkin"
)

func Init() {
	priceUpdateIntervalHours, err := op.SettingGetInt(model.SettingKeyModelInfoUpdateInterval)
	if err != nil {
		log.Errorf("failed to get model info update interval: %v", err)
		return
	}
	priceUpdateInterval := time.Duration(priceUpdateIntervalHours) * time.Hour
	// 注册价格更新任务
	Register(string(model.SettingKeyModelInfoUpdateInterval), priceUpdateInterval, true, func() {
		if err := price.UpdateLLMPrice(context.Background()); err != nil {
			log.Warnf("failed to update price info: %v", err)
		}
	})

	// 注册基础URL延迟任务
	Register(TaskBaseUrlDelay, 24*time.Hour, true, ChannelBaseUrlDelayTask)

	// 注册LLM同步任务
	syncLLMIntervalHours, err := op.SettingGetInt(model.SettingKeySyncLLMInterval)
	if err != nil {
		log.Warnf("failed to get sync LLM interval: %v", err)
		return
	}
	syncLLMInterval := time.Duration(syncLLMIntervalHours) * time.Hour
	Register(string(model.SettingKeySyncLLMInterval), syncLLMInterval, true, SyncModelsTask)

	siteSyncIntervalHours, err := op.SettingGetInt(model.SettingKeySiteSyncInterval)
	if err != nil {
		log.Warnf("failed to get site sync interval: %v", err)
		return
	}
	siteSyncInterval := time.Duration(siteSyncIntervalHours) * time.Hour
	Register(string(model.SettingKeySiteSyncInterval), siteSyncInterval, true, SiteSyncTask)

	siteCheckinIntervalHours, err := op.SettingGetInt(model.SettingKeySiteCheckinInterval)
	if err != nil {
		log.Warnf("failed to get site checkin interval: %v", err)
		return
	}
	siteCheckinInterval := time.Duration(siteCheckinIntervalHours) * time.Hour
	Register(string(model.SettingKeySiteCheckinInterval), siteCheckinInterval, true, SiteCheckinTask)

	// 注册统计保存任务
	statsSaveIntervalMinutes, err := op.SettingGetInt(model.SettingKeyStatsSaveInterval)
	if err != nil {
		log.Warnf("failed to get stats save interval: %v", err)
		return
	}
	statsSaveInterval := time.Duration(statsSaveIntervalMinutes) * time.Minute
	Register(TaskStatsSave, statsSaveInterval, false, op.StatsSaveDBTask)
	// 注册中继日志保存任务
	Register(TaskRelayLogSave, 10*time.Minute, false, func() {
		if err := op.RelayLogSaveDBTask(context.Background()); err != nil {
			log.Warnf("relay log save db task failed: %v", err)
		}
	})
}
