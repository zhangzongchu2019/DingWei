package pgtest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// TestPool_RoundTrip 是测试环境的【正向】验收:临时 PG 起得来、走的是 R0 真正要走的
// 连接路径(repo.OpenPG,pgx 驱动)、能建表能读写能回滚、收尾自证删净。
//
// 🔴 拿不到 PG 时分两类(判据只有 unavailable 一个出口):**环境本就缺失** ⇒ Skip(集成层,与
// internal/datad 的 DATAD_TEST_PG_DSN 门控同理);**环境在却起不来** ⇒ 默认 Fail(翻转见包首注),
// 只有显式置 HUB_TEST_PG_SKIP_OK=1 才降级成 Skip。本用例同时是端到端对照里被当靶子跑的那一个。
func TestPool_RoundTrip(t *testing.T) {
	pool := Pool(t)
	ctx := context.Background()
	// 🔴 这里【故意不用】repo 包那个「系统上下文」构造器(建表/迁移/健康检查专用的那个)。
	// 它每一处都是评审面,被 scripts/repo-boundary-gate.sh 记成只许下行的棘轮
	// (specs/gates/boundary-baseline.txt: system 基线 12),且该闸【不豁免 _test.go】——
	// 本包首版在测试里真调了一处,当场把 12 顶成 13、多出一条红。
	// 测试不该把评审额度悄悄花掉,故改用双轴上下文:Pool.Exec 只校验「上下文存在且非零值」,
	// 用真租户上下文既过校验,也顺带走了一遍生产会走的那条路径。
	// ⚠️ 代价如实写:对 DDL 而言双轴上下文语义不完美(建表本属无租户语义的系统操作)。
	// R0 的 PG 迁移路径【必然】要新增真正的系统上下文调用点,那条棘轮该涨多少、走谁的评审,
	// 属设计问题,已上报组长,不在这里就地消化。
	// 🔴 本段注释【故意不写出那个构造器的调用字面量】:该闸按纯正则计数,连注释一起数
	// (基线 12 里已有 2 处是注释,见 fanout/spool.go:40、datad/pgstore.go:93)。
	// 在散文里写一遍就会凭空占掉一格评审额度 —— 已作为闸缺陷上报,此处先不踩。
	sys, err := repo.NewAccessCtx("r0-pgtest", "r0-pgtest")
	if err != nil {
		t.Fatalf("构造双轴上下文: %v", err)
	}

	// ① 连的确实是 PostgreSQL,且是 repo.OpenPG 这条路(不是本包私开的句柄)。
	row, err := pool.QueryRow(ctx, sys, `select version()`)
	if err != nil {
		t.Fatalf("QueryRow(version) 被拒: %v", err)
	}
	var version string
	if err := row.Scan(&version); err != nil {
		t.Fatalf("scan version: %v", err)
	}
	if !strings.Contains(version, "PostgreSQL") {
		t.Fatalf("连到的不是 PostgreSQL: %q", version)
	}
	t.Logf("临时 PG: %s", version)

	// ② 持久性判据:fanout spool 的既有语义是「COMMIT 返回=已 fsync」
	//    (internal/fanout/spool.go:3,synchronous=FULL)。tools/pgtest 显式钉了
	//    synchronous_commit=on/fsync=on;这里实测它真的生效,不让它随库默认漂走。
	//    🔴 这条是【R0 换存储后必须保住的语义】,不是随手加的健康检查。
	for _, param := range []string{"synchronous_commit", "fsync"} {
		r, err := pool.QueryRow(ctx, sys, `show `+param)
		if err != nil {
			t.Fatalf("show %s 被拒: %v", param, err)
		}
		var got string
		if err := r.Scan(&got); err != nil {
			t.Fatalf("scan %s: %v", param, err)
		}
		if got != "on" {
			t.Errorf("%s=%q,要 on —— 换存储会丢掉 fanout 的 fsync 语义", param, got)
		}
	}

	// ③ DDL＋写读往返。走 repo 的受控面,不走裸句柄。
	if _, err := pool.Exec(ctx, sys, `create table r0_probe (k text primary key, v text not null)`); err != nil {
		t.Fatalf("建表: %v", err)
	}
	if _, err := pool.Exec(ctx, sys, `insert into r0_probe (k, v) values ($1, $2)`, "a", "1"); err != nil {
		t.Fatalf("插入: %v", err)
	}
	r, err := pool.QueryRow(ctx, sys, `select v from r0_probe where k = $1`, "a")
	if err != nil {
		t.Fatalf("查询被拒: %v", err)
	}
	var got string
	if err := r.Scan(&got); err != nil {
		t.Fatalf("scan v: %v", err)
	}
	if got != "1" {
		t.Errorf("写读往返不一致: got %q, want %q", got, "1")
	}

	// ④ 主键冲突要报【错】,不能被降格成空结果集(§22.2a #2a 同一条判法)。
	if _, err := pool.Exec(ctx, sys, `insert into r0_probe (k, v) values ($1, $2)`, "a", "2"); err == nil {
		t.Error("重复主键插入居然成功 —— 约束没生效")
	}

	// ⑤ InTx 回滚:fn 返回错误即回滚,回滚后读不到那行。
	sentinel := errors.New("故意回滚")
	err = pool.InTx(ctx, sys, nil, func(tx repo.Tx) error {
		if _, e := tx.Exec(ctx, `insert into r0_probe (k, v) values ($1, $2)`, "b", "9"); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("InTx 没把哨兵错误交回: %v", err)
	}
	r2, err := pool.QueryRow(ctx, sys, `select count(*) from r0_probe where k = $1`, "b")
	if err != nil {
		t.Fatalf("回滚后查询被拒: %v", err)
	}
	var n int
	if err := r2.Scan(&n); err != nil {
		t.Fatalf("scan count: %v", err)
	}
	if n != 0 {
		t.Errorf("回滚没生效: k=b 仍有 %d 行", n)
	}

	// ⑥「随清」由 Pool 登记的 assertCleanup 独立 stat 判定,不在这里重复断言。
	//   它的【反向对照】是 TestCleanupIsReal_KeepRuntimeNegativeControl。
}

// TestCleanupIsReal_KeepRuntimeNegativeControl 是「随清」判据的【反向对照】。
//
// 为什么必须有:TestPool_RoundTrip 里「运行目录已删」这条,如果 tools/pgtest 把
// CLEANED 写死成 1、或者助手根本不 stat,用例照样绿 —— 那就是「判据不起作用但看得见 ✓」,
// 与 scripts/repo-boundary-gate.sh 头注里记过的事故同型。
// 这里用 -keep-runtime 起同一个工具,要求【目录必须还在】且 CLEANED 必须【不是】1:
// 两个方向都对得上,才说明 CLEANED=1 是真信号。
func TestCleanupIsReal_KeepRuntimeNegativeControl(t *testing.T) {
	bin, err := toolBinary(t)
	if err != nil {
		// 与 acquire 同一套分类:工具编不出来属「环境在却坏了」,严格模式下要 Fail。
		kind := envBroken
		if errors.Is(err, errEnvMissing) {
			kind = envMissing
		}
		unavailable(t, kind, "内嵌 PG 工具不可用: %v", err)
	}
	if strings.TrimSpace(os.Getenv(DSNEnv)) != "" {
		t.Skipf("%s 已设:外部给库,本反证针对内嵌 PG 的清理自证,不适用", DSNEnv)
	}
	runtime := filepath.Join(t.TempDir(), "keep")
	cmd := exec.Command(bin, "-runtime", runtime, "-keep-runtime")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin 管道: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout 管道: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr // 不进测试输出:PG 与 pg_ctl 日志有几十行,会把判据淹掉;失败时再打
	if err := cmd.Start(); err != nil {
		t.Fatalf("起工具: %v", err)
	}
	// 🔴 这里【不】用 readProtocol:它读到 READY=1 就收工,而本反证要的是 READY 之后
	// 那几行(STOPPED/CLEANED/RUNTIME_STATE/LOGDIR_STATE/PGLOG_REDIRECTED/REAPED)。
	// 改用生产那套 newProtocol:await 等就绪、waitEOF 等收尾行、snapshot 取值。
	//
	// 🔴 旧版在这儿自己抄了一遍扫描器,抄出两个毛病(复验 🟡② 撞上其一):
	//   ① stdout 在 READY 之前就关了(工具早退)⇒ 那个 reader 不发任何信号,
	//      select 只剩 startTimeout 一条出口 ⇒ 一路卡满 6 分钟,复验 M5 就是这么把整包卡死的;
	//   ② kv 由 goroutine 写、主流程读,没有同步 ⇒ -race 下是实打实的数据竞争。
	// 换成 newProtocol 之后两个毛病一起没:它在 EOF 时会给 ready 发错误,kv 由互斥锁看着。
	// 顺带的好处是:本反证走的解析路径与生产完全同一条,不再是「另抄一份、各测各的」。
	proto := newProtocol(stdout)
	if err := proto.await(startTimeout); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatalf("内嵌 PG 在 %s 内未就绪: %v\nstderr 末尾:\n%s",
			startTimeout, err, tail(stderr.String(), 2000))
	}
	if _, err := os.Stat(runtime); err != nil {
		t.Errorf("-keep-runtime 下运行目录本该在,却 stat 不到: %v", err)
	}
	_ = stdin.Close()
	if !proto.waitEOF(stopTimeout) {
		_ = cmd.Process.Kill()
		proto.waitEOF(5 * time.Second)
		t.Fatalf("工具在 %s 内没关 stdout,已 kill:收尾那几行读不到,本反证无从判", stopTimeout)
	}
	if err := cmd.Wait(); err != nil {
		t.Logf("收尾非零(-keep-runtime 下属预期之外,记录不判失败): %v", err)
	}
	kv := proto.snapshot()
	if got := kv["CLEANED"]; got == "1" {
		t.Errorf("🔴 -keep-runtime 下仍自报 CLEANED=1 ⇒ CLEANED 是写死的,不是真信号")
	}
	if got := kv["CLEANED"]; got != "kept" {
		t.Errorf("CLEANED=%q,期望 kept(工具要能区分「删了」与「留着」)", got)
	}
	if kv["STOPPED"] != "1" {
		t.Errorf("STOPPED=%q,期望 1:库没真停下就不该算收尾完成", kv["STOPPED"])
	}
	// 两个目录各自的状态都要能指出来:CLEANED 是合成值,合成值对了不代表两边都对
	// (旧版只有一个布尔,「运行目录删了、日志目录漏了」会合成出同一个「删了」)。
	if got := kv["RUNTIME_STATE"]; got != "kept" {
		t.Errorf("RUNTIME_STATE=%q,期望 kept", got)
	}
	if got := kv["LOGDIR_STATE"]; got != "kept" {
		t.Errorf("LOGDIR_STATE=%q,期望 kept", got)
	}
	if _, ok := kv["REAPED"]; !ok {
		t.Errorf("工具没报 REAPED:「有没有收掉孤儿 postmaster」就只剩推断,没有读数")
	} else {
		t.Logf("REAPED=%s(收掉的孤儿 postmaster 数;僵尸残留由 residue 那道独立核对判)", kv["REAPED"])
	}
	// 🔴 REAP_SECONDS 是【收尸自己花了多久】的自报。必须有它:上一版收尸改用静默窗后
	// 每次起停贵了 5–9 秒,而这件事只能靠「两棵树各跑整包 -count=2」对出来 ——
	// 耗时藏在整包读数里,归因全靠推断。有了这个数,「收尸贵不贵」是一条直接读数。
	if v, ok := kv["REAP_SECONDS"]; !ok {
		t.Errorf("工具没报 REAP_SECONDS:收尸耗时又只能从整包用例时间外推(上一版就是这么把 2.55 倍变慢查出来的)")
	} else if _, err := strconv.ParseFloat(v, 64); err != nil {
		t.Errorf("REAP_SECONDS=%q 不是个数:自报的读数解析不了就等于没报", v)
	} else {
		t.Logf("REAP_SECONDS=%s(主扫＋收尾两遍合计;终止条件是「已无后代」,正常路径不该撞到上限)", v)
	}
	// REAP_LEFTOVER 只在【时间上限触发】时出现。它是排障坐标,不是失败判据 ⇒ 只如实转述:
	// 出现了就说明有后代在 5 秒里没退出,那时候残留格红不红都由 residue 那道独立核对说了算。
	if v, ok := kv["REAP_LEFTOVER"]; ok {
		t.Logf("🔵 REAP_LEFTOVER=%s:收尸撞到时间上限,还有这么多个后代没退出(排障坐标,不是判据)", v)
	}

	// 🔴 日志泄漏(复验 🟡③)的【正证】:库那个日志文件确实落进了工具自己的日志目录。
	// embedded-postgres v1.34.0 的 logging.go:18 用 os.CreateTemp("",…) 建它,认 TMPDIR;
	// 工具起库前把 TMPDIR 指到了日志目录 ⇒ 文件该在这儿。它在这儿,系统临时目录里就没有它。
	// 反过来,PGLOG_REDIRECTED 只是工具的自报,真正的判据是下面这次 ReadDir。
	logDir := kv["LOGDIR"]
	if logDir == "" {
		t.Errorf("工具没报 LOGDIR:PG 日志漏在哪儿都无从核对")
	} else {
		entries, err := os.ReadDir(logDir)
		if err != nil {
			t.Errorf("-keep-runtime 下日志目录本该在,却读不了 %s: %v", logDir, err)
		} else {
			found := ""
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), pgLogPrefix) {
					found = e.Name()
					break
				}
			}
			if found == "" {
				t.Errorf("🔴 日志目录 %s 里没有 %s* ⇒ TMPDIR 重定向没接住库的日志文件,系统临时目录仍在漏", logDir, pgLogPrefix)
			} else {
				t.Logf("库的日志文件确实落在工具的日志目录里: %s", filepath.Join(logDir, found))
			}
		}
		if got := kv["PGLOG_REDIRECTED"]; got != "1" {
			t.Errorf("PGLOG_REDIRECTED=%q,期望 1(与上面那次 ReadDir 应互相印证)", got)
		}
		// 本反证用 -keep-runtime 把它留下来了,自己收:留在系统临时目录就是新残留。
		_ = os.RemoveAll(logDir)
		if _, err := os.Stat(logDir); err == nil {
			t.Errorf("日志目录删不掉,本反证自己留了残留: %s", logDir)
		}
	}

	// 反证结束后自己收拾:这是 t.TempDir() 下的目录,t 会删,但库进程要先真停下。
	if _, err := os.Stat(runtime); err == nil {
		_ = os.RemoveAll(runtime)
	}
}

