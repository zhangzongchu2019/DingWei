package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
		owner              string // ""＝确是本目录的 postmaster
		want               string
	}{
		{"清理关掉（cutoff<=0）⇒ 一个都不动", 0, old, old, false, "", "skip_new"},
		{"目录不够旧 ⇒ 不敢动（可能属于正在跑的另一次）", cut, fresh, fresh, false, "", "skip_new"},
		{"目录够旧、PG 已不在 ⇒ 直接删", cut, old, old, false, "", "remove"},
		{"目录够旧、没有 pid 文件 ⇒ 直接删", cut, old, time.Time{}, false, "no_pid", "remove"},
		{"目录够旧、PG 还活着且 pid 文件也够旧 ⇒ 判为孤儿，先杀再删", cut, old, old, true, "", "terminate"},
		{"目录够旧、PG 还活着但 pid 文件是新的 ⇒ 只报不杀", cut, old, fresh, true, "", "skip_live_pg"},
		{"目录够旧、PG 活着而 pid 时刻判不出 ⇒ 只报不杀（不猜）", cut, old, time.Time{}, true, "", "skip_live_pg"},
		// 🔴 下面四行是复验件 REVIEW-G2-PGTEST-REAP-tosonnet50ts-20261001 第二节那条缺陷的判据：
		// 「够旧 ＋ 还活着」不足以授权 SIGTERM，还得核那个 PID 是不是本目录的 postmaster。
		{"PID 已被别的进程复用 ⇒ 不杀，只删目录", cut, old, old, true, "comm=sleep", "pid_reused"},
		{"comm 是 postgres 而数据目录不符 ⇒ 同样不杀（那是别人正在跑的一次）", cut, old, old, true, "datadir_mismatch", "pid_reused"},
		{"归属判不出 ⇒ 连目录也不动（删了可能是抽走一个活 PG 的脚下）", cut, old, old, true, pidOwnerUnknown, "skip_live_pg"},
		{"归属核对排在时刻判断之前：pid 文件是新的也一样先判复用", cut, old, fresh, true, "comm=sleep", "pid_reused"},
	}
	for _, c := range cases {
		if got := staleVerdict(now, c.cutoff, c.dirMtime, c.pidMtime, c.pidAlive, c.owner); got != c.want {
			t.Errorf("%s: got=%s want=%s", c.name, got, c.want)
		}
	}
}

func TestPidOwnerReason(t *testing.T) {
	// 归属判据的【纯函数】部分：读 /proc 那半在 pidOwnsDataDir（真进程版见下面几格）。
	// 🔴 这里的 dataDir 与各条 cmdline 都是【不存在】的路径 ⇒ normPath 走 Abs＋Clean 那条退路，
	// 判据本身（逐字比 comm、-D 的下一格、cwd）与符号链接无关，符号链接另有一格钉。
	const data = "/nonexistent/dingwei-pgtest-x/data"
	cases := []struct {
		name         string
		procOK       bool
		comm         string
		cmdline      []string
		cwd, dataDir string
		want         string
	}{
		{"proc 读不出 ⇒ 判不出，不猜成「不属于」", false, "", nil, "", data, pidOwnerUnknown},
		{"comm 不是 postgres ⇒ 判成不属于，并把 comm 如实报出来", true, "sleep\n", []string{"sleep", "300"}, data, data, "comm=sleep"},
		{"comm 只做逐字比：postgres-foo 不算（授权杀进程的判据宁可漏杀）", true, "postgres-foo\n", nil, data, data, "comm=postgres-foo"},
		{"cmdline 的 -D 指着本目录 ⇒ 归属成立", true, "postgres\n",
			[]string{"/u/l/pg/bin/postgres", "-D", data, "-p", "5432", "-k", "/tmp"}, "/somewhere/else", data, ""},
		{"cmdline 不符而 cwd 是本目录 ⇒ 归属成立（cwd 兜底，见 pidOwnerReason 头注）", true, "postgres\n",
			[]string{"postgres"}, data, data, ""},
		{"comm 对而 cmdline／cwd 都不符 ⇒ datadir_mismatch", true, "postgres\n",
			[]string{"postgres", "-D", "/nonexistent/other/data"}, "/nonexistent/other/data", data, "datadir_mismatch"},
		{"-D 在末尾没有值 ⇒ 不算命中", true, "postgres\n", []string{"postgres", "-D"}, "", data, "datadir_mismatch"},
		{"comm 读到了而 cmdline／cwd 都读不到 ⇒ 判成不属于（不是判不出）", true, "postgres\n", nil, "", data, "datadir_mismatch"},
		{"dataDir 为空 ⇒ 无从核对，如实报", true, "postgres\n", nil, "", "", "no_datadir"},
	}
	for _, c := range cases {
		if got := pidOwnerReason(c.procOK, c.comm, c.cmdline, c.cwd, c.dataDir); got != c.want {
			t.Errorf("%s: got=%q want=%q", c.name, got, c.want)
		}
	}
}

