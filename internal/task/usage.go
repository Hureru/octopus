package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func UsageMaintenanceTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := op.UsageFactsFlushPending(ctx); err != nil {
		log.Warnf("usage facts flush failed: %v", err)
		return
	}
	for batch := 0; batch < 20; batch++ {
		count, err := op.UsageFactsBackfill(ctx, 500)
		if err != nil {
			log.Warnf("usage facts backfill failed: %v", err)
			return
		}
		if count == 0 {
			break
		}
	}
	for batch := 0; batch < 100; batch++ {
		count, err := op.UsageAggregatePending(ctx, 1000)
		if err != nil {
			log.Warnf("usage aggregate update failed: %v", err)
			return
		}
		if count == 0 {
			break
		}
	}
	if deleted, err := op.UsageAggregateRetention(ctx, time.Now(), 0); err != nil {
		log.Warnf("usage aggregate retention failed: %v", err)
	} else if deleted > 0 {
		log.Debugf("usage aggregate retention removed %d hourly rows", deleted)
	}
}
