package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func outboundFixture(n int) (model.OutboundIntent, model.SessionDelivery) {
	v := model.OutboundIntent{SenderID: "sender", SourceMessageID: fmt.Sprint(n), TargetID: "target", Digest: strings.Repeat("a", 64)}
	return v, model.SessionDelivery{ID: fmt.Sprintf("dw-dlv_%032x", n+1), KeyID: "fixture", SessionName: "seat", Envelope: `{}`, SourceMessageID: v.SourceMessageID}
}
func TestOutboundIntentAtomicConcurrentRestartAndRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, e := OpenSQLite(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	v, d := outboundFixture(1)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, _, err := db.CommitOutboundSessionDelivery(ctx, v, d)
			if err != nil || got.DeliveryID != d.ID {
				t.Errorf("same intent: %v %+v", err, got)
			}
		}()
	}
	wg.Wait()
	var count int
	if e = db.db.QueryRow(`SELECT count(*) FROM session_delivery WHERE source_message_id=?`, v.SourceMessageID).Scan(&count); e != nil || count != 1 {
		t.Fatalf("not one recipient row: %d %v", count, e)
	}
	db.Close()
	db, e = OpenSQLite(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, _, e = db.CommitOutboundSessionDelivery(ctx, v, d); e != nil {
		t.Fatal(e)
	}
	// Removing a full payload is not permission to re-execute the source ID.
	if _, e = db.db.Exec(`DELETE FROM session_delivery WHERE id=?`, d.ID); e != nil {
		t.Fatal(e)
	}
	got, created, e := db.CommitOutboundSessionDelivery(ctx, v, d)
	if e != nil || created || got.DeliveryID != d.ID {
		t.Fatalf("retention reopened ID: %+v %t %v", got, created, e)
	}
	var remaining int
	db.db.QueryRow(`SELECT count(*) FROM session_delivery WHERE id=?`, d.ID).Scan(&remaining)
	if remaining != 0 {
		t.Fatal("payload was recreated")
	}
	v.Digest = strings.Repeat("b", 64)
	if _, _, e = db.CommitOutboundSessionDelivery(ctx, v, d); !errors.Is(e, ErrOutboundConflict) {
		t.Fatalf("conflict lost: %v", e)
	}
}
func TestOutboundIntentRecipientFailureRollsBackAcceptance(t *testing.T) {
	db, ctx := newTestSQLite(t)
	v, d := outboundFixture(2)
	if _, e := db.db.Exec(`CREATE TRIGGER fail_outbound_queue BEFORE INSERT ON session_delivery BEGIN SELECT RAISE(ABORT,'synthetic queue failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, _, e := db.CommitOutboundSessionDelivery(ctx, v, d); e == nil {
		t.Fatal("queue failure accepted")
	}
	got, e := db.GetOutboundIntent(ctx, v.SenderID, v.SourceMessageID)
	if e != nil || got != nil {
		t.Fatalf("partial acceptance fence: %+v %v", got, e)
	}
	if _, e = db.db.Exec(`DROP TRIGGER fail_outbound_queue`); e != nil {
		t.Fatal(e)
	}
	if _, created, e := db.CommitOutboundSessionDelivery(ctx, v, d); e != nil || !created {
		t.Fatalf("same-ID recovery failed: %t %v", created, e)
	}
}

func TestOutboundIntentMigrationRejectsPartialOrChangedSchema(t *testing.T) {
	for _, sql := range []string{`DROP INDEX outbound_intent_sender`, `ALTER TABLE outbound_intent ADD COLUMN unexpected TEXT`} {
		t.Run(sql, func(t *testing.T) {
			db := openMigrated(t, filepath.Join(t.TempDir(), "schema.db"))
			defer db.Close()
			ctx := context.Background()
			if _, err := db.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version='0077_outbound_intent.sql'`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.db.ExecContext(ctx, sql); err != nil {
				t.Fatal(err)
			}
			if err := db.Migrate(ctx); err == nil || !strings.Contains(err.Error(), "0077_outbound_intent.sql") {
				t.Fatal("unverified outbound ledger schema accepted", err)
			}
		})
	}
}
