package sitesync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

const agentRouterSessionCookie = "session=agent-session"

// newAgentRouterServer 模拟 AgentRouter：没有签到接口，登录下发 session cookie 并在
// data.checked_in 里告知是否新增额度；管理接口要求 session cookie + 正确的 New-Api-User。
func newAgentRouterServer(t *testing.T, checkedIn *bool, logins *int, wrongUserIDs *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/user/login" {
			*logins++
			var body struct{ Username, Password string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Username != "agent-user" || body.Password != "agent-pass" {
				_, _ = w.Write([]byte(`{"success":false,"message":"用户名或密码错误"}`))
				return
			}
			w.Header().Add("Set-Cookie", agentRouterSessionCookie+"; Path=/; HttpOnly")
			_, _ = fmt.Fprintf(w, `{"success":true,"message":"","data":{"id":4242,"username":"agent-user","checked_in":%t}}`, *checkedIn)
			return
		}
		if r.URL.Path == "/models" || r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
			return
		}
		if userID := r.Header.Get("New-API-User"); userID != "" && userID != "4242" {
			*wrongUserIDs = append(*wrongUserIDs, r.URL.Path+"="+userID)
		}
		if !strings.Contains(r.Header.Get("Cookie"), agentRouterSessionCookie) || r.Header.Get("New-API-User") != "4242" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未登录且未提供 access token"}`))
			return
		}
		switch r.URL.Path {
		case "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"}]}`))
		case "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":4242,"quota":12500000,"used_quota":500000}}`))
		case "/api/log/self":
			if r.URL.Query().Get("type") == "4" {
				_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"quota":12500000}],"total":1}}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[],"total":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCheckinAgentRouterIsLoginSideEffect(t *testing.T) {
	checkedIn, logins := true, 0
	var wrongUserIDs []string
	server := newAgentRouterServer(t, &checkedIn, &logins, &wrongUserIDs)
	site := &model.Site{BaseURL: server.URL, Platform: model.SitePlatformAgentRouter}
	ctx := context.Background()

	// Access Token 账号额外填了用户名密码：登录即签到，但不能用 session cookie 覆盖系统访问令牌
	tokenAccount := &model.SiteAccount{CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "system-token", Username: "agent-user", Password: "agent-pass"}
	result, resolvedToken, err := checkinAccountState(ctx, site, tokenAccount)
	if err != nil || result.Status != model.SiteExecutionStatusSuccess || !strings.Contains(result.Message, "签到成功") {
		t.Fatalf("expected login to check in: result=%+v err=%v", result, err)
	}
	if resolvedToken != "" {
		t.Fatalf("access token account must keep its stored token, got %q", resolvedToken)
	}

	// 已签到过：登录成功但 checked_in=false
	checkedIn = false
	result, _, err = checkinAccountState(ctx, site, tokenAccount)
	if err != nil || result.Status != model.SiteExecutionStatusSuccess || !strings.Contains(result.Message, "没有新增额度") {
		t.Fatalf("expected already-checked-in result: result=%+v err=%v", result, err)
	}

	// 用户名密码凭据：session cookie 就是它的 access token
	passwordAccount := &model.SiteAccount{CredentialType: model.SiteCredentialTypeUsernamePassword, Username: "agent-user", Password: "agent-pass"}
	if _, resolvedToken, err = checkinAccountState(ctx, site, passwordAccount); err != nil || !strings.Contains(resolvedToken, agentRouterSessionCookie) {
		t.Fatalf("expected session cookie as resolved token, got %q err=%v", resolvedToken, err)
	}

	// 密码错误：报错而不是假装签到成功
	if _, _, err = checkinAccountState(ctx, site, &model.SiteAccount{CredentialType: model.SiteCredentialTypeUsernamePassword, Username: "agent-user", Password: "wrong"}); err == nil {
		t.Fatal("expected login failure to surface as error")
	}

	// 只有 Access Token：触发不了登录，跳过且不发任何请求
	logins = 0
	result, _, err = checkinAccountState(ctx, site, &model.SiteAccount{CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "system-token"})
	if err != nil || result.Status != model.SiteExecutionStatusSkipped || logins != 0 {
		t.Fatalf("expected skip without login: result=%+v err=%v logins=%d", result, err, logins)
	}
}

// 用户名密码账号：同步走 New API 流程，用登录拿到的 session cookie + 登录响应里的用户 ID，不探测 ID。
func TestSyncAgentRouterWithPasswordUsesSessionCookieAndLoginUserID(t *testing.T) {
	checkedIn, logins := false, 0
	var wrongUserIDs []string
	server := newAgentRouterServer(t, &checkedIn, &logins, &wrongUserIDs)

	snapshot, err := syncAccountState(context.Background(), &model.Site{
		BaseURL:  server.URL,
		Platform: model.SitePlatformAgentRouter,
	}, &model.SiteAccount{
		Name:           "agent-user",
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "agent-user",
		Password:       "agent-pass",
	})
	if err != nil {
		t.Fatalf("syncAccountState returned error: %v", err)
	}
	if len(snapshot.tokens) != 1 || snapshot.tokens[0].Token != "managed-key" {
		t.Fatalf("unexpected synced tokens: %+v", snapshot.tokens)
	}
	if len(snapshot.models) != 1 || snapshot.models[0].ModelName != "gpt-4o-mini" {
		t.Fatalf("unexpected synced models: %+v", snapshot.models)
	}
	if !strings.Contains(snapshot.accessToken, agentRouterSessionCookie) {
		t.Fatalf("expected session cookie as access token, got %q", snapshot.accessToken)
	}
	if snapshot.balance != 25 || snapshot.balanceUsed != 1 {
		t.Fatalf("unexpected balance: balance=%v used=%v", snapshot.balance, snapshot.balanceUsed)
	}
	if snapshot.todayIncome != 25 {
		t.Fatalf("income logs must be fetched with the session cookie, got today income %v", snapshot.todayIncome)
	}
	if logins != 1 {
		t.Fatalf("expected exactly one login per sync, got %d", logins)
	}
	if len(wrongUserIDs) != 0 {
		t.Fatalf("user id must come from the login response, not probing; got requests %v", wrongUserIDs)
	}
}

// 余额接口被拒时不探测用户 ID：这个 ID 来自登录响应或已在本次同步里用过，换 ID 救不回来，只会多发几十个请求。
func TestAgentRouterBalanceFailureDoesNotProbeUserIDs(t *testing.T) {
	var probedUserIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userID := r.Header.Get("New-API-User"); userID != "4242" {
			probedUserIDs = append(probedUserIDs, userID)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
	}))
	defer server.Close()

	site := &model.Site{BaseURL: server.URL, Platform: model.SitePlatformAgentRouter}
	fetchSiteAccountBalance(context.Background(), site, &model.SiteAccount{}, agentRouterSessionCookie, 4242)
	if len(probedUserIDs) != 0 {
		t.Fatalf("expected no user id probing, got %v", probedUserIDs)
	}
}

func TestDetectAgentRouterByTitle(t *testing.T) {
	if got := matchTitlePlatform("<html><head><title>Agent Router</title></head></html>"); got != model.SitePlatformAgentRouter {
		t.Fatalf("expected agentrouter, got %q", got)
	}
}
