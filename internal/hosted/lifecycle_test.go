package hosted

// 生命周期用例（复验返修后）：全部跨系统调用记进【同一条事件日志】，逐项断言完整序列——
// 复验（tosonnet55tc2k REVIEW-G4-UNFREEZE-BATCH）指出旧写法把身份调用与节点指令记在两个切片，
// 跨系统先后无从判定（删闸不红）。本文件的断言 shape 吸收其场景源码（zzLog 模式）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// evLog 单一事件日志（所有假件写同一条）。
type evLog struct {
	mu sync.Mutex
	ev []string
}

func (l *evLog) add(s string) { l.mu.Lock(); l.ev = append(l.ev, s); l.mu.Unlock() }
func (l *evLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ev...)
}
func (l *evLog) reset() { l.mu.Lock(); l.ev = nil; l.mu.Unlock() }

type lcSusp struct {
	log       *evLog
	failOn    string // "suspend"/"resume"/"reissue"
	suspended bool   // 凭据当前是否暂停：与真 identity 一样，already 反映「本来就是这个态」
}

func (f *lcSusp) SuspendHostedAgent(ctx context.Context, id, by string) (bool, error) {
	f.log.add("idp:suspend")
	if f.failOn == "suspend" {
		return false, errors.New("injected suspend")
	}
	already := f.suspended
	f.suspended = true
	return already, nil
}
func (f *lcSusp) ResumeHostedAgent(ctx context.Context, id, by string) (bool, error) {
	f.log.add("idp:resume")
	if f.failOn == "resume" {
		return false, errors.New("injected resume")
	}
	already := !f.suspended
	f.suspended = false
	return already, nil
}
func (f *lcSusp) ReissueHostedToken(ctx context.Context, id, by string) (EnrollToken, error) {
	f.log.add("idp:reissue")
	if f.failOn == "reissue" {
		return EnrollToken{}, errors.New("injected reissue")
	}
	return EnrollToken{ApplicationID: "app2", PollToken: "tok2", ExpiresAt: time.Now().Add(2 * time.Hour)}, nil
}

type lcIdp struct {
	IdentityIssuer
	log    *evLog
	failOn string
}

func (z *lcIdp) RetireHostedAgent(ctx context.Context, id, by string) (bool, error) {
	z.log.add("idp:retire")
	if z.failOn == "retire" {
		return false, errors.New("injected retire")
	}
	return z.IdentityIssuer.RetireHostedAgent(ctx, id, by)
}

// AbortHostedAgent 新建链回收：吊销那一步照记 idp:retire（与 RetireHostedAgent 同一事件），失败注入同口径。
func (z *lcIdp) AbortHostedAgent(ctx context.Context, id, by string) (bool, error) {
	z.log.add("idp:retire")
	if z.failOn == "retire" {
		return false, errors.New("injected retire")
	}
	return z.IdentityIssuer.AbortHostedAgent(ctx, id, by)
}

type lcGw struct {
	log    *evLog
	failOn string // 命中 ⇒ 节点在线、报告失败
	downOn string // 命中 ⇒ 发不到节点（传输错误：节点不通）
	sent   []NodeCommand
}

func (g *lcGw) Send(ctx context.Context, nodeID string, c NodeCommand) (NodeResult, error) {
	g.log.add("node:" + c.Type)
	g.sent = append(g.sent, c)
	if c.Type == g.downOn {
		return NodeResult{}, errors.New("injected: node unreachable")
	}
	if c.Type == g.failOn {
		return NodeResult{OK: false, Error: "injected"}, nil
	}
	return NodeResult{OK: true}, nil
}

type lcBk struct {
	log    *evLog
	failOn bool
	ctxErr error
}

func (b *lcBk) BackupSeatVolume(ctx context.Context, nodeID, seatID string) (string, error) {
	b.log.add("backup")
	b.ctxErr = ctx.Err()
	if b.failOn {
		return "", errors.New("injected backup")
	}
	return "bk://" + nodeID + "/" + seatID + "/v1", nil
}

type cancellingLifecycleGateway struct {
	inner  *lcGw
	cancel context.CancelFunc
}

func (g *cancellingLifecycleGateway) Send(ctx context.Context, nodeID string, cmd NodeCommand) (NodeResult, error) {
	res, err := g.inner.Send(ctx, nodeID, cmd)
	if cmd.Type == "stop_seat" {
		g.cancel() // model the browser disconnecting after cleanup has begun
	}
	return res, err
}

// mkLifecycle 建在线席位＋全日志假件（创建期的节点指令清零后再进用例）。
func mkLifecycle(t *testing.T) (*Lifecycle, *evLog, *lcSusp, *lcGw, *lcBk, *Store, string) {
	t.Helper()
	o, idp, _, s := mkOrch(t)
	out, err := o.Create(context.Background(), CreateRequest{
		OwnerLC: "zzc", By: "zzc", Name: "life1", GroupCode: "g1",
		NodeID: "auto", CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	o.RunJob(context.Background(), out.AgentID)
	if err := o.MarkOnline(context.Background(), out.AgentID); err != nil { // Agent 以专属凭据连入（板 ac33b1cb）
		t.Fatalf("mark online: %v", err)
	}
	log := &evLog{}
	susp := &lcSusp{log: log}
	gw := &lcGw{log: log}
	bk := &lcBk{log: log}
	lc := NewLifecycle(o.st, o.pool, &lcIdp{IdentityIssuer: idp, log: log}, susp, gw, fakeCatalog{}, bk)
	return lc, log, susp, gw, bk, s, out.AgentID
}

func eqSeq(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v want %v", name, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v want %v", name, got, want)
		}
	}
}

