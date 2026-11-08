//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestResolveCacheRootWithoutHomeUsesStableTempPath(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	got, err := resolveCacheRoot("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, "dingwei-pgtest-cache-"+strconv.Itoa(os.Getuid()))
	if got != want || !filepath.IsAbs(got) {
		t.Fatalf("no-home cache root = %q, want absolute %q", got, want)
	}
	info, err := os.Lstat(got)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("new fallback mode = %04o, want 0700", info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		t.Fatalf("new fallback owner = %#v, want uid %d", info.Sys(), os.Getuid())
	}
}

func TestResolveCacheRootRejectsGroupOrWorldWritableFallback(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	path := filepath.Join(tmp, "dingwei-pgtest-cache-"+strconv.Itoa(os.Getuid()))
	if err := os.Mkdir(path, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}

	if got, err := resolveCacheRoot(""); err == nil {
		t.Fatalf("accepted unsafe fallback %q", got)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != 0o777 {
		t.Fatalf("unsafe pre-existing directory was changed: info=%v err=%v", info, err)
	}
}

func TestResolveCacheRootRejectsSymlinkFallback(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	path := filepath.Join(tmp, "dingwei-pgtest-cache-"+strconv.Itoa(os.Getuid()))
	target := filepath.Join(tmp, "safe-target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	if got, err := resolveCacheRoot(""); err == nil {
		t.Fatalf("accepted symlink fallback %q", got)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("pre-existing symlink was changed: info=%v err=%v", info, err)
	}
}

func TestResolveCacheRootUsesUserCacheBeforeSystemTemp(t *testing.T) {
	t.Setenv("HOME", "")
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("TMPDIR", t.TempDir())

	got, err := resolveCacheRoot("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cacheDir, ".embedded-postgres-go")
	if got != want || !filepath.IsAbs(got) {
		t.Fatalf("user-cache fallback = %q, want absolute %q", got, want)
	}
}

func TestResolveCacheRootMakesExplicitRelativePathAbsolute(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveCacheRoot("relative-pgtest-cache")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "relative-pgtest-cache")
	if got != want || !filepath.IsAbs(got) {
		t.Fatalf("explicit cache root = %q, want absolute %q", got, want)
	}
}
