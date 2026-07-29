package relay

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
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
	ActualModel          string
	Stats                model.StatsMetrics
	UsedWS               bool
	WSMode               *model.RelayLogWSMode
	WSExecMode           *model.RelayLogWSExecMode
	WSRecovery           *model.RelayLogWSRecovery
	Outcome              model.RequestOutcome
	TransportTermination model.TransportTermination
	CompletionEvidence   model.CompletionEvidence
	TerminalEvent        string
	CanonicalModelName   string
	CanonicalModelID     int
	RouteCandidateID     int
	InboundProtocol      model.ProtocolName
	OutboundProtocol     model.ProtocolName
	ProtocolMode         string
	ProtocolPolicy       model.ProtocolPolicy
	ProtocolAllowLossy   bool
	ProtocolWarnings     []string
	ProtocolFailureStage model.ProtocolFailureStage
	HeaderPolicyTrace    string
	EffectivePrice       *model.EffectivePrice
	PriceOriginalCost    float64
	TokenSource          model.UsageValueSource

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

func (m *RelayMetrics) SetRouting(
	canonical model.CanonicalModel,
	candidateID int,
	inboundProtocol model.ProtocolName,
	outboundProtocol model.ProtocolName,
	protocolMode string,
) {
	m.CanonicalModelName = canonical.Name
	m.CanonicalModelID = canonical.ID
	m.RouteCandidateID = candidateID
	m.InboundProtocol = inboundProtocol
	m.OutboundProtocol = outboundProtocol
	m.ProtocolMode = protocolMode
}

func (m *RelayMetrics) SetProtocolDecision(
	inboundProtocol model.ProtocolName,
	outboundProtocol model.ProtocolName,
	mode model.ProtocolExecutionMode,
	policy model.ProtocolPolicy,
	allowLossy bool,
	warnings []string,
) {
	m.InboundProtocol = inboundProtocol
	m.OutboundProtocol = outboundProtocol
	m.ProtocolMode = string(mode)
	m.ProtocolPolicy = policy
	m.ProtocolAllowLossy = allowLossy
	m.ProtocolWarnings = append(m.ProtocolWarnings[:0], warnings...)
}

func (m *RelayMetrics) SetProtocolFailureStage(stage model.ProtocolFailureStage) {
	if stage != "" {
		m.ProtocolFailureStage = stage
	}
}

func (m *RelayMetrics) SetEffectivePrice(value model.EffectivePrice) {
	m.EffectivePrice = &value
}

func (m *RelayMetrics) ClearEffectivePrice() {
	m.EffectivePrice = nil
	m.PriceOriginalCost = 0
}

func (m *RelayMetrics) AttemptUsageSnapshot() *model.AttemptUsageSnapshot {
	if m == nil {
		return nil
	}
	inputTokens := m.Stats.InputToken
	if m.BillInputTokens != nil {
		inputTokens = max(0, intPointerValue(m.BillInputTokens)) +
			max(0, intPointerValue(m.CacheReadTokens)) +
			max(0, intPointerValue(m.CacheWriteTokens))
	}
	costUSD := m.Stats.InputCost + m.Stats.OutputCost
	if inputTokens == 0 && m.Stats.OutputToken == 0 && costUSD == 0 &&
		m.TokenSource != model.UsageValueSourceReported &&
		m.TokenSource != model.UsageValueSourceEstimated {
		return nil
	}
	tokenSource := m.TokenSource
	if tokenSource == "" {
		tokenSource = model.UsageValueSourceUnknown
	}
	snapshot := &model.AttemptUsageSnapshot{
		InputTokens:       inputTokens,
		OutputTokens:      m.Stats.OutputToken,
		CacheReadTokens:   intPointerValue(m.CacheReadTokens),
		CacheWriteTokens:  intPointerValue(m.CacheWriteTokens),
		CostUSD:           costUSD,
		TokenSource:       tokenSource,
		PriceOriginalCost: m.PriceOriginalCost,
	}
	if !m.FirstTokenTime.IsZero() {
		snapshot.FTUTKnown = true
		snapshot.FTUTMS = m.FirstTokenTime.Sub(m.StartTime).Milliseconds()
	}
	if m.EffectivePrice != nil {
		snapshot.PriceQuoteID = m.EffectivePrice.QuoteID
		snapshot.PriceSource = m.EffectivePrice.Source
		snapshot.PriceUnit = m.EffectivePrice.Unit
		snapshot.PriceCurrency = m.EffectivePrice.Currency
		snapshot.PriceInput = m.EffectivePrice.Input
		snapshot.PriceOutput = m.EffectivePrice.Output
		snapshot.PriceCacheRead = m.EffectivePrice.CacheRead
		snapshot.PriceCacheWrite = m.EffectivePrice.CacheWrite
		snapshot.PricePerRequest = m.EffectivePrice.PerRequest
		snapshot.PriceMultiplier = m.EffectivePrice.GroupMultiplier
		snapshot.PriceRateToUSD = m.EffectivePrice.ExchangeRateToUSD
		snapshot.PriceObservedAt = m.EffectivePrice.ObservedAt
		snapshot.PriceStale = m.EffectivePrice.Stale
		snapshot.PriceConvertible = m.EffectivePrice.Convertible
		snapshot.PriceMatchReason = m.EffectivePrice.MatchReason
	}
	return snapshot
}

