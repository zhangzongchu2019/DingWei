// mcs-demo —— 带任务面板的演示版(张宗楚 07-24;低优先演示件,不在 P3 评审范围)。
//
// 一进程整机演示:文件库 Board+调度守护(loopback 模拟工人)+收件面(五闸真拦)
// +鉴权盖章 mock+消息回放+极简只读面板(127.0.0.1)。
// 模拟件③【人工操作入口】=op 子命令(应急夺权/单点急停/升格/豁免,直调 guarded 原语,全留账)。
//
// 用法:
//
//	mcs-demo run    [-db demo.db] [-listen 127.0.0.1:8646] [-replay file.jsonl] [-work-delay 2s]
//	mcs-demo op takeover -db demo.db -owner zzc-zzc-1 -to zzc-neworch-1 -epoch 1 -idem k1 [-reason ...]
//	mcs-demo op override -db demo.db -owner zzc-zzc-1 -target n3 -directive pause -epoch 1 -state running -idem k2
//	mcs-demo op promote  -db demo.db -task t-demo -role designer
//	mcs-demo op exempt   -db demo.db -task t-demo -who zzc-qa-1 -reason "阶段性第3席"
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/mcs/board"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/demo"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/hubdriver"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/mcsd"
	"github.com/zhangzongchu2019/dingwei/internal/mcs/pool"
)

const (
	orchPrincipal  = "zzc-orchd-1" // 调度守护身份(演示)
	ownerPrincipal = "zzc-zzc-1"   // 业主(演示=张宗楚测试会话的盖章身份)
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "run":
		runDemo(os.Args[2:])
	case "op":
		runOp(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: mcs-demo run|op …(见文件头注释)")
	os.Exit(2)
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcs-demo:", err)
		os.Exit(1)
	}
}

// ---- run:整机演示 ----

func runDemo(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	db := fs.String("db", "mcs-demo.db", "Board 库文件")
	listen := fs.String("listen", "127.0.0.1:8646", "面板地址(强制回环)")
	replayFile := fs.String("replay", "", "回放 JSONL(空=不回放)")
	replayEvery := fs.Duration("replay-interval", 800*time.Millisecond, "回放逐条间隔")
	workDelay := fs.Duration("work-delay", 2*time.Second, "模拟工人单节点耗时")
	workers := fs.String("workers", "", "M2:真工人会话全名逗号表(设=替换模拟件,经 hub 派发)")
	spool := fs.String("receipt-spool", "", "M2:回执 spool 文件(CLI 收 mcs_receipt_v1 追加,demo 尾随入账)")
	die(fs.Parse(args))

	host, _, err := net.SplitHostPort(*listen)
	die(err)
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		die(fmt.Errorf("面板只允许回环地址(只读演示铁律),got %s", *listen))
	}

	var drv pool.DriverAdapter
	var loop *mcsd.LoopbackDriver
	if *workers != "" { // M2:真工人模式(替换 demo 模拟件)
		home, _ := os.UserHomeDir()
		drv = &hubdriver.Adapter{
			Registry: strings.Split(*workers, ","),
			Send:     &hubdriver.SendPy{},
			Online:   &hubdriver.OnlineList{Path: filepath.Join(home, ".dingwei", "devo48.DingWeiOnlineSessions.list")},
			From:     "zzc-devo48-2642",
		}
	} else {
		loop = &mcsd.LoopbackDriver{Behavior: mcsd.LoopbackBehavior{WorkDelay: *workDelay}}
		drv = loop
	}
	d, err := mcsd.New(mcsd.Config{
		BoardPath: *db, BoardID: "board1", Owner: orchPrincipal,
		Pool: pool.Config{Min: 2, Max: 3},
	}, drv, nil)
	die(err)
	if loop != nil {
		loop.Bind(d)
	}
	if *spool != "" { // 回执 spool 尾随:CLI 会话收到回执落此文件,一行一 JSON
		go tailReceipts(d, *spool)
	}

	// 鉴权盖章 mock(模拟件②):显式登记会话→身份章;未登记会话盖不了章=进不来。
	auth := demo.NewMockAuth()
	auth.Bind("feishu-zzc", ownerPrincipal, true) // 业主(演示)
	auth.Bind("sess-orch", orchPrincipal, false)  // 调度者控制台
	auth.Bind("sess-des", "zzc-des-1", false)
	auth.Bind("sess-impl", "zzc-impl-1", false)
	auth.Bind("sess-tst", "zzc-tst-1", false)

	// C6 演示配置:开应急面+业主集(生产=P5 原子共切,这里显式演示态)
	d.B.SetEmergencyEnabled(true)
	d.B.SetOwnerPrincipals(auth.Owners())

	// 收件面(五闸真拦):调度者小组+业主集+门方登记
	rc := mcsd.NewReceiver(d.B, map[string]bool{orchPrincipal: true}, auth.Owners(), true)
	rc.SetGateKeeper("t-demo", "seg1", "zzc-tst-1")

	die(seed(d))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	if *replayFile != "" {
		go func() {
			f, err := os.Open(*replayFile)
			if err != nil {
				fmt.Fprintln(os.Stderr, "回放打开失败:", err)
				return
			}
			defer f.Close()
			time.Sleep(1 * time.Second) // 等守护起来,面板能看到逐条进来
			_, err = demo.Replay(ctx, f, auth, rc, func(r demo.ReplayResult) {
				if !r.Stamped {
					fmt.Printf("[回放#%d] 盖章拒:%s(session=%s)\n", r.Line, r.StampErr, r.Rec.Session)
				} else {
					fmt.Printf("[回放#%d] %s op=%q → %s(闸%d)%s\n", r.Line, r.Rec.Session, r.Rec.MetaOp,
						r.Decision.Verdict, r.Decision.Gate, suffix(r))
				}
				time.Sleep(*replayEvery) // 逐条放,面板可看到事件流入
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, "回放中断:", err)
			}
		}()
	}

	srv := &http.Server{Addr: *listen, Handler: demo.PanelHandler(d.B)}
	go func() { <-ctx.Done(); _ = srv.Close(); _ = d.Shutdown() }()
	fmt.Printf("演示面板: http://%s  (Ctrl-C 退出;库=%s)\n", *listen, *db)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		die(err)
	}
}

