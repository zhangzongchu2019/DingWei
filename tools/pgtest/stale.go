package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 「下一次启动时按前缀清掉上一次没清干净的现场」—— 组长 2026-10-01T18:36:20Z 追加的那条。
//
// 为什么需要它：本工具的退出清理有两条触发路径（stdin 关闭、收到 SIGINT/SIGTERM），
// 而 `go test` 超时被杀时可能出现【两条都不成立】的形状 —— 整个进程组被 SIGKILL 打死，
// 工具自己也没机会跑收尾。tosonnet50ts 实测三次（09:13／09:53／10:28），每次留下
// 一个孤儿 postgres ＋ 一对 /tmp/dingwei-pgtest-* 目录，且【谁都清不掉】：
// 僵尸只能由父进程收，而容器 PID 1 是 `sleep infinity`，不收。
// ⇒ 预防（parentGone 看门狗）之外，还得有一条【事后】的清理路径，否则残留会一直攒。
//
// 🔴 判据是「按前缀 ＋ 够旧」，不是「按前缀」：只按前缀就会把【正在跑的另一次】的
//
//	运行目录连锅端。够旧的门槛见 staleCutoff。

// stalePrefixes 是本工具两个目录的前缀（与 main.go 里 MkdirTemp 的模板同源）。
// 运行目录 dingwei-pgtest-*、日志目录 dingwei-pgtest-log-* ⇒ 一个前缀全覆盖。
var stalePrefixes = []string{"dingwei-pgtest-"}

// staleCutoff 是「多旧才算上一次留下的」默认门槛。
//
// 取 2 小时的依据：本工具的一次运行实测 3–13 秒，最长的整包 -count=50 也就 12–60 分钟；
// 2 小时之上还活着的，只能是【被杀后没人管】的现场。🔴 这是门槛不是保证：
// 若真有人跑超过 2 小时的单次起库，本清理会误判 —— 故 -stale-cutoff 可调、可关（≤0 即关）。
const staleCutoff = 2 * time.Hour

// pidOwnerUnknown 是「归属判不出」这个专用记号（见 pidOwnsDataDir）。
//
// 🔴 它与「判成不属于本目录」是两件事，不得合并：判成不属于 ⇒ 可以放心删目录（那个进程
//
//	与这个目录无关）；判不出 ⇒ 连「删目录」都不安全（万一它真是本目录的 postmaster，
//	删掉数据目录等于把一个活 PG 的脚下抽走，还留下一个谁都清不掉的孤儿）。
const pidOwnerUnknown = "unknown"

// staleReport 是一次清理的实况，逐目录点名（不报一个总数了事）。
type staleReport struct {
	Removed     []string // 目录已删且 stat 不到
	Terminated  []string // 目录里有【够旧】的活 postmaster：先 SIGTERM 再删
	Reused      []string // postmaster.pid 里的 PID 【不是本目录的 postmaster】⇒ 不杀，只删目录
	SkippedNew  []string // 不够旧 ⇒ 不敢动（可能属于正在跑的另一次）
	SkippedLive []string // 只报不动：活 postmaster 不够旧，或 PID 归属判不出
	Failed      []string // 该删而删不掉（RemoveAll 报错，或报了成功却还在）
}

// total 是「动了几个」（删掉的＋先杀再删的＋PID 被复用只删目录的），供协议行用。
// 🔴 三个名单互斥：一个目录只进其中一个，加起来才是「动了几个」。
func (r staleReport) total() int { return len(r.Removed) + len(r.Terminated) + len(r.Reused) }

func (r staleReport) String() string {
	return fmt.Sprintf("removed=%d terminated=%d pid_reused=%d skipped_new=%d skipped_live=%d failed=%d",
		len(r.Removed), len(r.Terminated), len(r.Reused), len(r.SkippedNew), len(r.SkippedLive), len(r.Failed))
}

