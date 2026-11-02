package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

func r10DB(t *testing.T) (*SQLite, td.Mutation, time.Time) {
	t.Helper()
	s := openMigrated(t, filepath.Join(t.TempDir(), "locks.db"))
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	facts := []td.BindingFact{}
	for _, id := range []string{"dispatcher", "holder", "next"} {
		if e := s.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: id, Provider: "feishu", ProviderIssuer: "test", ProviderSubject: id, DisplayName: id, Active: true}); e != nil {
			t.Fatal(e)
		}
		b := specread.Binding{Principal: id, Provider: "github", Instance: "github.com", AccountID: "123", Login: id}
		if e := s.SetSpecReadBinding(ctx, "admin", b); e != nil {
			t.Fatal(e)
		}
		facts = append(facts, td.BindingFact{Principal: id, Provider: b.Provider, Instance: b.Instance, AccountID: b.AccountID, Login: b.Login})
	}
	m := td.Mutation{OperationID: "grant_operation_0001", Kind: "grant", Key: td.Key{Provider: "github", Instance: "github.com", Repository: "1", ChangeDir: "openspec/changes/one", TaskID: "T-01"}, ActorKind: "user", ActorUserLC: "dispatcher", HolderAgentID: "agent-1", HolderOwnerLC: "holder", AssignedByUserLC: "dispatcher", AuthorizationGroup: "group", CommitSHA: strings.Repeat("a", 40), TasksBlobSHA: strings.Repeat("b", 40), TaskLine: 3, MonitorRef: "main", PRDimension: "pr_not_opened", ComparisonState: "clean", ObservedCommitSHA: strings.Repeat("a", 40), Bindings: facts}
	return s, m, time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
}
func r10Apply(t *testing.T, s *SQLite, m td.Mutation, now time.Time) (td.Result, error) {
	t.Helper()
	calls := 0
	r, e := s.ApplyTaskDispatch(context.Background(), m, func() time.Time { calls++; return now }, func() error { return nil })
	if calls != 1 {
		t.Fatalf("R10_CLOCK_NOT_ONCE %d", calls)
	}
	return r, e
}
func r10Holder(m td.Mutation, l td.Lock, kind, id string) td.Mutation {
	m.Kind = kind
	m.OperationID = id
	m.ActorKind = "agent"
	m.ActorUserLC = ""
	m.ActorAgentID = l.HolderAgentID
	m.ActorOwnerLC = l.HolderOwnerLC
	m.ExpectedGeneration = l.Generation
	m.ExpectedToken = l.LeaseToken
	return m
}
func r10Handoff(l td.Lock) td.Handoff {
	return td.Handoff{Commit: l.CommitSHA, BlobSHA: strings.Repeat("c", 40), Path: l.ChangeDir + "/handoff.md", ExecutionCommit: l.CommitSHA, TaskID: l.TaskID, RecoveryState: "stopped"}
}

