package store

import (
	"context"
	cr "github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"path/filepath"
	"strings"
	"testing"
)

func TestR6MigrationLedgerAndBindingIsolation(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t, filepath.Join(t.TempDir(), "r6.db"))
	defer db.Close()
	var count int
	if e := db.db.QueryRow(`SELECT count(*) FROM coordinate_review_agent_binding`).Scan(&count); e != nil || count != 0 {
		t.Fatal("R6_MAP_SEEDED", e)
	}
	b := specread.Binding{Principal: cr.AgentActor("agentA"), Provider: "github", Instance: "github.com", AccountID: "12", Login: "agent"}
	if e := db.SetCoordinateReviewAgentBinding(ctx, "admin", "agentA", &b); e != nil {
		t.Fatal(e)
	}
	got, e := db.CoordinateReviewAgentBinding(ctx, "agentA")
	if e != nil || got == nil || *got != b {
		t.Fatal("R6_MAP_NOT_PERSISTED", e)
	}
	if got, e := db.CoordinateReviewAgentBinding(ctx, "agentB"); e != nil || got != nil {
		t.Fatal("R6_MAP_CROSSED_IDENTITY")
	}
	if e := db.SetCoordinateReviewAgentBinding(ctx, "admin", "agentA", nil); e != nil {
		t.Fatal(e)
	}
	if got, _ := db.CoordinateReviewAgentBinding(ctx, "agentA"); got != nil {
		t.Fatal("R6_MAP_REVOKE_CACHED")
	}
	if e := db.db.QueryRow(`SELECT count(*) FROM coordinate_review_binding_audit`).Scan(&count); e != nil || count != 2 {
		t.Fatal("R6_MAP_AUDIT", e, count)
	}
	if _, e := db.db.Exec(`DELETE FROM schema_migrations WHERE version='0071_coordinate_review.sql'`); e != nil {
		t.Fatal(e)
	}
	if e := db.Migrate(ctx); e != nil {
		t.Fatal("R6_LEDGER_BACKFILL", e)
	}
	if _, e := db.db.Exec(`DELETE FROM schema_migrations WHERE version='0071_coordinate_review.sql'; DROP TRIGGER coordinate_review_history`); e != nil {
		t.Fatal(e)
	}
	if e := db.Migrate(ctx); e == nil || !strings.Contains(e.Error(), "partially present") {
		t.Fatal("R6_PARTIAL_LEDGER_ALLOWED", e)
	}
}
func TestR6EventOwnerImmutableAndDifferentEvents(t *testing.T) {
	db := openMigrated(t, filepath.Join(t.TempDir(), "events.db"))
	defer db.Close()
	ctx := context.Background()
	if e := db.ClaimNotificationEvent(ctx, "shared-event-000001", "r6", "assignmentA"); e != nil {
		t.Fatal(e)
	}
	if e := db.ClaimNotificationEvent(ctx, "shared-event-000001", "r9", "decisionA"); e == nil {
		t.Fatal("R6_DOUBLE_SEMANTIC_OWNER")
	}
	if e := db.ClaimNotificationEvent(ctx, "different-event-001", "r9", "decisionB"); e != nil {
		t.Fatal(e)
	}
	for _, sql := range []string{`UPDATE notification_event_owner SET semantic_owner='r9' WHERE event_id='shared-event-000001'`, `DELETE FROM notification_event_owner WHERE event_id='shared-event-000001'`} {
		if _, e := db.db.Exec(sql); e == nil {
			t.Fatal("R6_EVENT_OWNER_MUTABLE")
		}
	}
}
