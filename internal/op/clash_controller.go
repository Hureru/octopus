package op

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

type ClashControllerInput struct {
	ID        int    `json:"id,omitempty"`
	Name      string `json:"name"`
	APIURL    string `json:"api_url"`
	ProxyURL  string `json:"proxy_url"`
	GroupName string `json:"group_name"`
	Secret    string `json:"secret,omitempty"`
	Enabled   bool   `json:"enabled"`
}

type ClashGroupState struct {
	Now string   `json:"now"`
	All []string `json:"all"`
}

var clashControllerLocks sync.Map

const (
	clashSwitchLeaseTTL        = 90 * time.Second
	clashSwitchLeaseRetryDelay = 50 * time.Millisecond
	clashSwitchConfirmAttempts = 5
	clashSwitchConfirmDelay    = 100 * time.Millisecond
	clashControllerHTTPTimeout = 15 * time.Second
)

func ClashControllerList(ctx context.Context) ([]model.ClashController, error) {
	var items []model.ClashController
	err := db.GetDB().WithContext(ctx).Order("id ASC").Find(&items).Error
	return items, err
}

func ClashControllerGet(ctx context.Context, id int) (*model.ClashController, error) {
	var item model.ClashController
	if err := db.GetDB().WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, fmt.Errorf("clash controller not found")
	}
	return &item, nil
}

