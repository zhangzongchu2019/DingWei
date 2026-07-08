package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/scheduler"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const testSecOpsAdminOpenID = "ou_testadmin0000000000"

func newTestHub(t *testing.T) (*Hub, *store.SQLite, context.Context) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "m8.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return New(db), db, ctx
}

func useTestSecOpsAdminOpenID(t *testing.T) {
	t.Helper()
	old := secOpsAdminOpenID
	secOpsAdminOpenID = testSecOpsAdminOpenID
	t.Cleanup(func() {
		secOpsAdminOpenID = old
	})
}

func useTestSecOpsAdminOwnerKey(t *testing.T, ownerKey string) {
	t.Helper()
	old := secOpsAdminOwnerKey
	secOpsAdminOwnerKey = ownerKey
	t.Cleanup(func() {
		secOpsAdminOwnerKey = old
	})
}

func TestControlPlaneP1DispatchPersistsL1DoneAndLLMPending(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	rosterMsg := model.Message{ID: "ct-roster", ChatEntityID: "dev:personal:ou_u1", BotChannelID: "dev", ChatType: model.ChatPersonal}
	result, err := hub.Dispatch(ctx, rosterMsg, "#roster")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "DingWei在线清单") {
		t.Fatalf("roster result=%+v", result)
	}
	task, err := db.GetControlTask(ctx, "ct-roster")
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.Status != "done" || task.Intent != "command.roster" || task.Layer != "L1" {
		t.Fatalf("roster task=%+v", task)
	}

	unknownMsg := model.Message{ID: "ct-unknown", ChatEntityID: "dev:personal:ou_u1", BotChannelID: "dev", ChatType: model.ChatPersonal}
	result, err = hub.Dispatch(ctx, unknownMsg, "请帮我判断该找谁")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "已受理 #ct-unknown") {
		t.Fatalf("unknown should ack, got %+v", result)
	}
	task, err = db.GetControlTask(ctx, "ct-unknown")
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.Status != "llm_pending" || task.Intent != "unknown" || task.Layer != "L1" {
		t.Fatalf("unknown task=%+v", task)
	}

	dup, err := hub.Dispatch(ctx, unknownMsg, "请帮我判断该找谁")
	if err != nil {
		t.Fatal(err)
	}
	if !dup.Matched || dup.Reply != "已受理 #ct-unknown" {
		t.Fatalf("duplicate in-flight should ack without re-dispatch, got %+v", dup)
	}

	expireAt := time.Now().UTC().Add(-time.Minute)
	if _, _, err := db.EnqueueControlTask(ctx, model.ControlTask{
		ID:           "ct-expired",
		Source:       "feishu",
		SourceAddr:   feishuAddress("ou_u1", "dev", "UnifiedRobot"),
		OwnerKey:     "u1",
		BotChannelID: "dev",
		RawInput:     "stale",
		Status:       "queued",
		ExpireAt:     &expireAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Dispatch(ctx, model.Message{ID: "ct-trigger", ChatEntityID: "dev:personal:ou_u1", BotChannelID: "dev", ChatType: model.ChatPersonal}, "#roster"); err != nil {
		t.Fatal(err)
	}
	out := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "任务 #ct-expired 已超时") {
		t.Fatalf("expired notification=%+v", out)
	}

	if _, _, err := db.EnqueueControlTask(ctx, model.ControlTask{
		ID:           "ct-failed",
		Source:       "feishu",
		SourceAddr:   feishuAddress("ou_u1", "dev", "UnifiedRobot"),
		OwnerKey:     "u1",
		BotChannelID: "dev",
		RawInput:     "fail",
		Status:       "queued",
		MaxAttempts:  1,
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := db.RetryControlTask(ctx, "ct-failed", "boom")
	if err != nil {
		t.Fatal(err)
	}
	if failed == nil || failed.Status != "failed" {
		t.Fatalf("failed task=%+v", failed)
	}
	if err := hub.notifyControlTask(ctx, *failed, controlTaskFailedReply(*failed)); err != nil {
		t.Fatal(err)
	}
	out = waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "任务 #ct-failed 处理失败：boom") {
		t.Fatalf("failed notification=%+v", out)
	}
}

func TestControlPlaneP2LeaseClaimIsExclusiveAndReclaimsExpired(t *testing.T) {
	_, db, ctx := newTestHub(t)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"l2-a", "l2-b"} {
		if _, _, err := db.EnqueueControlTask(ctx, model.ControlTask{
			ID:         id,
			Source:     "feishu",
			SourceAddr: "ou_1#dev#UnifiedRobot",
			OwnerKey:   "u1",
			RawInput:   id,
			Status:     "llm_pending",
			CreatedAt:  now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := db.ClaimNextL2ControlTask(ctx, "w1", now.Add(time.Minute), now)
	if err != nil || first == nil || first.ID != "l2-a" {
		t.Fatalf("first claim=%+v err=%v", first, err)
	}
	second, err := db.ClaimNextL2ControlTask(ctx, "w2", now.Add(time.Minute), now)
	if err != nil || second == nil || second.ID != "l2-b" {
		t.Fatalf("second claim=%+v err=%v", second, err)
	}
	none, err := db.ClaimNextL2ControlTask(ctx, "w3", now.Add(time.Minute), now)
	if err != nil || none != nil {
		t.Fatalf("third claim=%+v err=%v", none, err)
	}
	reclaimed, err := db.ClaimNextL2ControlTask(ctx, "w4", now.Add(2*time.Minute), now.Add(2*time.Minute))
	if err != nil || reclaimed == nil || reclaimed.ID != "l2-a" || reclaimed.LeaseOwner != "w4" {
		t.Fatalf("reclaimed=%+v err=%v", reclaimed, err)
	}
}

func TestControlPlaneP2DispatchAndClarifyWithMockLLM(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dev := dialSession(t, ctx, srv.URL, "developer", key.ID, secret)
	defer dev.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, key.ID, "developer")

	hub.L2 = &fakeTriageProvider{out: `{"intent":"dispatch","targets":[{"session":"developer","instruction":"请处理这个任务"}],"confidence":0.95}`}
	task := model.ControlTask{ID: "l2-dispatch", Source: "feishu", SourceAddr: feishuAddress("ou_u1", "dev", "UnifiedRobot"), OwnerKey: "u1", BotChannelID: "dev", RawInput: "帮我处理", Status: "llm_pending"}
	if _, _, err := db.EnqueueControlTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	hub.processL2Task(ctx, task, hub.effectiveL2Config())
	got := readEnvelope(t, ctx, dev)
	if got.Body != "请处理这个任务" || got.Meta["control_task_id"] != "l2-dispatch" {
		t.Fatalf("dispatch envelope=%+v", got)
	}
	out := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "已分诊给 #developer") {
		t.Fatalf("dispatch source reply=%+v", out)
	}
	stored, err := db.GetControlTask(ctx, "l2-dispatch")
	if err != nil || stored == nil || stored.Status != "done" || stored.Layer != "L2" || stored.Intent != "dispatch" {
		t.Fatalf("stored dispatch task=%+v err=%v", stored, err)
	}

	hub.L2 = &fakeTriageProvider{out: `{"intent":"dispatch","targets":[{"session":"developer","instruction":"低置信"}],"confidence":0.2}`}
	clarifyTask := model.ControlTask{ID: "l2-clarify", Source: "feishu", SourceAddr: feishuAddress("ou_u1", "dev", "UnifiedRobot"), OwnerKey: "u1", BotChannelID: "dev", RawInput: "不明确", Status: "llm_pending"}
	if _, _, err := db.EnqueueControlTask(ctx, clarifyTask); err != nil {
		t.Fatal(err)
	}
	hub.processL2Task(ctx, clarifyTask, hub.effectiveL2Config())
	out = waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "不确定该派给谁") {
		t.Fatalf("clarify source reply=%+v", out)
	}
	stored, err = db.GetControlTask(ctx, "l2-clarify")
	if err != nil || stored == nil || stored.Status != "done" || stored.Intent != "clarify" {
		t.Fatalf("stored clarify task=%+v err=%v", stored, err)
	}
}

func TestControlPlaneP2ProviderFailureRetriesAndFails(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.L2 = &fakeTriageProvider{err: errors.New("llm down")}
	if _, _, err := db.EnqueueControlTask(ctx, model.ControlTask{
		ID:           "l2-fail",
		Source:       "feishu",
		SourceAddr:   feishuAddress("ou_u1", "dev", "UnifiedRobot"),
		OwnerKey:     "u1",
		BotChannelID: "dev",
		RawInput:     "失败",
		Status:       "llm_pending",
		MaxAttempts:  1,
	}); err != nil {
		t.Fatal(err)
	}
	task, err := db.ClaimNextL2ControlTask(ctx, "w1", time.Now().Add(time.Minute), time.Now())
	if err != nil || task == nil {
		t.Fatalf("claim=%+v err=%v", task, err)
	}
	hub.processL2Task(ctx, *task, hub.effectiveL2Config())
	stored, err := db.GetControlTask(ctx, "l2-fail")
	if err != nil || stored == nil || stored.Status != "failed" {
		t.Fatalf("failed task=%+v err=%v", stored, err)
	}
	out := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "处理失败") {
		t.Fatalf("failed notification=%+v", out)
	}
	stats, err := db.ControlTaskStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.L2FailureRate != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestControlPlaneP2ProviderFailureRequeuesUntilMaxAttempts(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.L2 = &fakeTriageProvider{err: errors.New("temporary llm down")}
	if _, _, err := db.EnqueueControlTask(ctx, model.ControlTask{
		ID:           "l2-retry",
		Source:       "feishu",
		SourceAddr:   feishuAddress("ou_u1", "dev", "UnifiedRobot"),
		OwnerKey:     "u1",
		BotChannelID: "dev",
		RawInput:     "重试",
		Status:       "llm_pending",
		MaxAttempts:  3,
	}); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		task, err := db.ClaimNextL2ControlTask(ctx, fmt.Sprintf("w%d", attempt), time.Now().Add(time.Minute), time.Now())
		if err != nil || task == nil {
			t.Fatalf("claim attempt %d task=%+v err=%v", attempt, task, err)
		}
		hub.processL2Task(ctx, *task, hub.effectiveL2Config())
		stored, err := db.GetControlTask(ctx, "l2-retry")
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != "llm_pending" || stored.Attempts != attempt || stored.LeaseOwner != "" || stored.LeaseUntil != nil {
			t.Fatalf("after attempt %d task=%+v", attempt, stored)
		}
	}
	task, err := db.ClaimNextL2ControlTask(ctx, "w3", time.Now().Add(time.Minute), time.Now())
	if err != nil || task == nil {
		t.Fatalf("claim third task=%+v err=%v", task, err)
	}
	hub.processL2Task(ctx, *task, hub.effectiveL2Config())
	stored, err := db.GetControlTask(ctx, "l2-retry")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.Attempts != 3 {
		t.Fatalf("after third failure task=%+v", stored)
	}
	out := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "处理失败") {
		t.Fatalf("failed notification=%+v", out)
	}
}

