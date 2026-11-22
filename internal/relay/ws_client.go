package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const (
	wsClientReadLimit = 16 * 1024 * 1024 // 16MB per message
	// 回合进行中客户端又发来的消息先排在这里，最多这么多条（每条不超过 wsClientReadLimit）。
	// 正常的客户端一个回合里最多多发一条（比如打断）。
	wsClientQueueSize = 4
)

// 是变量，测试里要改短
var wsClientMaxAge = 60 * time.Minute

// wsClientSession 是一条客户端 WS 连接自己的东西，连接关闭时一起释放。
type wsClientSession struct {
	conn        *websocket.Conn
	apiKeyID    int
	requestSeen bool        // 这条连接上已经有过通过 Key 检查的请求（预热不算）
	headers     http.Header // 握手请求的头，每个请求都带给上游
	upstream    wsUpstream  // 这条连接独占的上游 WS，用到时才建
	// 每个模型一份对话存档。
	// shortcut: 存档大小没有上限，随对话变长，连接关闭（最长 60 分钟）才释放；有人拿它占内存时再加上限。
	states map[string]*wsConversationState
}

type wsRelayResult struct {
	Success           bool
	ResponseID        string
	ResetConversation bool
	Written           bool
	Canceled          bool
	Err               error
	PublicError       *wsPublicError
}

// HandleWSResponse handles WebSocket upgrade for /v1/responses.
func HandleWSResponse(c *gin.Context) {
	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow cross-origin
	})
	if err != nil {
		log.Warnf("websocket upgrade failed: %v", err)
		return
	}
	defer conn.CloseNow()

	conn.SetReadLimit(wsClientReadLimit)

	ctx, cancel := context.WithTimeout(c.Request.Context(), wsClientMaxAge)
	defer cancel()

	apiKeyID := c.GetInt("api_key_id")
	session := &wsClientSession{
		conn:     conn,
		apiKeyID: apiKeyID,
		headers:  c.Request.Header,
		states:   map[string]*wsConversationState{},
	}
	defer session.upstream.close()

	log.Debugf("ws client connected (apikey=%d)", apiKeyID)

	// 连接到了时限：先告诉客户端，再正常关闭。进行中的回合由 ctx 取消。
	stop := context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeWSError(ctx, conn, 400, "websocket_connection_limit_reached",
				"Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue.")
			conn.Close(websocket.StatusNormalClosure, "connection limit reached")
		}
	})
	defer stop()

	// 读协程：回合进行中也一直读客户端。这样客户端的 ping 有人回，它一断开进行中的请求马上取消。
	// 读到的消息排队交给下面的循环，一次处理一条。
	// shortcut: 队列排满（客户端在一个回合里连发好几条消息）时读协程停下来等，这期间和以前一样不回 ping、也发现不了断开；
	// 真有客户端这样用时再把队列加大。
	messages := make(chan []byte, wsClientQueueSize)
	// 读客户端不能用带时限的 ctx：时限一到这个库会直接断开连接，上面的通知就发不出去了
	readCtx := c.Request.Context()
	safe.Go("ws-client-reader", func() {
		defer close(messages)
		defer cancel() // 客户端走了：取消进行中的回合
		for {
			msgType, data, err := conn.Read(readCtx)
			if err != nil {
				closeStatus := websocket.CloseStatus(err)
				if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway || ctx.Err() != nil {
					log.Debugf("ws client disconnected normally (apikey=%d)", apiKeyID)
				} else {
					log.Warnf("ws client read error (apikey=%d): %v", apiKeyID, err)
				}
				return
			}
			if msgType != websocket.MessageText {
				continue
			}
			select {
			case messages <- data:
			case <-readCtx.Done():
				return // 处理函数已经返回（比如它 panic 了），没人再收消息
			}
		}
	})

	// Message loop：读协程退出（连接断了）才结束
	for data := range messages {
		if ctx.Err() != nil {
			continue // 客户端已经走了，或者连接到了时限：还排着的消息不处理
		}

		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			writeWSError(ctx, conn, 400, "invalid_request", "Failed to parse message")
			continue
		}

		if msg.Type == "response.interrupt" {
			// Codex 用它打断进行中的回复，这里读到时那个回合已经结束了。
			// shortcut: 不执行也不回应，回合照常跑完；Codex 要放弃回合时等不到确认会自己断开，读协程随即取消请求。
			// 不能回 error：Codex 会把它当成下一个回合的结果。要省下被打断的回合的用量时，再把它转给上游。
			continue
		}
		if msg.Type != "response.create" {
			writeWSError(ctx, conn, 400, "invalid_request",
				fmt.Sprintf("Unknown message type: %s", msg.Type))
			continue
		}

		session.processWSResponseCreate(ctx, data)
	}
}

