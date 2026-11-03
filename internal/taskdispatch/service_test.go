package taskdispatch_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

const c1 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const c2 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const dir = "openspec/changes/example"

func blob(text string) string {
	h := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(text), text)))
	return hex.EncodeToString(h[:])
}

type provider struct {
	mu             sync.Mutex
	files          map[string]map[string]string
	head           string
	denied         map[string]bool
	deniedRepo     map[string]map[string]bool
	pr             specread.PullRequest
	prErr          error
	polls, prPolls int
	onBlob         func()
	waitRef        map[string]chan struct{}
	foreignCommit  map[string]map[string]bool
	truncateTasks  bool
}

func (p *provider) Repository(_ context.Context, id string) (specread.Repository, error) {
	return specread.Repository{ID: id, FullName: "test/repository"}, nil
}
func (p *provider) Authorize(_ context.Context, r specread.Repository, b specread.Binding) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.denied[b.Principal] || p.deniedRepo[r.ID][b.Principal] {
		return specread.ErrForbidden
	}
	return nil
}
func (p *provider) Resolve(ctx context.Context, _ specread.Repository, ref string) (string, error) {
	p.mu.Lock()
	if specread.ValidSHA(ref) {
		p.mu.Unlock()
		return ref, nil
	}
	p.polls++
	head := p.head
	wait := p.waitRef[ref]
	p.mu.Unlock()
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return "", specread.ErrTimeout
		}
	}
	return head, nil
}
func (p *provider) Tree(_ context.Context, repo specread.Repository, c string) ([]specread.Entry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	files, ok := p.files[c]
	if !ok || p.foreignCommit[repo.ID][c] {
		return nil, specread.ErrNotFound
	}
	entries := []specread.Entry{}
	for path, text := range files {
		entries = append(entries, specread.Entry{Path: path, SHA: blob(text), Size: int64(len(text)), Mode: "100644", Type: "blob"})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}
func (p *provider) Blob(_ context.Context, _ specread.Repository, e specread.Entry) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.onBlob != nil {
		p.onBlob()
	}
	for _, files := range p.files {
		for _, text := range files {
			if blob(text) == e.SHA {
				if p.truncateTasks && e.Path == dir+"/tasks.md" {
					return []byte("- [ ] T-01 build\n"), nil
				}
				return []byte(text), nil
			}
		}
	}
	return nil, specread.ErrNotFound
}
func (p *provider) Source(specread.Repository, string, string) string {
	return "https://github.com/test/repository"
}
func (p *provider) PullRequest(context.Context, specread.Repository, int64) (specread.PullRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prPolls++
	return p.pr, p.prErr
}

type env struct {
	s     *td.Service
	db    *store.SQLite
	ids   *identity.MemStore
	p     *provider
	r     td.Request
	now   time.Time
	audit atomic.Int64
}

