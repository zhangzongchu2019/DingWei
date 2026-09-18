package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/fileservice"
)

func verifyRetention(workDir string) error {
	info, err := os.Stat(workDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("work directory must already be a directory")
	}
	root, err := os.MkdirTemp(workDir, "file-service-retention-")
	if err != nil {
		return fmt.Errorf("create isolated retention work directory: %w", err)
	}
	defer os.RemoveAll(root)

	repo, err := fileservice.OpenRepository(filepath.Join(root, "metadata.db"))
	if err != nil {
		return err
	}
	defer repo.Close()
	objects, err := fileservice.NewLocalObjectStore(filepath.Join(root, "objects"))
	if err != nil {
		return err
	}
	clock := &fileservice.FixedClock{Time: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	manager := &fileservice.Manager{
		Repo: repo, Objects: objects, Clock: clock,
		MaxFileBytes: 1024, QuotaBytes: 1024 * 1024,
		Retention: 60 * 24 * time.Hour, TombstoneRetention: 90 * 24 * time.Hour,
		ObjectPrefix: "files",
	}
	put := func(body string) (fileservice.File, error) {
		return manager.Put(context.Background(), fileservice.Upload{
			OwnerKey: "verify-owner", SessionName: "verify-agent",
			OriginalName: "untrusted.sh", MediaType: "application/octet-stream",
			ContentLength: int64(len(body)), Body: strings.NewReader(body),
		})
	}
	first, err := put("ordinary-one")
	if err != nil {
		return fmt.Errorf("put first ordinary object: %w", err)
	}
	second, err := put("ordinary-two")
	if err != nil {
		return fmt.Errorf("put second ordinary object: %w", err)
	}
	pinned, err := put("pinned-bytes")
	if err != nil {
		return fmt.Errorf("put pinned object: %w", err)
	}
	if filepath.Dir(first.ObjectKey) != filepath.Dir(second.ObjectKey) || filepath.Dir(first.ObjectKey) != filepath.Dir(pinned.ObjectKey) {
		return errors.New("verification objects did not share one date directory")
	}
	if err := manager.SetArchivedByOwner(context.Background(), pinned.ID, true, "verify-principal", "verify-owner", "retention verification"); err != nil {
		return fmt.Errorf("pin verification object: %w", err)
	}
	pinned, err = repo.Get(context.Background(), pinned.ID)
	if err != nil {
		return err
	}
	dateDir := filepath.Join(objects.Root, filepath.FromSlash(filepath.Dir(first.ObjectKey)))
	if info, err := os.Stat(dateDir); err != nil || !info.IsDir() {
		return fmt.Errorf("date directory missing before sweep: %w", err)
	}

	clock.Time = first.ExpiresAt.Add(time.Second)
	if err := manager.Sweep(context.Background()); err != nil {
		return fmt.Errorf("sweep date directory: %w", err)
	}
	if info, err := os.Stat(dateDir); err != nil || !info.IsDir() {
		return fmt.Errorf("shared date directory removed despite archived object: %v", err)
	}
	for _, f := range []fileservice.File{first, second} {
		got, err := repo.Get(context.Background(), f.ID)
		if err != nil || got.State != fileservice.StateExpired || got.RemovedAt == nil {
			return fmt.Errorf("ordinary object %s not expired: state=%s removed=%v err=%v", f.ID, got.State, got.RemovedAt, err)
		}
	}
	kept, err := repo.Get(context.Background(), pinned.ID)
	if err != nil || kept.State != fileservice.StateReady || !kept.Archived || kept.RemovedAt != nil {
		return fmt.Errorf("pinned metadata changed: state=%s archived=%t removed=%v err=%v", kept.State, kept.Archived, kept.RemovedAt, err)
	}
	pinnedPath := filepath.Join(objects.Root, filepath.FromSlash(kept.ObjectKey))
	bytes, err := os.ReadFile(pinnedPath)
	if err != nil || string(bytes) != "pinned-bytes" {
		return fmt.Errorf("pinned bytes changed: bytes=%q err=%v", bytes, err)
	}
	return nil
}