func (m *RelayMetrics) SetInternalResponse(resp *transformerModel.InternalLLMResponse, actualModel string) {
	m.InternalResponse = resp
	m.ActualModel = actualModel
	m.Stats.InputToken = 0
	m.Stats.OutputToken = 0
	m.Stats.InputCost = 0
	m.Stats.OutputCost = 0
	m.BillInputTokens = nil
	m.CacheReadTokens = nil
	m.CacheWriteTokens = nil
	m.PriceOriginalCost = 0
	m.TokenSource = model.UsageValueSourceUnknown

	if resp == nil {
		return
	}

	inputReported := false
	if usage := resp.Usage; usage != nil {
		nonCachedInput := usage.BillableNonCachedInput()
		cacheReadTokens := usage.BillableCacheReadInput()
		cacheWriteTokens := usage.BillableCacheWriteInput()

		m.BillInputTokens = intPtr(int(nonCachedInput))
		m.CacheReadTokens = intPtr(int(cacheReadTokens))
		m.CacheWriteTokens = intPtr(int(cacheWriteTokens))
		m.Stats.InputToken = usage.PromptTokens
		m.Stats.OutputToken = usage.CompletionTokens
		inputReported = usage.EffectiveInputTokens() > 0
		m.TokenSource = model.UsageValueSourceReported
	}

	// 降级：上游未上报 input（usage 缺失，或 usage 中输入侧全为 0）时，用请求侧
	// 估算的 TransportInputTokens 兜底，使 input token/费用不为 0；output 无法从
	// 请求侧估算，保持 0。tiktoken 统一用 o200k_base，对 Claude/Gemini 为近似值。
	if !inputReported && m.TransportInputTokens != nil && *m.TransportInputTokens > 0 {
		estimated := int64(*m.TransportInputTokens)
		m.Stats.InputToken = estimated
		m.BillInputTokens = intPtr(int(estimated))
		m.TokenSource = model.UsageValueSourceEstimated
	}
	m.calculateCosts(actualModel)
}

func (m *RelayMetrics) calculateCosts(actualModel string) {
	effective := m.effectivePriceForCost(actualModel)
	if effective == nil || effective.Source == model.PriceQuoteSourceUnknown {
		return
	}
	nonCachedInput := intPointerValue(m.BillInputTokens)
	cacheRead := intPointerValue(m.CacheReadTokens)
	cacheWrite := intPointerValue(m.CacheWriteTokens)
	inputRaw, outputRaw, inputUSD, outputUSD := effectivePriceCosts(
		effective,
		nonCachedInput,
		cacheRead,
		cacheWrite,
		m.Stats.OutputToken,
	)
	m.PriceOriginalCost = inputRaw + outputRaw
	m.Stats.InputCost = inputUSD
	m.Stats.OutputCost = outputUSD
}

func (m *RelayMetrics) effectivePriceForCost(actualModel string) *model.EffectivePrice {
	if m.EffectivePrice != nil && m.EffectivePrice.Source != model.PriceQuoteSourceUnknown {
		return m.EffectivePrice
	}
	modelPrice := resolveModelPrice(actualModel)
	if modelPrice == nil {
		return m.EffectivePrice
	}
	candidateID := m.RouteCandidateID
	m.EffectivePrice = &model.EffectivePrice{
		RouteCandidateID:  candidateID,
		Source:            model.PriceQuoteSourceGlobal,
		Unit:              model.PriceUnitPerMillionTokens,
		Currency:          "USD",
		Input:             modelPrice.Input,
		Output:            modelPrice.Output,
		CacheRead:         modelPrice.CacheRead,
		CacheWrite:        modelPrice.CacheWrite,
		GroupMultiplier:   1,
		ExchangeRateToUSD: 1,
		Convertible:       true,
		MatchReason:       "global model fallback",
	}
	return m.EffectivePrice
}