func TestNormPathSeesThroughSymlinks(t *testing.T) {
	// 🔴 为什么单钉一格：临时目录本身就常是符号链接，而 postmaster 的 cmdline／cwd 里写的是
	// 它自己看到的那一条。两侧不都归一，就会把【同一个目录】判成两个 —— 于是真孤儿杀不掉
	// （这一格测的是「不误杀」的反面：该杀的必须认得出来）。
	realRoot := t.TempDir()
	realData := filepath.Join(realRoot, "data")
	if err := os.MkdirAll(realData, 0o755); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(t.TempDir(), "tmp-link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("建符号链接失败，这一格测不到东西: %v", err)
	}
	linkData := filepath.Join(linkRoot, "data")

	if got, want := normPath(linkData), normPath(realData); got != want {
		t.Fatalf("normPath 没解析符号链接: %q vs %q", got, want)
	}
	if got := pidOwnerReason(true, "postgres\n", []string{"postgres", "-D", realData}, "", linkData); got != "" {
		t.Errorf("cmdline 走真路径、dataDir 走符号链接 ⇒ 必须判成同一个目录: got=%q", got)
	}
	if got := pidOwnerReason(true, "postgres\n", nil, realData, linkData); got != "" {
		t.Errorf("cwd 那条同样要过符号链接: got=%q", got)
	}
}

// mkStale 造一个「上一次留下的」现场：运行目录（可含 data/postmaster.pid），时刻往回拨。
func mkStale(t *testing.T, root, name string, age time.Duration, pid int) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if pid > 0 {
		putStalePid(t, dir, age, age, pid)
		return dir
	}
	mtime := time.Now().Add(-age)
	if err := os.Chtimes(dir, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return dir
}

// putStalePid 往【已存在】的残留目录里写 postmaster.pid，并把 pid 文件与目录的时刻分别往回拨。
//
// 🔴 为什么要从 mkStale 里拆出来：核归属那几格必须【先起替身进程、再拿它的 PID 写 pid 文件】，
//
//	而 mkStale 的顺序是「先有 PID 才有目录」，替身的 cwd 又必须是目录里的 data ⇒ 顺序反了。
//
// 🔴 为什么两个时刻分开给：判据里有「目录够旧而 pid 文件是新的」那一档（＝某次跑得很久的
//
//	并发运行），两个时刻绑成一个参数就造不出那个现场。0 就是「刚写的」。
func putStalePid(t *testing.T, dir string, pidAge, dirAge time.Duration, pid int) {
	t.Helper()
	p := filepath.Join(dir, "data", "postmaster.pid")
	if err := os.WriteFile(p, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pidMtime := time.Now().Add(-pidAge)
	if err := os.Chtimes(p, pidMtime, pidMtime); err != nil {
		t.Fatal(err)
	}
	dirMtime := time.Now().Add(-dirAge)
	if err := os.Chtimes(dir, dirMtime, dirMtime); err != nil {
		t.Fatal(err)
	}
}

// commOf 用【外部命令】核一个 PID 的 comm，不用生产代码那套读法 ——
// 前提核对若与被测函数同源，就等于自己给自己作证（同 pidalive_linux_test.go 的 psState）。
func commOf(pid int) (string, bool) {
	if out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output(); err == nil {
		return strings.TrimSpace(string(out)), true
	}
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
		return strings.TrimSpace(string(b)), true
	}
	return "", false
}

