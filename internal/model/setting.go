package model

import (
	"fmt"
	"net/url"
	"strconv"
)

type SettingKey string

const (
	SettingKeyProxyURL                         SettingKey = "proxy_url"
	SettingKeyStatsSaveInterval                SettingKey = "stats_save_interval"                  // 将统计信息写入数据库的周期(分钟)
	SettingKeyModelInfoUpdateInterval          SettingKey = "model_info_update_interval"           // 模型信息更新间隔(小时)
	SettingKeySyncLLMInterval                  SettingKey = "sync_llm_interval"                    // LLM 同步间隔(小时)
	SettingKeySiteSyncInterval                 SettingKey = "site_sync_interval"                   // 站点账号同步间隔(小时)
	SettingKeySiteCheckinInterval              SettingKey = "site_checkin_interval"                // 站点自动签到间隔(小时)
	SettingKeyRelayLogKeepPeriod               SettingKey = "relay_log_keep_period"                // 日志保存时间范围(天)
	SettingKeyRelayLogKeepEnabled              SettingKey = "relay_log_keep_enabled"               // 是否保留历史日志
	SettingKeyCORSAllowOrigins                 SettingKey = "cors_allow_origins"                   // 跨域白名单(逗号分隔, 如 "example.com,example2.com"). 为空不允许跨域, "*"允许所有
	SettingKeyCircuitBreakerThreshold          SettingKey = "circuit_breaker_threshold"            // 熔断触发阈值（连续失败次数）
	SettingKeyCircuitBreakerCooldown           SettingKey = "circuit_breaker_cooldown"             // 熔断基础冷却时间（秒）
	SettingKeyCircuitBreakerMaxCooldown        SettingKey = "circuit_breaker_max_cooldown"         // 熔断最大冷却时间（秒），指数退避上限
	SettingKeyResponsesWSEnabled               SettingKey = "responses_ws_enabled"                 // 是否启用 OpenAI Responses WS 上游能力（仅客户端 WS 入站）
	SettingKeyResponsesWSDefaultMode           SettingKey = "responses_ws_default_mode"            // OpenAI Responses WS 默认模式：off/transform/passthrough
	SettingKeySSEHeartbeatInterval             SettingKey = "sse_heartbeat_interval"               // SSE 流式心跳间隔（秒），0 表示禁用
	SettingKeySSEPreStreamHeartbeatDelay       SettingKey = "sse_pre_stream_heartbeat_delay"       // SSE 上游流建立前心跳首次延迟（秒），0 表示禁用
	SettingKeyGroupHealthEnabled               SettingKey = "group_health_enabled"                 // 是否启用分组健康检查功能
	SettingKeyProjectedChannelAutoGroupEnabled SettingKey = "projected_channel_auto_group_enabled" // 全局站点投影渠道自动分组模式（0关闭/1模糊/2精确/3正则，兼容旧 true/false）
	SettingKeyJWTSecret                        SettingKey = "jwt_secret"                           // JWT 签名密钥（自动生成）
	SettingKeyStatsSiteModelBackfilled         SettingKey = "stats_site_model_backfilled"          // 站点渠道小时聚合是否已回填历史日志
	SettingKeyOutlierRetireEnabled             SettingKey = "outlier_retire_enabled"               // 被动离群退役(POR)总开关
	SettingKeyOutlierRetireInterval            SettingKey = "outlier_retire_interval"              // POR 任务轮询间隔(分钟)
	SettingKeyOutlierWindowCapacity            SettingKey = "outlier_window_capacity"              // POR 滚动窗口评估样本上限(≤20)
	SettingKeyOutlierWindowMinutes             SettingKey = "outlier_window_minutes"               // POR 滚动窗口时间窗(分钟)
	SettingKeyOutlierMinSamples                SettingKey = "outlier_min_samples"                  // POR 最小样本数,不足则跳过判定
	SettingKeyOutlierFailRatePct               SettingKey = "outlier_fail_rate_pct"                // POR 失败率阈值(百分比)
	SettingKeyOutlierConsecFails               SettingKey = "outlier_consec_fails"                 // POR 连续失败阈值
	SettingKeyOutlierRecoverStreak             SettingKey = "outlier_recover_streak"               // POR 连续探活成功恢复阈值
	SettingKeyOutlierReapMinutes               SettingKey = "outlier_reap_minutes"                 // POR 窗口内存回收 TTL(分钟)
	SettingKeyOutlierCFRecoverMinutes          SettingKey = "outlier_cf_recover_minutes"           // POR CF 退役渠道恢复探活冷却(分钟)
	SettingKeyApiBaseUrl                       SettingKey = "api_base_url"                         // 对外服务基础地址，用于一键导出客户端配置，为空时不显示导出入口
	SettingKeyWebDAVURL                        SettingKey = "webdav_url"                            // WebDAV 服务器地址
	SettingKeyWebDAVUsername                   SettingKey = "webdav_username"                       // WebDAV 用户名
	SettingKeyWebDAVPassword                   SettingKey = "webdav_password"                       // WebDAV 密码
	SettingKeyWebDAVBackupPath                 SettingKey = "webdav_backup_path"                    // WebDAV 远程备份目录
	SettingKeyWebDAVBackupInterval             SettingKey = "webdav_backup_interval"                // WebDAV 自动备份间隔(小时)，0=禁用
	SettingKeyWebDAVRetentionCount             SettingKey = "webdav_retention_count"                // WebDAV 保留备份份数
	SettingKeyWebDAVIncludeStats               SettingKey = "webdav_include_stats"                  // WebDAV 备份是否包含统计数据
)

