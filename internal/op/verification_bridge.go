package op

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"golang.org/x/net/publicsuffix"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	verificationPairingDefaultTTL = 30 * 24 * time.Hour
	verificationPairingMaxTTL     = 365 * 24 * time.Hour
	verificationCookieMaxCount    = 50
	verificationCookieMaxBytes    = 16 << 10
	verificationTaskClaimTTL      = 10 * time.Minute
)

type VerificationBridgePairingCreated struct {
	Pairing model.VerificationBridgePairing `json:"pairing"`
	Token   string                          `json:"token"`
}

type VerificationTaskClaimed struct {
	Task      model.VerificationTask `json:"task"`
	TaskToken string                 `json:"task_token"`
}

type VerificationCookieInput struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
}

func VerificationBridgePairingCreate(
	ctx context.Context,
	name string,
	ttlDays int,
) (*VerificationBridgePairingCreated, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("pairing name is required")
	}
	ttl := time.Duration(ttlDays) * 24 * time.Hour
	if ttl <= 0 {
		ttl = verificationPairingDefaultTTL
	}
	if ttl > verificationPairingMaxTTL {
		ttl = verificationPairingMaxTTL
	}
	token, err := randomHexToken(32)
	if err != nil {
		return nil, err
	}
	pairing := model.VerificationBridgePairing{
		Name:      name,
		TokenHash: verificationTokenHash(token),
		ExpiresAt: time.Now().Add(ttl),
	}
	if err := db.GetDB().WithContext(ctx).Create(&pairing).Error; err != nil {
		return nil, err
	}
	return &VerificationBridgePairingCreated{Pairing: pairing, Token: token}, nil
}

func VerificationBridgePairingList(ctx context.Context) ([]model.VerificationBridgePairing, error) {
	var items []model.VerificationBridgePairing
	err := db.GetDB().WithContext(ctx).Order("created_at DESC, id DESC").Find(&items).Error
	return items, err
}

func VerificationBridgePairingRevoke(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("pairing id is required")
	}
	now := time.Now()
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.VerificationBridgePairing{}).
			Where("id = ? AND revoked_at IS NULL", id).
			Update("revoked_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("active pairing not found")
		}
		return releaseVerificationTasksForPairing(tx, id, now)
	})
}

func VerificationTaskList(
	ctx context.Context,
	accountID int,
) ([]model.VerificationTask, error) {
	if err := expireVerificationTasks(ctx); err != nil {
		return nil, err
	}
	query := db.GetDB().WithContext(ctx).Model(&model.VerificationTask{})
	if accountID > 0 {
		query = query.Where(
			"session_id IN (?)",
			db.GetDB().Model(&model.VerificationSession{}).
				Select("id").
				Where("site_account_id = ?", accountID),
		)
	}
	var items []model.VerificationTask
	err := query.Order("created_at DESC, id DESC").Limit(100).Find(&items).Error
	return items, err
}

func VerificationTaskClaim(
	ctx context.Context,
	pairingToken string,
) (*VerificationTaskClaimed, error) {
	pairing, err := verificationBridgePairingByToken(ctx, pairingToken)
	if err != nil {
		return nil, err
	}
	taskToken, err := randomHexToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var task model.VerificationTask
	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := activeVerificationPairing(tx, pairing.ID, now); err != nil {
			return err
		}
		if err := refreshVerificationTaskClaims(tx, now); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("status = ? AND expires_at > ?", model.VerificationTaskPending, now).
			Order("created_at ASC, id ASC").
			First(&task).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("no pending verification task")
			}
			return err
		}
		result := tx.Model(&model.VerificationTask{}).
			Where("id = ? AND status = ?", task.ID, model.VerificationTaskPending).
			Updates(map[string]any{
				"status":           model.VerificationTaskClaimed,
				"pairing_id":       pairing.ID,
				"claim_token_hash": verificationTokenHash(taskToken),
				"claimed_at":       now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("verification task was claimed concurrently")
		}
		return tx.First(&task, task.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &VerificationTaskClaimed{Task: task, TaskToken: taskToken}, nil
}

func VerificationTaskComplete(
	ctx context.Context,
	pairingToken string,
	taskToken string,
	cookies []VerificationCookieInput,
	userAgent string,
) (*model.VerificationSession, error) {
	pairing, err := verificationBridgePairingByToken(ctx, pairingToken)
	if err != nil {
		return nil, err
	}
	var task model.VerificationTask
	if err := db.GetDB().WithContext(ctx).
		Where("pairing_id = ? AND claim_token_hash = ?", pairing.ID, verificationTokenHash(strings.TrimSpace(taskToken))).
		First(&task).Error; err != nil {
		return nil, fmt.Errorf("verification task not found")
	}
	if task.Status != model.VerificationTaskClaimed {
		return nil, fmt.Errorf("verification task is not claimed")
	}
	cookieHeader, err := verificationCookieHeader(task.TargetHost, cookies)
	if err != nil {
		return nil, err
	}
	effectiveUserAgent := strings.TrimSpace(userAgent)
	if task.UserAgent != "" && effectiveUserAgent != "" && effectiveUserAgent != task.UserAgent {
		return nil, fmt.Errorf("verification user agent does not match the task binding")
	}
	if effectiveUserAgent == "" {
		effectiveUserAgent = task.UserAgent
	}

	var session model.VerificationSession
	now := time.Now()
	var completionErr error
	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := activeVerificationPairing(tx, pairing.ID, now); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, task.ID).Error; err != nil {
			return err
		}
		if task.Status != model.VerificationTaskClaimed ||
			task.PairingID == nil || *task.PairingID != pairing.ID {
			return fmt.Errorf("verification task was already consumed")
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&session, task.SessionID).Error; err != nil {
			return err
		}
		if now.After(task.ExpiresAt) || now.After(session.ExpiresAt) {
			if err := expireVerificationSession(tx, &session, now); err != nil {
				return err
			}
			completionErr = errVerificationSessionExpired
			return nil
		}
		resolvedUserAgent, resolveErr := resolveVerificationUserAgent(session.UserAgent, effectiveUserAgent)
		if resolveErr != nil {
			return resolveErr
		}
		if err := completeVerificationSessionRecord(
			tx,
			&session,
			cookieHeader,
			resolvedUserAgent,
			"bridge",
			now,
		); err != nil {
			return err
		}
		return tx.Model(&task).Updates(map[string]any{
			"status":           model.VerificationTaskCompleted,
			"completed_at":     now,
			"claim_token_hash": "",
		}).Error
	})
	if err != nil {
		return nil, err
	}
	if completionErr != nil {
		return nil, completionErr
	}
	session.CookieEncrypted = ""
	return &session, nil
}

