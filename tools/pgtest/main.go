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
// 收尾还收掉 pg_ctl 留下的孤儿 postmaster(见 becomeSubreaper),不在进程表里留僵尸;
// 收尸的终止条件是【已无后代】而不是「猜一个静默窗」(见 reap_loop.go,裁定 ruling-2026-10-01T174744Z 二)。
// 另有两条兜底:父进程被过继即自行收尾(watchdog.go)、启动时按前缀清掉上一次被杀留下的现场(stale.go)。
//
// 协议行一览(助手 internal/store/pgtest 依赖这些键,少一个都会被判据发现):
//
//	PORT RUNTIME LOGDIR —— 在 Start() 【之前】就报:起库失败或被助手 kill 时,
//	                      助手至少知道该去核哪两个目录,不会抓瞎。
//	BINARIES CACHE DSN START_SECONDS READY=1
//	STOPPED=1/0 CLEANED=1/kept/0 RUNTIME_STATE=… LOGDIR_STATE=…
//	PGLOG_REDIRECTED=1/0 REAPED=n(累计:主扫＋收尾那一遍) REAP_SECONDS=…(收尸自己花了多久,自证不靠外推)
//	REAP_LEFTOVER=n(只在时间上限触发时出现:还有几个后代没退出)
//	STALE_CLEANED=n STALE_LIVE_PG=n STALE_SKIPPED=n STALE_PID_REUSED=n
//	                      (启动时按前缀清上一次被杀留下的现场;见 stale.go。
//	                       PID_REUSED＝pid 文件里的 PID 已被别的进程复用 ⇒ 只删目录、不杀进程)
//	PARENT_GONE=1(只在「父进程被过继」这条退出路径上出现;见 watchdog.go)
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
		cacheRoot   = flag.String("cache-root", "", "下载与解压缓存根(二进制解压在其下 extracted/);缺省依次用 HOME、用户缓存或系统临时目录;与 -runtime 无关")
		runtime     = flag.String("runtime", "", "本次运行目录(数据目录在其下);空=新建临时目录,退出时删除")
		db          = flag.String("db", "hubtest", "库名")
		user        = flag.String("user", "hubtest", "用户名")
		password    = flag.String("password", "hubtest", "口令(临时库,只为起得来,不是机密)")
		startTO     = flag.Duration("start-timeout", 5*time.Minute, "启动超时;首次要下载二进制,故给足")
		keepRuntime = flag.Bool("keep-runtime", false, "退出时【不】删运行目录与日志目录(排障用;默认删)")
		staleCut    = flag.Duration("stale-cutoff", staleCutoff, "启动时清理【上一次被杀留下】的残留目录的门槛:目录与其中的 postmaster 都要比这个时长更旧才动;<=0 关闭这项清理")
	)
	flag.Parse()

	if err := run(*portFlag, *listen, *version, *cacheRoot, *runtime, *db, *user, *password, *startTO, *keepRuntime, *staleCut); err != nil {
		fmt.Fprintf(os.Stderr, "pgtest: %v\n", err)
		os.Exit(1)
	}
}

// resolveCacheRoot never delegates cache placement to embedded-postgres' relative
// .embedded-postgres-go default. Explicit paths remain supported; otherwise the
// preference is the user's home, the OS user-cache directory, then a stable
// directory under the system temp root.
func resolveCacheRoot(explicit string) (string, error) {
	root := explicit
	if root == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			root = filepath.Join(home, ".embedded-postgres-go")
		}
	}
	if root == "" {
		if cache, err := os.UserCacheDir(); err == nil && cache != "" {
			root = filepath.Join(cache, ".embedded-postgres-go")
		}
	}
	if root == "" {
		root = filepath.Join(os.TempDir(), "dingwei-pgtest-cache")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return absolute, nil
}

