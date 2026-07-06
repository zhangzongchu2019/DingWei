package scheduler

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

type fakeRunner struct {
	out    string
	prompt string
}

func (f *fakeRunner) Run(_ context.Context, prompt string) (string, error) {
	f.prompt = prompt
	return f.out, nil
}

func TestCoordinateBacksUpAndWritesTeamSchedule(t *testing.T) {
	dir := t.TempDir()
	team := filepath.Join(dir, "AI-研究工作内容清单.md")
	personal := filepath.Join(dir, "工作计划-UserOne.md")
	if err := os.WriteFile(team, []byte("旧团队\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personal, []byte("个人计划\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{out: "新团队\n"}
	svc := New(Config{TeamFile: team, PersonalDir: dir, BackupDir: filepath.Join(dir, "backup")}, runner, &clock.Fake{T: time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)}, nil)
	path, err := svc.Coordinate(context.Background(), "新增任务")
	if err != nil {
		t.Fatal(err)
	}
	if path != team {
		t.Fatalf("path=%q", path)
	}
	data, _ := os.ReadFile(team)
	if string(data) != "新团队\n" {
		t.Fatalf("team file=%q", data)
	}
	backup, err := os.ReadFile(filepath.Join(dir, "backup", "20260702-080000", "AI-研究工作内容清单.md"))
	if err != nil || string(backup) != "旧团队\n" {
		t.Fatalf("backup=%q err=%v", backup, err)
	}
	if !strings.Contains(runner.prompt, "个人计划") || !strings.Contains(runner.prompt, "新增任务") {
		t.Fatalf("prompt missing context: %s", runner.prompt)
	}
	if !strings.Contains(runner.prompt, "不要新增时间戳") || !strings.Contains(runner.prompt, "保持原 Markdown 结构") {
		t.Fatalf("prompt missing format guard: %s", runner.prompt)
	}
}

func TestRunEvidenceWritesReportAndNotifiesGroup(t *testing.T) {
	dir := t.TempDir()
	team := filepath.Join(dir, "AI-研究工作内容清单.md")
	transcripts := filepath.Join(dir, "transcripts")
	if err := os.MkdirAll(transcripts, 0o750); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(team, []byte("团队计划\n"), 0o640)
	_ = os.WriteFile(filepath.Join(transcripts, "a.txt"), []byte("完成了模型评测\n"), 0o640)
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	runner := &fakeRunner{out: strings.Join([]string{
		"UserOne：已执行：模型评测",
		"UserTwo：无证据：销售单联调",
		"UserThree：计划外：补充数据核验",
		"主线：模型评测已推进，销售单仍需补证据。",
		"详细行1",
		"详细行2",
		"详细行3",
		"详细行4",
		"详细行5",
		"详细行6",
		"详细行7",
	}, "\n")}
	svc := New(Config{TeamFile: team, PersonalDir: dir, ReportDir: dir, TranscriptDirs: []string{transcripts}, NotifyChatID: "oc_group", NotifyBotID: "dev"}, runner, &clock.Fake{T: time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)}, outbound)
	path, err := svc.RunEvidence(ctx, "定时")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "佐证报告-20260702-120000.md") {
		t.Fatalf("report path=%s", path)
	}
	report, _ := os.ReadFile(path)
	if !strings.Contains(string(report), "销售单联调") || !strings.Contains(runner.prompt, "完成了模型评测") {
		t.Fatalf("report=%s prompt=%s", report, runner.prompt)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil || msg.ChatEntityID != "dev:group:oc_group" || msg.ChatType != model.ChatGroup || !strings.Contains(msg.Content, "完整佐证报告见") {
		t.Fatalf("outbound msg=%+v err=%v", msg, err)
	}
	if strings.Contains(msg.Content, "详细行7") {
		t.Fatalf("outbound should be summary, got full report: %s", msg.Content)
	}
}

