package specconfirm_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sc "github.com/zhangzongchu2019/dingwei/internal/specconfirm"
)

type authority struct {
	read, committee bool
	err             error
	last            sc.Coordinate
	writes          int
}

func (a *authority) WithAccess(ctx context.Context, actor string, c sc.Coordinate, write bool, fn func(context.Context) error) error {
	a.last = c
	if a.err != nil {
		return a.err
	}
	if actor != "member" || !a.read || (write && !a.committee) {
		return sc.ErrForbidden
	}
	if write {
		a.writes++
	}
	return fn(ctx)
}

type memory struct{ records []sc.Confirmation }

func (m *memory) SaveSpecConfirmation(_ context.Context, c sc.Confirmation) (sc.Confirmation, error) {
	for _, old := range m.records {
		if old.ID == c.ID {
			if old.SameFact(c) {
				return old, nil
			}
			return sc.Confirmation{}, sc.ErrConflict
		}
	}
	m.records = append(m.records, c)
	return c, nil
}
func (m *memory) ListSpecConfirmations(_ context.Context, c sc.Coordinate) ([]sc.Confirmation, error) {
	out := []sc.Confirmation{}
	for _, r := range m.records {
		if r.Coordinate.SameChange(c) {
			out = append(out, r)
		}
	}
	return out, nil
}

type reader struct {
	snapshots map[string]sc.Snapshot
	calls     int
	wrong     bool
}

