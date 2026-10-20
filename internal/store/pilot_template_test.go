package store

import (
	"context"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"strings"
	"testing"
	"time"
)

func templateFixture(t *testing.T) (*SQLite, time.Time, TemplateDocument, TemplatePrincipalSnapshot) {
	t.Helper()
	s := newConsoleSpecStore(t)
	now := time.Now().UTC()
	for _, id := range []string{"dev", "review"} {
		if e := s.UpsertConsoleAgent(context.Background(), model.ConsoleAgent{ID: id, KeyID: "key-" + id, SessionName: id, OwnerPrincipal: "owner-a", Active: true}); e != nil {
			t.Fatal(e)
		}
	}
	source, e := s.SaveTemplateSource(context.Background(), "owner-a", "source", TemplateSource{URL: "https://fixture.feishu.cn/docx/SourceDoc", Kind: "manual_export", Revision: "export-v1", Content: "R1: a bounded deliverable"}, func() error { return nil }, now)
	if e != nil {
		t.Fatal(e)
	}
	d := TemplateDocument{Kind: "parent", Title: "bounded parent", Goal: "deliver only R1", Scope: "fixture only", SourceID: source.ID, TeamMode: "single", Defaults: TemplateFields{Acceptance: "exact fixture output", Method: "local fixture regression", Deliverable: "versioned artifact", Environment: "isolated fixture", Executor: "agent:dev", Reviewer: "agent:review", Signoff: "user:owner-a"}, Entries: []TemplateEntry{{ID: "R1", Kind: "requirement", Title: "bounded output", OwnerRef: "R1"}}}
	snap := TemplatePrincipalSnapshot{Active: map[string]bool{"user:owner-a": true, "agent:dev": true, "agent:review": true}, AgentOwners: map[string]string{"dev": "owner-a", "review": "owner-a"}}
	return s, now, d, snap
}
func templateSave(t *testing.T, s *SQLite, now time.Time, d TemplateDocument, snap TemplatePrincipalSnapshot, req string) TemplateView {
	t.Helper()
	v, e := s.SaveTemplateDraft(context.Background(), "owner-a", "", TemplateOperation{RequestID: req, Document: &d}, snap, func() error { return nil }, now)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func templateAcceptAll(t *testing.T, s *SQLite, now time.Time, v TemplateView, snap TemplatePrincipalSnapshot) TemplateView {
	t.Helper()
	for _, r := range v.Roles {
		a := BlockerActor{}
		if strings.HasPrefix(r.Principal, "user:") {
			a.UserID = strings.TrimPrefix(r.Principal, "user:")
		} else {
			a.AgentID = strings.TrimPrefix(r.Principal, "agent:")
		}
		ref, _ := s.TemplateReferences(context.Background(), v.ID)
		var e error
		v, e = s.UpdateTemplateResponsibility(context.Background(), ref, a, TemplateOperation{RequestID: fmt.Sprintf("accept-%s-%s-%s-%d", v.ID, r.EntryID, r.Role, v.Version), ExpectedVersion: v.Version, PlanSHA: v.PlanSHA, EntryIDs: []string{r.EntryID}, Role: r.Role, Decision: "accepted", ConfirmHuman: true, Reason: "independently checked bounded contract"}, snap, func() error { return nil }, now)
		if e != nil {
			t.Fatalf("role %s: %v missing=%v", r.Role, e, v.Missing)
		}
	}
	return v
}
func templateConfirm(t *testing.T, s *SQLite, now time.Time, v TemplateView, snap TemplatePrincipalSnapshot) TemplateView {
	t.Helper()
	ref, _ := s.TemplateReferences(context.Background(), v.ID)
	out, e := s.ConfirmTemplateDraft(context.Background(), ref, "owner-a", TemplateOperation{RequestID: "confirm-" + v.ID, ExpectedVersion: v.Version, PlanSHA: v.PlanSHA, ConfirmHuman: true}, snap, func() error { return nil }, now)
	if e != nil {
		t.Fatalf("confirm: %v missing=%v", e, v.Missing)
	}
	return out
}
func templateReviewFinal(t *testing.T, s *SQLite, now time.Time, v TemplateView) {
	t.Helper()
	ctx := context.Background()
	rev, e := s.ConsoleSpecRevision(ctx, "owner-a", v.SpecID, v.SpecRevision, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SubmitConsoleSpecReview(ctx, v.SpecID, "owner-a", now); e != nil {
		t.Fatal(e)
	}
	q, e := s.StartSpecAgentRequest(ctx, SpecAgentRequest{ID: "review-" + v.SpecID, ClientID: "client-" + v.SpecID, SpecID: v.SpecID, AgentID: "review", Actor: "owner-a", Kind: "review", Revision: v.SpecRevision, SHA: rev.ContentSHA256, Boot: "fixture"}, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	a := model.ConsoleAgent{ID: "review", KeyID: "key-review", SessionName: "review", OwnerPrincipal: "owner-a", Active: true}
	if _, e = s.CompleteSpecAgentRequest(ctx, q.ID, a, r8Result(q), nil, now); e != nil {
		t.Fatal(e)
	}
	gate, e := s.ConsoleSpecGateReview(ctx, v.SpecID, v.SpecRevision)
	if e != nil {
		t.Fatal(e)
	}
	gate.IssuesJSON = `[{"id":"I1","text":"check owner quote","response":"verified fixture","outcome":"false_positive"}]`
	if e = s.SaveConsoleSpecGateReview(ctx, v.SpecID, "owner-a", *gate, now); e != nil {
		t.Fatal(e)
	}
	if e = s.FinalizeConsoleSpec(ctx, v.SpecID, "owner-a", now); e != nil {
		t.Fatal(e)
	}
}
func TestPilotTemplateActualResponsibilityAndR8Finalization(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	v := templateSave(t, s, now, d, snap, "draft")
	if v.CanConfirm || v.ExecutionAuthorized || v.BusinessSignedOff {
		t.Fatal("TEMPLATE_PREVIEW_IS_AUTHORIZATION")
	}
	v = templateAcceptAll(t, s, now, v, snap)
	if !v.CanConfirm {
		t.Fatal("TEMPLATE_RECEIPTS_NOT_COMPLETE", v.Missing)
	}
	v = templateConfirm(t, s, now, v, snap)
	spec, _ := s.ConsoleSpec(context.Background(), "owner-a", v.SpecID, false)
	if spec.State != "editing" || v.ExecutionAuthorized || v.BusinessSignedOff {
		t.Fatal("TEMPLATE_CONFIRM_FINALIZED_OR_EXECUTED")
	}
	if e := s.FinalizeConsoleSpec(context.Background(), v.SpecID, "owner-a", now); e == nil {
		t.Fatal("TEMPLATE_FRESH_AGENT_REVIEW_BYPASSED")
	}
	templateReviewFinal(t, s, now, v)
}
func TestPilotTemplateMissingSixFieldsAndIdentity(t *testing.T) {
	for _, field := range []string{"acceptance", "method", "deliverable", "environment", "executor", "signoff"} {
		t.Run(field, func(t *testing.T) {
			s, now, d, snap := templateFixture(t)
			switch field {
			case "acceptance":
				d.Defaults.Acceptance = "unknown"
			case "method":
				d.Defaults.Method = "待定"
			case "deliverable":
				d.Defaults.Deliverable = "TBD"
			case "environment":
				d.Defaults.Environment = "未认领"
			case "executor":
				d.Defaults.Executor = "unknown"
			case "signoff":
				d.Defaults.Signoff = "agent:review"
			}
			v := templateSave(t, s, now, d, snap, field)
			ref, _ := s.TemplateReferences(context.Background(), v.ID)
			if _, e := s.ConfirmTemplateDraft(context.Background(), ref, "owner-a", TemplateOperation{RequestID: "confirm", ExpectedVersion: 1, PlanSHA: v.PlanSHA, ConfirmHuman: true}, snap, func() error { return nil }, now); e == nil {
				t.Fatal("TEMPLATE_SIX_FIELD_GATE_BYPASSED", field)
			}
			if len(v.Missing) < 4 {
				t.Fatal("TEMPLATE_MISSING_NOT_EXPLICIT", v.Missing)
			}
		})
	}
}
func TestPilotTemplateReceiptCannotProxyAndWithdrawal(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	v := templateSave(t, s, now, d, snap, "draft")
	ref, _ := s.TemplateReferences(context.Background(), v.ID)
	u := TemplateOperation{RequestID: "proxy", ExpectedVersion: 1, PlanSHA: v.PlanSHA, EntryIDs: []string{"R1"}, Role: "executor", Decision: "accepted"}
	if _, e := s.UpdateTemplateResponsibility(context.Background(), ref, BlockerActor{UserID: "owner-a"}, u, snap, func() error { return nil }, now); e == nil {
		t.Fatal("TEMPLATE_OWNER_PROXY_ACCEPTED")
	}
	u.Role = "signoff"
	if _, e := s.UpdateTemplateResponsibility(context.Background(), ref, BlockerActor{AgentID: "review"}, u, snap, func() error { return nil }, now); e == nil {
		t.Fatal("TEMPLATE_AGENT_SIGNOFF_ACCEPTED")
	}
	v = templateConfirm(t, s, now, templateAcceptAll(t, s, now, v, snap), snap)
	ref, _ = s.TemplateReferences(context.Background(), v.ID)
	u = TemplateOperation{RequestID: "withdraw", ExpectedVersion: 1, PlanSHA: v.PlanSHA, EntryIDs: []string{"R1"}, Role: "executor", Decision: "withdrawn", Reason: "no longer able"}
	if _, e := s.UpdateTemplateResponsibility(context.Background(), ref, BlockerActor{AgentID: "dev"}, u, snap, func() error { return nil }, now); e != nil {
		t.Fatal(e)
	}
	if e := s.SubmitConsoleSpecReview(context.Background(), v.SpecID, "owner-a", now); e == nil {
		t.Fatal("TEMPLATE_WITHDRAWAL_DID_NOT_BLOCK")
	}
}
func TestPilotTemplateCheckFailureRollsBack(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	_, e := s.SaveTemplateDraft(context.Background(), "owner-a", "", TemplateOperation{RequestID: "rollback", Document: &d}, snap, func() error { return errors.New("revoked") }, now)
	if e == nil {
		t.Fatal("TEMPLATE_REVOKE_CHECK_IGNORED")
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM pilot_template_draft`).Scan(&n)
	if n != 0 {
		t.Fatal("TEMPLATE_REVOKED_DRAFT_COMMITTED")
	}
}
func TestPilotTemplateSourceProvenanceDedupeAndSSRF(t *testing.T) {
	s, now, _, _ := templateFixture(t)
	ctx := context.Background()
	input := TemplateSource{URL: "https://fixture.feishu.cn/docx/SourceDoc", Revision: "export-v1", Kind: "manual_export", Content: "R1: a bounded deliverable"}
	v, e := s.SaveTemplateSource(ctx, "owner-a", "same-source", input, func() error { return nil }, now)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM pilot_spec_source`).Scan(&n)
	if n != 1 || v.SHA != templateContentSHA(input.Content) {
		t.Fatal("TEMPLATE_SOURCE_DUPLICATED_OR_CLIENT_SHA")
	}
	for _, url := range []string{"http://fixture.feishu.cn/docx/Doc", "https://127.0.0.1/docx/Doc", "https://fixture.feishu.cn.evil.invalid/docx/Doc", "https://fixture.feishu.cn/docx/Doc?token=x", "https://user:pass@fixture.feishu.cn/docx/Doc", "https://fixture.feishu.cn:443/docx/Doc", "https://fixture.feishu.cn/docx/%2e%2e"} {
		if _, _, _, e := ParseTemplateSourceURL(url); e == nil {
			t.Fatal("TEMPLATE_SSRF_SOURCE_ACCEPTED", url)
		}
	}
	input.Content = "changed"
	if _, e = s.SaveTemplateSource(ctx, "owner-a", "same-source", input, func() error { return nil }, now); e == nil {
		t.Fatal("TEMPLATE_SOURCE_REQUEST_REBOUND")
	}
	if _, e = s.TemplateSource(ctx, "owner-b", v.ID); e == nil {
		t.Fatal("TEMPLATE_SOURCE_CROSS_OWNER_READ")
	}
}
func TestPilotTemplateRevisionInvalidatesReceiptsAndKeepsOldBody(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	v := templateAcceptAll(t, s, now, templateSave(t, s, now, d, snap, "draft"), snap)
	oldSHA := v.ContentSHA
	d = v.Document
	d.Entries[0].Fields.Deliverable = "different artifact"
	v2, e := s.SaveTemplateDraft(context.Background(), "owner-a", v.ID, TemplateOperation{RequestID: "newversion", ExpectedVersion: 1, Document: &d}, snap, func() error { return nil }, now)
	if e != nil {
		t.Fatal(e)
	}
	if v2.Version != 2 || v2.CanConfirm || v2.ContentSHA == oldSHA {
		t.Fatal("TEMPLATE_SILENT_PLAN_REWRITE")
	}
	var sha string
	s.db.QueryRow(`SELECT content_sha256 FROM pilot_template_revision WHERE draft_id=? AND version=1`, v.ID).Scan(&sha)
	if sha != oldSHA {
		t.Fatal("TEMPLATE_HISTORY_OVERWRITTEN")
	}
	for _, r := range v2.Roles {
		if r.Accepted {
			t.Fatal("TEMPLATE_OLD_RECEIPT_REUSED")
		}
	}
	ref, _ := s.TemplateReferences(context.Background(), v.ID)
	if _, e = s.UpdateTemplateResponsibility(context.Background(), ref, BlockerActor{AgentID: "dev"}, TemplateOperation{RequestID: "late", ExpectedVersion: 1, PlanSHA: v.PlanSHA, EntryIDs: []string{"R1"}, Role: "executor", Decision: "accepted"}, snap, func() error { return nil }, now); e == nil {
		t.Fatal("TEMPLATE_LATE_PLAN_RECEIPT_ACCEPTED")
	}
}
func TestPilotTemplateRiskUpgradeUnknownAndStockBudget(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	v := templateSave(t, s, now, d, snap, "light")
	if v.Mode != "light" || v.Document.RiskAssessment != "unassessed" {
		t.Fatal("TEMPLATE_UNKNOWN_RISK_FALSIFIED")
	}
	for _, risk := range []string{"production", "security", "attribute_security", "attribute_production"} {
		r := d
		switch risk {
		case "production":
			r.Production = true
		case "security":
			r.Security = true
		case "attribute_security":
			r.Full.Attributes = map[string]string{"安全面": "是"}
		case "attribute_production":
			r.Full.Attributes = map[string]string{"在产影响": "灰度可回滚"}
		}
		v = templateSave(t, s, now, r, snap, risk)
		if v.Mode != "full" || v.CanConfirm {
			t.Fatal("TEMPLATE_RISK_DID_NOT_FORCE_FULL", risk)
		}
		missing := false
		for _, m := range v.Missing {
			if strings.HasPrefix(m.Path, "full.") {
				missing = true
			}
		}
		if !missing {
			t.Fatal("TEMPLATE_FULL_GATE_MISSING")
		}
	}
	stock := StockTemplateSample()
	if len(stock.Entries) != 13 {
		t.Fatal("TEMPLATE_STOCK_CASES_CHANGED")
	}
	for _, e := range stock.Entries {
		want := 0
		if e.ID == "B06-applied-lost" {
			want = 1
		}
		if e.BusinessWriteLimit != want {
			t.Fatal("TEMPLATE_STOCK_WRITE_BUDGET_CHANGED")
		}
	}
	stock = templateExpand(stock)
	stock.Entries[0].BusinessWriteLimit = 1
	missing := templateValidate(stock, TemplateSource{}, now)
	seen := false
	for _, m := range missing {
		if strings.Contains(m.Reason, "stock13_original_kind_and_budget_required") {
			seen = true
		}
	}
	if !seen {
		t.Fatal("TEMPLATE_STOCK_TAMPERED_BUDGET_ACCEPTED", missing)
	}
}
func TestPilotTemplateRealFinalSuccessorAndStickyEnrollment(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	ctx := context.Background()
	v := templateConfirm(t, s, now, templateAcceptAll(t, s, now, templateSave(t, s, now, d, snap, "parent"), snap), snap)
	templateReviewFinal(t, s, now, v)
	original, e := s.ConsoleSpecRevision(ctx, "owner-a", v.SpecID, 1, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AppendConsoleSpecRevision(ctx, "owner-a", v.SpecID, 1, specRevision(v.SpecID, 2, "changed"), now); !errors.Is(e, ErrConsoleSpecFinal) {
		t.Fatal("TEMPLATE_FINAL_MUTATED_IN_PLACE", e)
	}
	// A child must anchor the exact final parent, not a mutable editor preview.
	child := d
	child.Kind = "child"
	child.Parent = &TemplateParent{SpecID: v.SpecID, Revision: 1, SHA: original.ContentSHA256}
	child.Title = "selected child"
	cv := templateSave(t, s, now, child, snap, "child")
	cv = templateAcceptAll(t, s, now, cv, snap)
	if !cv.CanConfirm {
		t.Fatal("TEMPLATE_FINAL_PARENT_REJECTED", cv.Missing)
	}
	cv = templateConfirm(t, s, now, cv, snap)
	if cv.SpecID == v.SpecID {
		t.Fatal("TEMPLATE_CHILD_REUSED_PARENT")
	}
	changed := d
	changed.PredecessorSpecID = v.SpecID
	changed.ExpectedRevision = 1
	changed.ChangeReason = "new bounded requirement"
	changed.Entries[0].Fields.Acceptance = "new acceptance"
	nv := templateSave(t, s, now, changed, snap, "successor")
	nv = templateConfirm(t, s, now, templateAcceptAll(t, s, now, nv, snap), snap)
	if nv.SpecRevision != 2 || nv.SpecID == v.SpecID {
		t.Fatal("TEMPLATE_DERIVED_SUCCESSOR_MISSING")
	}
	old, _ := s.ConsoleSpecRevision(ctx, "owner-a", v.SpecID, 1, false)
	copy, _ := s.ConsoleSpecRevision(ctx, "owner-a", nv.SpecID, 1, false)
	if old.ContentSHA256 != original.ContentSHA256 || copy.ContentMarkdown != original.ContentMarkdown {
		t.Fatal("TEMPLATE_FINAL_HISTORY_OVERWRITTEN")
	}
	next, _ := s.ConsoleSpec(ctx, "owner-a", nv.SpecID, false)
	if next.DerivedFrom != v.SpecID || next.State != "editing" {
		t.Fatal("TEMPLATE_REAL_DERIVE_NOT_USED")
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM pilot_template_impact WHERE predecessor_spec_id=?`, v.SpecID).Scan(&n)
	if n != 1 {
		t.Fatal("TEMPLATE_OLD_TASK_IMPACT_MISSING")
	}
	// Actual legacy append creates a new revision, but cannot lose opt-in gates.
	if _, e = s.AppendConsoleSpecRevision(ctx, "owner-a", nv.SpecID, 2, specRevision(nv.SpecID, 3, "manual replacement"), now); e != nil {
		t.Fatal(e)
	}
	if e = s.SubmitConsoleSpecReview(ctx, nv.SpecID, "owner-a", now); e == nil {
		t.Fatal("TEMPLATE_ENROLLMENT_DROPPED_ON_LEGACY_REVISION")
	}
}
func TestPilotTemplateImmutableArtifactsNotExecution(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	v := templateSave(t, s, now, d, snap, "draft")
	a := TemplateArtifacts(v)
	if len(a) != 5 || !strings.Contains(a["reviewer-checklist.md"], "- [ ] R1") || !strings.Contains(a["analyze.json"], `"executed": false`) || !strings.Contains(a["analyze.json"], `"execution_status": "NOT_RUN"`) {
		t.Fatal("TEMPLATE_ARTIFACT_FAKED_RESULT", a)
	}
	if a["spec.md"] != v.ContentMarkdown || !strings.Contains(a["constitution.md"], PilotTemplateVersion) {
		t.Fatal("TEMPLATE_ARTIFACT_VERSION_UNBOUND")
	}
}
func TestPilotTemplateD3ConfirmsActualDerivedSuccessor(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	ctx := context.Background()
	v := templateConfirm(t, s, now, templateAcceptAll(t, s, now, templateSave(t, s, now, d, snap, "original"), snap), snap)
	templateReviewFinal(t, s, now, v)
	c, _ := s.QuestionAuthorizationContext(ctx, v.SpecID, "", "")
	actor := BlockerActor{UserID: "owner-a"}
	if _, err := s.SetQuestionPolicy(ctx, c, actor, QuestionUpdate{RequestID: "policy", PMUser: "owner-a", LeaderUser: "owner-a"}, func() error { return nil }, now); err != nil {
		t.Fatal(err)
	}
	out, err := s.UpdateQuestion(ctx, c, actor, "", QuestionUpdate{RequestID: "question", Operation: "create", ExpectedVersion: 1, Kind: "change", Title: "bounded change", ObservedFact: "missing acceptance", Question: "clarify", ExpectedAnswer: "new version"}, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	q := out.Question
	c, _ = s.QuestionAuthorizationContext(ctx, "", "", q.ID)
	out, err = s.UpdateQuestion(ctx, c, actor, q.ID, QuestionUpdate{RequestID: "proposal", Operation: "answer", ExpectedVersion: q.Version, EventID: "event", AnswerKind: "substantive", Body: "explicit new condition", Evidence: "git:proposal"}, func() error { return nil }, now)
	if err != nil {
		t.Fatal(err)
	}
	var cid string
	s.db.QueryRow(`SELECT id FROM pilot_question_change WHERE question_id=?`, q.ID).Scan(&cid)
	changed := d
	changed.PredecessorSpecID = v.SpecID
	changed.ExpectedRevision = 1
	changed.ChangeReason = "owner-approved proposal"
	changed.Goal = "new bounded goal"
	nv := templateSave(t, s, now, changed, snap, "successor")
	nv = templateConfirm(t, s, now, templateAcceptAll(t, s, now, nv, snap), snap)
	c, _ = s.QuestionAuthorizationContext(ctx, "", "", q.ID)
	u := QuestionUpdate{RequestID: "decide", Operation: "confirm_change", ExpectedVersion: out.Question.Version, ChangeID: cid, NewSpecID: nv.SpecID, NewRevision: 1, Reason: "actual successor"}
	if _, err = s.UpdateQuestion(ctx, c, actor, q.ID, u, func() error { return nil }, now); err == nil {
		t.Fatal("TEMPLATE_D3_UNCHANGED_DERIVED_COPY_ACCEPTED")
	}
	u.RequestID = "decide-real"
	u.NewRevision = 2
	out, err = s.UpdateQuestion(ctx, c, actor, q.ID, u, func() error { return nil }, now)
	if err != nil || out.Question.State != "superseded" {
		t.Fatal("TEMPLATE_D3_REAL_SUCCESSOR_DEADLOCK", err)
	}
	var target string
	s.db.QueryRow(`SELECT new_spec_id FROM pilot_question_change WHERE id=?`, cid).Scan(&target)
	if target != nv.SpecID {
		t.Fatal("TEMPLATE_D3_SUCCESSOR_LINK_LOST")
	}
}
func TestPilotTemplateTaskAssignmentAndImpactGuard(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	ctx := context.Background()
	v := templateConfirm(t, s, now, templateAcceptAll(t, s, now, templateSave(t, s, now, d, snap, "parent"), snap), snap)
	templateReviewFinal(t, s, now, v)
	p := model.ConsoleTaskProposal{ID: "template-task", SpecID: v.SpecID, Title: "bounded", TaskContent: "only R1", Status: "planned", DependsOnJSON: `[]`, AcceptanceJSON: `[{"id":"R1"}]`, CreatedAt: now, ReviewedBy: "owner-a"}
	if e := s.CreateConsoleTaskProposal(ctx, p, "owner-a", false); e != nil {
		t.Fatal(e)
	}
	if _, e := s.AssignConsoleTaskFlow(ctx, p.ID, "owner-a", "group", "review", "dev", 1, func() error { return nil }, now); e == nil {
		t.Fatal("TEMPLATE_TASK_PAIR_DIFFERS_FROM_ACCEPTED_ROLES")
	}
	state, e := s.AssignConsoleTaskFlow(ctx, p.ID, "owner-a", "group", "dev", "review", 1, func() error { return nil }, now)
	if e != nil {
		t.Fatal(e)
	}
	if e = taskBlockersClear(ctx, s.db, p.ID); e != nil {
		t.Fatal(e)
	}
	changed := d
	changed.PredecessorSpecID = v.SpecID
	changed.ExpectedRevision = 1
	changed.ChangeReason = "new contract"
	changed.Scope = "changed bounded scope"
	nv := templateSave(t, s, now, changed, snap, "successor")
	templateConfirm(t, s, now, templateAcceptAll(t, s, now, nv, snap), snap)
	if e = taskBlockersClear(ctx, s.db, p.ID); e == nil {
		t.Fatal("TEMPLATE_OLD_SCOPE_EXECUTION_NOT_BLOCKED", state)
	}
	if _, e = s.AssignConsoleTaskFlow(ctx, p.ID, "owner-a", "group", "dev", "review", state.Version, func() error { return nil }, now); e == nil {
		t.Fatal("TEMPLATE_NOOP_ASSIGN_BYPASSED_IMPACT")
	}
}
func TestPilotTemplateParentMustBeFinalAndCurrent(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	ctx := context.Background()
	v := templateConfirm(t, s, now, templateAcceptAll(t, s, now, templateSave(t, s, now, d, snap, "parent"), snap), snap)
	r, _ := s.ConsoleSpecRevision(ctx, "owner-a", v.SpecID, 1, false)
	child := d
	child.Kind = "child"
	child.Parent = &TemplateParent{SpecID: v.SpecID, Revision: 1, SHA: r.ContentSHA256}
	cv := templateSave(t, s, now, child, snap, "child")
	found := false
	for _, m := range cv.Missing {
		if m.Path == "parent" {
			found = true
		}
	}
	if !found {
		t.Fatal("TEMPLATE_MUTABLE_PARENT_ACCEPTED")
	}
	templateReviewFinal(t, s, now, v)
	child.Parent.SHA = strings.Repeat("a", 64)
	cv = templateSave(t, s, now, child, snap, "bad-sha")
	found = false
	for _, m := range cv.Missing {
		if m.Path == "parent" {
			found = true
		}
	}
	if !found {
		t.Fatal("TEMPLATE_PARENT_SHA_DRIFT_ACCEPTED")
	}
}
func TestPilotTemplateConsentCannotAttachIncompleteContract(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	d.Defaults.Environment = "unknown"
	v := templateSave(t, s, now, d, snap, "incomplete")
	ref, _ := s.TemplateReferences(context.Background(), v.ID)
	_, e := s.UpdateTemplateResponsibility(context.Background(), ref, BlockerActor{AgentID: "dev"}, TemplateOperation{RequestID: "accept", ExpectedVersion: 1, PlanSHA: v.PlanSHA, EntryIDs: []string{"R1"}, Role: "executor", Decision: "accepted"}, snap, func() error { return nil }, now)
	if e == nil {
		t.Fatal("TEMPLATE_INCOMPLETE_CONTRACT_ACCEPTED")
	}
}
func TestPilotTemplateMissingReviewOrAgentSignoffRejected(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	for _, mode := range []string{"missing", "self", "agent_signoff", "multi_missing"} {
		r := d
		switch mode {
		case "missing":
			r.Defaults.Reviewer = ""
		case "self":
			r.Defaults.Reviewer = r.Defaults.Executor
		case "agent_signoff":
			r.Defaults.Signoff = "agent:review"
		case "multi_missing":
			r.TeamMode = "multi"
			r.Defaults.Reviewer = ""
		}
		v := templateSave(t, s, now, r, snap, mode)
		found := false
		for _, m := range v.Missing {
			if strings.Contains(m.Path, "reviewer") || strings.Contains(m.Path, "signoff") {
				found = true
			}
		}
		if !found {
			t.Fatal("TEMPLATE_INDEPENDENT_REVIEW_OR_HUMAN_SIGNOFF_BYPASSED", mode)
		}
	}
}
func TestPilotTemplateConfirmRollbackAndIdempotence(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	ctx := context.Background()
	v := templateAcceptAll(t, s, now, templateSave(t, s, now, d, snap, "draft"), snap)
	ref, _ := s.TemplateReferences(ctx, v.ID)
	u := TemplateOperation{RequestID: "confirm", ExpectedVersion: 1, PlanSHA: v.PlanSHA, ConfirmHuman: true}
	if _, e := s.ConfirmTemplateDraft(ctx, ref, "owner-a", u, snap, func() error { return errors.New("revoked at commit") }, now); e == nil {
		t.Fatal("TEMPLATE_CONFIRM_REVOKE_IGNORED")
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM console_spec`).Scan(&n)
	if n != 0 {
		t.Fatal("TEMPLATE_CONFIRM_PARTIAL_COMMIT")
	}
	out, e := s.ConfirmTemplateDraft(ctx, ref, "owner-a", u, snap, func() error { return nil }, now)
	if e != nil {
		t.Fatal(e)
	}
	ref, _ = s.TemplateReferences(ctx, v.ID)
	again, e := s.ConfirmTemplateDraft(ctx, ref, "owner-a", u, snap, func() error { return nil }, now)
	if e != nil || again.SpecID != out.SpecID {
		t.Fatal("TEMPLATE_CONFIRM_REPLAY_DUPLICATES_SPEC", e)
	}
	s.db.QueryRow(`SELECT count(*) FROM console_spec`).Scan(&n)
	if n != 1 {
		t.Fatal("TEMPLATE_CONFIRM_REPLAY_DUPLICATES_SPEC")
	}
	s.db.QueryRow(`SELECT count(*) FROM pilot_template_receipt`).Scan(&n)
	if n != 3 {
		t.Fatal("TEMPLATE_RECEIPT_COUNT", n)
	}
}
func TestPilotTemplateLegacyTaskPathsCannotSkipCurrentContract(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	ctx := context.Background()
	v := templateConfirm(t, s, now, templateAcceptAll(t, s, now, templateSave(t, s, now, d, snap, "draft"), snap), snap)
	templateReviewFinal(t, s, now, v)
	p := model.ConsoleTaskProposal{ID: "legacy", SpecID: v.SpecID, Title: "bounded", TaskContent: "R1", Status: "in_progress", DependsOnJSON: `[]`, CreatedAt: now, ReviewedBy: "owner-a"}
	if e := s.CreateConsoleTaskProposal(ctx, p, "owner-a", false); e == nil {
		t.Fatal("TEMPLATE_CREATE_IMPLICITLY_STARTED_TASK")
	}
	p.Status = "planned"
	if e := s.CreateConsoleTaskProposal(ctx, p, "owner-a", false); e != nil {
		t.Fatal(e)
	}
	if e := s.UpdateConsoleTaskProposalPlan(ctx, p.ID, "owner-a", false, "R1", "in_progress", 0, []string{}, "actual results"); e == nil {
		t.Fatal("TEMPLATE_LEGACY_STATUS_BYPASSED_GROUP_AUTH")
	}
	if e := s.UpdateConsoleSpecTaskAssignees(ctx, p.ID, "owner-a", "dev", "review"); e != nil {
		t.Fatal(e)
	}
	start := model.ConsoleTaskDriverStart{SpecID: v.SpecID, Revision: 1, ContentSHA256: v.ContentSHA, RepositoryName: "fixture", BranchName: "review/fixture", Schedule: "bounded", TaskEndpoint: "local", TokenInstructions: "private", AnnouncedBy: "owner-a", AnnouncedAt: now}
	if e := s.StartConsoleTaskDriver(ctx, start); e == nil {
		t.Fatal("TEMPLATE_LEGACY_DRIVER_BYPASSED_GROUP_AUTH")
	}
}
func TestPilotTemplateMigrationAtomicRollbackAndReapply(t *testing.T) {
	s := newConsoleSpecStore(t)
	ctx := context.Background()
	for _, name := range []string{"pilot_template_audit", "pilot_template_request", "pilot_template_impact", "pilot_template_binding", "pilot_template_enrollment", "pilot_template_receipt", "pilot_template_role", "pilot_template_revision", "pilot_template_draft", "pilot_spec_source"} {
		if _, e := s.db.Exec(`DROP TABLE ` + name); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.db.Exec(`ALTER TABLE pilot_question_change DROP COLUMN new_spec_id`); e != nil {
		t.Fatal(e)
	}
	const name = "0060_pilot_spec_template.sql"
	s.db.Exec(`DELETE FROM schema_migrations WHERE version=?`, name)
	raw, e := migrationFS.ReadFile("migrations/" + name)
	if e != nil {
		t.Fatal(e)
	}
	digest := templateContentSHA(string(raw))
	if e = s.applyMigration(ctx, name, string(raw)+";BAD_TEMPLATE_MIGRATION;", digest); e == nil {
		t.Fatal("TEMPLATE_BAD_MIGRATION_ACCEPTED")
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND (name LIKE 'pilot_template_%' OR name='pilot_spec_source')`).Scan(&n)
	if n != 0 {
		t.Fatal("TEMPLATE_MIGRATION_PARTIAL_TABLES", n)
	}
	s.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=?`, name).Scan(&n)
	if n != 0 {
		t.Fatal("TEMPLATE_MIGRATION_PARTIAL_LEDGER")
	}
	var col int
	s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('pilot_question_change') WHERE name='new_spec_id'`).Scan(&col)
	if col != 0 {
		t.Fatal("TEMPLATE_MIGRATION_PARTIAL_D3_COLUMN")
	}
	if e = s.applyMigration(ctx, name, string(raw), digest); e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND (name LIKE 'pilot_template_%' OR name='pilot_spec_source')`).Scan(&n)
	if n != 10 {
		t.Fatal("TEMPLATE_MIGRATION_TABLE_COUNT", n)
	}
}
func TestPilotTemplateFullRequiresScopedHumanReceipt(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	d.Production = true
	d.Full = TemplateFull{Attributes: map[string]string{"外部契约变更": "兼容新增", "数据模型变更": "加列加表", "影响范围": "单模块", "在产影响": "灰度可回滚", "回滚成本": "重启即回滚", "安全面": "否", "预估节点数": "1"}, RuleVersion: "SpecDoc-v3.77", RuleIDs: []string{"G1"}, ContentFaces: map[string]string{}, AuthorizationReference: "owner:scope1", AuthorizationScope: "fixture-only action1", AuthorizationPurpose: "verify bounded fixture", AuthorizationExpiresAt: now.Add(time.Hour).Format(time.RFC3339), ReviewPrincipal: "user:owner-a", ReviewReference: "git:human-review", AnalyzeReference: "git:analyze", DeliverableContractReference: "git:contract", ProbesReference: "git:probes"}
	for _, k := range []string{"system_integration", "runtime_constraints", "estimates_with_basis_and_recheck", "environment", "design_constraints", "input_sources"} {
		d.Full.ContentFaces[k] = "bounded fixture; basis=local; recheck=before action"
	}
	v := templateSave(t, s, now, d, snap, "full")
	ref, _ := s.TemplateReferences(context.Background(), v.ID)
	u := TemplateOperation{RequestID: "human", ExpectedVersion: 1, PlanSHA: v.PlanSHA, EntryIDs: []string{"__full__"}, Role: "full_review", Decision: "accepted", Reason: "actual scoped review"}
	if _, e := s.UpdateTemplateResponsibility(context.Background(), ref, BlockerActor{UserID: "owner-a"}, u, snap, func() error { return nil }, now); e == nil {
		t.Fatal("TEMPLATE_FULL_HUMAN_CHECKBOX_BYPASSED")
	}
	v = templateAcceptAll(t, s, now, v, snap)
	if !v.CanConfirm || v.Mode != "full" {
		t.Fatal("TEMPLATE_FULL_SCOPE_RECEIPTS_FAILED", v.Missing)
	}
	v = templateConfirm(t, s, now, v, snap)
	if v.ExecutionAuthorized || v.BusinessSignedOff {
		t.Fatal("TEMPLATE_FULL_RECEIPT_BECAME_EXTERNAL_AUTHORIZATION")
	}
}
func TestPilotTemplateActualExecutorCannotPerformIndependentReview(t *testing.T) {
	s, now, d, snap := templateFixture(t)
	d.TeamMode = "multi"
	ctx := context.Background()
	v := templateConfirm(t, s, now, templateAcceptAll(t, s, now, templateSave(t, s, now, d, snap, "draft"), snap), snap)
	if e := s.SubmitConsoleSpecReview(ctx, v.SpecID, "owner-a", now); e != nil {
		t.Fatal(e)
	}
	q, e := s.StartSpecAgentRequest(ctx, SpecAgentRequest{ID: "actual-dev-review", ClientID: "actual-dev-review", SpecID: v.SpecID, AgentID: "dev", Actor: "owner-a", Kind: "review", Revision: 1, SHA: v.ContentSHA, Boot: "fixture"}, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CompleteSpecAgentRequest(ctx, q.ID, model.ConsoleAgent{ID: "dev", KeyID: "key-dev", SessionName: "dev", OwnerPrincipal: "owner-a", Active: true}, r8Result(q), nil, now); e != nil {
		t.Fatal(e)
	}
	g, e := s.ConsoleSpecGateReview(ctx, v.SpecID, 1)
	if e != nil {
		t.Fatal(e)
	}
	g.IssuesJSON = `[{"id":"I1","text":"check owner quote","response":"human checked","outcome":"false_positive"}]`
	if e = s.SaveConsoleSpecGateReview(ctx, v.SpecID, "owner-a", *g, now); e != nil {
		t.Fatal(e)
	}
	if e = s.FinalizeConsoleSpec(ctx, v.SpecID, "owner-a", now); e == nil {
		t.Fatal("TEMPLATE_ACTUAL_EXECUTOR_ACCEPTED_AS_INDEPENDENT_REVIEW")
	}
}
