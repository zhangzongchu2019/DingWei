package worker

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
	"github.com/zhangzongchu2019/dingwei/internal/coordination"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/schedule"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func newTestProcessor(t *testing.T) (*Processor, *store.SQLite, *feishu.Fake, context.Context) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	fake := &feishu.Fake{}
	clk := &clock.Fake{T: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)}
	db.SetClock(clk) // 让 store 的 GetPending 与 worker/schedule 共享同一假时钟
	p := &Processor{
		Inbound:  bus.NewDBQueue(db, model.DirectionIn),
		Outbound: bus.NewDBQueue(db, model.DirectionOut),
		Feishu:   fake,
		Schedule: schedule.New(db, clk, time.UTC),
		Repo:     db,
	}
	return p, db, fake, ctx
}

func enqueueInbound(t *testing.T, ctx context.Context, p *Processor, id, owner, text string) {
	t.Helper()
	if err := p.Inbound.Enqueue(ctx, model.Message{
		ID:           id,
		ChatEntityID: owner,
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      textContent(text),
	}); err != nil {
		t.Fatalf("enqueue %s: %v", id, err)
	}
}

func TestProcessorScheduleAddConfirmEndToEnd(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	enqueueInbound(t, ctx, p, "m1", owner, "+ 07/20-07/22 写方案")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process add = %v,%v", ok, err)
	}
	if list, _ := db.ListSchedules(ctx, owner); len(list) != 0 {
		t.Fatalf("schedule written before confirm: %+v", list)
	}
	if got := fake.SentMessages(); len(got) != 1 || !strings.Contains(got[0].Text, "回『确认』生效") {
		t.Fatalf("preview reply = %+v", got)
	}

	enqueueInbound(t, ctx, p, "m2", owner, "确认")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process confirm = %v,%v", ok, err)
	}
	list, _ := db.ListSchedules(ctx, owner)
	if len(list) != 1 || list[0].Task != "写方案" || list[0].StartDate != "2026-07-20" {
		t.Fatalf("schedule after confirm = %+v", list)
	}
	if got := fake.SentMessages(); len(got) != 2 || !strings.Contains(got[1].Text, "已生效") {
		t.Fatalf("confirm reply = %+v", got)
	}
}

func TestProcessorPrefixRouteTakesPrecedence(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"
	p.Prefix = fakePrefix{result: model.PrefixDispatchResult{Matched: true, Reply: "外部服务已处理"}}

	enqueueInbound(t, ctx, p, "m1", owner, "+ 07/20 不应写入")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process prefix = %v,%v", ok, err)
	}
	if list, _ := db.ListSchedules(ctx, owner); len(list) != 0 {
		t.Fatalf("prefix message fell through to schedule: %+v", list)
	}
	if got := fake.SentMessages(); len(got) != 1 || got[0].Text != "外部服务已处理" {
		t.Fatalf("prefix reply = %+v", got)
	}
}

func TestProcessorPrefixOfflineFailureIsAcked(t *testing.T) {
	p, _, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"
	p.Prefix = fakePrefix{result: model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：bot hello"}}

	enqueueInbound(t, ctx, p, "m1", owner, "bot hello")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process prefix offline = %v,%v", ok, err)
	}
	if got := fake.SentMessages(); len(got) != 1 || !strings.Contains(got[0].Text, "投递失败") {
		t.Fatalf("offline reply = %+v", got)
	}
	if msg, err := p.Inbound.Dequeue(ctx); err != nil || msg != nil {
		t.Fatalf("message was not acked, next=%+v err=%v", msg, err)
	}
}