func VerificationTaskRelease(
	ctx context.Context,
	pairingToken string,
	taskToken string,
) error {
	pairing, err := verificationBridgePairingByToken(ctx, pairingToken)
	if err != nil {
		return err
	}
	taskToken = strings.TrimSpace(taskToken)
	if taskToken == "" {
		return fmt.Errorf("task token is required")
	}
	now := time.Now()
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := activeVerificationPairing(tx, pairing.ID, now); err != nil {
			return err
		}
		result := tx.Model(&model.VerificationTask{}).
			Where(
				"pairing_id = ? AND claim_token_hash = ? AND status = ?",
				pairing.ID,
				verificationTokenHash(taskToken),
				model.VerificationTaskClaimed,
			).
			Updates(map[string]any{
				"status":           model.VerificationTaskPending,
				"pairing_id":       nil,
				"claim_token_hash": "",
				"claimed_at":       nil,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("verification task is not releasable")
		}
		return nil
	})
}

func VerificationSessionManualComplete(
	ctx context.Context,
	sessionID int64,
	cookie string,
	userAgent string,
) (*model.VerificationSession, error) {
	return completeVerificationSession(ctx, sessionID, cookie, userAgent, "manual")
}

func completeVerificationSessionRecord(
	tx *gorm.DB,
	session *model.VerificationSession,
	cookie string,
	userAgent string,
	source string,
	completedAt time.Time,
) error {
	if session == nil || session.Status != model.VerificationSessionPending {
		return fmt.Errorf("verification session is not pending")
	}
	encrypted, err := EncryptSecret(cookie)
	if err != nil {
		return err
	}
	if userAgent != "" {
		session.UserAgent = userAgent
	}
	session.CookieEncrypted = encrypted
	session.Status = model.VerificationSessionCompleted
	session.CompletedAt = &completedAt
	session.Source = source
	if err := tx.Save(session).Error; err != nil {
		return err
	}
	return tx.Model(&model.SiteAccount{}).Where("id = ?", session.SiteAccountID).Updates(map[string]any{
		"verification_cookie_encrypted": encrypted,
		"verification_user_agent":       session.UserAgent,
		"verification_proxy_config_id":  session.ProxyConfigID,
		"verification_clash_node":       session.ClashNode,
		"verification_expires_at":       session.ExpiresAt,
	}).Error
}

func verificationBridgePairingByToken(
	ctx context.Context,
	token string,
) (*model.VerificationBridgePairing, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("pairing token is required")
	}
	var pairing model.VerificationBridgePairing
	if err := db.GetDB().WithContext(ctx).
		Where("token_hash = ?", verificationTokenHash(token)).
		First(&pairing).Error; err != nil {
		return nil, fmt.Errorf("verification bridge is not paired")
	}
	if pairing.RevokedAt != nil || time.Now().After(pairing.ExpiresAt) {
		return nil, fmt.Errorf("verification bridge pairing expired or revoked")
	}
	now := time.Now()
	if err := db.GetDB().WithContext(ctx).Model(&pairing).
		Update("last_seen_at", now).Error; err != nil {
		return nil, err
	}
	pairing.LastSeenAt = &now
	return &pairing, nil
}

