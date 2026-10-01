package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestStaleVerdict(t *testing.T) {
	now := time.Date(2026, 10, 1, 19, 0, 0, 0, time.UTC)
	old := now.Add(-3 * time.Hour)
	fresh := now.Add(-1 * time.Minute)
	cut := 2 * time.Hour

	cases := []struct {
		name               string
		cutoff             time.Duration
		dirMtime, pidMtime time.Time
		pidAlive           bool
		want               string
	}{
		{"清理关掉（cutoff<=0）⇒ 一个都不动", 0, old, old, false, "skip_new"},
		{"目录不够旧 ⇒ 不敢动（可能属于正在跑的另一次）", cut, fresh, fresh, false, "skip_new"},
		{"目录够旧、PG 已不在 ⇒ 直接删", cut, old, old, false, "remove"},
		{"目录够旧、没有 pid 文件 ⇒ 直接删", cut, old, time.Time{}, false, "remove"},
		{"目录够旧、PG 还活着且 pid 文件也够旧 ⇒ 判为孤儿，先杀再删", cut, old, old, true, "terminate"},
		{"目录够旧、PG 还活着但 pid 文件是新的 ⇒ 只报不杀", cut, old, fresh, true, "skip_live_pg"},
		{"目录够旧、PG 活着而 pid 时刻判不出 ⇒ 只报不杀（不猜）", cut, old, time.Time{}, true, "skip_live_pg"},
	}
	for _, c := range cases {
		if got := staleVerdict(now, c.cutoff, c.dirMtime, c.pidMtime, c.pidAlive); got != c.want {
			t.Errorf("%s: got=%s want=%s", c.name, got, c.want)
		}
	}
}

// mkStale 造一个「上一次留下的」现场：运行目录（可含 data/postmaster.pid），时刻往回拨。
func mkStale(t *testing.T, root, name string, age time.Duration, pid int) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	mtime := time.Now().Add(-age)
	if pid > 0 {
		p := filepath.Join(dir, "data", "postmaster.pid")
		if err := os.WriteFile(p, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCleanStaleRemovesOnlyOldPrefixedDirs(t *testing.T) {
	root := t.TempDir()
	old := mkStale(t, root, "dingwei-pgtest-old", 3*time.Hour, 0)
	new1 := mkStale(t, root, "dingwei-pgtest-new", time.Minute, 0)
	other := mkStale(t, root, "unrelated-thing", 3*time.Hour, 0)

	rep := cleanStale(root, stalePrefixes, time.Now(), staleCutoff)
	if len(rep.Removed) != 1 || rep.Removed[0] != old {
		t.Errorf("该删的没删对: Removed=%v want [%s]", rep.Removed, old)
	}
	if _, err := os.Stat(old); err == nil {
		t.Errorf("RemoveAll 报了成功而目录仍在 %s", old)
	}
	if len(rep.SkippedNew) != 1 || rep.SkippedNew[0] != new1 {
		t.Errorf("不够旧的目录必须被跳过（可能是正在跑的另一次）: SkippedNew=%v", rep.SkippedNew)
	}
	if _, err := os.Stat(new1); err != nil {
		t.Errorf("不够旧的目录被动了: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("前缀不匹配的目录被动了（判据是「按前缀」不是「按够旧」）: %v", err)
	}
	if rep.total() != 1 {
		t.Errorf("total=%d want 1（协议行报的就是这个数）", rep.total())
	}
}

func TestCleanStaleTerminatesOldOrphanPostmaster(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("这一格要真起一个进程并终止它，Windows 上本工具不终止进程（见 reap_other.go）")
	}
	cmd := exec.Command("sleep", "30")
	if e := cmd.Start(); e != nil {
		t.Fatalf("起替身 postmaster 失败: %v", e)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	pid := cmd.Process.Pid

	root := t.TempDir()
	dir := mkStale(t, root, "dingwei-pgtest-orphan", 3*time.Hour, pid)

	rep := cleanStale(root, stalePrefixes, time.Now(), staleCutoff)
	if len(rep.Terminated) != 1 {
		t.Fatalf("够旧的孤儿 postmaster 没被处置: %+v", rep)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("孤儿目录仍在 %s", dir)
	}
	if runtime.GOOS == "linux" {
		deadline := time.Now().Add(3 * time.Second)
		for pidAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if pidAlive(pid) {
			t.Errorf("孤儿 postmaster %d 还活着 —— 「PG 随之退出」这一条没做到", pid)
		}
	}
	if rep.total() != 1 || len(rep.Removed) != 0 {
		t.Errorf("Terminated 与 Removed 必须互斥（加起来才是动了几个）: %+v", rep)
	}
}

func TestCleanStaleReportsLivePGWhenNotOldEnough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上判不出 PID 是否活着（见 reap_other.go），这一格不适用")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("起替身 postmaster 失败: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	root := t.TempDir()
	// 目录够旧、而 pid 文件是刚写的 ⇒ 「某次跑得很久的并发运行」那一档：只报不杀。
	dir := mkStale(t, root, "dingwei-pgtest-busy", 3*time.Hour, cmd.Process.Pid)
	if err := os.Chtimes(filepath.Join(dir, "data", "postmaster.pid"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	rep := cleanStale(root, stalePrefixes, time.Now(), staleCutoff)
	if len(rep.SkippedLive) != 1 {
		t.Fatalf("活着的 PG 且不够旧 ⇒ 必须只报不杀: %+v", rep)
	}
	if !pidAlive(cmd.Process.Pid) {
		t.Errorf("不该杀的进程被杀了（那会毁掉一次正在跑的测试）")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("只报不杀，目录也就不该动: %v", err)
	}
}
