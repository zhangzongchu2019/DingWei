package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🔴 本格钉的是【接线】，不是重试逻辑本身（那在 portretry_test.go，纯单元、不起 PG）。
//
// 为什么非要有这一格：startWithPortRetry 把【最终起成功的那个端口】交回来，调用方必须用它
// 拼 DSN 并交给消费方。这一步接错了 —— 重试成功、DSN 却还指着原来那个被抢的号 ——
// 单元格全绿，而后果比不重试更坏：消费方会去连一个【别人的】库（或连不上而报一个
// 看不懂的错），读数上还看不出来。故这一格真起一次库，只认协议行里的事实。
//
// 🔴 判据全部来自助手自己报的协议行，不从测试侧另算：PORT_RETRY（换过端口且出声）、
// PORT 的最后一个值（消费方按"同键后写覆盖"读到的就是它）、DSN 里的端口（必须等于上一个）、
// READY=1（库真的在【新】端口上就绪 —— embeddedpostgres 的 Start 自己连过才算成功）、
// CLEANED=1（重试这条路径上收尾没坏）。
func TestPortRetryEndToEndUsesFinalPortForDSN(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：本格要真起一次内嵌 PG（约 2-3s；冷缓存下首次要下载，故 -start-timeout 给足）")
	}
	bin := buildTool(t)

	// 占住一个端口不放，制造真实的抢占（不是伪造错误文案）。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占端口失败（本格的前提造不出来）: %v", err)
	}
	occupied := ln.Addr().(*net.TCPAddr).Port
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-port", fmt.Sprint(occupied), "-start-timeout", "5m")
	cmd.Stdin = nil // 立即 EOF：助手报完 READY 就走收尾，不留孤儿
	out, err := cmd.Output()
	kv := parseProtocolLines(string(out))
	if err != nil {
		t.Fatalf("助手在端口 %d 被占时退出码非 0（重试没救回来）: %v\n协议行: %v\nstderr: %s",
			occupied, err, kv, stderrOf(cmd))
	}

	retry := kv["PORT_RETRY"]
	if len(retry) != 1 {
		t.Fatalf("PORT_RETRY = %v，要恰好一条（端口被占必须换号重试，且必须【出声】）", retry)
	}
	if want := fmt.Sprint(occupied) + "->"; !strings.HasPrefix(retry[0], want) {
		t.Errorf("PORT_RETRY = %q，要以 %q 开头（报的是【从哪个号】换走的）", retry[0], want)
	}
	ports := kv["PORT"]
	if len(ports) != 2 {
		t.Fatalf("PORT 报了 %v，要两个值（尝试前一个、换号后一个）：消费方按同键后写覆盖读，"+
			"少报一次它就会拿到那个被占的号", ports)
	}
	final := ports[len(ports)-1]
	if final == fmt.Sprint(occupied) {
		t.Fatalf("最终 PORT 还是被占的那个 %s —— 等于没换号", final)
	}
	if !strings.HasSuffix(retry[0], "->"+final) {
		t.Errorf("PORT_RETRY=%q 与最终 PORT=%s 对不上（排障时两条读数必须能互相印证）", retry[0], final)
	}

	dsn := firstOf(kv["DSN"])
	if dsn == "" {
		t.Fatal("没有 DSN 协议行 —— 消费方无从连库")
	}
	if !strings.Contains(dsn, ":"+final+"/") {
		t.Fatalf("DSN=%q 里的端口不是最终起成功的 %s —— 🔴 这就是「重试了但没用上」："+
			"消费方会去连那个被别人占着的号", dsn, final)
	}
	if strings.Contains(dsn, ":"+fmt.Sprint(occupied)+"/") {
		t.Fatalf("DSN=%q 里还留着被占的原端口 %d", dsn, occupied)
	}
	if firstOf(kv["READY"]) != "1" {
		t.Errorf("READY = %v，要 1（库必须真的在【新】端口上就绪，不是「换了个号就报成功」）", kv["READY"])
	}
	if firstOf(kv["CLEANED"]) != "1" {
		t.Errorf("CLEANED = %v，要 1（重试这条路径上收尾也要干净，别留孤儿与残留目录）", kv["CLEANED"])
	}
}

// TestNoConflictNoRetryEndToEnd 是上一格的对照：没人抢端口时【不该】出现 PORT_RETRY。
//
// 🔴 少了它，"无脑重试三次"也能让上一格全绿 —— 那会把每次起库都拖慢，
// 而且 PORT 报两个值会让消费方读到错的号。故两格各判各的。
func TestNoConflictNoRetryEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：本格要真起一次内嵌 PG")
	}
	bin := buildTool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-start-timeout", "5m")
	cmd.Stdin = nil
	out, err := cmd.Output()
	kv := parseProtocolLines(string(out))
	if err != nil {
		t.Fatalf("无冲突时助手退出码非 0: %v\n协议行: %v\nstderr: %s", err, kv, stderrOf(cmd))
	}
	if r := kv["PORT_RETRY"]; len(r) != 0 {
		t.Errorf("没人抢端口却报了 PORT_RETRY=%v —— 重试触发条件过宽", r)
	}
	if p := kv["PORT"]; len(p) != 1 {
		t.Errorf("PORT = %v，要恰好一个值", p)
	}
	if firstOf(kv["READY"]) != "1" || firstOf(kv["CLEANED"]) != "1" {
		t.Errorf("READY=%v CLEANED=%v，都要 1", kv["READY"], kv["CLEANED"])
	}
}

// buildTool 用【当前源码】编出助手。🔴 不用树内预构建产物 tools/pgtest/pgtest：
// 那份可能是旧的，用它跑就等于拿旧二进制证明新代码（消费侧 internal/store/pgtest
// 为同一件事专门做了源文件哈希核对）。
func buildTool(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("没有 go 工具链，编不出助手: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "pgtest-e2e")
	cmd := exec.Command(goBin, "build", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build 助手失败: %v", err)
	}
	return bin
}

// parseProtocolLines 按消费方同一口径解析 KEY=VALUE（同键可多次，故值是切片）。
func parseProtocolLines(out string) map[string][]string {
	kv := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			continue
		}
		kv[k] = append(kv[k], v)
	}
	return kv
}

func firstOf(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

func stderrOf(cmd *exec.Cmd) string {
	if ee, ok := cmd.Err.(*exec.ExitError); ok {
		return string(ee.Stderr)
	}
	return "(无)"
}
