// Package pgtest 给 Hub 存储层提供【临时 PG＋随起随清】的集成用例环境(R0)。
//
// 三级取库顺序,与仓内既有惯例对齐(internal/datad 的 DATAD_TEST_PG_DSN 门控):
//
//	① 环境变量 HUB_TEST_PG_DSN 已设 ⇒ 直接用它,本包不拥有其生命周期(不启不删)。
//	   🔴 该 DSN 必须指【独立测试库】,【永不指生产库】—— 与 datad 同一条纪律。
//	② 否则起 tools/pgtest 子进程(内嵌 PG:免特权、免 docker、免 sudo),用完关掉并删净。
//	③ 两条都走不通(无 go 工具链、模块缓存冷且无网、HUB_TEST_PG_EMBEDDED=0)⇒ t.Skip
//	   并【说明原因】,不静默降级成「假装过了」。
//
// 🔴 本包【不出现】sql.Open:建库句柄只许在 internal/repo
// (scripts/repo-boundary-gate.sh 判据一「仓储包外 sql.Open 恒零」),故连接一律走 repo.OpenPG。
// 🔴 本包【不给主 go.mod 添依赖】:内嵌 PG 的第三方依赖关在 tools/pgtest 那个独立 module 里,
// 主模块直接依赖数不变(dependency-ratchet.sh 基线 direct_count=6 不触闸)。
package pgtest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// DSNEnv 是「外部已备好 PG」时的连接串环境变量名。
const DSNEnv = "HUB_TEST_PG_DSN"

// EmbeddedOffEnv 控制「本包能否自行起内嵌 PG」。
//
// 🔴 语义按【字面】读,不要反着来:值是 0/false/off/no/否 ⇒ 【禁用】内嵌 PG,
// 此时没有 HUB_TEST_PG_DSN 就 Skip。未设或其它值 ⇒ 允许(默认允许,便于本地直接跑)。
// 本包首版把这条写反了(=0 反而允许),是跑 Skip 路径时当场露出来的,已修并补了用例。
const EmbeddedOffEnv = "HUB_TEST_PG_EMBEDDED"

// 起库与收尾的时间上限。首次要下载 PG 二进制,故给足;之后走缓存,实测 4 秒级。
const (
	startTimeout = 6 * time.Minute
	stopTimeout  = 90 * time.Second
)

// Pool 返回一个连到临时 PG 的 repo.Pool,并把「关池＋停库＋删净」全部登记进 t.Cleanup。
//
// 拿不到库时 t.Skip(不 Fail):这条用例属【集成】层,核心逻辑仍由 SQLite/Mem 同套用例覆盖,
// 与 internal/datad 的门控理由一致。
func Pool(t *testing.T) *repo.Pool {
	t.Helper()
	dsn, runtimeDir, stop := acquire(t)
	// 🔴 t.Cleanup 是 LIFO:【后登记者先跑】。要求的执行顺序是
	//   关池 → 停库 → 判运行目录已删,
	// 故登记顺序必须反过来:先登断言,再登停库,最后登关池。
	// 登记顺序写反的后果很隐蔽:断言会读到【还没删】的目录,于是「随清」恒判失败;
	// 或更糟 —— 停库抢在关池之前,关池对着一个已经没了的服务报错,真问题被噪音盖住。
	assertCleanup(t, runtimeDir)
	if stop != nil {
		t.Cleanup(func() { stop(t) })
	}
	pool, err := repo.OpenPG(context.Background(), dsn)
	if err != nil {
		if stop != nil {
			stop(t) // 幂等:与上面登记的收尾只会真跑一次
		}
		t.Fatalf("repo.OpenPG 连不上临时 PG(%s 未设,走内嵌 PG): %v", DSNEnv, err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("关池失败: %v", err)
		}
	})
	return pool
}

// DSN 只取连接串,不要池(用于要自己控制连接形态的场景,例如三池分权)。
// 生命周期同样登记进 t.Cleanup。
func DSN(t *testing.T) string {
	t.Helper()
	dsn, runtimeDir, stop := acquire(t)
	// 同 Pool:LIFO ⇒ 先登断言(最后跑),再登停库(先跑)。
	assertCleanup(t, runtimeDir)
	if stop != nil {
		t.Cleanup(func() { stop(t) })
	}
	return dsn
}

// acquire 返回 (dsn, 运行目录, 收尾函数)。
//
// 收尾函数为 nil ⇒ 库不是本包起的(HUB_TEST_PG_DSN 由外部给),运行目录也为空,
// 此时【不】去判「有没有删净」—— 不是本包的东西,不该由本包断言。
func acquire(t *testing.T) (string, string, func(*testing.T)) {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv(DSNEnv)); dsn != "" {
		return dsn, "", nil
	}
	if off, why := embeddedDisabled(); off {
		t.Skipf("%s 未设,且 %s ⇒ 不自行起内嵌 PG", DSNEnv, why)
	}
	bin, err := toolBinary(t)
	if err != nil {
		t.Skipf("%s 未设,且内嵌 PG 工具不可用(%v):跳过 PG 集成用例", DSNEnv, err)
	}
	return start(t, bin)
}

