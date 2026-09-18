package fileservice

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizedMediaWildcardCandidatesAndFixtureFallbacks(t *testing.T) {
	tests := []struct{ fixture, media, wantMedia, wantExt string }{
		{"sample-quicktime.mov", "video/quicktime", "video/quicktime", "mov"},
		{"sample-active.svg", "image/svg+xml", "application/octet-stream", "bin"},
		{"sample-unknown.dat", "application/x-dingwei-unknown", "application/octet-stream", "bin"},
	}
	for _, tt := range tests {
		body, err := os.ReadFile(filepath.Join("../../view-service/fixtures-0206", tt.fixture))
		if err != nil {
			t.Fatal(err)
		}
		m, _, _ := testManager(t, 1<<20)
		m.MaxFileBytes = 1 << 20
		f, err := m.Put(context.Background(), Upload{OwnerKey: "owner", SessionName: "session", MediaType: tt.media, ContentLength: int64(len(body)), Body: bytes.NewReader(body)})
		if err != nil {
			t.Fatalf("fixture=%s: %v", tt.fixture, err)
		}
		if f.MediaType != tt.wantMedia || !strings.HasSuffix(f.ObjectKey, "."+tt.wantExt) {
			t.Fatalf("fixture=%s media=%q key=%q", tt.fixture, f.MediaType, f.ObjectKey)
		}
	}
	for media, ext := range map[string]string{"video/x-matroska": "mkv", "image/heic": "heic", "image/bmp": "bmp", "image/tiff": "tiff"} {
		gotMedia, gotExt := normalizedMedia(media)
		if gotMedia != media || gotExt != ext {
			t.Fatalf("media=%q got=(%q,%q)", media, gotMedia, gotExt)
		}
	}
}

type memoryObjects struct {
	mu         sync.Mutex
	data       map[string][]byte
	failDelete bool
}

func (s *memoryObjects) Put(_ context.Context, k string, r io.Reader, _ int64, _ string) error {
	b, e := io.ReadAll(r)
	if e == nil {
		s.mu.Lock()
		s.data[k] = b
		s.mu.Unlock()
	}
	return e
}
func (s *memoryObjects) Stat(_ context.Context, k string) (ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[k]
	if !ok {
		return ObjectInfo{}, errors.New("missing")
	}
	return ObjectInfo{Size: int64(len(b))}, nil
}
func (s *memoryObjects) Open(_ context.Context, k string, off, n int64) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[k]
	if !ok {
		return nil, errors.New("missing")
	}
	end := int64(len(b))
	if n >= 0 && off+n < end {
		end = off + n
	}
	return io.NopCloser(bytes.NewReader(b[off:end])), nil
}
func (s *memoryObjects) Delete(_ context.Context, k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDelete {
		return errors.New("injected delete failure")
	}
	delete(s.data, k)
	return nil
}
func (s *memoryObjects) Move(_ context.Context, from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[from]
	if !ok {
		return errors.New("missing")
	}
	if _, exists := s.data[to]; exists {
		return errors.New("target exists")
	}
	s.data[to] = b
	delete(s.data, from)
	return nil
}

func testManager(t *testing.T, quota int64) (*Manager, *FixedClock, *memoryObjects) {
	t.Helper()
	r, e := OpenRepository(filepath.Join(t.TempDir(), "f.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	c := &FixedClock{Time: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	o := &memoryObjects{data: map[string][]byte{}}
	return &Manager{Repo: r, Objects: o, Clock: c, MaxFileBytes: 32, QuotaBytes: quota, Retention: 60 * 24 * time.Hour, TombstoneRetention: 90 * 24 * time.Hour}, c, o
}

func TestPutIntegrityFailuresNeverBecomeReady(t *testing.T) {
	m, _, o := testManager(t, 100)
	for _, tc := range []struct {
		name   string
		length int64
		body   string
		sha    string
	}{
		{"missing-length", -1, "abc", ""}, {"short-body", 4, "abc", ""}, {"long-body", 2, "abc", ""}, {"bad-sha", 3, "abc", "00"}, {"over-limit", 33, string(bytes.Repeat([]byte{'x'}, 33)), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", OriginalName: "x", ContentLength: tc.length, Body: strings.NewReader(tc.body), ExpectedSHA256: tc.sha}); e == nil {
				t.Fatal("upload unexpectedly succeeded")
			}
		})
	}
	if n, e := m.Repo.ReadyBytes(context.Background()); e != nil || n != 0 {
		t.Fatalf("ready=%d err=%v", n, e)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.data) != 0 {
		t.Fatalf("orphan objects=%d", len(o.data))
	}
}

