package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

type releaseGateway struct {
	calls int
	mode  string
}

func (g *releaseGateway) Send(_ context.Context, _ string, c NodeCommand) (NodeResult, error) {
	g.calls++
	if c.Type != "release_soft_hold" {
		return NodeResult{}, errors.New("unexpected command")
	}
	if g.mode == "timeout" {
		return NodeResult{}, errors.New("private transport secret")
	}
	if g.mode == "refuse" {
		return NodeResult{Error: "private engine secret"}, nil
	}
	var p map[string]string
	_ = json.Unmarshal(c.Params, &p)
	if g.mode == "wrong_receipt" {
		p["hold_id"] = strings.Repeat("e", 32)
	}
	p["state"] = "stopped"
	b, _ := json.Marshal(p)
	return NodeResult{OK: true, Payload: b}, nil
}

func releaseNodeFixture(t *testing.T, s *Store) map[string]any {
	t.Helper()
	ctx := context.Background()
	rev := strings.Repeat("a", 64)
	if err := s.UpsertNode(ctx, Node{ID: "nd_release", Name: "release", QuotaMode: "soft", QuotaPolicyRevision: rev, Labels: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	frame := map[string]any{"quota_mode": "soft", "quota_policy_revision": rev, "hold_release_version": 1, "soft_quota_policy": map[string]any{"warn_percent": 80}, "soft_seats": map[string]any{"ag_release": map[string]any{"limit_bytes": 1000, "usage": map[string]int{"allocated_bytes": 10, "apparent_bytes": 10}, "hold": "disk_limit", "hold_id": strings.Repeat("b", 32), "stop_state": "stopped", "writer_root": "/private/secret", "container_ids": []string{"secret-cid"}}}}
	setReleaseFrame(t, s, frame)
	return frame
}
func setReleaseFrame(t *testing.T, s *Store, f map[string]any) {
	t.Helper()
	b, _ := json.Marshal(f)
	if err := s.SetNodeOnline(context.Background(), "nd_release", b); err != nil {
		t.Fatal(err)
	}
}
func TestSoftHoldHubReleaseAuditAndUncertainNoResend(t *testing.T) {
	s := NewStore(testpool(t))
	ctx := context.Background()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	releaseNodeFixture(t, s)
	for _, mode := range []string{"success", "timeout", "refuse", "wrong_receipt"} {
		t.Run(mode, func(t *testing.T) {
			g := &releaseGateway{mode: mode}
			err := s.ReleaseSoftHold(ctx, g, "owner", "nd_release", "ag_release", strings.Repeat("b", 32))
			if (err == nil) != (mode == "success") || g.calls != 1 {
				t.Fatalf("result %v calls%d", err, g.calls)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("unsanitized error")
			}
		})
	}
	if count, err := s.CountAudit(ctx, "node.soft_quota.release_requested", "nd_release"); err != nil || count != 4 {
		t.Fatalf("request audit %d %v", count, err)
	}
	if count, _ := s.CountAudit(ctx, "node.soft_quota.released", "nd_release"); count != 1 {
		t.Fatal("success audit absent")
	}
	if count, _ := s.CountAudit(ctx, "node.soft_quota.release_refused", "nd_release"); count != 3 {
		t.Fatal("failure audit absent")
	}
	for _, variant := range []string{"hard", "revision", "version", "generation", "pending", "offline", "stale"} {
		t.Run(variant, func(t *testing.T) {
			f := releaseNodeFixture(t, s)
			switch variant {
			case "hard":
				f["quota_mode"] = "hard"
			case "revision":
				f["quota_policy_revision"] = strings.Repeat("f", 64)
			case "version":
				f["hold_release_version"] = 0
			case "generation":
				f["soft_seats"].(map[string]any)["ag_release"].(map[string]any)["hold_id"] = strings.Repeat("f", 32)
			case "pending":
				f["soft_seats"].(map[string]any)["ag_release"].(map[string]any)["stop_state"] = "pending"
			}
			setReleaseFrame(t, s, f)
			if variant == "offline" {
				_ = s.SetNodeState(ctx, "nd_release", "offline")
			}
			if variant == "stale" {
				_, _ = s.exec(ctx, `UPDATE g4_node SET last_seen_at=now()-interval '61 seconds' WHERE id='nd_release'`)
			}
			g := &releaseGateway{}
			if s.ReleaseSoftHold(ctx, g, "owner", "nd_release", "ag_release", strings.Repeat("b", 32)) == nil || g.calls != 0 {
				t.Fatal("preflight bypass")
			}
		})
	}
}

func TestSoftQuotaMonitorProjectionAndDurableTwoRecipientNotices(t *testing.T) {
	s := NewStore(testpool(t))
	ctx := context.Background()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	f := releaseNodeFixture(t, s)
	event := map[string]any{"id": strings.Repeat("c", 32), "seat_id": "ag_release", "reason": "disk_limit", "at": "2026-10-17T16:00:00Z"}
	f["quota_events"] = []any{event}
	setReleaseFrame(t, s, f)
	setReleaseFrame(t, s, f)
	var count int
	row, _ := s.qrow(ctx, `SELECT count(*) FROM g4_quota_notice`)
	if err := row.Scan(&count); err != nil || count != 2 {
		t.Fatalf("dedup/two destinations %d %v", count, err)
	}
	views, err := s.QuotaMonitor(ctx)
	if err != nil || len(views) != 1 || !views[0].Fresh || views[0].Seats["ag_release"].HoldID == "" {
		t.Fatalf("monitor %+v %v", views, err)
	}
	b, _ := json.Marshal(views)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "container_ids") || strings.Contains(string(b), "writer_root") {
		t.Fatal("private fields escaped")
	}
	delivered := map[string]string{}
	if err = s.DeliverQuotaNotices(ctx, func(_ context.Context, n QuotaNotice) error {
		if n.Recipient == "ops" {
			return errors.New("secret offline route")
		}
		delivered[n.Recipient] = n.DeliveryID()
		if !strings.Contains(n.Text(), "只有业主") || strings.Contains(n.Text(), "secret") {
			t.Fatal("notice contract")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 || delivered["owner"] == "" {
		t.Fatal("one recipient failure blocked other")
	}
	_, _ = s.exec(ctx, `UPDATE g4_quota_notice SET next_attempt_at=now()`)
	if err = s.DeliverQuotaNotices(ctx, func(_ context.Context, n QuotaNotice) error {
		if n.Recipient != "ops" {
			t.Fatal("owner redelivered")
		}
		delivered[n.Recipient] = n.DeliveryID()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2 || delivered["owner"] == delivered["ops"] {
		t.Fatal("recipient receipts collapsed")
	}
	if err = s.DeliverQuotaNotices(ctx, func(context.Context, QuotaNotice) error { t.Fatal("delivered event replayed"); return nil }); err != nil {
		t.Fatal(err)
	}
	// Atomic refusal: a polluted reason creates neither a receipt nor a notice.
	event["id"] = strings.Repeat("d", 32)
	event["reason"] = "/private/secret"
	b, _ = json.Marshal(f)
	if s.SetNodeOnline(ctx, "nd_release", b) == nil {
		t.Fatal("polluted event accepted")
	}
	row, _ = s.qrow(ctx, `SELECT count(*) FROM g4_quota_notice`)
	_ = row.Scan(&count)
	if count != 2 {
		t.Fatal("invalid event notified")
	}
}

func TestSoftHoldOwnerIntentAndNodeProofBeforeSeparateRestart(t *testing.T) {
	s := NewStore(testpool(t))
	ctx := context.Background()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	f := releaseNodeFixture(t, s)
	if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,plan,state,state_detail,created_by) VALUES('ag_release','nd_release','codex','gpt','token_file','1c1.5g40','stopped','soft_quota_held','owner')`); err != nil {
		t.Fatal(err)
	}
	seat := f["soft_seats"].(map[string]any)["ag_release"].(map[string]any)
	seat["hold"] = ""
	seat["last_release_id"] = strings.Repeat("b", 32)
	setReleaseFrame(t, s, f)
	v, _ := s.GetSeat(ctx, "ag_release")
	if v.StateDetail != "soft_quota_held" {
		t.Fatal("node snapshot without owner intent released hold")
	}
	seat["hold"] = "disk_limit"
	setReleaseFrame(t, s, f)
	g := &releaseGateway{mode: "timeout"}
	if s.ReleaseSoftHold(ctx, g, "owner", "nd_release", "ag_release", strings.Repeat("b", 32)) == nil || g.calls != 1 {
		t.Fatal("uncertain send not preserved")
	}
	seat["hold"] = ""
	seat["last_release_id"] = strings.Repeat("f", 32)
	setReleaseFrame(t, s, f)
	v, _ = s.GetSeat(ctx, "ag_release")
	if v.StateDetail != "soft_quota_held" {
		t.Fatal("different generation released hold")
	}
	seat["last_release_id"] = strings.Repeat("b", 32)
	setReleaseFrame(t, s, f)
	setReleaseFrame(t, s, f)
	v, _ = s.GetSeat(ctx, "ag_release")
	if v.State != "stopped" || v.StateDetail != "soft_quota_released" || g.calls != 1 {
		t.Fatalf("no-resend confirmation %+v calls%d", v, g.calls)
	}
	if n, _ := s.CountAudit(ctx, "node.soft_quota.release_confirmed", "nd_release"); n != 1 {
		t.Fatal("confirmation audit not idempotent")
	}
	// No automatic start. A later, explicit restart still traverses soft/B gates.
	plan, _ := PlanByID("1c1.5g40")
	var healthy map[string]any
	_ = json.Unmarshal(reasonFrame(t, strings.Repeat("a", 64), netCapacitySnapshot(), seatcapacity.DefaultPolicy(), map[string]Plan{"ag_release": plan}, nil), &healthy)
	healthy["soft_seats"] = f["soft_seats"]
	setReleaseFrame(t, s, healthy)
	log := &evLog{}
	gw := &lcGw{log: log}
	life := NewLifecycle(s, nil, nil, &lcSusp{log: log}, gw, nil, nil)
	if err := life.Restart(ctx, "ag_release", "owner", false); err != nil {
		t.Fatalf("explicit recovered restart %v", err)
	}
	if len(gw.sent) != 1 || gw.sent[0].Type != "restart_seat" {
		t.Fatal("restart sequence changed")
	}
	v, _ = s.GetSeat(ctx, "ag_release")
	if v.State != "starting" {
		t.Fatal("restart inferred online without new connection")
	}
}