type Setting struct {
	Key   SettingKey `json:"key" gorm:"primaryKey"`
	Value string     `json:"value" gorm:"not null"`
}

func DefaultSettings() []Setting {
	return []Setting{
		{Key: SettingKeyProxyURL, Value: ""},
		{Key: SettingKeyStatsSaveInterval, Value: "10"},               // 默认10分钟保存一次统计信息
		{Key: SettingKeyCORSAllowOrigins, Value: ""},                  // CORS 默认不允许跨域，设置为 "*" 才允许所有来源
		{Key: SettingKeyModelInfoUpdateInterval, Value: "24"},         // 默认24小时更新一次模型信息
		{Key: SettingKeySyncLLMInterval, Value: "24"},                 // 默认24小时同步一次LLM
		{Key: SettingKeySiteSyncInterval, Value: "12"},                // 默认12小时同步一次站点账号信息
		{Key: SettingKeySiteCheckinInterval, Value: "24"},             // 默认24小时自动签到一次
		{Key: SettingKeyRelayLogKeepPeriod, Value: "7"},               // 默认日志保存7天
		{Key: SettingKeyRelayLogKeepEnabled, Value: "true"},           // 默认保留历史日志
		{Key: SettingKeyCircuitBreakerThreshold, Value: "5"},          // 默认连续失败5次触发熔断
		{Key: SettingKeyCircuitBreakerCooldown, Value: "60"},          // 默认基础冷却60秒
		{Key: SettingKeyCircuitBreakerMaxCooldown, Value: "600"},      // 默认最大冷却600秒（10分钟）
		{Key: SettingKeyResponsesWSEnabled, Value: "false"},           // 默认关闭 OpenAI Responses WS 新路径
		{Key: SettingKeyResponsesWSDefaultMode, Value: "passthrough"}, // 启用后默认使用协议保真的 passthrough
		{Key: SettingKeySSEHeartbeatInterval, Value: "0"},             // 默认禁用 SSE 流式心跳
		{Key: SettingKeySSEPreStreamHeartbeatDelay, Value: "0"},       // 默认禁用 SSE 上游流建立前心跳
		{Key: SettingKeyGroupHealthEnabled, Value: "false"},           // 默认不显示/运行分组健康检查，避免打扰主界面
		{Key: SettingKeyProjectedChannelAutoGroupEnabled, Value: "0"}, // 默认不强制站点投影渠道自动分组
		{Key: SettingKeyJWTSecret, Value: ""},                         // 为空时自动生成
		{Key: SettingKeyStatsSiteModelBackfilled, Value: "false"},
		{Key: SettingKeyOutlierRetireEnabled, Value: "false"}, // 默认关闭被动离群退役，保守上线
		{Key: SettingKeyOutlierRetireInterval, Value: "2"},    // 默认每 2 分钟评估一次
		{Key: SettingKeyOutlierWindowCapacity, Value: "20"},   // 评估取最近 20 条
		{Key: SettingKeyOutlierWindowMinutes, Value: "10"},    // 时间窗 10 分钟
		{Key: SettingKeyOutlierMinSamples, Value: "8"},        // 样本不足 8 条直接 PASS
		{Key: SettingKeyOutlierFailRatePct, Value: "85"},      // 失败率 ≥85% 才候选
		{Key: SettingKeyOutlierConsecFails, Value: "10"},      // 连续失败 ≥10 次
		{Key: SettingKeyOutlierRecoverStreak, Value: "2"},     // 连续探活成功 2 次恢复
		{Key: SettingKeyOutlierReapMinutes, Value: "30"},      // 窗口 30 分钟无流量回收
		{Key: SettingKeyOutlierCFRecoverMinutes, Value: "30"}, // CF 退役渠道 30 分钟后才探活恢复
		{Key: SettingKeyApiBaseUrl, Value: ""},                  // 默认为空，不显示客户端导出入口
		{Key: SettingKeyWebDAVURL, Value: ""},                   // 默认为空，未配置
		{Key: SettingKeyWebDAVUsername, Value: ""},              // 默认为空
		{Key: SettingKeyWebDAVPassword, Value: ""},              // 默认为空
		{Key: SettingKeyWebDAVBackupPath, Value: "/octopus-backups"}, // 默认远程目录
		{Key: SettingKeyWebDAVBackupInterval, Value: "0"},       // 默认禁用自动备份
		{Key: SettingKeyWebDAVRetentionCount, Value: "10"},      // 默认保留10份
		{Key: SettingKeyWebDAVIncludeStats, Value: "true"},      // 默认包含统计数据
	}
}

