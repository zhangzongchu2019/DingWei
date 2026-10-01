package pgtest

// 本文件是「随起随清」的【独立核对】面。
//
// 🔴 为什么不采信子进程自报的 CLEANED/PGLOG_REDIRECTED:同一件事两处都只自证,
// 就等于没有证。工具完全可以把 CLEANED 写死成 1(复验的 M2 变异就是这么干的),
// 那时用例照绿、判据已经死了。这里在起库【前后】各拍一次快照,只比【增量】。
//
// 🔴 为什么按增量而不是按绝对值:跑测试的容器里可能早就躺着别人留下的残留 ——
// 本席实测 PID 1 是 `sleep infinity`,不收孤儿,修复前每起一次库就留一个 postgres 僵尸,
// 已攒下 25 个且【谁都清不掉】(僵尸只能由父进程收,PID 1 不收)。
// 按绝对值判会让这批历史残留把之后每一次跑都判红;判据一红就没人看了,那比没有判据更糟。
// 代价如实写:增量判据看不见【本次之前】就存在的残留,那部分只能靠人巡机。

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// pgLogPrefix 是 embedded-postgres v1.34.0 给日志文件起的名(logging.go:18 的 os.CreateTemp)。
const pgLogPrefix = "embedded_postgres_log"

// zombieRecheckDelay 是「僵尸二次采样」的间隔。
//
// postmaster 退出到父进程 wait 它之间有一个极短窗口,那时它就是僵尸。
// 只采一次会把别人【正常收尾中】的状态判成残留;隔一小会儿再采一次,瞬时的自然消失。
const zombieRecheckDelay = 250 * time.Millisecond

// residueWatch 是一次「起库前后」的残留快照。零值不可用,请用 newResidueWatch。
type residueWatch struct {
	tempDir   string
	logs      map[string]struct{}
	logsOK    bool // 快照没拍成(读不了临时目录)⇒ 本项核对不适用,不判也不假装判过
	zombies   map[int]struct{}
	zombiesOK bool // /proc 读不到(非 Linux)⇒ 同上
}

// newResidueWatch 拍起库【前】的快照。必须在起库之前调用,否则没有基准可比。
func newResidueWatch() *residueWatch {
	w := &residueWatch{
		tempDir: os.TempDir(),
		logs:    map[string]struct{}{},
		zombies: map[int]struct{}{},
	}
	w.logs, w.logsOK = listPGLogs(w.tempDir)
	w.zombies, w.zombiesOK = listZombiePostgres()
	return w
}

// listPGLogs 列出目录里 embedded-postgres 的日志文件名。
func listPGLogs(dir string) (map[string]struct{}, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	out := make(map[string]struct{})
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), pgLogPrefix) {
			out[e.Name()] = struct{}{}
		}
	}
	return out, true
}

// listZombiePostgres 扫 /proc,找「comm 以 postgres 开头且状态为 Z」的 pid。
//
// 非 Linux 上 /proc 不存在 ⇒ 返回 ok=false,调用方据此跳过本项(不判、也不假装判过)。
func listZombiePostgres() (map[int]struct{}, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	out := make(map[int]struct{})
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue // 进程正好退出:不是残留,跳过
		}
		comm, state, ok := parseProcStat(string(data))
		if !ok {
			continue
		}
		if state == "Z" && strings.HasPrefix(comm, "postgres") {
			out[pid] = struct{}{}
		}
	}
	return out, true
}

// parseProcStat 从 /proc/<pid>/stat 取 comm 与状态。
//
// 🔴 不能按空格分列取第二、第三段:comm 用圆括号包着,而且【可以含空格与右括号】
// (内核会截断成 15 字符,但内容随进程名)。故按【最后一个】')' 切,后面第一段才是状态。
// 本席实测僵尸那一行长这样:604120 (postgres) Z 1 604120 …
func parseProcStat(s string) (comm, state string, ok bool) {
	open := strings.IndexByte(s, '(')
	closing := strings.LastIndexByte(s, ')')
	if open < 0 || closing <= open {
		return "", "", false
	}
	rest := strings.Fields(s[closing+1:])
	if len(rest) == 0 {
		return "", "", false
	}
	return s[open+1 : closing], rest[0], true
}

// check 报出【本次新增】的残留。它在 t.Cleanup 里跑,时序上晚于停库(登记顺序见 assertCleanup)。
func (w *residueWatch) check(t *testing.T) {
	t.Helper()
	if w == nil {
		return
	}
	if w.logsOK {
		if now, ok := listPGLogs(w.tempDir); ok {
			if added := addedNames(w.logs, now); len(added) > 0 {
				t.Errorf("🔴 系统临时目录 %s 多出 %d 个 PG 日志文件,「随清」不成立: %s\n"+
					"工具本该把 TMPDIR 指到自己的日志目录(embedded-postgres 的 logging.go:18 认它),"+
					"漏出来说明重定向失效或日志目录没删。", w.tempDir, len(added), strings.Join(added, ", "))
			}
		}
	}
	if !w.zombiesOK {
		return
	}
	if added := addedPIDs(w.zombies, listZombiePostgresOrEmpty()); len(added) > 0 {
		time.Sleep(zombieRecheckDelay) // 二次采样,避开正常收尾的瞬时窗口
		if again := addedPIDs(w.zombies, listZombiePostgresOrEmpty()); len(again) > 0 {
			t.Errorf("🔴 收尾后进程表里多出 %d 个 postgres 僵尸(pid: %s)。\n"+
				"成因:pg_ctl start 起了 postmaster 就退出,postmaster 成为孤儿过继给 PID 1,"+
				"而容器 PID 1(`sleep infinity`)不收尸 ⇒ 留下就再也清不掉,pgrep postgres 会把它读成「库还活着」。\n"+
				"工具本该用 PR_SET_CHILD_SUBREAPER 接管孤儿并在 Stop() 之后收尸(见 tools/pgtest/reap_linux.go)。\n"+
				"注:本项按【增量】判,历史残留不算。", len(again), strings.Join(again, ", "))
		}
	}
}

func listZombiePostgresOrEmpty() map[int]struct{} {
	out, ok := listZombiePostgres()
	if !ok {
		return map[int]struct{}{}
	}
	return out
}

// addedNames 返回 now 里有、before 里没有的名字(排序,保证文案稳定可复现)。
func addedNames(before, now map[string]struct{}) []string {
	var out []string
	for name := range now {
		if _, was := before[name]; !was {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// addedPIDs 同 addedNames,给 pid 用(排序同样是为了文案稳定)。
func addedPIDs(before, now map[int]struct{}) []string {
	var out []string
	for pid := range now {
		if _, was := before[pid]; !was {
			out = append(out, strconv.Itoa(pid))
		}
	}
	sort.Strings(out)
	return out
}
