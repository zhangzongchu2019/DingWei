package nodeagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupLockFailureHasNoDirectorySideEffects(t *testing.T) {
	for _, mode := range []string{"missing-parent", "lock-is-directory"} {
		t.Run(mode, func(t *testing.T) {
			beforeLock, beforeChown, beforeFind, beforeRun := quotaLockPath, chownForSeatFn, findmntTarget, xfsRun
			defer func() {
				quotaLockPath, chownForSeatFn, findmntTarget, xfsRun = beforeLock, beforeChown, beforeFind, beforeRun
			}()
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			quotaLockPath = filepath.Join(root, "missing", "quota.lock")
			if mode == "lock-is-directory" {
				quotaLockPath = root
			}
			chowns, commands := 0, 0
			chownForSeatFn = func(string) error { chowns++; return nil }
			findmntTarget = func(string) (string, error) { commands++; return "/fixture", nil }
			xfsRun = func(...string) ([]byte, error) { commands++; return nil, nil }
			existing := filepath.Join(root, "ag_existing")
			if err := os.Mkdir(existing, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(existing, "keep")
			if err := os.WriteFile(marker, []byte("existing-seat"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"ag_new", "ag_existing"} {
				err := (xfsQuota{}).Setup(root, id, 16384)
				if err == nil || !strings.Contains(err.Error(), "quota lock:") {
					t.Fatalf("want actual lock-open failure, got %v", err)
				}
			}
			if _, err := os.Lstat(filepath.Join(root, "ag_new")); !os.IsNotExist(err) {
				t.Fatalf("LOCKFAIL_SETUP_RESIDUE: new seat remains: %v", err)
			}
			if chowns != 0 || commands != 0 {
				t.Fatalf("LOCKFAIL_SETUP_SIDE_EFFECT: chowns=%d commands=%d", chowns, commands)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "existing-seat" {
				t.Fatalf("existing seat changed: %q %v", data, err)
			}
			// Zero-quota setup intentionally does not need an XFS lock.
			if err := (xfsQuota{}).Setup(root, "ag_zero", 0); err != nil {
				t.Fatalf("zero-quota setup unexpectedly required lock: %v", err)
			}
			if chowns != 1 {
				t.Fatalf("zero-quota setup did not chown once: %d", chowns)
			}
		})
	}
}
