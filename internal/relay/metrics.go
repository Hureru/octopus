package relay

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"github.com/bestruirui/octopus/internal/utils/tokenizer"
)

// RelayMetrics 负责最终的日志收集与持久化
type RelayMetrics struct {
	APIKeyID     int
	RequestModel string
	StartTime    time.Time

	// 首 Token 时间
	FirstTokenTime time.Time

	// 请求和响应内容
	RawRequest       []byte
	InternalRequest  *transformerModel.InternalLLMRequest
	InternalResponse *transformerModel.InternalLLMResponse

	// 统计指标
	ActualModel string
	Stats       model.StatsMetrics
	UsedWS      bool
	WSMode      *model.RelayLogWSMode
	WSExecMode  *model.RelayLogWSExecMode
	WSRecovery  *model.RelayLogWSRecovery

	TransportInputTokens *int

	logID int64 // 进行中时提前分配的日志 ID；0 表示还没在日志页露过面

	effort         string // reasoningEffort 的结果缓存
	effortResolved bool
}

func NewRelayMetrics(apiKeyID int, requestModel string, rawBody []byte, req *transformerModel.InternalLLMRequest) *RelayMetrics {
	return &RelayMetrics{
		APIKeyID:        apiKeyID,
		RequestModel:    requestModel,
		StartTime:       time.Now(),
		RawRequest:      rawBody,
		InternalRequest: req,
	}
}

// reasoningEffort 返回客户端请求的思考等级，未指定时为空。
// 入站转换把各协议归一到 InternalRequest.ReasoningEffort，但 Responses 入站只保留白名单内的值
// （xhigh、none 会被清空），而透传时原值照样发给了上游；归一字段为空时再读一次原始请求体，
// 日志才和实际发出去的一致。
func (m *RelayMetrics) reasoningEffort() string {
	if m.effortResolved {
		return m.effort
	}
	m.effortResolved = true
	if m.InternalRequest != nil {
		m.effort = m.InternalRequest.ReasoningEffort
	}
	if m.effort == "" && len(m.RawRequest) > 0 {
		var raw struct {
			ReasoningEffort string `json:"reasoning_effort"` // OpenAI Chat
			Reasoning       struct {
				Effort string `json:"effort"`
			} `json:"reasoning"` // OpenAI Responses
			OutputConfig struct {
				Effort string `json:"effort"`
			} `json:"output_config"` // Anthropic Messages
		}
		// 字段类型对不上时只是那一项读不到，其余照常，所以不看错误。
		_ = json.Unmarshal(m.RawRequest, &raw)
		m.effort = cmp.Or(raw.ReasoningEffort, raw.Reasoning.Effort, raw.OutputConfig.Effort)
	}
	return m.effort
}

func (m *RelayMetrics) SetFirstTokenTime(t time.Time) {
	m.FirstTokenTime = t
}

func (m *RelayMetrics) SetTransportRequestPayload(payload []byte, modelName string) {
	if len(payload) == 0 {
		return
	}
	count := tokenizer.CountTokens(string(payload), modelName)
	m.TransportInputTokens = intPtr(count)
}

func (m *RelayMetrics) SetWSMode(mode model.RelayLogWSMode) {
	if mode == "" {
		return
	}
	m.WSMode = wsModePtr(mode)
}

func (m *RelayMetrics) SetWSExecMode(mode model.RelayLogWSExecMode) {
	if mode == "" {
		return
	}
	m.WSExecMode = wsExecModePtr(mode)
}

func (m *RelayMetrics) SetWSRecovery(recovery model.RelayLogWSRecovery) {
	if recovery == "" {
		return
	}
	m.WSRecovery = wsRecoveryPtr(recovery)
}

