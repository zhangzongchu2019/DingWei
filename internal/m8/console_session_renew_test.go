package m8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// D1(业主第 7 条):页面一直开着就不必到期重走飞书登录。
// 🔴 这一组用注入时钟跨过 12 小时 —— 真等 12 小时不是验收;
//    而只断言「代码里有 UPDATE」证明不了行为。

func newRenewHub(t *testing.T, created, expires, now time.Time) (*Hub, func() *model.ConsoleSession) {
	t.Helper()
	hub, db, ctx := newTestHub(t)
	hub.consoleAuthEnabled = true
	hub.NowFunc = func() time.Time { return now }
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ua", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConsoleSession(ctx, model.ConsoleSession{ID: "login-a", PrincipalID: "a", TokenHash: hashConsoleToken("raw-a"), ExpiresAt: expires}, created); err != nil {
		t.Fatal(err)
	}
	reload := func() *model.ConsoleSession {
		s, err := db.ResolveConsoleSession(ctx, hashConsoleToken("raw-a"), now)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	return hub, reload
}

func callConsole(t *testing.T, hub *Hub) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/console/agents", nil)
	req.AddCookie(&http.Cookie{Name: consoleSessionCookie, Value: "raw-a"})
	rr := httptest.NewRecorder()
	hub.RequireConsolePrincipal(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })(rr, req)
	return rr
}

func renewedCookie(t *testing.T, rr *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rr.Result().Cookies() {
		if c.Name == consoleSessionCookie {
			return c
		}
	}
	return nil
}

func TestConsoleSessionSlidesWhenPageKeepsPolling(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	created := now.Add(-11 * time.Hour)
	// 剩 1 小时 < 12h/2 ⇒ 该续
	hub, reload := newRenewHub(t, created, now.Add(time.Hour), now)
	rr := callConsole(t, hub)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("请求应放行, got %d", rr.Code)
	}
	got := reload()
	want := now.Add(consoleSessionTTL)
	if !got.ExpiresAt.Equal(want) {
		t.Fatalf("应滑到 %v, 实际 %v", want, got.ExpiresAt)
	}
	if renewedCookie(t, rr) == nil {
		t.Fatal("🔴 只延后 DB 不够:cookie 的 MaxAge 在登录那刻写死,必须一并重发")
	}
}

// realLoginCookie 走一次**真实登录流程**,取登录侧实际发出的那个 Set-Cookie。
//
// 🔴 不能用 newConsoleSessionCookie() 造 want:那样 want 与 got 同源(都出自生产构造器),
//    断言就退化成「续期 == 构造器」,而不是「续期 == 登录」——
//    **登录侧被改成不走构造器时它不会红**,而那恰恰是保留这条断言唯一要防的情形。
//    (组长 2026-09-18 复核发现;假绿的又一种:want 与 got 同源。)
func realLoginCookie(t *testing.T) *http.Cookie {
	t.Helper()
	h, _ := newLoginHub(&model.ConsolePrincipal{ID: "owner-a", Active: true})
	code, err := h.consoleLogins().issue(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.dispatchConsoleLogin(context.Background(), msgFrom("ou_owner", "app"), "#console "+code); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.HandleConsoleLoginStatus(rr, httptest.NewRequest(http.MethodGet, "/console/login/status?code="+code, nil))
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("登录未发出唯一 cookie: %v", cookies)
	}
	return cookies[0]
}

// 🔴 组长定的硬闸:续期的 Set-Cookie 必须与**登录实际发出的那个**逐属性一致。
// 漏一个 Secure 不会让任何断言变红,只会让 cookie 允许明文发送 ⇒ 这里逐项比。
func TestRenewedCookieMatchesLoginCookieAttributeByAttribute(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	hub, _ := newRenewHub(t, now.Add(-11*time.Hour), now.Add(time.Hour), now)
	rr := callConsole(t, hub)
	got := renewedCookie(t, rr)
	if got == nil {
		t.Fatal("未重发 cookie")
	}
	want := realLoginCookie(t)
	if got.Name != want.Name || got.Path != want.Path ||
		got.MaxAge != want.MaxAge || got.HttpOnly != want.HttpOnly ||
		got.Secure != want.Secure || got.SameSite != want.SameSite {
		t.Fatalf("续期 cookie 与【登录实发】cookie 不一致\n续期 %+v\n登录 %+v", got, want)
	}
	// Value 不参与上面的比较:登录发的是它新签的 token,续期发的必须是**手上这张**。
	// 续期若换了 token,等于把同一会话的凭据在无声中轮换掉。
	if got.Value != "raw-a" {
		t.Fatalf("续期必须重发原 token,实际 %q", got.Value)
	}
	// 🔴 上面比的是「续期 == 登录」,那是**一致性**,不是**正确性**:
	//    两侧【一致地】变差(共用构造器本身被降级)时,一致性断言全绿。
	//    实证:把 newConsoleSessionCookie 的 Secure 改成 false,本条曾仍 PASS,
	//    只有既有的 TestConsoleLoginHTTPPageToCookie 变红 —— 保障落在别人的测试上。
	//    ⇒ 这里再钉一遍绝对属性,不依赖另一条测试继续存在。
	if !got.Secure || !got.HttpOnly || got.SameSite != http.SameSiteLaxMode || got.Path != "/" {
		t.Fatalf("续期 cookie 的绝对属性不合格: %+v", got)
	}
}