const newerImage = "hub.local/dingwei/base-codex@sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

func TestDestroyCleanupSurvivesClientCancellation(t *testing.T) {
	parallelHostedTest(t)
	lc, _, _, gw, bk, st, id := mkLifecycle(t)
	ctx, cancel := context.WithCancel(context.Background())
	lc.gw = &cancellingLifecycleGateway{inner: gw, cancel: cancel}
	if err := lc.Destroy(ctx, id, "zzc", false, true); err != nil {
		t.Fatalf("destroy after client cancellation: %v", err)
	}
	if bk.ctxErr != nil {
		t.Fatalf("backup inherited client cancellation: %v", bk.ctxErr)
	}
	seat, err := st.GetSeat(context.Background(), id)
	if err != nil || seat.State != "destroyed" {
		t.Fatalf("cleanup did not finish: seat=%+v err=%v", seat, err)
	}
}

func TestLifecycleJobPersistsProgressAndMachineReason(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	t.Run("completed", func(t *testing.T) {
		lc, _, _, _, _, _, id := mkLifecycle(t)
		jobID, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "disable", Confirm: true})
		if err != nil {
			t.Fatal(err)
		}
		lc.RunJob(ctx, jobID)
		got, err := lc.JobProgressFor(ctx, jobID, "zzc")
		if err != nil || got.Stage != "completed" || got.AgentID != id || got.Reason != "" {
			t.Fatalf("completed job = %+v err=%v", got, err)
		}
	})
	t.Run("failed", func(t *testing.T) {
		lc, _, _, gw, _, _, id := mkLifecycle(t)
		gw.downOn = "stop_seat"
		jobID, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "disable", Confirm: true})
		if err != nil {
			t.Fatal(err)
		}
		lc.RunJob(ctx, jobID)
		got, err := lc.JobProgressFor(ctx, jobID, "zzc")
		if err != nil || got.Stage != "failed" || got.Reason != "node_offline" {
			t.Fatalf("failed job = %+v err=%v", got, err)
		}
	})
	t.Run("restart waits for reconnect", func(t *testing.T) {
		lc, _, _, _, _, st, id := mkLifecycle(t)
		jobID, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "restart"})
		if err != nil {
			t.Fatal(err)
		}
		lc.RunJob(ctx, jobID)
		got, err := lc.JobProgressFor(ctx, jobID, "zzc")
		if err != nil || got.Stage != "running" {
			t.Fatalf("pre-reconnect job = %+v err=%v", got, err)
		}
		if err := st.SetSeatState(ctx, id, "online", "", "online", nil); err != nil {
			t.Fatal(err)
		}
		got, err = lc.JobProgressFor(ctx, jobID, "zzc")
		if err != nil || got.Stage != "completed" || got.AgentID != id {
			t.Fatalf("post-reconnect job = %+v err=%v", got, err)
		}
	})
}

func TestLifecycleJobDuplicatePolicy(t *testing.T) {
	parallelHostedTest(t)
	lc, _, _, _, _, _, id := mkLifecycle(t)
	ctx := context.Background()
	first, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "disable", Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "disable", Confirm: true})
	if err != nil || second != first {
		t.Fatalf("same operation retry = %q err=%v, want existing %q", second, err, first)
	}
	if _, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "restart"}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("different active operation must be rejected: %v", err)
	}
}

func TestConcurrentDestroyRequestsShareOneJobAndOneBackup(t *testing.T) {
	parallelHostedTest(t)
	lc, log, _, _, _, _, id := mkLifecycle(t)
	ctx := context.Background()
	// Force both requests past activeJobForSeat before either INSERT becomes
	// visible.  Without this trigger the scheduler may serialize the two
	// goroutines, letting the pre-check (rather than the partial unique index
	// and ON CONFLICT path) make the test green.
	if _, err := lc.st.exec(ctx, `
CREATE FUNCTION g4_test_lifecycle_enqueue_delay() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_sleep(0.25);
  RETURN NEW;
END
$$;
CREATE TRIGGER g4_test_lifecycle_enqueue_delay
BEFORE INSERT ON g4_lifecycle_job
FOR EACH ROW EXECUTE FUNCTION g4_test_lifecycle_enqueue_delay()`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		id  string
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			jobID, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "destroy", Confirm: true})
			results <- result{jobID, err}
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.id == "" || a.id != b.id {
		t.Fatalf("concurrent destroy enqueue: a=%+v b=%+v", a, b)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() { defer wg.Done(); lc.RunJob(ctx, a.id) }()
	}
	wg.Wait()
	backups := 0
	for _, event := range log.get() {
		if event == "backup" {
			backups++
		}
	}
	if backups != 1 {
		t.Fatalf("concurrent destroy ran %d backups, want exactly one; events=%v", backups, log.get())
	}
}