func TestControlPlaneP3DecomposeChildrenAggregateAndNotify(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	alpha := dialSession(t, ctx, srv.URL, "alpha", key.ID, secret)
	defer alpha.Close(websocket.StatusNormalClosure, "done")
	beta := dialSession(t, ctx, srv.URL, "beta", key.ID, secret)
	defer beta.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, key.ID, "alpha")
	waitSessionOnline(t, hub, key.ID, "beta")

	hub.L2 = &sequenceTriageProvider{outs: []string{
		`{"intent":"decompose","subtasks":[{"session":"alpha","instruction":"整理需求"},{"session":"beta","instruction":"评估风险"}],"confidence":0.95}`,
		`{"intent":"aggregate","reply":"综合回复：需求和风险都已完成。","confidence":0.92}`,
	}}
	task := model.ControlTask{
		ID:           "p3-parent",
		Source:       "feishu",
		SourceAddr:   feishuAddress("ou_u1", "dev", "UnifiedRobot"),
		OwnerKey:     "u1",
		BotChannelID: "dev",
		RawInput:     "请团队拆分处理",
		Status:       "llm_pending",
		Priority:     7,
	}
	if _, _, err := db.EnqueueControlTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	hub.processL2Task(ctx, task, hub.effectiveL2Config())

	gotAlpha := readEnvelope(t, ctx, alpha)
	if gotAlpha.Body != "整理需求" || gotAlpha.Meta["control_task_id"] != "p3-parent-sub-01" {
		t.Fatalf("alpha child envelope=%+v", gotAlpha)
	}
	gotBeta := readEnvelope(t, ctx, beta)
	if gotBeta.Body != "评估风险" || gotBeta.Meta["control_task_id"] != "p3-parent-sub-02" {
		t.Fatalf("beta child envelope=%+v", gotBeta)
	}
	parent, err := db.GetControlTask(ctx, "p3-parent")
	if err != nil || parent == nil || parent.Status != "awaiting_children" || parent.Intent != "decompose" {
		t.Fatalf("parent after decompose=%+v err=%v", parent, err)
	}
	children, err := db.ListControlSubtasks(ctx, "p3-parent")
	if err != nil || len(children) != 2 {
		t.Fatalf("children=%+v err=%v", children, err)
	}
	for _, child := range children {
		if child.Status != "awaiting_result" || child.Priority != 7 {
			t.Fatalf("child should await result and inherit priority: %+v", child)
		}
	}

	if err := writeEnvelope(ctx, alpha, model.Envelope{
		ID:   "p3-parent-alpha-reply",
		To:   "workpulse#" + key.ID,
		From: "alpha#" + key.ID,
		Body: "需求完成",
		Meta: map[string]any{"control_task_id": "p3-parent-sub-01"},
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		child, err := db.GetControlTask(ctx, "p3-parent-sub-01")
		if err != nil || child == nil || child.Status != "done" || child.Result != "需求完成" {
			return false
		}
		parent, err = db.GetControlTask(ctx, "p3-parent")
		return err == nil && parent != nil && parent.Status == "awaiting_children"
	})
	if err := writeEnvelope(ctx, beta, model.Envelope{
		ID:   "p3-parent-beta-reply",
		To:   "workpulse#" + key.ID,
		From: "beta#" + key.ID,
		Body: "风险完成",
		Meta: map[string]any{"control_task_id": "p3-parent-sub-02"},
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		parent, err = db.GetControlTask(ctx, "p3-parent")
		return err == nil && parent != nil && parent.Status == "done" && parent.Intent == "aggregate" && strings.Contains(parent.Result, "综合回复")
	})
	out := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "综合回复：需求和风险都已完成。") {
		t.Fatalf("aggregate source reply=%+v", out)
	}
}

func TestControlPlaneP3PartialChildFailureStillAggregates(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	alpha := dialSession(t, ctx, srv.URL, "alpha", key.ID, secret)
	defer alpha.Close(websocket.StatusNormalClosure, "done")
	beta := dialSession(t, ctx, srv.URL, "beta", key.ID, secret)
	defer beta.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, key.ID, "alpha")
	waitSessionOnline(t, hub, key.ID, "beta")

	hub.L2 = &sequenceTriageProvider{outs: []string{
		`{"intent":"decompose","subtasks":[{"session":"alpha","instruction":"整理需求"},{"session":"beta","instruction":"评估风险"}],"confidence":0.95}`,
		`{"intent":"aggregate","reply":"部分完成：需求完成，风险子任务失败。","confidence":0.88}`,
	}}
	task := model.ControlTask{
		ID:           "p3-partial",
		Source:       "feishu",
		SourceAddr:   feishuAddress("ou_u1", "dev", "UnifiedRobot"),
		OwnerKey:     "u1",
		BotChannelID: "dev",
		RawInput:     "请团队拆分处理",
		Status:       "llm_pending",
	}
	if _, _, err := db.EnqueueControlTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	hub.processL2Task(ctx, task, hub.effectiveL2Config())
	gotAlpha := readEnvelope(t, ctx, alpha)
	if gotAlpha.Body != "整理需求" || gotAlpha.Meta["control_task_id"] != "p3-partial-sub-01" {
		t.Fatalf("alpha child envelope=%+v", gotAlpha)
	}
	gotBeta := readEnvelope(t, ctx, beta)
	if gotBeta.Body != "评估风险" || gotBeta.Meta["control_task_id"] != "p3-partial-sub-02" {
		t.Fatalf("beta child envelope=%+v", gotBeta)
	}
	if err := hub.CompleteControlSubtask(ctx, "p3-partial-sub-01", "需求完成", "done", ""); err != nil {
		t.Fatal(err)
	}
	if err := hub.CompleteControlSubtask(ctx, "p3-partial-sub-02", "", "failed", "风险评估失败"); err != nil {
		t.Fatal(err)
	}
	failedChild, err := db.GetControlTask(ctx, "p3-partial-sub-02")
	if err != nil || failedChild == nil || failedChild.Status != "failed" || !strings.Contains(failedChild.Error, "风险评估失败") {
		t.Fatalf("failed child=%+v err=%v", failedChild, err)
	}
	parent, err := db.GetControlTask(ctx, "p3-partial")
	if err != nil || parent == nil || parent.Status != "done" || parent.Intent != "aggregate" {
		t.Fatalf("partial parent=%+v err=%v", parent, err)
	}
	out := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "部分完成") {
		t.Fatalf("partial aggregate reply=%+v", out)
	}
}

func TestControlPlaneP3AggregateFailureRetriesAndFails(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.L2 = &fakeTriageProvider{err: errors.New("aggregate llm down")}
	parent := model.ControlTask{
		ID:           "p3-agg-fail",
		Source:       "feishu",
		SourceAddr:   feishuAddress("ou_u1", "dev", "UnifiedRobot"),
		OwnerKey:     "u1",
		BotChannelID: "dev",
		RawInput:     "请团队拆分处理",
		Status:       "llm_pending",
		MaxAttempts:  1,
	}
	if _, _, err := db.EnqueueControlTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	children := []model.ControlTask{{
		ID:          "p3-agg-fail-sub-01",
		ParentID:    "p3-agg-fail",
		Source:      "session",
		OwnerKey:    "u1",
		RawInput:    "整理需求",
		Intent:      "dispatch",
		Layer:       "L2",
		Result:      "需求完成",
		Status:      "done",
		MaxAttempts: 1,
	}}
	parent.Target = `[{"session":"alpha","instruction":"整理需求"}]`
	if err := db.CreateControlSubtasks(ctx, parent, children); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNextAggregateControlTask(ctx, "agg-w1", time.Now().Add(time.Minute), time.Now())
	if err != nil || claimed == nil || claimed.ID != "p3-agg-fail" {
		t.Fatalf("aggregate claim=%+v err=%v", claimed, err)
	}
	hub.processL2Task(ctx, *claimed, hub.effectiveL2Config())
	stored, err := db.GetControlTask(ctx, "p3-agg-fail")
	if err != nil || stored == nil || stored.Status != "failed" || stored.Attempts != 1 {
		t.Fatalf("aggregate failed parent=%+v err=%v", stored, err)
	}
	out := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if out == nil || !strings.Contains(messageText(t, out), "处理失败") {
		t.Fatalf("aggregate failure reply=%+v", out)
	}
}

