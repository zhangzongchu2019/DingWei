package pgtest

// 🔴 这一格钉的是【父进程死了、两头管道都破裂之后，工具的收尾还能不能跑完】。
//
// 为什么值得单独立一格：本工具的 stdout 是协议管道、stderr 被助手接到 bytes.Buffer
// （os/exec 对非 *os.File 的 Stderr 也是建管道＋goroutine 转抄）⇒ 测试进程一死，
// 【两头都裂】。而 Go 运行时对 fd 1/2 上的 SIGPIPE 走默认处置：直接打死进程。
// 于是「测试进程被杀」这个正是收尾逻辑存在的理由的场景，反过来会把收尾打死在半路。
//
// 实测（同一个二进制、同一次起库，唯一差别是有没有 ignoreSigPipe）：
//
//	没有：pg.Stop() 把库停了，工具随即死在往破裂管道写的那一步 ⇒ cleanDir 与收尸都没跑到，
//	      两个目录留在 /tmp，postmaster 过继给 PID 1 变【永久僵尸】（本容器 PID 1 是
//	      `sleep infinity`，不收尸 ⇒ 再也清不掉，还会被残留格读成「本次新增」把绿判成红）；
//	有  ：两个目录删净、活 postgres 0、僵尸增量 0。
//
// 这一格是【确定性】的（不靠概率撞上竞态），故可以当判据用；删掉 ignoreSigPipe() 必红。

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCleanupSurvivesBrokenStdoutPipe(t *testing.T) {
	bin, err := toolBinary(t)
	if err != nil {
		if errors.Is(err, errEnvMissing) {
			t.Skipf("环境缺失，跳过：%v", err)
		}
		t.Fatalf("取工具二进制: %v", err)
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Stdout = outW
	cmd.Stderr = errW
	cmd.Stdin = inR
	if err := cmd.Start(); err != nil {
		t.Fatalf("起工具: %v", err)
	}
	// 子进程已继承到 fd ⇒ 父进程这边的副本必须关掉，否则 EOF 永远不来。
	_ = outW.Close()
	_ = errW.Close()
	_ = inR.Close()

	// stderr 必须有人读，否则管道写满会把工具堵死（那是测「堵」，不是测「裂」）。
	errDone := make(chan struct{})
	go func() {
		defer close(errDone)
		sc := bufio.NewScanner(errR)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
		}
	}()

	// 读协议行到 READY，顺手把两个目录路径记下来（收尾之后要核它们真的没了）。
	var runtimeDir, logDir string
	ready := false
	deadline := time.Now().Add(5 * time.Minute) // 首次可能要下载 PG 二进制
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		switch key {
		case "RUNTIME":
			runtimeDir = value
		case "LOGDIR":
			logDir = value
		case "READY":
			ready = value == "1"
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等 READY 超时；已读到 RUNTIME=%q LOGDIR=%q", runtimeDir, logDir)
		}
	}
	if !ready {
		t.Fatalf("没读到 READY=1（RUNTIME=%q LOGDIR=%q）", runtimeDir, logDir)
	}
	if runtimeDir == "" || logDir == "" {
		t.Fatalf("工具没报全两个目录: RUNTIME=%q LOGDIR=%q", runtimeDir, logDir)
	}
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Fatalf("起库后运行目录应当存在: %v", err)
	}

	// 🔴 制造「测试进程被杀」的形状：先掐断两头管道的读端（工具此后的写一律 EPIPE），
	// 再关 stdin（＝助手那头的 StdinPipe 随进程一起没了）触发收尾。
	_ = outR.Close()
	_ = errR.Close()
	<-errDone
	_ = inW.Close()

	waitErr := cmd.Wait() // 收尾期间没人接读数，退出码非 0 是正常的；本格判的是【现场】
	t.Logf("工具退出: err=%v（管道破裂后读数没人接，退出码不作判据）", waitErr)

	// 收尾要在有限时间内把两个目录删净。停库那一步含 shutdown checkpoint，
	// 宿主盘有争抢时实测要 2.5s 以上，故给足 90s 并按 100ms 轮询。
	gone := func(p string) bool { _, err := os.Stat(p); return os.IsNotExist(err) }
	end := time.Now().Add(90 * time.Second)
	for time.Now().Before(end) {
		if gone(runtimeDir) && gone(logDir) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !gone(runtimeDir) || !gone(logDir) {
		t.Errorf("🔴 两头管道破裂后收尾没跑完：现场留着没删 RUNTIME=%s(在=%v) LOGDIR=%s(在=%v)。\n"+
			"     这一格红的典型原因是工具死在 SIGPIPE 上（见 tools/pgtest/sigpipe_unix.go 头注）：\n"+
			"     库可能已停，但 cleanDir 与收尸都没跑到 ⇒ 留残留＋留一个 PID 1 永不收的 postgres 僵尸。\n"+
			"     现场留证不删，查完请自行删除这两个目录。",
			runtimeDir, !gone(runtimeDir), logDir, !gone(logDir))
	}
}
