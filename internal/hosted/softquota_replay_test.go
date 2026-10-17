package hosted

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSoftHoldReleasePendingEventConverges(t *testing.T) {
	for _, mode := range []string{"success", "timeout"} {
		for _, timing := range []string{"already_received", "first_after_release", "legacy_held_error"} {
			t.Run(mode+"/"+timing, func(t *testing.T) {
				ctx := context.Background()
				s := NewStore(testpool(t))
				if err := s.ApplySchema(ctx); err != nil {
					t.Fatal(err)
				}
				f := releaseNodeFixture(t, s)
				if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,plan,state,state_detail,created_by) VALUES('ag_release','nd_release','codex','gpt','token_file','1c1.5g40','stopped','soft_quota_held','owner')`); err != nil {
					t.Fatal(err)
				}
				f["quota_events"] = []any{map[string]any{"id": strings.Repeat("b", 32), "seat_id": "ag_release", "reason": "disk_limit", "at": "2026-10-17T17:00:00Z"}}
				if timing != "first_after_release" {
					setReleaseFrame(t, s, f)
				}
				g := &releaseGateway{mode: mode}
				err := s.ReleaseSoftHold(ctx, g, "owner", "nd_release", "ag_release", strings.Repeat("b", 32))
				if (err == nil) != (mode == "success") || g.calls != 1 {
					t.Fatalf("release %v calls%d", err, g.calls)
				}
				if timing == "legacy_held_error" {
					if _, err := s.exec(ctx, `UPDATE g4_hosted_seat SET state='error',state_detail='soft_quota_held' WHERE agent_id='ag_release'`); err != nil {
						t.Fatal(err)
					}
				}
				seat := f["soft_seats"].(map[string]any)["ag_release"].(map[string]any)
				seat["hold"] = ""
				seat["hold_id"] = ""
				seat["stop_state"] = ""
				seat["last_release_id"] = strings.Repeat("b", 32)
				for i := 0; i < 3; i++ {
					setReleaseFrame(t, s, f)
					assertReleasedSeat(t, s)
				}
				delete(f, "quota_events") // Receipt ACK finally reaches the node.
				for i := 0; i < 2; i++ {
					setReleaseFrame(t, s, f)
					assertReleasedSeat(t, s)
				}
				if g.calls != 1 {
					t.Fatal("automatic command/retry after snapshot")
				}
				for action, want := range map[string]int{"node.soft_quota": 1, "node.soft_quota.release_requested": 1, "node.soft_quota.release_confirmed": 1} {
					if n, err := s.CountAudit(ctx, action, "nd_release"); err != nil || n != want {
						t.Fatalf("%s audit %d %v", action, n, err)
					}
				}
				deliveries := map[string]int{}
				for i := 0; i < 2; i++ {
					if err := s.DeliverQuotaNotices(ctx, func(_ context.Context, n QuotaNotice) error { deliveries[n.Recipient]++; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				if len(deliveries) != 2 || deliveries["owner"] != 1 || deliveries["ops"] != 1 {
					t.Fatalf("notification replay %+v", deliveries)
				}
			})
		}
	}
}
func assertReleasedSeat(t *testing.T, s *Store) {
	t.Helper()
	v, err := s.GetSeat(context.Background(), "ag_release")
	if err != nil || v.State != "stopped" || v.StateDetail != "soft_quota_released" {
		t.Fatalf("released seat stuck: %+v %v", v, err)
	}
}

func TestSoftHoldReleaseReplayPreservesCurrentHazards(t *testing.T) {
	for _, boundary := range []string{"new_hold", "new_hold_without_event", "ordinary_error", "destroyed", "disabled", "no_owner_intent", "wrong_generation"} {
		t.Run(boundary, func(t *testing.T) {
			ctx := context.Background()
			s := NewStore(testpool(t))
			if err := s.ApplySchema(ctx); err != nil {
				t.Fatal(err)
			}
			f := releaseNodeFixture(t, s)
			if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,plan,state,state_detail,created_by) VALUES('ag_release','nd_release','codex','gpt','token_file','1c1.5g40','stopped','soft_quota_held','owner')`); err != nil {
				t.Fatal(err)
			}
			event := map[string]any{"id": strings.Repeat("b", 32), "seat_id": "ag_release", "reason": "disk_limit", "at": "2026-10-17T17:00:00Z"}
			f["quota_events"] = []any{event}
			setReleaseFrame(t, s, f)
			g := &releaseGateway{}
			if boundary != "no_owner_intent" {
				if err := s.ReleaseSoftHold(ctx, g, "owner", "nd_release", "ag_release", strings.Repeat("b", 32)); err != nil {
					t.Fatal(err)
				}
			}
			seat := f["soft_seats"].(map[string]any)["ag_release"].(map[string]any)
			seat["last_release_id"] = strings.Repeat("b", 32)
			seat["hold"] = ""
			seat["hold_id"] = ""
			seat["stop_state"] = ""
			wantState, wantDetail := "stopped", "soft_quota_held"
			switch boundary {
			case "new_hold", "new_hold_without_event":
				seat["hold"] = "disk_limit"
				seat["hold_id"] = strings.Repeat("c", 32)
				seat["stop_state"] = "stopped"
				// B is current, while release A and event A are still present.
				if boundary == "new_hold" {
					f["quota_events"] = []any{event, map[string]any{"id": strings.Repeat("c", 32), "seat_id": "ag_release", "reason": "disk_limit", "at": "2026-10-17T17:10:00Z"}}
				}
			case "ordinary_error":
				wantState, wantDetail = "error", "engine_failed"
			case "destroyed", "disabled":
				wantState, wantDetail = boundary, "retired"
			case "wrong_generation":
				seat["last_release_id"] = strings.Repeat("d", 32)
			}
			if boundary == "ordinary_error" || boundary == "destroyed" || boundary == "disabled" {
				if _, err := s.exec(ctx, `UPDATE g4_hosted_seat SET state=$1,state_detail=$2 WHERE agent_id='ag_release'`, wantState, wantDetail); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 3; i++ {
				setReleaseFrame(t, s, f)
			}
			delete(f, "quota_events")
			setReleaseFrame(t, s, f)
			v, err := s.GetSeat(ctx, "ag_release")
			if err != nil || v.State != wantState || v.StateDetail != wantDetail {
				t.Fatalf("%s overwritten: %+v %v", boundary, v, err)
			}
			if boundary == "new_hold" || boundary == "new_hold_without_event" {
				node, err := s.GetNode(ctx, "nd_release")
				if err != nil {
					t.Fatal(err)
				}
				var frame map[string]any
				_ = json.Unmarshal(node.Capacity, &frame)
				if frame["soft_seats"].(map[string]any)["ag_release"].(map[string]any)["hold_id"] != strings.Repeat("c", 32) {
					t.Fatal("new generation cleared")
				}
			}
			if n, _ := s.CountAudit(ctx, "node.soft_quota.release_confirmed", "nd_release"); (boundary == "new_hold" || boundary == "new_hold_without_event" || boundary == "no_owner_intent" || boundary == "wrong_generation") && n != 0 {
				t.Fatal("release confirmed without current no-hold/matching owner proof")
			}
			wantCalls := 1
			if boundary == "no_owner_intent" {
				wantCalls = 0
			}
			if g.calls != wantCalls {
				t.Fatal("replay sent a command")
			}
		})
	}
}