// TestReadProtocol_NegativeControls 判协议解析不是「来什么都能凑出个 DSN」。
func TestReadProtocol_NegativeControls(t *testing.T) {
	t.Run("噪音行被忽略,协议行照取", func(t *testing.T) {
		in := "waiting for server to start....\nLOG:  database system is ready\n done\nPORT=1\nDSN=postgres://x\nREADY=1\n"
		kv, err := readProtocol(strings.NewReader(in), 5*time.Second)
		if err != nil {
			t.Fatalf("不该报错: %v", err)
		}
		if kv["DSN"] != "postgres://x" || kv["PORT"] != "1" {
			t.Errorf("协议行没取对: %v", kv)
		}
		if _, ok := kv["waiting for server to start...."]; ok {
			t.Error("噪音行被当成协议行了")
		}
	})
	t.Run("缺 READY 必须报错", func(t *testing.T) {
		_, err := readProtocol(strings.NewReader("DSN=postgres://x\n"), 5*time.Second)
		if err == nil {
			t.Fatal("没有 READY=1 却报成功 —— 会把没起好的库当成起好了")
		}
	})
	t.Run("READY 在超时内没来必须报错", func(t *testing.T) {
		// 用一个永不产出 READY 的 reader:管道不写也不关。
		pr, pw := newPipe()
		defer pw.Close()
		_, err := readProtocol(pr, 200*time.Millisecond)
		if err == nil {
			t.Fatal("超时路径没报错")
		}
		if !strings.Contains(err.Error(), "超时") {
			t.Errorf("错误文案没说明是超时: %v", err)
		}
	})
}

