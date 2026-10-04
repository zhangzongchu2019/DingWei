package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startRealViewService(t *testing.T, hubInternal string, v2 bool, secretFile string) string {
	t.Helper()
	port := freeTCPPort(t)
	tmp := t.TempDir()
	cmd := exec.Command("python3", "../../view-service/view_service.py")
	cmd.Env = append(os.Environ(),
		"VS_PORT="+strconv.Itoa(port),
		"VS_HUB_INTERNAL="+hubInternal,
		"VS_STATE_FILE="+filepath.Join(tmp, "state.json"),
		"VS_PAGE_DB="+filepath.Join(tmp, "pages.db"),
		"VS_VIEW_UNLOCK_V2="+map[bool]string{false: "0", true: "1"}[v2],
		"VS_TARGET_NAME=canary",
		"VS_TO_HUB_SECRET_FILE="+secretFile,
	)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, base+"/status", nil)
		req.Header.Set("X-DW-Session", "kimi.zzc")
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
			return base
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("view-service did not start: %s", output.String())
	return ""
}

func TestNewPlatformOwnerInputThroughRealViewServiceModes(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "v2"}[v2], func(t *testing.T) {
			e := newShimEnv(t)
			agent, status, body := e.dial(e.zKimiCred, "")
			if agent == nil {
				t.Fatalf("agent dial: %d %s", status, body)
			}
			defer agent.Close(websocket.StatusNormalClosure, "done")
			waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "kimi")

			secretFile := filepath.Join(t.TempDir(), "vs-to-hub.key")
			if err := os.WriteFile(secretFile, []byte("abcdef0123456789abcdef0123456789"), 0o600); err != nil {
				t.Fatal(err)
			}
			internalMux := http.NewServeMux()
			internalMux.HandleFunc("POST /internal/view/authorize", e.hub.HandleInternalViewAuthorize)
			internalMux.HandleFunc("POST /internal/view/{session}/input", e.hub.HandleInternalViewInput)
			internalMux.HandleFunc("POST /internal/view/{session}/interrupt", e.hub.HandleInternalViewInterrupt)
			internalMux.HandleFunc("POST /internal/view/{session}/cancel", e.hub.HandleInternalViewCancel)
			internalMux.HandleFunc("POST /internal/view-v2/{session}/input", e.hub.HandleInternalViewV2Input)
			internal := httptest.NewServer(internalMux)
			defer internal.Close()
			vsURL := startRealViewService(t, internal.URL, v2, secretFile)

			configFile := filepath.Join(t.TempDir(), "routes.json")
			rawConfig, _ := json.Marshal(map[string]any{
				"sessions": map[string]string{"kimi.zzc": "canary"},
				"targets": map[string]any{"canary": map[string]any{
					"url": vsURL, "view_unlock_v2": true, "vs_to_hub_secret_file": secretFile,
				}},
			})
			if err := os.WriteFile(configFile, rawConfig, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("VIEW_ROUTING_CONFIG", configFile)
			resetViewRoutingCacheForTest()
			defer resetViewRoutingCacheForTest()

			frontMux := http.NewServeMux()
			frontMux.HandleFunc("GET /view/{sessionName}", e.hub.RequireConsoleAgent("view", e.hub.HandleTerminalViewPage))
			frontMux.HandleFunc("/view/{sessionName}/{rest...}", e.hub.RequireConsoleViewSubPath(e.hub.HandleViewSubPath))
			frontMux.HandleFunc("POST /api/console/agents/{sessionName}/capability", e.hub.RequireConsoleAgent("interact", e.hub.HandleConsoleCapability))
			front := httptest.NewServer(frontMux)
			defer front.Close()
			if !v2 {
				for name, token := range map[string]string{"unauthenticated": "", "non-owner": e.tokLBL} {
					req, _ := http.NewRequest(http.MethodGet, front.URL+"/view/kimi.zzc", nil)
					if token != "" {
						req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
					}
					res, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					res.Body.Close()
					if res.StatusCode == http.StatusOK {
						t.Fatalf("%s opened owner view", name)
					}
				}
				e.hub.mu.Lock()
				writer := e.hub.terminals[terminalKey(AgentKeyID("zzc"), "kimi")].writerPage
				e.hub.mu.Unlock()
				if writer != "" {
					t.Fatalf("rejected viewer gained writer grant: %q", writer)
				}
			}

			do := func(method, path string, payload any, headers map[string]string) (int, []byte) {
				var body io.Reader
				if payload != nil {
					raw, _ := json.Marshal(payload)
					body = bytes.NewReader(raw)
				}
				req, _ := http.NewRequest(method, front.URL+path, body)
				req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
				if method != http.MethodGet {
					req.Header.Set("Origin", npOrigin)
					req.Header.Set("Content-Type", "application/json")
				}
				for key, value := range headers {
					req.Header.Set(key, value)
				}
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				raw, _ := io.ReadAll(res.Body)
				return res.StatusCode, raw
			}

			if status, _ := do(http.MethodGet, "/view/kimi.zzc", nil, nil); status != http.StatusOK {
				t.Fatalf("owner view page: %d", status)
			}
			var inputPayload map[string]any
			headers := map[string]string{}
			if v2 {
				status, raw := do(http.MethodPost, "/view/kimi.zzc/page", nil, nil)
				var page struct {
					PageID    string `json:"page_id"`
					PageToken string `json:"page_token"`
				}
				_ = json.Unmarshal(raw, &page)
				if status != http.StatusCreated || page.PageID == "" || page.PageToken == "" {
					t.Fatalf("create page: %d %s", status, raw)
				}
				status, raw = do(http.MethodPost, "/api/console/agents/kimi.zzc/capability", map[string]string{"page_id": page.PageID}, nil)
				var capability struct {
					Capability string `json:"capability"`
				}
				_ = json.Unmarshal(raw, &capability)
				if status != http.StatusOK || capability.Capability == "" {
					t.Fatalf("issue capability: %d %s", status, raw)
				}
				headers["X-DW-Console-Page"] = page.PageID
				headers["X-DW-Console-Capability"] = capability.Capability
				inputPayload = map[string]any{"text": "owner via real view service", "page_id": page.PageID, "page_token": page.PageToken, "request_id": "0123456789abcdef0123456789abcdef"}
			} else {
				// The readiness probe registered the real VS virtual viewer. The owner page
				// grant makes legacy /status and /input use its normal writer token.
				if status, raw := do(http.MethodGet, "/view/kimi.zzc/status", nil, nil); status != http.StatusOK || !strings.Contains(string(raw), `"can_write": true`) {
					t.Fatalf("legacy owner grant: %d %s", status, raw)
				}
				inputPayload = map[string]any{"text": "owner via real view service"}
			}
			if status, raw := do(http.MethodPost, "/view/kimi.zzc/input", inputPayload, headers); status != http.StatusOK {
				t.Fatalf("view input: %d %s", status, raw)
			}
			readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			env := readEnvelope(t, readCtx, agent)
			if env.Body != "owner via real view service\r" {
				t.Fatalf("agent body=%q", env.Body)
			}
		})
	}
}

