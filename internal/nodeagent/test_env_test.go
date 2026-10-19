package nodeagent

import (
	"os"
	"path/filepath"
	"testing"
)

// Positive roots must satisfy the production guard independently of the invoking
// user's umask. Negative permission/symlink fixtures explicitly create their own bad roots.
func protectedTestDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

// XFS command fixtures use the real flock implementation on a private file,
// never the host's production lock. These tests mutate package hooks and are serial.
func privateQuotaLock(t *testing.T) {
	t.Helper()
	before := quotaLockPath
	quotaLockPath = filepath.Join(protectedTestDir(t), "quota.lock")
	t.Cleanup(func() { quotaLockPath = before })
}

// Fixture repair must not turn invalid roots into valid production inputs.
func TestProtectedSeatRootStillRejectsWritableOrAliasedRoots(t *testing.T) {
	_, p := softFixture(t)
	for _, mode := range []string{"soft", "hard"} {
		p.Mode = mode
		for _, perm := range []os.FileMode{0770, 0702, 0777} {
			root := protectedTestDir(t)
			if err := os.Chmod(root, perm); err != nil {
				t.Fatal(err)
			}
			if q, err := newSoftQuota(p, root, filepath.Join(protectedTestDir(t), "state"), "nd_bad_root"); err == nil {
				q.close()
				t.Fatalf("%s writable root %o accepted", mode, perm)
			}
		}
		link := filepath.Join(protectedTestDir(t), "link")
		if err := os.Symlink(protectedTestDir(t), link); err != nil {
			t.Fatal(err)
		}
		if q, err := newSoftQuota(p, link, filepath.Join(protectedTestDir(t), "state"), "nd_bad_root"); err == nil {
			q.close()
			t.Fatalf("%s symlink root accepted", mode)
		}
	}
}