// PublishProgress 把重试中的请求推给日志页，用作 balancer.Iterator.OnChange。
// 只有已经失败过的请求才会露面：一次就成功的请求仍然只在结束时产生一条日志。
func (m *RelayMetrics) PublishProgress(attempts []model.ChannelAttempt) {
	if len(attempts) == 0 {
		return
	}
	if m.logID == 0 {
		if !slices.ContainsFunc(attempts, func(a model.ChannelAttempt) bool { return a.Status == model.AttemptFailed }) {
			return
		}
		m.logID = snowflake.GenerateID()
	}
	last := attempts[len(attempts)-1]
	relayLog := model.RelayLog{
		ID:               m.logID,
		Time:             m.StartTime.Unix(),
		RequestModelName: m.RequestModel,
		ChannelId:        last.ChannelID,
		ChannelName:      last.ChannelName,
		ActualModelName:  last.ModelName,
		ReasoningEffort:  m.reasoningEffort(),
		UseTime:          int(time.Since(m.StartTime).Milliseconds()),
		Attempts:         attempts,
		TotalAttempts:    len(attempts),
		UsedWS:           m.UsedWS,
	}
	if apiKey, err := op.APIKeyGet(m.APIKeyID, context.Background()); err == nil {
		relayLog.RequestAPIKeyName = apiKey.Name
	}
	op.RelayLogLiveUpdate(relayLog)
}

// DropLive 在请求退出时兜底清理：正常路径 saveLog 已经清掉了，这里防的是 panic，
// 否则这条记录会一直留在内存里，每个新打开的日志页都会收到一张永远转圈的卡片。
func (m *RelayMetrics) DropLive() {
	if m.logID != 0 {
		op.RelayLogLiveDelete(m.logID)
	}
}

func (m *RelayMetrics) SetInternalResponse(resp *transformerModel.InternalLLMResponse, actualModel string) {
	m.InternalResponse = resp
	m.ActualModel = actualModel

	if resp == nil {
		return
	}

	inputReported := false
	if usage := resp.Usage; usage != nil {
		nonCachedInput := usage.BillableNonCachedInput()
		cacheReadTokens := usage.BillableCacheReadInput()
		cacheWriteTokens := usage.BillableCacheWriteInput()

		// 归一化：总输入 = 非缓存 + 缓存读 + 缓存写，缓存是子集，命中率不会超过 100%。
		m.Stats.InputToken = nonCachedInput + cacheReadTokens + cacheWriteTokens
		m.Stats.CacheReadToken = cacheReadTokens
		m.Stats.CacheWriteToken = cacheWriteTokens
		m.Stats.OutputToken = usage.CompletionTokens
		inputReported = m.Stats.InputToken > 0

		if modelPrice := resolveModelPrice(actualModel); modelPrice != nil {
			m.Stats.InputCost = (float64(cacheReadTokens)*modelPrice.CacheRead +
				float64(cacheWriteTokens)*modelPrice.CacheWrite +
				float64(nonCachedInput)*modelPrice.Input) * 1e-6
			m.Stats.OutputCost = float64(usage.CompletionTokens) * modelPrice.Output * 1e-6
		}
	}

	// 降级：上游未上报 input（usage 缺失，或 usage 中输入侧全为 0）时，用请求侧
	// 估算的 TransportInputTokens 兜底，使 input token/费用不为 0；output 无法从
	// 请求侧估算，保持 0。tiktoken 统一用 o200k_base，对 Claude/Gemini 为近似值。
	if !inputReported && m.TransportInputTokens != nil && *m.TransportInputTokens > 0 {
		estimated := int64(*m.TransportInputTokens)
		m.Stats.InputToken = estimated
		if modelPrice := resolveModelPrice(actualModel); modelPrice != nil {
			m.Stats.InputCost = float64(estimated) * modelPrice.Input * 1e-6
		}
	}
}

func (m *RelayMetrics) Save(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt) {
	m.SaveWithChannelStats(ctx, success, err, attempts, true)
}

