package reminder

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.SQLite, *bus.DBQueue, context.Context, *clock.Fake) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "reminder.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	out := bus.NewDBQueue(db, model.DirectionOut)
	clk := &clock.Fake{T: time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)}
	return New(db, out, clk, time.UTC), db, out, ctx, clk
}

func TestRunOnceSendsDailyAndDeadlineOnce(t *testing.T) {
	svc, db, out, ctx, _ := newTestService(t)
	owner := "dev:personal:alice"
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSchedule(ctx, model.Schedule{ID: "s1", OwnerKey: owner, StartDate: "2026-07-20", EndDate: "2026-07-21", Task: "写方案", Status: "planned", Priority: 100}); err != nil {
		t.Fatal(err)
	}

	n, err := svc.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sent=%d want 2", n)
	}
	n, err = svc.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second sent=%d want 0", n)
	}
	first, _ := out.Dequeue(ctx)
	second, _ := out.Dequeue(ctx)
	if first == nil || second == nil {
		t.Fatalf("missing reminders first=%+v second=%+v", first, second)
	}
	text := first.Content + second.Content
	if !strings.Contains(text, "任务提醒") || !strings.Contains(text, "Deadline 预警") {
		t.Fatalf("reminder content = %s", text)
	}
}

func TestRunOnceSkipsDoneAndNoTask(t *testing.T) {
	svc, db, _, ctx, _ := newTestService(t)
	owner := "dev:personal:alice"
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSchedule(ctx, model.Schedule{ID: "s1", OwnerKey: owner, StartDate: "2026-07-20", EndDate: "2026-07-20", Task: "已完成", Status: "done", Priority: 100}); err != nil {
		t.Fatal(err)
	}
	n, err := svc.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("sent=%d want 0", n)
	}
}

func TestRunOnceBroadcastsRiskToOwnerAndManagersOnce(t *testing.T) {
	svc, db, out, ctx, _ := newTestService(t)
	owner := "dev:personal:alice"
	manager := "dev:personal:mgr"
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: manager, Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReportRisk(ctx, model.Risk{ID: "r1", OwnerKey: owner, Content: "测试环境不稳", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	n, err := svc.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sent=%d want 2", n)
	}
	n, _ = svc.RunOnce(ctx)
	if n != 0 {
		t.Fatalf("second sent=%d want 0", n)
	}
	var recipients []string
	for {
		msg, err := out.Dequeue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if msg == nil {
			break
		}
		recipients = append(recipients, msg.ChatEntityID)
		_ = out.Ack(ctx, msg.ID)
	}
	got := strings.Join(recipients, ",")
	if !strings.Contains(got, owner) || !strings.Contains(got, manager) {
		t.Fatalf("recipients = %v", recipients)
	}
}
