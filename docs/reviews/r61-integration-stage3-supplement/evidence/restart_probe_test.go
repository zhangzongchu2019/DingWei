package m8

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// Only this added overlay test exists outside the frozen source. Production
// files are not edited. Each worker is a distinct OS process running the real
// Hub/handlers against the same persisted SQLite and external fixture PG.
func TestStage3HistoryWorker(t *testing.T) {
	if os.Getenv("STAGE3_WORKER") != "1" {
		t.Skip("fixture child only")
	}
	ctx := context.Background()
	pool, err := repo.OpenPG(ctx, os.Getenv("STAGE3_FIXTURE_DSN"))
	if err != nil {
		t.Fatal("fixture PG open failed")
	}
	defer pool.Close()
	db, err := store.OpenSQLite(os.Getenv("STAGE3_APP_DB"))
	if err != nil {
		t.Fatal("fixture app SQLite open failed")
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal("fixture app migrate failed")
	}
	history, err := store.OpenSessionHistory(os.Getenv("STAGE3_HISTORY_DB"))
	if err != nil {
		t.Fatal("fixture history open failed")
	}
	defer history.Close()
	if err = history.SetEnabled(ctx, true, time.Now()); err != nil {
		t.Fatal("fixture history enable failed")
	}
	h := New(db, ConsoleAuthDisabled())
	defer h.Close()
	h.newSessionNotice = false
	svc := identity.NewService(identity.NewPGStore(pool), nil, nil)
	svc.NameAllowed = func(n string) bool { return !isReservedForNaming(n) }
	svc.OnRevoked = h.DisconnectAgent
	svc.OnSuspended = h.DisconnectAgent
	svc.OnCredentialIssued = h.DisconnectAgentRotated
	h.Identity = svc
	h.Onboarding = identity.NewOnboarding(svc)
	h.PublicOrigin = npOrigin
	h.SessionHistory = history
	h.SessionHistoryEnabled = true
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", h.HandleAgentWS)
	mux.HandleFunc("GET /view/{sessionName}/history", h.RequireConsoleAgent("view", h.HandleSessionHistory))
	server := httptest.NewServer(mux)
	defer server.Close()
	ready, _ := json.Marshal(map[string]any{"url": server.URL, "pid": os.Getpid(), "fresh_hub": true})
	if err = os.WriteFile(os.Getenv("STAGE3_READY"), ready, 0600); err != nil {
		t.Fatal("fixture ready write failed")
	}
	scan := bufio.NewScanner(os.Stdin)
	if !scan.Scan() || scan.Text() != "quit" {
		t.Fatal("fixture stop command missing")
	}
}

