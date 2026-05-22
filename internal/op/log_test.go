package op

import (
	"errors"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func resetRelayLogStateForTest() {
	relayLogPendingLock.Lock()
	relayLogPending = make([]model.RelayLog, 0, relayLogBatchSize)
	relayLogPendingBytes = 0
	relayLogPendingLock.Unlock()

	relayLogRecentLock.Lock()
	relayLogRecent = make([]model.RelayLog, 0, relayLogRecentMaxSize)
	relayLogRecentLock.Unlock()

	relayLogDroppedTotal.Store(0)
	relayLogLastDropWarn.Store(0)
	for {
		select {
		case <-relayLogFlushSignal:
		default:
			return
		}
	}
}

func TestRelayLogAddQueuesWithoutDBWrite(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	resetRelayLogStateForTest()

	logCount := relayLogBatchSize + 5
	for i := 0; i < logCount; i++ {
		if err := RelayLogAdd(ctx, model.RelayLog{Time: time.Now().Unix(), RequestModelName: "gpt-4o-mini", Success: true}); err != nil {
			t.Fatalf("RelayLogAdd failed: %v", err)
		}
	}

	if got := RelayLogPendingLen(); got != logCount {
		t.Fatalf("expected pending logs to stay queued, got %d", got)
	}
	var dbCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.RelayLog{}).Count(&dbCount).Error; err != nil {
		t.Fatalf("count relay logs failed: %v", err)
	}
	if dbCount != 0 {
		t.Fatalf("RelayLogAdd wrote to DB synchronously, db rows=%d", dbCount)
	}
}

func TestRelayLogListDefaultsToLightFieldsAndNoContentKeyword(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	resetRelayLogStateForTest()

	rows := []model.RelayLog{
		{ID: 101, Time: 101, RequestModelName: "gpt-visible", RequestAPIKeyName: "key-a", ChannelId: 1, ChannelName: "primary", ActualModelName: "gpt-visible", RequestContent: "hidden-needle", ResponseContent: "hidden-response", Success: true},
		{ID: 102, Time: 102, RequestModelName: "claude", RequestAPIKeyName: "key-b", ChannelId: 1, ChannelName: "secondary", ActualModelName: "claude", Error: "visible failure", RequestContent: "plain", Success: false},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("create relay logs failed: %v", err)
	}

	result, err := RelayLogListWithFilter(ctx, RelayLogListFilter{Page: 1, PageSize: 10, WithTotal: true})
	if err != nil {
		t.Fatalf("RelayLogListWithFilter failed: %v", err)
	}
	if result.Total != 2 || len(result.Logs) != 2 {
		t.Fatalf("unexpected list result: %+v", result)
	}
	for _, item := range result.Logs {
		if item.RequestContent != "" || item.ResponseContent != "" {
			t.Fatalf("expected list to omit content fields by default, got %+v", item)
		}
	}

	contentResult, err := RelayLogListWithFilter(ctx, RelayLogListFilter{Keyword: "hidden-needle", Page: 1, PageSize: 10, WithTotal: true})
	if err != nil {
		t.Fatalf("RelayLogListWithFilter keyword failed: %v", err)
	}
	if contentResult.Total != 0 || len(contentResult.Logs) != 0 {
		t.Fatalf("default keyword unexpectedly searched content: %+v", contentResult)
	}

	contentResult, err = RelayLogListWithFilter(ctx, RelayLogListFilter{Keyword: "hidden-needle", KeywordScope: RelayLogKeywordScopeContent, StartTime: intPtr(0), EndTime: intPtr(200), Page: 1, PageSize: 10, WithTotal: true})
	if err != nil {
		t.Fatalf("RelayLogListWithFilter content keyword failed: %v", err)
	}
	if contentResult.Total != 1 || len(contentResult.Logs) != 1 || contentResult.Logs[0].ID != 101 {
		t.Fatalf("content keyword did not find expected row: %+v", contentResult)
	}
}

func intPtr(v int) *int { return &v }

