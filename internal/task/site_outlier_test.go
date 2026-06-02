package task

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/grouphealth"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// fakeProber 实现 channelProber，按 channelID 返回预设探活结果，并记录探活调用。
type fakeProber struct {
	byChannel map[int]grouphealth.ProbeResult
	fallback  grouphealth.ProbeResult
	calls     []int
}

func (f *fakeProber) RunCandidate(ctx context.Context, channel model.Channel, usedKey model.ChannelKey, modelName string) grouphealth.ProbeResult {
	f.calls = append(f.calls, channel.ID)
	if r, ok := f.byChannel[channel.ID]; ok {
		return r
	}
	return f.fallback
}

func probeFail() grouphealth.ProbeResult {
	return grouphealth.ProbeResult{Success: false, HTTPStatus: 500, ErrorMessage: "upstream error: 500"}
}

func probeOK() grouphealth.ProbeResult {
	return grouphealth.ProbeResult{Success: true, HTTPStatus: 200}
}

func testOutlierConfig() outlierConfig {
	return outlierConfig{
		window: outlierwindow.Config{
			Capacity:    20,
			TimeWindow:  10 * time.Minute,
			MinSamples:  8,
			FailRate:    0.85,
			ConsecFails: 10,
		},
		reapTTL:           30 * time.Minute,
		recoverStreak:     2,
		cfRecoverCooldown: 30 * time.Minute,
	}
}

func setupOutlierTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	dbPath := filepath.Join(t.TempDir(), "octopus-outlier-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	return context.Background()
}

// createSiteAccountFixture 建 site + account（满足 SiteChannelBinding 的外键），返回 (siteID, accountID)。
func createSiteAccountFixture(t *testing.T, ctx context.Context) (int, int) {
	t.Helper()
	site := &model.Site{Name: "Outlier Site", Platform: model.SitePlatformNewAPI, BaseURL: "https://example.com", Enabled: true}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	account := &model.SiteAccount{SiteID: site.ID, Name: "primary", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "tok", Enabled: true}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	return site.ID, account.ID
}

// createProjectedChannel 建一个投影渠道（含可用 key + model）及其 binding，返回 channelID。
// withKey=false 时不带 key（模拟无法探活的渠道）。建好后清空其滚动窗口避免跨用例串扰。
func createProjectedChannel(t *testing.T, ctx context.Context, siteID, accountID int, name string, enabled, withKey bool) int {
	t.Helper()
	ch := &model.Channel{
		Name:     name,
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  enabled,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
		Model:    "gpt-4o",
	}
	if withKey {
		ch.Keys = []model.ChannelKey{{Enabled: true, ChannelKey: "sk-" + name}}
	}
	if err := op.ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("ChannelCreate %s failed: %v", name, err)
	}
	binding := model.SiteChannelBinding{
		SiteID:        siteID,
		SiteAccountID: accountID,
		GroupKey:      name,
		ChannelID:     ch.ID,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatalf("create binding for %s failed: %v", name, err)
	}
	outlierwindow.Clear(ch.ID)
	return ch.ID
}

func reportN(channelID int, success bool, n int, base time.Time) {
	for i := 0; i < n; i++ {
		outlierwindow.Report(channelID, success, 0, base.Add(time.Duration(i)*time.Second))
	}
}

func mustChannelEnabled(t *testing.T, ctx context.Context, channelID int) bool {
	t.Helper()
	ch, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet %d failed: %v", channelID, err)
	}
	return ch.Enabled
}