func setup(t *testing.T) *env {
	t.Helper()
	t.Setenv("DW_TASK_DISPATCH_LOCK_ENABLED", "1")
	ctx := context.Background()
	e := &env{now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "r10.db"))
	if err != nil {
		t.Fatal(err)
	}
	e.db = db
	t.Cleanup(func() { db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ids := identity.NewMemStore()
	e.ids = ids
	for _, id := range []string{"dispatcher", "holder", "next", "outsider"} {
		if err = ids.CreateUser(ctx, identity.User{UserID: id, ShortName: id, State: identity.UserActive, Type: identity.UserMember, CreatedAt: e.now}); err != nil {
			t.Fatal(err)
		}
		if err = db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: id, Provider: "feishu", ProviderIssuer: "test", ProviderSubject: id, DisplayName: id, Active: true}); err != nil {
			t.Fatal(err)
		}
		if err = db.SetSpecReadBinding(ctx, "admin", specread.Binding{Principal: id, Provider: "github", Instance: "github.com", AccountID: "123", Login: id}); err != nil {
			t.Fatal(err)
		}
		if err = ids.CreateHostedAgent(ctx, identity.Agent{ID: "agent-" + id, OwnerLC: id, Name: id, CreatedAt: e.now}, identity.Application{ID: "app-" + id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = ids.CreateGroup(ctx, identity.Group{Code: "group", ShortName: "group", CreatorLC: "dispatcher", LeaderAgentID: "agent-dispatcher", CreatedAt: e.now}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"holder", "next"} {
		if err = ids.JoinGroup(ctx, "group", "agent-"+id, id, e.now); err != nil {
			t.Fatal(err)
		}
	}
	base := map[string]string{dir + "/tasks.md": "# Tasks\n- [ ] T-01 build\n- [ ] T-02 other\n", dir + "/proposal.md": "# Proposal\noriginal\n", dir + "/specs/one/spec.md": "# Requirements\noriginal\n"}
	base[dir+"/tasks.md"] += "\n## 交接记录\ninitial\n"
	p := &provider{head: c1, files: map[string]map[string]string{c1: base}, denied: map[string]bool{}, pr: specread.PullRequest{ID: 7, Repository: "1", HeadSHA: c1, State: "open", BaseRef: "main", HeadRef: "work", UpdatedAt: e.now}}
	e.p = p
	e.s = &td.Service{Repo: db, Identity: ids, Reader: &specread.Service{Bindings: db, Providers: map[string]specread.Provider{"github:github.com": p}, Repositories: map[string]bool{"github:github.com:1": true, "github:github.com:2": true}}, Now: func() time.Time { return e.now }, AuditFailure: func(context.Context, string, string) { e.audit.Add(1) }}
	e.r = td.Request{OperationID: "grant_operation_00001", Kind: "grant", Key: td.Key{Provider: "github", Instance: "github.com", Repository: "1", ChangeDir: dir, TaskID: "T-01"}, Group: "group", HolderAgentID: "agent-holder", Commit: c1, MonitorRef: "main"}
	return e
}
func (e *env) grant(t *testing.T) td.Lock {
	t.Helper()
	r, err := e.s.Execute(context.Background(), td.Actor{UserLC: "dispatcher"}, e.r)
	if err != nil {
		t.Fatal("R10_VALID_GRANT_REJECTED", err)
	}
	return r.Lock
}
func (e *env) holder(l td.Lock, kind string) td.Request {
	return td.Request{OperationID: kind + "_holder_operation_01", Kind: kind, Key: l.Key, Generation: l.Generation, Token: l.LeaseToken}
}
func (e *env) change(sha, path, text string) {
	e.p.mu.Lock()
	defer e.p.mu.Unlock()
	files := map[string]string{}
	for path, text := range e.p.files[c1] {
		files[path] = text
	}
	files[path] = text
	e.p.files[sha] = files
	e.p.head = sha
}

func TestR10ServiceCurrentGroupAndBothRead(t *testing.T) {
	for _, name := range []string{"valid", "dispatcher_not_qualified", "target_not_member", "inactive_user", "inactive_agent", "missing_agent_mapping", "short_sha", "foreign_commit", "truncated_task_read", "target_binding_removed", "dispatcher_read_revoked", "holder_read_revoked", "wrong_provider", "missing_ref", "duplicate_task", "completed_task", "fixed_commit_not_ref"} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			ctx := context.Background()
			actor := td.Actor{UserLC: "dispatcher"}
			pass := name == "valid"
			switch name {
			case "dispatcher_not_qualified":
				actor.UserLC = "outsider"
			case "target_not_member":
				e.r.HolderAgentID = "agent-outsider"
			case "inactive_user":
				if err := e.ids.SetUserState(ctx, "holder", identity.UserDisabled, e.now); err != nil {
					t.Fatal(err)
				}
			case "inactive_agent":
				if _, err := e.ids.RetireAgent(ctx, "agent-holder", "holder", e.now); err != nil {
					t.Fatal(err)
				}
			case "missing_agent_mapping":
				e.r.HolderAgentID = "agent-missing"
			case "short_sha":
				e.r.Commit = "aaaaaaaa"
			case "foreign_commit":
				e.r.Commit = c2
				e.p.files[c2] = e.p.files[c1]
				e.p.foreignCommit = map[string]map[string]bool{"1": {c2: true}}
			case "truncated_task_read":
				e.p.files[c1][dir+"/tasks.md"] = "- [ ] T-01 build\n- [ ] T-01 hidden duplicate\n"
				e.p.truncateTasks = true
			case "target_binding_removed":
				if err := e.db.DeleteSpecReadBinding(ctx, "admin", "holder", "github", "github.com"); err != nil {
					t.Fatal(err)
				}
			case "dispatcher_read_revoked":
				e.p.denied["dispatcher"] = true
			case "holder_read_revoked":
				e.p.denied["holder"] = true
			case "wrong_provider":
				e.r.Key.Provider = "gongfeng"
			case "missing_ref":
				e.r.MonitorRef = ""
			case "duplicate_task":
				e.p.files[c1][dir+"/tasks.md"] = "- [ ] T-01 one\n- [ ] T-01 two"
			case "completed_task":
				e.p.files[c1][dir+"/tasks.md"] = "- [x] T-01 done"
			case "fixed_commit_not_ref":
				e.r.Commit = "main"
			}
			result, err := e.s.Execute(ctx, actor, e.r)
			if pass {
				if err != nil || result.Lock.CommitSHA != c1 {
					t.Fatal("R10_VALID_FIXED_COORDINATE", err)
				}
			} else {
				if err == nil {
					t.Fatalf("R10_AUTH_OR_TASK_GUARD_BYPASSED case=%s", name)
				}
				l, _ := e.db.TaskDispatchLock(ctx, e.r.Key)
				if l != nil {
					t.Fatal("R10_DENIED_GRANTED", name)
				}
				if e.audit.Load() == 0 {
					t.Fatal("R10_DENIED_NOT_AUDITED", name)
				}
			}
		})
	}
}
func TestR10ServicePRDimensionsAndSubmit(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	l := e.grant(t)
	renew := e.holder(l, "renew")
	e.now = e.now.Add(15 * time.Minute)
	r, err := e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, renew)
	if err != nil || r.Lock.PRDimension != "pr_not_opened" {
		t.Fatal("R10_NOT_OPENED_RENEW", err)
	}
	submit := e.holder(r.Lock, "submit")
	if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, submit); !errors.Is(err, td.ErrPRRequired) {
		t.Fatal("R10_NOT_OPENED_SUBMITTED", err)
	}
	reg := e.holder(r.Lock, "register_pr")
	reg.PullRequestID = 7
	e.p.pr.Repository = "2"
	if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, reg); err == nil {
		t.Fatal("R10_CROSS_REPO_PR_REGISTERED")
	}
	current, _ := e.db.TaskDispatchLock(ctx, l.Key)
	if current.PullRequestID != 0 {
		t.Fatal("R10_FOREIGN_PR_CHANGED_LOCK")
	}
	e.p.pr.Repository = "1"
	r, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, reg)
	if err != nil || r.Lock.PullRequestID != 7 {
		t.Fatal(err)
	}
	e.p.prErr = specread.ErrForbidden
	for _, kind := range []string{"renew", "continue", "submit"} {
		req := e.holder(r.Lock, kind)
		req.OperationID = kind + "_unknown_registered_1"
		if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, req); !errors.Is(err, td.ErrComparison) {
			t.Fatalf("R10_REGISTERED_UNKNOWN_ALLOWED kind=%s err=%v", kind, err)
		}
	}
	e.p.prErr = nil
	submit = e.holder(r.Lock, "submit")
	submit.OperationID = "submit_readable_pr_0001"
	if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, submit); err != nil {
		t.Fatal("R10_HEALTHY_PR_BLOCKED", err)
	}
}
func TestR10ServiceReplayCrossRepositoryReleaseAndOwnerFence(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	l := e.grant(t)
	again, err := e.s.Execute(ctx, td.Actor{UserLC: "dispatcher"}, e.r)
	if err != nil || !again.Replay || again.Lock.LeaseToken != l.LeaseToken {
		t.Fatal("R10_SERVICE_REPLAY", err)
	}
	other := e.r
	other.Key.Repository = "2"
	other.OperationID = "grant_other_repo_0001"
	// The consumer task references the provider's exact fixed version in Git.
	// Its independent blob/C is retained, without copying the description to DB.
	consumerTasks := "# Tasks\n- [ ] T-01 consume provider github:github.com:1@" + c1 + "\n"
	e.change(c2, dir+"/tasks.md", consumerTasks)
	other.Commit = c2
	if r, err := e.s.Execute(ctx, td.Actor{UserLC: "dispatcher"}, other); err != nil || r.Lock.Repository != "2" || r.Lock.CommitSHA != c2 || r.Lock.TasksBlobSHA != blob(consumerTasks) {
		t.Fatal("R10_CROSS_REPO_LOCK_COLLISION", err)
	}
	providerLock, providerErr := e.db.TaskDispatchLock(ctx, l.Key)
	if providerErr != nil || providerLock == nil || providerLock.CommitSHA != c1 || providerLock.LeaseToken != l.LeaseToken {
		t.Fatal("R10_CONSUMER_CHANGED_PROVIDER_AUTHORITY", providerErr)
	}
	rel := e.holder(l, "release")
	rel.OperationID = "release_nonholder_operation_01"
	if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-next"}, rel); err == nil {
		t.Fatal("R10_NONHOLDER_RELEASE")
	}
	rel.OperationID = "release_missing_pointer_operation_01"
	if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, rel); !errors.Is(err, td.ErrInvalid) {
		t.Fatal("R10_RELEASE_WITHOUT_HANDOFF", err)
	}
	record := fmt.Sprintf("# Handoff\n%s\n%s\nstopped\n", l.TaskID, l.CommitSHA)
	e.p.files[c1][dir+"/handoff.md"] = record
	rel.OperationID = "release_valid_pointer_operation_01"
	rel.Handoff = td.Handoff{Commit: c1, Path: dir + "/handoff.md", BlobSHA: blob(record), ExecutionCommit: c1, TaskID: l.TaskID, RecoveryState: "stopped"}
	released, err := e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, rel)
	if err != nil {
		t.Fatal("R10_VALID_RELEASE", err)
	}
	if released.Lock.State != "released" || released.CurrentLease || released.Lock.LeaseToken != "" || released.Lock.Generation <= l.Generation || !released.Lock.UpdatedAt.Equal(e.now) || released.Lock.Handoff != rel.Handoff {
		t.Fatal("R10_RELEASE_AUTHORITY_NOT_REVOKED", released)
	}
	view, inspectErr := e.s.Inspect(ctx, td.Actor{AgentID: "agent-holder"}, l.Key, rel.OperationID)
	if inspectErr != nil || view.Effective || view.Lock == nil || view.Lock.State != "released" || view.Lock.LeaseToken != "" || len(view.Events) == 0 || view.Events[len(view.Events)-1].Type != "release" {
		t.Fatal("R10_RELEASE_PERSISTENCE_OR_AUDIT_MISSING", view, inspectErr)
	}
	oldRenew := e.holder(l, "renew")
	oldRenew.OperationID = "released_old_token_operation_01"
	if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, oldRenew); !errors.Is(err, td.ErrConflict) {
		t.Fatal("R10_RELEASED_OLD_TOKEN_RENEWED", err)
	}
	if err = e.ids.SetUserState(ctx, "holder", identity.UserDisabled, e.now); err != nil {
		t.Fatal(err)
	}
	if _, err = e.s.Inspect(ctx, td.Actor{AgentID: "agent-holder"}, l.Key, ""); err == nil {
		t.Fatal("R10_DISABLED_OWNER_INHERITED")
	}
}