// embeddedDisabled 判「是否禁止本包自行起内嵌 PG」,并给出可直接进 Skip 文案的理由。
//
// 抽成纯函数是为了可单测:这条判据控制的是【会不会 Skip】,写反了不会有任何用例变红 ——
// 只会让「按令不许起库」的环境里悄悄起了一个库,或反之让能跑的环境全跳掉。
func embeddedDisabled() (bool, string) {
	raw := strings.TrimSpace(os.Getenv(EmbeddedOffEnv))
	switch strings.ToLower(raw) {
	case "", "1", "true", "on", "yes":
		return false, ""
	default:
		return true, fmt.Sprintf("%s=%s(按字面读作禁用)", EmbeddedOffEnv, raw)
	}
}

// start 起 tools/pgtest 子进程,读它的协议行,并返回幂等的收尾函数。
func start(t *testing.T, bin string) (string, string, func(*testing.T)) {
	t.Helper()
	cmd := exec.Command(bin)
	// 🔴 给子进程一个真管道作 stdin:关掉它＝告诉子进程「父进程没了,该收尾了」。
	// 子进程把 stdin 关闭也当退出条件,故跑测试的进程被 kill 时不会留下孤儿 PG。
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Skipf("建 stdin 管道失败: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Skipf("建 stdout 管道失败: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Skipf("起 %s 失败: %v", bin, err)
	}

	proto := newProtocol(stdout)
	if err := proto.await(startTimeout); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Skipf("内嵌 PG 未在 %s 内就绪: %v\n子进程 stderr 末尾:\n%s",
			startTimeout, err, tail(stderr.String(), 2000))
	}
	dsn := proto.get("DSN")
	if dsn == "" {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Skipf("内嵌 PG 协议行里没有 DSN(已读到 %v)", proto.snapshot())
	}

	var once sync.Once
	stop := func(tt *testing.T) {
		once.Do(func() {
			_ = stdin.Close()
			// 🔴 顺序必须是【先读到 EOF,再 cmd.Wait()】。
			// cmd.Wait() 会关闭 stdout 管道;反过来的话,收尾那两行(STOPPED/CLEANED)
			// 会被截断成读不到,于是「库自己说清干净了」这条自证退化成空字符串 ——
			// 判据不起作用,而用例看上去照样在跑。本包首版就是这么错的,已修。
			if !proto.waitEOF(stopTimeout) {
				_ = cmd.Process.Kill()
				proto.waitEOF(5 * time.Second)
				tt.Errorf("内嵌 PG 在 %s 内没停下也没关 stdout,已 kill(「随清」不成立)", stopTimeout)
			}
			if err := cmd.Wait(); err != nil {
				tt.Errorf("内嵌 PG 收尾非零: %v\nstderr 末尾:\n%s", err, tail(stderr.String(), 2000))
			}
			if got := proto.get("STOPPED"); got != "1" {
				tt.Errorf("内嵌 PG 自报 STOPPED=%q,不是 1(库可能没真停下)", got)
			}
			if got := proto.get("CLEANED"); got != "1" {
				tt.Errorf("内嵌 PG 自报 CLEANED=%q,不是 1:运行目录 %s 可能没删净",
					got, proto.get("RUNTIME"))
			}
		})
	}
	return dsn, proto.get("RUNTIME"), stop
}

// protocol 流式解析子进程 stdout 的 KEY=VALUE 协议行。
//
// 🔴 为什么是流式而不是「读到 READY 就返回」:收尾值(STOPPED/CLEANED)在 READY 之后才打印。
// 读到位就收工的话,那两行永远读不到,自证值恒为空 —— 而空值又不像失败,极易被忽略。
// 非协议行一律忽略:PG 与 pg_ctl 的日志已由子进程导到 stderr,但这里不假定 stdout 一定干净。
type protocol struct {
	mu    sync.Mutex
	kv    map[string]string
	ready chan error
	done  chan struct{}
}

func newProtocol(r io.Reader) *protocol {
	p := &protocol{
		kv:    map[string]string{},
		ready: make(chan error, 1),
		done:  make(chan struct{}),
	}
	go p.run(r)
	return p
}

func (p *protocol) run(r io.Reader) {
	defer close(p.done)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	announced := false
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			continue
		}
		p.mu.Lock()
		p.kv[key] = value
		p.mu.Unlock()
		if key == "READY" && value == "1" && !announced {
			announced = true
			p.ready <- nil
		}
	}
	if announced {
		return
	}
	if err := sc.Err(); err != nil {
		p.ready <- fmt.Errorf("读协议行: %w", err)
		return
	}
	p.ready <- errors.New("stdout 在 READY=1 之前就关了")
}

