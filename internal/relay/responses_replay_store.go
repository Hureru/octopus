package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/relay/balancer"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

// responsesReplayStateEntry stores HTTP replay state with TTL.
type responsesReplayStateEntry struct {
	state     *wsConversationState
	expiresAt time.Time
}

// responsesReplayStore is an in-process map keyed by apiKeyID:groupID:requestModel:hash(responseID).
var responsesReplayStore sync.Map

// responsesReplayStateKey generates the store key for a given replay state.
func responsesReplayStateKey(apiKeyID, groupID int, requestModel, responseID string) string {
	requestModel = strings.TrimSpace(requestModel)
	responseID = strings.TrimSpace(responseID)
	if requestModel == "" || responseID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(responseID))
	hashStr := hex.EncodeToString(hash[:])[:16]
	return fmt.Sprintf("%d:%d:%s:%s", apiKeyID, groupID, requestModel, hashStr)
}

// loadResponsesReplayState retrieves replay state by previous_response_id.
func loadResponsesReplayState(apiKeyID, groupID int, requestModel, responseID string) *wsConversationState {
	key := responsesReplayStateKey(apiKeyID, groupID, requestModel, responseID)
	if key == "" {
		return nil
	}

	v, ok := responsesReplayStore.Load(key)
	if !ok {
		return nil
	}

	entry, ok := v.(*responsesReplayStateEntry)
	if !ok || entry == nil || entry.state == nil {
		responsesReplayStore.Delete(key)
		return nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		responsesReplayStore.Delete(key)
		return nil
	}

	return cloneWSConversationState(entry.state)
}

// storeResponsesReplayState saves replay state after a successful HTTP turn.
func storeResponsesReplayState(apiKeyID, groupID int, requestModel string, state *wsConversationState, ttl time.Duration) {
	requestModel = strings.TrimSpace(requestModel)
	if requestModel == "" || state == nil {
		return
	}
	responseID := strings.TrimSpace(state.LastResponseID)
	if responseID == "" {
		return
	}
	if ttl <= 0 {
		ttl = wsClientMaxAge
	}

	key := responsesReplayStateKey(apiKeyID, groupID, requestModel, responseID)
	if key == "" {
		return
	}

	cloned := cloneWSConversationState(state)
	if cloned == nil {
		return
	}
	cloned.RequestModel = requestModel

	responsesReplayStore.Store(key, &responsesReplayStateEntry{
		state:     cloned,
		expiresAt: time.Now().Add(ttl),
	})
}

// resolveResponsesReplayState attempts to load replay state from the HTTP replay store
// when the request contains previous_response_id.
func resolveResponsesReplayState(apiKeyID, groupID int, requestModel string, req *transformerModel.InternalLLMRequest) *wsConversationState {
	requestModel = strings.TrimSpace(requestModel)
	if requestModel == "" || req == nil {
		return nil
	}
	prevID := req.OpenAIPreviousResponseID()
	if prevID == "" {
		return nil
	}
	return loadResponsesReplayState(apiKeyID, groupID, requestModel, prevID)
}

// responsesReplayStateToSticky converts replay state into a sticky balancer entry
// to preferentially route to the last successful channel/key.
func responsesReplayStateToSticky(state *wsConversationState) *balancer.SessionEntry {
	if state == nil || state.ChannelID <= 0 {
		return nil
	}
	return &balancer.SessionEntry{
		ChannelID:    state.ChannelID,
		ChannelKeyID: state.ChannelKeyID,
		Timestamp:    time.Now(),
	}
}

// resetResponsesReplayStore clears the entire replay store (for testing).
func resetResponsesReplayStore() {
	responsesReplayStore = sync.Map{}
}