// fakePostmaster 起一个「在 /proc 里看着就是 dataDir 的 postmaster」的替身，返回其 PID。
//
// 🔴 为什么不用一个 `sleep` 当替身（旧版就是这么写的）：加了归属核对之后，comm=sleep 的替身
//
//	会被判成「PID 已被复用」而【不该】被终止 —— 那是另一格的判据
//	（TestCleanStaleDoesNotKillReusedPID）。这一格要的是「归属成立 ⇒ 该杀就杀」，
//	所以替身必须在 comm 与 datadir 两条上都像真的。
//
// 做法：把 sleep 复制成名叫 postgres 的可执行文件（内核用 exec 进来的那个文件名填 comm，
// 实测逐字＝postgres），cwd 设在 dataDir（实测真 postmaster 的 cwd 就在数据目录里）。
// 🔴 用【复制】不用软链：不依赖内核对 symlink 取名那一处的行为，判据面小一格。
func fakePostmaster(t *testing.T, dataDir string) int {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("这一格要真起进程并按 /proc 核归属；Windows 上本工具既不判活也不终止（见 reap_other.go）")
	}
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("没有 sleep，起不了替身: %v", err)
	}
	src, err := os.Open(sleepBin)
	if err != nil {
		t.Fatalf("打不开 %s: %v", sleepBin, err)
	}
	defer src.Close()
	bin := filepath.Join(t.TempDir(), "postgres")
	dst, err := os.OpenFile(bin, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("建替身可执行文件失败: %v", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		t.Fatalf("复制 sleep → postgres 失败: %v", err)
	}
	if err := dst.Close(); err != nil {
		t.Fatalf("收替身可执行文件失败: %v", err)
	}

	cmd := exec.Command(bin, "30")
	cmd.Dir = dataDir
	if err := cmd.Start(); err != nil {
		t.Fatalf("起替身 postmaster 失败: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	// 前提断言：comm 必须逐字是 postgres。不成立就说明这一格什么也没测到，必须红。
	got, ok := commOf(pid)
	if !ok {
		t.Fatalf("核不出 %d 的 comm ⇒ 前提没法断言", pid)
	}
	if got != "postgres" {
		t.Fatalf("前提没成立: comm=%q want %q（替身不像 postgres，这一格测不到归属核对）", got, "postgres")
	}
	return pid
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
	root := t.TempDir()
	dir := mkStale(t, root, "dingwei-pgtest-orphan", 3*time.Hour, 0)
	// 替身必须「看着就是本目录的 postmaster」（comm=postgres、cwd=data 目录）：
	// 归属核对加进来之后，随便一个 sleep 会走 pid_reused 那一档，不再被终止。
	pid := fakePostmaster(t, filepath.Join(dir, "data"))
	putStalePid(t, dir, 3*time.Hour, 3*time.Hour, pid)

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
	root := t.TempDir()
	// 目录够旧、而 pid 文件是刚写的 ⇒ 「某次跑得很久的并发运行」那一档：只报不杀。
	// 🔴 这一格要的正是【归属成立】的那一侧：comm=postgres、cwd 就在本目录的 data 里，
	// 只有「不够旧」这一条挡着 —— 若它也走了 pid_reused，说明归属核对把真 PG 判成了外人。
	dir := mkStale(t, root, "dingwei-pgtest-busy", 3*time.Hour, 0)
	pid := fakePostmaster(t, filepath.Join(dir, "data"))
	putStalePid(t, dir, 0, 3*time.Hour, pid) // pid 文件刚写的、目录够旧

	rep := cleanStale(root, stalePrefixes, time.Now(), staleCutoff)
	if len(rep.SkippedLive) != 1 {
		t.Fatalf("活着的 PG 且不够旧 ⇒ 必须只报不杀: %+v", rep)
	}
	if len(rep.Reused) != 0 {
		t.Errorf("归属成立的 PG 被当成「PID 已被复用」了（判据过严，真孤儿会杀不掉）: %v", rep.Reused)
	}
	if !pidAlive(pid) {
		t.Errorf("不该杀的进程被杀了（那会毁掉一次正在跑的测试）")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("只报不杀，目录也就不该动: %v", err)
	}
}

// ── 下面两格是复验件第二节那条缺陷的【真进程】反测 ────────────────────────────
//
// 复验方 tosonnet50ts 的复现形状：旧目录里写上一个【无关活进程】的 PID，工具就把它
// SIGTERM 了。两格分别钉「无关进程」（comm 就不对）与「别的目录的 postmaster」
// （comm 对、datadir 不对）—— 后者说明只核 comm 是不够的。

func TestCleanStaleDoesNotKillReusedPID(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("归属核对手上的读数是 /proc（见 reap_linux.go）；非 Linux 上 pidAlive 恒 false，走的是 remove 档，这一格不适用")
	}
	victim := exec.Command("sleep", "300") // 受害者：一个与 PG 毫不相干的活进程
	if err := victim.Start(); err != nil {
		t.Fatalf("起受害者失败: %v", err)
	}
	defer func() {
		_ = victim.Process.Kill()
		_, _ = victim.Process.Wait()
	}()
	vpid := victim.Process.Pid

	root := t.TempDir()
	dir := mkStale(t, root, "dingwei-pgtest-reused", 3*time.Hour, vpid)

	rep := cleanStale(root, stalePrefixes, time.Now(), staleCutoff)
	if !pidAlive(vpid) {
		t.Errorf("🔴 无关进程 %d 被 SIGTERM 了 —— 这正是复验件第二节复现的缺陷（PID 复用 ⇒ 误杀）", vpid)
	}
	if len(rep.Reused) != 1 {
		t.Fatalf("PID 被复用必须如实进 Reused 名单（不是混进 Terminated 冒充读数）: %+v", rep)
	}
	if len(rep.Terminated) != 0 {
		t.Errorf("Terminated 名单里混进了没被我们终止的东西: %v", rep.Terminated)
	}
	if !strings.Contains(rep.Reused[0], "comm=sleep") {
		t.Errorf("读数必须说得出【为什么】判成不是我们的: %q", rep.Reused[0])
	}
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("目录该删（那个 PID 与它无关，删目录不伤任何人）: %s", dir)
	}
	if rep.total() != 1 || len(rep.Removed) != 0 {
		t.Errorf("三个名单必须互斥（加起来才是「动了几个」）: %+v", rep)
	}
}