func suffix(r demo.ReplayResult) string {
	var parts []string
	if r.Decision.EngineOp != "" {
		parts = append(parts, "引擎op="+r.Decision.EngineOp)
	}
	if r.Decision.Kind != "" {
		parts = append(parts, "入账="+r.Decision.Kind)
	}
	if r.Decision.NeedsLLM {
		parts = append(parts, "待LLM判读(不授权)")
	}
	if r.Decision.Suspect {
		parts = append(parts, "疑似控制→要求结构化重投")
	}
	if r.Decision.Reason != "" {
		parts = append(parts, "拒因="+r.Decision.Reason)
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, " ") + "]"
}

// seed 种演示任务:四角色三元组+6 节点 DAG(1→2/3 并行→4;5→6 链)。幂等(任务已在=跳过)。
func seed(d *mcsd.Daemon) error {
	ctx := context.Background()
	var n int
	if err := d.B.DB().QueryRow(`SELECT COUNT(*) FROM task WHERE task_id='t-demo'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	rb := func(w, cli, llm string) board.RoleBinding { return board.RoleBinding{Who: w, CLI: cli, LLM: llm} }
	ra := board.RoleAssignments{
		Designer:    rb("zzc-des-1", "claude", "claude-opus-4-8"),
		Implementer: rb("zzc-impl-1", "claude", "claude-fable-5"),
		Tester:      rb("zzc-tst-1", "codex", "gpt-5.6"),
		QAOwner:     rb("zzc-qa-1", "human", "human"),
	}
	if err := d.B.CreateTask(ctx, "t-demo", d.Cfg.BoardID, "演示任务:多CLI协作发布", ra, d.ID); err != nil {
		return err
	}
	type nd struct {
		id     string
		seq    int
		status string
		deps   []string
	}
	for _, x := range []nd{
		{"n1-设计定稿", 1, "ready", nil},
		{"n2-实现主链", 2, "pending", []string{"n1-设计定稿"}},
		{"n3-实现面板", 3, "pending", []string{"n1-设计定稿"}},
		{"n4-集成测试", 4, "pending", []string{"n2-实现主链", "n3-实现面板"}},
		{"n5-文档", 5, "ready", nil},
		{"n6-验收", 6, "pending", []string{"n4-集成测试", "n5-文档"}},
	} {
		if err := d.B.AddNode(ctx, x.id, "t-demo", x.seq, x.status, x.deps); err != nil {
			return err
		}
	}
	return nil
}

// ---- op:人工操作入口(模拟件③;直调 guarded 原语,全留账) ----

func runOp(args []string) {
	if len(args) < 1 {
		usage()
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("op "+sub, flag.ExitOnError)
	db := fs.String("db", "mcs-demo.db", "Board 库文件")
	owner := fs.String("owner", ownerPrincipal, "业主 principal(takeover/override)")
	to := fs.String("to", "", "takeover: 新调度者身份")
	target := fs.String("target", "", "override: 目标节点")
	directive := fs.String("directive", "pause", "override: 白名单指令")
	state := fs.String("state", "", "override: 目标节点当前态(expected)")
	epoch := fs.Int64("epoch", 0, "expected epoch(先看面板/库)")
	idem := fs.String("idem", "", "幂等键(一决定一键)")
	task := fs.String("task", "t-demo", "promote/exempt: 任务")
	role := fs.String("role", "", "promote: 轻角色(designer|qa_owner)")
	who := fs.String("who", "", "exempt: 豁免对象")
	reason := fs.String("reason", "演示操作", "理由(落账)")
	die(fs.Parse(rest))

	b, err := board.Open(*db, nil)
	die(err)
	defer b.Close()
	_, _ = b.DB().Exec(`PRAGMA busy_timeout=3000`) // 与运行中守护共库(WAL),写忙等 3s

	ctx := context.Background()
	switch sub {
	case "takeover":
		b.SetEmergencyEnabled(true)
		b.SetOwnerPrincipals(map[string]bool{*owner: true})
		oc, err := b.OwnerTakeover(ctx, "board1", board.OwnerTakeoverReq{
			OwnerPrincipal: *owner, ToOwner: *to, ExpectedEpoch: *epoch, IdempotencyID: *idem, Reason: *reason})
		die(err)
		fmt.Println("owner_takeover →", oc)
	case "override":
		b.SetEmergencyEnabled(true)
		b.SetOwnerPrincipals(map[string]bool{*owner: true})
		oc, err := b.OwnerOverride(ctx, board.OwnerOverrideReq{
			OwnerPrincipal: *owner, Target: *target, Directive: *directive,
			ExpectedEpoch: *epoch, ExpectedState: *state, IdempotencyID: *idem, Reason: *reason})
		die(err)
		fmt.Println("owner_override →", oc)
	case "promote", "exempt":
		id, err := currentOrchestrator(b)
		die(err)
		if sub == "promote" {
			oc, err := b.PromoteExecSeat(ctx, *task, *role, id)
			die(err)
			fmt.Println("promote_exec_seat →", oc, "(rejected=第3席,须换席或先 exempt)")
		} else {
			oc, err := b.GrantRoleExemption(ctx, *task, *who, *reason, id)
			die(err)
			fmt.Println("role_exemption →", oc)
		}
	default:
		usage()
	}
}

// currentOrchestrator 调度者控制台身份=当前 orchestrator 行(owner+epoch)。
func currentOrchestrator(b *board.Board) (board.Identity, error) {
	var id board.Identity
	err := b.DB().QueryRow(`SELECT owner, epoch FROM orchestrator WHERE board_id='board1'`).
		Scan(&id.Owner, &id.Epoch)
	return id, err
}

// tailReceipts M2 回执桥:尾随 spool 文件(一行一 mcs_receipt_v1 JSON),入 daemon guarded 账。
// 解析失败/未知 kind 显式打日志跳过(入口闭合),不 panic 不静默。
func tailReceipts(d *mcsd.Daemon, path string) {
	var off int64
	for {
		time.Sleep(500 * time.Millisecond)
		f, err := os.Open(path)
		if err != nil {
			continue // 文件未建=还没有回执
		}
		if _, err := f.Seek(off, 0); err != nil {
			f.Close()
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			off += int64(len(sc.Bytes()) + 1)
			if line == "" || !strings.HasPrefix(line, "{") {
				continue
			}
			if err := d.HandleHubReceipt(context.Background(), []byte(line)); err != nil {
				log.Printf("spool 回执拒: %v", err)
			}
		}
		f.Close()
	}
}
