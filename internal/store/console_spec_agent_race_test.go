package store

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// Fixture-only SQLite function: block an actual audit INSERT while its
// production transaction still holds the sole writer connection. Register at
// process initialization, never concurrently with new database connections.
var r8TransactionBarriers sync.Map

type r8TransactionBarrier struct {
	ready, release chan struct{}
	once           sync.Once
}

func init() {
	sqlite.MustRegisterScalarFunction("r8_test_transaction_barrier", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		key, _ := args[0].(string)
		if value, ok := r8TransactionBarriers.Load(key); ok {
			barrier := value.(*r8TransactionBarrier)
			barrier.once.Do(func() { close(barrier.ready) })
			<-barrier.release
		}
		return nil, nil
	})
}
func TestR8DeterministicTransferReplyCommitOrders(t *testing.T) {
	for _, kind := range []string{"review", "criteria"} {
		for _, order := range []string{"cancel_first", "reply_first"} {
			t.Run(kind+"/"+order, func(t *testing.T) {
				db, now, q, a := r8Fixture(t, kind)
				ctx := context.Background()
				key := newID()
				barrier := &r8TransactionBarrier{ready: make(chan struct{}), release: make(chan struct{})}
				r8TransactionBarriers.Store(key, barrier)
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
				t.Cleanup(func() { release(); r8TransactionBarriers.Delete(key) })
				action := "agent_cancel_transfer"
				if order == "reply_first" {
					action = "agent_" + kind + "_received"
				}
				_, err := db.db.ExecContext(ctx, fmt.Sprintf(`CREATE TRIGGER r8_barrier AFTER INSERT ON console_spec_event WHEN NEW.action='%s' BEGIN SELECT r8_test_transaction_barrier('%s'); END`, action, key))
				if err != nil {
					t.Fatal(err)
				}
				transferred, replied := make(chan error, 1), make(chan error, 1)
				transfer := func() {
					transferred <- db.CancelTransferSpecAgent(ctx, "s", "owner-a", "乙", "editing", q.ID, 1, q.SHA, now)
				}
				reply := func() { _, e := db.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now); replied <- e }
				if order == "cancel_first" {
					go transfer()
				} else {
					go reply()
				}
				select {
				case <-barrier.ready:
				case <-time.After(5 * time.Second):
					t.Fatal("R8_TX_BARRIER_NOT_REACHED")
				}
				before := db.db.Stats().WaitCount
				if order == "cancel_first" {
					go reply()
				} else {
					go transfer()
				}
				deadline := time.Now().Add(5 * time.Second)
				for db.db.Stats().WaitCount == before && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				observed := db.db.Stats().WaitCount
				if observed <= before {
					release()
					t.Fatal("R8_COMPETING_TRANSACTION_NOT_BLOCKED")
				}
				release()
				var transferErr, replyErr error
				select {
				case transferErr = <-transferred:
				case <-time.After(5 * time.Second):
					t.Fatal("transfer did not finish")
				}
				select {
				case replyErr = <-replied:
				case <-time.After(5 * time.Second):
					t.Fatal("reply did not finish")
				}
				current, e := db.SpecAgentRequest(ctx, q.ID)
				if e != nil {
					t.Fatal(e)
				}
				gate, e := db.ConsoleSpecGateReview(ctx, "s", 1)
				if e != nil {
					t.Fatal(e)
				}
				if order == "cancel_first" {
					if transferErr != nil || replyErr == nil || current.Status != "cancelled" || gate != nil {
						t.Fatal("R8_CANCEL_FIRST_ORDER_BROKEN", transferErr, replyErr, current.Status)
					}
				} else {
					if replyErr != nil {
						t.Fatal(replyErr)
					}
					if kind == "review" {
						if transferErr == nil || current.Status != "completed" || gate == nil {
							t.Fatal("R8_REVIEW_REPLY_FIRST_ORDER_BROKEN", transferErr, current.Status)
						}
					} else {
						if transferErr != nil || current.Status != "cancelled" || gate != nil {
							t.Fatal("R8_CRITERIA_REPLY_FIRST_ORDER_BROKEN", transferErr, current.Status)
						}
					}
				}
				t.Logf("kind=%s order=%s sqlite_wait_count=%d->%d transfer=%v reply=%v status=%s gate_present=%t", kind, order, before, observed, transferErr, replyErr, current.Status, gate != nil)
			})
		}
	}
}
