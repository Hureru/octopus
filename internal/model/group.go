package model

import "time"

type GroupMode int

const (
	GroupModeRoundRobin GroupMode = 1 // 轮询：依次循环选择渠道
	GroupModeRandom     GroupMode = 2 // 随机：每次随机选择一个渠道
	GroupModeFailover   GroupMode = 3 // 故障转移：按优先级选择，失败时降级到下一个
	GroupModeWeighted   GroupMode = 4 // 加权分配：按优权重分配流量
)

type Group struct {
	ID                int         `json:"id" gorm:"primaryKey"`
	Name              string      `json:"name" gorm:"unique;not null"`
	Mode              GroupMode   `json:"mode" gorm:"not null"`
	MatchRegex        string      `json:"match_regex"`
	FirstTokenTimeOut int         `json:"first_token_time_out"`               // 单个渠道首个Token响应超时时间(秒)
	SessionKeepTime   int         `json:"session_keep_time"`                  // 会话保持时间(秒) 0 为禁用
	RetryEnabled      bool        `json:"retry_enabled" gorm:"default:false"` // 启用同通道重试+透传429/503
	MaxRetries        int         `json:"max_retries" gorm:"default:3"`       // 同通道最大重试次数(RetryEnabled启用时生效)
	Pinned            bool        `json:"pinned" gorm:"default:false;index"`  // 置顶
	PinnedAt          *time.Time  `json:"pinned_at,omitempty"`                // 置顶时间，置顶时写入，取消置顶时置空
	ActivePresetID    *int        `json:"active_preset_id,omitempty"`         // 当前激活的预设ID，仅 UI 标记，不参与路由
	Items             []GroupItem `json:"items,omitempty" gorm:"foreignKey:GroupID"`
}

type GroupItem struct {
	ID        int    `json:"id" gorm:"primaryKey"`
	GroupID   int    `json:"group_id" gorm:"not null;index:idx_group_channel_model,unique"` // 创建时不携带此字段,更新时需要
	ChannelID int    `json:"channel_id" gorm:"not null;index:idx_group_channel_model,unique"`
	ModelName string `json:"model_name" gorm:"not null;index:idx_group_channel_model,unique"`
	Priority  int    `json:"priority"`
	Weight    int    `json:"weight"`
}

// GroupPreset 分组的路由配置预设（命名快照）
// 切换预设 = 用此快照覆盖 Group 的实时 Mode/超时/重试/regex + group_items 表
type GroupPreset struct {
	ID                int               `json:"id" gorm:"primaryKey"`
	GroupID           int               `json:"group_id" gorm:"not null;index:idx_group_preset_name,unique"`
	Name              string            `json:"name" gorm:"not null;index:idx_group_preset_name,unique"`
	Mode              GroupMode         `json:"mode" gorm:"not null"`
	MatchRegex        string            `json:"match_regex"`
	FirstTokenTimeOut int               `json:"first_token_time_out"`
	SessionKeepTime   int               `json:"session_keep_time"`
	RetryEnabled      bool              `json:"retry_enabled"`
	MaxRetries        int               `json:"max_retries"`
	Items             []GroupPresetItem `json:"items" gorm:"serializer:json;type:text"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// GroupPresetItem 预设中的渠道-模型条目（JSON 序列化为 Items 字段）
// 不含 ID/GroupID，激活时重新生成
type GroupPresetItem struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model_name"`
	Priority  int    `json:"priority"`
	Weight    int    `json:"weight"`
}

// GroupUpdateRequest 分组更新请求 - 仅包含变更的数据
type GroupUpdateRequest struct {
	ID                int                      `json:"id" binding:"required"`
	Name              *string                  `json:"name,omitempty"`                 // 仅在名称变更时发送
	Mode              *GroupMode               `json:"mode,omitempty"`                 // 仅在模式变更时发送
	MatchRegex        *string                  `json:"match_regex,omitempty"`          // 仅在匹配正则变更时发送
	FirstTokenTimeOut *int                     `json:"first_token_time_out,omitempty"` // 仅在超时变更时发送(秒)
	SessionKeepTime   *int                     `json:"session_keep_time,omitempty"`    // 仅在会话保持时间变更时发送(秒)
	RetryEnabled      *bool                    `json:"retry_enabled,omitempty"`        // 启用同通道重试+透传429/503
	MaxRetries        *int                     `json:"max_retries,omitempty"`          // 同通道最大重试次数
	ItemsToAdd        []GroupItemAddRequest    `json:"items_to_add,omitempty"`         // 新增的 items
	ItemsToUpdate     []GroupItemUpdateRequest `json:"items_to_update,omitempty"`      // 更新的 items (priority 变更)
	ItemsToDelete     []int                    `json:"items_to_delete,omitempty"`      // 删除的 item IDs
}