// TestUsable_NegativeControls 判「构建产物可复用」的判据不是恒真。
func TestUsable_NegativeControls(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	bin := filepath.Join(dir, "bin")
	if err := os.WriteFile(src, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if usable(bin, []string{src}) {
		t.Error("可执行文件不存在却判可用")
	}
	if err := os.WriteFile(bin, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !usable(bin, []string{src}) {
		t.Error("刚建好、源文件更旧,本该判可用")
	}
	// 源文件比产物新 ⇒ 必须判【不】可用,否则会拿旧二进制跑出假绿。
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}
	if usable(bin, []string{src}) {
		t.Error("🔴 源文件更新了仍判可用 ⇒ 会拿过期二进制跑测试")
	}
	if usable(bin, []string{filepath.Join(dir, "不存在.go")}) {
		t.Error("源文件缺失时应判不可用(宁可重建)")
	}
}

func newPipe() (*os.File, *os.File) {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	return r, w
}

// TestEmbeddedDisabled_SemanticsByLiteralReading 锁住「环境变量怎么读」这条判据。
//
// 🔴 为什么值得单独立一条:这个开关控制的是【用例会不会 Skip】。写反了不会有任何用例变红 ——
// 只会让「按令不许起库」的环境里悄悄起了一个库(本包首版就是 =0 反而允许),
// 或反过来让本来能跑的环境全部跳掉、而跳掉看上去像通过。两头都无声。
func TestEmbeddedDisabled_SemanticsByLiteralReading(t *testing.T) {
	cases := []struct {
		value string
		want  bool // true = 禁用内嵌 PG
		why   string
	}{
		{"", false, "设为空等同未设 ⇒ 允许(默认便于本地直接跑)"},
		{"1", false, "1 是「开」"},
		{"true", false, "true 是「开」"},
		{"TRUE", false, "大小写不该改变语义"},
		{"on", false, "on 是「开」"},
		{"yes", false, "yes 是「开」"},
		{"0", true, "🔴 首版写反的就是这一格:0 是「关」,不是「开」"},
		{"false", true, "false 是「关」"},
		{"off", true, "off 是「关」"},
		{"no", true, "no 是「关」"},
		{"否", true, "非白名单值一律按【关】处理(fail-closed:宁可不擅自起库)"},
		{"随便写点什么", true, "同上:认不出的值不许滑成「允许」"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Setenv(EmbeddedOffEnv, tc.value)
			got, why := embeddedDisabled()
			if got != tc.want {
				t.Errorf("%s=%q: embeddedDisabled()=%v,要 %v(%s)", EmbeddedOffEnv, tc.value, got, tc.want, tc.why)
			}
			if got && strings.TrimSpace(why) == "" {
				t.Errorf("判为禁用却没给理由 ⇒ Skip 文案会缺原因,等于没说明为什么没跑")
			}
		})
	}
}

