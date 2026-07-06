package bus

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestDBQueueWrapsRepositoryByDirection(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	inbound := NewDBQueue(db, model.DirectionIn)
	outbound := NewDBQueue(db, model.DirectionOut)

	if err := inbound.Enqueue(ctx, model.Message{ID: "in1", ChatEntityID: "chat1", BotChannelID: "bot1", ChatType: model.ChatPersonal, Content: `{"text":"in"}`}); err != nil {
		t.Fatalf("inbound enqueue: %v", err)
	}
	if err := outbound.Enqueue(ctx, model.Message{ID: "out1", ChatEntityID: "chat1", BotChannelID: "bot1", ChatType: model.ChatPersonal, Content: `{"text":"out"}`}); err != nil {
		t.Fatalf("outbound enqueue: %v", err)
	}
	inMsg, err := inbound.Dequeue(ctx)
	if err != nil {
		t.Fatalf("inbound dequeue: %v", err)
	}
	if inMsg == nil || inMsg.ID != "in1" || inMsg.Direction != model.DirectionIn {
		t.Fatalf("inbound message = %+v", inMsg)
	}
	outMsg, err := outbound.Dequeue(ctx)
	if err != nil {
		t.Fatalf("outbound dequeue: %v", err)
	}
	if outMsg == nil || outMsg.ID != "out1" || outMsg.Direction != model.DirectionOut {
		t.Fatalf("outbound message = %+v", outMsg)
	}
}

func TestAsyncDBQueueFlushesByBatchSize(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "async-batch.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	q := NewAsyncDBQueue(ctx, db, model.DirectionIn, AsyncDBQueueConfig{
		QueueSize:     10,
		BatchSize:     2,
		FlushInterval: time.Hour,
	})
	if err := q.Enqueue(ctx, model.Message{ID: "a1", ChatEntityID: "chat1", BotChannelID: "bot1", ChatType: model.ChatPersonal, Content: `{"text":"1"}`}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, model.Message{ID: "a2", ChatEntityID: "chat1", BotChannelID: "bot1", ChatType: model.ChatPersonal, Content: `{"text":"2"}`}); err != nil {
		t.Fatal(err)
	}
	waitForMessage(t, ctx, q, "a1")
	waitForMessage(t, ctx, q, "a2")
}

func TestAsyncDBQueueFlushesByTimer(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "async-timer.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	q := NewAsyncDBQueue(ctx, db, model.DirectionOut, AsyncDBQueueConfig{
		QueueSize:     10,
		BatchSize:     20,
		FlushInterval: 10 * time.Millisecond,
	})
	if err := q.Enqueue(ctx, model.Message{ID: "t1", ChatEntityID: "chat1", BotChannelID: "bot1", ChatType: model.ChatPersonal, Content: `{"text":"timer"}`}); err != nil {
		t.Fatal(err)
	}
	waitForMessage(t, ctx, q, "t1")
}

func TestAsyncDBQueueBackpressuresWhenFull(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &AsyncDBQueue{
		direction: model.DirectionIn,
		ch:        make(chan model.Message, 1),
	}
	if err := q.Enqueue(ctx, model.Message{ID: "b1", ChatEntityID: "chat1", BotChannelID: "bot1", ChatType: model.ChatPersonal, Content: `{"text":"1"}`}); err != nil {
		t.Fatal(err)
	}
	blocked := make(chan error, 1)
	go func() {
		blocked <- q.Enqueue(ctx, model.Message{ID: "b2", ChatEntityID: "chat1", BotChannelID: "bot1", ChatType: model.ChatPersonal, Content: `{"text":"2"}`})
	}()
	select {
	case err := <-blocked:
		t.Fatalf("enqueue should block while queue is full, err=%v", err)
	case <-time.After(30 * time.Millisecond):
	}
	<-q.ch
	select {
	case err := <-blocked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked enqueue did not resume after queue space was released")
	}
}

func TestNewBestEffortDBQueueUsesAsyncForSQLite(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "async-best-effort.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	q := NewBestEffortDBQueue(ctx, db, model.DirectionIn, AsyncDBQueueConfig{QueueSize: 10, BatchSize: 2, FlushInterval: time.Hour})
	if _, ok := q.(*AsyncDBQueue); !ok {
		t.Fatalf("expected async queue, got %T", q)
	}
}

func waitForMessage(t *testing.T, ctx context.Context, q Queue, id string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		msg, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if msg != nil {
			if msg.ID != id {
				t.Fatalf("message id=%s want %s", msg.ID, id)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("message %s not flushed", id)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
