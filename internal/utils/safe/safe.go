package safe

import (
	"fmt"
	"runtime/debug"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
)

// Go runs fn in a goroutine and guarantees panic recovery.
func Go(name string, fn func()) {
	go func() {
		Run(name, fn)
	}()
}

// Run executes fn and converts panics into logs instead of process exits.
func Run(name string, fn func()) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("panic recovered (%s): %v\n%s", name, r, debug.Stack())
			return
		}
		log.Debugf("safe run finished (%s), elapsed=%s", name, time.Since(start))
	}()

	if fn == nil {
		log.Warnf("safe run skipped (%s): fn is nil", name)
		return
	}

	fn()
}

// RecoverHandler returns a recovery function for defer use in request-scoped code.
func RecoverHandler(name string, onPanic func(error)) func() {
	return func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic recovered (%s): %v", name, r)
			log.Errorf("%v\n%s", err, debug.Stack())
			if onPanic != nil {
				onPanic(err)
			}
		}
	}
}
