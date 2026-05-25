package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/coder/websocket"
)

type wsPassthroughStats struct {
	ResponseID string
	Model      string
	Usage      *transformerModel.Usage
	RawOutput  json.RawMessage
	Error      *wsUpstreamEventError
}

type wsUpstreamEventError struct {
	Status  int
	Code    string
	Type    string
	Message string
}

func (e *wsUpstreamEventError) Error() string {
	if e == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Code != "" {
		parts = append(parts, "code="+e.Code)
	}
	if e.Type != "" {
		parts = append(parts, "type="+e.Type)
	}
	if e.Status > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.Status))
	}
	if len(parts) == 0 {
		return "upstream ws error"
	}
	return strings.Join(parts, ", ")
}

func (ra *relayAttempt) forwardViaWSPassthrough(ctx context.Context) (int, error) {
	continuation := requiresUpstreamWSContinuation(ra.internalRequest)
	preferredConnID := ""
	if continuation {
		preferredConnID, _ = getWSResponseConn(currentPreviousResponseID(ra.internalRequest))
	}
	pc := TryUpstreamWSWithPreference(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey, ra.usedKey.ID, ra.clientRequestHeaders(), preferredConnID)
	if pc == nil {
		log.Debugf("upstream WS passthrough unavailable for channel %s (key=%d, continuation=%t)", ra.channel.Name, ra.usedKey.ID, continuation)
		return -1, nil
	}

	payload, err := ra.buildWSPassthroughRequestPayload()
	if err != nil {
		wsUpstreamPool.Put(pc)
		return -1, nil
	}
	ra.metrics.SetTransportRequestPayload(payload, ra.internalRequest.Model)
	if err := wsUpstreamPool.SendRaw(ctx, pc, payload); err != nil {
		log.Warnf("upstream WS passthrough send failed for channel %s: %v", ra.channel.Name, err)
		wsUpstreamPool.RemoveConn(pc)
		if isUpstreamWSConnectionBroken(err) {
			statusCode, redialErr, recovered := ra.retryViaFreshUpstreamWSPassthrough(ctx, payload)
			if recovered || redialErr != nil {
				return statusCode, redialErr
			}
			if continuation {
				return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
			}
		}
		wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		return -1, nil
	}

	ra.metrics.UsedWS = true
	ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModePassthrough)
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	stats, err := ra.handleWSPassthroughStream(ctx, pc)
	if err != nil {
		ra.applyWSPassthroughStats(stats)
		wsUpstreamPool.RemoveConn(pc)
		if continuation && !ra.streamPayloadWritten.Load() && shouldReconnectUpstreamWSBeforeReplay(err) {
			statusCode, redialErr, recovered := ra.retryViaFreshUpstreamWSPassthrough(ctx, payload)
			if recovered || redialErr != nil {
				return statusCode, redialErr
			}
		}
		if continuation && isContinuationTransportFailure(err) {
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation")
		}
		if ra.requestContext().Err() == nil {
			wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		}
		return http.StatusBadGateway, err
	}
	wsUpstreamPool.Put(pc)
	wsUpstreamPool.RecordWSSuccess(ra.channel.ID)
	ra.applyWSPassthroughStats(stats)
	ra.recordSuccessfulWSAffinity(pc)
	return http.StatusOK, nil
}

func (ra *relayAttempt) retryViaFreshUpstreamWSPassthrough(ctx context.Context, payload []byte) (int, error, bool) {
	redialed := TryUpstreamWSWithPreference(ctx, ra.channel, ra.channel.GetBaseUrl(), ra.usedKey.ChannelKey, ra.usedKey.ID, ra.clientRequestHeaders(), "", true)
	if redialed == nil {
		return 0, nil, false
	}
	if err := wsUpstreamPool.SendRaw(ctx, redialed, payload); err != nil {
		wsUpstreamPool.RemoveConn(redialed)
		wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		if requiresUpstreamWSContinuation(ra.internalRequest) {
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation"), true
		}
		return -1, nil, true
	}
	ra.metrics.UsedWS = true
	ra.metrics.SetWSExecMode(dbmodel.RelayLogWSExecModePassthrough)
	if ra.metrics.WSMode == nil {
		ra.metrics.SetWSMode(defaultWSModeForRequest(ra.internalRequest))
	}
	ra.metrics.SetWSRecovery(dbmodel.RelayLogWSRecoveryReconnect)
	stats, err := ra.handleWSPassthroughStream(ctx, redialed)
	if err != nil {
		ra.applyWSPassthroughStats(stats)
		wsUpstreamPool.RemoveConn(redialed)
		if requiresUpstreamWSContinuation(ra.internalRequest) && isContinuationTransportFailure(err) {
			return http.StatusConflict, fmt.Errorf("upstream continuation transport unavailable; please restart the conversation"), true
		}
		if ra.requestContext().Err() == nil {
			wsUpstreamPool.RecordWSFailure(ra.channel.ID)
		}
		return http.StatusBadGateway, err, true
	}
	wsUpstreamPool.Put(redialed)
	wsUpstreamPool.RecordWSSuccess(ra.channel.ID)
	ra.applyWSPassthroughStats(stats)
	ra.recordSuccessfulWSAffinity(redialed)
	return http.StatusOK, nil, true
}

