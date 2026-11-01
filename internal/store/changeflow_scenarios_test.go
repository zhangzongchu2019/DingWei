package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cf "github.com/zhangzongchu2019/dingwei/internal/changeflow"
	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

type coreProvider struct {
	mu            sync.Mutex
	f             cf.Snapshot
	allowed       bool
	denied        map[string]bool
	reads         int
	change        func(*cf.Snapshot)
	remote        map[string]cf.Comment
	writes        int
	unknown, fail bool
}

func (p *coreProvider) WithAccess(_ context.Context, actor string, c cf.Coordinate, action string, fn func(cf.Reader) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads = 0
	if !p.allowed || actor == "" || p.denied[action] || c.Provider != p.f.Coordinate.Provider || c.Instance != p.f.Coordinate.Instance || c.Repository != p.f.Coordinate.Repository {
		return cf.ErrForbidden
	}
	return fn(p)
}
func (p *coreProvider) Read(_ context.Context, c cf.Coordinate) (cf.Snapshot, error) {
	p.reads++
	if p.reads == 2 && p.change != nil {
		p.change(&p.f)
	}
	b, _ := json.Marshal(p.f)
	var f cf.Snapshot
	_ = json.Unmarshal(b, &f)
	return f, nil
}
func (p *coreProvider) UpsertComment(_ context.Context, c cf.Comment) (cf.CommentReceipt, error) {
	if p.remote == nil {
		p.remote = map[string]cf.Comment{}
	}
	loc := "https://provider.invalid/mr/42#risk-" + c.ReminderID
	if _, ok := p.remote[c.OperationKey]; ok {
		return cf.CommentReceipt{Status: "delivered", Location: loc}, nil
	}
	if p.fail {
		return cf.CommentReceipt{Status: "failed"}, nil
	}
	p.remote[c.OperationKey] = c
	p.writes++
	if p.unknown {
		p.unknown = false
		return cf.CommentReceipt{Status: "unknown"}, errors.New("response lost")
	}
	return cf.CommentReceipt{Status: "delivered", Location: loc}, nil
}
func coreFacts() cf.Snapshot {
	c := cf.Coordinate{Provider: "github", ChangeID: "42", Coordinate: specconfirm.Coordinate{Instance: "github.com", Repository: "owner/repo", ChangeDir: "openspec/changes/demo", Commit: strings.Repeat("a", 40)}}
	rules, e := cf.ParseRules("", nil)
	if e != nil {
		panic(e)
	}
	return cf.Snapshot{Coordinate: c, Owner: "owner", CurrentHead: c.Commit, Version: "facts-1", LabelsKnown: true, ProposalKnown: true, Labels: []string{"risk:funds"}, Input: cf.RiskInput{Coordinate: c, Base: strings.Repeat("b", 40), Head: c.Commit, Complete: true, Labels: []string{"risk:funds"}}, Rules: rules, ReviewsKnown: true, RequiredReviewers: []string{"reviewer"}, Reviews: []cf.Review{{Actor: "reviewer", Commit: c.Commit, Decision: "APPROVED", Valid: true, Evidence: "git:review/1"}}, PMKnown: true, PMPoints: []string{"PM01"}, Responses: []cf.PMResponse{{Point: "PM01", Commit: c.Commit, Decision: "agree", Valid: true, Evidence: "git:pm/1"}}, TasksKnown: true, Tasks: []cf.Task{{ID: "T01", Actor: "dev", Summary: "implementation", Commit: c.Commit, Evidence: "git:handoff/1", Complete: true}}, ScenariosKnown: true, Scenarios: []string{"S01", "S02"}, ApplicabilityKnown: true, Applicable: map[string]bool{c.Commit: true}, Release: cf.Release{ID: "release-1", Environment: "staging", Commit: c.Commit, Coordinate: c, Success: true, Verified: true, Evidence: "report:release/1"}}
}

type coreEnv struct {
	db  *SQLite
	p   *coreProvider
	s   *cf.Service
	c   cf.Coordinate
	ctx context.Context
	n   int
}

