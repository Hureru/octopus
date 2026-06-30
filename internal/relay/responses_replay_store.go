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
	responsesReplayStoreMaxEntries    = 10000
	responsesReplayStoreMaxSize       = 100 * 1024 * 1024
	responsesReplayStoreSweepInterval = 5 * time.Minute
)

type responsesReplayStateEntry struct {
	state     *wsConversationState
	expiresAt time.Time
	size      int
}

var responsesReplayStore sync.Map

var responsesReplayStoreStats struct {
	entries   atomic.Int64
	totalSize atomic.Int64
}

func init() {
	startResponsesReplayStoreSweeper()
}

var responsesReplayStoreSweepStop = make(chan struct{})

func startResponsesReplayStoreSweeper() {
	go func() {
		ticker := time.NewTicker(responsesReplayStoreSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sweepExpiredResponsesReplayStates()
			case <-responsesReplayStoreSweepStop:
				return
			}
		}
	}()
}

func sweepExpiredResponsesReplayStates() {
	now := time.Now()
	removed := 0
	responsesReplayStore.Range(func(key, value interface{}) bool {
		entry, ok := value.(*responsesReplayStateEntry)
		if !ok || entry == nil {
			// 使用 CompareAndDelete 确保只删除我们检查过的这个 entry
			if responsesReplayStore.CompareAndDelete(key, value) {
				responsesReplayStoreStats.entries.Add(-1)
				removed++
			}
			return true
		}
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			// 使用 CompareAndDelete 防止误删并发写入的新 entry
			if responsesReplayStore.CompareAndDelete(key, entry) {
				responsesReplayStoreStats.entries.Add(-1)
				responsesReplayStoreStats.totalSize.Add(-int64(entry.size))
				removed++
			}
		}
		return true
	})
	if removed > 0 {
		log.Debugf("HTTP replay store sweep: removed %d expired entries, current entries=%d, size=%d",
			removed, responsesReplayStoreStats.entries.Load(), responsesReplayStoreStats.totalSize.Load())
	}
}

func responsesReplayStateKey(apiKeyID, groupID int, requestModel, responseID string) string {
	requestModel = strings.TrimSpace(requestModel)
	responseID = strings.TrimSpace(responseID)
	if requestModel == "" || responseID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(responseID))
	hashStr := hex.EncodeToString(hash[:])[:32]
	return fmt.Sprintf("%d:%d:%s:%s", apiKeyID, groupID, requestModel, hashStr)
}

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
		// 使用 CompareAndDelete 确保只删除我们检查过的这个 entry
		if responsesReplayStore.CompareAndDelete(key, v) {
			responsesReplayStoreStats.entries.Add(-1)
		}
		return nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		// 使用 CompareAndDelete 防止误删并发写入的新 entry
		if responsesReplayStore.CompareAndDelete(key, entry) {
			responsesReplayStoreStats.entries.Add(-1)
			responsesReplayStoreStats.totalSize.Add(-int64(entry.size))
		}
		return nil
	}

	return cloneWSConversationState(entry.state)
}

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

	estimatedSize := estimateStateSize(cloned)
	newEntry := &responsesReplayStateEntry{
		state:     cloned,
		expiresAt: time.Now().Add(ttl),
		size:      estimatedSize,
	}

	// 使用 Swap 保证统计一致性：先尝试存入，再根据旧值调整统计
	old, loaded := responsesReplayStore.Swap(key, newEntry)
	if loaded {
		// 更新已有 key：调整 size 差值，entries 不变
		if oldEntry, ok := old.(*responsesReplayStateEntry); ok && oldEntry != nil {
			responsesReplayStoreStats.totalSize.Add(int64(estimatedSize) - int64(oldEntry.size))
			// 替换后也需检查容量：新 entry 可能远大于旧 entry
			if int64(estimatedSize) > int64(oldEntry.size) &&
				responsesReplayStoreStats.totalSize.Load() > responsesReplayStoreMaxSize {
				// 超出容量，回滚为旧 entry
				if responsesReplayStore.CompareAndSwap(key, newEntry, oldEntry) {
					responsesReplayStoreStats.totalSize.Add(int64(oldEntry.size) - int64(estimatedSize))
				}
				log.Warnf("HTTP replay store size limit reached after replacement (size=%d), rolling back",
					responsesReplayStoreStats.totalSize.Load())
			}
		} else {
			responsesReplayStoreStats.totalSize.Add(int64(estimatedSize))
		}
	} else {
		// 新 key：增加 entries，检查容量，超限则回滚
		currentEntries := responsesReplayStoreStats.entries.Add(1)
		responsesReplayStoreStats.totalSize.Add(int64(estimatedSize))

		if currentEntries > responsesReplayStoreMaxEntries ||
			responsesReplayStoreStats.totalSize.Load() > responsesReplayStoreMaxSize {
			// 超出容量，使用 CompareAndDelete 回滚（只删除我们刚写入的 newEntry）
			if responsesReplayStore.CompareAndDelete(key, newEntry) {
				responsesReplayStoreStats.entries.Add(-1)
				responsesReplayStoreStats.totalSize.Add(-int64(estimatedSize))
			}
			log.Warnf("HTTP replay store capacity limit reached (entries=%d, size=%d), skipping save",
				currentEntries-1, responsesReplayStoreStats.totalSize.Load())
		}
	}
}

func estimateStateSize(state *wsConversationState) int {
	if state == nil {
		return 0
	}
	size := 256
	size += len(state.DownstreamSessionID) + len(state.RequestModel) + len(state.LastResponseID)
	size += len(state.ReplayWindowItems)
	for _, msg := range state.Transcript {
		size += estimateMessageSize(msg)
	}
	for _, alias := range state.ReplayAliases {
		size += len(alias) + 16
	}
	return size
}

func estimateMessageSize(msg transformerModel.Message) int {
	size := 128
	if msg.Content.Content != nil {
		size += len(*msg.Content.Content)
	}
	for _, part := range msg.Content.MultipleContent {
		size += 64
		if part.Text != nil {
			size += len(*part.Text)
		}
		if part.ImageURL != nil {
			size += len(part.ImageURL.URL)
		}
	}
	for _, tc := range msg.ToolCalls {
		size += len(tc.ID) + len(tc.Function.Name) + len(tc.Function.Arguments) + 32
	}
	if msg.ToolCallID != nil {
		size += len(*msg.ToolCallID)
	}
	if msg.ReasoningContent != nil {
		size += len(*msg.ReasoningContent)
	}
	for _, img := range msg.Images {
		size += 64
		if img.Text != nil {
			size += len(*img.Text)
		}
		if img.ImageURL != nil {
			size += len(img.ImageURL.URL)
		}
	}
	return size
}

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

func resetResponsesReplayStore() {
	responsesReplayStore = sync.Map{}
	responsesReplayStoreStats.entries.Store(0)
	responsesReplayStoreStats.totalSize.Store(0)
}
