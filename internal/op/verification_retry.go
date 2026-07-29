package op

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	verificationRetryStaleAfter = 2 * time.Minute
	verificationRetryMessageMax = 2048
)

type VerificationRetryWork struct {
	Task    model.VerificationTask
	Session model.VerificationSession
	Token   string
}

func VerificationRetryAcquire(ctx context.Context, sessionID int64) (*VerificationRetryWork, error) {
	if sessionID <= 0 {
		return nil, fmt.Errorf("verification session id is required")
	}
	token, err := randomHexToken(24)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	work := &VerificationRetryWork{Token: token}
	acquired := false
	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ?", sessionID).
			First(&work.Task).Error; err != nil {
			return fmt.Errorf("verification task not found")
		}
		if work.Task.Status != model.VerificationTaskCompleted {
			return nil
		}
		switch work.Task.Operation {
		case model.SiteOperationSync, model.SiteOperationCheckin:
		default:
			return nil
		}
		switch work.Task.RetryStatus {
		case model.VerificationRetryPending:
		case model.VerificationRetryRunning:
			if work.Task.RetryStartedAt != nil &&
				now.Sub(*work.Task.RetryStartedAt) < verificationRetryStaleAfter {
				return nil
			}
		default:
			return nil
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&work.Session, sessionID).Error; err != nil {
			return fmt.Errorf("verification session not found")
		}
		if !work.Session.ExpiresAt.After(now) {
			return expireVerificationSession(tx, &work.Session, now)
		}
		if work.Session.Status != model.VerificationSessionCompleted {
			return tx.Model(&work.Task).Updates(map[string]any{
				"retry_status":     model.VerificationRetryCanceled,
				"retry_token_hash": "",
			}).Error
		}
		if err := tx.Model(&work.Task).Updates(map[string]any{
			"retry_status":       model.VerificationRetryRunning,
			"retry_token_hash":   verificationTokenHash(token),
			"retry_message":      "",
			"retry_started_at":   now,
			"retry_completed_at": nil,
		}).Error; err != nil {
			return err
		}
		acquired = true
		return tx.First(&work.Task, work.Task.ID).Error
	})
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, nil
	}
	return work, nil
}

func VerificationRetryFinish(
	ctx context.Context,
	taskID int64,
	token string,
	status model.VerificationRetryStatus,
	message string,
) error {
	if taskID <= 0 || strings.TrimSpace(token) == "" {
		return fmt.Errorf("verification retry task and token are required")
	}
	if status != model.VerificationRetrySucceeded && status != model.VerificationRetryFailed {
		return fmt.Errorf("unsupported verification retry status: %s", status)
	}
	now := time.Now()
	result := db.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where(
			"id = ? AND retry_status = ? AND retry_token_hash = ?",
			taskID,
			model.VerificationRetryRunning,
			verificationTokenHash(strings.TrimSpace(token)),
		).
		Updates(map[string]any{
			"retry_status":       status,
			"retry_token_hash":   "",
			"retry_message":      trimVerificationRetryMessage(message),
			"retry_completed_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("verification retry is no longer active")
	}
	return nil
}

func VerificationRetryRequeue(ctx context.Context, sessionID int64) error {
	if sessionID <= 0 {
		return fmt.Errorf("verification session id is required")
	}
	result := db.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where(
			"session_id = ? AND status = ? AND retry_status IN ?",
			sessionID,
			model.VerificationTaskCompleted,
			[]model.VerificationRetryStatus{
				model.VerificationRetryFailed,
				model.VerificationRetrySucceeded,
			},
		).
		Where(
			"session_id IN (?)",
			db.GetDB().Model(&model.VerificationSession{}).
				Select("id").
				Where("status = ? AND expires_at > ?", model.VerificationSessionCompleted, time.Now()),
		).
		Updates(map[string]any{
			"retry_status":       model.VerificationRetryPending,
			"retry_token_hash":   "",
			"retry_message":      "",
			"retry_started_at":   nil,
			"retry_completed_at": nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("verification retry is not requeueable")
	}
	return nil
}

func VerificationRetryPendingSessionIDs(ctx context.Context, limit int) ([]int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	now := time.Now()
	staleBefore := now.Add(-verificationRetryStaleAfter)
	var sessionIDs []int64
	err := db.GetDB().WithContext(ctx).
		Model(&model.VerificationTask{}).
		Joins("JOIN verification_sessions ON verification_sessions.id = verification_tasks.session_id").
		Where("verification_tasks.status = ?", model.VerificationTaskCompleted).
		Where(
			"verification_sessions.status = ? AND verification_sessions.expires_at > ?",
			model.VerificationSessionCompleted,
			now,
		).
		Where("verification_tasks.operation IN ?", []model.SiteOperationType{
			model.SiteOperationSync,
			model.SiteOperationCheckin,
		}).
		Where(
			"verification_tasks.retry_status = ? OR (verification_tasks.retry_status = ? AND verification_tasks.retry_started_at <= ?)",
			model.VerificationRetryPending,
			model.VerificationRetryRunning,
			staleBefore,
		).
		Order("verification_tasks.created_at ASC, verification_tasks.id ASC").
		Limit(limit).
		Pluck("session_id", &sessionIDs).Error
	return sessionIDs, err
}

func trimVerificationRetryMessage(message string) string {
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) > verificationRetryMessageMax {
		return string(runes[:verificationRetryMessageMax])
	}
	return message
}