func TestLifecycleJobRecoveryLeavesNoActiveRows(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, op, initialStage, wantStage, wantReason string
		prepare                                       func(*Lifecycle, string)
	}{
		{"queued restart already online", "restart", "queued", "completed", "", nil},
		{"running enable already online", "enable", "running", "completed", "", func(lc *Lifecycle, id string) {
			_ = lc.st.SetSeatState(ctx, id, "disabled", "", "disable", nil)
		}},
		{"ambiguous queued disable", "disable", "queued", "failed", "internal_error", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lc, _, _, _, _, _, id := mkLifecycle(t)
			if tc.prepare != nil {
				tc.prepare(lc, id)
			}
			jobID, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: tc.op, Confirm: true})
			if err != nil {
				t.Fatal(err)
			}
			if tc.initialStage == "running" {
				if _, err := lc.st.exec(ctx, `UPDATE g4_lifecycle_job SET stage='running' WHERE id=$1`, jobID); err != nil {
					t.Fatal(err)
				}
				if err := lc.st.SetSeatState(ctx, id, "online", "", "online", nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := lc.RecoverJobs(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := lc.JobProgressFor(ctx, jobID, "zzc")
			if err != nil || got.Stage != tc.wantStage || got.Reason != tc.wantReason {
				t.Fatalf("recovered job=%+v err=%v, want stage=%s reason=%s", got, err, tc.wantStage, tc.wantReason)
			}
			var active int
			row, err := lc.st.qrow(ctx, `SELECT count(*) FROM g4_lifecycle_job WHERE stage IN ('queued','running')`)
			if err != nil || row.Scan(&active) != nil || active != 0 {
				t.Fatalf("active lifecycle jobs after recovery=%d err=%v", active, err)
			}
		})
	}
}

func TestDestroyCanBeRetriedAfterHubDiesMidJob(t *testing.T) {
	parallelHostedTest(t)
	lc, log, _, _, _, _, id := mkLifecycle(t)
	ctx := context.Background()
	deadJob, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "destroy", Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	// Model the persisted state after the worker claimed the job and the Hub
	// died before it could record a terminal result.
	if _, err := lc.st.exec(ctx, `UPDATE g4_lifecycle_job SET stage='running' WHERE id=$1`, deadJob); err != nil {
		t.Fatal(err)
	}
	if err := lc.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	dead, err := lc.JobProgressFor(ctx, deadJob, "zzc")
	if err != nil || dead.Stage != "failed" || dead.Reason != "internal_error" {
		t.Fatalf("dead job not released: %+v err=%v", dead, err)
	}
	retry, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "destroy", Confirm: true})
	if err != nil || retry == deadJob {
		t.Fatalf("destroy retry id=%q err=%v, old=%q", retry, err, deadJob)
	}
	lc.RunJob(ctx, retry)
	got, err := lc.JobProgressFor(ctx, retry, "zzc")
	if err != nil || got.Stage != "completed" {
		t.Fatalf("destroy retry did not complete: %+v err=%v", got, err)
	}
	backups := 0
	for _, event := range log.get() {
		if event == "backup" {
			backups++
		}
	}
	if backups != 1 {
		t.Fatalf("retry backup count=%d, want one: %v", backups, log.get())
	}
}

// 四操作＋重启的跨系统先后：同一条日志逐项等于预期（复验 L1/L2/L3/L4/L5 删闸对应）。
func TestLifecycleOrderOnOneLog(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	t.Run("upgrade", func(t *testing.T) {
		lc, log, _, _, _, _, id := mkLifecycle(t)
		if err := lc.Upgrade(ctx, id, "zzc", newerImage, true); err != nil {
			t.Fatal(err)
		}
		eqSeq(t, "upgrade", log.get(), []string{"idp:reissue", "node:upgrade_seat"})
	})
	t.Run("disable_then_enable", func(t *testing.T) {
		lc, log, _, _, _, _, id := mkLifecycle(t)
		if err := lc.Disable(ctx, id, "zzc", true, true); err != nil {
			t.Fatal(err)
		}
		eqSeq(t, "disable", log.get(), []string{"idp:suspend", "node:stop_seat"})
		log.reset()
		if err := lc.Enable(ctx, id, "zzc", true); err != nil {
			t.Fatal(err)
		}
		eqSeq(t, "enable", log.get(), []string{"idp:resume", "node:start_seat"})
	})
	t.Run("restart_resumes_first", func(t *testing.T) {
		lc, log, _, _, _, _, id := mkLifecycle(t)
		if err := lc.Restart(ctx, id, "zzc", true); err != nil {
			t.Fatal(err)
		}
		eqSeq(t, "restart", log.get(), []string{"idp:resume", "node:restart_seat"})
	})
	t.Run("destroy", func(t *testing.T) {
		lc, log, _, _, _, _, id := mkLifecycle(t)
		if err := lc.Destroy(ctx, id, "zzc", true, true); err != nil {
			t.Fatal(err)
		}
		eqSeq(t, "destroy", log.get(), []string{"node:stop_seat", "backup", "idp:retire", "node:destroy_seat"})
	})
}

