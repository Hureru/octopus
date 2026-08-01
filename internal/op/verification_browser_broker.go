package op

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	verificationBrowserRequestMaxBytes  = 256 << 10
	verificationBrowserResponseMaxBytes = 4 << 20
	verificationBrowserHeaderMaxBytes   = 64 << 10
	verificationBrowserRequestTimeout   = 55 * time.Second
	verificationBrowserPollTimeout      = 20 * time.Second
)

type VerificationBrowserBinding struct {
	PairingID int64  `json:"-"`
	TaskID    int64  `json:"-"`
	SessionID int64  `json:"-"`
	TargetURL string `json:"-"`
}

type VerificationBrowserRequestInput struct {
	Binding VerificationBrowserBinding `json:"-"`
	Method  string                     `json:"method"`
	URL     string                     `json:"url"`
	Headers map[string]string          `json:"headers,omitempty"`
	Body    string                     `json:"body,omitempty"`
}

type VerificationBrowserRequestClaimed struct {
	RequestID    string            `json:"request_id"`
	RequestToken string            `json:"request_token"`
	TaskID       int64             `json:"task_id"`
	SessionID    int64             `json:"session_id"`
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

type VerificationBrowserRequestCompletion struct {
	RequestID    string            `json:"request_id"`
	RequestToken string            `json:"request_token"`
	Status       int               `json:"status"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	FinalURL     string            `json:"final_url,omitempty"`
	Error        string            `json:"error,omitempty"`
}

type VerificationBrowserResponse struct {
	Status   int
	Headers  map[string]string
	Body     string
	FinalURL string
}

type verificationBrowserPending struct {
	pairingID  int64
	targetURL  string
	claimed    bool
	token      string
	tokenHash  string
	request    VerificationBrowserRequestClaimed
	responseCh chan verificationBrowserResult
}

type verificationBrowserResult struct {
	response *VerificationBrowserResponse
	err      error
}

type verificationBrowserBroker struct {
	mu      sync.Mutex
	pending map[string]*verificationBrowserPending
	notify  map[int64]chan struct{}
}

var defaultVerificationBrowserBroker = newVerificationBrowserBroker()

func newVerificationBrowserBroker() *verificationBrowserBroker {
	return &verificationBrowserBroker{
		pending: make(map[string]*verificationBrowserPending),
		notify:  make(map[int64]chan struct{}),
	}
}

func VerificationBrowserRequest(
	ctx context.Context,
	input VerificationBrowserRequestInput,
) (*VerificationBrowserResponse, error) {
	return defaultVerificationBrowserBroker.request(ctx, input)
}

func VerificationBrowserRequestClaim(
	ctx context.Context,
	pairingToken string,
) (*VerificationBrowserRequestClaimed, error) {
	pairing, err := verificationBridgePairingByToken(ctx, pairingToken)
	if err != nil {
		return nil, err
	}
	pollCtx, cancel := context.WithTimeout(ctx, verificationBrowserPollTimeout)
	defer cancel()
	request, err := defaultVerificationBrowserBroker.claim(pollCtx, pairing.ID)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return nil, nil
	}
	return request, err
}

func VerificationBrowserRequestComplete(
	ctx context.Context,
	pairingToken string,
	completion VerificationBrowserRequestCompletion,
) error {
	pairing, err := verificationBridgePairingByToken(ctx, pairingToken)
	if err != nil {
		return err
	}
	return defaultVerificationBrowserBroker.complete(pairing.ID, completion)
}

func (broker *verificationBrowserBroker) request(
	ctx context.Context,
	input VerificationBrowserRequestInput,
) (*VerificationBrowserResponse, error) {
	if err := validateVerificationBrowserRequest(input); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, verificationBrowserRequestTimeout)
	defer cancel()

	requestID, err := randomHexToken(16)
	if err != nil {
		return nil, err
	}
	requestToken, err := randomHexToken(24)
	if err != nil {
		return nil, err
	}
	pending := &verificationBrowserPending{
		pairingID: input.Binding.PairingID,
		targetURL: input.Binding.TargetURL,
		token:     requestToken,
		tokenHash: verificationTokenHash(requestToken),
		request: VerificationBrowserRequestClaimed{
			RequestID: requestID,
			TaskID:    input.Binding.TaskID,
			SessionID: input.Binding.SessionID,
			Method:    strings.ToUpper(strings.TrimSpace(input.Method)),
			URL:       strings.TrimSpace(input.URL),
			Headers:   cloneStringMap(input.Headers),
			Body:      input.Body,
			ExpiresAt: verificationBrowserRequestExpiry(requestCtx),
		},
		responseCh: make(chan verificationBrowserResult, 1),
	}

	broker.mu.Lock()
	broker.pending[requestID] = pending
	notify := broker.notifyChannel(input.Binding.PairingID)
	broker.mu.Unlock()
	select {
	case notify <- struct{}{}:
	default:
	}

	select {
	case result := <-pending.responseCh:
		return result.response, result.err
	case <-requestCtx.Done():
		broker.removePending(requestID, pending)
		return nil, requestCtx.Err()
	}
}

func (broker *verificationBrowserBroker) claim(
	ctx context.Context,
	pairingID int64,
) (*VerificationBrowserRequestClaimed, error) {
	if pairingID <= 0 {
		return nil, fmt.Errorf("verification browser pairing is required")
	}
	for {
		broker.mu.Lock()
		for _, pending := range broker.pending {
			if pending.pairingID != pairingID || pending.claimed {
				continue
			}
			pending.claimed = true
			pending.request.RequestToken = pending.token
			pending.token = ""
			request := pending.request
			broker.mu.Unlock()
			return &request, nil
		}
		notify := broker.notifyChannel(pairingID)
		broker.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-notify:
		}
	}
}

func (broker *verificationBrowserBroker) complete(
	pairingID int64,
	completion VerificationBrowserRequestCompletion,
) error {
	if err := validateVerificationBrowserCompletion(completion); err != nil {
		return err
	}
	broker.mu.Lock()
	pending := broker.pending[completion.RequestID]
	if pending == nil {
		broker.mu.Unlock()
		return fmt.Errorf("verification browser request not found or already consumed")
	}
	if pending.pairingID != pairingID {
		broker.mu.Unlock()
		return fmt.Errorf("verification browser request belongs to another pairing")
	}
	if !pending.claimed ||
		pending.tokenHash != verificationTokenHash(strings.TrimSpace(completion.RequestToken)) {
		broker.mu.Unlock()
		return fmt.Errorf("verification browser request token is invalid")
	}
	if completion.FinalURL != "" &&
		!sameVerificationBrowserOrigin(pending.targetURL, completion.FinalURL) {
		broker.mu.Unlock()
		return fmt.Errorf("verification browser response left the task target origin")
	}
	delete(broker.pending, completion.RequestID)
	broker.mu.Unlock()

	result := verificationBrowserResult{
		response: &VerificationBrowserResponse{
			Status:   completion.Status,
			Headers:  cloneStringMap(completion.Headers),
			Body:     completion.Body,
			FinalURL: completion.FinalURL,
		},
	}
	if message := strings.TrimSpace(completion.Error); message != "" {
		result.response = nil
		result.err = fmt.Errorf("browser request failed: %s", message)
	}
	pending.responseCh <- result
	return nil
}

func (broker *verificationBrowserBroker) cancelPairing(pairingID int64, err error) {
	broker.cancelMatching(
		func(pending *verificationBrowserPending) bool {
			return pending.pairingID == pairingID
		},
		err,
	)
}

func (broker *verificationBrowserBroker) cancelSession(sessionID int64, err error) {
	broker.cancelMatching(
		func(pending *verificationBrowserPending) bool {
			return pending.request.SessionID == sessionID
		},
		err,
	)
}

func (broker *verificationBrowserBroker) cancelMatching(
	match func(*verificationBrowserPending) bool,
	err error,
) {
	if err == nil {
		err = fmt.Errorf("verification browser request canceled")
	}
	broker.mu.Lock()
	items := make([]*verificationBrowserPending, 0)
	for requestID, pending := range broker.pending {
		if !match(pending) {
			continue
		}
		delete(broker.pending, requestID)
		items = append(items, pending)
	}
	broker.mu.Unlock()
	for _, pending := range items {
		pending.responseCh <- verificationBrowserResult{err: err}
	}
}

func (broker *verificationBrowserBroker) notifyChannel(pairingID int64) chan struct{} {
	channel := broker.notify[pairingID]
	if channel == nil {
		channel = make(chan struct{}, 1)
		broker.notify[pairingID] = channel
	}
	return channel
}

func (broker *verificationBrowserBroker) removePending(
	requestID string,
	expected *verificationBrowserPending,
) {
	broker.mu.Lock()
	if broker.pending[requestID] == expected {
		delete(broker.pending, requestID)
	}
	broker.mu.Unlock()
}

func validateVerificationBrowserRequest(input VerificationBrowserRequestInput) error {
	if input.Binding.PairingID <= 0 ||
		input.Binding.TaskID <= 0 ||
		input.Binding.SessionID <= 0 {
		return fmt.Errorf("verification browser request binding is incomplete")
	}
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return fmt.Errorf("verification browser request method is not allowed")
	}
	if !sameVerificationBrowserOrigin(input.Binding.TargetURL, input.URL) {
		return fmt.Errorf("verification browser request is outside the task target origin")
	}
	if len(input.Body) > verificationBrowserRequestMaxBytes {
		return fmt.Errorf("verification browser request body is too large")
	}
	if headerMapBytes(input.Headers) > verificationBrowserHeaderMaxBytes {
		return fmt.Errorf("verification browser request headers are too large")
	}
	for name := range input.Headers {
		if verificationBrowserHeaderForbidden(name) {
			return fmt.Errorf("verification browser request contains a forbidden header")
		}
	}
	return nil
}

func validateVerificationBrowserCompletion(
	completion VerificationBrowserRequestCompletion,
) error {
	if strings.TrimSpace(completion.RequestID) == "" ||
		strings.TrimSpace(completion.RequestToken) == "" {
		return fmt.Errorf("verification browser request id and token are required")
	}
	if completion.Error == "" &&
		(completion.Status < 100 || completion.Status > 599) {
		return fmt.Errorf("verification browser response status is invalid")
	}
	if completion.Error == "" && strings.TrimSpace(completion.FinalURL) == "" {
		return fmt.Errorf("verification browser response final url is required")
	}
	if len(completion.Body) > verificationBrowserResponseMaxBytes {
		return fmt.Errorf("verification browser response body is too large")
	}
	if len(completion.Error) > verificationRetryMessageMax {
		return fmt.Errorf("verification browser response error is too large")
	}
	if headerMapBytes(completion.Headers) > verificationBrowserHeaderMaxBytes {
		return fmt.Errorf("verification browser response headers are too large")
	}
	return nil
}

func sameVerificationBrowserOrigin(left string, right string) bool {
	leftOrigin, leftErr := normalizedVerificationBrowserOrigin(left)
	rightOrigin, rightErr := normalizedVerificationBrowserOrigin(right)
	return leftErr == nil && rightErr == nil && leftOrigin == rightOrigin
}

func normalizedVerificationBrowserOrigin(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("invalid verification browser origin")
	}
	scheme := strings.ToLower(parsed.Scheme)
	port := parsed.Port()
	switch scheme {
	case "http":
		if port == "" {
			port = "80"
		}
	case "https":
		if port == "" {
			port = "443"
		}
	default:
		return "", fmt.Errorf("invalid verification browser origin")
	}
	return scheme + "://" + net.JoinHostPort(strings.ToLower(parsed.Hostname()), port), nil
}

func verificationBrowserHeaderForbidden(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "",
		"accept-charset",
		"accept-encoding",
		"access-control-request-headers",
		"access-control-request-method",
		"connection",
		"content-length",
		"cookie",
		"date",
		"dnt",
		"expect",
		"host",
		"keep-alive",
		"origin",
		"permissions-policy",
		"referer",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade",
		"user-agent",
		"via":
		return true
	default:
		return strings.HasPrefix(name, "proxy-") ||
			strings.HasPrefix(name, "sec-") ||
			strings.HasPrefix(name, "x-http-method")
	}
}

func headerMapBytes(headers map[string]string) int {
	total := 0
	for name, value := range headers {
		total += len(name) + len(value)
	}
	return total
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func verificationBrowserRequestExpiry(ctx context.Context) time.Time {
	expiresAt := time.Now().Add(verificationBrowserRequestTimeout)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(expiresAt) {
		return deadline
	}
	return expiresAt
}
