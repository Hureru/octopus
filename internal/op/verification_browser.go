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

type VerificationBridgeIdentity struct {
	Pairing         model.VerificationBridgePairing `json:"pairing"`
	SiteID          int                             `json:"site_id"`
	SiteName        string                          `json:"site_name"`
	SiteAccountID   int                             `json:"site_account_id"`
	SiteAccountName string                          `json:"site_account_name"`
	LatestTask      *model.VerificationTask         `json:"latest_task,omitempty"`
}

type VerificationBrowserReady struct {
	Session model.VerificationSession `json:"session"`
	Task    model.VerificationTask    `json:"task"`
}

func VerificationBridgeIdentify(
	ctx context.Context,
	pairingToken string,
) (*VerificationBridgeIdentity, error) {
	pairing, err := verificationBridgePairingByToken(ctx, pairingToken)
	if err != nil {
		return nil, err
	}
	account, err := SiteAccountGet(pairing.SiteAccountID, ctx)
	if err != nil {
		return nil, fmt.Errorf("paired site account not found")
	}
	siteRecord, err := SiteGet(account.SiteID, ctx)
	if err != nil {
		return nil, fmt.Errorf("paired site not found")
	}

	var latestTask model.VerificationTask
	taskErr := db.GetDB().WithContext(ctx).
		Where(
			"session_id IN (?)",
			db.GetDB().Model(&model.VerificationSession{}).
				Select("id").
				Where("site_account_id = ?", account.ID),
		).
		Order("created_at DESC, id DESC").
		First(&latestTask).Error
	if taskErr != nil && taskErr != gorm.ErrRecordNotFound {
		return nil, taskErr
	}

	identity := &VerificationBridgeIdentity{
		Pairing:         *pairing,
		SiteID:          siteRecord.ID,
		SiteName:        siteRecord.Name,
		SiteAccountID:   account.ID,
		SiteAccountName: account.Name,
	}
	if taskErr == nil {
		identity.LatestTask = &latestTask
	}
	return identity, nil
}

func VerificationBridgePairingRotate(
	ctx context.Context,
	id int64,
) (*VerificationBridgePairingCreated, error) {
	if id <= 0 {
		return nil, fmt.Errorf("pairing id is required")
	}
	token, err := randomHexToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var pairing model.VerificationBridgePairing
	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&pairing, id).Error; err != nil {
			return fmt.Errorf("active pairing not found")
		}
		if pairing.RevokedAt != nil || !pairing.ExpiresAt.After(now) {
			return fmt.Errorf("verification bridge pairing expired or revoked")
		}
		if err := tx.Model(&pairing).Updates(map[string]any{
			"token_hash":   verificationTokenHash(token),
			"last_seen_at": nil,
		}).Error; err != nil {
			return err
		}
		if err := releaseVerificationTasksForPairing(tx, pairing.ID, now); err != nil {
			return err
		}
		return tx.First(&pairing, pairing.ID).Error
	})
	if err != nil {
		return nil, err
	}
	defaultVerificationBrowserBroker.cancelPairing(
		id,
		fmt.Errorf("verification bridge token was rotated"),
	)
	return &VerificationBridgePairingCreated{Pairing: pairing, Token: token}, nil
}

func VerificationTaskBrowserReady(
	ctx context.Context,
	pairingToken string,
	taskToken string,
	userAgent string,
) (*VerificationBrowserReady, error) {
	pairing, err := verificationBridgePairingByToken(ctx, pairingToken)
	if err != nil {
		return nil, err
	}
	taskToken = strings.TrimSpace(taskToken)
	if taskToken == "" {
		return nil, fmt.Errorf("task token is required")
	}
	claimTokenHash := verificationTokenHash(taskToken)
	now := time.Now()
	var task model.VerificationTask
	var session model.VerificationSession
	var completionErr error

	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := activeVerificationPairing(tx, pairing.ID, now); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("pairing_id = ? AND claim_token_hash = ?", pairing.ID, claimTokenHash).
			First(&task).Error; err != nil {
			return fmt.Errorf("verification task not found")
		}
		if task.Status != model.VerificationTaskClaimed ||
			task.PairingID == nil || *task.PairingID != pairing.ID {
			return fmt.Errorf("verification task was already consumed")
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&session, task.SessionID).Error; err != nil {
			return fmt.Errorf("verification session not found")
		}
		if !task.ExpiresAt.After(now) || !session.ExpiresAt.After(now) {
			if err := expireVerificationSession(tx, &session, now); err != nil {
				return err
			}
			completionErr = errVerificationSessionExpired
			return nil
		}
		if task.ClaimedAt == nil ||
			!task.ClaimedAt.Add(verificationTaskClaimTTL).After(now) {
			if err := releaseClaimedVerificationTask(tx, &task); err != nil {
				return err
			}
			completionErr = errVerificationTaskClaimExpired
			return nil
		}
		resolvedUserAgent, err := resolveVerificationUserAgent(
			session.UserAgent,
			userAgent,
		)
		if err != nil {
			return err
		}
		installed, err := completeBrowserVerificationSession(
			tx,
			&session,
			resolvedUserAgent,
			now,
		)
		if err != nil {
			return err
		}
		if !installed {
			completionErr = errVerificationSessionSuperseded
			fields := canceledVerificationTaskFields()
			fields["completed_at"] = now
			return tx.Model(&task).Updates(fields).Error
		}
		if err := tx.Model(&task).Updates(map[string]any{
			"status":           model.VerificationTaskCompleted,
			"completed_at":     now,
			"claim_token_hash": "",
		}).Error; err != nil {
			return err
		}
		return tx.First(&task, task.ID).Error
	})
	if err != nil {
		return nil, err
	}
	if completionErr != nil {
		return nil, completionErr
	}
	session.CookieEncrypted = ""
	return &VerificationBrowserReady{Session: session, Task: task}, nil
}

func releaseClaimedVerificationTask(tx *gorm.DB, task *model.VerificationTask) error {
	return tx.Model(task).Updates(map[string]any{
		"status":           model.VerificationTaskPending,
		"pairing_id":       nil,
		"claim_token_hash": "",
		"claimed_at":       nil,
	}).Error
}

func completeBrowserVerificationSession(
	tx *gorm.DB,
	session *model.VerificationSession,
	userAgent string,
	completedAt time.Time,
) (bool, error) {
	if session == nil || session.Status != model.VerificationSessionPending {
		return false, fmt.Errorf("verification session is not pending")
	}
	fields := clearedVerificationCredentialFields()
	fields["verification_session_fence_id"] = session.ID
	result := tx.Model(&model.SiteAccount{}).
		Where(
			"id = ? AND verification_session_fence_id < ?",
			session.SiteAccountID,
			session.ID,
		).
		Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		session.Status = model.VerificationSessionSuperseded
		session.CookieEncrypted = ""
		return false, tx.Save(session).Error
	}

	ttl := session.ExpiresAt.Sub(session.CreatedAt)
	if ttl <= 0 || ttl > time.Hour {
		ttl = 15 * time.Minute
	}
	session.UserAgent = strings.TrimSpace(userAgent)
	session.CookieEncrypted = ""
	session.Status = model.VerificationSessionCompleted
	session.ExpiresAt = completedAt.Add(ttl)
	session.CompletedAt = &completedAt
	session.Source = "browser"
	return true, tx.Save(session).Error
}
