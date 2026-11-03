package taskdispatch_test

import (
	"context"
	"encoding/json"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
	"strings"
	"testing"
	"time"
)

func TestR10CrossRepositoryBatchDoesNotHidePartialFailure(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	a, b := e.r, e.r
	a.OperationID = "batch_repo_a_operation_1"
	b.OperationID = "batch_repo_b_operation_1"
	b.Key.Repository = "2"
	e.p.deniedRepo = map[string]map[string]bool{"2": {"holder": true}}
	result, err := e.s.ExecuteBatch(ctx, td.Actor{UserLC: "dispatcher"}, []td.Request{a, b})
	if err != nil || !result.Partial || result.Succeeded != 1 || result.Failed != 1 || len(result.Items) != 2 || result.Items[0].Result == nil || result.Items[1].Result != nil || result.Items[1].Error == "" {
		t.Fatal("R10_BATCH_PARTIAL_SUCCESS_HIDDEN", result, err)
	}
	if l, err := e.db.TaskDispatchLock(ctx, b.Key); err != nil || l != nil {
		t.Fatal("R10_A_PERMISSION_INHERITED_IN_B", l, err)
	}
	if l, err := e.db.TaskDispatchLock(ctx, a.Key); err != nil || l == nil || l.Repository != "1" {
		t.Fatal("R10_B_FAILURE_ROLLED_BACK_A", l, err)
	}
	bytes, _ := json.Marshal(result)
	if strings.Contains(string(bytes), "original") {
		t.Fatal("R10_BATCH_SOURCE_BODY_LEAK")
	}
}
func TestR10UnknownNoticeAfterRestartCannotCreateSecondGrant(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	l := e.grant(t)
	notices, err := e.db.TaskDispatchNotices(ctx, "pending", 1000)
	if err != nil || len(notices) != 1 {
		t.Fatal(err, len(notices))
	}
	id := notices[0].ID
	ok, err := e.db.UpdateTaskDispatchNotice(ctx, id, "pending", "attempting", "", "", e.now)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err = e.s.RecoverNotices(ctx); err != nil {
		t.Fatal(err)
	}
	notice, err := e.db.TaskDispatchNotice(ctx, id)
	if err != nil || notice.State != "unknown" || notice.SourceEnvelopeID != notices[0].SourceEnvelopeID {
		t.Fatal("R10_RESTART_NOTICE_FALSE_DELIVERED", notice, err)
	}
	current, err := e.db.TaskDispatchLock(ctx, l.Key)
	if err != nil || current.LeaseToken != l.LeaseToken || current.Generation != l.Generation {
		t.Fatal("R10_UNKNOWN_NOTICE_REGRANTED", current, err)
	}
	if ok, err = e.db.UpdateTaskDispatchNotice(ctx, id, "unknown", "available", "", "", e.now); err == nil || ok {
		t.Fatal("R10_UNKNOWN_AUTOMATICALLY_CONFIRMED")
	}
	e.now = e.now.Add(81 * time.Second)
	view, err := e.s.Inspect(ctx, td.Actor{AgentID: "agent-holder"}, l.Key, "")
	if err != nil || !view.MonitoringLag {
		t.Fatal("R10_MONITOR_STALENESS_HIDDEN", view, err)
	}
}
