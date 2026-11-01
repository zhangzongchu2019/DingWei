package specread

import "context"

// AuthorizeRead is the shared R5 subject/repository fence, also used by R6
// metadata operations when a historical commit has become unreadable. It does
// not resolve a ref, cache authorization, or grant access through the service account.
func (s *Service) AuthorizeRead(ctx context.Context, actor string, c Coordinate) (Binding, Repository, error) {
	if s == nil || s.Bindings == nil {
		return Binding{}, Repository{}, ErrUnavailable
	}
	p := s.Providers[c.Provider+":"+c.Instance]
	if p == nil {
		return Binding{}, Repository{}, ErrUnavailable
	}
	if actor == "" || !s.Repositories[c.Provider+":"+c.Instance+":"+c.Repository] {
		return Binding{}, Repository{}, ErrForbidden
	}
	b, e := s.Bindings.SpecReadBinding(ctx, actor, c.Provider, c.Instance)
	if e != nil {
		return Binding{}, Repository{}, ErrUnavailable
	}
	if b == nil || !b.Valid() || b.Principal != actor || b.Provider != c.Provider || b.Instance != c.Instance {
		return Binding{}, Repository{}, ErrForbidden
	}
	repo, e := p.Repository(ctx, c.Repository)
	if e != nil {
		return Binding{}, Repository{}, e
	}
	if repo.ID != c.Repository {
		return Binding{}, Repository{}, ErrInvalid
	}
	if e = p.Authorize(ctx, repo, *b); e != nil {
		return Binding{}, Repository{}, e
	}
	return *b, repo, nil
}
