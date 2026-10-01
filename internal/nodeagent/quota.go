package nodeagent

// 席位磁盘三件套之 XFS 项目配额（T-G4-12；磁盘路径已定：只读 rootfs＋带限 tmpfs＋
// 席位专属目录 XFS 项目配额 bind——不写 --storage-opt size，Docker 29 快照器不生效已实测）。
//
// M1 运维复验（88e2283d）修订点：
//  ① 挂载点：findmnt --target 真实解析（.68 是 /data1/dingwei-node68，不再两档硬编码）；
//  ② 属主：Setup 建目录即 chown 10001:10001（非 root 容器可用；权限仍 0700/0600，不放宽）；
//  ③ 项目 ID：从 `xfs_quota report -p -n` 权威读数解析（不经 /etc/projid），alloc+setup 全程
//     flock 跨进程锁；冲突拒，不覆盖；
//  ④ Clear：每步失败上抛＋清完复查 report——项目残留（用量>0 或目录仍在）即返回错误（D5）。
//
// 可注入点（与 netDial 同型）：findmntTarget／xfsRun／chownForSeat／quotaLockPath。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// findmntTarget 求路径所在挂载点（findmnt -n -o TARGET --target <path>）。
var findmntTarget = func(path string) (string, error) {
	out, err := exec.Command("findmnt", "-n", "-o", "TARGET", "--target", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("findmnt --target %s: %v: %s", path, err, out)
	}
	mnt := strings.TrimSpace(string(out))
	if mnt == "" {
		return "", fmt.Errorf("findmnt --target %s: 无挂载点", path)
	}
	return mnt, nil
}

// xfsRun 执行 xfs_quota（参数逐个传，不经过 shell）。
var xfsRun = func(args ...string) ([]byte, error) {
	full := append([]string{"-x", "-c"}, args...)
	return exec.Command("xfs_quota", full...).CombinedOutput()
}

// lsattrOut 取目录的项目号输出（lsattr -d -p；可注入——D5 复查用例要伪造"有项目号"）。
var lsattrOut = func(dir string) (string, error) {
	out, err := exec.Command("lsattr", "-d", "-p", dir).CombinedOutput()
	return string(out), err
}

// quotaLockPath 项目配额分配的跨进程锁（flock）。
var quotaLockPath = "/run/lock/dingwei-node-quota.lock"

