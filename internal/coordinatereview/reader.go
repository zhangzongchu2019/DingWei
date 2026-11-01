package coordinatereview

import (
	"context"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

// R5Reader is the sole source reader. Agent identity mapping is supplied through
// the same R5 Bindings interface; no raw URL/SSH provider exists here.
type R5Reader struct{ Service *specread.Service }

func (r R5Reader) Read(ctx context.Context, actor string, c Coordinate, path string) (specread.Result, error) {
	if r.Service == nil {
		return specread.Result{}, ErrUnavailable
	}
	return r.Service.Read(ctx, actor, specread.Coordinate{Provider: c.Provider, Instance: c.Instance, Repository: c.Repository, Commit: c.Commit, Path: path}, "")
}
func (r R5Reader) Resolve(ctx context.Context, actor string, c Coordinate, ref string) (Coordinate, error) {
	if r.Service == nil {
		return Coordinate{}, ErrUnavailable
	}
	out, e := r.Service.Read(ctx, actor, specread.Coordinate{Provider: c.Provider, Instance: c.Instance, Repository: c.Repository}, ref)
	if e != nil {
		return Coordinate{}, e
	}
	c.Commit = out.Commit
	return c, nil
}
func (r R5Reader) Snapshot(ctx context.Context, actor string, c Coordinate) (specconfirm.Snapshot, error) {
	snap := specconfirm.Snapshot{Coordinate: c.Coordinate, Files: map[string][]byte{}}
	root, e := r.Read(ctx, actor, c, c.ChangeDir+"/proposal.md")
	if e != nil {
		return snap, e
	}
	total := 0
	for _, entry := range root.Entries {
		if !strings.HasPrefix(entry.Path, c.ChangeDir+"/") || !entry.Markdown() {
			continue
		}
		if len(snap.Files) >= 256 || entry.Size > specread.MaxFile {
			return snap, ErrUnavailable
		}
		item, e := r.Read(ctx, actor, c, entry.Path)
		if e != nil {
			return snap, e
		}
		total += len(item.Text)
		if total > 8<<20 {
			return snap, ErrUnavailable
		}
		snap.Files[entry.Path] = []byte(item.Text)
	}
	if _, ok := snap.Files[c.ChangeDir+"/proposal.md"]; !ok {
		return snap, ErrUnavailable
	}
	return snap, nil
}