func (m *RelayMetrics) SaveWithChannelStats(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt, updateChannelStats bool) {
	duration := time.Since(m.StartTime)

	globalStats := m.Stats // 只含 token 与费用
	globalStats.WaitTime = duration.Milliseconds()
	if success {
		globalStats.RequestSuccess = 1
	} else {
		globalStats.RequestFailed = 1
	}

	channelID, channelName := finalChannel(attempts)
	op.StatsTotalUpdate(globalStats)
	op.StatsHourlyUpdate(globalStats)
	op.StatsDailyUpdate(context.Background(), globalStats)
	op.StatsAPIKeyUpdate(m.APIKeyID, globalStats)
	if updateChannelStats {
		op.StatsChannelUpdate(channelID, globalStats)
	} else {
		updateFinalChannelUsageStats(channelID, globalStats)
	}
	op.StatsSiteModelHourlyRecordAttempts(attempts, m.ActualModel)

	// 上游未上报 usage（或输入侧全为 0）时打告警，便于定位是哪个通道缺失 usage。
	if success && (m.InternalResponse == nil || m.InternalResponse.Usage == nil ||
		m.InternalResponse.Usage.EffectiveInputTokens() == 0) {
		fallbackInput := 0
		if m.TransportInputTokens != nil {
			fallbackInput = *m.TransportInputTokens
		}
		log.Debugw("relay.usage_missing",
			"actual_model", m.ActualModel,
			"channel_id", channelID,
			"channel", channelName,
			"had_usage", m.InternalResponse != nil && m.InternalResponse.Usage != nil,
			"fallback_input_tokens", fallbackInput,
		)
	}

	if conf.AppConfig.Log.Relay.Summary || !success {
		fields := []interface{}{
			"model", m.RequestModel,
			"actual_model", m.ActualModel,
			"channel_id", channelID,
			"channel", channelName,
			"success", success,
			"duration_ms", duration.Milliseconds(),
			"input_token", m.Stats.InputToken,
			"output_token", m.Stats.OutputToken,
			"input_cost", m.Stats.InputCost,
			"output_cost", m.Stats.OutputCost,
			"total_cost", m.Stats.InputCost + m.Stats.OutputCost,
			"attempts", len(attempts),
			"ws", m.UsedWS,
		}
		if success {
			log.Infow("relay.complete", fields...)
		} else {
			log.Warnw("relay.complete", fields...)
		}
	}

	m.saveLog(ctx, success, err, duration, attempts, channelID, channelName)
}

func finalChannel(attempts []model.ChannelAttempt) (int, string) {
	var lastID int
	var lastName string
	for i := len(attempts) - 1; i >= 0; i-- {
		a := attempts[i]
		if a.Status == model.AttemptSuccess {
			return a.ChannelID, a.ChannelName
		}
		if a.Status == model.AttemptFailed && lastID == 0 {
			lastID = a.ChannelID
			lastName = a.ChannelName
		}
	}
	return lastID, lastName
}

func (m *RelayMetrics) saveLog(ctx context.Context, success bool, err error, duration time.Duration, attempts []model.ChannelAttempt, channelID int, channelName string) {
	actualModel := m.ActualModel
	if actualModel == "" {
		actualModel = m.RequestModel
	}

	relayLog := model.RelayLog{
		ID:               m.logID,
		Time:             m.StartTime.Unix(),
		RequestModelName: m.RequestModel,
		ChannelName:      channelName,
		ChannelId:        channelID,
		ActualModelName:  actualModel,
		ReasoningEffort:  m.reasoningEffort(),
		UseTime:          int(duration.Milliseconds()),
		Attempts:         attempts,
		TotalAttempts:    len(attempts),
		UsedWS:           m.UsedWS,
	}

	if apiKey, getErr := op.APIKeyGet(m.APIKeyID, ctx); getErr == nil {
		relayLog.RequestAPIKeyName = apiKey.Name
	}

	// 首字时间
	if !m.FirstTokenTime.IsZero() {
		relayLog.Ftut = int(m.FirstTokenTime.Sub(m.StartTime).Milliseconds())
	}

	// Usage：统一从 Stats 读取。Stats 在 SetInternalResponse 中已由上游 usage 填充，
	// 或在 usage 缺失时由 TransportInputTokens 降级填充，确保降级值也写入日志。
	relayLog.InputTokens = int(m.Stats.InputToken)
	relayLog.OutputTokens = int(m.Stats.OutputToken)
	relayLog.Cost = m.Stats.InputCost + m.Stats.OutputCost
	relayLog.TransportInputTokens = m.TransportInputTokens
	relayLog.BillInputTokens = intPtr(int(m.Stats.InputToken - m.Stats.CacheReadToken - m.Stats.CacheWriteToken))
	relayLog.CacheReadTokens = intPtr(int(m.Stats.CacheReadToken))
	relayLog.CacheWriteTokens = intPtr(int(m.Stats.CacheWriteToken))
	relayLog.WSMode = m.WSMode
	relayLog.WSExecMode = m.WSExecMode
	relayLog.WSRecovery = m.WSRecovery

	// 请求内容：优先原始请求体，保留 provider 专有字段（如 Anthropic cache_control）
	if len(m.RawRequest) > 0 {
		relayLog.RequestContent = string(m.RawRequest)
	} else if m.InternalRequest != nil {
		if reqJSON, jsonErr := json.Marshal(m.InternalRequest); jsonErr == nil {
			relayLog.RequestContent = string(reqJSON)
		}
	}

	// 响应内容
	if m.InternalResponse != nil {
		respForLog := m.filterResponseForLog(m.InternalResponse)
		if respJSON, jsonErr := json.Marshal(respForLog); jsonErr == nil {
			relayLog.ResponseContent = string(respJSON)
		}
	}

	// 错误信息
	if err != nil {
		relayLog.Error = err.Error()
	}
	relayLog.Success = success

	if logErr := op.RelayLogAdd(ctx, relayLog); logErr != nil {
		log.Warnf("failed to save relay log: %v", logErr)
	}
}