func TestSoftHoldLegacyEventStateAppliedOnce(t *testing.T) {
	ctx := context.Background()
	s := NewStore(testpool(t))
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	f := releaseNodeFixture(t, s)
	if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,plan,state,state_detail,created_by) VALUES('ag_release','nd_release','codex','gpt','token_file','1c1.5g40','online','','owner')`); err != nil {
		t.Fatal(err)
	}
	delete(f, "soft_seats")
	f["quota_events"] = []any{map[string]any{"id": strings.Repeat("b", 32), "seat_id": "ag_release", "reason": "disk_limit", "at": "2026-10-17T17:00:00Z"}}
	setReleaseFrame(t, s, f)
	v, _ := s.GetSeat(ctx, "ag_release")
	if v.State != "error" || v.StateDetail != "soft_quota_held" {
		t.Fatal("first legacy event did not hold")
	}
	// A later unrelated failure must not be overwritten by historical receipt replay.
	if _, err := s.exec(ctx, `UPDATE g4_hosted_seat SET state_detail='engine_failed' WHERE agent_id='ag_release'`); err != nil {
		t.Fatal(err)
	}
	setReleaseFrame(t, s, f)
	v, _ = s.GetSeat(ctx, "ag_release")
	if v.State != "error" || v.StateDetail != "engine_failed" {
		t.Fatalf("legacy receipt side effect repeated %+v", v)
	}
}