func verificationTargetHost(targetURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil {
		return "", fmt.Errorf("invalid verification target: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", fmt.Errorf("verification target must be an http or https URL")
	}
	return strings.ToLower(parsed.Hostname()), nil
}

func verificationCookieHeader(
	targetHost string,
	cookies []VerificationCookieInput,
) (string, error) {
	if len(cookies) == 0 || len(cookies) > verificationCookieMaxCount {
		return "", fmt.Errorf("verification cookies must contain 1 to %d entries", verificationCookieMaxCount)
	}
	targetHost = strings.ToLower(strings.TrimSpace(targetHost))
	values := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		if !validCookieName(name) || strings.ContainsAny(cookie.Value, "\r\n;") {
			return "", fmt.Errorf("invalid verification cookie")
		}
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cookie.Domain)), ".")
		if domain == "" {
			domain = targetHost
		}
		if !verificationCookieDomainAllowed(targetHost, domain) {
			return "", fmt.Errorf("cookie domain %q is outside verification target", cookie.Domain)
		}
		path := strings.TrimSpace(cookie.Path)
		if path != "" && !strings.HasPrefix(path, "/") {
			return "", fmt.Errorf("invalid verification cookie path")
		}
		values[name] = cookie.Value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+values[name])
	}
	header := strings.Join(parts, "; ")
	if len(header) > verificationCookieMaxBytes {
		return "", fmt.Errorf("verification cookie header is too large")
	}
	return header, nil
}

func validCookieName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return false
		}
	}
	return true
}

func verificationCookieDomainAllowed(targetHost string, domain string) bool {
	targetHost = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(targetHost)), ".")
	domain = strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "."), ".")
	if targetHost == "" || domain == "" || strings.ContainsAny(domain, " /\\\r\n\t") {
		return false
	}
	if targetHost == domain {
		return true
	}
	if net.ParseIP(targetHost) != nil || net.ParseIP(domain) != nil {
		return false
	}
	publicSuffix, _ := publicsuffix.PublicSuffix(domain)
	if publicSuffix == domain {
		return false
	}
	return strings.HasSuffix(targetHost, "."+domain)
}

func expireVerificationTasks(ctx context.Context) error {
	now := time.Now()
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := refreshVerificationTaskClaims(tx, now); err != nil {
			return err
		}
		return tx.Model(&model.VerificationSession{}).
			Where("status = ? AND expires_at <= ?", model.VerificationSessionPending, now).
			Update("status", model.VerificationSessionExpired).Error
	})
}

func activeVerificationPairing(
	tx *gorm.DB,
	id int64,
	now time.Time,
) (*model.VerificationBridgePairing, error) {
	var pairing model.VerificationBridgePairing
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pairing, id).Error; err != nil {
		return nil, fmt.Errorf("verification bridge is not paired")
	}
	if pairing.RevokedAt != nil || now.After(pairing.ExpiresAt) {
		return nil, fmt.Errorf("verification bridge pairing expired or revoked")
	}
	return &pairing, nil
}

func refreshVerificationTaskClaims(tx *gorm.DB, now time.Time) error {
	if err := tx.Model(&model.VerificationTask{}).
		Where("status IN ? AND expires_at <= ?", []model.VerificationTaskStatus{
			model.VerificationTaskPending,
			model.VerificationTaskClaimed,
		}, now).
		Updates(map[string]any{
			"status":           model.VerificationTaskExpired,
			"claim_token_hash": "",
			"pairing_id":       nil,
			"claimed_at":       nil,
			"retry_status":     model.VerificationRetryCanceled,
			"retry_token_hash": "",
		}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.VerificationTask{}).
		Where(
			"status = ? AND pairing_id IN (?)",
			model.VerificationTaskClaimed,
			tx.Model(&model.VerificationBridgePairing{}).
				Select("id").
				Where("revoked_at IS NOT NULL OR expires_at <= ?", now),
		).
		Updates(map[string]any{
			"status":           model.VerificationTaskPending,
			"claim_token_hash": "",
			"pairing_id":       nil,
			"claimed_at":       nil,
		}).Error; err != nil {
		return err
	}
	return tx.Model(&model.VerificationTask{}).
		Where("status = ? AND claimed_at <= ? AND expires_at > ?",
			model.VerificationTaskClaimed,
			now.Add(-verificationTaskClaimTTL),
			now,
		).
		Updates(map[string]any{
			"status":           model.VerificationTaskPending,
			"claim_token_hash": "",
			"pairing_id":       nil,
			"claimed_at":       nil,
		}).Error
}

func releaseVerificationTasksForPairing(tx *gorm.DB, pairingID int64, now time.Time) error {
	if err := tx.Model(&model.VerificationTask{}).
		Where("pairing_id = ? AND status = ? AND expires_at <= ?",
			pairingID,
			model.VerificationTaskClaimed,
			now,
		).
		Updates(map[string]any{
			"status":           model.VerificationTaskExpired,
			"claim_token_hash": "",
			"pairing_id":       nil,
			"claimed_at":       nil,
		}).Error; err != nil {
		return err
	}
	return tx.Model(&model.VerificationTask{}).
		Where("pairing_id = ? AND status = ? AND expires_at > ?",
			pairingID,
			model.VerificationTaskClaimed,
			now,
		).
		Updates(map[string]any{
			"status":           model.VerificationTaskPending,
			"claim_token_hash": "",
			"pairing_id":       nil,
			"claimed_at":       nil,
		}).Error
}