func effectivePriceCosts(
	effective *model.EffectivePrice,
	nonCachedInput int64,
	cacheRead int64,
	cacheWrite int64,
	outputTokens int64,
) (inputRaw, outputRaw, inputUSD, outputUSD float64) {
	if effective == nil || effective.Source == model.PriceQuoteSourceUnknown {
		return 0, 0, 0, 0
	}
	if effective.Unit == model.PriceUnitPerRequest {
		inputRaw = effective.PerRequest
	} else {
		inputRaw = (float64(cacheRead)*effective.CacheRead +
			float64(cacheWrite)*effective.CacheWrite +
			float64(nonCachedInput)*effective.Input) * 1e-6
		outputRaw = float64(outputTokens) * effective.Output * 1e-6
	}
	if effective.Convertible && effective.ExchangeRateToUSD > 0 {
		inputUSD = inputRaw * effective.ExchangeRateToUSD
		outputUSD = outputRaw * effective.ExchangeRateToUSD
	}
	return inputRaw, outputRaw, inputUSD, outputUSD
}

func (m *RelayMetrics) Save(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt) {
	m.SaveWithChannelStats(ctx, success, err, attempts, true)
}

func (m *RelayMetrics) SaveWithChannelStats(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt, updateChannelStats bool) {
	outcome := model.RequestOutcomeFailed
	if success {
		outcome = model.RequestOutcomeSuccess
	} else if isClientCancellation(ctx, err) {
		outcome = model.RequestOutcomeClientCanceled
	}
	m.SaveOutcomeWithChannelStats(ctx, outcome, err, attempts, updateChannelStats)
}

func (m *RelayMetrics) SaveOutcomeWithChannelStats(
	ctx context.Context,
	outcome model.RequestOutcome,
	err error,
	attempts []model.ChannelAttempt,
	updateChannelStats bool,
) {
	if outcome == "" {
		outcome = model.RequestOutcomeIndeterminate
	}
	m.Outcome = outcome
	duration := time.Since(m.StartTime)

	globalStats := model.StatsMetrics{
		WaitTime:    duration.Milliseconds(),
		InputToken:  m.Stats.InputToken,
		OutputToken: m.Stats.OutputToken,
		InputCost:   m.Stats.InputCost,
		OutputCost:  m.Stats.OutputCost,
	}
	switch outcome {
	case model.RequestOutcomeSuccess:
		globalStats.RequestSuccess = 1
	case model.RequestOutcomeFailed:
		globalStats.RequestFailed = 1
	case model.RequestOutcomeClientCanceled:
		globalStats.RequestCanceled = 1
	default:
		globalStats.RequestIndeterminate = 1
	}

	channelID, channelName := finalChannel(attempts)
	op.StatsTotalUpdate(globalStats)
	op.StatsHourlyUpdate(globalStats)
	op.StatsDailyUpdate(context.Background(), globalStats)
	op.StatsAPIKeyUpdate(m.APIKeyID, globalStats)
	channelStats := globalStats
	channelStats.RequestCanceled = 0
	channelStats.RequestIndeterminate = 0
	if updateChannelStats {
		op.StatsChannelUpdate(channelID, channelStats)
	} else {
		updateFinalChannelUsageStats(channelID, channelStats)
	}
	op.StatsSiteModelHourlyRecordAttempts(attempts, m.ActualModel)

	// 上游未上报 usage（或输入侧全为 0）时打告警，便于定位是哪个通道缺失 usage。
	if outcome == model.RequestOutcomeSuccess && (m.InternalResponse == nil || m.InternalResponse.Usage == nil ||
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

	if conf.AppConfig.Log.Relay.Summary || outcome != model.RequestOutcomeSuccess {
		fields := []interface{}{
			"model", m.RequestModel,
			"actual_model", m.ActualModel,
			"channel_id", channelID,
			"channel", channelName,
			"outcome", outcome,
			"protocol_mode", m.ProtocolMode,
			"protocol_failure_stage", m.ProtocolFailureStage,
			"duration_ms", duration.Milliseconds(),
			"input_token", m.Stats.InputToken,
			"output_token", m.Stats.OutputToken,
			"input_cost", m.Stats.InputCost,
			"output_cost", m.Stats.OutputCost,
			"total_cost", m.Stats.InputCost + m.Stats.OutputCost,
			"attempts", len(attempts),
			"ws", m.UsedWS,
		}
		if outcome == model.RequestOutcomeSuccess {
			log.Infow("relay.complete", fields...)
		} else if outcome == model.RequestOutcomeClientCanceled {
			log.Infow("relay.complete", fields...)
		} else {
			log.Warnw("relay.complete", fields...)
		}
	}

	m.saveLog(ctx, outcome, err, duration, attempts, channelID, channelName)
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
		if a.Status == model.AttemptCanceled && lastID == 0 {
			lastID = a.ChannelID
			lastName = a.ChannelName
		}
	}
	return lastID, lastName
}