func TestProcessorPrefixMissSilencesScheduleTriggers(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"
	p.Prefix = fakePrefix{}

	enqueueInbound(t, ctx, p, "m1", owner, "我改了代码，顺延一下测试计划")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process prefix miss = %v,%v", ok, err)
	}
	if got := fake.SentMessages(); len(got) != 0 {
		t.Fatalf("prefix miss should be silent, got replies=%+v", got)
	}
	if list, _ := db.ListSchedules(ctx, owner); len(list) != 0 {
		t.Fatalf("prefix miss triggered schedule: %+v", list)
	}
	if msg, err := p.Inbound.Dequeue(ctx); err != nil || msg != nil {
		t.Fatalf("message was not acked, next=%+v err=%v", msg, err)
	}
}

type fakePrefix struct {
	result model.PrefixDispatchResult
	err    error
}

func (f fakePrefix) Dispatch(context.Context, model.Message, string) (model.PrefixDispatchResult, error) {
	return f.result, f.err
}

func TestProcessorCancelDoesNotWriteSchedule(t *testing.T) {
	p, db, _, ctx := newTestProcessor(t)
	owner := "dev:personal:bob"

	enqueueInbound(t, ctx, p, "m1", owner, "+ 08/01 任务X")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", owner, "取消")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process cancel = %v,%v", ok, err)
	}
	if list, _ := db.ListSchedules(ctx, owner); len(list) != 0 {
		t.Fatalf("schedule written after cancel: %+v", list)
	}
}

func TestProcessorPendingOverrideKeepsLatestOnly(t *testing.T) {
	p, db, _, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	enqueueInbound(t, ctx, p, "m1", owner, "+ 07/01 第一版")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", owner, "+ 07/02 第二版")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m3", owner, "确认")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process confirm = %v,%v", ok, err)
	}
	list, _ := db.ListSchedules(ctx, owner)
	if len(list) != 1 || list[0].Task != "第二版" || list[0].StartDate != "2026-07-02" {
		t.Fatalf("pending override result = %+v", list)
	}
}

func TestProcessorReplaceAndPostponeEndToEnd(t *testing.T) {
	p, db, _, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	enqueueInbound(t, ctx, p, "m1", owner, "全量\n07/10-07/11 A\n07/20 B")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", owner, "确认")
	_, _ = p.ProcessOne(ctx)

	enqueueInbound(t, ctx, p, "m3", owner, "顺延 07/20 +3天")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m4", owner, "确认")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process postpone confirm = %v,%v", ok, err)
	}
	list, _ := db.ListSchedules(ctx, owner)
	if len(list) != 2 {
		t.Fatalf("replace/postpone count = %+v", list)
	}
	if list[0].Task != "A" || list[0].StartDate != "2026-07-10" {
		t.Fatalf("first schedule changed unexpectedly = %+v", list)
	}
	if list[1].Task != "B" || list[1].StartDate != "2026-07-23" || list[1].EndDate != "2026-07-23" {
		t.Fatalf("postponed schedule = %+v", list)
	}
}

func TestProcessorGroupOwnerUsesSender(t *testing.T) {
	p, db, _, ctx := newTestProcessor(t)
	msg := model.Message{
		ID:           "m1",
		ChatEntityID: "dev:group:chat1",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: "u1",
		Content:      textContent("+ 07/20 单日任务"),
	}
	if err := p.Inbound.Enqueue(ctx, msg); err != nil {
		t.Fatal(err)
	}
	_, _ = p.ProcessOne(ctx)
	msg.ID = "m2"
	msg.Content = textContent("确认")
	if err := p.Inbound.Enqueue(ctx, msg); err != nil {
		t.Fatal(err)
	}
	_, _ = p.ProcessOne(ctx)
	list, _ := db.ListSchedules(ctx, "dev:personal:u1")
	if len(list) != 1 || list[0].StartDate != "2026-07-20" || list[0].EndDate != "2026-07-20" {
		t.Fatalf("group sender schedule = %+v", list)
	}
}