func TestR10ServiceHandoffReadsSuccessorAndFixedRecoveryPointer(t *testing.T) {
	for _, name := range []string{"valid", "successor_read_revoked", "pointer_unreadable", "unverified_recovery", "holder_cannot_transfer"} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			l := e.grant(t)
			record := fmt.Sprintf("# Recovery\n%s\n%s\nunknown\n", l.TaskID, l.CommitSHA)
			e.p.files[c1][dir+"/handoff.md"] = record
			r := e.holder(l, "handoff")
			r.HolderAgentID = "agent-next"
			r.Commit = c1
			r.MonitorRef = "main"
			r.Handoff = td.Handoff{Commit: c1, Path: dir + "/handoff.md", BlobSHA: blob(record), ExecutionCommit: c1, TaskID: l.TaskID, RecoveryState: "unknown"}
			actor := td.Actor{UserLC: "dispatcher"}
			switch name {
			case "successor_read_revoked":
				e.p.denied["next"] = true
			case "pointer_unreadable":
				r.Handoff.Path = dir + "/missing.md"
			case "unverified_recovery":
				r.Handoff.RecoveryState = ""
			case "holder_cannot_transfer":
				actor = td.Actor{AgentID: "agent-holder"}
			}
			result, err := e.s.Execute(context.Background(), actor, r)
			if name == "valid" {
				if err != nil || result.Lock.HolderAgentID != "agent-next" || result.Lock.Generation != l.Generation+1 || result.Lock.Handoff.RecoveryState != "unknown" {
					t.Fatal("R10_SERVICE_HANDOFF_FAILED_OR_FALSE_STOPPED", result, err)
				}
				old := e.holder(l, "renew")
				if _, err = e.s.Execute(context.Background(), td.Actor{AgentID: "agent-holder"}, old); err == nil {
					t.Fatal("R10_HANDOFF_OLD_HOLDER_RENEWED")
				}
			} else {
				if err == nil {
					t.Fatal("R10_HANDOFF_AUTHORITY_OR_POINTER_BYPASSED", name)
				}
				current, _ := e.db.TaskDispatchLock(context.Background(), l.Key)
				if current == nil || current.Generation != l.Generation || current.HolderAgentID != l.HolderAgentID {
					t.Fatal("R10_DENIED_HANDOFF_CHANGED_HOLDER", name)
				}
			}
		})
	}
}

