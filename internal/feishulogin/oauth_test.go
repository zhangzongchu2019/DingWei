package feishulogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAuthorizationURLUsesCurrentFeishuOAuth(t *testing.T) {
	client, err := NewFeishuClient(OAuthConfig{AppID: "app-1", AppSecret: "secret", RedirectURI: testRedirectURI})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := client.AuthorizationURL("state-1", testRedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "accounts.feishu.cn" || u.Path != "/open-apis/authen/v1/authorize" {
		t.Fatalf("not the current OAuth authorization endpoint: %s", u)
	}
	q := u.Query()
	if q.Get("client_id") != "app-1" || q.Get("response_type") != "code" ||
		q.Get("redirect_uri") != testRedirectURI || q.Get("state") != "state-1" {
		t.Fatalf("unexpected authorization query: %v", q)
	}
	if q.Get("scope") == "offline_access" || strings.Contains(raw, "secret") {
		t.Fatalf("authorization URL disclosed a secret or requested refresh token: %s", raw)
	}
}

func TestOAuthExchangeAndUserInfoWithFakeServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("token method=%s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		for key, want := range map[string]string{
			"grant_type": "authorization_code", "client_id": "app-1", "client_secret": "app-secret-test",
			"code": "code-1", "redirect_uri": testRedirectURI,
		} {
			if got := r.Form.Get(key); got != want {
				t.Errorf("form %s=%q want %q", key, got, want)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"feishu-access-test","token_type":"Bearer","expires_in":3600,"refresh_token":"must-not-escape"}`))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer feishu-access-test" {
			t.Errorf("Authorization=%q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": validProfile()})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := NewFeishuClient(OAuthConfig{
		AppID: "app-1", AppSecret: "app-secret-test", RedirectURI: testRedirectURI,
		TokenURL: server.URL + "/token", UserInfoURL: server.URL + "/user", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.ExchangeCode(context.Background(), "code-1", testRedirectURI)
	if err != nil || token != "feishu-access-test" {
		t.Fatalf("ExchangeCode token=%v err=%v", token, err)
	}
	if fmt.Sprint(token) != "[redacted]" || fmt.Sprintf("%#v", token) != "[redacted]" {
		t.Fatal("access token formatting was not redacted")
	}
	profile, err := client.UserInfo(context.Background(), token)
	if err != nil || profile.OpenID != "open-1" || profile.TenantKey != "tenant-1" {
		t.Fatalf("UserInfo profile=%+v err=%v", profile, err)
	}
}

func TestOAuthFailuresAreSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"code":1,"access_token":"feishu-secret-in-error","error_description":"feishu-secret-in-error"}`))
			return
		}
		http.Error(w, "feishu-secret-in-error", http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewFeishuClient(OAuthConfig{
		AppID: "app-1", AppSecret: "secret", RedirectURI: testRedirectURI,
		TokenURL: server.URL + "/token", UserInfoURL: server.URL + "/user", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ExchangeCode(context.Background(), "code", testRedirectURI)
	if !errors.Is(err, ErrUpstream) || strings.Contains(err.Error(), "feishu-secret-in-error") {
		t.Fatalf("token error leaked response: %v", err)
	}
	_, err = client.UserInfo(context.Background(), "token")
	if !errors.Is(err, ErrUpstream) || strings.Contains(err.Error(), "feishu-secret-in-error") {
		t.Fatalf("user info error leaked response: %v", err)
	}
}

func TestOAuthRejectsOversizedOrInvalidResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat(" ", maxResponseBytes+1)))
	}))
	defer server.Close()
	client, err := NewFeishuClient(OAuthConfig{AppID: "app", AppSecret: "secret", RedirectURI: testRedirectURI,
		TokenURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExchangeCode(context.Background(), "code", testRedirectURI); !errors.Is(err, ErrUpstream) {
		t.Fatalf("oversized response error=%v", err)
	}
}
