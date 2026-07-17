package portal

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// helper: open test DB with migration
func newTestDBPortal(t *testing.T) (*store.SQLite, context.Context) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "portal-test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db, ctx
}

// TestR14P1_PortalSchedule_CacheInvalidation 验证门户 DB 动态渲染 + 缓存失效
func TestR14P1_PortalSchedule_CacheInvalidation(t *testing.T) {
	db, ctx := newTestDBPortal(t)

	// 写入初始 team doc
	if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default", Kind: "team", Content: "# 初始团队排期\n- [ ] 任务X",
		Source: "test", CreatedBy: "tester",
	}); err != nil {
		t.Fatalf("append initial: %v", err)
	}

	// 分配成员 + personal doc
	if err := db.AssignProjectMember(ctx, "proj:default", "u1"); err != nil {
		t.Fatalf("assign member: %v", err)
	}
	if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default", Kind: "personal", OwnerKey: "u1",
		Content: "# u1排期\n- [ ] 个人任务", Source: "test", CreatedBy: "tester",
	}); err != nil {
		t.Fatalf("append personal: %v", err)
	}

	srv := NewScheduleServer(db)
	srv.TTL = 100 * time.Millisecond // 短 TTL 便于测试

	// 第一次渲染
	page1, err := srv.Render(ctx, "proj:default")
	if err != nil {
		t.Fatalf("Render 1: %v", err)
	}
	if !strings.Contains(page1, "任务X") {
		t.Error("❌ 首次渲染不包含 '任务X'")
	} else {
		t.Log("✅ 首次渲染包含团队任务")
	}
	if !strings.Contains(page1, "个人任务") {
		t.Error("❌ 首次渲染不包含个人任务")
	} else {
		t.Log("✅ 首次渲染包含个人排期")
	}

	// 缓存命中：立即再取应返回相同内容
	page2, err := srv.Render(ctx, "proj:default")
	if err != nil {
		t.Fatalf("Render 2 (cache): %v", err)
	}
	if page1 != page2 {
		t.Error("❌ 缓存命中时内容应完全一致")
	} else {
		t.Log("✅ 缓存命中返回一致内容")
	}

	// 写入新版本 → 触发签名变化 → 缓存立即失效
	if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default", Kind: "team", Content: "# 更新后团队排期\n- [ ] 任务Y",
		Source: "test", CreatedBy: "tester",
	}); err != nil {
		t.Fatalf("append update: %v", err)
	}

	page3, err := srv.Render(ctx, "proj:default")
	if err != nil {
		t.Fatalf("Render 3 (after update): %v", err)
	}
	if !strings.Contains(page3, "任务Y") {
		t.Error("❌ 写入后渲染不包含新内容 '任务Y'")
	} else {
		t.Log("✅ 写入后缓存失效，读到新版内容 '任务Y'")
	}
}

// TestR14P1_PortalSchedule_HTMLStructure 验证渲染的 HTML 结构
func TestR14P1_PortalSchedule_HTMLStructure(t *testing.T) {
	db, ctx := newTestDBPortal(t)

	if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default", Kind: "team",
		Content:   "# 团队排期\n## 本周\n- [ ] 任务1\n- [x] 任务2\n\n```mermaid\ngraph TD\nA-->B\n```",
		Source:    "test", CreatedBy: "tester",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	srv := NewScheduleServer(db)
	page, err := srv.Render(ctx, "proj:default")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// 应有完整 HTML 结构
	for _, expect := range []string{"<!doctype html>", "<title>", "</html>"} {
		if !strings.Contains(page, expect) {
			t.Errorf("❌ 渲染缺少 %q", expect)
		}
	}
	t.Log("✅ HTML 结构完整")

	// 应有 mermaid 支持
	if !strings.Contains(page, "mermaid") {
		t.Error("❌ 渲染缺少 mermaid 引用")
	} else {
		t.Log("✅ 包含 mermaid 支持")
	}
}
