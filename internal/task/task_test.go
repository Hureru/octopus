package task

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func TestSchedulerStartsTasksRegisteredBeforeAndAfterStart(t *testing.T) {
	resetSchedulerForTest(t)
	before := make(chan struct{}, 1)
	Register("before-start", 5*time.Millisecond, false, func() {
		select {
		case before <- struct{}{}:
		default:
		}
	})
	startScheduler()
	waitForTaskSignal(t, before, "task registered before scheduler start")

	after := make(chan struct{}, 1)
	Register("after-start", 5*time.Millisecond, false, func() {
		select {
		case after <- struct{}{}:
		default:
		}
	})
	waitForTaskSignal(t, after, "task registered after scheduler start")
}

func TestSchedulerCanDisableAndReenableTask(t *testing.T) {
	resetSchedulerForTest(t)
	var runs atomic.Int64
	signal := make(chan struct{}, 8)
	Register("toggle", 5*time.Millisecond, false, func() {
		runs.Add(1)
		select {
		case signal <- struct{}{}:
		default:
		}
	})
	startScheduler()
	waitForTaskSignal(t, signal, "initial task run")

	Update("toggle", 0)
	time.Sleep(15 * time.Millisecond)
	drainTaskSignals(signal)
	baseline := runs.Load()
	time.Sleep(20 * time.Millisecond)
	if got := runs.Load(); got != baseline {
		t.Fatalf("disabled task kept running: before=%d after=%d", baseline, got)
	}

	Update("toggle", 5*time.Millisecond)
	waitForTaskSignal(t, signal, "reenabled task run")
}

func TestSchedulerStartsOneLoopDuringConcurrentRegisterAndStart(t *testing.T) {
	resetSchedulerForTest(t)
	var startupRuns atomic.Int64
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		Register("race", time.Hour, true, func() {
			startupRuns.Add(1)
		})
	}()
	go func() {
		defer wait.Done()
		startScheduler()
	}()
	wait.Wait()

	deadline := time.Now().Add(time.Second)
	for startupRuns.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := startupRuns.Load(); got != 1 {
		t.Fatalf("concurrent start created %d startup executions, want 1", got)
	}
}

func TestSettingIntervalSpecsMapStatsToRegisteredTask(t *testing.T) {
	var statsSpec *settingIntervalSpec
	for index := range settingIntervalSpecs {
		if settingIntervalSpecs[index].key == model.SettingKeyStatsSaveInterval {
			statsSpec = &settingIntervalSpecs[index]
			break
		}
	}
	if statsSpec == nil {
		t.Fatal("stats interval spec is missing")
	}
	if statsSpec.taskName != TaskStatsSave {
		t.Fatalf("stats task name = %q, want %q", statsSpec.taskName, TaskStatsSave)
	}
}

func resetSchedulerForTest(t *testing.T) {
	t.Helper()
	tasksMu.Lock()
	entries := make([]*taskEntry, 0, len(tasks))
	for _, entry := range tasks {
		entries = append(entries, entry)
	}
	tasks = make(map[string]*taskEntry)
	schedulerStarted.Store(false)
	tasksMu.Unlock()

	for _, entry := range entries {
		entry.stopOnce.Do(func() {
			close(entry.stopCh)
		})
		if entry.loopStarted.Load() {
			select {
			case <-entry.doneCh:
			case <-time.After(time.Second):
				t.Fatalf("task loop %s did not stop", entry.name)
			}
		}
	}
	t.Cleanup(func() {
		tasksMu.RLock()
		current := make([]*taskEntry, 0, len(tasks))
		for _, entry := range tasks {
			current = append(current, entry)
		}
		tasksMu.RUnlock()
		for _, entry := range current {
			entry.stopOnce.Do(func() {
				close(entry.stopCh)
			})
		}
	})
}

func waitForTaskSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func drainTaskSignals(signal <-chan struct{}) {
	for {
		select {
		case <-signal:
		default:
			return
		}
	}
}
