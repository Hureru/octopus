package task

import (
	"context"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/grouphealth"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/sitesync"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// outlierConfig 一轮评估使用的阈值，从 Setting 读取。
type outlierConfig struct {
	window            outlierwindow.Config
	reapTTL           time.Duration
	recoverStreak     int
	cfRecoverCooldown time.Duration
}

// channelProber 抽象探活能力，便于测试注入；*grouphealth.Prober 满足该接口。
type channelProber interface {
	RunCandidate(ctx context.Context, channel model.Channel, usedKey model.ChannelKey, modelName string) grouphealth.ProbeResult
}

// SiteOutlierRetireTask 被动离群退役（POR）控制面任务：
// 阶段0 恢复探活 → 阶段1 门1窗口评估 → 阶段2 同站佐证 → 阶段3 探活确认 → 软退役。
func SiteOutlierRetireTask() {
	enabled, err := op.SettingGetBool(model.SettingKeyOutlierRetireEnabled)
	if err != nil || !enabled {
		return // 总开关关闭：任务已注册，运行时打开即可生效，无需重启
	}

	startTime := time.Now()
	defer func() {
		log.Debugf("site outlier retire task finished, cost: %s", time.Since(startTime))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cfg := loadOutlierConfig()
	outlierwindow.Configure(cfg.window)
	runOutlierRetire(ctx, grouphealth.NewProber(), cfg, time.Now())
}

// runOutlierRetire 承载 POR 一轮评估的全部阶段，prober 通过接口注入以便测试。
func runOutlierRetire(ctx context.Context, prober channelProber, cfg outlierConfig, now time.Time) {
	// 阶段0：对已退役渠道做恢复探活
	recoverRetired(ctx, prober, cfg, now)

	// 拉取全部投影渠道绑定，按 siteAccountID 分组
	bindings, err := op.SiteChannelBindingListAll(ctx)
	if err != nil {
		log.Warnf("POR list bindings failed: %v", err)
		return
	}
	byAccount := make(map[int][]int, len(bindings))
	accountOf := make(map[int]int, len(bindings))
	for _, b := range bindings {
		byAccount[b.SiteAccountID] = append(byAccount[b.SiteAccountID], b.ChannelID)
		accountOf[b.ChannelID] = b.SiteAccountID
	}

	// 阶段1：门1 窗口聚合，筛出候选
	type candidate struct {
		channelID int
		stats     outlierwindow.WindowStats
	}
	var candidates []candidate
	for _, b := range bindings {
		ch, err := op.ChannelGet(b.ChannelID, ctx)
		if err != nil || !ch.Enabled {
			continue
		}
		st := outlierwindow.Evaluate(b.ChannelID, now)
		if st.Candidate {
			candidates = append(candidates, candidate{channelID: b.ChannelID, stats: st})
		}
	}

	// 阶段2 + 阶段3：同站佐证 → 探活确认 → 退役
	handledSiteOutage := make(map[int]bool)
	for _, cand := range candidates {
		accID := accountOf[cand.channelID]
		siblings := byAccount[accID]

		healthy, total := countHealthySiblings(ctx, siblings, cand.channelID, now)
		if healthy == 0 {
			// 门2：账号级整站故障 → 探活确认后禁用该账号下所有 enabled 投影渠道
			if handledSiteOutage[accID] {
				continue // 同账号多候选，整站禁用每轮只执行一次
			}
			handledSiteOutage[accID] = true
			handleSiteOutage(ctx, prober, accID, siblings, total, now)
			continue
		}
		if enabledSiblingCount(ctx, siblings) <= 1 {
			// 守护：永不退役某 siteAccount 最后一个 enabled 投影渠道
			continue
		}
		retireViaProbe(ctx, prober, cand.channelID, accID, cand.stats, healthy, total, now)
	}

	if reaped := outlierwindow.Reap(now, cfg.reapTTL); reaped > 0 {
		log.Debugf("POR reaped %d idle outlier windows", reaped)
	}
}

// recoverRetired 阶段0：对 retired 渠道探活，连续成功达阈值则恢复。
func recoverRetired(ctx context.Context, prober channelProber, cfg outlierConfig, now time.Time) {
	retired, err := op.SiteChannelOutlierListRetired(ctx)
	if err != nil {
		log.Warnf("POR list retired failed: %v", err)
		return
	}
	for _, st := range retired {
		ch, err := op.ChannelGet(st.ChannelID, ctx)
		if err != nil {
			// 渠道已被删除，清理残留退役记录
			_ = op.SiteChannelOutlierClear(st.ChannelID, ctx)
			continue
		}
		// CF 退役渠道按更长冷却才探活，减少无谓探测
		if st.CloudflareBlocked && st.RetiredAt != nil && now.Sub(*st.RetiredAt) < cfg.cfRecoverCooldown {
			continue
		}
		usedKey := ch.GetChannelKey()
		modelName := firstModelName(ch.Model)
		if usedKey.ID == 0 || strings.TrimSpace(usedKey.ChannelKey) == "" || modelName == "" {
			continue
		}
		res := prober.RunCandidate(ctx, *ch, usedKey, modelName)
		recovered, err := op.SiteChannelOutlierMarkProbe(st.ChannelID, res.Success, cfg.recoverStreak, now, ctx)
		if err != nil {
			log.Warnf("POR mark probe channel=%d failed: %v", st.ChannelID, err)
			continue
		}
		if recovered {
			if err := op.ChannelEnabledManaged(st.ChannelID, true, ctx); err != nil {
				log.Warnf("POR re-enable channel=%d failed: %v", st.ChannelID, err)
				continue
			}
			_ = op.SiteChannelOutlierClear(st.ChannelID, ctx)
			outlierwindow.Clear(st.ChannelID)
			log.Infof("POR recovered channel=%d (%s)", ch.ID, ch.Name)
		}
	}
}

// retireViaProbe 阶段3：探活确认，失败则软退役（含 CF 指纹识别）。
func retireViaProbe(ctx context.Context, prober channelProber, channelID, siteAccountID int, st outlierwindow.WindowStats, healthy, total int, now time.Time) {
	ch, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		return
	}
	usedKey := ch.GetChannelKey()
	modelName := firstModelName(ch.Model)
	if usedKey.ID == 0 || strings.TrimSpace(usedKey.ChannelKey) == "" || modelName == "" {
		return // 无可用 key/model 无法探活，保守不退役
	}

	res := prober.RunCandidate(ctx, *ch, usedKey, modelName)
	if res.Success {
		// 门3 探活成功 → 之前的窗口失败可能是瞬时事件，清窗放行
		outlierwindow.Clear(channelID)
		return
	}

	isCF := sitesync.IsCloudflareProtectionResponse(res.HTTPStatus, res.Header, []byte(res.ErrorMessage))
	reason := "passive outlier: window + probe failed"
	if isCF {
		reason = "passive outlier: cloudflare protection on probe"
	}
	snap := model.OutlierSnapshot{
		Samples:          st.Samples,
		Failures:         st.Failures,
		FailureRate:      st.FailureRate,
		ConsecutiveFails: st.ConsecutiveFails,
		LastSuccessAt:    st.LastSuccessAt,
		ProbeHTTPStatus:  res.HTTPStatus,
		ProbeError:       res.ErrorMessage,
		ProbeCloudflare:  isCF,
		SiblingHealthy:   healthy,
		SiblingTotal:     total,
	}
	if err := op.SiteChannelOutlierRetire(channelID, siteAccountID, reason, isCF, snap, now, ctx); err != nil {
		log.Warnf("POR retire channel=%d failed: %v", channelID, err)
		return
	}
	if err := op.ChannelEnabledManaged(channelID, false, ctx); err != nil {
		log.Warnf("POR disable channel=%d failed: %v", channelID, err)
		return
	}
	log.Warnf("POR retired channel=%d (%s) failRate=%.2f probeStatus=%d cf=%v",
		channelID, ch.Name, st.FailureRate, res.HTTPStatus, isCF)
}

// handleSiteOutage 账号级整站故障处理：探活确认后禁用该账号下所有 enabled 投影渠道。
// 与单渠道 retireViaProbe 的区别：
//   - 不受 enabledSiblingCount<=1 守护限制（整站挂了留最后一个坏渠道无意义，
//     恢复靠阶段0主动探活，无需保留 enabled 渠道）；
//   - 退役 reason/Snapshot 标记为站点级（SiblingHealthy=0），便于诊断。
func handleSiteOutage(ctx context.Context, prober channelProber, accountID int, siblings []int, total int, now time.Time) {
	// 探活确认（安全阀）：门1/门2 仅是统计证据，可能是瞬时全站抖动。
	// 对该账号下第一个可探活渠道发探针，成功则判定误判、清窗放行，不禁用。
	probed := false
	var probe grouphealth.ProbeResult
	for _, chID := range siblings {
		ch, err := op.ChannelGet(chID, ctx)
		if err != nil || !ch.Enabled {
			continue
		}
		usedKey := ch.GetChannelKey()
		modelName := firstModelName(ch.Model)
		if usedKey.ID == 0 || strings.TrimSpace(usedKey.ChannelKey) == "" || modelName == "" {
			continue
		}
		probe = prober.RunCandidate(ctx, *ch, usedKey, modelName)
		probed = true
		if probe.Success {
			for _, sib := range siblings {
				outlierwindow.Clear(sib)
			}
			log.Infof("POR site outage probe ok account=%d, skip disable", accountID)
			return
		}
		break // 门2 已证明所有兄弟高失败，再加主动探针失败即确认，不必探多个
	}
	if !probed {
		// 无任何可探活渠道（缺 key/model），保守不禁用
		log.Warnf("POR site outage account=%d has no probeable channel, skip", accountID)
		return
	}

	// 确认整站故障 → 禁用该账号下所有 enabled 投影渠道
	isCF := sitesync.IsCloudflareProtectionResponse(probe.HTTPStatus, probe.Header, []byte(probe.ErrorMessage))
	reason := "passive outlier: site-level outage (all channels failing + probe failed)"
	if isCF {
		reason = "passive outlier: site-level outage (cloudflare protection on probe)"
	}
	disabled := 0
	for _, chID := range siblings {
		select {
		case <-ctx.Done():
			return
		default:
		}
		ch, err := op.ChannelGet(chID, ctx)
		if err != nil || !ch.Enabled {
			continue
		}
		chStats := outlierwindow.Evaluate(chID, now)
		snap := model.OutlierSnapshot{
			Samples:          chStats.Samples,
			Failures:         chStats.Failures,
			FailureRate:      chStats.FailureRate,
			ConsecutiveFails: chStats.ConsecutiveFails,
			LastSuccessAt:    chStats.LastSuccessAt,
			ProbeHTTPStatus:  probe.HTTPStatus,
			ProbeError:       probe.ErrorMessage,
			ProbeCloudflare:  isCF,
			SiblingHealthy:   0,
			SiblingTotal:     total,
		}
		if err := op.SiteChannelOutlierRetire(chID, accountID, reason, isCF, snap, now, ctx); err != nil {
			log.Warnf("POR site outage retire channel=%d failed: %v", chID, err)
			continue
		}
		if err := op.ChannelEnabledManaged(chID, false, ctx); err != nil {
			log.Warnf("POR site outage disable channel=%d failed: %v", chID, err)
			continue
		}
		disabled++
	}
	log.Warnf("POR site-level outage account=%d disabled %d channels (cf=%v)", accountID, disabled, isCF)
}

// countHealthySiblings 统计兄弟渠道（排除自身）中健康的数量。
// 健康 = enabled 且门1未判候选（含样本不足，视为无负证据）。
func countHealthySiblings(ctx context.Context, siblings []int, self int, now time.Time) (healthy, total int) {
	for _, sib := range siblings {
		if sib == self {
			continue
		}
		ch, err := op.ChannelGet(sib, ctx)
		if err != nil || !ch.Enabled {
			continue
		}
		total++
		if !outlierwindow.Evaluate(sib, now).Candidate {
			healthy++
		}
	}
	return healthy, total
}

// enabledSiblingCount 统计某账号下当前 enabled 的投影渠道数（含候选自身）。
func enabledSiblingCount(ctx context.Context, siblings []int) int {
	count := 0
	for _, sib := range siblings {
		ch, err := op.ChannelGet(sib, ctx)
		if err == nil && ch.Enabled {
			count++
		}
	}
	return count
}

// firstModelName 从渠道的逗号分隔模型串取第一个非空模型名。
func firstModelName(models string) string {
	for _, m := range strings.Split(models, ",") {
		if s := strings.TrimSpace(m); s != "" {
			return s
		}
	}
	return ""
}

func loadOutlierConfig() outlierConfig {
	getInt := func(key model.SettingKey, def int) int {
		v, err := op.SettingGetInt(key)
		if err != nil || v <= 0 {
			return def
		}
		return v
	}
	capacity := getInt(model.SettingKeyOutlierWindowCapacity, 20)
	if capacity > outlierwindow.PhysicalCap() {
		log.Warnf("POR window capacity %d exceeds physical cap %d, clamped", capacity, outlierwindow.PhysicalCap())
		capacity = outlierwindow.PhysicalCap()
	}
	failPct := getInt(model.SettingKeyOutlierFailRatePct, 85)
	return outlierConfig{
		window: outlierwindow.Config{
			Capacity:    capacity,
			TimeWindow:  time.Duration(getInt(model.SettingKeyOutlierWindowMinutes, 10)) * time.Minute,
			MinSamples:  getInt(model.SettingKeyOutlierMinSamples, 8),
			FailRate:    float64(failPct) / 100.0,
			ConsecFails: getInt(model.SettingKeyOutlierConsecFails, 10),
		},
		reapTTL:           time.Duration(getInt(model.SettingKeyOutlierReapMinutes, 30)) * time.Minute,
		recoverStreak:     getInt(model.SettingKeyOutlierRecoverStreak, 2),
		cfRecoverCooldown: time.Duration(getInt(model.SettingKeyOutlierCFRecoverMinutes, 30)) * time.Minute,
	}
}
