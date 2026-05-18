package relay

import (
	"context"
	"strings"

	"github.com/bestruirui/octopus/internal/utils/log"
)

func (ra *relayAttempt) recordSuccessfulWSAffinity(pc *pooledConn) {
	if ra == nil || ra.metrics == nil || ra.metrics.InternalResponse == nil || pc == nil {
		return
	}
	responseID := strings.TrimSpace(ra.metrics.InternalResponse.ID)
	if responseID == "" {
		return
	}
	ttl := wsAffinityTTL(ra.groupSessionTTL)
	bindWSResponseConn(responseID, pc.id, ttl)
	if ra.apiKeyID <= 0 || ra.groupID <= 0 || strings.TrimSpace(ra.requestModel) == "" {
		return
	}
	scope := wsAffinityScope{
		APIKeyID:     ra.apiKeyID,
		GroupID:      ra.groupID,
		RequestModel: ra.requestModel,
		ResponseID:   responseID,
	}
	entry := wsAffinityEntry{
		ChannelID:     ra.channel.ID,
		ChannelKeyID:  ra.usedKey.ID,
		UpstreamModel: ra.internalRequest.Model,
	}
	ctx := ra.requestContext()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := getWSAffinityStore().Set(ctx, scope, entry, ttl); err != nil {
		log.Debugf("failed to persist ws response affinity (apikey=%d, group=%d, request_model=%s, response_id=%s): %v", ra.apiKeyID, ra.groupID, ra.requestModel, responseID, err)
	}
}