func TestR10NonholderWithReadablePointerCannotRelease(t *testing.T) {
	for _, name := range []string{"nonholder", "missing", "unreadable", "stale_generation"} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			l := e.grant(t)
			record := fmt.Sprintf("# Recovery\n%s\n%s\n", l.TaskID, l.CommitSHA)
			e.p.files[c1][dir+"/handoff.md"] = record
			r := e.holder(l, "release")
			r.Handoff = td.Handoff{Commit: c1, Path: dir + "/handoff.md", BlobSHA: blob(record), ExecutionCommit: c1, TaskID: l.TaskID, RecoveryState: "unknown"}
			actor := td.Actor{AgentID: "agent-holder"}
			expected := td.ErrForbidden
			switch name {
			case "nonholder":
				actor = td.Actor{AgentID: "agent-next"}
			case "missing":
				r.Handoff = td.Handoff{}
				expected = td.ErrInvalid
			case "unreadable":
				r.Handoff.Path = dir + "/missing.md"
				expected = td.ErrComparison
			case "stale_generation":
				r.Generation++
				expected = td.ErrConflict
			}
			if _, err := e.s.Execute(context.Background(), actor, r); !errors.Is(err, expected) {
				t.Fatal("R10_NONHOLDER_WITH_POINTER_RELEASED", name, err)
			}
			current, _ := e.db.TaskDispatchLock(context.Background(), l.Key)
			if current == nil || current.Generation != l.Generation || current.HolderAgentID != l.HolderAgentID || current.LeaseToken != l.LeaseToken {
				t.Fatal("R10_NONHOLDER_CHANGED_CURRENT_LOCK", name)
			}
		})
	}
}

