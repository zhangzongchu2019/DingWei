package changeflow

import (
	"context"
	"sort"
	"strings"
)

func canonicalRisk(in RiskInput) RiskInput {
	in.Labels = append([]string(nil), in.Labels...)
	sort.Strings(in.Labels)
	labels := in.Labels[:0]
	for _, v := range in.Labels {
		if len(labels) == 0 || labels[len(labels)-1] != v {
			labels = append(labels, v)
		}
	}
	in.Labels = labels
	in.Files = append([]FileDiff(nil), in.Files...)
	sort.Slice(in.Files, func(i, j int) bool {
		return in.Files[i].OldPath+"\x00"+in.Files[i].NewPath < in.Files[j].OldPath+"\x00"+in.Files[j].NewPath
	})
	return in
}
func riskFacts(f Snapshot, c Coordinate) ([]Hit, string, error) {
	if !f.LabelsKnown || f.Owner == "" || f.Input.Coordinate != c || digest(canonicalRisk(RiskInput{Labels: f.Labels}).Labels) != digest(canonicalRisk(f.Input).Labels) {
		return nil, "", ErrUnavailable
	}
	hits, e := Evaluate(f.Input, f.Rules)
	if e != nil {
		return nil, "", e
	}
	return hits, digest(struct {
		Input          RiskInput
		Version, Owner string
	}{canonicalRisk(f.Input), f.Rules.Version, f.Owner}), nil
}

// Detect writes the Console projection and the immutable revision together.
// Comment delivery is independent and never creates a merge/lifecycle gate.
func (s *Service) Detect(ctx context.Context, actor string, c Coordinate) error {
	e := s.mutate(ctx, actor, c, "detect", func(st *State, f Snapshot) error {
		hits, input, e := riskFacts(f, c)
		if e != nil {
			return e
		}
		if st.Reminder == nil {
			if len(hits) == 0 {
				return nil
			}
			st.Reminder = &Reminder{ID: digest(c), Coordinate: c}
		}
		rem := st.Reminder
		if !f.MergedAt.IsZero() && reference(f.MergeEvidence) {
			rem.MergedAt = f.MergedAt.UTC()
			rem.MergeEvidence = f.MergeEvidence
		}
		if len(rem.Revisions) > 0 && rem.Revisions[len(rem.Revisions)-1].InputDigest == input {
			return nil
		}
		if len(rem.Revisions) >= 500 {
			return ErrUnavailable
		}
		rev := RiskRevision{ID: digest([]string{rem.ID, input}), InputDigest: input, RuleVersion: f.Rules.Version, Owner: f.Owner, Hits: hits, At: s.now()}
		if len(rem.Revisions) > 0 {
			old := rem.Revisions[len(rem.Revisions)-1]
			if digest(old.Hits) == digest(hits) && old.Owner == rev.Owner {
				rev.Acknowledged = old.Acknowledged
			}
		}
		rev.Console = Delivery{Status: "delivered", Location: "report:console/" + rem.ID, Key: rev.ID, At: rev.At}
		rev.MR = Delivery{Status: "pending", Key: rev.ID}
		rem.Revisions = append(rem.Revisions, rev)
		return nil
	})
	if e != nil {
		return e
	}
	// Missing write credentials do not erase the persisted Console reminder.
	_ = s.Deliver(ctx, actor, c)
	return nil
}
func (s *Service) Deliver(ctx context.Context, actor string, c Coordinate) error {
	if e := s.ready(); e != nil {
		return e
	}
	if c.Validate() != nil || actor == "" {
		return ErrInvalid
	}
	e := s.Provider.WithAccess(ctx, actor, c, "comment", func(r Reader) error {
		return s.Store.Transact(ctx, c, func(st *State) error {
			if st.Reminder == nil || len(st.Reminder.Revisions) == 0 {
				return nil
			}
			rev := &st.Reminder.Revisions[len(st.Reminder.Revisions)-1]
			if rev.MR.Status == "delivered" {
				return nil
			}
			f, e := r.Read(ctx, c)
			if e != nil {
				return e
			}
			if e = f.valid(c); e != nil {
				return e
			}
			_, input, e := riskFacts(f, c)
			if e != nil {
				return e
			}
			if input != rev.InputDigest {
				return ErrConflict
			}
			receipt, writeErr := r.UpsertComment(ctx, Comment{Coordinate: c, ReminderID: st.Reminder.ID, Revision: rev.ID, OperationKey: rev.ID, Hits: append([]Hit(nil), rev.Hits...), RuleVersion: rev.RuleVersion, Owner: rev.Owner})
			status := receipt.Status
			if writeErr != nil {
				status = "unknown"
			}
			if status != "delivered" && status != "failed" && status != "unknown" {
				status = "unknown"
			}
			if status == "delivered" && !reference(receipt.Location) {
				status = "unknown"
			}
			rev.MR = Delivery{Status: status, Location: receipt.Location, Key: rev.ID, At: s.now()}
			if len(rev.Attempts) >= 200 {
				return ErrUnavailable
			}
			rev.Attempts = append(rev.Attempts, rev.MR)
			last, e := r.Read(ctx, c)
			if e != nil {
				return e
			}
			if digest(f) != digest(last) {
				return ErrConflict
			}
			return nil
		})
	})
	if e != nil {
		// A failed independent write authorization is visible using current read /
		// detect authorization, never by bypassing a revoked repository ACL.
		_ = s.mutate(ctx, actor, c, "detect", func(st *State, _ Snapshot) error {
			if st.Reminder != nil && len(st.Reminder.Revisions) > 0 {
				r := &st.Reminder.Revisions[len(st.Reminder.Revisions)-1]
				if r.MR.Status != "delivered" {
					r.MR = Delivery{Status: "failed", Key: r.ID, At: s.now()}
					if len(r.Attempts) >= 200 {
						return ErrUnavailable
					}
					r.Attempts = append(r.Attempts, r.MR)
				}
			}
			return nil
		})
	}
	return e
}
func (s *Service) Acknowledge(ctx context.Context, actor string, c Coordinate, revision string) error {
	return s.mutate(ctx, actor, c, "ack", func(st *State, f Snapshot) error {
		if f.Owner != actor {
			return ErrForbidden
		}
		if st.Reminder == nil || len(st.Reminder.Revisions) == 0 {
			return ErrInvalid
		}
		r := &st.Reminder.Revisions[len(st.Reminder.Revisions)-1]
		if r.ID != revision {
			return ErrConflict
		}
		_, input, e := riskFacts(f, c)
		if e != nil {
			return e
		}
		if input != r.InputDigest {
			return ErrConflict
		}
		if r.Acknowledged == nil {
			r.Acknowledged = &Acknowledgement{Actor: actor, Revision: revision, At: s.now()}
		}
		return nil
	})
}

