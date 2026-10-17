package hosted

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLoginQuotaAuditDoesNotMutateHostedSeat(t *testing.T) {
	_, _, _, s := mkOrch(t)
	ctx := context.Background()
	revision := strings.Repeat("a", 64)
	if err := s.UpsertNode(ctx, Node{ID: "nd_loginquota", Name: "loginquota", QuotaMode: "soft", QuotaPolicyRevision: revision, Labels: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	key := "login-0123456789abcdef0123456789abcdef"
	// Deliberate namespace collision proves typed login events cannot update seats.
	if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,plan,created_by,state) VALUES($1,'nd_loginquota','codex','gpt','token_file','2c4g','zzc','online')`, key); err != nil {
		t.Fatal(err)
	}
	frame := json.RawMessage(`{"quota_mode":"soft","quota_policy_revision":"` + revision + `","soft_ready":false,"soft_seats":{"` + key + `":{"writer_kind":"login","hold":"node_space_emergency","stop_state":"stopped"}},"quota_events":[{"id":"` + strings.Repeat("b", 32) + `","writer_kind":"login","seat_id":"` + key + `","reason":"node_space_emergency","at":"2026-10-17T09:00:00Z"}]}`)
	for i := 0; i < 2; i++ {
		if err := s.SetNodeQuotaCapacity(ctx, "nd_loginquota", frame); err != nil {
			t.Fatal(err)
		}
	}
	seat, err := s.GetSeat(ctx, key)
	if err != nil || seat.State != "online" {
		t.Fatalf("login event changed seat: %+v %v", seat, err)
	}
	row, err := s.qrow(ctx, `SELECT count(*) FROM g4_audit WHERE target='nd_loginquota' AND action='node.soft_quota' AND detail->>'writer_kind'='login'`)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := row.Scan(&count); err != nil || count != 1 {
		t.Fatalf("typed audit %d %v", count, err)
	}
	if len(NodeQuotaEventIDs(frame)) != 1 {
		t.Fatal("typed receipt absent")
	}
	for _, kind := range []string{"seat", "other"} {
		bad := json.RawMessage(strings.ReplaceAll(string(frame), `"writer_kind":"login"`, `"writer_kind":"`+kind+`"`))
		if s.SetNodeQuotaCapacity(ctx, "nd_loginquota", bad) == nil {
			t.Fatal("unknown kind accepted")
		}
	}
}

func TestLoginQuotaEventRequiresTypedLoginIdentity(t *testing.T) {
	raw := `{"quota_events":[{"id":"` + strings.Repeat("b", 32) + `","writer_kind":"login","seat_id":"ag_not_login","reason":"disk_limit","at":"2026-10-17T09:00:00Z"}]}`
	if _, err := quotaEvents(json.RawMessage(raw)); err == nil {
		t.Fatal("login kind accepted arbitrary seat")
	}
}