// 每道闸失败即止（复验 L6/L8/L17/L18＋状态闸）。
func TestFailedGateStopsTheChain(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	t.Run("backup_fail_no_retire", func(t *testing.T) {
		lc, log, _, _, bk, _, id := mkLifecycle(t)
		bk.failOn = true
		if err := lc.Destroy(ctx, id, "zzc", true, true); err == nil {
			t.Fatal("备份失败必须拒销毁")
		}
		eqSeq(t, "backup_fail", log.get(), []string{"node:stop_seat", "backup"}) // 先停、再备份失败：Retire/销毁都没发生
	})
	t.Run("suspend_fail_no_stop", func(t *testing.T) {
		lc, log, susp, _, _, _, id := mkLifecycle(t)
		susp.failOn = "suspend"
		if err := lc.Disable(ctx, id, "zzc", true, true); err == nil {
			t.Fatal("Suspend 失败必须拒")
		}
		eqSeq(t, "suspend_fail", log.get(), []string{"idp:suspend"})
	})
	t.Run("resume_fail_no_start", func(t *testing.T) {
		lc, log, susp, _, _, s, id := mkLifecycle(t)
		if err := lc.Disable(ctx, id, "zzc", true, true); err != nil {
			t.Fatal(err)
		}
		log.reset()
		susp.failOn = "resume"
		if err := lc.Enable(ctx, id, "zzc", true); err == nil {
			t.Fatal("Resume 失败必须拒")
		}
		eqSeq(t, "resume_fail", log.get(), []string{"idp:resume"})
		seat, _ := s.GetSeat(ctx, id)
		if seat.State != "disabled" {
			t.Fatalf("启用失败应仍 disabled: %+v", seat)
		}
	})
	t.Run("reissue_fail_no_upgrade_seat_back_online", func(t *testing.T) {
		lc, log, susp, _, _, s, id := mkLifecycle(t)
		susp.failOn = "reissue"
		if err := lc.Upgrade(ctx, id, "zzc", newerImage, true); err == nil {
			t.Fatal("Reissue 失败必须拒升级")
		}
		eqSeq(t, "reissue_fail", log.get(), []string{"idp:reissue"})
		seat, _ := s.GetSeat(ctx, id)
		if seat.State != "online" {
			t.Fatalf("回滚应回 online: %+v", seat)
		}
	})
	t.Run("enable_non_disabled_refused_no_side_effect", func(t *testing.T) {
		lc, log, _, _, _, _, id := mkLifecycle(t)
		if err := lc.Enable(ctx, id, "zzc", true); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("非 disabled 启用应 invalid_state, 得 %v", err)
		}
		eqSeq(t, "enable_refused", log.get(), nil) // 什么都不许发生
	})
}

// 复验阻断 3：重启一律先 Resume 再发 restart_seat（凭据若处于暂停，不先恢复就会「页面在跑、凭据拒连」）。
// 第 7 条之后停用失败已不再留下暂停的凭据（见 TestNodeDownLeavesStateAndCredential）；这里的 Resume 是兜底。
func TestRestartAfterFailedStopResumesCredential(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, log, _, gw, _, s, id := mkLifecycle(t)
	gw.failOn = "stop_seat"
	if err := lc.Disable(ctx, id, "zzc", true, true); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("停容器失败应报 node_offline: %v", err)
	}
	if seat, _ := s.GetSeat(ctx, id); seat.State != "online" {
		t.Fatalf("停用失败席位状态不得改: %+v", seat)
	}
	gw.failOn = ""
	log.reset()
	if err := lc.Restart(ctx, id, "zzc", true); err != nil {
		t.Fatal(err)
	}
	eqSeq(t, "restart_after_failed_stop", log.get(), []string{"idp:resume", "node:restart_seat"})
	seat, _ := s.GetSeat(ctx, id)
	if seat.State != "starting" { // 容器重启后等 Agent 连入（板 79498c60）
		t.Fatalf("重启后应 starting: %+v", seat)
	}
}

// 复验阻断 4：归属本层强制——陌生人三操作全拒零副作用；管理用户销毁别人的也拒（业主 8971bddf）。
func TestOwnershipEnforced(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	for name, op := range map[string]func(lc *Lifecycle, id string) error{
		"upgrade": func(lc *Lifecycle, id string) error { return lc.Upgrade(ctx, id, "mallory", newerImage, false) },
		"disable": func(lc *Lifecycle, id string) error { return lc.Disable(ctx, id, "mallory", false, true) },
		"destroy": func(lc *Lifecycle, id string) error { return lc.Destroy(ctx, id, "mallory", false, true) },
		// 管理用户销毁别人的 Agent ⇒ 同样拒（吊销不豁免）
		"destroy_admin": func(lc *Lifecycle, id string) error { return lc.Destroy(ctx, id, "admin", true, true) },
	} {
		t.Run(name, func(t *testing.T) {
			lc, log, _, _, _, _, id := mkLifecycle(t)
			if err := op(lc, id); !errors.Is(err, ErrNotOwner) {
				t.Fatalf("%s 应 not_owner, 得 %v", name, err)
			}
			eqSeq(t, name+"_no_side_effect", log.get(), nil)
		})
	}
}