// TestPool_SkipsEndToEndWhenEmbeddedDisabled 是上一条的【端到端对照】:
// 纯函数判对了不等于 Pool 真会 Skip(中间还隔着 acquire 的三级顺序)。
// 这里用子进程真跑一遍 -run TestPool_RoundTrip,要求输出里出现 SKIP、且【不许】出现
// 「临时 PG:」那行日志 —— 后者一旦出现,说明库其实被起起来了,Skip 没生效。
func TestPool_SkipsEndToEndWhenEmbeddedDisabled(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("找不到 go 工具链,跑不了端到端对照: %v", err)
	}
	cmd := exec.Command(goBin, "test", "-run", "TestPool_RoundTrip", "-v", "-count=1", ".")
	cmd.Env = append(os.Environ(), EmbeddedOffEnv+"=0")
	// 🔴 必须显式清掉外部 DSN:否则三级顺序会走①,用例照跑不 Skip,
	// 那就测不到「禁用内嵌 PG」这一格,而是测到了别的东西 —— 绿得没有意义。
	cmd.Env = append(cmd.Env, DSNEnv+"=")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // 全 Skip 时 go test 报 "ok ... [no tests to run]" 或非零,都不作判据
	got := out.String()
	if !strings.Contains(got, "--- SKIP: TestPool_RoundTrip") {
		t.Fatalf("端到端没 Skip。输出:\n%s", got)
	}
	if strings.Contains(got, "临时 PG:") {
		t.Fatalf("🔴 报了 SKIP 却仍然起了库(出现了版本日志)⇒ 两级判据互相冒充。输出:\n%s", got)
	}
	if !strings.Contains(got, EmbeddedOffEnv) {
		t.Errorf("Skip 文案里没点明是哪个环境变量导致的 ⇒ 读日志的人不知道为什么没跑。输出:\n%s", got)
	}
}

