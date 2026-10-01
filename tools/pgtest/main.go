// Command pgtest 起一个【临时 PostgreSQL】,供 Hub 存储层集成用例使用,用完即清。
//
// 🔴 为什么是独立 module(不并进主 go.mod):
//
//	主仓有依赖棘轮闸 scripts/dependency-ratchet.sh —— 直接依赖数只许降不许升
//	(基线 specs/gates/dependency-baseline.txt: direct_count=6),而上调基线属【评审面】,
//	须随 spec §4.7.1 ⑤ 的突破论证一并提交,不得静默改。
//	embedded-postgres 会带进 lib/pq 与 xi2/xz;若加进主 go.mod,直接依赖 6→7 即触闸。
//	⇒ 放独立 module:主 go.mod 直接依赖数不变,棘轮闸照绿。
//	  代价:本工具要单独 go build。测试助手 internal/store/pgtest 会自动 build,不必手工。
//
// 🔴 本工具【不出现】sql.Open —— 建库句柄只许在 internal/repo(scripts/repo-boundary-gate.sh
//
//	判据一「仓储包外 sql.Open 恒零」)。这里起的是【服务进程】,连接由调用方经 repo.OpenPG 建。
//
// stdout 只走协议行(KEY=VALUE);PG 与 pg_ctl 的全部日志一律导到 stderr。
// 生命周期:打印 READY=1 后,直到 stdin 关闭或收到 SIGINT/SIGTERM 才停;
// 停完自己删【运行目录与日志目录】(库的 Stop() 两个都不删),并打印 CLEANED=1/kept/0 ——
// 「随起随清」的后半句由本工具负责,且【自证】(删不掉就报 CLEANED=0,不假装成功)。
// 收尾还收掉 pg_ctl 留下的孤儿 postmaster(见 becomeSubreaper),不在进程表里留僵尸。
//
// 协议行一览(助手 internal/store/pgtest 依赖这些键,少一个都会被判据发现):
//
//	PORT RUNTIME LOGDIR —— 在 Start() 【之前】就报:起库失败或被助手 kill 时,
//	                      助手至少知道该去核哪两个目录,不会抓瞎。
//	BINARIES CACHE DSN START_SECONDS READY=1
//	STOPPED=1/0 CLEANED=1/kept/0 RUNTIME_STATE=… LOGDIR_STATE=…
//	PGLOG_REDIRECTED=1/0 REAPED=n
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

func main() {
	var (
		portFlag    = flag.Uint("port", 0, "监听端口;0=自动挑一个空闲端口")
		listen      = flag.String("listen", "127.0.0.1", "监听地址(只应留在环回)")
		version     = flag.String("version", "", "PostgreSQL 版本串;空=库默认(当前 V18)")
		cacheRoot   = flag.String("cache-root", "", "下载与解压缓存根(二进制解压在其下 extracted/);空=$HOME/.embedded-postgres-go;与 -runtime 无关")
		runtime     = flag.String("runtime", "", "本次运行目录(数据目录在其下);空=新建临时目录,退出时删除")
		db          = flag.String("db", "hubtest", "库名")
		user        = flag.String("user", "hubtest", "用户名")
		password    = flag.String("password", "hubtest", "口令(临时库,只为起得来,不是机密)")
		startTO     = flag.Duration("start-timeout", 5*time.Minute, "启动超时;首次要下载二进制,故给足")
		keepRuntime = flag.Bool("keep-runtime", false, "退出时【不】删运行目录与日志目录(排障用;默认删)")
	)
	flag.Parse()

	if err := run(*portFlag, *listen, *version, *cacheRoot, *runtime, *db, *user, *password, *startTO, *keepRuntime); err != nil {
		fmt.Fprintf(os.Stderr, "pgtest: %v\n", err)
		os.Exit(1)
	}
}

