package feishulogin

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

var csrfInputPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

type connectFixture struct {
	handler *Handler
	states  *MemoryStateStore
	oauth   *fakeOAuth
	issuer  *fakeIssuer
}

func newConnectFixture(t *testing.T) *connectFixture {
	t.Helper()
	now := time.Date(2026, 11, 6, 5, 30, 0, 0, time.UTC)
	states := NewMemoryStateStore()
	oauth := &fakeOAuth{profile: validProfile()}
	issuer := &fakeIssuer{result: IssuedToken{Token: "local-test-token", SeatID: "seat-1", Group: "AG7"}}
	handler := newTestHandler(t, "app-1", states, oauth, issuer, &now)
	handler.random = cryptorand.Reader
	return &connectFixture{handler: handler, states: states, oauth: oauth, issuer: issuer}
}

func (f *connectFixture) getForm(t *testing.T, attemptID string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	target := "/feishu/connect"
	if attemptID != "" {
		target += "?attempt_id=" + url.QueryEscape(attemptID)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeConnectPage(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	matches := csrfInputPattern.FindStringSubmatch(recorder.Body.String())
	if len(matches) != 2 {
		t.Fatalf("CSRF input not found in response %d: %s", recorder.Code, recorder.Body.String())
	}
	return recorder, matches[1]
}

func (f *connectFixture) postForm(t *testing.T, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/feishu/connect", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	f.handler.ServeConnectPage(recorder, request)
	return recorder
}

func validConnectValues(csrfToken string) url.Values {
	return url.Values{
		"csrf_token":   {csrfToken},
		"attempt_id":   {"outer-attempt-1"},
		"short_name":   {"mybot"},
		"target_agent": {"Helper.zzc"},
	}
}

func (f *connectFixture) countStates(purpose StatePurpose) int {
	f.states.mu.Lock()
	defer f.states.mu.Unlock()
	count := 0
	for _, attempt := range f.states.attempts {
		if attempt.Purpose == purpose {
			count++
		}
	}
	return count
}

func oauthStateFromRedirect(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("POST status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil || location.Query().Get("state") == "" {
		t.Fatalf("invalid redirect %q: %v", recorder.Header().Get("Location"), err)
	}
	return location.Query().Get("state")
}

func assertNoEmbeddingHeaders(t *testing.T, header http.Header) {
	t.Helper()
	if header.Get("X-Frame-Options") != "DENY" ||
		!strings.Contains(header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("missing anti-embedding headers: %+v", header)
	}
}

func TestConnectPageGETRendersSelfContainedValidatedForm(t *testing.T) {
	fixture := newConnectFixture(t)
	recorder, token := fixture.getForm(t, "outer-attempt-1")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status=%d", recorder.Code)
	}
	assertNoEmbeddingHeaders(t, recorder.Header())
	if recorder.Header().Get("Cache-Control") != "no-store" ||
		recorder.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("missing privacy headers: %+v", recorder.Header())
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`name="short_name"`, `required`, `pattern="[a-z0-9]{1,8}"`,
		`name="target_agent"`, `外部 Agent 名将为 短名_AG序号`,
		`name="attempt_id" value="outer-attempt-1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(body), "<script") || strings.Contains(body, "Helper.zzc") {
		t.Fatalf("form contains script or an enumerated target: %s", body)
	}
	fixture.states.mu.Lock()
	attempt := fixture.states.attempts[hashState(token)]
	fixture.states.mu.Unlock()
	if attempt.Purpose != StatePurposeConnectForm || attempt.Request.AttemptID != "outer-attempt-1" ||
		attempt.Request.Principal != (extshared.Principal{}) {
		t.Fatalf("unexpected form state: %+v", attempt)
	}
}

func TestConnectPageRejectsInvalidShortName(t *testing.T) {
	for _, shortName := range []string{"", "Bad", "abc_def", "abc-def", "toolongname", "机器人"} {
		t.Run(shortName, func(t *testing.T) {
			fixture := newConnectFixture(t)
			_, token := fixture.getForm(t, "outer-attempt-1")
			values := validConnectValues(token)
			values.Set("short_name", shortName)
			recorder := fixture.postForm(t, values)
			if recorder.Code != http.StatusBadRequest || fixture.countStates(StatePurposeOAuth) != 0 {
				t.Fatalf("short %q status=%d OAuth states=%d", shortName, recorder.Code, fixture.countStates(StatePurposeOAuth))
			}
			assertNoEmbeddingHeaders(t, recorder.Header())
		})
	}
}

func TestConnectPageRejectsMalformedTargetWithoutLookup(t *testing.T) {
	for _, target := range []string{"", "helper", ".zzc", "helper.", "help er.zzc", "a.zzc", "helper.z", "ki.mi.zzc"} {
		t.Run(target, func(t *testing.T) {
			fixture := newConnectFixture(t)
			_, token := fixture.getForm(t, "outer-attempt-1")
			values := validConnectValues(token)
			values.Set("target_agent", target)
			recorder := fixture.postForm(t, values)
			if recorder.Code != http.StatusBadRequest || fixture.countStates(StatePurposeOAuth) != 0 {
				t.Fatalf("target %q status=%d OAuth states=%d", target, recorder.Code, fixture.countStates(StatePurposeOAuth))
			}
		})
	}
}

func TestConnectPagePOSTBindsIntentToOAuthState(t *testing.T) {
	fixture := newConnectFixture(t)
	_, token := fixture.getForm(t, "outer-attempt-1")
	state := oauthStateFromRedirect(t, fixture.postForm(t, validConnectValues(token)))
	fixture.states.mu.Lock()
	attempt := fixture.states.attempts[hashState(state)]
	fixture.states.mu.Unlock()
	if attempt.Purpose != StatePurposeOAuth || attempt.Request.ShortName != "mybot" ||
		attempt.Request.TargetAgent != "Helper.zzc" || attempt.Request.AttemptID != "outer-attempt-1" ||
		attempt.Request.Principal != (extshared.Principal{}) {
		t.Fatalf("OAuth state did not bind the original intent: %+v", attempt)
	}
}

func TestCallbackIgnoresQueryShortNameAndTarget(t *testing.T) {
	fixture := newConnectFixture(t)
	_, token := fixture.getForm(t, "outer-attempt-1")
	state := oauthStateFromRedirect(t, fixture.postForm(t, validConnectValues(token)))
	query := url.Values{
		"state":        {state},
		"code":         {"code-1"},
		"short_name":   {"attacker"},
		"target_agent": {"Other.evil"},
		"redirect_uri": {"https://evil.example/callback"},
	}
	result, err := fixture.handler.Callback(context.Background(), CallbackRequestFromQuery(query))
	if err != nil {
		t.Fatal(err)
	}
	if fixture.issuer.calls != 1 || fixture.issuer.request.ShortName != "mybot" ||
		fixture.issuer.request.TargetAgent != "Helper.zzc" || result.AttemptID != "outer-attempt-1" {
		t.Fatalf("callback trusted query overrides: calls=%d request=%+v result=%+v",
			fixture.issuer.calls, fixture.issuer.request, result)
	}
}

func TestConnectPageCSRFReplay(t *testing.T) {
	fixture := newConnectFixture(t)
	_, token := fixture.getForm(t, "outer-attempt-1")
	values := validConnectValues(token)
	first := fixture.postForm(t, values)
	_ = oauthStateFromRedirect(t, first)
	second := fixture.postForm(t, values)
	if second.Code != http.StatusForbidden || fixture.countStates(StatePurposeOAuth) != 1 {
		t.Fatalf("replayed CSRF status=%d OAuth states=%d", second.Code, fixture.countStates(StatePurposeOAuth))
	}
	assertNoEmbeddingHeaders(t, second.Header())
}

func TestConnectPageRejectsMissingCSRFAndAttemptOverride(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		fixture := newConnectFixture(t)
		values := validConnectValues("")
		if recorder := fixture.postForm(t, values); recorder.Code != http.StatusForbidden {
			t.Fatalf("missing CSRF status=%d", recorder.Code)
		}
	})
	t.Run("attempt override", func(t *testing.T) {
		fixture := newConnectFixture(t)
		_, token := fixture.getForm(t, "outer-attempt-1")
		values := validConnectValues(token)
		values.Set("attempt_id", "other-attempt")
		if recorder := fixture.postForm(t, values); recorder.Code != http.StatusForbidden ||
			fixture.countStates(StatePurposeOAuth) != 0 {
			t.Fatalf("attempt override status=%d OAuth states=%d", recorder.Code, fixture.countStates(StatePurposeOAuth))
		}
	})
}

func TestConnectAndOAuthStatesAreNotInterchangeable(t *testing.T) {
	t.Run("form token as OAuth state", func(t *testing.T) {
		fixture := newConnectFixture(t)
		_, token := fixture.getForm(t, "outer-attempt-1")
		_, err := fixture.handler.Callback(context.Background(), CallbackRequest{State: token, Code: "code"})
		if !errors.Is(err, ErrInvalidState) || fixture.oauth.exchangeCalls.Load() != 0 {
			t.Fatalf("form token reached OAuth exchange: err=%v exchanges=%d", err, fixture.oauth.exchangeCalls.Load())
		}
	})
	t.Run("OAuth state as form token", func(t *testing.T) {
		fixture := newConnectFixture(t)
		started, err := fixture.handler.Start(context.Background(), StartRequest{ShortName: "mybot", TargetAgent: "Helper.zzc"})
		if err != nil {
			t.Fatal(err)
		}
		location, _ := url.Parse(started.AuthorizationURL)
		values := validConnectValues(location.Query().Get("state"))
		if recorder := fixture.postForm(t, values); recorder.Code != http.StatusForbidden {
			t.Fatalf("OAuth state accepted as CSRF: status=%d", recorder.Code)
		}
	})
}

func TestConnectPageRejectsOtherMethodsWithSecurityHeaders(t *testing.T) {
	fixture := newConnectFixture(t)
	recorder := httptest.NewRecorder()
	fixture.handler.ServeConnectPage(recorder, httptest.NewRequest(http.MethodPut, "/feishu/connect", nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("PUT status=%d Allow=%q", recorder.Code, recorder.Header().Get("Allow"))
	}
	assertNoEmbeddingHeaders(t, recorder.Header())
}
