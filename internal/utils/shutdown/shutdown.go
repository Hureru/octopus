package shutdown

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type logger interface {
	Infof(template string, args ...interface{})
	Errorf(template string, args ...interface{})
	Warnf(template string, args ...interface{})
	Debugf(template string, args ...interface{})
}

var ilog logger
var funcs []func() error
var stateMu sync.Mutex
var requestCh chan struct{}
var requestOnce *sync.Once
var shutdownOnce *sync.Once

func Init(log logger) {
	stateMu.Lock()
	defer stateMu.Unlock()
	ilog = log
	funcs = make([]func() error, 0)
	requestCh = make(chan struct{})
	requestOnce = &sync.Once{}
	shutdownOnce = &sync.Once{}
}

func Register(fn func() error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	funcs = append(funcs, fn)
}

func Listen() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(quit)
	ilog.Infof("Program started, press Ctrl+C to exit")

	stateMu.Lock()
	requested := requestCh
	stateMu.Unlock()
	select {
	case sig := <-quit:
		ilog.Warnf("Received exit signal: %v", sig)
	case <-requested:
		ilog.Warnf("Received application shutdown request")
	}
	Shutdown()
}

func Shutdown() {
	stateMu.Lock()
	once := shutdownOnce
	stateMu.Unlock()
	if once == nil {
		return
	}
	once.Do(func() {
		stateMu.Lock()
		hooks := append([]func() error(nil), funcs...)
		stateMu.Unlock()
		for i := len(hooks) - 1; i >= 0; i-- {
			if err := hooks[i](); err != nil {
				ilog.Errorf("Closing functions execution failed: %v", err)
			}
		}
		ilog.Infof("Shutdown completed successfully")
	})
}

// Request asks the goroutine blocked in Listen to run the normal shutdown path.
func Request() {
	stateMu.Lock()
	ch := requestCh
	once := requestOnce
	stateMu.Unlock()
	if ch == nil || once == nil {
		return
	}
	once.Do(func() { close(ch) })
}
