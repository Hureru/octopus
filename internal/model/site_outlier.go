package model

import "time"

// SiteChannelOutlierStatus 渠道离群退役状态。
type SiteChannelOutlierStatus string

const (
	OutlierStatusActive  SiteChannelOutlierStatus = "active"
	OutlierStatusRetired SiteChannelOutlierStatus = "retired"
)

// SiteChannelOutlierState 记录被动离群退役（POR）在数据面对单个投影渠道的退役状态。
// 与控制面 SiteUserGroup.ProjectionSuspended 各管各的维度：本表按 channel.ID 维度，
// 是退役/恢复的持久化 source of truth（进程内滚动窗口仅为临时证据，重启清空）。
type SiteChannelOutlierState struct {
	ChannelID         int                      `json:"channel_id" gorm:"primaryKey"` // 一对一，天然去重
	SiteAccountID     int                      `json:"site_account_id" gorm:"index;not null;default:0"`
	Status            SiteChannelOutlierStatus `json:"status" gorm:"type:varchar(16);index;not null;default:'active'"`
	Reason            string                   `json:"reason" gorm:"type:varchar(255)"`
	CloudflareBlocked bool                     `json:"cloudflare_blocked" gorm:"not null;default:false"` // 门3 CF 指纹命中
	RetiredAt         *time.Time               `json:"retired_at"`
	RecoverStreak     int                      `json:"recover_streak" gorm:"not null;default:0"` // 连续探活成功次数
	LastProbeAt       *time.Time               `json:"last_probe_at"`
	Snapshot          OutlierSnapshot          `json:"snapshot" gorm:"serializer:json"` // 退役时的窗口/探活快照（诊断用）
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

// OutlierSnapshot 退役时刻的证据快照，便于运维诊断（不参与判定）。
type OutlierSnapshot struct {
	Samples          int       `json:"samples"`
	Failures         int       `json:"failures"`
	FailureRate      float64   `json:"failure_rate"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	LastSuccessAt    time.Time `json:"last_success_at,omitempty"`
	ProbeHTTPStatus  int       `json:"probe_http_status,omitempty"`
	ProbeError       string    `json:"probe_error,omitempty"`
	ProbeCloudflare  bool      `json:"probe_cloudflare,omitempty"`
	SiblingHealthy   int       `json:"sibling_healthy"`
	SiblingTotal     int       `json:"sibling_total"`
}