type View struct {
	RiskStatus       string    `json:"risk_status"`
	RecordedReleases []Release `json:"recorded_releases"`

	State             State          `json:"state"`
	CurrentHead       string         `json:"current_head"`
	OldVersion        bool           `json:"old_version"`
	BlockingReason    string         `json:"blocking_reason"`
	Facts             SnapshotView   `json:"facts"`
	AllExecutedPassed bool           `json:"all_executed_passed"`
	Counts            map[string]int `json:"scenario_counts"`
}

// Only facts/coordinates, never patches, added source lines or credentials.
type SnapshotView struct {
	ProposalCommittee bool

	LabelsKnown, ProposalKnown                        bool
	Labels                                            []string
	CommitteeScope                                    bool
	ReviewsKnown, PMKnown, TasksKnown, ScenariosKnown bool
	RequiredReviewers                                 []string
	Reviews                                           []Review
	PMPoints                                          []string
	Responses                                         []PMResponse
	Committee                                         Committee
	Tasks                                             []Task
	Scenarios                                         []string
	Release                                           Release
}

func (s *Service) View(ctx context.Context, actor string, c Coordinate) (View, error) {
	var out View
	if e := s.ready(); e != nil {
		return out, e
	}
	if c.Validate() != nil || strings.TrimSpace(actor) == "" {
		return out, ErrInvalid
	}
	e := s.Provider.WithAccess(ctx, actor, c, "read", func(r Reader) error {
		f, e := r.Read(ctx, c)
		if e != nil {
			return e
		}
		if e = f.valid(c); e != nil {
			return e
		}
		st, e := s.Store.Read(ctx, c)
		if e != nil {
			return e
		}
		out.State = st
		hits, input, riskErr := riskFacts(f, c)
		out.RiskStatus = "unknown"
		if riskErr == nil {
			out.RiskStatus = "stale"
			if st.Reminder == nil && len(hits) == 0 {
				out.RiskStatus = "current"
			}
			if st.Reminder != nil && len(st.Reminder.Revisions) > 0 && st.Reminder.Revisions[len(st.Reminder.Revisions)-1].InputDigest == input {
				out.RiskStatus = "current"
			}
		}
		for _, event := range st.Events {
			if event.Kind == "advance" && event.To == Released {
				out.RecordedReleases = append(out.RecordedReleases, event.Facts.Release)
			}
		}
		out.CurrentHead = f.CurrentHead
		out.OldVersion = f.CurrentHead != c.Commit
		out.Facts = snapshotView(f)
		next := rank(st.Phase) + 1
		if next >= len(phases) {
			next = rank(st.Phase)
		}
		if e = guard(st, f, phases[next], ""); e != nil {
			out.BlockingReason = e.Error()
		}
		out.Counts = map[string]int{"PASS": 0, "FAIL": 0, "NOT_RUN": 0, "NOT_APPLICABLE": 0}
		latest := latestResults(st)
		for _, id := range f.Scenarios {
			if v, ok := latest[id]; ok {
				out.Counts[v.Status]++
			}
		}
		out.AllExecutedPassed = f.ScenariosKnown && len(f.Scenarios) > 0 && out.Counts["PASS"] == len(f.Scenarios) && !out.OldVersion && guard(st, f, Archived, "") == nil
		return nil
	})
	return out, e
}

func snapshotView(f Snapshot) SnapshotView {
	return SnapshotView{ProposalCommittee: f.ProposalCommittee, LabelsKnown: f.LabelsKnown, ProposalKnown: f.ProposalKnown, Labels: f.Labels, CommitteeScope: committeeScope(f), ReviewsKnown: f.ReviewsKnown, PMKnown: f.PMKnown, TasksKnown: f.TasksKnown, ScenariosKnown: f.ScenariosKnown, RequiredReviewers: f.RequiredReviewers, Reviews: f.Reviews, PMPoints: f.PMPoints, Responses: f.Responses, Committee: f.Committee, Tasks: f.Tasks, Scenarios: f.Scenarios, Release: f.Release}
}