func TestConsoleSessionNotRenewedWhileMoreThanHalfRemains(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	expires := now.Add(11 * time.Hour) // 剩 11h > 6h ⇒ 不该写库
	hub, reload := newRenewHub(t, now.Add(-time.Hour), expires, now)
	rr := callConsole(t, hub)
	if got := reload(); !got.ExpiresAt.Equal(expires) {
		t.Fatalf("不该续期, 实际被改成 %v", got.ExpiresAt)
	}
	if renewedCookie(t, rr) != nil {
		t.Fatal("没续期就不该重发 cookie")
	}
}

// D1-b:『一直打开不用重登』≠『永远不用重登』。30 天绝对上限必须封顶。
func TestConsoleSessionCappedByAbsoluteTTL(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	created := now.Add(-consoleSessionAbsoluteTTL + 2*time.Hour) // 距上限只剩 2h
	hub, reload := newRenewHub(t, created, now.Add(time.Hour), now)
	callConsole(t, hub)
	want := created.Add(consoleSessionAbsoluteTTL)
	if got := reload(); !got.ExpiresAt.Equal(want) {
		t.Fatalf("应封顶到 %v, 实际 %v", want, got.ExpiresAt)
	}
}

func TestConsoleSessionAtAbsoluteCapStopsRenewing(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	created := now.Add(-consoleSessionAbsoluteTTL + time.Hour)
	expires := created.Add(consoleSessionAbsoluteTTL) // 已经贴顶
	hub, reload := newRenewHub(t, created, expires, now)
	rr := callConsole(t, hub)
	if got := reload(); !got.ExpiresAt.Equal(expires) {
		t.Fatalf("贴顶后不得再延, 实际 %v", got.ExpiresAt)
	}
	if renewedCookie(t, rr) != nil {
		t.Fatal("贴顶后不该重发 cookie")
	}
}

// 写放大闸:一分钟内多个标签页同时轮询,只应写一次库。
func TestConsoleSessionTouchThrottled(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	hub, reload := newRenewHub(t, now.Add(-11*time.Hour), now.Add(time.Hour), now)
	callConsole(t, hub)
	first := reload().ExpiresAt
	hub.NowFunc = func() time.Time { return now.Add(30 * time.Second) }
	rr := callConsole(t, hub)
	if got := reload(); !got.ExpiresAt.Equal(first) {
		t.Fatalf("30 秒内不应再写库, %v -> %v", first, got.ExpiresAt)
	}
	if renewedCookie(t, rr) != nil {
		t.Fatal("被节流时不该重发 cookie")
	}
}

// 阴性:已吊销的会话进不来(「也不得被续活」在 store 层单测,见 console_auth_touch_test.go)。
func TestRevokedConsoleSessionIsRejected(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	hub, db, ctx := newTestHub(t)
	hub.consoleAuthEnabled = true
	hub.NowFunc = func() time.Time { return now }
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "a", Provider: "feishu", ProviderIssuer: "app", ProviderSubject: "ua", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConsoleSession(ctx, model.ConsoleSession{ID: "login-a", PrincipalID: "a", TokenHash: hashConsoleToken("raw-a"), ExpiresAt: now.Add(time.Hour)}, now.Add(-11*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeConsoleSession(ctx, "login-a", now); err != nil {
		t.Fatal(err)
	}
	if rr := callConsole(t, hub); rr.Code != http.StatusUnauthorized {
		t.Fatalf("已吊销应 401, got %d", rr.Code)
	}
}

// CreatedAt 取不到时 fail-closed:不续,让用户重登,而不是让上限未知的会话无限延长。
func TestConsoleSessionWithoutCreatedAtIsNotRenewed(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	hub, _ := newRenewHub(t, now.Add(-11*time.Hour), now.Add(time.Hour), now)
	session := &model.ConsoleSession{ID: "login-a", ExpiresAt: now.Add(time.Hour)}
	req := httptest.NewRequest("GET", "/api/console/agents", nil)
	rr := httptest.NewRecorder()
	repo, ok := hub.Repo.(consoleAuthRepository)
	if !ok {
		t.Fatal("repo 不支持 console auth")
	}
	hub.renewConsoleSession(req, rr, repo, session, "raw-a", now)
	if !session.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatal("CreatedAt 为零值时不得续期")
	}
	if renewedCookie(t, rr) != nil {
		t.Fatal("不续期就不该发 cookie")
	}
}
