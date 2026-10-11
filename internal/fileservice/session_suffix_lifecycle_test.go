package fileservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalDeleteCanonicalSessionAndRejectMalformedSuffix(t *testing.T) {
	for _, session := range []string{"timer", "timer.zzc", "timer.z9", "timer.z", "timer..zzc", "timer.zzc.extra", "timer.", "timer.zz-c"} {
		t.Run(session, func(t *testing.T) {
			store := newLocalStore(t)
			key := "u_zhangzongchu/" + session + "/26/1011/0123456789abcdef0123456789abcdef.yml"
			if err := store.Put(context.Background(), key, strings.NewReader("keep"), 4, "text/plain"); err != nil {
				t.Fatal(err)
			}
			err := store.Delete(context.Background(), key)
			valid := session == "timer" || session == "timer.zzc" || session == "timer.z9"
			if valid {
				if err != nil {
					t.Fatalf("canonical session deletion failed: %v", err)
				}
				if _, err := os.Stat(mustPath(t, store, key)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("deleted object still exists: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("malformed session suffix permitted deletion")
				}
				if body, err := os.ReadFile(mustPath(t, store, key)); err != nil || string(body) != "keep" {
					t.Fatalf("rejected key changed stored bytes: %v", err)
				}
			}
		})
	}
}

func dottedSessionManager(t *testing.T) (*Manager, *FixedClock, *LocalObjectStore) {
	t.Helper()
	store := newLocalStore(t)
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	clock := &FixedClock{Time: time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)}
	return &Manager{Repo: repo, Objects: store, Clock: clock, MaxFileBytes: 32, QuotaBytes: 100, Retention: 7 * 24 * time.Hour, TombstoneRetention: 90 * 24 * time.Hour}, clock, store
}

func TestCanonicalSessionExpiryRemovesOnlyUnarchivedObject(t *testing.T) {
	manager, clock, store := dottedSessionManager(t)
	put := func(body string) File {
		t.Helper()
		file, err := manager.Put(context.Background(), Upload{OwnerKey: "u_zhangzongchu", SessionName: "timer.zzc", OriginalName: "preprod.yml", ContentLength: int64(len(body)), Body: strings.NewReader(body)})
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	drop, keep := put("drop"), put("keep")
	if err := manager.SetArchivedByOwner(context.Background(), keep.ID, true, "owner", "u_zhangzongchu", "retain"); err != nil {
		t.Fatal(err)
	}
	clock.Time = drop.ExpiresAt.Add(time.Second)
	if err := manager.Sweep(context.Background()); err != nil {
		t.Fatalf("canonical-session retention sweep failed: %v", err)
	}
	if _, err := os.Stat(mustPath(t, store, drop.ObjectKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired bytes remain: %v", err)
	}
	if body, err := os.ReadFile(mustPath(t, store, keep.ObjectKey)); err != nil || string(body) != "keep" {
		t.Fatalf("archived bytes changed: %v", err)
	}
	state, err := manager.Repo.Get(context.Background(), drop.ID)
	if err != nil || state.State != StateExpired || state.RemovedAt == nil {
		t.Fatalf("expired object lacks completed tombstone: %+v %v", state, err)
	}
}

func TestCanonicalSessionIntegrityFailureCleansLocalObject(t *testing.T) {
	manager, _, store := dottedSessionManager(t)
	_, err := manager.Put(context.Background(), Upload{OwnerKey: "u_zhangzongchu", SessionName: "timer.zzc", OriginalName: "preprod.yml", ExpectedSHA256: strings.Repeat("0", 64), ContentLength: 4, Body: strings.NewReader("data")})
	if !errors.Is(err, ErrIntegrity) {
		t.Fatalf("want integrity rejection, got %v", err)
	}
	err = filepath.Walk(store.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			t.Errorf("integrity-rejected upload left local object: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
