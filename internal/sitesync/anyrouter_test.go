package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

const (
	anyRouterChallengeACW    = "699dbedad126579b6bc0ebb91eaae8d7af3548b5"
	anyRouterChallengeCookie = "challenge-seed"
	shieldLoginToken         = "login-session-token"
	cookieOnlySession        = "cookie-only-session"
	cookieShieldedToken      = "MTc3MTg2NDk3MHxkWE5sY201aGJXVTliR2x1ZFhoa2IxOHhNekU1TXpZPXxzaWc="
)

const anyRouterChallengeHTML = `<html><script>var arg1='B8B78DDA1D5CD2598AAEE9DD6975913AA490BAE3';(function(a,c){var G=a0j,d=a();while(!![]){try{var e=-parseInt(G(0x117))/0x1*(parseInt(G(0x111))/0x2)+-parseInt(G(0xfb))/0x3*(parseInt(G(0x10e))/0x4)+-parseInt(G(0x101))/0x5*(-parseInt(G(0xfd))/0x6)+-parseInt(G(0x102))/0x7*(parseInt(G(0x122))/0x8)+parseInt(G(0x112))/0x9+parseInt(G(0x11d))/0xa*(parseInt(G(0x11c))/0xb)+parseInt(G(0x114))/0xc;if(e===c)break;else d['push'](d['shift']());}catch(f){d['push'](d['shift']());}}}(a0i,0x760bf),!(function(){var L=a0j,j=(function(){var B=!![];return function(C,D){var E=B?function(){var H=a0j;if(D){var F=D[H(0x10d)](C,arguments);return D=null,F;}}:function(){};return B=![],E;};}()),k=j(this,function(){var I=a0j;return k[I(0xff)]()[I(0x123)](I(0x10f))[I(0xff)]()[I(0x107)](k)[I(0x123)](I(0x10f));});k();var l=(function(){var B=!![];return function(C,D){var E=B?function(){var J=a0j;if(D){var F=D[J(0x10d)](C,arguments);return D=null,F;}}:function(){};return B=![],E;};}());(function(){l(this,function(){var K=a0j,B=new RegExp(K(0x118)),C=new RegExp(K(0x106),'i'),D=b(K(0x100));!B[K(0x104)](D+K(0x105))||!C[K(0x104)](D+K(0x11b))?D('0'):b();})();}());for(var m=[0xf,0x23,0x1d,0x18,0x21,0x10,0x1,0x26,0xa,0x9,0x13,0x1f,0x28,0x1b,0x16,0x17,0x19,0xd,0x6,0xb,0x27,0x12,0x14,0x8,0xe,0x15,0x20,0x1a,0x2,0x1e,0x7,0x4,0x11,0x5,0x3,0x1c,0x22,0x25,0xc,0x24],p=L(0x115),q=[],u='',v='',w=L(0x116),x=0x0;x<arg1[w];x++)for(var y=arg1[x],z=0x0;z<m[w];z++)m[z]==x+0x1&&(q[z]=y);for(u=q[L(0xfc)](''),x=0x0;x<u[w]&&x<p[w];x+=0x2){var A=(parseInt(u[L(0x11a)](x,x+0x2),0x10)^parseInt(p[L(0x11a)](x,x+0x2),0x10))[L(0xff)](0x10);0x1==A[w]&&(A='0'+A),v+=A;}document[L(0x121)]='acw_sc__v2='+v+L(0x120)+new Date(Date[L(0x119)]()+0x36ee80)[L(0x10c)]()+L(0x109),document[L(0xfe)][L(0x103)]();}()));function a0i(){var N=['mJKZmgTStNvVyq','C3rYAw5N','y2fSBa','o2v4CgLYzxm9','y29VA2LL','mteZmZy3mNLbu2PszW','C2vHCMnO','D2HPBguGkhrYDwuPihT9','mJq1ndi0rKHuthnj','AM9PBG','nNH1rKHOuq','Bg9JyxrPB24','Dg9tDhjPBMC','Aw5PDa','mJi2odi1nwnMre1IyG','n0HxChPJva','CMvSB2fK','DgvZDa','y2HHAw4','xcTCkYaQkd86w2eTEKeTwL8KxvSWltLHlxPblvPFjf0Qkq','y29UC3rYDwn0B3i','y291BNrLCG','o21HEc1Hz2u9mZyWmdTWyxrOps87','zgvIDq','ywn0Aw9U','Dg9htvrtDhjPBMC','yxbWBhK','mJbiru1MChi','kcGOlISPkYKRksSK','z2DLCG','nKHJq01Aqq','nJe5nZu5ogH1twPUDa','C3rHDgvpyMPLy3q','mZu5mdu5mNbcB2Pxyq','mZaWmde3nJaWmdG1nJaWnJa2mtuWmtuZmZaWmZy5mdaYnZGWmdm3nq','BgvUz3rO','mtqWnti2shvUBNDv','zNvUy3rPB24GkLWOicPCkq','BM93','C2XPy2u','Aw5WDxq','ntm5BwrLuMXi'];a0i=function(){return N;};return a0i();}function a0j(a,b){var c=a0i();return a0j=function(d,e){d=d-0xfb;var f=c[d];if(a0j['tGHEKR']===undefined){var g=function(l){var m='abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/=';var n='',o='',p=n+g;for(var q=0x0,r,s,t=0x0;s=l['charAt'](t++);~s&&(r=q%0x4?r*0x40+s:s,q++%0x4)?n+=p['charCodeAt'](t+0xa)-0xa!==0x0?String['fromCharCode'](0xff&r>>(-0x2*q&0x6)):q:0x0){s=m['indexOf'](s);}for(var u=0x0,v=n['length'];u<v;u++){o+='%'+('00'+n['charCodeAt'](u)['toString'](0x10))['slice'](-0x2);}return decodeURIComponent(o);};a0j['CrMtTV']=g,a=arguments,a0j['tGHEKR']=!![];}var h=c[0x0],i=d+h,j=a[i];if(!j){var k=function(l){this['jyamLv']=l,this['WxwaRR']=[0x1,0x0,0x0],this['GuwnVk']=function(){return'newState';},this['CxWuMi']='\x5cw+\x20*\x5c(\x5c)\x20*{\x5cw+\x20*',this['FxAgTK']='[\x27|\x22].+[\x27|\x22];?\x20*}';};k['prototype']['dQvDam']=function(){var l=new RegExp(this['CxWuMi']+this['FxAgTK']),m=l['test'](this['GuwnVk']['toString']())?--this['WxwaRR'][0x1]:--this['WxwaRR'][0x0];return this['fQYikn'](m);},k['prototype']['fQYikn']=function(l){if(!Boolean(~l))return l;return this['Hbwxmd'](this['jyamLv']);},k['prototype']['Hbwxmd']=function(l){for(var m=0x0,n=this['WxwaRR']['length'];m<n;m++){this['WxwaRR']['push'](Math['round'](Math['random']())),n=this['WxwaRR']['length'];}return l(this['WxwaRR'][0x0]);},new k(a0j)['dQvDam'](),f=a0j['CrMtTV'](f),a[i]=f;}else f=j;return f;},a0j(a,b);}function b(a){function c(d){var M=a0j;if(typeof d===M(0x11e))return function(e){}[M(0x107)](M(0x124))[M(0x10d)](M(0x108));else(''+d/d)[M(0x116)]!==0x1||d%0x14===0x0?function(){return!![];}[M(0x107)](M(0x10a)+M(0x110))[M(0x11f)](M(0x10b)):function(){return![];}[M(0x107)](M(0x10a)+M(0x110))[M(0x10d)](M(0x113));c(++d);}try{if(a)return c;else c(0x0);}catch(d){}}</script></html>`

