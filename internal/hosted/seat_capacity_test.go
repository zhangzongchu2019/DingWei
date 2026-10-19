package hosted

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

func capacityTestFrame(t *testing.T, revision string, snapshot seatcapacity.Snapshot, managed map[string]Plan) json.RawMessage {
	t.Helper()
	resources := []seatcapacity.Resources{}
	seats := map[string]any{}
	for id, p := range managed {
		resources = append(resources, capacityPlan(p))
		seats[id] = map[string]any{"hold": ""}
	}
	v := seatcapacity.Evaluate(seatcapacity.DefaultPolicy(), snapshot, resources, time.Now(), time.Minute)
	raw, err := json.Marshal(map[string]any{"quota_mode": "soft", "quota_policy_revision": revision, "soft_ready": true, "cpu_total": 64, "mem_free_mb": 65536, "seat_capacity": v, "soft_seats": seats})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func netCapacitySnapshot() seatcapacity.Snapshot {
	return seatcapacity.Snapshot{Known: true, At: time.Now(), Total: seatcapacity.Resources{CPU: 8000, Memory: 16 << 30, Disk: 320 << 30}}
}

// Existing quota/hold fixtures now explicitly advertise valid B prerequisites.
// Their original assertions/trigger conditions remain; B's absent/malformed
// capability negatives are covered separately below, never silently synthesized.
func withTestSeatCapacity(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var managed []seatcapacity.Resources
	if seats, ok := m["soft_seats"].(map[string]any); ok {
		for range seats {
			managed = append(managed, seatcapacity.Resources{CPU: 2000, Memory: 4 << 30, Disk: 8 << 30})
		}
	}
	v := seatcapacity.Evaluate(seatcapacity.DefaultPolicy(), seatcapacity.Snapshot{Known: true, At: time.Now(), Total: seatcapacity.Resources{CPU: 64000, Memory: 64 << 30, Disk: 1 << 40}}, managed, time.Now(), time.Minute)
	m["seat_capacity"] = v
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCapacityRealCreatePendingEightNinthAndExistingStart(t *testing.T) {
	ctx := context.Background()
	o, idp, _, s := mkOrch(t)
	rev := strings.Repeat("a", 64)
	if err := s.upsertR31LegacyNode(ctx, Node{ID: "nd_capacity", Name: "capacity", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.setR31LegacyOnline(ctx, "nd_capacity", capacityTestFrame(t, rev, netCapacitySnapshot(), nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutQuota(ctx, Quota{UserLC: "zzc", AgentsLimit: 32, VCPULimit: 32, MemMBLimit: 128 << 10, DiskMBLimit: 1 << 20}, "admin"); err != nil {
		t.Fatal(err)
	}
	account := acctOf(t, o.pool)
	request := CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "minimum", NodeID: "nd_capacity", AllowSoft: true, CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: account, Plan: "1c1.5g40"}
	managed := map[string]Plan{}
	for i := 0; i < 8; i++ {
		request.Name = fmt.Sprintf("minimum%d", i)
		out, err := o.Create(ctx, request)
		if err != nil {
			t.Fatalf("actual create %d refused: %v", i+1, err)
		}
		managed[out.AgentID], _ = PlanByID(request.Plan)
	}
	request.Name = "ninth"
	if _, err := o.Create(ctx, request); !errors.Is(err, ErrNoNodeCapacity) {
		t.Fatalf("ninth: %v", err)
	}
	if len(idp.issued) != 8 {
		t.Fatalf("refused create issued identity: %v", idp.issued)
	}
	usage, err := s.UsageFor(ctx, idp.agentIDs)
	if err != nil || usage.Agents != 8 || usage.VCPU != 8 || usage.MemMB != 12288 || usage.DiskMB != 327680 {
		t.Fatalf("real quota debit: %+v %v", usage, err)
	}
	options, err := o.Options(ctx, "zzc")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range options.Nodes {
		if n.ID == "nd_capacity" {
			if n.SeatCapacity == nil || n.SeatCapacity.TotalSlots != 8 || n.SeatCapacity.Occupied != 8 || n.SeatCapacity.AvailableSlots != 0 || n.QuotaEligible == nil || *n.QuotaEligible {
				t.Fatalf("pending not shown/full not disabled: %+v", n)
			}
		}
	}
	// A fresh report now contains those same reservations: do not count twice.
	if err := s.setR31LegacyOnline(ctx, "nd_capacity", capacityTestFrame(t, rev, netCapacitySnapshot(), managed)); err != nil {
		t.Fatal(err)
	}
	n, err := s.GetNode(ctx, "nd_capacity")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.nodeSeatCapacity(ctx, n)
	if err != nil || v.Occupied != 8 {
		t.Fatalf("reported+queued counted twice: %+v %v", v, err)
	}
	seat, err := s.GetSeat(ctx, idp.agentIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	l := &Lifecycle{st: s}
	if err := l.quotaStartGuard(ctx, seat); err != nil {
		t.Fatalf("full node existing reservation restart: %v", err)
	}
	bad := netCapacitySnapshot()
	bad.Known = false
	if err := s.setR31LegacyOnline(ctx, n.ID, capacityTestFrame(t, rev, bad, managed)); err != nil {
		t.Fatal(err)
	}
	if err := l.quotaStartGuard(ctx, seat); !errors.Is(err, ErrNoNodeCapacity) {
		t.Fatalf("unknown start: %v", err)
	}
	after, err := s.GetSeat(ctx, seat.AgentID)
	if err != nil || after.State != seat.State || after.StateDetail != seat.StateDetail {
		t.Fatalf("B refusal mutated/held seat: %+v %v", after, err)
	}
}

func TestCapacityStoreSerializesConcurrentReservations(t *testing.T) {
	ctx := context.Background()
	o, _, _, s := mkOrch(t)
	rev := strings.Repeat("b", 64)
	if err := s.upsertR31LegacyNode(ctx, Node{ID: "nd_capacity", Name: "capacity", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.setR31LegacyOnline(ctx, "nd_capacity", capacityTestFrame(t, rev, netCapacitySnapshot(), nil)); err != nil {
		t.Fatal(err)
	}
	account := acctOf(t, o.pool)
	results := make(chan error, 9)
	begin := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 9; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-begin
			results <- s.CreateSeat(ctx, Seat{AgentID: fmt.Sprintf("ag_parallel%d", i), NodeID: "nd_capacity", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountRef: sql.NullString{String: account, Valid: true}, Plan: "1c1.5g40", CreatedBy: "zzc", QuotaConsent: []byte(`{}`)})
		}(i)
	}
	close(begin)
	wg.Wait()
	close(results)
	accepted, refused := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrNoNodeCapacity) {
			requireCapacityReasons(t, err, "cpu", "disk")
			refused++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 8 || refused != 1 {
		t.Fatalf("parallel capacity: accepted=%d refused=%d", accepted, refused)
	}
}

func TestCapacityUnknownReservesForeignLoadAndLegacyHard(t *testing.T) {
	ctx := context.Background()
	o, _, _, s := mkOrch(t)
	rev := strings.Repeat("c", 64)
	if err := s.upsertR31LegacyNode(ctx, Node{ID: "nd_capacity", Name: "capacity", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	p, _ := PlanByID("1c1.5g40")
	for _, tc := range []struct {
		name   string
		change func(*seatcapacity.Snapshot)
	}{
		{"unknown", func(s *seatcapacity.Snapshot) { s.Known = false }},
		{"stale", func(s *seatcapacity.Snapshot) { s.At = time.Now().Add(-time.Minute - time.Second) }},
		{"cpu_external", func(s *seatcapacity.Snapshot) { s.External.CPU = 7001 }},
		{"memory_reserve", func(s *seatcapacity.Snapshot) { s.Reserved.Memory = (16 << 30) - (1536 << 20) + 1 }},
		{"disk_external", func(s *seatcapacity.Snapshot) { s.External.Disk = (280 << 30) + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := netCapacitySnapshot()
			tc.change(&snapshot)
			if err := s.setR31LegacyOnline(ctx, "nd_capacity", capacityTestFrame(t, rev, snapshot, nil)); err != nil {
				t.Fatal(err)
			}
			if _, err := o.pickNode(ctx, "nd_capacity", "", p, "", true); !errors.Is(err, ErrNoNodeCapacity) {
				t.Fatalf("invalid capacity admitted: %v", err)
			}
		})
	}
	t.Run("stale_wire", func(t *testing.T) {
		// Preserve the node's originally known=true bit while its cached sample
		// ages. A fresh heartbeat must not make old CPU/memory/disk data fresh.
		var frame map[string]any
		if err := json.Unmarshal(capacityTestFrame(t, rev, netCapacitySnapshot(), nil), &frame); err != nil {
			t.Fatal(err)
		}
		frame["seat_capacity"].(map[string]any)["at"] = time.Now().Add(-time.Minute - time.Second).Format(time.RFC3339Nano)
		raw, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.setR31LegacyOnline(ctx, "nd_capacity", raw); err != nil {
			t.Fatal(err)
		}
		if _, err := o.pickNode(ctx, "nd_capacity", "", p, "", true); !errors.Is(err, ErrNoNodeCapacity) {
			t.Fatalf("fresh heartbeat laundered old capacity: %v", err)
		}
	})
	for _, raw := range []string{`{"quota_mode":"soft","quota_policy_revision":"` + rev + `","soft_ready":true,"mem_free_mb":65536}`, `{"quota_mode":"soft","quota_policy_revision":"` + rev + `","soft_ready":true,"mem_free_mb":65536,"seat_capacity":{"version":1,"known":true}}`} {
		if err := s.setR31LegacyOnline(ctx, "nd_capacity", []byte(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err := o.pickNode(ctx, "nd_capacity", "", p, "", true); !errors.Is(err, ErrNoNodeCapacity) {
			t.Fatalf("missing/malformed B admitted: %v", err)
		}
	}
	if err := s.SetNodeOnline(ctx, "nd_free", []byte(`{"mem_free_mb":16384}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := o.pickNode(ctx, "nd_free", "", p, ""); !errors.Is(err, ErrNoNodeCapacity) {
		t.Fatalf("legacy hard missing snapshot admitted: %v", err)
	}
	n, err := s.GetNode(ctx, "nd_free")
	if err != nil {
		t.Fatal(err)
	}
	if v, err := s.nodeSeatCapacity(ctx, n); err != nil || v == nil || v.Known {
		t.Fatalf("legacy hard falsely advertises measured capacity: %+v %v", v, err)
	}
}
