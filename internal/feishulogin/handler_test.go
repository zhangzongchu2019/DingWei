package feishulogin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

const testRedirectURI = "https://hub.example.test/oauth/feishu/callback"

type fakeOAuth struct {
	profile       UserInfo
	exchangeErr   error
	userInfoErr   error
	exchangeCalls atomic.Int32
	userInfoCalls atomic.Int32
	lastCode      string
	lastRedirect  string
	lastToken     UserAccessToken
}

func (f *fakeOAuth) AuthorizationURL(state, redirectURI string) (string, error) {
	return DefaultAuthorizeURL + "?" + url.Values{"state": {state}, "redirect_uri": {redirectURI}}.Encode(), nil
}

func (f *fakeOAuth) ExchangeCode(_ context.Context, code, redirectURI string) (UserAccessToken, error) {
	f.exchangeCalls.Add(1)
	f.lastCode, f.lastRedirect = code, redirectURI
	if f.exchangeErr != nil {
		return "", f.exchangeErr
	}
	return "feishu-test-token", nil
}

func (f *fakeOAuth) UserInfo(_ context.Context, token UserAccessToken) (UserInfo, error) {
	f.userInfoCalls.Add(1)
	f.lastToken = token
	if f.userInfoErr != nil {
		return UserInfo{}, f.userInfoErr
	}
	return f.profile, nil
}

type fakeIssuer struct {
	calls   int
	request IssueRequest
	result  IssuedToken
	err     error
}

func (f *fakeIssuer) IssueToken(_ context.Context, req IssueRequest) (IssuedToken, error) {
	f.calls++
	f.request = req
	return f.result, f.err
}

func validProfile() UserInfo {
	return UserInfo{Name: "张伟", OpenID: "open-1", UnionID: "union-1", TenantKey: "tenant-1"}
}