// 复验阻断 1+2：Hub 下发的 upgrade_seat／create_seat 参数满足节点契约
// （image＋resources.memory_mb≥4/cpu_shares≥2＋token_file；升级的令牌是 Reissue 的新令牌）。
func TestHubParamsSatisfyNodeContract(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, _, _, gw, _, _, id := mkLifecycle(t)
	if err := lc.Upgrade(ctx, id, "zzc", newerImage, true); err != nil {
		t.Fatal(err)
	}
	var up map[string]any
	for _, c := range gw.sent {
		if c.Type == "upgrade_seat" {
			_ = json.Unmarshal(c.Params, &up)
		}
	}
	if up == nil {
		t.Fatal("应有 upgrade_seat")
	}
	res := up["resources"].(map[string]any)
	if res["memory_mb"].(float64) < 4 || res["cpu_shares"].(float64) < 2 {
		t.Fatalf("resources 不满足节点契约: %v", res)
	}
	tf := up["token_file"].(map[string]any)
	if tf["application_id"] != "app2" || tf["poll_token"] != "tok2" {
		t.Fatalf("升级令牌必须是 Reissue 的新令牌: %v", tf)
	}
	// 容器启动契约（a26a825f528a）：升级后新容器 SH_CRED_KIND＝席位账号形态（删掉这行必红）
	if up["cred_kind"] != "token_file" {
		t.Fatalf("upgrade_seat 须带 cred_kind: %v", up)
	}
	if up["cli"] != "codex" { // 组长 R-B12-SEATMODE 第 2 条：升级后的新容器同一 CLI
		t.Fatalf("upgrade_seat 须带 cli: %v", up)
	}
	if tf["expires_at"] == "" || up["image"] != newerImage {
		t.Fatalf("token/image: %v", up)
	}
}

