package op

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VerificationSessionCreateRequest struct {
	SiteAccountID        int                     `json:"site_account_id"`
	ProxyConfigID        *int                    `json:"proxy_config_id,omitempty"`
	ClashNode            string                  `json:"clash_node,omitempty"`
	UserAgent            string                  `json:"user_agent,omitempty"`
	TTLMinutes           int                     `json:"ttl_minutes,omitempty"`
	UseAccountPreference bool                    `json:"use_account_preference,omitempty"`
	Operation            model.SiteOperationType `json:"operation,omitempty"`
}

type VerificationSessionCreated struct {
	Session model.VerificationSession `json:"session"`
	Task    model.VerificationTask    `json:"task"`
}

var errVerificationSessionExpired = errors.New("verification session expired")
var verificationSessionEnsureLocks sync.Map

func VerificationSessionCreate(ctx context.Context, request VerificationSessionCreateRequest) (*VerificationSessionCreated, error) {
	account, normalized, err := resolveVerificationSessionRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	return createVerificationSession(ctx, account, normalized)
}

func createVerificationSession(
	ctx context.Context,
	account *model.SiteAccount,
	request VerificationSessionCreateRequest,
) (*VerificationSessionCreated, error) {
	if account == nil {
		return nil, fmt.Errorf("site account not found")
	}
	ttl := request.TTLMinutes
	if ttl <= 0 || ttl > 60 {
		ttl = 15
	}
	session := model.VerificationSession{
		SiteID:        account.SiteID,
		SiteAccountID: account.ID,
		ProxyConfigID: cloneOptionalInt(request.ProxyConfigID),
		ClashNode:     request.ClashNode,
		UserAgent:     request.UserAgent,
		Status:        model.VerificationSessionPending,
		ExpiresAt:     time.Now().Add(time.Duration(ttl) * time.Minute),
	}
	siteRecord, err := SiteGet(account.SiteID, ctx)
	if err != nil {
		return nil, fmt.Errorf("site not found")
	}
	targetURL := strings.TrimRight(strings.TrimSpace(siteRecord.BaseURL), "/")
	targetHost, err := verificationTargetHost(targetURL)
	if err != nil {
		return nil, err
	}
	task := model.VerificationTask{
		Status:        model.VerificationTaskPending,
		TargetURL:     targetURL,
		TargetHost:    targetHost,
		ProxyConfigID: cloneOptionalInt(session.ProxyConfigID),
		ClashNode:     session.ClashNode,
		UserAgent:     session.UserAgent,
		ExpiresAt:     session.ExpiresAt,
		Operation:     request.Operation,
		RetryStatus:   verificationRetryStatusForOperation(request.Operation),
	}
	if err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		task.SessionID = session.ID
		return tx.Create(&task).Error
	}); err != nil {
		return nil, err
	}
	return &VerificationSessionCreated{Session: session, Task: task}, nil
}

func resolveVerificationSessionRequest(
	ctx context.Context,
	request VerificationSessionCreateRequest,
) (*model.SiteAccount, VerificationSessionCreateRequest, error) {
	account, err := SiteAccountGet(request.SiteAccountID, ctx)
	if err != nil {
		return nil, request, fmt.Errorf("site account not found")
	}
	request.ClashNode = strings.TrimSpace(request.ClashNode)
	request.UserAgent = strings.TrimSpace(request.UserAgent)
	switch request.Operation {
	case "", model.SiteOperationSync, model.SiteOperationCheckin:
	default:
		return nil, request, fmt.Errorf("unsupported verification retry operation: %s", request.Operation)
	}
	if request.UseAccountPreference {
		if request.ProxyConfigID == nil {
			request.ProxyConfigID = cloneOptionalInt(account.PreferredProxyConfigID)
		}
		if request.ClashNode == "" {
			request.ClashNode = strings.TrimSpace(account.PreferredClashNode)
		}
	}
	return account, request, nil
}

