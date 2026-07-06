package schedule

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestParseDateRange(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	s, e, err := ParseDateRange("07/20-07/22", now, time.UTC)
	if err != nil || s != "2026-07-20" || e != "2026-07-22" {
		t.Fatalf("range = %q,%q,%v", s, e, err)
	}
	// 中式日期 + 单日
	s2, e2, err := ParseDateRange("7月5日", now, time.UTC)
	if err != nil || s2 != "2026-07-05" || e2 != "2026-07-05" {
		t.Fatalf("cn date = %q,%q,%v", s2, e2, err)
	}
	// 跨年推断：12 月时输入 01/05 → 次年
	dec := time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)
	s3, _, _ := ParseDateRange("01/05", dec, time.UTC)
	if s3 != "2027-01-05" {
		t.Fatalf("cross-year = %q want 2027-01-05", s3)
	}
}

func newTestSvc(t *testing.T) (*Service, context.Context) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clk := &clock.Fake{T: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)}
	db.SetClock(clk) // 让 store 的 GetPending 与 schedule 共享同一假时钟
	return New(db, clk, time.UTC), ctx
}

func TestHandleConfirmFlow(t *testing.T) {
	svc, ctx := newTestSvc(t)

	// 新增 → diff 预览
	prev, err := svc.Handle(ctx, "alice", "+ 07/20-07/22 写方案")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prev, "🆕") || !strings.Contains(prev, "写方案") {
		t.Fatalf("preview = %q", prev)
	}
	// 确认前不应入库
	if list, _ := svc.Repo.ListSchedules(ctx, "alice"); len(list) != 0 {
		t.Fatalf("before confirm len=%d", len(list))
	}
	// 确认 → 入库
	if r, err := svc.Confirm(ctx, "alice"); err != nil || !strings.Contains(r, "已生效") {
		t.Fatalf("confirm = %q,%v", r, err)
	}
	list, _ := svc.Repo.ListSchedules(ctx, "alice")
	if len(list) != 1 || list[0].StartDate != "2026-07-20" || list[0].Task != "写方案" {
		t.Fatalf("after confirm = %+v", list)
	}

	// 顺延 +3 天 → 确认
	if _, err := svc.Handle(ctx, "alice", "顺延 07/20 +3天"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.Repo.ListSchedules(ctx, "alice")
	if len(list) != 1 || list[0].StartDate != "2026-07-23" {
		t.Fatalf("after postpone = %+v", list)
	}

	// 删除 → 确认
	if _, err := svc.Handle(ctx, "alice", "- 写方案"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.Repo.ListSchedules(ctx, "alice"); len(list) != 0 {
		t.Fatalf("after delete len=%d", len(list))
	}
}

func TestCancelDiscardsPending(t *testing.T) {
	svc, ctx := newTestSvc(t)
	if _, err := svc.Handle(ctx, "bob", "+ 08/01-08/02 任务X"); err != nil {
		t.Fatal(err)
	}
	if r, _ := svc.Cancel(ctx, "bob"); !strings.Contains(r, "已取消") {
		t.Fatalf("cancel = %q", r)
	}
	// 取消后确认应无变更，且不入库
	if r, _ := svc.Confirm(ctx, "bob"); !strings.Contains(r, "没有待确认") {
		t.Fatalf("confirm-after-cancel = %q", r)
	}
	if list, _ := svc.Repo.ListSchedules(ctx, "bob"); len(list) != 0 {
		t.Fatalf("len=%d", len(list))
	}
}

func TestReplaceAndIsolation(t *testing.T) {
	svc, ctx := newTestSvc(t)
	// alice 两条
	_, _ = svc.Handle(ctx, "alice", "+ 07/01-07/02 A\n+ 07/03-07/04 B")
	_, _ = svc.Confirm(ctx, "alice")
	// 全量替换为一条
	if _, err := svc.Handle(ctx, "alice", "全量\n07/10-07/11 C"); err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Confirm(ctx, "alice")
	list, _ := svc.Repo.ListSchedules(ctx, "alice")
	if len(list) != 1 || list[0].Task != "C" {
		t.Fatalf("after replace = %+v", list)
	}
}

func TestNew_NilLocation_FallbackToShanghai(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := New(db, &clock.Fake{T: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)}, nil)
	if svc.Loc.String() != "Asia/Shanghai" {
		t.Fatalf("nil loc → Loc=%s, want Asia/Shanghai", svc.Loc)
	}
}

func TestHandleKeywordNotFoundMessage(t *testing.T) {
	svc, ctx := newTestSvc(t)
	msg, err := svc.Handle(ctx, "alice", "- 不存在")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "未匹配到排期关键词") {
		t.Fatalf("message = %q", msg)
	}
}

func TestConfirm_ExpiredRejected(t *testing.T) {
	svc, ctx := newTestSvc(t)
	if _, err := svc.Handle(ctx, "alice", "+ 07/20-07/22 写方案"); err != nil {
		t.Fatal(err)
	}
	// 推进假时钟越过 TTL（10min）
	fakeClock := svc.Clock.(*clock.Fake)
	fakeClock.Advance(11 * time.Minute)

	r, err := svc.Confirm(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r, "没有待确认") {
		t.Fatalf("expired Confirm reply = %q, want 没有待确认", r)
	}
	// 不应入库
	list, _ := svc.Repo.ListSchedules(ctx, "alice")
	if len(list) != 0 {
		t.Fatalf("expired pending should not be confirmed: %+v", list)
	}
}

func TestCancel_ExpiredRejected(t *testing.T) {
	svc, ctx := newTestSvc(t)
	if _, err := svc.Handle(ctx, "bob", "+ 08/01-08/02 任务X"); err != nil {
		t.Fatal(err)
	}
	fakeClock := svc.Clock.(*clock.Fake)
	fakeClock.Advance(11 * time.Minute)

	r, err := svc.Cancel(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r, "没有待确认") {
		t.Fatalf("expired Cancel reply = %q, want 没有待确认", r)
	}
}

func TestHandleModify(t *testing.T) {
	svc, ctx := newTestSvc(t)

	// 先建一条
	if _, err := svc.Handle(ctx, "alice", "+ 07/20-07/22 写方案"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(ctx, "alice"); err != nil {
		t.Fatal(err)
	}

	// 改期
	prev, err := svc.Handle(ctx, "alice", "改 方案 07/25-07/28")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prev, "✏️") || !strings.Contains(prev, "2026-07-25") {
		t.Fatalf("modify preview = %q", prev)
	}
	if _, err := svc.Confirm(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.Repo.ListSchedules(ctx, "alice")
	if len(list) != 1 || list[0].StartDate != "2026-07-25" || list[0].EndDate != "2026-07-28" {
		t.Fatalf("after modify = %+v", list)
	}
}