func TestControlPlaneP3L2ClaimHonorsPriority(t *testing.T) {
	_, db, ctx := newTestHub(t)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	for _, task := range []model.ControlTask{
		{ID: "prio-low", Priority: 1},
		{ID: "prio-high", Priority: 9},
		{ID: "prio-mid", Priority: 5},
	} {
		task.Source = "feishu"
		task.SourceAddr = "ou_1#dev#UnifiedRobot"
		task.OwnerKey = "u1"
		task.RawInput = task.ID
		task.Status = "llm_pending"
		task.CreatedAt = now
		if _, _, err := db.EnqueueControlTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := db.ClaimNextL2ControlTask(ctx, "prio-w", now.Add(time.Minute), now)
	if err != nil || claimed == nil || claimed.ID != "prio-high" {
		t.Fatalf("priority claim=%+v err=%v", claimed, err)
	}
}

func TestControlPlaneP3AggregateClaimReclaimsExpiredLease(t *testing.T) {
	_, db, ctx := newTestHub(t)
	now := time.Now().UTC()
	parent := model.ControlTask{
		ID:          "p3-agg-reclaim",
		Source:      "feishu",
		SourceAddr:  "ou_1#dev#UnifiedRobot",
		OwnerKey:    "u1",
		RawInput:    "聚合",
		Status:      "llm_pending",
		MaxAttempts: 3,
	}
	if _, _, err := db.EnqueueControlTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	parent.Target = `[{"session":"alpha","instruction":"整理需求"}]`
	if err := db.CreateControlSubtasks(ctx, parent, []model.ControlTask{{
		ID:          "p3-agg-reclaim-sub-01",
		ParentID:    "p3-agg-reclaim",
		Source:      "session",
		OwnerKey:    "u1",
		RawInput:    "整理需求",
		Status:      "done",
		Result:      "完成",
		MaxAttempts: 3,
	}}); err != nil {
		t.Fatal(err)
	}
	first, err := db.ClaimNextAggregateControlTask(ctx, "agg-w1", now.Add(time.Minute), now)
	if err != nil || first == nil || first.ID != "p3-agg-reclaim" || first.LeaseOwner != "agg-w1" {
		t.Fatalf("first aggregate claim=%+v err=%v", first, err)
	}
	none, err := db.ClaimNextAggregateControlTask(ctx, "agg-w2", now.Add(2*time.Minute), now.Add(30*time.Second))
	if err != nil || none != nil {
		t.Fatalf("unexpired aggregate claim=%+v err=%v", none, err)
	}
	reclaimed, err := db.ClaimNextAggregateControlTask(ctx, "agg-w2", now.Add(3*time.Minute), now.Add(2*time.Minute))
	if err != nil || reclaimed == nil || reclaimed.ID != "p3-agg-reclaim" || reclaimed.LeaseOwner != "agg-w2" {
		t.Fatalf("reclaimed aggregate claim=%+v err=%v", reclaimed, err)
	}
	stats, err := db.ControlTaskStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.L2InFlight != 1 {
		t.Fatalf("stats should count aggregating lease as L2 in-flight: %+v", stats)
	}
}

type fakeTriageProvider struct {
	out string
	err error
}

func (f *fakeTriageProvider) Name() string { return "fake-triage" }
func (f *fakeTriageProvider) Complete(_ context.Context, _, user string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if !strings.Contains(user, "request_id") {
		return "", errors.New("missing request_id")
	}
	return f.out, nil
}

type sequenceTriageProvider struct {
	outs  []string
	index int
}

func (f *sequenceTriageProvider) Name() string { return "sequence-triage" }
func (f *sequenceTriageProvider) Complete(_ context.Context, _, user string) (string, error) {
	if !strings.Contains(user, "request_id") {
		return "", errors.New("missing request_id")
	}
	if f.index >= len(f.outs) {
		return "", fmt.Errorf("unexpected l2 call %d", f.index+1)
	}
	out := f.outs[f.index]
	f.index++
	return out, nil
}

func TestIssueAPIKeyStoresHashAndBindScope(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, meta, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" || strings.Contains(meta.KeyHash, secret) || !strings.HasPrefix(meta.ID, "FB-") {
		t.Fatalf("secret/hash/key_id invalid secret=%q meta=%+v", secret, meta)
	}
	resolved, err := db.ResolveServiceAPISecret(ctx, meta.ID, HashAPIKey(secret))
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil || resolved.ID != meta.ID || resolved.KeyHash == secret {
		t.Fatalf("resolved key = %+v", resolved)
	}
	if err := hub.BindAccount(ctx, meta.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal([]string{"dev:personal:alice"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "openapi", AccountScopeJSON: string(scope), Enabled: true}); err != nil {
		t.Fatalf("AddPrefixRule bound scope: %v", err)
	}
	badScope, _ := json.Marshal([]string{"dev:personal:bob"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r2", ServiceID: "svc1", MatchExpr: "codex", AccountScopeJSON: string(badScope), Enabled: true}); err == nil {
		t.Fatalf("AddPrefixRule accepted unbound account scope")
	}
	if err := db.RevokeServiceAPIKey(ctx, meta.ID); err != nil {
		t.Fatalf("RevokeServiceAPIKey: %v", err)
	}
	revoked, err := db.ResolveServiceAPISecret(ctx, meta.ID, HashAPIKey(secret))
	if err != nil {
		t.Fatalf("Resolve revoked key: %v", err)
	}
	if revoked != nil {
		t.Fatalf("revoked key resolved: %+v", revoked)
	}
}

func TestSystemKeywordRoutesToSystemHandler(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := db.UpsertSystemService(ctx, model.SystemService{Name: "scheduler", Description: "调度", Delivery: "internal", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSystemRoute(ctx, model.SystemRoute{Keyword: "#进度汇报", ServiceName: "scheduler", Action: "record", Priority: 1, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSystemRoute(ctx, model.SystemRoute{Keyword: "#调整日程", ServiceName: "scheduler", Action: "coordinate", Priority: 1, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSystemRoute(ctx, model.SystemRoute{Keyword: "sys:调度", ServiceName: "scheduler", Action: "auto", Priority: 99, Active: true}); err != nil {
		t.Fatal(err)
	}
	handler := &fakeSystemHandler{reply: "已提交"}
	hub.System = handler
	result, err := hub.Dispatch(ctx, model.Message{ID: "sys1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "@_user_1 #进度汇报 本周做了X")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "已提交" {
		t.Fatalf("result=%+v", result)
	}
	if handler.service != "scheduler" || handler.action != "record" || handler.body != "本周做了X" {
		t.Fatalf("handler=%+v", handler)
	}
	result, err = hub.Dispatch(ctx, model.Message{ID: "sys2", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "#调整日程 UserThree顺延3天")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || handler.action != "coordinate" || handler.body != "UserThree顺延3天" {
		t.Fatalf("coordinate result=%+v handler=%+v", result, handler)
	}
	result, err = hub.Dispatch(ctx, model.Message{ID: "sys3", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "我想 #调整日程")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "已受理 #sys3") {
		t.Fatalf("middle keyword should only ack for L2: %+v", result)
	}
	result, err = hub.Dispatch(ctx, model.Message{ID: "sys4", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "sys:调度 协调团队排期")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || handler.action != "auto" || handler.body != "协调团队排期" {
		t.Fatalf("sys compat result=%+v handler=%+v", result, handler)
	}
	hub.RegisterBot("dev", "UnifiedRobot")
	result, err = hub.Dispatch(ctx, model.Message{ID: "sys5", ChatEntityID: "dev:group:oc_team", BotChannelID: "dev", ChatType: model.ChatGroup}, "@UnifiedRobot #调整日程 UserThree顺延3天")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || handler.action != "coordinate" || handler.body != "UserThree顺延3天" {
		t.Fatalf("literal bot mention should strip to system route result=%+v handler=%+v", result, handler)
	}
}

func TestSecurityOpsRoutesToAllSystemSessions(t *testing.T) {
	useTestSecOpsAdminOpenID(t)
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("bot-test", "bot-test")
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: secOpsAdminOpenID, Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: secOpsOwnerKey, DisplayName: secOpsMemberName, Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "sec", Name: "sec", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "sec", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, secOpsOwnerKey); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	claude := dialSession(t, ctx, srv.URL, "sec-claude", key.ID, secret)
	defer claude.Close(websocket.StatusNormalClosure, "done")
	deepseek := dialSession(t, ctx, srv.URL, "sec-deepseek", key.ID, secret)
	defer deepseek.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, key.ID, "sec-claude")
	waitSessionOnline(t, hub, key.ID, "sec-deepseek")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "secops1",
		ChatEntityID: "dev:group:oc_c610",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: secOpsAdminOpenID,
		Content:      `{"text":"@SYSTEM-V-TASK-INTERNAL #系统安全 加白名单 1.2.3.4"}`,
	}, "@SYSTEM-V-TASK-INTERNAL #系统安全 加白名单 1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "2个sec-ops会话") {
		t.Fatalf("secops result=%+v", result)
	}
	gotClaude := readEnvelope(t, ctx, claude)
	gotDeepseek := readEnvelope(t, ctx, deepseek)
	got := gotClaude.Body + "\n" + gotDeepseek.Body
	if strings.Count(got, "加白名单 1.2.3.4") != 2 || strings.Contains(got, "#系统安全") || strings.Contains(got, "@SYSTEM-V-TASK-INTERNAL") {
		t.Fatalf("secops bodies not stripped:\nclaude=%+v\ndeepseek=%+v", gotClaude, gotDeepseek)
	}
	if gotClaude.Meta["group_chat_id"] != "oc_c610" || gotDeepseek.Meta["sender_open_id"] != secOpsAdminOpenID {
		t.Fatalf("secops meta missing source: claude=%+v deepseek=%+v", gotClaude.Meta, gotDeepseek.Meta)
	}

	result, err = hub.Dispatch(ctx, model.Message{
		ID:           "secops2",
		ChatEntityID: "bot-test:group:oc_c610",
		BotChannelID: "bot-test",
		ChatType:     model.ChatGroup,
		SenderOpenID: secOpsAdminOpenID,
		Content:      `{"text":"@SYSTEM-V-TASK-INTERNAL #系统安全 撤销封禁 2.2.2.2"}`,
	}, "@SYSTEM-V-TASK-INTERNAL #系统安全 撤销封禁 2.2.2.2")
	if err != nil || !result.Matched {
		t.Fatalf("cc secops result=%+v err=%v", result, err)
	}
	if readEnvelope(t, ctx, claude).Body != "撤销封禁 2.2.2.2" || readEnvelope(t, ctx, deepseek).Body != "撤销封禁 2.2.2.2" {
		t.Fatal("cc connector secops did not fan out stripped command")
	}
}

func TestParseTerminalInputCommand(t *testing.T) {
	action, session, code, ok := parseTerminalInputCommand("#解锁输入 developer ab12cd")
	if !ok || action != "unlock" || session != "developer" || code != "AB12CD" {
		t.Fatalf("unlock parse = %q %q %q %v", action, session, code, ok)
	}
	action, session, code, ok = parseTerminalInputCommand("#锁定输入 developer")
	if !ok || action != "lock" || session != "developer" || code != "" {
		t.Fatalf("lock parse = %q %q %q %v", action, session, code, ok)
	}
	if _, _, _, ok := parseTerminalInputCommand("#解锁输入 developer"); ok {
		t.Fatal("accepted incomplete unlock")
	}
	if _, _, _, ok := parseTerminalInputCommand("#锁定输入 developer extra"); ok {
		t.Fatal("accepted invalid lock")
	}
}

func TestSecurityOpsRejectsUnauthorizedSender(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: secOpsOwnerKey, DisplayName: secOpsMemberName, Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "sec", Name: "sec", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "sec", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, secOpsOwnerKey); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	secSession := dialSession(t, ctx, srv.URL, "sec-claude", key.ID, secret)
	defer secSession.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, key.ID, "sec-claude")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "secops-deny",
		ChatEntityID: "dev:group:oc_c610",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: "ou_alice",
		Content:      `{"text":"@SYSTEM-V-TASK-INTERNAL#sec-claude #系统安全 加白名单 6.6.6.6"}`,
	}, "@SYSTEM-V-TASK-INTERNAL#sec-claude #系统安全 加白名单 6.6.6.6")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "无权限") {
		t.Fatalf("non u1 secops result=%+v", result)
	}
	readCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, _, err := secSession.Read(readCtx); err == nil {
		t.Fatal("unauthorized secops command was delivered to sec-claude")
	}
}

func TestSecurityOpsAllowsBoundAdminOwner(t *testing.T) {
	useTestSecOpsAdminOwnerKey(t, "u1")
	hub, db, ctx := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: secOpsOwnerKey, DisplayName: secOpsMemberName, Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_alias", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_alias", BoundOwner: "u1", Active: true}); err != nil {
		t.Fatal(err)
	}
	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "secops-owner",
		ChatEntityID: "dev:group:oc_c610",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: "ou_alias",
		Content:      `{"text":"@SYSTEM-V-TASK-INTERNAL #系统安全 改规则 404阈值=30"}`,
	}, "@SYSTEM-V-TASK-INTERNAL #系统安全 改规则 404阈值=30")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || strings.Contains(result.Reply, "无权限") || !strings.Contains(result.Reply, "无在线") {
		t.Fatalf("bound u1 secops result=%+v", result)
	}
}

type fakeSystemHandler struct {
	service string
	action  string
	body    string
	reply   string
}

func (f *fakeSystemHandler) HandleSystemRequest(_ context.Context, serviceName, action, body string, _ model.Message) (string, error) {
	f.service = serviceName
	f.action = action
	f.body = body
	return f.reply, nil
}

func TestDispatchForwardsOnlineWSWithStripPrefix(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/service/svc1?api_key=" + plain
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("client read: %v", err)
			return
		}
		var req ForwardRequest
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		if req.Text != "hello" || req.ChatEntityID != "dev:personal:alice" {
			t.Errorf("request = %+v", req)
		}
		resp, _ := json.Marshal(ForwardResponse{Reply: "service ok"})
		if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
			t.Errorf("client write: %v", err)
		}
	}()
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal, Content: `{"text":"bot hello"}`}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "service ok" {
		t.Fatalf("result = %+v", result)
	}
	<-done
}

func TestDispatchOfflineReturnsFailureNoReplay(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	_ = setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", false)
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "该消息投递失败") || !strings.Contains(result.Reply, "bot hello") {
		t.Fatalf("result = %+v", result)
	}
}

