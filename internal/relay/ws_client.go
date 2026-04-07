package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const (
	wsClientMaxAge    = 60 * time.Minute
	wsClientReadLimit = 16 * 1024 * 1024 // 16MB per message
)

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
	supportedModels := c.GetString("supported_models")

	log.Infof("ws client connected (apikey=%d)", apiKeyID)

	var conversationState *wsConversationState

	// Message loop
	for {
		select {
		case <-ctx.Done():
			writeWSError(ctx, conn, 400, "websocket_connection_limit_reached",
				"Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue.")
			conn.Close(websocket.StatusNormalClosure, "connection limit reached")
			return
		default:
		}

		msgType, data, err := conn.Read(ctx)
		if err != nil {
			closeStatus := websocket.CloseStatus(err)
			if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway {
				log.Infof("ws client disconnected normally (apikey=%d)", apiKeyID)
			} else {
				log.Warnf("ws client read error (apikey=%d): %v", apiKeyID, err)
			}
			return
		}

		if msgType != websocket.MessageText {
			continue
		}

		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			writeWSError(ctx, conn, 400, "invalid_request", "Failed to parse message")
			continue
		}

		if msg.Type != "response.create" {
			writeWSError(ctx, conn, 400, "invalid_request",
				fmt.Sprintf("Unknown message type: %s", msg.Type))
			continue
		}

		conversationState = processWSResponseCreate(ctx, conn, data, apiKeyID, supportedModels, conversationState)
	}
}

