package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// ============================================================================
// R14 P1 验收测试矩阵
// 按 docs/交办-多项目组排期-R14.md §7 P1 DoD 逐条验证
// ============================================================================

// helper: open in-memory DB, run Migrate, return SQLite + ctx
func newTestDB_R14P1(t *testing.T) (*SQLite, context.Context) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "r14p1-test.db"))
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

// ----------------------------------------------
// ② 迁移 0008 验收
// ----------------------------------------------

func TestR14P1_Migration0008_TablesExist(t *testing.T) {
	db, _ := newTestDB_R14P1(t)

	for _, tbl := range []string{"project", "schedule_doc", "project_member", "seen_person_group"} {
		if !tableExists(t, db, tbl) {
			t.Errorf("❌ 迁移 0008: 表 %s 不存在", tbl)
		} else {
			t.Logf("✅ 表 %s 存在", tbl)
		}
	}
}

func TestR14P1_Migration0008_DefaultProjectBackfill(t *testing.T) {
	db, ctx := newTestDB_R14P1(t)

	// 验证 proj:default 已创建 (不论环境变量是否设了 WP_SCHEDULE_NOTIFY_CHAT)
	proj, err := db.GetProject(ctx, "proj:default")
	if err != nil {
		t.Fatalf("❌ GetProject(proj:default) 报错: %v", err)
	}
	if proj == nil {
		t.Fatal("❌ proj:default 未创建")
	}
	t.Logf("✅ proj:default 已创建: name=%q notify_chat=%q notify_bot=%q", proj.Name, proj.NotifyChatID, proj.NotifyBotID)

	// 验证 name 必须为 "Default Project"
	if proj.Name != "Default Project" {
		t.Errorf("❌ proj:default name 期望 'Default Project', 实际 %q", proj.Name)
	} else {
		t.Logf("✅ proj:default name = 'Default Project'")
	}
}

func TestR14P1_Migration0008_NoDefaultBusinessMembers(t *testing.T) {
	db, ctx := newTestDB_R14P1(t)

	members, err := db.ListProjectMembers(ctx, "proj:default")
	if err != nil {
		t.Fatalf("❌ ListProjectMembers 报错: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("❌ proj:default 不应 seed 业务成员: %+v", members)
	}
	t.Log("✅ proj:default 未 seed 业务成员")
}

func TestR14P1_Migration0008_NoDefaultScheduleDocImport(t *testing.T) {
	db, ctx := newTestDB_R14P1(t)

	team, err := db.LatestScheduleDoc(ctx, "proj:default", "team", "")
	if err != nil {
		t.Fatalf("❌ LatestScheduleDoc(team) 报错: %v", err)
	}
	if team != nil {
		t.Fatalf("❌ proj:default 不应 seed team schedule_doc: %+v", team)
	}
	personal, err := db.LatestScheduleDoc(ctx, "proj:default", "personal", "u1")
	if err != nil {
		t.Fatalf("❌ LatestScheduleDoc(personal) 报错: %v", err)
	}
	if personal != nil {
		t.Fatalf("❌ proj:default 不应 seed personal schedule_doc: %+v", personal)
	}
	t.Log("✅ 空库不再导入任何业务排期文档")
}

// ----------------------------------------------
// ④ 取最新版
// ----------------------------------------------

func TestR14P1_LatestScheduleDoc_ReturnsMaxVersion(t *testing.T) {
	db, ctx := newTestDB_R14P1(t)

	// 写 v1, v2, v3
	for i := 1; i <= 3; i++ {
		_, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
			ProjectID: "proj:default", Kind: "team", Content: fmt.Sprintf("# v%d", i),
			Source: "test", CreatedBy: "tester",
		})
		if err != nil {
			t.Fatalf("append v%d: %v", i, err)
		}
	}

	latest, err := db.LatestScheduleDoc(ctx, "proj:default", "team", "")
	if err != nil {
		t.Fatalf("LatestScheduleDoc: %v", err)
	}
	if latest.Version != 3 {
		t.Errorf("❌ LatestScheduleDoc version=%d (want 3)", latest.Version)
	} else {
		t.Logf("✅ LatestScheduleDoc returns version %d", latest.Version)
	}
	if !strings.Contains(latest.Content, "v3") {
		t.Errorf("❌ LatestScheduleDoc content=%s (want v3)", latest.Content)
	} else {
		t.Log("✅ LatestScheduleDoc content 是最新版")
	}

	// ListScheduleDocVersions: 返回所有版本，降序
	versions, err := db.ListScheduleDocVersions(ctx, "proj:default", "team", "")
	if err != nil {
		t.Fatalf("ListScheduleDocVersions: %v", err)
	}
	if len(versions) != 3 {
		t.Errorf("❌ ListScheduleDocVersions 返回 %d 条 (want 3)", len(versions))
	}
	if versions[0].Version != 3 || versions[2].Version != 1 {
		t.Errorf("❌ 版本排序错误: [0]=%d [2]=%d", versions[0].Version, versions[2].Version)
	} else {
		t.Log("✅ ListScheduleDocVersions 降序正确")
	}
}