func (m *RelayMetrics) saveLog(ctx context.Context, outcome model.RequestOutcome, err error, duration time.Duration, attempts []model.ChannelAttempt, channelID int, channelName string) {
	actualModel := m.ActualModel
	if actualModel == "" {
		actualModel = m.RequestModel
	}

	relayLog := model.RelayLog{
		Time:                 m.StartTime.Unix(),
		RequestModelName:     m.RequestModel,
		RequestAPIKeyID:      m.APIKeyID,
		ChannelName:          channelName,
		ChannelId:            channelID,
		ActualModelName:      actualModel,
		CanonicalModelName:   m.CanonicalModelName,
		RouteCandidateID:     m.RouteCandidateID,
		InboundProtocol:      m.InboundProtocol,
		OutboundProtocol:     m.OutboundProtocol,
		ProtocolMode:         m.ProtocolMode,
		ProtocolPolicy:       m.ProtocolPolicy,
		ProtocolAllowLossy:   m.ProtocolAllowLossy,
		ProtocolWarnings:     append([]string(nil), m.ProtocolWarnings...),
		ProtocolFailureStage: m.ProtocolFailureStage,
		UseTime:              int(duration.Milliseconds()),
		Attempts:             attempts,
		TotalAttempts:        len(attempts),
		UsedWS:               m.UsedWS,
		Outcome:              outcome,
		TransportTermination: m.TransportTermination,
		CompletionEvidence:   m.CompletionEvidence,
		TerminalEvent:        m.TerminalEvent,
		HeaderPolicyTrace:    m.HeaderPolicyTrace,
		TokenSource:          m.TokenSource,
	}
	if m.EffectivePrice != nil {
		relayLog.PriceQuoteID = m.EffectivePrice.QuoteID
		relayLog.PriceSource = m.EffectivePrice.Source
		relayLog.PriceUnit = m.EffectivePrice.Unit
		relayLog.PriceCurrency = m.EffectivePrice.Currency
		relayLog.PriceInput = m.EffectivePrice.Input
		relayLog.PriceOutput = m.EffectivePrice.Output
		relayLog.PriceCacheRead = m.EffectivePrice.CacheRead
		relayLog.PriceCacheWrite = m.EffectivePrice.CacheWrite
		relayLog.PricePerRequest = m.EffectivePrice.PerRequest
		relayLog.PriceGroupMultiplier = m.EffectivePrice.GroupMultiplier
		relayLog.PriceExchangeRateUSD = m.EffectivePrice.ExchangeRateToUSD
		relayLog.PriceObservedAt = m.EffectivePrice.ObservedAt
		relayLog.PriceStale = m.EffectivePrice.Stale
		relayLog.PriceConvertible = m.EffectivePrice.Convertible
		relayLog.PriceOriginalCost = m.PriceOriginalCost
		relayLog.PriceMatchReason = m.EffectivePrice.MatchReason
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
	relayLog.BillInputTokens = m.BillInputTokens
	relayLog.CacheReadTokens = m.CacheReadTokens
	relayLog.CacheWriteTokens = m.CacheWriteTokens
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
	relayLog.Success = outcome == model.RequestOutcomeSuccess

	if logErr := op.RelayLogAdd(ctx, relayLog); logErr != nil {
		log.Warnf("failed to save relay log: %v", logErr)
	}
}

func updateFinalChannelUsageStats(channelID int, metrics model.StatsMetrics) {
	if channelID == 0 {
		return
	}
	usageStats := model.StatsMetrics{
		InputToken:  metrics.InputToken,
		OutputToken: metrics.OutputToken,
		InputCost:   metrics.InputCost,
		OutputCost:  metrics.OutputCost,
	}
	if usageStats.InputToken == 0 && usageStats.OutputToken == 0 && usageStats.InputCost == 0 && usageStats.OutputCost == 0 {
		return
	}
	op.StatsChannelUpdate(channelID, usageStats)
}

func intPtr(value int) *int {
	return &value
}

func intPointerValue(value *int) int64 {
	if value == nil {
		return 0
	}
	return int64(*value)
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
