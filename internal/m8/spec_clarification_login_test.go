package m8

import (
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClarificationLoginReturnBoundedAndNotAnAuthenticationGrant(t *testing.T) {
	e, _, _, q := clarificationHTTPFixture(t)
	target := "/console/spec-clarifications?question_id=" + url.QueryEscape(q.ID)
	page := e.hub.RequireClarificationPage(e.hub.HandleClarificationPage)
	r := httptest.NewRequest("GET", target, nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	page(w, r)
	if w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "/console/login?clarification_state=") {
		t.Fatal("CLARIFICATION_LOGIN_RETURN_LOST", w.Code, w.Header())
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	state := loc.Query().Get("clarification_state")
	cookies := w.Result().Cookies()
	if !clarificationLoginToken(state) || len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 600 {
		t.Fatal("CLARIFICATION_LOGIN_STATE_COOKIE_WEAK", cookies)
	}
	callback := e.hub.RequireConsolePrincipal(e.hub.HandleClarificationReturn)
	r = httptest.NewRequest("GET", "/console/clarification-return?state="+state, nil)
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	callback(w, r)
	if w.Code != 401 {
		t.Fatal("CLARIFICATION_RETURN_AUTHENTICATES_FROM_LINK", w.Code, w.Body)
	}
	// A logged-in user still needs the browser binding, and the final theme API
	// will recheck that user's current object-level grant independently.
	r = httptest.NewRequest("GET", "/console/clarification-return?state="+state, nil)
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokLBL})
	w = httptest.NewRecorder()
	callback(w, r)
	if w.Code != 400 {
		t.Fatal("CLARIFICATION_RETURN_COOKIE_NOT_BOUND", w.Code, w.Body)
	}
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	callback(w, r)
	if w.Code != 302 || w.Header().Get("Location") != target {
		t.Fatal("CLARIFICATION_RETURN_FIXED_TARGET_FAILED", w.Code, w.Header(), w.Body)
	}
	w = httptest.NewRecorder()
	callback(w, r)
	if w.Code != 409 {
		t.Fatal("CLARIFICATION_RETURN_SINGLE_USE_BROKEN", w.Code, w.Body)
	}
	for _, query := range []string{"?question_id=q&return_to=https://outside.invalid", "?question_id=q&question_id=other", "?question_id=%0A"} {
		r = httptest.NewRequest("GET", "/console/spec-clarifications"+query, nil)
		r.Header.Set("Accept", "text/html")
		w = httptest.NewRecorder()
		page(w, r)
		if w.Code != 400 {
			t.Fatal("CLARIFICATION_LOGIN_ARBITRARY_REDIRECT", query, w.Code)
		}
	}
	// Only an active normal session reaches the page directly.
	r = httptest.NewRequest("GET", target, nil)
	r.Header.Set("Accept", "text/html")
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokLBL})
	w = httptest.NewRecorder()
	page(w, r)
	if w.Code != 200 {
		t.Fatal("CLARIFICATION_LOGIN_ACTIVE_COOKIE_FAILED", w.Code, w.Body)
	}
}
