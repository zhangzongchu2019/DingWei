package fileservice

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRemovalCandidatesExpireFirstThenFIFOQuota(t *testing.T) {
	r, err := OpenRepository(filepath.Join(t.TempDir(), "nested", "files.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 9, 16, 0, 0, 0, time.UTC)
	files := []File{
		{ID: "old", ObjectKey: "o/old", OwnerKey: "o", SessionName: "s", OriginalName: "old", MediaType: "text/plain", SizeBytes: 4, SHA256: "a", State: StateReady, CreatedAt: now.Add(-3 * time.Hour), ExpiresAt: now.Add(time.Hour)},
		{ID: "new", ObjectKey: "o/new", OwnerKey: "o", SessionName: "s", OriginalName: "new", MediaType: "text/plain", SizeBytes: 4, SHA256: "b", State: StateReady, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		{ID: "expired", ObjectKey: "o/expired", OwnerKey: "o", SessionName: "s", OriginalName: "expired", MediaType: "text/plain", SizeBytes: 2, SHA256: "c", State: StateReady, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now},
	}
	for _, f := range files {
		if err := r.InsertReady(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.RemovalCandidates(ctx, now, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "expired" || got[1].ID != "old" {
		t.Fatalf("candidates=%v", got)
	}
	for _, f := range got {
		state := StateEvicted
		reason := "quota"
		if !f.ExpiresAt.After(now) {
			state = StateExpired
			reason = "retention"
		}
		if err := r.Tombstone(ctx, f.ID, state, now, reason); err != nil {
			t.Fatal(err)
		}
	}
	f, err := r.Get(ctx, "expired")
	if err != nil {
		t.Fatal(err)
	}
	if f.State != StateExpired || f.RemovedAt == nil || f.OriginalName != "expired" || f.SHA256 != "c" {
		t.Fatalf("tombstone lost audit fields: %+v", f)
	}
	bytes, err := r.ReadyBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bytes != 4 {
		t.Fatalf("ready bytes=%d", bytes)
	}
	all, err := r.ListStable(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].ID != "old" || all[1].ID != "expired" || all[2].ID != "new" {
		t.Fatalf("stable audit order=%v", all)
	}
	tombstones, err := r.ListStable(ctx, StateExpired, 10)
	if err != nil || len(tombstones) != 1 || tombstones[0].ID != "expired" {
		t.Fatalf("tombstones=%v err=%v", tombstones, err)
	}
}

func TestInjectedClockCrossesRetentionBoundary(t *testing.T) {
	boundary := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	clock := &FixedClock{Time: boundary.Add(-time.Nanosecond)}
	if !clock.Now().Before(boundary) {
		t.Fatal("clock should begin before boundary")
	}
	clock.Time = boundary
	if clock.Now().Before(boundary) {
		t.Fatal("clock did not cross boundary")
	}
}