func VerificationSessionEnsure(
	ctx context.Context,
	request VerificationSessionCreateRequest,
) (*VerificationSessionCreated, error) {
	account, request, err := resolveVerificationSessionRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	release, err := acquireVerificationSessionEnsureGuard(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	defer release()

	now := time.Now()
	var session model.VerificationSession
	query := db.GetDB().WithContext(ctx).
		Where("site_account_id = ? AND status = ? AND expires_at > ?",
			request.SiteAccountID,
			model.VerificationSessionPending,
			now,
		)
	if request.ProxyConfigID == nil {
		query = query.Where("proxy_config_id IS NULL")
	} else {
		query = query.Where("proxy_config_id = ?", *request.ProxyConfigID)
	}
	query = query.
		Where("clash_node = ?", request.ClashNode).
		Where("user_agent = ?", request.UserAgent)
	if err := query.Order("created_at DESC").First(&session).Error; err == nil {
		var task model.VerificationTask
		if taskErr := db.GetDB().WithContext(ctx).
			Where("session_id = ? AND status IN ?",
				session.ID,
				[]model.VerificationTaskStatus{model.VerificationTaskPending, model.VerificationTaskClaimed},
			).
			Where("expires_at > ?", now).
			First(&task).Error; taskErr == nil {
			if request.Operation == "" || task.Operation == request.Operation {
				return &VerificationSessionCreated{Session: session, Task: task}, nil
			}
		}
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return createVerificationSession(ctx, account, request)
}

func acquireVerificationSessionEnsureGuard(
	ctx context.Context,
	accountID int,
) (func(), error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("site account id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	created := make(chan struct{}, 1)
	created <- struct{}{}
	lockValue, _ := verificationSessionEnsureLocks.LoadOrStore(accountID, created)
	lock := lockValue.(chan struct{})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock:
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			lock <- struct{}{}
		})
	}, nil
}

func VerificationSessionList(ctx context.Context, accountID int) ([]model.VerificationSession, error) {
	if err := expireVerificationTasks(ctx); err != nil {
		return nil, err
	}
	query := db.GetDB().WithContext(ctx).Model(&model.VerificationSession{})
	if accountID > 0 {
		query = query.Where("site_account_id = ?", accountID)
	}
	var items []model.VerificationSession
	err := query.Order("created_at DESC").Limit(100).Find(&items).Error
	for i := range items {
		items[i].CookieEncrypted = ""
	}
	return items, err
}

func VerificationSessionRevoke(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("verification session id is required")
	}
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session model.VerificationSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, id).Error; err != nil {
			return fmt.Errorf("verification session not found")
		}
		if session.Status == model.VerificationSessionRevoked {
			return nil
		}
		session.Status = model.VerificationSessionRevoked
		session.CookieEncrypted = ""
		if err := tx.Save(&session).Error; err != nil {
			return err
		}
		if err := cancelVerificationTasksForSession(tx, session.ID); err != nil {
			return err
		}
		if err := cancelVerificationRetryForSession(tx, session.ID); err != nil {
			return err
		}
		return tx.Model(&model.SiteAccount{}).
			Where(
				"id = ? AND verification_session_fence_id = ?",
				session.SiteAccountID,
				session.ID,
			).
			Updates(clearedVerificationCredentialFields()).Error
	})
	if err == nil {
		defaultVerificationBrowserBroker.cancelSession(
			id,
			fmt.Errorf("verification session was revoked"),
		)
	}
	return err
}