func (r *reader) ReadSnapshot(_ context.Context, c sc.Coordinate) (sc.Snapshot, error) {
	r.calls++
	s, ok := r.snapshots[c.Commit]
	if !ok {
		return s, errors.New("commit disappeared")
	}
	if r.wrong {
		s.Coordinate.Commit = strings.Repeat("f", 40)
	}
	return s, nil
}
func coord() sc.Coordinate {
	return sc.Coordinate{Instance: "git.code.tencent.com", Repository: "wego-ai/dingwei", ChangeDir: "openspec/changes/example", Commit: strings.Repeat("a", 40)}
}
func req() sc.Request {
	return sc.Request{Coordinate: coord(), OperationID: "operation-00000001", Meaning: "同意范围与需求"}
}
func snap(c sc.Coordinate, extra map[string]string) sc.Snapshot {
	s := sc.Snapshot{Coordinate: c, Files: map[string][]byte{c.ChangeDir + "/proposal.md": []byte("scope A")}}
	for k, v := range extra {
		s.Files[c.ChangeDir+"/"+k] = []byte(v)
	}
	return s
}
func TestR7ConfirmationBindsActorTimeMeaningAndExactCommit(t *testing.T) {
	ctx := context.Background()
	a := &authority{read: true, committee: true}
	m := &memory{}
	now := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	s := &sc.Service{Store: m, Authority: a, Now: func() time.Time { return now }}
	original, e := s.Confirm(ctx, "member", req())
	if e != nil {
		t.Fatal(e)
	}
	if original.ConfirmedBy != "member" || !original.ConfirmedAt.Equal(now) || original.Meaning != req().Meaning || original.Coordinate != coord() {
		t.Fatalf("bad binding %+v", original)
	}
	now = now.Add(time.Hour)
	retry, e := s.Confirm(ctx, "member", req())
	if e != nil || !retry.ConfirmedAt.Equal(original.ConfirmedAt) || len(m.records) != 1 {
		t.Fatalf("retry %+v %v", retry, e)
	}
	views, e := s.List(ctx, "member", coord())
	if e != nil || len(views) != 1 || views[0].Assessment.Status != sc.Confirmed {
		t.Fatalf("exact %+v %v", views, e)
	}
	moved := coord()
	moved.Commit = strings.Repeat("b", 40)
	views, e = s.List(ctx, "member", moved)
	if e != nil || views[0].Commit != original.Commit || views[0].Assessment.Status != sc.Unknown {
		t.Fatalf("force moved %+v %v", views, e)
	}
	a.committee = false
	if _, e = s.Confirm(ctx, "member", req()); !errors.Is(e, sc.ErrForbidden) {
		t.Fatal("noncommittee confirmed")
	}
	a.read = false
	if _, e = s.List(ctx, "member", coord()); !errors.Is(e, sc.ErrForbidden) {
		t.Fatal("revoked history readable")
	}
	a.err = sc.ErrUnavailable
	if _, e = s.Confirm(ctx, "member", req()); !errors.Is(e, sc.ErrUnavailable) {
		t.Fatal("unknown permission allowed")
	}
	var off *sc.Service
	if _, e = off.Confirm(ctx, "member", req()); !errors.Is(e, sc.ErrUnavailable) {
		t.Fatal("nil service allowed")
	}
	s.Authority = nil
	if _, e = s.List(ctx, "member", coord()); !errors.Is(e, sc.ErrUnavailable) {
		t.Fatal("nil authority allowed")
	}
}
func TestR7ConfirmationPolicyDisabledUnknownAndForcePush(t *testing.T) {
	ctx := context.Background()
	from, to := coord(), coord()
	to.Commit = strings.Repeat("b", 40)
	r := &reader{snapshots: map[string]sc.Snapshot{from.Commit: snap(from, nil), to.Commit: snap(to, nil)}}
	s := &sc.Service{Reader: r, Policy: sc.ConservativePolicyA{}}
	if got := s.Assess(ctx, from, to); got.Status != sc.Unknown || r.calls != 0 {
		t.Fatalf("A enabled without approval %+v calls %d", got, r.calls)
	}
	s.PolicyApproved = true
	if got := s.Assess(ctx, from, to); got.Status != sc.Unchanged || got.FromCommit != from.Commit || got.ToCommit != to.Commit {
		t.Fatalf("unrelated commit misclassified %+v", got)
	}
	for _, file := range []string{"specs/cap/spec.md", "design.md", "proposal.md", "tasks.md"} {
		t.Run(file, func(t *testing.T) {
			r.snapshots[to.Commit] = snap(to, map[string]string{file: "material change"})
			if got := s.Assess(ctx, from, to); got.Status != sc.Stale {
				t.Fatalf("missed material %s %+v", file, got)
			}
		})
	}
	delete(r.snapshots, from.Commit)
	if got := s.Assess(ctx, from, to); got.Status != sc.Unknown {
		t.Fatalf("missing old commit reused %+v", got)
	}
	r.snapshots[from.Commit] = snap(from, nil)
	r.wrong = true
	if got := s.Assess(ctx, from, to); got.Status != sc.Unknown {
		t.Fatalf("wrong commit reused %+v", got)
	}
	other := to
	other.Repository = "other/repo"
	if got := s.Assess(ctx, from, other); got.Status != sc.Unknown {
		t.Fatal("cross repo considered same")
	}
}
func TestR7PolicyAChangesAndUnknownInventories(t *testing.T) {
	p := sc.ConservativePolicyA{}
	a, b := coord(), coord()
	b.Commit = strings.Repeat("b", 40)
	old := snap(a, map[string]string{"tasks.md": "- [ ] T-01\n## 交接记录\nfirst"})
	for _, body := range []string{"- [x] T-01\n## 交接记录\nfirst", "- [ ] T-01\n## 交接记录\nsecond"} {
		next := snap(b, map[string]string{"tasks.md": body})
		if got, e := p.Compare(old, next); e != nil || got != sc.NoMaterial {
			t.Fatalf("approved PM exemption missing %v %v", got, e)
		}
	}
	for _, s := range []sc.Snapshot{{Coordinate: b}, {Coordinate: b, Files: map[string][]byte{}}, snap(b, map[string]string{"../outside.md": "escape"})} {
		if got, e := p.Compare(old, s); e == nil || got != sc.Indeterminate {
			t.Fatalf("incomplete tree accepted %v %v", got, e)
		}
	}
	archived := b
	archived.ChangeDir = "openspec/changes/archive/2026-11-01-example"
	if got, e := p.Compare(old, snap(archived, map[string]string{"tasks.md": "- [ ] T-01\n## 交接记录\nfirst"})); e != nil || got != sc.NoMaterial {
		t.Fatalf("archive exemption missing %v %v", got, e)
	}
}
func TestR7RejectsMutableAndMalformedCoordinates(t *testing.T) {
	for _, bad := range []string{"main", strings.Repeat("a", 8), strings.Repeat("A", 40), "a" + strings.Repeat("b", 39) + "\n"} {
		c := coord()
		c.Commit = bad
		if c.Validate() == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	for _, dir := range []string{"openspec/changes/../x", "openspec/changes/x/", "openspec/changes/x%2fy", "https://other/x"} {
		c := coord()
		c.ChangeDir = dir
		if c.Validate() == nil {
			t.Fatalf("accepted path %q", dir)
		}
	}
}

type brokenAuthority struct{ skip bool }

func (a brokenAuthority) WithAccess(ctx context.Context, _ string, _ sc.Coordinate, _ bool, fn func(context.Context) error) error {
	if !a.skip {
		_ = fn(ctx)
	}
	return nil
}
func TestR7AuthorityCannotSkipOperationOrHideConflict(t *testing.T) {
	s := &sc.Service{Store: &memory{}, Authority: brokenAuthority{skip: true}}
	if _, e := s.Confirm(context.Background(), "member", req()); !errors.Is(e, sc.ErrUnavailable) {
		t.Fatalf("skipped callback returned success: %v", e)
	}
	s.Authority = brokenAuthority{}
	if _, e := s.Confirm(context.Background(), "member", req()); e != nil {
		t.Fatal(e)
	}
	r := req()
	r.Commit = strings.Repeat("b", 40)
	if _, e := s.Confirm(context.Background(), "member", r); !errors.Is(e, sc.ErrConflict) {
		t.Fatalf("authority hid store error: %v", e)
	}
}
