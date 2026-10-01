package nodeagent

// 席位磁盘三件套之 XFS 项目配额（T-G4-12；磁盘路径已定：只读 rootfs＋带限 tmpfs＋
// 席位专属目录 XFS 项目配额 bind——不写 --storage-opt size，Docker 29 快照器不生效已实测）。
//
// 可测试性：quotaOps 是接口——真机走 xfs_quota＋/etc/projects|projid（root/systemd），
// 单测用内存假实现断言"建席设限、毁席清零"的调用序列与参数。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type quotaOps interface {
	// Setup 建席位目录并设项目配额（diskMB=0 表示只建目录不设限——预记账口径）。
	Setup(root, seatID string, diskMB int64) error
	// Clear 清项目配额并删席位目录。
	Clear(root, seatID string) error
}

// ---------- 真机实现 ----------

type xfsQuota struct{}

var seatIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

func validSeatID(id string) bool { return seatIDPattern.MatchString(id) }

func seatDir(root, seatID string) string { return filepath.Join(root, seatID) }

func (xfsQuota) Setup(root, seatID string, diskMB int64) error {
	if !validSeatID(seatID) {
		return fmt.Errorf("seat_id 非法: %q", seatID)
	}
	dir := seatDir(root, seatID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if diskMB <= 0 {
		return nil
	}
	name := "seat-" + seatID
	pid, err := allocProjectID()
	if err != nil {
		return err
	}
	if err := appendLineUnique("/etc/projects", name+":"+dir); err != nil {
		return err
	}
	if err := appendLineUnique("/etc/projid", fmt.Sprintf("%s:%d", name, pid)); err != nil {
		return err
	}
	mnt := mountPointOf(root)
	if out, err := exec.Command("xfs_quota", "-x", "-c", "project -s "+name, mnt).CombinedOutput(); err != nil {
		return fmt.Errorf("xfs project -s: %v: %s", err, out)
	}
	if out, err := exec.Command("xfs_quota", "-x", "-c", fmt.Sprintf("limit -p bhard=%dm %s", diskMB, name), mnt).CombinedOutput(); err != nil {
		return fmt.Errorf("xfs limit: %v: %s", err, out)
	}
	return nil
}

func (xfsQuota) Clear(root, seatID string) error {
	if !validSeatID(seatID) {
		return fmt.Errorf("seat_id 非法: %q", seatID)
	}
	name := "seat-" + seatID
	mnt := mountPointOf(root)
	_ = exec.Command("xfs_quota", "-x", "-c", "limit -p bhard=0 "+name, mnt).Run()
	removeLine("/etc/projects", name+":")
	removeLine("/etc/projid", name+":")
	return os.RemoveAll(seatDir(root, seatID))
}

// mountPointOf 承载探测：生产真 XFS（/data/dingwei-newplat/agents → /data）、
// 实验回环（/data/dingwei-node 自身）。与复验脚本同一规则。
func mountPointOf(root string) string {
	if strings.HasPrefix(root, "/data/dingwei-node") {
		return "/data/dingwei-node"
	}
	return "/data"
}

// allocProjectID 在 60000-69999 段找第一个空闲项目 ID。
func allocProjectID() (int, error) {
	raw, _ := os.ReadFile("/etc/projid")
	used := map[int]bool{}
	for _, ln := range strings.Split(string(raw), "\n") {
		var id int
		if _, err := fmt.Sscanf(ln, "%*[^:]:%d", &id); err == nil {
			used[id] = true
		}
	}
	for id := 60000; id < 70000; id++ {
		if !used[id] {
			return id, nil
		}
	}
	return 0, fmt.Errorf("无空闲项目 ID（60000-69999 段满）")
}

func appendLineUnique(path, line string) error {
	raw, _ := os.ReadFile(path)
	for _, ln := range strings.Split(string(raw), "\n") {
		if ln == line {
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

func removeLine(path, prefix string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var keep []string
	for _, ln := range strings.Split(string(raw), "\n") {
		if ln != "" && !strings.HasPrefix(ln, prefix) {
			keep = append(keep, ln)
		}
	}
	_ = os.WriteFile(path, []byte(strings.Join(keep, "\n")+"\n"), 0o644)
}

// ---------- 单测假实现 ----------

type fakeQuotaCall struct {
	Op, Root, SeatID string
	DiskMB           int64
}

type fakeQuota struct {
	calls   []fakeQuotaCall
	dirs    map[string]bool
	failAt  string // 命中该 Op 即返回错误（反向用例）
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
	delete(f.dirs, seatDir(root, seatID))
	return nil
}

// XFSQuota 真机配额实现（root/systemd；cmd/nodeagent 用）。
func XFSQuota() quotaOps { return xfsQuota{} }
