package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func newplatHub(t *testing.T) *m8.Hub {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "np.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m8.New(db, m8.ConsoleAuthDisabled())
}

func TestNewPlatformConfigFromEnv(t *testing.T) {
	t.Setenv("DW_NEWPLAT", "")
	if _, on, err := newPlatformConfigFromEnv(); on || err != nil {
		t.Fatalf("off by default: on=%v err=%v", on, err)
	}
	t.Setenv("DW_NEWPLAT", "1")
	t.Setenv("DW_TRUSTED_ORIGINS", "")
	t.Setenv("DW_PUBLIC_ORIGIN", "")
	if _, on, err := newPlatformConfigFromEnv(); !on || !errors.Is(err, errNewPlatformConfig) {
		t.Fatalf("missing origin: on=%v err=%v", on, err)
	}
	t.Setenv("DW_PUBLIC_ORIGIN", "https://dw.cmjrt.com:55055")
	t.Setenv("DW_NEWPLAT_PG_DSN", "")
	if _, on, err := newPlatformConfigFromEnv(); !on || !errors.Is(err, errNewPlatformPGDSN) {
		t.Fatalf("missing PG DSN: on=%v err=%v", on, err)
	}
	t.Setenv("DW_NEWPLAT_PG_DSN", "postgres://h/db")
	t.Setenv("DW_TRUST_PROXY", "1")
	t.Setenv("DW_TRUSTED_ORIGINS", " https://dw123.cmjrt.com:50443 ")
	cfg, on, err := newPlatformConfigFromEnv()
	if !on || err != nil || cfg.publicOrigin != "https://dw.cmjrt.com:55055" || cfg.pgDSN != "postgres://h/db" || !cfg.trustProxy || len(cfg.trustedOrigins) != 1 || cfg.trustedOrigins[0].Host != "dw123.cmjrt.com" {
		t.Fatalf("cfg=%+v on=%v err=%v", cfg, on, err)
	}
	for _, invalid := range []string{"   ", ",https://a.example:50443", "https://a.example:50443,HTTPS://A.EXAMPLE:50443", "http://a.example:50443", "https://a.example:*"} {
		t.Setenv("DW_TRUSTED_ORIGINS", invalid)
		if _, on, err := newPlatformConfigFromEnv(); !on || err == nil || !strings.Contains(err.Error(), "DW_TRUSTED_ORIGINS") {
			t.Fatalf("invalid DW_TRUSTED_ORIGINS %q: on=%v err=%v", invalid, on, err)
		}
	}
}

// 不再有自动建号：空库上装配成功，但一个用户也不建（第一个管理用户由初始化脚本建）。
func TestWireNewPlatformCreatesNoUser(t *testing.T) {
	hub := newplatHub(t)
	store := identity.NewMemStore()
	if err := wireNewPlatform(context.Background(), http.NewServeMux(), hub, newPlatformConfig{publicOrigin: "https://x"}, store, nil); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.CountUsers(context.Background()); n != 0 {
		t.Fatalf("users=%d after wiring, want 0 (no systemdefault, no auto-create)", n)
	}
}

func TestWireNewPlatformRoutes(t *testing.T) {
	hub := newplatHub(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS) // main.go 照常注册旧入口，由 Hub 回 410
	if err := wireNewPlatform(context.Background(), mux, hub, newPlatformConfig{publicOrigin: "https://x"}, identity.NewMemStore(), nil); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	get := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b := make([]byte, 256)
		n, _ := res.Body.Read(b)
		return res.StatusCode, strings.TrimSpace(string(b[:n]))
	}
	checks := []struct {
		method, path, body string
		status             int
		want               string
	}{
		{"POST", "/api/enroll/applications", `{"owner_user_id":"nosuch","pairing_salt":"000102030405060708090a0b0c0d0e0f","pairing_digest":"99c90773ea9ce66a355bd6a2a6e55122c3caf5a164785c90b1d58c2d63bb8680","device_fingerprint":"d","client_version":"v"}`, 404, `"unknown_owner"`},
		{"GET", "/api/console/agents", "", 401, `"unauthenticated"`},
		{"GET", "/console/login/status?code=X", "", 200, `"pending"`},
		{"GET", "/ws/agent", "", 401, `"credential_invalid"`},
		{"GET", "/ws/session/x?key_id=k", "", 410, `"use_ws_agent"`},
	}
	for _, c := range checks {
		st, body := get(c.method, c.path, c.body)
		if st != c.status || !strings.Contains(body, c.want) {
			t.Errorf("%s %s: %d %s, want %d %s", c.method, c.path, st, body, c.status, c.want)
		}
	}
	// 吊销、停用、凭据被取走（旧凭据作废）三处都要让 Hub 断开在线连接；漏接任何一处，生产上那条连接会挂着不断。
	if hub.Identity.OnRevoked == nil || hub.Identity.OnSuspended == nil || hub.Identity.OnCredentialIssued == nil {
		t.Fatalf("disconnect hooks not wired: revoked=%v suspended=%v credential_issued=%v",
			hub.Identity.OnRevoked != nil, hub.Identity.OnSuspended != nil, hub.Identity.OnCredentialIssued != nil)
	}
}

type fakeLoginPager struct{}

func (fakeLoginPager) HandleNewPlatformLoginPage(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("login-page"))
}

// 登录页按接口断言登记：有这个方法就挂到 GET /console/login，没有就不挂（不 panic、不挂占位）。
func TestRegisterNewPlatformLoginPage(t *testing.T) {
	mux := http.NewServeMux()
	if !registerNewPlatformLoginPage(mux, fakeLoginPager{}) {
		t.Fatal("pager with the method must be registered")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/console/login", nil))
	if rec.Code != 200 || rec.Body.String() != "login-page" {
		t.Fatalf("GET /console/login: %d %q", rec.Code, rec.Body.String())
	}
	mux2 := http.NewServeMux()
	if registerNewPlatformLoginPage(mux2, struct{}{}) {
		t.Fatal("value without the method must not be registered")
	}
	rec2 := httptest.NewRecorder()
	mux2.ServeHTTP(rec2, httptest.NewRequest("GET", "/console/login", nil))
	if rec2.Code != 404 {
		t.Fatalf("unregistered route should 404, got %d", rec2.Code)
	}
}