// TestToolBinaryCache_KeyedOnContentNotMtime 判「构建产物可复用」的凭据是【源内容】,不是时间戳。
//
// 🔴 这条是复验 🔴① 的反向对照。旧版把产物放在【全机共用】的 ~/.cache/dingwei-pgtest/pgtest,
// 又只按「源文件不比产物新」判复用 ⇒ 在 A 工作树里会拿到 B 工作树的源码编出来的二进制。
// 复验实测撞上过:先在一次性工作树里编了个变异工具(时间较新),回到被检工作树后 5 次连红 ——
// 那是【假红】,跑的是别处编的工具;顺序反过来就是【假绿】(修好的源码被更晚编出的未修版顶替)。
// 两种都不会有任何判据报警,因为判据看的是时间,而时间跟内容无关。
//
// 这里不真去 go build(一次要几秒,而且被判的不是编译器),只判【定名】这一步:
// 定名只由内容决定 ⇒ 内容不同就不可能互相顶替,内容相同就仍然复用(缓存没被修坏)。
func TestToolBinaryCache_KeyedOnContentNotMtime(t *testing.T) {
	// 🔴 时间戳一律钉成同一个值:本条要判的正是「时间戳完全相同时还分不分得清」。
	fixed := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	write := func(dir, name, body string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, fixed, fixed); err != nil {
			t.Fatal(err)
		}
	}
	// newTree 造一棵「工具源目录」,返回它的内容哈希。
	newTree := func(files map[string]string) string {
		t.Helper()
		dir := t.TempDir()
		for name, body := range files {
			write(dir, name, body)
		}
		return digestOf(t, dir)
	}
	base := map[string]string{"main.go": "package main // 甲\n", "go.mod": "module x\n", "go.sum": ""}

	t.Run("内容不同、时间戳相同 ⇒ 产物名必须不同", func(t *testing.T) {
		other := map[string]string{"main.go": "package main // 乙\n", "go.mod": "module x\n", "go.sum": ""}
		d1, d2 := newTree(base), newTree(other)
		if d1 == d2 {
			t.Errorf("🔴 两份内容不同、时间戳相同的源得到了同一个产物名 %s ⇒ 跨工作树会互相顶替(复验 🔴① 原样重演)",
				cacheBinaryPath("CACHE", d1))
		}
	})
	t.Run("内容相同 ⇒ 产物名相同(缓存仍然生效)", func(t *testing.T) {
		d1, d2 := newTree(base), newTree(base)
		if d1 != d2 {
			t.Errorf("同样的源算出两个哈希(%s / %s) ⇒ 每次跑都要重新构建,缓存等于废了", d1, d2)
		}
		if got, want := cacheBinaryPath("/x", d1), filepath.Join("/x", "pgtest-"+d1); got != want {
			t.Errorf("产物名没按 pgtest-<hash> 定: got %s, want %s", got, want)
		}
	})
	t.Run("只改时间戳、内容不变 ⇒ 产物名不变", func(t *testing.T) {
		dir := t.TempDir()
		for name, body := range base {
			write(dir, name, body)
		}
		before := digestOf(t, dir)
		future := time.Now().Add(time.Hour)
		if err := os.Chtimes(filepath.Join(dir, "main.go"), future, future); err != nil {
			t.Fatal(err)
		}
		if after := digestOf(t, dir); after != before {
			t.Errorf("时间戳一变哈希就变(%s → %s) ⇒ 还在拿时间当凭据,只是换了个地方", before, after)
		}
	})
	t.Run("多一个源文件 ⇒ 产物名必须变", func(t *testing.T) {
		dir := t.TempDir()
		for name, body := range base {
			write(dir, name, body)
		}
		before := digestOf(t, dir)
		write(dir, "extra.go", "package main\n")
		if after := digestOf(t, dir); after == before {
			t.Errorf("🔴 新增源文件不影响哈希 ⇒ 写死「只哈希 main.go＋go.mod」的老毛病还在,工具拆文件后又会拿旧二进制跑")
		}
	})
	t.Run("内容在两个文件之间挪一挪 ⇒ 产物名必须变", func(t *testing.T) {
		// 光把内容拼起来哈希的话,这两棵树的字节流一模一样(都是 ABCD),会撞成同一个名字。
		d1 := newTree(map[string]string{"main.go": "AB", "go.mod": "CD", "go.sum": ""})
		d2 := newTree(map[string]string{"main.go": "ABC", "go.mod": "D", "go.sum": ""})
		if d1 == d2 {
			t.Errorf("拼接歧义没挡住:两棵树哈希相同(%s)", d1)
		}
	})
	t.Run("构建产物自己不参与哈希", func(t *testing.T) {
		dir := t.TempDir()
		for name, body := range base {
			write(dir, name, body)
		}
		before := digestOf(t, dir)
		write(dir, "pgtest", "\x7fELF 假装是个二进制")
		if after := digestOf(t, dir); after != before {
			t.Errorf("把构建产物算进了哈希(%s → %s) ⇒ 构建一次名字就变,缓存永不命中", before, after)
		}
	})
}

