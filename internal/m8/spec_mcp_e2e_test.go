package m8

// Cross-runtime integration: actual MCP subprocess -> real private Unix IPC ->
// helper WS grant exchange -> human cookie/Origin approval -> Agent HTTP ->
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
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

type specE2EHTTPGate struct{ entered, release chan struct{} }

type specMCPBridge struct {
	t       *testing.T
	input   *json.Encoder
	scan    *bufio.Scanner
	cmd     *exec.Cmd
	private []string
	stderr  *bytes.Buffer
	other   []string // private capture of non-protocol stdout; never printed
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
		b.other = append(b.other, line)
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
func (b *specMCPBridge) stop() {
	b.stopWithDiagnosticExpectation(false)
}
func (b *specMCPBridge) stopWithDiagnosticExpectation(wantPrivate bool) {
	b.t.Helper()
	if err := b.input.Encode(map[string]bool{"stop": true}); err != nil {
		b.t.Fatal(err)
	}
	b.phase("A3_E2E_CLEAN")
	if err := b.cmd.Wait(); err != nil {
		b.t.Fatalf("local child exit: %v", err)
	}
	if b.hasPrivateDiagnostics() != wantPrivate {
		if wantPrivate {
			b.t.Fatal("diagnostic-leak mutation was not detected")
		}
		b.assertNoPrivateDiagnostics()
	}
}

func (b *specMCPBridge) hasPrivateDiagnostics() bool {
	diagnostics := append([]string{}, b.other...)
	if b.stderr != nil {
		diagnostics = append(diagnostics, b.stderr.String())
	}
	for _, private := range b.private {
		if private == "" {
			continue
		}
		for _, diagnostic := range diagnostics {
			if strings.Contains(diagnostic, private) {
				return true
			}
		}
	}
	return false
}

func (b *specMCPBridge) assertNoPrivateDiagnostics() {
	b.t.Helper()
	if b.hasPrivateDiagnostics() {
		// Never include captured diagnostics: a failing assertion must not turn a
		// test-private credential/capability into a CI log artifact.
		b.t.Fatal("private value found in captured helper/MCP diagnostics")
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
	return startSpecMCPBridgeAsDiagnostic(t, e, python, secret, name, keyID, false)
}
func startSpecMCPBridgeAsDiagnostic(t *testing.T, e *shimEnv, python, secret, name, keyID string, injectDiagnosticLeak bool) *specMCPBridge {
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
	if injectDiagnosticLeak {
		cmd.Env = append(cmd.Env, "A3_E2E_TEST_DIAGNOSTIC_LEAK=1")
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		in.Close()
		cancel()
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	})
	b := &specMCPBridge{t: t, input: json.NewEncoder(in), scan: bufio.NewScanner(out), cmd: cmd, private: []string{secret}, stderr: stderr}
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
		t.Skip("set DW_SPEC_E2E_PYTHON to an interpreter with helper runtime dependencies")
	}
	e := newShimEnv(t)
	h := e.hub
	if err := h.ReconcileNewPlatformConsole(e.ctx); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", h.HandleAgentWS)
	mux.HandleFunc("GET /api/console/specs", h.RequireConsolePrincipal(h.RequireConsolePlatformAction("read", h.HandleConsoleSpecs)))
	mux.HandleFunc("GET /console/agents/{sessionName}/spec-grant-requests", h.RequireConsoleAgent("interact", h.HandleConsoleSpecGrantPage))
	mux.HandleFunc("GET /api/console/agents/{sessionName}/spec-grant-requests", h.RequireConsoleAgent("interact", h.HandleConsoleSpecGrantRequests))
	mux.HandleFunc("POST /api/console/agents/{sessionName}/spec-grant-requests/{requestID}/{decision}", h.RequireConsoleAgent("interact", h.HandleConsoleSpecGrantDecision))
	var gateMu sync.Mutex
	var nextGate *specE2EHTTPGate
	agentCreate := h.RequireConsoleSpecAgentCapability(func(*http.Request) string { return ConsoleAgentCapabilitySpecsNew }, h.HandleConsoleSpecs)
	mux.HandleFunc("POST /api/agent/console/specs", func(w http.ResponseWriter, r *http.Request) {
		gateMu.Lock()
		g := nextGate
		nextGate = nil
		gateMu.Unlock()
		if g != nil {
			close(g.entered)
			select {
			case <-g.release:
			case <-r.Context().Done():
				return
			}
		}
		agentCreate(w, r)
	})
	mux.HandleFunc("PATCH /api/agent/console/specs/{specID}", h.RequireConsoleSpecAgentCapability(func(r *http.Request) string { return ConsoleAgentSpecCapabilityPage(r.PathValue("specID")) }, h.HandleConsoleSpec))
	mux.HandleFunc("GET /api/specs/shared/{specID}", h.HandleConsoleSpec)
	srv := httptest.NewServer(mux)
	e.srv = srv
	t.Cleanup(srv.Close)
	b := startSpecMCPBridge(t, e, python)
	grant := func(update bool, id string) string {
		t.Helper()
		r := b.call(map[bool]string{false: "dw_spec_create", true: "dw_spec_update"}[update], specE2EArgs(update, id))
		if r["status"] != "approval_required" {
			b.t.Fatalf("grant request status=%v", r["status"])
		}
		g, _ := r["grant_request_id"].(string)
		if g == "" {
			b.t.Fatal("missing grant request id")
		}
		return g
	}
	decide := func(g, decision, tok string) int {
		t.Helper()
		st, _ := e.do("POST", "/api/console/agents/Kimi.zzc/spec-grant-requests/"+g+"/"+decision, tok, "", "", "")
		if st == 204 && decision == "approve" {
			h.specGrantMu.Lock()
			raw := h.specGrantRequests[g].capability
			h.specGrantMu.Unlock()
			if raw == "" {
				b.t.Fatal("approval returned without a private capability")
			}
			b.private = append(b.private, raw)
		}
		return st
	}
	count := func() int {
		t.Helper()
		v, err := e.db.ListConsoleSpecs(e.ctx, "zzc", false)
		if err != nil {
			t.Fatal(err)
		}
		return len(v)
	}
	invoke := func(update bool, id, g string) map[string]any {
		a := specE2EArgs(update, id)
		a["grant_request_id"] = g
		return b.call(map[bool]string{false: "dw_spec_create", true: "dw_spec_update"}[update], a)
	}
	t.Run("cold_owner_approval_without_prior_console_projection", func(t *testing.T) {
		b.t = t
		g := grant(false, "")
		if st, page := e.do("GET", "/console/agents/Kimi.zzc/spec-grant-requests", e.tokZZC, "", "", ""); st != 200 || !strings.Contains(page, "Agent spec approvals") {
			t.Fatalf("actual linked approval page=%d", st)
		}
		if st := decide(g, "approve", e.tokZZC); st != 204 {
			t.Errorf("fresh authenticated owner approve=%d, want 204; no Console identity/session projection primed", st)
		}
	})
	// A normal authenticated Console read is a separate, explicit warm-path
	// precondition. Do not seed raw capability rows or hide the cold-path result.
	if st, _ := e.do("GET", "/api/console/specs", e.tokZZC, "", "", ""); st != 200 {
		t.Fatalf("normal Console read=%d", st)
	}
	startCount := count()
	var specID string
	t.Run("pending_and_human_auth_boundaries", func(t *testing.T) {
		b.t = t
		g := grant(false, "")
		if r := invoke(false, "", g); r["status"] != "pending" {
			t.Errorf("unapproved status=%v", r["status"])
		}
		if count() != startCount {
			t.Error("pending grant wrote spec")
		}
		for _, test := range []struct {
			tok  string
			want int
		}{{"", 401}, {e.tokLBL, 404}} {
			if st := decide(g, "approve", test.tok); st != test.want {
				t.Errorf("non-owner/no-cookie approval=%d want=%d", st, test.want)
			}
		}
		req, _ := http.NewRequest("POST", srv.URL+"/api/console/agents/Kimi.zzc/spec-grant-requests/"+g+"/approve", nil)
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
		req.Header.Set("Origin", "https://foreign.invalid")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Error("foreign Origin approved grant")
		}
		if r := invoke(false, "", g); r["status"] != "pending" {
			t.Errorf("rejected human request changed grant: %v", r["status"])
		}
		st, list := e.do("GET", "/api/console/agents/Kimi.zzc/spec-grant-requests", e.tokZZC, "", "", "")
		if st != 200 || !strings.Contains(list, g) || strings.Contains(list, "\"capability\"") {
			t.Errorf("human listing status=%d or private output", st)
		}
		if st := decide(g, "approve", e.tokZZC); st != 204 {
			t.Fatalf("owner approve status=%d", st)
		}
		r := invoke(false, "", g)
		if r["status"] != "ok" || r["revision"] != float64(1) {
			t.Fatalf("create status=%v revision=%v http=%v", r["status"], r["revision"], r["http_status"])
		}
		specID, _ = r["spec_id"].(string)
		if specID == "" || r["shared_url"] != "/api/specs/shared/"+specID {
			t.Error("missing id/shared URL")
		}
		st, list = e.do("GET", "/api/console/specs", e.tokZZC, "", "", "")
		var visible struct {
			Specs []struct {
				ID string `json:"id"`
			} `json:"specs"`
		}
		if err := json.Unmarshal([]byte(list), &visible); st != http.StatusOK || err != nil {
			t.Fatalf("Console spec list status=%d err=%v", st, err)
		}
		found := false
		for _, item := range visible.Specs {
			found = found || item.ID == specID
		}
		if !found {
			t.Error("created spec absent from real Console list")
		}
		if count() != startCount+1 {
			t.Error("create count mismatch")
		}
		if r := invoke(false, "", g); r["status"] != "consumed" {
			t.Errorf("replay status=%v", r["status"])
		}
		if count() != startCount+1 {
			t.Error("replayed grant duplicated spec")
		}
	})
	t.Run("update_revision_attribution_and_shared_read", func(t *testing.T) {
		b.t = t
		if specID == "" {
			t.Fatal("create did not provide spec for update")
		}
		before, err := e.db.ConsoleSpecRevision(e.ctx, "zzc", specID, 1, false)
		if err != nil || before == nil {
			t.Fatal("missing revision one")
		}
		g := grant(true, specID)
		if st := decide(g, "approve", e.tokZZC); st != 204 {
			t.Fatalf("update approve=%d", st)
		}
		r := invoke(true, specID, g)
		if r["status"] != "ok" || r["revision"] != float64(2) {
			t.Fatalf("update status=%v revision=%v http=%v", r["status"], r["revision"], r["http_status"])
		}
		after, err := e.db.ConsoleSpecRevision(e.ctx, "zzc", specID, 1, false)
		if err != nil || after == nil || *before != *after {
			t.Error("old revision changed")
		}
		rev, err := e.db.ConsoleSpecRevision(e.ctx, "zzc", specID, 2, false)
		if err != nil || rev == nil {
			t.Fatal("missing revision two")
		}
		var acceptance specAcceptanceBundle
		if err := json.Unmarshal([]byte(rev.AcceptanceJSON), &acceptance); err != nil || len(acceptance.Criteria) != 1 {
			t.Fatal("invalid stored acceptance")
		}
		if acceptance.Criteria[0].GeneratedBy != e.agentID("zzc", "kimi") || acceptance.Criteria[0].Source != "ai" {
			t.Error("model-forged acceptance actor/source survived authenticated attestation")
		}
		if rev.CreatedBy != e.agentID("zzc", "kimi") || rev.CreatedByKind != "agent" {
			t.Errorf("revision attribution kind=%q matchesAgent=%v", rev.CreatedByKind, rev.CreatedBy == e.agentID("zzc", "kimi"))
		}
		st, body := e.do("GET", "/api/specs/shared/"+specID, "", "", "", "")
		if st != 200 || !strings.Contains(body, "# Updated") || !strings.Contains(body, `"revision":2`) {
			t.Errorf("shared read status=%d content/revision mismatch", st)
		}
		stale := grant(true, specID)
		if st := decide(stale, "approve", e.tokZZC); st != 204 {
			t.Fatal("stale grant approve")
		}
		if r := invoke(true, specID, stale); r["status"] != "error" || r["http_status"] != float64(409) {
			t.Errorf("stale update status=%v http=%v", r["status"], r["http_status"])
		}
	})
	t.Run("deny_revoke_expire_and_action_scope", func(t *testing.T) {
		b.t = t
		baseline := count()
		for _, decision := range []string{"deny", "revoke"} {
			g := grant(false, "")
			if decision == "revoke" {
				if st := decide(g, "approve", e.tokZZC); st != 204 {
					t.Fatal("approve before revoke")
				}
			}
			if st := decide(g, decision, e.tokZZC); st != 204 {
				t.Fatalf("%s status=%d", decision, st)
			}
			want := map[string]string{"deny": "denied", "revoke": "revoked"}[decision]
			if r := invoke(false, "", g); r["status"] != want {
				t.Errorf("%s poll=%v", decision, r["status"])
			}
		}
		g := grant(false, "")
		h.specGrantMu.Lock()
		h.specGrantRequests[g].ExpiresAt = h.now().Add(-time.Second)
		h.specGrantMu.Unlock()
		if r := invoke(false, "", g); r["status"] != "expired" {
			t.Errorf("expired poll=%v", r["status"])
		}
		if st := decide(g, "approve", e.tokZZC); st != 404 {
			t.Errorf("expired request approved: status=%d", st)
		}
		// Expiry seams change only deadline fields under the real grant mutex;
		// no granted state, raw token or database authorization is fabricated.
		g = grant(false, "")
		if st := decide(g, "approve", e.tokZZC); st != 204 {
			t.Fatal("expiring capability approval")
		}
		h.specGrantMu.Lock()
		h.specGrantRequests[g].CapabilityExpiresAt = h.now().Add(-time.Second)
		h.specGrantMu.Unlock()
		if r := invoke(false, "", g); r["status"] != "expired" {
			t.Errorf("expired capability poll=%v", r["status"])
		}
		if specID != "" {
			g = grant(false, "")
			if st := decide(g, "approve", e.tokZZC); st != 204 {
				t.Fatal("scope approve")
			}
			if r := invoke(true, specID, g); r["status"] != "error" || r["http_status"] != float64(403) {
				t.Errorf("create grant used for update status=%v http=%v", r["status"], r["http_status"])
			}
			g = grant(true, specID)
			if st := decide(g, "approve", e.tokZZC); st != 204 {
				t.Fatal("update-scope approval")
			}
			if r := invoke(false, "", g); r["status"] != "error" || r["http_status"] != float64(403) {
				t.Errorf("update grant used for create status=%v http=%v", r["status"], r["http_status"])
			}
		}
		if count() != baseline {
			t.Error("negative path created spec")
		}
	})
	t.Run("different_agent_cannot_poll_owner_grant", func(t *testing.T) {
		b.t = t
		g := grant(false, "")
		peer := startSpecMCPBridgeAs(t, e, python, e.lKimiCr, "kimi", "u_lbl")
		a := specE2EArgs(false, "")
		a["grant_request_id"] = g
		if r := peer.call("dw_spec_create", a); r["status"] != "not_found" {
			t.Errorf("cross-owner same-name Agent poll=%v", r["status"])
		}
		peer.stop()
		if r := invoke(false, "", g); r["status"] != "pending" {
			t.Errorf("cross-owner poll changed owner's grant=%v", r["status"])
		}
	})
	t.Run("same_owner_other_agent_cannot_poll_grant", func(t *testing.T) {
		b.t = t
		g := grant(false, "")
		// e.cred belongs to zzc's separately enrolled Opus50TS Agent, so this
		// exercises same owner but a distinct Agent/session binding.
		peer := startSpecMCPBridgeAs(t, e, python, e.cred, "Opus50TS", "u_zzc")
		a := specE2EArgs(false, "")
		a["grant_request_id"] = g
		if r := peer.call("dw_spec_create", a); r["status"] != "not_found" {
			t.Errorf("same-owner other-Agent poll=%v", r["status"])
		}
		peer.stop()
		if r := invoke(false, "", g); r["status"] != "pending" {
			t.Errorf("same-owner peer poll changed grant=%v", r["status"])
		}
	})
	t.Run("revoke_after_consumption_before_agent_http", func(t *testing.T) {
		b.t = t
		baseline := count()
		g := grant(false, "")
		if st := decide(g, "approve", e.tokZZC); st != 204 {
			t.Fatalf("approve=%d", st)
		}
		gate := &specE2EHTTPGate{entered: make(chan struct{}), release: make(chan struct{})}
		gateMu.Lock()
		nextGate = gate
		gateMu.Unlock()
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(gate.release) }) }
		defer release()
		// Initiate real MCP call; the test barrier only delays the HTTP arrival,
		// leaving grant exchange, authorization and database code unchanged.
		a := specE2EArgs(false, "")
		a["grant_request_id"] = g
		if err := b.input.Encode(map[string]any{"name": "dw_spec_create", "args": a}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-gate.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("Agent HTTP did not arrive")
		}
		h.specGrantMu.Lock()
		state := h.specGrantRequests[g].State
		h.specGrantMu.Unlock()
		if state != "consumed" {
			t.Errorf("HTTP barrier grant state=%q", state)
		}
		if st := decide(g, "revoke", e.tokZZC); st != 204 {
			t.Errorf("consumed revoke=%d", st)
		}
		release()
		r := b.unwrap(b.readPacket())
		if r["status"] != "error" || r["http_status"] != float64(403) {
			t.Errorf("revoked in-flight write status=%v http=%v", r["status"], r["http_status"])
		}
		if count() != baseline {
			t.Error("consumed revoked grant wrote spec")
		}
	})
	t.Run("update_grant_cannot_target_another_spec", func(t *testing.T) {
		b.t = t
		if specID == "" {
			t.Fatal("missing first spec")
		}
		g := grant(false, "")
		if st := decide(g, "approve", e.tokZZC); st != 204 {
			t.Fatal("second create approval")
		}
		r := invoke(false, "", g)
		if r["status"] != "ok" {
			t.Fatal("second create failed")
		}
		other := r["spec_id"].(string)
		g = grant(true, specID)
		if st := decide(g, "approve", e.tokZZC); st != 204 {
			t.Fatal("target scope approval")
		}
		r = invoke(true, other, g)
		if r["status"] != "error" || r["http_status"] != float64(403) {
			t.Errorf("wrong target status=%v http=%v", r["status"], r["http_status"])
		}
		rev, err := e.db.ConsoleSpecRevision(e.ctx, "zzc", other, 2, false)
		if err != nil || rev != nil {
			t.Error("wrong-target spec gained revision")
		}
	})
	t.Run("shim_rejects_private_or_forged_arguments", func(t *testing.T) {
		b.t = t
		for _, key := range []string{"capability", "created_by", "agent_id"} {
			a := specE2EArgs(false, "")
			a[key] = "forged"
			p := b.packet("dw_spec_create", a)
			if p["error"] == nil {
				t.Errorf("unknown private/identity argument %s accepted", key)
			}
		}
	})
	b.t = t
	b.stop()
	t.Run("diagnostic_leak_mutation_is_detected_privately", func(t *testing.T) {
		leak := startSpecMCPBridgeAsDiagnostic(t, e, python, e.zKimiCred, "Kimi", "u_zzc", true)
		leak.stopWithDiagnosticExpectation(true)
	})
	// Closing the child stdin and context is owned by cleanup even after a red assertion.
}
