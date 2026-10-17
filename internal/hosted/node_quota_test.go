package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestQuotaRegistrationAndCapacityCannotSelfDowngrade(t *testing.T) {
	n := Node{Labels: json.RawMessage(`{}`)}
	if err := normalizeQuotaRegistration(&n); err != nil {
		t.Fatal(err)
	}
	if mode, _ := registeredQuota(n); mode != "hard" {
		t.Fatal("hard default changed")
	}
	for _, mode := range []string{"auto", "Soft", "invalid"} {
		if normalizeQuotaRegistration(&Node{QuotaMode: mode}) == nil {
			t.Fatal("invalid mode accepted")
		}
	}
	n = Node{Labels: json.RawMessage(`{"quota_mode":"soft"}`)}
	if normalizeQuotaRegistration(&n) == nil {
		t.Fatal("labels enabled soft")
	}
	n = Node{QuotaMode: "soft", Labels: json.RawMessage(`{}`)}
	if normalizeQuotaRegistration(&n) == nil {
		t.Fatal("unbound soft policy accepted")
	}
	rev := strings.Repeat("a", 64)
	n = Node{QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: json.RawMessage(`{"allow_soft":"true"}`)}
	if err := normalizeQuotaRegistration(&n); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	n.LastSeenAt = &now
	n.Capacity = json.RawMessage(`{"quota_mode":"soft","quota_policy_revision":"` + rev + `","soft_ready":true}`)
	if !nodeQuotaEligible(n) {
		t.Fatal("matched soft refused")
	}
	for _, raw := range []string{`{}`, `{"quota_mode":"hard"}`, `{"quota_mode":"soft","soft_ready":true}`, `{"quota_mode":"soft","quota_policy_revision":"` + rev + `","soft_ready":false}`} {
		n.Capacity = json.RawMessage(raw)
		if nodeQuotaEligible(n) {
			t.Fatal("unready/mismatched soft eligible")
		}
	}
	n = Node{Labels: json.RawMessage(`{}`), Capacity: json.RawMessage(`{"quota_mode":"soft","soft_ready":true}`)}
	if nodeQuotaEligible(n) {
		t.Fatal("capacity downgraded hard registration")
	}
}

