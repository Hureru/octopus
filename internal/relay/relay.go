package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tmaxmax/go-sse"
)

// Handler 处理入站请求并转发到上游服务
func Handler(inboundType inbound.InboundType, c *gin.Context) {
	// 解析请求
	internalRequest, inAdapter, err := parseRequest(inboundType, c)
	if err != nil {
		return
	}
	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, internalRequest.Model) {
			resp.Error(c, http.StatusBadRequest, "model not supported")
			return
		}
	}

	requestModel := internalRequest.Model
	apiKeyID := c.GetInt("api_key_id")

	// 获取通道分组
	group, err := op.GroupGetEnabledMap(requestModel, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "model not found")
		return
	}

	// 创建迭代器（策略排序 + 粘性优先）
	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.Error(c, http.StatusServiceUnavailable, "no available channel")
		return
	}

	// 初始化 Metrics
	metrics := NewRelayMetrics(apiKeyID, requestModel, internalRequest)

	// 请求级上下文
	req := &relayRequest{
		c:               c,
		requestHeaders:  c.Request.Header.Clone(),
		inAdapter:       inAdapter,
		internalRequest: internalRequest,
		metrics:         metrics,
		apiKeyID:        apiKeyID,
		requestModel:    requestModel,
		iter:            iter,
	}

	var lastErr error
	var lastResult attemptResult

	// 同通道重试次数：启用时使用配置值，否则 1 次（不重试）
	maxSameChannelRetries := 1
	if group.RetryEnabled {
		maxSameChannelRetries = group.MaxRetries
		if maxSameChannelRetries <= 0 {
			maxSameChannelRetries = 3
		}
	}

	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Infof("request context canceled, stopping retry")
			metrics.Save(c.Request.Context(), false, context.Canceled, iter.Attempts())
			return
		default:
		}

		item := iter.Item()

		// 获取通道
		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}

		// 出站适配器
		outAdapter := outbound.Get(channel.Type)
		if outAdapter == nil {
			iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}

		// 类型兼容性检查
		if internalRequest.IsEmbeddingRequest() && !outbound.IsEmbeddingChannelType(channel.Type) {
			iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with embedding request")
			continue
		}
		if internalRequest.IsChatRequest() && !outbound.IsChatChannelType(channel.Type) {
			iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with chat request")
			continue
		}

		// 设置实际模型
		internalRequest.Model = item.ModelName

		log.Infof("request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t)",
			requestModel, group.Mode, channel.Name, item.ModelName,
			iter.Index()+1, iter.Len(), iter.IsSticky())

		selectOpts := dbmodel.ChannelKeySelectOptions{
			IgnoreRecent429Cooldown: group.RetryEnabled,
			ExcludeKeyIDs:           make(map[int]struct{}),
			PreferredKeyID:          iter.StickyKeyID(),
		}
		var usedKey dbmodel.ChannelKey
		for {
			usedKey = channel.GetChannelKey(selectOpts)
			if usedKey.ChannelKey == "" {
				break
			}
			if !iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				break
			}
			selectOpts.ExcludeKeyIDs[usedKey.ID] = struct{}{}
			usedKey = dbmodel.ChannelKey{}
		}
		if usedKey.ChannelKey == "" {
			if len(selectOpts.ExcludeKeyIDs) == 0 {
				iter.Skip(channel.ID, 0, channel.Name, "no available key")
			}
			continue
		}

		// 同通道重试循环
		var result attemptResult
		for retryNum := 0; retryNum < maxSameChannelRetries; retryNum++ {
			// 重试前等待退避
			if retryNum > 0 {
				delay := computeBackoff(retryNum, result.RetryAfter)
				log.Infof("same-channel retry %d/%d for %s, waiting %v",
					retryNum, maxSameChannelRetries, channel.Name, delay)
				select {
				case <-c.Request.Context().Done():
					log.Infof("request context canceled during retry backoff")
					balancer.AbortHalfOpen(channel.ID, usedKey.ID, internalRequest.Model)
					metrics.Save(c.Request.Context(), false, context.Canceled, iter.Attempts())
					return
				case <-time.After(delay):
				}
			}

			// 构造尝试级上下文
			ra := &relayAttempt{
				relayRequest:         req,
				outAdapter:           outAdapter,
				channel:              channel,
				usedKey:              usedKey,
				firstTokenTimeOutSec: group.FirstTokenTimeOut,
			}

			result = ra.attempt()
			if result.Success || result.Written || result.Canceled || result.ResetConversation || !isRetryableStatus(result.StatusCode) {
				break
			}
		}

		// 同通道重试耗尽后记录熔断器失败
		if !result.Success && !result.Written && !result.Canceled && !result.ResetConversation {
			failureKind := circuitFailureKind(group.RetryEnabled, result.StatusCode)
			balancer.RecordFailure(channel.ID, usedKey.ID, internalRequest.Model, failureKind)
			if failureKind == balancer.FailureHard {
				maybeLearnManagedRoute(c.Request.Context(), channel.ID, internalRequest.Model, inboundType, result.Err)
			}
		}

		if result.Success {
			metrics.Save(c.Request.Context(), true, nil, iter.Attempts())
			return
		}
		if result.Canceled {
			balancer.AbortHalfOpen(channel.ID, usedKey.ID, internalRequest.Model)
			metrics.Save(c.Request.Context(), false, result.Err, iter.Attempts())
			return
		}
		if result.ResetConversation {
			balancer.AbortHalfOpen(channel.ID, usedKey.ID, internalRequest.Model)
			metrics.Save(c.Request.Context(), false, result.Err, iter.Attempts())
			if publicErr, ok := classifyWSPublicError(result.Err, result.StatusCode); ok {
				resp.Error(c, publicErr.Status, publicErr.Message)
			} else {
				resp.Error(c, result.StatusCode, result.Err.Error())
			}
			return
		}
		if result.Written {
			balancer.AbortHalfOpen(channel.ID, usedKey.ID, internalRequest.Model)
			metrics.Save(c.Request.Context(), false, result.Err, iter.Attempts())
			return
		}
		lastErr = result.Err
		lastResult = result
	}

	// 所有候选通道均失败
	metrics.Save(c.Request.Context(), false, lastErr, iter.Attempts())

	// 透传 429/503 状态码和 Retry-After 头，让客户端 SDK 的重试机制接管
	if isPassthroughStatus(lastResult.StatusCode) {
		if lastResult.RetryAfter > 0 {
			c.Header("Retry-After", fmt.Sprintf("%d", int(lastResult.RetryAfter.Seconds())))
		}
		resp.Error(c, lastResult.StatusCode, "channel failed")
		return
	}
	if lastResult.StatusCode > 0 {
		resp.Error(c, lastResult.StatusCode, "channel failed")
		return
	}
	resp.Error(c, http.StatusBadGateway, "channel failed")
}

