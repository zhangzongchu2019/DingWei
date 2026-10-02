package main

// 🔴 这一格钉的是「本工具在【任何平台】都编得出来」。
//
// 为什么它是判据而不是洁癖：reap_other.go 的头注写得明白 —— 拆成带构建标签的两个文件，
// 就是为了 `go build` 在任何平台都过；**工具编不出来，整套 PG 用例就全跳了**（助手那边
// 编不出来算环境缺失，严格模式下 Skip）。也就是说这类断裂不会红在任何一处产品用例上，
// 只会让全组的 PG 用例静默跳过 —— 那比红更糟。
//
// 本件自查就撞到一个实例：reap_other.go 的 waitGonePID 用了 time.Duration 却没导入 "time"
// ⇒ GOOS=windows/darwin 一律编不过，而 linux 上什么都看不出来（仓里也没有跨平台编译的闸）。
// 已补导入，并加本格钉住。

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestToolCrossCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过跨平台编译（冷缓存下要为目标平台各编一遍标准库，约几十秒；此后走 GOCACHE）")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("没有 go 工具链，判不了跨平台编译：%v", err)
	}
	for _, goos := range []string{"windows", "darwin", "linux"} {
		out := filepath.Join(t.TempDir(), "pgtest-"+goos)
		cmd := exec.Command(goBin, "build", "-o", out, ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos)
		cmd.Dir = "."
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("🔴 GOOS=%s 编不过 —— 本工具编不出来，整套 PG 用例会静默跳过（见本文件头注与 reap_other.go）：%v\n%s",
				goos, err, o)
			continue
		}
		if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
			t.Errorf("GOOS=%s 报了成功却没有产物：%v", goos, err)
		}
	}
}