func TestDispatchDoesNotMatchUnscopedAccount(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	_ = setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot", false)
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:bob", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "已受理 #m1") {
		t.Fatalf("unscoped account should only ack for L2, got: %+v", result)
	}
}

func TestDispatchEmptyScopeUsesBoundAccountsOnly(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "bot", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	alice, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !alice.Matched {
		t.Fatalf("bound account should match empty scope")
	}
	bob, err := hub.Dispatch(ctx, model.Message{ID: "m2", ChatEntityID: "dev:personal:bob", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !bob.Matched || !strings.Contains(bob.Reply, "已受理 #m2") {
		t.Fatalf("unbound account should only ack for L2: %+v", bob)
	}
}

func TestAddPrefixRuleRejectsWildcardOverlapInSameScope(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal([]string{"dev:personal:alice"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "openAPI???", AccountScopeJSON: string(scope), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r2", ServiceID: "svc1", MatchExpr: "openAPI1", AccountScopeJSON: string(scope), Enabled: true}); err == nil {
		t.Fatalf("accepted overlapping wildcard prefix")
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r3", ServiceID: "svc1", MatchExpr: "*", AccountScopeJSON: string(scope), Enabled: true}); err == nil {
		t.Fatalf("accepted high-risk * prefix")
	}
}

func TestAddPrefixRuleAllowsSamePatternForDisjointAccounts(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc2", Name: "svc2", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key1, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	_, key2, err := hub.IssueAPIKey(ctx, "svc2", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key1.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key2.ID, "dev:personal:bob"); err != nil {
		t.Fatal(err)
	}
	scopeAlice, _ := json.Marshal([]string{"dev:personal:alice"})
	scopeBob, _ := json.Marshal([]string{"dev:personal:bob"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "open*", AccountScopeJSON: string(scopeAlice), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r2", ServiceID: "svc2", MatchExpr: "openAPI", AccountScopeJSON: string(scopeBob), Enabled: true}); err != nil {
		t.Fatalf("disjoint account scope reported conflict: %v", err)
	}
}

func TestDispatchWildcardStripPrefix(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	plain := setupRoute(t, hub, ctx, "svc1", "dev:personal:alice", "bot???", true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/service/{serviceID}", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/service/svc1?api_key=" + plain
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("client read: %v", err)
			return
		}
		var req ForwardRequest
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		if req.Text != "hello" {
			t.Errorf("request text = %q", req.Text)
		}
		resp, _ := json.Marshal(ForwardResponse{Reply: "service ok"})
		if err := conn.Write(ctx, websocket.MessageText, resp); err != nil {
			t.Errorf("client write: %v", err)
		}
	}()
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot123 hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "service ok" {
		t.Fatalf("wildcard strip/failure reply = %+v", result)
	}
	<-done
}

func TestDispatchNonEmptyScopeRequiresStillBoundAccount(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal([]string{"dev:personal:alice"})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "r1", ServiceID: "svc1", MatchExpr: "bot", AccountScopeJSON: string(scope), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeServiceAPIKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "bot hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "已受理 #m1") {
		t.Fatalf("revoked key should only ack for L2: %+v", result)
	}
}

func setupRoute(t *testing.T, hub *Hub, ctx context.Context, serviceID, account, prefix string, strip bool) string {
	t.Helper()
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: serviceID, Name: serviceID, DeliveryType: "ws", ReplyMode: "sync", TimeoutMs: 1000, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	plain, key, err := hub.IssueAPIKey(ctx, serviceID, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, account); err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal([]string{account})
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{
		ID:               serviceID + "-rule",
		ServiceID:        serviceID,
		MatchExpr:        prefix,
		AccountScopeJSON: string(scope),
		StripPrefix:      strip,
		Enabled:          true,
	}); err != nil {
		t.Fatal(err)
	}
	return plain
}

func TestHashAPIKeyStable(t *testing.T) {
	a := HashAPIKey("secret")
	b := HashAPIKey("secret")
	if a != b || a == "secret" || len(a) == 0 {
		t.Fatalf("hash invalid a=%q b=%q", a, b)
	}
}

func TestSessionAddressingFourFlows(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	keyID := key.ID
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	home := dialSession(t, ctx, srv.URL, "home", keyID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	developer := dialSession(t, ctx, srv.URL, "developer", keyID, secret)
	defer developer.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "fm1",
		ChatEntityID: "dev:personal:alice",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#home 你好"}`,
	}, "#home 你好")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("dispatch result = %+v", result)
	}
	gotHome := readEnvelope(t, ctx, home)
	if gotHome.To != "home#"+keyID || gotHome.From != "alice#"+keyID+"#UnifiedRobot" || gotHome.Body != "你好" {
		t.Fatalf("feishu->home envelope = %+v", gotHome)
	}
	assertEnvelopeDoesNotContain(t, gotHome, secret)

	devEnv := model.Envelope{ID: "e2", To: "home#" + keyID, From: "developer#" + keyID, Body: "内部消息"}
	if err := writeEnvelope(ctx, developer, devEnv); err != nil {
		t.Fatalf("developer write: %v", err)
	}
	gotHome = readEnvelope(t, ctx, home)
	if gotHome.ID != "e2" || gotHome.Body != "内部消息" || gotHome.From != "developer#"+keyID {
		t.Fatalf("developer->home envelope = %+v", gotHome)
	}
	assertEnvelopeDoesNotContain(t, gotHome, secret)
	claimed, _, err := hub.claimEnvelope(devEnv)
	if err != nil {
		t.Fatalf("duplicate same envelope returned error: %v", err)
	}
	if claimed {
		t.Fatalf("duplicate same envelope was claimed again")
	}
	diffEnv := devEnv
	diffEnv.Body = "不同内容"
	if _, _, err := hub.claimEnvelope(diffEnv); err == nil {
		t.Fatalf("duplicate id with different body was accepted")
	}

	feishuEnv := model.Envelope{ID: "e3", To: "alice#" + keyID + "#UnifiedRobot", From: "home#" + keyID, Body: "回飞书"}
	if err := writeEnvelope(ctx, home, feishuEnv); err != nil {
		t.Fatalf("home write: %v", err)
	}
	msg := waitOutbound(t, ctx, outbound)
	if msg == nil || msg.ChatEntityID != "dev:personal:alice" || msg.BotChannelID != "dev" || !strings.Contains(msg.Content, "回飞书") {
		t.Fatalf("outbound message = %+v", msg)
	}
	if msg.ChatType != model.ChatPersonal || strings.Contains(messageText(t, msg), "<at user_id=") {
		t.Fatalf("personal outbound should not render at mention: %+v text=%q", msg, messageText(t, msg))
	}

	groupResult, err := hub.Dispatch(ctx, model.Message{
		ID:           "fm-group",
		ChatEntityID: "dev:group:oc_group",
		BotChannelID: "dev",
		ChatType:     model.ChatGroup,
		SenderOpenID: "ou_alice",
		Content:      `{"text":"@_user_1 #home 群里你好"}`,
	}, "@_user_1 #home 群里你好")
	if err != nil {
		t.Fatal(err)
	}
	if !groupResult.Matched || groupResult.Reply != "" {
		t.Fatalf("group dispatch result = %+v", groupResult)
	}
	gotGroup := readEnvelope(t, ctx, home)
	if gotGroup.To != "home#"+keyID || gotGroup.From != "ou_alice#"+keyID+"#UnifiedRobot" || gotGroup.Body != "群里你好" {
		t.Fatalf("group feishu->home envelope = %+v", gotGroup)
	}
	if gotGroup.Meta["group_chat_id"] != "oc_group" || gotGroup.Meta["sender_open_id"] != "ou_alice" {
		t.Fatalf("group inbound meta = %+v", gotGroup.Meta)
	}
	assertEnvelopeDoesNotContain(t, gotGroup, secret)

	groupEnv := model.Envelope{
		ID:   "e4",
		To:   "oc_group#" + keyID + "#UnifiedRobot",
		From: "home#" + keyID,
		Body: "群回复",
		Meta: map[string]any{"at": []any{"ou_alice"}},
	}
	if err := writeEnvelope(ctx, home, groupEnv); err != nil {
		t.Fatalf("home group write: %v", err)
	}
	msg = waitOutbound(t, ctx, outbound)
	if msg == nil || msg.ChatEntityID != "dev:group:oc_group" || msg.BotChannelID != "dev" || msg.ChatType != model.ChatGroup {
		t.Fatalf("group outbound message = %+v", msg)
	}
	if text := messageText(t, msg); text != `<at user_id="ou_alice"></at> 群回复` {
		t.Fatalf("group outbound text = %q msg=%+v", text, msg)
	}
}

func TestSessionReplyUsesSourceBotChannelContext(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	keyID := key.ID
	if err := hub.BindAccount(ctx, keyID, "is3-Connector:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, keyID, "is3-Connector:personal:ou_sender"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, keyID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("is3-Connector", "is3-Connector")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ccbot := dialSession(t, ctx, srv.URL, "ccbot", keyID, secret)
	defer ccbot.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, keyID, "ccbot")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "source-personal",
		ChatEntityID: "is3-Connector:personal:ou_u1",
		BotChannelID: "is3-Connector",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#ccbot ping"}`,
	}, "#ccbot ping")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("personal dispatch result=%+v", result)
	}
	gotPersonal := readEnvelope(t, ctx, ccbot)
	if gotPersonal.Meta["source_bot_channel_id"] != "is3-Connector" || gotPersonal.Meta["source_chat_type"] != string(model.ChatPersonal) || gotPersonal.Meta["source_open_id"] != "ou_u1" {
		t.Fatalf("personal source meta=%+v", gotPersonal.Meta)
	}
	if err := writeEnvelope(ctx, ccbot, model.Envelope{
		ID:   "reply-personal",
		To:   "ou_u1#" + keyID + "#UnifiedRobot",
		From: "ccbot#" + keyID,
		Body: "个人回复",
		Meta: gotPersonal.Meta,
	}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, ctx, outbound)
	if msg == nil || msg.BotChannelID != "is3-Connector" || msg.ChatEntityID != "is3-Connector:personal:ou_u1" || msg.ChatType != model.ChatPersonal {
		t.Fatalf("personal reply outbound=%+v", msg)
	}

	result, err = hub.Dispatch(ctx, model.Message{
		ID:           "source-group",
		ChatEntityID: "is3-Connector:group:oc_team",
		BotChannelID: "is3-Connector",
		ChatType:     model.ChatGroup,
		SenderOpenID: "ou_sender",
		Content:      `{"text":"@_user_1 #ccbot group ping"}`,
	}, "@_user_1 #ccbot group ping")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("group dispatch result=%+v", result)
	}
	gotGroup := readEnvelope(t, ctx, ccbot)
	if gotGroup.Meta["source_bot_channel_id"] != "is3-Connector" || gotGroup.Meta["source_chat_type"] != string(model.ChatGroup) || gotGroup.Meta["source_chat_id"] != "oc_team" || gotGroup.Meta["source_sender_openid"] != "ou_sender" {
		t.Fatalf("group source meta=%+v", gotGroup.Meta)
	}
	if err := writeEnvelope(ctx, ccbot, model.Envelope{
		ID:   "reply-group",
		To:   "oc_team#" + keyID + "#UnifiedRobot",
		From: "ccbot#" + keyID,
		Body: "群回复",
		Meta: gotGroup.Meta,
	}); err != nil {
		t.Fatal(err)
	}
	msg = waitOutbound(t, ctx, outbound)
	if msg == nil || msg.BotChannelID != "is3-Connector" || msg.ChatEntityID != "is3-Connector:group:oc_team" || msg.ChatType != model.ChatGroup {
		t.Fatalf("group reply outbound=%+v", msg)
	}
	if text := messageText(t, msg); text != `<at user_id="ou_sender"></at> 群回复` {
		t.Fatalf("group reply text=%q msg=%+v", text, msg)
	}

	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "reply-fallback",
		To:   "ou_u1#" + keyID + "#UnifiedRobot",
		From: "ccbot#" + keyID,
		Body: "默认回复",
	}); err != nil {
		t.Fatal(err)
	}
	msg = waitOutbound(t, ctx, outbound)
	if msg == nil || msg.BotChannelID != "dev" || msg.ChatEntityID != "dev:personal:ou_u1" || msg.ChatType != model.ChatPersonal {
		t.Fatalf("fallback outbound=%+v", msg)
	}
}

