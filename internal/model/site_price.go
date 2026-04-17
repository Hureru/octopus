package model

import "time"

// SitePrice 存储来自上游站点的 (账号, 分组, 模型) 维度价格快照。
// 字段语义对齐 LLMPrice（USD/百万 tokens），便于与 models.dev 全局价格回退互换。
type SitePrice struct {
	ID            int    `json:"id" gorm:"primaryKey"`
	SiteAccountID int    `json:"site_account_id" gorm:"uniqueIndex:idx_site_price_account_group_model;not null"`
	GroupKey      string `json:"group_key" gorm:"uniqueIndex:idx_site_price_account_group_model;not null;default:'default'"`
	ModelName     string `json:"model_name" gorm:"uniqueIndex:idx_site_price_account_group_model;not null"`

	QuotaType int `json:"quota_type" gorm:"default:0"`

	InputPrice      float64 `json:"input_price"`
	OutputPrice     float64 `json:"output_price"`
	CacheReadPrice  float64 `json:"cache_read_price"`
	CacheWritePrice float64 `json:"cache_write_price"`
	FlatPrice       float64 `json:"flat_price"`

	ModelRatio      float64 `json:"model_ratio"`
	CompletionRatio float64 `json:"completion_ratio"`
	GroupRatio      float64 `json:"group_ratio"`

	UpdatedAt time.Time `json:"updated_at"`
}

// ToLLMPrice 将 SitePrice 适配为通用的 LLMPrice 结构，供 relay 计费直接使用。
func (p SitePrice) ToLLMPrice() LLMPrice {
	return LLMPrice{
		Input:      p.InputPrice,
		Output:     p.OutputPrice,
		CacheRead:  p.CacheReadPrice,
		CacheWrite: p.CacheWritePrice,
	}
}
