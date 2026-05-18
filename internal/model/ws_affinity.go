package model

import "time"

// WSResponseAffinity stores the preferred upstream route for an OpenAI Responses
// response id. The response id itself is hashed before storage.
type WSResponseAffinity struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	APIKeyID       int       `json:"api_key_id" gorm:"not null;index:idx_ws_response_affinity_scope,unique"`
	GroupID        int       `json:"group_id" gorm:"not null;index:idx_ws_response_affinity_scope,unique"`
	RequestModel   string    `json:"request_model" gorm:"size:191;not null;index:idx_ws_response_affinity_scope,unique"`
	ResponseIDHash string    `json:"response_id_hash" gorm:"size:64;not null;index:idx_ws_response_affinity_scope,unique"`
	ChannelID      int       `json:"channel_id" gorm:"not null"`
	ChannelKeyID   int       `json:"channel_key_id" gorm:"not null"`
	UpstreamModel  string    `json:"upstream_model" gorm:"size:191"`
	ExpiresAt      time.Time `json:"expires_at" gorm:"not null;index"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