func newCore(t *testing.T) *coreEnv {
	t.Helper()
	db := openMigrated(t, filepath.Join(t.TempDir(), "core.db"))
	t.Cleanup(func() { db.Close() })
	p := &coreProvider{f: coreFacts(), allowed: true, denied: map[string]bool{}}
	return &coreEnv{db: db, p: p, c: p.f.Coordinate, s: &cf.Service{Store: db, Provider: p}, ctx: context.Background()}
}
func (e *coreEnv) state(t *testing.T) cf.State {
	t.Helper()
	st, err := e.db.Read(e.ctx, e.c)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
func (e *coreEnv) op() string { e.n++; return fmt.Sprintf("operation-%06d", e.n) }
func (e *coreEnv) advance(t *testing.T, to cf.Phase) error {
	t.Helper()
	return e.s.Advance(e.ctx, "owner", cf.AdvanceRequest{Coordinate: e.c, OperationID: e.op(), Expected: e.state(t).Phase, To: to, Environment: func() string {
		if to == cf.Released {
			return "staging"
		}
		return ""
	}()})
}
func (e *coreEnv) at(t *testing.T, phase cf.Phase) {
	t.Helper()
	ranks := map[cf.Phase]int{cf.Draft: 0, cf.ReviewPhase: 1, cf.Approved: 2, cf.Implementing: 3, cf.Acceptance: 4, cf.Archived: 5, cf.Released: 6}
	for _, p := range []cf.Phase{cf.ReviewPhase, cf.Approved, cf.Implementing, cf.Acceptance, cf.Archived, cf.Released} {
		if e.state(t).Phase == phase {
			return
		}
		if ranks[p] <= ranks[e.state(t).Phase] {
			continue
		}
		if p == cf.Archived && len(e.state(t).Results) == 0 {
			e.results(t, "PASS")
		}
		if err := e.advance(t, p); err != nil {
			t.Fatal(p, err)
		}
	}
}
func (e *coreEnv) results(t *testing.T, status string) {
	t.Helper()
	for _, id := range e.p.f.Scenarios {
		q := cf.ResultRequest{Coordinate: e.c, OperationID: e.op(), Scenario: id, Status: status, TestedCommit: e.c.Commit, Evidence: "log:scenario/" + id}
		if status == "NOT_RUN" || status == "NOT_APPLICABLE" {
			q.Reason = "explicitly deferred"
		}
		if err := e.s.RecordResult(e.ctx, "qa", q); err != nil {
			t.Fatal(err)
		}
	}
}
func (e *coreEnv) detect(t *testing.T) {
	t.Helper()
	if err := e.s.Detect(e.ctx, "owner", e.c); err != nil {
		t.Fatal(err)
	}
}
func (e *coreEnv) last(t *testing.T) cf.RiskRevision {
	t.Helper()
	return e.state(t).Reminder.Revisions[len(e.state(t).Reminder.Revisions)-1]
}
func mustReject(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("must reject")
	}
}
func cloneFacts(f cf.Snapshot) cf.Snapshot {
	b, _ := json.Marshal(f)
	var out cf.Snapshot
	json.Unmarshal(b, &out)
	return out
}