func (s *Setting) Validate() error {
	switch s.Key {
	case SettingKeyModelInfoUpdateInterval, SettingKeySyncLLMInterval, SettingKeySiteSyncInterval,
		SettingKeySiteCheckinInterval, SettingKeyRelayLogKeepPeriod,
		SettingKeyCircuitBreakerThreshold, SettingKeyCircuitBreakerCooldown, SettingKeyCircuitBreakerMaxCooldown:
		_, err := strconv.Atoi(s.Value)
		if err != nil {
			return fmt.Errorf("setting value must be an integer")
		}
		return nil
	case SettingKeyOutlierWindowCapacity:
		// 评估样本上限受环形缓冲物理容量约束（≤20，见 outlierwindow.physicalCap）。
		return validateIntRange(s.Value, 1, 20)
	case SettingKeyOutlierFailRatePct:
		// 失败率阈值为百分比，超出 [1,100] 会被运行时回退默认值，与展示不符。
		return validateIntRange(s.Value, 1, 100)
	case SettingKeyOutlierRetireInterval, SettingKeyOutlierWindowMinutes, SettingKeyOutlierMinSamples,
		SettingKeyOutlierConsecFails, SettingKeyOutlierRecoverStreak,
		SettingKeyOutlierReapMinutes, SettingKeyOutlierCFRecoverMinutes,
		SettingKeyWebDAVRetentionCount:
		// 时间窗/样本/连击/间隔等：0 或负值无意义，下限为 1。
		return validateIntMin(s.Value, 1)
	case SettingKeySSEHeartbeatInterval, SettingKeySSEPreStreamHeartbeatDelay, SettingKeyWebDAVBackupInterval:
		value, err := strconv.Atoi(s.Value)
		if err != nil {
			return fmt.Errorf("setting value must be an integer")
		}
		if value < 0 {
			return fmt.Errorf("setting value must be non-negative")
		}
		return nil
	case SettingKeyRelayLogKeepEnabled, SettingKeyResponsesWSEnabled, SettingKeyGroupHealthEnabled, SettingKeyStatsSiteModelBackfilled, SettingKeyOutlierRetireEnabled, SettingKeyWebDAVIncludeStats:
		if s.Value != "true" && s.Value != "false" {
			return fmt.Errorf("setting value must be true or false")
		}
		return nil
	case SettingKeyProjectedChannelAutoGroupEnabled:
		if _, ok := ParseAutoGroupSettingValue(s.Value); !ok {
			return fmt.Errorf("setting value must be one of 0, 1, 2, 3, true, false")
		}
		return nil
	case SettingKeyResponsesWSDefaultMode:
		switch s.Value {
		case "off", "transform", "passthrough":
			return nil
		default:
			return fmt.Errorf("setting value must be one of off, transform, passthrough")
		}
	case SettingKeyProxyURL:
		if s.Value == "" {
			return nil
		}
		parsedURL, err := url.Parse(s.Value)
		if err != nil {
			return fmt.Errorf("proxy URL is invalid: %w", err)
		}
		validSchemes := map[string]bool{
			"http":   true,
			"https":  true,
			"socks5": true,
		}
		if !validSchemes[parsedURL.Scheme] {
			return fmt.Errorf("proxy URL scheme must be http, https, socks, or socks5")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("proxy URL must have a host")
		}
		return nil
	case SettingKeyApiBaseUrl:
		if s.Value == "" {
			return nil
		}
		parsedURL, err := url.Parse(s.Value)
		if err != nil {
			return fmt.Errorf("api base URL is invalid: %w", err)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("api base URL scheme must be http or https")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("api base URL must have a host")
		}
		return nil
	case SettingKeyWebDAVURL:
		if s.Value == "" {
			return nil
		}
		parsedURL, err := url.Parse(s.Value)
		if err != nil {
			return fmt.Errorf("WebDAV URL is invalid: %w", err)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("WebDAV URL scheme must be http or https")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("WebDAV URL must have a host")
		}
		return nil
	}

	return nil
}

// validateIntRange 校验 v 为整数且落在闭区间 [lo, hi]。
func validateIntRange(v string, lo, hi int) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("setting value must be an integer")
	}
	if n < lo || n > hi {
		return fmt.Errorf("setting value must be between %d and %d", lo, hi)
	}
	return nil
}

// validateIntMin 校验 v 为整数且不小于 lo。
func validateIntMin(v string, lo int) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("setting value must be an integer")
	}
	if n < lo {
		return fmt.Errorf("setting value must be at least %d", lo)
	}
	return nil
}
