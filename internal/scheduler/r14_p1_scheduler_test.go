package scheduler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func newTestDB(t *testing.T) (*store.SQLite, context.Context) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "sched-test.db"))
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
// ③ 多项目隔离（核心 DoD）
// ----------------------------------------------

func TestR14P1_MultiProjectIsolation(t *testing.T) {
	db, ctx := newTestDB(t)

	// 创建两个项目，各自不同的 notify_chat_id
	for _, p := range []model.Project{
		{ID: "proj:teamA", Name: "团队A", NotifyChatID: "oc_groupA123", Active: true},
		{ID: "proj:teamB", Name: "团队B", NotifyChatID: "oc_groupB456", Active: true},
	} {
		if err := db.UpsertProject(ctx, p); err != nil {
			t.Fatalf("upsert %s: %v", p.ID, err)
		}
	}

	// 模拟 #修改日程：分别写 team doc
	docA, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:teamA", Kind: "team", Content: "# 团队A排期\n- [ ] A任务",
		Source: "coordinate", CreatedBy: "test",
	})
	if err != nil {
		t.Fatalf("append teamA doc: %v", err)
	}
	t.Logf("✅ teamA doc v%d created", docA.Version)

	docB, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:teamB", Kind: "team", Content: "# 团队B排期\n- [ ] B任务",
		Source: "coordinate", CreatedBy: "test",
	})
	if err != nil {
		t.Fatalf("append teamB doc: %v", err)
	}
	t.Logf("✅ teamB doc v%d created", docB.Version)

	// 验证各写各自
	teamADoc, _ := db.LatestScheduleDoc(ctx, "proj:teamA", "team", "")
	teamBDoc, _ := db.LatestScheduleDoc(ctx, "proj:teamB", "team", "")
	if teamADoc == nil || !strings.Contains(teamADoc.Content, "A任务") {
		t.Error("❌ teamA doc 内容错")
	} else {
		t.Log("✅ teamA doc 内容正确: A任务")
	}
	if teamBDoc == nil || !strings.Contains(teamBDoc.Content, "B任务") {
		t.Error("❌ teamB doc 内容错")
	} else {
		t.Log("✅ teamB doc 内容正确: B任务")
	}

	// 验证互不串：teamA 不含 B，teamB 不含 A
	if strings.Contains(teamADoc.Content, "B任务") {
		t.Error("❌ 串库! teamA doc 包含 B 内容")
	}
	if strings.Contains(teamBDoc.Content, "A任务") {
		t.Error("❌ 串库! teamB doc 包含 A 内容")
	}
	t.Log("✅ 多项目隔离: 互不串")

	// version 各自递增
	docA2, _ := db.AppendScheduleDoc(ctx, model.ScheduleDoc{
		ProjectID: "proj:teamA", Kind: "team", Content: "# 团队A排期 v2",
		Source: "coordinate", CreatedBy: "test",
	})
	if docA2.Version != 2 {
		t.Errorf("❌ teamA 第二版 version=%d (want 2)", docA2.Version)
	} else {
		t.Logf("✅ teamA 第二版 version=%d", docA2.Version)
	}

	// teamB 不受影响
	teamBDocAfter, _ := db.LatestScheduleDoc(ctx, "proj:teamB", "team", "")
	if teamBDocAfter.Version != 1 {
		t.Errorf("❌ teamB version 被意外提升为 %d (want 1)", teamBDocAfter.Version)
	} else {
		t.Log("✅ teamB version 未受 teamA 写入影响")
	}

	// GetProjectByGroupChat
	p, err := db.GetProjectByGroupChat(ctx, "oc_groupA123")
	if err != nil {
		t.Fatalf("GetProjectByGroupChat: %v", err)
	}
	if p == nil || p.ID != "proj:teamA" {
		t.Errorf("❌ GetProjectByGroupChat(oc_groupA123) 期望 proj:teamA, got %v", p)
	} else {
		t.Log("✅ GetProjectByGroupChat(groupA123) → proj:teamA")
	}

	// NotifyProject: 验证发给各自群
	svc := &Service{Repo: db}
	// teamA notify
	if err := svc.NotifyProject(ctx, "proj:teamA", "A组排期已更新"); err != nil {
		t.Errorf("❌ NotifyProject(teamA) 报错: %v", err)
	} else {
		t.Log("✅ NotifyProject(teamA) 成功（发给 A 群）")
	}
	// teamB notify
	if err := svc.NotifyProject(ctx, "proj:teamB", "B组排期已更新"); err != nil {
		t.Errorf("❌ NotifyProject(teamB) 报错: %v", err)
	} else {
		t.Log("✅ NotifyProject(teamB) 成功（发给 B 群）")
	}
}

// ----------------------------------------------
// ⑤ validateTeamDoc 硬校验
// ----------------------------------------------

