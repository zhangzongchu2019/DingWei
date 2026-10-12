package m8

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMuxSubjectPublicFailureAndLegacyCompatibility(t *testing.T) {
	v := newViewEnv(t)
	key := bytes.Repeat([]byte("k"), 32)
	root := t.TempDir()
	keyPath := filepath.Join(root, "forward")
	var calls, legacy atomic.Int64
	vs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		encoded := r.Header.Get("X-DW-Mux-Subject")
		if encoded == "" {
			legacy.Add(1)
			io.WriteString(w, "legacy")
			return
		}
		body, err := base64.RawURLEncoding.DecodeString(encoded)
		var subject map[string]string
		if err != nil || json.Unmarshal(body, &subject) != nil {
			w.WriteHeader(401)
			return
		}
		signature := signViewServiceRequest(key, hubToVSDirection, r.Method, r.URL.Path, r.Header.Get("X-DW-Timestamp"), r.Header.Get("X-DW-Nonce"), body)
		if r.Header.Get("X-DW-Signature") != signature || subject["full_name"] != "kimi.zzc" || subject["agent_id"] == "forged" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, "projection")
	}))
	defer vs.Close()
	routing := ViewRouting{Default: "v2", Targets: map[string]ViewTarget{"v2": {URL: vs.URL, ViewUnlockV2: true, HubToVSSecretFile: keyPath}, "v1": {URL: vs.URL}}, Sessions: map[string]string{}}
	config := filepath.Join(root, "routing.json")
	update := func() {
		b, _ := json.Marshal(routing)
		if err := os.WriteFile(config, b, 0600); err != nil {
			t.Fatal(err)
		}
		resetViewRoutingCacheForTest()
	}
	t.Setenv("VIEW_ROUTING_CONFIG", config)
	update()
	mux := http.NewServeMux()
	mux.HandleFunc("/view/{sessionName}/{rest...}", v.hub.RequireConsoleViewSubPath(func(w http.ResponseWriter, r *http.Request) {
		v.hub.proxyToViewService(w, r, r.PathValue("sessionName"), vs.URL)
	}))
	public := httptest.NewServer(mux)
	defer public.Close()
	get := func(path, token string) (int, string) {
		req, _ := http.NewRequest("GET", public.URL+path, nil)
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		for _, h := range []string{"X-DW-Mux-Subject", "X-DW-VS-Target", "X-DW-Timestamp", "X-DW-Nonce", "X-DW-Signature"} {
			req.Header.Set(h, "forged")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(b))
	}
	for _, bad := range []string{"missing", "short", "directory", "wrong-signature"} {
		os.RemoveAll(keyPath)
		switch bad {
		case "short":
			os.WriteFile(keyPath, []byte("short"), 0600)
		case "directory":
			os.Mkdir(keyPath, 0700)
		case "wrong-signature":
			os.WriteFile(keyPath, bytes.Repeat([]byte("x"), 32), 0600)
		}
		before := calls.Load()
		for _, ep := range []string{"events", "status"} {
			code, body := get("/view/kimi.zzc/"+ep, v.tokZZC)
			if code != 503 || body != "{\"error\":\"projection_subject_unavailable\"}" {
				t.Fatalf("%s/%s: %d %s", bad, ep, code, body)
			}
		}
		if bad != "wrong-signature" && calls.Load() != before {
			t.Fatal("failed generation reached VS")
		}
		if legacy.Load() != 0 {
			t.Fatal("V2 fell into legacy")
		}
	}
	os.RemoveAll(keyPath)
	os.WriteFile(keyPath, key, 0600)
	for _, ep := range []string{"events", "status"} {
		code, body := get("/view/kimi.zzc/"+ep, v.tokZZC)
		if code != 200 || body != "projection" {
			t.Fatalf("restored: %d %s", code, body)
		}
	}
	if code, _ := get("/view/kimi.zzc/status", v.tokLBL); code != 404 {
		t.Fatalf("foreign actor: %d", code)
	}
	routing.Default = "v1"
	update()
	os.Remove(keyPath)
	if code, body := get("/view/kimi.zzc/status", v.tokZZC); code != 200 || body != "legacy" {
		t.Fatalf("V1 compatibility: %d %s", code, body)
	}
}
