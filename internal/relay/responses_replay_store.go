package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/relay/balancer"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const (
	responsesReplayStoreMaxEntries = 10000                // 最大条目数
	responsesReplayStoreMaxSize    = 100 * 1024 * 1024    // 最大总大小 100MB（估算）
	responsesReplayStoreSweepInterval = 5 * time.Minute   // 清理间隔
)

// responsesReplayStateEntry stores HTTP replay state with TTL.
type responsesReplayStateEntry struct {
	state     *wsConversationState
	expiresAt time.Time
	size      int // 估算大小
}

// responsesReplayStore is an in-process map keyed by apiKeyID:groupID:requestModel:hash(responseID).
var responsesReplayStore sync.Map

var responsesReplayStoreStats struct {
	entries     atomic.Int64
	totalSize   atomic.Int64
	sweepTicker *time.Ticker
	sweepStop   chan struct{}
	sweepOnce   sync.Once
}

func init() {
	startResponsesReplayStoreSweeper()
}

// startResponsesReplayStoreSweeper 启动后台清理协程
func startResponsesReplayStoreSweeper() {
	responsesReplayStoreStats.sweepOnce.Do(func() {
		responsesReplayStoreStats.sweepTicker = time.NewTicker(responsesReplayStoreSweepInterval)
		responsesReplayStoreStats.sweepStop = make(chan struct{})
		go func() {
			for {
				select {
				case <-responsesReplayStoreStats.sweepTicker.C:
					sweepExpiredResponsesReplayStates()
				case <-responsesReplayStoreStats.sweepStop:
					return
				}
			}
		}()
	})
}

// sweepExpiredResponsesReplayStates 清理过期条目
func sweepExpiredResponsesReplayStates() {
	now := time.Now()
	removed := 0
	responsesReplayStore.Range(func(key, value interface{}) bool {
		entry, ok := value.(*responsesReplayStateEntry)
		if !ok || entry == nil {
			responsesReplayStore.Delete(key)
			responsesReplayStoreStats.entries.Add(-1)
			removed++
			return true
		}
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			responsesReplayStore.Delete(key)
			responsesReplayStoreStats.entries.Add(-1)
			responsesReplayStoreStats.totalSize.Add(-int64(entry.size))
			removed++
		}
		return true
	})
	if removed > 0 {
		log.Debugf("HTTP replay store sweep: removed %d expired entries, current entries=%d, size=%d",
			removed, responsesReplayStoreStats.entries.Load(), responsesReplayStoreStats.totalSize.Load())
	}
}

// responsesReplayStateKey generates the store key for a given replay state.
func responsesReplayStateKey(apiKeyID, groupID int, requestModel, responseID string) string {
	requestModel = strings.TrimSpace(requestModel)
	responseID = strings.TrimSpace(responseID)
	if requestModel == "" || responseID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(responseID))
	hashStr := hex.EncodeToString(hash[:])[:32] // 使用 128-bit 降低碰撞风险
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

	// 容量检查
	currentEntries := responsesReplayStoreStats.entries.Load()
	if currentEntries >= responsesReplayStoreMaxEntries {
		log.Warnf("HTTP replay store capacity limit reached (%d entries), skipping save", currentEntries)
		return
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

	// 估算大小
	estimatedSize := estimateStateSize(cloned)
	currentSize := responsesReplayStoreStats.totalSize.Load()
	if currentSize+int64(estimatedSize) > responsesReplayStoreMaxSize {
		log.Warnf("HTTP replay store size limit reached (%d bytes), skipping save", currentSize)
		return
	}

	// 检查是否更新已有条目
	if existing, ok := responsesReplayStore.Load(key); ok {
		if oldEntry, ok := existing.(*responsesReplayStateEntry); ok && oldEntry != nil {
			responsesReplayStoreStats.totalSize.Add(-int64(oldEntry.size))
		}
	} else {
		responsesReplayStoreStats.entries.Add(1)
	}

	responsesReplayStore.Store(key, &responsesReplayStateEntry{
		state:     cloned,
		expiresAt: time.Now().Add(ttl),
		size:      estimatedSize,
	})
	responsesReplayStoreStats.totalSize.Add(int64(estimatedSize))
}

// estimateStateSize 估算状态大小（字节）
func estimateStateSize(state *wsConversationState) int {
	if state == nil {
		return 0
	}
	size := 256 // 基础结构
	size += len(state.DownstreamSessionID) + len(state.RequestModel) + len(state.LastResponseID)
	size += len(state.ReplayWindowItems)
	size += len(state.Transcript) * 512 // 每条消息估算 512 字节
	size += len(state.ReplayAliases) * 64
	return size
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
