package bus

import (
	"context"
	"path/filepath"
	"testing"

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
