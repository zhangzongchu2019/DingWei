package taskdispatch

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// AuthorizeNotice reads only current authority, not history or other recipients'
// outbox pages. A queued notice never substitutes for a lease-operation check.
func (s *Service) AuthorizeNotice(ctx context.Context, n Notice) (*Lock, error) {
	if !s.ready() {
		return nil, ErrUnavailable
	}
	actor := Actor{UserLC: n.RecipientID}
	if n.RecipientKind == "agent" {
		actor = Actor{AgentID: n.RecipientID}
	} else if n.RecipientKind != "user" {
		return nil, ErrForbidden
	}
	owner, e := s.actorOwner(ctx, actor)
	if e != nil || owner != n.RecipientOwnerLC {
		return nil, ErrForbidden
	}
	if _, e = s.binding(ctx, owner, n.Key); e != nil {
		return nil, e
	}
	l, e := s.Repo.TaskDispatchLock(ctx, n.Key)
	if e != nil {
		return nil, e
	}
	if l == nil || l.Generation != n.Generation {
		return nil, ErrConflict
	}
	if (n.Type == "grant" || n.Type == "handoff") && !l.Effective(s.Now().UTC()) {
		return nil, ErrConflict
	}
	if n.RecipientKind == "agent" {
		if l.HolderAgentID != n.RecipientID || l.HolderOwnerLC != owner {
			return nil, ErrForbidden
		}
		fence, ok := s.Identity.(identity.TaskAuthorizationStore)
		if !ok {
			return nil, ErrUnavailable
		}
		e = fence.WithTaskAuthorization(ctx, owner, l.AuthorizationGroup, []string{l.HolderAgentID}, func(agents []identity.Agent, check func() error) error {
			if len(agents) != 1 || agents[0].OwnerLC != owner {
				return ErrForbidden
			}
			if _, err := s.binding(ctx, owner, n.Key); err != nil {
				return err
			}
			return check()
		})
		if e != nil {
			return nil, e
		}
	}
	return l, nil
}
