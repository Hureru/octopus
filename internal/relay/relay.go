package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/relay/stream"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
	"github.com/tmaxmax/go-sse"
)

type streamHeartbeatWriter interface {
	Write([]byte) (int, error)
	Flush()
}

func streamHeartbeatInterval() time.Duration {
	interval, err := op.SettingGetInt(dbmodel.SettingKeySSEHeartbeatInterval)
	if err != nil || interval <= 0 {
		return 0
	}
	return time.Duration(interval) * time.Second
}

func newStreamHeartbeatTicker() (*time.Ticker, <-chan time.Time) {
	interval := streamHeartbeatInterval()
	if interval <= 0 {
		return nil, nil
	}
	ticker := time.NewTicker(interval)
	return ticker, ticker.C
}

func writeSSEHeartbeat(writer streamHeartbeatWriter) error {
	if _, err := writer.Write([]byte(":\n\n")); err != nil {
		return err
	}
	writer.Flush()
	return nil
}

func Handler(inboundType inbound.InboundType, c *gin.Context) {
	// 解析请求
	rawBody, internalRequest, inAdapter, err := parseRequest(inboundType, c)
	if err != nil {
		return
	}
	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, internalRequest.Model) {
			resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
			return
		}
	}

	requestModel := internalRequest.Model
	apiKeyID := c.GetInt("api_key_id")

	// 获取通道分组
	group, err := op.GroupGetEnabledMap(requestModel, c.Request.Context())
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return
	}

	// === HTTP Replay 机制 ===
	// 当 HTTP 请求携带 previous_response_id 时，尝试从本地加载上一次成功的 replay 状态，
	// 优先路由到同一渠道/key，并将请求转为自包含形式（合并历史，移除 previous_response_id）。
	var responsesReplayState *wsConversationState
	if inboundType == inbound.InboundTypeOpenAIResponse && internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {
		if prevID := internalRequest.OpenAIPreviousResponseID(); prevID != "" {
			responsesReplayState = resolveResponsesReplayState(apiKeyID, group.ID, requestModel, internalRequest)
			if responsesReplayState != nil {
				log.Debugf("loaded HTTP replay state (apikey=%d, group=%d, model=%s, previous_response_id=%s, channel=%d, key=%d)",
					apiKeyID, group.ID, requestModel, prevID, responsesReplayState.ChannelID, responsesReplayState.ChannelKeyID)
				// 转换请求为自包含形式（移除 previous_response_id，合并历史）
				// BuildReplayRequest 返回 nil 表示合并失败，应保留原始请求
				if replayed := responsesReplayState.BuildReplayRequest(internalRequest); replayed != nil {
					internalRequest = replayed
					log.Debugf("HTTP replay request transformed (apikey=%d, removed previous_response_id, merged history)", apiKeyID)
				} else {
					log.Warnf("HTTP replay history merge failed (apikey=%d, group=%d, model=%s, previous_response_id=%s), keeping original request",
						apiKeyID, group.ID, requestModel, prevID)
					responsesReplayState = nil // 放弃 replay，使用原始请求
				}
			} else {
				log.Debugf("no HTTP replay state found (apikey=%d, group=%d, model=%s, previous_response_id=%s)",
					apiKeyID, group.ID, requestModel, prevID)
			}
		}
	}

	// 创建迭代器（策略排序 + 粘性优先）
	// 如果有 replay state，注入为 sticky 偏好
	var preferredSticky *balancer.SessionEntry
	if responsesReplayState != nil {
		preferredSticky = responsesReplayStateToSticky(responsesReplayState)
		if preferredSticky != nil {
			log.Debugf("HTTP replay sticky routing preference (channel=%d, key=%d)", preferredSticky.ChannelID, preferredSticky.ChannelKeyID)
		}
	}
	iter := balancer.NewIteratorWithPreference(group, apiKeyID, requestModel, preferredSticky)
	if iter.Len() == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}

	// === 早期心跳 ===
	// 在所有 forward / 重试 / 退避之前启动早期心跳协程，覆盖前置阶段（连接慢、failover、退避叠加）
	// 期间向客户端发 SSE 注释字节，避免被 Cloudflare 在 120s 零字节阈值上判 524。
	// 仅对流式请求生效；非流式无法发送 SSE 注释（破坏 application/json 协议），
	// 不施加任何本地超时——上游慢响应应让其自然完成或由上游/CF 自身处理。
	isStream := internalRequest.Stream != nil && *internalRequest.Stream
	hb := startEarlyHeartbeat(c, isStream)
	defer hb.Stop()

	// 初始化 Metrics
	metrics := NewRelayMetrics(apiKeyID, requestModel, rawBody, internalRequest)
	// 如果触发了 HTTP replay，记录 ws_mode=replay 和 ws_recovery=replay
	if responsesReplayState != nil {
		metrics.SetWSMode(dbmodel.RelayLogWSModeReplay)
		metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReplay)
	}
	responsesPassthroughRequired := internalRequest.HasOpenAIResponsesPassthrough()
	responsesPassthroughCapableFound := false

	// 请求级上下文
	req := &relayRequest{
		c:               c,
		inAdapter:       inAdapter,
		internalRequest: internalRequest,
		metrics:         metrics,
		apiKeyID:        apiKeyID,
		requestModel:    requestModel,
		groupID:         group.ID,
		groupSessionTTL: group.SessionKeepTime,
		iter:            iter,
		rawBody:         rawBody,
		heartbeat:       hb,
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
			log.Debugf("request context canceled, stopping retry")
			metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
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
		if responsesPassthroughRequired {
			if channel.Type == outbound.OutboundTypeOpenAIResponse {
				responsesPassthroughCapableFound = true
			} else {
				iter.Skip(channel.ID, 0, channel.Name, "openai responses passthrough required")
				continue
			}
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

		log.Debugf("request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t)",
			requestModel, group.Mode, channel.Name, item.ModelName,
			iter.Index()+1, iter.Len(), iter.IsSticky())

		selectOpts := dbmodel.ChannelKeySelectOptions{
			ExcludeKeyIDs:  make(map[int]struct{}),
			PreferredKeyID: iter.StickyKeyID(),
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
					log.Debugf("request context canceled during retry backoff")
					metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
					return
				case <-time.After(delay):
				}

				// 重建 outAdapter 以重置流式状态（toolIndex, toolCalls 等）
				outAdapter = outbound.Get(channel.Type)
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
			if result.Success || result.Written || result.Canceled || result.ResetConversation || result.FirstTokenTimeout || !isRetryableStatus(result.StatusCode) {
				break
			}
		}

		// 同通道重试耗尽后记录熔断器失败
		if !result.Success && !result.Written && !result.Canceled && !result.ResetConversation {
			failureKind := circuitFailureKind(group.RetryEnabled, result.StatusCode)
			balancer.RecordFailure(channel.ID, usedKey.ID, internalRequest.Model, failureKind)
			outlierwindow.Report(channel.ID, false, result.StatusCode, time.Now())
			if failureKind == balancer.FailureHard {
				maybeLearnManagedRoute(c.Request.Context(), channel.ID, internalRequest.Model, inboundType, result.Err)
			}
		}

		if result.Success {
			outlierwindow.Report(channel.ID, true, result.StatusCode, time.Now())

			// === HTTP Replay 状态保存 ===
			// 成功后，如果是 OpenAI Responses HTTP 请求，保存 replay 状态供后续续接
			// 注意：exact replay 请求成功后也需要保存新状态，否则只能续接一轮
			// 优先使用 metrics.InternalResponse（streaming 安全），避免二次 GetInternalResponse 消耗聚合器
			if inboundType == inbound.InboundTypeOpenAIResponse &&
				req.internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {
				internalResponse := metrics.InternalResponse
				if internalResponse == nil {
					var err error
					internalResponse, err = inAdapter.GetInternalResponse(c.Request.Context())
					if err != nil {
						log.Debugf("failed to get internal response for replay state save: %v", err)
					}
				}
				if internalResponse != nil {
					// 如果是 exact replay 请求，基于已有状态继续累积
					var newState *wsConversationState
					if req.internalRequest.IsOpenAIExactReplayRequest() && responsesReplayState != nil {
						newState = cloneWSConversationState(responsesReplayState)
						if newState != nil {
							newState.ChannelID = channel.ID
							newState.ChannelKeyID = usedKey.ID
						}
					}
					if newState == nil {
						newState = &wsConversationState{
							RequestModel: requestModel,
							ChannelID:    channel.ID,
							ChannelKeyID: usedKey.ID,
						}
					}
					newState.ApplySuccessfulTurn(req.internalRequest, internalResponse)
					if newState.LastResponseID != "" {
						ttl := wsConversationStateTTL(group.SessionKeepTime)
						storeResponsesReplayState(apiKeyID, group.ID, requestModel, newState, ttl)
						log.Debugf("saved HTTP replay state (apikey=%d, group=%d, model=%s, response_id=%s, channel=%d, key=%d, ttl=%v, is_replay=%t)",
							apiKeyID, group.ID, requestModel, newState.LastResponseID, channel.ID, usedKey.ID, ttl, req.internalRequest.IsOpenAIExactReplayRequest())
					}
				}
			}

			metrics.SaveWithChannelStats(c.Request.Context(), true, nil, iter.Attempts(), false)
			return
		}
		if result.Canceled {
			metrics.SaveWithChannelStats(c.Request.Context(), false, result.Err, iter.Attempts(), false)
			return
		}
		if result.ResetConversation {
			metrics.SaveWithChannelStats(c.Request.Context(), false, result.Err, iter.Attempts(), false)
			if publicErr, ok := classifyWSPublicError(result.Err, result.StatusCode); ok {
				hb.FlushOrError(c, publicErr.Status, publicErr.Message)
			} else {
				hb.FlushOrError(c, result.StatusCode, result.Err.Error())
			}
			return
		}
		if result.Written {
			metrics.SaveWithChannelStats(c.Request.Context(), false, result.Err, iter.Attempts(), false)
			return
		}
		lastErr = result.Err
		lastResult = result
	}

	// 所有候选通道均失败
	if responsesPassthroughRequired && !responsesPassthroughCapableFound {
		err := fmt.Errorf("openai responses native tools require an openai responses channel")
		metrics.SaveWithChannelStats(c.Request.Context(), false, err, iter.Attempts(), false)
		hb.FlushOrError(c, http.StatusBadRequest, "当前请求包含 OpenAI Responses 原生工具，仅支持 OpenAI Responses 通道直通")
		return
	}
	metrics.SaveWithChannelStats(c.Request.Context(), false, lastErr, iter.Attempts(), false)

	// 透传 429/503 状态码和 Retry-After 头，让客户端 SDK 的重试机制接管
	if isPassthroughStatus(lastResult.StatusCode) {
		if lastResult.RetryAfter > 0 {
			c.Header("Retry-After", fmt.Sprintf("%d", int(lastResult.RetryAfter.Seconds())))
		}
		hb.FlushOrError(c, lastResult.StatusCode, "channel failed")
		return
	}
	if lastResult.StatusCode > 0 {
		hb.FlushOrError(c, lastResult.StatusCode, "channel failed")
		return
	}
	hb.FlushOrError(c, http.StatusBadGateway, "channel failed")
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
		// Passthrough handlers collect response at stream end via PassthroughConfig.CollectMetrics
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
		written := ra.streamPayloadWritten.Load()
		if written {
			ra.collectResponse()
		}
		op.ChannelKeyUpdate(ra.usedKey)
		span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())
		return attemptResult{
			Success:    false,
			Written:    written,
			Canceled:   true,
			Err:        fwdErr,
			StatusCode: statusCode,
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

	written := ra.streamPayloadWritten.Load()
	if written {
		ra.collectResponse()
	}
	firstTokenTimeout := isFirstTokenTimeout(nil, fwdErr)
	return attemptResult{
		Success:           false,
		Written:           written,
		ResetConversation: statusCode == http.StatusConflict && needsConversationRestart(relayErrorMessage(fwdErr)),
		FirstTokenTimeout: firstTokenTimeout,
		Err:               fmt.Errorf("channel %s failed: %v", ra.channel.Name, fwdErr),
		StatusCode:        statusCode,
		RetryAfter:        ra.retryAfter,
	}
}