func TestR14P1_ValidateTeamDoc_HardValidation(t *testing.T) {
	prev := "# 团队排期\n\n```mermaid\ngraph TD\nA-->B\n```\n\n<!-- WP:KEEP:0 -->\n保护内容\n<!-- /WP:KEEP -->"

	// 1. 空内容 → 应拒绝
	if err := ValidateTeamDoc("", prev); err == nil {
		t.Error("❌ validateTeamDoc('') 应拒绝空内容")
	} else {
		t.Logf("✅ 空内容拒绝: %v", err)
	}

	// 2. 超短内容 (<20 runes) → 应拒绝
	if err := ValidateTeamDoc("短", prev); err == nil {
		t.Error("❌ validateTeamDoc('短') 应拒绝超短内容")
	} else {
		t.Logf("✅ 超短内容拒绝: %v", err)
	}

	// 3. mermaid 围栏奇数 → 应拒绝
	oddFence := "# 团队排期 v2\n\n```mermaid\ngraph TD\nA-->B\n```\n\n新内容足够长\n\n```\n奇数围栏\n<!-- WP:KEEP:0 -->\n保护内容\n<!-- /WP:KEEP -->"
	if err := ValidateTeamDoc(oddFence, prev); err == nil {
		t.Error("❌ 奇数 mermaid 围栏应拒绝")
	} else {
		t.Logf("✅ 奇数围栏拒绝: %v", err)
	}

	// 4. KEEP 块数减少 → 应拒绝
	noKeep := "# 团队排期 v2\n\n足够长的新内容没有保护块\n\n```mermaid\ngraph TD\nA-->B\n```"
	if err := ValidateTeamDoc(noKeep, prev); err == nil {
		t.Error("❌ KEEP 块数减少应拒绝")
	} else {
		t.Logf("✅ KEEP 减少拒绝: %v", err)
	}

	// 5. 正常内容 → 应通过
	good := "# 团队排期 v2\n\n足够长的新内容\n\n```mermaid\ngraph TD\nA-->B\n```\n\n<!-- WP:KEEP:0 -->\n保护内容\n<!-- /WP:KEEP -->"
	if err := ValidateTeamDoc(good, prev); err != nil {
		t.Errorf("❌ 正常内容应通过, 但被拒绝: %v", err)
	} else {
		t.Log("✅ 正常内容通过校验")
	}

	// 6. 非 UTF-8 序列 → 应拒绝
	if err := ValidateTeamDoc("# 团队\xfe\xfe排期 v2\n\n足够长的内容测试用\n\n```mermaid\ngraph TD\nA-->B\n```\n\n<!-- WP:KEEP:0 -->\n保护内容\n<!-- /WP:KEEP -->", prev); err == nil {
		t.Error("❌ 非 UTF-8 内容应拒绝")
	} else {
		t.Logf("✅ 非 UTF-8 拒绝: %v", err)
	}
}

// ----------------------------------------------
// ⑧ 默认项目回退（resolveProject）
// ----------------------------------------------

func TestR14P1_DefaultProjectFallback(t *testing.T) {
	db, ctx := newTestDB(t)
	_ = ctx

	svc := &Service{Repo: db}

	// 无群信息（个人消息）
	sourcePersonal := model.Message{ChatType: model.ChatPersonal, ChatEntityID: "ou_person123"}
	if pid := svc.ResolveProjectForTest(sourcePersonal); pid != "proj:default" {
		t.Errorf("❌ 个人消息应回退 proj:default, got %s", pid)
	} else {
		t.Log("✅ 个人消息 → proj:default")
	}

	// 群消息但 chat_id 不匹配任何 project
	sourceUnknown := model.Message{ChatType: model.ChatGroup, ChatEntityID: "unifiedrobot:group:oc_unknown999"}
	if pid := svc.ResolveProjectForTest(sourceUnknown); pid != "proj:default" {
		t.Errorf("❌ 未知群应回退 proj:default, got %s", pid)
	} else {
		t.Log("✅ 未知群消息 → proj:default")
	}

	// 匹配已知群
	if err := db.UpsertProject(ctx, model.Project{
		ID: "proj:known", Name: "已知项目", NotifyChatID: "oc_knownChat", Active: true,
	}); err != nil {
		t.Fatalf("upsert known: %v", err)
	}
	sourceKnown := model.Message{ChatType: model.ChatGroup, ChatEntityID: "unifiedrobot:group:oc_knownChat"}
	if pid := svc.ResolveProjectForTest(sourceKnown); pid != "proj:known" {
		t.Errorf("❌ 已知群应匹配 proj:known, got %s", pid)
	} else {
		t.Log("✅ 已知群消息 → proj:known")
	}

	// chat_id 为 "oc_" 前缀的直接匹配
	if err := db.UpsertProject(ctx, model.Project{
		ID: "proj:direct", Name: "直连", NotifyChatID: "oc_directChatID", Active: true,
	}); err != nil {
		t.Fatalf("upsert direct: %v", err)
	}
	sourceDirect := model.Message{ChatType: model.ChatGroup, ChatEntityID: "oc_directChatID"}
	if pid := svc.ResolveProjectForTest(sourceDirect); pid != "proj:direct" {
		t.Errorf("❌ oc_ 前缀直接匹配应到 proj:direct, got %s", pid)
	} else {
		t.Log("✅ oc_ 前缀直接匹配 → proj:direct")
	}
}
