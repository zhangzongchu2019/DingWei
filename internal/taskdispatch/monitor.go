package taskdispatch

import (
	"context"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"sort"
	"sync"
	"time"
)

// Tick reads all active leases and gives every repository/ref/PR group the
// same fifteen-second read deadline. Independent repositories cannot queue
// behind a failing provider. A further five seconds is reserved for committing
// the observation and its durable outbox under the original identity fence.
func (s *Service) Tick(ctx context.Context) error {
	if !s.ready() {
		return ErrUnavailable
	}
	started := time.Now()
	readCtx, cancelRead := context.WithDeadline(ctx, started.Add(15*time.Second))
	defer cancelRead()
	fenceCtx, cancelFence := context.WithDeadline(ctx, started.Add(20*time.Second))
	defer cancelFence()
	locks, e := s.Repo.TaskDispatchActive(readCtx)
	if e != nil {
		return e
	}
	groups := map[string][]Lock{}
	for _, l := range locks {
		key := fmt.Sprintf("%s/%s/%s/%s/%d", l.Provider, l.Instance, l.Repository, l.MonitorRef, l.PullRequestID)
		groups[key] = append(groups[key], l)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var wg sync.WaitGroup
	for _, k := range keys {
		batch := groups[k]
		wg.Add(1)
		go func() { defer wg.Done(); s.monitorGroup(ctx, readCtx, fenceCtx, batch) }()
	}
	wg.Wait()
	return ctx.Err()
}
func (s *Service) monitorGroup(ctx, readCtx, fenceCtx context.Context, locks []Lock) {
	var shared *pollFact
	for _, l := range locks {
		if ctx.Err() != nil {
			return
		}
		now := s.Now().UTC()
		m := mutationForLock(l, "observe", operationFor(l, "observe", now))
		if !now.Before(l.ExpiresAt) {
			m.Kind = "expire"
			writeCtx, cancel := context.WithTimeout(ctx, NoticeBudget)
			_, e := s.Repo.ApplyTaskDispatch(writeCtx, m, s.Now, func() error { return writeCtx.Err() })
			cancel()
			if e != nil && s.AuditFailure != nil {
				s.AuditFailure(ctx, m.OperationID, errClass(e))
			}
			continue
		}
		owner, e := s.actorOwner(readCtx, Actor{AgentID: l.HolderAgentID})
		fence, ok := s.Identity.(identity.TaskAuthorizationStore)
		if e == nil && owner == l.HolderOwnerLC && ok {
			e = fence.WithTaskAuthorization(fenceCtx, owner, l.AuthorizationGroup, []string{l.HolderAgentID}, func(agents []identity.Agent, check func() error) error {
				if len(agents) != 1 || agents[0].OwnerLC != l.HolderOwnerLC {
					return ErrForbidden
				}
				// An unbound/revoked owner must not poison another owner's shared poll.
				// This check performs no mutable-ref request. Only one poll is attempted
				// after a currently authorized owner is found in the group.
				if _, err := s.binding(readCtx, owner, l.Key); err != nil {
					return err
				}
				if shared == nil {
					fact := s.poll(readCtx, owner, m)
					shared = &fact
				}
				s.observe(readCtx, owner, &m, shared)
				writeCtx, cancel := context.WithTimeout(fenceCtx, NoticeBudget)
				defer cancel()
				_, err := s.Repo.ApplyTaskDispatch(writeCtx, m, s.Now, check)
				return err
			})
		} else if e == nil {
			e = ErrForbidden
		}
		if e != nil {
			// A separate downgrade operation cannot replay an earlier clean result if
			// SQLite committed but releasing the PG fence returned an uncertain error.
			m = mutationForLock(l, "observe", operationFor(l, "observe_unknown", now))
			m.ComparisonState = "comparison_unknown"
			m.ComparisonError = "identity_or_authorization_unknown"
			m.Bindings = nil
			if m.PullRequestID > 0 {
				m.PRDimension = "comparison_unknown"
			} else {
				m.PRDimension = "pr_not_opened"
			}
			writeCtx, cancel := context.WithTimeout(ctx, NoticeBudget)
			_, _ = s.Repo.ApplyTaskDispatch(writeCtx, m, s.Now, func() error { return writeCtx.Err() })
			cancel()
			if s.AuditFailure != nil {
				s.AuditFailure(ctx, m.OperationID, errClass(e))
			}
		}
	}
}