// parseRequest 解析并验证入站请求
// 返回值中的 rawBody 为客户端原始请求字节，供同格式直通路径重用。
func parseRequest(inboundType inbound.InboundType, c *gin.Context) ([]byte, *model.InternalLLMRequest, model.Inbound, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, nil, err
	}

	inAdapter := inbound.Get(inboundType)
	internalRequest, err := inAdapter.TransformRequest(c.Request.Context(), body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, nil, err
	}

	// Pass through the original query parameters
	internalRequest.Query = c.Request.URL.Query()

	if err := internalRequest.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return nil, nil, nil, err
	}

	return body, internalRequest, inAdapter, nil
}

// forward 转发请求到上游服务
func (ra *relayAttempt) forward() (int, error) {
	ctx := ra.requestContext()

	// 尝试上游 WebSocket（仅 OpenAI Response outbound 类型；必须是客户端 WS 入站且新开关显式启用）
	if ra.channel.Type == outbound.OutboundTypeOpenAIResponse &&
		ra.internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {

		shouldTryWS := false
		// Passthrough is now handled by forwardViaHTTP via PassthroughCapable interface
		if ra.internalRequest.IsOpenAIExactReplayRequest() {
			shouldTryWS = false
		} else if ra.c == nil {
			wsMode := effectiveResponsesWSMode(ra.channel)
			shouldTryWS = shouldEnableResponsesWS(ra.channel) && wsMode != responsesWSModeOff
		} else if requiresUpstreamWSContinuation(ra.internalRequest) {
			// Safety: HTTP ingress must not proactively use upstream WS for fresh requests,
			// but an explicit continuation cannot be safely failovered as ordinary HTTP.
			shouldTryWS = true
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
			ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryDowngrade)
			// statusCode == -1 means WS not available, fall through to HTTP
		}
	}

	return ra.forwardViaHTTP(ctx)
}