// 用例1：账号下所有渠道高失败 + 探活失败 → 全部禁用并写入站点级退役记录，且每轮只探活一次。
func TestRunOutlierRetire_SiteOutageDisablesAll(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	cfg := testOutlierConfig()
	outlierwindow.Configure(cfg.window)
	now := time.Now()
	base := now.Add(-time.Minute)

	siteID, acc := createSiteAccountFixture(t, ctx)
	ids := []int{
		createProjectedChannel(t, ctx, siteID, acc, "ch-a", true, true),
		createProjectedChannel(t, ctx, siteID, acc, "ch-b", true, true),
		createProjectedChannel(t, ctx, siteID, acc, "ch-c", true, true),
	}
	for _, id := range ids {
		reportN(id, false, 12, base)
	}

	prober := &fakeProber{fallback: probeFail()}
	runOutlierRetire(ctx, prober, cfg, now)

	for _, id := range ids {
		if mustChannelEnabled(t, ctx, id) {
			t.Errorf("channel %d expected disabled after site outage", id)
		}
		state, err := op.SiteChannelOutlierGet(id, ctx)
		if err != nil {
			t.Fatalf("expected retired state for channel %d: %v", id, err)
		}
		if state.Status != model.OutlierStatusRetired {
			t.Errorf("channel %d status = %q, want retired", id, state.Status)
		}
		if !strings.Contains(state.Reason, "site-level") {
			t.Errorf("channel %d reason = %q, want site-level marker", id, state.Reason)
		}
		if state.Snapshot.SiblingHealthy != 0 {
			t.Errorf("channel %d snapshot SiblingHealthy = %d, want 0", id, state.Snapshot.SiblingHealthy)
		}
	}
	if len(prober.calls) != 1 {
		t.Errorf("expected exactly one probe for site outage (dedup), got %d calls: %v", len(prober.calls), prober.calls)
	}
}

// 用例2：账号疑似整站故障但探活成功 → 不禁用，并清空各渠道窗口放行。
func TestRunOutlierRetire_SiteOutageProbeSuccessSkips(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	cfg := testOutlierConfig()
	outlierwindow.Configure(cfg.window)
	now := time.Now()
	base := now.Add(-time.Minute)

	siteID, acc := createSiteAccountFixture(t, ctx)
	ids := []int{
		createProjectedChannel(t, ctx, siteID, acc, "ok-a", true, true),
		createProjectedChannel(t, ctx, siteID, acc, "ok-b", true, true),
	}
	for _, id := range ids {
		reportN(id, false, 12, base)
	}

	prober := &fakeProber{fallback: probeOK()}
	runOutlierRetire(ctx, prober, cfg, now)

	for _, id := range ids {
		if !mustChannelEnabled(t, ctx, id) {
			t.Errorf("channel %d expected to stay enabled when probe succeeds", id)
		}
		if _, err := op.SiteChannelOutlierGet(id, ctx); err == nil {
			t.Errorf("channel %d should not have a retired record", id)
		}
		if got := outlierwindow.Evaluate(id, now).Samples; got != 0 {
			t.Errorf("channel %d window expected cleared, got %d samples", id, got)
		}
	}
}

// 用例3：单渠道高失败但兄弟健康 → 走单渠道退役，不触发整站禁用。
func TestRunOutlierRetire_SingleChannelRetireNotSiteOutage(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	cfg := testOutlierConfig()
	outlierwindow.Configure(cfg.window)
	now := time.Now()
	base := now.Add(-time.Minute)

	siteID, acc := createSiteAccountFixture(t, ctx)
	bad := createProjectedChannel(t, ctx, siteID, acc, "bad", true, true)
	good1 := createProjectedChannel(t, ctx, siteID, acc, "good-1", true, true)
	good2 := createProjectedChannel(t, ctx, siteID, acc, "good-2", true, true)
	reportN(bad, false, 12, base)
	reportN(good1, true, 12, base)
	reportN(good2, true, 12, base)

	prober := &fakeProber{fallback: probeFail()}
	runOutlierRetire(ctx, prober, cfg, now)

	if mustChannelEnabled(t, ctx, bad) {
		t.Errorf("bad channel %d expected disabled", bad)
	}
	state, err := op.SiteChannelOutlierGet(bad, ctx)
	if err != nil {
		t.Fatalf("expected retired state for bad channel: %v", err)
	}
	if strings.Contains(state.Reason, "site-level") {
		t.Errorf("single-channel retire reason should not be site-level, got %q", state.Reason)
	}
	if state.Snapshot.SiblingHealthy != 2 {
		t.Errorf("single-channel snapshot SiblingHealthy = %d, want 2", state.Snapshot.SiblingHealthy)
	}
	for _, id := range []int{good1, good2} {
		if !mustChannelEnabled(t, ctx, id) {
			t.Errorf("healthy sibling %d expected to stay enabled", id)
		}
		if _, err := op.SiteChannelOutlierGet(id, ctx); err == nil {
			t.Errorf("healthy sibling %d should not be retired", id)
		}
	}
}