func TestConcurrentPutEvictsOldestAndKeepsTombstone(t *testing.T) {
	m, c, _ := testManager(t, 6)
	first, e := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", OriginalName: "old", ContentLength: 4, Body: strings.NewReader("old!")})
	if e != nil {
		t.Fatal(e)
	}
	c.Time = c.Time.Add(time.Second)
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	for _, body := range []string{"new1", "new2"} {
		go func(v string) {
			defer wg.Done()
			_, e := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", OriginalName: v, ContentLength: 4, Body: strings.NewReader(v)})
			errs <- e
		}(body)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if n, e := m.Repo.ReadyBytes(context.Background()); e != nil || n > 6 {
		t.Fatalf("ready=%d err=%v", n, e)
	}
	f, e := m.Repo.Get(context.Background(), first.ID)
	if e != nil {
		t.Fatal(e)
	}
	if f.State != StateEvicted || f.RemovedAt == nil {
		t.Fatalf("oldest lacks tombstone: %+v", f)
	}
}

func TestSweepRetriesDurableDeletingState(t *testing.T) {
	m, c, objects := testManager(t, 100)
	f, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", OriginalName: "old", ContentLength: 4, Body: strings.NewReader("data")})
	if err != nil {
		t.Fatal(err)
	}
	c.Time = c.Time.Add(61 * 24 * time.Hour)
	objects.failDelete = true
	if err := m.Sweep(context.Background()); err == nil {
		t.Fatal("injected object deletion failure was swallowed")
	}
	got, err := m.Repo.Get(context.Background(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateDeleting || got.RemovalReason != "retention" || got.RemovedAt != nil {
		t.Fatalf("deletion intent was not durable: %+v", got)
	}
	objects.failDelete = false
	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err = m.Repo.Get(context.Background(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateExpired || got.RemovedAt == nil || got.RemovalReason != "retention" {
		t.Fatalf("deletion retry did not close tombstone: %+v", got)
	}
	// A prior attempt may already have removed the object;
	// another sweep remains a no-op.
	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestArchivedFileSurvivesWhileDefaultExpiresAndTombstonePurgesLater(t *testing.T) {
	m, clock, objects := testManager(t, 100)
	archived, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", OriginalName: "keep", ContentLength: 4, Body: strings.NewReader("keep")})
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", OriginalName: "drop", ContentLength: 4, Body: strings.NewReader("drop")})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetArchivedByOwner(context.Background(), archived.ID, true, "owner-principal", "o", "keep"); err != nil {
		t.Fatal(err)
	}
	clock.Time = ordinary.ExpiresAt.Add(time.Second)
	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	kept, err := m.Repo.Get(context.Background(), archived.ID)
	if err != nil || !kept.Archived || kept.State != StateReady {
		t.Fatalf("archived file=%+v err=%v", kept, err)
	}
	deleted, err := m.Repo.Get(context.Background(), ordinary.ID)
	if err != nil || deleted.State != StateExpired || deleted.RemovedAt == nil {
		t.Fatalf("ordinary file=%+v err=%v", deleted, err)
	}
	objects.mu.Lock()
	_, archivedBytesExist := objects.data[kept.ObjectKey]
	_, ordinaryBytesExist := objects.data[ordinary.ObjectKey]
	objects.mu.Unlock()
	if !archivedBytesExist || ordinaryBytesExist {
		t.Fatalf("object bytes archived=%t ordinary=%t", archivedBytesExist, ordinaryBytesExist)
	}

	clock.Time = clock.Time.Add(89 * 24 * time.Hour)
	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Repo.Get(context.Background(), ordinary.ID); err != nil {
		t.Fatalf("tombstone purged before 90d: %v", err)
	}
	clock.Time = clock.Time.Add(24*time.Hour + time.Second)
	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Repo.Get(context.Background(), ordinary.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("tombstone remains after 90d: %v", err)
	}
}

func TestMarkDeletingRejectsArchivedRow(t *testing.T) {
	m, _, _ := testManager(t, 100)
	f, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", ContentLength: 4, Body: strings.NewReader("keep")})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetArchivedByOwner(context.Background(), f.ID, true, "p", "o", "keep"); err != nil {
		t.Fatal(err)
	}
	if err := m.Repo.MarkDeleting(context.Background(), f.ID, "retention"); err == nil {
		t.Fatal("archived row entered deleting")
	}
	got, _ := m.Repo.Get(context.Background(), f.ID)
	if got.State != StateReady || !got.Archived {
		t.Fatalf("file=%+v", got)
	}
}

func TestFinishRemovalRechecksArchivedFailClosed(t *testing.T) {
	m, _, objects := testManager(t, 100)
	f, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", ContentLength: 4, Body: strings.NewReader("keep")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Repo.db.Exec(`UPDATE files SET state='deleting',archived=1,removal_reason='retention' WHERE id=?`, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.finishRemoval(context.Background(), f); err == nil {
		t.Fatal("archived deleting row was removed")
	}
	objects.mu.Lock()
	_, ok := objects.data[f.ObjectKey]
	objects.mu.Unlock()
	if !ok {
		t.Fatal("archived bytes deleted")
	}
}

func TestFinishRemovalRequiresDurableDeletingIntent(t *testing.T) {
	m, _, objects := testManager(t, 100)
	f, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", ContentLength: 4, Body: strings.NewReader("keep")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Repo.db.Exec(`UPDATE files SET removal_reason='retention' WHERE id=?`, f.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.finishRemoval(context.Background(), f); err == nil {
		t.Fatal("ready row deleted without durable intent")
	}
	objects.mu.Lock()
	_, ok := objects.data[f.ObjectKey]
	objects.mu.Unlock()
	if !ok {
		t.Fatal("bytes deleted before intent")
	}
}