func circuitFailureKind(retryEnabled bool, statusCode int) balancer.FailureKind {
	if retryEnabled && isPassthroughStatus(statusCode) {
		return balancer.FailureSoftRateLimit
	}
	return balancer.FailureHard
}

// attempt 统一管理一次通道尝试的完整生命周期
func (ra *relayAttempt) attempt() attemptResult {
	span := ra.iter.StartAttempt(ra.channel.ID, ra.usedKey.ID, ra.channel.Name)

	// 转发请求
	statusCode, fwdErr := ra.forward()

	// 更新 channel key 状态
	ra.usedKey.StatusCode = statusCode
	ra.usedKey.LastUseTimeStamp = time.Now().Unix()

	if fwdErr == nil {
		// ====== 成功 ======
		ra.collectResponse()
		ra.usedKey.TotalCost += ra.metrics.Stats.InputCost + ra.metrics.Stats.OutputCost
		op.ChannelKeyUpdate(ra.usedKey)

		span.End(dbmodel.AttemptSuccess, statusCode, "")

		// Channel 维度统计
		op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
			WaitTime:       span.Duration().Milliseconds(),
			RequestSuccess: 1,
		})

		// 熔断器：记录成功
		balancer.RecordSuccess(ra.channel.ID, ra.usedKey.ID, ra.internalRequest.Model)
		// 会话保持：更新粘性记录
		balancer.SetSticky(ra.apiKeyID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)

		return attemptResult{Success: true}
	}

	// ====== 失败 ======
	if isClientCancellation(ra.requestContext(), fwdErr) {
		return attemptResult{
			Success:  false,
			Written:  false,
			Canceled: true,
			Err:      fwdErr,
		}
	}

	op.ChannelKeyUpdate(ra.usedKey)
	span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())

	// Channel 维度统计
	op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
		WaitTime:      span.Duration().Milliseconds(),
		RequestFailed: 1,
	})

	// 注意：熔断器记录已移至 Handler() 的同通道重试循环外，
	// 避免重试期间过早触发熔断

	written := ra.getStreamWriter().Written()
	if written {
		ra.collectResponse()
	}
	return attemptResult{
		Success:           false,
		Written:           written,
		ResetConversation: statusCode == http.StatusConflict && needsConversationRestart(relayErrorMessage(fwdErr)),
		Err:               fmt.Errorf("channel %s failed: %v", ra.channel.Name, fwdErr),
		StatusCode:        statusCode,
		RetryAfter:        ra.retryAfter,
	}
}