func TestResolveAnyRouterManagedAccessTokenSolvesShieldChallenge(t *testing.T) {
	var cookies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		if r.URL.Path != "/api/user/login" {
			http.NotFound(w, r)
			return
		}
		cookieHeader := r.Header.Get("Cookie")
		if !strings.Contains(cookieHeader, "acw_sc__v2="+anyRouterChallengeACW) {
			w.Header().Add("Set-Cookie", "cdn_sec_tc="+anyRouterChallengeCookie+"; Path=/; HttpOnly")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(anyRouterChallengeHTML))
			return
		}
		if !strings.Contains(cookieHeader, "cdn_sec_tc="+anyRouterChallengeCookie) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"message":"missing shield cookie"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"token":"` + shieldLoginToken + `"}}`))
	}))
	defer server.Close()

	token, err := resolveAnyRouterManagedAccessToken(context.Background(), &model.Site{
		BaseURL: server.URL,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "shield-user",
		Password:       "shield-pass",
	})
	if err != nil {
		t.Fatalf("resolveAnyRouterManagedAccessToken returned error: %v", err)
	}
	if token != shieldLoginToken {
		t.Fatalf("expected token %q, got %q", shieldLoginToken, token)
	}

	joined := strings.Join(cookies, "\n")
	if !strings.Contains(joined, "acw_sc__v2="+anyRouterChallengeACW) {
		t.Fatalf("expected retry cookie to include acw_sc__v2, cookies=%q", joined)
	}
	if !strings.Contains(joined, "cdn_sec_tc="+anyRouterChallengeCookie) {
		t.Fatalf("expected retry cookie to include cdn_sec_tc, cookies=%q", joined)
	}
}

func TestResolveAnyRouterManagedAccessTokenFallsBackToSessionCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/login" {
			http.NotFound(w, r)
			return
		}
		cookieHeader := r.Header.Get("Cookie")
		if !strings.Contains(cookieHeader, "acw_sc__v2="+anyRouterChallengeACW) {
			w.Header().Add("Set-Cookie", "cdn_sec_tc="+anyRouterChallengeCookie+"; Path=/; HttpOnly")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(anyRouterChallengeHTML))
			return
		}
		w.Header().Add("Set-Cookie", "session="+cookieOnlySession+"; Path=/; HttpOnly")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer server.Close()

	token, err := resolveAnyRouterManagedAccessToken(context.Background(), &model.Site{
		BaseURL: server.URL,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "cookie-only-user",
		Password:       "cookie-only-pass",
	})
	if err != nil {
		t.Fatalf("resolveAnyRouterManagedAccessToken returned error: %v", err)
	}
	for _, expected := range []string{
		"session=" + cookieOnlySession,
		"acw_sc__v2=" + anyRouterChallengeACW,
		"cdn_sec_tc=" + anyRouterChallengeCookie,
	} {
		if !strings.Contains(token, expected) {
			t.Fatalf("expected returned session token to contain %q, got %q", expected, token)
		}
	}
}

func TestAnyRouterCookieTokenCanProbeUserIDAndSyncTokens(t *testing.T) {
	var observedUserIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieHeader := r.Header.Get("Cookie")
		userIDHeader := r.Header.Get("New-API-User")
		if userIDHeader != "" {
			observedUserIDs = append(observedUserIDs, userIDHeader)
		}

		switch r.URL.Path {
		case "/api/user/self":
			if strings.Contains(r.Header.Get("Authorization"), cookieShieldedToken) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
				return
			}
			if !strings.Contains(cookieHeader, "acw_sc__v2="+anyRouterChallengeACW) {
				w.Header().Add("Set-Cookie", "cdn_sec_tc="+anyRouterChallengeCookie+"; Path=/; HttpOnly")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(anyRouterChallengeHTML))
				return
			}
			if userIDHeader != "131936" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":false,"message":"missing New-Api-User"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":131936,"username":"linuxdo_131936"}}`))
		case "/api/token/":
			if strings.Contains(r.Header.Get("Authorization"), cookieShieldedToken) {
				w.WriteHeader(http.StatusUnauthorized)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
				return
			}
			if !strings.Contains(cookieHeader, "acw_sc__v2="+anyRouterChallengeACW) {
				w.Header().Add("Set-Cookie", "cdn_sec_tc="+anyRouterChallengeCookie+"; Path=/; HttpOnly")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(anyRouterChallengeHTML))
				return
			}
			if userIDHeader != "131936" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":false,"message":"missing New-Api-User"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[{"key":"shielded-cookie-key","group":"default"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := &model.Site{BaseURL: server.URL}
	userID, err := anyRouterDiscoverUserID(context.Background(), site, nil, cookieShieldedToken)
	if err != nil {
		t.Fatalf("anyRouterDiscoverUserID returned error: %v", err)
	}
	if userID != 131936 {
		t.Fatalf("expected discovered user id 131936, got %d", userID)
	}

	tokens, err := fetchAnyRouterManagementTokens(context.Background(), site, nil, cookieShieldedToken, userID)
	if err != nil {
		t.Fatalf("fetchAnyRouterManagementTokens returned error: %v", err)
	}
	if len(tokens) != 1 || tokens[0].Token != "shielded-cookie-key" {
		t.Fatalf("unexpected synced tokens: %+v", tokens)
	}
	if !strings.Contains(strings.Join(observedUserIDs, ","), "131936") {
		t.Fatalf("expected New-API-User probing to include 131936, observed=%v", observedUserIDs)
	}
}
