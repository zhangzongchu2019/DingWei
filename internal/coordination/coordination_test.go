package coordination

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func newCoordTest(t *testing.T) (*Service, *store.SQLite, context.Context, string) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "coord.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "global", "schedule.json")
	svc := New(db, bus.NewDBQueue(db, model.DirectionOut), &clock.Fake{T: time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)}, path)
	return svc, db, ctx, path
}

func TestSyncGlobalScheduleWritesSchedulesAndProgress(t *testing.T) {
	svc, db, ctx, path := newCoordTest(t)
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "dev:personal:alice", DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSchedule(ctx, model.Schedule{ID: "s1", OwnerKey: "dev:personal:alice", StartDate: "2026-07-01", EndDate: "2026-07-02", Task: "写方案", Status: "planned", Priority: 100}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddProgress(ctx, model.Progress{OwnerKey: "dev:personal:alice", TaskKey: "写方案", Note: "完成60%", Percent: 60, ReportedAt: time.Date(2026, 6, 29, 11, 0, 0, 0, time.UTC), Source: "self"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncGlobalSchedule(ctx); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, path)
	var export Export
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatal(err)
	}
	if len(export.Members) != 1 || export.Members[0].DisplayName != "Alice" {
		t.Fatalf("export members = %+v", export.Members)
	}
	if len(export.Members[0].Schedules) != 1 || export.Members[0].Schedules[0].Task != "写方案" {
		t.Fatalf("export schedules = %+v", export.Members[0].Schedules)
	}
	if len(export.Members[0].Progress) != 1 || export.Members[0].Progress[0].Note != "完成60%" {
		t.Fatalf("export progress = %+v", export.Members[0].Progress)
	}
}

func TestNotifyImpactSendsOwnerAndManagers(t *testing.T) {
	svc, db, ctx, _ := newCoordTest(t)
	owner := "dev:personal:alice"
	manager := "dev:personal:mgr"
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: manager, DisplayName: "Manager", Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.NotifyImpact(ctx, owner, "进度变更", "写方案：完成60%"); err != nil {
		t.Fatal(err)
	}
	first, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || second == nil {
		t.Fatalf("expected 2 notifications, got first=%+v second=%+v", first, second)
	}
	got := first.ChatEntityID + "\n" + second.ChatEntityID + "\n" + first.Content + "\n" + second.Content
	if !strings.Contains(got, owner) || !strings.Contains(got, manager) || !strings.Contains(got, "进度变更") || !strings.Contains(got, "写方案") {
		t.Fatalf("notifications = %s", got)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
