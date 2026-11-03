package taskdispatch

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"time"
)

type View struct {
	Lock          *Lock      `json:"lock"`
	Events        []Event    `json:"events"`
	Operation     *Operation `json:"operation,omitempty"`
	Notices       []Notice   `json:"notices"`
	Effective     bool       `json:"effective"`
	NextEventID   int64      `json:"next_event_id"`
	HasMoreEvents bool       `json:"has_more_events"`
	MonitoringLag bool       `json:"monitoring_lag"`
}

func (s *Service) Inspect(ctx context.Context, actor Actor, k Key, operationID string) (View, error) {
	return s.InspectAfter(ctx, actor, k, operationID, 0)
}
func (s *Service) InspectAfter(ctx context.Context, actor Actor, k Key, operationID string, after int64) (View, error) {
	out := View{}
	if !s.ready() {
		return out, ErrUnavailable
	}
	if k.Validate() != nil || after < 0 {
		return out, ErrInvalid
	}
	owner, e := s.actorOwner(ctx, actor)
	if e != nil {
		return out, e
	}
	if _, e = s.binding(ctx, owner, k); e != nil {
		return out, e
	}
	l, e := s.Repo.TaskDispatchLock(ctx, k)
	if e != nil {
		return out, e
	}
	if l != nil {
		now := s.Now().UTC()
		out.Effective = l.Effective(now)
		out.MonitoringLag = l.ComparisonState == "comparison_unknown" || l.LastCheckedAt.IsZero() || now.Before(l.LastCheckedAt) || now.Sub(l.LastCheckedAt) > PollInterval+15*time.Second+NoticeBudget
		redacted := l.Redacted()
		out.Lock = &redacted
		if actor.AgentID != "" {
			if l.HolderAgentID != actor.AgentID || l.HolderOwnerLC != owner {
				return View{}, ErrForbidden
			}
			fence, ok := s.Identity.(identity.TaskAuthorizationStore)
			if !ok {
				return View{}, ErrUnavailable
			}
			e = fence.WithTaskAuthorization(ctx, owner, l.AuthorizationGroup, []string{actor.AgentID}, func(agents []identity.Agent, check func() error) error {
				if len(agents) != 1 || agents[0].OwnerLC != owner {
					return ErrForbidden
				}
				if e = check(); e != nil {
					return e
				}
				if out.Effective {
					copy := *l
					out.Lock = &copy
				}
				return nil
			})
			if e != nil {
				return View{}, e
			}
		}
	}
	out.Events, e = s.Repo.TaskDispatchEvents(ctx, k, after)
	if e != nil {
		return View{}, e
	}
	if len(out.Events) > 0 {
		out.NextEventID = out.Events[len(out.Events)-1].ID
		out.HasMoreEvents = len(out.Events) == 100
	}
	if operationID != "" {
		op, err := s.Repo.TaskDispatchOperation(ctx, k, operationID)
		if err != nil {
			return View{}, err
		}
		if op != nil {
			op.Result.Lock = op.Result.Lock.Redacted()
			op.Result.CurrentLease = l != nil && op.Result.Lock.Generation == l.Generation && out.Effective
			if l != nil && op.Result.Lock.Generation == l.Generation && out.Effective {
				if actor.AgentID != "" && op.ActorAgentID == actor.AgentID && op.ActorOwnerLC == owner {
					op.Result.Lock.LeaseToken = l.LeaseToken
				}
				if actor.UserLC != "" && op.ActorUserLC == owner {
					fence, ok := s.Identity.(identity.TaskAuthorizationStore)
					if !ok {
						return View{}, ErrUnavailable
					}
					err = fence.WithTaskAuthorization(ctx, owner, l.AuthorizationGroup, []string{l.HolderAgentID}, func(agents []identity.Agent, check func() error) error {
						if len(agents) != 1 || agents[0].OwnerLC != l.HolderOwnerLC {
							return ErrForbidden
						}
						return check()
					})
					if err == nil {
						op.Result.Lock.LeaseToken = l.LeaseToken
					}
				}
			}
			out.Operation = op
		}
	}
	out.Notices = []Notice{}
	for _, state := range []string{"pending", "available", "queued", "unknown"} {
		notices, err := s.Repo.TaskDispatchNotices(ctx, state, 1000)
		if err != nil {
			return View{}, err
		}
		for _, n := range notices {
			if n.Key == k && ((actor.AgentID != "" && n.RecipientKind == "agent" && n.RecipientID == actor.AgentID && n.RecipientOwnerLC == owner) || (actor.UserLC != "" && n.RecipientKind == "user" && n.RecipientID == owner)) {
				out.Notices = append(out.Notices, n)
			}
		}
	}
	return out, nil
}