func TestR10ServiceMaterialDriftBlocksEveryProtectedActionUntilExplicitSync(t *testing.T) {
	for _, kind := range []string{"continue", "renew", "submit"} {
		t.Run(kind, func(t *testing.T) {
			e := setup(t)
			l := e.grant(t)
			ctx := context.Background()
			reg := e.holder(l, "register_pr")
			reg.PullRequestID = 7
			registered, err := e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, reg)
			if err != nil {
				t.Fatal(err)
			}
			l = registered.Lock
			e.change(c2, dir+"/proposal.md", "# Proposal\nnew material requirement\n")
			e.now = e.now.Add(time.Minute)
			blocked := e.holder(l, kind)
			if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, blocked); !errors.Is(err, td.ErrNeedsSync) {
				t.Fatal("R10_MATERIAL_DRIFT_ACTION_ACCEPTED", kind, err)
			}
			current, _ := e.db.TaskDispatchLock(ctx, l.Key)
			if current == nil || !current.SyncRequired || current.CommitSHA != c1 || !current.ExpiresAt.Equal(l.ExpiresAt) {
				t.Fatal("R10_MATERIAL_DRIFT_REBOUND_OR_EXTENDED", kind, current)
			}
			e.p.pr.HeadSHA = c2
			sync := e.holder(l, "sync")
			sync.Commit = c2
			result, err := e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, sync)
			if err != nil || result.Lock.CommitSHA != c2 || result.Lock.SyncRequired || result.Lock.Generation <= l.Generation {
				t.Fatal("R10_EXPLICIT_SYNC_FAILED", kind, err, result)
			}
			next := e.holder(result.Lock, kind)
			next.OperationID = "after_sync_" + kind + "_operation_01"
			if _, err = e.s.Execute(ctx, td.Actor{AgentID: "agent-holder"}, next); err != nil {
				t.Fatal("R10_SYNCED_ACTION_REJECTED", kind, err)
			}
		})
	}
}
