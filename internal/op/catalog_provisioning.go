package op

import (
	"github.com/bestruirui/octopus/internal/model"
)

// CatalogGroupProvisioningMode 返回当前的目录建组策略。
// 设置缺失时回退到 DefaultSettings 中的默认值，与 SettingGetBool 的处理方式保持一致。
func CatalogGroupProvisioningMode() model.CatalogGroupProvisioning {
	value, err := SettingGetString(model.SettingKeyCatalogGroupProvisioning)
	if err != nil {
		return defaultCatalogGroupProvisioning()
	}
	mode, ok := model.ParseCatalogGroupProvisioning(value)
	if !ok {
		return defaultCatalogGroupProvisioning()
	}
	return mode
}

func defaultCatalogGroupProvisioning() model.CatalogGroupProvisioning {
	for _, setting := range model.DefaultSettings() {
		if setting.Key != model.SettingKeyCatalogGroupProvisioning {
			continue
		}
		if mode, ok := model.ParseCatalogGroupProvisioning(setting.Value); ok {
			return mode
		}
	}
	return model.CatalogGroupProvisioningManual
}
