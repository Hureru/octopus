package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

type responsesCompactRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input,omitempty"`
	PreviousResponseID *string         `json:"previous_response_id,omitempty"`
}

type responsesCompactResponse struct {
	ID        string                         `json:"id"`
	Object    string                         `json:"object"`
	CreatedAt int64                          `json:"created_at"`
	Output    []openaiOutbound.ResponsesItem `json:"output"`
	Usage     *openaiOutbound.ResponsesUsage `json:"usage,omitempty"`
	Error     *transformerModel.ErrorDetail  `json:"error,omitempty"`
}

// HandleResponsesCompact proxies OpenAI-compatible /responses/compact requests upstream.
func HandleResponsesCompact(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	var compactReq responsesCompactRequest
	if err := json.Unmarshal(body, &compactReq); err != nil {
		resp.Error(c, http.StatusBadRequest, fmt.Sprintf("failed to decode responses compact request: %v", err))
		return
	}
	if strings.TrimSpace(compactReq.Model) == "" {
		resp.Error(c, http.StatusBadRequest, "model is required")
		return
	}
	if len(compactReq.Input) == 0 && compactReq.PreviousResponseID == nil {
		resp.Error(c, http.StatusBadRequest, "either input or previous_response_id is required")
		return
	}

	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, compactReq.Model) {
			resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
			return
		}
	}

	requestModel := compactReq.Model
	routingModel := requestModel
	var canonicalModel *dbmodel.CanonicalModel
	if canonical, ok := op.CatalogResolveIdentity(requestModel); ok {
		if !canonical.Enabled {
			resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "model disabled")
			return
		}
		routingModel = canonical.Name
		canonicalModel = &canonical
	}
	apiKeyID := c.GetInt("api_key_id")

	group, err := op.GroupGetEnabledMap(routingModel, c.Request.Context())
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return
	}
	features := []dbmodel.ProtocolFeature{dbmodel.ProtocolFeatureResponsesState}
	if compactReq.PreviousResponseID != nil {
		features = append(features, dbmodel.ProtocolFeatureContinuation)
	}
	group, preview, plannedCanonical, err := op.CatalogPlanGroup(
		c.Request.Context(),
		requestModel,
		dbmodel.ProtocolRouteRequirements{
			InboundProtocol: dbmodel.ProtocolOpenAIResponses,
			Features:        features,
		},
		group,
	)
	if err != nil {
		resp.ErrorWithCode(c, http.StatusInternalServerError, CodeRelayNoAvailableChannel, "route planning failed")
		return
	}
	if plannedCanonical != nil {
		canonicalModel = plannedCanonical
	}
	protocolDecisions := routeDecisionMap(preview)

	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	recordProtocolPlanningSkips(c.Request.Context(), iter, preview)
	if iter.Len() == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}

	metricsReq := &transformerModel.InternalLLMRequest{
		Model:        requestModel,
		RawRequest:   body,
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}
	metrics := NewRelayMetrics(apiKeyID, requestModel, body, metricsReq)
	if canonicalModel != nil {
		metrics.SetRouting(
			*canonicalModel,
			0,
			dbmodel.ProtocolOpenAIResponses,
			dbmodel.ProtocolOpenAIResponses,
			string(dbmodel.ProtocolExecutionModePassthrough),
		)
	}

	var lastErr error
	var lastStatusCode int
	var lastRetryAfter time.Duration

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
			log.Infof("compact request context canceled, stopping retry")
			metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}

		item := iter.Item()
		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}
		if !supportsResponsesCompact(channel.Type) {
			iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with responses compact")
			continue
		}
		decision := protocolDecisions[relayRouteDecisionKey(channel.ID, item.ModelName)]
		candidateID := decision.RouteCandidateID
		if candidateID == 0 && canonicalModel != nil {
			if candidate, candidateErr := op.CatalogCandidateFor(
				c.Request.Context(),
				canonicalModel.ID,
				channel.ID,
				item.ModelName,
			); candidateErr == nil {
				candidateID = candidate.ID
			}
		}
		metrics.ClearEffectivePrice()
		if canonicalModel != nil {
			metrics.SetRouting(
				*canonicalModel,
				candidateID,
				dbmodel.ProtocolOpenAIResponses,
				dbmodel.ProtocolOpenAIResponses,
				string(dbmodel.ProtocolExecutionModePassthrough),
			)
		} else {
			metrics.RouteCandidateID = candidateID
		}
		if effectivePrice, priceErr := op.EffectivePriceForCandidate(
			c.Request.Context(),
			candidateID,
			item.ModelName,
		); priceErr == nil {
			metrics.SetEffectivePrice(effectivePrice)
		}
		metrics.SetProtocolDecision(
			dbmodel.ProtocolOpenAIResponses,
			dbmodel.ProtocolOpenAIResponses,
			dbmodel.ProtocolExecutionModePassthrough,
			decision.ProtocolPolicy.Normalize(dbmodel.ProtocolPolicyAuto),
			decision.AllowLossy,
			decision.Warnings,
		)
		setProtocolWarningHeader(c, decision.Warnings)

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

		var attemptErr error
		var statusCode int
		var retryAfter time.Duration
		var success bool

		for retryNum := 0; retryNum < maxSameChannelRetries; retryNum++ {
			if retryNum > 0 {
				delay := computeBackoff(retryNum, retryAfter)
				select {
				case <-c.Request.Context().Done():
					metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
					return
				case <-time.After(delay):
				}
			}

			attemptBody, bodyErr := compactRequestBodyForModel(body, item.ModelName)
			if bodyErr != nil {
				attemptErr = bodyErr
				break
			}
			statusCode, retryAfter, attemptErr = forwardResponsesCompact(
				c,
				metrics,
				iter,
				channel,
				usedKey,
				attemptBody,
				item.ModelName,
				metrics.CanonicalModelID,
				candidateID,
			)
			if attemptErr == nil {
				success = true
				break
			}
			if !isRetryableStatus(statusCode) {
				break
			}
		}

		usedKey.StatusCode = statusCode
		usedKey.LastUseTimeStamp = time.Now().Unix()
		op.ChannelKeyUpdate(usedKey)

		if success {
			op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestSuccess: 1})
			balancer.RecordSuccess(channel.ID, usedKey.ID, requestModel)
			balancer.SetSticky(apiKeyID, requestModel, channel.ID, usedKey.ID)
			outlierwindow.Report(channel.ID, true, statusCode, time.Now())
			metrics.SaveWithChannelStats(c.Request.Context(), true, nil, iter.Attempts(), false)
			return
		}

		op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestFailed: 1})
		failureKind := circuitFailureKind(group.RetryEnabled, statusCode)
		balancer.RecordFailure(channel.ID, usedKey.ID, requestModel, failureKind)
		outlierwindow.Report(channel.ID, false, statusCode, time.Now())
		lastErr = attemptErr
		lastStatusCode = statusCode
		lastRetryAfter = retryAfter
	}

	metrics.SaveWithChannelStats(c.Request.Context(), false, lastErr, iter.Attempts(), false)
	if lastErr == nil && lastStatusCode == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}
	if isPassthroughStatus(lastStatusCode) {
		if lastRetryAfter > 0 {
			c.Header("Retry-After", fmt.Sprintf("%d", int(lastRetryAfter.Seconds())))
		}
		resp.Error(c, lastStatusCode, "channel failed")
		return
	}
	if lastStatusCode > 0 {
		resp.Error(c, lastStatusCode, "channel failed")
		return
	}
	resp.Error(c, http.StatusBadGateway, "channel failed")
}

