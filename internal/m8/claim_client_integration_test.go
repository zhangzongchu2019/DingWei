package m8

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// All HTTP/WS endpoints are loopback and all identities are synthetic.
// This enters the trusted-ingress handler, not the real Feishu platform.
func TestDeviceClaimAcrossActualClientAndHub(t *testing.T) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("run scripts/test-desktop-hub-roundtrip.sh for cross-runtime enrollment")
	}
	for _, mode := range []string{"new", "reuse", "lost_ack", "takeover", "cancel_then_new"} {
		t.Run(mode, func(t *testing.T) { claimClientIntegration(t, python, mode, "memory") })
	}
}

// This variant must own its test database. Never reuse a caller's DSN or
// downgrade a failed startup to a skip. pgtest checks cleanup and new residue.
func TestDeviceClaimAcrossActualClientAndPostgres(t *testing.T) {
	python := os.Getenv("DW_DESKTOP_INTEGRATION_PYTHON")
	if python == "" {
		t.Skip("run scripts/test-desktop-hub-roundtrip.sh for cross-runtime PostgreSQL enrollment")
	}
	t.Setenv(pgtest.DSNEnv, "")
	t.Setenv(pgtest.EmbeddedOffEnv, "1")
	t.Setenv(pgtest.SkipOKEnv, "0")
	t.Setenv(pgtest.StrictEnv, "1")
	for _, mode := range []string{"new", "reuse", "lost_ack", "takeover", "cancel_then_new"} {
		t.Run(mode, func(t *testing.T) { claimClientIntegration(t, python, mode, "postgres") })
	}
}