func TestR1_003_S01(t *testing.T) {
	for _, kind := range []string{"github", "gongfeng"} {
		t.Run(kind, func(t *testing.T) {
			e := newCore(t)
			e.c.Provider = kind
			e.p.f.Coordinate = e.c
			e.p.f.Input.Coordinate = e.c
			e.detect(t)
			if len(e.last(t).Hits) != 1 {
				t.Fatal("provider-dependent classification")
			}
		})
	}
}
func TestR1_003_S02(t *testing.T) {
	for _, name := range []string{"service-only", "revoked", "no-comment", "wrong-commit", "wrong-instance", "wrong-repo"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			switch name {
			case "service-only", "revoked":
				e.p.allowed = false
			case "no-comment":
				e.p.denied["comment"] = true
			case "wrong-commit":
				e.p.f.Coordinate.Commit = strings.Repeat("b", 40)
			case "wrong-instance":
				e.c.Instance = "other.invalid"
			case "wrong-repo":
				e.c.Repository = "other/repo"
			}
			err := e.s.Detect(e.ctx, "owner", e.c)
			if name == "no-comment" {
				if err != nil || e.last(t).MR.Status != "failed" || e.last(t).Console.Status != "delivered" {
					t.Fatal("write permission not independent", err)
				}
				return
			}
			mustReject(t, err)
			if e.p.writes != 0 {
				t.Fatal("cross-repository comment")
			}
		})
	}
}
func TestR1_004_S01(t *testing.T) {
	e := newCore(t)
	e.p.f.Input.Files = []cf.FileDiff{{NewPath: "safe.txt", Complete: true, Patch: "@@ -0,0 +1 @@\n+price private-source-sentinel\n"}}
	e.detect(t)
	e.detect(t)
	r := e.last(t)
	if e.p.writes != 1 || len(e.state(t).Reminder.Revisions) != 1 || r.Console.Status != "delivered" || r.MR.Status != "delivered" {
		t.Fatal("duplicate or missing channel")
	}
	b, _ := json.Marshal(e.p.remote)
	if strings.Contains(string(b), "private-source-sentinel") {
		t.Fatal("source in comment")
	}
}
func TestR1_004_S02(t *testing.T) {
	for _, name := range []string{"failed", "unknown", "audit-failure"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			if name == "audit-failure" {
				e.db.db.Exec(`CREATE TRIGGER fail_core BEFORE INSERT ON changeflow_evidence BEGIN SELECT RAISE(ABORT,'audit fail'); END;`)
				mustReject(t, e.s.Detect(e.ctx, "owner", e.c))
				if e.p.writes != 0 || e.state(t).Reminder != nil {
					t.Fatal("partial persistence")
				}
				return
			}
			e.p.fail = name == "failed"
			e.p.unknown = name == "unknown"
			e.detect(t)
			if e.last(t).MR.Status == "delivered" {
				t.Fatal("false delivery")
			}
			e.p.fail = false
			e.detect(t)
			if e.last(t).MR.Status != "delivered" || e.p.writes != 1 || len(e.last(t).Attempts) != 2 {
				t.Fatal("retry duplicated remote write or lost attempts")
			}
		})
	}
}
func TestR1_005_S01(t *testing.T) {
	e := newCore(t)
	e.p.f.MergedAt = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	e.p.f.MergeEvidence = "git:merge/1"
	e.detect(t)
	r := e.last(t)
	if err := e.s.Acknowledge(e.ctx, "owner", e.c, r.ID); err != nil {
		t.Fatal(err)
	}
	at := e.last(t).Acknowledged.At
	e.s.Acknowledge(e.ctx, "owner", e.c, r.ID)
	if !e.last(t).Acknowledged.At.Equal(at) || e.state(t).Reminder.MergedAt.IsZero() {
		t.Fatal("lost original timestamps")
	}
	if e.state(t).Phase != cf.Draft {
		t.Fatal("knowledge advanced phase")
	}
}
func TestR1_005_S02(t *testing.T) {
	e := newCore(t)
	e.detect(t)
	mustReject(t, e.s.Acknowledge(e.ctx, "other", e.c, e.last(t).ID))
	mustReject(t, e.s.Acknowledge(e.ctx, "owner", e.c, "message-ACK"))
	if e.last(t).Acknowledged != nil || !e.state(t).Reminder.MergedAt.IsZero() {
		t.Fatal("fake acknowledgement/merge")
	}
}
func TestR1_006_S01(t *testing.T) {
	e := newCore(t)
	e.detect(t)
	old := e.c
	oldRev := e.last(t)
	e.s.Acknowledge(e.ctx, "owner", e.c, oldRev.ID)
	e.c.Commit = strings.Repeat("c", 40)
	e.p.f.Coordinate = e.c
	e.p.f.Input.Coordinate = e.c
	e.p.f.Input.Head = e.c.Commit
	e.p.f.CurrentHead = e.c.Commit
	e.detect(t)
	e.detect(t)
	if e.last(t).Acknowledged != nil || e.p.writes != 2 {
		t.Fatal("new commit not independently notified")
	}
	st, _ := e.db.Read(e.ctx, old)
	if st.Reminder.Revisions[0].Acknowledged == nil {
		t.Fatal("old history erased")
	}
}
func TestR1_006_S02(t *testing.T) {
	e := newCore(t)
	e.detect(t)
	e.c.Commit = strings.Repeat("c", 40)
	e.p.f.Coordinate = e.c
	e.p.f.Input.Coordinate = e.c
	e.p.f.Input.Head = e.c.Commit
	e.p.f.CurrentHead = e.c.Commit
	e.p.f.Input.Labels = nil
	e.p.f.Labels = nil
	e.detect(t)
	if e.state(t).Reminder != nil || e.p.writes != 1 {
		t.Fatal("old risk copied")
	}
}
func TestR1_006_S03(t *testing.T) {
	for _, source := range []string{"labels", "rules"} {
		for _, kind := range []string{"increase", "decrease", "zero"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				e := newCore(t)
				if kind == "decrease" {
					e.p.f.Labels = append(e.p.f.Labels, "risk:privacy")
					e.p.f.Input.Labels = e.p.f.Labels
				}
				if source == "rules" {
					e.p.f.Input.Labels = nil
					e.p.f.Labels = nil
					e.p.f.Input.Files = []cf.FileDiff{{NewPath: "custom.txt", Complete: true, Binary: true}}
					raw := []byte("version: 1\nrules:\n - id: custom\n   category: risk:funds\n   paths: ['custom.txt']\n")
					e.p.f.Rules, _ = cf.ParseRules(cf.GitBlob(raw), raw)
					if kind == "decrease" {
						e.p.f.Input.Labels = []string{"risk:privacy"}
						e.p.f.Labels = e.p.f.Input.Labels
					}
				}
				e.detect(t)
				old := e.last(t)
				e.s.Acknowledge(e.ctx, "owner", e.c, old.ID)
				if source == "labels" {
					switch kind {
					case "increase":
						e.p.f.Labels = append(e.p.f.Labels, "risk:privacy")
					case "decrease":
						e.p.f.Labels = []string{"risk:funds"}
					case "zero":
						e.p.f.Labels = nil
					}
					e.p.f.Input.Labels = e.p.f.Labels
				} else {
					raw := []byte("version: 1\nrules: []\n")
					if kind == "increase" {
						raw = []byte("version: 1\nrules:\n - id: custom\n   category: risk:funds\n   paths: ['custom.txt']\n - id: extra\n   category: risk:privacy\n   paths: ['custom.txt']\n")
					}
					e.p.f.Rules, _ = cf.ParseRules(cf.GitBlob(raw), raw)
				}
				e.detect(t)
				r := e.last(t)
				if r.ID == old.ID || r.Acknowledged != nil || len(e.state(t).Reminder.Revisions) != 2 || e.state(t).Reminder.Revisions[0].Acknowledged == nil {
					t.Fatal("changed set carried knowledge/history lost")
				}
				if kind == "zero" && len(r.Hits) != 0 {
					t.Fatal("zero set not represented")
				}
			})
		}
	}
}
func TestR1_006_S04(t *testing.T) {
	for _, source := range []string{"labels", "rules"} {
		t.Run(source, func(t *testing.T) {
			e := newCore(t)
			e.detect(t)
			e.s.Acknowledge(e.ctx, "owner", e.c, e.last(t).ID)
			at := e.last(t).Acknowledged.At
			if source == "labels" {
				e.p.f.Labels = append(e.p.f.Labels, "non-risk-label")
				e.p.f.Input.Labels = e.p.f.Labels
			} else {
				raw := []byte("version: 1\nrules:\n - id: unused\n   category: risk:privacy\n   paths: ['unmatched/**']\n")
				e.p.f.Rules, _ = cf.ParseRules(cf.GitBlob(raw), raw)
			}
			e.detect(t)
			e.detect(t)
			if len(e.state(t).Reminder.Revisions) != 2 || e.last(t).Acknowledged == nil || !e.last(t).Acknowledged.At.Equal(at) {
				t.Fatal("unchanged set reset/replayed")
			}
		})
	}
}
func TestR1_006_S05(t *testing.T) {
	e := newCore(t)
	e.detect(t)
	old := e.last(t).ID
	e.p.f.Labels = append(e.p.f.Labels, "risk:privacy")
	e.p.f.Input.Labels = e.p.f.Labels
	e.detect(t)
	mustReject(t, e.s.Acknowledge(e.ctx, "owner", e.c, old))
	e.p.f.Input.Complete = false
	mustReject(t, e.s.Detect(e.ctx, "owner", e.c))
	if len(e.state(t).Reminder.Revisions) != 2 || e.last(t).Acknowledged != nil {
		t.Fatal("unknown cleared state")
	}
}
func TestR1_007_S01(t *testing.T) {
	e := newCore(t)
	e.detect(t)
	if e.last(t).Acknowledged != nil {
		t.Fatal("precondition")
	}
	e.at(t, cf.Approved)
	if e.state(t).Phase != cf.Approved {
		t.Fatal("risk imposed gate")
	}
}
func TestR1_007_S02(t *testing.T) {
	e := newCore(t)
	e.p.fail = true
	e.detect(t)
	e.at(t, cf.ReviewPhase)
	e.p.f.PMKnown = false
	mustReject(t, e.advance(t, cf.Approved))
	if e.state(t).Phase != cf.ReviewPhase {
		t.Fatal("reminder substituted PM")
	}
}
func TestR1_008_S01(t *testing.T) {
	e := newCore(t)
	e.detect(t)
	e.s.Acknowledge(e.ctx, "owner", e.c, e.last(t).ID)
	e.p.f.Responses = nil
	e.at(t, cf.ReviewPhase)
	mustReject(t, e.advance(t, cf.Approved))
	if len(e.p.f.PMPoints) != 1 {
		t.Fatal("PM points cleared")
	}
}
func TestR1_008_S02(t *testing.T) {
	e := newCore(t)
	e.detect(t)
	e.p.f.PMKnown = false
	e.s.Acknowledge(e.ctx, "owner", e.c, e.last(t).ID)
	e.at(t, cf.ReviewPhase)
	mustReject(t, e.advance(t, cf.Approved))
	if len(e.state(t).Events) != 1 {
		t.Fatal("knowledge synthesized approval")
	}
}