// parseRequest 解析并验证入站请求
func parseRequest(inboundType inbound.InboundType, c *gin.Context) (*model.InternalLLMRequest, model.Inbound, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	inAdapter := inbound.Get(inboundType)
	internalRequest, err := inAdapter.TransformRequest(c.Request.Context(), body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	// Pass through the original query parameters
	internalRequest.Query = c.Request.URL.Query()

	if err := internalRequest.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return nil, nil, err
	}

	return internalRequest, inAdapter, nil
}

// forward 转发请求到上游服务
func (ra *relayAttempt) forward() (int, error) {
	ctx := ra.requestContext()

	// 尝试上游 WebSocket（仅 OpenAI Response outbound 类型）
	if ra.channel.Type == outbound.OutboundTypeOpenAIResponse &&
		ra.internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {

		shouldTryWS := false
		wsUpgradeEnabled, _ := op.SettingGetBool(dbmodel.SettingKeyRelayWSUpgradeEnabled)
		if wsUpgradeEnabled {
			// 设置启用：无论客户端协议都主动尝试 WS 上游
			shouldTryWS = true
		} else {
			// 设置禁用：仅当客户端也是 WS 时才尝试 WS 上游
			shouldTryWS = (ra.c == nil)
		}

		if shouldTryWS {
			statusCode, err := ra.forwardViaWS(ctx)
			if statusCode != -1 {
				return statusCode, err
			}
			if requiresUpstreamWSContinuation(ra.internalRequest) {
				balancer.DeleteSticky(ra.apiKeyID, ra.requestModel)
				return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
			}
			// statusCode == -1 means WS not available, fall through to HTTP
		}
	}

	return ra.forwardViaHTTP(ctx)
}

// forwardViaWS attempts to forward via upstream WebSocket.
// Returns statusCode=-1 if WS is not available (caller should fall through to HTTP).
func (ra *relayAttempt) forwardViaWS(ctx context.Context) (int, error) {
	headers := buildUpstreamHeaders(ra.sourceRequestHeaders(), ra.channel, "Bearer "+ra.usedKey.ChannelKey, true)
	pc := TryUpstreamWS(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ID, headers)
	if pc == nil {
		return -1, nil // WS not available
	}

	log.Infof("using upstream WebSocket for channel %s (key=%d)", ra.channel.Name, ra.usedKey.ID)

	// Build the Responses API request body
	responsesReq := openaiOutbound.ConvertToResponsesRequest(ra.internalRequest)
	reqBody, err := json.Marshal(responsesReq)
	if err != nil {
		wsUpstreamPool.Put(ra.channel.ID, ra.usedKey.ID, pc)
		return -1, nil // fall through to HTTP
	}
	ra.metrics.SetTransportRequestPayload(reqBody, ra.internalRequest.Model)

	// Send response.create message
	if err := wsUpstreamPool.SendResponseCreate(ctx, pc, reqBody); err != nil {
		log.Warnf("upstream WS send failed for channel %s: %v", ra.channel.Name, err)
		pc.conn.Close(websocket.StatusGoingAway, "send failed")
		wsUpstreamPool.Remove(ra.channel.ID, ra.usedKey.ID, pc.headerSig)
		if requiresUpstreamWSContinuation(ra.internalRequest) && isUpstreamWSConnectionBroken(err) {
			balancer.DeleteSticky(ra.apiKeyID, ra.requestModel)
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
		}
		if !requiresUpstreamWSContinuation(ra.internalRequest) && isUpstreamWSConnectionBroken(err) {
			redialed := TryUpstreamWS(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ID, headers, true)
			if redialed != nil {
				retryErr := wsUpstreamPool.SendResponseCreate(ctx, redialed, reqBody)
				if retryErr == nil {
					ra.metrics.UsedWS = true
					if ra.metrics.WSMode == nil {
						ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
					}
					reader := newWSUpstreamReader(redialed, ra.channel.ID, ra.usedKey.ID)
					err = ra.handleWSStreamResponse(ctx, reader)
					if err != nil {
						reader.CloseWithError()
						return reader.StatusCode(), err
					}
					reader.Close()
					return 200, nil
				}
				log.Warnf("upstream WS redial send failed for channel %s: %v", ra.channel.Name, retryErr)
				redialed.conn.Close(websocket.StatusGoingAway, "send failed after redial")
				wsUpstreamPool.Remove(ra.channel.ID, ra.usedKey.ID, redialed.headerSig)
			}
		}
		return -1, nil // fall through to HTTP
	}

	// Read events from WS and process through the transform pipeline
	ra.metrics.UsedWS = true
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	reader := newWSUpstreamReader(pc, ra.channel.ID, ra.usedKey.ID)
	err = ra.handleWSStreamResponse(ctx, reader)
	if err != nil {
		reader.CloseWithError()
		return reader.StatusCode(), err
	}

	reader.Close()
	return 200, nil
}