// await 等 READY=1;超时或解析失败都返回错误(不返回半成品 kv,免得调用方拿空值当成功)。
func (p *protocol) await(timeout time.Duration) error {
	select {
	case err := <-p.ready:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("等 READY=1 超时(%s)", timeout)
	}
}

func (p *protocol) get(key string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kv[key]
}

func (p *protocol) snapshot() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]string, len(p.kv))
	for k, v := range p.kv {
		out[k] = v
	}
	return out
}

// waitEOF 等 stdout 读到 EOF。报告为 false 表示超时(调用方应据此 kill)。
func (p *protocol) waitEOF(timeout time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// readProtocol 是给单元用例用的薄封装:等到 READY=1 即返回当时的快照。
//
// ⚠️ 只用于判「解析与超时」这两条判据。要拿收尾值请用 newProtocol＋waitEOF,
// 本封装在 READY 之后不再等,读不到 STOPPED/CLEANED。
func readProtocol(r io.Reader, timeout time.Duration) (map[string]string, error) {
	p := newProtocol(r)
	if err := p.await(timeout); err != nil {
		return nil, err
	}
	return p.snapshot(), nil
}

// toolBinary 定位或构建 tools/pgtest 的可执行文件。
//
// 顺序:PGTEST_BIN(显式指定) → tools/pgtest/pgtest(已构建) → go build 到用户缓存目录。
// 构建产物按源码新旧判定是否要重建,故同一份源码只会构建一次。
func toolBinary(t *testing.T) (string, error) {
	t.Helper()
	if p := strings.TrimSpace(os.Getenv("PGTEST_BIN")); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("PGTEST_BIN=%s 不可用: %w", p, err)
		}
		return p, nil
	}
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	toolDir := filepath.Join(root, "tools", "pgtest")
	sources := []string{filepath.Join(toolDir, "main.go"), filepath.Join(toolDir, "go.mod")}
	if prebuilt := filepath.Join(toolDir, "pgtest"); usable(prebuilt, sources) {
		return prebuilt, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil || cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "dingwei-pgtest-cache")
	}
	cacheDir = filepath.Join(cacheDir, "dingwei-pgtest")
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return "", fmt.Errorf("建构建缓存目录: %w", err)
	}
	bin := filepath.Join(cacheDir, "pgtest")
	if usable(bin, sources) {
		return bin, nil
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("找不到 go 工具链,无法构建内嵌 PG 工具: %w", err)
	}
	build := exec.Command(goBin, "build", "-o", bin, ".")
	build.Dir = toolDir
	var out strings.Builder
	build.Stdout = &out
	build.Stderr = &out
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("go build ./tools/pgtest 失败: %w\n%s", err, tail(out.String(), 2000))
	}
	return bin, nil
}

// usable 判可执行文件是否已存在且不比任一源文件旧。
func usable(bin string, sources []string) bool {
	bi, err := os.Stat(bin)
	if err != nil || bi.IsDir() {
		return false
	}
	for _, s := range sources {
		si, err := os.Stat(s)
		if err != nil {
			return false // 源文件都找不到 ⇒ 宁可重建,不拿来历不明的二进制跑
		}
		if si.ModTime().After(bi.ModTime()) {
			return false
		}
	}
	return true
}

// repoRoot 从当前工作目录向上找 tools/pgtest/go.mod,以此定位仓根。
//
// 不用 runtime.Caller:测试的 cwd 就是包目录,向上找更直白,也不受编译期路径影响。
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("取 cwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "tools", "pgtest", "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("向上找不到 tools/pgtest/go.mod,定位仓根失败")
		}
		dir = parent
	}
}

// assertCleanup 在测试结束时判「运行目录真的没了」。
//
// 🔴 只在本包自己起了库时判(runtimeDir 非空)。库是外部经 HUB_TEST_PG_DSN 给的,
// 它的目录不归本包管,也就不该由本包断言。
// 🔴 子进程自报 CLEANED=1 只是【它的说法】;这里再独立 stat 一次 ——
// 同一件事不许两处都只自证,自报与实测不符就报错。
// 🔴 必须在【停库之前】登记:t.Cleanup 是 LIFO,先登记者后跑,
// 这样 stat 发生在运行目录本该已被删之后。顺序见 Pool 的注释。
func assertCleanup(t *testing.T, runtimeDir string) {
	t.Helper()
	if runtimeDir == "" {
		return
	}
	t.Cleanup(func() {
		if _, err := os.Stat(runtimeDir); err == nil {
			t.Errorf("临时 PG 运行目录仍在,「随清」不成立: %s", runtimeDir)
		}
	})
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
