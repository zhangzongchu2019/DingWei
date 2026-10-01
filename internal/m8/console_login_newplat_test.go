package m8

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 新平台登录页的结构级闸：行为由 view-service/console-login-regression.js 在真浏览器里验。
func TestNewPlatformLoginPageServedPublicNoStoreAndHoldsNoSecrets(t *testing.T) {
	hub, _, _ := newTestHub(t)
	rr := httptest.NewRecorder()
	hub.HandleNewPlatformLoginPage(rr, httptest.NewRequest(http.MethodGet, "/console/login", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d", rr.Code)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("登录页不得被缓存: Cache-Control=%q", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("登录页不得被嵌入 iframe: X-Frame-Options=%q", got)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`id=pwPass type=password`, `id=chOld type=password`, `id=chNew type=password`, `id=chNew2 type=password`,
		`/console/login/password`, `/console/login/code`, `/console/login/status`, `/api/console/account/password`,
		"must_change_password",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("登录页缺少 %q", want)
		}
	}
	// 密码与会话令牌不得进浏览器存储或日志
	for _, forbidden := range []string{"localStorage", "sessionStorage", "console.log", "document.cookie"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("登录页含 %q:密码/会话不得落浏览器存储或进日志", forbidden)
		}
	}
}
