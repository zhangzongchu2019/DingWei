package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

type capacityReplyGateway struct {
	calls     []string
	failType  string
	result    NodeResult
	transport error
}

func (g *capacityReplyGateway) Send(_ context.Context, _ string, cmd NodeCommand) (NodeResult, error) {
	g.calls = append(g.calls, cmd.Type)
	if cmd.Type == g.failType {
		return g.result, g.transport
	}
	return NodeResult{OK: true}, nil
}
func registerCapacityReplyNode(t *testing.T, s *Store, id, rev string, managed map[string]Plan) {
	t.Helper()
	ctx := context.Background()
	if err := s.UpsertNode(ctx, Node{ID: id, Name: id, QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOnline(ctx, id, reasonFrame(t, rev, netCapacitySnapshot(), seatcapacity.DefaultPolicy(), managed, nil)); err != nil {
		t.Fatal(err)
	}
}
func TestCapacityReplyStrictProtocolAndRegistration(t *testing.T) {
	ctx := context.Background()
	_, _, _, s := mkOrch(t)
	rev := strings.Repeat("f", 64)
	registerCapacityReplyNode(t, s, "nd_reply", rev, nil)
	for _, tc := range []struct {
		reply string
		want  []string
	}{
		{"seat_capacity_blocked: seat_capacity_cpu", []string{"cpu"}},
		{"seat_capacity_blocked: seat_capacity_disk+memory+cpu", []string{"cpu", "memory", "disk"}},
		{"seat_capacity_blocked: seat_capacity_load_unknown", []string{"load_unknown"}},
		{"seat_capacity_blocked: seat_capacity_policy_invalid", []string{"load_unknown"}},
		{"seat_capacity_blocked: seat_capacity_seat_limit", []string{"seat_limit"}},
	} {
		requireCapacityReasons(t, s.capacityResult(ctx, "nd_reply", NodeResult{Error: tc.reply}), tc.want...)
		if s.capacityResult(ctx, "nd_reply", NodeResult{OK: true, Error: tc.reply}) != nil {
			t.Fatal("OK reply reinterpreted")
		}
		if s.capacityResult(ctx, "nd_free", NodeResult{Error: tc.reply}) != nil {
			t.Fatal("legacy hard changed")
		}
	}
	for _, raw := range []string{"engine cpu failure", "prefix seat_capacity_blocked: seat_capacity_cpu", "seat_capacity_blocked: seat_capacity_cpu\n/private/credential", "seat_capacity_blocked: seat_capacity_cpu+cpu", "seat_capacity_blocked: seat_capacity_cpu+secret", "seat_capacity_blocked: seat_capacity_", "seat_capacity_blocked: seat_capacity_cpu ", "seat_capacity_blocked: seat_capacity_" + strings.Repeat("cpu+", 200)} {
		if err := s.capacityResult(ctx, "nd_reply", NodeResult{Error: raw}); err != nil {
			t.Fatalf("near-match accepted: %v", err)
		}
	}
	for _, change := range []string{"mode", "revision", "version", "missing"} {
		var frame map[string]any
		if err := json.Unmarshal(reasonFrame(t, rev, netCapacitySnapshot(), seatcapacity.DefaultPolicy(), nil, nil), &frame); err != nil {
			t.Fatal(err)
		}
		switch change {
		case "mode":
			frame["quota_mode"] = "hard"
		case "revision":
			frame["quota_policy_revision"] = strings.Repeat("a", 64)
		case "version":
			frame["seat_capacity"].(map[string]any)["version"] = 2
		case "missing":
			delete(frame, "seat_capacity")
		}
		raw, _ := json.Marshal(frame)
		if err := s.SetNodeOnline(ctx, "nd_reply", raw); err != nil {
			t.Fatal(err)
		}
		if s.capacityResult(ctx, "nd_reply", NodeResult{Error: "seat_capacity_blocked: seat_capacity_cpu"}) != nil {
			t.Fatalf("unbound B reply accepted: %s", change)
		}
	}
}
func TestCapacityFinalGateLifecycleJobs(t *testing.T) {
	ctx := context.Background()
	life, log, susp, _, _, s, id := mkLifecycle(t)
	original, err := s.GetSeat(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := PlanByID(original.Plan)
	rev := strings.Repeat("e", 64)
	registerCapacityReplyNode(t, s, original.NodeID, rev, map[string]Plan{id: p})
	for _, op := range []string{"restart", "enable", "refresh_cred", "upgrade"} {
		for _, dimension := range []string{"cpu", "memory", "disk", "load_unknown"} {
			t.Run(op+"/"+dimension, func(t *testing.T) {
				state := "online"
				susp.suspended = false
				if op == "enable" {
					state = "disabled"
					susp.suspended = true
				}
				if err := s.SetSeatState(ctx, id, state, "", "fixture", nil); err != nil {
					t.Fatal(err)
				}
				log.reset()
				command := map[string]string{"restart": "restart_seat", "enable": "start_seat", "refresh_cred": "refresh_cred_seat", "upgrade": "upgrade_seat"}[op]
				gateway := &capacityReplyGateway{failType: command, result: NodeResult{Error: "seat_capacity_blocked: seat_capacity_" + dimension}}
				life.gw = gateway
				job, err := life.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: op, Version: "hub.local/dingwei/new@sha256:" + strings.Repeat("ab", 32)})
				if err != nil {
					t.Fatal(err)
				}
				life.RunJob(ctx, job)
				view, err := life.JobProgressFor(ctx, job, "zzc")
				if err != nil || view.Stage != "failed" || view.Reason != "node_no_capacity" || !reflect.DeepEqual(view.CapacityReasons, []string{dimension}) {
					t.Fatalf("final node gate %s: %+v %v", command, view, err)
				}
				if _, err := life.JobProgressFor(ctx, job, "eve"); !errors.Is(err, ErrNotOwner) {
					t.Fatalf("job owner guard: %v", err)
				}
				after, err := s.GetSeat(ctx, id)
				if err != nil || after.State != state || after.StateDetail != "" || after.AccountRef != original.AccountRef {
					t.Fatalf("capacity gate changed existing seat: %+v %v", after, err)
				}
				if susp.suspended != (op == "enable") || !reflect.DeepEqual(gateway.calls, []string{command}) {
					t.Fatalf("resume/no-resend: suspended=%v calls=%v", susp.suspended, gateway.calls)
				}
			})
		}
	}
	// Actual job regressions: untrusted text and normal engine/transport errors
	// retain old generic meanings; true hold remains a separate persistent state.
	for _, tc := range []struct {
		name, reply, want string
		transport         error
		hard              bool
	}{
		{name: "engine", reply: "engine failure /private/credential", want: "start_failed"},
		{name: "near-match", reply: "seat_capacity_blocked: seat_capacity_cpu\n/private/credential", want: "start_failed"},
		{name: "transport", reply: "seat_capacity_blocked: seat_capacity_cpu", want: "node_offline", transport: errors.New("fixture transport")},
		{name: "hold", reply: "soft_quota_blocked: disk_limit", want: "soft_quota_held"},
		{name: "hard", reply: "seat_capacity_blocked: seat_capacity_cpu", want: "start_failed", hard: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.hard {
				if err := s.UpsertNode(ctx, Node{ID: original.NodeID, Name: "hard", Labels: []byte(`{}`)}); err != nil {
					t.Fatal(err)
				}
				if err := s.SetNodeOnline(ctx, original.NodeID, []byte(`{"mem_free_mb":16384}`)); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetSeatState(ctx, id, "online", "", "fixture", nil); err != nil {
				t.Fatal(err)
			}
			susp.suspended = false
			gateway := &capacityReplyGateway{failType: "restart_seat", result: NodeResult{Error: tc.reply}, transport: tc.transport}
			life.gw = gateway
			job, err := life.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "restart"})
			if err != nil {
				t.Fatal(err)
			}
			life.RunJob(ctx, job)
			view, err := life.JobProgressFor(ctx, job, "zzc")
			if err != nil || view.Reason != tc.want || len(view.CapacityReasons) != 0 {
				t.Fatalf("legacy reply meaning: %+v %v", view, err)
			}
			raw, _ := json.Marshal(view)
			if strings.Contains(string(raw), "/private") || len(gateway.calls) != 1 {
				t.Fatalf("raw leak/retry: %s %v", raw, gateway.calls)
			}
			if tc.name == "hold" {
				seat, _ := s.GetSeat(ctx, id)
				if seat.StateDetail != "soft_quota_held" {
					t.Fatalf("real hold lost %+v", seat)
				}
			}
		})
	}
}
func TestCapacityFinalGateCreateJobsAndCleanup(t *testing.T) {
	ctx := context.Background()
	o, idp, _, s := mkOrch(t)
	rev := strings.Repeat("c", 64)
	registerCapacityReplyNode(t, s, "nd_reply", rev, nil)
	if err := s.PutQuota(ctx, Quota{UserLC: "zzc", AgentsLimit: 32, VCPULimit: 64, MemMBLimit: 128 << 10, DiskMBLimit: 1 << 20}, "admin"); err != nil {
		t.Fatal(err)
	}
	account := acctOf(t, o.pool)
	for _, command := range []string{"pull_image", "create_seat"} {
		for _, dim := range []string{"cpu", "memory", "disk", "load_unknown"} {
			t.Run(command+"/"+dim, func(t *testing.T) {
				gateway := &capacityReplyGateway{failType: command, result: NodeResult{Error: "seat_capacity_blocked: seat_capacity_" + dim}}
				o.gw = gateway
				out, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "finalgate", NodeID: "nd_reply", AllowSoft: true, CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: account, Plan: "1c1.5g40"})
				if err != nil {
					t.Fatal(err)
				}
				aborted := len(idp.aborted)
				o.RunJob(ctx, out.AgentID)
				view, err := o.JobProgressFor(ctx, out.AgentID, "zzc")
				if err != nil || view.Stage != "failed" || view.Reason != "node_no_capacity" || !reflect.DeepEqual(view.CapacityReasons, []string{dim}) {
					t.Fatalf("create final gate: %+v %v", view, err)
				}
				if _, err := o.JobProgressFor(ctx, out.AgentID, "eve"); !errors.Is(err, ErrNotOwner) {
					t.Fatal("owner guard lost")
				}
				seat, err := s.GetSeat(ctx, out.AgentID)
				if err != nil || seat.AccountRef.Valid || seat.StateDetail == "soft_quota_held" || len(idp.aborted) != aborted+1 || idp.aborted[len(idp.aborted)-1] != out.AgentID {
					t.Fatalf("create cleanup changed: %+v %v aborted=%v", seat, err, idp.aborted)
				}
				if _, ok := o.takeToken(out.AgentID); ok {
					t.Fatal("refused create kept enrollment token")
				}
				want := []string{"pull_image"}
				if command == "create_seat" {
					want = append(want, "create_seat")
				}
				if !reflect.DeepEqual(gateway.calls, want) {
					t.Fatalf("resend/extra commands: %v", gateway.calls)
				}
			})
		}
	}
}
