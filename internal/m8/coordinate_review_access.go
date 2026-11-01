package m8

import (
	"context"
	"errors"
	"os"
	"strings"

	cr "github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

type coordinateReviewRepository interface {
	cr.Store
	CoordinateReviewAgentBinding(context.Context, string) (*specread.Binding, error)
	SetCoordinateReviewAgentBinding(context.Context, string, string, *specread.Binding) error
}
type reviewBindings struct {
	h       *Hub
	console specread.Bindings
	repo    coordinateReviewRepository
}

func (b reviewBindings) SpecReadBinding(ctx context.Context, actor, provider, instance string) (*specread.Binding, error) {
	if strings.HasPrefix(actor, "agent:") {
		if provider != "github" || instance != "github.com" {
			return nil, specread.ErrUnavailable
		}
		return b.repo.CoordinateReviewAgentBinding(ctx, strings.TrimPrefix(actor, "agent:"))
	}
	return b.console.SpecReadBinding(ctx, actor, provider, instance)
}
func (b reviewBindings) SetSpecReadBinding(context.Context, string, specread.Binding) error {
	return cr.ErrForbidden
}
func (b reviewBindings) DeleteSpecReadBinding(context.Context, string, string, string, string) error {
	return cr.ErrForbidden
}

// ConfigureCoordinateReviews is startup-only. The off/default state does not
// create mappings, enable R5 or activate any delivery channel.
func (h *Hub) ConfigureCoordinateReviews(sender QuestionSender) {
	h.CoordinateReviewEnabled = os.Getenv("DW_OPENSPEC_REVIEW_ENABLED") == "1"
	h.CoordinateReviewSender = nil
	h.CoordinateReviews = nil
	if !h.CoordinateReviewEnabled || sender == nil || h.SpecReader == nil || h.Identity == nil {
		return
	}
	repo, ok := h.Repo.(coordinateReviewRepository)
	if !ok {
		return
	}
	if _, ok = h.Identity.Store.(identity.CoordinateReviewAuthorizationStore); !ok {
		return
	}
	reader := *h.SpecReader
	reader.Bindings = reviewBindings{h, h.SpecReader.Bindings, repo}
	h.CoordinateReviewSender = sender
	h.CoordinateReviews = &cr.Service{Store: repo, Reader: cr.R5Reader{Service: &reader}, Access: reviewAccess{h, &reader}, Now: h.now}
}
func (h *Hub) coordinateReviewReady() bool {
	if h.SpecReader != nil {
		if p, ok := h.SpecReader.Providers["github:github.com"].(interface{ Ready() bool }); ok && !p.Ready() {
			return false
		}
	}
	return h.CoordinateReviewEnabled && h.CoordinateReviewSender != nil && h.CoordinateReviews != nil && h.SpecReader != nil && h.Identity != nil
}
func (h *Hub) coordinateReviewEntry(ctx context.Context, actor string) bool {
	if !h.coordinateReviewReady() {
		return false
	}
	b, e := h.SpecReader.Bindings.SpecReadBinding(ctx, actor, "github", "github.com")
	if e != nil || b == nil {
		return false
	}
	agents, e := h.Identity.Store.ListAgents(ctx, actor)
	if e != nil {
		return false
	}
	repo := h.Repo.(coordinateReviewRepository)
	for _, a := range agents {
		if a.State != identity.AgentActive {
			continue
		}
		b, e := repo.CoordinateReviewAgentBinding(ctx, a.ID)
		if e == nil && b != nil {
			return true
		}
	}
	return false
}

type reviewAccess struct {
	h      *Hub
	reader *specread.Service
}

func (a reviewAccess) WithReviewAccess(ctx context.Context, actor string, r cr.Record, fn func(func() error) error) error {
	return a.withAccess(ctx, actor, r, func(check func() error, _ identity.Identity) error { return fn(check) })
}
func (a reviewAccess) WithReviewNotice(ctx context.Context, r cr.Record, fn func(func() error, string, string) error) error {
	return a.withAccess(ctx, r.AssignedBy, r, func(check func() error, b identity.Identity) error {
		return fn(check, b.Issuer, b.Subject)
	})
}
func (a reviewAccess) withAccess(ctx context.Context, actor string, r cr.Record, fn func(func() error, identity.Identity) error) error {
	if !a.h.coordinateReviewReady() {
		return cr.ErrUnavailable
	}
	if actor != r.AssignedBy && actor != cr.AgentActor(r.AgentID) {
		return cr.ErrForbidden
	}
	auth, ok := a.h.Identity.Store.(identity.CoordinateReviewAuthorizationStore)
	if !ok {
		return cr.ErrUnavailable
	}
	// The same lock is taken for both the R5 Console mapping and the independent
	// Agent mapping updates, so captured identities cannot change at commit.
	a.h.coordinateReviewAuthMu.RLock()
	defer a.h.coordinateReviewAuthMu.RUnlock()
	return auth.WithCoordinateReviewAuthorization(ctx, r.AssignedBy, r.AgentID, func(agent identity.Agent, contact identity.Identity, identityCheck func() error) error {
		if route, ok := ctx.Value(coordinateReviewRouteKey{}).(string); ok && route != sessionAddress(agent.NameLC(), AgentKeyID(agent.OwnerLC)) {
			return cr.ErrForbidden
		}
		c := specread.Coordinate{Provider: r.Coordinate.Provider, Instance: r.Coordinate.Instance, Repository: r.Coordinate.Repository}
		owner, repo, e := a.reader.AuthorizeRead(ctx, r.AssignedBy, c)
		if e != nil {
			return reviewAccessError(e)
		}
		agentBinding, other, e := a.reader.AuthorizeRead(ctx, cr.AgentActor(agent.ID), c)
		if e != nil {
			return reviewAccessError(e)
		}
		if repo != other {
			return cr.ErrUnavailable
		}
		provider := a.reader.Providers[c.Provider+":"+c.Instance]
		check := func() error {
			if e := identityCheck(); e != nil {
				return cr.ErrForbidden
			}
			if e := provider.Authorize(ctx, repo, owner); e != nil {
				return reviewAccessError(e)
			}
			return reviewAccessError(provider.Authorize(ctx, repo, agentBinding))
		}
		if e = check(); e != nil {
			return e
		}
		return fn(check, contact)
	})
}
func reviewAccessError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, specread.ErrForbidden) || errors.Is(e, identity.ErrForbidden) || errors.Is(e, identity.ErrNotFound) {
		return cr.ErrForbidden
	}
	if errors.Is(e, specread.ErrInvalid) {
		return cr.ErrInvalid
	}
	return cr.ErrUnavailable
}
