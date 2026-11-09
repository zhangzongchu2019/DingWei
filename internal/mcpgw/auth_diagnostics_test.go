package mcpgw

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

type diagnosticsOAuth struct {
	profile     feishulogin.UserInfo
	exchangeErr error
}

func (o diagnosticsOAuth) AuthorizationURL(state, redirectURI string) (string, error) {
	return "https://accounts.example/authorize?" + url.Values{"state": {state}, "redirect_uri": {redirectURI}}.Encode(), nil
}
func (o diagnosticsOAuth) ExchangeCode(context.Context, string, string) (feishulogin.UserAccessToken, error) {
	if o.exchangeErr != nil {
		return "", o.exchangeErr
	}
	return "feishu-token-secret", nil
}
func (o diagnosticsOAuth) UserInfo(context.Context, feishulogin.UserAccessToken) (feishulogin.UserInfo, error) {
	return o.profile, nil
}

type diagnosticsIssuer struct {
	err error
}

func (i diagnosticsIssuer) IssueToken(context.Context, feishulogin.IssueRequest) (feishulogin.IssuedToken, error) {
	if i.err != nil {
		return feishulogin.IssuedToken{}, i.err
	}
	return feishulogin.IssuedToken{}, errors.New("unexpected successful issue")
}

var diagnosticsCSRF = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func runCallbackFailure(t *testing.T, oauth diagnosticsOAuth, issuer diagnosticsIssuer, expired bool) (*httptest.ResponseRecorder, string, string) {
	t.Helper()
	g, _, mux := oauthFixture(t)
	var logBytes strings.Builder
	g.cfg.Logger = slog.New(slog.NewJSONHandler(&logBytes, nil))
	clock := g.cfg.Now().UTC()
	login, err := feishulogin.NewHandler(feishulogin.Config{
		AppID: "app", TeamTenantKey: "team", RedirectURI: testOrigin + "/mcp/oauth/feishu/callback",
		OAuth: oauth, States: LoginStates{Store: g.cfg.Store}, Tokens: issuer,
		Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	g.cfg.Login = login
	mux = http.NewServeMux()
	g.Register(mux)
	attempt, cookies := browserStart(t, mux)
	connect := httptest.NewRequest(http.MethodGet, testOrigin+"/feishu/connect?attempt_id="+url.QueryEscape(attempt), nil)
	addCookies(connect, cookies)
	page := serve(mux, connect)
	match := diagnosticsCSRF.FindStringSubmatch(page.Body.String())
	if page.Code != http.StatusOK || len(match) != 2 {
		t.Fatalf("diagnostics connect page status=%d body=%s", page.Code, page.Body)
	}
	form := url.Values{"attempt_id": {attempt}, "csrf_token": {html.UnescapeString(match[1])}, "short_name": {"bot"}, "target_agent": {"missing.zzc"}}
	post := httptest.NewRequest(http.MethodPost, testOrigin+"/feishu/connect", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", testOrigin)
	addCookies(post, cookies)
	started := serve(mux, post)
	if started.Code != http.StatusSeeOther {
		t.Fatalf("diagnostics start status=%d body=%s", started.Code, started.Body)
	}
	stateURL, err := url.Parse(started.Header().Get("Location"))
	if err != nil || stateURL.Query().Get("state") == "" {
		t.Fatalf("diagnostics start Location=%q", started.Header().Get("Location"))
	}
	if expired {
		clock = clock.Add(feishulogin.DefaultStateTTL)
	}
	callbackURL := testOrigin + "/mcp/oauth/feishu/callback?" + url.Values{
		"state": {stateURL.Query().Get("state")}, "code": {"callback-code-secret"},
	}.Encode()
	callback := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	callback.Header.Set("Accept", "text/html")
	addCookies(callback, cookies)
	result := serve(mux, callback)
	return result, logBytes.String(), attempt
}

func TestCallbackFailureReasonsPageAndStructuredLog(t *testing.T) {
	valid := feishulogin.UserInfo{Name: "Alice", EnName: "alice", OpenID: "open_id-secret", TenantKey: "team"}
	cases := []struct {
		name, reason, description, target string
		oauth                             diagnosticsOAuth
		issuer                            diagnosticsIssuer
		expired                           bool
	}{
		{name: "target missing", reason: string(feishulogin.FailureTargetUnavailable), description: "目标 Agent 不存在或不可用", target: "missing",
			oauth: diagnosticsOAuth{profile: valid}, issuer: diagnosticsIssuer{err: errors.Join(extshared.ErrTargetNotInAG, extshared.ErrTargetMissing)}},
		{name: "target exists unavailable", reason: string(feishulogin.FailureTargetUnavailable), description: "目标 Agent 不存在或不可用", target: "exists",
			oauth: diagnosticsOAuth{profile: valid}, issuer: diagnosticsIssuer{err: extshared.ErrTargetNotInAG}},
		{name: "tenant rejected", reason: string(feishulogin.FailureTenantRejected), description: "当前飞书账号不属于本组织", target: "unverified",
			oauth: diagnosticsOAuth{profile: feishulogin.UserInfo{Name: "Alice", EnName: "alice", OpenID: "open_id-secret", TenantKey: "another-team"}}},
		{name: "authorization expired", reason: string(feishulogin.FailureAuthorizationUsed), description: "授权已过期或已使用", target: "unverified", oauth: diagnosticsOAuth{profile: valid}, expired: true},
		{name: "Feishu unavailable", reason: string(feishulogin.FailureFeishuUnavailable), description: "飞书服务暂不可用", target: "unverified",
			oauth: diagnosticsOAuth{exchangeErr: errors.New("upstream access-token-secret")}},
		{name: "other", reason: string(feishulogin.FailureOther), description: "接入暂未完成", target: "unverified",
			oauth: diagnosticsOAuth{profile: valid}, issuer: diagnosticsIssuer{err: errors.New("internal token-secret open_id-secret")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, logLine, attemptID := runCallbackFailure(t, tc.oauth, tc.issuer, tc.expired)
			if response.Code != http.StatusBadRequest || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("CALLBACK_FAILURE_HTML status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
			}
			body := response.Body.String()
			if !strings.Contains(body, tc.description) || !strings.Contains(body, "id=\"oauth-error\">access_denied") {
				t.Fatalf("CALLBACK_FAILURE_ERROR_MAPPING reason=%s body=%s", tc.reason, body)
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(logLine)), &record); err != nil {
				t.Fatalf("CALLBACK_FAILURE_LOG_JSON %q: %v", logLine, err)
			}
			if record["reason_code"] != tc.reason || record["target_category"] != tc.target || record["msg"] != "MCP Feishu authorization callback failed" {
				t.Fatalf("CALLBACK_FAILURE_LOG_MAPPING record=%v", record)
			}
			attempt, ok := record["attempt_prefix"].(string)
			if !ok || len(attempt) != 8 || attempt != attemptID[:8] {
				t.Fatalf("CALLBACK_FAILURE_ATTEMPT_PREFIX %v", record["attempt_prefix"])
			}
			utc, ok := record["utc"].(string)
			if !ok {
				t.Fatalf("CALLBACK_FAILURE_UTC missing: %v", record)
			}
			stamp, err := time.Parse(time.RFC3339Nano, utc)
			if err != nil || stamp.Location() != time.UTC {
				t.Fatalf("CALLBACK_FAILURE_UTC invalid %q (%v)", utc, err)
			}
			for _, secret := range []string{"callback-code-secret", "feishu-token-secret", "access-token-secret", "local-token", "open_id-secret", "missing.zzc"} {
				if strings.Contains(body, secret) || strings.Contains(logLine, secret) {
					t.Fatalf("CALLBACK_FAILURE_NO_LEAK secret=%q body=%s log=%s", secret, body, logLine)
				}
			}
		})
	}
}

func TestCallbackFailureJSONKeepsOAuthErrorAndAddsDescription(t *testing.T) {
	g, _, _ := oauthFixture(t)
	g.cfg.Logger = slog.New(slog.NewJSONHandler(&strings.Builder{}, nil))
	req := httptest.NewRequest(http.MethodGet, testOrigin+"/mcp/oauth/feishu/callback", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	g.failCallback(w, req, http.StatusBadRequest, "access_denied", feishulogin.FailureTargetUnavailable, "attempt-123456", feishulogin.TargetCategoryMissing)
	var body callbackFailureBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "access_denied" || !strings.Contains(body.ErrorDescription, "目标 Agent 不存在") || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("CALLBACK_FAILURE_JSON_ERROR_SEMANTICS body=%+v headers=%v", body, w.Header())
	}
}

func TestCallbackOriginFailureUsesSafeDiagnostics(t *testing.T) {
	g, _, mux := oauthFixture(t)
	var logBytes strings.Builder
	g.cfg.Logger = slog.New(slog.NewJSONHandler(&logBytes, nil))
	request := httptest.NewRequest(http.MethodGet, testOrigin+"/mcp/oauth/feishu/callback?code=private-code&state=private-state", nil)
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Origin", "https://untrusted.example")
	response := serve(mux, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "origin_rejected") {
		t.Fatalf("CALLBACK_GUARD_DIAGNOSTICS response=%d body=%s", response.Code, response.Body)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(logBytes.String())), &record); err != nil {
		t.Fatalf("CALLBACK_GUARD_DIAGNOSTICS log=%q err=%v", logBytes.String(), err)
	}
	if record["reason_code"] != string(feishulogin.FailureOther) {
		t.Fatalf("CALLBACK_GUARD_DIAGNOSTICS record=%v", record)
	}
	for _, secret := range []string{"private-code", "private-state", "untrusted.example"} {
		if strings.Contains(response.Body.String(), secret) || strings.Contains(logBytes.String(), secret) {
			t.Fatalf("CALLBACK_GUARD_DIAGNOSTICS leaked %q", secret)
		}
	}
}