// ----------------------------------------------
// ⑦ 个人 NL 直设 → append personal 新版
// ----------------------------------------------

func TestR14P1_PersonalNL_AppendScheduleDoc(t *testing.T) {
	db, ctx := newTestDB_R14P1(t)

	// 先写入初始 personal doc 并写 model.Schedule（模拟已有 schedule 行）
	if err := db.UpsertSchedule(ctx, model.Schedule{
		ID: "sched-1", OwnerKey: "u1", Task: "旧任务A",
		StartDate: "07/01", EndDate: "07/07", Status: "进行中",
	}); err != nil {
		t.Fatalf("upsert schedule: %v", err)
	}
	if err := db.UpsertSchedule(ctx, model.Schedule{
		ID: "sched-2", OwnerKey: "u1", Task: "旧任务B",
		StartDate: "07/08", EndDate: "07/14", Status: "计划中",
	}); err != nil {
		t.Fatalf("upsert schedule 2: %v", err)
	}

	// 模拟 appendPersonalDoc 行为
	items, err := db.ListSchedules(ctx, "u1")
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("ListSchedules returned %d items (want >=2)", len(items))
	}
	t.Logf("✅ 旧 schedule 行保留: %d 条", len(items))

	// append personal schedule_doc
	doc1, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default", Kind: "personal", OwnerKey: "u1",
		Content: "# u1\n- 07/01-07/07 旧任务A [进行中]\n- 07/08-07/14 旧任务B [计划中]",
		Source:  "nl", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("append personal doc v1: %v", err)
	}
	t.Logf("✅ personal doc v%d appended (NL)", doc1.Version)

	// 再次 append（模拟下次 NL 变更）
	doc2, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default", Kind: "personal", OwnerKey: "u1",
		Content: "# u1\n- 07/01-07/07 旧任务A [已完成]\n- 07/08-07/14 旧任务B [进行中]\n- 07/15-07/21 新任务C [计划中]",
		Source:  "nl", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatalf("append personal doc v2: %v", err)
	}
	t.Logf("✅ personal doc v%d appended (第二次 NL)", doc2.Version)

	// 验证旧 schedule 行仍在
	itemsAfter, _ := db.ListSchedules(ctx, "u1")
	if len(itemsAfter) < 2 {
		t.Errorf("❌ 旧 schedule 行丢失: %d 条", len(itemsAfter))
	} else {
		t.Logf("✅ 旧 schedule 行保留: %d 条", len(itemsAfter))
	}

	// 最新版应包含新任务
	latest, _ := db.LatestScheduleDoc(ctx, "proj:default", "personal", "u1")
	if latest.Version != 2 {
		t.Errorf("❌ personal latest version=%d (want 2)", latest.Version)
	} else {
		t.Logf("✅ LatestScheduleDoc personal v%d", latest.Version)
	}
	if !strings.Contains(latest.Content, "新任务C") {
		t.Error("❌ 最新版不包含 '新任务C'")
	} else {
		t.Log("✅ 最新版包含新任务C")
	}

	// 版本历史保留
	versions, _ := db.ListScheduleDocVersions(ctx, "proj:default", "personal", "u1")
	if len(versions) != 2 {
		t.Errorf("❌ 版本历史 %d 条 (want 2)", len(versions))
	} else {
		t.Logf("✅ 版本历史 %d 条，旧版本可追溯", len(versions))
	}
}