func (s *wsClientSession) processWSResponseCreate(ctx context.Context, data []byte) {
	conn, apiKeyID := s.conn, s.apiKeyID
	var reqBody map[string]json.RawMessage
	if err := json.Unmarshal(data, &reqBody); err != nil {
		writeWSError(ctx, conn, 400, "invalid_request", "Failed to parse request body")
		return
	}

	// Remove WS-only fields
	delete(reqBody, "type")
	requestModel := strings.TrimSpace(extractWSRequestModel(reqBody))
	requestedPreviousResponseID := ""
	if raw, ok := reqBody["previous_response_id"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &requestedPreviousResponseID)
		requestedPreviousResponseID = strings.TrimSpace(requestedPreviousResponseID)
	}
	stateKey := requestModel
	// 只有接得上存档最后一个回复的请求才用存档拼成完整请求。
	// 其余情况存档对这个请求来说不完整，绝不拿来回放。
	replayState := s.states[stateKey]
	if replayState != nil && (requestedPreviousResponseID == "" || requestedPreviousResponseID != strings.TrimSpace(replayState.LastResponseID)) {
		replayState = nil
	}
	preferredSticky := responsesReplayStateToSticky(replayState)

	// 预热（generate:false）：Codex 连上后先发它，等到 response.completed 才发真正的请求。
	// 这个字段只有 WS 上有，不发给上游。
	prewarm := string(reqBody["generate"]) == "false"
	delete(reqBody, "generate")

	// 一条连接最长开 60 分钟：Key 的状态、限额和可用模型每条消息都重新查，和 HTTP 每个请求都查一样。
	apiKey, err := op.APIKeyGet(apiKeyID, ctx)
	if err != nil {
		writeWSError(ctx, conn, http.StatusUnauthorized, apperror.CodeAuthInvalidToken, resp.ErrUnauthorized)
		return
	}
	// 每分钟次数：握手时算过的那一次抵这条连接上的第一个请求；预热在本地应答，不算。
	if retryAfter, err := op.APIKeyCheck(apiKey, !prewarm && s.requestSeen); err != nil {
		event := wsErrorEvent(apperror.Status(err), apperror.Code(err), apperror.Message(err))
		if retryAfter > 0 {
			// 相当于 HTTP 的 Retry-After 响应头，Codex 从这里读
			event["headers"] = map[string]string{"retry-after": strconv.Itoa(retryAfter)}
		}
		if err := writeWSEvent(ctx, conn, event); err != nil {
			log.Debugf("ws error event write failed: %v", err)
		}
		return
	}
	s.requestSeen = s.requestSeen || !prewarm

	// Force stream mode
	reqBody["stream"] = json.RawMessage("true")

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		writeWSError(ctx, conn, 500, "server_error", "Failed to build request")
		return
	}

	// Parse request
	inAdapter := inbound.Get(inbound.InboundTypeOpenAIResponse)
	internalRequest, err := inAdapter.TransformRequest(ctx, bodyBytes)
	if err != nil {
		writeWSError(ctx, conn, 400, "invalid_request", err.Error())
		return
	}
	originalRequest := cloneInternalRequest(internalRequest)
	// 续传只发一次：直接把“存档 + 新输入”拼成不带 previous_response_id 的完整请求。
	executionRequest := originalRequest
	relayBody := bodyBytes // 同协议直通时发给上游的原始请求
	if replayState != nil {
		if replayed := replayState.BuildReplayRequest(originalRequest); replayed != nil {
			executionRequest = replayed
			relayBody = buildRawReplayBody(bodyBytes, replayed.OpenAIRawInputItems())
		} else {
			replayState = nil
			preferredSticky = nil
		}
	}
	// 本地没有存档的续传，历史在上游那里。刚完成那个回复的上游连接还在的话，回到它所在的渠道，
	// 不看共用的会话保持：用同一个 Key 的别的客户端可能已经把它改到别的渠道了。
	if preferredSticky == nil && requestedPreviousResponseID != "" && s.upstream.conn != nil && s.upstream.lastResponseID == requestedPreviousResponseID {
		preferredSticky = &balancer.SessionEntry{ChannelID: s.upstream.channelID, ChannelKeyID: s.upstream.keyID, Timestamp: time.Now()}
	}

	// Check supported models
	if apiKey.SupportedModels != "" && !slices.Contains(strings.Split(apiKey.SupportedModels, ","), executionRequest.Model) {
		writeWSError(ctx, conn, 400, "invalid_request", "model not supported")
		return
	}

	requestModel = executionRequest.Model
	// 存档只在两种成功之后更新：完整请求（从它重新开始）和接上了存档的请求（追加）。
	// 原样转发的续传，历史在上游那里，本地记不全，所以不记。
	remember := func(resp *transformerModel.InternalLLMResponse, attempts []dbmodel.ChannelAttempt) {
		if replayState == nil && continuesUpstreamHistory(originalRequest) {
			return
		}
		if replayState == nil {
			replayState = &wsConversationState{}
		}
		if channelID, keyID := finalChannelKey(attempts); channelID > 0 {
			replayState.ChannelID = channelID
			replayState.ChannelKeyID = keyID
		}
		replayState.ApplySuccessfulTurn(originalRequest, resp)
		s.states[stateKey] = replayState
		log.Debugf("ws relay success state stored (apikey=%d, request_model=%s, last_response_id=%s, channel=%d, key=%d)",
			apiKeyID, requestModel, strings.TrimSpace(replayState.LastResponseID), replayState.ChannelID, replayState.ChannelKeyID)
	}
	if prewarm {
		if replayState == nil && requestedPreviousResponseID != "" {
			// 接不上存档的预热不能在本地应答：下一轮会带着预热 ID 来，而它前面的历史本地没有。
			writeWSError(ctx, conn, 409, "previous_response_not_found",
				fmt.Sprintf("Previous response with id '%s' not found.", requestedPreviousResponseID))
			return
		}
		// 在本地应答，不占用上游。输入记进存档：下一轮带着预热 ID 来时，拼成完整请求发给上游。
		prewarmID, err := writeWSPrewarmAck(ctx, conn, requestModel)
		if err != nil {
			log.Debugf("ws prewarm ack write failed (apikey=%d): %v", apiKeyID, err)
			return
		}
		remember(&transformerModel.InternalLLMResponse{ID: prewarmID}, nil)
		return
	}

	req, group, err := newWSRelayRequest(ctx, conn, inAdapter, apiKeyID, requestModel, cloneInternalRequest(executionRequest), originalRequest, preferredSticky, bodyBytes)
	if err != nil {
		status := 404
		code := "model_not_found"
		if err.Error() == "no available channel" {
			status = 503
			code = "no_available_channel"
		}
		writeWSError(ctx, conn, status, code, err.Error())
		return
	}
	defer req.metrics.DropLive()
	req.rawBody = relayBody
	req.clientHeaders = s.headers
	req.upstream = &s.upstream
	req.previousResponseID = requestedPreviousResponseID
	req.continuationBody = bodyBytes
	if replayState != nil {
		req.metrics.SetWSMode(dbmodel.RelayLogWSModeReplay)
	}
	log.Debugf("ws relay prepared (apikey=%d, request_model=%s, previous_response_id=%s, replay=%t)",
		apiKeyID, requestModel, requestedPreviousResponseID, replayState != nil)

	result := finalizeWSRelay(ctx, conn, req, runWSRelay(ctx, req, group))
	if result.Success {
		remember(req.metrics.InternalResponse, req.iter.Attempts())
	} else if result.ResetConversation {
		log.Debugf("ws relay clearing conversation state (apikey=%d, request_model=%s, err=%v)", apiKeyID, requestModel, result.Err)
		delete(s.states, stateKey)
	}
	// 其余情况不动存档：它只会被 previous_response_id 对得上的请求用到。
}

