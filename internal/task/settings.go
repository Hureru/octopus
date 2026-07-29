package task

import (
	"fmt"
	"strconv"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

const maxConfiguredIntervalHours = 24 * 365 * 10

type settingIntervalSpec struct {
	key      model.SettingKey
	taskName string
	unit     time.Duration
}

var settingIntervalSpecs = []settingIntervalSpec{
	{key: model.SettingKeyModelInfoUpdateInterval, taskName: string(model.SettingKeyModelInfoUpdateInterval), unit: time.Hour},
	{key: model.SettingKeySyncLLMInterval, taskName: string(model.SettingKeySyncLLMInterval), unit: time.Hour},
	{key: model.SettingKeySiteSyncInterval, taskName: string(model.SettingKeySiteSyncInterval), unit: time.Hour},
	{key: model.SettingKeySiteCheckinInterval, taskName: string(model.SettingKeySiteCheckinInterval), unit: time.Hour},
	{key: model.SettingKeyStatsSaveInterval, taskName: TaskStatsSave, unit: time.Minute},
	{key: model.SettingKeyOutlierRetireInterval, taskName: string(model.SettingKeyOutlierRetireInterval), unit: time.Minute},
	{key: model.SettingKeyWebDAVBackupInterval, taskName: string(model.SettingKeyWebDAVBackupInterval), unit: time.Hour},
}

// ReloadSettingIntervals makes a restored settings snapshot effective without
// requiring a process restart. Values are bounded before converting to a
// time.Duration so an imported integer cannot overflow into a negative ticker.
func ReloadSettingIntervals() error {
	for _, spec := range settingIntervalSpecs {
		value, err := op.SettingGetInt(spec.key)
		if err != nil {
			return fmt.Errorf("read %s: %w", spec.key, err)
		}
		if err := updateSettingInterval(spec, value); err != nil {
			return err
		}
	}
	return nil
}

func UpdateSettingInterval(key model.SettingKey, value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}
	for _, spec := range settingIntervalSpecs {
		if spec.key == key {
			return updateSettingInterval(spec, parsed)
		}
	}
	return fmt.Errorf("setting %s does not control a scheduled task", key)
}

func updateSettingInterval(spec settingIntervalSpec, value int) error {
	if value < 0 || value > maxConfiguredIntervalHours {
		return fmt.Errorf("setting %s is outside the supported interval range", spec.key)
	}
	tasksMu.RLock()
	_, exists := tasks[spec.taskName]
	tasksMu.RUnlock()
	if !exists {
		return fmt.Errorf("task %s is not registered", spec.taskName)
	}
	Update(spec.taskName, time.Duration(value)*spec.unit)
	return nil
}
