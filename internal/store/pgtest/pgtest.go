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
// 🔴 Skip 只许用于【环境本就缺失】。「环境在却起不来」(工具编得出而库起不来、协议行缺 DSN、
// 起库超时)是真故障,**默认判 Fail**。首版默认是 Skip,复验 🟡② 指出那会把真故障盖成绿:
// 挂在 Pool 上的其它域用例(G3 身份存储的 PG 契约、G4 hosted)会静默全跳,而整包仍然报 ok。
// ⇒ 默认已翻转(组长裁定 ruling-2026-10-01T130835Z,取复验 bd6942a3 的甲案)。
// 🔴 要允许 Skip 只有一条路:显式置 HUB_TEST_PG_SKIP_OK=1(见 SkipOKEnv)。
// 🔴「随起随清」不采信子进程自报:见 residue.go,起库前后各拍一次快照,只报【新增】残留
// (系统临时目录里的 PG 日志文件、进程表里的 postgres 僵尸)。
//
// 🔴 本包【不出现】sql.Open:建库句柄只许在 internal/repo
// (scripts/repo-boundary-gate.sh 判据一「仓储包外 sql.Open 恒零」),故连接一律走 repo.OpenPG。
// 🔴 本包【不给主 go.mod 添依赖】:内嵌 PG 的第三方依赖关在 tools/pgtest 那个独立 module 里,
// 主模块直接依赖数不变(dependency-ratchet.sh 基线 direct_count=6 不触闸)。
package pgtest

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// SkipOKEnv 是「允许把【环境在却起不来】降级成 Skip」的**唯一**开关。
//
// 🔴 未设 = 不允许 = 判 Fail(默认严)。理由见包首注与 SkipOKEnv 的裁定号:
// 默认宽会让起库失败静默变成 Skip,而 Skip 在日志里看上去像通过。
// 🔴 与 EmbeddedOffEnv 一样按【字面】读,认不出的值【报错】而不是当成「不允许」:
// 一个打错的开关静默生效,正是本开关要防的那类事(判据死了但看着还绿)。
const SkipOKEnv = "HUB_TEST_PG_SKIP_OK"

// StrictEnv 是默认翻转**之前**的旧开关(那时要显式置位才判 Fail)。翻转后它不再表达任何新语义,
// 但 🔴 仍然被读取,且只接受两种处置:
//
//	· 置「开」的值(1/true/on/yes/是)⇒ 与默认一致(判 Fail),原样认下 ——
//	  过渡期各组被要求「PG 读数一律带 HUB_TEST_PG_STRICT=1」(裁定 ruling-2026-10-01T130835Z 末行),
//	  翻转合入后那些读数不该反过来报错;
//	· 置「关」的值(0/false/off/no/否)⇒ **报错**并指名改用 SkipOKEnv。
//	  理由:旧语义里「关」＝允许 Skip。若翻转后静默忽略它,一处 CI 上遗留的 STRICT=0 就会
//	  让人以为「这里允许跳过」而实际判据已翻 —— 那是开关读错里最坏的一种(看着绿、判据不明)。
//
// 认不出的值同样报错。旧读数全部改口后本常量可删(删时连它的用例一起删)。
const StrictEnv = "HUB_TEST_PG_STRICT"

// errEnvMissing 标记「环境本就缺失」这一类失败(没有 go 工具链、按令禁用内嵌 PG)。
// 🔴 只有这一类仍然 Skip(与 SkipOKEnv 无关);其余一律算「环境在却起不来」。
var errEnvMissing = errors.New("PG 测试环境缺失")

// 起库与收尾的时间上限。首次要下载 PG 二进制,故给足;之后走缓存,实测 4 秒级。
const (
	startTimeout = 6 * time.Minute
	stopTimeout  = 90 * time.Second
)