func extractWSRequestModel(reqBody map[string]json.RawMessage) string {
	if len(reqBody) == 0 {
		return ""
	}
	modelRaw, ok := reqBody["model"]
	if !ok {
		return ""
	}
	var requestModel string
	if err := json.Unmarshal(modelRaw, &requestModel); err != nil {
		return ""
	}
	return requestModel
}

func newWSRelayRequest(
	ctx context.Context,
	conn *websocket.Conn,
	inAdapter transformerModel.Inbound,
	apiKeyID int,
	requestModel string,
	executionRequest *transformerModel.InternalLLMRequest,
	metricsRequest *transformerModel.InternalLLMRequest,
	preferredSticky *balancer.SessionEntry,
	rawBody []byte,
) (*relayRequest, *dbmodel.Group, error) {
	group, err := op.GroupGetEnabledMap(requestModel, ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("model not found")
	}

	iter := balancer.NewIteratorWithPreference(group, apiKeyID, requestModel, preferredSticky)
	if iter.Len() == 0 {
		return nil, nil, fmt.Errorf("no available channel")
	}

	metrics := NewRelayMetrics(apiKeyID, requestModel, rawBody, metricsRequest)
	iter.OnChange = metrics.PublishProgress

	return &relayRequest{
		c:               nil,
		ctx:             ctx,
		inAdapter:       inAdapter,
		internalRequest: executionRequest,
		metrics:         metrics,
		apiKeyID:        apiKeyID,
		requestModel:    requestModel,
		groupID:         group.ID,
		groupSessionTTL: group.SessionKeepTime,
		iter:            iter,
		streamWriter:    NewWSStreamWriter(ctx, conn),
	}, &group, nil
}