// GroupItemAddRequest 新增 item 请求
type GroupItemAddRequest struct {
	ChannelID int    `json:"channel_id" binding:"required"`
	ModelName string `json:"model_name" binding:"required"`
	Priority  int    `json:"priority,omitempty"`
	Weight    int    `json:"weight,omitempty"`
}

// GroupItemUpdateRequest 更新 item 请求
type GroupItemUpdateRequest struct {
	ID       int `json:"id" binding:"required"`
	Priority int `json:"priority,omitempty"`
	Weight   int `json:"weight,omitempty"`
}
type GroupIDAndLLMName struct {
	ChannelID int
	ModelName string
}

// GroupPresetCreateRequest 新增预设：服务端从 Group 当前实时状态取快照；
// 也用于 create-blank 和 clone 接口（均只需 Name）
type GroupPresetCreateRequest struct {
	Name string `json:"name" binding:"required"`
}

// GroupPresetUpdateRequest 直接编辑预设的 JSON 内容
// Items 为整体替换语义（非增量），nil 表示不变
// 若该预设是 active，编辑结果会同步镜像到所属 Group（live binding）
type GroupPresetUpdateRequest struct {
	Name              *string            `json:"name,omitempty"`
	Mode              *GroupMode         `json:"mode,omitempty"`
	MatchRegex        *string            `json:"match_regex,omitempty"`
	FirstTokenTimeOut *int               `json:"first_token_time_out,omitempty"`
	SessionKeepTime   *int               `json:"session_keep_time,omitempty"`
	RetryEnabled      *bool              `json:"retry_enabled,omitempty"`
	MaxRetries        *int               `json:"max_retries,omitempty"`
	Items             *[]GroupPresetItem `json:"items,omitempty"`
}

// GroupPinRequest 置顶/取消置顶
type GroupPinRequest struct {
	Pinned *bool `json:"pinned" binding:"required"`
}

type GroupAutoGroupConfig struct {
	ProjectedGlobalAutoGroup AutoGroupType          `json:"projected_global_auto_group"`
	Sources                  []GroupAutoGroupSource `json:"sources"`
}

type GroupAutoGroupSource struct {
	ChannelID          int           `json:"channel_id"`
	ChannelName        string        `json:"channel_name"`
	Enabled            bool          `json:"enabled"`
	Managed            bool          `json:"managed"`
	AutoGroup          AutoGroupType `json:"auto_group"`
	EffectiveAutoGroup AutoGroupType `json:"effective_auto_group"`
	GlobalOverride     bool          `json:"global_override"`
	ModelCount         int           `json:"model_count"`
	Models             []string      `json:"models"`
	SiteID             *int          `json:"site_id,omitempty"`
	SiteName           string        `json:"site_name,omitempty"`
	SiteAccountID      *int          `json:"site_account_id,omitempty"`
	SiteAccountName    string        `json:"site_account_name,omitempty"`
	SiteGroupKey       string        `json:"site_group_key,omitempty"`
	SiteGroupName      string        `json:"site_group_name,omitempty"`
	EndpointType       string        `json:"endpoint_type,omitempty"`
}

type GroupAutoGroupSourceUpdateRequest struct {
	ChannelID int            `json:"channel_id" binding:"required"`
	AutoGroup *AutoGroupType `json:"auto_group,omitempty"`
}

type GroupAutoGroupConfigUpdateRequest struct {
	ProjectedGlobalAutoGroup *AutoGroupType                      `json:"projected_global_auto_group,omitempty"`
	Items                    []GroupAutoGroupSourceUpdateRequest `json:"items,omitempty"`
	RunNow                   bool                                `json:"run_now"`
}

type GroupAutoGroupRunRequest struct {
	ChannelIDs []int `json:"channel_ids,omitempty"`
}