// handleWSStreamResponse processes events from an upstream WebSocket reader.
func (ra *relayAttempt) handleWSStreamResponse(ctx context.Context, reader *wsUpstreamReader) error {
	// Determine client writer
	writer := ra.getStreamWriter()

	// Set SSE response headers (for HTTP clients; WS clients handle this differently)
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")

	firstToken := true
	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if ra.firstTokenTimeOutSec > 0 {
		firstTokenTimer = time.NewTimer(time.Duration(ra.firstTokenTimeOutSec) * time.Second)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			log.Infof("client disconnected during ws stream")
			return nil
		case <-firstTokenC:
			log.Warnf("first token timeout (%ds) on ws stream, switching channel", ra.firstTokenTimeOutSec)
			return fmt.Errorf("first token timeout (%ds)", ra.firstTokenTimeOutSec)
		default:
		}

		eventData, err := reader.ReadEvent(ctx)
		if err != nil {
			if err == io.EOF {
				log.Infof("ws stream end")
				return nil
			}
			return fmt.Errorf("ws stream read error: %w", err)
		}

		// Transform through outbound → internal → inbound pipeline
		data, err := ra.transformStreamData(ctx, string(eventData))
		if err != nil || len(data) == 0 {
			continue
		}

		if firstToken {
			ra.metrics.SetFirstTokenTime(time.Now())
			firstToken = false
			if firstTokenTimer != nil {
				if !firstTokenTimer.Stop() {
					select {
					case <-firstTokenTimer.C:
					default:
					}
				}
				firstTokenTimer = nil
				firstTokenC = nil
			}
		}

		writer.Write(data)
		writer.Flush()
	}
}

