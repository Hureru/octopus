package model

import "time"

type GroupHealthStatus string

const (
	GroupHealthStatusRunning GroupHealthStatus = "running"
	GroupHealthStatusSuccess GroupHealthStatus = "success"
	GroupHealthStatusPartial GroupHealthStatus = "partial"
	GroupHealthStatusFailed  GroupHealthStatus = "failed"
)

type GroupHealthAttemptStatus string

const (
	GroupHealthAttemptStatusSuccess GroupHealthAttemptStatus = "success"
	GroupHealthAttemptStatusFailed  GroupHealthAttemptStatus = "failed"
	GroupHealthAttemptStatusSkipped GroupHealthAttemptStatus = "skipped"
)

type GroupHealthProbeMode string

const (
	GroupHealthProbeModeStandard GroupHealthProbeMode = "standard"
	GroupHealthProbeModeFull     GroupHealthProbeMode = "full"
)

type GroupHealthSnapshot struct {
	ID                  int                  `json:"id" gorm:"primaryKey"`
	GroupID             int                  `json:"group_id" gorm:"index:idx_group_health_group_started"`
	GroupName           string               `json:"group_name" gorm:"type:varchar(255);not null"`
	GroupMode           GroupMode            `json:"group_mode" gorm:"not null"`
	ProbeMode           GroupHealthProbeMode `json:"probe_mode" gorm:"type:varchar(16);not null;default:'standard'"`
	RequestModel        string               `json:"request_model" gorm:"type:varchar(255);not null"`
	Status              GroupHealthStatus    `json:"status" gorm:"type:varchar(16);index:idx_group_health_status_started;not null"`
	StartedAt           time.Time            `json:"started_at" gorm:"index:idx_group_health_group_started;not null"`
	FinishedAt          *time.Time           `json:"finished_at"`
	DurationMS          int64                `json:"duration_ms" gorm:"not null;default:0"`
	SuccessfulChannelID *int                 `json:"successful_channel_id"`
	Message             string               `json:"message"`
	Attempts            []GroupHealthAttempt `json:"attempts,omitempty" gorm:"foreignKey:SnapshotID"`
}

type GroupHealthAttempt struct {
	ID           int                      `json:"id" gorm:"primaryKey"`
	SnapshotID   int                      `json:"snapshot_id" gorm:"index:idx_group_health_attempt_snapshot_priority;not null"`
	GroupItemID  int                      `json:"group_item_id" gorm:"not null"`
	ChannelID    int                      `json:"channel_id" gorm:"not null"`
	ChannelName  string                   `json:"channel_name" gorm:"type:varchar(255);not null"`
	ChannelKeyID int                      `json:"channel_key_id" gorm:"not null;default:0"`
	KeyRemark    string                   `json:"key_remark"`
	ModelName    string                   `json:"model_name" gorm:"type:varchar(255);not null"`
	Priority     int                      `json:"priority" gorm:"index:idx_group_health_attempt_snapshot_priority;not null"`
	Weight       int                      `json:"weight" gorm:"not null;default:0"`
	Status       GroupHealthAttemptStatus `json:"status" gorm:"type:varchar(16);not null"`
	HTTPStatus   int                      `json:"http_status" gorm:"not null;default:0"`
	DurationMS   int64                    `json:"duration_ms" gorm:"not null;default:0"`
	ErrorMessage string                   `json:"error_message"`
}

type GroupHealthGroupView struct {
	GroupID   int                  `json:"group_id"`
	GroupName string               `json:"group_name"`
	GroupMode GroupMode            `json:"group_mode"`
	Latest    *GroupHealthSnapshot `json:"latest,omitempty"`
}