func supportsResponsesCompact(channelType outbound.OutboundType) bool {
	switch channelType {
	case outbound.OutboundTypeOpenAIResponse:
		return true
	default:
		return false
	}
}

func forwardResponsesCompact(
	c *gin.Context,
	metrics *RelayMetrics,
	iter *balancer.Iterator,
	channel *dbmodel.Channel,
	usedKey dbmodel.ChannelKey,
	requestBody []byte,
	actualModel string,
	canonicalModelID int,
	routeCandidateID int,
) (int, time.Duration, error) {
	span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)
	span.SetRouteCandidateID(metrics.RouteCandidateID)
	request, err := buildResponsesCompactRequest(c.Request.Context(), channel, usedKey.ChannelKey, requestBody)
	if err != nil {
		span.End(dbmodel.AttemptFailed, 0, err.Error())
		return 0, 0, fmt.Errorf("failed to create compact request: %w", err)
	}
	metrics.SetTransportRequestPayload(requestBody, metrics.RequestModel)
	policy := copyProxyHeaders(
		c.Request.Context(),
		c.Request.Header,
		channel,
		request.Header,
		canonicalModelID,
		routeCandidateID,
	)
	if len(policy.Trace) > 0 {
		if payload, marshalErr := json.Marshal(policy.Trace); marshalErr == nil {
			metrics.HeaderPolicyTrace = string(payload)
		}
	}

	response, err := sendCompactRequest(channel, request)
	if err != nil {
		span.End(dbmodel.AttemptFailed, 0, err.Error())
		return 0, 0, fmt.Errorf("failed to send compact request: %w", err)
	}
	defer response.Body.Close()

	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		span.End(dbmodel.AttemptFailed, response.StatusCode, readErr.Error())
		return response.StatusCode, 0, fmt.Errorf("failed to read compact response body: %w", readErr)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryAfter := parseRetryAfter(response.Header.Get("Retry-After"))
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		span.End(dbmodel.AttemptFailed, statusCode, string(body))
		return statusCode, retryAfter, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	copyProxyResponseHeaders(c.Writer.Header(), response.Header)
	contentType := response.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	c.Data(response.StatusCode, contentType, body)

	var compactResp responsesCompactResponse
	if err := json.Unmarshal(body, &compactResp); err == nil {
		metrics.SetInternalResponse(compactResponseToInternalResponse(&compactResp), actualModel)
	}

	span.SetUsage(metrics.AttemptUsageSnapshot())
	span.End(dbmodel.AttemptSuccess, response.StatusCode, "")
	return response.StatusCode, 0, nil
}

