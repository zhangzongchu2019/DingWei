package mcpgw

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestConnectAttemptFailureHTMLAndJSONAreUniformAndBound(t *testing.T) {
	var htmlBody, jsonBody string
	for _, accept := range []string{"text/html,application/xhtml+xml,application/json;q=0.8", "application/json"} {
		for _, failure := range []string{"missing", "expired", "no_cookie", "wrong_cookie"} {
			t.Run(failure+"/"+accept, func(t *testing.T) {
				g, _, mux := fixture(t)
				attempt, cookies := browserStart(t, mux)
				if failure == "missing" {
					attempt = "nonexistent-attempt-no-secret"
				}
				if failure == "expired" {
					now := g.cfg.Now()
					g.cfg.Now = func() time.Time { return now.Add(5 * time.Minute) }
				}
				r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id="+url.QueryEscape(attempt), nil)
				r.Header.Set("Accept", accept)
				if failure != "no_cookie" {
					for _, cookie := range cookies {
						copy := *cookie
						if failure == "wrong_cookie" {
							copy.Value = "unrelated-browser"
						}
						r.AddCookie(&copy)
					}
				}
				w := serve(mux, r)
				if w.Code != 400 {
					t.Fatalf("CONNECT_BINDING_REJECT: %s got=%d body=%s", failure, w.Code, w.Body)
				}
				if strings.Contains(accept, "text/html") {
					if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(w.Body.String(), connectAttemptFailureDescription) {
						t.Fatalf("CONNECT_HTML_EXPLANATION: %s %s", w.Header(), w.Body)
					}
					if htmlBody == "" {
						htmlBody = w.Body.String()
					}
					if w.Body.String() != htmlBody {
						t.Fatal("HTML revealed attempt failure category")
					}
					if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("failure page lacks passive/no-referrer/no-store guards")
					}
				} else {
					if w.Header().Get("Content-Type") != "application/json" || w.Body.String() != "{\"error\":\"invalid_request\"}\n" {
						t.Fatalf("CONNECT_JSON_UNCHANGED: %s %s", w.Header(), w.Body)
					}
					if jsonBody == "" {
						jsonBody = w.Body.String()
					}
					if w.Body.String() != jsonBody {
						t.Fatal("JSON revealed failure category")
					}
				}
				if strings.Contains(w.Body.String(), attempt) || strings.Contains(w.Body.String(), "unrelated-browser") || strings.Contains(w.Body.String(), "client.example") {
					t.Fatal("failure response reflected authorization state")
				}
			})
		}
	}
}

func TestConnectOriginalBrowserWorksUntilExactFiveMinuteExpiry(t *testing.T) {
	g, _, mux := fixture(t)
	now := g.cfg.Now()
	attempt, cookies := browserStart(t, mux)
	request := func() *http.Request {
		r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id="+url.QueryEscape(attempt), nil)
		r.Header.Set("Accept", "text/html")
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		return r
	}
	g.cfg.Now = func() time.Time { return now.Add(5*time.Minute - time.Nanosecond) }
	if w := serve(mux, request()); w.Code != 200 {
		t.Fatalf("valid original browser was rejected before deadline: %d %s", w.Code, w.Body)
	}
	g.cfg.Now = func() time.Time { return now.Add(5 * time.Minute) }
	if w := serve(mux, request()); w.Code != 400 {
		t.Fatalf("CONNECT_FIVE_MINUTE_EXPIRY: got=%d body=%s", w.Code, w.Body)
	}
}

func TestConnectHTMLRejectedByAcceptQualityKeepsJSON(t *testing.T) {
	_, _, mux := fixture(t)
	r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id=missing", nil)
	r.Header.Set("Accept", "application/json, text/html;q=0")
	if w := serve(mux, r); w.Code != 400 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatal("q=0 must not select HTML")
	}
}
