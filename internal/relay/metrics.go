package relay

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
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
	ActualModel       string
	Stats             model.StatsMetrics
	UsedWS            bool
	WSMode            *model.RelayLogWSMode
	WSRecovery        *model.RelayLogWSRecovery
	SelectedChannelID int

	TransportInputTokens *int
	BillInputTokens      *int
	CacheReadTokens      *int
	CacheWriteTokens     *int
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

func (m *RelayMetrics) SetWSRecovery(recovery model.RelayLogWSRecovery) {
	if recovery == "" {
		return
	}
	m.WSRecovery = wsRecoveryPtr(recovery)
}

// SetSelectedChannel 记录此次命中的通道 ID，用于 SetInternalResponse 时按站点 (账号, 分组) 查询价格。
func (m *RelayMetrics) SetSelectedChannel(channelID int) {
	m.SelectedChannelID = channelID
}

func (m *RelayMetrics) SetInternalResponse(resp *transformerModel.InternalLLMResponse, actualModel string) {
	m.InternalResponse = resp
	m.ActualModel = actualModel

	if resp == nil || resp.Usage == nil {
		return
	}

	usage := resp.Usage
	nonCachedInput := usage.BillableNonCachedInput()
	cacheReadTokens := usage.BillableCacheReadInput()
	cacheWriteTokens := usage.BillableCacheWriteInput()

	m.BillInputTokens = intPtr(int(nonCachedInput))
	m.CacheReadTokens = intPtr(int(cacheReadTokens))
	m.CacheWriteTokens = intPtr(int(cacheWriteTokens))
	m.Stats.InputToken = usage.PromptTokens
	m.Stats.OutputToken = usage.CompletionTokens

	modelPrice := resolveModelPrice(m.SelectedChannelID, actualModel)
	if modelPrice == nil {
		return
	}
	m.Stats.InputCost = (float64(cacheReadTokens)*modelPrice.CacheRead +
		float64(cacheWriteTokens)*modelPrice.CacheWrite +
		float64(nonCachedInput)*modelPrice.Input) * 1e-6
	m.Stats.OutputCost = float64(usage.CompletionTokens) * modelPrice.Output * 1e-6
}

func (m *RelayMetrics) Save(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt) {
	duration := time.Since(m.StartTime)

	globalStats := model.StatsMetrics{
		WaitTime:    duration.Milliseconds(),
		InputToken:  m.Stats.InputToken,
		OutputToken: m.Stats.OutputToken,
		InputCost:   m.Stats.InputCost,
		OutputCost:  m.Stats.OutputCost,
	}
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
	op.StatsChannelUpdate(channelID, globalStats)
	op.StatsSiteModelHourlyRecordAttempts(attempts, m.ActualModel)

	log.Infof("relay complete: model=%s, channel=%d(%s), success=%t, duration=%dms, input_token=%d, output_token=%d, input_cost=%f, output_cost=%f, total_cost=%f, attempts=%d",
		m.RequestModel, channelID, channelName, success, duration.Milliseconds(),
		m.Stats.InputToken, m.Stats.OutputToken,
		m.Stats.InputCost, m.Stats.OutputCost, m.Stats.InputCost+m.Stats.OutputCost,
		len(attempts))

	m.saveLog(ctx, err, duration, attempts, channelID, channelName)
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

func (m *RelayMetrics) saveLog(ctx context.Context, err error, duration time.Duration, attempts []model.ChannelAttempt, channelID int, channelName string) {
	actualModel := m.ActualModel
	if actualModel == "" {
		actualModel = m.RequestModel
	}

	relayLog := model.RelayLog{
		Time:             m.StartTime.Unix(),
		RequestModelName: m.RequestModel,
		ChannelName:      channelName,
		ChannelId:        channelID,
		ActualModelName:  actualModel,
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

	// Usage
	if m.InternalResponse != nil && m.InternalResponse.Usage != nil {
		relayLog.InputTokens = int(m.InternalResponse.Usage.PromptTokens)
		relayLog.OutputTokens = int(m.InternalResponse.Usage.CompletionTokens)
		relayLog.Cost = m.Stats.InputCost + m.Stats.OutputCost
	}
	relayLog.TransportInputTokens = m.TransportInputTokens
	relayLog.BillInputTokens = m.BillInputTokens
	relayLog.CacheReadTokens = m.CacheReadTokens
	relayLog.CacheWriteTokens = m.CacheWriteTokens
	relayLog.WSMode = m.WSMode
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

	if logErr := op.RelayLogAdd(ctx, relayLog); logErr != nil {
		log.Warnf("failed to save relay log: %v", logErr)
	}
}

func intPtr(value int) *int {
	return &value
}

// resolveModelPrice 优先从站点 (账号, 分组) 维度查价；缺失时回退到 models.dev 全局价格。
// 保证与原 price.GetLLMPrice 相同的签名（*model.LLMPrice），不改变调用方计费数学。
func resolveModelPrice(channelID int, actualModel string) *model.LLMPrice {
	if channelID > 0 {
		binding, err := op.SiteChannelBindingGetByChannelID(channelID, context.Background())
		if err == nil && binding != nil {
			baseGroupKey, _ := model.ParseSiteChannelBindingKey(binding.GroupKey)
			if sitePrice, ok := op.SitePriceGet(binding.SiteAccountID, baseGroupKey, actualModel); ok {
				resolved := sitePrice
				return &resolved
			}
		}
	}
	return price.GetLLMPrice(actualModel)
}

func wsModePtr(value model.RelayLogWSMode) *model.RelayLogWSMode {
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
