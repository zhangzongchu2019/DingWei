package m8

// Cross-runtime integration: actual MCP subprocess -> real private Unix IPC ->
// authenticated helper WS + direct Agent HTTP ->
// SQLite revisions/shared read. The mux wires production handlers; this is not
// a production deployment, browser/model test, or full cmd/workpulse process.
// Run from the repository root:
// DW_SPEC_E2E_PYTHON=/absolute/path/to/runtime/python go test ./internal/m8 \
//   -run '^TestSpecMCPEndToEnd$' -count=1 -timeout 3m -v
// Parent under test when introduced: 4df8ce635e3b05c35f2cc91902ca5caefa77b76c.
// The cold approval assertion deliberately remains red on that parent.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type specMCPBridge struct {
	t        *testing.T
	input    *json.Encoder
	scan     *bufio.Scanner
	cmd      *exec.Cmd
	private  []string
	logPaths []string
}

func (b *specMCPBridge) phase(prefix string) string {
	b.t.Helper()
	for b.scan.Scan() {
		line := b.scan.Text()
		if strings.HasPrefix(line, "A3_E2E_ERROR ") {
			b.t.Fatal("local helper/MCP child failed: " + strings.TrimPrefix(line, "A3_E2E_ERROR "))
		}
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	b.t.Fatal("local helper/MCP child ended before phase " + prefix)
	return ""
}
func (b *specMCPBridge) packet(name string, args map[string]any) map[string]any {
	b.t.Helper()
	if err := b.input.Encode(map[string]any{"name": name, "args": args}); err != nil {
		b.t.Fatal(err)
	}
	return b.readPacket()
}
func (b *specMCPBridge) readPacket() map[string]any {
	b.t.Helper()
	wire := b.phase("A3_E2E_RESULT ")
	for _, private := range b.private {
		if private != "" && strings.Contains(wire, private) {
			b.t.Fatal("private credential/capability crossed model boundary")
		}
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(wire), &p); err != nil {
		b.t.Fatal("invalid model RPC JSON")
	}
	return p
}
func (b *specMCPBridge) call(name string, args map[string]any) map[string]any {
	b.t.Helper()
	return b.unwrap(b.packet(name, args))
}
func (b *specMCPBridge) unwrap(p map[string]any) map[string]any {
	b.t.Helper()
	if p["error"] != nil {
		b.t.Fatal("unexpected MCP validation error")
	}
	result, ok := p["result"].(map[string]any)
	if !ok {
		b.t.Fatal("missing RPC result")
	}
	content := result["content"].([]any)[0].(map[string]any)["text"].(string)
	var frame struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(content), &frame); err != nil || !frame.OK || frame.Result == nil {
		b.t.Fatalf("invalid IPC result envelope: ok=%v", frame.OK)
	}
	return frame.Result
}
func scanSpecMCPPrivateLogs(paths, private []string) error {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return errors.New("private log capture unavailable")
		}
		for _, secret := range private {
			if secret != "" && bytes.Contains(data, []byte(secret)) {
				return errors.New("private credential/capability detected in captured logs")
			}
		}
	}
	return nil
}
func (b *specMCPBridge) stop() {
	b.t.Helper()
	if err := b.input.Encode(map[string]bool{"stop": true}); err != nil {
		b.t.Fatal(err)
	}
	b.phase("A3_E2E_CLEAN")
	for b.scan.Scan() {
	} // drain the entire helper stdout tail into its private capture
	if err := b.cmd.Wait(); err != nil {
		b.t.Fatalf("local child exit: %v", err)
	}
	if err := scanSpecMCPPrivateLogs(b.logPaths, b.private); err != nil {
		b.t.Fatal(err)
	}
}
func specE2EArgs(update bool, id string) map[string]any {
	var args map[string]any
	_ = json.Unmarshal([]byte(validSpecBody), &args)
	args["title"] = "Local MCP spec E2E"
	args["acceptance"].([]any)[0].(map[string]any)["generated_by"] = "forged-agent-from-model"
	if update {
		args["spec_id"] = id
		args["expected_revision"] = 1
		args["content_markdown"] = "# Updated\nmsg:1"
	}
	return args
}
func startSpecMCPBridge(t *testing.T, e *shimEnv, python string) *specMCPBridge {
	return startSpecMCPBridgeAs(t, e, python, e.zKimiCred, "Kimi", "u_zzc")
}
func startSpecMCPBridgeAs(t *testing.T, e *shimEnv, python, secret, name, keyID string) *specMCPBridge {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	cmd := exec.CommandContext(ctx, python, "-B", filepath.Join(root, "internal/m8/testdata/a3_spec_mcp_client.py"))
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if k == "HOME" || k == "PYTHONPATH" || strings.HasPrefix(k, "SH_") || strings.HasPrefix(k, "DW_") || strings.HasPrefix(k, "DINGWEI_") || strings.HasPrefix(k, "CODEX_") || strings.HasPrefix(k, "CLAUDE_") || strings.HasPrefix(k, "OPENCODE_") {
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "PYTHONPATH="+filepath.Join(root, "tools/sessionhelper"), "PYTHONDONTWRITEBYTECODE=1")
	stdoutLog, err := os.OpenFile(filepath.Join(home, "helper.stdout"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("private stdout capture unavailable")
	}
	stderrLog, err := os.OpenFile(filepath.Join(home, "helper.stderr"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		stdoutLog.Close()
		t.Fatal("private stderr capture unavailable")
	}
	cmd.Stderr = stderrLog
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	capturedOut := io.TeeReader(out, stdoutLog)
	var b *specMCPBridge
	t.Cleanup(func() {
		in.Close()
		cancel()
		_, _ = io.Copy(io.Discard, capturedOut)
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
		stdoutLog.Close()
		stderrLog.Close()
		if b != nil {
			if err := scanSpecMCPPrivateLogs(b.logPaths, b.private); err != nil {
				t.Error(err)
			}
		}
	})
	b = &specMCPBridge{t: t, input: json.NewEncoder(in), scan: bufio.NewScanner(capturedOut), cmd: cmd, private: []string{secret}, logPaths: []string{filepath.Join(home, "helper.stdout"), filepath.Join(home, "helper.stderr"), filepath.Join(home, "mcp.stdout"), filepath.Join(home, "mcp.stderr")}}
	b.scan.Buffer(make([]byte, 4096), 1<<20)
	if err = b.input.Encode(map[string]string{"state": home, "ws_base": "ws" + strings.TrimPrefix(e.srv.URL, "http"), "secret": secret, "name": name, "key_id": keyID}); err != nil {
		t.Fatal(err)
	}
	b.phase("A3_E2E_READY")
	return b
}

func TestSpecMCPEndToEnd(t *testing.T) {
	python := os.Getenv("DW_SPEC_E2E_PYTHON")
	if python == "" {
		t.Skip("set DW_SPEC_E2E_PYTHON for actual helper/MCP subprocesses")
	}
	hubDir := t.TempDir()
	_ = os.Chmod(hubDir, 0700)
	hubPath := filepath.Join(hubDir, "hub.log")
	hubLog, err := os.OpenFile(hubPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	prior := log.Writer()
	log.SetOutput(hubLog)
	var private []string
	t.Cleanup(func() {
		log.SetOutput(prior)
		_ = hubLog.Sync()
		if err := scanSpecMCPPrivateLogs([]string{hubPath}, private); err != nil {
			t.Error(err)
		}
		hubLog.Close()
	})
	e := newShimEnv(t)
	private = []string{e.zKimiCred, e.lKimiCr, e.cred}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", e.hub.HandleAgentWS)
	var dropResponse atomic.Bool
	var directCreates atomic.Int32
	agentCreate := e.hub.RequireAgentSpecDirect(e.hub.HandleConsoleSpecs)
	mux.HandleFunc("POST /api/agent/console/specs", func(w http.ResponseWriter, r *http.Request) {
		directCreates.Add(1)
		if !dropResponse.Swap(false) {
			agentCreate(w, r)
			return
		}
		// Commit using the production handler, then drop the real HTTP response.
		// Unknown means query before retry, not a fabricated rollback promise.
		recorder := httptest.NewRecorder()
		agentCreate(recorder, r)
		if recorder.Code != 201 {
			t.Error("lost-response fixture did not commit")
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	mux.HandleFunc("PATCH /api/agent/console/specs/{specID}", e.hub.RequireAgentSpecDirect(e.hub.HandleConsoleSpec))
	mux.HandleFunc("GET /api/specs/shared/{specID}", e.hub.HandleConsoleSpec)
	mux.HandleFunc("GET /api/console/specs", e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("read", e.hub.HandleConsoleSpecs)))
	srv := httptest.NewServer(mux)
	e.srv = srv
	t.Cleanup(srv.Close)
	b := startSpecMCPBridge(t, e, python)
	var id string
	t.Run("one_call_create_no_approval", func(t *testing.T) {
		b.t = t
		out := b.call("dw_spec_create", specE2EArgs(false, ""))
		if out["status"] != "ok" {
			t.Fatalf("direct create status=%v", out["status"])
		}
		id, _ = out["spec_id"].(string)
		if id == "" {
			t.Fatal("missing persisted ID")
		}
	})
	if id == "" {
		t.Fatal("create prerequisite failed")
	}
	t.Run("same_agent_update", func(t *testing.T) {
		b.t = t
		out := b.call("dw_spec_update", specE2EArgs(true, id))
		if out["status"] != "ok" || out["revision"] != float64(2) {
			t.Fatal("self update not persisted")
		}
	})
	t.Run("stale_revision_conflict", func(t *testing.T) {
		b.t = t
		out := b.call("dw_spec_update", specE2EArgs(true, id))
		if out["http_status"] != float64(409) {
			t.Fatal("missing revision conflict")
		}
	})
	t.Run("console_list_contains_created_id", func(t *testing.T) {
		status, body := e.do("GET", "/api/console/specs", e.tokZZC, "", "", "")
		if status != 200 || !strings.Contains(body, id) {
			t.Fatal("real Console HTTP list missing spec")
		}
	})
	t.Run("shared_read_latest_revision", func(t *testing.T) {
		status, body := e.do("GET", "/api/specs/shared/"+id, "", "", "", "")
		if status != 200 || !strings.Contains(body, "# Updated") {
			t.Fatal("real shared HTTP missing update")
		}
	})
	t.Run("model_cannot_supply_actor_or_grant", func(t *testing.T) {
		b.t = t
		for _, field := range []string{"created_by", "grant_request_id", "capability"} {
			args := specE2EArgs(false, "")
			args[field] = "synthetic-forged"
			if b.packet("dw_spec_create", args)["error"] == nil {
				t.Fatal("model supplied privileged argument")
			}
		}
	})
	t.Run("committed_response_lost_is_unknown_without_retry", func(t *testing.T) {
		b.t = t
		before := directCreates.Load()
		dropResponse.Store(true)
		out := b.call("dw_spec_create", specE2EArgs(false, ""))
		if out["outcome"] != "unknown" || out["retryable"] != false {
			t.Fatal("lost response fabricated a retry or rollback")
		}
		if directCreates.Load() != before+1 {
			t.Fatal("helper blindly retried committed write")
		}
		items, err := e.db.ListConsoleSpecs(e.ctx, "zzc", false)
		if err != nil || len(items) != 2 {
			t.Fatal("lost response did not retain exactly one committed spec")
		}
	})
	b.t = t
	b.stop()
	peer := startSpecMCPBridgeAs(t, e, python, e.cred, "Opus50TS", "u_zzc")
	t.Run("same_owner_other_agent_denied", func(t *testing.T) {
		peer.t = t
		out := peer.call("dw_spec_update", specE2EArgs(true, id))
		if out["http_status"] != float64(403) {
			t.Fatal("peer mutated spec")
		}
	})
	peer.t = t
	peer.stop()
	other := startSpecMCPBridgeAs(t, e, python, e.lKimiCr, "kimi", "u_lbl")
	t.Run("other_owner_denied", func(t *testing.T) {
		other.t = t
		out := other.call("dw_spec_update", specE2EArgs(true, id))
		if out["http_status"] != float64(403) {
			t.Fatal("cross owner mutated spec")
		}
	})
	other.t = t
	other.stop()
}

func TestSpecMCPPrivateLogScan(t *testing.T) {
	for _, channel := range []string{"helper.stdout", "helper.stderr", "mcp.stdout", "mcp.stderr", "hub.log"} {
		t.Run(channel, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, channel)
			private := "synthetic-private-capability-canary"
			if err := os.WriteFile(path, []byte("clean output"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := scanSpecMCPPrivateLogs([]string{path}, []string{private}); err != nil {
				t.Fatal("clean stream rejected")
			}
			if err := os.WriteFile(path, []byte("logged: "+private), 0600); err != nil {
				t.Fatal(err)
			}
			err := scanSpecMCPPrivateLogs([]string{path}, []string{private})
			if err == nil {
				t.Fatal("raw capability leak in captured stream was missed")
			}
			if strings.Contains(err.Error(), private) {
				t.Fatal("detector exposed matched private bytes")
			}
		})
	}
}
