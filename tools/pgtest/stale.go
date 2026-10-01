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

// staleReport 是一次清理的实况，逐目录点名（不报一个总数了事）。
type staleReport struct {
	Removed     []string // 目录已删且 stat 不到
	Terminated  []string // 目录里有【够旧】的活 postmaster：先 SIGTERM 再删
	SkippedNew  []string // 不够旧 ⇒ 不敢动（可能属于正在跑的另一次）
	SkippedLive []string // 有活 postmaster 但【不够旧】⇒ 只报不杀
	Failed      []string // 该删而删不掉（RemoveAll 报错，或报了成功却还在）
}

// total 是「动了几个」（删掉的＋先杀再删的），供协议行用。
func (r staleReport) total() int { return len(r.Removed) + len(r.Terminated) }

func (r staleReport) String() string {
	return fmt.Sprintf("removed=%d terminated=%d skipped_new=%d skipped_live=%d failed=%d",
		len(r.Removed), len(r.Terminated), len(r.SkippedNew), len(r.SkippedLive), len(r.Failed))
}

// staleVerdict 是清理的【全部判断】，纯函数（不碰文件系统 ⇒ 可确定性反测）。
//
//	dirMtime   目录本身的修改时刻
//	pidMtime   data/postmaster.pid 的修改时刻（≈ PG 的启动时刻）；没有 pid 文件时传零值
//	pidAlive   postmaster.pid 里那个 PID 现在还活着吗
//
// 取值：remove（直接删）· terminate（先 SIGTERM 再删）· skip_new（不够旧）
//
//	· skip_live_pg（有活 PG 且不够旧 ⇒ 只报不杀）
//
// 🔴 为什么「活 PG」还要再判一次够不够旧：目录够旧而 PG 还活着，有两种可能 ——
//
//	上一次被杀留下的孤儿（该杀），或某次跑得很久的并发运行（不该杀）。
//	用 postmaster.pid 的时刻再卡一道：PG 自己也够旧，才认定是孤儿。
func staleVerdict(now time.Time, cutoff time.Duration, dirMtime, pidMtime time.Time, pidAlive bool) string {
	if cutoff <= 0 {
		return "skip_new" // 清理已关（-stale-cutoff ≤ 0）：一个都不动
	}
	if now.Sub(dirMtime) < cutoff {
		return "skip_new"
	}
	if !pidAlive {
		return "remove"
	}
	if pidMtime.IsZero() || now.Sub(pidMtime) < cutoff {
		return "skip_live_pg"
	}
	return "terminate"
}

// cleanStale 扫 root 下前缀命中的目录，按 staleVerdict 处置。
//
// 🔴 不采信 RemoveAll 的返回值：删完再 stat 一次，还在就算 Failed（与 cleanDir 同款要求）。
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
		terminated := false
		switch staleVerdict(now, cutoff, info.ModTime(), pidMtime, alive) {
		case "skip_new":
			rep.SkippedNew = append(rep.SkippedNew, dir)
			continue
		case "skip_live_pg":
			rep.SkippedLive = append(rep.SkippedLive, fmt.Sprintf("%s(pid=%d)", dir, pid))
			continue
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
		if terminated {
			continue // 已记进 Terminated，不重复记 Removed（两个名单互斥，加起来才是「动了几个」）
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