// staleVerdict 是清理的【全部判断】，纯函数（不碰文件系统 ⇒ 可确定性反测）。
//
//	dirMtime   目录本身的修改时刻
//	pidMtime   data/postmaster.pid 的修改时刻（≈ PG 的启动时刻）；没有 pid 文件时传零值
//	pidAlive   postmaster.pid 里那个 PID 现在还活着吗
//	pidOwner   那个 PID 是不是【本目录的】postmaster：""＝确是；pidOwnerUnknown＝判不出；
//	           其余值＝判成不是（值本身就是理由，会原样进读数）
//
// 取值：remove（直接删）· terminate（先 SIGTERM 再删）· skip_new（不够旧）
//
//	· skip_live_pg（只报不动）· pid_reused（PID 已被别的进程复用 ⇒ 不杀，只删目录）
//
// 🔴 为什么「活 PG」还要再判一次够不够旧：目录够旧而 PG 还活着，有两种可能 ——
//
//	上一次被杀留下的孤儿（该杀），或某次跑得很久的并发运行（不该杀）。
//	用 postmaster.pid 的时刻再卡一道：PG 自己也够旧，才认定是孤儿。
//
// 🔴 为什么「够旧」还不够、必须再核归属（复验件 REVIEW-G2-PGTEST-REAP-tosonnet50ts-20261001
//
//	第二节，复验方已现场复现）：pid 文件里那个数字只是【当时】的 postmaster。目录与 pid 文件
//	都熬过 2 小时之后，这个 PID 完全可能已被同一用户的别的进程复用（容器里 PID 空间小，
//	跑了几小时的机器上不稀奇），于是「够旧 ＋ 还活着」这条原判据会把一个【毫不相干】的
//	进程 SIGTERM 掉。杀错进程的代价远大于留一个残留目录 ⇒ 归属核对放在时刻判断【之前】：
//	判成不是我们的 ⇒ 一律不杀；判不出 ⇒ 连目录也不动。
func staleVerdict(now time.Time, cutoff time.Duration, dirMtime, pidMtime time.Time, pidAlive bool, pidOwner string) string {
	if cutoff <= 0 {
		return "skip_new" // 清理已关（-stale-cutoff ≤ 0）：一个都不动
	}
	if now.Sub(dirMtime) < cutoff {
		return "skip_new"
	}
	if !pidAlive {
		return "remove"
	}
	if pidOwner == pidOwnerUnknown {
		return "skip_live_pg" // 归属判不出 ⇒ 不杀也不删（删了可能是抽走一个活 PG 的脚下）
	}
	if pidOwner != "" {
		return "pid_reused" // 确是别的进程占着这个 PID ⇒ 不杀，只删目录
	}
	if pidMtime.IsZero() || now.Sub(pidMtime) < cutoff {
		return "skip_live_pg"
	}
	return "terminate"
}

// normPath 把一条路径归一成「解析符号链接之后的绝对路径」，供归属核对逐字比。
//
// 🔴 为什么必须解析：临时目录本身就常是符号链接（macOS 的 /var → /private/var、
//
//	容器里把 /tmp 挂成 symlink 的也有），而 postmaster 的 cmdline 里写的是它自己
//	看到的那一条 ⇒ 逐字比会把【同一个目录】判成两个，于是该杀的孤儿杀不掉。
//	两侧都过这一道，才算「双向」核对。
//
// 解析不了（路径已不存在等）就退回 Abs＋Clean：比不出来就【不匹配】，方向是保守的。
func normPath(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return filepath.Clean(p)
}

// pidOwnerReason 是归属判据的【纯函数】部分（读 /proc 的那半在 pidOwnsDataDir）：
// 给它已经从 /proc 取来的三样东西，它只回答「是不是本目录的 postmaster」，不是就说为什么。
//
//	procOK   comm 读到了吗；读不到 ⇒ 归属判不出（返回 pidOwnerUnknown，不猜）
//	comm     /proc/<pid>/comm 原样内容
//	cmdline  /proc/<pid>/cmdline 按 NUL 切开的各段
//	cwd      /proc/<pid>/cwd 的读链接结果
//
// 判据（与本席一次性 PG 探针的实测读数同源）：comm 逐字等于 postgres，
// 且 cmdline 里 -D 的下一格 ＝ dataDir，或 cwd ＝ dataDir —— 两条命中一条即算归属。
//
// 🔴 comm 用【逐字等于】而不是前缀/包含：前缀匹配会把 postgres-foo 这类名字放进来，
//
//	而这一格的用途是「授权杀进程」，宁可漏杀不可错杀。
//
// 🔴 cwd 那条不是多余的兜底：cmdline 只认 `-D <dir>` 分成两格的写法（实测 pg_ctl 就是这么起的），
//
//	若将来换成 `-D<dir>` 这种 glued 写法，cmdline 这条会失配，而 postmaster 的 cwd 实测就在
//	数据目录里 ⇒ 归属仍然判得出来，不至于把该杀的孤儿判成「不是我们的」。
func pidOwnerReason(procOK bool, comm string, cmdline []string, cwd, dataDir string) string {
	if !procOK {
		return pidOwnerUnknown
	}
	if name := strings.TrimSpace(comm); name != "postgres" {
		return "comm=" + name
	}
	want := normPath(dataDir)
	if want == "" {
		return "no_datadir"
	}
	for i, f := range cmdline {
		if f == "-D" && i+1 < len(cmdline) && normPath(cmdline[i+1]) == want {
			return ""
		}
	}
	if normPath(cwd) == want {
		return ""
	}
	return "datadir_mismatch"
}

