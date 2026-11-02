package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
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
	rng := rand.New(rand.NewSource(20261102))
	for round := 0; round < 50; round++ {
		m.Key.TaskID = fmt.Sprintf("T-%02d", round)
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := []td.Result{}
		fail := []error{}
		start := make(chan struct{})
		for _, i := range rng.Perm(50) {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
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
		close(start)
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
		replay := m
		replay.OperationID = wins[0].OperationID
		replay.CommitSHA = l.CommitSHA
		repeated, replayErr := r10Apply(t, s, replay, now)
		if replayErr != nil || !repeated.Replay || !repeated.CurrentLease || repeated.Lock.LeaseToken != l.LeaseToken {
			t.Fatal("R10_ROUND_RETRY_CHANGED_AUTHORITY", round, replayErr)
		}
		renew := r10Holder(m, l, "renew", fmt.Sprintf("renew_round_%03d_0001", round))
		expire := renew
		expire.Kind = "expire"
		expire.ActorKind = "system"
		expire.OperationID = fmt.Sprintf("expire_round_%03d_0001", round)
		type boundaryResult struct {
			kind   string
			result td.Result
			err    error
		}
		boundary := make(chan boundaryResult, 2)
		barrier := make(chan struct{})
		requests := []td.Mutation{renew, expire}
		for _, index := range rng.Perm(2) {
			request := requests[index]
			go func() {
				<-barrier
				r, err := s.ApplyTaskDispatch(ctx, request, func() time.Time { return l.ExpiresAt }, func() error { return nil })
				boundary <- boundaryResult{request.Kind, r, err}
			}()
		}
		close(barrier)
		var expired td.Result
		for i := 0; i < 2; i++ {
			b := <-boundary
			if b.kind == "renew" {
				if !errors.Is(b.err, td.ErrConflict) {
					t.Fatal("R10_EXPIRY_BOUNDARY", round, b.err)
				}
			} else {
				if b.err != nil || b.result.Lock.State != "expired" || b.result.Lock.HolderAgentID != l.HolderAgentID {
					t.Fatal("R10_EXPIRE_REASSIGNED", round, b.err)
				}
				expired = b.result
			}
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

func TestR10ReplayAfterHandoffCannotIssueOldPermission(t *testing.T) {
	s, m, now := r10DB(t)
	ctx := context.Background()
	first, err := r10Apply(t, s, m, now)
	if err != nil || !first.CurrentLease {
		t.Fatal(err)
	}
	transfer := m
	transfer.Kind = "handoff"
	transfer.OperationID = "handoff_for_replay_001"
	transfer.ExpectedGeneration = first.Lock.Generation
	transfer.ExpectedToken = first.Lock.LeaseToken
	transfer.HolderAgentID = "agent-2"
	transfer.HolderOwnerLC = "next"
	transfer.Handoff = r10Handoff(first.Lock)
	moved, err := r10Apply(t, s, transfer, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	old, err := r10Apply(t, s, m, now.Add(2*time.Minute))
	if err != nil || !old.Replay || old.CurrentLease || old.Lock.LeaseToken != "" {
		t.Fatal("R10_REPLAY_REACTIVATED_OLD_GENERATION", old, err)
	}
	current, err := s.TaskDispatchLock(ctx, m.Key)
	if err != nil || current.Generation != moved.Lock.Generation || current.HolderAgentID != "agent-2" || current.LeaseToken != moved.Lock.LeaseToken {
		t.Fatal("R10_REPLAY_CHANGED_CURRENT_HOLDER", current, err)
	}
	events, _ := s.TaskDispatchEvents(ctx, m.Key, 0)
	notices, _ := s.TaskDispatchNotices(ctx, "pending", 100)
	if len(events) != 2 || len(notices) != 2 {
		t.Fatal("R10_REPLAY_DUPLICATED_HANDOFF_NOTICE", len(events), len(notices))
	}
}
func TestR10CorruptPersistedCoordinatesFailClosed(t *testing.T) {
	for _, column := range []string{"commit_sha", "lease_token", "monitor_ref"} {
		t.Run(column, func(t *testing.T) {
			s, m, now := r10DB(t)
			r, err := r10Apply(t, s, m, now)
			if err != nil {
				t.Fatal(err)
			}
			value := "bad"
			if column == "lease_token" {
				value = strings.Repeat("z", 64)
			}
			if column == "monitor_ref" {
				value = ""
			}
			if _, err = s.db.Exec(`UPDATE r10_task_lock SET `+column+`=?`, value); err != nil {
				t.Fatal(err)
			}
			if _, err = s.TaskDispatchLock(context.Background(), m.Key); !errors.Is(err, td.ErrUnknown) {
				t.Fatal("R10_CORRUPT_AUTHORITY_ACCEPTED", column, err)
			}
			if _, err = s.TaskDispatchActive(context.Background()); !errors.Is(err, td.ErrUnknown) {
				t.Fatal("R10_CORRUPT_MONITOR_ACCEPTED", column, err)
			}
			renew := r10Holder(m, r.Lock, "renew", "corrupt_renew_operation")
			if _, err = r10Apply(t, s, renew, now.Add(time.Minute)); !errors.Is(err, td.ErrUnknown) {
				t.Fatal("R10_CORRUPT_LEASE_RENEWED", column, err)
			}
		})
	}
}

func TestR10NoticeCursorDoesNotStarveLaterPendingEntries(t *testing.T) {
	s, m, now := r10DB(t)
	ctx := context.Background()
	if _, err := r10Apply(t, s, m, now); err != nil {
		t.Fatal(err)
	}
	first, err := s.TaskDispatchNotices(ctx, "pending", 1000)
	if err != nil || len(first) != 1 {
		t.Fatal(err)
	}
	base := first[0]
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 1001; i++ {
		n := base
		n.ID = fmt.Sprintf("offline_%04d", i)
		n.SourceEnvelopeID = n.ID
		n.RecipientID = n.ID
		_, err = tx.Exec(`INSERT INTO r10_task_lock_outbox(`+taskDispatchNoticeColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, n.ID, n.OperationID, n.Key.Provider, n.Key.Instance, n.Key.Repository, n.Key.ChangeDir, n.Key.TaskID, n.Generation, n.RecipientKind, n.RecipientID, n.RecipientOwnerLC, n.Type, n.State, n.SourceEnvelopeID, n.DeliveryID, taskDispatchTime(n.CreatedAt), "", "")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var at time.Time
	id := ""
	pages := 0
	for {
		items, e := s.TaskDispatchNoticesAfter(ctx, "pending", 1000, at, id)
		if e != nil {
			t.Fatal(e)
		}
		if len(items) == 0 {
			break
		}
		pages++
		for _, n := range items {
			if seen[n.ID] {
				t.Fatal("R10_NOTICE_CURSOR_DUPLICATE", n.ID)
			}
			seen[n.ID] = true
		}
		last := items[len(items)-1]
		at, id = last.CreatedAt, last.ID
	}
	if len(seen) != 1002 || pages != 2 {
		t.Fatal("R10_PENDING_NOTICE_STARVED", len(seen), pages)
	}
}

func TestR10HandoffEventFailureCannotPartiallyTransfer(t *testing.T) {
	s, m, now := r10DB(t)
	first, err := r10Apply(t, s, m, now)
	if err != nil {
		t.Fatal(err)
	}
	hand := m
	hand.OperationID = "handoff_atomic_failure_0001"
	hand.Kind = "handoff"
	hand.ExpectedGeneration = first.Lock.Generation
	hand.ExpectedToken = first.Lock.LeaseToken
	hand.HolderAgentID = "agent-next"
	hand.HolderOwnerLC = "next"
	hand.Handoff = r10Handoff(first.Lock)
	if _, err = s.db.Exec(`CREATE TRIGGER r10_handoff_fail BEFORE INSERT ON r10_task_lock_event WHEN NEW.event_type='handoff' BEGIN SELECT RAISE(ABORT,'injected handoff event failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = r10Apply(t, s, hand, now.Add(time.Minute)); err == nil {
		t.Fatal("R10_HANDOFF_HALF_COMMIT_SUCCESS")
	}
	current, err := s.TaskDispatchLock(context.Background(), m.Key)
	if err != nil || current == nil || current.HolderAgentID != first.Lock.HolderAgentID || current.Generation != first.Lock.Generation || current.LeaseToken != first.Lock.LeaseToken {
		t.Fatal("R10_HANDOFF_HALF_COMMIT_AUTHORITY", current, err)
	}
	events, _ := s.TaskDispatchEvents(context.Background(), m.Key, 0)
	notices, _ := s.TaskDispatchNotices(context.Background(), "pending", 100)
	if len(events) != 1 || len(notices) != 1 {
		t.Fatal("R10_HANDOFF_HALF_COMMIT_AUDIT", len(events), len(notices))
	}
}

func TestR10ExactExpiryAndWrongTokenCannotRenew(t *testing.T) {
	for _, name := range []string{"exact_expiry", "wrong_token"} {
		t.Run(name, func(t *testing.T) {
			s, m, now := r10DB(t)
			first, err := r10Apply(t, s, m, now)
			if err != nil {
				t.Fatal(err)
			}
			renew := r10Holder(m, first.Lock, "renew", "boundary_renew_operation_0001")
			at := first.Lock.ExpiresAt
			if name == "wrong_token" {
				at = now.Add(time.Minute)
				renew.ExpectedToken = strings.Repeat("f", 64)
			}
			if _, err = r10Apply(t, s, renew, at); !errors.Is(err, td.ErrConflict) {
				t.Fatal("R10_BOUNDARY_OR_TOKEN_RENEWED", name, err)
			}
			current, _ := s.TaskDispatchLock(context.Background(), m.Key)
			if current == nil || !current.ExpiresAt.Equal(first.Lock.ExpiresAt) {
				t.Fatal("R10_REFUSED_RENEW_CHANGED_EXPIRY", name)
			}
		})
	}
}