func processWSResponseCreate(
	ctx context.Context,
	conn *websocket.Conn,
	data []byte,
	apiKeyID int,
	supportedModels string,
	conversationState *wsConversationState,
) *wsConversationState {
	var reqBody map[string]json.RawMessage
	if err := json.Unmarshal(data, &reqBody); err != nil {
		writeWSError(ctx, conn, 400, "invalid_request", "Failed to parse request body")
		return conversationState
	}

	// Remove WS-only fields
	delete(reqBody, "type")
	requestModel := strings.TrimSpace(extractWSRequestModel(reqBody))
	allowStoredRestore := wsRequestExplicitlyRequestsContinuation(reqBody)
	requestedPreviousResponseID := ""
	if raw, ok := reqBody["previous_response_id"]; ok && len(raw) > 0 {
		_ = json.Unmarshal(raw, &requestedPreviousResponseID)
		requestedPreviousResponseID = strings.TrimSpace(requestedPreviousResponseID)
	}
	hadLocalState := conversationState != nil
	conversationState = resolveWSConversationState(apiKeyID, requestModel, conversationState, allowStoredRestore)
	hasResolvedState := conversationState != nil
	resolvedLastResponseID := ""
	if conversationState != nil {
		resolvedLastResponseID = strings.TrimSpace(conversationState.LastResponseID)
	}
	log.Debugf("ws response.create state resolved (apikey=%d, request_model=%s, requested_prev=%s, explicit_continuation=%t, had_local_state=%t, resolved_state=%t, resolved_last_response_id=%s)",
		apiKeyID, requestModel, requestedPreviousResponseID, allowStoredRestore, hadLocalState, hasResolvedState, resolvedLastResponseID)
	rewriteWSPreviousResponseID(reqBody, conversationState)
	preferredSticky := wsConversationStateToSticky(conversationState)

	// Check for generate: false (warmup)
	if genRaw, ok := reqBody["generate"]; ok {
		var generate bool
		if json.Unmarshal(genRaw, &generate) == nil && !generate {
			if err := bestEffortWarmupUpstreamWS(ctx, apiKeyID, supportedModels, reqBody); err != nil {
				log.Warnf("ws warmup failed (apikey=%d): %v", apiKeyID, err)
			} else {
				log.Infof("ws warmup ready (apikey=%d)", apiKeyID)
			}
			delete(reqBody, "generate")
			writeWSEvent(ctx, conn, map[string]interface{}{
				"type": "response.created",
				"response": map[string]interface{}{
					"object": "response",
					"id":     fmt.Sprintf("resp_warmup_%d", time.Now().UnixNano()),
					"status": "completed",
					"output": []interface{}{},
				},
			})
			return conversationState
		}
		delete(reqBody, "generate")
	}

	injectWSPreviousResponseID(reqBody, conversationState)

	// Force stream mode
	reqBody["stream"] = json.RawMessage("true")

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		writeWSError(ctx, conn, 500, "server_error", "Failed to build request")
		return conversationState
	}

	// Parse request
	inAdapter := inbound.Get(inbound.InboundTypeOpenAIResponse)
	internalRequest, err := inAdapter.TransformRequest(ctx, bodyBytes)
	if err != nil {
		writeWSError(ctx, conn, 400, "invalid_request", err.Error())
		return conversationState
	}
	originalRequest := cloneInternalRequest(internalRequest)

	// Check supported models
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		found := false
		for _, m := range supportedModelsArray {
			if m == internalRequest.Model {
				found = true
				break
			}
		}
		if !found {
			writeWSError(ctx, conn, 400, "invalid_request", "model not supported")
			return conversationState
		}
	}

	requestModel = internalRequest.Model
	req, group, err := newWSRelayRequest(ctx, conn, inAdapter, apiKeyID, requestModel, cloneInternalRequest(internalRequest), originalRequest, preferredSticky)
	if err != nil {
		status := 404
		code := "model_not_found"
		if err.Error() == "no available channel" {
			status = 503
			code = "no_available_channel"
		}
		writeWSError(ctx, conn, status, code, err.Error())
		return conversationState
	}

	autoRestart := conversationState != nil && conversationState.CanAutoRestart(originalRequest)
	failedPreviousResponseID := currentPreviousResponseID(originalRequest)
	log.Debugf("ws relay prepared (apikey=%d, request_model=%s, previous_response_id=%s, auto_replay=%t, preferred_channel=%d, preferred_key=%d)",
		apiKeyID, requestModel, failedPreviousResponseID, autoRestart,
		func() int {
			if preferredSticky == nil {
				return 0
			}
			return preferredSticky.ChannelID
		}(),
		func() int {
			if preferredSticky == nil {
				return 0
			}
			return preferredSticky.ChannelKeyID
		}())
	result := runWSRelay(ctx, req, group)
	if result.ResetConversation && autoRestart && !req.streamWriter.Written() {
		log.Debugf("ws relay switching to replay (apikey=%d, request_model=%s, failed_previous_response_id=%s, reset_conversation=%t)",
			apiKeyID, requestModel, failedPreviousResponseID, result.ResetConversation)
		balancer.DeleteSticky(apiKeyID, requestModel)
		replayedRequest := conversationState.BuildReplayRequest(originalRequest)
		replayReq, replayGroup, replayErr := newWSRelayRequest(ctx, conn, inAdapter, apiKeyID, requestModel, replayedRequest, originalRequest, preferredSticky)
		if replayErr == nil {
			replayReq.metrics.SetWSMode(dbmodel.RelayLogWSModeReplay)
			replayReq.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReplay)
			req = replayReq
			group = replayGroup
			result = runWSRelay(ctx, req, group)
		}
	}

	result = finalizeWSRelay(ctx, conn, req, result)
	if result.Success {
		if conversationState == nil {
			conversationState = &wsConversationState{}
		}
		if channelID, keyID := finalChannelKey(req.iter.Attempts()); channelID > 0 {
			conversationState.ChannelID = channelID
			conversationState.ChannelKeyID = keyID
		}
		if req.metrics.WSMode != nil && *req.metrics.WSMode == dbmodel.RelayLogWSModeReplay {
			conversationState.RememberReplayAlias(failedPreviousResponseID)
		}
		conversationState.ApplySuccessfulTurn(originalRequest, req.metrics.InternalResponse)
		storeWSConversationState(apiKeyID, requestModel, conversationState, wsConversationStateTTL(group.SessionKeepTime))
		log.Debugf("ws relay success state stored (apikey=%d, request_model=%s, ws_mode=%v, ws_recovery=%v, last_response_id=%s, channel=%d, key=%d)",
			apiKeyID, requestModel, req.metrics.WSMode, req.metrics.WSRecovery,
			strings.TrimSpace(conversationState.LastResponseID), conversationState.ChannelID, conversationState.ChannelKeyID)
		return conversationState
	}
	if result.ResetConversation {
		log.Debugf("ws relay clearing conversation state (apikey=%d, request_model=%s, err=%v)", apiKeyID, requestModel, result.Err)
		deleteWSConversationState(apiKeyID, requestModel)
		return nil
	}

	return conversationState
}