func run(port uint, listen, version, cacheRoot, runtime, db, user, password string, startTO time.Duration, keepRuntime bool, staleCut time.Duration) error {
	// 🔴 第一件事:忽略 SIGPIPE。必须在【任何一次写 stdout/stderr 之前】,包括下面那句警告。
	// 理由与实测两场读数见 sigpipe_unix.go 的头注(一句话:父进程被杀 ⇒ 两头管道都破裂 ⇒
	// Go 运行时对 fd 1/2 的 SIGPIPE 走默认处置直接打死本进程,于是收尾死在半路:
	// 库停了,但目录没删、postmaster 没收到 ⇒ 留残留＋留一个 PID 1 永不收的僵尸)。
	ignoreSigPipe()

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

	// 启动时先按前缀清掉【上一次被杀而没清掉】的现场(孤儿 postmaster ＋ 一对目录)。
	// 判据是「按前缀 ＋ 够旧」,不是「按前缀」—— 只按前缀会把正在跑的另一次连锅端;
	// 门槛与「活 PG 还要再判一次够不够旧」的理由见 stale.go。
	stale := cleanStale(os.TempDir(), stalePrefixes, time.Now(), staleCut)
	emit("STALE_CLEANED", fmt.Sprint(stale.total()))
	emit("STALE_LIVE_PG", fmt.Sprint(len(stale.SkippedLive)))
	emit("STALE_SKIPPED", fmt.Sprint(len(stale.SkippedNew)))
	emit("STALE_PID_REUSED", fmt.Sprint(len(stale.Reused)))
	if n := len(stale.Terminated); n > 0 {
		fmt.Fprintf(os.Stderr, "pgtest: 终止了 %d 个孤儿 postmaster 并删掉其目录: %v\n", n, stale.Terminated)
	}
	// 🔴 这一档必须出声:它说的是「pid 文件里那个数字已经指向别的进程了」——
	// 上一版没有归属核对,在这里会直接 SIGTERM 那个无关进程(复验方已现场复现)。
	if n := len(stale.Reused); n > 0 {
		fmt.Fprintf(os.Stderr, "pgtest: 有 %d 个残留目录的 postmaster.pid 里那个 PID 【不是本目录的 postmaster】(已被别的进程复用),只删目录、不碰进程: %v\n", n, stale.Reused)
	}
	if n := len(stale.SkippedLive); n > 0 {
		fmt.Fprintf(os.Stderr, "pgtest: 有 %d 个残留目录只报不动(PG 还活着但【不够旧】,或 PID 归属判不出): %v\n", n, stale.SkippedLive)
	}
	if n := len(stale.Failed); n > 0 {
		fmt.Fprintf(os.Stderr, "pgtest: 有 %d 个残留目录删不掉(不假装清过): %v\n", n, stale.Failed)
	}

	if port == 0 {
		free, err := pickFreePort(listen)
		if err != nil {
			return err
		}
		port = free
	}
	cache, err := resolveCacheRoot(cacheRoot)
	if err != nil {
		return fmt.Errorf("解析绝对 PG 缓存目录: %w", err)
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
	// -cache-root 的字面意思被吃掉;②取不到 HOME 时 cache 为空且不调用 CachePath,
	// embedded-postgres 就回退到当前工作目录下的相对 .embedded-postgres-go。
	// 现在统一:cache 由 -cache-root 或 resolveCacheRoot 决定(缺省依次 HOME、用户缓存、
	// 系统临时目录),binaries 在 cache 下;两者始终是显式绝对路径。
	// -runtime 只管数据目录,以及「留着给人看的现场」—— 那才是它存在的理由。
	binaries := filepath.Join(cache, "extracted")

	cfg := embeddedpostgres.DefaultConfig().
		Port(uint32(port)).
		Database(db).
		Username(user).
		Password(password).
		RuntimePath(runtime).
		DataPath(dataPath).
		CachePath(cache).
		BinariesPath(binaries).
		StartTimeout(startTO).
		Logger(os.Stderr) // 🔴 不进 stdout:stdout 只留协议行
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

	// 🔴 换端口必须【重建】一个 Database 实例,不能拿同一个实例重试 —— 但理由【不是】started 标志:
	// 端口只在构造时进实例:NewDatabase(config) 把 Config 存进私有字段 config 的【副本】
	// (embedded_postgres.go:36,44,57),而 Config.Port 是值接收者、返回一个新 Config(config.go:57),
	// *EmbeddedPostgres 又只导出 Start/Stop ⇒ 已建好的实例【没有任何改端口的口子】,换号只能重新 NewDatabase。
	//
	// 🔴 订正(复验 R-G2-REVIEW-QUEUE-50 ② 指出;已在 v1.34.0 源码逐行核过,行号均指该版):
	// 旧注写「Start() 第一行就判 ep.started,第二次只会拿到 ErrServerAlreadyStarted」—— 不成立。
	// started 只在【成功路径】置 true(:127,在 startPostgres :119 与日志 flush 之后),
	// 而两条可重试的端口签名都在 :127 之前就返回了:① ensurePortAvailable :74(文案在 :247)、
	// ② pg_ctl 起失败 :119(错误里包着 PG 日志原文)。这两种失败时 started 仍是 false,
	// 重用同一实例并不会拿到 ErrServerAlreadyStarted。
	// (唯一「出错却把 started 留在 true」的是 :130 createDatabase 与 :139 healthCheck 两条失败路径,
	//  它们都不是端口签名、按 isPortTaken 的窄判据不重试 ⇒ 永远走不到第二次 Start。)
	//
	// 🔴 顺带一条对签名②有用的事实:Start() 每次都先 os.RemoveAll(ep.config.runtimePath)(:95)
	// 再重新解压二进制,所以每次重试天然从干净目录开始,不会捡上一次「起到一半」的残留。
	var pg *embeddedpostgres.EmbeddedPostgres
	startOnce := func(p uint) error {
		pg = embeddedpostgres.NewDatabase(cfg.Port(uint32(p)))
		return pg.Start()
	}

	// 这三个键在 Start() 之前就报出去(理由见文件头注):失败路径上助手也要能核对残留。
	emit("PORT", fmt.Sprint(port))
	emit("RUNTIME", runtime)
	emit("LOGDIR", logDir)

	// 🔴 START_SECONDS 覆盖【全部尝试】的耗时,不是最后一次的:这个数的意思是
	// "拿到一个能用的库花了多久",重试真发生过就该体现出来(PORT_RETRY 同时也在,不会误读)。
	startedAt := time.Now()
	// 端口被抢时换端口重试(上限 portAttempts;只认那两种签名,别的失败立刻原样交回 ——
	// 判据与理由见 portretry.go)。
	// 🔴 notify 里【重报一次 PORT】:消费方 internal/store/pgtest 的协议解析是同键后写覆盖
	// (pgtest.go 的 p.kv[key] = value),故它最终读到的是成功那次的端口;
	// DSN 本来就在起库成功之后才 emit,两处不会打架。
	port, err = startWithPortRetry(port, portAttempts, startOnce,
		func() (uint, error) { return pickFreePort(listen) },
		func(from, to uint) {
			emit("PORT_RETRY", fmt.Sprintf("%d->%d", from, to))
			emit("PORT", fmt.Sprint(to))
		})
	if err != nil {
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

	// 三条退出路径:stdin 关闭、收到信号、父进程被过继(看门狗)。
	// stdin 关闭必须算退出条件 —— 否则跑测试的进程被 kill 后,这里会留下一个孤儿 PG。
	// 🔴 看门狗是【第二条】独立的父死判据,不替代 stdin:两者的失效形状不同
	//    (stdin 依赖它是管道;看门狗依赖父进程不是 PID 1)。为什么不用 PR_SET_PDEATHSIG
	//    以及各自覆盖不到什么,写在 watchdog.go 的头注里,不在这儿复述。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	stdinClosed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(stdinClosed)
	}()
	parentGone := watchParent(os.Getppid(), parentPollInterval, os.Getppid, time.Sleep)
	select {
	case <-sig:
	case <-stdinClosed:
	case <-parentGone:
		emit("PARENT_GONE", "1")
	}

	stopErr := pg.Stop()
	// 🔴 收尸只许在 Stop() 返回【之后】:库自己起的 pg_ctl/initdb 由它的 cmd.Run() 负责 wait,
	// 中途插手会把退出状态抢走,让库报「wait: no child processes」这种假错。
	// 🔴 「只收一次」不够:Stop() 返回时 postmaster 的那几个子进程可能还没退出,
	// 漏收的孤儿过继给 PID 1(容器里是 `sleep infinity`,永不收尸)⇒ 留成永久僵尸,
	// 被 internal/store/pgtest 的残留格读成「本次新增」而**把绿判成红**(实测 ≈2.7% 的起停)。
	// ⇒ 反复收,终止条件＝【已无后代】(/proc 数得到就按它,数不到退回 wait4 的 ECHILD),
	//   时间上限只作兜底,且触发时把「还有几个没退出」报出去。详见 reap_loop.go 与它的反测。
	sweepMain := reapUntilGone(reapOrphansOnce, descendantCount, time.Sleep, time.Now, reapInterval, reapTimeout)
	reportReap("主扫", sweepMain)
	reaped := sweepMain.Reaped
	reapSeconds := sweepMain.Elapsed

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
	// 收尾再扫一遍:删目录那几步又过去了几十毫秒,晚退出的孤儿这时候才到齐。
	// 终止条件与主扫同一个(【已无后代】),故不需要「比主扫更短的静默窗」这种猜测参数。
	sweepFinal := reapUntilGone(reapOrphansOnce, descendantCount, time.Sleep, time.Now, reapInterval, reapTimeout)
	reportReap("收尾", sweepFinal)
	reaped += sweepFinal.Reaped
	reapSeconds += sweepFinal.Elapsed

	emit("REAPED", fmt.Sprint(reaped))
	emit("REAP_SECONDS", fmt.Sprintf("%.3f", reapSeconds.Seconds()))
	// REAP_LEFTOVER 只在【时间上限触发】时出现:它是排障坐标(还有几个后代没退出),
	// 不是失败判据 —— 收不到就是收不到,残留格照旧由 internal/store/pgtest 独立判。
	if sweepMain.Capped || sweepFinal.Capped {
		left := sweepFinal.Remaining
		if sweepMain.Remaining > left {
			left = sweepMain.Remaining
		}
		emit("REAP_LEFTOVER", fmt.Sprint(left))
	}
	if stopErr != nil || cleaned == "0" {
		return fmt.Errorf("收尾未全绿: stop_err=%v cleaned=%s(runtime=%s logdir=%s)",
			stopErr, cleaned, runtimeState, logState)
	}
	return nil
}

// reportReap 把一次收尸的【非正常】结局打到 stderr(正常结局不打,免得每次起停都刷一屏)。
//
// 🔴 上限触发一定要出声:静默收手正是上一版的毛病 —— 输出里看不出「真没了」与
//
//	「等不下去了」的区别,于是「变慢」这件事只能靠两棵树对跑整包用例才敢下结论。
func reportReap(stage string, r reapReport) {
	if r.Err != nil {
		fmt.Fprintf(os.Stderr, "pgtest: %s收尸时报错(不吞,如实报): %v\n", stage, r.Err)
	}
	if r.Capped {
		fmt.Fprintf(os.Stderr, "pgtest: %s收尸到时间上限 %v 才收手:还有 %d 个后代没退出(共收 %d 条、%d 轮、耗时 %v)\n",
			stage, reapTimeout, r.Remaining, r.Reaped, r.Rounds, r.Elapsed)
	}
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
// ⚠️ 取到后要先关掉监听再交给 PG,这中间存在抢占窗口(实测会撞上,不是"理论上的":
// 两个 pgtest 消费方并行时 port 46399 被抢,红的是【与端口无关的用例】)。
// 🔴 本函数【自己不重试】—— 重试在调用侧 startWithPortRetry,理由是重试要能换端口、
// 要能出声(PORT_RETRY)、还要能分辨"端口被抢"与"真失败",那三件都不是挑端口这一步的职责。
// 原先这里写的"不做重试…比静默重试更容易定位"已被推翻:静默由 PORT_RETRY 解决,
// 而"让无关用例红"比"慢三轮"坏得多(排障的人会去怀疑自己刚改的代码)。详见 portretry.go 头注。
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