func TestCrossMemberMentionRoutesToTargetSessionAndRepliesToSource(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	u1Secret, u1Key, err := hub.IssueAPIKey(ctx, "svc1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	u2Secret, u2Key, err := hub.IssueAPIKey(ctx, "svc1", "u2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, u1Key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, u2Key.ID, "dev:personal:ou_u2"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u2", DisplayName: "UserTwo", FeishuOpenID: "ou_u2", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u1 := dialSession(t, ctx, srv.URL, "home", u1Key.ID, u1Secret)
	defer u1.Close(websocket.StatusNormalClosure, "done")
	u2 := dialSession(t, ctx, srv.URL, "u2", u2Key.ID, u2Secret)
	defer u2.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "cross-1",
		ChatEntityID: "dev:personal:ou_u1",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"@UserTwo#u2 看看这个方案"}`,
	}, "@UserTwo#u2 看看这个方案")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("cross dispatch result=%+v", result)
	}
	gotFeishu := readEnvelope(t, ctx, u2)
	if gotFeishu.To != "u2#"+u2Key.ID || gotFeishu.From != "ou_u1#"+u2Key.ID+"#UnifiedRobot" || gotFeishu.Body != "看看这个方案" {
		t.Fatalf("cross envelope=%+v", gotFeishu)
	}
	if gotFeishu.Meta["reply_prefix"] != "【UserTwo·u2】" || gotFeishu.Meta["cross_member"] != "u2" {
		t.Fatalf("cross meta=%+v", gotFeishu.Meta)
	}

	if err := writeEnvelope(ctx, u1, model.Envelope{ID: "cross-ws", To: "@UserTwo#u2", From: "home#" + u1Key.ID, Body: "会话主动消息"}); err != nil {
		t.Fatal(err)
	}
	gotWS := readEnvelope(t, ctx, u2)
	if gotWS.To != "u2#"+u2Key.ID || gotWS.From != "home#"+u2Key.ID || gotWS.Body != "会话主动消息" {
		t.Fatalf("cross ws envelope=%+v", gotWS)
	}
	if gotWS.Meta["source_session_name"] != "home" || gotWS.Meta["source_key_id"] != u1Key.ID || gotWS.Meta["reply_prefix"] != "【UserTwo·u2】" {
		t.Fatalf("cross ws meta=%+v", gotWS.Meta)
	}

	if err := writeEnvelope(ctx, u2, model.Envelope{ID: "cross-reply", To: gotFeishu.From, From: gotFeishu.To, Body: "【UserTwo·u2】已看"}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, ctx, outbound)
	if msg == nil || msg.ChatEntityID != "dev:personal:ou_u1" || !strings.Contains(messageText(t, msg), "【UserTwo·u2】已看") {
		t.Fatalf("cross reply outbound=%+v", msg)
	}
}

func TestAgentNetworkSkillPushAckAndSessionSelectorEnvelope(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	home := dialSessionWithHeaderRaw(t, ctx, srv.URL, "home", key.ID, secret, nil)
	defer home.Close(websocket.StatusNormalClosure, "done")
	skill := readEnvelopeIncludingSystem(t, ctx, home)
	if skill.Meta["type"] != agentNetworkSkillPushType || !strings.Contains(skill.Body, agentNetworkSkillAck) || !strings.Contains(skill.Body, "#developer 请核对X") {
		t.Fatalf("agent skill push=%+v", skill)
	}
	if err := writeEnvelope(ctx, home, model.Envelope{
		ID:   "skill-ack",
		To:   "workpulse#" + key.ID,
		From: "home#" + key.ID,
		Body: agentNetworkSkillAck,
		Meta: map[string]any{"type": agentNetworkSkillAckType},
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return hub.sessionClients[key.ID]["home"].skillInstalled
	})

	developer := dialSession(t, ctx, srv.URL, "developer", key.ID, secret)
	defer developer.Close(websocket.StatusNormalClosure, "done")
	if err := writeEnvelope(ctx, home, model.Envelope{ID: "selector", To: "#developer", From: "home#" + key.ID, Body: "请核对X"}); err != nil {
		t.Fatal(err)
	}
	got := readEnvelope(t, ctx, developer)
	if got.To != "developer#"+key.ID || got.From != "home#"+key.ID || got.Body != "请核对X" {
		t.Fatalf("selector routed envelope=%+v", got)
	}
}

func TestCrossMemberMentionRejectsNoDirectorySession(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	u1Secret, u1Key, err := hub.IssueAPIKey(ctx, "svc1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	u2Secret, u2Key, err := hub.IssueAPIKey(ctx, "svc1", "u2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, u1Key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, u2Key.ID, "dev:personal:ou_u2"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u2", DisplayName: "UserTwo", FeishuOpenID: "ou_u2", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u1 := dialSession(t, ctx, srv.URL, "home", u1Key.ID, u1Secret)
	defer u1.Close(websocket.StatusNormalClosure, "done")
	secOps := dialSessionWithQuery(t, ctx, srv.URL, "sec-ops", u2Key.ID, u2Secret, "no_directory=1")
	defer secOps.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "cross-hidden",
		ChatEntityID: "dev:personal:ou_u1",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"@UserTwo#sec-ops 帮忙看"}`,
	}, "@UserTwo#sec-ops 帮忙看")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "该会话为专职隔离会话不接受任务派发" {
		t.Fatalf("hidden cross dispatch result=%+v", result)
	}

	result, err = hub.Dispatch(ctx, model.Message{
		ID:           "direct-hidden",
		ChatEntityID: "dev:personal:ou_u2",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#sec-ops 自查"}`,
	}, "#sec-ops 自查")
	if err != nil || !result.Matched {
		t.Fatalf("direct hidden dispatch result=%+v err=%v", result, err)
	}
	got := readEnvelope(t, ctx, secOps)
	if got.To != "sec-ops#"+u2Key.ID || got.Body != "自查" {
		t.Fatalf("direct hidden envelope=%+v", got)
	}
}

func TestCrossMemberMentionRequiresSessionWhenMultipleOnline(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "u2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u2"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u2", DisplayName: "UserTwo", FeishuOpenID: "ou_u2", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	dev := dialSession(t, ctx, srv.URL, "dev", key.ID, secret)
	defer dev.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{ID: "cross-amb", ChatEntityID: "dev:personal:ou_u1", BotChannelID: "dev", ChatType: model.ChatPersonal}, "@u2 帮我看")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "多个在线会话") {
		t.Fatalf("ambiguous cross result=%+v", result)
	}
}

func TestAggregateReviewCommandBypassesPersonalSessionAmbiguity(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "reviewer", DisplayName: "Reviewer", FeishuOpenID: "ou_reviewer", Role: model.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, project := range []model.Project{
		{ID: "proj:aggregate", Name: "Aggregate", OwnerKey: "reviewer", NotifyChatID: "oc_group", NotifyBotID: "dev", Active: true},
		{ID: "proj:source", Name: "Source", OwnerKey: "source-owner", Active: true},
	} {
		if err := db.UpsertProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetProjectAggregateSources(ctx, "proj:aggregate", []string{"proj:source"}); err != nil {
		t.Fatal(err)
	}
	week := "2026-06-29"
	if err := db.UpsertProjectWeeklyReport(ctx, model.ProjectWeeklyReport{
		ID:        "aggregate-weekly-proj:aggregate-20260629",
		ProjectID: "proj:aggregate",
		Week:      week,
		Content:   "聚合周报草稿",
		Status:    "draft",
		CreatedAt: time.Date(2026, 7, 5, 22, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 5, 22, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	svc := scheduler.New(scheduler.Config{}, nil, &clock.Fake{T: time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC)}, outbound)
	svc.Repo = db
	hub.System = svc

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	dev := dialSession(t, ctx, srv.URL, "dev", key.ID, secret)
	defer dev.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "approve-aggregate",
		ChatEntityID: "dev:personal:ou_reviewer",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		SenderOpenID: "ou_reviewer",
	}, "批准发送")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "已批准并发送") || strings.Contains(result.Reply, "多个在线会话") {
		t.Fatalf("aggregate review result=%+v", result)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("published message=%+v err=%v", msg, err)
	}
	if msg.ChatEntityID != "dev:group:oc_group" || msg.ChatType != model.ChatGroup {
		t.Fatalf("published message target=%+v", msg)
	}
}

func TestCrossMemberMentionHonorsDMOptOut(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u2", DisplayName: "UserTwo", FeishuOpenID: "ou_u2", Role: model.RoleMember, DMOptOut: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	result, err := hub.Dispatch(ctx, model.Message{ID: "cross-optout", ChatEntityID: "dev:personal:ou_u1", BotChannelID: "dev", ChatType: model.ChatPersonal}, "@UserTwo#u2 hi")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "未开放会话接入") {
		t.Fatalf("optout cross result=%+v", result)
	}
}

