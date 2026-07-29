package task

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
)

type taskEntry struct {
	name        string
	fn          func()
	runOnStart  bool
	ticker      *time.Ticker
	stopCh      chan struct{}
	doneCh      chan struct{}
	updateCh    chan struct{}
	stopOnce    sync.Once
	interval    atomic.Int64
	loopStarted atomic.Bool
	running     atomic.Bool
}

var (
	tasks            = make(map[string]*taskEntry)
	tasksMu          sync.RWMutex
	schedulerStarted atomic.Bool
)

// Register 注册一个定时任务
// runOnStart: 是否在启动时立即执行一次
func Register(name string, interval time.Duration, runOnStart bool, fn func()) {
	tasksMu.Lock()
	if _, exists := tasks[name]; exists {
		tasksMu.Unlock()
		log.Warnf("task %s already registered, skipping", name)
		return
	}

	entry := &taskEntry{
		name:       name,
		fn:         fn,
		runOnStart: runOnStart,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		updateCh:   make(chan struct{}, 1),
	}
	entry.interval.Store(int64(max(interval, 0)))
	tasks[name] = entry
	tasksMu.Unlock()

	if schedulerStarted.Load() {
		startTaskLoop(entry)
	}
	log.Debugf("task %s registered with interval %v, runOnStart: %v", name, interval, runOnStart)
}

// Update 更新任务的执行间隔
// 当 interval 为 0 时暂停任务；后续设置为正数会复用原函数并重新启动。
func Update(name string, interval time.Duration) {
	tasksMu.RLock()
	entry, exists := tasks[name]
	tasksMu.RUnlock()
	if !exists {
		log.Warnf("task %s not found", name)
		return
	}

	entry.interval.Store(int64(max(interval, 0)))
	select {
	case entry.updateCh <- struct{}{}:
		log.Infof("task %s interval updated to %v", name, interval)
	default:
		// A pending notification will read the latest atomic interval.
	}
}

// RUN 启动所有注册的任务
func RUN() {
	startScheduler()

	// 阻塞主协程
	select {}
}

func startScheduler() {
	schedulerStarted.Store(true)
	tasksMu.RLock()
	for _, entry := range tasks {
		startTaskLoop(entry)
	}
	tasksMu.RUnlock()
}

func startTaskLoop(entry *taskEntry) {
	if entry == nil || !entry.loopStarted.CompareAndSwap(false, true) {
		return
	}
	safe.Go("task-loop:"+entry.name, func() {
		runTask(entry)
	})
}

func runTask(entry *taskEntry) {
	defer close(entry.doneCh)

	// 根据配置决定是否在启动时立即执行
	if entry.runOnStart && time.Duration(entry.interval.Load()) > 0 {
		triggerTask(entry, "startup")
	}

	var tickerC <-chan time.Time
	resetTicker := func() {
		if entry.ticker != nil {
			entry.ticker.Stop()
			entry.ticker = nil
		}
		interval := time.Duration(entry.interval.Load())
		if interval > 0 {
			entry.ticker = time.NewTicker(interval)
			tickerC = entry.ticker.C
		} else {
			tickerC = nil
		}
	}
	resetTicker()
	defer func() {
		if entry.ticker != nil {
			entry.ticker.Stop()
		}
	}()

	for {
		select {
		case <-tickerC:
			triggerTask(entry, "ticker")
		case <-entry.updateCh:
			resetTicker()
		case <-entry.stopCh:
			return
		}
	}
}

func triggerTask(entry *taskEntry, trigger string) {
	if entry == nil {
		return
	}
	if !entry.running.CompareAndSwap(false, true) {
		log.Warnf("task %s skipped: previous run still in progress (trigger=%s)", entry.name, trigger)
		return
	}
	safe.Go("task-exec:"+entry.name+":"+trigger, func() {
		defer entry.running.Store(false)
		entry.fn()
	})
}
