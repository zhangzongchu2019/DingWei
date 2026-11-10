package feishulogin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

type defaultsFake struct {
	names     []string
	invalid   map[string]bool
	recent    string
	recentErr error
}

func (d *defaultsFake) Allowed(tenant string) []string {
	if tenant != "tenant-1" {
		return nil
	}
	return append([]string(nil), d.names...)
}
func (d *defaultsFake) Valid(_ context.Context, s string) bool { return !d.invalid[s] }
func (d *defaultsFake) Recent(_ context.Context, p extshared.Principal) (string, error) {
	if p.Subject == "" {
		panic("unverified recent lookup")
	}
	return d.recent, d.recentErr
}
func setDefaults(f *connectFixture, names ...string) *defaultsFake {
	d := &defaultsFake{names: names, invalid: map[string]bool{}}
	f.handler.defaults = d
	f.oauth.profile.EnName = "Alice"
	return d
}

func TestConsentNameGeneration(t *testing.T) {
	now := time.Date(2026, 11, 9, 16, 2, 3, 0, time.FixedZone("UTC+8", 8*3600))
	for _, tt := range []struct{ name, en, cn, want string }{
		{"english", " AL Ice-42! ", "张三", "alice261109080203"},
		{"polyphonic", "", "重庆单行", "zqdx261109080203"},
		{"empty", "", "", "pm261109080203"},
		{"symbols", " @123! ", "！？", "pm261109080203"},
		{"long", "ABCDEFGHIJKL", "李四", "abcdefgh261109080203"},
		{"chinese", "123", "张三", "zs261109080203"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := AutomaticShortName(UserInfo{EnName: tt.en, Name: tt.cn}, now)
			if got != tt.want {
				t.Fatalf("ZC_NAME_%s got=%s want=%s", tt.name, got, tt.want)
			}
		})
	}
}
func TestConsentSingleAndDisabled(t *testing.T) {
	for _, mode := range []string{"absent", "empty", "other-tenant", "single", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			f := newConnectFixture(t)
			switch mode {
			case "empty":
				setDefaults(f)
			case "other-tenant":
				setDefaults(f, "Helper.zzc")
				f.handler.teamTenantKey = "other"
			case "single":
				setDefaults(f, "Helper.zzc")
			case "invalid":
				d := setDefaults(f, "Helper.zzc")
				d.invalid["Helper.zzc"] = true
			}
			if mode == "invalid" {
				w := httpResponse(f)
				if w != 403 {
					t.Fatalf("ZC_INVALID_TARGET status=%d", w)
				}
				return
			}
			w, _ := f.getForm(t, "outer-attempt-1")
			b := w.Body.String()
			automatic := mode == "single"
			if strings.Contains(b, `name="short_name"`) == automatic || strings.Contains(b, `name="target_agent"`) == automatic {
				t.Fatalf("ZC_FORM_MODE_%s %s", mode, b)
			}
			if automatic && !strings.Contains(b, "同意并进入飞书") {
				t.Fatal("ZC_SINGLE_CONSENT_BUTTON")
			}
		})
	}
}
func httpResponse(f *connectFixture) int {
	r := httptest.NewRecorder()
	f.handler.ServeConnectPage(r, httptest.NewRequest(http.MethodGet, "/feishu/connect", nil))
	return r.Code
}

