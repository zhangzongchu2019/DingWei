package coordinatereview

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

type Service struct {
	Store  Store
	Access Access
	Reader Reader
	Now    func() time.Time
	// This mutex orders open/cancel/replace against actual sends within the
	// serving process. Persistent notice claims prevent multi-process retries.
	mu sync.Mutex
}

func (s *Service) ready() bool {
	return s != nil && s.Store != nil && s.Access != nil && s.Reader != nil
}
func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) fence(ctx context.Context, actor string, r Record, fn func(func() error) error) error {
	if !s.ready() {
		return ErrUnavailable
	}
	calls := 0
	var inner error
	e := s.Access.WithReviewAccess(ctx, actor, r, func(check func() error) error {
		calls++
		if calls != 1 || check == nil {
			return ErrUnavailable
		}
		inner = fn(check)
		return inner
	})
	if e != nil {
		return e
	}
	if calls != 1 {
		return ErrUnavailable
	}
	return inner
}
func (s *Service) Create(ctx context.Context, actor string, q Create) (out Record, err error) {
	if !s.ready() {
		return out, ErrUnavailable
	}
	if q.EventID == "" {
		q.EventID = ID(actor, q.Operation)
	}
	r := Record{EventID: q.EventID, ID: ID(actor, q.Operation), Operation: q.Operation, Coordinate: q.Coordinate, AgentID: q.AgentID, AssignedBy: actor, TrackRef: q.TrackRef, Created: s.now(), Dispatch: "queued", Results: []Result{}, Comparisons: []Comparison{}, Notices: []Notice{}, Alerts: []Alert{}}
	if r.Validate() != nil {
		return out, ErrInvalid
	}
	release, e := s.lock(ctx)
	if e != nil {
		return out, e
	}
	defer release()
	err = s.fence(ctx, actor, r, func(check func() error) error {
		if _, e := s.Reader.Read(ctx, actor, r.Coordinate, r.Coordinate.ChangeDir+"/proposal.md"); e != nil {
			return e
		}
		if _, e := s.Reader.Read(ctx, AgentActor(r.AgentID), r.Coordinate, r.Coordinate.ChangeDir+"/proposal.md"); e != nil {
			return e
		}
		var e error
		out, e = s.Store.CreateCoordinateReview(ctx, r, check)
		return e
	})
	return
}
func (s *Service) Get(ctx context.Context, actor, id string) (out Record, err error) {
	if !s.ready() {
		return out, ErrUnavailable
	}
	if !strings.HasPrefix(id, "coordreview_") || len(id) != 76 {
		return out, ErrInvalid
	}
	release, e := s.lock(ctx)
	if e != nil {
		return out, e
	}
	defer release()
	r, e := s.Store.CoordinateReview(ctx, id)
	if e != nil {
		return out, e
	}
	err = s.fence(ctx, actor, r, func(check func() error) error {
		if e := check(); e != nil {
			return e
		}
		out = r
		return nil
	})
	return
}
func (s *Service) List(ctx context.Context, actor, after string) ([]Record, error) {
	if !s.ready() {
		return nil, ErrUnavailable
	}
	ids, e := s.Store.CoordinateReviewIDs(ctx, after, 100)
	if e != nil {
		return nil, e
	}
	out := []Record{}
	for _, id := range ids {
		r, e := s.Get(ctx, actor, id)
		if e == nil {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *Service) Open(ctx context.Context, actor, id string) (Record, error) {
	return s.mutate(ctx, actor, id, func(r *Record) error {
		if actor != AgentActor(r.AgentID) || !r.Pending() {
			return ErrForbidden
		}
		if r.Opened.IsZero() {
			r.Opened = s.now()
		}
		return nil
	})
}
func (s *Service) Cancel(ctx context.Context, actor, id string) (Record, error) {
	return s.mutate(ctx, actor, id, func(r *Record) error {
		if actor != r.AssignedBy {
			return ErrForbidden
		}
		if r.Cancelled.IsZero() {
			r.Cancelled = s.now()
		}
		return nil
	})
}
func (s *Service) mutate(ctx context.Context, actor, id string, fn func(*Record) error) (out Record, err error) {
	if !s.ready() {
		return out, ErrUnavailable
	}
	release, e := s.lock(ctx)
	if e != nil {
		return out, e
	}
	defer release()
	r, e := s.Store.CoordinateReview(ctx, id)
	if e != nil {
		return out, e
	}
	err = s.fence(ctx, actor, r, func(check func() error) error {
		var x error
		out, x = s.Store.MutateCoordinateReview(ctx, id, func(r *Record) error {
			if e := check(); e != nil {
				return e
			}
			return fn(r)
		})
		return x
	})
	return
}
func (s *Service) Submit(ctx context.Context, actor, id string, q Result) (Record, error) {
	if !operation.MatchString(q.Operation) || !validEvidence(q.Evidence) || (q.Verdict != "PASS" && q.Verdict != "FAIL" && q.Verdict != "NEEDS_CHANGE" && q.Verdict != "UNABLE") {
		return Record{}, ErrInvalid
	}
	return s.mutate(ctx, actor, id, func(r *Record) error {
		if actor != AgentActor(r.AgentID) || q.Commit != r.Coordinate.Commit {
			return ErrForbidden
		}
		if !r.Cancelled.IsZero() {
			return ErrConflict
		}
		// Superseded reviews can append a late result only to their original SHA.
		for _, old := range r.Results {
			if old.Operation == q.Operation {
				if old.Commit == q.Commit && old.Verdict == q.Verdict && old.Evidence == q.Evidence && old.Reviewer == r.AgentID {
					return nil
				}
				return ErrConflict
			}
		}
		if len(r.Results) >= 100 {
			return ErrUnavailable
		}
		q.At = s.now()
		q.Reviewer = r.AgentID
		r.Results = append(r.Results, q)
		if q.Verdict != "UNABLE" && r.ReadStage < 4 {
			return ErrConflict
		}
		if q.Verdict == "UNABLE" {
			if r.Unable.IsZero() {
				r.Unable = q.At
			}
		} else if r.Completed.IsZero() {
			r.Completed = q.At
		}
		return nil
	})
}
func (s *Service) Read(ctx context.Context, actor, id, path string) (out specread.Result, err error) {
	if !s.ready() {
		return out, ErrUnavailable
	}
	release, e := s.lock(ctx)
	if e != nil {
		return out, e
	}
	defer release()
	r, e := s.Store.CoordinateReview(ctx, id)
	if e != nil {
		return out, e
	}
	if actor != AgentActor(r.AgentID) || !r.Cancelled.IsZero() {
		return out, ErrForbidden
	}
	if !specread.ValidPath(path) {
		return out, ErrInvalid
	}
	err = s.fence(ctx, actor, r, func(check func() error) error {
		var x error
		out, x = s.Reader.Read(ctx, actor, r.Coordinate, path)
		if x != nil {
			return x
		}
		if e := check(); e != nil {
			return e
		}
		_, x = s.Store.MutateCoordinateReview(ctx, id, func(q *Record) error {
			if e := check(); e != nil {
				return e
			}
			if !q.Cancelled.IsZero() {
				return ErrForbidden
			}
			if q.ReadStage == 0 && strings.HasPrefix(path, "openspec/specs/") && strings.HasSuffix(path, "/context.md") {
				q.ReadStage = 1
			}
			if q.ReadStage == 1 && strings.HasPrefix(path, "openspec/specs/") && strings.HasSuffix(path, "/spec.md") {
				q.ReadStage = 2
			}
			if q.ReadStage == 2 && path == q.Coordinate.ChangeDir+"/proposal.md" {
				q.ReadStage = 3
			}
			return nil
		})
		return x
	})
	return
}
func (s *Service) Timeline(ctx context.Context, actor, id string) (Record, error) {
	return s.mutate(ctx, actor, id, func(q *Record) error {
		if actor != AgentActor(q.AgentID) || q.ReadStage < 3 {
			return ErrConflict
		}
		q.ReadStage = 4
		return nil
	})
}

// A comparison is an append-only fact. Neither result commits nor assignment
// coordinates are ever rewritten to the target's SHA.
func (s *Service) Compare(ctx context.Context, actor, id string, current Coordinate) (out Record, err error) {
	if !s.ready() {
		return out, ErrUnavailable
	}
	if current.Validate() != nil {
		return out, ErrInvalid
	}
	release, e := s.lock(ctx)
	if e != nil {
		return out, e
	}
	defer release()
	r, e := s.Store.CoordinateReview(ctx, id)
	if e != nil {
		return out, e
	}
	if current.Provider != r.Coordinate.Provider || current.Instance != r.Coordinate.Instance || current.Repository != r.Coordinate.Repository {
		return out, ErrInvalid
	}
	err = s.fence(ctx, actor, r, func(check func() error) error {
		// Target authorization is independently checked by the same R5 reader.
		assessor := specconfirm.Service{Reader: boundSnapshot{s.Reader, ctx, actor, current.Provider}, Policy: specconfirm.ConservativePolicyA{}, PolicyApproved: true}
		a := assessor.Assess(ctx, r.Coordinate.Coordinate, current.Coordinate)
		if current == r.Coordinate {
			if _, e := s.Reader.Read(ctx, actor, current, current.ChangeDir+"/proposal.md"); e != nil {
				a.Status = specconfirm.Unknown
				a.Reason = "待核对：原版本无法读取"
			}
		}
		if a.Status == specconfirm.Confirmed {
			a.Status = specconfirm.Status("assigned_version")
		}
		if a.Status == specconfirm.Unchanged {
			a.Status = specconfirm.Status("no_material_change")
		}

		var x error
		out, x = s.Store.MutateCoordinateReview(ctx, id, func(latest *Record) error {
			if e := check(); e != nil {
				return e
			}
			latest.CheckedAt = s.now()
			if n := len(latest.Comparisons); n > 0 {
				last := latest.Comparisons[n-1]
				if last.Current == current && last.Assessment == a {
					return nil
				}
			}
			if len(latest.Comparisons) >= 1000 {
				return ErrUnavailable
			}
			latest.Comparisons = append(latest.Comparisons, Comparison{Current: current, Assessment: a, At: s.now()})
			if a.Status == specconfirm.Stale {
				if latest.Superseded.IsZero() {
					latest.Superseded = s.now()
				}
				found := false
				for _, alert := range latest.Alerts {
					if alert.Current == current {
						found = true
					}
				}
				if !found {
					latest.Alerts = append(latest.Alerts, Alert{Current: current, Dispatch: "queued"})
				}
			}
			return nil
		})
		return x
	})
	return
}

type boundSnapshot struct {
	r               Reader
	ctx             context.Context
	actor, provider string
}

func (b boundSnapshot) ReadSnapshot(ctx context.Context, c specconfirm.Coordinate) (specconfirm.Snapshot, error) {
	return b.r.Snapshot(ctx, b.actor, Coordinate{Provider: b.provider, Coordinate: c})
}

// Dispatch sends an idempotent coordinate envelope. The durable Hub transport
// reconciles its stable source ID; no repository contents enter send.
func (s *Service) Dispatch(ctx context.Context, id string, send func(Record) error) error {
	if !s.ready() || send == nil {
		return ErrUnavailable
	}
	release, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer release()
	r, e := s.Store.CoordinateReview(ctx, id)
	if e != nil {
		return e
	}
	if r.Dispatch != "queued" || !r.Pending() {
		return nil
	}
	return s.fence(ctx, r.AssignedBy, r, func(check func() error) error {
		r, e = s.Store.MutateCoordinateReview(ctx, id, func(q *Record) error {
			if !q.Pending() || q.Dispatch != "queued" {
				return ErrConflict
			}
			if e := check(); e != nil {
				return e
			}
			q.Dispatch = "attempting"
			return nil
		})
		if e != nil {
			return e
		}
		e = check()
		if e == nil {
			e = send(r)
		}
		state := "accepted"
		if e != nil {
			state = "unknown"
		}
		_, x := s.Store.MutateCoordinateReview(ctx, id, func(q *Record) error { q.Dispatch = state; return nil })
		if x != nil {
			return x
		}
		return e
	})
}

// Remind reserves one of only three attempts before transport. A crashed or
// ambiguous attempt consumes its slot; no retry can reset the budget.
func (s *Service) Remind(ctx context.Context, id string, send func(Record, int, string, string) (string, error)) error {
	if !s.ready() || send == nil {
		return ErrUnavailable
	}
	release, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer release()
	r, e := s.Store.CoordinateReview(ctx, id)
	if e != nil {
		return e
	}
	now := s.now()
	due := func(q Record) bool {
		if !q.Unread() || len(q.Notices) >= 3 || now.Before(q.Created.Add(30*time.Minute+time.Duration(len(q.Notices))*time.Hour)) {
			return false
		}
		if len(q.Notices) > 0 && now.Before(q.Notices[len(q.Notices)-1].At.Add(time.Hour)) {
			return false
		}
		return true
	}
	if !due(r) {
		return nil
	}
	access, ok := s.Access.(NoticeAccess)
	if !ok {
		return ErrUnavailable
	}
	return access.WithReviewNotice(ctx, r, func(check func() error, issuer, subject string) error {
		r, e = s.Store.MutateCoordinateReview(ctx, id, func(q *Record) error {
			if !due(*q) {
				return ErrConflict
			}
			if e := check(); e != nil {
				return e
			}
			q.Notices = append(q.Notices, Notice{Slot: len(q.Notices) + 1, At: now, State: "attempting"})
			return nil
		})
		if e != nil {
			return e
		}
		slot := len(r.Notices)
		receipt := ""
		e = check()
		if e == nil {
			if issuer == "" || subject == "" {
				e = ErrUnavailable
			} else {
				receipt, e = send(r, slot, issuer, subject)
			}
		}
		state := "sent"
		if e != nil || receipt == "" {
			state = "unknown"
		}
		_, x := s.Store.MutateCoordinateReview(ctx, id, func(q *Record) error { n := &q.Notices[slot-1]; n.State = state; n.Receipt = receipt; return nil })
		if x != nil {
			return x
		}
		return e
	})
}

// Track resolves only through the R5 interface. Resolution failures create an
// explicit unknown fact; they never imply unchanged or change the original SHA.
func (s *Service) Track(ctx context.Context, id string) error {
	r, e := s.Store.CoordinateReview(ctx, id)
	if e != nil {
		return e
	}
	if r.TrackRef == "" || !r.Cancelled.IsZero() {
		return nil
	}
	current, e := s.Reader.Resolve(ctx, r.AssignedBy, r.Coordinate, r.TrackRef)
	if e != nil {
		_, e = s.mutate(ctx, r.AssignedBy, id, func(q *Record) error {
			a := specconfirm.Assessment{Status: specconfirm.Unknown, FromCommit: q.Coordinate.Commit, PolicyVersion: (specconfirm.ConservativePolicyA{}).Version(), Reason: "ref_unreadable"}
			if len(q.Comparisons) > 0 && q.Comparisons[len(q.Comparisons)-1].Assessment.Reason == a.Reason {
				return nil
			}
			q.Comparisons = append(q.Comparisons, Comparison{Current: q.Coordinate, Assessment: a, At: s.now()})
			return nil
		})
		return e
	}
	if len(r.Comparisons) > 0 {
		last := r.Comparisons[len(r.Comparisons)-1]
		if last.Current == current && last.Assessment.Status != specconfirm.Unknown && s.now().Sub(r.CheckedAt) < time.Minute {
			return nil
		}
	}
	_, e = s.Compare(ctx, r.AssignedBy, id, current)
	return e
}
func (s *Service) DispatchAlert(ctx context.Context, id string, send func(Record, Coordinate) error) error {
	release, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer release()
	r, e := s.Store.CoordinateReview(ctx, id)
	if e != nil {
		return e
	}
	if !r.Cancelled.IsZero() {
		return nil
	}
	for i, a := range r.Alerts {
		if a.Dispatch != "queued" {
			continue
		}
		return s.fence(ctx, r.AssignedBy, r, func(check func() error) error {
			_, e = s.Store.MutateCoordinateReview(ctx, id, func(q *Record) error {
				if e := check(); e != nil {
					return e
				}
				if q.Alerts[i].Dispatch != "queued" {
					return ErrConflict
				}
				q.Alerts[i].Dispatch = "attempting"
				return nil
			})
			if e != nil {
				return e
			}
			e = check()
			if e == nil {
				e = send(r, a.Current)
			}
			state := "accepted"
			if e != nil {
				state = "unknown"
			}
			_, x := s.Store.MutateCoordinateReview(ctx, id, func(q *Record) error { q.Alerts[i].Dispatch = state; return nil })
			if x != nil {
				return x
			}
			return e
		})
	}
	return nil
}

// The store lease spans provider checks and the actual send, not a SQL TX.
// Separate Hub instances/store handles cannot send after a committed open.
func (s *Service) lock(ctx context.Context) (func(), error) {
	for !s.mu.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ErrUnavailable
		case <-time.After(time.Millisecond):
		}
	}
	release, e := s.Store.BeginCoordinateReview(ctx)
	if e != nil {
		s.mu.Unlock()
		return nil, ErrUnavailable
	}
	return func() { release(); s.mu.Unlock() }, nil
}
