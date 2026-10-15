package m8

// Local integration: Console cookie+Origin -> real authenticated Hub WS -> real
// python -m sessionhelper under the stable launcher -> HTTPS artifact/manifest
// verification -> private slot -> explicit test-supervised restart -> health.
// This is not Docker unless-stopped validation or a production/real-Agent test.
import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	dwversion "github.com/zhangzongchu2019/dingwei/internal/version"
)

type updateE2EPlan struct {
	Mount      string `json:"mount"`
	Artifact   string `json:"artifact"`
	FactoryPin struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Tree    string `json:"tree"`
	} `json:"factory_pin"`
	Pin struct {
		Commit  string `json:"commit"`
		Tree    string `json:"tree"`
		SHA     string `json:"sha256"`
		Lock    string `json:"lock_sha256"`
		Version string `json:"version"`
	} `json:"pin"`
}

func TestHelperSelfUpdateEndToEnd(t *testing.T) {
	t.Run("install_restart_confirm", func(t *testing.T) { runHelperSelfUpdateE2E(t, false) })
	t.Run("sha_mismatch_keeps_factory", func(t *testing.T) { runHelperSelfUpdateE2E(t, true) })
}

// Run this test overlay against unchanged r14 product sources, with its actual
// CLI binary and two immutable package commits. Not a same-version Hub rebuild.
func TestHelper0232MixedVersionEndToEnd(t *testing.T) {
	if os.Getenv("DW_HELPER_E2E_RELEASE_COMMIT") == "" {
		t.Skip("explicit immutable release pins required")
	}
	if dwversion.Version != "0.2.3.1" {
		t.Fatalf("mixed gate requires unchanged r14 Hub0.2.3.1, got %s", dwversion.Version)
	}
	if os.Getenv("DW_HELPER_E2E_FACTORY_COMMIT") != "2a8947291acde2ac6e6504a56f1902a808a49f57" {
		t.Fatal("factory must be exact r14")
	}
	if os.Getenv("DW_HELPER_E2E_HUB_BINARY") == "" {
		t.Fatal("actual r14 CLI binary required")
	}
	runHelperSelfUpdateE2E(t, false)
}