// forwardViaWS attempts to forward via upstream WebSocket.
// Returns statusCode=-1 if WS is not available (caller should fall through to HTTP).
func (ra *relayAttempt) forwardViaWS(ctx context.Context) (int, error) {
	if ra.c == nil && effectiveResponsesWSMode(ra.channel) == responsesWSModePassthrough && !ra.internalRequest.IsOpenAIExactReplayRequest() {
		return ra.forwardViaWSPassthrough(ctx)
	}
	continuation := requiresUpstreamWSContinuation(ra.internalRequest)
	preferredConnID := ""
	if continuation {
		preferredConnID, _ = getWSResponseConn(currentPreviousResponseID(ra.internalRequest))
	}
	pc := TryUpstreamWSWithPreference(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey, ra.usedKey.ID, ra.clientRequestHeaders(), preferredConnID)
	if pc == nil {
		log.Debugf("upstream WS unavailable for channel %s (key=%d, continuation=%t)", ra.channel.Name, ra.usedKey.ID, continuation)
		return -1, nil // WS not available
	}

	log.Debugf("using upstream WebSocket for channel %s (key=%d)", ra.channel.Name, ra.usedKey.ID)
	log.Debugf("upstream WS selected (channel=%s, key=%d, continuation=%t, previous_response_id=%s)",
		ra.channel.Name, ra.usedKey.ID, continuation, currentPreviousResponseID(ra.internalRequest))

	// Build the Responses API request body
	responsesReq := openaiOutbound.ConvertToResponsesRequest(ra.internalRequest)
	reqBody, err := json.Marshal(responsesReq)
	if err != nil {
		wsUpstreamPool.Put(pc)
		return -1, nil // fall through to HTTP
	}
	ra.metrics.SetTransportRequestPayload(reqBody, ra.internalRequest.Model)

	// Send response.create message
	if err := wsUpstreamPool.SendResponseCreate(ctx, pc, reqBody); err != nil {
		log.Warnf("upstream WS send failed for channel %s: %v", ra.channel.Name, err)
		log.Debugf("upstream WS send failed before stream start (channel=%s, key=%d, continuation=%t, err=%v)",
			ra.channel.Name, ra.usedKey.ID, continuation, err)
		wsUpstreamPool.RemoveConn(pc)
		if isUpstreamWSConnectionBroken(err) {
			log.Debugf("upstream WS send failure eligible for redial (channel=%s, key=%d, continuation=%t)",
				ra.channel.Name, ra.usedKey.ID, continuation)
			statusCode, redialErr, recovered := ra.retryViaFreshUpstreamWS(ctx, reqBody)
			if recovered || redialErr != nil {
				return statusCode, redialErr
			}
			if requiresUpstreamWSContinuation(ra.internalRequest) {
				balancer.DeleteSticky(ra.apiKeyID, ra.requestModel)
				return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
			}
		}
		wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		return -1, nil // fall through to HTTP
	}

	// Read events from WS and process through the transform pipeline
	ra.metrics.UsedWS = true
	ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModeTransform)
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	reader := newWSUpstreamReader(pc, ra.channel.ID, ra.usedKey.ID)
	err = ra.handleWSStreamResponseV2(ctx, reader)
	if err != nil {
		reader.CloseWithError()
		log.Debugf("upstream WS stream failed (channel=%s, key=%d, continuation=%t, written=%t, status=%d, err=%v)",
			ra.channel.Name, ra.usedKey.ID, continuation, ra.getStreamWriter().Written(), reader.StatusCode(), err)
		if requiresUpstreamWSContinuation(ra.internalRequest) && !ra.streamPayloadWritten.Load() && shouldReconnectUpstreamWSBeforeReplay(err) {
			log.Debugf("upstream WS stream failure eligible for reconnect before replay (channel=%s, key=%d, previous_response_id=%s)",
				ra.channel.Name, ra.usedKey.ID, currentPreviousResponseID(ra.internalRequest))
			statusCode, redialErr, recovered := ra.retryViaFreshUpstreamWS(ctx, reqBody)
			if recovered || redialErr != nil {
				return statusCode, redialErr
			}
		}
		if requiresUpstreamWSContinuation(ra.internalRequest) && isContinuationTransportFailure(err) {
			balancer.DeleteSticky(ra.apiKeyID, ra.requestModel)
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
		}
		if ra.requestContext().Err() == nil {
			wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		}
		return reader.StatusCode(), err
	}

	reader.Close()
	wsUpstreamPool.RecordWSSuccess(ra.channel.ID)
	ra.recordSuccessfulWSAffinity(pc)
	return 200, nil
}