func TestCleanStaleDoesNotKillForeignPostmaster(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("同上：归属核对只有 Linux 那一版（见 reap_linux.go）")
	}
	// 一个【真像 postgres】、而数据目录在别处的进程：comm 对了，归属仍然不该认。
	other := t.TempDir()
	pid := fakePostmaster(t, other)

	root := t.TempDir()
	dir := mkStale(t, root, "dingwei-pgtest-foreign", 3*time.Hour, pid)

	rep := cleanStale(root, stalePrefixes, time.Now(), staleCutoff)
	if !pidAlive(pid) {
		t.Errorf("🔴 别的目录的 postmaster %d 被杀了 —— 只核 comm 不核 datadir 就会这样（毁掉一次正在跑的测试）", pid)
	}
	if len(rep.Reused) != 1 || len(rep.Terminated) != 0 {
		t.Fatalf("comm=postgres 而 datadir 不符 ⇒ 必须只删目录、不碰进程: %+v", rep)
	}
	if !strings.Contains(rep.Reused[0], "datadir_mismatch") {
		t.Errorf("读数必须说得出为什么: %q", rep.Reused[0])
	}
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("目录该删: %s", dir)
	}
}

// ── 补格（组长 2026-10-02T02:52:19Z 令）：读 /proc 那半对【不存在的 PID】必须报「判不出」 ──
//
// TestPidOwnerReason 钉的是纯函数那半（procOK=false ⇒ pidOwnerUnknown）；这一格钉真去读
// /proc 的那半：PID 不在 ⇒ comm 读不出来 ⇒ 必须落到同一个 pidOwnerUnknown，一个口径。
//
// 🔴 为什么值得单钉一格：这个返回值是 stale.go 授权动作的唯一依据 ——
//
//	报 ""（判成本目录的 postmaster）⇒ terminate ⇒ 对一个我们不认识的 PID 发 SIGTERM；
//	报任何「不属于」的理由          ⇒ pid_reused ⇒ 删目录（可能抽走一个活 PG 的脚下）；
//	只有 pidOwnerUnknown            ⇒ skip_live_pg（不杀也不删，只报数）。
//
//	PID 恰恰是内核会回收复用的东西：读不到只能等于「判不出」，不能等于任何一种结论。
//	反向对照两刀都红：把那一支改成 return ""（⇒ 误杀）或 return "datadir_mismatch"
//	（⇒ 误删目录），本格都抓得到 —— 它钉的是【确切的值】，不是「非空就算过」。
func TestPidOwnsDataDirReportsUnknownForAbsentPID(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 🔴 挑五次而不是挑一次：PID 会被回收，「扫到它不在」与「真去读它」之间理论上可能被
	// 别的进程占走。占了就重挑；五次都挑不出就红 —— 不静默 skip，那一格等于没跑过。
	for attempt := 0; attempt < 5; attempt++ {
		pid := candidateAbsentPID()
		if pid <= 0 || procDirExists(pid) {
			continue
		}
		got := pidOwnsDataDir(pid, dataDir)
		if procDirExists(pid) {
			continue // 调用期间被占走了 ⇒ 这次的读数不作数
		}
		if got != pidOwnerUnknown {
			t.Fatalf("PID %d 此刻不存在 ⇒ 必须报判不出 %q，实得 %q（报空串就会去 SIGTERM 一个不认识的进程；报别的理由就会删掉目录）",
				pid, pidOwnerUnknown, got)
		}
		return
	}
	t.Fatal("五次都没挑到一个【调用期间始终不存在】的 PID ⇒ 本格什么也没验到，不算通过")
}

// candidateAbsentPID 交回一个大概率不存在的 PID：Linux 上从 pid_max 往下扫，取第一个没有
// /proc 条目的；非 Linux 没有 /proc（reap_other.go 那一版对任何 PID 都报判不出），扫第一个
// 就返回 —— 本格要的口径在两个平台上是同一句，所以不 skip。
func candidateAbsentPID() int {
	start := 4194304
	if b, err := os.ReadFile("/proc/sys/kernel/pid_max"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n > 1024 {
			start = n - 1
		}
	}
	for pid := start; pid > start-4096 && pid > 1; pid-- {
		if !procDirExists(pid) {
			return pid
		}
	}
	return -1
}

// procDirExists 只看 /proc/<pid> 这个目录在不在。
// 🔴 与被测函数读 comm 那条路【不同源】：前提核对若与被测代码同源，就是自己给自己作证
// （同 commOf／psState 的理由）。非 Linux 上恒 false —— 那里本来就没有 /proc。
func procDirExists(pid int) bool {
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}
