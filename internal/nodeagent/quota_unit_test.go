package nodeagent

import (
	"errors"
	"os"
	"fmt"
	"strings"
	"testing"
)

// M1 返修 阻断3：项目 ID 从 report 权威读数解析（不经 /etc/projid）＋冲突拒。
func TestAllocProjectIDParsesReportAndRejectsCollisions(t *testing.T) {
	orig := xfsRun
	defer func() { xfsRun = orig }()
	xfsRun = func(args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "report -p -n") {
			return []byte("Project quota on /data (/dev/vdb)\n#0        100   0   0\n#60000    50    0   16384\n#60002    10    0   16384\n"), nil
		}
		return nil, errors.New("unexpected: " + joined)
	}
	id, err := allocProjectID("/data")
	if err != nil || id != 60001 { // 60000 占用、60002 占用 ⇒ 首个空闲是 60001
		t.Fatalf("应分配 60001, 得 %d err=%v", id, err)
	}
	// 反向：段满 ⇒ 必拒（不覆盖既有项目）
	xfsRun = func(args ...string) ([]byte, error) {
		var b strings.Builder
		for i := 60000; i < 70000; i++ {
			fmt.Fprintf(&b, "#%d 0 0 16384\n", i)
		}
		return []byte(b.String()), nil
	}
	if _, err := allocProjectID("/data"); err == nil {
		t.Fatal("段满必须拒（冲突不覆盖）")
	}
}

// 阻断4：Clear 各步失败上抛＋清后复查残留即红（lsattr/xfsRun 注入伪造项目号与残留）。
func TestClearPropagatesAndVerifies(t *testing.T) {
	origFind, origRun, origLs := findmntTarget, xfsRun, lsattrOut
	defer func() { findmntTarget, xfsRun, lsattrOut = origFind, origRun, origLs }()
	root := t.TempDir()
	dir := seatDir(root, "ag_x1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	findmntTarget = func(path string) (string, error) { return "/data", nil }
	lsattrOut = func(d string) (string, error) { return "61001 --------------e-- " + d, nil } // 带项目号
	// ① limit 失败 ⇒ 上抛
	xfsRun = func(args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "limit -p bhard=0") {
			return nil, errors.New("xfs_quota limit failed")
		}
		return []byte(""), nil
	}
	if err := (xfsQuota{}).Clear(root, "ag_x1"); err == nil || !strings.Contains(err.Error(), "bhard") {
		t.Fatalf("limit 失败必须上抛, 得 %v", err)
	}
	// ② 清后复查仍有用量 ⇒ D5 红
	xfsRun = func(args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "report -p -n") {
			return []byte("#61001    16384 0 16384\n"), nil
		}
		return []byte(""), nil
	}
	if err := (xfsQuota{}).Clear(root, "ag_x1"); err == nil || !strings.Contains(err.Error(), "D5") {
		t.Fatalf("残留用量必须报 D5 未闭合, 得 %v", err)
	}
	// ③ 干净路径：limit 成＋report 无该项目 ⇒ 通
	xfsRun = func(args ...string) ([]byte, error) { return []byte(""), nil }
	if err := (xfsQuota{}).Clear(root, "ag_x1"); err != nil {
		t.Fatalf("干净清除应通: %v", err)
	}
}

// 阻断4 反向：复查报"项目仍有用量" ⇒ 必红（projectUsage 解析与判红直测）。
func TestProjectUsageReportParsing(t *testing.T) {
	orig := xfsRun
	defer func() { xfsRun = orig }()
	xfsRun = func(args ...string) ([]byte, error) {
		return []byte("Project quota on /data\n#0        100   0   0\n#61001    16384 0 16384\n"), nil
	}
	present, used, err := projectUsage("/data", 61001)
	if err != nil || !present || used != 16384 {
		t.Fatalf("解析: present=%v used=%d err=%v", present, used, err)
	}
	present, _, err = projectUsage("/data", 61002)
	if err != nil || present {
		t.Fatalf("不存在的项目应 present=false: %v %v", present, err)
	}
}

// 复验 r2 阻断①：Setup 中途失败必须自拆（目录不留、项目不残留）。
func TestSetupFailureLeavesNoResidue(t *testing.T) {
	origFind, origRun, origLs, origCh := findmntTarget, xfsRun, lsattrOut, chownForSeatFn
	defer func() { findmntTarget, xfsRun, lsattrOut, chownForSeatFn = origFind, origRun, origLs, origCh }()
	root := t.TempDir()
	dir := seatDir(root, "ag_s1")
	findmntTarget = func(path string) (string, error) { return "/data", nil }
	lsattrOut = func(d string) (string, error) { return "61001 --------------e-- " + d, nil }
	chownForSeatFn = func(string) error { return nil }
	xfsRun = func(args ...string) ([]byte, error) {
		j := strings.Join(args, " ")
		if strings.Contains(j, "limit -p") {
			return nil, errors.New("limit failed")
		}
		return []byte(""), nil
	}
	if err := (xfsQuota{}).Setup(root, "ag_s1", 16384); err == nil {
		t.Fatal("limit 失败 Setup 必须报错")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("Setup 失败目录必须自拆（无残留）: %v", err)
	}
}

// 复验 r2 阻断②：Clear 清后项目【仍在报表】（哪怕用量 0）⇒ 必红；摘净 ⇒ 绿。
func TestClearRedWhenProjectStillPresent(t *testing.T) {
	origFind, origRun, origLs := findmntTarget, xfsRun, lsattrOut
	defer func() { findmntTarget, xfsRun, lsattrOut = origFind, origRun, origLs }()
	root := t.TempDir()
	dir := seatDir(root, "ag_c1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	findmntTarget = func(string) (string, error) { return "/data", nil }
	lsattrOut = func(d string) (string, error) { return "61001 --------------e-- " + d, nil }
	xfsRun = func(args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "report -p -n") {
			return []byte("#61001    0 0 16384\n"), nil // present, used=0
		}
		return []byte(""), nil
	}
	if err := (xfsQuota{}).Clear(root, "ag_c1"); err == nil || !strings.Contains(err.Error(), "D5") {
		t.Fatalf("项目仍在报表（用量0）必须红, 得 %v", err)
	}
	xfsRun = func(args ...string) ([]byte, error) { return []byte(""), nil }
	if err := (xfsQuota{}).Clear(root, "ag_c1"); err != nil {
		t.Fatalf("摘净后应通: %v", err)
	}
}