// digestOf 算一棵「工具源目录」的内容哈希(测试用薄封装,把两个错误出口收成 t.Fatal)。
func digestOf(t *testing.T, dir string) string {
	t.Helper()
	sources, err := toolSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := sourcesDigest(dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// TestSkipAllowed_SemanticsByLiteralReading 锁住「环境在却起不来能否降级成 Skip」这条判据怎么读。
//
// 🔴 与被它替换掉的 TestStrictMode_SemanticsByLiteralReading 同理,而这里的失误更隐蔽:
// 判据控制的是【会不会 Skip】,写反不会有任何用例变红 —— 失效之后的表现恰恰是【全绿】。
// 故连「认不出的值怎么办」也要判:必须报错,不许滑成「允许」也不许滑成「不允许」,
// 否则一个打错的 SKIP_OK=ture 就把判据悄悄改了向,而日志上什么也看不见。
//
// 🔴 翻转之后有两条不变量,本用例逐格钉住:
//
//	一、返回 true 只有【一个】来源 —— SkipOKEnv 显式置了「开」的值;
//	二、StrictEnv 的任何取值都不得让本函数返回 true(旧「关」不再等于「允许 Skip」,而是报错)。
func TestSkipAllowed_SemanticsByLiteralReading(t *testing.T) {
	cases := []struct {
		name    string
		strict  string // 旧开关 HUB_TEST_PG_STRICT:翻转后只认「开」值,效果与默认相同
		skipOK  string // 新开关 HUB_TEST_PG_SKIP_OK:唯一能允许 Skip 的那个
		want    bool
		wantErr bool
		why     string
	}{
		{"两个都不设", "", "", false, false, "🔴 默认不允许 Skip —— 这一格就是翻转本身(反向对照:把默认改回宽松,本格立刻变 true)"},
		{"SKIP_OK=1", "", "1", true, false, "显式置 1 是唯一能允许 Skip 的路"},
		{"SKIP_OK=true", "", "true", true, false, "true 是允许"},
		{"SKIP_OK=TRUE", "", "TRUE", true, false, "大小写不该改变语义"},
		{"SKIP_OK=on", "", "on", true, false, "on 是允许"},
		{"SKIP_OK=yes", "", "yes", true, false, "yes 是允许"},
		{"SKIP_OK=是", "", "是", true, false, "中文「是」也是允许(与 EmbeddedOffEnv 认「否」对称)"},
		{"SKIP_OK=0", "", "0", false, false, "显式 0 是不允许,与默认同"},
		{"SKIP_OK=false", "", "false", false, false, "false 是不允许"},
		{"SKIP_OK=off", "", "off", false, false, "off 是不允许"},
		{"SKIP_OK=no", "", "no", false, false, "no 是不允许"},
		{"SKIP_OK=否", "", "否", false, false, "中文「否」是不允许"},
		{"SKIP_OK=ture", "", "ture", false, true, "🔴 打错的 true 必须报错:它看上去像允许了、其实没允许,是最坏的一种静默失效"},
		{"SKIP_OK=2", "", "2", false, true, "认不出的值一律报错,不许滑成任何一侧"},
		{"STRICT=1", "1", "", false, false, "旧开关的「开」原样认下:过渡期各组读数带着 STRICT=1,翻转后不该反而报错"},
		{"STRICT=1 且 SKIP_OK=1", "1", "1", true, false, "旧「开」不压制新开关:允许 Skip 仍只由 SKIP_OK 决定"},
		{"STRICT=0", "0", "", false, true, "🔴 旧「关」不再表示「允许 Skip」⇒ 必须报错并指名改口,不许静默忽略(遗留的 =0 会让人以为这里能跳)"},
		{"STRICT=0 且 SKIP_OK=1", "0", "1", false, true, "即便新开关已置,遗留的 STRICT=0 仍报错:要跳过先把旧变量从环境里去掉"},
		{"STRICT=ture", "ture", "", false, true, "旧开关认不出的值同样报错"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(StrictEnv, tc.strict)
			t.Setenv(SkipOKEnv, tc.skipOK)
			got, err := skipAllowed()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("STRICT=%q SKIP_OK=%q 本该报错,却返回 (%v, nil):%s", tc.strict, tc.skipOK, got, tc.why)
				}
				if !strings.Contains(err.Error(), StrictEnv) && !strings.Contains(err.Error(), SkipOKEnv) {
					t.Errorf("报错文案没点明是哪个环境变量读不懂: %v", err)
				}
				if got {
					t.Errorf("🔴 报错的同时返回了「允许 Skip」⇒ 只看布尔值的调用方会照样放行: %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("STRICT=%q SKIP_OK=%q 不该报错: %v", tc.strict, tc.skipOK, err)
			}
			if got != tc.want {
				t.Errorf("STRICT=%q SKIP_OK=%q: skipAllowed()=%v,要 %v(%s)", tc.strict, tc.skipOK, got, tc.want, tc.why)
			}
		})
	}
}

// TestSkipOKDefault_EndToEnd 是默认翻转的【端到端】对照:纯函数判对了不等于 Pool 真会 Fail。
//
// 用一个「起了就死、连一行协议都不打」的假工具充当故障环境(PGTEST_BIN 指过去),
// 形状与复验 M5 那一刀相同:环境在、工具在,库起不来。
// 🔴 第一格就是翻转本身 —— 把默认改回宽松,这一格立刻由 --- FAIL 变 --- SKIP,
// 这正是复验方要跑的反向对照(裁定 ruling-2026-10-01T130835Z)。
func TestSkipOKDefault_EndToEnd(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("找不到 go 工具链,跑不了端到端对照: %v", err)
	}
	fake := filepath.Join(t.TempDir(), "pgtest-fake")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// runPool 在子进程里只跑 TestPool_RoundTrip,返回它的输出。
	// extra 里给的开关排在最后 ⇒ os/exec 的 dedupEnv 以【最后一次】为准,盖得住 os.Environ() 里的遗留值。
	runPool := func(extra ...string) string {
		cmd := exec.Command(goBin, "test", "-run", "TestPool_RoundTrip", "-v", "-count=1", ".")
		cmd.Env = append(os.Environ(),
			"PGTEST_BIN="+fake, // 工具「在」,但它起不了库 ⇒ 属「环境在却起不来」
			DSNEnv+"=",         // 🔴 必须清掉外部 DSN,否则三级顺序走①,这一格根本没测到
			EmbeddedOffEnv+"=", // 也不许被「禁用内嵌 PG」抢先 Skip 掉(那是另一格)
			StrictEnv+"=",      // 两个开关都先清空,免得环境里的遗留值替本格做了决定
			SkipOKEnv+"=",
		)
		cmd.Env = append(cmd.Env, extra...)
		var out strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &out
		_ = cmd.Run() // 退出码不作判据:Fail 与「没有用例可跑」都可能非零,判据是输出里的行
		return out.String()
	}
	const (
		failLine = "--- FAIL: TestPool_RoundTrip"
		skipLine = "--- SKIP: TestPool_RoundTrip"
	)

	t.Run("默认 ⇒ FAIL,不许盖成 SKIP", func(t *testing.T) {
		got := runPool()
		if strings.Contains(got, skipLine) {
			t.Fatalf("🔴 默认仍然 Skip ⇒ 起库失败照旧被盖成绿(翻转没生效,复验 🟡② 未修)。输出:\n%s", got)
		}
		if !strings.Contains(got, failLine) {
			t.Fatalf("默认没 Fail。输出:\n%s", got)
		}
		if !strings.Contains(got, SkipOKEnv) {
			t.Errorf("Fail 文案没提到 %s ⇒ 读日志的人不知道确需跳过时该置哪个开关。输出:\n%s", SkipOKEnv, got)
		}
	})
	t.Run("SKIP_OK=1 ⇒ SKIP,且文案点明是谁放行的", func(t *testing.T) {
		got := runPool(SkipOKEnv + "=1")
		if !strings.Contains(got, skipLine) {
			t.Fatalf("显式置了 %s=1 却没 Skip。输出:\n%s", SkipOKEnv, got)
		}
		if !strings.Contains(got, SkipOKEnv) {
			t.Errorf("Skip 文案没点明是 %s 放行的 ⇒ 日志上看不出这次绿是「真过」还是「被放过」。输出:\n%s", SkipOKEnv, got)
		}
	})
	t.Run("SKIP_OK 认不出的值 ⇒ FAIL,并说清是开关读不懂", func(t *testing.T) {
		got := runPool(SkipOKEnv + "=ture")
		if !strings.Contains(got, failLine) {
			t.Fatalf("打错的开关值没让用例失败 ⇒ 被静默当成某一侧。输出:\n%s", got)
		}
		if !strings.Contains(got, SkipOKEnv) {
			t.Errorf("报错没点明是哪个环境变量读不懂。输出:\n%s", got)
		}
	})
	t.Run("过渡期读数 STRICT=1 ⇒ 仍 FAIL,且不因旧开关报错", func(t *testing.T) {
		got := runPool(StrictEnv + "=1")
		if !strings.Contains(got, failLine) {
			t.Fatalf("带着过渡期要求的 %s=1 反而没 Fail。输出:\n%s", StrictEnv, got)
		}
		if strings.Contains(got, "已不再表示") {
			t.Errorf("🔴 %s=1 被判成废弃报错 ⇒ 过渡期各组照裁定带的读数会全红。输出:\n%s", StrictEnv, got)
		}
	})
	t.Run("遗留 STRICT=0 ⇒ FAIL,且文案指名改用 SKIP_OK", func(t *testing.T) {
		got := runPool(StrictEnv + "=0")
		if !strings.Contains(got, failLine) {
			t.Fatalf("遗留的 %s=0 没让用例失败 ⇒ 旧「关」被静默忽略,判据翻了而环境里还写着允许跳过。输出:\n%s", StrictEnv, got)
		}
		if !strings.Contains(got, SkipOKEnv) {
			t.Errorf("报错没指名改用 %s ⇒ 撞上来的人不知道怎么改口。输出:\n%s", SkipOKEnv, got)
		}
	})
}