func VerificationSessionClearAccount(ctx context.Context, accountID int) error {
	if accountID <= 0 {
		return fmt.Errorf("site account id is required")
	}
	var sessionIDs []int64
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account model.SiteAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&account, accountID).Error; err != nil {
			return fmt.Errorf("site account not found")
		}
		if err := tx.Model(&model.VerificationSession{}).
			Where("site_account_id = ? AND status IN ?", accountID, []model.VerificationSessionStatus{
				model.VerificationSessionPending,
				model.VerificationSessionCompleted,
			}).
			Pluck("id", &sessionIDs).Error; err != nil {
			return err
		}
		if len(sessionIDs) > 0 {
			if err := tx.Model(&model.VerificationSession{}).
				Where("id IN ?", sessionIDs).
				Updates(map[string]any{
					"status":           model.VerificationSessionRevoked,
					"cookie_encrypted": "",
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.VerificationTask{}).
				Where("session_id IN ? AND status IN ?", sessionIDs, []model.VerificationTaskStatus{
					model.VerificationTaskPending,
					model.VerificationTaskClaimed,
				}).
				Updates(canceledVerificationTaskFields()).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.VerificationTask{}).
				Where("session_id IN ? AND retry_status IN ?", sessionIDs, []model.VerificationRetryStatus{
					model.VerificationRetryPending,
					model.VerificationRetryRunning,
				}).
				Update("retry_status", model.VerificationRetryCanceled).Error; err != nil {
				return err
			}
		}
		fenceID := account.VerificationSessionFenceID
		for _, sessionID := range sessionIDs {
			if sessionID > fenceID {
				fenceID = sessionID
			}
		}
		fields := clearedVerificationCredentialFields()
		fields["verification_session_fence_id"] = fenceID
		return tx.Model(&model.SiteAccount{}).
			Where("id = ?", accountID).
			Updates(fields).Error
	})
	if err == nil {
		for _, sessionID := range sessionIDs {
			defaultVerificationBrowserBroker.cancelSession(
				sessionID,
				fmt.Errorf("verification account session was cleared"),
			)
		}
	}
	return err
}

func VerificationHeadersForAccount(
	account *model.SiteAccount,
	proxyConfigID *int,
	clashNode string,
) (cookie string, userAgent string, ok bool) {
	if account == nil || account.VerificationCookieEncrypted == "" ||
		account.VerificationExpiresAt == nil || !account.VerificationExpiresAt.After(time.Now()) {
		return "", "", false
	}
	if !sameOptionalInt(account.VerificationProxyConfigID, proxyConfigID) {
		return "", "", false
	}
	if strings.TrimSpace(account.VerificationClashNode) != strings.TrimSpace(clashNode) {
		return "", "", false
	}
	value, err := DecryptSecret(account.VerificationCookieEncrypted)
	if err != nil || value == "" {
		return "", "", false
	}
	return value, account.VerificationUserAgent, true
}

func verificationTokenHash(token string) string {
	value := sha256.Sum256([]byte(token))
	return hex.EncodeToString(value[:])
}

func normalizeCookieHeader(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "cookie:") {
		value = value[len("cookie:"):]
	}
	return strings.TrimSpace(value)
}

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func completeVerificationSession(
	ctx context.Context,
	sessionID int64,
	cookie string,
	userAgent string,
	source string,
) (*model.VerificationSession, error) {
	cookie = normalizeCookieHeader(cookie)
	if sessionID <= 0 || cookie == "" {
		return nil, fmt.Errorf("session id and cookie are required")
	}
	now := time.Now()
	var session model.VerificationSession
	var completionErr error
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&session, sessionID).Error; err != nil {
			return fmt.Errorf("verification session not found")
		}
		if !session.ExpiresAt.After(now) {
			if err := expireVerificationSession(tx, &session, now); err != nil {
				return err
			}
			completionErr = errVerificationSessionExpired
			return nil
		}
		effectiveUserAgent, err := resolveVerificationUserAgent(session.UserAgent, userAgent)
		if err != nil {
			return err
		}
		installed, err := completeVerificationSessionRecord(
			tx,
			&session,
			cookie,
			effectiveUserAgent,
			source,
			now,
		)
		if err != nil {
			return err
		}
		if !installed {
			completionErr = errVerificationSessionSuperseded
			return tx.Model(&model.VerificationTask{}).
				Where("session_id = ? AND status IN ?", session.ID, []model.VerificationTaskStatus{
					model.VerificationTaskPending,
					model.VerificationTaskClaimed,
				}).
				Updates(canceledVerificationTaskFields()).Error
		}
		return tx.Model(&model.VerificationTask{}).
			Where("session_id = ? AND status IN ?", session.ID, []model.VerificationTaskStatus{
				model.VerificationTaskPending,
				model.VerificationTaskClaimed,
			}).
			Updates(map[string]any{
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

func expireVerificationSession(tx *gorm.DB, session *model.VerificationSession, now time.Time) error {
	if tx == nil || session == nil {
		return fmt.Errorf("verification session is required")
	}
	hadCredential := session.CookieEncrypted != ""
	session.Status = model.VerificationSessionExpired
	session.CookieEncrypted = ""
	if err := tx.Save(session).Error; err != nil {
		return err
	}
	if hadCredential {
		if err := tx.Model(&model.SiteAccount{}).
			Where(
				"id = ? AND verification_session_fence_id = ?",
				session.SiteAccountID,
				session.ID,
			).
			Updates(clearedVerificationCredentialFields()).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&model.VerificationTask{}).
		Where("session_id = ? AND status IN ?", session.ID, []model.VerificationTaskStatus{
			model.VerificationTaskPending,
			model.VerificationTaskClaimed,
		}).
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
	return tx.Model(&model.VerificationTask{}).
		Where("session_id = ? AND retry_status IN ?", session.ID, []model.VerificationRetryStatus{
			model.VerificationRetryPending,
			model.VerificationRetryRunning,
		}).
		Updates(map[string]any{
			"retry_status":     model.VerificationRetryCanceled,
			"retry_token_hash": "",
		}).Error
}

func VerificationSessionCleanup(ctx context.Context, now time.Time) (int64, error) {
	var cleaned int64
	var expiredSessionIDs []int64
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := refreshVerificationTaskClaims(tx, now); err != nil {
			return err
		}
		var sessions []model.VerificationSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"(status IN ? AND expires_at <= ?) OR (status = ? AND cookie_encrypted <> '')",
				[]model.VerificationSessionStatus{
					model.VerificationSessionPending,
					model.VerificationSessionCompleted,
				},
				now,
				model.VerificationSessionExpired,
			).
			Find(&sessions).Error; err != nil {
			return err
		}
		for index := range sessions {
			if err := expireVerificationSession(tx, &sessions[index], now); err != nil {
				return err
			}
			expiredSessionIDs = append(expiredSessionIDs, sessions[index].ID)
			cleaned++
		}
		return nil
	})
	if err == nil {
		for _, sessionID := range expiredSessionIDs {
			defaultVerificationBrowserBroker.cancelSession(
				sessionID,
				fmt.Errorf("verification session expired"),
			)
		}
	}
	return cleaned, err
}