// forwardViaHTTP forwards the request using traditional HTTP.
func (ra *relayAttempt) forwardViaHTTP(ctx context.Context) (int, error) {
	// 构建出站请求
	outboundRequest, err := ra.outAdapter.TransformRequest(
		ctx,
		ra.internalRequest,
		ra.channel.GetBaseUrl(),
		ra.usedKey.ChannelKey,
	)
	if err != nil {
		log.Warnf("failed to create request: %v", err)
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	if requestBody, readErr := readOutboundRequestBody(outboundRequest); readErr == nil {
		ra.metrics.SetTransportRequestPayload(requestBody, ra.internalRequest.Model)
	}

	// 复制请求头
	ra.copyHeaders(outboundRequest)

	// 发送请求
	response, err := ra.sendRequest(outboundRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	// 检查响应状态
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		ra.retryAfter = parseRetryAfter(response.Header.Get("Retry-After"))
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return response.StatusCode, fmt.Errorf("failed to read response body: %w", err)
		}
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		return statusCode, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	// 处理响应
	if ra.internalRequest.Stream != nil && *ra.internalRequest.Stream {
		if err := ra.handleStreamResponse(ctx, response); err != nil {
			return 0, err
		}
		return response.StatusCode, nil
	}
	if err := ra.handleResponse(ctx, response); err != nil {
		return 0, err
	}
	return response.StatusCode, nil
}

func defaultWSModeForRequest(req *model.InternalLLMRequest) dbmodel.RelayLogWSMode {
	if requiresUpstreamWSContinuation(req) {
		return dbmodel.RelayLogWSModeContinuation
	}
	return dbmodel.RelayLogWSModeFresh
}

func readOutboundRequestBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	if req.GetBody != nil {
		bodyReader, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer bodyReader.Close()
		return io.ReadAll(bodyReader)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return body, nil
}

// getStreamWriter returns the appropriate stream writer for the current request.
func (ra *relayAttempt) getStreamWriter() StreamWriter {
	if ra.streamWriter != nil {
		return ra.streamWriter
	}
	return ra.c.Writer
}

// copyHeaders 复制请求头，过滤 hop-by-hop 头
func (ra *relayAttempt) copyHeaders(outboundRequest *http.Request) {
	copyHeaderMap(outboundRequest.Header, buildUpstreamHeaders(ra.sourceRequestHeaders(), ra.channel, outboundRequest.Header.Get("Authorization"), false))
}

// sendRequest 发送 HTTP 请求
func (ra *relayAttempt) sendRequest(req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHttpClient(ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, err
	}

	response, err := httpClient.Do(req)
	if err != nil {
		if isClientCancellation(req.Context(), err) {
			log.Infof("request canceled before upstream response: %v", err)
		} else {
			log.Warnf("failed to send request: %v", err)
		}
		return nil, err
	}

	return response, nil
}

// handleStreamResponse 处理流式响应
func (ra *relayAttempt) handleStreamResponse(ctx context.Context, response *http.Response) error {
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	writer := ra.getStreamWriter()

	// 设置 SSE 响应头
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")

	firstToken := true

	type sseReadResult struct {
		data string
		err  error
	}
	results := make(chan sseReadResult, 1)
	safe.Go("relay-stream-read", func() {
		defer close(results)
		readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
		for ev, err := range sse.Read(response.Body, readCfg) {
			if err != nil {
				results <- sseReadResult{err: err}
				return
			}
			results <- sseReadResult{data: ev.Data}
		}
	})

	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if firstToken && ra.firstTokenTimeOutSec > 0 {
		firstTokenTimer = time.NewTimer(time.Duration(ra.firstTokenTimeOutSec) * time.Second)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			log.Infof("client disconnected, stopping stream")
			return nil
		case <-firstTokenC:
			log.Warnf("first token timeout (%ds), switching channel", ra.firstTokenTimeOutSec)
			_ = response.Body.Close()
			return fmt.Errorf("first token timeout (%ds)", ra.firstTokenTimeOutSec)
		case r, ok := <-results:
			if !ok {
				log.Infof("stream end")
				return nil
			}
			if r.err != nil {
				log.Warnf("failed to read event: %v", r.err)
				return fmt.Errorf("failed to read stream event: %w", r.err)
			}

			data, err := ra.transformStreamData(ctx, r.data)
			if err != nil || len(data) == 0 {
				continue
			}
			if firstToken {
				ra.metrics.SetFirstTokenTime(time.Now())
				firstToken = false
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}

			ra.getStreamWriter().Write(data)
			ra.getStreamWriter().Flush()
		}
	}
}

// transformStreamData 转换流式数据
func (ra *relayAttempt) transformStreamData(ctx context.Context, data string) ([]byte, error) {
	internalStream, err := ra.outAdapter.TransformStream(ctx, []byte(data))
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}
	if internalStream == nil {
		return nil, nil
	}

	inStream, err := ra.inAdapter.TransformStream(ctx, internalStream)
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}

	return inStream, nil
}

// handleResponse 处理非流式响应
func (ra *relayAttempt) handleResponse(ctx context.Context, response *http.Response) error {
	internalResponse, err := ra.outAdapter.TransformResponse(ctx, response)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform outbound response: %w", err)
	}

	inResponse, err := ra.inAdapter.TransformResponse(ctx, internalResponse)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform inbound response: %w", err)
	}

	ra.c.Data(http.StatusOK, "application/json", inResponse)
	return nil
}

// collectResponse 收集响应信息
func (ra *relayAttempt) collectResponse() {
	internalResponse, err := ra.inAdapter.GetInternalResponse(ra.requestContext())
	if err != nil || internalResponse == nil {
		return
	}

	actualModel := strings.TrimSpace(internalResponse.Model)
	if actualModel == "" {
		actualModel = strings.TrimSpace(ra.internalRequest.Model)
	}
	ra.metrics.SetInternalResponse(internalResponse, actualModel)
}