// 用例4：整站禁用后，探活连续成功达阈值 → 阶段0 自动重启用并清除退役记录。
func TestRunOutlierRetire_SiteOutageRecovers(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	cfg := testOutlierConfig()
	outlierwindow.Configure(cfg.window)
	now := time.Now()

	siteID, acc := createSiteAccountFixture(t, ctx)
	ids := []int{
		createProjectedChannel(t, ctx, siteID, acc, "rec-a", true, true),
		createProjectedChannel(t, ctx, siteID, acc, "rec-b", true, true),
	}
	// 构造已退役初态：禁用 + 写 retired 记录。
	snap := model.OutlierSnapshot{SiblingHealthy: 0, SiblingTotal: len(ids) - 1}
	for _, id := range ids {
		if err := op.SiteChannelOutlierRetire(id, acc, "passive outlier: site-level outage", false, snap, now, ctx); err != nil {
			t.Fatalf("seed retire channel %d failed: %v", id, err)
		}
		if err := op.ChannelEnabledManaged(id, false, ctx); err != nil {
			t.Fatalf("seed disable channel %d failed: %v", id, err)
		}
	}

	prober := &fakeProber{fallback: probeOK()}
	// recoverStreak=2：第一轮未达阈值，第二轮恢复。
	runOutlierRetire(ctx, prober, cfg, now)
	for _, id := range ids {
		if mustChannelEnabled(t, ctx, id) {
			t.Fatalf("channel %d should still be disabled after first probe (streak<2)", id)
		}
	}
	runOutlierRetire(ctx, prober, cfg, now.Add(time.Minute))
	for _, id := range ids {
		if !mustChannelEnabled(t, ctx, id) {
			t.Errorf("channel %d expected re-enabled after recover streak reached", id)
		}
		if _, err := op.SiteChannelOutlierGet(id, ctx); err == nil {
			t.Errorf("channel %d retired record should be cleared after recovery", id)
		}
	}
}

// 用例5：单渠道账号整站故障 → 禁用唯一渠道（突破“最后一个 enabled 渠道”守护）。
func TestRunOutlierRetire_SiteOutageBreaksLastChannelGuard(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	cfg := testOutlierConfig()
	outlierwindow.Configure(cfg.window)
	now := time.Now()
	base := now.Add(-time.Minute)

	siteID, acc := createSiteAccountFixture(t, ctx)
	only := createProjectedChannel(t, ctx, siteID, acc, "solo", true, true)
	reportN(only, false, 12, base)

	prober := &fakeProber{fallback: probeFail()}
	runOutlierRetire(ctx, prober, cfg, now)

	if mustChannelEnabled(t, ctx, only) {
		t.Errorf("sole channel %d expected disabled on site outage despite last-channel guard", only)
	}
	if _, err := op.SiteChannelOutlierGet(only, ctx); err != nil {
		t.Errorf("sole channel %d expected retired record: %v", only, err)
	}
}

// 用例6：账号下无可探活渠道（缺 key）→ 保守不禁用。
func TestRunOutlierRetire_SiteOutageNoProbeableChannelSkips(t *testing.T) {
	ctx := setupOutlierTestDB(t)
	cfg := testOutlierConfig()
	outlierwindow.Configure(cfg.window)
	now := time.Now()
	base := now.Add(-time.Minute)

	siteID, acc := createSiteAccountFixture(t, ctx)
	ids := []int{
		createProjectedChannel(t, ctx, siteID, acc, "nokey-a", true, false),
		createProjectedChannel(t, ctx, siteID, acc, "nokey-b", true, false),
	}
	for _, id := range ids {
		reportN(id, false, 12, base)
	}

	prober := &fakeProber{fallback: probeFail()}
	runOutlierRetire(ctx, prober, cfg, now)

	for _, id := range ids {
		if !mustChannelEnabled(t, ctx, id) {
			t.Errorf("channel %d without probeable key should stay enabled", id)
		}
		if _, err := op.SiteChannelOutlierGet(id, ctx); err == nil {
			t.Errorf("channel %d should not be retired without probe confirmation", id)
		}
	}
	if len(prober.calls) != 0 {
		t.Errorf("expected no probe when no channel has key/model, got %v", prober.calls)
	}
}
