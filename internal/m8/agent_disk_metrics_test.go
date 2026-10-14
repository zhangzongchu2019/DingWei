package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/agentmetrics"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Recorder + temporary identity/store fixtures only; no socket, live DB or collector.
func diskEnv(t *testing.T) (*Hub, *sessionClient, *identity.MemStore) {
	t.Helper()
	h, _, ctx := newTestHub(t)
	mem := identity.NewMemStore()
	if err := mem.CreateUser(ctx, identity.User{UserID: "zzc", ShortName: "zzc", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	svc := identity.NewService(mem, nil, nil)
	h.Identity = svc
	e := &agentEnv{t: t, ctx: ctx, hub: h, svc: svc}
	_, id := e.enroll("disk-fixture", "DiskAgent")
	c := &sessionClient{keyID: AgentKeyID("zzc"), sessionName: "diskagent", metricsAgentID: id, metricsGeneration: strings.Repeat("c", 32)}
	h.sessionClients[c.keyID] = map[string]*sessionClient{c.sessionName: c}
	return h, c, mem
}
func diskEnvelope(now time.Time) model.Envelope {
	return model.Envelope{Body: fmt.Sprintf(`{"schema":"agent-disk/v1","status":"ok","producer":"helper","sample_instance":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","sample_seq":"1","sampled_at_ms":%d,"elapsed_send_age_ms":0,"path_basis":"cli_workdir_filesystem","filesystem_instance":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","used_bytes":20,"total_bytes":100,"available_bytes":70}`, now.UnixMilli()), Meta: map[string]any{"type": "agent_environment_metrics"}}
}
func TestAgentDiskCurrentCredentialIngress(t *testing.T) {
	h, c, mem := diskEnv(t)
	now := time.Now()
	h.NowFunc = func() time.Time { return now }
	env := diskEnvelope(now)
	if !h.handleAgentDiskMetrics(context.Background(), c, env) {
		t.Fatal("not consumed")
	}
	s := h.currentAgentDisk(context.Background(), "DiskAgent.zzc", c.metricsAgentID)
	if s.Status != "ok" || s.Sample.Total == nil || *s.Sample.Total != 100 || s.Agent != c.metricsAgentID {
		t.Fatalf("snapshot %+v", s)
	}
	// Reading and exact retransmission cannot renew the original sample.
	now = now.Add(time.Second)
	h.handleAgentDiskMetrics(context.Background(), c, env)
	if got := h.currentAgentDisk(context.Background(), "DiskAgent.zzc", c.metricsAgentID); got.ReceivedAt != s.ReceivedAt || got.ExpiresInMS != 359000 {
		t.Fatal(got)
	}
	now = now.Add(359 * time.Second)
	if got := h.currentAgentDisk(context.Background(), "DiskAgent.zzc", c.metricsAgentID); got.Status != "expired" || got.Sample.Used != nil {
		t.Fatal(got)
	}
	if err := mem.SetUserState(context.Background(), "zzc", identity.UserDisabled, now); err != nil {
		t.Fatal(err)
	}
	if got := h.currentAgentDisk(context.Background(), "DiskAgent.zzc", c.metricsAgentID); got.Status != "unavailable" {
		t.Fatal(got)
	}
}
func TestAgentDiskRejectsStaleAndRelabeledSource(t *testing.T) {
	for _, mode := range []string{"old_connection", "shared_key", "body_identity", "wrong_agent", "hosted"} {
		t.Run(mode, func(t *testing.T) {
			h, c, mem := diskEnv(t)
			env := diskEnvelope(time.Now())
			switch mode {
			case "old_connection":
				copy := *c
				h.sessionClients[c.keyID][c.sessionName] = &copy
			case "shared_key":
				c.metricsAgentID = ""
			case "body_identity":
				env.Body = strings.Replace(env.Body, `"schema":`, `"agent_id":"other","schema":`, 1)
			case "wrong_agent":
				c.metricsAgentID = "other"
			case "hosted":
				ag := identity.Agent{ID: "hosted", OwnerLC: "zzc", Name: "Hosted", State: identity.AgentActive}
				if err := mem.CreateHostedAgent(context.Background(), ag, identity.Application{ID: "hostapp", OwnerLC: "zzc"}, nil); err != nil {
					t.Fatal(err)
				}
				c.metricsAgentID = ag.ID
				c.sessionName = "hosted"
				h.sessionClients[c.keyID][c.sessionName] = c
			}
			if !h.handleAgentDiskMetrics(context.Background(), c, env) || c.diskMetrics != nil && c.diskMetrics.Read(agentmetrics.Binding{Agent: c.metricsAgentID, Connection: c.metricsGeneration, Current: true}, time.Now()).Status == "ok" {
				t.Fatal("accepted invalid source")
			}
		})
	}
}
func TestAgentDiskSignedReadOnlyRoute(t *testing.T) {
	h, c, _ := diskEnv(t)
	h.handleAgentDiskMetrics(context.Background(), c, diskEnvelope(time.Now()))
	dir := t.TempDir()
	secret := []byte(strings.Repeat("s", 32))
	key := filepath.Join(dir, "key")
	os.WriteFile(key, secret, 0600)
	config, _ := json.Marshal(map[string]any{"sessions": map[string]string{"DiskAgent.zzc": "fixture"}, "targets": map[string]any{"fixture": map[string]any{"url": "http://127.0.0.1:19321", "view_unlock_v2": true, "vs_to_hub_secret_file": key}}})
	file := filepath.Join(dir, "routes")
	os.WriteFile(file, config, 0600)
	t.Setenv("VIEW_ROUTING_CONFIG", file)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/view-v2/{session}/resources", h.HandleInternalAgentDiskResources)
	call := func(body, nonce, direction, target string) *httptest.ResponseRecorder {
		path := "/internal/view-v2/DiskAgent.zzc/resources"
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
		req.Header.Set("X-DW-VS-Target", target)
		req.Header.Set("X-DW-Timestamp", stamp)
		req.Header.Set("X-DW-Nonce", nonce)
		req.Header.Set("X-DW-Signature", signViewServiceRequest(secret, direction, "POST", path, stamp, nonce, []byte(body)))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	body := fmt.Sprintf(`{"agent_id":%q,"full_name":"DiskAgent.zzc"}`, c.metricsAgentID)
	nonce := strings.Repeat("a", 32)
	w := call(body, nonce, vsToHubDirection, "fixture")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ok"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		name, body, nonce, dir, target string
		code                           int
	}{
		{"replay", body, nonce, vsToHubDirection, "fixture", 409},
		{"wrong_direction", body, strings.Repeat("b", 32), hubToVSDirection, "fixture", 401},
		{"wrong_target", body, strings.Repeat("c", 32), vsToHubDirection, "other", 503},
		{"relabel", `{"agent_id":"other","full_name":"DiskAgent.zzc"}`, strings.Repeat("d", 32), vsToHubDirection, "fixture", 403},
		{"unknown_key", strings.TrimSuffix(body, "}") + `,"principal":"fake"}`, strings.Repeat("e", 32), vsToHubDirection, "fixture", 400},
		{"oversize", strings.Repeat("x", 2049), strings.Repeat("f", 32), vsToHubDirection, "fixture", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(tc.body, tc.nonce, tc.dir, tc.target)
			if w.Code != tc.code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestAgentDiskRetiredNameCannotInheritOldConnection(t *testing.T) {
	h, c, mem := diskEnv(t)
	h.handleAgentDiskMetrics(context.Background(), c, diskEnvelope(time.Now()))
	if _, err := mem.RetireAgent(context.Background(), c.metricsAgentID, "zzc", time.Now()); err != nil {
		t.Fatal(err)
	}
	e := &agentEnv{t: t, ctx: context.Background(), hub: h, svc: h.Identity}
	_, replacement := e.enroll("replacement-device", "DiskAgent")
	if replacement == c.metricsAgentID {
		t.Fatal("fixture reused stable ID")
	}
	h.handleAgentDiskMetrics(context.Background(), c, diskEnvelope(time.Now()))
	got := h.currentAgentDisk(context.Background(), "DiskAgent.zzc", replacement)
	if got.Status != "unavailable" || got.Sample.Total != nil {
		t.Fatal(got)
	}
}

// Count real ingress identity lookups through the existing Store interface;
// no fake metrics result or DB connection is substituted for the handler.
type metricsReadCounter struct {
	identity.Store
	agentGets, userGets int
}

func (s *metricsReadCounter) GetAgent(ctx context.Context, id string) (*identity.Agent, error) {
	s.agentGets++
	return s.Store.GetAgent(ctx, id)
}
func (s *metricsReadCounter) GetUser(ctx context.Context, id string) (*identity.User, error) {
	s.userGets++
	return s.Store.GetUser(ctx, id)
}
func TestAgentDiskCheapAdmission(t *testing.T) {
	for _, mode := range []string{"burst", "oversize", "invalid_first_attempt"} {
		t.Run(mode, func(t *testing.T) {
			h, c, mem := diskEnv(t)
			counter := &metricsReadCounter{Store: mem}
			h.Identity.Store = counter
			now := time.Now()
			begun := now
			h.NowFunc = func() time.Time { return now }
			send := func(body string) {
				env := diskEnvelope(now)
				if body != "" {
					env.Body = body
				}
				if !h.handleAgentDiskMetrics(context.Background(), c, env) {
					t.Fatal("metrics not consumed")
				}
			}
			if mode == "oversize" {
				send(strings.Repeat("x", agentmetrics.MaxBytes+1))
				if counter.agentGets != 0 || counter.userGets != 0 || !c.lastMetricsAttempt.IsZero() {
					t.Fatalf("oversize incurred identity reads agent=%d user=%d", counter.agentGets, counter.userGets)
				}
				return
			}
			if mode == "invalid_first_attempt" {
				send("{}")
			} else {
				send("")
			}
			now = begun.Add(10 * time.Second)
			send("")
			now = begun.Add(25*time.Second - time.Millisecond)
			send("")
			if counter.agentGets != 1 || counter.userGets != 1 {
				t.Fatalf("25s burst incurred identity reads agent=%d user=%d, want 1 each", counter.agentGets, counter.userGets)
			}
			if !c.lastMetricsAttempt.Equal(begun) {
				t.Fatal("suppressed messages moved eligible-attempt time")
			}
			if mode == "invalid_first_attempt" && c.diskMetrics.Read(agentmetrics.Binding{Agent: c.metricsAgentID, Connection: c.metricsGeneration, Current: true}, now).Status != "unavailable" {
				t.Fatal("invalid attempt acquired a sample")
			}
			now = begun.Add(25 * time.Second)
			env := diskEnvelope(now)
			env.Body = strings.Replace(env.Body, `"sample_seq":"1"`, `"sample_seq":"2"`, 1)
			h.handleAgentDiskMetrics(context.Background(), c, env)
			if counter.agentGets != 2 || counter.userGets != 2 {
				t.Fatalf("full 25s boundary did not query agent=%d user=%d", counter.agentGets, counter.userGets)
			}
			snapshot := c.diskMetrics.Read(agentmetrics.Binding{Agent: c.metricsAgentID, Connection: c.metricsGeneration, Current: true}, now)
			if snapshot.Status != "ok" || snapshot.Sample.Seq != "2" {
				t.Fatalf("full 25s boundary did not accept next sample %+v", snapshot)
			}
		})
	}
}