func TestConsentEmptyAllowedSetKeepsLegacyEntryAndEightByteLimit(t *testing.T) {
	f := newConnectFixture(t)
	setDefaults(f)
	_, csrf := f.getForm(t, "outer-attempt-1")
	values := validConnectValues(csrf)
	values.Set("short_name", "123456789")
	if w := f.postForm(t, values); w.Code != http.StatusBadRequest || f.countStates(StatePurposeOAuth) != 0 {
		t.Fatalf("R57_EMPTY_CONSENT_MUST_KEEP_LEGACY_LIMIT status=%d", w.Code)
	}
	_, csrf = f.getForm(t, "outer-attempt-1")
	values = validConnectValues(csrf)
	values.Set("short_name", "legacy8")
	state := oauthStateFromRedirect(t, f.postForm(t, values))
	result, err := f.handler.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
	if err != nil || result.Token == "" || f.issuer.request.Automatic || f.issuer.request.ShortName != "legacy8" || f.issuer.request.TargetAgent != "Helper.zzc" {
		t.Fatalf("R57_EMPTY_CONSENT_LEGACY_ISSUANCE request=%+v err=%v", f.issuer.request, err)
	}
}
func TestConsentSingleTrustedIdentityAndReplay(t *testing.T) {
	f := newConnectFixture(t)
	setDefaults(f, "Helper.zzc")
	_, token := f.getForm(t, "outer-attempt-1")
	v := validConnectValues(token)
	v.Del("short_name")
	v.Del("target_agent")
	state := oauthStateFromRedirect(t, f.postForm(t, v))
	result, e := f.handler.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
	if e != nil || result.AttemptID != "outer-attempt-1" || f.issuer.calls != 1 || f.issuer.request.ShortName != "alice261106053000" || f.issuer.request.TargetAgent != "Helper.zzc" {
		t.Fatalf("ZC_SINGLE_TRUSTED request=%+v err=%v", f.issuer.request, e)
	}
	if _, e = f.handler.Callback(context.Background(), CallbackRequest{State: state, Code: "code"}); e == nil || f.issuer.calls != 1 {
		t.Fatal("ZC_CALLBACK_REPLAY")
	}
}
func TestConsentOverridesAndRevocation(t *testing.T) {
	for _, mode := range []string{"short-override", "target-override", "revoked", "removed", "config-absent", "empty-profile", "wrong-tenant"} {
		t.Run(mode, func(t *testing.T) {
			f := newConnectFixture(t)
			d := setDefaults(f, "Helper.zzc")
			_, token := f.getForm(t, "outer-attempt-1")
			v := validConnectValues(token)
			v.Del("short_name")
			v.Del("target_agent")
			if mode == "short-override" {
				v.Set("short_name", "evil")
			}
			if mode == "target-override" {
				v.Set("target_agent", "Other.zzc")
			}
			post := f.postForm(t, v)
			if strings.HasSuffix(mode, "override") {
				if post.Code != 400 || f.issuer.calls != 0 {
					t.Fatalf("ZC_OVERRIDE_%s status=%d", mode, post.Code)
				}
				return
			}
			state := oauthStateFromRedirect(t, post)
			if mode == "revoked" {
				d.invalid["Helper.zzc"] = true
			}
			if mode == "removed" {
				d.names = nil
			}
			if mode == "config-absent" {
				f.handler.defaults = nil
			}
			if mode == "empty-profile" {
				f.oauth.profile.Name = ""
				f.oauth.profile.EnName = ""
			}
			if mode == "wrong-tenant" {
				f.oauth.profile.TenantKey = "evil"
			}
			_, e := f.handler.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
			if mode == "empty-profile" {
				if e != nil || f.issuer.request.ShortName != "pm261106053000" {
					t.Fatalf("ZC_EMPTY_PROFILE %v %+v", e, f.issuer.request)
				}
				return
			}
			if e == nil || f.issuer.calls != 0 {
				t.Fatalf("ZC_FINAL_%s err=%v calls=%d", mode, e, f.issuer.calls)
			}
		})
	}
}
func TestConsentMultipleSelectionBoundOnce(t *testing.T) {
	f := newConnectFixture(t)
	d := setDefaults(f, "Helper.zzc", "Other.zzc", "Dead.zzc")
	d.invalid["Dead.zzc"] = true
	d.recent = "Other.zzc"
	_, token := f.getForm(t, "outer-attempt-1")
	v := validConnectValues(token)
	v.Del("short_name")
	v.Del("target_agent")
	state := oauthStateFromRedirect(t, f.postForm(t, v))
	ctx := context.Background()
	pending, e := f.handler.Callback(ctx, CallbackRequest{State: state, Code: "code"})
	if e != nil || pending.Selection == "" || fmt.Sprint(pending.Targets) != "[Helper.zzc Other.zzc]" || pending.Preferred != "Other.zzc" || f.issuer.calls != 0 {
		t.Fatalf("ZC_MULTI_FILTER %+v %v", pending, e)
	}
	// Moving the clock cannot change the name generated for this attempt.
	f.handler.now = func() time.Time { return time.Date(2026, 11, 6, 5, 31, 0, 0, time.UTC) }
	result, e := f.handler.Callback(ctx, CallbackRequest{Selection: pending.Selection, Target: "Other.zzc"})
	if e != nil || result.AttemptID != "outer-attempt-1" || f.issuer.request.ShortName != "alice261106053000" || f.issuer.request.TargetAgent != "Other.zzc" {
		t.Fatalf("ZC_SELECTION_NAME_ONCE %+v %v", f.issuer.request, e)
	}
	if _, e = f.handler.Callback(ctx, CallbackRequest{Selection: pending.Selection, Target: "Helper.zzc"}); e == nil || f.issuer.calls != 1 {
		t.Fatal("ZC_SELECTION_REPLAY")
	}
}
func TestConsentSelectionRejectsChangedPolicy(t *testing.T) {
	for _, mode := range []string{"outside", "removed", "revoked", "expired", "wrong-purpose", "recent-outside", "recent-error"} {
		t.Run(mode, func(t *testing.T) {
			f := newConnectFixture(t)
			d := setDefaults(f, "Helper.zzc", "Other.zzc")
			d.recent = "Other.zzc"
			if mode == "recent-outside" {
				d.recent = "Evil.zzc"
			}
			if mode == "recent-error" {
				d.recentErr = fmt.Errorf("db unavailable")
			}
			started, e := f.handler.Start(context.Background(), StartRequest{Automatic: true, AttemptID: "a"})
			if e != nil {
				t.Fatal(e)
			}
			state := stateFromURL(started.AuthorizationURL)
			p, e := f.handler.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
			if mode == "recent-error" {
				if e == nil {
					t.Fatal("ZC_RECENT_ERROR")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if mode == "recent-outside" {
				if p.Preferred != "" {
					t.Fatal("ZC_RECENT_OUTSIDE")
				}
				return
			}
			target := "Other.zzc"
			switch mode {
			case "outside":
				target = "Evil.zzc"
			case "removed":
				d.names = []string{"Helper.zzc"}
			case "revoked":
				d.invalid[target] = true
			case "expired":
				f.handler.now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
			case "wrong-purpose":
				f.states.mu.Lock()
				a := f.states.attempts[hashState(p.Selection)]
				a.Purpose = StatePurposeOAuth
				f.states.attempts[hashState(p.Selection)] = a
				f.states.mu.Unlock()
			}
			if _, e = f.handler.Callback(context.Background(), CallbackRequest{Selection: p.Selection, Target: target}); e == nil || f.issuer.calls != 0 {
				t.Fatalf("ZC_SELECTION_%s %v", mode, e)
			}
		})
	}
}
func TestConsentConcurrentCallback(t *testing.T) {
	f := newConnectFixture(t)
	setDefaults(f, "Helper.zzc")
	started, e := f.handler.Start(context.Background(), StartRequest{Automatic: true, TargetAgent: "Helper.zzc"})
	if e != nil {
		t.Fatal(e)
	}
	state := stateFromURL(started.AuthorizationURL)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.handler.Callback(context.Background(), CallbackRequest{State: state, Code: "code"})
			if err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 || f.issuer.calls != 1 {
		t.Fatalf("ZC_CONCURRENT_ONCE %d %d", ok, f.issuer.calls)
	}
}

func stateFromURL(raw string) string { u, _ := url.Parse(raw); return u.Query().Get("state") }

func TestConsentIssueExpiry(t *testing.T) {
	f := newConnectFixture(t)
	setDefaults(f, "Helper.zzc")
	_, e := f.handler.issue(context.Background(), LoginAttempt{ExpiresAt: f.handler.now(), Request: IssueRequest{Automatic: true, ShortName: "alice261106053000", TargetAgent: "Helper.zzc"}})
	if e != ErrStateExpired || f.issuer.calls != 0 {
		t.Fatalf("ZC_ISSUE_EXPIRED %v", e)
	}
}
