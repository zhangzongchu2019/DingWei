package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const deliveryACKMigration = "0065_session_delivery_ack_v2.sql"

func oldDeliveryFixture(t *testing.T) (*SQLite, context.Context) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "old-delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for _, name := range []string{"0027_session_delivery.sql", "0031_session_delivery_provenance.sql", "0053_session_delivery_failed_count.sql"} {
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.db.ExecContext(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func applyDeliveryACKMigration(t *testing.T, db *SQLite, ctx context.Context) {
	t.Helper()
	body, err := migrationFS.ReadFile("migrations/" + deliveryACKMigration)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.applyMigration(ctx, deliveryACKMigration, string(body), fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
		t.Fatal(err)
	}
}

func TestSessionDeliveryACKV2MigrationPreservesHistoricalRowsAndUnknownSemantics(t *testing.T) {
	db, ctx := oldDeliveryFixture(t)
	stamp := "2026-10-26T15:11:20.391709564Z"
	states := []string{"queued", "delivered", "injected", "processed", "dead", "cancelled"}
	for _, state := range states {
		_, err := db.db.ExecContext(ctx, `INSERT INTO session_delivery
			(id,key_id,session_name,envelope_json,source_message_id,source_type,provenance,state,attempts,next_attempt_at,lease_until,last_error,created_at,delivered_at,processed_at,updated_at,failed_count)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, state, "owner", "seat", `{"body":"historical text"}`, "msg-"+state, "session", "original-provenance", state, 7, stamp, stamp, "original-reason", stamp, stamp, stamp, stamp, 5)
		if err != nil {
			t.Fatal(err)
		}
	}
	applyDeliveryACKMigration(t, db, ctx)
	for _, state := range states {
		d, err := db.GetSessionDelivery(ctx, state)
		if err != nil || d == nil {
			t.Fatalf("historical %s lost: %+v %v", state, d, err)
		}
		if d.State != state || d.Envelope != `{"body":"historical text"}` || d.Attempts != 7 || d.LastError != "original-reason" || d.Provenance != "original-provenance" || d.SourceMessageID != "msg-"+state || d.DeliveredAt == nil || d.ProcessedAt == nil {
			t.Fatalf("historical content/state changed: %+v", d)
		}
		if d.AckProtocol != 0 || d.ProcessingMode != "" || d.AckGeneration != 0 || d.InjectedAt != nil || d.TurnID != "" || d.TurnOutcome != "" {
			t.Fatalf("historical %s was promoted to a known admission: %+v", state, d)
		}
		var failed, nulls int
		if err := db.db.QueryRowContext(ctx, `SELECT failed_count,
			(ack_protocol IS NULL)+(processing_mode IS NULL)+(ack_generation IS NULL)+(injected_at IS NULL)+(turn_id IS NULL)+(turn_outcome IS NULL)
			FROM session_delivery WHERE id=?`, state).Scan(&failed, &nulls); err != nil || failed != 5 || nulls != 6 {
			t.Fatalf("historical %s changed failed count or invented protocol: failed=%d nulls=%d err=%v", state, failed, nulls, err)
		}
	}
	var count int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM session_delivery`).Scan(&count); err != nil || count != len(states) {
		t.Fatalf("migration changed row count: count=%d err=%v", count, err)
	}
}

func TestSessionDeliveryACKV2MetadataRoundTripsWithoutRewritingReceiptTimes(t *testing.T) {
	db, ctx := newTestSQLite(t)
	now := time.Now().UTC()
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "new-v2", KeyID: "owner", SessionName: "seat", Envelope: `{}`, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	admitted := now.Add(-2 * time.Second).Truncate(time.Microsecond)
	_, err := db.db.ExecContext(ctx, `UPDATE session_delivery SET ack_protocol=2,processing_mode='driver',ack_generation=3,injected_at=?,turn_id='turn-original',turn_outcome='interrupted' WHERE id='new-v2'`, admitted.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("restart migration must not repeat or reset metadata: %v", err)
	}
	d, err := db.GetSessionDelivery(ctx, "new-v2")
	if err != nil || d.AckProtocol != 2 || d.ProcessingMode != "driver" || d.AckGeneration != 3 || d.InjectedAt == nil || !d.InjectedAt.Equal(admitted) || d.TurnID != "turn-original" || d.TurnOutcome != "interrupted" {
		t.Fatalf("v2 evidence did not round trip: %+v %v", d, err)
	}
}

func TestSessionDeliveryACKV2EnqueueDoesNotInferProtocolFromNewHub(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "not-yet-dispatched", KeyID: "owner", SessionName: "seat", Envelope: `{}`}); err != nil {
		t.Fatal(err)
	}
	d, err := db.GetSessionDelivery(ctx, "not-yet-dispatched")
	if err != nil || d.AckProtocol != 0 || d.ProcessingMode != "" || d.AckGeneration != 0 || d.InjectedAt != nil {
		t.Fatalf("new Hub version invented dispatch semantics: %+v %v", d, err)
	}
}

func TestSessionDeliveryACKV2MigrationRollsBackAllColumnsOnFailure(t *testing.T) {
	db, ctx := oldDeliveryFixture(t)
	body, err := migrationFS.ReadFile("migrations/" + deliveryACKMigration)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.applyMigration(ctx, deliveryACKMigration, string(body)+"\nTHIS IS NOT SQL;", "synthetic-digest"); err == nil {
		t.Fatal("broken migration unexpectedly succeeded")
	}
	var count int
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('session_delivery') WHERE name IN ('ack_protocol','processing_mode','ack_generation','injected_at','turn_id','turn_outcome')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration left partial columns: count=%d err=%v", count, err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=?`, deliveryACKMigration).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration was recorded: count=%d err=%v", count, err)
	}
	applyDeliveryACKMigration(t, db, ctx)
}

func TestSessionDeliveryACKV2SchemaRejectsInvalidProtocolGenerationAndOutcome(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "constraints", KeyID: "owner", SessionName: "seat", Envelope: `{}`}); err != nil {
		t.Fatal(err)
	}
	for _, update := range []string{"ack_protocol=0", "ack_protocol=3", "ack_generation=0", "ack_generation=-1", "turn_outcome='assumed_success'"} {
		if _, err := db.db.ExecContext(ctx, `UPDATE session_delivery SET `+update+` WHERE id='constraints'`); err == nil {
			t.Fatalf("invalid ACK metadata was accepted: %s", update)
		}
	}
}

func TestSessionDeliveryACKV2InvalidAdmissionTimestampFailsClosed(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: "bad-time", KeyID: "owner", SessionName: "seat", Envelope: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `UPDATE session_delivery SET injected_at='not-a-time' WHERE id='bad-time'`); err != nil {
		t.Fatal(err)
	}
	if d, err := db.GetSessionDelivery(ctx, "bad-time"); err == nil || d != nil {
		t.Fatalf("invalid evidence time was silently converted to zero: %+v %v", d, err)
	}
}
