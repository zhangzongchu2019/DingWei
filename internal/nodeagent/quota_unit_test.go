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
	// r7 删闸：目标 ID 的截断行（只有 #pid 没有用量列）⇒ 解析错误，不得谎报 present=false
	xfsRun = func(args ...string) ([]byte, error) {
		return []byte("Project quota on /data\n#61001\n"), nil
	}
	if _, _, err := projectUsage("/data", 61001); err == nil {
		t.Fatal("目标行字段不足必须报解析错误")
	}
	// r7 删闸：目标行用量非整数 ⇒ 解析错误
	xfsRun = func(args ...string) ([]byte, error) {
		return []byte("#61001    notanumber 0 16384\n"), nil
	}
	if _, _, err := projectUsage("/data", 61001); err == nil {
		t.Fatal("目标行用量非整数必须报解析错误")
	}
	// 其它项目的截断行不影响对目标 ID 的判定（不命中即跳过，不误伤）
	if present, _, err := projectUsage("/data", 61002); err != nil || present {
		t.Fatalf("其它项目截断行不应影响: present=%v err=%v", present, err)
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
	// r7 删闸：清后复查遇到目标 ID 的截断行 ⇒ Clear 必须报错（不得当"已不在报表"放行）
	xfsRun = func(args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "report -p -n") {
			return []byte("#61001\n"), nil // 截断行：只有 ID 没有用量列
		}
		return []byte(""), nil
	}
	if err := (xfsQuota{}).Clear(root, "ag_c1"); err == nil {
		t.Fatal("复查遇截断目标行必须红（残留漏报闸）")
	}
	xfsRun = func(args ...string) ([]byte, error) { return []byte(""), nil }
	if err := (xfsQuota{}).Clear(root, "ag_c1"); err != nil {
		t.Fatalf("摘净后应通: %v", err)
	}
}

// r3：Setup 回滚必须摘映射（project -C -p <dir> <pid>，目录尚在时）再删目录；
// 命令参数完整断言＋阶段覆盖。
func TestSetupRollbackClearsProjectMapping(t *testing.T) {
	origFind, origRun, origLs, origCh := findmntTarget, xfsRun, lsattrOut, chownForSeatFn
	defer func() { findmntTarget, xfsRun, lsattrOut, chownForSeatFn = origFind, origRun, origLs, origCh }()
	root := t.TempDir()
	dir := seatDir(root, "ag_r3")
	findmntTarget = func(string) (string, error) { return "/data", nil }
	lsattrOut = func(d string) (string, error) { return "60000 --------------e-- " + d, nil } // report 空 ⇒ 分配 60000，断言过
	chownForSeatFn = func(string) error { return nil }
	var calls []string
	xfsRun = func(args ...string) ([]byte, error) {
		j := strings.Join(args, " ")
		calls = append(calls, j)
		if strings.Contains(j, "report -p -n") {
			return []byte(""), nil
		}
		if strings.Contains(j, "limit -p bhard=16384m") {
			return nil, errors.New("limit failed") // 🔴 limit 步失败触发回滚（此时 project+limit 都已碰过）
		}
		return []byte(""), nil
	}
	if err := (xfsQuota{}).Setup(root, "ag_r3", 16384); err == nil {
		t.Fatal("limit 失败 Setup 必须报错")
	}
	// 完整命令参数断言：摘映射必须是 project -C -p <dir> 61001 /data（缺 -p 会按 /etc 映射找而失败——r3 探针抓的就是这个）
	wantC := "project -C -p " + dir + " 60000 /data" // report 空 ⇒ 首个空闲 ID 是 60000
	var sawC bool
	for _, c := range calls {
		if strings.Contains(c, wantC) {
			sawC = true
		}
	}
	if !sawC {
		t.Fatalf("回滚必须含 %q, 实得 %v", wantC, calls)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("回滚后目录必须不在: %v", err)
	}
	// 回滚还须含 limit 归零（r5："limit（若已设）归零"——limit 步碰过即算已设，失败也归零兜底）
	wantLimit0 := "limit -p bhard=0 60000 /data"
	var sawLimit0 bool
	for _, c := range calls {
		if strings.Contains(c, wantLimit0) {
			sawLimit0 = true
		}
	}
	if !sawLimit0 {
		t.Fatalf("回滚必须含 limit 归零 %q, 实得 %v", wantLimit0, calls)
	}
	// 阶段覆盖：project -s 成功但 lsattr 失败 ⇒ 同样摘映射＋删目录
	lsattrOut = func(string) (string, error) { return "", errors.New("lsattr failed") }
	calls = nil
	if err := (xfsQuota{}).Setup(root, "ag_r3", 16384); err == nil {
		t.Fatal("lsattr 失败 Setup 必须报错")
	}
	sawC = false
	for _, c := range calls {
		if strings.Contains(c, wantC) {
			sawC = true
		}
	}
	if !sawC {
		t.Fatalf("lsattr 失败的回滚也必须摘映射 %q, 实得 %v", wantC, calls)
	}
}

