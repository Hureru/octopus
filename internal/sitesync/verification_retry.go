package sitesync

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

type verificationRetryRunner struct {
	syncAccount    func(context.Context, int) (*model.SiteSyncResult, error)
	checkinAccount func(context.Context, int) (*model.SiteCheckinResult, error)
}

func RetryVerificationSession(ctx context.Context, sessionID int64) error {
	return retryVerificationSession(ctx, sessionID, verificationRetryRunner{
		syncAccount:    SyncAccount,
		checkinAccount: CheckinAccount,
	})
}

func retryVerificationSession(
	ctx context.Context,
	sessionID int64,
	runner verificationRetryRunner,
) error {
	work, err := op.VerificationRetryAcquire(ctx, sessionID)
	if err != nil || work == nil {
		return err
	}
	if work.Session.Source == "browser" {
		if work.Task.PairingID == nil || *work.Task.PairingID <= 0 {
			return finishVerificationRetryFailure(
				work,
				fmt.Errorf("browser verification retry has no pairing binding"),
			)
		}
		if deadline, ok := ctx.Deadline(); !ok || work.Session.ExpiresAt.Before(deadline) {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, work.Session.ExpiresAt)
			defer cancel()
		}
		ctx = withVerificationBrowserTransport(
			ctx,
			op.VerificationBrowserBinding{
				PairingID: *work.Task.PairingID,
				TaskID:    work.Task.ID,
				SessionID: work.Session.ID,
				TargetURL: work.Task.TargetURL,
			},
			op.VerificationBrowserRequest,
		)
	}

	success := false
	message := ""
	var runErr error
	switch work.Task.Operation {
	case model.SiteOperationSync:
		if runner.syncAccount == nil {
			runErr = fmt.Errorf("verification retry sync runner is unavailable")
			break
		}
		result, err := runner.syncAccount(ctx, work.Session.SiteAccountID)
		runErr = err
		if result != nil {
			message = result.Message
			success = err == nil &&
				result.Status != model.SiteExecutionStatusFailed &&
				result.Status != model.SiteExecutionStatusSkipped
		}
	case model.SiteOperationCheckin:
		if runner.checkinAccount == nil {
			runErr = fmt.Errorf("verification retry checkin runner is unavailable")
			break
		}
		result, err := runner.checkinAccount(ctx, work.Session.SiteAccountID)
		runErr = err
		if result != nil {
			message = result.Message
			success = err == nil && result.Status == model.SiteExecutionStatusSuccess
		}
	default:
		runErr = fmt.Errorf("unsupported verification retry operation: %s", work.Task.Operation)
	}
	if runErr != nil {
		message = sanitizeSiteStatusMessage(runErr)
	}
	if !success && message == "" {
		message = "verification retry did not complete successfully"
	}

	status := model.VerificationRetryFailed
	if success {
		status = model.VerificationRetrySucceeded
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if finishErr := op.VerificationRetryFinish(
		finishCtx,
		work.Task.ID,
		work.Token,
		status,
		message,
	); finishErr != nil {
		if runErr != nil {
			return fmt.Errorf("%v; persist verification retry result: %w", runErr, finishErr)
		}
		return finishErr
	}
	if runErr != nil {
		return runErr
	}
	if !success {
		return fmt.Errorf("%s", message)
	}
	return nil
}

func finishVerificationRetryFailure(
	work *op.VerificationRetryWork,
	runErr error,
) error {
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if finishErr := op.VerificationRetryFinish(
		finishCtx,
		work.Task.ID,
		work.Token,
		model.VerificationRetryFailed,
		sanitizeSiteStatusMessage(runErr),
	); finishErr != nil {
		return fmt.Errorf("%v; persist verification retry result: %w", runErr, finishErr)
	}
	return runErr
}
