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
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

const (
	wsClientMaxAge    = 60 * time.Minute
	wsClientReadLimit = 16 * 1024 * 1024 // 16MB per message
)

// cachedWSResponse stores the last response for previous_response_id chaining.
type cachedWSResponse struct {
	ID string
}

type wsRelayResult struct {
	Success           bool
	ResponseID        string
	ResetConversation bool
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

	var lastRespCache *cachedWSResponse

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

		lastRespCache = processWSResponseCreate(ctx, conn, data, apiKeyID, supportedModels, lastRespCache)
	}
}

func processWSResponseCreate(
	ctx context.Context,
	conn *websocket.Conn,
	data []byte,
	apiKeyID int,
	supportedModels string,
	lastCache *cachedWSResponse,
) *cachedWSResponse {
	var reqBody map[string]json.RawMessage
	if err := json.Unmarshal(data, &reqBody); err != nil {
		writeWSError(ctx, conn, 400, "invalid_request", "Failed to parse request body")
		return lastCache
	}

	// Remove WS-only fields
	delete(reqBody, "type")

	// Check for generate: false (warmup)
	if genRaw, ok := reqBody["generate"]; ok {
		var generate bool
		if json.Unmarshal(genRaw, &generate) == nil && !generate {
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
			return lastCache
		}
		delete(reqBody, "generate")
	}

	injectWSPreviousResponseID(reqBody, lastCache)

	// Force stream mode
	reqBody["stream"] = json.RawMessage("true")

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		writeWSError(ctx, conn, 500, "server_error", "Failed to build request")
		return lastCache
	}

	// Parse request
	inAdapter := inbound.Get(inbound.InboundTypeOpenAIResponse)
	internalRequest, err := inAdapter.TransformRequest(ctx, bodyBytes)
	if err != nil {
		writeWSError(ctx, conn, 400, "invalid_request", err.Error())
		return lastCache
	}

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
			return lastCache
		}
	}

	requestModel := internalRequest.Model

	group, err := op.GroupGetEnabledMap(requestModel, ctx)
	if err != nil {
		writeWSError(ctx, conn, 404, "model_not_found", "model not found")
		return lastCache
	}

	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		writeWSError(ctx, conn, 503, "no_available_channel", "no available channel")
		return lastCache
	}

	wsWriter := NewWSStreamWriter(ctx, conn)
	metrics := NewRelayMetrics(apiKeyID, requestModel, internalRequest)

	req := &relayRequest{
		c:               nil,
		ctx:             ctx,
		inAdapter:       inAdapter,
		internalRequest: internalRequest,
		metrics:         metrics,
		apiKeyID:        apiKeyID,
		requestModel:    requestModel,
		iter:            iter,
		streamWriter:    wsWriter,
	}

	result := executeWSRelay(ctx, conn, req, &group)

	if result.Success && result.ResponseID != "" {
		return &cachedWSResponse{ID: result.ResponseID}
	}
	if result.ResetConversation {
		return nil
	}

	return lastCache
}

func executeWSRelay(ctx context.Context, conn *websocket.Conn, req *relayRequest, group *dbmodel.Group) wsRelayResult {
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
			return wsRelayResult{}
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
					return wsRelayResult{}
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
			if result.Success || result.Written || result.Canceled || !isRetryableStatus(result.StatusCode) {
				break
			}
		}

		if !result.Success && !result.Written && !result.Canceled {
			failureKind := circuitFailureKind(group.RetryEnabled, result.StatusCode)
			balancer.RecordFailure(channel.ID, usedKey.ID, req.internalRequest.Model, failureKind)
		}

		if result.Success {
			req.metrics.Save(ctx, true, nil, req.iter.Attempts())
			// Get response ID from metrics (already collected by attempt → collectResponse)
			var respID string
			if req.metrics.InternalResponse != nil {
				respID = req.metrics.InternalResponse.ID
			}
			return wsRelayResult{Success: true, ResponseID: respID}
		}
		if result.Canceled || result.Written {
			req.metrics.Save(ctx, false, result.Err, req.iter.Attempts())
			return wsRelayResult{}
		}
		lastErr = result.Err
		lastResult = result
	}

	req.metrics.Save(ctx, false, lastErr, req.iter.Attempts())
	if publicErr, ok := classifyWSPublicError(lastErr, lastResult.StatusCode); ok {
		if publicErr.ResetConversation {
			balancer.DeleteSticky(req.apiKeyID, req.requestModel)
		}
		writeWSError(ctx, conn, publicErr.Status, publicErr.Code, publicErr.Message)
		return wsRelayResult{ResetConversation: publicErr.ResetConversation}
	}
	writeWSError(ctx, conn, 502, "all_channels_failed", "All channels failed")
	return wsRelayResult{}
}

func injectWSPreviousResponseID(reqBody map[string]json.RawMessage, lastCache *cachedWSResponse) {
	if lastCache == nil || strings.TrimSpace(lastCache.ID) == "" {
		return
	}
	if _, exists := reqBody["previous_response_id"]; exists {
		return
	}
	reqBody["previous_response_id"] = json.RawMessage(fmt.Sprintf("%q", lastCache.ID))
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