// Pool 返回一个连到临时 PG 的 repo.Pool,并把「关池＋停库＋删净」全部登记进 t.Cleanup。
//
// 拿不到库时怎么办,分两种(判据见 unavailable):环境本就缺失 ⇒ t.Skip(这条用例属【集成】层,
// 核心逻辑仍由 SQLite/Mem 同套用例覆盖,与 internal/datad 的门控理由一致);
// 环境在却起不来 ⇒ 严格模式下 t.Fatalf,不许盖成绿。
func Pool(t *testing.T) *repo.Pool {
	t.Helper()
	ac := acquire(t)
	// 🔴 t.Cleanup 是 LIFO:【后登记者先跑】。要求的执行顺序是
	//   关池 → 停库 → 判两个目录已删＋核残留增量,
	// 故登记顺序必须反过来:先登断言,再登停库,最后登关池。
	// 登记顺序写反的后果很隐蔽:断言会读到【还没删】的目录,于是「随清」恒判失败;
	// 或更糟 —— 停库抢在关池之前,关池对着一个已经没了的服务报错,真问题被噪音盖住。
	assertCleanup(t, ac)
	if ac.stop != nil {
		t.Cleanup(func() { ac.stop(t) })
	}
	pool, err := repo.OpenPG(context.Background(), ac.dsn)
	if err != nil {
		if ac.stop != nil {
			ac.stop(t) // 幂等:与上面登记的收尾只会真跑一次
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
	ac := acquire(t)
	// 同 Pool:LIFO ⇒ 先登断言(最后跑),再登停库(先跑)。
	assertCleanup(t, ac)
	if ac.stop != nil {
		t.Cleanup(func() { ac.stop(t) })
	}
	return ac.dsn
}

// acquired 是「这次拿到的库」的全部现场:连接串、要核对的两个目录、残留快照、收尾函数。
//
// 🔴 以前这里是 (string, string, func) 三元组;修日志泄漏要再加一个目录时,
// 两个相邻 string 参数按位置传错编译器也不报错 —— 断言会去 stat 错的目录,
// 而「stat 不到」看上去恰恰像通过。故收成一个具名结构,让每个值都带名字。
type acquired struct {
	dsn        string
	runtimeDir string // 工具报的 RUNTIME:PG 数据目录在其下
	logDir     string // 工具报的 LOGDIR:TMPDIR 被指到这儿,PG 日志落它里面
	residue    *residueWatch
	stop       func(*testing.T)
}

// owned 判库是不是本包起的。外部经 HUB_TEST_PG_DSN 给的库,它的目录与残留都不归本包管,
// 此时【不】去判「有没有删净」—— 不是本包的东西,不该由本包断言。
func (a acquired) owned() bool { return a.stop != nil }

// acquire 取库(三级顺序见文件头注)。拿不到时它不会返回:unavailable 会 Skip 或 Fatalf。
func acquire(t *testing.T) acquired {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv(DSNEnv)); dsn != "" {
		return acquired{dsn: dsn}
	}
	if off, why := embeddedDisabled(); off {
		unavailable(t, envMissing, "%s 未设,且 %s ⇒ 不自行起内嵌 PG", DSNEnv, why)
	}
	bin, err := toolBinary(t)
	if err != nil {
		// 「没有 go 工具链」是环境缺失(仍 Skip);「工具编不出来」是环境在却坏了(严格模式 Fail)。
		kind := envBroken
		if errors.Is(err, errEnvMissing) {
			kind = envMissing
		}
		unavailable(t, kind, "%s 未设,且内嵌 PG 工具不可用(%v):跳过 PG 集成用例", DSNEnv, err)
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

// unavailKind 区分「拿不到库」的两类原因。这一区分决定了是 Skip 还是 Fail,
// 分错就等于把真故障盖成绿(envBroken 报成 envMissing),或把「按令不许起库」判成故障。
type unavailKind int

const (
	envMissing unavailKind = iota // 环境本就缺失 ⇒ 一律 Skip
	envBroken                     // 环境在却起不来 ⇒ 默认 Fail;只有显式 SkipOKEnv 才 Skip
)

// skipAllowed 判「环境在却起不来」这一类失败**能否**降级成 Skip。认不出的值返回错误
// (理由见 SkipOKEnv 与 StrictEnv 的注释)。
//
// 🔴 抽成纯函数是为了可单测:这条判据控制的是【会不会把真故障盖成绿】,写反了不会有任何用例变红 ——
// 失效之后的表现恰恰是【全绿】(与 embeddedDisabled 同一条理由)。
//
// 🔴 返回 true 只有【一个】来源:SkipOKEnv 显式置了「开」的值。StrictEnv 的任何取值都不会让本函数
// 返回 true —— 旧开关的「关」不再等于「允许 Skip」,而是报错(见 StrictEnv 注释)。
func skipAllowed() (bool, error) {
	switch raw := strings.ToLower(strings.TrimSpace(os.Getenv(StrictEnv))); raw {
	case "":
	case "1", "true", "on", "yes", "是":
		// 旧开关的「开」＝新默认(判 Fail):认下,不改变判定。
	case "0", "false", "off", "no", "否":
		return false, fmt.Errorf(
			"%s=%s 已不再表示「允许 Skip」:判据默认已翻转为 Fail(组长裁定 ruling-2026-10-01T130835Z)。"+
				"要允许 Skip 请显式置 %s=1;不需要就把 %s 从环境里去掉(旧读数的 =1 仍可原样带着,与本默认一致)",
			StrictEnv, raw, SkipOKEnv, StrictEnv)
	default:
		return false, fmt.Errorf(
			"%s=%q 认不出:本开关已随默认翻转废弃(只认「开」值,效果与默认相同);允许 Skip 请用 %s=1",
			StrictEnv, raw, SkipOKEnv)
	}

	switch raw := strings.ToLower(strings.TrimSpace(os.Getenv(SkipOKEnv))); raw {
	case "":
		return false, nil
	case "1", "true", "on", "yes", "是":
		return true, nil
	case "0", "false", "off", "no", "否":
		return false, nil
	default:
		return false, fmt.Errorf(
			"%s=%q 认不出:只接受 1/true/on/yes/是(允许 Skip)与 0/false/off/no/否(不允许,即默认)",
			SkipOKEnv, raw)
	}
}

// unavailable 是「拿不到库」的唯一出口:按原因分类,决定 Skip 还是 Fail。
//
// 🔴 它不会返回(Skip/Fatal 都走 runtime.Goexit),故调用方之后不必再写 return。
// 收成一个出口是为了让分类漏不掉:散在各处的 t.Skipf 每加一处就是一次「悄悄降级」的机会,
// 而复验 🟡② 撞上的正是这个 —— 起库失败被 Skip 盖住,整包仍然 ok。
func unavailable(t *testing.T, kind unavailKind, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	allowSkip, err := skipAllowed()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if kind == envBroken {
		if allowSkip {
			t.Skipf("%s\n(提示:这是「环境在却起不来」,本属真故障;因 %s 已显式置位才降级成 Skip)", msg, SkipOKEnv)
		}
		t.Fatalf("🔴 环境在却起不来,默认判 Fail,不许盖成 Skip(确需跳过请显式置 %s=1)。%s", SkipOKEnv, msg)
	}
	t.Skipf("%s", msg)
}

// start 起 tools/pgtest 子进程,读它的协议行,并返回幂等的收尾函数。
func start(t *testing.T, bin string) acquired {
	t.Helper()
	// 🔴 残留快照必须在起库【之前】拍:之后拍的就没有基准可比了。
	residue := newResidueWatch()
	cmd := exec.Command(bin)
	// 🔴 给子进程一个真管道作 stdin:关掉它＝告诉子进程「父进程没了,该收尾了」。
	// 子进程把 stdin 关闭也当退出条件,故跑测试的进程被 kill 时不会留下孤儿 PG。
	stdin, err := cmd.StdinPipe()
	if err != nil {
		unavailable(t, envBroken, "建 stdin 管道失败: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		unavailable(t, envBroken, "建 stdout 管道失败: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		unavailable(t, envBroken, "起 %s 失败: %v", bin, err)
	}

	proto := newProtocol(stdout)
	if err := proto.await(startTimeout); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		unavailable(t, envBroken, "内嵌 PG 未在 %s 内就绪: %v\n子进程 stderr 末尾:\n%s%s",
			startTimeout, err, tail(stderr.String(), 2000), leftoverNote(proto))
	}
	dsn := proto.get("DSN")
	if dsn == "" {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		unavailable(t, envBroken, "内嵌 PG 协议行里没有 DSN(已读到 %v)%s", proto.snapshot(), leftoverNote(proto))
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
			// 工具自报「PG 日志确实落进了它自己的日志目录」。这一格失效意味着
			// 系统临时目录又在漏文件,而 residue 那道独立核对会同时报 —— 两个信号,不互相替代。
			if got := proto.get("PGLOG_REDIRECTED"); got == "0" {
				tt.Errorf("内嵌 PG 自报 PGLOG_REDIRECTED=0:TMPDIR 重定向没生效,PG 日志漏在系统临时目录")
			}
		})
	}
	runtimeDir, logDir := proto.get("RUNTIME"), proto.get("LOGDIR")
	if runtimeDir == "" || logDir == "" {
		// 走到这里说明工具不是本仓 tools/pgtest 编出来的(例如 PGTEST_BIN 指了个旧产物)。
		// 判 Fail 而不是悄悄放过:少一个键,「随清」就有一半无从核对。
		t.Errorf("内嵌 PG 协议行缺键(RUNTIME=%q LOGDIR=%q):工具须是【本仓当前源码】编出的 tools/pgtest",
			runtimeDir, logDir)
	}
	return acquired{dsn: dsn, runtimeDir: runtimeDir, logDir: logDir, residue: residue, stop: stop}
}

// leftoverNote 给失败文案补一句「哪两个目录留着没删」。
//
// 🔴 起库失败时【故意不删】运行目录与日志目录:里面的 PG 日志是失败原因的唯一现场,
// 删了就查不了(工具被 kill 时也来不及删)。代价是失败会留残留,故必须把路径打进文案,
// 让人知道去哪儿看、看完自己删 —— 而不是留一堆没人知道来历的目录。
func leftoverNote(p *protocol) string {
	runtimeDir, logDir := p.get("RUNTIME"), p.get("LOGDIR")
	if runtimeDir == "" && logDir == "" {
		return ""
	}
	return fmt.Sprintf("\n起库失败,现场【留证不删】: RUNTIME=%s LOGDIR=%s(查完请自行删除)", runtimeDir, logDir)
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
// 顺序:PGTEST_BIN(显式指定) → tools/pgtest/pgtest(树内预构建) → go build 到用户缓存目录。
//
// 🔴 缓存产物按【源内容哈希】定名,不再按修改时间判可否复用(复验 🔴① 修的就是这里)。
// 缓存目录 ~/.cache/dingwei-pgtest 是【全机共用】的,而每个工作树各有自己的一份源码:
// 只按 mtime 判,就会在 A 树里拿到 B 树的源码编出来的二进制。复验实测撞上过一次 ——
// 变异工具编得较新,回到被检工作树后 5 次连红(那是【假红】:跑的是别处编的工具);
// 顺序反过来就是【假绿】:修好的源码被一份更晚编出的未修版二进制顶替,
// 判据全在跑、跑的却不是这份代码。名字带上内容哈希之后,「命中」本身就等于
// 「同一份源编出来的」,不必再信时间戳。
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
	sources, err := toolSources(toolDir)
	if err != nil {
		return "", err
	}
	// 树内预构建产物:每个工作树各一份,不会被别处编的东西顶替,故仍按新旧判(mtime)即可。
	if prebuilt := filepath.Join(toolDir, "pgtest"); usable(prebuilt, sources) {
		return prebuilt, nil
	}
	digest, err := sourcesDigest(toolDir, sources)
	if err != nil {
		return "", err
	}
	cacheDir, err := buildCacheDir()
	if err != nil {
		return "", err
	}
	bin := cacheBinaryPath(cacheDir, digest)
	if fi, err := os.Stat(bin); err == nil && !fi.IsDir() && fi.Size() > 0 {
		return bin, nil // 名字即内容哈希 ⇒ 命中就是同一份源编出来的
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		// 「没有 go 工具链」属环境缺失(errEnvMissing ⇒ 严格模式下仍 Skip),
		// 与「工具编不出来」(环境在却坏了 ⇒ 严格模式 Fail)分开报。
		return "", fmt.Errorf("找不到 go 工具链,无法构建内嵌 PG 工具(%w): %w", errEnvMissing, err)
	}
	// 🔴 先编到临时名再 rename:同目录 rename 是原子的,并发跑的另一个工作树
	// 不会读到半成品(读到半截的可执行文件,报的还是 exec format error 这种指不到根因的错)。
	tmp := fmt.Sprintf("%s.tmp-%d", bin, os.Getpid())
	build := exec.Command(goBin, "build", "-o", tmp, ".")
	build.Dir = toolDir
	var out strings.Builder
	build.Stdout = &out
	build.Stderr = &out
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("go build ./tools/pgtest 失败: %w\n%s", err, tail(out.String(), 2000))
	}
	if err := os.Rename(tmp, bin); err != nil {
		// 并发下可能撞上 ETXTBSY:别的进程正在执行同名产物。同哈希 ⇒ 同内容,
		// 现成的那一份就是对的,不必非由本进程安放。
		if _, statErr := os.Stat(bin); statErr == nil {
			_ = os.Remove(tmp)
			return bin, nil
		}
		return "", fmt.Errorf("安放构建产物 %s: %w", bin, err)
	}
	return bin, nil
}

