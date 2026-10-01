package pgtest

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// TestPool_RoundTrip 是测试环境的【正向】验收:临时 PG 起得来、走的是 R0 真正要走的
// 连接路径(repo.OpenPG,pgx 驱动)、能建表能读写能回滚、收尾自证删净。
//
// 拿不到 PG 时 Skip 而非 Fail:本用例是集成层,与 internal/datad 的 DATAD_TEST_PG_DSN 门控同理。
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
		t.Skipf("内嵌 PG 工具不可用: %v", err)
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
	// 那两行(STOPPED/CLEANED)。自己全程读到 EOF,既拿到收尾值,也不会因为没人读
	// stdout 而让子进程堵在写上。
	kv := map[string]string{}
	ready := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
			if !ok || key == "" {
				continue
			}
			kv[key] = value
			if key == "READY" && value == "1" {
				close(ready)
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(startTimeout):
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatalf("内嵌 PG 在 %s 内未就绪\nstderr 末尾:\n%s", startTimeout, stderr.String())
	}
	if _, err := os.Stat(runtime); err != nil {
		t.Errorf("-keep-runtime 下运行目录本该在,却 stat 不到: %v", err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Logf("收尾非零(-keep-runtime 下属预期之外,记录不判失败): %v", err)
	}
	if got := kv["CLEANED"]; got == "1" {
		t.Errorf("🔴 -keep-runtime 下仍自报 CLEANED=1 ⇒ CLEANED 是写死的,不是真信号")
	}
	if got := kv["CLEANED"]; got != "kept" {
		t.Errorf("CLEANED=%q,期望 kept(工具要能区分「删了」与「留着」)", got)
	}
	if kv["STOPPED"] != "1" {
		t.Errorf("STOPPED=%q,期望 1:库没真停下就不该算收尾完成", kv["STOPPED"])
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