func TestR8_001_S01(t *testing.T) {
	e := newCore(t)
	e.at(t, cf.Released)
	st := e.state(t)
	if st.Phase != cf.Released || len(st.Events) != 6 {
		t.Fatal("not adjacent seven phases")
	}
	for _, v := range st.Events {
		if v.Actor == "" || v.At.IsZero() || v.Coordinate != e.c || v.FactVersion == "" {
			t.Fatal("incomplete audit")
		}
	}
}
func TestR8_001_S02(t *testing.T) {
	for _, name := range []string{"skip", "invalid-state", "wrong-before", "short-sha", "branch", "cross-repo"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			q := cf.AdvanceRequest{Coordinate: e.c, OperationID: e.op(), Expected: cf.Draft, To: cf.ReviewPhase}
			switch name {
			case "skip":
				q.To = cf.Implementing
			case "invalid-state":
				q.To = "COMPLETE"
			case "wrong-before":
				q.Expected = cf.ReviewPhase
				q.To = cf.Approved
			case "short-sha":
				q.Coordinate.Commit = "abcdef0"
			case "branch":
				q.Coordinate.Commit = "main"
			case "cross-repo":
				q.Coordinate.Repository = "other/repo"
			}
			mustReject(t, e.s.Advance(e.ctx, "owner", q))
			if len(e.state(t).Events) != 0 {
				t.Fatal("false advance")
			}
		})
	}
}
func TestR8_002_S01(t *testing.T) {
	for _, decision := range []string{"agree", "disagree", "modify", "zero-points"} {
		t.Run(decision, func(t *testing.T) {
			e := newCore(t)
			if decision == "zero-points" {
				e.p.f.PMPoints = nil
				e.p.f.Responses = nil
			} else {
				e.p.f.Responses[0].Decision = decision
			}
			e.at(t, cf.Approved)
			if e.state(t).Events[1].Facts.CommitteeScope {
				t.Fatal("ordinary depends on R7")
			}
			if decision != "zero-points" && e.p.f.Responses[0].Decision != decision {
				t.Fatal("PM opinion rewritten")
			}
		})
	}
}
func TestR8_002_S02(t *testing.T) {
	for _, name := range []string{"head-moved", "old-approval", "revoked", "missing-reviewer", "missing-pm", "ambiguous-pm", "expired-pm", "old-pm", "unknown-pm", "unknown-reviewers"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			e.at(t, cf.ReviewPhase)
			switch name {
			case "head-moved":
				e.p.f.CurrentHead = strings.Repeat("b", 40)
			case "old-approval":
				e.p.f.Reviews[0].Commit = strings.Repeat("b", 40)
			case "revoked":
				e.p.f.Reviews[0].Valid = false
			case "missing-reviewer":
				e.p.f.RequiredReviewers = append(e.p.f.RequiredReviewers, "missing")
			case "missing-pm":
				e.p.f.Responses = nil
			case "ambiguous-pm":
				e.p.f.Responses[0].Decision = "maybe"
			case "expired-pm":
				e.p.f.Responses[0].Valid = false
			case "old-pm":
				e.p.f.Responses[0].Commit = strings.Repeat("b", 40)
			case "unknown-pm":
				e.p.f.PMKnown = false
			case "unknown-reviewers":
				e.p.f.ReviewsKnown = false
			}
			e.p.f.CommitteeKnown = true
			e.p.f.Committee = cf.Committee{Commit: e.c.Commit, Valid: true, Authorized: true, Evidence: "git:committee/1"}
			mustReject(t, e.advance(t, cf.Approved))
			if e.state(t).Phase != cf.ReviewPhase {
				t.Fatal("false approval")
			}
		})
	}
}
func TestR8_002_S03(t *testing.T) {
	for _, name := range []string{"label", "proposal", "both"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			if name != "proposal" {
				e.p.f.Labels = append(e.p.f.Labels, "committee-review")
			}
			e.p.f.ProposalCommittee = name != "label"
			e.p.f.CommitteeKnown = true
			e.p.f.Committee = cf.Committee{Commit: e.c.Commit, Valid: true, Authorized: true, Evidence: "git:committee/1"}
			e.at(t, cf.Approved)
			if !e.state(t).Events[1].Facts.CommitteeScope {
				t.Fatal("missing explicit classification")
			}
			if e.state(t).Events[1].Facts.ProposalCommittee != (name != "label") {
				t.Fatal("proposal classification source lost")
			}

		})
	}
}
func TestR8_002_S04(t *testing.T) {
	for _, name := range []string{"missing", "old", "unverified-authority", "unchanged-only"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			e.p.f.Labels = append(e.p.f.Labels, "committee-review")
			e.at(t, cf.ReviewPhase)
			e.p.f.CommitteeKnown = name != "missing"
			e.p.f.Committee = cf.Committee{Commit: e.c.Commit, Valid: true, Authorized: true, Evidence: "git:committee/1"}
			switch name {
			case "old":
				e.p.f.Committee.Commit = strings.Repeat("b", 40)
			case "unverified-authority":
				e.p.f.Committee.Authorized = false
			case "unchanged-only":
				e.p.f.Committee.Valid = false
			}
			mustReject(t, e.advance(t, cf.Approved))
		})
	}
}
func TestR8_002_S05(t *testing.T) {
	for _, name := range []string{"risk-label", "committee-author", "near-label"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			switch name {
			case "committee-author":
				e.p.f.Owner = "committee-member"
			case "near-label":
				e.p.f.Labels = append(e.p.f.Labels, "committee-review-note")
			}
			e.at(t, cf.Approved)
		})
	}
}
func TestR8_002_S06(t *testing.T) {
	for _, name := range []string{"head", "label", "proposal", "unknown-labels", "unknown-reviewers"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			e.at(t, cf.ReviewPhase)
			e.p.change = func(f *cf.Snapshot) {
				switch name {
				case "head":
					f.CurrentHead = strings.Repeat("b", 40)
				case "label":
					f.Labels = append(f.Labels, "committee-review")
				case "proposal":
					f.ProposalCommittee = true
				case "unknown-labels":
					f.LabelsKnown = false
				case "unknown-reviewers":
					f.ReviewsKnown = false
				}
			}
			mustReject(t, e.advance(t, cf.Approved))
			if e.state(t).Phase != cf.ReviewPhase || len(e.state(t).Events) != 1 {
				t.Fatal("commit used stale snapshot")
			}
		})
	}
}
func TestR8_003_S01(t *testing.T) {
	e := newCore(t)
	other := strings.Repeat("b", 40)
	e.p.f.Tasks[0].Commit = other
	e.p.f.Applicable[other] = true
	e.at(t, cf.Acceptance)
	if e.state(t).Phase != cf.Acceptance || len(e.state(t).Results) != 0 {
		t.Fatal("acceptance implies passed results")
	}
}
func TestR8_003_S02(t *testing.T) {
	for _, name := range []string{"no-handoff", "incomplete", "unknown-set", "old-task", "empty-set", "no-evidence", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			e.at(t, cf.Implementing)
			switch name {
			case "no-handoff":
				e.p.f.Tasks[0].Summary = ""
			case "incomplete":
				e.p.f.Tasks[0].Complete = false
			case "unknown-set":
				e.p.f.TasksKnown = false
			case "old-task":
				e.p.f.Tasks[0].Commit = strings.Repeat("b", 40)
			case "empty-set":
				e.p.f.Tasks = nil
			case "no-evidence":
				e.p.f.Tasks[0].Evidence = ""
			case "duplicate":
				e.p.f.Tasks = append(e.p.f.Tasks, e.p.f.Tasks[0])
			}
			mustReject(t, e.advance(t, cf.Acceptance))
			if e.state(t).Phase != cf.Implementing {
				t.Fatal("checkbox-only pass")
			}
		})
	}
}
func TestR8_004_S01(t *testing.T) {
	for _, status := range []string{"PASS", "NOT_RUN", "NOT_APPLICABLE"} {
		t.Run(status, func(t *testing.T) {
			e := newCore(t)
			e.at(t, cf.Acceptance)
			e.results(t, status)
			if err := e.advance(t, cf.Archived); err != nil {
				t.Fatal(err)
			}
			v, err := e.s.View(e.ctx, "owner", e.c)
			if err != nil || v.State.Phase != cf.Archived || (status != "PASS" && v.AllExecutedPassed) {
				t.Fatal("unexecuted washed green", v, err)
			}
		})
	}
}
func TestR8_004_S02(t *testing.T) {
	for _, name := range []string{"fail", "missing-scenario", "no-evidence", "no-reason", "unknown-set"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			e.at(t, cf.Acceptance)
			switch name {
			case "fail":
				e.results(t, "FAIL")
			case "missing-scenario":
				q := cf.ResultRequest{Coordinate: e.c, OperationID: e.op(), Scenario: "S01", Status: "PASS", TestedCommit: e.c.Commit, Evidence: "log:one"}
				if err := e.s.RecordResult(e.ctx, "qa", q); err != nil {
					t.Fatal(err)
				}
			case "no-evidence", "no-reason":
				q := cf.ResultRequest{Coordinate: e.c, OperationID: e.op(), Scenario: "S01", Status: "PASS", TestedCommit: e.c.Commit}
				if name == "no-reason" {
					q.Status = "NOT_RUN"
					q.Evidence = "report:skip"
				}
				mustReject(t, e.s.RecordResult(e.ctx, "qa", q))
			case "unknown-set":
				e.results(t, "PASS")
				e.p.f.ScenariosKnown = false
			}
			mustReject(t, e.advance(t, cf.Archived))
			if e.state(t).Phase != cf.Acceptance {
				t.Fatal("bad archive")
			}
		})
	}
}
func TestR8_005_S01(t *testing.T) {
	e := newCore(t)
	e.at(t, cf.Archived)
	evidence := append([]cf.Result(nil), e.state(t).Results...)
	if err := e.advance(t, cf.Released); err != nil {
		t.Fatal(err)
	}
	st := e.state(t)
	if len(st.Results) != len(evidence) || st.Events[len(st.Events)-1].Facts.Release.Environment != "staging" {
		t.Fatal("release environment/history wrong")
	}
}
func TestR8_005_S02(t *testing.T) {
	for _, name := range []string{"archive-only", "no-environment", "failed", "other-change", "old-commit", "staging-as-production"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			e.at(t, cf.Archived)
			environment := "staging"
			switch name {
			case "archive-only":
				e.p.f.Release.ID = ""
			case "no-environment":
				e.p.f.Release.Environment = ""
			case "failed":
				e.p.f.Release.Success = false
			case "other-change":
				e.p.f.Release.Coordinate.ChangeID = "other"
			case "old-commit":
				e.p.f.Release.Commit = strings.Repeat("b", 40)
			case "staging-as-production":
				environment = "production"
			}
			mustReject(t, e.s.Advance(e.ctx, "owner", cf.AdvanceRequest{Coordinate: e.c, OperationID: e.op(), Expected: cf.Archived, To: cf.Released, Environment: environment}))
			if e.state(t).Phase != cf.Archived {
				t.Fatal("fake release")
			}
		})
	}
}
func TestR8_006_S01(t *testing.T) {
	e := newCore(t)
	e.p.f.Scenarios = []string{"S01", "S02", "S03", "S04"}
	for i, status := range []string{"PASS", "FAIL", "NOT_RUN", "NOT_APPLICABLE"} {
		q := cf.ResultRequest{Coordinate: e.c, OperationID: e.op(), Scenario: e.p.f.Scenarios[i], Status: status, TestedCommit: e.c.Commit, Evidence: "report:case", Reason: "explicit reason"}
		if err := e.s.RecordResult(e.ctx, "qa", q); err != nil {
			t.Fatal(err)
		}
	}
	old := e.state(t).Results[1]
	q := cf.ResultRequest{Coordinate: e.c, OperationID: e.op(), Scenario: old.Scenario, Status: "PASS", TestedCommit: e.c.Commit, Evidence: "log:corrected", Supersedes: old.ID}
	if err := e.s.RecordResult(e.ctx, "qa", q); err != nil {
		t.Fatal(err)
	}
	st := e.state(t)
	if len(st.Results) != 5 || st.Results[1].Status != "FAIL" || st.Results[4].Supersedes != old.ID || st.Results[4].Actor != "qa" || st.Results[4].At.IsZero() {
		t.Fatal("correction overwrote history")
	}
}
func TestR8_006_S02(t *testing.T) {
	for _, name := range []string{"test-name", "empty-evidence", "bad-state", "unknown-id", "no-reason", "forbidden", "old-tested-commit", "bad-correction"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			q := cf.ResultRequest{Coordinate: e.c, OperationID: e.op(), Scenario: "S01", Status: "PASS", TestedCommit: e.c.Commit, Evidence: "log:case"}
			switch name {
			case "test-name":
				q.Evidence = "TestExample"
			case "empty-evidence":
				q.Evidence = ""
			case "bad-state":
				q.Status = "SKIPPED"
			case "unknown-id":
				q.Scenario = "unknown"
			case "no-reason":
				q.Status = "NOT_APPLICABLE"
			case "forbidden":
				e.p.denied["result"] = true
			case "old-tested-commit":
				q.TestedCommit = strings.Repeat("b", 40)
			case "bad-correction":
				q.Supersedes = "not-an-original"
			}
			mustReject(t, e.s.RecordResult(e.ctx, "qa", q))
			if len(e.state(t).Results) != 0 {
				t.Fatal("invalid result persisted")
			}
		})
	}
}
func TestR8_007_S01(t *testing.T) {
	e := newCore(t)
	e.at(t, cf.ReviewPhase)
	q := cf.AdvanceRequest{Coordinate: e.c, OperationID: e.op(), Expected: cf.ReviewPhase, To: cf.Approved}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- e.s.Advance(e.ctx, "owner", q) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	st := e.state(t)
	if len(st.Events) != 2 {
		t.Fatal("duplicate advance")
	}
	at := st.Events[1].At
	if err := e.s.Advance(e.ctx, "owner", q); err != nil || !e.state(t).Events[1].At.Equal(at) {
		t.Fatal("retry changed timestamp", err)
	}
	q.OperationID = e.op()
	mustReject(t, e.s.Advance(e.ctx, "owner", q))
}
func TestR8_007_S02(t *testing.T) {
	for _, name := range []string{"head", "revoked", "committee", "pm", "scenarios", "dependency"} {
		t.Run(name, func(t *testing.T) {
			e := newCore(t)
			e.at(t, cf.Acceptance)
			e.results(t, "PASS")
			e.p.change = func(f *cf.Snapshot) {
				switch name {
				case "head":
					f.CurrentHead = strings.Repeat("b", 40)
				case "revoked":
					f.Reviews[0].Valid = false
				case "committee":
					f.ProposalCommittee = true
				case "pm":
					f.Responses[0].Valid = false
				case "scenarios":
					f.Scenarios = append(f.Scenarios, "S03")
				case "dependency":
					f.PMKnown = false
				}
			}
			mustReject(t, e.advance(t, cf.Archived))
			if e.state(t).Phase != cf.Acceptance {
				t.Fatal("stale commit advance")
			}
		})
	}
	t.Run("new-local-FAIL", func(t *testing.T) {
		e := newCore(t)
		e.at(t, cf.Acceptance)
		e.results(t, "PASS")
		old := e.state(t).Results[0]
		if err := e.s.RecordResult(e.ctx, "qa", cf.ResultRequest{Coordinate: e.c, OperationID: e.op(), Scenario: old.Scenario, Status: "FAIL", TestedCommit: e.c.Commit, Evidence: "log:new-failure", Supersedes: old.ID}); err != nil {
			t.Fatal(err)
		}
		mustReject(t, e.advance(t, cf.Archived))
	})
}
func TestR8_008_S01(t *testing.T) {
	e := newCore(t)
	for _, name := range []string{"strict", "tasks", "archive", "merge"} {
		if err := e.s.RecordTool(e.ctx, "owner", cf.ToolRequest{Coordinate: e.c, OperationID: e.op(), Name: name, Evidence: "log:tool", Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	st := e.state(t)
	if st.Phase != cf.Draft || len(st.Events) != 0 || len(st.Tools) != 4 {
		t.Fatal("tool advanced business phase")
	}
}
func TestR8_008_S02(t *testing.T) {
	for _, status := range []string{"FAIL", "NOT_RUN"} {
		t.Run(status, func(t *testing.T) {
			e := newCore(t)
			e.at(t, cf.Acceptance)
			e.results(t, status)
			if err := e.s.RecordTool(e.ctx, "owner", cf.ToolRequest{Coordinate: e.c, OperationID: e.op(), Name: "archive", Evidence: "log:cli", Success: true}); err != nil {
				t.Fatal(err)
			}
			err := e.advance(t, cf.Archived)
			if status == "FAIL" {
				mustReject(t, err)
			} else if err != nil {
				t.Fatal(err)
			}
			v, err := e.s.View(e.ctx, "owner", e.c)
			if err != nil || v.AllExecutedPassed || v.State.Phase == cf.Released {
				t.Fatal("archive washed evidence", err)
			}
		})
	}
}
func TestR8_009_S01(t *testing.T) {
	e := newCore(t)
	e.at(t, cf.Acceptance)
	e.results(t, "NOT_RUN")
	e.advance(t, cf.Archived)
	v, err := e.s.View(e.ctx, "owner", e.c)
	if err != nil || v.Counts["NOT_RUN"] != 2 || v.AllExecutedPassed || v.State.Coordinate != e.c || len(v.State.Events) != 5 {
		t.Fatal("missing view facts", err)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "Patch") {
		t.Fatal("source in Console")
	}
	e.p.f.CurrentHead = strings.Repeat("b", 40)
	v, err = e.s.View(e.ctx, "owner", e.c)
	if err != nil || !v.OldVersion || v.BlockingReason == "" {
		t.Fatal("old version not distinguished")
	}
}
func TestR8_009_S02(t *testing.T) {
	t.Run("revoked-or-provider-rebound", func(t *testing.T) {
		e := newCore(t)
		e.at(t, cf.ReviewPhase)
		e.p.allowed = false
		if _, err := e.s.View(e.ctx, "owner", e.c); err == nil {
			t.Fatal("cached private view")
		}
		e.p.allowed = true
		other := e.c
		other.Provider = "gongfeng"
		if _, err := e.s.View(e.ctx, "owner", other); err == nil {
			t.Fatal("provider migration reused ACL")
		}
	})
	t.Run("unknown", func(t *testing.T) {
		e := newCore(t)
		e.at(t, cf.Acceptance)
		e.results(t, "PASS")
		e.p.f.PMKnown = false
		v, err := e.s.View(e.ctx, "owner", e.c)
		if err != nil || v.BlockingReason == "" || v.AllExecutedPassed {
			t.Fatal("unknown green", err)
		}
	})
	t.Run("audit-commit-failure", func(t *testing.T) {
		e := newCore(t)
		e.at(t, cf.ReviewPhase)
		if _, err := e.db.db.Exec(`CREATE TRIGGER fail_update_core BEFORE UPDATE ON changeflow_evidence BEGIN SELECT RAISE(ABORT,'audit failure'); END;`); err != nil {
			t.Fatal(err)
		}
		mustReject(t, e.advance(t, cf.Approved))
		if e.state(t).Phase != cf.ReviewPhase || len(e.state(t).Events) != 1 {
			t.Fatal("phase without audit")
		}
	})
}

func TestChangeflowMigrationRestartAndHistoryFence(t *testing.T) {
	e := newCore(t)
	e.detect(t)
	e.s.Acknowledge(e.ctx, "owner", e.c, e.last(t).ID)
	e.at(t, cf.ReviewPhase)
	var count int
	if err := e.db.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version='0069_changeflow_evidence.sql'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("r45 migration ledger", count, err)
	}
	if err := e.db.Migrate(e.ctx); err != nil {
		t.Fatal("migration retry", err)
	}
	for _, change := range []func(*cf.State){func(st *cf.State) { st.Events[0].Actor = "spoof" }, func(st *cf.State) { st.Reminder.Revisions[0].Hits = nil }, func(st *cf.State) { st.Reminder = nil }, func(st *cf.State) { st.Events = nil }} {
		mustReject(t, e.db.Transact(e.ctx, e.c, func(st *cf.State) error { change(st); return nil }))
	}
	if len(e.state(t).Events) != 1 || len(e.last(t).Hits) == 0 {
		t.Fatal("history overwritten")
	}
}

func TestChangeflowSQLiteReopen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "restart.db")
	db := openMigrated(t, file)
	p := &coreProvider{f: coreFacts(), allowed: true, denied: map[string]bool{}}
	service := &cf.Service{Store: db, Provider: p}
	if err := service.Detect(context.Background(), "owner", p.f.Coordinate); err != nil {
		t.Fatal(err)
	}
	before, err := db.Read(context.Background(), p.f.Coordinate)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db = openMigrated(t, file)
	defer db.Close()
	after, err := db.Read(context.Background(), p.f.Coordinate)
	if err != nil || !jsonSame(before, after) {
		t.Fatal("restart lost reminder/audit", err)
	}
}