// toolSources 枚举参与构建的源文件(排序,保证哈希稳定)。
//
// 🔴 不能写死 main.go＋go.mod:工具以后拆文件时,漏进哈希的那个源文件改了而产物名不变,
// 又会拿旧二进制跑 —— 与这次要修的是同一类错,只是换了个入口。故按目录实际内容枚举(含子目录)。
// 只收 *.go / go.mod / go.sum:树内预构建产物 tools/pgtest/pgtest 不是源,不参与哈希。
func toolSources(toolDir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(toolDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum" {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("枚举 %s 的源文件: %w", toolDir, err)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 下找不到任何 .go/go.mod/go.sum,不像工具源目录", toolDir)
	}
	return out, nil
}

// sourcesDigest 算源内容的 sha256,取前 16 位十六进制(足够区分,文件名也短)。
//
// 每份文件先写「相对路径＋长度」再写内容,并以 0 字节分隔:光把内容拼起来的话,
// 把字节在两个文件之间挪一挪就能凑出同一个哈希(分隔符与长度前缀一起才挡住这种歧义)。
func sourcesDigest(toolDir string, sources []string) (string, error) {
	h := sha256.New()
	for _, src := range sources {
		rel, err := filepath.Rel(toolDir, src)
		if err != nil {
			rel = src
		}
		body, err := os.ReadFile(src)
		if err != nil {
			return "", fmt.Errorf("读源文件 %s: %w", src, err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(body))
		h.Write(body)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// buildCacheDir 取构建缓存目录(与旧版同一个位置,便于人工清理)。
func buildCacheDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil || cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "dingwei-pgtest-cache")
	}
	cacheDir = filepath.Join(cacheDir, "dingwei-pgtest")
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return "", fmt.Errorf("建构建缓存目录: %w", err)
	}
	return cacheDir, nil
}

