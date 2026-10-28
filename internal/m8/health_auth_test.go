package m8

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func healthFixture(t *testing.T) (*healthAuthHandler, []byte) {
	t.Helper()
	key := []byte(strings.Repeat("test-monitor-only-", 3))
	path := filepath.Join(t.TempDir(), "monitor.key")
	if err := os.WriteFile(path, key, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WP_HEALTH_SECRET_FILE", path)
	t.Setenv("WP_HEALTH_TARGET", "staging-hub")
	h := NewHealthAuthHandler().(*healthAuthHandler)
	h.now = func() time.Time { return time.Unix(1800000000, 0) }
	return h, key
}
func healthRequest(key []byte, ts int64, nonce, target, path string) *http.Request {
	timestamp := fmt.Sprint(ts)
	mac := hmac.New(sha256.New, key)
	fmt.Fprint(mac, "dingwei-health/v1\nGET\n"+HealthAuthPath+"\n"+target+"\n"+timestamp+"\n"+nonce)
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("X-DW-Health-Timestamp", timestamp)
	r.Header.Set("X-DW-Health-Nonce", nonce)
	r.Header.Set("X-DW-Health-Signature", hex.EncodeToString(mac.Sum(nil)))
	return r
}
func TestHealthAuthDecisions(t *testing.T) {
	for _, name := range []string{"valid", "wrong_signature", "expired", "future", "missing_key", "short_key", "oversize_key", "wrong_target", "wrong_path", "query", "missing_signature", "duplicate_header", "malformed_nonce", "missing_target"} {
		t.Run(name, func(t *testing.T) {
			h, key := healthFixture(t)
			ts := h.now().Unix()
			target := "staging-hub"
			path := HealthAuthPath
			nonce := strings.Repeat("a", 32)
			switch name {
			case "wrong_signature":
				key = []byte("another-key-with-at-least-32-bytes")
			case "expired":
				ts -= 61
			case "future":
				ts += 61
			case "missing_key":
				h.secretFile += ".absent"
			case "short_key":
				_ = os.WriteFile(h.secretFile, []byte("short"), 0600)
			case "oversize_key":
				_ = os.WriteFile(h.secretFile, []byte(strings.Repeat("k", 4097)), 0600)
			case "wrong_target":
				target = "prod-hub"
			case "wrong_path":
				path = "/internal/other"
			case "query":
				path += "?private=value"
			case "malformed_nonce":
				nonce = "short"
			case "missing_target":
				h.target = ""
			}
			r := healthRequest(key, ts, nonce, target, path)
			if name == "missing_signature" {
				r.Header.Del("X-DW-Health-Signature")
			}
			if name == "duplicate_header" {
				r.Header.Add("X-DW-Health-Nonce", nonce)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := 401
			if name == "valid" {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("%s decision: got %d want %d", name, w.Code, want)
			}
			if name == "valid" {
				var payload map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				if len(payload) != 3 || payload["health"] != "ok" || payload["target"] != "staging-hub" || payload["version"] != "0.2.3.15" {
					t.Fatalf("response allowlist: %v", payload)
				}
			}
		})
	}
}
func TestHealthAuthConcurrentReplayAndCapacity(t *testing.T) {
	h, key := healthFixture(t)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, healthRequest(key, h.now().Unix(), strings.Repeat("b", 32), h.target, HealthAuthPath))
			if w.Code == 200 {
				successes.Add(1)
			} else if w.Code != 401 {
				t.Errorf("replay status %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("replay accepted %d times", successes.Load())
	}
	// Unauthenticated requests cannot consume a valid nonce.
	r := healthRequest(key, h.now().Unix(), strings.Repeat("c", 32), h.target, HealthAuthPath)
	r.Header.Set("X-DW-Health-Signature", strings.Repeat("0", 64))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, healthRequest(key, h.now().Unix(), strings.Repeat("c", 32), h.target, HealthAuthPath))
	if w.Code != 200 {
		t.Fatalf("invalid signature poisoned nonce: %d", w.Code)
	}
	for i := 0; i < 10000; i++ {
		h.nonces[fmt.Sprint(i)] = h.now().Unix() + 60
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, healthRequest(key, h.now().Unix(), strings.Repeat("d", 32), h.target, HealthAuthPath))
	if w.Code != 401 {
		t.Fatalf("capacity did not fail closed: %d", w.Code)
	}
}
func TestHealthAuthPublicRoutingBoundary(t *testing.T) {
	// The production wiring must publish the health handler on internalMux only.
	source, err := os.ReadFile("../../cmd/workpulse/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(source), "m8.NewHealthAuthHandler()") != 1 {
		t.Fatal("health handler must have exactly one internal registration")
	}
	if strings.Count(string(source), `internalMux.Handle("GET "+m8.HealthAuthPath, m8.NewHealthAuthHandler())`) != 1 {
		t.Fatal("missing internal-only health registration")
	}
	configs, err := filepath.Glob("../../deploy/nginx*.conf")
	if err != nil || len(configs) == 0 {
		t.Fatal("missing nginx fixtures")
	}
	for _, path := range configs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), HealthAuthPath) {
			t.Fatalf("health route exposed in nginx config %s", path)
		}
	}
}

func TestHealthAuthViewProxyNeverReachesInternalUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer upstream.Close()
	h := &Hub{}
	for _, path := range []string{"/view/test/internal/health/auth", "/view/test/internal/control/view-page/unlock", "/view/test/%69nternal/health/auth"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		if !h.proxyToViewService(w, r, "test", upstream.URL) || w.Code != 404 {
			t.Fatalf("internal proxy path %s status %d", path, w.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("internal endpoint forwarded %d times", calls.Load())
	}
}