func TestSoftNodeSchedulingConsentPriorityCapacityAndAudit(t *testing.T) {
	o, _, _, s := mkOrch(t)
	ctx := context.Background()
	rev := strings.Repeat("a", 64)
	if err := s.UpsertNode(ctx, Node{ID: "nd_soft", Name: "soft-node", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: json.RawMessage(`{"pool":"transition","allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	frame := json.RawMessage(`{"quota_mode":"soft","quota_policy_revision":"` + rev + `","soft_ready":true,"mem_free_mb":65536,"cpu_total":16,"disk_free_mb":102400}`)
	if err := s.SetNodeOnline(ctx, "nd_soft", frame); err != nil {
		t.Fatal(err)
	}
	plan := Plan{MemMB: 4096}
	n, err := o.pickNode(ctx, "auto", "", plan, "", true)
	if err != nil || n.ID != "nd_free" {
		t.Fatalf("must prefer eligible hard %+v %v", n, err)
	}
	if _, err = o.pickNode(ctx, "nd_soft", "", plan, ""); !errors.Is(err, ErrNoNodeCapacity) {
		t.Fatal("manual soft bypassed consent")
	}
	n, err = o.pickNode(ctx, "nd_soft", "", plan, "", true)
	if err != nil || n.ID != "nd_soft" {
		t.Fatalf("consenting manual soft %+v %v", n, err)
	}
	n, err = o.pickNode(ctx, "auto", "transition", plan, "", true)
	if err != nil || n.ID != "nd_soft" {
		t.Fatalf("consenting soft pool %+v %v", n, err)
	}
	if _, err = o.pickNode(ctx, "auto", "transition", plan, ""); !errors.Is(err, ErrNoNodeCapacity) {
		t.Fatal("automatic soft fallback without consent")
	}
	// More memory in an ineligible/undersized hard node must not hide another fit.
	if err := s.SetNodeOnline(ctx, "nd_free", json.RawMessage(`{"mem_free_mb":1024}`)); err != nil {
		t.Fatal(err)
	}
	n, err = o.pickNode(ctx, "auto", "", plan, "", true)
	if err != nil || n.ID != "nd_soft" {
		t.Fatalf("filtered candidates %+v %v", n, err)
	}
	acct := acctOf(t, o.pool)
	_, err = o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "softseat", NodeID: "nd_soft", AllowSoft: true, CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acct, Plan: "2c4g"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := o.Options(ctx, "zzc")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range view.Nodes {
		if v.ID == "nd_soft" {
			found = true
			if v.QuotaMode != "soft" || v.QuotaNotice != "无硬磁盘配额" || (v.QuotaEligible == nil || !*v.QuotaEligible) {
				t.Fatalf("Console mode %+v", v)
			}
		}
	}
	if !found {
		t.Fatal("soft missing from Console")
	}
	// Event receipt is durable and idempotent; it cannot retire/delete the seat.
	event := `{"quota_mode":"soft","quota_policy_revision":"` + rev + `","soft_ready":false,"quota_events":[{"id":"` + strings.Repeat("b", 32) + `","seat_id":"ag_fixture","reason":"disk_limit","at":"2026-10-17T07:00:00Z"}]}`
	for i := 0; i < 2; i++ {
		if err := s.SetNodeOnline(ctx, "nd_soft", json.RawMessage(event)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	row, err := s.qrow(ctx, `SELECT COUNT(*) FROM g4_audit WHERE action='node.soft_quota' AND target='nd_soft'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit duplication count%d err%v", count, err)
	}
	if _, err = o.pickNode(ctx, "nd_soft", "", plan, "", true); !errors.Is(err, ErrNoNodeCapacity) {
		t.Fatal("unready soft selected")
	}
	if len(NodeQuotaEventIDs(json.RawMessage(event))) != 1 {
		t.Fatal("receipt ack missing")
	}
	if err := s.SetNodeOnline(ctx, "nd_free", json.RawMessage(event)); err == nil {
		t.Fatal("hard node accepted soft events")
	}
}

func TestQuotaEventValidation(t *testing.T) {
	for _, raw := range []string{`{broken`, `{"quota_events":[{}]}`, `{"quota_events":[{"id":"x","seat_id":"../escape","reason":"disk_limit","at":"2026-10-17T07:00:00Z"}]}`} {
		if _, err := quotaEvents(json.RawMessage(raw)); err == nil {
			t.Fatal("bad event accepted")
		}
	}
}

func TestSoftCreateFailureDoesNotAbortOrReleaseIdentity(t *testing.T) {
	o, idp, gw, s := mkOrch(t)
	ctx := context.Background()
	rev := strings.Repeat("c", 64)
	if err := s.UpsertNode(ctx, Node{ID: "nd_soft", Name: "soft", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: json.RawMessage(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeOnline(ctx, "nd_soft", json.RawMessage(`{"quota_mode":"soft","quota_policy_revision":"`+rev+`","soft_ready":true,"mem_free_mb":65536}`)); err != nil {
		t.Fatal(err)
	}
	out, err := o.Create(ctx, CreateRequest{AllowSoft: true, OwnerLC: "zzc", By: "zzc", Name: "held", NodeID: "nd_soft", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"})
	if err != nil {
		t.Fatal(err)
	}
	gw.failOn = "create_seat"
	o.RunJob(ctx, out.AgentID)
	seat, err := s.GetSeat(ctx, out.AgentID)
	if err != nil || seat.StateDetail != "soft_quota_held" || len(idp.aborted) != 0 || len(idp.retired) != 0 {
		t.Fatalf("soft partial creation retired identity %+v aborted%v retired%v err%v", seat, idp.aborted, idp.retired, err)
	}
	var count int
	row, err := s.qrow(ctx, `SELECT COUNT(*) FROM g4_audit WHERE action='seat.soft_quota_consent' AND target=$1`, out.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&count); err != nil || count != 1 {
		t.Fatalf("consent audit count%d err%v", count, err)
	}
}

func TestSoftHeldLifecycleRefusesBeforeSideEffects(t *testing.T) {
	l, log, _, gw, _, s, id := mkLifecycle(t)
	ctx := context.Background()
	seat, _ := s.GetSeat(ctx, id)
	rev := strings.Repeat("d", 64)
	if err := s.UpsertNode(ctx, Node{ID: seat.NodeID, Name: "transition", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: json.RawMessage(`{"allow_soft":"true"}`)}); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"quota_mode":"soft","quota_policy_revision":"` + rev + `","soft_ready":true,"mem_free_mb":16384,"soft_seats":{"` + id + `":{"hold":"disk_limit","stop_state":"stopped"}}}`)
	if err := s.SetNodeOnline(ctx, seat.NodeID, payload); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"upgrade", "restart", "refresh_cred", "enable"} {
		state := "online"
		if op == "enable" {
			state = "disabled"
		}
		s.SetSeatState(ctx, id, state, "", "fixture", nil)
		log.reset()
		before := len(gw.sent)
		var err error
		switch op {
		case "upgrade":
			err = l.Upgrade(ctx, id, "zzc", "new-image", false)
		case "restart":
			err = l.Restart(ctx, id, "zzc", false)
		case "refresh_cred":
			err = l.RefreshCred(ctx, id, "zzc")
		case "enable":
			err = l.Enable(ctx, id, "zzc", false)
		}
		if !errors.Is(err, ErrSoftQuotaHeld) || len(log.get()) != 0 || len(gw.sent) != before {
			t.Fatalf("%s side effect %+v %v", op, log.get(), err)
		}
		if _, err = l.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: op}); !errors.Is(err, ErrSoftQuotaHeld) {
			t.Fatalf("%s job bypass %v", op, err)
		}
	}
}

func TestHardNodeResourceExtensionNegotiated(t *testing.T) {
	o, _, _, s := mkOrch(t)
	_ = o
	ctx := context.Background()
	plan, _ := PlanByID("2c4g")
	r := s.resourceParams(ctx, "nd_free", plan)
	if _, ok := r["cpu_limit_millicores"]; ok {
		t.Fatal("legacy hard command shape changed")
	}
	if err := s.SetNodeOnline(ctx, "nd_free", json.RawMessage(`{"resource_limits_version":1,"quota_mode":"hard","mem_free_mb":16384}`)); err != nil {
		t.Fatal(err)
	}
	r = s.resourceParams(ctx, "nd_free", plan)
	if r["cpu_limit_millicores"] != 2000 {
		t.Fatalf("explicit CPU limit missing %+v", r)
	}
}
