package store

import (
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSubAgentOfflineRollbackLegacyClaimer(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.BeginSubAgentConnection(ctx, "root", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReportSubAgentTree(ctx, "root", subTree()); err != nil {
		t.Fatal(err)
	}
	scope, err := db.ResolveSubAgent(ctx, "root", 1, []string{"aa"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "u_zzc", SessionName: "sender", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionEndpointDeliveryACK(ctx, "u_zzc", "sender", true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"unexecuted-a", "unexecuted-b", "maybe-executed", "acknowledged"} {
		d := model.SessionDelivery{ID: id, KeyID: "u_zzc", SessionName: "parent", SourceMessageID: id, SourceType: "session_inbound", Envelope: `{"from":"sender#u_zzc","to":"aa.parent.zzc#u_zzc","body":"unchanged"}`, SubRoute: &subagent.DeliveryRoute{To: scope}}
		if err := db.EnqueueSessionDelivery(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	db.db.ExecContext(ctx, `UPDATE session_delivery SET state='sent',attempts=1,ack_protocol=2 WHERE id='maybe-executed'`)
	db.db.ExecContext(ctx, `UPDATE session_delivery SET state='processed',processed_at='original-ack',ack_generation=7 WHERE id='acknowledged'`)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "legacy-root", KeyID: "u_zzc", SessionName: "parent", Envelope: `{"body":"legacy"}`}); err != nil {
		t.Fatal(err)
	}
	result, err := db.PrepareSubAgentRollback(ctx, "root")
	if err != nil || result.Returned != 2 || result.Unknown != 1 || result.Terminal != 1 {
		t.Fatal("AA010_ROLLBACK_CLASSIFICATION", result, err)
	}
	// This is precisely the unscoped legacy claimer, not a sub-route-aware shim.
	for i := 0; i < 3; i++ {
		got, err := db.ClaimSessionDelivery(ctx, "u_zzc", "parent", now.Add(time.Hour*time.Duration(i+1)))
		if err != nil {
			t.Fatal(err)
		}
		if got != nil && got.ID != "legacy-root" {
			t.Fatal("AA010_LEGACY_REPLAY_PARENT", got.ID)
		}
	}
	unknown, _ := db.GetSessionDelivery(ctx, "maybe-executed")
	if unknown.State != "sent" || unknown.ProcessedAt != nil {
		t.Fatal("AA010_UNKNOWN_NOT_FAKE_ACK", unknown)
	}
	ack, _ := db.GetSessionDelivery(ctx, "acknowledged")
	if ack.State != "processed" || ack.AckGeneration != 7 {
		t.Fatal("AA010_ACK_PRESERVED", ack)
	}
	var notices int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM session_delivery WHERE source_type='failure_notice' AND source_message_id IN ('unexecuted-a','unexecuted-b')`).Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices != 2 {
		t.Fatal("AA010_ROLLBACK_PER_DELIVERY_RETURN", notices)
	}
	nodes, _ := db.ListSubAgentNodes(ctx, "root")
	if len(nodes) != 2 || nodes[0].State != "draining" {
		t.Fatal("AA010_ROLLBACK_TOMBSTONES", nodes)
	}
	if err := db.BeginSubAgentConnection(ctx, "root", 2); err == nil || !strings.Contains(err.Error(), "rollback barrier") {
		t.Fatal("AA010_ROLLBACK_NO_REOPEN", err)
	}
	again, err := db.PrepareSubAgentRollback(ctx, "root")
	if err != nil || again.Returned != 0 {
		t.Fatal("AA010_ROLLBACK_IDEMPOTENT", again, err)
	}
	if output := os.Getenv("M3_ROLLBACK_DB"); output != "" {
		if _, err := db.db.ExecContext(ctx, `VACUUM INTO ?`, output); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("AA010_OFFLINE_ROLLBACK returned=%d unknown=%d terminal=%d legacy_claimer_child_rows=0; ACK and tombstones retained", result.Returned, result.Unknown, result.Terminal)
}
