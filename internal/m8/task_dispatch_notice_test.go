package m8

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

func r10NoticeEnv(t *testing.T) (*agentEnv, *store.SQLite, *td.Service, td.Mutation) {
	t.Helper()
	t.Setenv("DW_TASK_DISPATCH_LOCK_ENABLED", "1")
	e := newAgentEnv(t)
	db := e.hub.Repo.(*store.SQLite)
	group, err := e.svc.CreateGroup(e.ctx, "zzc", e.agID, "R10notice")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.hub.syncNewPlatformUser(e.ctx, "zzc", nil); err != nil {
		t.Fatal(err)
	}
	if err = db.SetSpecReadBinding(e.ctx, "admin", specread.Binding{Principal: "zzc", Provider: "github", Instance: "github.com", AccountID: "123", Login: "zzc"}); err != nil {
		t.Fatal(err)
	}
	e.hub.SpecReader = &specread.Service{Bindings: db, Repositories: map[string]bool{"github:github.com:1": true}, Providers: map[string]specread.Provider{"github:github.com": &r5HTTPProvider{allow: true}}}
	m := td.Mutation{OperationID: "notification_grant_0001", Kind: "grant", Key: td.Key{Provider: "github", Instance: "github.com", Repository: "1", ChangeDir: "openspec/changes/example", TaskID: "T-01"}, ActorKind: "user", ActorUserLC: "zzc", HolderAgentID: e.agID, HolderOwnerLC: "zzc", AssignedByUserLC: "zzc", AuthorizationGroup: group.Code, CommitSHA: strings.Repeat("a", 40), TasksBlobSHA: strings.Repeat("b", 40), TaskLine: 1, MonitorRef: "main", PRDimension: "pr_not_opened", ComparisonState: "clean", ObservedCommitSHA: strings.Repeat("a", 40), Bindings: []td.BindingFact{{Principal: "zzc", Provider: "github", Instance: "github.com", AccountID: "123", Login: "zzc"}}}
	return e, db, e.hub.taskDispatchService(), m
}

type r10NoticePageRepo struct {
	td.Repository
	visited map[string]bool
	pages   int
}