func bestEffortWarmupUpstreamWS(
	ctx context.Context,
	apiKeyID int,
	supportedModels string,
	reqBody map[string]json.RawMessage,
) error {
	requestModel := strings.TrimSpace(extractWSRequestModel(reqBody))
	if requestModel == "" {
		return fmt.Errorf("warmup request missing model")
	}

	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		found := false
		for _, modelName := range supportedModelsArray {
			if modelName == requestModel {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("model not supported")
		}
	}

	group, err := op.GroupGetEnabledMap(requestModel, ctx)
	if err != nil {
		return fmt.Errorf("model not found")
	}

	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		return fmt.Errorf("no available channel")
	}

	var lastErr error
	for iter.Next() {
		item := iter.Item()

		channel, err := op.ChannelGet(item.ChannelID, ctx)
		if err != nil {
			lastErr = err
			continue
		}
		if !channel.Enabled || channel.Type != outbound.OutboundTypeOpenAIResponse {
			continue
		}

		selectOpts := dbmodel.ChannelKeySelectOptions{
			ExcludeKeyIDs:  make(map[int]struct{}),
			PreferredKeyID: iter.StickyKeyID(),
		}

		for {
			usedKey := channel.GetChannelKey(selectOpts)
			if usedKey.ChannelKey == "" {
				break
			}
			if iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				selectOpts.ExcludeKeyIDs[usedKey.ID] = struct{}{}
				continue
			}

			if err := warmupUpstreamWSConnection(ctx, channel, usedKey); err != nil {
				lastErr = err
				selectOpts.ExcludeKeyIDs[usedKey.ID] = struct{}{}
				continue
			}

			balancer.SetSticky(apiKeyID, requestModel, channel.ID, usedKey.ID)
			return nil
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no ws-capable channel available for warmup")
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

func warmupUpstreamWSConnection(ctx context.Context, channel *dbmodel.Channel, usedKey dbmodel.ChannelKey) error {
	warmupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	pc := TryUpstreamWS(warmupCtx, channel, channel.GetBaseUrl(), usedKey.ChannelKey, usedKey.ID, nil)
	if pc == nil {
		return fmt.Errorf("upstream ws unavailable")
	}

	wsUpstreamPool.Put(pc)
	return nil
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
) (*relayRequest, *dbmodel.Group, error) {
	group, err := op.GroupGetEnabledMap(requestModel, ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("model not found")
	}

	iter := balancer.NewIteratorWithPreference(group, apiKeyID, requestModel, preferredSticky)
	if iter.Len() == 0 {
		return nil, nil, fmt.Errorf("no available channel")
	}

	return &relayRequest{
		c:               nil,
		ctx:             ctx,
		inAdapter:       inAdapter,
		internalRequest: executionRequest,
		metrics:         NewRelayMetrics(apiKeyID, requestModel, metricsRequest),
		apiKeyID:        apiKeyID,
		requestModel:    requestModel,
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
			IgnoreRecent429Cooldown: group.RetryEnabled,
			ExcludeKeyIDs:           make(map[int]struct{}),
			PreferredKeyID:          req.iter.StickyKeyID(),
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

		log.Infof("ws request model %s, forwarding to channel: %s model: %s (attempt %d/%d)",
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
			}

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

		if !result.Success && !result.Written && !result.Canceled && !result.ResetConversation {
			failureKind := circuitFailureKind(group.RetryEnabled, result.StatusCode)
			balancer.RecordFailure(channel.ID, usedKey.ID, req.internalRequest.Model, failureKind)
		}

		if result.Success {
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

	if publicErr, ok := classifyWSPublicError(lastErr, lastResult.StatusCode); ok {
		return wsRelayResult{ResetConversation: publicErr.ResetConversation, Err: lastErr, PublicError: &publicErr}
	}
	return wsRelayResult{Err: lastErr}
}

func finalizeWSRelay(ctx context.Context, conn *websocket.Conn, req *relayRequest, result wsRelayResult) wsRelayResult {
	if result.Success {
		req.metrics.Save(ctx, true, nil, req.iter.Attempts())
		return result
	}

	req.metrics.Save(ctx, false, result.Err, req.iter.Attempts())
	if result.Canceled || result.Written {
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

func injectWSPreviousResponseID(reqBody map[string]json.RawMessage, state *wsConversationState) {
	if state == nil || strings.TrimSpace(state.LastResponseID) == "" {
		return
	}
	if _, exists := reqBody["previous_response_id"]; exists {
		return
	}
	reqBody["previous_response_id"] = json.RawMessage(fmt.Sprintf("%q", state.LastResponseID))
}

func rewriteWSPreviousResponseID(reqBody map[string]json.RawMessage, state *wsConversationState) {
	if state == nil || len(reqBody) == 0 {
		return
	}
	raw, ok := reqBody["previous_response_id"]
	if !ok || len(raw) == 0 {
		return
	}
	var previousResponseID string
	if err := json.Unmarshal(raw, &previousResponseID); err != nil {
		return
	}
	if !state.ShouldRewritePreviousResponseID(previousResponseID) {
		return
	}
	reqBody["previous_response_id"] = json.RawMessage(fmt.Sprintf("%q", state.LastResponseID))
}

func currentPreviousResponseID(req *transformerModel.InternalLLMRequest) string {
	if req == nil || req.PreviousResponseID == nil {
		return ""
	}
	return strings.TrimSpace(*req.PreviousResponseID)
}

func wsRequestExplicitlyRequestsContinuation(reqBody map[string]json.RawMessage) bool {
	if len(reqBody) == 0 {
		return false
	}
	if raw, ok := reqBody["previous_response_id"]; ok && len(raw) > 0 {
		var previousResponseID string
		if err := json.Unmarshal(raw, &previousResponseID); err == nil && strings.TrimSpace(previousResponseID) != "" {
			return true
		}
	}
	if raw, ok := reqBody["conversation"]; ok && len(raw) > 0 && string(raw) != "null" {
		return true
	}
	return false
}

func writeWSError(ctx context.Context, conn *websocket.Conn, status int, code, message string) {
	errEvent := map[string]interface{}{
		"type":   "error",
		"status": status,
		"error": map[string]interface{}{
			"type":    "invalid_request_error",
			"code":    code,
			"message": message,
		},
	}
	writeWSEvent(ctx, conn, errEvent)
}

func writeWSEvent(ctx context.Context, conn *websocket.Conn, event interface{}) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	conn.Write(ctx, websocket.MessageText, data)
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