func newTestHandler(t *testing.T, appID string, states StateStore, oauth *fakeOAuth, issuer *fakeIssuer, now *time.Time) *Handler {
	t.Helper()
	h, err := NewHandler(Config{
		AppID: appID, TeamTenantKey: "tenant-1", RedirectURI: testRedirectURI,
		OAuth: oauth, States: states, Tokens: issuer, Now: func() time.Time { return *now },
		Random: strings.NewReader(strings.Repeat("r", 4096)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func startForTest(t *testing.T, h *Handler) (StartResult, string) {
	t.Helper()
	started, err := h.Start(context.Background(), StartRequest{
		ShortName: "mybot", TargetAgent: "helper.zzc", AttemptID: "login-attempt-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(started.AuthorizationURL)
	if err != nil || u.Query().Get("state") == "" {
		t.Fatalf("invalid authorization URL %q: %v", started.AuthorizationURL, err)
	}
	return started, u.Query().Get("state")
}

func TestCallbackSuccessUsesServerProofAndBoundRequest(t *testing.T) {
	now := time.Date(2026, 11, 6, 5, 0, 0, 0, time.UTC)
	oauth := &fakeOAuth{profile: validProfile()}
	expiresAt := now.Add(14 * 24 * time.Hour)
	scopes := []string{"list_online", "send_message", "wait_for_messages", "ack"}
	issuer := &fakeIssuer{result: IssuedToken{
		Token: "local-test-token", SeatID: "seat-1", ExternalAgentFullName: "mybot_AG7.owner",
		Group: "AG7", ExpiresAt: expiresAt, Audience: "https://dw123.cmjrt.com/mcp", Scopes: scopes,
	}}
	h := newTestHandler(t, "app-1", NewMemoryStateStore(), oauth, issuer, &now)
	started, state := startForTest(t, h)
	if want := now.Add(DefaultStateTTL); !started.ExpiresAt.Equal(want) {
		t.Fatalf("expiry=%v want %v", started.ExpiresAt, want)
	}
	got, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "local-test-token" || got.SeatID != "seat-1" ||
		got.ExternalAgentFullName != "mybot_AG7.owner" || got.Group != "AG7" ||
		!got.ExpiresAt.Equal(expiresAt) || got.Audience != "https://dw123.cmjrt.com/mcp" ||
		!slices.Equal(got.Scopes, scopes) {
		t.Fatalf("unexpected callback result: %+v", got)
	}
	proof := issuer.request.Principal
	if issuer.calls != 1 || proof.Key() != "feishu|tenant-1|app-1|open-1" ||
		proof.DisplayName != "张伟" || issuer.request.ShortName != "mybot" ||
		issuer.request.TargetAgent != "helper.zzc" || issuer.request.AttemptID != "login-attempt-1" {
		t.Fatalf("unexpected issue call: calls=%d request=%+v", issuer.calls, issuer.request)
	}
	if oauth.lastCode != "code-1" || oauth.lastRedirect != testRedirectURI || oauth.lastToken != "feishu-test-token" {
		t.Fatal("callback did not use the bound OAuth inputs")
	}
}

func TestCallbackRejectsWrongTenant(t *testing.T) {
	now := time.Now()
	profile := validProfile()
	profile.TenantKey = "tenant-2"
	oauth := &fakeOAuth{profile: profile}
	issuer := &fakeIssuer{}
	h := newTestHandler(t, "app-1", NewMemoryStateStore(), oauth, issuer, &now)
	_, state := startForTest(t, h)
	result, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
	if !errors.Is(err, extshared.ErrTenantRejected) || issuer.calls != 0 || result.Token != "" {
		t.Fatalf("wrong tenant was not rejected before issuance: result=%+v err=%v calls=%d", result, err, issuer.calls)
	}
}

func TestCallbackAttemptMismatchRejectsBeforeIssueAndConsumesState(t *testing.T) {
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	oauth, issuer := &fakeOAuth{profile: validProfile()}, &fakeIssuer{}
	store := NewMemoryStateStore()
	h := newTestHandler(t, "app-1", store, oauth, issuer, &now)
	_, state := startForTest(t, h)

	_, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code", AttemptID: "different-attempt"})
	reason, _, _, ok := CallbackFailureDetails(err)
	if !errors.Is(err, ErrInvalidState) || !ok || reason != FailureAuthorizationUsed {
		t.Fatalf("CALLBACK_ATTEMPT_MISMATCH_BEFORE_ISSUE expected authorization-used failure, got %v", err)
	}
	if issuer.calls != 0 {
		t.Fatalf("CALLBACK_ATTEMPT_MISMATCH_BEFORE_ISSUE IssueToken calls=%d", issuer.calls)
	}
	_, replayErr := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code", AttemptID: "login-attempt-1"})
	if !errors.Is(replayErr, ErrInvalidState) {
		t.Fatalf("CALLBACK_ATTEMPT_MISMATCH_BEFORE_ISSUE state was reusable: %v", replayErr)
	}
}

func TestCallbackRejectsInvalidIdentityWithoutIssuance(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UserInfo)
		want   error
	}{
		{name: "missing tenant", mutate: func(p *UserInfo) { p.TenantKey = "" }, want: extshared.ErrTenantRejected},
		{name: "missing open id", mutate: func(p *UserInfo) { p.OpenID = "" }, want: ErrIdentityIncomplete},
		{name: "display name cannot generate", mutate: func(p *UserInfo) { p.Name = "bad\x00name"; p.EnName = "" }, want: ErrDisplayName},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			profile := validProfile()
			tt.mutate(&profile)
			oauth := &fakeOAuth{profile: profile}
			issuer := &fakeIssuer{}
			h := newTestHandler(t, "app-1", NewMemoryStateStore(), oauth, issuer, &now)
			_, state := startForTest(t, h)
			_, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
			if !errors.Is(err, tt.want) || issuer.calls != 0 {
				t.Fatalf("error=%v want=%v issuer calls=%d", err, tt.want, issuer.calls)
			}
		})
	}
}

func TestCallbackRejectsStateMismatchAndExpiry(t *testing.T) {
	t.Run("mismatch", func(t *testing.T) {
		now := time.Now()
		oauth, issuer := &fakeOAuth{profile: validProfile()}, &fakeIssuer{}
		h := newTestHandler(t, "app-1", NewMemoryStateStore(), oauth, issuer, &now)
		_, _ = startForTest(t, h)
		_, err := h.Callback(context.Background(), CallbackRequest{State: "different", Code: "code"})
		if !errors.Is(err, ErrInvalidState) || oauth.exchangeCalls.Load() != 0 || issuer.calls != 0 {
			t.Fatalf("mismatch err=%v exchanges=%d issues=%d", err, oauth.exchangeCalls.Load(), issuer.calls)
		}
	})
	t.Run("expired", func(t *testing.T) {
		now := time.Now()
		oauth, issuer := &fakeOAuth{profile: validProfile()}, &fakeIssuer{}
		h := newTestHandler(t, "app-1", NewMemoryStateStore(), oauth, issuer, &now)
		_, state := startForTest(t, h)
		now = now.Add(DefaultStateTTL)
		_, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
		if !errors.Is(err, ErrStateExpired) || oauth.exchangeCalls.Load() != 0 || issuer.calls != 0 {
			t.Fatalf("expiry err=%v exchanges=%d issues=%d", err, oauth.exchangeCalls.Load(), issuer.calls)
		}
	})
}

func TestCallbackStateReplay(t *testing.T) {
	now := time.Now()
	oauth := &fakeOAuth{profile: validProfile()}
	issuer := &fakeIssuer{result: IssuedToken{Token: "one-local-token"}}
	h := newTestHandler(t, "app-1", NewMemoryStateStore(), oauth, issuer, &now)
	_, state := startForTest(t, h)
	callback := CallbackRequest{State: state, Code: "code"}
	if _, err := h.Callback(context.Background(), callback); err != nil {
		t.Fatal(err)
	}
	second, err := h.Callback(context.Background(), callback)
	if !errors.Is(err, ErrInvalidState) || second.Token != "" || oauth.exchangeCalls.Load() != 1 || issuer.calls != 1 {
		t.Fatalf("replay produced a second token: result=%+v err=%v exchanges=%d issues=%d", second, err, oauth.exchangeCalls.Load(), issuer.calls)
	}
}

func TestStateBoundToApplicationAndRedirect(t *testing.T) {
	t.Run("different app", func(t *testing.T) {
		now := time.Now()
		states := NewMemoryStateStore()
		first := newTestHandler(t, "app-1", states, &fakeOAuth{}, &fakeIssuer{}, &now)
		_, state := startForTest(t, first)
		oauth, issuer := &fakeOAuth{profile: validProfile()}, &fakeIssuer{}
		second := newTestHandler(t, "app-2", states, oauth, issuer, &now)
		_, err := second.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
		if !errors.Is(err, ErrInvalidState) || oauth.exchangeCalls.Load() != 0 || issuer.calls != 0 {
			t.Fatalf("cross-app state err=%v exchanges=%d issues=%d", err, oauth.exchangeCalls.Load(), issuer.calls)
		}
	})
	t.Run("different redirect", func(t *testing.T) {
		now := time.Now()
		states := NewMemoryStateStore()
		first := newTestHandler(t, "app-1", states, &fakeOAuth{}, &fakeIssuer{}, &now)
		_, state := startForTest(t, first)
		oauth, issuer := &fakeOAuth{profile: validProfile()}, &fakeIssuer{}
		second, err := NewHandler(Config{AppID: "app-1", TeamTenantKey: "tenant-1", RedirectURI: "https://other.example.test/callback",
			OAuth: oauth, States: states, Tokens: issuer, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		_, err = second.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
		if !errors.Is(err, ErrInvalidState) || oauth.exchangeCalls.Load() != 0 || issuer.calls != 0 {
			t.Fatalf("cross-redirect state err=%v exchanges=%d issues=%d", err, oauth.exchangeCalls.Load(), issuer.calls)
		}
	})
}

func TestStartValidatesBoundIssuanceRequest(t *testing.T) {
	now := time.Now()
	h := newTestHandler(t, "app-1", NewMemoryStateStore(), &fakeOAuth{}, &fakeIssuer{}, &now)
	for _, req := range []StartRequest{{ShortName: "Bad", TargetAgent: "helper.zzc"}, {ShortName: "mybot", TargetAgent: " "}} {
		if _, err := h.Start(context.Background(), req); !errors.Is(err, ErrInvalidStart) {
			t.Fatalf("Start(%+v) error=%v", req, err)
		}
	}
}

func TestCallbackErrorsDoNotDiscloseTokens(t *testing.T) {
	t.Run("Feishu token", func(t *testing.T) {
		now := time.Now()
		oauth := &fakeOAuth{exchangeErr: errors.New("feishu-access-plaintext")}
		h := newTestHandler(t, "app-1", NewMemoryStateStore(), oauth, &fakeIssuer{}, &now)
		_, state := startForTest(t, h)
		_, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
		if !errors.Is(err, ErrUpstream) || strings.Contains(err.Error(), "feishu-access-plaintext") {
			t.Fatalf("upstream error was not sanitized: %v", err)
		}
	})
	t.Run("local token", func(t *testing.T) {
		now := time.Now()
		issuer := &fakeIssuer{err: errors.New("local-token-plaintext")}
		h := newTestHandler(t, "app-1", NewMemoryStateStore(), &fakeOAuth{profile: validProfile()}, issuer, &now)
		_, state := startForTest(t, h)
		result, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
		if !errors.Is(err, ErrTokenIssue) || result.Token != "" || strings.Contains(err.Error(), "local-token-plaintext") {
			t.Fatalf("issuer error was not sanitized: result=%+v err=%v", result, err)
		}
	})
}

func TestCallbackFailureReasonCodesAndTargetCategory(t *testing.T) {
	cases := []struct {
		name       string
		configure  func(*fakeOAuth, *fakeIssuer, *time.Time)
		wantReason CallbackFailureReason
		wantTarget CallbackTargetCategory
	}{
		{name: "target missing", configure: func(_ *fakeOAuth, issuer *fakeIssuer, _ *time.Time) {
			issuer.err = errors.Join(extshared.ErrTargetNotInAG, extshared.ErrTargetMissing)
		}, wantReason: FailureTargetUnavailable, wantTarget: TargetCategoryMissing},
		{name: "tenant rejected", configure: func(oauth *fakeOAuth, _ *fakeIssuer, _ *time.Time) {
			profile := validProfile()
			profile.TenantKey = "other-tenant"
			oauth.profile = profile
		}, wantReason: FailureTenantRejected, wantTarget: TargetCategoryUnverified},
		{name: "authorization expired", configure: func(_ *fakeOAuth, _ *fakeIssuer, now *time.Time) {
			*now = now.Add(DefaultStateTTL)
		}, wantReason: FailureAuthorizationUsed, wantTarget: TargetCategoryUnverified},
		{name: "Feishu unavailable", configure: func(oauth *fakeOAuth, _ *fakeIssuer, _ *time.Time) {
			oauth.exchangeErr = errors.New("private upstream detail")
		}, wantReason: FailureFeishuUnavailable, wantTarget: TargetCategoryUnverified},
		{name: "other identity error", configure: func(oauth *fakeOAuth, _ *fakeIssuer, _ *time.Time) {
			profile := validProfile()
			profile.OpenID = ""
			oauth.profile = profile
		}, wantReason: FailureOther, wantTarget: TargetCategoryUnverified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
			oauth, issuer := &fakeOAuth{profile: validProfile()}, &fakeIssuer{}
			h := newTestHandler(t, "app-1", NewMemoryStateStore(), oauth, issuer, &now)
			_, state := startForTest(t, h)
			tc.configure(oauth, issuer, &now)
			_, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "callback-code-secret", AttemptID: "login-attempt-1"})
			if err == nil {
				t.Fatal("expected callback failure")
			}
			reason, prefix, target, ok := CallbackFailureDetails(err)
			if !ok || reason != tc.wantReason || prefix != "login-at" || target != tc.wantTarget {
				t.Fatalf("CALLBACK_FAILURE_REASON_MAPPING got=(%q,%q,%q,%v) want=(%q,login-at,%q,true)", reason, prefix, target, ok, tc.wantReason, tc.wantTarget)
			}
			if strings.Contains(err.Error(), "callback-code-secret") || strings.Contains(err.Error(), "open-1") || strings.Contains(err.Error(), "private upstream detail") {
				t.Fatalf("CALLBACK_FAILURE_DISCLOSURE %v", err)
			}
		})
	}
}

func TestCallbackDelegatesRevocationAndExpiryToA(t *testing.T) {
	for _, cause := range []error{extshared.ErrTokenRevoked, extshared.ErrTokenExpired} {
		t.Run(fmt.Sprint(cause), func(t *testing.T) {
			now := time.Now()
			issuer := &fakeIssuer{err: cause}
			h := newTestHandler(t, "app-1", NewMemoryStateStore(), &fakeOAuth{profile: validProfile()}, issuer, &now)
			_, state := startForTest(t, h)
			result, err := h.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
			if !errors.Is(err, ErrTokenIssue) || result.Token != "" || issuer.calls != 1 {
				t.Fatalf("lifecycle rejection result=%+v err=%v calls=%d", result, err, issuer.calls)
			}
		})
	}
}

func TestMemoryStateStoreConcurrentConsume(t *testing.T) {
	store := NewMemoryStateStore()
	attempt := LoginAttempt{StateHash: "hash", AppID: "app", ExpiresAt: time.Now().Add(time.Minute)}
	if err := store.Create(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Consume(context.Background(), "hash", time.Now()); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("concurrent Consume succeeded %d times", successes.Load())
	}
}

func TestNewHandlerRequiresIndependentTenantConfiguration(t *testing.T) {
	now := time.Now()
	_, err := NewHandler(Config{AppID: "app", RedirectURI: testRedirectURI, OAuth: &fakeOAuth{}, States: NewMemoryStateStore(), Tokens: &fakeIssuer{}, Now: func() time.Time { return now }})
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("missing tenant config error=%v", err)
	}
}
