package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/sitesync"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const verificationRetrySweepLimit = 10

func VerificationRetryTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	sessionIDs, err := op.VerificationRetryPendingSessionIDs(ctx, verificationRetrySweepLimit)
	if err != nil {
		log.Warnf("list pending verification retries failed: %v", err)
		return
	}
	for _, sessionID := range sessionIDs {
		runCtx, runCancel := context.WithTimeout(ctx, 90*time.Second)
		err := sitesync.RetryVerificationSession(runCtx, sessionID)
		runCancel()
		if err != nil {
			log.Warnw(
				"pending verification retry failed",
				"session_id", sessionID,
				"error", err,
			)
		}
	}
}