func TestStage3HistorySurvivesHubProcessRestart(t *testing.T) {
	dir := os.Getenv("STAGE3_OUTPUT")
	if dir == "" {
		t.Fatal("private fixture output required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn := pgtest.DSN(t)
	pool, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal("fixture parent PG open failed")
	}
	defer pool.Close()
	if _, err = migrate.Migrate(ctx, pool, migrate.HubDomains()); err != nil {
		t.Fatal("fixture identity migrate failed")
	}
	svc := identity.NewService(identity.NewPGStore(pool), nil, nil)
	svc.NameAllowed = func(n string) bool { return !isReservedForNaming(n) }
	if err = svc.Store.CreateUser(ctx, identity.User{UserID: "zzc", ShortName: "zzc", Type: identity.UserMember, State: identity.UserActive, CreatedAt: time.Now()}); err != nil {
		t.Fatal("fixture user create failed")
	}
	e := &agentEnv{t: t, ctx: ctx, svc: svc}
	agentCred, agentID := e.enroll("restart-agent-device", "Opus50TS")
	targetCred, targetID := e.enroll("restart-target-device", "Kimi")
	timerCred, timerID := e.enroll("restart-timer-device", "timer")
	for _, id := range []string{agentID, timerID} {
		g, err := svc.CreateGroup(ctx, "zzc", id, tg())
		if err != nil {
			t.Fatal("fixture group create failed")
		}
		if err = svc.JoinGroup(ctx, "zzc", g.Code, targetID); err != nil {
			t.Fatal("fixture group join failed")
		}
	}
	token := fmt.Sprintf("restart-fixture-session-%d", time.Now().UnixNano())
	if err = svc.Store.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret(token), UserIDLC: "zzc", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal("fixture console session create failed")
	}
	app := filepath.Join(dir, "app.db")
	hist := filepath.Join(dir, "history.db")
	type worker struct {
		cmd     *exec.Cmd
		input   io.WriteCloser
		log     *os.File
		url     string
		pid     int
		stopped bool
	}
	start := func(n int) *worker {
		w := &worker{}
		ready := filepath.Join(dir, fmt.Sprintf("ready-%d.json", n))
		w.cmd = exec.Command(os.Args[0], "-test.run=^TestStage3HistoryWorker$", "-test.v")
		w.cmd.Env = append(os.Environ(), "STAGE3_WORKER=1", "STAGE3_FIXTURE_DSN="+dsn, "STAGE3_APP_DB="+app, "STAGE3_HISTORY_DB="+hist, "STAGE3_READY="+ready)
		w.log, err = os.OpenFile(filepath.Join(dir, fmt.Sprintf("worker-%d.log", n)), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			t.Fatal("fixture worker log create failed")
		}
		w.cmd.Stdout = w.log
		w.cmd.Stderr = w.log
		w.input, err = w.cmd.StdinPipe()
		if err != nil {
			t.Fatal("fixture worker pipe failed")
		}
		if err = w.cmd.Start(); err != nil {
			t.Fatal("fixture worker start failed")
		}
		t.Cleanup(func() {
			if !w.stopped {
				_ = w.input.Close()
				_ = w.cmd.Process.Kill()
				_ = w.cmd.Wait()
			}
			_ = w.log.Close()
		})
		waitFor(t, 20*time.Second, "fresh Hub worker ready", func() bool {
			b, er := os.ReadFile(ready)
			if er != nil {
				return false
			}
			var r struct {
				URL string `json:"url"`
				PID int    `json:"pid"`
			}
			if json.Unmarshal(b, &r) != nil {
				return false
			}
			w.url = r.URL
			w.pid = r.PID
			return w.url != "" && w.pid > 0
		})
		return w
	}
	stop := func(w *worker) {
		if _, er := io.WriteString(w.input, "quit\n"); er != nil {
			t.Fatal("fixture graceful stop write failed")
		}
		_ = w.input.Close()
		done := make(chan error, 1)
		go func() { done <- w.cmd.Wait() }()
		select {
		case er := <-done:
			if er != nil {
				t.Fatal("fixture Hub worker did not stop cleanly")
			}
		case <-time.After(20 * time.Second):
			_ = w.cmd.Process.Kill()
			<-done
			t.Fatal("fixture Hub stop timed out")
		}
		w.stopped = true
		_ = w.log.Close()
	}
	get := func(base string) store.SessionHistoryPage {
		req, _ := http.NewRequestWithContext(ctx, "GET", base+"/view/Kimi.zzc/history", nil)
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
		res, er := http.DefaultClient.Do(req)
		if er != nil {
			t.Fatal("fixture history HTTP failed")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("fixture history HTTP status=%d", res.StatusCode)
		}
		var p store.SessionHistoryPage
		if json.NewDecoder(res.Body).Decode(&p) != nil {
			t.Fatal("fixture history decode failed")
		}
		return p
	}
	dial := func(base, cred, query string) *websocket.Conn {
		header := http.Header{}
		header.Set("Authorization", "Bearer "+cred)
		u := "ws" + strings.TrimPrefix(base, "http") + "/ws/agent" + query
		c, _, er := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: header})
		if er != nil {
			t.Fatal("fixture authenticated websocket dial failed")
		}
		return c
	}
	first := start(1)
	target := dial(first.url, targetCred, "?delivery_ack_v1=1")
	if err = writeEnvelope(ctx, target, model.Envelope{ID: "restart-cap", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal("fixture capability write failed")
	}
	probe, err := store.OpenSQLite(app)
	if err != nil {
		t.Fatal("fixture app observer failed")
	}
	defer probe.Close()
	waitFor(t, 5*time.Second, "real Hub ACK capability persisted", func() bool {
		ok, _, er := probe.SessionEndpointDeliveryACK(ctx, AgentKeyID("zzc"), "kimi")
		return er == nil && ok
	})
	for _, input := range []struct{ name, cred string }{{"agent", agentCred}, {"timer", timerCred}} {
		source := dial(first.url, input.cred, "")
		message := "restart-message-" + input.name
		if err = writeEnvelope(ctx, source, model.Envelope{ID: message, To: "kimi#" + AgentKeyID("zzc"), Body: "synthetic restart history " + input.name}); err != nil {
			t.Fatal("fixture source send failed")
		}
		var received model.Envelope
		for {
			received = readEnvelope(t, ctx, target)
			if received.ID == message {
				break
			}
		}
		delivery := metaString(received.Meta, "delivery_id")
		if delivery == "" {
			t.Fatal("fixture canonical delivery missing")
		}
		event := map[string]any{"kind": "user_input", "text": "synthetic restart history " + input.name, "delivery_id": delivery, "origin_delivery_id": delivery, "turn_id": "restart-turn-" + input.name, "data": map[string]any{"lifecycle": "admitted", "source": "agent"}, "provenance": map[string]string{"kind": "human", "attested_by": "client"}}
		raw, _ := json.Marshal(event)
		if err = writeEnvelope(ctx, target, model.Envelope{ID: "restart-echo-" + input.name, Body: string(raw), Meta: map[string]any{"type": terminalSessionEventType}}); err != nil {
			t.Fatal("fixture admitted echo failed")
		}
		source.CloseNow()
	}
	var before store.SessionHistoryPage
	waitFor(t, 5*time.Second, "both real Hub history rows", func() bool { before = get(first.url); return len(before.Events) == 2 })
	trusted := func(p store.SessionHistoryPage) map[string]any {
		m := map[string]any{}
		labels := map[string]bool{}
		for _, r := range p.Events {
			var event map[string]any
			if json.Unmarshal(r.Event, &event) != nil {
				t.Fatal("history event invalid")
			}
			prov, ok := event["provenance"].(map[string]any)
			if !ok || prov["attested_by"] != "hub" || prov["trust_decision"] != "trusted" {
				t.Fatal("restart test positive lacks actual trusted provenance")
			}
			kind := fmt.Sprint(prov["kind"])
			if kind != "agent" {
				t.Fatal("unexpected provenance kind")
			}
			label := strings.ToLower(fmt.Sprint(prov["display_name"]))
			labels[label] = true
			m[r.EventID] = event
		}
		if !labels["opus50ts"] || !labels["timer"] {
			t.Fatal("agent/timer positive pair missing")
		}
		return m
	}
	beforeFields := trusted(before)
	target.CloseNow()
	stop(first)
	second := start(2)
	if second.pid == first.pid {
		t.Fatal("Hub OS process did not restart")
	}
	after := get(second.url)
	afterFields := trusted(after)
	if !reflect.DeepEqual(beforeFields, afterFields) {
		t.Fatal("persisted history/provenance changed across cold Hub process restart")
	}
	stop(second)
	out := map[string]any{"source": "4581b34ac1e4b9973d12c559cf6ac7772107bebf", "hub_process_before": first.pid, "hub_process_after": second.pid, "different_os_processes": true, "same_app_db": true, "same_history_db": true, "persistent_identity": "real isolated PostgreSQL shared across two Hub processes", "before": before, "after": after, "full_event_equality": true, "trusted_agent_and_timer_labels": true, "ingress": "real authenticated WS/actual admitted echo; synthetic protocol events, no model/CLI"}
	b, _ := json.MarshalIndent(out, "", "  ")
	if err = os.WriteFile(filepath.Join(dir, "restart-result.json"), b, 0600); err != nil {
		t.Fatal("fixture result export failed")
	}
	t.Logf("RESTART_OK before_pid=%d after_pid=%d rows=%d exact_event_and_provenance_equality=true", first.pid, second.pid, len(after.Events))
}
