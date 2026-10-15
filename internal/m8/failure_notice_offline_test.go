package m8

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func seedOfflineFailure(t *testing.T, db *store.SQLite, id string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "source", SessionName: "sender", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionEndpointDeliveryACK(ctx, "source", "sender", true); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(model.Envelope{ID: "message-" + id, From: "sender#source", To: "never#target", Body: "task"})
	if err := db.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: id, KeyID: "target", SessionName: "never", Envelope: string(raw), SourceMessageID: "message-" + id, SourceType: "session_inbound", CreatedAt: now.Add(-31 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
}
func TestOfflineFailureNoticeStartupAndPeriodicWithoutReconnect(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "offline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seedOfflineFailure(t, db, "startup", now)
	h := New(db, ConsoleAuthDisabled())
	defer h.Close()
	n, err := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID("startup"))
	if err != nil || n == nil || n.State != "queued" {
		t.Fatalf("startup did not persist offline notice %+v %v", n, err)
	}
	seedOfflineFailure(t, db, "periodic", now)
	ticks := make(chan time.Time, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); h.runCapabilityPendingCleanup(ctx, stop, ticks) }()
	ticks <- now
	waitFor(t, time.Second, "global periodic expiry without any endpoint connection", func() bool {
		n, _ := db.GetSessionDelivery(ctx, model.FailureNoticeDeliveryID("periodic"))
		return n != nil
	})
	close(stop)
	<-done
	d, _ := db.GetSessionDelivery(ctx, "periodic")
	if d.State != "dead" || d.LastError != "delivery_expired" {
		t.Fatalf("normal queue not globally expired %+v", d)
	}
}
