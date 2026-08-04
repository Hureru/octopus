package shutdown

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type testLogger struct{}

func (testLogger) Infof(string, ...interface{})  {}
func (testLogger) Errorf(string, ...interface{}) {}
func (testLogger) Warnf(string, ...interface{})  {}
func (testLogger) Debugf(string, ...interface{}) {}

func TestRequestRunsShutdownHooksOnceInReverseOrder(t *testing.T) {
	Init(testLogger{})

	var mu sync.Mutex
	order := make([]string, 0, 2)
	register := func(name string) {
		Register(func() error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			return nil
		})
	}
	register("first")
	register("second")

	done := make(chan struct{})
	go func() {
		Listen()
		close(done)
	}()
	Request()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Listen did not return after Request")
	}

	Request()
	Shutdown()
	mu.Lock()
	defer mu.Unlock()
	if got := fmt.Sprint(order); got != "[second first]" {
		t.Fatalf("shutdown hook order = %s, want [second first]", got)
	}
}
