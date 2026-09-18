package m8

import (
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

// 🔴 组长定的硬闸:续期的 Set-Cookie 必须与登录时【逐属性】一致。
// 漏一个 Secure 不会让任何断言变红,只会让 cookie 允许明文发送 ⇒ 这里逐项比。
func TestRenewedCookieMatchesLoginCookieAttributeByAttribute(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	hub, _ := newRenewHub(t, now.Add(-11*time.Hour), now.Add(time.Hour), now)
	rr := callConsole(t, hub)
	got := renewedCookie(t, rr)
	if got == nil {
		t.Fatal("未重发 cookie")
	}
	want := newConsoleSessionCookie("raw-a")
	if got.Name != want.Name || got.Value != want.Value || got.Path != want.Path ||
		got.MaxAge != want.MaxAge || got.HttpOnly != want.HttpOnly ||
		got.Secure != want.Secure || got.SameSite != want.SameSite {
		t.Fatalf("续期 cookie 与登录 cookie 不一致\n续期 %+v\n登录 %+v", got, want)
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