// TestResidueWatch_NegativeControls 判「独立核对残留」这道防线不是摆设。
//
// 🔴 与复验 M4′ 同型:工具可以撒谎(把 CLEANED 写死成 1),那时只有这道独立核对会红。
// 可这道核对本身要是恒不报(前缀写错、/proc 解析取错列),表现【恰恰像通过】——
// 所以必须用「故意造出残留」的方式证明它真会报,而不是等它哪天自己红。
func TestResidueWatch_NegativeControls(t *testing.T) {
	w := newResidueWatch()

	t.Run("系统临时目录里新出现的 PG 日志文件必须被抓到", func(t *testing.T) {
		if !w.logsOK {
			t.Skipf("读不到系统临时目录 %s,本项不适用", w.tempDir)
		}
		f, err := os.CreateTemp(w.tempDir, pgLogPrefix) // 与库同一个前缀(logging.go:18)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f.Name())
		f.Close()
		defer func() { _ = os.Remove(f.Name()) }() // 自己造的残留自己收
		now, ok := listPGLogs(w.tempDir)
		if !ok {
			t.Fatal("第二次快照没拍成")
		}
		if added := addedNames(w.logs, now); len(added) != 1 || added[0] != name {
			t.Errorf("🔴 增量没抓到刚造的残留: got %v, want [%s] ⇒ 这道独立核对是摆设", added, name)
		}
		// 收掉之后必须【不再】报:一直红的判据等于没有判据(没人会看第二次)。
		if err := os.Remove(f.Name()); err != nil {
			t.Fatal(err)
		}
		if after, ok := listPGLogs(w.tempDir); ok {
			if added := addedNames(w.logs, after); len(added) != 0 {
				t.Errorf("残留已经收掉了却还在报 %v ⇒ 判据不随实况走", added)
			}
		}
	})

	t.Run("真造一个 comm=postgres 的僵尸,必须被抓到", func(t *testing.T) {
		if !w.zombiesOK {
			t.Skip("/proc 读不到(非 Linux),本项不适用")
		}
		trueBin, err := exec.LookPath("true")
		if err != nil {
			t.Skipf("找不到 true,造不出僵尸: %v", err)
		}
		body, err := os.ReadFile(trueBin)
		if err != nil {
			t.Fatal(err)
		}
		fake := filepath.Join(t.TempDir(), "postgres") // comm 取自可执行文件名
		if err := os.WriteFile(fake, body, 0o700); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(fake)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cmd.Wait() }() // 兜底:自己造的僵尸必须自己收,别留给下一次跑
		// 故意不 Wait ⇒ 它退出后就是本进程的僵尸,与 postmaster 被 PID 1 挂着同型。
		deadline := time.Now().Add(10 * time.Second)
		found := false
		for time.Now().Before(deadline) {
			if z, ok := listZombiePostgres(); ok {
				if _, hit := z[cmd.Process.Pid]; hit {
					found = true
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !found {
			t.Fatalf("🔴 真造了一个 comm=postgres 的僵尸(pid %d)却没被抓到 ⇒ /proc 解析或前缀判错了,这道核对是摆设",
				cmd.Process.Pid)
		}
		if err := cmd.Wait(); err != nil {
			t.Logf("收尸返回(true 退出码 0,这里通常不该有错): %v", err)
		}
		if z, ok := listZombiePostgres(); ok {
			if _, hit := z[cmd.Process.Pid]; hit {
				t.Errorf("收尸之后还报同一个 pid ⇒ 判据不随实况走(那样历史残留会让它永远红)")
			}
		}
	})

	t.Run("/proc/<pid>/stat 解析:comm 里有空格和右括号也不能取错列", func(t *testing.T) {
		cases := []struct {
			in        string
			wantComm  string
			wantState string
			wantOK    bool
			why       string
		}{
			{"604120 (postgres) Z 1 604120 604120 0 -1 4227340", "postgres", "Z", true, "本席实测那行僵尸"},
			{"1 (sleep infinity) S 0 1 1 0 -1 4227340", "sleep infinity", "S", true, "comm 含空格:按空格分列会取错(本容器 PID 1 就是它)"},
			{"7 (a ) b) R 1 7 7 0 -1", "a ) b", "R", true, "comm 含右括号:必须按【最后一个】')' 切"},
			{"garbage", "", "", false, "没有括号 ⇒ 认不出来,宁可说认不出,不要瞎取一列"},
			{"7 (x)", "", "", false, "括号后没有字段 ⇒ 取不到状态"},
		}
		for _, tc := range cases {
			comm, state, ok := parseProcStat(tc.in)
			if ok != tc.wantOK || (ok && (comm != tc.wantComm || state != tc.wantState)) {
				t.Errorf("parseProcStat(%q) = (%q,%q,%v),要 (%q,%q,%v):%s",
					tc.in, comm, state, ok, tc.wantComm, tc.wantState, tc.wantOK, tc.why)
			}
		}
	})
}
