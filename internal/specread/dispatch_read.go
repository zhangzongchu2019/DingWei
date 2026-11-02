package specread

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

type PullRequest struct {
	ID         int64     `json:"id"`
	Repository string    `json:"repository"`
	HeadSHA    string    `json:"head_sha"`
	State      string    `json:"state"`
	BaseRef    string    `json:"base_ref"`
	HeadRef    string    `json:"head_ref"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type PullRequestReader interface {
	PullRequest(context.Context, Repository, int64) (PullRequest, error)
}

// PullRequest is a read-only provider extension. A PR from a different stable
// base repository is rejected, including a forged provider response.
func (g *GitHub) PullRequest(ctx context.Context, r Repository, id int64) (PullRequest, error) {
	var v struct {
		Number    int64     `json:"number"`
		State     string    `json:"state"`
		UpdatedAt time.Time `json:"updated_at"`
		Head      struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref  string `json:"ref"`
			Repo struct {
				ID json.Number `json:"id"`
			} `json:"repo"`
		} `json:"base"`
	}
	if id <= 0 {
		return PullRequest{}, ErrInvalid
	}
	if e := g.get(ctx, fmt.Sprintf("%s/pulls/%d", repoAPI(r), id), &v); e != nil {
		return PullRequest{}, e
	}
	if v.Number != id || string(v.Base.Repo.ID) != r.ID || !ValidSHA(v.Head.SHA) || (v.State != "open" && v.State != "closed") || v.UpdatedAt.IsZero() || v.Head.Ref == "" || v.Base.Ref == "" {
		return PullRequest{}, ErrInvalid
	}
	return PullRequest{ID: id, Repository: r.ID, HeadSHA: v.Head.SHA, State: v.State, BaseRef: v.Base.Ref, HeadRef: v.Head.Ref, UpdatedAt: v.UpdatedAt}, nil
}
func (s *Service) ReadPullRequest(ctx context.Context, actor string, c Coordinate, id int64) (PullRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	binding, repo, e := s.AuthorizeRead(ctx, actor, c)
	if e != nil {
		return PullRequest{}, e
	}
	p, ok := s.Providers[c.Provider+":"+c.Instance].(PullRequestReader)
	if !ok {
		return PullRequest{}, ErrUnavailable
	}
	pr, e := p.PullRequest(ctx, repo, id)
	if e != nil {
		return PullRequest{}, e
	}
	if pr.ID != id || pr.Repository != c.Repository || !ValidSHA(pr.HeadSHA) || pr.UpdatedAt.IsZero() || (pr.State != "open" && pr.State != "closed") || pr.BaseRef == "" || pr.HeadRef == "" {
		return PullRequest{}, ErrInvalid
	}
	after, _, e := s.AuthorizeRead(ctx, actor, c)
	if e != nil {
		return PullRequest{}, e
	}
	if after != binding {
		return PullRequest{}, ErrForbidden
	}
	return pr, nil
}

// ReadDispatchSnapshot reuses R5 authorization, transport and bounded Read for
// every blob. Raw tree validation prevents silently omitting symlinks/modules.
// Bodies are comparison inputs only and never become dispatch database rows.
func (s *Service) ReadDispatchSnapshot(ctx context.Context, actor string, c Coordinate, dir string) (specconfirm.Snapshot, error) {
	snap := specconfirm.Snapshot{Coordinate: specconfirm.Coordinate{Instance: c.Instance, Repository: c.Repository, ChangeDir: dir, Commit: c.Commit}}
	if snap.Coordinate.Validate() != nil {
		return snap, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, repo, e := s.AuthorizeRead(ctx, actor, c)
	if e != nil {
		return snap, e
	}
	entries, e := s.Providers[c.Provider+":"+c.Instance].Tree(ctx, repo, c.Commit)
	if e != nil {
		return snap, e
	}
	files := map[string][]byte{}
	total := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Path, dir+"/") {
			continue
		}
		if entry.Mode == "120000" || entry.Mode == "160000" || entry.Type == "commit" {
			return snap, ErrInvalid
		}
		if !strings.HasSuffix(strings.ToLower(entry.Path), ".md") {
			continue
		}
		if !entry.Markdown() || !strings.HasSuffix(entry.Path, ".md") || !ValidSHA(entry.SHA) || entry.Size < 0 || entry.Size > MaxFile || len(files) >= 256 {
			return snap, ErrInvalid
		}
		if _, exists := files[entry.Path]; exists {
			return snap, ErrInvalid
		}
		fileCoord := c
		fileCoord.Path = entry.Path
		item, err := s.Read(ctx, actor, fileCoord, "")
		if err != nil {
			return snap, err
		}
		total += len(item.Text)
		if total > 8<<20 {
			return snap, ErrLimit
		}
		files[entry.Path] = []byte(item.Text)
	}
	if _, ok := files[dir+"/proposal.md"]; !ok {
		return snap, ErrNotFound
	}
	snap.Files = files
	return snap, nil
}