// cacheBinaryPath 由内容哈希给出产物路径:名字里的哈希就是「这份产物由哪份源编出来」的凭证。
//
// ⚠️ 代价如实写:源码每换一版就多一个产物,旧的【不】自动清理 ——
// 清理会与并发跑的其它工作树打架(它可能正在执行那个文件)。单个约 8.8MB,
// 要清就人工清整个目录。
func cacheBinaryPath(cacheDir, digest string) string {
	return filepath.Join(cacheDir, "pgtest-"+digest)
}

// usable 判可执行文件是否已存在且不比任一源文件旧。
//
// 🔴 现在【只】用于树内预构建产物 tools/pgtest/pgtest:那份是每个工作树各自的,
// 不存在跨树顶替,按新旧判够用。共用缓存目录一律走内容哈希命名(见 toolBinary),
// 因为「产物比源新」根本不能证明「产物是这份源编的」。
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

// assertCleanup 在测试结束时判「运行目录与日志目录真的都没了」,并核残留增量。
//
// 🔴 只在本包自己起了库时判(ac.owned())。库是外部经 HUB_TEST_PG_DSN 给的,
// 它的目录不归本包管,也就不该由本包断言。
// 🔴 子进程自报 CLEANED=1 只是【它的说法】;这里再独立 stat 一次 ——
// 同一件事不许两处都只自证,自报与实测不符就报错。
// 🔴 两个目录都要 stat:复验 🟡③ 那处泄漏(系统临时目录里的 embedded_postgres_log*)
// 之所以两道自证都没看见,就是因为旧版只核运行目录这一个地方。
// stat 之外还有一道 residue.check(见 residue.go):它核的是【系统临时目录与进程表】,
// 那两处不在本包给出的路径里,工具撒不撒谎都盖不住。
// 🔴 必须在【停库之前】登记:t.Cleanup 是 LIFO,先登记者后跑,
// 这样 stat 发生在运行目录本该已被删之后。顺序见 Pool 的注释。
func assertCleanup(t *testing.T, ac acquired) {
	t.Helper()
	if !ac.owned() {
		return
	}
	t.Cleanup(func() {
		for _, dir := range []struct {
			kind string
			path string
		}{
			{"运行目录", ac.runtimeDir},
			{"日志目录", ac.logDir},
		} {
			if dir.path == "" {
				continue // 缺键已在 start() 里报过,这里不重复
			}
			if _, err := os.Stat(dir.path); err == nil {
				t.Errorf("临时 PG %s仍在,「随清」不成立: %s", dir.kind, dir.path)
			}
		}
		ac.residue.check(t)
	})
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
