package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
	t.Setenv("DW_PUBLIC_ORIGIN", "")
	if _, on, err := newPlatformConfigFromEnv(); !on || !errors.Is(err, errNewPlatformConfig) {
		t.Fatalf("missing origin: on=%v err=%v", on, err)
	}
	t.Setenv("DW_PUBLIC_ORIGIN", "https://dw.cmjrt.com:55055")
	t.Setenv("DW_TRUST_PROXY", "1")
	cfg, on, err := newPlatformConfigFromEnv()
	if !on || err != nil || cfg.publicOrigin != "https://dw.cmjrt.com:55055" || !cfg.trustProxy {
		t.Fatalf("cfg=%+v on=%v err=%v", cfg, on, err)
	}
}

func TestWireNewPlatformRefusesWithoutBootstrapSecret(t *testing.T) {
	hub := newplatHub(t)
	pwf := filepath.Join(t.TempDir(), "pw")
	_ = os.WriteFile(pwf, []byte("Boot-Pass-0001"), 0o644) // 权限过宽
	err := wireNewPlatform(context.Background(), http.NewServeMux(), hub, newPlatformConfig{publicOrigin: "https://x", passwordFile: pwf})
	if !errors.Is(err, identity.ErrBootstrapSecret) {
		t.Fatalf("err=%v, want ErrBootstrapSecret", err)
	}
	if hub.Identity != nil || hub.AgentCredentialOnly {
		t.Fatal("hub must not be switched to new-platform mode when bootstrap fails")
	}
}

func TestWireNewPlatformRoutes(t *testing.T) {
	hub := newplatHub(t)
	pwf := filepath.Join(t.TempDir(), "pw")
	_ = os.WriteFile(pwf, []byte("Boot-Pass-0001"), 0o600)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS) // main.go 照常注册旧入口，由 Hub 回 410
	if err := wireNewPlatform(context.Background(), mux, hub, newPlatformConfig{publicOrigin: "https://x", passwordFile: pwf}); err != nil {
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
	if n, _ := hub.Identity.Store.CountUsers(context.Background()); n != 1 {
		t.Fatalf("bootstrap users=%d, want 1", n)
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