func TestPersonalMessageDefaultsToOnlyOnlineSession(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "u2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u2"); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u2 := dialSession(t, ctx, srv.URL, "u2", key.ID, secret)
	defer u2.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "default-1",
		ChatEntityID: "dev:personal:ou_u2",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"你好"}`,
	}, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("default dispatch result=%+v", result)
	}
	got := readEnvelope(t, ctx, u2)
	if got.To != "u2#"+key.ID || got.From != "ou_u2#"+key.ID+"#UnifiedRobot" || got.Body != "你好" {
		t.Fatalf("default envelope=%+v", got)
	}
	if got.Meta["reply_prefix"] != "【u2】" {
		t.Fatalf("default envelope meta=%+v", got.Meta)
	}

	if err := writeEnvelope(ctx, u2, model.Envelope{ID: "default-reply", To: got.From, From: got.To, Body: "【u2】你好"}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, ctx, outbound)
	if msg == nil || msg.ChatEntityID != "dev:personal:ou_u2" || !strings.Contains(messageText(t, msg), "【u2】你好") {
		t.Fatalf("default reply outbound=%+v", msg)
	}
}

func TestPersonalMessageDefaultRouteRequiresExplicitSessionWhenMultipleOnline(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "u2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u2"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u2 := dialSession(t, ctx, srv.URL, "u2", key.ID, secret)
	defer u2.Close(websocket.StatusNormalClosure, "done")
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{ID: "default-many", ChatEntityID: "dev:personal:ou_u2", BotChannelID: "dev", ChatType: model.ChatPersonal}, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "多个在线会话") || !strings.Contains(result.Reply, "u2") || !strings.Contains(result.Reply, "home") {
		t.Fatalf("multi default result=%+v", result)
	}
}

func TestPersonalMessageDefaultRouteNoSessionAndGroupAreIgnored(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "u2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u2"); err != nil {
		t.Fatal(err)
	}
	personal, err := hub.Dispatch(ctx, model.Message{ID: "default-none", ChatEntityID: "dev:personal:ou_u2", BotChannelID: "dev", ChatType: model.ChatPersonal}, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if !personal.Matched || !strings.Contains(personal.Reply, "已受理 #default-none") {
		t.Fatalf("no-session personal result=%+v", personal)
	}
	group, err := hub.Dispatch(ctx, model.Message{ID: "default-group", ChatEntityID: "dev:group:oc_group", BotChannelID: "dev", ChatType: model.ChatGroup, SenderOpenID: "ou_u2"}, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if !group.Matched || !strings.Contains(group.Reply, "已受理 #default-group") {
		t.Fatalf("group default result=%+v", group)
	}
}

func TestSessionAddressingRejectsCrossKey(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	keyID := key.ID
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", keyID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	if err := writeEnvelope(ctx, home, model.Envelope{ID: "bad", To: "developer#other", From: "home#" + keyID, Body: "x"}); err != nil {
		t.Fatalf("write bad envelope: %v", err)
	}
	got := readEnvelope(t, ctx, home)
	if !strings.Contains(got.Body, "key_id") {
		t.Fatalf("cross-key error envelope = %+v", got)
	}
	assertEnvelopeDoesNotContain(t, got, secret)
}

func TestCollectEnvelopeStoresOnlyAndHonorsOptOut(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	outbound := bus.NewDBQueue(db, model.DirectionOut)
	hub.Outbound = outbound
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "collect-1",
		To:   "workpulse#" + key.ID,
		From: "home#" + key.ID,
		Body: "完成自主研发",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": "collect", "role": "claude", "session": "home"},
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := db.RecentMessages(ctx, model.MessageFilter{ChatEntityID: "home#" + key.ID, Limit: 10})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("collect msgs=%+v err=%v", msgs, err)
	}
	if msgs[0].Direction != model.DirectionCollect || msgs[0].Status != "done" || !strings.Contains(msgs[0].Content, "完成自主研发") {
		t.Fatalf("collect message = %+v", msgs[0])
	}
	next, err := db.ClaimNextMessage(ctx, model.DirectionCollect)
	if err != nil || next != nil {
		t.Fatalf("collect should not be claimable next=%+v err=%v", next, err)
	}
	if out, err := db.ClaimNextMessage(ctx, model.DirectionOut); err != nil || out != nil {
		t.Fatalf("collect should not enqueue outbound out=%+v err=%v", out, err)
	}

	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, EvidenceOptOut: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "collect-2",
		To:   "workpulse#" + key.ID,
		From: "home#" + key.ID,
		Body: "不应采集",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": "collect", "role": "claude", "session": "home"},
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err = db.RecentMessages(ctx, model.MessageFilter{ChatEntityID: "home#" + key.ID, Limit: 10})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("optout collect msgs=%+v err=%v", msgs, err)
	}
}

func TestSessionEndpointTracksClientIPAndOffline(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn := dialSessionWithHeader(t, ctx, srv.URL, "home", key.ID, secret, http.Header{"X-Forwarded-For": {"203.0.113.7, 10.0.0.1"}, "X-Real-IP": {"198.51.100.9"}})
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].Active || endpoints[0].ClientIP != "203.0.113.7" {
		t.Fatalf("endpoint after connect=%+v err=%v", endpoints, err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "done")
	deadline := time.Now().Add(2 * time.Second)
	for {
		endpoints, err = db.ListSessionEndpoints(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(endpoints) == 1 && !endpoints[0].Active && endpoints[0].ClientIP == "203.0.113.7" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("endpoint not offline after close: %+v", endpoints)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionEndpointStoresToolAndModel(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn := dialSessionWithQuery(t, ctx, srv.URL, "developer", key.ID, secret, "tool=CODEX&model=gpt-5.5&full_session_name=sh-developer-e0d12642")
	defer conn.Close(websocket.StatusNormalClosure, "done")
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%+v err=%v", endpoints, err)
	}
	if endpoints[0].Tool != "CODEX" || endpoints[0].Model != "gpt-5.5" {
		t.Fatalf("tool/model not stored: %+v", endpoints[0])
	}
	if endpoints[0].SessionName != "developer" || endpoints[0].FullSessionName != "sh-developer-e0d12642" {
		t.Fatalf("session names not stored correctly: %+v", endpoints[0])
	}
}

func TestSessionEndpointStoresHandshakeMirrorToBotUnchanged(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mirrorTo := "ou_u1#" + key.ID + "#is3-Connector"
	conn := dialSessionWithQuery(t, ctx, srv.URL, "ts3-poc", key.ID, secret, "target_bot=bot-test&mirror_to="+url.QueryEscape(mirrorTo))
	defer conn.Close(websocket.StatusNormalClosure, "done")
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints=%+v err=%v", endpoints, err)
	}
	if !endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != mirrorTo {
		t.Fatalf("mirror_to should preserve source bot: %+v want=%q", endpoints[0], mirrorTo)
	}

	reconnect := dialSessionWithQuery(t, ctx, srv.URL, "ts3-poc", key.ID, secret, "target_bot=bot-test")
	defer reconnect.Close(websocket.StatusNormalClosure, "done")
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 {
		t.Fatalf("endpoints after reconnect=%+v err=%v", endpoints, err)
	}
	if !endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != mirrorTo {
		t.Fatalf("reconnect without mirror_to should keep stored target: %+v want=%q", endpoints[0], mirrorTo)
	}
}

func TestSessionNameAutoSuffixWithinOwner(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_u1", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_u1", BoundOwner: "u1", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_u1_2", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_u1_2", BoundOwner: "u1", Active: true}); err != nil {
		t.Fatal(err)
	}
	secret1, key1, err := hub.IssueAPIKey(ctx, "svc1", "u11")
	if err != nil {
		t.Fatal(err)
	}
	secret2, key2, err := hub.IssueAPIKey(ctx, "svc1", "u12")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key1.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key2.ID, "dev:personal:ou_u1_2"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	first := dialSession(t, ctx, srv.URL, "developer", key1.ID, secret1)
	defer first.Close(websocket.StatusNormalClosure, "done")
	second := dialSession(t, ctx, srv.URL, "developer", key2.ID, secret2)
	defer second.Close(websocket.StatusNormalClosure, "done")

	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, ep := range endpoints {
		if ep.Active {
			got[ep.KeyID] = ep.SessionName
		}
	}
	if got[key1.ID] != "developer" || got[key2.ID] != "developer2" {
		t.Fatalf("effective names = %+v endpoints=%+v", got, endpoints)
	}

	env := model.Envelope{ID: "suffix-rewrite", To: "workpulse#" + key2.ID, From: "developer#" + key2.ID, Body: "ok"}
	if err := writeEnvelope(ctx, second, env); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		endpoints, err = db.ListSessionEndpoints(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, ep := range endpoints {
			if ep.KeyID == key2.ID && ep.SessionName == "developer2" && ep.Active {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("rewritten effective session missing: %+v", endpoints)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionNameReconnectKeepsNameAndReleaseReuses(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_u1", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_u1", BoundOwner: "u1", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:personal:ou_u1_2", BotChannelID: "dev", Type: model.ChatPersonal, FeishuID: "ou_u1_2", BoundOwner: "u1", Active: true}); err != nil {
		t.Fatal(err)
	}
	secret1, key1, err := hub.IssueAPIKey(ctx, "svc1", "u11")
	if err != nil {
		t.Fatal(err)
	}
	secret2, key2, err := hub.IssueAPIKey(ctx, "svc1", "u12")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key1.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key2.ID, "dev:personal:ou_u1_2"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	first := dialSession(t, ctx, srv.URL, "developer", key1.ID, secret1)
	reconnect := dialSession(t, ctx, srv.URL, "developer", key1.ID, secret1)
	defer reconnect.Close(websocket.StatusNormalClosure, "done")
	_ = first.Close(websocket.StatusNormalClosure, "replaced")
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	activeByKey := map[string]string{}
	for _, ep := range endpoints {
		if ep.Active {
			activeByKey[ep.KeyID] = ep.SessionName
		}
	}
	if activeByKey[key1.ID] != "developer" {
		t.Fatalf("reconnect should keep developer: %+v", endpoints)
	}

	_ = reconnect.Close(websocket.StatusNormalClosure, "done")
	waitEndpointInactive(t, ctx, db, key1.ID, "developer")
	second := dialSession(t, ctx, srv.URL, "developer", key2.ID, secret2)
	defer second.Close(websocket.StatusNormalClosure, "done")
	endpoints, err = db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range endpoints {
		if ep.KeyID == key2.ID && ep.Active && ep.SessionName == "developer" {
			return
		}
	}
	t.Fatalf("released name was not reused: %+v", endpoints)
}

func TestSessionEndpointOwnerActiveUniqueIndex(t *testing.T) {
	_, db, ctx := newTestHub(t)
	now := time.Now()
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-1", SessionName: "developer", OwnerKey: "u1", LastSeenAt: now, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-2", SessionName: "developer", OwnerKey: "u1", LastSeenAt: now, Active: true}); err == nil {
		t.Fatal("expected active owner/session unique violation")
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-1", SessionName: "developer", OwnerKey: "u1", LastSeenAt: now, Active: false}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-2", SessionName: "developer", OwnerKey: "u1", LastSeenAt: now, Active: true}); err != nil {
		t.Fatalf("name should be reusable after inactive: %v", err)
	}
}

func TestOnlineDirectoryBroadcastOwnerIsolationAndUnknownMetadata(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = 30 * time.Millisecond
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u2", DisplayName: "UserTwo", FeishuOpenID: "ou_u2", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	u1Secret, u1Key, err := hub.IssueAPIKey(ctx, "svc1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	u2Secret, u2Key, err := hub.IssueAPIKey(ctx, "svc1", "u2")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, u1Key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, u2Key.ID, "dev:personal:ou_u2"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u1Dev := dialSessionWithQuery(t, ctx, srv.URL, "developer", u1Key.ID, u1Secret, "tool=CODEX&model=gpt-5.5&full_session_name=sh-developer-e0d12642")
	defer u1Dev.Close(websocket.StatusNormalClosure, "done")
	u1Home := dialSession(t, ctx, srv.URL, "home", u1Key.ID, u1Secret)
	defer u1Home.Close(websocket.StatusNormalClosure, "done")
	u2Dev := dialSessionWithQuery(t, ctx, srv.URL, "developer", u2Key.ID, u2Secret, "tool=CLAUDE&model=opus&full_session_name=sh-developer-u2")
	defer u2Dev.Close(websocket.StatusNormalClosure, "done")
	u1SecOps := dialSessionWithQuery(t, ctx, srv.URL, "sec-ops", u1Key.ID, u1Secret, "tool=SEC&model=monitor&no_directory=1")
	defer u1SecOps.Close(websocket.StatusNormalClosure, "done")

	u1DevMsg := readEnvelope(t, ctx, u1Dev)
	u1HomeMsg := readEnvelope(t, ctx, u1Home)
	u2Msg := readEnvelope(t, ctx, u2Dev)
	if !strings.Contains(u1DevMsg.Body, "sh-developer-e0d12642") || !strings.Contains(u1DevMsg.Body, "home") {
		t.Fatalf("u1 directory missing own sessions: %s", u1DevMsg.Body)
	}
	if strings.Contains(u1DevMsg.Body, "sec-ops") || strings.Contains(u1HomeMsg.Body, "sec-ops") {
		t.Fatalf("no_directory session leaked into directory: %s / %s", u1DevMsg.Body, u1HomeMsg.Body)
	}
	if strings.Contains(u1DevMsg.Body, "u2") || strings.Contains(u1HomeMsg.Body, "u2") {
		t.Fatalf("u1 directory leaked u2: %s / %s", u1DevMsg.Body, u1HomeMsg.Body)
	}
	if !strings.Contains(u2Msg.Body, "sh-developer-u2") || strings.Contains(u2Msg.Body, "ou_u1") || strings.Contains(u2Msg.Body, "home") {
		t.Fatalf("u2 directory wrong/leaked: %s", u2Msg.Body)
	}
	for _, want := range []string{"【DingWei在线清单】", "#developer · CODEX/gpt-5.5", "· 全名:sh-developer-e0d12642", "@u1#developer", "(末"} {
		if !strings.Contains(u1DevMsg.Body, want) {
			t.Fatalf("u1 directory missing %q: %s", want, u1DevMsg.Body)
		}
	}
	if !strings.Contains(u1DevMsg.Body, "#home · 未知/未知") {
		t.Fatalf("old session metadata should render unknown: %s", u1DevMsg.Body)
	}
	result, err := hub.Dispatch(ctx, model.Message{
		ID:           "hidden-direct",
		ChatEntityID: "dev:personal:ou_u1",
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#sec-ops 直接派活"}`,
	}, "#sec-ops 直接派活")
	if err != nil || !result.Matched {
		t.Fatalf("direct hidden session dispatch result=%+v err=%v", result, err)
	}
	hidden := readEnvelope(t, ctx, u1SecOps)
	if hidden.To != "sec-ops#"+u1Key.ID || hidden.Body != "直接派活" {
		t.Fatalf("hidden session should still be addressable: %+v", hidden)
	}
	for i := 0; i < 2; i++ {
		msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
		if err != nil || msg == nil {
			t.Fatalf("dm %d missing msg=%+v err=%v", i, msg, err)
		}
		if msg.ChatEntityID != "dev:personal:ou_u1" && msg.ChatEntityID != "dev:personal:ou_u2" {
			t.Fatalf("unexpected dm target: %+v", msg)
		}
	}
}

