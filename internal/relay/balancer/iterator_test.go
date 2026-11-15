package balancer

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestIteratorOnChangeReportsRunningAndFailedAttempts(t *testing.T) {
	it := &Iterator{candidates: []model.GroupItem{{ChannelID: 1, ModelName: "m"}}, index: 0, stickyIdx: -1}
	var calls [][]model.ChannelAttempt
	it.OnChange = func(attempts []model.ChannelAttempt) { calls = append(calls, attempts) }

	span := it.StartAttempt(1, 10, "ch")
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0].Status != model.AttemptRunning {
		t.Fatalf("StartAttempt should report one running attempt, got %+v", calls)
	}

	span.End(model.AttemptFailed, 500, "boom")
	if len(calls) != 2 || len(calls[1]) != 1 || calls[1][0].Status != model.AttemptFailed || calls[1][0].Msg != "boom" {
		t.Fatalf("failed End should report the failed attempt without a running one, got %+v", calls)
	}

	// 回调拿到的是快照：改它不能影响迭代器内部的记录
	calls[1][0].Msg = "mutated"
	if it.Attempts()[0].Msg != "boom" {
		t.Fatalf("OnChange snapshot shares memory with iterator attempts")
	}

	span = it.StartAttempt(1, 10, "ch")
	if got := calls[len(calls)-1]; len(got) != 2 || got[0].Status != model.AttemptFailed || got[1].Status != model.AttemptRunning {
		t.Fatalf("retry should report failed + running, got %+v", got)
	}

	before := len(calls)
	span.End(model.AttemptSuccess, 200, "")
	if len(calls) != before {
		t.Fatalf("successful End must not report progress, got %d extra calls", len(calls)-before)
	}
	for _, a := range it.Attempts() {
		if a.Status == model.AttemptRunning {
			t.Fatalf("running status leaked into persisted attempts: %+v", it.Attempts())
		}
	}
}
