package model

import "time"

type SiteProxyPreferenceStatus string

const (
	SiteProxyPreferenceHealthy  SiteProxyPreferenceStatus = "healthy"
	SiteProxyPreferenceCooling  SiteProxyPreferenceStatus = "cooling"
	SiteProxyPreferenceStale    SiteProxyPreferenceStatus = "stale"
	SiteProxyPreferenceDisabled SiteProxyPreferenceStatus = "disabled"
)

type SiteProxyPreference struct {
	ID                  int                       `json:"id" gorm:"primaryKey"`
	IdentityKey         string                    `json:"identity_key" gorm:"size:64;not null;uniqueIndex"`
	SiteID              int                       `json:"site_id" gorm:"not null;index"`
	SiteAccountID       int                       `json:"site_account_id" gorm:"not null;default:0;index"`
	ProxyMode           ProxyUsageMode            `json:"proxy_mode" gorm:"type:varchar(16);not null"`
	ProxyConfigID       int                       `json:"proxy_config_id" gorm:"not null;default:0;index"`
	ClashControllerID   int                       `json:"clash_controller_id" gorm:"not null;default:0;index"`
	ClashNode           string                    `json:"clash_node,omitempty" gorm:"size:191"`
	Status              SiteProxyPreferenceStatus `json:"status" gorm:"type:varchar(16);not null;default:'healthy';index"`
	ConsecutiveFailures int                       `json:"consecutive_failures"`
	SuccessCount        int64                     `json:"success_count"`
	FailureCount        int64                     `json:"failure_count"`
	AverageLatencyMS    float64                   `json:"average_latency_ms"`
	CooldownUntil       *time.Time                `json:"cooldown_until,omitempty" gorm:"index"`
	LastSuccessAt       *time.Time                `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time                `json:"last_failure_at,omitempty"`
	ExpiresAt           *time.Time                `json:"expires_at,omitempty" gorm:"index"`
	Manual              bool                      `json:"manual" gorm:"not null;default:false"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
}

type ClashSwitchLease struct {
	LeaseKey   string    `json:"-" gorm:"size:191;primaryKey"`
	OwnerToken string    `json:"-" gorm:"size:64;not null"`
	ExpiresAt  time.Time `json:"-" gorm:"index"`
	UpdatedAt  time.Time `json:"-"`
}

type ClashController struct {
	ID              int       `json:"id" gorm:"primaryKey"`
	Name            string    `json:"name" gorm:"size:128;unique;not null"`
	APIURL          string    `json:"api_url" gorm:"not null"`
	ProxyURL        string    `json:"proxy_url" gorm:"not null"`
	GroupName       string    `json:"group_name" gorm:"size:191;not null"`
	SecretEncrypted string    `json:"-" gorm:"type:text"`
	Enabled         bool      `json:"enabled" gorm:"not null;default:true"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SiteOperationType string

const (
	SiteOperationSync    SiteOperationType = "sync"
	SiteOperationCheckin SiteOperationType = "checkin"
)

type SiteOperationAttempt struct {
	ID                int64             `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID            int               `json:"site_id" gorm:"not null;index"`
	SiteAccountID     int               `json:"site_account_id" gorm:"not null;index"`
	Operation         SiteOperationType `json:"operation" gorm:"type:varchar(24);not null;index"`
	AttemptNumber     int               `json:"attempt_number"`
	ProxyMode         ProxyUsageMode    `json:"proxy_mode" gorm:"type:varchar(16);not null"`
	ProxyConfigID     *int              `json:"proxy_config_id,omitempty"`
	ClashControllerID *int              `json:"clash_controller_id,omitempty"`
	ClashNode         string            `json:"clash_node,omitempty" gorm:"size:191"`
	StartedAt         time.Time         `json:"started_at" gorm:"index"`
	DurationMS        int64             `json:"duration_ms"`
	Success           bool              `json:"success"`
	FailureClass      string            `json:"failure_class,omitempty" gorm:"size:64"`
	Message           string            `json:"message,omitempty" gorm:"type:text"`
	OperationID       string            `json:"operation_id,omitempty" gorm:"size:64;index"`
	PathLabel         string            `json:"path_label,omitempty" gorm:"size:191"`
	StopReason        string            `json:"stop_reason,omitempty" gorm:"size:64"`
}

type VerificationSessionStatus string

