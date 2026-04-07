package relay

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/relay/balancer"
)

type wsConversationStateEntry struct {
	state     *wsConversationState
	expiresAt time.Time
}

var wsConversationStore sync.Map // key: apiKeyID:requestModel -> *wsConversationStateEntry

func wsConversationStateKey(apiKeyID int, requestModel string) string {
	return fmt.Sprintf("%d:%s", apiKeyID, strings.TrimSpace(requestModel))
}

func loadWSConversationState(apiKeyID int, requestModel string) *wsConversationState {
	requestModel = strings.TrimSpace(requestModel)
	if requestModel == "" {
		return nil
	}

	v, ok := wsConversationStore.Load(wsConversationStateKey(apiKeyID, requestModel))
	if !ok {
		return nil
	}

	entry, ok := v.(*wsConversationStateEntry)
	if !ok || entry == nil || entry.state == nil {
		wsConversationStore.Delete(wsConversationStateKey(apiKeyID, requestModel))
		return nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		wsConversationStore.Delete(wsConversationStateKey(apiKeyID, requestModel))
		return nil
	}

	return cloneWSConversationState(entry.state)
}

func storeWSConversationState(apiKeyID int, requestModel string, state *wsConversationState, ttl time.Duration) {
	requestModel = strings.TrimSpace(requestModel)
	if requestModel == "" || state == nil {
		return
	}
	if ttl <= 0 {
		ttl = wsClientMaxAge
	}

	cloned := cloneWSConversationState(state)
	if cloned == nil {
		return
	}
	cloned.RequestModel = requestModel

	wsConversationStore.Store(wsConversationStateKey(apiKeyID, requestModel), &wsConversationStateEntry{
		state:     cloned,
		expiresAt: time.Now().Add(ttl),
	})
}

func deleteWSConversationState(apiKeyID int, requestModel string) {
	requestModel = strings.TrimSpace(requestModel)
	if requestModel == "" {
		return
	}
	wsConversationStore.Delete(wsConversationStateKey(apiKeyID, requestModel))
}

func resolveWSConversationState(apiKeyID int, requestModel string, localState *wsConversationState, allowStoredRestore bool) *wsConversationState {
	requestModel = strings.TrimSpace(requestModel)
	if requestModel == "" {
		return localState
	}
	if localState != nil && localState.MatchesRequestModel(requestModel) {
		return localState
	}
	if !allowStoredRestore {
		return nil
	}
	return loadWSConversationState(apiKeyID, requestModel)
}

func wsConversationStateToSticky(state *wsConversationState) *balancer.SessionEntry {
	if state == nil || state.ChannelID <= 0 {
		return nil
	}
	return &balancer.SessionEntry{
		ChannelID:    state.ChannelID,
		ChannelKeyID: state.ChannelKeyID,
		Timestamp:    time.Now(),
	}
}

func wsConversationStateTTL(sessionKeepTimeSec int) time.Duration {
	if sessionKeepTimeSec <= 0 {
		return wsClientMaxAge
	}
	ttl := time.Duration(sessionKeepTimeSec) * time.Second
	if ttl > wsClientMaxAge {
		return wsClientMaxAge
	}
	return ttl
}

func resetWSConversationStateStore() {
	wsConversationStore = sync.Map{}
}