func TestOnlineDirectoryDebouncesFlapping(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = 80 * time.Millisecond
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	time.Sleep(20 * time.Millisecond)
	dev := dialSession(t, ctx, srv.URL, "sh-developer-e0d12642", key.ID, secret)
	defer dev.Close(websocket.StatusNormalClosure, "done")

	gotHome := readEnvelope(t, ctx, home)
	gotDev := readEnvelope(t, ctx, dev)
	if strings.Count(gotHome.Body, "\n1. #") != 1 || strings.Count(gotHome.Body, "\n2. #") != 1 || strings.Count(gotDev.Body, "\n1. #") != 1 || strings.Count(gotDev.Body, "\n2. #") != 1 {
		t.Fatalf("debounced directory should contain final two sessions:\nhome=%s\ndev=%s", gotHome.Body, gotDev.Body)
	}
	assertNoEnvelope(t, ctx, home, 120*time.Millisecond)
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil || msg.ChatEntityID != "dev:personal:ou_u1" {
		t.Fatalf("dm missing after debounce msg=%+v err=%v", msg, err)
	}
	msg, err = db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg != nil {
		t.Fatalf("debounce should enqueue one dm, got msg=%+v err=%v", msg, err)
	}
}

func TestOnlineDirectorySkipsNoDirectoryPresenceBroadcast(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-hidden", "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := db.BindAPIKeyAccount(ctx, "FB-visible", "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-hidden", SessionName: "sec-ops", OwnerKey: "u1", LastSeenAt: time.Now(), Active: true, NoDirectory: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSessionEndpoint(ctx, model.SessionEndpoint{KeyID: "FB-visible", SessionName: "developer", OwnerKey: "u1", LastSeenAt: time.Now(), Active: true}); err != nil {
		t.Fatal(err)
	}
	hub.scheduleOnlineBroadcastForSession(ctx, "FB-hidden", "sec-ops")
	hub.mu.Lock()
	hiddenTimer := hub.onlineTimers["u1"]
	hub.mu.Unlock()
	if hiddenTimer != nil {
		hiddenTimer.Stop()
		t.Fatal("no_directory presence should not schedule online directory broadcast")
	}
	hub.scheduleOnlineBroadcastForSession(ctx, "FB-visible", "developer")
	hub.mu.Lock()
	visibleTimer := hub.onlineTimers["u1"]
	hub.mu.Unlock()
	if visibleTimer == nil {
		t.Fatal("visible presence should schedule online directory broadcast")
	}
	visibleTimer.Stop()
}

func TestOnlineDirectoryBroadcastMarksSingleMirrorPrimary(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "alice", DisplayName: "Alice", FeishuOpenID: "ou_alice", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")
	developer := dialSession(t, ctx, srv.URL, "developer", key.ID, secret)
	defer developer.Close(websocket.StatusNormalClosure, "done")
	review := dialSession(t, ctx, srv.URL, "review", key.ID, secret)
	defer review.Close(websocket.StatusNormalClosure, "done")

	if err := hub.BroadcastOnlineDirectories(ctx, false); err != nil {
		t.Fatal(err)
	}
	envs := []model.Envelope{
		readEnvelope(t, ctx, home),
		readEnvelope(t, ctx, developer),
		readEnvelope(t, ctx, review),
	}
	keys := map[string]bool{}
	primaries := 0
	for _, env := range envs {
		if env.Meta["type"] != "online_directory" {
			t.Fatalf("unexpected broadcast meta: %+v", env.Meta)
		}
		key, _ := env.Meta["broadcast_dedup_key"].(string)
		if key == "" {
			t.Fatalf("missing broadcast_dedup_key: %+v", env.Meta)
		}
		keys[key] = true
		if primary, _ := env.Meta["mirror_primary"].(bool); primary {
			primaries++
		}
	}
	if len(keys) != 1 || primaries != 1 {
		t.Fatalf("broadcast mirror metadata keys=%v primaries=%d envs=%+v", keys, primaries, envs)
	}
}

func TestProducerSessionMetadataAndOnlineDirectory(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "system-v-task-internal", DisplayName: "SYSTEM-V-TASK-INTERNAL", Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "system")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "system-v-task-internal"); err != nil {
		t.Fatal(err)
	}
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	producer := dialSessionWithQuery(t, ctx, srv.URL, "producer", key.ID, secret, "producer=1&target_group=oc_ai")
	defer producer.Close(websocket.StatusNormalClosure, "done")

	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].Producer || endpoints[0].TargetGroup != "oc_ai" {
		t.Fatalf("producer endpoint=%+v err=%v", endpoints, err)
	}
	if err := hub.BroadcastOnlineDirectories(ctx, true); err != nil {
		t.Fatal(err)
	}
	got := readEnvelope(t, ctx, producer)
	if !strings.Contains(got.Body, "Producer:是→oc_ai") {
		t.Fatalf("producer directory missing marker: %s", got.Body)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg != nil {
		t.Fatalf("system producer directory should not DM real user msg=%+v err=%v", msg, err)
	}
}

func TestProducerSessionTargetBotQueryRoutesGroup(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("bot-test", "bot-test")
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "system-v-task-internal", DisplayName: "SYSTEM-V-TASK-INTERNAL", Role: model.RoleSystem, Active: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "system")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "system-v-task-internal"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	producer := dialSessionWithQuery(t, ctx, srv.URL, "producer", key.ID, secret, "producer=1&target_group=oc_alert&target_bot=bot-test")
	defer producer.Close(websocket.StatusNormalClosure, "done")

	if err := writeEnvelope(ctx, producer, model.Envelope{
		ID:   "producer-query-target-bot",
		To:   "oc_alert#" + key.ID + "#UnifiedRobot",
		From: "producer#" + key.ID,
		Body: "告警",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"producer": true, "target_group": "oc_alert", "no_mirror": true},
	}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, ctx, hub.Outbound.(*bus.DBQueue))
	if msg.BotChannelID != "bot-test" || msg.ChatEntityID != "bot-test:group:oc_alert" {
		t.Fatalf("producer query target_bot msg=%+v", msg)
	}
}

func TestClientIPFromRequestTrustsOnlyLoopbackProxy(t *testing.T) {
	trusted := httptest.NewRequest("GET", "/ws/session/home", nil)
	trusted.RemoteAddr = "127.0.0.1:1234"
	trusted.Header.Set("X-Forwarded-For", "203.0.113.8, 10.0.0.2")
	if got := clientIPFromRequest(trusted); got != "203.0.113.8" {
		t.Fatalf("trusted proxy client ip=%q", got)
	}
	untrusted := httptest.NewRequest("GET", "/ws/session/home", nil)
	untrusted.RemoteAddr = "198.51.100.10:5678"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.8")
	if got := clientIPFromRequest(untrusted); got != "198.51.100.10" {
		t.Fatalf("untrusted proxy client ip=%q", got)
	}
}