func (ra *relayAttempt) retryViaFreshUpstreamWS(ctx context.Context, reqBody []byte) (int, error, bool) {
	log.Debugf("attempting fresh upstream WS redial (channel=%s, key=%d, previous_response_id=%s)",
		ra.channel.Name, ra.usedKey.ID, currentPreviousResponseID(ra.internalRequest))
	redialed := TryUpstreamWS(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey, ra.usedKey.ID, ra.clientRequestHeaders(), true)
	if redialed == nil {
		log.Debugf("fresh upstream WS redial unavailable (channel=%s, key=%d)", ra.channel.Name, ra.usedKey.ID)
		return 0, nil, false
	}

	retryErr := wsUpstreamPool.SendResponseCreate(ctx, redialed, reqBody)
	if retryErr != nil {
		log.Warnf("upstream WS redial send failed for channel %s: %v", ra.channel.Name, retryErr)
		log.Debugf("fresh upstream WS redial send failed (channel=%s, key=%d, err=%v)", ra.channel.Name, ra.usedKey.ID, retryErr)
		wsUpstreamPool.RemoveConn(redialed)
		wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		if requiresUpstreamWSContinuation(ra.internalRequest) {
			balancer.DeleteSticky(ra.apiKeyID, ra.requestModel)
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation"), true
		}
		return -1, nil, true
	}

	ra.metrics.UsedWS = true
	ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModeTransform)
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReconnect)
	reader := newWSUpstreamReader(redialed, ra.channel.ID, ra.usedKey.ID)
	streamErr := ra.handleWSStreamResponseV2(ctx, reader)
	if streamErr != nil {
		reader.CloseWithError()
		log.Debugf("fresh upstream WS redial stream failed (channel=%s, key=%d, status=%d, err=%v)",
			ra.channel.Name, ra.usedKey.ID, reader.StatusCode(), streamErr)
		if requiresUpstreamWSContinuation(ra.internalRequest) && isContinuationTransportFailure(streamErr) {
			balancer.DeleteSticky(ra.apiKeyID, ra.requestModel)
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation"), true
		}
		if ra.requestContext().Err() == nil {
			wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		}
		return reader.StatusCode(), streamErr, true
	}
	log.Debugf("fresh upstream WS redial succeeded (channel=%s, key=%d, previous_response_id=%s)",
		ra.channel.Name, ra.usedKey.ID, currentPreviousResponseID(ra.internalRequest))
	reader.Close()
	wsUpstreamPool.RecordWSSuccess(ra.channel.ID)
	ra.recordSuccessfulWSAffinity(redialed)
	return http.StatusOK, nil, true
}

func isContinuationTransportFailure(err error) bool {
	// Check for empty stream error (both old message and new error type)
	if errors.Is(err, stream.ErrEmptyUpstreamStream) {
		return true
	}
	message := relayErrorMessage(err)
	return isUpstreamWSConnectionBroken(err) ||
		needsConversationRestart(message) ||
		strings.Contains(message, "ws stream ended before first event")
}

func (ra *relayAttempt) clientRequestHeaders() http.Header {
	if ra == nil || ra.c == nil || ra.c.Request == nil {
		return nil
	}
	return ra.c.Request.Header
}