// 复验阻断 5：节点离线销毁——席位 destroyed、无节点指令、留对账记录（审计＋事件标记）。
func TestDestroyOfflineNodeLeavesReconcileRecord(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, log, _, _, _, s, id := mkLifecycle(t)
	seat, _ := s.GetSeat(ctx, id)
	if err := s.SetNodeState(ctx, seat.NodeID, "offline"); err != nil {
		t.Fatal(err)
	}
	if err := lc.Destroy(ctx, id, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	// 节点离线：只吊销，不备份、不删（备份与删数据都留给重连对账，lead-g4-t53-review 第 4 条）
	eqSeq(t, "offline_destroy", log.get(), []string{"idp:retire"})
	seat, _ = s.GetSeat(ctx, id)
	if seat.State != "destroyed" {
		t.Fatalf("应 destroyed: %+v", seat)
	}
	row, err := s.qrow(ctx, `SELECT COUNT(*) FROM g4_audit WHERE action='seat.destroy_degraded' AND target=$1`, id)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := row.Scan(&n); err != nil || n != 1 {
		t.Fatalf("离线销毁须留对账审计一条: n=%d err=%v", n, err)
	}
	// 销毁事件带 reconcile_pending 标记（节点重连后对账的取数点）
	row, err = s.qrow(ctx, `SELECT detail FROM g4_seat_event WHERE agent_id=$1 AND kind='destroy' ORDER BY id DESC LIMIT 1`, id)
	if err != nil {
		t.Fatal(err)
	}
	var detail string
	if err := row.Scan(&detail); err != nil || !strings.Contains(detail, "reconcile_pending") || !strings.Contains(detail, "backup_pending") {
		t.Fatalf("销毁事件须带 reconcile_pending 与 backup_pending: %s err=%v", detail, err)
	}
	if p, err := s.SeatBackupPending(ctx, id); err != nil || !p {
		t.Fatalf("离线销毁后应待备份: %v %v", p, err)
	}
}

// 保留：节点失败自动回滚（旧态仍在）＋裸调 confirm 闸。
func TestUpgradeRollbackOnNodeFailure(t *testing.T) {
	parallelHostedTest(t)
	lc, _, _, gw, _, s, id := mkLifecycle(t)
	gw.failOn = "upgrade_seat"
	if err := lc.Upgrade(context.Background(), id, "zzc", newerImage, true); !errors.Is(err, ErrRolledBack) {
		t.Fatalf("失败应 upgrade_failed_rolled_back, 得 %v", err)
	}
	seat, _ := s.GetSeat(context.Background(), id)
	if seat.State != "online" {
		t.Fatalf("回滚后应 online（旧态仍在）: %+v", seat)
	}
}

func TestConfirmRequiredGates(t *testing.T) {
	parallelHostedTest(t)
	lc, log, _, _, _, _, id := mkLifecycle(t)
	if err := lc.Disable(context.Background(), id, "zzc", true, false); err != ErrConfirmRequired {
		t.Fatalf("裸调停用应 confirm_required, 得 %v", err)
	}
	if err := lc.Destroy(context.Background(), id, "zzc", true, false); err != ErrConfirmRequired {
		t.Fatalf("裸调销毁应 confirm_required, 得 %v", err)
	}
	eqSeq(t, "confirm_gates_no_side_effect", log.get(), nil)
}

// 销毁后账号回池（复验 L13 对应格保留）。
func TestDestroyReturnsAccountToPool(t *testing.T) {
	parallelHostedTest(t)
	lc, _, _, _, _, s, id := mkLifecycle(t)
	if err := lc.Destroy(context.Background(), id, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	acct, err := s.GetAccount(context.Background(), acctOf(t, mkPoolSvcFromStore(t, s)))
	if err != nil {
		// 回池后 ListAdmin 仍在（账号不删）——直接查库里那枚
		t.Fatal(err)
	}
	if acct.State != "available" || acct.BoundAgentID.Valid {
		t.Fatalf("销毁后账号应回池: %+v", acct)
	}
}

// 停用中不可升级（先启用）。
func TestUpgradeWhileDisabledRejected(t *testing.T) {
	parallelHostedTest(t)
	lc, _, _, _, _, _, id := mkLifecycle(t)
	if err := lc.Disable(context.Background(), id, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	if err := lc.Upgrade(context.Background(), id, "zzc", "", true); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("停用中升级应 invalid_state, 得 %v", err)
	}
}

func mkPoolSvcFromStore(t *testing.T, s *Store) *PoolService {
	t.Helper()
	svc, err := NewPoolService(s, mkMasterKeyPath(t))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// 组长 7fabddb0 ⑤后半句：重连后清理——ReconcileNode 对待对账席位补发 destroy_seat，
// 成功写 reconcile_done；再次对账不再发（幂等收口）。
func TestReconcileOnNodeReconnect(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, log, _, gw, bk, s, id := mkLifecycle(t)
	seat, _ := s.GetSeat(ctx, id)
	if err := s.SetNodeState(ctx, seat.NodeID, "offline"); err != nil {
		t.Fatal(err)
	}
	if err := lc.Destroy(ctx, id, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	pending, err := s.SeatsPendingReconcile(ctx, seat.NodeID)
	if err != nil || len(pending) != 1 || pending[0] != id {
		t.Fatalf("离线销毁后应有一席待对账: %v err=%v", pending, err)
	}
	// 节点重连（对账走编排器的 gw；备份器与销毁同一个，事件记在同一条日志上）
	orch := NewOrchestrator(s, lc.pool, lc.idp, lc.gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})

	// ① 没有备份器 ⇒ 不删，留待下次
	log.reset()
	orch.ReconcileNode(ctx, seat.NodeID)
	eqSeq(t, "no backuper", log.get(), nil)
	if pending, _ = s.SeatsPendingReconcile(ctx, seat.NodeID); len(pending) != 1 {
		t.Fatalf("没有备份器不得删数据: %v", pending)
	}
	orch.SetBackuper(bk)

	// ② 备份失败 ⇒ 不发 destroy_seat，仍待备份、仍待对账
	bk.failOn = true
	log.reset()
	orch.ReconcileNode(ctx, seat.NodeID)
	eqSeq(t, "backup failed", log.get(), []string{"backup"})
	if p, _ := s.SeatBackupPending(ctx, id); !p {
		t.Fatal("备份失败后仍应待备份")
	}

	// ③ 备份成功 ⇒ 先备份、再 destroy_seat，两步都完成
	bk.failOn = false
	log.reset()
	orch.ReconcileNode(ctx, seat.NodeID)
	eqSeq(t, "backup then destroy", log.get(), []string{"backup", "node:destroy_seat"})
	pending, _ = s.SeatsPendingReconcile(ctx, seat.NodeID)
	if len(pending) != 0 {
		t.Fatalf("对账后应无待清: %v", pending)
	}
	if p, _ := s.SeatBackupPending(ctx, id); p {
		t.Fatal("备份成功后不应再待备份")
	}
	_ = gw
}

// 备份已在某次重连里做过、但当时 destroy_seat 失败：下次重连不再重复备份，只补删。
func TestReconcileDoesNotBackupTwice(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, log, _, gw, bk, s, id := mkLifecycle(t)
	seat, _ := s.GetSeat(ctx, id)
	_ = s.SetNodeState(ctx, seat.NodeID, "offline")
	if err := lc.Destroy(ctx, id, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	orch := NewOrchestrator(s, lc.pool, lc.idp, lc.gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
	orch.SetBackuper(bk)
	gw.failOn = "destroy_seat"
	log.reset()
	orch.ReconcileNode(ctx, seat.NodeID)
	eqSeq(t, "first reconnect", log.get(), []string{"backup", "node:destroy_seat"})
	gw.failOn = ""
	log.reset()
	orch.ReconcileNode(ctx, seat.NodeID)
	eqSeq(t, "second reconnect", log.get(), []string{"node:destroy_seat"})
	if pending, _ := s.SeatsPendingReconcile(ctx, seat.NodeID); len(pending) != 0 {
		t.Fatalf("still pending: %v", pending)
	}
}

// 组长裁定 lead-g4-joint-run-1 第 7 条：节点离线 ⇒ 503 node_offline，且不改席位状态、不动凭据。
// 两种「离线」都要守：库里已记离线（guard 先判，什么都不发生）；库里还记在线但发不到（发送失败后撤回凭据动作）。
func TestNodeDownLeavesStateAndCredential(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	t.Run("node recorded offline: nothing happens", func(t *testing.T) {
		for _, op := range []string{"disable", "restart", "upgrade"} {
			lc, log, susp, _, _, s, id := mkLifecycle(t)
			seat, _ := s.GetSeat(ctx, id)
			if err := s.SetNodeState(ctx, seat.NodeID, "offline"); err != nil {
				t.Fatal(err)
			}
			var err error
			switch op {
			case "disable":
				err = lc.Disable(ctx, id, "zzc", false, true)
			case "restart":
				err = lc.Restart(ctx, id, "zzc", false)
			case "upgrade":
				err = lc.Upgrade(ctx, id, "zzc", newerImage, false)
			}
			if !errors.Is(err, ErrNodeOffline) {
				t.Errorf("%s: %v, want node_offline", op, err)
			}
			eqSeq(t, op+" offline", log.get(), nil)
			if after, _ := s.GetSeat(ctx, id); after.State != "online" || susp.suspended {
				t.Errorf("%s: state %s suspended=%v, want online and credential untouched", op, after.State, susp.suspended)
			}
		}
	})
	t.Run("disable: stop unreachable ⇒ suspend undone", func(t *testing.T) {
		lc, log, susp, gw, _, s, id := mkLifecycle(t)
		gw.downOn = "stop_seat"
		if err := lc.Disable(ctx, id, "zzc", false, true); !errors.Is(err, ErrNodeOffline) {
			t.Fatalf("%v, want node_offline", err)
		}
		eqSeq(t, "disable down", log.get(), []string{"idp:suspend", "node:stop_seat", "idp:resume"})
		if seat, _ := s.GetSeat(ctx, id); seat.State != "online" || susp.suspended {
			t.Fatalf("state %s suspended=%v, want online and credential active", seat.State, susp.suspended)
		}
	})
	t.Run("disable on already-suspended credential: no resume on failure", func(t *testing.T) {
		lc, log, susp, gw, _, _, id := mkLifecycle(t)
		susp.suspended = true // 凭据本来就暂停（例如别的路径先停了）：撤回不能把它放开
		gw.downOn = "stop_seat"
		_ = lc.Disable(ctx, id, "zzc", false, true)
		eqSeq(t, "disable down, already suspended", log.get(), []string{"idp:suspend", "node:stop_seat"})
		if !susp.suspended {
			t.Fatal("credential that was suspended before must stay suspended")
		}
	})
	t.Run("enable: start unreachable ⇒ resume undone, still disabled", func(t *testing.T) {
		lc, log, susp, gw, _, s, id := mkLifecycle(t)
		if err := lc.Disable(ctx, id, "zzc", false, true); err != nil {
			t.Fatal(err)
		}
		log.reset()
		gw.downOn = "start_seat"
		if err := lc.Enable(ctx, id, "zzc", false); !errors.Is(err, ErrNodeOffline) {
			t.Fatalf("%v, want node_offline", err)
		}
		eqSeq(t, "enable down", log.get(), []string{"idp:resume", "node:start_seat", "idp:suspend"})
		if seat, _ := s.GetSeat(ctx, id); seat.State != "disabled" || !susp.suspended {
			t.Fatalf("state %s suspended=%v, want disabled and credential suspended", seat.State, susp.suspended)
		}
	})
	t.Run("restart: unreachable ⇒ state back to online, no credential change", func(t *testing.T) {
		lc, log, susp, gw, _, s, id := mkLifecycle(t)
		gw.downOn = "restart_seat"
		if err := lc.Restart(ctx, id, "zzc", false); !errors.Is(err, ErrNodeOffline) {
			t.Fatalf("%v, want node_offline", err)
		}
		// 凭据本来在用：Resume 是 no-op（already），不撤回。
		eqSeq(t, "restart down", log.get(), []string{"idp:resume", "node:restart_seat"})
		if seat, _ := s.GetSeat(ctx, id); seat.State != "online" || susp.suspended {
			t.Fatalf("state %s suspended=%v, want online and credential active", seat.State, susp.suspended)
		}
	})
	t.Run("upgrade: unreachable ⇒ node_offline, not rolled_back; image unchanged", func(t *testing.T) {
		lc, _, _, gw, _, s, id := mkLifecycle(t)
		before, _ := s.GetSeat(ctx, id)
		gw.downOn = "upgrade_seat"
		if err := lc.Upgrade(ctx, id, "zzc", newerImage, false); !errors.Is(err, ErrNodeOffline) {
			t.Fatalf("%v, want node_offline", err)
		}
		if after, _ := s.GetSeat(ctx, id); after.State != "online" || after.ImageRef != before.ImageRef {
			t.Fatalf("after %+v, want online with image %s", after, before.ImageRef)
		}
	})
	t.Run("node reports failure (reachable): start_failed sentinel, not Go text", func(t *testing.T) {
		lc, _, _, gw, _, _, id := mkLifecycle(t)
		gw.failOn = "restart_seat"
		if err := lc.Restart(ctx, id, "zzc", false); !errors.Is(err, ErrStartFailed) {
			t.Fatalf("%v, want start_failed sentinel", err)
		}
	})
}

// 组长裁定第 4 条：备份不可用 ⇒ backup_failed，原因放 Detail；Retire 与销毁都没发生。
func TestDestroyBackupFailedTyped(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, log, _, _, bk, _, id := mkLifecycle(t)
	bk.failOn = true
	err := lc.Destroy(ctx, id, "zzc", false, true)
	var be *BackupFailedError
	if !errors.As(err, &be) || !errors.Is(err, ErrBackupFailed) || err.Error() != "backup_failed" || be.Detail() != "injected backup" {
		t.Fatalf("%v (%T), want BackupFailedError{injected backup}", err, err)
	}
	eqSeq(t, "backup_failed", log.get(), []string{"node:stop_seat", "backup"})
}

// 复验三刀守卫（86d78377 附录 A）：①对账只发 destroy_seat ②节点失败不标记完成 ③只清本节点的。
func TestReconcileGuards(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, _, _, gw, bk, s, id := mkLifecycle(t)
	seat, _ := s.GetSeat(ctx, id)
	if err := s.SetNodeState(ctx, seat.NodeID, "offline"); err != nil {
		t.Fatal(err)
	}
	if err := lc.Destroy(ctx, id, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	// 另一个节点的待对账席位（不得被本节点对账清掉）
	if err := s.upsertR31LegacyNode(ctx, Node{ID: "nd_other", Name: "other", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSeat(ctx, Seat{AgentID: "ag_other", NodeID: "nd_other", CLI: "codex", LLM: "gpt",
		CredKind: "token_file", Plan: "2c4g", ImageRef: "r", CreatedBy: "zzc"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeatState(ctx, "ag_other", "destroyed", "", "destroy", json.RawMessage(`{"reconcile_pending":true}`)); err != nil {
		t.Fatal(err)
	}
	// ③ 节点过滤：对本节点对账
	orch := NewOrchestrator(s, lc.pool, lc.idp, gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
	orch.SetBackuper(bk)
	orch.ReconcileNode(ctx, seat.NodeID)
	for _, c := range gw.sent {
		// ① 只许 destroy_seat
		if c.Type != "destroy_seat" {
			t.Fatalf("对账只许补发 destroy_seat: %+v", gw.sent)
		}
	}
	if len(gw.sent) != 1 {
		t.Fatalf("本节点一席待清 ⇒ 恰好一条 destroy_seat: %+v", gw.sent)
	}
	// ③ 别的节点的待对账席位不动
	pending, _ := s.SeatsPendingReconcile(ctx, "nd_other")
	if len(pending) != 1 {
		t.Fatalf("别节点的待对账不得被清: %v", pending)
	}
	// ② 失败不标记完成
	lc2, _, _, gw2, bk2, s2, id2 := mkLifecycle(t)
	seat2, _ := s2.GetSeat(ctx, id2)
	_ = s2.SetNodeState(ctx, seat2.NodeID, "offline")
	if err := lc2.Destroy(ctx, id2, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	gw2.failOn = "destroy_seat"
	orch2 := NewOrchestrator(s2, lc2.pool, lc2.idp, gw2, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
	orch2.SetBackuper(bk2)
	orch2.ReconcileNode(ctx, seat2.NodeID)
	pending2, _ := s2.SeatsPendingReconcile(ctx, seat2.NodeID)
	if len(pending2) != 1 {
		t.Fatalf("失败不得标记完成（留待下次重连）: %v", pending2)
	}
}

// 组长取甲（2026-10-02）：在线销毁先停容器、再备份、再删。停不下来 ⇒ node_offline，不备份、不吊销、不删、席位不变；
// 停了但备份失败 ⇒ 席位记 error／backup_failed（Agent 确实不在了），凭据与账号都不动。
func TestDestroyStopsBeforeBackup(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	for name, setup := range map[string]func(gw *lcGw){
		"stop unreachable":   func(gw *lcGw) { gw.downOn = "stop_seat" },
		"stop reported fail": func(gw *lcGw) { gw.failOn = "stop_seat" },
	} {
		t.Run(name, func(t *testing.T) {
			lc, log, _, gw, _, s, id := mkLifecycle(t)
			setup(gw)
			if err := lc.Destroy(ctx, id, "zzc", false, true); !errors.Is(err, ErrNodeOffline) {
				t.Fatalf("%v, want node_offline", err)
			}
			eqSeq(t, name, log.get(), []string{"node:stop_seat"}) // 没备份、没吊销、没删
			seat, _ := s.GetSeat(ctx, id)
			if seat.State != "online" {
				t.Fatalf("seat changed: %+v", seat)
			}
			if !seat.AccountRef.Valid { // 0.2.1.101：未销毁 ⇒ 本席仍引用账号（不释放）
				t.Fatalf("account reference must stay: %+v", seat)
			}
		})
	}
	t.Run("backup fails after stop", func(t *testing.T) {
		lc, log, _, _, bk, s, id := mkLifecycle(t)
		bk.failOn = true
		var be *BackupFailedError
		if err := lc.Destroy(ctx, id, "zzc", false, true); !errors.As(err, &be) {
			t.Fatalf("%v, want backup_failed", err)
		}
		eqSeq(t, "backup fails after stop", log.get(), []string{"node:stop_seat", "backup"})
		seat, _ := s.GetSeat(ctx, id)
		if seat.State != "error" || seat.StateDetail != "backup_failed" {
			t.Fatalf("stopped but not backed up: %+v, want error/backup_failed", seat)
		}
		if !seat.AccountRef.Valid { // 0.2.1.101：未销毁 ⇒ 本席仍引用账号（不释放）
			t.Fatalf("account reference must stay: %+v", seat)
		}
	})
}

// 包 B2 修订 3：upgrade 用落库记录值，不随节点默认漂移。
// 反向：upgrade 参数不带 seat.Model（或取节点默认）⇒ 红。
func TestUpgradeUsesRecordedModel(t *testing.T) {
	parallelHostedTest(t)
	lc, _, _, gw, _, s, id := mkLifecycle(t)
	// 把席位的生效模型改落库值（建席时定死语义）
	if err := s.SetSeatModel(context.Background(), id, "gpt-6.1-sol"); err != nil {
		t.Fatal(err)
	}
	if err := lc.Upgrade(context.Background(), id, "zzc", "v2", false); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	var found bool
	for _, c := range gw.sent {
		if c.Type == "upgrade_seat" {
			var p map[string]any
			_ = json.Unmarshal(c.Params, &p)
			if p["model"] == "gpt-6.1-sol" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("upgrade_seat 参数须带记录值 model: %+v", gw.sent)
	}
}