func cancelVerificationTasksForSession(tx *gorm.DB, sessionID int64) error {
	return tx.Model(&model.VerificationTask{}).
		Where("session_id = ? AND status IN ?", sessionID, []model.VerificationTaskStatus{
			model.VerificationTaskPending,
			model.VerificationTaskClaimed,
		}).
		Updates(canceledVerificationTaskFields()).Error
}

func canceledVerificationTaskFields() map[string]any {
	return map[string]any{
		"status":           model.VerificationTaskCanceled,
		"claim_token_hash": "",
		"pairing_id":       nil,
		"claimed_at":       nil,
		"retry_status":     model.VerificationRetryCanceled,
		"retry_token_hash": "",
	}
}

func cancelVerificationRetryForSession(tx *gorm.DB, sessionID int64) error {
	return tx.Model(&model.VerificationTask{}).
		Where("session_id = ? AND retry_status IN ?", sessionID, []model.VerificationRetryStatus{
			model.VerificationRetryPending,
			model.VerificationRetryRunning,
		}).
		Updates(map[string]any{
			"retry_status":     model.VerificationRetryCanceled,
			"retry_token_hash": "",
		}).Error
}

func clearedVerificationCredentialFields() map[string]any {
	return map[string]any{
		"verification_cookie_encrypted": "",
		"verification_user_agent":       "",
		"verification_proxy_config_id":  nil,
		"verification_clash_node":       "",
		"verification_expires_at":       nil,
	}
}

func resolveVerificationUserAgent(bound string, supplied string) (string, error) {
	bound = strings.TrimSpace(bound)
	supplied = strings.TrimSpace(supplied)
	if bound != "" && supplied != "" && bound != supplied {
		return "", fmt.Errorf("verification user agent does not match the session binding")
	}
	if supplied != "" {
		return supplied, nil
	}
	return bound, nil
}

func cloneOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func verificationRetryStatusForOperation(operation model.SiteOperationType) model.VerificationRetryStatus {
	switch operation {
	case model.SiteOperationSync, model.SiteOperationCheckin:
		return model.VerificationRetryPending
	default:
		return model.VerificationRetryNone
	}
}
