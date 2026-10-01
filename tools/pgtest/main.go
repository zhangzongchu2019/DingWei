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
// 停完自己删运行目录(库的 Stop() 不删目录),并打印 CLEANED=1/0 ——
// 「随起随清」的后半句由本工具负责,且【自证】(删不掉就报 CLEANED=0,不假装成功)。
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

func main() {
	var (
		portFlag    = flag.Uint("port", 0, "监听端口;0=自动挑一个空闲端口")
		listen      = flag.String("listen", "127.0.0.1", "监听地址(只应留在环回)")
		version     = flag.String("version", "", "PostgreSQL 版本串;空=库默认(当前 V18)")
		cacheRoot   = flag.String("cache-root", "", "下载与解压缓存根;空=库默认($HOME/.embedded-postgres-go)")
		runtime     = flag.String("runtime", "", "本次运行目录(数据目录在其下);空=新建临时目录,退出时删除")
		db          = flag.String("db", "hubtest", "库名")
		user        = flag.String("user", "hubtest", "用户名")
		password    = flag.String("password", "hubtest", "口令(临时库,只为起得来,不是机密)")
		startTO     = flag.Duration("start-timeout", 5*time.Minute, "启动超时;首次要下载二进制,故给足")
		keepRuntime = flag.Bool("keep-runtime", false, "退出时【不】删运行目录(排障用;默认删)")
	)
	flag.Parse()

	if err := run(*portFlag, *listen, *version, *cacheRoot, *runtime, *db, *user, *password, *startTO, *keepRuntime); err != nil {
		fmt.Fprintf(os.Stderr, "pgtest: %v\n", err)
		os.Exit(1)
	}
}

func run(port uint, listen, version, cacheRoot, runtime, db, user, password string, startTO time.Duration, keepRuntime bool) error {
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

	binaries := filepath.Join(runtime, "binaries")
	cache := cacheRoot
	if !ownRuntime {
		// 显式给了 runtime 就一切放它下面(含二进制),行为可预期。
		cache = filepath.Join(runtime, "cache")
	} else if cache == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cache = filepath.Join(home, ".embedded-postgres-go")
		}
		binaries = filepath.Join(cache, "extracted")
	} else {
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
	startedAt := time.Now()
	if err := pg.Start(); err != nil {
		if ownRuntime {
			_ = os.RemoveAll(runtime)
		}
		return fmt.Errorf("起 PG 失败(端口 %d,运行目录 %s): %w", port, runtime, err)
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", user, password, listen, port, db)
	emit("PORT", fmt.Sprint(port))
	emit("RUNTIME", runtime)
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
	cleaned := "1"
	if !keepRuntime && ownRuntime {
		if err := os.RemoveAll(runtime); err != nil {
			cleaned = "0"
			fmt.Fprintf(os.Stderr, "pgtest: 删运行目录失败 %s: %v\n", runtime, err)
		} else if _, err := os.Stat(runtime); err == nil {
			// RemoveAll 报成功而目录还在 ⇒ 不算清干净,如实报 0。
			cleaned = "0"
			fmt.Fprintf(os.Stderr, "pgtest: RemoveAll 返回成功但目录仍在 %s\n", runtime)
		}
	} else if keepRuntime || !ownRuntime {
		cleaned = "kept"
	}
	emit("STOPPED", map[bool]string{true: "1", false: "0"}[stopErr == nil])
	if stopErr != nil {
		fmt.Fprintf(os.Stderr, "pgtest: 停 PG 失败: %v\n", stopErr)
	}
	emit("CLEANED", cleaned)
	if stopErr != nil || cleaned == "0" {
		return fmt.Errorf("收尾未全绿: stop_err=%v cleaned=%s", stopErr, cleaned)
	}
	return nil
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