func (r *r10NoticePageRepo) TaskDispatchLock(ctx context.Context, k td.Key) (*td.Lock, error) {
	r.visited[k.TaskID] = true
	return r.Repository.TaskDispatchLock(ctx, k)
}
func (r *r10NoticePageRepo) TaskDispatchNoticesAfter(ctx context.Context, state string, limit int, at time.Time, id string) ([]td.Notice, error) {
	r.pages++
	return r.Repository.TaskDispatchNoticesAfter(ctx, state, limit, at, id)
}
func TestR10OfflineFirstThousandNoticesCannotStarveLaterPending(t *testing.T) {
	e, db, _, m := r10NoticeEnv(t)
	for i := 0; i < 1001; i++ {
		attempt := m
		attempt.OperationID = fmt.Sprintf("pending_notice_%04d_operation", i)
		attempt.Key.TaskID = fmt.Sprintf("T-%04d", i)
		if _, err := db.ApplyTaskDispatch(e.ctx, attempt, e.hub.now, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	wrapped := &r10NoticePageRepo{Repository: db, visited: map[string]bool{}}
	wrapped.Repository = db
	// Explicit field names avoid conflating the old broad repository with the new
	// optional capability. No identity, source or application behavior is faked.
	e.hub.Repo = &r10NoticePageBoth{Repository: db, dispatch: wrapped}
	svc := e.hub.taskDispatchService()
	e.hub.dispatchTaskLockNotices(e.ctx, svc)
	if len(wrapped.visited) != 1001 || wrapped.pages < 3 {
		t.Fatal("R10_OFFLINE_PENDING_NOTICE_STARVATION", len(wrapped.visited), wrapped.pages)
	}
	pending, err := db.TaskDispatchNotices(e.ctx, "pending", 1000)
	if err != nil || len(pending) != 1000 {
		t.Fatal("R10_OFFLINE_NOTICE_LOST", len(pending), err)
	}
	last := pending[len(pending)-1]
	tail, err := db.TaskDispatchNoticesAfter(e.ctx, "pending", 1000, last.CreatedAt, last.ID)
	if err != nil || len(tail) != 1 {
		t.Fatal("R10_OFFLINE_TAIL_NOTICE_LOST", len(tail), err)
	}
}

// The old/new interfaces are embedded through named adapters because both have
// the type name Repository. Every behavior delegates to the real SQLite store.
type r10NoticePageBoth struct {
	store.Repository
	dispatch *r10NoticePageRepo
}

func (r *r10NoticePageBoth) TaskDispatchLock(c context.Context, k td.Key) (*td.Lock, error) {
	return r.dispatch.TaskDispatchLock(c, k)
}
func (r *r10NoticePageBoth) TaskDispatchActive(c context.Context) ([]td.Lock, error) {
	return r.dispatch.Repository.TaskDispatchActive(c)
}
func (r *r10NoticePageBoth) TaskDispatchEvents(c context.Context, k td.Key, a int64) ([]td.Event, error) {
	return r.dispatch.Repository.TaskDispatchEvents(c, k, a)
}
func (r *r10NoticePageBoth) TaskDispatchNotices(c context.Context, s string, l int) ([]td.Notice, error) {
	return r.dispatch.Repository.TaskDispatchNotices(c, s, l)
}
func (r *r10NoticePageBoth) TaskDispatchNoticesAfter(c context.Context, s string, l int, a time.Time, id string) ([]td.Notice, error) {
	return r.dispatch.TaskDispatchNoticesAfter(c, s, l, a, id)
}
func (r *r10NoticePageBoth) TaskDispatchNotice(c context.Context, id string) (*td.Notice, error) {
	return r.dispatch.Repository.TaskDispatchNotice(c, id)
}
func (r *r10NoticePageBoth) TaskDispatchOperation(c context.Context, k td.Key, id string) (*td.Operation, error) {
	return r.dispatch.Repository.TaskDispatchOperation(c, k, id)
}
func (r *r10NoticePageBoth) ApplyTaskDispatch(c context.Context, m td.Mutation, n func() time.Time, check func() error) (td.Result, error) {
	return r.dispatch.Repository.ApplyTaskDispatch(c, m, n, check)
}
func (r *r10NoticePageBoth) UpdateTaskDispatchNotice(c context.Context, id, old, next, delivery, reason string, at time.Time) (bool, error) {
	return r.dispatch.Repository.UpdateTaskDispatchNotice(c, id, old, next, delivery, reason, at)
}
func TestR10NoticeReliableFrameIsQueuedNotOpenedAndOldHelperWaits(t *testing.T) {
	e, db, svc, m := r10NoticeEnv(t)
	if _, err := db.ApplyTaskDispatch(e.ctx, m, e.hub.now, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	e.hub.dispatchTaskLockNotices(e.ctx, svc)
	pending, _ := db.TaskDispatchNotices(e.ctx, "pending", 100)
	if len(pending) != 1 {
		t.Fatal("R10_OFFLINE_DELIVERY_LOST")
	}
	conn, status, _ := e.dial(e.cred, "")
	if status != 101 {
		t.Fatal(status)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(e.ctx, 8*time.Second)
	defer cancel()
	capability := func(taskFlow bool) {
		t.Helper()
		if err := writeEnvelope(ctx, conn, model.Envelope{To: "workpulse#" + AgentKeyID("zzc"), Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true, "task_flow_v1": taskFlow}}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			e.hub.mu.Lock()
			c := e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]
			e.hub.mu.Unlock()
			if c != nil {
				c.stateMu.Lock()
				ok := c.deliveryACKV1 && c.taskFlowV1 == taskFlow
				c.stateMu.Unlock()
				if ok {
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("R10_CAPABILITY_NEGOTIATION_TIMEOUT")
	}
	capability(false)
	e.hub.dispatchTaskLockNotices(ctx, svc)
	pending, _ = db.TaskDispatchNotices(ctx, "pending", 100)
	if len(pending) != 1 {
		t.Fatal("R10_UNKNOWN_TYPE_SENT_TO_OLD_HELPER")
	}
	capability(true)
	e.hub.dispatchTaskLockNotices(ctx, svc)
	var delivery model.Envelope
	for {
		delivery = readEnvelopeIncludingSystem(t, ctx, conn)
		if metaString(delivery.Meta, "kind") == "r10_task_notice" {
			break
		}
	}
	if metaString(delivery.Meta, "type") != "delivery" || metaString(delivery.Meta, "delivery_id") == "" || !strings.Contains(delivery.Body, "未开PR") || !strings.Contains(delivery.Body, "github:github.com:1") {
		t.Fatal("R10_NOTICE_NOT_RELIABLE_OR_COORDINATE_MISSING")
	}
	queued, err := db.TaskDispatchNotices(ctx, "queued", 100)
	if err != nil || len(queued) != 1 || queued[0].Reason != "hub_queued_not_opened" {
		t.Fatal("R10_QUEUED_FALSE_OPENED", len(queued), err)
	}
	events, err := db.TaskDispatchEvents(ctx, m.Key, 0)
	if err != nil || len(events) != 1 || events[0].Type != "grant" {
		t.Fatal("R10_NOTICE_CREATED_EXECUTION_OR_COMPLETION_FACT", len(events), err)
	}
}
func TestR10LateNoticeCannotCrossHandoffGeneration(t *testing.T) {
	e, db, _, m := r10NoticeEnv(t)
	granted, err := db.ApplyTaskDispatch(e.ctx, m, e.hub.now, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	notices, _ := db.TaskDispatchNotices(e.ctx, "pending", 100)
	n := notices[0]
	if ok, err := db.UpdateTaskDispatchNotice(e.ctx, n.ID, "pending", "attempting", "", "", e.hub.now()); err != nil || !ok {
		t.Fatal(err)
	}
	release := m
	release.OperationID = "release_for_late_notice_1"
	release.Kind = "release"
	release.ActorKind = "agent"
	release.ActorUserLC = ""
	release.ActorAgentID = e.agID
	release.ActorOwnerLC = "zzc"
	release.ExpectedGeneration = granted.Lock.Generation
	release.ExpectedToken = granted.Lock.LeaseToken
	release.Handoff = td.Handoff{Commit: m.CommitSHA, BlobSHA: m.TasksBlobSHA, Path: m.Key.ChangeDir + "/handoff.md", ExecutionCommit: m.CommitSHA, TaskID: m.Key.TaskID, RecoveryState: "stopped"}
	if _, err = db.ApplyTaskDispatch(e.ctx, release, e.hub.now, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	env := model.Envelope{ID: n.SourceEnvelopeID, Meta: map[string]any{"kind": "r10_task_notice", "r10_notice_id": n.ID}}
	err = e.hub.RouteEnvelope(context.WithValue(e.ctx, taskDispatchContextKey{}, n.ID), env)
	if !errors.Is(err, td.ErrConflict) {
		t.Fatal("R10_STALE_GENERATION_NOTICE_ROUTED", err)
	}
}