func (ra *relayAttempt) handleWSStreamResponseV2(ctx context.Context, reader *wsUpstreamReader) error {
	defer ra.closeFirstTokenBudget()

	// Hand off early heartbeat
	ra.heartbeat.Hand()

	// Build transform function
	transform := func(ctx context.Context, data []byte) ([]byte, error) {
		return ra.transformStreamData(ctx, string(data))
	}

	// Determine first token timeout
	var firstTokenTimeout time.Duration
	if ra.firstTokenTimeOutSec > 0 && ra.firstTokenBudget == nil {
		firstTokenTimeout = time.Duration(ra.firstTokenTimeOutSec) * time.Second
	}

	// Create StreamProcessor
	processor := stream.NewStreamProcessor(stream.StreamConfig{
		Source:            stream.NewWSSource(reader),
		Transform:         transform,
		Writer:            ra.getStreamWriter(),
		Context:           ctx,
		FirstTokenTimeout: firstTokenTimeout,
		HeartbeatInterval: streamHeartbeatInterval(),
		OnFirstToken: func() {
			ra.metrics.SetFirstTokenTime(time.Now())
			ra.stopFirstTokenTimer()
		},
	})

	// Run processor
	err := processor.Run()

	// Track payload written for metrics collection
	if processor.PayloadWritten() {
		ra.streamPayloadWritten.Store(true)
	}

	// Handle first token timeout specifically
	if err != nil && strings.Contains(err.Error(), "first token timeout") {
		return ra.firstTokenTimeoutError()
	}

	// Check for context cancellation with first token timeout
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ctx, err); timeoutErr != nil {
			return timeoutErr
		}
	}

	return err
}

// forwardViaHTTP forwards the request using traditional HTTP.
func (ra *relayAttempt) forwardViaHTTP(ctx context.Context) (int, error) {
	// Check for passthrough capability using interface
	if pt, ok := ra.outAdapter.(model.PassthroughCapable); ok &&
		len(ra.rawBody) > 0 &&
		pt.CanPassthrough(ra.internalRequest.RawAPIFormat) {
		// Additional checks for OpenAI Responses edge cases
		if ra.internalRequest.RawAPIFormat == model.APIFormatOpenAIResponse {
			if ra.c == nil || ra.internalRequest.IsOpenAIExactReplayRequest() || requiresUpstreamWSContinuation(ra.internalRequest) {
				// Fall through to standard path
			} else {
				return ra.forwardViaHTTPPassthrough(ctx, pt)
			}
		} else {
			return ra.forwardViaHTTPPassthrough(ctx, pt)
		}
	}

	return ra.forwardViaHTTPStandard(ctx)
}