func updateFinalChannelUsageStats(channelID int, metrics model.StatsMetrics) {
	if channelID == 0 {
		return
	}
	usageStats := metrics
	usageStats.WaitTime, usageStats.RequestSuccess, usageStats.RequestFailed = 0, 0, 0
	if usageStats == (model.StatsMetrics{}) {
		return
	}
	op.StatsChannelUpdate(channelID, usageStats)
}

func intPtr(value int) *int {
	return &value
}

// resolveModelPrice returns the global price configured for the actual model.
func resolveModelPrice(actualModel string) *model.LLMPrice {
	return price.GetLLMPrice(actualModel)
}

func wsModePtr(value model.RelayLogWSMode) *model.RelayLogWSMode {
	return &value
}

func wsExecModePtr(value model.RelayLogWSExecMode) *model.RelayLogWSExecMode {
	return &value
}

func wsRecoveryPtr(value model.RelayLogWSRecovery) *model.RelayLogWSRecovery {
	return &value
}

// filterResponseForLog 创建响应的浅拷贝，过滤掉 images、MultipleContent 中的图片数据和 Audio.Data 以减少存储压力
func (m *RelayMetrics) filterResponseForLog(resp *transformerModel.InternalLLMResponse) *transformerModel.InternalLLMResponse {
	if resp == nil {
		return nil
	}

	filterMsg := func(msg *transformerModel.Message) *transformerModel.Message {
		if msg == nil {
			return nil
		}
		c := *msg
		c.Images = nil
		if len(c.Content.MultipleContent) > 0 {
			parts := make([]transformerModel.MessageContentPart, 0, len(c.Content.MultipleContent))
			for _, p := range c.Content.MultipleContent {
				if p.Type == "image_url" && p.ImageURL != nil {
					parts = append(parts, transformerModel.MessageContentPart{
						Type:     "image_url",
						ImageURL: &transformerModel.ImageURL{URL: "[image data omitted for storage]"},
					})
				} else {
					parts = append(parts, p)
				}
			}
			c.Content = transformerModel.MessageContent{Content: c.Content.Content, MultipleContent: parts}
		}
		if c.Audio != nil && c.Audio.Data != "" {
			a := *c.Audio
			a.Data = "[audio data omitted for storage]"
			c.Audio = &a
		}
		return &c
	}

	filtered := *resp
	filtered.Choices = make([]transformerModel.Choice, len(resp.Choices))
	for i, choice := range resp.Choices {
		filtered.Choices[i] = choice
		filtered.Choices[i].Message = filterMsg(choice.Message)
		filtered.Choices[i].Delta = filterMsg(choice.Delta)
	}
	return &filtered
}