func TestProcessorCollaboratorCannotModifyScheduleButCanReportProgress(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:c1"
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, DisplayName: "协作人", Role: model.RoleCollaborator, Active: true}); err != nil {
		t.Fatal(err)
	}

	enqueueInbound(t, ctx, p, "m1", owner, "+ 07/20 协作排期")
	_, _ = p.ProcessOne(ctx)
	if got := fake.SentMessages(); len(got) != 1 || !strings.Contains(got[0].Text, "权限不足") {
		t.Fatalf("collaborator schedule reply = %+v", got)
	}
	if list, _ := db.ListSchedules(ctx, owner); len(list) != 0 {
		t.Fatalf("collaborator schedule written: %+v", list)
	}

	enqueueInbound(t, ctx, p, "m2", owner, "进度 协作事项 已完成接口联调")
	_, _ = p.ProcessOne(ctx)
	progress, _ := db.LatestProgress(ctx, owner)
	if len(progress) != 1 || progress[0].TaskKey != "协作事项" || !strings.Contains(progress[0].Note, "接口联调") {
		t.Fatalf("collaborator progress = %+v", progress)
	}
}

func TestProcessorProgressLatestAndDone(t *testing.T) {
	p, db, _, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	enqueueInbound(t, ctx, p, "m1", owner, "+ 07/20 训练模型")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", owner, "确认")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m3", owner, "进度 训练 完成30%")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m4", owner, "进度 训练 完成60%")
	_, _ = p.ProcessOne(ctx)

	progress, _ := db.LatestProgress(ctx, owner)
	if len(progress) != 1 || progress[0].TaskKey != "训练模型" || progress[0].Note != "完成60%" {
		t.Fatalf("latest progress = %+v", progress)
	}

	enqueueInbound(t, ctx, p, "m5", owner, "完成 训练")
	_, _ = p.ProcessOne(ctx)
	list, _ := db.ListSchedules(ctx, owner)
	if len(list) != 1 || list[0].Status != "done" {
		t.Fatalf("done schedule = %+v", list)
	}
}

func TestProcessorAppealCancelsPendingAndNotifiesManagers(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"
	manager := "dev:personal:mgr"
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: manager, DisplayName: "Manager", Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal([]schedule.Change{{Label: "🆕", Action: "insert", New: model.Schedule{OwnerKey: owner, StartDate: "2026-07-20", EndDate: "2026-07-20", Task: "代改"}}})
	if _, err := db.PutPending(ctx, owner, string(payload), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	enqueueInbound(t, ctx, p, "appeal1", owner, "申诉 时间不合适")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process appeal = %v,%v", ok, err)
	}
	if pending, err := db.GetPending(ctx, owner); err != nil || pending != nil {
		t.Fatalf("pending after appeal=%+v err=%v", pending, err)
	}
	if got := fake.SentMessages(); len(got) == 0 || !strings.Contains(got[len(got)-1].Text, "已提交申诉") {
		t.Fatalf("appeal reply = %+v", got)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil || msg.ChatEntityID != manager || !strings.Contains(msg.Content, "时间不合适") {
		t.Fatalf("manager notice=%+v err=%v", msg, err)
	}
}

func TestProcessorChangeCoordinationSyncsFileAndNotifications(t *testing.T) {
	p, db, _, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"
	manager := "dev:personal:mgr"
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: owner, DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: manager, DisplayName: "Manager", Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "global.json")
	coord := coordination.New(db, p.Outbound, &clock.Fake{T: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)}, path)
	p.Coord = coord
	p.Schedule.Coordinator = coord

	enqueueInbound(t, ctx, p, "m1", owner, "+ 07/20 写方案")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", owner, "确认")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process confirm = %v,%v", ok, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "写方案") || !strings.Contains(string(data), "Alice") {
		t.Fatalf("global file after schedule = %s", data)
	}

	enqueueInbound(t, ctx, p, "m3", owner, "进度 方案 完成50%")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process progress = %v,%v", ok, err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "完成50%") {
		t.Fatalf("global file after progress = %s", data)
	}
	if n := queuedOutboundCount(t, db, ctx); n < 4 {
		t.Fatalf("expected owner+manager notifications for schedule and progress, got %d", n)
	}
}

