package store

import (
	"context"
	"fmt"
	"os"
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

	// 验证 name 必须为 "研发一组"
	if proj.Name != "研发一组" {
		t.Errorf("❌ proj:default name 期望 '研发一组', 实际 %q", proj.Name)
	} else {
		t.Logf("✅ proj:default name = '研发一组'")
	}
}

func TestR14P1_Migration0008_DefaultMembers(t *testing.T) {
	db, ctx := newTestDB_R14P1(t)

	members, err := db.ListProjectMembers(ctx, "proj:default")
	if err != nil {
		t.Fatalf("❌ ListProjectMembers 报错: %v", err)
	}
	if len(members) < 3 {
		t.Errorf("❌ proj:default 成员数 %d (期望 >=3: zsf, fulei, tanping)", len(members))
	} else {
		t.Logf("✅ proj:default 成员数 %d", len(members))
	}

	got := make(map[string]bool)
	for _, m := range members {
		got[m.OwnerKey] = true
		t.Logf("  成员: owner_key=%q display=%q", m.OwnerKey, m.DisplayName)
	}
	for _, want := range []string{"zsf", "fulei", "tanping"} {
		if !got[want] {
			t.Errorf("❌ 缺少成员 %q", want)
		} else {
			t.Logf("✅ 成员 %q 已分配", want)
		}
	}
}

func TestR14P1_Migration0008_MDImportAsV1(t *testing.T) {
	// 写入临时 md 文件，验证导入为 v1
	td := t.TempDir()
	teamMD := filepath.Join(td, "AI-研究工作内容清单.md")
	zsfMD := filepath.Join(td, "工作计划-张三丰.md")
	fuleiMD := filepath.Join(td, "工作计划-符坚.md")
	tanpingMD := filepath.Join(td, "工作计划-唐盛.md")

	os.WriteFile(teamMD, []byte("# 团队排期 v1\n## 本周\n- [ ] 任务A"), 0644)
	os.WriteFile(zsfMD, []byte("# 张三丰排期\n- 任务1"), 0644)
	os.WriteFile(fuleiMD, []byte("# 符坚排期\n- 任务2"), 0644)
	os.WriteFile(tanpingMD, []byte("# 唐盛排期\n- 任务3"), 0644)

	t.Setenv("WP_SCHEDULE_TEAM_FILE", teamMD)
	t.Setenv("WP_SCHEDULE_PERSONAL_DIR", td)

	db, ctx := newTestDB_R14P1(t)

	// team doc v1
	team, err := db.LatestScheduleDoc(ctx, "proj:default", "team", "")
	if err != nil {
		t.Fatalf("❌ LatestScheduleDoc(team) 报错: %v", err)
	}
	if team == nil {
		t.Fatal("❌ team schedule_doc v1 未导入")
	}
	if team.Version != 1 {
		t.Errorf("❌ team version=%d (want 1)", team.Version)
	}
	if !strings.Contains(team.Content, "任务A") {
		t.Errorf("❌ team content 不包含预期内容: %s", team.Content)
	}
	t.Logf("✅ team doc imported as v1, content=%d chars", len(team.Content))

	// personal docs v1
	for _, tc := range []struct{ owner, wantContent string }{
		{"zsf", "任务1"},
		{"fulei", "任务2"},
		{"tanping", "任务3"},
	} {
		doc, err := db.LatestScheduleDoc(ctx, "proj:default", "personal", tc.owner)
		if err != nil {
			t.Fatalf("❌ LatestScheduleDoc(personal, %s) 报错: %v", tc.owner, err)
		}
		if doc == nil {
			t.Errorf("❌ personal schedule_doc v1 for %s 未导入", tc.owner)
			continue
		}
		if doc.Version != 1 {
			t.Errorf("❌ %s version=%d (want 1)", tc.owner, doc.Version)
		}
		if !strings.Contains(doc.Content, tc.wantContent) {
			t.Errorf("❌ %s content 不包含 %q: %s", tc.owner, tc.wantContent, doc.Content)
		} else {
			t.Logf("✅ personal doc %s imported v1: %q", tc.owner, tc.wantContent)
		}
	}
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
		ID: "sched-1", OwnerKey: "zsf", Task: "旧任务A",
		StartDate: "07/01", EndDate: "07/07", Status: "进行中",
	}); err != nil {
		t.Fatalf("upsert schedule: %v", err)
	}
	if err := db.UpsertSchedule(ctx, model.Schedule{
		ID: "sched-2", OwnerKey: "zsf", Task: "旧任务B",
		StartDate: "07/08", EndDate: "07/14", Status: "计划中",
	}); err != nil {
		t.Fatalf("upsert schedule 2: %v", err)
	}

	// 模拟 appendPersonalDoc 行为
	items, err := db.ListSchedules(ctx, "zsf")
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("ListSchedules returned %d items (want >=2)", len(items))
	}
	t.Logf("✅ 旧 schedule 行保留: %d 条", len(items))

	// append personal schedule_doc
	doc1, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default", Kind: "personal", OwnerKey: "zsf",
		Content:   "# zsf\n- 07/01-07/07 旧任务A [进行中]\n- 07/08-07/14 旧任务B [计划中]",
		Source:    "nl", CreatedBy: "zsf",
	})
	if err != nil {
		t.Fatalf("append personal doc v1: %v", err)
	}
	t.Logf("✅ personal doc v%d appended (NL)", doc1.Version)

	// 再次 append（模拟下次 NL 变更）
	doc2, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:default", Kind: "personal", OwnerKey: "zsf",
		Content:   "# zsf\n- 07/01-07/07 旧任务A [已完成]\n- 07/08-07/14 旧任务B [进行中]\n- 07/15-07/21 新任务C [计划中]",
		Source:    "nl", CreatedBy: "zsf",
	})
	if err != nil {
		t.Fatalf("append personal doc v2: %v", err)
	}
	t.Logf("✅ personal doc v%d appended (第二次 NL)", doc2.Version)

	// 验证旧 schedule 行仍在
	itemsAfter, _ := db.ListSchedules(ctx, "zsf")
	if len(itemsAfter) < 2 {
		t.Errorf("❌ 旧 schedule 行丢失: %d 条", len(itemsAfter))
	} else {
		t.Logf("✅ 旧 schedule 行保留: %d 条", len(itemsAfter))
	}

	// 最新版应包含新任务
	latest, _ := db.LatestScheduleDoc(ctx, "proj:default", "personal", "zsf")
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
	versions, _ := db.ListScheduleDocVersions(ctx, "proj:default", "personal", "zsf")
	if len(versions) != 2 {
		t.Errorf("❌ 版本历史 %d 条 (want 2)", len(versions))
	} else {
		t.Logf("✅ 版本历史 %d 条，旧版本可追溯", len(versions))
	}
}