func runWSRelay(ctx context.Context, req *relayRequest, group *dbmodel.Group) wsRelayResult {
	maxSameChannelRetries := 1
	if group.RetryEnabled {
		maxSameChannelRetries = group.MaxRetries
		if maxSameChannelRetries <= 0 {
			maxSameChannelRetries = 3
		}
	}

	var lastErr error
	var lastResult attemptResult
	// 和 HTTP 入站一致：请求里有转换不了的原生工具或条目时，只能走 OpenAI Responses 渠道
	nativeRequired := req.internalRequest.HasOpenAIResponsesPassthrough()
	nativeChannelFound := false

	for req.iter.Next() {
		select {
		case <-ctx.Done():
			return wsRelayResult{Canceled: true, Err: ctx.Err()}
		default:
		}

		item := req.iter.Item()

		channel, err := op.ChannelGet(item.ChannelID, ctx)
		if err != nil {
			req.iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			req.iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}
		if nativeRequired {
			if channel.Type != outbound.OutboundTypeOpenAIResponse {
				req.iter.Skip(channel.ID, 0, channel.Name, "openai responses passthrough required")
				continue
			}
			nativeChannelFound = true
		}

		outAdapter := outbound.Get(channel.Type)
		if outAdapter == nil {
			req.iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}

		if !outbound.IsChatChannelType(channel.Type) {
			req.iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with chat request")
			continue
		}

		req.internalRequest.Model = item.ModelName

		selectOpts := dbmodel.ChannelKeySelectOptions{
			ExcludeKeyIDs:  make(map[int]struct{}),
			PreferredKeyID: req.iter.StickyKeyID(),
		}

		var usedKey dbmodel.ChannelKey
		for {
			usedKey = channel.GetChannelKey(selectOpts)
			if usedKey.ChannelKey == "" {
				break
			}
			if !req.iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				break
			}
			selectOpts.ExcludeKeyIDs[usedKey.ID] = struct{}{}
			usedKey = dbmodel.ChannelKey{}
		}
		if usedKey.ChannelKey == "" {
			if len(selectOpts.ExcludeKeyIDs) == 0 {
				req.iter.Skip(channel.ID, 0, channel.Name, "no available key")
			}
			continue
		}

		log.Debugf("ws request model %s, forwarding to channel: %s model: %s (attempt %d/%d)",
			req.requestModel, channel.Name, item.ModelName, req.iter.Index()+1, req.iter.Len())

		var result attemptResult
		for retryNum := 0; retryNum < maxSameChannelRetries; retryNum++ {
			if retryNum > 0 {
				delay := computeBackoff(retryNum, result.RetryAfter)
				select {
				case <-ctx.Done():
					return wsRelayResult{Canceled: true, Err: ctx.Err()}
				case <-time.After(delay):
				}

				// 重建 outAdapter 以重置流式状态（toolIndex, toolCalls 等）
				outAdapter = outbound.Get(channel.Type)
			}

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

		// 下面这一段和 HTTP 入站的 Handler 一样
		if !result.Success && !result.Written && !result.Canceled && !result.ResetConversation {
			failureKind := circuitFailureKind(group.RetryEnabled, result.StatusCode)
			balancer.RecordFailure(channel.ID, usedKey.ID, req.internalRequest.Model, failureKind)
			outlierwindow.Report(channel.ID, false, result.StatusCode, time.Now())
			if failureKind == balancer.FailureHard {
				maybeLearnManagedRoute(ctx, channel.ID, req.internalRequest.Model, inbound.InboundTypeOpenAIResponse, result.Err)
			}
		}

		if result.Success {
			outlierwindow.Report(channel.ID, true, result.StatusCode, time.Now())
			var respID string
			if req.metrics.InternalResponse != nil {
				respID = req.metrics.InternalResponse.ID
			}
			return wsRelayResult{Success: true, ResponseID: respID}
		}
		if result.ResetConversation {
			if publicErr, ok := classifyWSPublicError(result.Err, result.StatusCode); ok {
				return wsRelayResult{ResetConversation: publicErr.ResetConversation, Err: result.Err, PublicError: &publicErr}
			}
			return wsRelayResult{ResetConversation: true, Err: result.Err}
		}
		if result.Canceled || result.Written {
			return wsRelayResult{Written: result.Written, Canceled: result.Canceled, Err: result.Err}
		}
		lastErr = result.Err
		lastResult = result
	}

	if nativeRequired && !nativeChannelFound {
		return wsRelayResult{
			Err:         fmt.Errorf("openai responses native tools require an openai responses channel"),
			PublicError: &wsPublicError{Status: 400, Code: "invalid_request", Message: "当前请求包含 OpenAI Responses 原生工具，仅支持 OpenAI Responses 通道直通"},
		}
	}
	if publicErr, ok := classifyWSPublicError(lastErr, lastResult.StatusCode); ok {
		return wsRelayResult{ResetConversation: publicErr.ResetConversation, Err: lastErr, PublicError: &publicErr}
	}
	return wsRelayResult{Err: lastErr}
}