func compactRequestBodyForModel(requestBody []byte, modelName string) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(requestBody, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode compact request: %w", err)
	}
	modelJSON, err := json.Marshal(strings.TrimSpace(modelName))
	if err != nil {
		return nil, err
	}
	payload["model"] = modelJSON
	return json.Marshal(payload)
}

func buildResponsesCompactRequest(ctx context.Context, channel *dbmodel.Channel, key string, requestBody []byte) (*http.Request, error) {
	parsedURL, err := url.Parse(strings.TrimSuffix(channel.GetBaseUrl(), "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedURL.Path = parsedURL.Path + "/responses/compact"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

func copyProxyHeaders(
	ctx context.Context,
	src http.Header,
	channel *dbmodel.Channel,
	dst http.Header,
	canonicalModelID int,
	routeCandidateID int,
) dbmodel.ResolvedHeaderPolicy {
	policy, err := op.ResolveHeaderPolicy(
		ctx,
		channel.ID,
		canonicalModelID,
		routeCandidateID,
	)
	if err != nil {
		log.Warnf("resolve compact header policy failed (channel=%d): %v", channel.ID, err)
		policy = op.HeaderPolicyFailureFallback()
	}
	op.ApplyHeaderPolicy(dst, src, channel.CustomHeader, policy)
	dst.Set("Content-Type", "application/json")
	// 防止 Go 默认 User-Agent 泄露到上游
	if dst.Get("User-Agent") == "" {
		dst.Set("User-Agent", "")
	}
	return policy
}

func copyProxyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func sendCompactRequest(channel *dbmodel.Channel, req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHTTPClientWithContext(req.Context(), channel)
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func compactResponseToInternalResponse(resp *responsesCompactResponse) *transformerModel.InternalLLMResponse {
	if resp == nil {
		return nil
	}
	return &transformerModel.InternalLLMResponse{
		ID:      resp.ID,
		Object:  resp.Object,
		Created: resp.CreatedAt,
		Usage:   convertCompactUsage(resp.Usage),
	}
}

func convertCompactUsage(usage *openaiOutbound.ResponsesUsage) *transformerModel.Usage {
	if usage == nil {
		return nil
	}
	result := &transformerModel.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.InputTokenDetails.CachedTokens > 0 {
		result.PromptTokensDetails = &transformerModel.PromptTokensDetails{
			CachedTokens: usage.InputTokenDetails.CachedTokens,
		}
	}
	if usage.OutputTokenDetails.ReasoningTokens > 0 {
		result.CompletionTokensDetails = &transformerModel.CompletionTokensDetails{
			ReasoningTokens: usage.OutputTokenDetails.ReasoningTokens,
		}
	}
	return result
}
