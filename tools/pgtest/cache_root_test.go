package main

import (
	"os"
	"path/filepath"
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
	want := filepath.Join(tmp, "dingwei-pgtest-cache")
	if got != want || !filepath.IsAbs(got) {
		t.Fatalf("no-home cache root = %q, want absolute %q", got, want)
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