const (
	VerificationSessionPending    VerificationSessionStatus = "pending"
	VerificationSessionCompleted  VerificationSessionStatus = "completed"
	VerificationSessionExpired    VerificationSessionStatus = "expired"
	VerificationSessionRevoked    VerificationSessionStatus = "revoked"
	VerificationSessionSuperseded VerificationSessionStatus = "superseded"
)

type VerificationSession struct {
	ID              int64                     `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID          int                       `json:"site_id" gorm:"not null;index"`
	SiteAccountID   int                       `json:"site_account_id" gorm:"not null;index"`
	ProxyConfigID   *int                      `json:"proxy_config_id,omitempty"`
	ClashNode       string                    `json:"clash_node,omitempty"`
	UserAgent       string                    `json:"user_agent,omitempty" gorm:"type:text"`
	CookieEncrypted string                    `json:"-" gorm:"type:text"`
	Status          VerificationSessionStatus `json:"status" gorm:"type:varchar(24);not null;index"`
	ExpiresAt       time.Time                 `json:"expires_at" gorm:"index"`
	CompletedAt     *time.Time                `json:"completed_at,omitempty"`
	Source          string                    `json:"source,omitempty" gorm:"size:24"`
	CreatedAt       time.Time                 `json:"created_at"`
}

type VerificationBridgePairing struct {
	ID            int64      `json:"id" gorm:"primaryKey;autoIncrement"`
	Name          string     `json:"name" gorm:"size:128;not null"`
	SiteAccountID int        `json:"site_account_id" gorm:"not null;default:0;index"`
	TokenHash     string     `json:"-" gorm:"size:64;uniqueIndex;not null"`
	ExpiresAt     time.Time  `json:"expires_at" gorm:"index"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty" gorm:"index"`
	CreatedAt     time.Time  `json:"created_at"`
}

type VerificationTaskStatus string
type VerificationRetryStatus string

const (
	VerificationTaskPending   VerificationTaskStatus = "pending"
	VerificationTaskClaimed   VerificationTaskStatus = "claimed"
	VerificationTaskCompleted VerificationTaskStatus = "completed"
	VerificationTaskExpired   VerificationTaskStatus = "expired"
	VerificationTaskCanceled  VerificationTaskStatus = "canceled"
)

const (
	VerificationRetryNone      VerificationRetryStatus = "none"
	VerificationRetryPending   VerificationRetryStatus = "pending"
	VerificationRetryRunning   VerificationRetryStatus = "running"
	VerificationRetrySucceeded VerificationRetryStatus = "succeeded"
	VerificationRetryFailed    VerificationRetryStatus = "failed"
	VerificationRetryCanceled  VerificationRetryStatus = "canceled"
)

type VerificationTask struct {
	ID               int64                   `json:"id" gorm:"primaryKey;autoIncrement"`
	SessionID        int64                   `json:"session_id" gorm:"not null;uniqueIndex"`
	PairingID        *int64                  `json:"pairing_id,omitempty" gorm:"index"`
	ClaimTokenHash   string                  `json:"-" gorm:"size:64"`
	Status           VerificationTaskStatus  `json:"status" gorm:"type:varchar(24);not null;index"`
	TargetURL        string                  `json:"target_url" gorm:"type:text;not null"`
	TargetHost       string                  `json:"target_host" gorm:"size:255;not null"`
	ProxyConfigID    *int                    `json:"proxy_config_id,omitempty"`
	ClashNode        string                  `json:"clash_node,omitempty" gorm:"size:191"`
	UserAgent        string                  `json:"user_agent,omitempty" gorm:"type:text"`
	ExpiresAt        time.Time               `json:"expires_at" gorm:"index"`
	ClaimedAt        *time.Time              `json:"claimed_at,omitempty"`
	CompletedAt      *time.Time              `json:"completed_at,omitempty"`
	Operation        SiteOperationType       `json:"operation,omitempty" gorm:"type:varchar(24);not null;default:'';index"`
	RetryStatus      VerificationRetryStatus `json:"retry_status" gorm:"type:varchar(24);not null;default:'none';index"`
	RetryTokenHash   string                  `json:"-" gorm:"size:64"`
	RetryMessage     string                  `json:"retry_message,omitempty" gorm:"type:text"`
	RetryStartedAt   *time.Time              `json:"retry_started_at,omitempty"`
	RetryCompletedAt *time.Time              `json:"retry_completed_at,omitempty"`
	CreatedAt        time.Time               `json:"created_at"`
}