func run(port uint, listen, version, cacheRoot, runtime, db, user, password string, startTO time.Duration, keepRuntime bool) error {
	// 🔴 先接管「后代孤儿」,再起库。
	// pg_ctl start 把 postmaster 起来之后自己就退出了 ⇒ postmaster 是孤儿,默认过继给 PID 1;
	// 而跑测试的容器 PID 1 是 `sleep infinity`,不收尸 ⇒ 每起一次库就在进程表里留一个
	// postgres 僵尸(本席实测攒了 25 个,PID 1 不收 ⇒ 谁都清不掉)。
	// 后果不只是难看:复查残留的人 `pgrep postgres` 会数到僵尸,读成「库还活着」。
	// 设了 PR_SET_CHILD_SUBREAPER,孤儿改为过继给本进程,收尾时 reapOrphans 就能真收掉。
	// 失败只警告不中止:它影响的是残留,不是功能。
	if err := becomeSubreaper(); err != nil {
		fmt.Fprintf(os.Stderr, "pgtest: 设 PR_SET_CHILD_SUBREAPER 失败(收尾后可能留下 postgres 僵尸): %v\n", err)
	}

	if port == 0 {
		free, err := pickFreePort(listen)
		if err != nil {
			return err
		}
		port = free
	}

	// 运行目录:每次全新,退出即删。二进制与下载缓存放在它【外面】,
	// 因为 Start() 会 RemoveAll(runtimePath),放里面等于每次都重新下载解压。
	ownRuntime := runtime == ""
	if ownRuntime {
		dir, err := os.MkdirTemp("", "dingwei-pgtest-")
		if err != nil {
			return fmt.Errorf("建运行目录: %w", err)
		}
		runtime = dir
	}
	dataPath := filepath.Join(runtime, "data")

	// 🔴 日志目录(复验 🟡③ 修的泄漏)。
	// embedded-postgres v1.34.0 在 Start() 里调 newSyncedLogger("", …)
	// (embedded_postgres.go:78 → logging.go:18 的 os.CreateTemp("", "embedded_postgres_log")),
	// dir 传空串 ⇒ 落 os.TempDir()(Linux 上认 TMPDIR),而且【用完不删】:
	// 每起一次库就在系统临时目录漏一个约 2.7KB 的文件。
	// 🔴 不能把它放进运行目录:那次 CreateTemp 发生在 embedded_postgres.go:95 的
	// RemoveAll(runtimePath) 【之前】,放进去会被连锅端 —— 句柄还开着、文件已被 unlink,
	// 于是「看着不漏了」而日志也读不到了,两头都不如实。
	// ⇒ 独立目录 + 起库前把 TMPDIR 指过去;退出时两个目录都删,CLEANED 以【两个都没了】为准。
	logDir, err := os.MkdirTemp("", "dingwei-pgtest-log-")
	if err != nil {
		if ownRuntime {
			_ = os.RemoveAll(runtime)
		}
		return fmt.Errorf("建日志目录: %w", err)
	}
	if err := os.Setenv("TMPDIR", logDir); err != nil {
		_ = os.RemoveAll(logDir)
		if ownRuntime {
			_ = os.RemoveAll(runtime)
		}
		return fmt.Errorf("把 TMPDIR 指向日志目录 %s: %w", logDir, err)
	}

	// 二进制与下载缓存的位置:🔴【不】跟着 -runtime 走。
	//
	// 旧版在显式给了 -runtime 时把 cache/binaries 也塞进运行目录,而 Start() 上来就
	// RemoveAll(runtimePath)(embedded_postgres.go:95)⇒ 解压好的二进制每次都被删掉、
	// 每次都要重新解压一遍:实测那条反证用例 8.4s,而默认路径只要 3.0s;
	// -count=50 就是 50 次解压(每次约 250MB 写)。
	// 旧版还有两处自相矛盾:①-cache-root 与 -runtime 同时给时,binaries 仍指向 runtime/binaries,
	// -cache-root 的字面意思被吃掉;②取不到 HOME 时 cache 为空,binaries 会变成相对路径 "extracted"。
	// 现在统一:cache 由 -cache-root 决定(缺省 ~/.embedded-postgres-go),binaries 在 cache 下;
	// -runtime 只管数据目录,以及「留着给人看的现场」—— 那才是它存在的理由。
	cache := cacheRoot
	if cache == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cache = filepath.Join(home, ".embedded-postgres-go")
		}
	}
	binaries := filepath.Join(runtime, "binaries") // cache 取不到时的退路:至少能跑,代价是每次重新解压
	if cache != "" {
		binaries = filepath.Join(cache, "extracted")
	}

	cfg := embeddedpostgres.DefaultConfig().
		Port(uint32(port)).
		Database(db).
		Username(user).
		Password(password).
		RuntimePath(runtime).
		DataPath(dataPath).
		BinariesPath(binaries).
		StartTimeout(startTO).
		Logger(os.Stderr) // 🔴 不进 stdout:stdout 只留协议行
	if cache != "" {
		cfg = cfg.CachePath(cache)
	}
	if version != "" {
		cfg = cfg.Version(embeddedpostgres.PostgresVersion(version))
	}
	// synchronous_commit=on:fanout spool 的既有语义是「COMMIT 返回=已 fsync」
	// (internal/fanout/spool.go:3,synchronous=FULL)。换 PG 后这条持久性判据要有等价物,
	// 故这里显式钉死,不让它随库默认漂。
	cfg = cfg.StartParameters(map[string]string{
		"synchronous_commit": "on",
		"fsync":              "on",
	})

	pg := embeddedpostgres.NewDatabase(cfg)

	// 这三个键在 Start() 之前就报出去(理由见文件头注):失败路径上助手也要能核对残留。
	emit("PORT", fmt.Sprint(port))
	emit("RUNTIME", runtime)
	emit("LOGDIR", logDir)

	startedAt := time.Now()
	if err := pg.Start(); err != nil {
		if ownRuntime {
			_ = os.RemoveAll(runtime)
		}
		if !keepRuntime {
			_ = os.RemoveAll(logDir)
		}
		return fmt.Errorf("起 PG 失败(端口 %d,运行目录 %s): %w", port, runtime, err)
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", user, password, listen, port, db)
	emit("BINARIES", binaries)
	emit("CACHE", cache)
	emit("DSN", dsn)
	emit("START_SECONDS", fmt.Sprintf("%.1f", time.Since(startedAt).Seconds()))
	emit("READY", "1")

	// 两条退出路径:父进程没了(stdin 关闭)或收到信号。
	// stdin 关闭必须算退出条件 —— 否则跑测试的进程被 kill 后,这里会留下一个孤儿 PG。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	stdinClosed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(stdinClosed)
	}()
	select {
	case <-sig:
	case <-stdinClosed:
	}

	stopErr := pg.Stop()
	// 🔴 收尸只许在 Stop() 返回【之后】:库自己起的 pg_ctl/initdb 由它的 cmd.Run() 负责 wait,
	// 中途插手会把退出状态抢走,让库报「wait: no child processes」这种假错。
	reaped := reapOrphans()

	// 自证「TMPDIR 重定向真生效」:库的日志文件确实落在了日志目录里。
	// 这条是给【重定向失效】留的报警器 —— 将来库换实现、或不再认 TMPDIR,
	// 泄漏会悄悄回到系统临时目录,而两道目录 stat 都看不见(它们只核自己那两个目录)。
	redirected := pgLogInDir(logDir)
	if !redirected {
		fmt.Fprintf(os.Stderr, "pgtest: 日志目录 %s 里没有 embedded_postgres_log* ⇒ TMPDIR 重定向没生效,系统临时目录可能又在漏文件\n", logDir)
	}

	// 两个目录各自判、再合成一个 CLEANED:任一个「该删没删」⇒ 0;有任一个按配置该留 ⇒ kept;都删净 ⇒ 1。
	// 运行目录:显式给了 -runtime 就是调用方的东西,本工具不删(与旧行为一致)。
	runtimeState := cleanDir(runtime, keepRuntime || !ownRuntime)
	logState := cleanDir(logDir, keepRuntime) // 日志目录永远是本工具自己建的,只有 -keep-runtime 才留
	cleaned := cleanupVerdict(runtimeState, logState)

	emit("STOPPED", map[bool]string{true: "1", false: "0"}[stopErr == nil])
	if stopErr != nil {
		fmt.Fprintf(os.Stderr, "pgtest: 停 PG 失败: %v\n", stopErr)
	}
	emit("CLEANED", cleaned)
	emit("RUNTIME_STATE", runtimeState)
	emit("LOGDIR_STATE", logState)
	emit("PGLOG_REDIRECTED", map[bool]string{true: "1", false: "0"}[redirected])
	emit("REAPED", fmt.Sprint(reaped))
	if stopErr != nil || cleaned == "0" {
		return fmt.Errorf("收尾未全绿: stop_err=%v cleaned=%s(runtime=%s logdir=%s)",
			stopErr, cleaned, runtimeState, logState)
	}
	return nil
}

