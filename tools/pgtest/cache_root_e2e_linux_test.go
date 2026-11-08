//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNoHomeCacheStaysOutOfWorkingTreeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: starts one embedded PostgreSQL instance")
	}
	bin := buildTool(t)
	work := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	env, err := exec.LookPath("env")
	if err != nil {
		t.Fatalf("env command unavailable: %v", err)
	}
	cmd := exec.CommandContext(ctx, env, "-u", "HOME", "-u", "XDG_CACHE_HOME", "TMPDIR=/tmp",
		bin, "-start-timeout", "5m")
	cmd.Dir = work
	cmd.Stdin = strings.NewReader("") // EOF after READY triggers normal stop and cleanup.
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pgtest without HOME failed: %v\nstdout: %s\nstderr: %s", err, out, stderrOf(cmd))
	}
	kv := parseProtocolLines(string(out))
	wantCache := filepath.Join("/tmp", "dingwei-pgtest-cache-"+strconv.Itoa(os.Getuid()))
	wantBinaries := filepath.Join(wantCache, "extracted")
	if cache := firstOf(kv["CACHE"]); cache != wantCache || !filepath.IsAbs(cache) {
		t.Errorf("CACHE=%q, want absolute fallback %q; protocol=%v", cache, wantCache, kv)
	}
	if binaries := firstOf(kv["BINARIES"]); binaries != wantBinaries || !filepath.IsAbs(binaries) {
		t.Errorf("BINARIES=%q, want absolute path %q; protocol=%v", binaries, wantBinaries, kv)
	}
	if firstOf(kv["READY"]) != "1" || firstOf(kv["CLEANED"]) != "1" {
		t.Errorf("pgtest did not start and clean successfully: protocol=%v", kv)
	}
	if _, err := os.Stat(wantBinaries); err != nil {
		t.Errorf("expected binaries cache not present at %s: %v", wantBinaries, err)
	}
	if _, err := os.Lstat(filepath.Join(work, ".embedded-postgres-go")); !os.IsNotExist(err) {
		t.Errorf("relative fallback cache leaked into working directory: lstat err=%v", err)
	}
}