func TestRunEvidenceUsesRuntimeNotifyConfigAndMemberSessions(t *testing.T) {
	dir := t.TempDir()
	team := filepath.Join(dir, "AI-研究工作内容清单.md")
	_ = os.WriteFile(team, []byte("团队计划\n"), 0o640)
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u2", DisplayName: "UserTwo", FeishuOpenID: "ou_u2", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-u2", SessionName: "u2", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-u2", "dev:personal:ou_u2"); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueMessage(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:ou_u2", Direction: model.DirectionIn, BotChannelID: "dev", ChatType: model.ChatPersonal, Content: `{"text":"完成销售单联调"}`}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertAppConfig(ctx, "schedule.notify_chat", `"oc_runtime"`); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertAppConfig(ctx, "schedule.notify_bot", `"runtimebot"`); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	runner := &fakeRunner{out: "UserTwo：已执行：销售单联调\n主线：销售单主链路有证据。"}
	svc := New(Config{TeamFile: team, PersonalDir: dir, ReportDir: dir, NotifyChatID: "oc_env", NotifyBotID: "envbot"}, runner, &clock.Fake{T: time.Date(2026, 7, 2, 13, 0, 0, 0, time.UTC)}, outbound)
	svc.Repo = db
	if _, err := svc.RunEvidence(ctx, "定时"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.prompt, "成员：UserTwo") || !strings.Contains(runner.prompt, "u2#FB-u2") || !strings.Contains(runner.prompt, "完成销售单联调") {
		t.Fatalf("prompt missing member evidence: %s", runner.prompt)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil || msg.ChatEntityID != "runtimebot:group:oc_runtime" {
		t.Fatalf("runtime notify msg=%+v err=%v", msg, err)
	}
}

func TestRecordSystemRequestStoresProgressWithoutLLM(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{
		OwnerKey:     "u2",
		DisplayName:  "UserTwo",
		FeishuOpenID: "ou_u2",
		Role:         model.RoleMember,
		Active:       true,
	}); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{out: "不应调用"}
	svc := New(Config{TeamFile: filepath.Join(dir, "team.md"), PersonalDir: dir}, runner, &clock.Fake{T: time.Date(2026, 7, 2, 14, 0, 0, 0, time.UTC)}, nil)
	svc.Repo = db
	reply, err := svc.HandleSystemRequest(ctx, "scheduler", "record", "完成销售单联调", model.Message{
		ChatEntityID: "dev:group:oc_team",
		SenderOpenID: "ou_u2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "已记录进度上报") {
		t.Fatalf("reply=%q", reply)
	}
	if runner.prompt != "" {
		t.Fatalf("record action called LLM runner: %s", runner.prompt)
	}
	progress, err := db.LatestProgress(ctx, "u2")
	if err != nil || len(progress) != 1 {
		t.Fatalf("progress=%+v err=%v", progress, err)
	}
	if progress[0].TaskKey != "自然语言进度上报" || progress[0].Note != "完成销售单联调" || progress[0].Source != "feishu" {
		t.Fatalf("progress row=%+v", progress[0])
	}
}

func TestEvidenceCronConfigUsesRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := New(Config{}, &fakeRunner{}, &clock.Fake{T: time.Date(2026, 7, 2, 14, 0, 0, 0, time.UTC)}, nil)
	svc.Repo = db
	spec, tz := svc.EvidenceCronConfig(ctx)
	if spec != "0 0,6 * * *" || tz != "UTC" {
		t.Fatalf("default cron=%q tz=%q", spec, tz)
	}
	if err := db.UpsertAppConfig(ctx, "schedule.evidence_cron", `"5 18 * * *"`); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertAppConfig(ctx, "schedule.evidence_tz", `"UTC"`); err != nil {
		t.Fatal(err)
	}
	spec, tz = svc.EvidenceCronConfig(ctx)
	if spec != "5 18 * * *" || tz != "UTC" {
		t.Fatalf("runtime cron=%q tz=%q", spec, tz)
	}
}

func TestNotifyCronConfigUsesRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := New(Config{}, &fakeRunner{}, &clock.Fake{T: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)}, nil)
	svc.Repo = db
	group, tz := svc.GroupNotifyCronConfig(ctx)
	if group != "0 0" || tz != "UTC" {
		t.Fatalf("default group cron=%q tz=%q", group, tz)
	}
	personal, tz := svc.PersonalNotifyCronConfig(ctx)
	if personal != "0 0,6" || tz != "UTC" {
		t.Fatalf("default personal cron=%q tz=%q", personal, tz)
	}
	if err := db.UpsertAppConfig(ctx, "notify.group_cron", `"15 1"`); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertAppConfig(ctx, "notify.personal_cron", `"5 0,6"`); err != nil {
		t.Fatal(err)
	}
	group, _ = svc.GroupNotifyCronConfig(ctx)
	personal, _ = svc.PersonalNotifyCronConfig(ctx)
	if group != "15 1" || personal != "5 0,6" {
		t.Fatalf("runtime notify cron group=%q personal=%q", group, personal)
	}
}

func TestRunGroupNotificationsSkipsProjectEvidenceOverrides(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	projects := []model.Project{
		{ID: "proj:daily", Name: "日更项目", NotifyChatID: "oc_daily", NotifyBotID: "dev", Active: true},
		{ID: "proj:weekly", Name: "覆盖项目", NotifyChatID: "oc_weekly", NotifyBotID: "dev", EvidenceCron: "0 2 * * 1,3", Active: true},
	}
	for _, project := range projects {
		if err := db.UpsertProject(ctx, project); err != nil {
			t.Fatal(err)
		}
		if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{ProjectID: project.ID, Kind: "team", Content: "团队排期\n", Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	svc := New(Config{ReportDir: dir, NotifyBotID: "dev"}, &fakeRunner{out: "主线：正常推进。"}, &clock.Fake{T: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)}, outbound)
	svc.Repo = db
	if err := svc.RunGroupNotifications(ctx, "定时群通知"); err != nil {
		t.Fatal(err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("group msg=%+v err=%v", msg, err)
	}
	if msg.ChatEntityID != "dev:group:oc_daily" || msg.ChatType != model.ChatGroup {
		t.Fatalf("group msg target=%+v", msg)
	}
	next, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Fatalf("project evidence override should not be sent by group cron: %+v", next)
	}
}

func TestResolveNotifyTargetInheritsParentAndStopsCycles(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:parent", Name: "父项目", NotifyChatID: "oc_parent", NotifyBotID: "parentbot", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:child", Name: "子项目", ParentID: "proj:parent", NotifyBotID: "childbot", Active: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(Config{NotifyChatID: "oc_global", NotifyBotID: "globalbot"}, &fakeRunner{}, &clock.Fake{T: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)}, nil)
	svc.Repo = db
	target, err := svc.ResolveNotifyTarget(ctx, "proj:child")
	if err != nil {
		t.Fatal(err)
	}
	if target.ChatID != "oc_parent" || target.BotID != "parentbot" || target.Source != "proj:parent" {
		t.Fatalf("inherited target=%+v", target)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:a", Name: "A", ParentID: "proj:b", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:b", Name: "B", ParentID: "proj:a", Active: true}); err != nil {
		t.Fatal(err)
	}
	target, err = svc.ResolveNotifyTarget(ctx, "proj:a")
	if err != nil {
		t.Fatal(err)
	}
	if target.ChatID != "oc_global" || target.BotID != "globalbot" || target.Source != "global" {
		t.Fatalf("cycle fallback target=%+v", target)
	}
}