func ClashControllerUpsert(ctx context.Context, input ClashControllerInput) (*model.ClashController, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.APIURL = strings.TrimRight(strings.TrimSpace(input.APIURL), "/")
	input.ProxyURL = strings.TrimSpace(input.ProxyURL)
	input.GroupName = strings.TrimSpace(input.GroupName)
	if input.Name == "" || input.APIURL == "" || input.ProxyURL == "" || input.GroupName == "" {
		return nil, fmt.Errorf("name, api_url, proxy_url and group_name are required")
	}
	if err := validateClashDedicatedGroup(input.GroupName); err != nil {
		return nil, err
	}
	if err := validateHTTPURL(input.APIURL); err != nil {
		return nil, fmt.Errorf("invalid api_url: %w", err)
	}
	normalizedProxy, err := model.NormalizeProxyURL(input.ProxyURL)
	if err != nil {
		return nil, err
	}
	input.ProxyURL = normalizedProxy

	item := model.ClashController{
		ID:        input.ID,
		Name:      input.Name,
		APIURL:    input.APIURL,
		ProxyURL:  input.ProxyURL,
		GroupName: input.GroupName,
		Enabled:   input.Enabled,
	}
	if input.ID > 0 {
		existing, err := ClashControllerGet(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		item.SecretEncrypted = existing.SecretEncrypted
	}
	if input.Secret != "" {
		encrypted, err := EncryptSecret(input.Secret)
		if err != nil {
			return nil, err
		}
		item.SecretEncrypted = encrypted
	}
	if input.ID > 0 {
		if err := db.GetDB().WithContext(ctx).Save(&item).Error; err != nil {
			return nil, err
		}
	} else {
		if err := db.GetDB().WithContext(ctx).Create(&item).Error; err != nil {
			return nil, err
		}
	}
	return ClashControllerGet(ctx, item.ID)
}

func ClashControllerDelete(ctx context.Context, id int) error {
	var count int64
	if err := db.GetDB().WithContext(ctx).Model(&model.ProxyConfiguration{}).
		Where("clash_controller_id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("clash controller is still referenced")
	}
	return db.GetDB().WithContext(ctx).Delete(&model.ClashController{}, id).Error
}

func ClashControllerState(ctx context.Context, id int) (ClashGroupState, error) {
	controller, err := ClashControllerGet(ctx, id)
	if err != nil {
		return ClashGroupState{}, err
	}
	return fetchClashGroupState(ctx, controller)
}

func ClashSwitchNode(ctx context.Context, id int, node string) error {
	release, err := ClashSwitchNodeForOperation(ctx, id, node)
	if release != nil {
		defer release()
	}
	return err
}

// ClashSwitchNodeForOperation keeps the controller/group guard until the
// returned release function is called. Site recovery uses it to prevent
// another operation from changing the selected node while traffic is in flight.
func ClashSwitchNodeForOperation(
	ctx context.Context,
	id int,
	node string,
) (release func(), err error) {
	controller, err := ClashControllerGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := validateClashDedicatedGroup(controller.GroupName); err != nil {
		return nil, err
	}
	release, err = acquireClashOperationGuard(ctx, controller)
	if err != nil {
		return nil, err
	}
	if err := switchClashNode(ctx, controller, node); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func validateClashDedicatedGroup(groupName string) error {
	if strings.EqualFold(strings.TrimSpace(groupName), "GLOBAL") {
		return fmt.Errorf("group_name must be a dedicated proxy group, not GLOBAL")
	}
	return nil
}

func fetchClashGroupState(ctx context.Context, controller *model.ClashController) (ClashGroupState, error) {
	if controller == nil || !controller.Enabled {
		return ClashGroupState{}, fmt.Errorf("clash controller is disabled")
	}
	httpClient, err := newClashControllerHTTPClient()
	if err != nil {
		return ClashGroupState{}, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		controller.APIURL+"/proxies/"+url.PathEscape(controller.GroupName),
		nil,
	)
	if err != nil {
		return ClashGroupState{}, err
	}
	if err := applyClashAuthorization(request, controller); err != nil {
		return ClashGroupState{}, err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return ClashGroupState{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return ClashGroupState{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ClashGroupState{}, fmt.Errorf("clash controller returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Now string   `json:"now"`
		All []string `json:"all"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ClashGroupState{}, err
	}
	if len(payload.All) == 0 {
		return ClashGroupState{}, fmt.Errorf("configured clash group is not a selector or has no nodes")
	}
	return ClashGroupState{Now: payload.Now, All: payload.All}, nil
}

func switchClashNode(ctx context.Context, controller *model.ClashController, node string) error {
	node = strings.TrimSpace(node)
	if controller == nil || node == "" {
		return fmt.Errorf("clash controller and node are required")
	}
	state, err := fetchClashGroupState(ctx, controller)
	if err != nil {
		return err
	}
	found := false
	for _, candidate := range state.All {
		if candidate == node {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("clash node %q is not in group %q", node, controller.GroupName)
	}
	if state.Now == node {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"name": node})
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		controller.APIURL+"/proxies/"+url.PathEscape(controller.GroupName),
		bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if err := applyClashAuthorization(request, controller); err != nil {
		return err
	}
	httpClient, err := newClashControllerHTTPClient()
	if err != nil {
		return err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("switch clash node failed: %d %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	for attempt := 0; attempt < clashSwitchConfirmAttempts; attempt++ {
		confirmed, confirmErr := fetchClashGroupState(ctx, controller)
		if confirmErr == nil && confirmed.Now == node {
			return nil
		}
		if attempt == clashSwitchConfirmAttempts-1 {
			if confirmErr != nil {
				return fmt.Errorf("confirm clash node switch: %w", confirmErr)
			}
			return fmt.Errorf("clash controller did not confirm node %q", node)
		}
		timer := time.NewTimer(clashSwitchConfirmDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func acquireClashOperationGuard(
	ctx context.Context,
	controller *model.ClashController,
) (func(), error) {
	if controller == nil {
		return nil, fmt.Errorf("clash controller is required")
	}
	created := make(chan struct{}, 1)
	created <- struct{}{}
	lockValue, _ := clashControllerLocks.LoadOrStore(controller.ID, created)
	lock := lockValue.(chan struct{})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock:
	}
	lease, err := acquireClashSwitchLease(ctx, controller)
	if err != nil {
		lock <- struct{}{}
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := releaseClashSwitchLease(releaseCtx, lease); err != nil {
				log.Warnw(
					"release clash switch lease failed",
					"lease_key", lease.key,
					"error", err,
				)
			}
			lock <- struct{}{}
		})
	}, nil
}

type clashSwitchLeaseHandle struct {
	key   string
	owner string
}

func acquireClashSwitchLease(
	ctx context.Context,
	controller *model.ClashController,
) (*clashSwitchLeaseHandle, error) {
	if controller == nil || controller.ID <= 0 {
		return nil, fmt.Errorf("clash controller is required")
	}
	owner, err := randomHexToken(24)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%d:%s", controller.ID, strings.ToLower(strings.TrimSpace(controller.GroupName)))
	for {
		now := time.Now()
		expiresAt := now.Add(clashSwitchLeaseTTL)
		result := db.GetDB().WithContext(ctx).Model(&model.ClashSwitchLease{}).
			Where("lease_key = ? AND expires_at <= ?", key, now).
			Updates(map[string]any{
				"owner_token": owner,
				"expires_at":  expiresAt,
				"updated_at":  now,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			return &clashSwitchLeaseHandle{key: key, owner: owner}, nil
		}

		lease := model.ClashSwitchLease{
			LeaseKey: key, OwnerToken: owner, ExpiresAt: expiresAt, UpdatedAt: now,
		}
		if createErr := db.GetDB().WithContext(ctx).Create(&lease).Error; createErr == nil {
			return &clashSwitchLeaseHandle{key: key, owner: owner}, nil
		} else {
			var count int64
			if countErr := db.GetDB().WithContext(ctx).Model(&model.ClashSwitchLease{}).
				Where("lease_key = ?", key).Count(&count).Error; countErr != nil {
				return nil, createErr
			}
			if count == 0 {
				return nil, createErr
			}
		}

		timer := time.NewTimer(clashSwitchLeaseRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func releaseClashSwitchLease(ctx context.Context, lease *clashSwitchLeaseHandle) error {
	if lease == nil {
		return nil
	}
	return db.GetDB().WithContext(ctx).
		Where("lease_key = ? AND owner_token = ?", lease.key, lease.owner).
		Delete(&model.ClashSwitchLease{}).Error
}

func randomHexToken(size int) (string, error) {
	if size <= 0 {
		size = 24
	}
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func newClashControllerHTTPClient() (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}
	cloned := transport.Clone()
	cloned.Proxy = nil
	return &http.Client{
		Transport: cloned,
		Timeout:   clashControllerHTTPTimeout,
	}, nil
}

func applyClashAuthorization(request *http.Request, controller *model.ClashController) error {
	if request == nil || controller == nil || controller.SecretEncrypted == "" {
		return nil
	}
	secret, err := DecryptSecret(controller.SecretEncrypted)
	if err != nil {
		return err
	}
	if secret != "" {
		request.Header.Set("Authorization", "Bearer "+secret)
	}
	return nil
}

func validateHTTPURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("host is required")
	}
	return nil
}

func ClashControllerProxyConfiguration(ctx context.Context, controllerID int) (*model.ProxyConfiguration, error) {
	controller, err := ClashControllerGet(ctx, controllerID)
	if err != nil {
		return nil, err
	}
	var item model.ProxyConfiguration
	err = db.GetDB().WithContext(ctx).Where("clash_controller_id = ?", controllerID).First(&item).Error
	if err == nil {
		return &item, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	item = model.ProxyConfiguration{
		Name:              "Clash - " + controller.Name,
		URL:               controller.ProxyURL,
		ClashControllerID: &controller.ID,
		Enabled:           true,
		Remark:            "Managed by Clash controller",
	}
	if err := ProxyConfigurationCreate(&item, ctx); err != nil {
		return nil, err
	}
	return &item, nil
}