func TestSessionWildcardRouteDeliversEnvelope(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "tenant1", Name: "tenant", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "tenant1", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	sessionService := sessionServiceID(key.ID, "home")
	if err := db.UpsertRegisteredService(ctx, model.RegisteredService{ID: sessionService, Name: "会话 home", DeliveryType: "session", ReplyMode: "none", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.AddPrefixRule(ctx, model.RoutingRule{ID: "sr1", ServiceID: sessionService, MatchExpr: "dev*", StripPrefix: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{ID: "m-wild", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal, Content: `{"text":"dev hello"}`}, "dev hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || result.Reply != "" {
		t.Fatalf("dispatch result=%+v", result)
	}
	got := readEnvelope(t, ctx, home)
	if got.To != "home#"+key.ID || got.Body != "hello" || got.Meta["route_id"] != "sr1" {
		t.Fatalf("wildcard envelope=%+v", got)
	}
}

func TestMirrorCommandDMControlsSessionAndGroupIgnored(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	hub.RegisterBot("dev", "UnifiedRobot")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")

	result, err := hub.Dispatch(ctx, model.Message{ID: "m1", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "mirror on home")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "镜像已开启") {
		t.Fatalf("mirror on result = %+v", result)
	}
	got := readEnvelope(t, ctx, home)
	if got.To != "home#"+key.ID || got.From != "workpulse#"+key.ID || got.Meta["type"] != "mirror_control" || got.Meta["enabled"] != true {
		t.Fatalf("mirror on envelope = %+v", got)
	}
	if got.Meta["mirror_to"] != "alice#"+key.ID+"#UnifiedRobot" {
		t.Fatalf("mirror target = %+v", got.Meta["mirror_to"])
	}
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil || len(endpoints) != 1 || !endpoints[0].MirrorEnabled {
		t.Fatalf("mirror state after on endpoints=%+v err=%v", endpoints, err)
	}

	groupResult, err := hub.Dispatch(ctx, model.Message{ID: "m2", ChatEntityID: "dev:group:oc_group", BotChannelID: "dev", ChatType: model.ChatGroup, SenderOpenID: "alice"}, "mirror off home")
	if err != nil {
		t.Fatal(err)
	}
	if !groupResult.Matched || groupResult.Reply != "" {
		t.Fatalf("group mirror result = %+v", groupResult)
	}
	endpoints, _ = db.ListSessionEndpoints(ctx)
	if !endpoints[0].MirrorEnabled {
		t.Fatalf("group mirror command changed state: %+v", endpoints[0])
	}

	result, err = hub.Dispatch(ctx, model.Message{ID: "m3", ChatEntityID: "dev:personal:alice", BotChannelID: "dev", ChatType: model.ChatPersonal}, "mirror off home")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Matched || !strings.Contains(result.Reply, "镜像已关闭") {
		t.Fatalf("mirror off result = %+v", result)
	}
	got = readEnvelope(t, ctx, home)
	if got.Meta["type"] != "mirror_control" || got.Meta["enabled"] != false || got.Meta["mirror_to"] != "" {
		t.Fatalf("mirror off envelope = %+v", got)
	}
	endpoints, _ = db.ListSessionEndpoints(ctx)
	if endpoints[0].MirrorEnabled || endpoints[0].MirrorTo != "" {
		t.Fatalf("mirror state after off: %+v", endpoints[0])
	}
}

func TestParseSelectorRejectsHashSpace(t *testing.T) {
	if name, _, ok := parseSelector("#home hello"); !ok || name != "home" {
		t.Fatalf("#home selector not parsed: name=%q ok=%v", name, ok)
	}
	if name, body, ok := parseSelector("@_user_1 #home hello"); !ok || name != "home" || body != "hello" {
		t.Fatalf("@ placeholder selector not parsed: name=%q body=%q ok=%v", name, body, ok)
	}
	if _, _, ok := parseSelector("# home hello"); ok {
		t.Fatalf("# followed by space should not be parsed as selector")
	}
	if action, session, ok := parseMirrorCommand("mirror on home"); !ok || action != "on" || session != "home" {
		t.Fatalf("mirror command not parsed: action=%q session=%q ok=%v", action, session, ok)
	}
	hub, _, _ := newTestHub(t)
	hub.RegisterBot("dev", "UnifiedRobot")
	if name, body, ok := hub.parseSelector("dev", "@UnifiedRobot #home hello"); !ok || name != "home" || body != "hello" {
		t.Fatalf("literal bot mention selector not parsed: name=%q body=%q ok=%v", name, body, ok)
	}
}

func TestRouteToSessionAcksDeliveredInboundMessage(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:alice"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	home := dialSession(t, ctx, srv.URL, "home", key.ID, secret)
	defer home.Close(websocket.StatusNormalClosure, "done")

	if err := db.EnqueueMessage(ctx, model.Message{
		ID:           "fm-delivered",
		ChatEntityID: "dev:personal:alice",
		Direction:    model.DirectionIn,
		BotChannelID: "dev",
		ChatType:     model.ChatPersonal,
		Content:      `{"text":"#home ping"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "fm-delivered",
		To:   "home#" + key.ID,
		From: "alice#" + key.ID + "#UnifiedRobot",
		Body: "ping",
		TS:   time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	got := readEnvelope(t, ctx, home)
	if got.ID != "fm-delivered" || got.Body != "ping" {
		t.Fatalf("delivered envelope = %+v", got)
	}
	next, err := db.ClaimNextMessage(ctx, model.DirectionIn)
	if err != nil {
		t.Fatalf("ClaimNextMessage: %v", err)
	}
	if next != nil {
		t.Fatalf("delivered inbound message was re-claimable: %+v", next)
	}
}

func TestNoMirrorSystemEnvelopeSkipsFeishuMirrorButNormalMirrors(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("bot-test", "bot-test")
	keyID := "FB-test"
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "sys-online",
		To:   "ou_alice#" + keyID + "#UnifiedRobot",
		From: "home#" + keyID,
		Body: "在线清单",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"type": "online_directory", "system": true, "no_mirror": true},
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg != nil {
		t.Fatalf("system no_mirror should not enqueue mirror msg=%+v err=%v", msg, err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "normal-mirror",
		To:   "ou_alice#" + keyID + "#UnifiedRobot",
		From: "home#" + keyID,
		Body: "普通 AI 输出",
		TS:   time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	msg, err = db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("normal mirror missing msg=%+v err=%v", msg, err)
	}
	if msg.ChatEntityID != "dev:personal:ou_alice" || !strings.Contains(msg.Content, "普通 AI 输出") {
		t.Fatalf("normal mirror msg=%+v", msg)
	}
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "bot-test:group:oc_group", BotChannelID: "bot-test", Type: model.ChatGroup, FeishuID: "oc_group", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "producer-group",
		To:   "oc_group#" + keyID + "#UnifiedRobot",
		From: "producer#" + keyID,
		Body: "系统任务产出",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"producer": true, "target_group": "oc_group", "no_mirror": true},
	}); err != nil {
		t.Fatal(err)
	}
	msg, err = db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("producer group msg missing msg=%+v err=%v", msg, err)
	}
	if msg.BotChannelID != "bot-test" || msg.ChatEntityID != "bot-test:group:oc_group" || !strings.Contains(msg.Content, "系统任务产出") {
		t.Fatalf("producer group msg=%+v", msg)
	}
}

func TestProducerTargetBotOverrideAndUnknownGroupDoesNotDefault(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	hub.RegisterBot("bot-test", "bot-test")
	keyID := "FB-test"
	if err := db.UpsertChatEntity(ctx, model.ChatEntity{ID: "dev:group:oc_alert", BotChannelID: "dev", Type: model.ChatGroup, FeishuID: "oc_alert", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "producer-override",
		To:   "oc_alert#" + keyID + "#UnifiedRobot",
		From: "producer#" + keyID,
		Body: "告警",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"producer": true, "target_group": "oc_alert", "target_bot": "bot-test", "no_mirror": true},
	}); err != nil {
		t.Fatal(err)
	}
	msg, err := db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg == nil {
		t.Fatalf("producer override missing msg=%+v err=%v", msg, err)
	}
	if msg.BotChannelID != "bot-test" || msg.ChatEntityID != "bot-test:group:oc_alert" {
		t.Fatalf("producer override msg=%+v", msg)
	}
	if err := hub.RouteEnvelope(ctx, model.Envelope{
		ID:   "producer-unknown",
		To:   "oc_unknown#" + keyID + "#UnifiedRobot",
		From: "producer#" + keyID,
		Body: "未知群",
		TS:   time.Now().Unix(),
		Meta: map[string]any{"producer": true, "target_group": "oc_unknown", "no_mirror": true},
	}); err == nil {
		t.Fatal("unknown producer target group should fail instead of defaulting to UnifiedRobot")
	}
	msg, err = db.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || msg != nil {
		t.Fatalf("unknown producer target should not enqueue msg=%+v err=%v", msg, err)
	}
}

func dialSession(t *testing.T, ctx context.Context, baseURL, sessionName, keyID, secret string) *websocket.Conn {
	t.Helper()
	return dialSessionWithHeader(t, ctx, baseURL, sessionName, keyID, secret, nil)
}

func dialSessionWithHeader(t *testing.T, ctx context.Context, baseURL, sessionName, keyID, secret string, header http.Header) *websocket.Conn {
	t.Helper()
	conn := dialSessionWithHeaderRaw(t, ctx, baseURL, sessionName, keyID, secret, header)
	discardInitialAgentNetworkSkill(t, ctx, conn)
	return conn
}

func dialSessionWithHeaderRaw(t *testing.T, ctx context.Context, baseURL, sessionName, keyID, secret string, header http.Header) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws/session/" + sessionName + "?key_id=" + keyID
	if header == nil {
		header = http.Header{}
	}
	header.Set("Authorization", "Bearer "+secret)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("Dial %s: %v", sessionName, err)
	}
	return conn
}

func dialSessionWithQuery(t *testing.T, ctx context.Context, baseURL, sessionName, keyID, secret, query string) *websocket.Conn {
	t.Helper()
	conn := dialSessionWithQueryRaw(t, ctx, baseURL, sessionName, keyID, secret, query)
	discardInitialAgentNetworkSkill(t, ctx, conn)
	return conn
}

func dialSessionWithQueryRaw(t *testing.T, ctx context.Context, baseURL, sessionName, keyID, secret, query string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws/session/" + sessionName + "?key_id=" + keyID
	if strings.TrimSpace(query) != "" {
		wsURL += "&" + query
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+secret)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("Dial %s: %v", sessionName, err)
	}
	return conn
}

func discardInitialAgentNetworkSkill(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	_, data, err := conn.Read(readCtx)
	if err != nil {
		return
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode initial envelope: %v", err)
	}
	if env.Meta["type"] != agentNetworkSkillPushType {
		t.Fatalf("expected initial agent network skill push, got %+v", env)
	}
}

func writeEnvelope(ctx context.Context, conn *websocket.Conn, env model.Envelope) error {
	payload, _ := json.Marshal(env)
	return conn.Write(ctx, websocket.MessageText, payload)
}

func readEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn) model.Envelope {
	t.Helper()
	for {
		env := readEnvelopeIncludingSystem(t, ctx, conn)
		if env.Meta["type"] != agentNetworkSkillPushType {
			return env
		}
	}
}

func readEnvelopeIncludingSystem(t *testing.T, ctx context.Context, conn *websocket.Conn) model.Envelope {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}

func assertNoEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn, wait time.Duration) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	_, data, err := conn.Read(readCtx)
	if err == nil {
		var env model.Envelope
		if err := json.Unmarshal(data, &env); err == nil && env.Meta["type"] == agentNetworkSkillPushType {
			return
		}
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func waitEndpointInactive(t *testing.T, ctx context.Context, db *store.SQLite, keyID, sessionName string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		endpoints, err := db.ListSessionEndpoints(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, ep := range endpoints {
			if ep.KeyID == keyID && ep.SessionName == sessionName && !ep.Active {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("endpoint did not become inactive key=%s session=%s endpoints=%+v", keyID, sessionName, endpoints)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func waitSessionOnline(t *testing.T, hub *Hub, keyID, sessionName string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if hub.sessionOnline(keyID, sessionName) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not become online key=%s session=%s", keyID, sessionName)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertEnvelopeDoesNotContain(t *testing.T, env model.Envelope, secret string) {
	t.Helper()
	payload, _ := json.Marshal(env)
	if strings.Contains(string(payload), secret) {
		t.Fatalf("envelope leaked secret %q: %s", secret, payload)
	}
}

func messageText(t *testing.T, msg *model.Message) string {
	t.Helper()
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(msg.Content), &payload); err != nil {
		t.Fatalf("unmarshal message content: %v content=%q", err, msg.Content)
	}
	return payload.Text
}

func waitOutbound(t *testing.T, ctx context.Context, q *bus.DBQueue) *model.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msg, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("outbound dequeue: %v", err)
		}
		if msg != nil {
			return msg
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}
