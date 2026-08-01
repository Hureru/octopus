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

	if cleaned, err := op.VerificationSessionCleanup(ctx, time.Now()); err != nil {
		log.Warnf("verification credential cleanup failed: %v", err)
	} else if cleaned > 0 {
		log.Debugf("verification credential cleanup expired %d sessions", cleaned)
	}

	sessionIDs, err := op.VerificationRetryPendingSessionIDs(ctx, verificationRetrySweepLimit)
	if err != nil {
		log.Warnf("list pending verification retries failed: %v", err)
		return
	}
	for _, sessionID := range sessionIDs {
		runCtx, runCancel := context.WithTimeout(ctx, 3*time.Minute)
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