// r3：Clear 的摘映射也用 project -C -p <dir>（缺 -p 按 /etc 找会失败——探针记录的形状）。
func TestClearUsesProjectCWithPath(t *testing.T) {
	origFind, origRun, origLs := findmntTarget, xfsRun, lsattrOut
	defer func() { findmntTarget, xfsRun, lsattrOut = origFind, origRun, origLs }()
	root := t.TempDir()
	dir := seatDir(root, "ag_c2")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	findmntTarget = func(string) (string, error) { return "/data", nil }
	lsattrOut = func(d string) (string, error) { return "61002 --------------e-- " + d, nil }
	var calls []string
	xfsRun = func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return []byte(""), nil // report 空 ⇒ 项目已不在 ⇒ 绿
	}
	if err := (xfsQuota{}).Clear(root, "ag_c2"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	wantLimit := "limit -p bhard=0 61002 /data"
	wantC := "project -C -p " + dir + " 61002 /data"
	var iLimit, iC = -1, -1
	for i, c := range calls {
		if strings.Contains(c, wantLimit) && iLimit == -1 {
			iLimit = i
		}
		if strings.Contains(c, wantC) && iC == -1 {
			iC = i
		}
	}
	if iLimit == -1 || iC == -1 || iC < iLimit {
		t.Fatalf("顺序与参数必须 limit归零→project -C -p <dir>: %v", calls)
	}
}

// r4 复验：lsattr 返回 err=nil 但输出空/畸形 ⇒ 不得 panic、走同一 rollback
//（摘映射 project -C -p 完整调用、目录消失）。
func TestSetupMalformedLsattrNoPanicFullRollback(t *testing.T) {
	origFind, origRun, origLs, origCh := findmntTarget, xfsRun, lsattrOut, chownForSeatFn
	defer func() { findmntTarget, xfsRun, lsattrOut, chownForSeatFn = origFind, origRun, origLs, origCh }()
	root := t.TempDir()
	dir := seatDir(root, "ag_r4")
	findmntTarget = func(string) (string, error) { return "/data", nil }
	lsattrOut = func(string) (string, error) { return "", nil } // 🔴 err=nil 空输出（探针形状）
	chownForSeatFn = func(string) error { return nil }
	var calls []string
	xfsRun = func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return []byte(""), nil
	}
	// 无 panic 是第一条断言（修复前这里下标越界）
	err := (xfsQuota{}).Setup(root, "ag_r4", 16384)
	if err == nil {
		t.Fatal("畸形 lsattr 输出必须报错")
	}
	if !strings.Contains(err.Error(), "项目号断言失败") {
		t.Fatalf("错误应为项目号断言失败: %v", err)
	}
	// rollback 完整：摘映射带 -p 与目录
	wantC := "project -C -p " + dir + " 60000 /data"
	var sawC bool
	for _, c := range calls {
		if strings.Contains(c, wantC) {
			sawC = true
		}
	}
	if !sawC {
		t.Fatalf("回滚必须含 %q, 实得 %v", wantC, calls)
	}
	if _, serr := os.Stat(dir); !os.IsNotExist(serr) {
		t.Fatalf("回滚后目录必须消失: %v", serr)
	}
	// r5：畸形输出（err=nil 非数字首列）同一形状——无 panic、进同一回滚
	lsattrOut = func(d string) (string, error) { return "garbage-no-number " + d, nil }
	calls = nil
	err = (xfsQuota{}).Setup(root, "ag_r4", 16384)
	if err == nil || !strings.Contains(err.Error(), "项目号断言失败") {
		t.Fatalf("畸形输出必须报项目号断言失败: %v", err)
	}
	sawC = false
	for _, c := range calls {
		if strings.Contains(c, wantC) {
			sawC = true
		}
	}
	if !sawC {
		t.Fatalf("畸形输出的回滚也必须摘映射 %q, 实得 %v", wantC, calls)
	}
	if _, serr := os.Stat(dir); !os.IsNotExist(serr) {
		t.Fatalf("畸形输出回滚后目录必须消失: %v", serr)
	}
}