func TestProcessorRiskReportGroupResolveAndQuery(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	enqueueInbound(t, ctx, p, "m1", owner, "风险 测试环境不稳定")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", owner, "风险 测试环境不稳定")
	_, _ = p.ProcessOne(ctx)
	risks, _ := db.ListOpenRisks(ctx, owner)
	if len(risks) != 1 || risks[0].Content != "测试环境不稳定" {
		t.Fatalf("grouped risks = %+v", risks)
	}

	enqueueInbound(t, ctx, p, "m3", owner, "我的风险")
	_, _ = p.ProcessOne(ctx)
	if got := fake.SentMessages(); len(got) < 3 || !strings.Contains(got[len(got)-1].Text, "测试环境不稳定") {
		t.Fatalf("risk query reply = %+v", got)
	}

	enqueueInbound(t, ctx, p, "m4", owner, "风险解除 环境")
	_, _ = p.ProcessOne(ctx)
	risks, _ = db.ListOpenRisks(ctx, owner)
	if len(risks) != 0 {
		t.Fatalf("resolved risks still open = %+v", risks)
	}
}

func TestProcessorManagerCanQueryTeamMemberCannot(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	manager := "dev:personal:mgr"
	member := "dev:personal:alice"
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: manager, DisplayName: "经理", Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: member, DisplayName: "Alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	enqueueInbound(t, ctx, p, "m1", member, "+ 07/20 写方案")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", member, "确认")
	_, _ = p.ProcessOne(ctx)

	enqueueInbound(t, ctx, p, "m3", member, "本周谁在做什么")
	_, _ = p.ProcessOne(ctx)
	if got := fake.SentMessages(); !strings.Contains(got[len(got)-1].Text, "权限不足") {
		t.Fatalf("member team query reply = %+v", got[len(got)-1])
	}

	enqueueInbound(t, ctx, p, "m4", manager, "本周谁在做什么")
	_, _ = p.ProcessOne(ctx)
	if got := fake.SentMessages(); !strings.Contains(got[len(got)-1].Text, "Alice") || !strings.Contains(got[len(got)-1].Text, "写方案") {
		t.Fatalf("manager team query reply = %+v", got[len(got)-1])
	}
}

func TestProcessorNoKeywordMatchRepliesAndAcks(t *testing.T) {
	p, _, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	enqueueInbound(t, ctx, p, "m1", owner, "- 不存在")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process no match = %v,%v", ok, err)
	}
	got := fake.SentMessages()
	if len(got) != 1 || !strings.Contains(got[0].Text, "未匹配到排期关键词") {
		t.Fatalf("no match reply = %+v", got)
	}
	if msg, err := p.Inbound.Dequeue(ctx); err != nil || msg != nil {
		t.Fatalf("message was not acked, next=%+v err=%v", msg, err)
	}
}

func TestProcessorAppeal_ExpiredRejected(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	// 直接写一条已过期的 pending（用假时钟当前时间 - 1 分钟作为过期时间）
	payload := `[{"label":"🆕","action":"insert","new":{"owner_key":"dev:personal:alice","start_date":"2026-07-20","end_date":"2026-07-20","task":"代改","status":"planned","priority":100}}]`
	expiredAt := time.Date(2026, 6, 29, 11, 59, 0, 0, time.UTC) // 假时钟是 12:00，这已过期
	if _, err := db.PutPending(ctx, owner, payload, expiredAt); err != nil {
		t.Fatal(err)
	}

	enqueueInbound(t, ctx, p, "appeal1", owner, "申诉 时间不合适")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process appeal expired = %v,%v", ok, err)
	}
	got := fake.SentMessages()
	last := got[len(got)-1].Text
	if !strings.Contains(last, "没有可申诉") {
		t.Fatalf("expired appeal reply = %q", last)
	}
	// 确认 pending 未被取消（仍为 pending，因为过期时 GetPending 返回 nil → handleAppeal 走 no-pending 分支）
	pending, err := db.GetPending(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if pending != nil {
		t.Fatalf("expired pending should be nil from GetPending: %+v", pending)
	}
}