// forwardViaHTTPPassthrough handles unified passthrough for any PassthroughCapable transformer.
func (ra *relayAttempt) forwardViaHTTPPassthrough(ctx context.Context, pt model.PassthroughCapable) (int, error) {
	// Build request via TransformRequestRaw
	outboundRequest, err := pt.TransformRequestRaw(
		ctx,
		ra.rawBody,
		ra.internalRequest.Model,
		ra.channel.GetBaseUrl(),
		ra.usedKey.ChannelKey,
		ra.internalRequest.Query,
	)
	if err != nil {
		log.Warnf("failed to create passthrough request: %v", err)
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Apply param overrides
	if err := ra.applyParamOverride(outboundRequest); err != nil {
		return 0, err
	}

	// Copy headers
	ra.copyHeaders(outboundRequest)
	if ra.channel.Type == outbound.OutboundTypeOpenAIResponse {
		outboundRequest.Header.Set("Content-Type", "application/json")
	}

	// Send request
	response, err := ra.sendRequest(outboundRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	// Check status
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		ra.retryAfter = parseRetryAfter(response.Header.Get("Retry-After"))
		body, _ := io.ReadAll(response.Body)
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		log.Warnf("upstream error from channel %s: status=%d, body=%s", ra.channel.Name, response.StatusCode, string(body))
		return statusCode, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	// Get passthrough config
	cfg := pt.PassthroughConfig()

	// Branch: streaming vs non-streaming
	if ra.internalRequest.Stream != nil && *ra.internalRequest.Stream {
		if err := ra.handleStreamResponsePassthroughV2(ctx, response, cfg); err != nil {
			return 0, err
		}
		return response.StatusCode, nil
	}
	return response.StatusCode, ra.handleResponsePassthrough(ctx, response, cfg)
}

// handleResponsePassthrough handles non-streaming passthrough responses.
func (ra *relayAttempt) handleResponsePassthrough(ctx context.Context, response *http.Response, cfg model.PassthroughConfig) error {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	ra.c.Data(http.StatusOK, contentType, body)

	// Sidecar metrics parse
	sidecarResp := &http.Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
	if internalResponse, err := ra.outAdapter.TransformResponse(ctx, sidecarResp); err == nil && internalResponse != nil {
		ra.inAdapter.TransformResponse(ctx, internalResponse)
		if cfg.CollectMetrics {
			ra.collectResponse()
		}
	}

	return nil
}

// forwardViaHTTPStandard 是 forwardViaHTTP 的原路径（直通判定失败时的兜底）。
// 留作显式出口，避免 passthrough 失败时的递归。
func (ra *relayAttempt) forwardViaHTTPStandard(ctx context.Context) (int, error) {
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
	if err := ra.applyParamOverride(outboundRequest); err != nil {
		return 0, err
	}

	// 复制请求头
	ra.copyHeaders(outboundRequest)
	if ra.channel.Type == outbound.OutboundTypeOpenAIResponse {
		outboundRequest.Header.Set("Content-Type", "application/json")
	}

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
		log.Warnf("upstream error from channel %s: status=%d, body=%s", ra.channel.Name, response.StatusCode, string(body))
		return statusCode, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	// 处理响应
	if ra.internalRequest.Stream != nil && *ra.internalRequest.Stream {
		// Use V2 StreamProcessor-based implementation
		if err := ra.handleStreamResponseV2(ctx, response); err != nil {
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

// applyParamOverride merges channel-level JSON request overrides and records the final upstream payload.
func (ra *relayAttempt) applyParamOverride(outboundRequest *http.Request) error {
	if err := helper.ApplyParamOverride(outboundRequest, ra.channel.ParamOverride); err != nil {
		return err
	}
	if requestBody, readErr := readOutboundRequestBody(outboundRequest); readErr == nil {
		ra.metrics.SetTransportRequestPayload(requestBody, ra.internalRequest.Model)
	}
	return nil
}

// copyHeaders 复制请求头，过滤 hop-by-hop 头
func (ra *relayAttempt) copyHeaders(outboundRequest *http.Request) {
	if ra.c != nil {
		for key, values := range ra.c.Request.Header {
			lowerKey := strings.ToLower(key)
			if hopByHopHeaders[lowerKey] {
				continue
			}
			// anthropic-beta 需要与出站默认值合并去重，避免覆盖掉
			// 透传路径预置的 prompt-caching / extended-cache-ttl 基线。
			if lowerKey == "anthropic-beta" {
				existing := outboundRequest.Header.Get(key)
				for _, value := range values {
					existing = mergeBetaHeader(existing, value)
				}
				if existing != "" {
					outboundRequest.Header.Set(key, existing)
				}
				continue
			}
			for _, value := range values {
				outboundRequest.Header.Set(key, value)
			}
		}
	}
	if outboundRequest.Header.Get("User-Agent") == "" {
		outboundRequest.Header.Set("User-Agent", "")
	}
	if len(ra.channel.CustomHeader) > 0 {
		for _, header := range ra.channel.CustomHeader {
			outboundRequest.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}
}

// mergeBetaHeader 合并两个逗号分隔的 anthropic-beta 字段值，去重并保留先后顺序。
func mergeBetaHeader(existing, incoming string) string {
	seen := make(map[string]struct{}, 8)
	merged := make([]string, 0, 8)
	for _, source := range []string{existing, incoming} {
		for _, entry := range strings.Split(source, ",") {
			normalized := strings.TrimSpace(entry)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			merged = append(merged, normalized)
		}
	}
	return strings.Join(merged, ",")
}

// sendRequest 发送 HTTP 请求
func (ra *relayAttempt) sendRequest(req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHTTPClientWithContext(req.Context(), ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, err
	}

	req = ra.attachFirstTokenBudget(req)

	response, err := httpClient.Do(req)
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(req.Context(), err); timeoutErr != nil {
			ra.closeFirstTokenBudget()
			return nil, timeoutErr
		}
		if isClientCancellation(req.Context(), err) {
			log.Infof("request canceled before upstream response: %v", err)
		} else {
			log.Warnf("failed to send request: %v", err)
		}
		ra.closeFirstTokenBudget()
		return nil, err
	}

	if response != nil && response.Body != nil && ra.firstTokenBudget != nil {
		response.Body = &closeWithFuncReadCloser{
			ReadCloser: response.Body,
			onClose:    ra.closeFirstTokenBudget,
		}
	}

	return response, nil
}

// handleStreamResponseV2 uses StreamProcessor for unified stream handling.
func (ra *relayAttempt) handleStreamResponseV2(ctx context.Context, response *http.Response) error {
	defer ra.closeFirstTokenBudget()

	// Content-Type validation
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	// Hand off early heartbeat
	ra.heartbeat.Hand()

	// Build transform function
	transform := func(ctx context.Context, data []byte) ([]byte, error) {
		return ra.transformStreamData(ctx, string(data))
	}

	// Determine first token timeout
	var firstTokenTimeout time.Duration
	if ra.firstTokenTimeOutSec > 0 && ra.firstTokenBudget == nil {
		firstTokenTimeout = time.Duration(ra.firstTokenTimeOutSec) * time.Second
	}

	// Create StreamProcessor
	processor := stream.NewStreamProcessor(stream.StreamConfig{
		Source:            stream.NewSSESource(response.Body, maxSSEEventSize),
		Transform:         transform,
		Writer:            ra.getStreamWriter(),
		Context:           ctx,
		FirstTokenTimeout: firstTokenTimeout,
		HeartbeatInterval: streamHeartbeatInterval(),
		OnFirstToken: func() {
			ra.metrics.SetFirstTokenTime(time.Now())
			ra.stopFirstTokenTimer()
		},
	})

	// Run processor
	err := processor.Run()

	// Track payload written for metrics collection
	if processor.PayloadWritten() {
		ra.streamPayloadWritten.Store(true)
	}

	// Handle first token timeout specifically
	if err != nil && strings.Contains(err.Error(), "first token timeout") {
		_ = response.Body.Close()
		return ra.firstTokenTimeoutError()
	}

	// Check for context cancellation with first token timeout
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ctx, err); timeoutErr != nil {
			return timeoutErr
		}
	}

	return err
}

// handleStreamResponsePassthroughV2 uses StreamProcessor for unified passthrough handling.
// Works with any PassthroughCapable transformer (Anthropic, OpenAI Responses, etc.).
func (ra *relayAttempt) handleStreamResponsePassthroughV2(ctx context.Context, response *http.Response, cfg model.PassthroughConfig) error {
	defer ra.closeFirstTokenBudget()

	// Content-Type validation
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	// Hand off early heartbeat
	ra.heartbeat.Hand()

	// Determine first token timeout
	var firstTokenTimeout time.Duration
	if ra.firstTokenTimeOutSec > 0 && ra.firstTokenBudget == nil {
		firstTokenTimeout = time.Duration(ra.firstTokenTimeOutSec) * time.Second
	}

	// Buffer for raw stream (for metrics collection)
	var rawStreamBuf bytes.Buffer

	// Create StreamProcessor
	processor := stream.NewStreamProcessor(stream.StreamConfig{
		Source:            stream.NewRawSource(response.Body, 32*1024),
		Transform:         nil, // Passthrough: no transformation
		Writer:            ra.getStreamWriter(),
		Context:           ctx,
		FirstTokenTimeout: firstTokenTimeout,
		HeartbeatInterval: streamHeartbeatInterval(),
		BufferRawStream:   true,
		TerminalEvents:    cfg.TerminalEvents,
		OnFirstToken: func() {
			ra.metrics.SetFirstTokenTime(time.Now())
			ra.stopFirstTokenTimer()
		},
		OnFinish: func(ctx context.Context, rawStream []byte) error {
			if len(rawStream) == 0 {
				return stream.ErrEmptyUpstreamStream
			}
			// Copy to buffer for metrics collection
			rawStreamBuf.Write(rawStream)

			// Collect passthrough metrics
			ra.collectPassthroughMetrics(ctx, rawStream)

			// Collect response if configured
			if cfg.CollectMetrics {
				ra.collectResponse()
			}

			log.Debugf("passthrough stream end")
			return nil
		},
	})

	// Run processor
	err := processor.Run()

	// Track payload written for metrics collection
	if processor.PayloadWritten() {
		ra.streamPayloadWritten.Store(true)
	}

	// Handle first token timeout specifically
	if err != nil && strings.Contains(err.Error(), "first token timeout") {
		_ = response.Body.Close()
		return ra.firstTokenTimeoutError()
	}

	// Check for context cancellation with first token timeout
	if err != nil {
		if timeoutErr := ra.firstTokenTimeoutIfNeeded(ctx, err); timeoutErr != nil {
			return timeoutErr
		}
	}

	// On disconnect with partial data, still try to collect metrics
	if err != nil && errors.Is(err, context.Canceled) && rawStreamBuf.Len() > 0 {
		ra.collectPassthroughMetrics(context.Background(), rawStreamBuf.Bytes())
		if cfg.CollectMetrics {
			ra.collectResponse()
		}
	}

	return err
}

// collectPassthroughMetrics parses raw SSE stream for metrics aggregation without mutating response.
func (ra *relayAttempt) collectPassthroughMetrics(ctx context.Context, rawStream []byte) {
	if len(rawStream) == 0 {
		return
	}

	// Try stream event adapter first (preferred)
	outEventAdapter, outOk := ra.outAdapter.(model.OutboundStreamEventTransformer)
	inEventAdapter, inOk := ra.inAdapter.(model.InboundStreamEventTransformer)
	if outOk && inOk {
		readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
		for ev, err := range sse.Read(bytes.NewReader(rawStream), readCfg) {
			if err != nil {
				log.Debugf("passthrough metrics parse skipped: %v", err)
				return
			}
			if events, terr := outEventAdapter.TransformStreamEvent(ctx, []byte(ev.Data)); terr == nil && len(events) > 0 {
				_, _ = inEventAdapter.TransformStreamEvents(ctx, events)
			}
		}
		return
	}

	// Fallback to traditional stream transformer
	readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
	for ev, err := range sse.Read(bytes.NewReader(rawStream), readCfg) {
		if err != nil {
			log.Debugf("passthrough metrics parse skipped: %v", err)
			return
		}
		if chunk, terr := ra.outAdapter.TransformStream(ctx, []byte(ev.Data)); terr == nil && chunk != nil {
			_, _ = ra.inAdapter.TransformStream(ctx, chunk)
		}
	}
}

// transformStreamData 转换流式数据
func (ra *relayAttempt) transformStreamData(ctx context.Context, data string) ([]byte, error) {
	events, ok, err := ra.decodeOutboundStreamEvents(ctx, []byte(data))
	if err != nil {
		log.Warnf("failed to transform stream events: %v", err)
		return nil, err
	}
	if ok {
		return ra.encodeInboundStreamEvents(ctx, events)
	}

	internalStream, err := ra.decodeOutboundStreamResponse(ctx, []byte(data))
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}
	if internalStream == nil {
		return nil, nil
	}

	return ra.encodeInboundStreamResponse(ctx, internalStream)
}

func (ra *relayAttempt) decodeOutboundStreamEvents(ctx context.Context, data []byte) ([]model.StreamEvent, bool, error) {
	outEventAdapter, ok := ra.outAdapter.(model.OutboundStreamEventTransformer)
	if !ok {
		return nil, false, nil
	}
	if _, ok := ra.inAdapter.(model.InboundStreamEventTransformer); !ok {
		return nil, false, nil
	}
	events, err := outEventAdapter.TransformStreamEvent(ctx, data)
	if err != nil {
		return nil, true, err
	}
	return events, true, nil
}

func (ra *relayAttempt) encodeInboundStreamEvents(ctx context.Context, events []model.StreamEvent) ([]byte, error) {
	if len(events) == 0 {
		return nil, nil
	}
	inEventAdapter, ok := ra.inAdapter.(model.InboundStreamEventTransformer)
	if !ok {
		return nil, nil
	}
	inStream, err := inEventAdapter.TransformStreamEvents(ctx, events)
	if err != nil {
		log.Warnf("failed to transform inbound stream events: %v", err)
		return nil, err
	}
	return inStream, nil
}

func (ra *relayAttempt) decodeOutboundStreamResponse(ctx context.Context, data []byte) (*model.InternalLLMResponse, error) {
	return ra.outAdapter.TransformStream(ctx, data)
}

func (ra *relayAttempt) encodeInboundStreamResponse(ctx context.Context, internalStream *model.InternalLLMResponse) ([]byte, error) {
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
	if ra == nil || ra.inAdapter == nil || ra.metrics == nil {
		return
	}
	if !ra.responseCollected.CompareAndSwap(false, true) {
		return
	}
	internalResponse, err := ra.inAdapter.GetInternalResponse(ra.requestContext())
	if err != nil {
		log.Debugf("collectResponse: failed to get internal response: %v", err)
		return
	}
	if internalResponse == nil {
		log.Debugf("collectResponse: internal response is nil (stream may not be complete)")
		return
	}

	actualModel := strings.TrimSpace(internalResponse.Model)
	if actualModel == "" && ra.internalRequest != nil {
		actualModel = strings.TrimSpace(ra.internalRequest.Model)
	}
	ra.metrics.SetInternalResponse(internalResponse, actualModel)
}

func (ra *relayAttempt) collectOpenAIResponsesPassthroughMetrics(ctx context.Context, rawStream []byte) {
	if len(rawStream) == 0 {
		return
	}
	outEventAdapter, outOk := ra.outAdapter.(model.OutboundStreamEventTransformer)
	inEventAdapter, inOk := ra.inAdapter.(model.InboundStreamEventTransformer)
	if outOk && inOk {
		readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
		for ev, err := range sse.Read(bytes.NewReader(rawStream), readCfg) {
			if err != nil {
				log.Debugf("openai responses passthrough metrics parse skipped: %v", err)
				return
			}
			if events, terr := outEventAdapter.TransformStreamEvent(ctx, []byte(ev.Data)); terr == nil && len(events) > 0 {
				_, _ = inEventAdapter.TransformStreamEvents(ctx, events)
			}
		}
		return
	}
	readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
	for ev, err := range sse.Read(bytes.NewReader(rawStream), readCfg) {
		if err != nil {
			log.Debugf("openai responses passthrough metrics parse skipped: %v", err)
			return
		}
		if internalStream, terr := ra.outAdapter.TransformStream(ctx, []byte(ev.Data)); terr == nil && internalStream != nil {
			_, _ = ra.inAdapter.TransformStream(ctx, internalStream)
		}
	}
}

// responsesPassthroughTerminalEvents / anthropicPassthroughTerminalEvents 定义各协议
// SSE 流的终态事件类型；缓存流中出现终态事件即视为上游响应已完整送达。
var (
	responsesPassthroughTerminalEvents = map[string]struct{}{
		"response.completed":  {},
		"response.failed":     {},
		"response.incomplete": {},
		"error":               {},
	}
	anthropicPassthroughTerminalEvents = map[string]struct{}{
		"message_stop": {},
		"error":        {},
	}
)

// streamReachedTerminalEvent 报告缓存的原始 SSE 流是否已包含协议终态事件。
// 客户端 SDK 收到终态事件后会立即断连而不等上游 EOF，断连取消会沿出站请求
// 传播打断上游读取；此时读取被取消不代表流未完成。
func streamReachedTerminalEvent(rawStream []byte, terminalTypes map[string]struct{}) bool {
	if len(rawStream) == 0 {
		return false
	}
	readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
	for ev, err := range sse.Read(bytes.NewReader(rawStream), readCfg) {
		if err != nil {
			break
		}
		typ := strings.TrimSpace(ev.Type)
		if typ == "" {
			var head struct {
				Type string `json:"type"`
			}
			if json.Unmarshal([]byte(ev.Data), &head) == nil {
				typ = head.Type
			}
		}
		if _, ok := terminalTypes[typ]; ok {
			return true
		}
	}
	return false
}

// forwardViaHTTPStandard 是 forwardViaHTTP 的原路径（直通判定失败时的兜底）。
// 留作显式出口，避免 passthrough 失败时的递归。

func (ra *relayAttempt) collectAnthropicPassthroughMetrics(ctx context.Context, rawStream []byte) {
	if len(rawStream) == 0 {
		return
	}
	outEventAdapter, outOk := ra.outAdapter.(model.OutboundStreamEventTransformer)
	inEventAdapter, inOk := ra.inAdapter.(model.InboundStreamEventTransformer)
	if outOk && inOk {
		readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
		for ev, err := range sse.Read(bytes.NewReader(rawStream), readCfg) {
			if err != nil {
				log.Debugf("anthropic passthrough metrics parse skipped: %v", err)
				return
			}
			if events, terr := outEventAdapter.TransformStreamEvent(ctx, []byte(ev.Data)); terr == nil && len(events) > 0 {
				_, _ = inEventAdapter.TransformStreamEvents(ctx, events)
			}
		}
		return
	}
	readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
	for ev, err := range sse.Read(bytes.NewReader(rawStream), readCfg) {
		if err != nil {
			log.Debugf("anthropic passthrough metrics parse skipped: %v", err)
			return
		}
		if internalStream, terr := ra.outAdapter.TransformStream(ctx, []byte(ev.Data)); terr == nil && internalStream != nil {
			_, _ = ra.inAdapter.TransformStream(ctx, internalStream)
		}
	}
}
