package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

func reasonFrame(t *testing.T, rev string, snapshot seatcapacity.Snapshot, policy seatcapacity.Policy, managed map[string]Plan, models []string) json.RawMessage {
	t.Helper()
	var resources []seatcapacity.Resources
	seats := map[string]any{}
	for id, p := range managed {
		resources = append(resources, capacityPlan(p))
		seats[id] = map[string]any{"hold": ""}
	}
	v := seatcapacity.Evaluate(policy, snapshot, resources, time.Now(), time.Minute)
	raw, err := json.Marshal(map[string]any{"quota_mode": "soft", "quota_policy_revision": rev, "soft_ready": true, "cpu_total": 64, "mem_free_mb": 65536, "seat_capacity": v, "soft_seats": seats, "models": models})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func requireCapacityReasons(t *testing.T, err error, want ...string) {
	t.Helper()
	if !errors.Is(err, ErrNoNodeCapacity) || !reflect.DeepEqual(CapacityErrorReasons(err), want) {
		t.Fatalf("capacity error: %v reasons=%v want=%v", err, CapacityErrorReasons(err), want)
	}
}
func TestCapacityReasonSanitizationAndCompatibility(t *testing.T) {
	raw := []string{"disk", "cpu", "cpu", "/private/credential", "memory", "seat_limit", "policy_invalid"}
	err := capacityFailure(raw)
	requireCapacityReasons(t, fmt.Errorf("wrapped: %w", err), "cpu", "memory", "disk", "seat_limit", "load_unknown")
	if err.Error() != ErrNoNodeCapacity.Error() {
		t.Fatalf("raw error text: %q", err.Error())
	}
	got := CapacityErrorReasons(err)
	got[0] = "injected"
	requireCapacityReasons(t, err, "cpu", "memory", "disk", "seat_limit", "load_unknown")
	if CapacityErrorReasons(ErrNoNodeCapacity) != nil || CapacityErrorReasons(errors.New("private upstream text")) != nil {
		t.Fatal("generic errors invented typed reasons")
	}
	requireCapacityReasons(t, &NodeCapacityError{}, "load_unknown")
}
func TestCapacityRequestReasonAndModelSelection(t *testing.T) {
	ctx := context.Background()
	o, _, _, s := mkOrch(t)
	rev := strings.Repeat("d", 64)
	if err := s.UpsertNode(ctx, Node{ID: "nd_reason", Name: "reason", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	p, _ := PlanByID("4c8g")
	for _, dimension := range []string{"cpu", "memory", "disk"} {
		t.Run(dimension, func(t *testing.T) {
			snapshot := netCapacitySnapshot()
			policy := seatcapacity.DefaultPolicy()
			switch dimension {
			case "cpu":
				snapshot.Total.CPU = 2000
			case "memory":
				snapshot.Total.Memory = 6 << 30
			case "disk":
				snapshot.Total.Disk = 12 << 30
				policy.Minimum.Disk = 8 << 30
			}
			if err := s.SetNodeOnline(ctx, "nd_reason", reasonFrame(t, rev, snapshot, policy, nil, []string{"gpt-review"})); err != nil {
				t.Fatal(err)
			}
			n, _ := s.GetNode(ctx, "nd_reason")
			v, err := s.nodeSeatCapacity(ctx, n)
			if err != nil || !v.Known || v.AvailableSlots < 1 || len(v.Reasons) != 0 {
				t.Fatalf("minimum must still fit: %+v %v", v, err)
			}
			for _, model := range []string{"", "gpt-review"} {
				_, err = o.pickNode(ctx, n.ID, "", p, model, true)
				requireCapacityReasons(t, err, dimension)
			}
			if _, err = o.pickNode(ctx, n.ID, "", p, "missing-model", true); !errors.Is(err, ErrModelNotAvailable) || CapacityErrorReasons(err) != nil {
				t.Fatalf("true missing model: %v", err)
			}
			// No consent means no candidate visibility/reason projection.
			if _, err = o.pickNode(ctx, n.ID, "", p, "gpt-review", false); !errors.Is(err, ErrModelNotAvailable) || CapacityErrorReasons(err) != nil {
				t.Fatalf("consent boundary: %v", err)
			}
			snapshot.Total = seatcapacity.Resources{CPU: 8000, Memory: 16 << 30, Disk: 320 << 30}
			if err := s.SetNodeOnline(ctx, n.ID, reasonFrame(t, rev, snapshot, policy, nil, []string{"gpt-review"})); err != nil {
				t.Fatal(err)
			}
			if _, err = o.pickNode(ctx, n.ID, "", p, "gpt-review", true); err != nil {
				t.Fatalf("matching model + adequate capacity: %v", err)
			}
		})
	}
	// Auto selection unions dimensions only across matching-model candidates.
	for i, dim := range []string{"cpu", "memory"} {
		id := fmt.Sprintf("nd_union%d", i)
		if err := s.UpsertNode(ctx, Node{ID: id, Name: id, QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true","pool":"reason-pool"}`)}); err != nil {
			t.Fatal(err)
		}
		snapshot := netCapacitySnapshot()
		if dim == "cpu" {
			snapshot.Total.CPU = 2000
		} else {
			snapshot.Total.Memory = 6 << 30
		}
		if err := s.SetNodeOnline(ctx, id, reasonFrame(t, rev, snapshot, seatcapacity.DefaultPolicy(), nil, []string{"gpt-review"})); err != nil {
			t.Fatal(err)
		}
	}
	_, err := o.pickNode(ctx, "auto", "reason-pool", p, "gpt-review", true)
	requireCapacityReasons(t, err, "cpu", "memory")
}
func TestCapacityStartReasonAndUnknown(t *testing.T) {
	ctx := context.Background()
	o, _, _, s := mkOrch(t)
	rev := strings.Repeat("e", 64)
	p, _ := PlanByID("4c8g")
	if err := s.UpsertNode(ctx, Node{ID: "nd_reason", Name: "reason", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	account := acctOf(t, o.pool)
	seat := Seat{AgentID: "ag_reason", NodeID: "nd_reason", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: p.ID, CreatedBy: "zzc"}
	// Use the actual transactional write before introducing a resource shrink.
	if err := s.SetNodeOnline(ctx, seat.NodeID, reasonFrame(t, rev, netCapacitySnapshot(), seatcapacity.DefaultPolicy(), nil, nil)); err != nil {
		t.Fatal(err)
	}
	seat.AccountRef.String = account
	seat.AccountRef.Valid = true
	seat.QuotaConsent = []byte(`{}`)
	if err := s.CreateSeat(ctx, seat); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetSeat(ctx, seat.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	life := &Lifecycle{st: s}
	for _, dim := range []string{"cpu", "memory", "disk", "load_unknown"} {
		t.Run(dim, func(t *testing.T) {
			snapshot := netCapacitySnapshot()
			policy := seatcapacity.DefaultPolicy()
			switch dim {
			case "cpu":
				snapshot.Total.CPU = 2000
			case "memory":
				snapshot.Total.Memory = 6 << 30
			case "disk":
				snapshot.Total.Disk = 12 << 30
				policy.Minimum.Disk = 8 << 30
			case "load_unknown":
				snapshot.Known = false
			}
			if err := s.SetNodeOnline(ctx, seat.NodeID, reasonFrame(t, rev, snapshot, policy, map[string]Plan{seat.AgentID: p}, nil)); err != nil {
				t.Fatal(err)
			}
			requireCapacityReasons(t, life.quotaStartGuard(ctx, before), dim)
			after, err := s.GetSeat(ctx, seat.AgentID)
			if err != nil || after.State != before.State || after.StateDetail != before.StateDetail {
				t.Fatalf("B refusal changed seat: %+v %v", after, err)
			}
		})
	}
	if err := s.SetNodeOnline(ctx, seat.NodeID, reasonFrame(t, rev, netCapacitySnapshot(), seatcapacity.DefaultPolicy(), map[string]Plan{seat.AgentID: p}, nil)); err != nil {
		t.Fatal(err)
	}
	if err := life.quotaStartGuard(ctx, before); err != nil {
		t.Fatalf("restore start: %v", err)
	}
}