func TestProcessorQueryMySchedule(t *testing.T) {
	p, db, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	// 先建立一条排期
	enqueueInbound(t, ctx, p, "m1", owner, "+ 07/20-07/22 写方案")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", owner, "确认")
	_, _ = p.ProcessOne(ctx)

	// 查询我的排期
	enqueueInbound(t, ctx, p, "m3", owner, "我的排期")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process query schedule = %v,%v", ok, err)
	}
	got := fake.SentMessages()
	last := got[len(got)-1].Text
	if !strings.Contains(last, "我的排期") || !strings.Contains(last, "写方案") || !strings.Contains(last, "2026-07-20") {
		t.Fatalf("schedule query reply = %q", last)
	}

	// 空排期
	_ = db.DeleteScheduleByID // already empty after delete? No, we need to test empty
	// 用另一个用户查
	enqueueInbound(t, ctx, p, "m4", "dev:personal:bob", "我的排期")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process empty schedule = %v,%v", ok, err)
	}
	got = fake.SentMessages()
	if !strings.Contains(got[len(got)-1].Text, "当前没有排期") {
		t.Fatalf("empty schedule reply = %q", got[len(got)-1].Text)
	}
}

func TestProcessorSendReply_NoOutboundDoesNotPanic(t *testing.T) {
	p, _, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	// 生产恒注入 Outbound，此分支为死代码；补覆盖以防未来重构误删
	p.Outbound = nil

	enqueueInbound(t, ctx, p, "m1", owner, "帮助")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process help sans outbound = %v,%v", ok, err)
	}
	got := fake.SentMessages()
	if len(got) != 1 || !strings.Contains(got[0].Text, "排期：") {
		t.Fatalf("help reply sans outbound = %+v", got)
	}
}

func TestProcessorQueryMyProgress(t *testing.T) {
	p, _, fake, ctx := newTestProcessor(t)
	owner := "dev:personal:alice"

	// 先建立排期并确认
	enqueueInbound(t, ctx, p, "m1", owner, "+ 07/20 训练模型")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m2", owner, "确认")
	_, _ = p.ProcessOne(ctx)

	// 提交进度（两条匹配到同一 schedule "训练模型"，LatestProgress 只保留最新一条）
	enqueueInbound(t, ctx, p, "m3", owner, "进度 训练 完成60%")
	_, _ = p.ProcessOne(ctx)
	enqueueInbound(t, ctx, p, "m4", owner, "进度 模型 接口联调完成")
	_, _ = p.ProcessOne(ctx)

	// 查询我的进度
	enqueueInbound(t, ctx, p, "m5", owner, "我的进度")
	if ok, err := p.ProcessOne(ctx); err != nil || !ok {
		t.Fatalf("process query progress = %v,%v", ok, err)
	}
	got := fake.SentMessages()
	last := got[len(got)-1].Text
	if !strings.Contains(last, "我的最新进度") || !strings.Contains(last, "接口联调完成") {
		t.Fatalf("progress query reply = %q", last)
	}

	// 空进度
	enqueueInbound(t, ctx, p, "m6", "dev:personal:bob", "我的进度")
	_, _ = p.ProcessOne(ctx)
	got = fake.SentMessages()
	if !strings.Contains(got[len(got)-1].Text, "当前没有进度记录") {
		t.Fatalf("empty progress reply = %q", got[len(got)-1].Text)
	}
}

func textContent(text string) string {
	b, _ := json.Marshal(map[string]string{"text": text})
	return string(b)
}

func queuedOutboundCount(t *testing.T, db *store.SQLite, ctx context.Context) int {
	t.Helper()
	count := 0
	for {
		msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
		if err != nil {
			t.Fatal(err)
		}
		if msg == nil {
			return count
		}
		count++
	}
}
