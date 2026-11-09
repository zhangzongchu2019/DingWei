package mcpgw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

type consentTargets struct {
	names   []string
	invalid bool
}

func (d *consentTargets) Allowed(t string) []string {
	if t != "team" {
		return nil
	}
	return d.names
}
func (d *consentTargets) Valid(_ context.Context, s string) bool { return !d.invalid }
func (d *consentTargets) Recent(_ context.Context, p extshared.Principal) (string, error) {
	return "Other.zzc", nil
}
func consentBegin(t *testing.T, mux *http.ServeMux, attempt string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id="+attempt, nil)
	addCookies(r, cookies)
	w := serve(mux, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), `type="text"`) {
		t.Fatalf("ZC_GW_FORM_HAS_INPUT %d %s", w.Code, w.Body)
	}
	m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(m) != 2 {
		t.Fatal("ZC_GW_CSRF")
	}
	v := url.Values{"attempt_id": {attempt}, "csrf_token": {m[1]}}
	r = httptest.NewRequest("POST", testOrigin+"/feishu/connect", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addCookies(r, cookies)
	w = serve(mux, r)
	if w.Code != 303 {
		t.Fatalf("ZC_GW_START %d %s", w.Code, w.Body)
	}
	u, _ := url.Parse(w.Header().Get("Location"))
	r = httptest.NewRequest("GET", testOrigin+"/mcp/oauth/feishu/callback?"+url.Values{"state": {u.Query().Get("state")}, "code": {"feishu-code"}}.Encode(), nil)
	addCookies(r, cookies)
	return serve(mux, r)
}
func TestConsentGatewaySingleAndMultiBrowserGuards(t *testing.T) {
	for _, mode := range []string{"single", "multi", "no-cookie", "wrong-cookie", "outside", "revoked", "expired", "replay", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			d := &consentTargets{names: []string{"target.zzc"}}
			if mode != "single" {
				d.names = append(d.names, "Other.zzc")
			}
			g, seats, mux := oauthConsentFixture(t, nil, d)
			attempt, cookies := browserStart(t, mux)
			w := consentBegin(t, mux, attempt, cookies)
			if mode == "single" {
				if w.Code != 302 || len(seats.issued) != 1 || len(seats.issued[0].ShortName) <= 12 {
					t.Fatalf("ZC_GW_SINGLE %d %+v", w.Code, seats.issued)
				}
				return
			}
			if w.Code != 200 || len(seats.issued) != 0 || !strings.Contains(w.Body.String(), `value="Other.zzc" checked`) {
				t.Fatalf("ZC_GW_MULTI_PAGE %d %s", w.Code, w.Body)
			}
			m := regexp.MustCompile(`name="selection" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
			if len(m) != 2 {
				t.Fatal("ZC_GW_SELECTION_TOKEN")
			}
			f := url.Values{"selection": {m[1]}, "target_agent": {"Other.zzc"}}
			switch mode {
			case "outside":
				f.Set("target_agent", "Evil.zzc")
			case "revoked":
				d.invalid = true
			case "expired":
				old := g.cfg.Now()
				g.cfg.Now = func() time.Time { return old.Add(10 * time.Minute) }
			case "duplicate":
				f.Add("target_agent", "target.zzc")
			}
			post := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", testOrigin+"/mcp/oauth/feishu/callback", strings.NewReader(f.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if mode != "no-cookie" {
					if mode == "wrong-cookie" {
						r.AddCookie(&http.Cookie{Name: cookies[0].Name, Value: "wrong"})
					} else {
						addCookies(r, cookies)
					}
				}
				return serve(mux, r)
			}
			w = post()
			success := mode == "multi" || mode == "replay"
			if success {
				if w.Code != 302 || len(seats.issued) != 1 || seats.issued[0].TargetAgent != "Other.zzc" {
					t.Fatalf("ZC_GW_SELECTION_SUCCESS %d %+v", w.Code, seats.issued)
				}
			} else if w.Code != 400 || len(seats.issued) != 0 {
				t.Fatalf("ZC_GW_SELECTION_%s %d %+v", mode, w.Code, seats.issued)
			}
			if mode == "replay" {
				if w = post(); w.Code != 400 || len(seats.issued) != 1 {
					t.Fatal("ZC_GW_SELECTION_REPLAY")
				}
			}
		})
	}
}

func TestConsentOneStartPerOAuthAttempt(t *testing.T) {
	d := &consentTargets{names: []string{"target.zzc"}}
	_, seats, mux := oauthConsentFixture(t, nil, d)
	attempt, cookies := browserStart(t, mux)
	var forms []url.Values
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("GET", testOrigin+"/feishu/connect?attempt_id="+attempt, nil)
		addCookies(r, cookies)
		w := serve(mux, r)
		m := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
		if len(m) != 2 {
			t.Fatal("missing csrf")
		}
		forms = append(forms, url.Values{"attempt_id": {attempt}, "csrf_token": {m[1]}})
	}

	var state string
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, f := range forms {
		f := f
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", testOrigin+"/feishu/connect", strings.NewReader(f.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			addCookies(r, cookies)
			responses <- serve(mux, r)
		}()
	}
	wg.Wait()
	close(responses)
	success, rejected := 0, 0
	for w := range responses {
		if w.Code == 303 {
			success++
			u, _ := url.Parse(w.Header().Get("Location"))
			state = u.Query().Get("state")
		} else if w.Code == 400 {
			rejected++
		} else {
			t.Fatalf("ZC_OUTER_START_STATUS %d", w.Code)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("ZC_OUTER_ATTEMPT_STARTED_TWICE %d %d", success, rejected)
	}

	r := httptest.NewRequest("GET", testOrigin+"/mcp/oauth/feishu/callback?"+url.Values{"state": {state}, "code": {"feishu-code"}}.Encode(), nil)
	addCookies(r, cookies)
	w := serve(mux, r)
	if w.Code != 302 || len(seats.issued) != 1 {
		t.Fatalf("ZC_ORIGINAL_START_LOST %d %d", w.Code, len(seats.issued))
	}
}
