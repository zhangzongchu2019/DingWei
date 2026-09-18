package fileservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type failingReader struct{ sent bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		copy(p, "part")
		return 4, nil
	}
	return 0, io.ErrUnexpectedEOF
}

func newLocalStore(t *testing.T) *LocalObjectStore {
	t.Helper()
	s, err := NewLocalObjectStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLocalPutIsAtomicAndNonExecutable(t *testing.T) {
	s := newLocalStore(t)
	key := "files/26/0918/id-20260918T000000Z"
	if err := s.Put(context.Background(), key, strings.NewReader("data"), 4, "text/plain"); err != nil {
		t.Fatal(err)
	}
	path, _ := s.path(key)
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "data" {
		t.Fatalf("bytes=%q err=%v", b, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || info.Mode().Perm()&0o111 != 0 {
		t.Fatalf("unsafe object mode=%#o", info.Mode().Perm())
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".tmp-*")); len(matches) != 0 {
		t.Fatalf("temporary object remains: %v", matches)
	}
}

func TestLocalPutFailureLeavesNoPartialObjectOrReadyMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		body io.Reader
		hook func(string) error
	}{
		{"short-write", &failingReader{}, nil},
		{"disk-full", strings.NewReader("data"), func(string) error { return syscall.ENOSPC }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newLocalStore(t)
			s.afterWrite = tc.hook
			r, err := OpenRepository(filepath.Join(t.TempDir(), "meta.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			clock := &FixedClock{Time: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
			m := &Manager{Repo: r, Objects: s, Clock: clock, MaxFileBytes: 32, QuotaBytes: 100, Retention: 60 * 24 * time.Hour, TombstoneRetention: 90 * 24 * time.Hour, ObjectPrefix: "files"}
			if _, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", ContentLength: 4, Body: tc.body}); err == nil {
				t.Fatal("failed write was accepted")
			}
			if n, err := r.ReadyBytes(context.Background()); err != nil || n != 0 {
				t.Fatalf("ready=%d err=%v", n, err)
			}
			var files []string
			_ = filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
				if err == nil && info.Mode().IsRegular() {
					files = append(files, path)
				}
				return nil
			})
			if len(files) != 0 {
				t.Fatalf("partial files remain: %v", files)
			}
		})
	}
}

func TestLocalPutRefusesExistingAndSymlinkTargets(t *testing.T) {
	for _, kind := range []string{"regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			s := newLocalStore(t)
			key := "files/26/0918/id-ts"
			path, _ := s.path(key)
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			if kind == "regular" {
				if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			if err := s.Put(context.Background(), key, strings.NewReader("new"), 3, ""); err == nil {
				t.Fatal("existing target was overwritten")
			}
			if kind == "regular" {
				b, _ := os.ReadFile(path)
				if string(b) != "old" {
					t.Fatalf("existing bytes=%q", b)
				}
			} else {
				b, _ := os.ReadFile(outside)
				if string(b) != "keep" {
					t.Fatalf("symlink target bytes=%q", b)
				}
				if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("symlink was replaced info=%v err=%v", info, err)
				}
			}
		})
	}
}

func TestLocalPutRefusesSymlinkDirectoryComponent(t *testing.T) {
	s := newLocalStore(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.Root, "files")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(context.Background(), "files/26/0918/id-ts", strings.NewReader("new"), 3, ""); err == nil {
		t.Fatal("symlink directory was followed")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("outside directory was modified: %v", entries)
	}
}

func TestPerFileSweepKeepsArchivedObjectAtStablePath(t *testing.T) {
	s := newLocalStore(t)
	r, err := OpenRepository(filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	clock := &FixedClock{Time: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	m := &Manager{Repo: r, Objects: s, Clock: clock, MaxFileBytes: 32, QuotaBytes: 100, Retention: 60 * 24 * time.Hour, TombstoneRetention: 90 * 24 * time.Hour, ObjectPrefix: "files"}
	keep, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", OriginalName: "../../run-me.sh", ContentLength: 4, Body: strings.NewReader("keep")})
	if err != nil {
		t.Fatal(err)
	}
	drop, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", ContentLength: 4, Body: strings.NewReader("drop")})
	if err != nil {
		t.Fatal(err)
	}
	drop2, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", ContentLength: 5, Body: strings.NewReader("drop2")})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetArchivedByOwner(context.Background(), keep.ID, true, "p", "o", "keep"); err != nil {
		t.Fatal(err)
	}
	kept, _ := r.Get(context.Background(), keep.ID)
	if kept.ObjectKey != keep.ObjectKey {
		t.Fatalf("archive changed key before=%s after=%s", keep.ObjectKey, kept.ObjectKey)
	}
	if strings.Contains(kept.ObjectKey, "run-me") || strings.HasSuffix(kept.ObjectKey, ".sh") {
		t.Fatalf("user filename entered object path: %s", kept.ObjectKey)
	}
	clock.Time = drop.ExpiresAt.Add(time.Second)
	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(mustPath(t, s, kept.ObjectKey)); err != nil || !bytes.Equal(b, []byte("keep")) {
		t.Fatalf("archived bytes=%q err=%v", b, err)
	}
	if _, err := os.Stat(mustPath(t, s, drop.ObjectKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dated object remains err=%v", err)
	}
	if _, err := os.Stat(mustPath(t, s, drop2.ObjectKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second dated object remains err=%v", err)
	}
	if _, err := os.Stat(filepath.Dir(mustPath(t, s, drop.ObjectKey))); err != nil {
		t.Fatalf("shared dated directory was removed err=%v", err)
	}
}

func TestDeleteDayDirectlyRejectsPinnedKey(t *testing.T) {
	s := newLocalStore(t)
	key := "files/pinned/id-20260918T120000Z"
	if err := s.Put(context.Background(), key, strings.NewReader("keep"), 4, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDay(context.Background(), key); err == nil {
		t.Fatal("DeleteDay accepted a pinned key")
	}
	b, err := os.ReadFile(mustPath(t, s, key))
	if err != nil || string(b) != "keep" {
		t.Fatalf("pinned bytes changed: bytes=%q err=%v", b, err)
	}
}

func TestDatedObjectRetentionIsBetweenSixtyAndSixtyOneDays(t *testing.T) {
	s := newLocalStore(t)
	r, err := OpenRepository(filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	clock := &FixedClock{Time: time.Date(2026, 9, 18, 12, 34, 56, 0, time.UTC)}
	m := &Manager{Repo: r, Objects: s, Clock: clock, MaxFileBytes: 32, QuotaBytes: 100, Retention: 60 * 24 * time.Hour, TombstoneRetention: 90 * 24 * time.Hour, ObjectPrefix: "files"}
	f, err := m.Put(context.Background(), Upload{OwnerKey: "o", SessionName: "s", ContentLength: 1, Body: strings.NewReader("x")})
	if err != nil {
		t.Fatal(err)
	}
	window := f.ExpiresAt.Sub(f.CreatedAt)
	if window < 60*24*time.Hour || window > 61*24*time.Hour {
		t.Fatalf("retention window=%s", window)
	}
}

func mustPath(t *testing.T, s *LocalObjectStore, key string) string {
	t.Helper()
	p, err := s.path(key)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