func TestR8_004_S01_ExecutedFactsIndependentOfRelease(t *testing.T) {
	e := newCore(t)
	e.at(t, cf.Archived)
	e.p.f.Release = cf.Release{}
	v, err := e.s.View(e.ctx, "owner", e.c)
	if err != nil || !v.AllExecutedPassed || v.BlockingReason == "" || v.State.Phase != cf.Archived {
		t.Fatal("missing release erased executed PASS facts", err)
	}
}

func TestR1_007_S02_UnknownRiskDoesNotAddLifecycleGate(t *testing.T) {
	e := newCore(t)
	e.p.f.Input.Complete = false
	mustReject(t, e.s.Detect(e.ctx, "owner", e.c))
	e.at(t, cf.Approved)
	v, err := e.s.View(e.ctx, "owner", e.c)
	if err != nil || v.RiskStatus != "unknown" || v.State.Phase != cf.Approved {
		t.Fatal("risk unknown forged green or imposed gate", err)
	}
}

func TestChangeflowMigrationLedgerRecoveryAndMismatch(t *testing.T) {
	for _, kind := range []string{"exact-retained-data", "mismatched-table", "view-instead-of-table"} {
		t.Run(kind, func(t *testing.T) {
			e := newCore(t)
			e.detect(t)
			before := e.state(t)
			if _, err := e.db.db.Exec(`DELETE FROM schema_migrations WHERE version='0069_changeflow_evidence.sql'`); err != nil {
				t.Fatal(err)
			}
			if kind == "mismatched-table" {
				e.db.db.Exec(`ALTER TABLE changeflow_evidence ADD COLUMN unexpected TEXT`)
			}
			if kind == "view-instead-of-table" {
				e.db.db.Exec(`DROP TABLE changeflow_evidence`)
				e.db.db.Exec(`CREATE VIEW changeflow_evidence AS SELECT 'x' coordinate_key,1 revision,'{}' body`)
			}
			err := e.db.Migrate(e.ctx)
			var count int
			e.db.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version='0069_changeflow_evidence.sql'`).Scan(&count)
			if kind == "exact-retained-data" {
				if err != nil || count != 1 || !jsonSame(before, e.state(t)) {
					t.Fatal("lost retained data or failed backfill", err)
				}
			} else if err == nil || count != 0 {
				t.Fatal("mismatch silently backfilled", err, count)
			}
		})
	}
}