// cleanDir 删目录并回报实况:kept=按配置就该留着;cleaned=删了且 stat 不到;dirty=该删却还在。
//
// 🔴 RemoveAll 返回 nil 不等于目录没了(挂载点、权限、别人并发重建都可能),故删完再 stat 一次。
// 这正是旧版把「删没删」写成 cleaned 一个布尔时容易糊过去的地方,拆开之后 CLEANED=0 能指到具体哪一个。
func cleanDir(dir string, keep bool) string {
	if dir == "" || keep {
		return "kept"
	}
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(os.Stderr, "pgtest: 删 %s 失败: %v\n", dir, err)
		return "dirty"
	}
	if _, err := os.Stat(dir); err == nil {
		fmt.Fprintf(os.Stderr, "pgtest: RemoveAll 返回成功但目录仍在 %s\n", dir)
		return "dirty"
	}
	return "cleaned"
}

// cleanupVerdict 把各目录的实况合成一个对外值,保持与旧协议兼容(1/kept/0)。
func cleanupVerdict(states ...string) string {
	kept := false
	for _, s := range states {
		if s == "dirty" {
			return "0"
		}
		if s == "kept" {
			kept = true
		}
	}
	if kept {
		return "kept"
	}
	return "1"
}

// pgLogInDir 判日志目录里有没有库起的那个日志文件(名字前缀由 logging.go:18 决定)。
func pgLogInDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "embedded_postgres_log") {
			return true
		}
	}
	return false
}

// pickFreePort 向内核要一个空闲端口。
//
// ⚠️ 取到后要先关掉监听再交给 PG,这中间存在【理论上的】抢占窗口。
// 不做重试:端口被抢时 PG 启动会直接失败并报端口号,比静默重试更容易定位。
func pickFreePort(listen string) (uint, error) {
	l, err := net.Listen("tcp", net.JoinHostPort(listen, "0"))
	if err != nil {
		return 0, fmt.Errorf("挑空闲端口: %w", err)
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("挑空闲端口: 地址类型不是 TCP")
	}
	return uint(addr.Port), nil
}

func emit(key, value string) {
	fmt.Fprintf(os.Stdout, "%s=%s\n", key, value)
}