// withQuotaLock 跨进程互斥执行 fn（alloc+setup 不得与并发建席撞 ID）。
func withQuotaLock(fn func() error) error {
	f, err := os.OpenFile(quotaLockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("quota lock: %v", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("quota flock: %v", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

type quotaOps interface {
	// Setup 建席位目录并设项目配额（diskMB=0 表示只建目录不设限——预记账口径）。
	Setup(root, seatID string, diskMB int64) error
	// Clear 清项目配额并删席位目录；任何一步失败或清后复查有残留即返回错误（D5）。
	Clear(root, seatID string) error
}

var seatIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

func validSeatID(id string) bool { return seatIDPattern.MatchString(id) }

func seatDir(root, seatID string) string { return filepath.Join(root, seatID) }

// ---------- 真机实现 ----------

type xfsQuota struct{}

// resolveMountPoint 求 seatRoot 的真实 XFS 挂载点（M1 复验阻断 1：.68 是 /data1/dingwei-node68，
// 两档硬编码会把配额发到 /data（ext4））。以 findmnt --target 为准，不猜。
func resolveMountPoint(root string) (string, error) {
	return findmntTarget(root)
}

// usedProjectIDs 从 `xfs_quota report -p -n <mnt>` 权威读数解析已用项目 ID（M1 复验阻断 3：
// 不经 /etc/projid——v3 直设法本无 /etc 条目；解析用明确 split+Atoi，不用 %*[^:]）。
func usedProjectIDs(mnt string) (map[int]bool, error) {
	out, err := xfsRun("report -p -n", mnt)
	if err != nil {
		return nil, fmt.Errorf("xfs_quota report: %v: %s", err, out)
	}
	used := map[int]bool{}
	for _, ln := range strings.Split(string(out), "\n") {
		fields := strings.Fields(ln)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "#") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(fields[0], "#"))
		if err != nil {
			continue // 表头等非数字行跳过
		}
		used[id] = true
	}
	return used, nil
}

// allocProjectID 在 60000-69999 段取第一个空闲 ID（调用方须在 quotaLock 内）。
func allocProjectID(mnt string) (int, error) {
	used, err := usedProjectIDs(mnt)
	if err != nil {
		return 0, err
	}
	for id := 60000; id < 70000; id++ {
		if !used[id] {
			return id, nil
		}
	}
	return 0, fmt.Errorf("无空闲项目 ID（60000-69999 段满）")
}

func (xfsQuota) Setup(root, seatID string, diskMB int64) error {
	if !validSeatID(seatID) {
		return fmt.Errorf("seat_id 非法: %q", seatID)
	}
	dir := seatDir(root, seatID)
	// 🔴 复验 r2 阻断①：Setup 中途失败必须自拆——任何一步失败，目录与已设项目都不留
	//（否则 create 看到 Setup 错误时目录已在，半成品残留）。
	cleanupSelf := func() { _ = os.RemoveAll(dir) }
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// 🔴 M1 复验阻断 2：目录必须归席位运行用户（10001:10001）——容器强制该 UID，
	// root:0700 会让席位连 /work 都进不去。权限保持 0700，不放宽 world-readable。
	if err := chownForSeat(dir); err != nil {
		cleanupSelf()
		return err
	}
	if diskMB <= 0 {
		return nil
	}
	mnt, err := resolveMountPoint(root)
	if err != nil {
		cleanupSelf()
		return err
	}
	return withQuotaLock(func() error {
		pid, err := allocProjectID(mnt)
		if err != nil {
			cleanupSelf()
			return err
		}
		// v3 直设法：project -s -p <dir> <id>（不经 /etc/projects|projid）
		if out, err := xfsRun(fmt.Sprintf("project -s -p %s %d", dir, pid), mnt); err != nil {
			cleanupSelf()
			return fmt.Errorf("xfs project -s: %v: %s", err, out)
		}
		// lsattr 断言项目号落对（v3 同款闸：两目录同落一个 ID 的 bug 不得回潮）
		if out, err := lsattrOut(dir); err != nil {
			cleanupSelf()
			return fmt.Errorf("lsattr -d -p %s: %v: %s", dir, err, out)
		} else {
			got, aerr := strconv.Atoi(strings.Fields(out)[0])
			if aerr != nil || got != pid {
				cleanupSelf()
				return fmt.Errorf("项目号断言失败: 期望 %d, 实得 %q", pid, strings.TrimSpace(out))
			}
		}
		if out, err := xfsRun(fmt.Sprintf("limit -p bhard=%dm %d", diskMB, pid), mnt); err != nil {
			cleanupSelf()
			return fmt.Errorf("xfs limit: %v: %s", err, out)
		}
		return nil
	})
}

// projectUsage 复查某项目号在 report 里的用量（blocks）；不存在 ⇒ present=false。
func projectUsage(mnt string, pid int) (present bool, usedBlocks int64, err error) {
	out, err := xfsRun("report -p -n", mnt)
	if err != nil {
		return false, 0, fmt.Errorf("xfs_quota report: %v: %s", err, out)
	}
	for _, ln := range strings.Split(string(out), "\n") {
		fields := strings.Fields(ln)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "#") {
			continue
		}
		id, aerr := strconv.Atoi(strings.TrimPrefix(fields[0], "#"))
		if aerr != nil || id != pid {
			continue
		}
		blocks, berr := strconv.ParseInt(fields[1], 10, 64)
		if berr != nil {
			return true, 0, fmt.Errorf("report 用量解析失败: %q", ln)
		}
		return true, blocks, nil
	}
	return false, 0, nil
}

func (xfsQuota) Clear(root, seatID string) error {
	if !validSeatID(seatID) {
		return fmt.Errorf("seat_id 非法: %q", seatID)
	}
	dir := seatDir(root, seatID)
	mnt, err := resolveMountPoint(root)
	if err != nil {
		return err
	}
	// 找该目录绑定的项目号（lsattr）——没有配额项目的席位（diskMB=0 预记账席）直接删目录即可
	pid := -1
	if out, err := lsattrOut(dir); err == nil {
		if fields := strings.Fields(out); len(fields) > 0 {
			pid, _ = strconv.Atoi(fields[0]) // 解析失败按无项目处理
		}
	}
	if pid > 0 {
		// 🔴 M1 复验阻断 4：每步失败上抛，不再静默吞错
		if out, err := xfsRun(fmt.Sprintf("limit -p bhard=0 %d", pid), mnt); err != nil {
			return fmt.Errorf("xfs limit bhard=0: %v: %s", err, out)
		}
		// 摘掉项目映射（project -C）——否则目录删了项目还在报表上（复验 r2 阻断②：
		// "项目在但用量 0"不算清除）。
		if out, err := xfsRun(fmt.Sprintf("project -C %d", pid), mnt); err != nil {
			return fmt.Errorf("xfs project -C: %v: %s", err, out)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除席位目录: %v", err)
	}
	if pid > 0 {
		// 清完复查（D5＋复验 r2 阻断②）：report 里该项目必须【消失】——存在即红
		//（不论用量；存在就证明映射没摘净）。
		present, used, err := projectUsage(mnt, pid)
		if err != nil {
			return err
		}
		if present {
			return fmt.Errorf("D5 清除未闭合: 项目 %d 仍在报表（用量 %d blocks）——项目存在即红", pid, used)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			return fmt.Errorf("D5 清除未闭合: 席位目录仍在 %s", dir)
		}
	}
	return nil
}

// ---------- 单测假实现 ----------

type fakeQuotaCall struct {
	Op, Root, SeatID string
	DiskMB           int64
}

type fakeQuota struct {
	calls  []fakeQuotaCall
	dirs   map[string]bool
	failAt string // 命中该 Op 即返回错误（反向用例）
}

func (f *fakeQuota) Setup(root, seatID string, diskMB int64) error {
	f.calls = append(f.calls, fakeQuotaCall{Op: "setup", Root: root, SeatID: seatID, DiskMB: diskMB})
	if f.failAt == "setup" {
		return fmt.Errorf("fake quota setup 失败")
	}
	if f.dirs == nil {
		f.dirs = map[string]bool{}
	}
	f.dirs[seatDir(root, seatID)] = true
	return nil
}

func (f *fakeQuota) Clear(root, seatID string) error {
	f.calls = append(f.calls, fakeQuotaCall{Op: "clear", Root: root, SeatID: seatID})
	if f.failAt == "clear" {
		return fmt.Errorf("fake quota clear 失败（M1 复验：清理失败必须上抛）")
	}
	delete(f.dirs, seatDir(root, seatID))
	return nil
}


// XFSQuota 真机配额实现（root/systemd；cmd/nodeagent 用）。
func XFSQuota() quotaOps { return xfsQuota{} }
