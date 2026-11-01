package changeflow

import (
	"context"
	"sort"
	"strings"
)

func committeeScope(f Snapshot) bool {
	if f.ProposalCommittee {
		return true
	}
	for _, l := range f.Labels {
		if l == "committee-review" {
			return true
		}
	}
	return false
}
func unique(ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		if !token.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func latestResults(st State) map[string]Result {
	m := map[string]Result{}
	for _, r := range st.Results {
		m[r.Scenario] = r
	}
	return m
}
func approval(f Snapshot) error {
	c := f.Coordinate
	if !f.LabelsKnown || !f.ProposalKnown || !f.ReviewsKnown || !f.PMKnown || !unique(f.RequiredReviewers) || !unique(f.PMPoints) {
		return block("当前分类、MR审阅或PM完整集合未知")
	}
	for _, actor := range f.RequiredReviewers {
		count := 0
		for _, r := range f.Reviews {
			if r.Actor == actor {
				count++
			}
		}
		if count != 1 {
			return block("MR有效批准记录缺失或冲突：" + actor)
		}
		valid := false
		for _, r := range f.Reviews {
			if r.Actor == actor && r.Commit == c.Commit && r.Valid && r.Decision == "APPROVED" && reference(r.Evidence) {
				valid = true
			}
		}
		if !valid {
			return block("缺少当前SHA所需MR批准：" + actor)
		}
	}
	for _, id := range f.PMPoints {
		count := 0
		for _, r := range f.Responses {
			if r.Point == id {
				count++
			}
		}
		if count != 1 {
			return block("PM当前回应缺失或冲突：" + id)
		}
		valid := false
		for _, r := range f.Responses {
			if r.Point == id && r.Commit == c.Commit && r.Valid && reference(r.Evidence) && (r.Decision == "agree" || r.Decision == "disagree" || r.Decision == "modify") {
				valid = true
			}
		}
		if !valid {
			return block("缺少当前SHA明确PM回应：" + id)
		}
	}
	if committeeScope(f) && (!f.CommitteeKnown || !f.Committee.Valid || !f.Committee.Authorized || f.Committee.Commit != c.Commit || !reference(f.Committee.Evidence)) {
		return block("显式委员会范围缺少当前SHA有效R7确认")
	}
	return nil
}
func guard(st State, f Snapshot, to Phase, environment string) error {
	if st.Coordinate != f.Coordinate || f.CurrentHead != st.Coordinate.Commit {
		return block("目标不是当前MR head，请重新核对")
	}
	if rank(to) < 0 {
		return ErrInvalid
	}
	if rank(to) >= rank(Approved) {
		if e := approval(f); e != nil {
			return e
		}
	}
	if rank(to) >= rank(Acceptance) {
		if !f.TasksKnown || len(f.Tasks) == 0 {
			return block("任务完整集合未知或为空")
		}
		ids := []string{}
		for _, t := range f.Tasks {
			ids = append(ids, t.ID)
			if !t.Complete || strings.TrimSpace(t.Actor) == "" || strings.TrimSpace(t.Summary) == "" || !reference(t.Evidence) || !f.applicable(t.Commit) {
				return block("任务未完成或缺少适用交接：" + t.ID)
			}
		}
		if !unique(ids) {
			return block("任务编号无效或重复")
		}
	}
	if rank(to) >= rank(Archived) {
		if !f.ScenariosKnown || len(f.Scenarios) == 0 || !unique(f.Scenarios) {
			return block("场景完整集合未知或为空")
		}
		latest := latestResults(st)
		for _, id := range f.Scenarios {
			r, ok := latest[id]
			validStatus := r.Status == "PASS" || r.Status == "FAIL" || r.Status == "NOT_RUN" || r.Status == "NOT_APPLICABLE"
			if !ok || !validStatus || r.Status == "FAIL" || !reference(r.Evidence) || !f.applicable(r.TestedCommit) || ((r.Status == "NOT_RUN" || r.Status == "NOT_APPLICABLE") && strings.TrimSpace(r.Reason) == "") {
				return block("场景缺证据、失败或未说明理由：" + id)
			}
		}
	}
	if to == Released {
		r := f.Release
		if !r.Success || !r.Verified || r.Coordinate != st.Coordinate || r.Commit != st.Coordinate.Commit || !token.MatchString(r.ID) || !token.MatchString(r.Environment) || !reference(r.Evidence) || (environment != "" && r.Environment != environment) {
			return block("缺少覆盖当前版本和指定环境的成功release事实")
		}
	}
	return nil
}

type AdvanceRequest struct {
	Coordinate  Coordinate `json:"coordinate"`
	OperationID string     `json:"operation_id"`
	Expected    Phase      `json:"expected"`
	To          Phase      `json:"to"`
	Environment string     `json:"environment,omitempty"`
}

func (s *Service) Advance(ctx context.Context, actor string, q AdvanceRequest) error {
	return s.mutate(ctx, actor, q.Coordinate, "advance", func(st *State, f Snapshot) error {
		if rank(q.Expected) < 0 || rank(q.To) != rank(q.Expected)+1 || q.Environment != "" && q.To != Released {
			return ErrInvalid
		}
		if q.To == Released && q.Environment == "" {
			return ErrInvalid
		}
		if e := guard(*st, f, q.To, q.Environment); e != nil {
			return e
		}
		id, done, e := operation(st, actor, q.OperationID, "advance", q)
		if e != nil || done {
			return e
		}
		if st.Phase != q.Expected {
			return ErrConflict
		}
		st.Events = append(st.Events, Event{ID: id, Kind: "advance", Actor: actor, From: st.Phase, To: q.To, Coordinate: q.Coordinate, At: s.now(), Evidence: factReferences(f), FactVersion: f.Version})
		event := &st.Events[len(st.Events)-1]
		event.Facts = snapshotView(f)
		for _, r := range latestResults(*st) {
			event.ResultRefs = append(event.ResultRefs, r.ID)
			event.Evidence = append(event.Evidence, r.Evidence)
		}
		sort.Strings(event.ResultRefs)
		st.Phase = q.To
		return nil
	})
}
func factReferences(f Snapshot) []string {
	var refs []string
	for _, r := range f.Reviews {
		if r.Valid {
			refs = append(refs, r.Evidence)
		}
	}
	for _, r := range f.Responses {
		if r.Valid {
			refs = append(refs, r.Evidence)
		}
	}
	if committeeScope(f) {
		refs = append(refs, f.Committee.Evidence)
	}
	for _, t := range f.Tasks {
		refs = append(refs, t.Evidence)
	}
	if f.Release.Verified {
		refs = append(refs, f.Release.Evidence)
	}
	return refs
}

type ResultRequest struct {
	Coordinate   Coordinate `json:"coordinate"`
	OperationID  string     `json:"operation_id"`
	Scenario     string     `json:"scenario"`
	Status       string     `json:"status"`
	TestedCommit string     `json:"tested_commit"`
	Evidence     string     `json:"evidence"`
	Reason       string     `json:"reason,omitempty"`
	Supersedes   string     `json:"supersedes,omitempty"`
}

func (s *Service) RecordResult(ctx context.Context, actor string, q ResultRequest) error {
	return s.mutate(ctx, actor, q.Coordinate, "result", func(st *State, f Snapshot) error {
		if !f.ScenariosKnown || !unique(f.Scenarios) || !f.applicable(q.TestedCommit) || !reference(q.Evidence) {
			return ErrInvalid
		}
		known := false
		for _, id := range f.Scenarios {
			if id == q.Scenario {
				known = true
			}
		}
		if !known {
			return ErrInvalid
		}
		switch q.Status {
		case "PASS", "FAIL":
		case "NOT_RUN", "NOT_APPLICABLE":
			if strings.TrimSpace(q.Reason) == "" {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		id, done, e := operation(st, actor, q.OperationID, "result", q)
		if e != nil || done {
			return e
		}
		if old, exists := latestResults(*st)[q.Scenario]; exists {
			if q.Supersedes != old.ID {
				return ErrConflict
			}
		} else if q.Supersedes != "" {
			return ErrInvalid
		}
		if len(st.Results) >= 2000 {
			return ErrUnavailable
		}
		st.Results = append(st.Results, Result{ID: id, Scenario: q.Scenario, Status: q.Status, TestedCommit: q.TestedCommit, Evidence: q.Evidence, Reason: q.Reason, Supersedes: q.Supersedes, Actor: actor, At: s.now()})
		return nil
	})
}

type ToolRequest struct {
	Coordinate  Coordinate `json:"coordinate"`
	OperationID string     `json:"operation_id"`
	Name        string     `json:"name"`
	Evidence    string     `json:"evidence"`
	Success     bool       `json:"success"`
}

func (s *Service) RecordTool(ctx context.Context, actor string, q ToolRequest) error {
	return s.mutate(ctx, actor, q.Coordinate, "tool", func(st *State, _ Snapshot) error {
		switch q.Name {
		case "strict", "archive", "tasks", "merge":
		default:
			return ErrInvalid
		}
		if !reference(q.Evidence) {
			return ErrInvalid
		}
		id, done, e := operation(st, actor, q.OperationID, "tool", q)
		if e != nil || done {
			return e
		}
		st.Tools = append(st.Tools, Tool{ID: id, Name: q.Name, Evidence: q.Evidence, Success: q.Success, Actor: actor, At: s.now()})
		return nil
	})
}