func (ra *relayAttempt) buildWSPassthroughRequestPayload() ([]byte, error) {
	body := ra.rawBody
	if len(body) == 0 {
		responsesReq := openaiOutbound.ConvertToResponsesRequest(ra.internalRequest)
		var err error
		body, err = json.Marshal(responsesReq)
		if err != nil {
			return nil, err
		}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["type"] = json.RawMessage(`"response.create"`)
	payload["stream"] = json.RawMessage(`true`)
	delete(payload, "background")
	if ra.internalRequest != nil && strings.TrimSpace(ra.internalRequest.Model) != "" {
		modelBytes, err := json.Marshal(ra.internalRequest.Model)
		if err != nil {
			return nil, err
		}
		payload["model"] = modelBytes
	}
	return json.Marshal(payload)
}

func (ra *relayAttempt) handleWSPassthroughStream(ctx context.Context, pc *pooledConn) (*wsPassthroughStats, error) {
	writer := ra.getStreamWriter()
	stats := &wsPassthroughStats{}
	firstEvent := true
	dropDownstream := false
	readCtx := ctx
	for {
		msgType, data, err := pc.conn.Read(readCtx)
		if err != nil {
			closeStatus := websocket.CloseStatus(err)
			if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway {
				if firstEvent {
					return stats, fmt.Errorf("ws stream ended before first event")
				}
				return stats, nil
			}
			return stats, fmt.Errorf("ws passthrough read error: %w", err)
		}
		if msgType != websocket.MessageText {
			continue
		}
		observeWSPassthroughEvent(stats, data)
		if stats.Error != nil {
			if !dropDownstream {
				out := ra.rewriteWSPassthroughDownstreamModel(data)
				if writeErr := writeWSPassthroughDownstream(ctx, writer, out); writeErr != nil {
					log.Debugf("ws passthrough: failed to forward upstream error frame downstream (channel=%d, key=%d): %v", ra.channel.ID, ra.usedKey.ID, writeErr)
				} else {
					ra.streamPayloadWritten.Store(true)
				}
			}
			return stats, stats.Error
		}
		if !dropDownstream {
			out := ra.rewriteWSPassthroughDownstreamModel(data)
			if writeErr := writeWSPassthroughDownstream(ctx, writer, out); writeErr != nil {
				if isClientCancellation(ctx, writeErr) || isUpstreamWSConnectionBroken(writeErr) {
					log.Debugf("ws passthrough downstream write failed; draining upstream (channel=%d, key=%d): %v", ra.channel.ID, ra.usedKey.ID, writeErr)
					dropDownstream = true
					ra.streamPayloadWritten.Store(true)
					if readCtx == ctx && isClientCancellation(ctx, writeErr) {
						drainCtx, drainCancel := context.WithTimeout(context.Background(), wsPassthroughDrainTimeout)
						defer drainCancel()
						readCtx = drainCtx
					}
					continue
				}
				return stats, writeErr
			}
			ra.streamPayloadWritten.Store(true)
		}
		firstEvent = false
		if isWSPassthroughTerminal(data) {
			return stats, nil
		}
	}
}

func writeWSPassthroughDownstream(ctx context.Context, writer StreamWriter, out []byte) error {
	if wsWriter, ok := writer.(*WSStreamWriter); ok {
		writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
		writeErr := wsWriter.conn.Write(writeCtx, websocket.MessageText, out)
		cancel()
		return writeErr
	}
	if _, writeErr := writer.Write([]byte("data: " + string(out) + "\n\n")); writeErr != nil {
		return writeErr
	}
	writer.Flush()
	return nil
}

func (ra *relayAttempt) rewriteWSPassthroughDownstreamModel(data []byte) []byte {
	if ra == nil || ra.internalRequest == nil || strings.TrimSpace(ra.requestModel) == "" || strings.TrimSpace(ra.internalRequest.Model) == strings.TrimSpace(ra.requestModel) {
		return data
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return data
	}
	if replaceModelInJSONValue(payload, ra.internalRequest.Model, ra.requestModel) {
		if rewritten, err := json.Marshal(payload); err == nil {
			return rewritten
		}
	}
	return data
}

func replaceModelInJSONValue(value any, upstreamModel, downstreamModel string) bool {
	switch v := value.(type) {
	case map[string]any:
		changed := false
		for key, child := range v {
			if key == "model" {
				if s, ok := child.(string); ok && s == upstreamModel {
					v[key] = downstreamModel
					changed = true
				}
				continue
			}
			if replaceModelInJSONValue(child, upstreamModel, downstreamModel) {
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for _, child := range v {
			if replaceModelInJSONValue(child, upstreamModel, downstreamModel) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func observeWSPassthroughEvent(stats *wsPassthroughStats, data []byte) {
	if stats == nil || len(data) == 0 {
		return
	}
	var event struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status int    `json:"status"`
		Error  *struct {
			Code    any    `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		Response *struct {
			ID     string               `json:"id"`
			Model  string               `json:"model"`
			Status string               `json:"status"`
			Output json.RawMessage      `json:"output"`
			Usage  *responsesUsageEvent `json:"usage"`
			Error  *struct {
				Code    any    `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
		Usage *responsesUsageEvent `json:"usage"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}
	if event.ID != "" {
		stats.ResponseID = event.ID
	}
	if event.Model != "" {
		stats.Model = event.Model
	}
	if event.Usage != nil {
		stats.Usage = event.Usage.toInternal()
	}
	if event.Error != nil {
		stats.Error = &wsUpstreamEventError{Status: event.Status, Code: normalizeWSUpstreamErrorCode(event.Error.Code), Type: event.Error.Type, Message: event.Error.Message}
	}
	if event.Response != nil {
		if event.Response.ID != "" {
			stats.ResponseID = event.Response.ID
		}
		if event.Response.Model != "" {
			stats.Model = event.Response.Model
		}
		if len(event.Response.Output) > 0 && !bytes.Equal(bytes.TrimSpace(event.Response.Output), []byte("null")) {
			stats.RawOutput = append(json.RawMessage(nil), event.Response.Output...)
		}
		if event.Response.Usage != nil {
			stats.Usage = event.Response.Usage.toInternal()
		}
		if event.Response.Error != nil {
			status := event.Status
			if status == 0 {
				status = http.StatusBadGateway
			}
			stats.Error = &wsUpstreamEventError{Status: status, Code: normalizeWSUpstreamErrorCode(event.Response.Error.Code), Type: event.Response.Error.Type, Message: event.Response.Error.Message}
		}
	}
}

func normalizeWSUpstreamErrorCode(code any) string {
	switch v := code.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

type responsesUsageEvent struct {
	InputTokens       int64 `json:"input_tokens"`
	InputTokenDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens       int64 `json:"output_tokens"`
	OutputTokenDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	TotalTokens int64 `json:"total_tokens"`
}

func (u *responsesUsageEvent) toInternal() *transformerModel.Usage {
	if u == nil {
		return nil
	}
	usage := &transformerModel.Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: u.TotalTokens}
	if u.InputTokenDetails.CachedTokens > 0 {
		usage.PromptTokensDetails = &transformerModel.PromptTokensDetails{CachedTokens: u.InputTokenDetails.CachedTokens}
	}
	if u.OutputTokenDetails.ReasoningTokens > 0 {
		usage.CompletionTokensDetails = &transformerModel.CompletionTokensDetails{ReasoningTokens: u.OutputTokenDetails.ReasoningTokens}
	}
	return usage
}

func isWSPassthroughTerminal(data []byte) bool {
	var event struct {
		Type     string `json:"type"`
		Response *struct {
			Status string `json:"status"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return false
	}
	if isWSStreamTerminalEvent(event.Type) {
		return true
	}
	if event.Response != nil {
		switch event.Response.Status {
		case "completed", "failed", "incomplete", "cancelled", "canceled":
			return true
		}
	}
	return false
}

func (ra *relayAttempt) applyWSPassthroughStats(stats *wsPassthroughStats) {
	if ra == nil || ra.metrics == nil || stats == nil {
		return
	}
	modelName := strings.TrimSpace(stats.Model)
	if modelName == "" && ra.internalRequest != nil {
		modelName = ra.internalRequest.Model
	}
	resp := &transformerModel.InternalLLMResponse{
		ID:                      stats.ResponseID,
		Object:                  "response",
		Created:                 time.Now().Unix(),
		Model:                   modelName,
		Usage:                   stats.Usage,
		RawResponsesOutputItems: stats.RawOutput,
	}
	ra.metrics.SetInternalResponse(resp, modelName)
}
