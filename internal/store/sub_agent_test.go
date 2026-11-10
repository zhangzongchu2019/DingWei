package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func subTree() subagent.Snapshot {
	return subagent.Snapshot{ConnSeq: 1, Revision: 1, Nodes: []subagent.Node{{NodeID: "a", Name: "aa", Aliases: []string{"alias"}, State: "active", Generation: 1}, {NodeID: "b", Name: "bb", State: "active", Generation: 1}}}
}
func requireSubError(t *testing.T, err error, code, label string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "sub_agent_"+code) {
		t.Fatalf("%s got %v want %s", label, err, code)
	}
}
func TestSubAgentSnapshotRecoveryAndTombstone(t *testing.T) {
	db, ctx := newTestSQLite(t)
	snap := subTree()
	if err := db.BeginSubAgentConnection(ctx, "root", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolveSubAgent(ctx, "root", 1, []string{"aa"}); err == nil {
		t.Fatal("AA003_RECOVERY_NOT_NOTFOUND")
	}
	if rev, err := db.ReportSubAgentTree(ctx, "root", snap); rev != 1 || err != nil {
		t.Fatal(rev, err)
	}
	if _, err := db.ReportSubAgentTree(ctx, "root", snap); err != nil {
		t.Fatal("AA009_IDEMPOTENT", err)
	}
	scope, err := db.ResolveSubAgent(ctx, "root", 1, []string{"alias"})
	if err != nil || scope.NodeID != "a" || scope.Path[0] != "aa" {
		t.Fatal("AA003_ALIAS_ROUTE", scope, err)
	}
	_, err = db.ResolveSubAgent(ctx, "root", 1, []string{"xx"})
	requireSubError(t, err, "not_found", "AA003_NOT_FOUND")
	changed := subTree()
	changed.Nodes[0].Reason = "changed"
	_, err = db.ReportSubAgentTree(ctx, "root", changed)
	requireSubError(t, err, "path_conflict", "AA009_SAME_REV_DIFFERENT_DIGEST")
	changed = subTree()
	changed.Revision = 2
	changed.BaseRevision = 1
	changed.Nodes[0].State = "ended"
	changed.Nodes[0].Reason = "completed"
	if _, err = db.ReportSubAgentTree(ctx, "root", changed); err != nil {
		t.Fatal(err)
	}
	_, err = db.ReportSubAgentTree(ctx, "root", snap)
	requireSubError(t, err, "path_conflict", "AA009_REVISION_ROLLBACK")
	_, err = db.ResolveSubAgent(ctx, "root", 1, []string{"alias"})
	requireSubError(t, err, "ended", "AA003_ENDED_DISTINCT")
	if err = db.BeginSubAgentConnection(ctx, "root", 2); err != nil {
		t.Fatal(err)
	}
	_, err = db.ReportSubAgentTree(ctx, "root", changed)
	requireSubError(t, err, "generation_mismatch", "AA009_STALE_CONNECTION")
	changed.ConnSeq = 2
	if _, err = db.ReportSubAgentTree(ctx, "root", changed); err != nil {
		t.Fatal("AA009_RECONNECT_IDEMPOTENT", err)
	}
	changed.Revision = 3
	changed.BaseRevision = 2
	changed.Nodes[0].State = "active"
	_, err = db.ReportSubAgentTree(ctx, "root", changed)
	requireSubError(t, err, "ended", "AA009_ENDED_CANNOT_REVIVE")
	changed.Nodes[0].Generation = 2
	_, err = db.ReportSubAgentTree(ctx, "root", changed)
	requireSubError(t, err, "generation_mismatch", "AA009_NODE_GENERATION_IMMUTABLE")
	changed.Nodes = changed.Nodes[1:]
	_, err = db.ReportSubAgentTree(ctx, "root", changed)
	requireSubError(t, err, "path_conflict", "AA009_OMISSION_CANNOT_ERASE")
	changed = subTree()
	changed.ConnSeq = 2
	changed.Revision = 3
	changed.BaseRevision = 2
	changed.Nodes[0].State = "ended"
	changed.Nodes[0].Aliases = nil
	changed.Nodes[1].Aliases = []string{"alias"}
	_, err = db.ReportSubAgentTree(ctx, "root", changed)
	requireSubError(t, err, "alias_conflict", "AA003_ALIAS_REBIND")
}
func TestSubAgentDeliverySidecarAtomicAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	defer func() { db.Close() }()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.BeginSubAgentConnection(ctx, "root", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReportSubAgentTree(ctx, "root", subTree()); err != nil {
		t.Fatal(err)
	}
	scope, err := db.ResolveSubAgent(ctx, "root", 1, []string{"aa"})
	if err != nil {
		t.Fatal(err)
	}
	delivery := model.SessionDelivery{ID: "scoped", KeyID: "u_user", SessionName: "root", Envelope: `{"body":"old"}`, SubRoute: &subagent.DeliveryRoute{To: scope}}
	if _, err = db.db.ExecContext(ctx, `CREATE TRIGGER inject_subroute_failure BEFORE INSERT ON session_delivery_sub_route BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if err = db.EnqueueSessionDelivery(ctx, delivery); err == nil {
		t.Fatal("AA009_INJECTED_FAILURE_MUST_FAIL")
	}
	if got, _ := db.GetSessionDelivery(ctx, delivery.ID); got != nil {
		t.Fatal("AA009_SIDECAR_ATOMIC", got)
	}
	if _, err = db.db.ExecContext(ctx, `DROP TRIGGER inject_subroute_failure`); err != nil {
		t.Fatal(err)
	}
	if err = db.EnqueueSessionDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	if err = db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "legacy", KeyID: "k", SessionName: "r", Envelope: `{"body":"legacy"}`}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := db.SessionDeliverySubRoute(ctx, "scoped")
	if err != nil || got == nil || got.To.NodeID != "a" {
		t.Fatal("AA009_RESTART", got, err)
	}
	if old, err := db.GetSessionDelivery(ctx, "legacy"); err != nil || old == nil || old.Envelope != `{"body":"legacy"}` {
		t.Fatal("AA009_LEGACY_READ", old, err)
	}
	if old, err := db.SessionDeliverySubRoute(ctx, "legacy"); err != nil || old != nil {
		t.Fatal("AA009_LEGACY_ROOT_SCOPE", old, err)
	}
	got.To.Generation = 99
	requireSubError(t, db.CheckSubAgentRoute(ctx, *got), "generation_mismatch", "AA009_GENERATION_CHECK")
	snap := subTree()
	snap.Revision = 2
	snap.BaseRevision = 1
	snap.Nodes[0].State = "ended"
	snap.Nodes[0].Reason = "operator_closed"
	if _, err = db.ReportSubAgentTree(ctx, "root", snap); err != nil {
		t.Fatal(err)
	}
	delivery.ID = "late"
	requireSubError(t, db.EnqueueSessionDelivery(ctx, delivery), "ended", "AA009_CLOSE_BEFORE_ENQUEUE")
	if got, _ := db.GetSessionDelivery(ctx, "late"); got != nil {
		t.Fatal("AA009_NO_LATE_ROW")
	}
}
func TestSubAgentMigrationPreservesOldData(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Remove only empty new objects to reproduce the immediately preceding schema.
	for _, q := range []string{`DROP TABLE session_delivery_sub_route`, `DROP TABLE sub_agent_alias`, `DROP TABLE sub_agent_registry`, `DELETE FROM schema_migrations WHERE version='0075_sub_agent_registry.sql'`} {
		if _, err = db.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	d := model.SessionDelivery{ID: "pre-upgrade", KeyID: "old", SessionName: "main", Envelope: `{"body":"unchanged"}`, CreatedAt: time.Now().UTC()}
	if err = db.EnqueueSessionDelivery(ctx, d); err != nil {
		t.Fatal(err)
	}
	before, err := db.GetSessionDelivery(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := db.GetSessionDelivery(ctx, d.ID)
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if err != nil || string(a) != string(b) {
		t.Fatal("AA009_MIGRATION_OLD_DATA", string(a), string(b), err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal("AA009_REPEAT_MIGRATION", err)
	}
	var fk string
	if err = db.db.QueryRowContext(ctx, `PRAGMA foreign_key_check`).Scan(&fk); err != sql.ErrNoRows {
		t.Fatal(err)
	}
}

func TestSubAgentFailureNoticeRetainsSourceNode(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.BeginSubAgentConnection(ctx, "root", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReportSubAgentTree(ctx, "root", subTree()); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "source", SessionName: "sender", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionEndpointDeliveryACK(ctx, "source", "sender", true); err != nil {
		t.Fatal(err)
	}
	source, err := db.ResolveSubAgent(ctx, "root", 1, []string{"aa"})
	if err != nil {
		t.Fatal(err)
	}
	source.FullName = "aa.sender.user"
	env := model.Envelope{ID: "child-send", From: "aa.sender.user#source", To: "target#other", Body: "PRIVATE body"}
	raw, _ := json.Marshal(env)
	d := model.SessionDelivery{ID: "child-failed", KeyID: "other", SessionName: "target", SourceMessageID: env.ID, SourceType: "session_inbound", Envelope: string(raw), SubRoute: &subagent.DeliveryRoute{From: source}, CreatedAt: time.Now().Add(-time.Hour)}
	if err = db.EnqueueSessionDelivery(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExpireSessionDeliveries(ctx, "other", "target", time.Now()); err != nil {
		t.Fatal(err)
	}
	notice, err := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID(d.ID))
	if err != nil || notice == nil {
		t.Fatal("AA004_FAILURE_NOTICE_EXISTS", notice, err)
	}
	if notice.SessionName != "sender" || strings.Contains(notice.Envelope, "PRIVATE") {
		t.Fatal("AA004_FAILURE_NOTICE_SCOPE", notice)
	}
	route, err := db.SessionDeliverySubRoute(ctx, notice.ID)
	if err != nil || route == nil || route.To.NodeID != "a" || route.To.Generation != 1 {
		t.Fatal("AA004_FAILURE_RETURN_TO_NODE", route, err)
	}
	var got model.Envelope
	if err = json.Unmarshal([]byte(notice.Envelope), &got); err != nil {
		t.Fatal(err)
	}
	if got.To != "aa.sender.user#source" || got.Meta["target_node_id"] != "a" {
		t.Fatal("AA004_FAILURE_NEVER_PARENT", got)
	}
}

func TestSubAgentPersistedSourceSurvivesReconnect(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.BeginSubAgentConnection(ctx, "root", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReportSubAgentTree(ctx, "root", subTree()); err != nil {
		t.Fatal(err)
	}
	source, err := db.ResolveSubAgent(ctx, "root", 1, []string{"aa"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.BeginSubAgentConnection(ctx, "root", 2); err != nil {
		t.Fatal(err)
	}
	if err = db.CheckSubAgentRoute(ctx, subagent.DeliveryRoute{From: source}); err != nil {
		t.Fatal("AA009_PERSISTED_SOURCE_RECONNECT", err)
	}
	// Retained source identity does not authorize a new ingress on the old socket.
	err = db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "stale-admission", KeyID: "k", SessionName: "r", Envelope: `{}`, SubRoute: &subagent.DeliveryRoute{From: source}})
	requireSubError(t, err, "registry_unavailable", "AA009_NEW_INGRESS_NEEDS_CURRENT_CONNECTION")
}