// cleanStale 扫 root 下前缀命中的目录，按 staleVerdict 处置。
//
// 🔴 不采信 RemoveAll 的返回值：删完再 stat 一次，还在就算 Failed（与 cleanDir 同款要求）。
// 🔴 也不采信 postmaster.pid 里的 PID「就是我们起的那个」：SIGTERM 之前必须核归属
//
//	（见 staleVerdict／pidOwnsDataDir）。核不过就只删目录、不碰进程，并在读数里如实标出来。
func cleanStale(root string, prefixes []string, now time.Time, cutoff time.Duration) staleReport {
	var rep staleReport
	entries, err := os.ReadDir(root)
	if err != nil {
		return rep // 读不到临时目录 ⇒ 什么也不动（不假装清过）
	}
	for _, e := range entries {
		if !e.IsDir() || !hasAnyPrefix(e.Name(), prefixes) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		info, err := e.Info()
		if err != nil {
			rep.Failed = append(rep.Failed, dir)
			continue
		}
		pid, pidMtime, alive := readPostmaster(dir)
		// 🔴 杀之前先核归属：这个 PID 现在还是不是【本目录的】postmaster。
		// 判据与理由见 staleVerdict 的头注（复验方已现场复现「PID 复用 ⇒ 误杀无关进程」）。
		owner := pidOwnsDataDir(pid, filepath.Join(dir, "data"))
		terminated, reused := false, false
		switch staleVerdict(now, cutoff, info.ModTime(), pidMtime, alive, owner) {
		case "skip_new":
			rep.SkippedNew = append(rep.SkippedNew, dir)
			continue
		case "skip_live_pg":
			if owner == pidOwnerUnknown {
				rep.SkippedLive = append(rep.SkippedLive, fmt.Sprintf("%s(pid=%d,归属判不出)", dir, pid))
			} else {
				rep.SkippedLive = append(rep.SkippedLive, fmt.Sprintf("%s(pid=%d)", dir, pid))
			}
			continue
		case "pid_reused":
			rep.Reused = append(rep.Reused, fmt.Sprintf("%s(pid=%d,%s)", dir, pid, owner))
			reused = true
		case "terminate":
			if err := terminatePID(pid); err != nil {
				fmt.Fprintf(os.Stderr, "pgtest: 给孤儿 postmaster %d 发 SIGTERM 失败: %v\n", pid, err)
			} else {
				waitGonePID(pid, 5*time.Second)
			}
			rep.Terminated = append(rep.Terminated, fmt.Sprintf("%s(pid=%d)", dir, pid))
			terminated = true
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "pgtest: 删残留目录 %s 失败: %v\n", dir, err)
			rep.Failed = append(rep.Failed, dir)
			continue
		}
		if _, err := os.Stat(dir); err == nil {
			fmt.Fprintf(os.Stderr, "pgtest: RemoveAll 返回成功但残留目录仍在 %s\n", dir)
			rep.Failed = append(rep.Failed, dir)
			continue
		}
		if terminated || reused {
			continue // 已记进 Terminated／Reused，不重复记 Removed（三个名单互斥，加起来才是「动了几个」）
		}
		rep.Removed = append(rep.Removed, dir)
	}
	return rep
}

// readPostmaster 读 <dir>/data/postmaster.pid：返回 PID、该文件的修改时刻、PID 是否还活着。
// 任一步不成立都如实报（pid=0／零时刻／false），不猜。
func readPostmaster(dir string) (int, time.Time, bool) {
	p := filepath.Join(dir, "data", "postmaster.pid")
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, time.Time{}, false
	}
	info, err := os.Stat(p)
	if err != nil {
		return 0, time.Time{}, false
	}
	first := strings.SplitN(string(b), "\n", 2)[0]
	pid, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil || pid <= 0 {
		return 0, info.ModTime(), false // PID 读不出 ⇒ 当它不活着（宁可直接删，不去杀一个未知进程）
	}
	return pid, info.ModTime(), pidAlive(pid)
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