func claimClientIntegration(t *testing.T, python, mode, backend string) {
	hub, _, base := newTestHub(t)
	ctx, cancel := context.WithTimeout(base, 25*time.Second)
	defer cancel()
	var store identity.Store = identity.NewMemStore()
	if backend == "postgres" {
		pool := pgtest.Pool(t)
		access, err := repo.NewAccessCtx("test", "claim-client-integration")
		if err != nil {
			t.Fatal(err)
		}
		row, err := pool.QueryRow(ctx, access, "SELECT current_setting('server_version_num')")
		var versionNumber int
		if err != nil || row.Scan(&versionNumber) != nil {
			t.Fatal("cannot identify isolated PostgreSQL version")
		}
		t.Logf("isolated PostgreSQL server_version_num=%d", versionNumber)
		report, err := migrate.Migrate(ctx, pool, migrate.HubDomains())
		if err != nil || strings.Join(report.Applied, ",") != "0001_core_baseline.sql,0100_identity_baseline.sql,0101_group_communication.sql,0102_device_claim.sql,0300_console_transport_slot.sql" {
			t.Fatal("production Hub migration ledger did not initialize expected identity/claim schema")
		}
		repeated, err := migrate.Migrate(ctx, pool, migrate.HubDomains())
		if err != nil || len(repeated.Applied) != 0 {
			t.Fatal("migration rerun was not idempotent")
		}
		if err := identity.NewPGStore(pool).CheckSchema(ctx); err != nil {
			t.Fatal("migrated identity schema not usable")
		}
		store = identity.NewPGStore(pool)
	}
	claimStore := store.(identity.ClaimStore)
	svc := identity.NewService(store, nil, nil)
	ob := identity.NewOnboarding(svc)
	ob.CodeKey = "local-synthetic-claim-key"
	hub.Identity, hub.Onboarding = svc, ob
	sender := identity.Identity{Provider: "feishu", Issuer: "fixture-app", Subject: "fixture-person"}
	requested, actual, short := "newperson", "newperson", "np"
	if mode == "reuse" || mode == "takeover" {
		actual, short = "existing", "ex"
		if err := store.CreateUser(ctx, identity.User{UserID: actual, ShortName: short, Type: identity.UserMember, State: identity.UserPendingBind}); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if err := store.CreateRegistrationCode(ctx, actual, "synthetic-reg", now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		bound := sender
		if mode == "takeover" {
			bound.Subject = "other-person"
			requested = actual
		}
		if _, err := store.RedeemRegistrationCode(ctx, "synthetic-reg", bound, now); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	var applies, polls, acks atomic.Int32
	var savedBeforeACK atomic.Bool
	mux := http.NewServeMux()
	(&httpapi.API{Svc: svc, Claims: ob, ClaimBots: func(context.Context) ([]httpapi.ClaimBot, error) {
		return []httpapi.ClaimBot{{Name: "Local fixture", AppID: "cli_fixtureapp001"}}, nil
	}}).Register(mux)
	mux.HandleFunc("GET /ws/agent", hub.HandleAgentWS)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/enroll/claims" {
			applies.Add(1)
			raw, err := io.ReadAll(r.Body)
			var fields map[string]any
			if err != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 7 || fields["code"] != nil || fields["poll_token"] != nil {
				http.Error(w, "unsafe test request", 400)
				return
			}
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
		} else if strings.HasSuffix(r.URL.Path, "/ack") {
			acks.Add(1)
			raw, err := os.ReadFile(filepath.Join(home, "credential.json"))
			var saved struct {
				Secret  string `json:"secret"`
				ClaimID string `json:"claim_id"`
			}
			info, statErr := os.Stat(filepath.Join(home, "credential.json"))
			if err != nil || statErr != nil || info.Mode().Perm() != 0600 || json.Unmarshal(raw, &saved) != nil {
				http.Error(w, "credential not saved", 500)
				return
			}
			c, err := claimStore.GetDeviceClaim(r.Context(), saved.ClaimID)
			if err != nil || identity.HashSecret(saved.Secret) != c.CredentialHash {
				http.Error(w, "wrong saved credential", 500)
				return
			}
			savedBeforeACK.Store(true)
		} else if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/enroll/claims/") {
			polls.Add(1)
		}
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, python, "-W", "error::RuntimeWarning", filepath.Join(root, "tools/sessionhelper/tests/claim_hub_integration_client.py"))
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "HOME" || name == "PYTHONPATH" || strings.HasPrefix(name, "SH_") || strings.HasPrefix(name, "DW_") || strings.HasPrefix(name, "CODEX_") || strings.HasPrefix(name, "CLAUDE_") || strings.HasPrefix(name, "OPENCODE_") || strings.HasPrefix(name, "DINGWEI_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "PYTHONPATH="+filepath.Join(root, "tools/sessionhelper"))
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = input.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Signal(os.Interrupt)
			force := time.AfterFunc(5*time.Second, func() { _ = cmd.Process.Kill() })
			_ = cmd.Wait()
			force.Stop()
		}
	}()
	encoder := json.NewEncoder(input)
	config := map[string]any{"base": srv.URL, "state": home, "mode": mode, "requested": requested, "actual": actual, "actual_short": short}
	send := func(v any) {
		t.Helper()
		if err := encoder.Encode(v); err != nil {
			t.Fatal("child control write failed")
		}
	}
	scanner := bufio.NewScanner(output)
	phase := func(want string) {
		t.Helper()
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "CLAIM_ERROR ") {
				t.Fatal("claim integration child failed; secrets not emitted")
			}
			if strings.HasPrefix(line, want+" ") {
				return
			}
		}
		t.Fatal("missing claim child phase")
	}
	send(config)
	if mode == "cancel_then_new" {
		phase("CLAIM_CANCEL_READY")
		var old struct {
			ID   string `json:"claim_id"`
			Code string `json:"code"`
		}
		raw, err := os.ReadFile(filepath.Join(home, "claim-pending.json"))
		if err != nil || json.Unmarshal(raw, &old) != nil {
			t.Fatal("private cancel state unavailable")
		}
		send(map[string]string{"operation": "cancel"})
		phase("CLAIM_CANCELLED")
		cancelled, err := claimStore.GetDeviceClaim(ctx, old.ID)
		if err != nil || cancelled.State != "cancelled" || cancelled.AgentID != "" {
			t.Fatal("client archive lacked real server cancellation")
		}
		stale := trustedMsgFrom(sender.Subject, sender.Issuer, model.IngressFeishuWSAuthenticated)
		stale.ChatType = model.ChatPersonal
		if _, handled, err := hub.dispatchNewPlatformCommand(ctx, stale, "#pair "+old.Code); err != nil || !handled {
			t.Fatal("cancelled code replay handling failed")
		}
		users, err := store.CountUsers(ctx)
		if err != nil || users != 0 {
			t.Fatal("cancelled claim created an account")
		}
		send(map[string]string{"operation": "new"})
	}
	phase("CLAIM_PAIR_READY")
	var pending struct {
		ID   string `json:"claim_id"`
		Code string `json:"code"`
	}
	raw, err := os.ReadFile(filepath.Join(home, "claim-pending.json"))
	if err != nil || json.Unmarshal(raw, &pending) != nil || !identity.ValidClaimCode(pending.Code) {
		t.Fatal("invalid private pending state")
	}
	info, err := os.Stat(filepath.Join(home, "claim-pending.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("pending state not private")
	}
	command := "#pair " + pending.Code
	message := trustedMsgFrom(sender.Subject, sender.Issuer, model.IngressFeishuWSAuthenticated)
	message.ChatType = model.ChatPersonal
	for _, bad := range []model.Message{func() model.Message { v := message; v.IngressProvenance = ""; return v }(), func() model.Message { v := message; v.ChatType = model.ChatGroup; return v }()} {
		if _, handled, err := hub.dispatchNewPlatformCommand(ctx, bad, command); err != nil || !handled {
			t.Fatal("untrusted claim handling failed")
		}
		claim, err := claimStore.GetDeviceClaim(ctx, pending.ID)
		if err != nil || claim.State != "awaiting_feishu" {
			t.Fatal("untrusted ingress consumed code")
		}
	}
	if _, handled, err := hub.dispatchNewPlatformCommand(ctx, message, command); err != nil || !handled {
		t.Fatal("trusted claim handling failed")
	}
	send(map[string]string{"operation": "paired"})
	if mode == "takeover" {
		phase("CLAIM_DENIED")
		if acks.Load() != 0 {
			t.Fatal("denied claim acknowledged")
		}
		ags, err := store.ListAgents(ctx, actual)
		if err != nil || len(ags) != 0 {
			t.Fatal("takeover created agent")
		}
	} else {
		if mode == "lost_ack" {
			phase("CLAIM_RESTART_READY")
			c, err := claimStore.GetDeviceClaim(ctx, pending.ID)
			if err != nil || c.State != "completed" {
				t.Fatal("lost response did not commit ACK")
			}
			saved, err := os.ReadFile(filepath.Join(home, "credential.json"))
			if err != nil {
				t.Fatal("saved credential missing")
			}
			savedHash := sha256.Sum256(saved)
			config["resume"], config["saved_sha"] = true, hex.EncodeToString(savedHash[:])
			send(map[string]string{"operation": "restart"})
			phase("CLAIM_RESUME_READY")
			send(config)
		}
		phase("CLAIM_CONNECTED")
		c, err := claimStore.GetDeviceClaim(ctx, pending.ID)
		if err != nil || c.State != "completed" || c.UserID != actual {
			t.Fatal("claim not completed for expected owner")
		}
		user, err := store.ResolveIdentity(ctx, sender)
		if err != nil || user.UserID != actual || user.Type != identity.UserMember {
			t.Fatal("wrong account binding or privilege")
		}
		agents, err := store.ListAgents(ctx, actual)
		if err != nil || len(agents) != 1 || !hub.AgentOnline(agents[0].ID) {
			t.Fatal("claimed agent not actually online")
		}
		users, err := store.CountUsers(ctx)
		if err != nil || users != 1 {
			t.Fatal("duplicate user created")
		}
		expectedApplies := int32(1)
		if mode == "cancel_then_new" {
			expectedApplies = 2
		}
		if !savedBeforeACK.Load() || applies.Load() != expectedApplies || polls.Load() != 1 {
			t.Fatal("recovery repeated application/poll or ACK preceded save")
		}
		expectedACK := int32(1)
		if mode == "lost_ack" {
			expectedACK = 2
		}
		if acks.Load() != expectedACK {
			t.Fatal("unexpected ACK count")
		}
		send(map[string]string{"operation": "stop"})
		phase("CLAIM_CLEAN")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal("claim child unsuccessful")
	}
	t.Log("actual claim HTTP/client + trusted synthetic ingress + private save/ACK + dedicated helper WS: PASS; backend=" + backend + ", no real Feishu")
}