func runHelperSelfUpdateE2E(t *testing.T, corrupt bool) {
	python := os.Getenv("DW_HELPER_E2E_PYTHON")
	if python == "" {
		python = os.Getenv("DW_SPEC_E2E_PYTHON")
	}
	if python == "" {
		t.Skip("set DW_HELPER_E2E_PYTHON to locked helper runtime Python")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("", "hu-e2-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	revision, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	setupPath := os.Getenv("DW_HELPER_E2E_SETUP")
	if setupPath == "" {
		setupPath = filepath.Join(root, "internal/m8/testdata/helper_update_e2e_setup.py")
	}
	cmd := exec.Command(python, "-B", setupPath, root, strings.TrimSpace(string(revision)), base)
	setup, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("local Git package setup: %v: %s", err, setup)
	}
	var plan updateE2EPlan
	if err = json.Unmarshal(setup, &plan); err != nil {
		t.Fatalf("package setup JSON: %v", err)
	}
	v := newViewEnv(t)
	h := v.hub
	mixed := os.Getenv("DW_HELPER_E2E_RELEASE_COMMIT") != ""
	var cliDB string
	if mixed {
		if plan.FactoryPin.Version != "0.2.3.1" || plan.Pin.Version != "0.2.3.2" {
			t.Fatal("wrong actual package version pair")
		}
		cliDB = filepath.Join(base, "hub-catalog.db")
		db, e := store.OpenSQLite(cliDB)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Migrate(v.ctx); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = db.Close() })
		h = New(db, ConsoleAuthDisabled())
		t.Cleanup(h.Close)
		h.Identity = v.hub.Identity
		h.Onboarding = v.hub.Onboarding
		h.newSessionNotice = false
		t.Logf("MIXED Hub commit=%s tree=%s actualVersion=%s factoryCommit=%s factoryTree=%s factoryVersion=%s candidateCommit=%s candidateTree=%s candidateVersion=%s packageSHA=%s lockSHA=%s", strings.TrimSpace(string(revision)), "860e3728da369cffc5687cb8da6d29ffc910fa59", dwversion.Version, plan.FactoryPin.Commit, plan.FactoryPin.Tree, plan.FactoryPin.Version, plan.Pin.Commit, plan.Pin.Tree, plan.Pin.Version, plan.Pin.SHA, plan.Pin.Lock)
	}
	payload, err := os.ReadFile(plan.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	if corrupt {
		payload = append(payload, 'x') // expected catalog SHA remains the original package.
	}
	var downloads atomic.Int32
	artifacts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("Agent credentials sent to file origin")
			http.Error(w, "denied", 403)
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(artifacts.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", h.HandleAgentWS)
	mux.HandleFunc("GET /api/console/helper-updates", h.RequireConsolePrincipal(h.HandleConsoleHelperUpdates))
	mux.HandleFunc("POST /api/console/helper-updates", h.RequireConsolePrincipal(h.HandleConsoleHelperUpdates))
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	h.PublicOrigin = server.URL
	ca := filepath.Join(base, "fixture-ca.pem")
	certs := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.TLS.Certificates[0].Certificate[0]}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: artifacts.TLS.Certificates[0].Certificate[0]})...)
	if err = os.WriteFile(ca, certs, 0600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(base, "private-state")
	if err = os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	ag, err := v.svc.Store.GetAgent(v.ctx, v.agID)
	if err != nil {
		t.Fatal(err)
	}
	credential, _ := json.Marshal(map[string]string{"agent_name": ag.Name, "secret": v.cred})
	if err = os.WriteFile(filepath.Join(state, "credential.json"), credential, 0600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	// Deterministic local CLI exercises the production CLIAdapter idle/readiness
	// path; EchoAdapter does not implement the update idle contract.
	fakeCLI := filepath.Join(base, "fake_cli.py")
	if err = os.WriteFile(fakeCLI, []byte("import sys\nprint('SYNTHETIC_READY', flush=True)\nfor line in sys.stdin:\n print('SYNTHETIC_REPLY', flush=True)\n print('SYNTHETIC_READY', flush=True)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(base, "helper.log")
	env := []string{"PATH=" + filepath.Dir(python) + ":/usr/bin:/bin", "HOME=" + home, "TMPDIR=" + base, "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH=" + plan.Mount, "SSL_CERT_FILE=" + ca,
		"SH_ENROLL=1", "SH_STATE_DIR=" + state, "SH_WS_BASE=" + "wss" + strings.TrimPrefix(server.URL, "https"), "SH_MODE=cli", "SH_CLI=claude", "SH_CLI_LAUNCH=pty", "SH_CLI_CMD=" + python + " -u " + fakeCLI, "SH_CLI_CWD=" + base, "SH_CLI_READY_PATTERNS=SYNTHETIC_READY", "SH_CLI_SETTLE_SECONDS=0", "SH_CLI_READY_TIMEOUT=5", "SH_PROXY=off", "SH_MCP_IPC_SOCK=" + filepath.Join(base, "m.sock")}
	type process struct {
		cmd  *exec.Cmd
		done chan struct{}
		log  *os.File
		err  error
	}
	start := func() *process {
		t.Helper()
		log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			t.Fatal(err)
		}
		c := exec.Command(python, "-B", "-m", "sessionhelper")
		c.Env = env
		c.Dir = base
		c.Stdout = log
		c.Stderr = log
		p := &process{cmd: c, done: make(chan struct{}), log: log}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { p.err = c.Wait(); close(p.done); _ = log.Close() }()
		t.Cleanup(func() {
			_ = c.Process.Signal(syscall.SIGTERM)
			select {
			case <-p.done:
			case <-time.After(8 * time.Second):
				_ = c.Process.Kill()
				<-p.done
			}
		})
		return p
	}
	diagnostic := func() string { b, _ := os.ReadFile(logPath); return strings.ReplaceAll(string(b), v.cred, "REDACTED") }
	waitVersion := func(version string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			h.mu.Lock()
			c := h.sessionClients[AgentKeyID("zzc")][ag.NameLC()]
			good := c != nil && c.version == version
			h.mu.Unlock()
			if good && h.sessionSupportsHelperUpdate(c) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("helper did not authenticate/capability version %s: %s", version, diagnostic())
	}
	first := start()
	waitVersion(plan.FactoryPin.Version)
	waitFor(t, 8*time.Second, "production CLIAdapter ready output reached authenticated Hub", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		terminal := h.terminals[terminalKey(AgentKeyID("zzc"), ag.NameLC())]
		return terminal != nil && strings.Contains(terminal.buffer, "SYNTHETIC_READY")
	})
	if mixed {
		binary := os.Getenv("DW_HELPER_E2E_HUB_BINARY")
		versionOutput, e := exec.Command(binary, "version").CombinedOutput()
		if e != nil || !bytes.Contains(versionOutput, []byte("0.2.3.1")) {
			t.Fatalf("old CLI version: %v %s", e, versionOutput)
		}
		cli := exec.Command(binary, "helper-update-register", "--operator", "synthetic-local-ops", "--sha256", plan.Pin.SHA, "--lock-sha256", plan.Pin.Lock, "--version", plan.Pin.Version, "--platform", "linux", "--url", artifacts.URL+"/candidate.tgz", "--file-service-origin", artifacts.URL)
		cli.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "TMPDIR=" + base, "WP_DB_PATH=" + cliDB, "WP_DATA_DIR=" + base, "WP_SECRET_KEY=LOCAL_SYNTHETIC_NOT_PRODUCTION"}
		out, e := cli.CombinedOutput()
		if e != nil || !bytes.Contains(out, []byte("HELPER_UPDATE_CATALOG_REGISTERED")) {
			t.Fatalf("real r14 CLI registration: %v %s", e, out)
		}
		t.Logf("actual unchanged r14 CLI registered packageSHA=%s", plan.Pin.SHA)
	} else if err = h.RegisterHelperUpdateCatalog(v.ctx, "ops", HelperUpdateCatalog{SHA256: plan.Pin.SHA, LockSHA256: plan.Pin.Lock, Version: plan.Pin.Version, URL: artifacts.URL + "/candidate.tgz", FileServiceOrigin: artifacts.URL, Platform: "linux"}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"release_sha256": plan.Pin.SHA, "agent_ids": []string{v.agID}})
	request, _ := http.NewRequest("POST", server.URL+"/api/console/helper-updates", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.URL)
	request.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: v.tokZZC})
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || !bytes.Contains(wire, []byte(`"ok":true`)) {
		t.Fatalf("Console issue failed: status%d %s", response.StatusCode, wire)
	}
	if corrupt {
		waitFor(t, 8*time.Second, "real helper rejected SHA and reported failed job", func() bool {
			job, _, _ := h.helperUpdateJob(v.ctx, v.agID)
			return job != nil && job.Stage == "failed"
		})
		if downloads.Load() != 1 {
			t.Fatalf("SHA negative downloads=%d, expected one actual fetch", downloads.Load())
		}
		select {
		case <-first.done:
			t.Fatalf("bad SHA stopped factory process: %v %s", first.err, diagnostic())
		default:
		}
		entries, err := os.ReadDir(filepath.Join(state, "update/slots"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatal("bad SHA installed a candidate slot")
		}
		waitVersion(plan.FactoryPin.Version)
		t.Log("PASS real HTTPS SHA mismatch -> authenticated failed receipt -> factory remains running, no slot/restart; NOT Docker")
		return
	}
	select {
	case <-first.done:
		if first.err != nil {
			t.Fatalf("factory exit: %v %s", first.err, diagnostic())
		}
	case <-time.After(20 * time.Second):
		job, _, _ := h.helperUpdateJob(v.ctx, v.agID)
		stage := "missing"
		if job != nil {
			stage = job.Stage
		}
		audits, _ := h.Repo.ListRecentAudit(v.ctx, "helper_update_", 8)
		for _, audit := range audits {
			t.Logf("safe helper audit action=%s detail=%s", audit.Action, audit.Target)
		}
		t.Fatalf("update did not request external restart; stage=%s downloads=%d: %s", stage, downloads.Load(), diagnostic())
	}
	waitFor(t, 3*time.Second, "old helper disconnected", func() bool { return !h.AgentOnline(v.agID) })
	t.Log("actual factory process exited for external test-supervised restart; old authenticated connection closed")
	second := start()
	_ = second
	waitVersion(plan.Pin.Version)
	waitFor(t, 10*time.Second, "Hub confirmation from real helper health", func() bool {
		job, _, _ := h.helperUpdateJob(v.ctx, v.agID)
		return job != nil && job.Stage == "confirmed"
	})
	if mixed {
		job, _, _ := h.helperUpdateJob(v.ctx, v.agID)
		t.Logf("confirmed canonical job=%s actualVersion=%s activationConnSeq=%d", job.JobID, job.Version, job.ActivationConnSeq)
	}
	journal, err := os.ReadFile(filepath.Join(state, "update/journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Pending any            `json:"pending"`
		Current map[string]any `json:"current"`
	}
	if err = json.Unmarshal(journal, &stored); err != nil {
		t.Fatal(err)
	}
	// Hub durability can precede helper receipt of the confirmation.
	waitFor(t, 3*time.Second, "helper accepted actual Hub lock+nonce confirmation", func() bool {
		b, _ := os.ReadFile(filepath.Join(state, "update/journal.json"))
		_ = json.Unmarshal(b, &stored)
		return stored.Pending == nil && stored.Current["sha256"] == plan.Pin.SHA
	})
	if _, err = os.Stat(filepath.Join(base, "m.sock")); err != nil {
		t.Fatal("actual MCP IPC did not bind")
	}
	t.Log("PASS real Console HTTP -> authenticated helper WS -> distinct HTTPS file origin -> Git package/lock/manifest -> installed slot -> test-supervised process restart -> target version -> actual helper health/Hub confirm -> durable current; NOT Docker")
}
