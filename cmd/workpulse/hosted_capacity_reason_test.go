package main

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

func httpCapacityFrame(t *testing.T, rev string, dimension string, managed bool) json.RawMessage {
	t.Helper()
	snapshot := seatcapacity.Snapshot{At: time.Now(), Known: true, Total: seatcapacity.Resources{CPU: 8000, Memory: 16 << 30, Disk: 320 << 30}}
	policy := seatcapacity.DefaultPolicy()
	switch dimension {
	case "cpu":
		snapshot.Total.CPU = 2000
	case "memory":
		snapshot.Total.Memory = 6 << 30
	case "disk":
		snapshot.Total.Disk = 12 << 30
		policy.Minimum.Disk = 8 << 30
	case "load_unknown":
		snapshot.Known = false
	case "stale":
		snapshot.At = time.Now().Add(-time.Minute - time.Second)
	}
	var seats []seatcapacity.Resources
	rows := map[string]any{}
	if managed {
		seats = append(seats, seatcapacity.Resources{CPU: 4000, Memory: 8 << 30, Disk: 16 << 30})
		rows["ag_reason"] = map[string]any{"hold": ""}
	}
	v := seatcapacity.Evaluate(policy, snapshot, seats, time.Now(), time.Minute)
	raw, err := json.Marshal(map[string]any{"quota_mode": "soft", "quota_policy_revision": rev, "soft_ready": true, "mem_free_mb": 16384, "cpu_total": 16, "disk_free_mb": 102400, "seat_capacity": v, "soft_seats": rows, "models": []string{"gpt-review"}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestHostedCapacityReasonsActualHTTP(t *testing.T) {
	h, deps, st, idp, gw := mkHostedHTTP(t)
	ctx := context.Background()
	rev := strings.Repeat("a", 64)
	if err := st.UpsertNode(ctx, hosted.Node{ID: "nd_rt", Name: "soft", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	accounts, err := st.ListUsableAccounts(ctx, "zzc", "codex", "gpt")
	if err != nil || len(accounts) != 1 {
		t.Fatalf("accounts %v", err)
	}
	for _, dimension := range []string{"cpu", "memory", "disk", "load_unknown", "stale"} {
		t.Run(dimension, func(t *testing.T) {
			if err := st.SetNodeOnline(ctx, "nd_rt", httpCapacityFrame(t, rev, dimension, false)); err != nil {
				t.Fatal(err)
			}
			if dimension != "load_unknown" && dimension != "stale" {
				code, body, opts := h.do("zzc", "GET", "/api/console/hosted/options", "")
				if code != 200 {
					t.Fatalf("options %d %s", code, body)
				}
				v := opts["nodes"].([]any)[0].(map[string]any)["seat_capacity"].(map[string]any)
				if v["known"] != true || v["available_slots"].(float64) < 1 || len(v["reasons"].([]any)) != 0 {
					t.Fatalf("minimum should fit: %v", v)
				}
			}
			want := dimension
			if dimension == "stale" {
				want = "load_unknown"
			}
			for _, model := range []string{"", "gpt-review", "missing-model"} {
				request, _ := json.Marshal(map[string]any{"name": "Reason", "node_id": "nd_rt", "allow_soft": true, "cli": "codex", "llm": "gpt", "account_id": accounts[0].ID, "plan_id": "4c8g", "model": model})
				code, body, m := h.do("zzc", "POST", "/api/console/hosted/agents", string(request))
				if model == "missing-model" {
					if code != 422 || m["error"] != "model_not_available" || m["capacity_reasons"] != nil {
						t.Fatalf("missing model %d %s", code, body)
					}
				} else if code != http.StatusConflict || m["error"] != "node_no_capacity" || !reflect.DeepEqual(m["capacity_reasons"], []any{want}) {
					t.Fatalf("request reason %d %s", code, body)
				}
			}
		})
	}
	if len(idp.ids) != 0 || len(gw.types()) != 0 {
		t.Fatalf("refusal issued identity/command: ids=%v commands=%v", idp.ids, gw.types())
	}
	// Actual lifecycle HTTP preflight projects the start error before enqueue,
	// credentials, state transition or a node command can be touched.
	if err := st.SetNodeOnline(ctx, "nd_rt", httpCapacityFrame(t, rev, "", false)); err != nil {
		t.Fatal(err)
	}
	seat := hosted.Seat{AgentID: "ag_reason", NodeID: "nd_rt", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "4c8g", CreatedBy: "zzc", QuotaConsent: []byte(`{}`)}
	seat.AccountRef.String = accounts[0].ID
	seat.AccountRef.Valid = true
	if err := st.CreateSeat(ctx, seat); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeatState(ctx, seat.AgentID, "online", "", "fixture", nil); err != nil {
		t.Fatal(err)
	}
	for _, dimension := range []string{"cpu", "memory", "disk", "load_unknown"} {
		if err := st.SetNodeOnline(ctx, "nd_rt", httpCapacityFrame(t, rev, dimension, true)); err != nil {
			t.Fatal(err)
		}
		code, body, m := h.do("zzc", "POST", "/api/console/hosted/agents/ag_reason/restart", `{}`)
		if code != 409 || m["error"] != "node_no_capacity" || !reflect.DeepEqual(m["capacity_reasons"], []any{dimension}) {
			t.Fatalf("restart reason %d %s", code, body)
		}
		current, err := st.GetSeat(ctx, seat.AgentID)
		if err != nil || current.State != "online" || current.StateDetail != "" {
			t.Fatalf("refusal changed seat %+v %v", current, err)
		}
	}
	// A capacity change after successful enqueue also survives the durable job
	// boundary. Run synchronously here, before any credential/node side effect.
	for _, dimension := range []string{"cpu", "load_unknown"} {
		if err := st.SetNodeOnline(ctx, "nd_rt", httpCapacityFrame(t, rev, "", true)); err != nil {
			t.Fatal(err)
		}
		job, err := deps.life.EnqueueJob(ctx, hosted.LifecycleJobRequest{AgentID: seat.AgentID, Actor: "zzc", Op: "restart"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetNodeOnline(ctx, "nd_rt", httpCapacityFrame(t, rev, dimension, true)); err != nil {
			t.Fatal(err)
		}
		deps.life.RunJob(ctx, job)
		code, body, m := h.do("zzc", "GET", "/api/console/hosted/jobs/"+job, "")
		if code != 200 || m["stage"] != "failed" || m["reason"] != "node_no_capacity" || !reflect.DeepEqual(m["capacity_reasons"], []any{dimension}) {
			t.Fatalf("durable job reason %d %s", code, body)
		}
		code, _, _ = h.do("eve", "GET", "/api/console/hosted/jobs/"+job, "")
		if code != 403 {
			t.Fatalf("job owner guard lost: %d", code)
		}
	}
	if len(gw.types()) != 0 || len(idp.ids) != 0 {
		t.Fatal("start refusal changed credentials/commands")
	}
}

// Only a protocol-shaped fake Gateway reply is injected. No node WebSocket,
// Engine or real resource exhaustion is represented by this HTTP fixture.
type finalGateHTTPGateway struct{ failType string }

func (g finalGateHTTPGateway) Send(_ context.Context, _ string, cmd hosted.NodeCommand) (hosted.NodeResult, error) {
	if cmd.Type == g.failType {
		return hosted.NodeResult{Error: "seat_capacity_blocked: seat_capacity_cpu+memory"}, nil
	}
	return hosted.NodeResult{OK: true}, nil
}
func TestHostedFinalNodeGateJobHTTP(t *testing.T) {
	h, deps, st, idp, _ := mkHostedHTTP(t)
	ctx := context.Background()
	rev := strings.Repeat("d", 64)
	if err := st.UpsertNode(ctx, hosted.Node{ID: "nd_rt", Name: "soft", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetNodeOnline(ctx, "nd_rt", httpCapacityFrame(t, rev, "", false)); err != nil {
		t.Fatal(err)
	}
	accounts, err := st.ListUsableAccounts(ctx, "zzc", "codex", "gpt")
	if err != nil {
		t.Fatal(err)
	}
	idp.abortDone = make(chan string, 1)
	for _, command := range []string{"pull_image", "create_seat"} {
		gateway := finalGateHTTPGateway{failType: command}
		deps.orch = hosted.NewOrchestrator(st, deps.pool, idp, gateway, fakeCombosRT{}, fakeImagesRT{})
		mux := http.NewServeMux()
		registerHostedRoutes(mux, deps)
		h.mux = mux
		request, _ := json.Marshal(map[string]any{"name": "Reply", "node_id": "nd_rt", "allow_soft": true, "cli": "codex", "llm": "gpt", "account_id": accounts[0].ID, "plan_id": "1c1.5g40"})
		code, body, m := h.do("zzc", "POST", "/api/console/hosted/agents", string(request))
		if code != http.StatusAccepted {
			t.Fatalf("create %d %s", code, body)
		}
		job := m["job_id"].(string)
		// Job publication precedes abort; join cleanup before inspecting fake identity
		// or reusing it in the next request (no test-side data race).
		select {
		case aborted := <-idp.abortDone:
			if aborted != job {
				t.Fatalf("abort %s != %s", aborted, job)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("job cleanup did not complete")
		}
		code, body, m = h.do("zzc", "GET", "/api/console/hosted/jobs/"+job, "")
		if code != 200 || m["stage"] != "failed" || m["reason"] != "node_no_capacity" || !reflect.DeepEqual(m["capacity_reasons"], []any{"cpu", "memory"}) {
			t.Fatalf("node final gate job HTTP %d %s", code, body)
		}
		code, _, _ = h.do("eve", "GET", "/api/console/hosted/jobs/"+job, "")
		if code != 403 {
			t.Fatalf("job owner: %d", code)
		}
		seat, err := st.GetSeat(ctx, job)
		if err != nil || seat.AccountRef.Valid || !idp.retired[job] {
			t.Fatalf("new seat cleanup: %+v %v", seat, err)
		}
	}
}
