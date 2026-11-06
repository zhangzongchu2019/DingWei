package m8

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestR5DefaultOffAndMissingConfig(t *testing.T) {
	for _, enabled := range []string{"", "1"} {
		t.Run("enabled="+enabled, func(t *testing.T) {
			t.Setenv("DW_OPENSPEC_READ_ENABLED", enabled)
			t.Setenv("DW_OPENSPEC_GITHUB_CREDENTIAL_FILE", "")
			h := newConsolePlatformTestHub(t)
			if h.SpecReader != nil {
				t.Fatal("configured without credentials")
			}
			for _, handler := range []http.HandlerFunc{h.HandleOpenSpecRead, h.HandleOpenSpecReadPage, h.HandleOpenSpecBinding} {
				r := httptest.NewRequest("GET", "/", nil)
				w := httptest.NewRecorder()
				handler(w, r)
				if w.Code != 503 {
					t.Fatal("disabled status", w.Code)
				}
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/console/platform", nil)
			r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
			h.HandleConsolePlatform(w, r)
			if strings.Contains(w.Body.String(), "/console/openspec\"") {
				t.Fatal("disabled entry visible")
			}
		})
	}
}
func TestR5BindingJSONOriginAndReadPage(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	h.SpecReader = &specread.Service{Bindings: h.Repo.(specread.Bindings)}
	b := specread.Binding{Principal: "owner", Provider: "github", Instance: "github.com", AccountID: "123", Login: "alice"}
	body, _ := json.Marshal(b)
	for _, origin := range []string{"", "https://evil.invalid", "http://example.com"} {
		r := httptest.NewRequest("PUT", "http://example.com/api/console/openspec-bindings", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
		w := httptest.NewRecorder()
		h.HandleOpenSpecBinding(w, r)
		if origin == "http://example.com" {
			if w.Code != 204 {
				t.Fatal(w.Code, w.Body)
			}
		} else if w.Code != 403 {
			t.Fatal("unsafe origin accepted", origin, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/console/openspec", nil)
	w := httptest.NewRecorder()
	h.HandleOpenSpecReadPage(w, r)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "img-src 'none'") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'sha256-") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("page missing policy")
	}
	if strings.Contains(w.Body.String(), "/*READ_SCRIPT*/") {
		t.Fatal("script not embedded")
	}
}

func TestR5BindingUsesTrustedOriginPolicy(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	h.SpecReader = &specread.Service{Bindings: h.Repo.(specread.Bindings)}
	h.PublicOrigin = "https://example.com"
	list, err := trustedorigin.ParseList("https://example.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	h.TrustedOrigins = list
	b := specread.Binding{Principal: "owner", Provider: "github", Instance: "github.com", AccountID: "123", Login: "alice"}
	body, _ := json.Marshal(b)
	for _, tc := range []struct {
		name, target, source string
		want                 int
	}{
		{"canonical", "https://example.com/api/console/openspec-bindings", "https://example.com", 204},
		{"allowlist with normalized case", "https://EXAMPLE.COM:50443/api/console/openspec-bindings", "HTTPS://EXAMPLE.COM:50443", 204},
		{"wrong port", "https://example.com:50444/api/console/openspec-bindings", "https://example.com:50444", 403},
		{"http impersonation", "http://example.com:50443/api/console/openspec-bindings", "http://example.com:50443", 403},
		{"missing origin", "https://example.com:50443/api/console/openspec-bindings", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPut, tc.target, strings.NewReader(string(body)))
			r.Header.Set("Content-Type", "application/json")
			if tc.source != "" {
				r.Header.Set("Origin", tc.source)
			}
			r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
			w := httptest.NewRecorder()
			h.HandleOpenSpecBinding(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
func TestR5AdminBindingMiddlewareCannotUseManagement(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	called := false
	handler := h.RequireConsolePlatformAdmin(func(w http.ResponseWriter, r *http.Request) { called = true })
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/console/openspec-bindings", nil)
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
	handler(w, r)
	if w.Code != 403 || called {
		t.Fatal("management role passed platform admin gate")
	}
}

func TestR5FeatureConfigurationAllGates(t *testing.T) {
	file := t.TempDir() + "/credential"
	if err := os.WriteFile(file, []byte("simulation-only-not-a-real-token"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"disabled", "missing_file", "missing_repositories", "invalid_repositories", "ready"} {
		t.Run(name, func(t *testing.T) {
			enabled, credential, repos := "1", file, "1"
			switch name {
			case "disabled":
				enabled = ""
			case "missing_file":
				credential = ""
			case "missing_repositories":
				repos = ""
			case "invalid_repositories":
				repos = "https://evil.invalid"
			}
			t.Setenv("DW_OPENSPEC_READ_ENABLED", enabled)
			t.Setenv("DW_OPENSPEC_GITHUB_CREDENTIAL_FILE", credential)
			t.Setenv("DW_OPENSPEC_GITHUB_REPOSITORY_IDS", repos)
			h := newConsolePlatformTestHub(t)
			if (h.SpecReader != nil) != (name == "ready") {
				t.Fatalf("configuration guard %s", name)
			}
		})
	}
}

type r5HTTPProvider struct {
	allow bool
	size  int64
}

func (*r5HTTPProvider) Repository(context.Context, string) (specread.Repository, error) {
	return specread.Repository{ID: "1", FullName: "owner/repo"}, nil
}
func (p *r5HTTPProvider) Authorize(context.Context, specread.Repository, specread.Binding) error {
	if !p.allow {
		return specread.ErrForbidden
	}
	return nil
}
func (*r5HTTPProvider) Resolve(context.Context, specread.Repository, string) (string, error) {
	return strings.Repeat("a", 40), nil
}
func (p *r5HTTPProvider) Tree(context.Context, specread.Repository, string) ([]specread.Entry, error) {
	size := p.size
	if size == 0 {
		size = 70000
	}
	return []specread.Entry{{Path: "openspec/spec.md", SHA: strings.Repeat("b", 40), Mode: "100644", Type: "blob", Size: size}}, nil
}
func (*r5HTTPProvider) Blob(context.Context, specread.Repository, specread.Entry) ([]byte, error) {
	return []byte(strings.Repeat("x", 69997) + "END"), nil
}
func (*r5HTTPProvider) Source(_ specread.Repository, sha, p string) string {
	return "https://github.com/owner/repo/blob/" + sha + "/" + p
}
func TestR5ReadHTTPQueriesAndNoOldBodyOnDenial(t *testing.T) {
	h := newConsolePlatformTestHub(t)
	bindings := h.Repo.(specread.Bindings)
	if e := bindings.SetSpecReadBinding(context.Background(), "admin", specread.Binding{Principal: "manager", Provider: "github", Instance: "github.com", AccountID: "123", Login: "alice"}); e != nil {
		t.Fatal(e)
	}
	p := &r5HTTPProvider{allow: true}
	h.SpecReader = &specread.Service{Bindings: bindings, Providers: map[string]specread.Provider{"github:github.com": p}, Repositories: map[string]bool{"github:github.com:1": true}}
	query := "/api/console/openspec-read?provider=github&instance=github.com&repository=1&ref=main&path=openspec/spec.md"
	call := func(u string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", u, nil)
		r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
		w := httptest.NewRecorder()
		h.HandleOpenSpecRead(w, r)
		return w
	}
	w := call(query)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "END") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("HTTP read failed", w.Code)
	}
	for _, suffix := range []string{"&ref=other", "&host=evil.invalid", "&commit=" + strings.Repeat("a", 40)} {
		if w := call(query + suffix); w.Code != 400 || strings.Contains(w.Body.String(), "END") {
			t.Fatal("ambiguous query accepted", w.Code)
		}
	}
	p.size = specread.MaxFile + 1
	w = call(query)
	if w.Code != 413 || !strings.Contains(w.Body.String(), "文件过大，请在仓库托管平台查看") || !strings.Contains(w.Body.String(), "https://github.com/owner/repo/blob/"+strings.Repeat("a", 40)+"/openspec/spec.md") || strings.Contains(w.Body.String(), "END") {
		t.Fatal("oversize feedback missing immutable source", w.Code, w.Body)
	}
	p.allow = false
	w = call(query)
	if w.Code != 403 || strings.Contains(w.Body.String(), "END") || strings.Contains(w.Body.String(), "text\"") {
		t.Fatal("denial leaked previous response", w.Code)
	}
}

func TestR5ErrorRedactsBackendDetails(t *testing.T) {
	w := httptest.NewRecorder()
	specReadError(w, errors.New("simulation-private-token-and-path"), specread.Result{})
	if w.Code != 503 || strings.Contains(w.Body.String(), "simulation-private") || !strings.Contains(w.Body.String(), "服务不可用") {
		t.Fatal("backend details leaked")
	}
}