func TestRelayLogListContainsKeywordRequiresMinLength(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	resetRelayLogStateForTest()

	_, err := RelayLogListWithFilter(ctx, RelayLogListFilter{Keyword: "ab", KeywordMode: RelayLogKeywordModeContains, Page: 1, PageSize: 10})
	if !errors.Is(err, ErrRelayLogContainsKeywordTooShort) {
		t.Fatalf("expected ErrRelayLogContainsKeywordTooShort, got %v", err)
	}
}

func TestRelayLogListPrefixKeywordMatchesModelPrefix(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	resetRelayLogStateForTest()

	rows := []model.RelayLog{
		{ID: 401, Time: 401, RequestModelName: "gpt-4o-mini", ChannelName: "primary", Success: true},
		{ID: 402, Time: 402, RequestModelName: "claude-3", ChannelName: "secondary", Success: false},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("create relay logs failed: %v", err)
	}

	result, err := RelayLogListWithFilter(ctx, RelayLogListFilter{Keyword: "gpt", Page: 1, PageSize: 10, WithTotal: true})
	if err != nil {
		t.Fatalf("prefix search failed: %v", err)
	}
	if result.Total != 1 || len(result.Logs) != 1 || result.Logs[0].ID != 401 {
		t.Fatalf("prefix search returned unexpected rows: %+v", result)
	}
	if result.SearchMode != "fast" {
		t.Fatalf("expected SearchMode=fast, got %q", result.SearchMode)
	}
}

func TestRelayLogListCursorReturnsNextCursorWithoutTotal(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	resetRelayLogStateForTest()

	rows := []model.RelayLog{
		{ID: 201, Time: 201, RequestModelName: "a", Success: true},
		{ID: 202, Time: 202, RequestModelName: "b", Success: true},
		{ID: 203, Time: 203, RequestModelName: "c", Success: true},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("create relay logs failed: %v", err)
	}

	first, err := RelayLogListWithFilter(ctx, RelayLogListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("RelayLogListWithFilter cursor failed: %v", err)
	}
	if first.Total != 0 || !first.HasMore || first.NextCursor == nil || len(first.Logs) != 2 || first.Logs[0].ID != 203 || first.Logs[1].ID != 202 {
		t.Fatalf("unexpected first cursor page: %+v", first)
	}
	second, err := RelayLogListWithFilter(ctx, RelayLogListFilter{Limit: 2, BeforeTime: &first.NextCursor.Time, BeforeID: &first.NextCursor.ID})
	if err != nil {
		t.Fatalf("RelayLogListWithFilter second cursor failed: %v", err)
	}
	if second.HasMore || second.NextCursor != nil || len(second.Logs) != 1 || second.Logs[0].ID != 201 {
		t.Fatalf("unexpected second cursor page: %+v", second)
	}
}

func TestRelayLogGetReturnsFullContent(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	resetRelayLogStateForTest()

	row := model.RelayLog{ID: 301, Time: 301, RequestModelName: "gpt", RequestContent: "full request", ResponseContent: "full response", Success: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create relay log failed: %v", err)
	}

	got, err := RelayLogGet(ctx, row.ID)
	if err != nil {
		t.Fatalf("RelayLogGet failed: %v", err)
	}
	if got.RequestContent != row.RequestContent || got.ResponseContent != row.ResponseContent {
		t.Fatalf("expected full content, got %+v", got)
	}
}

func TestRelayLogFlushPendingPersistsQueuedLogs(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	resetRelayLogStateForTest()

	for i := 0; i < 3; i++ {
		if err := RelayLogAdd(ctx, model.RelayLog{Time: int64(100 + i), RequestModelName: "model", Success: true}); err != nil {
			t.Fatalf("RelayLogAdd failed: %v", err)
		}
	}
	if err := RelayLogFlushPending(ctx); err != nil {
		t.Fatalf("RelayLogFlushPending failed: %v", err)
	}
	if got := RelayLogPendingLen(); got != 0 {
		t.Fatalf("expected pending queue to be empty, got %d", got)
	}
	var dbCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.RelayLog{}).Count(&dbCount).Error; err != nil {
		t.Fatalf("count relay logs failed: %v", err)
	}
	if dbCount != 3 {
		t.Fatalf("expected 3 persisted logs, got %d", dbCount)
	}
}