func TestNotifyProjectUsesInheritedParentChat(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:parent", Name: "父项目", NotifyChatID: "oc_parent", NotifyBotID: "parentbot", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertProject(ctx, model.Project{ID: "proj:child", Name: "子项目", ParentID: "proj:parent", Active: true}); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	svc := New(Config{}, &fakeRunner{}, &clock.Fake{T: time.Date(2026, 7, 4, 1, 0, 0, 0, time.UTC)}, outbound)
	svc.Repo = db
	if err := svc.NotifyProject(ctx, "proj:child", "继承通知"); err != nil {
		t.Fatal(err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("notify msg=%+v err=%v", msg, err)
	}
	if msg.ChatEntityID != "parentbot:group:oc_parent" {
		t.Fatalf("inherited notify msg=%+v", msg)
	}
}

func TestRunAggregateProjectBuildsNestedSummaryAndNotifies(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.Project{
		{ID: "proj:agg", Name: "总聚合", NotifyChatID: "oc_agg", NotifyBotID: "bot-test", EvidenceCron: "0 2 * * 1,3", Active: true},
		{ID: "proj:nested", Name: "嵌套聚合", Active: true},
		{ID: "proj:a", Name: "项目A", Active: true},
		{ID: "proj:b", Name: "项目B", Active: true},
	} {
		if err := db.UpsertProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	dirtyA := `# A

| 模块 | 状态 | 链接 |
| --- | --- | --- |
| 接口 | 完成 | https://open.feishu.cn/sheets/very/long/url |
<br>
\- A进展：完成接口联调，关键结论是本周可进入灰度。
排期看板:task-a, 2026-07-01, 2026-07-10
属性M5： f12, 2026-07-27, 2026-08-02
验收: active, a1, 2026-08-03, 3d
发布: milestone, m1, 2026-08-08, 0d
for AI
无需显卡支持
u1 8月盘子拥堵，需收敛并行任务。
[完整飞书文档](https://example.com/very/long/doc/url)
`
	if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{ProjectID: "proj:a", Kind: "team", Content: dirtyA, Source: "test"}); err != nil {
		t.Fatal(err)
	}
	dirtyB := `# B

gantt
dateFormat  YYYY-MM-DD
section 开发
B灰度:id1, 2026-07-02, 2026-07-11
属性M5： f12, 2026-07-27, 2026-08-02
修复: done, b1, 2026-08-01, 2d
上线: crit, b2, 2026-08-04, 1d
| 表格碎片
🟠 B进展：推进灰度，风险是回滚窗口待确认。<br>下周目标：完成验收。
u1 8月盘子拥堵，需收敛并行任务。
`
	if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{ProjectID: "proj:b", Kind: "team", Content: dirtyB, Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetProjectAggregateSources(ctx, "proj:nested", []string{"proj:b"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetProjectAggregateSources(ctx, "proj:agg", []string{"proj:a", "proj:nested"}); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	svc := New(Config{}, &fakeRunner{out: "should not call runner"}, &clock.Fake{T: time.Date(2026, 7, 4, 2, 0, 0, 0, time.UTC)}, outbound)
	svc.Repo = db
	summary, err := svc.RunEvidenceProject(ctx, "proj:agg", "聚合定时")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"总聚合", "项目A", "A进展", "嵌套聚合", "项目B", "B进展"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
	for _, bad := range []string{"|", "<br", "\\", "task-a,", "id1,", "https://", "[完整飞书文档]", "# A", "##", "属性M5", "active", "done", "crit", "milestone", "0d", "for AI", "无需显卡支持"} {
		if strings.Contains(summary, bad) {
			t.Fatalf("aggregate summary should be clean, found %q:\n%s", bad, summary)
		}
	}
	if strings.Count(summary, "u1 8月盘子拥堵") != 1 {
		t.Fatalf("duplicate cross-source mainline should appear once:\n%s", summary)
	}
	if lines := strings.Count(strings.TrimSpace(summary), "\n") + 1; lines > 10 {
		t.Fatalf("aggregate summary should stay around 10 lines, got %d:\n%s", lines, summary)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("aggregate msg=%+v err=%v", msg, err)
	}
	if msg.ChatEntityID != "bot-test:group:oc_agg" || !strings.Contains(msg.Content, "A进展") || !strings.Contains(msg.Content, "B进展") {
		t.Fatalf("aggregate outbound=%+v", msg)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
		t.Fatalf("aggregate content json: %v content=%s", err, msg.Content)
	}
	outboundText := payload["text"]
	for _, bad := range []string{"|", "<br", "\\", "https://", "task-a,", "id1,", "属性M5", "active", "done", "crit", "milestone", "0d", "for AI", "无需显卡支持"} {
		if strings.Contains(outboundText, bad) {
			t.Fatalf("aggregate outbound should be clean, found %q: %s", bad, outboundText)
		}
	}
	if strings.Count(outboundText, "u1 8月盘子拥堵") != 1 {
		t.Fatalf("duplicate outbound mainline should appear once: %s", outboundText)
	}
}

func TestRunWeeklyProjectReportsPersistsNonAggregateOwnerProjects(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seeded, err := db.ListProjects(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range seeded {
		project.Active = false
		if err := db.UpsertProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 7, 4, 2, 0, 0, 0, time.UTC)
	for _, p := range []model.Project{
		{ID: "proj:weekly", Name: "周报项目", OwnerKey: "alice", Active: true},
		{ID: "proj:placeholder", Name: "占位项目", Active: true},
		{ID: "proj:aggregate", Name: "聚合项目", OwnerKey: "manager", Active: true},
	} {
		if err := db.UpsertProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetProjectAggregateSources(ctx, "proj:aggregate", []string{"proj:weekly"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{ProjectID: "proj:weekly", Kind: "team", Content: "# 团队排期\n\n推进 M1。", Source: "seed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{ProjectID: "proj:weekly", Kind: "personal", OwnerKey: "alice", Content: "# Alice\n\n完成 M1 联调。", Source: "seed"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddProgress(ctx, model.Progress{OwnerKey: "alice", TaskKey: "M1", Note: "完成网关联调", Percent: 100, ReportedAt: now.Add(-24 * time.Hour), Source: "feishu"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAIEvidence(ctx, model.AIEvidence{OwnerKey: "bob", WorkItem: "阅读联调日志", Artifact: "trace.log", OccurredAt: now.Add(-12 * time.Hour), Confidence: 0.8}); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{out: "周报项目周报\n- 完成网关联调\n- AI交互仅作为佐证。"}
	svc := New(Config{}, runner, &clock.Fake{T: now}, nil)
	svc.Repo = db
	reports, err := svc.RunWeeklyProjectReports(ctx, "单测")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].ProjectID != "proj:weekly" || reports[0].Week != "2026-06-29" {
		t.Fatalf("weekly reports=%+v", reports)
	}
	if !strings.Contains(runner.prompt, "本周负责人进度提交（权威主干）") || !strings.Contains(runner.prompt, "完成网关联调") {
		t.Fatalf("prompt missing progress authority: %s", runner.prompt)
	}
	if !strings.Contains(runner.prompt, "AI交互佐证（仅印证与补全，不得当完成事项）") || !strings.Contains(runner.prompt, "阅读联调日志") {
		t.Fatalf("prompt missing evidence boundary: %s", runner.prompt)
	}
	stored, err := db.GetProjectWeeklyReport(ctx, "proj:weekly", "2026-06-29")
	if err != nil || stored == nil || !strings.Contains(stored.Content, "周报项目周报") {
		t.Fatalf("stored report=%+v err=%v", stored, err)
	}
	if skipped, err := db.GetProjectWeeklyReport(ctx, "proj:aggregate", "2026-06-29"); err != nil || skipped != nil {
		t.Fatalf("aggregate should be skipped report=%+v err=%v", skipped, err)
	}
}

func TestAggregateWeeklyDraftReviewAndApprovePublishes(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seeded, err := db.ListProjects(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range seeded {
		project.Active = false
		if err := db.UpsertProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "manager", DisplayName: "负责人", FeishuOpenID: "ou_manager", Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.Project{
		{ID: "proj:aggregate", Name: "聚合项目", OwnerKey: "manager", NotifyChatID: "oc_aggregate", NotifyBotID: "reviewbot", Active: true},
		{ID: "proj:source-a", Name: "来源A", OwnerKey: "alice", Active: true},
		{ID: "proj:source-b", Name: "来源B", OwnerKey: "bob", Active: true},
	} {
		if err := db.UpsertProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetProjectAggregateSources(ctx, "proj:aggregate", []string{"proj:source-a", "proj:source-b"}); err != nil {
		t.Fatal(err)
	}
	week := "2026-06-29"
	for _, report := range []model.ProjectWeeklyReport{
		{ProjectID: "proj:source-a", Week: week, Content: "来源A完成网关联调", Status: "final", CreatedAt: time.Date(2026, 7, 4, 2, 0, 0, 0, time.UTC)},
		{ProjectID: "proj:source-b", Week: week, Content: "来源B完成评测闭环", Status: "final", CreatedAt: time.Date(2026, 7, 4, 2, 0, 0, 0, time.UTC)},
	} {
		if err := db.UpsertProjectWeeklyReport(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	runner := &fakeRunner{out: "聚合周报草稿\n- 来源A完成网关联调\n- 来源B完成评测闭环"}
	clk := &clock.Fake{T: time.Date(2026, 7, 5, 22, 0, 0, 0, time.UTC)}
	svc := New(Config{}, runner, clk, outbound)
	svc.Repo = db
	reports, err := svc.RunAggregateWeeklyDrafts(ctx, "单测草稿")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].ProjectID != "proj:aggregate" || reports[0].Status != "draft" || reports[0].Week != week {
		t.Fatalf("aggregate reports=%+v", reports)
	}
	if !strings.Contains(runner.prompt, "来源A完成网关联调") || !strings.Contains(runner.prompt, "审阅草稿") {
		t.Fatalf("aggregate prompt missing sources/review guard: %s", runner.prompt)
	}
	dm, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || dm == nil {
		t.Fatalf("review dm=%+v err=%v", dm, err)
	}
	if dm.ChatEntityID != "reviewbot:personal:ou_manager" || !strings.Contains(dm.Content, "批准发送") || !strings.Contains(dm.Content, "不要发送") {
		t.Fatalf("review dm target/content=%+v", dm)
	}
	if next, err := db.ClaimNextMessage(ctx, model.DirectionOut); err != nil || next != nil {
		t.Fatalf("draft should not publish group before approval next=%+v err=%v", next, err)
	}
	reply, err := svc.HandleSystemRequest(ctx, "scheduler", "record", "批准发送", model.Message{
		ChatEntityID: "reviewbot:personal:ou_manager",
		BotChannelID: "reviewbot",
		ChatType:     model.ChatPersonal,
		SenderOpenID: "ou_manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "已批准并发送") {
		t.Fatalf("approve reply=%q", reply)
	}
	group, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || group == nil {
		t.Fatalf("published group=%+v err=%v", group, err)
	}
	if group.ChatEntityID != "reviewbot:group:oc_aggregate" || group.ChatType != model.ChatGroup || !strings.Contains(group.Content, "聚合周报草稿") {
		t.Fatalf("group publish=%+v", group)
	}
	stored, err := db.GetProjectWeeklyReport(ctx, "proj:aggregate", week)
	if err != nil || stored == nil || stored.Status != "published" || stored.PublishedAt == nil || stored.ApprovedAt == nil {
		t.Fatalf("stored aggregate=%+v err=%v", stored, err)
	}
}

func TestRunPersonalRemindersSendsDMsAtFakeTimes(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, member := range []model.Member{
		{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true},
		{OwnerKey: "bob", DisplayName: "Bob", FeishuOpenID: "ou_bob", Role: model.RoleMember, Active: true},
	} {
		if err := db.UpsertMember(ctx, member); err != nil {
			t.Fatal(err)
		}
		if err := db.AssignProjectMember(ctx, "proj:default", member.OwnerKey); err != nil {
			t.Fatal(err)
		}
		if _, err := db.AppendScheduleDoc(ctx, model.ScheduleDoc{ProjectID: "proj:default", Kind: "personal", OwnerKey: member.OwnerKey, Content: member.DisplayName + " 今日待办\n- 推进任务", Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertAppConfig(ctx, "schedule.notify_bot", `"dev"`); err != nil {
		t.Fatal(err)
	}
	clk := &clock.Fake{T: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	svc := New(Config{NotifyBotID: "fallback"}, &fakeRunner{}, clk, outbound)
	svc.Repo = db
	if err := svc.RunPersonalReminders(ctx, "定时个人提醒"); err != nil {
		t.Fatal(err)
	}
	clk.Advance(6 * time.Hour)
	if err := svc.RunPersonalReminders(ctx, "定时个人提醒"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for i := 0; i < 4; i++ {
		msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
		if err != nil || msg == nil {
			t.Fatalf("personal msg #%d=%+v err=%v", i, msg, err)
		}
		if msg.ChatType != model.ChatPersonal || !strings.HasPrefix(msg.ChatEntityID, "dev:personal:ou_") {
			t.Fatalf("personal target=%+v", msg)
		}
		if !strings.Contains(msg.Content, "个人排期摘要") || !strings.Contains(msg.Content, "待办提醒") {
			t.Fatalf("personal content=%s", msg.Content)
		}
		seen[msg.ChatEntityID]++
	}
	if seen["dev:personal:ou_alice"] != 2 || seen["dev:personal:ou_bob"] != 2 {
		t.Fatalf("personal DM counts=%v", seen)
	}
	next, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil {
		t.Fatal(err)
	}
	if next != nil {
		t.Fatalf("unexpected extra personal msg=%+v", next)
	}
}

func TestEvidenceSnapshotIncludesCollectMessagesAndSkipsOptOut(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "bob", DisplayName: "Bob", FeishuOpenID: "ou_bob", Role: model.RoleMember, EvidenceOptOut: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-alice", "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-bob", "dev:personal:ou_bob"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 2, 15, 0, 0, 0, time.UTC)
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-alice", SessionName: "home", LastSeenAt: now, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-bob", SessionName: "bobhome", LastSeenAt: now, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "collect-alice",
		ChatEntityID: "home#FB-alice",
		Direction:    model.DirectionCollect,
		BotChannelID: "sessionhelper",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"完成自主任务","role":"claude","session":"home","collect":true}`,
		Status:       "done",
		CreatedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "collect-bob",
		ChatEntityID: "bobhome#FB-bob",
		Direction:    model.DirectionCollect,
		BotChannelID: "sessionhelper",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"不应进入佐证","role":"claude","session":"bobhome","collect":true}`,
		Status:       "done",
		CreatedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(Config{TeamFile: filepath.Join(dir, "team.md"), PersonalDir: dir}, &fakeRunner{}, &clock.Fake{T: now}, nil)
	svc.Repo = db
	snapshot := svc.evidenceSnapshot(ctx)
	if !strings.Contains(snapshot, "完成自主任务") || !strings.Contains(snapshot, "home/claude") {
		t.Fatalf("snapshot missing collect message: %s", snapshot)
	}
	if strings.Contains(snapshot, "不应进入佐证") || strings.Contains(snapshot, "Bob") {
		t.Fatalf("snapshot included opt-out member: %s", snapshot)
	}
}

func TestEvidenceSnapshotCleansExpiredCollectMessages(t *testing.T) {
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "wp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 2, 15, 0, 0, 0, time.UTC)
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-alice", "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-alice", SessionName: "home", LastSeenAt: now, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "collect-old",
		ChatEntityID: "home#FB-alice",
		Direction:    model.DirectionCollect,
		BotChannelID: "sessionhelper",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"过期采集","role":"claude","session":"home","collect":true}`,
		Status:       "done",
		CreatedAt:    now.AddDate(0, 0, -3),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertAppConfig(ctx, "collect.retain_days", `1`); err != nil {
		t.Fatal(err)
	}
	svc := New(Config{TeamFile: filepath.Join(dir, "team.md"), PersonalDir: dir}, &fakeRunner{}, &clock.Fake{T: now}, nil)
	svc.Repo = db
	snapshot := svc.evidenceSnapshot(ctx)
	if strings.Contains(snapshot, "过期采集") {
		t.Fatalf("snapshot included expired collect: %s", snapshot)
	}
	msgs, err := db.RecentMessages(ctx, model.MessageFilter{ChatEntityID: "home#FB-alice", Limit: 10})
	if err != nil || len(msgs) != 0 {
		t.Fatalf("expired collect not cleaned msgs=%+v err=%v", msgs, err)
	}
}