func TestR10LockConcurrent50RoundsRestartAndExpiry(t *testing.T) {
	s, m, now := r10DB(t)
	ctx := context.Background()
	for round := 0; round < 50; round++ {
		m.Key.TaskID = fmt.Sprintf("T-%02d", round)
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := []td.Result{}
		fail := []error{}
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				attempt := m
				attempt.OperationID = fmt.Sprintf("round_%03d_attempt_%03d", round, i)
				if i%2 == 1 {
					attempt.CommitSHA = strings.Repeat("d", 40)
				}
				r, e := s.ApplyTaskDispatch(ctx, attempt, func() time.Time { return now }, func() error { return nil })
				mu.Lock()
				defer mu.Unlock()
				if e == nil {
					wins = append(wins, r)
				} else if !errors.Is(e, td.ErrConflict) {
					fail = append(fail, e)
				}
			}(i)
		}
		wg.Wait()
		if len(wins) != 1 || len(fail) != 0 {
			t.Fatalf("R10_SINGLE_WINNER round=%d wins=%d unexpected=%v", round, len(wins), fail)
		}
		l := wins[0].Lock
		if !l.Effective(now) || l.Generation != 1 || l.ExpiresAt.Sub(now) != time.Hour {
			t.Fatal("R10_LEASE", l)
		}
		// Fresh connection after each race exercises durable authority, not memory.
		var seq int
		var name, path string
		if e := s.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); e != nil {
			t.Fatal(e)
		}
		reopened, e := OpenSQLite(path)
		if e != nil {
			t.Fatal(e)
		}
		persisted, e := reopened.TaskDispatchLock(ctx, m.Key)
		reopened.Close()
		if e != nil || persisted == nil || persisted.LeaseToken != l.LeaseToken {
			t.Fatal("R10_RESTART_LOST", e)
		}
		renew := r10Holder(m, l, "renew", fmt.Sprintf("renew_round_%03d_0001", round))
		if _, e = r10Apply(t, s, renew, l.ExpiresAt); !errors.Is(e, td.ErrConflict) {
			t.Fatalf("R10_EXPIRY_BOUNDARY %v", e)
		}
		expire := renew
		expire.Kind = "expire"
		expire.ActorKind = "system"
		expire.OperationID = fmt.Sprintf("expire_round_%03d_0001", round)
		expired, e := r10Apply(t, s, expire, l.ExpiresAt)
		if e != nil || expired.Lock.State != "expired" || expired.Lock.HolderAgentID != l.HolderAgentID {
			t.Fatalf("R10_EXPIRE_REASSIGNED %v %+v", e, expired)
		}
		regrant := m
		regrant.ExpectedGeneration = expired.Lock.Generation
		regrant.Handoff = r10Handoff(l)
		regrant.OperationID = fmt.Sprintf("regrant_round_%03d_0001", round)
		again, e := r10Apply(t, s, regrant, l.ExpiresAt)
		if e != nil || again.Lock.Generation <= l.Generation || again.Lock.LeaseToken == l.LeaseToken {
			t.Fatalf("R10_REGRANT %v", e)
		}
		renew.OperationID = fmt.Sprintf("stale_round_%03d_0001", round)
		if _, e = r10Apply(t, s, renew, l.ExpiresAt); !errors.Is(e, td.ErrConflict) {
			t.Fatalf("R10_OLD_TOKEN_ACCEPTED %v", e)
		}
	}
}
func TestR10LockAtomicDecisionReplayAndFailure(t *testing.T) {
	s, m, now := r10DB(t)
	ctx := context.Background()
	if _, e := s.db.Exec(`CREATE TRIGGER r10_fail_event BEFORE INSERT ON r10_task_lock_event BEGIN SELECT RAISE(ABORT,'injected write failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := r10Apply(t, s, m, now); e == nil {
		t.Fatal("R10_HALF_COMMIT")
	}
	if l, e := s.TaskDispatchLock(ctx, m.Key); e != nil || l != nil {
		t.Fatal("R10_FAILED_WRITE_GRANTED", l, e)
	}
	if _, e := s.db.Exec(`DROP TRIGGER r10_fail_event`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ApplyTaskDispatch(ctx, m, func() time.Time { return now }, func() error { return td.ErrForbidden }); !errors.Is(e, td.ErrForbidden) {
		t.Fatal(e)
	}
	if l, _ := s.TaskDispatchLock(ctx, m.Key); l != nil {
		t.Fatal("R10_FENCE_FAILED_GRANTED")
	}
	r, e := r10Apply(t, s, m, now)
	if e != nil {
		t.Fatal(e)
	}
	again, e := r10Apply(t, s, m, now.Add(time.Minute))
	if e != nil || !again.Replay || again.Lock.LeaseToken != r.Lock.LeaseToken || !again.Lock.ExpiresAt.Equal(r.Lock.ExpiresAt) {
		t.Fatal("R10_REPLAY_REGRANTED", e)
	}
	events, _ := s.TaskDispatchEvents(ctx, m.Key, 0)
	notices, _ := s.TaskDispatchNotices(ctx, "pending", 100)
	if len(events) != 1 || len(notices) != 1 {
		t.Fatalf("R10_REPLAY_DUPLICATES %d %d", len(events), len(notices))
	}
	m.CommitSHA = strings.Repeat("c", 40)
	if _, e = r10Apply(t, s, m, now); !errors.Is(e, td.ErrConflict) {
		t.Fatal("R10_OPERATION_REUSED", e)
	}
}
func TestR10LockRenewPRUnknownSyncHandoffAndBinding(t *testing.T) {
	s, m, now := r10DB(t)
	ctx := context.Background()
	r, e := r10Apply(t, s, m, now)
	if e != nil {
		t.Fatal(e)
	}
	l := r.Lock
	renew := r10Holder(m, l, "renew", "renew_operation_0001")
	r, e = r10Apply(t, s, renew, now.Add(15*time.Minute))
	if e != nil || r.Lock.ExpiresAt.Sub(now) != 75*time.Minute {
		t.Fatal("R10_NOT_OPENED_CANNOT_RENEW", e)
	}
	l = r.Lock
	submit := r10Holder(m, l, "submit", "submit_operation_0001")
	if _, e = r10Apply(t, s, submit, now.Add(16*time.Minute)); !errors.Is(e, td.ErrPRRequired) {
		t.Fatal("R10_NOT_OPENED_SUBMITTED", e)
	}
	missing := m
	missing.Key.TaskID = "T-02"
	missing.OperationID = "missing_ref_operation"
	missing.MonitorRef = ""
	if _, e = r10Apply(t, s, missing, now); !errors.Is(e, td.ErrInvalid) {
		t.Fatal("R10_MISSING_REF_GRANTED", e)
	}
	stale := renew
	stale.OperationID = "token_mutant_check_01"
	stale.ExpectedToken = strings.Repeat("0", 64)
	if _, e = r10Apply(t, s, stale, now.Add(16*time.Minute)); !errors.Is(e, td.ErrConflict) {
		t.Fatal("R10_TOKEN_NOT_CHECKED", e)
	}
	rollback := renew
	rollback.OperationID = "rollback_clock_check_1"
	if _, e = r10Apply(t, s, rollback, now.Add(14*time.Minute)); !errors.Is(e, td.ErrClock) {
		t.Fatal("R10_CLOCK_ROLLBACK", e)
	}
	reg := r10Holder(m, l, "register_pr", "register_pr_operation")
	reg.PullRequestID = 7
	reg.PRDimension = "readable"
	reg.PRHeadSHA = m.CommitSHA
	r, e = r10Apply(t, s, reg, now.Add(16*time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	l = r.Lock
	observe := reg
	observe.Kind = "observe"
	observe.ActorKind = "system"
	observe.OperationID = "observe_changed_0001"
	observe.ComparisonState = "needs_sync"
	observe.ObservedCommitSHA = strings.Repeat("c", 40)
	r, e = r10Apply(t, s, observe, now.Add(17*time.Minute))
	if e != nil || !r.Lock.SyncRequired {
		t.Fatal("R10_DRIFT_NOT_BLOCKED", e)
	}
	observe.OperationID = "observe_unknown_0001"
	observe.ComparisonState = "comparison_unknown"
	r, e = r10Apply(t, s, observe, now.Add(18*time.Minute))
	if e != nil || r.Lock.ComparisonState != "comparison_unknown" {
		t.Fatal(e)
	}
	observe.OperationID = "observe_clean_000001"
	observe.ComparisonState = "clean"
	r, e = r10Apply(t, s, observe, now.Add(19*time.Minute))
	if e != nil || r.Lock.ComparisonState != "needs_sync" {
		t.Fatal("R10_DRIFT_LOST_AFTER_UNKNOWN", e)
	}
	syncM := r10Holder(reg, l, "sync", "sync_operation_00001")
	syncM.CommitSHA = strings.Repeat("c", 40)
	syncM.ObservedCommitSHA = syncM.CommitSHA
	r, e = r10Apply(t, s, syncM, now.Add(20*time.Minute))
	if e != nil || r.Lock.SyncRequired || r.Lock.Generation <= l.Generation {
		t.Fatal("R10_SYNC_FAILED", e)
	}
	l = r.Lock
	hand := r10Holder(reg, l, "handoff", "handoff_agent_denied1")
	hand.Handoff = r10Handoff(l)
	hand.HolderAgentID = "agent-2"
	hand.HolderOwnerLC = "next"
	if _, e = r10Apply(t, s, hand, now.Add(21*time.Minute)); !errors.Is(e, td.ErrForbidden) {
		t.Fatal("R10_AGENT_TRANSFERRED", e)
	}
	hand.ActorKind = "user"
	hand.ActorUserLC = "dispatcher"
	hand.ActorAgentID = ""
	hand.ActorOwnerLC = ""
	hand.OperationID = "handoff_user_allowed1"
	r, e = r10Apply(t, s, hand, now.Add(21*time.Minute))
	if e != nil || r.Lock.HolderAgentID != "agent-2" || r.Lock.LeaseToken == l.LeaseToken {
		t.Fatal("R10_HANDOFF_FAILED", e)
	}
	// A binding revoked after the remote read must still fail the SQLite fence.
	if e = s.DeleteSpecReadBinding(ctx, "admin", "next", "github", "github.com"); e != nil {
		t.Fatal(e)
	}
	rel := r10Holder(hand, r.Lock, "release", "release_revoked_0001")
	rel.Handoff = r10Handoff(r.Lock)
	if _, e = r10Apply(t, s, rel, now.Add(22*time.Minute)); !errors.Is(e, td.ErrForbidden) {
		t.Fatal("R10_REVOKED_BINDING_ACCEPTED", e)
	}
}
