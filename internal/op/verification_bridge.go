package op

import (
	"context"
	"errors"
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

var (
	errVerificationTaskClaimExpired  = errors.New("verification task claim expired")
	errVerificationSessionSuperseded = errors.New("verification session was superseded")
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
	Task           model.VerificationTask `json:"task"`
	TaskToken      string                 `json:"task_token"`
	ClaimExpiresAt time.Time              `json:"claim_expires_at"`
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
	siteAccountID int,
) (*VerificationBridgePairingCreated, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("pairing name is required")
	}
	if siteAccountID <= 0 {
		return nil, fmt.Errorf("site account scope is required")
	}
	var account model.SiteAccount
	if err := db.GetDB().WithContext(ctx).First(&account, siteAccountID).Error; err != nil {
		return nil, fmt.Errorf("site account scope not found")
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
		Name:          name,
		SiteAccountID: siteAccountID,
		TokenHash:     verificationTokenHash(token),
		ExpiresAt:     time.Now().Add(ttl),
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
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	if err == nil {
		defaultVerificationBrowserBroker.cancelPairing(
			id,
			fmt.Errorf("verification bridge pairing was revoked"),
		)
	}
	return err
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
			Where(
				"session_id IN (?)",
				tx.Model(&model.VerificationSession{}).
					Select("id").
					Where("site_account_id = ?", pairing.SiteAccountID),
			).
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
	claimExpiresAt := now.Add(verificationTaskClaimTTL)
	if task.ExpiresAt.Before(claimExpiresAt) {
		claimExpiresAt = task.ExpiresAt
	}
	if pairing.ExpiresAt.Before(claimExpiresAt) {
		claimExpiresAt = pairing.ExpiresAt
	}
	return &VerificationTaskClaimed{
		Task:           task,
		TaskToken:      taskToken,
		ClaimExpiresAt: claimExpiresAt,
	}, nil
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
	claimTokenHash := verificationTokenHash(strings.TrimSpace(taskToken))
	var task model.VerificationTask
	if err := db.GetDB().WithContext(ctx).
		Where("pairing_id = ? AND claim_token_hash = ?", pairing.ID, claimTokenHash).
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
	var completionErr error
	err = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := activeVerificationPairing(tx, pairing.ID, time.Now()); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, task.ID).Error; err != nil {
			return err
		}
		if task.Status != model.VerificationTaskClaimed ||
			task.PairingID == nil || *task.PairingID != pairing.ID ||
			task.ClaimTokenHash != claimTokenHash {
			return fmt.Errorf("verification task was already consumed")
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&session, task.SessionID).Error; err != nil {
			return err
		}
		now := time.Now()
		if !task.ExpiresAt.After(now) || !session.ExpiresAt.After(now) {
			if err := expireVerificationSession(tx, &session, now); err != nil {
				return err
			}
			completionErr = errVerificationSessionExpired
			return nil
		}
		if task.ClaimedAt == nil ||
			!task.ClaimedAt.Add(verificationTaskClaimTTL).After(now) {
			if err := tx.Model(&task).Updates(map[string]any{
				"status":           model.VerificationTaskPending,
				"pairing_id":       nil,
				"claim_token_hash": "",
				"claimed_at":       nil,
			}).Error; err != nil {
				return err
			}
			completionErr = errVerificationTaskClaimExpired
			return nil
		}
		resolvedUserAgent, resolveErr := resolveVerificationUserAgent(session.UserAgent, effectiveUserAgent)
		if resolveErr != nil {
			return resolveErr
		}
		installed, err := completeVerificationSessionRecord(
			tx,
			&session,
			cookieHeader,
			resolvedUserAgent,
			"bridge",
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
) (bool, error) {
	if session == nil || session.Status != model.VerificationSessionPending {
		return false, fmt.Errorf("verification session is not pending")
	}
	encrypted, err := EncryptSecret(cookie)
	if err != nil {
		return false, err
	}
	if userAgent != "" {
		session.UserAgent = userAgent
	}
	ttl := session.ExpiresAt.Sub(session.CreatedAt)
	if ttl <= 0 || ttl > time.Hour {
		ttl = 15 * time.Minute
	}
	session.ExpiresAt = completedAt.Add(ttl)
	session.CookieEncrypted = encrypted
	session.CompletedAt = &completedAt
	session.Source = source
	result := tx.Model(&model.SiteAccount{}).
		Where(
			"id = ? AND verification_session_fence_id < ?",
			session.SiteAccountID,
			session.ID,
		).
		Updates(map[string]any{
			"verification_cookie_encrypted": encrypted,
			"verification_session_fence_id": session.ID,
			"verification_user_agent":       session.UserAgent,
			"verification_proxy_config_id":  session.ProxyConfigID,
			"verification_clash_node":       session.ClashNode,
			"verification_expires_at":       session.ExpiresAt,
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		session.Status = model.VerificationSessionSuperseded
		session.CookieEncrypted = ""
		if err := tx.Save(session).Error; err != nil {
			return false, err
		}
		return false, nil
	}
	session.Status = model.VerificationSessionCompleted
	if err := tx.Save(session).Error; err != nil {
		return false, err
	}
	return true, nil
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
	if pairing.SiteAccountID <= 0 {
		return nil, fmt.Errorf("verification bridge pairing has no account scope")
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
	type selectedCookie struct {
		value  string
		domain string
		path   string
	}
	values := make(map[string][]selectedCookie, len(cookies))
	scopes := make(map[string]map[string]string, len(cookies))
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		if !validCookieName(name) || strings.ContainsAny(cookie.Value, "\r\n;") {
			return "", fmt.Errorf("invalid verification cookie")
		}
		if !verificationCookieAllowed(name) {
			continue
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
		if path == "" {
			path = "/"
		}
		scopeKey := domain + "\x00" + path
		if scopes[name] == nil {
			scopes[name] = make(map[string]string)
		}
		if existing, ok := scopes[name][scopeKey]; ok {
			if existing != cookie.Value {
				return "", fmt.Errorf("verification cookie %q has conflicting values for the same scope", name)
			}
			continue
		}
		scopes[name][scopeKey] = cookie.Value
		values[name] = append(values[name], selectedCookie{
			value:  cookie.Value,
			domain: domain,
			path:   path,
		})
	}
	if len(values) == 0 {
		return "", fmt.Errorf("verification cookies do not contain a supported Cloudflare credential")
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		selected := values[name]
		sort.SliceStable(selected, func(i, j int) bool {
			if len(selected[i].path) != len(selected[j].path) {
				return len(selected[i].path) > len(selected[j].path)
			}
			leftExact := selected[i].domain == targetHost
			rightExact := selected[j].domain == targetHost
			if leftExact != rightExact {
				return leftExact
			}
			if len(selected[i].domain) != len(selected[j].domain) {
				return len(selected[i].domain) > len(selected[j].domain)
			}
			return selected[i].value < selected[j].value
		})
		for _, cookie := range selected {
			parts = append(parts, name+"="+cookie.value)
		}
	}
	header := strings.Join(parts, "; ")
	if len(header) > verificationCookieMaxBytes {
		return "", fmt.Errorf("verification cookie header is too large")
	}
	return header, nil
}

func verificationCookieAllowed(name string) bool {
	name = strings.TrimSpace(name)
	return name == "cf_clearance" ||
		name == "__cf_bm" ||
		strings.HasPrefix(name, "cf_chl_")
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
	if pairing.RevokedAt != nil || !pairing.ExpiresAt.After(now) {
		return nil, fmt.Errorf("verification bridge pairing expired or revoked")
	}
	if pairing.SiteAccountID <= 0 {
		return nil, fmt.Errorf("verification bridge pairing has no account scope")
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