// finalizeWSRelay 记录回合的结果，并保证客户端收到结束事件：
// WS 上没有“流关闭”这个信号，不发的话客户端会一直等。
func finalizeWSRelay(ctx context.Context, conn *websocket.Conn, req *relayRequest, result wsRelayResult) wsRelayResult {
	terminal := req.streamWriter.(*WSStreamWriter).terminalEvent()
	if result.Success && terminal == "" {
		result = wsRelayResult{Written: true, Err: errors.New("upstream stream ended without a terminal event")}
	}
	req.metrics.SaveWithChannelStats(ctx, result.Success, result.Err, req.iter.Attempts(), false)
	if result.Success {
		return result
	}
	if terminal != "" || ctx.Err() != nil {
		// 结束事件已经发过了；或者客户端已经走了；或者连接到了时限，由 HandleWSResponse 通知
		return result
	}
	if result.Written {
		// 已经输出了一部分，不能再换渠道重来。客户端已经断开时这一帧发不出去，没有影响
		writeWSError(ctx, conn, 502, "upstream_stream_interrupted", "上游在回复完成前中断了，请重试")
		return result
	}
	if result.PublicError != nil {
		if result.PublicError.ResetConversation {
			balancer.DeleteSticky(req.apiKeyID, req.requestModel)
		}
		writeWSError(ctx, conn, result.PublicError.Status, result.PublicError.Code, result.PublicError.Message)
		return result
	}
	writeWSError(ctx, conn, 502, "all_channels_failed", "All channels failed")
	return result
}

func writeWSError(ctx context.Context, conn *websocket.Conn, status int, code, message string) {
	if err := writeWSEvent(ctx, conn, wsErrorEvent(status, code, message)); err != nil {
		log.Debugf("ws error event write failed: %v", err)
	}
}

func wsErrorEvent(status int, code, message string) map[string]any {
	errorType := "invalid_request_error"
	switch {
	case status == 429:
		errorType = "rate_limit_error"
	case status >= 500:
		errorType = "server_error"
	}
	return map[string]any{
		"type":   "error",
		"status": status,
		"error": map[string]any{
			"type":    errorType,
			"code":    code,
			"message": message,
		},
	}
}

func finalChannelKey(attempts []dbmodel.ChannelAttempt) (int, int) {
	var lastChannelID int
	var lastChannelKeyID int
	for i := len(attempts) - 1; i >= 0; i-- {
		attempt := attempts[i]
		if attempt.Status == dbmodel.AttemptSuccess {
			return attempt.ChannelID, attempt.ChannelKeyID
		}
		if attempt.Status == dbmodel.AttemptFailed && lastChannelID == 0 {
			lastChannelID = attempt.ChannelID
			lastChannelKeyID = attempt.ChannelKeyID
		}
	}
	return lastChannelID, lastChannelKeyID
}