// r5 修类不修实例：Clear 的 lsattr 解析同型加固——err/空输出/畸形输出一律失败上抛，
// 不得静默按"无项目"跳过的摘映射与复查（否则报表残留无人认领）。目录已不在 ⇒ 幂等绿。
func TestClearMalformedLsattrFailClosed(t *testing.T) {
	origFind, origRun, origLs := findmntTarget, xfsRun, lsattrOut
	defer func() { findmntTarget, xfsRun, lsattrOut = origFind, origRun, origLs }()
	root := t.TempDir()
	dir := seatDir(root, "ag_c3")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	findmntTarget = func(string) (string, error) { return "/data", nil }
	var calls []string
	xfsRun = func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return []byte(""), nil
	}
	// ① err=nil 空输出 ⇒ 拒，且不得动 xfs（没有可摘的映射）也不得删目录
	lsattrOut = func(string) (string, error) { return "", nil }
	if err := (xfsQuota{}).Clear(root, "ag_c3"); err == nil {
		t.Fatal("空输出必须报错")
	}
	if len(calls) != 0 {
		t.Fatalf("空输出时不得发任何 xfs 命令: %v", calls)
	}
	if _, serr := os.Stat(dir); serr != nil {
		t.Fatalf("失败上抛时目录必须还在（留给人查）: %v", serr)
	}
	// ② err=nil 畸形输出 ⇒ 同拒
	lsattrOut = func(d string) (string, error) { return "junk " + d, nil }
	if err := (xfsQuota{}).Clear(root, "ag_c3"); err == nil {
		t.Fatal("畸形输出必须报错")
	}
	if _, serr := os.Stat(dir); serr != nil {
		t.Fatalf("失败上抛时目录必须还在: %v", serr)
	}
	// ③ lsattr 出错但目录还在 ⇒ 拒（不得当无项目删目录）
	lsattrOut = func(string) (string, error) { return "", errors.New("lsattr boom") }
	if err := (xfsQuota{}).Clear(root, "ag_c3"); err == nil {
		t.Fatal("lsattr 出错且目录还在必须报错")
	}
	// ④ lsattr 出错且目录已不在 ⇒ 幂等绿（无配额可清）
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := (xfsQuota{}).Clear(root, "ag_c3"); err != nil {
		t.Fatalf("目录已不在应幂等成功: %v", err)
	}
	// ⑤ r6 正向删闸：项目号明确为 0（可验证的无配额状态）⇒ 删目录、零 xfs 命令、成功
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lsattrOut = func(d string) (string, error) { return "0 --------------e-- " + d, nil }
	calls = nil
	if err := (xfsQuota{}).Clear(root, "ag_c3"); err != nil {
		t.Fatalf("项目号 0（预记账席）应直接删目录成功: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("项目号 0 不得发任何 xfs 命令: %v", calls)
	}
	if _, serr := os.Stat(dir); !os.IsNotExist(serr) {
		t.Fatalf("项目号 0 路径目录必须已删: %v", serr)
	}
}