func TestNewPlatformLegacyViewControlUsesInternalSession(t *testing.T) {
	e := newViewEnv(t)
	agent, status, body := e.dial(e.zKimiCred, "")
	if agent == nil {
		t.Fatalf("agent dial: %d %s", status, body)
	}
	defer agent.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "kimi")
	_, viewerID, code, err := e.hub.registerViewViewer("kimi.zzc")
	if err != nil {
		t.Fatal(err)
	}
	token, err := e.hub.grantTerminalInput(AgentKeyID("zzc"), "kimi", code)
	if err != nil {
		t.Fatal(err)
	}
	e.hub.mu.Lock()
	granted := e.hub.terminals[terminalKey(AgentKeyID("zzc"), "kimi")]
	if granted == nil || granted.writerPage != viewerID || granted.writerToken != token {
		e.hub.mu.Unlock()
		t.Fatalf("grant state missing: state=%+v viewer=%q token=%q", granted, viewerID, token)
	}
	e.hub.mu.Unlock()
	resolvedKey, resolvedSession, resolved := e.hub.viewInternalSession(context.Background(), "kimi.zzc")
	if !resolved || resolvedKey != AgentKeyID("zzc") || resolvedSession != "kimi" {
		t.Fatalf("resolved target=%q/%q ok=%v", resolvedKey, resolvedSession, resolved)
	}

	interruptBody, _ := json.Marshal(viewInterruptRequest{ViewerID: viewerID, Token: token, DeliveryID: "delivery-1", TurnID: "turn-1"})
	var decoded viewInterruptRequest
	_ = json.Unmarshal(interruptBody, &decoded)
	if decoded.ViewerID != viewerID || decoded.Token != token {
		t.Fatalf("interrupt encoding lost grant: %+v", decoded)
	}
	interrupt := httptest.NewRequest(http.MethodPost, "/internal/view/kimi.zzc/interrupt", bytes.NewReader(interruptBody))
	interrupt.SetPathValue("session", "kimi.zzc")
	interruptResult := httptest.NewRecorder()
	e.hub.HandleInternalViewInterrupt(interruptResult, interrupt)
	if interruptResult.Code != http.StatusOK {
		t.Fatalf("legacy interrupt by full_name: %d %s", interruptResult.Code, interruptResult.Body.String())
	}
	readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	env := readEnvelope(t, readCtx, agent)
	if metaString(env.Meta, "type") != terminalInterruptType || env.Meta["delivery_id"] != "delivery-1" {
		t.Fatalf("interrupt envelope: %+v", env)
	}

	cancelBody, _ := json.Marshal(viewCancelRequest{ViewerID: viewerID, Token: token, MessageID: "missing-message", DeliveryID: "missing-delivery"})
	cancelReq := httptest.NewRequest(http.MethodPost, "/internal/view/kimi.zzc/cancel", bytes.NewReader(cancelBody))
	cancelReq.SetPathValue("session", "kimi.zzc")
	cancelResult := httptest.NewRecorder()
	e.hub.HandleInternalViewCancel(cancelResult, cancelReq)
	if cancelResult.Code == http.StatusForbidden {
		t.Fatalf("legacy cancel full_name failed before delivery lookup: %d %s", cancelResult.Code, cancelResult.Body.String())
	}
	if cancelResult.Code == http.StatusOK {
		t.Fatal("missing delivery unexpectedly cancelled")
	}

}
