package outlierwindow

import (
	"testing"
	"time"
)

func resetConfig() { Configure(defaultConfig) }

// report 连续上报 n 条同结果样本，时间从 base 起每条间隔 1s。
func report(channelID int, success bool, n int, base time.Time) time.Time {
	t := base
	for i := 0; i < n; i++ {
		Report(channelID, success, 0, t)
		t = t.Add(time.Second)
	}
	return t
}

func TestGate1_InsufficientSamples(t *testing.T) {
	resetConfig()
	const ch = 1001
	Clear(ch)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	report(ch, false, 7, base) // 7 < MinSamples(8)

	st := Evaluate(ch, base.Add(time.Minute))
	if st.Samples != 7 {
		t.Fatalf("Samples = %d, want 7", st.Samples)
	}
	if st.Candidate {
		t.Fatal("样本不足应 PASS（Candidate=false）")
	}
}

func TestGate1_DilutedBySuccess(t *testing.T) {
	resetConfig()
	const ch = 1002
	Clear(ch)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// 10 失败 + 10 成功，失败率 0.5 < 0.85
	t2 := report(ch, false, 10, base)
	report(ch, true, 10, t2)

	st := Evaluate(ch, base.Add(time.Minute))
	if st.Samples != 20 {
		t.Fatalf("Samples = %d, want 20", st.Samples)
	}
	if st.FailureRate >= 0.85 {
		t.Fatalf("FailureRate = %.2f, 不应达标", st.FailureRate)
	}
	if st.Candidate {
		t.Fatal("有成功稀释应 PASS")
	}
}

func TestGate1_ConsecutiveFailures(t *testing.T) {
	resetConfig()
	const ch = 1003
	Clear(ch)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	report(ch, false, 12, base) // 连续 12 次失败

	st := Evaluate(ch, base.Add(time.Minute))
	if st.ConsecutiveFails != 12 {
		t.Fatalf("ConsecutiveFails = %d, want 12", st.ConsecutiveFails)
	}
	if st.FailureRate != 1.0 {
		t.Fatalf("FailureRate = %.2f, want 1.0", st.FailureRate)
	}
	if !st.Candidate {
		t.Fatal("连续失败达标应成为候选")
	}
}

func TestGate1_NoSuccessTriggersCandidate(t *testing.T) {
	resetConfig()
	const ch = 1004
	Clear(ch)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	report(ch, false, 9, base) // 9 失败：consecutive 9 < 10，但窗口内无成功

	st := Evaluate(ch, base.Add(time.Minute))
	if st.ConsecutiveFails != 9 {
		t.Fatalf("ConsecutiveFails = %d, want 9", st.ConsecutiveFails)
	}
	if !st.LastSuccessAt.IsZero() {
		t.Fatal("不应有成功记录")
	}
	if !st.Candidate {
		t.Fatal("窗口内无成功应成为候选（noSuccess 分支）")
	}
}

func TestGate1_RecoveringNotCandidate(t *testing.T) {
	resetConfig()
	const ch = 1005
	Clear(ch)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// 18 失败后 2 次成功：失败率 0.9 达标，但最新是成功（consecutive=0、有近成功）
	t2 := report(ch, false, 18, base)
	report(ch, true, 2, t2)

	st := Evaluate(ch, base.Add(time.Minute))
	if st.FailureRate < 0.85 {
		t.Fatalf("FailureRate = %.2f, 应达标", st.FailureRate)
	}
	if st.ConsecutiveFails != 0 {
		t.Fatalf("ConsecutiveFails = %d, want 0", st.ConsecutiveFails)
	}
	if st.Candidate {
		t.Fatal("正在恢复（最新成功）不应退役")
	}
}

func TestTimeExpiry(t *testing.T) {
	resetConfig()
	const ch = 1006
	Clear(ch)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	report(ch, false, 12, base) // 全部发生在 base 附近

	// 在 base + 20min 评估，TimeWindow=10min → 全部过期
	st := Evaluate(ch, base.Add(20*time.Minute))
	if st.Samples != 0 {
		t.Fatalf("Samples = %d, want 0（应全部过期）", st.Samples)
	}
	if st.Candidate {
		t.Fatal("样本全过期应 PASS")
	}
}

func TestRingWraparound(t *testing.T) {
	resetConfig()
	const ch = 1007
	Clear(ch)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// 先 5 次成功，再 25 次失败；物理 cap=20，成功会被覆盖出窗
	t2 := report(ch, true, 5, base)
	report(ch, false, 25, t2)

	st := Evaluate(ch, base.Add(time.Minute))
	if st.Samples != physicalCap {
		t.Fatalf("Samples = %d, want %d", st.Samples, physicalCap)
	}
	if st.Failures != physicalCap {
		t.Fatalf("Failures = %d, want %d（成功应被环形覆盖）", st.Failures, physicalCap)
	}
	if !st.Candidate {
		t.Fatal("满窗全失败应成为候选")
	}
}

func TestClear(t *testing.T) {
	resetConfig()
	const ch = 1008
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	report(ch, false, 12, base)
	Clear(ch)

	st := Evaluate(ch, base.Add(time.Minute))
	if st.Samples != 0 {
		t.Fatalf("Clear 后 Samples = %d, want 0", st.Samples)
	}
}

func TestReap(t *testing.T) {
	resetConfig()
	const chOld = 1009
	const chFresh = 1010
	Clear(chOld)
	Clear(chFresh)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	report(chOld, false, 3, base)                  // lastSeen ≈ base
	report(chFresh, false, 3, base.Add(time.Hour)) // lastSeen ≈ base+1h

	// 在 base+1h 回收 ttl=30min：chOld 过老被回收，chFresh 保留
	reaped := Reap(base.Add(time.Hour), 30*time.Minute)
	if reaped < 1 {
		t.Fatalf("Reap = %d, 至少应回收 chOld", reaped)
	}
	if _, ok := store.Load(chOld); ok {
		t.Fatal("chOld 应被回收")
	}
	if _, ok := store.Load(chFresh); !ok {
		t.Fatal("chFresh 不应被回收")
	}
}

func TestConfigureClamp(t *testing.T) {
	Configure(Config{Capacity: 999, TimeWindow: 0, MinSamples: 0, FailRate: 2, ConsecFails: 0})
	c := currentConfig()
	if c.Capacity != physicalCap {
		t.Fatalf("Capacity = %d, 应封顶到 %d", c.Capacity, physicalCap)
	}
	if c.TimeWindow != defaultConfig.TimeWindow {
		t.Fatal("非法 TimeWindow 应回退默认")
	}
	if c.FailRate != defaultConfig.FailRate {
		t.Fatal("非法 FailRate 应回退默认")
	}
	resetConfig()
}
