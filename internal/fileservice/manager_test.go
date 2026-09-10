package fileservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

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

func testManager(t *testing.T, quota int64) (*Manager, *FixedClock, *memoryObjects) {
	t.Helper()
	r, e := OpenRepository(filepath.Join(t.TempDir(), "f.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	c := &FixedClock{Time: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	o := &memoryObjects{data: map[string][]byte{}}
	return &Manager{Repo: r, Objects: o, Clock: c, MaxFileBytes: 32, QuotaBytes: quota, Retention: 60 * 24 * time.Hour}, c, o
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
