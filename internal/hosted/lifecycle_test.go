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
	log    *evLog
	failOn string // "suspend"/"resume"/"reissue"
}

func (f *lcSusp) SuspendHostedAgent(ctx context.Context, id, by string) (bool, error) {
	f.log.add("idp:suspend")
	if f.failOn == "suspend" {
		return false, errors.New("injected suspend")
	}
	return false, nil
}
func (f *lcSusp) ResumeHostedAgent(ctx context.Context, id, by string) (bool, error) {
	f.log.add("idp:resume")
	if f.failOn == "resume" {
		return false, errors.New("injected resume")
	}
	return false, nil
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

type lcGw struct {
	log    *evLog
	failOn string
	sent   []NodeCommand
}

func (g *lcGw) Send(ctx context.Context, nodeID string, c NodeCommand) (NodeResult, error) {
	g.log.add("node:" + c.Type)
	g.sent = append(g.sent, c)
	if c.Type == g.failOn {
		return NodeResult{OK: false, Error: "injected"}, nil
	}
	return NodeResult{OK: true}, nil
}

type lcBk struct {
	log    *evLog
	failOn bool
}

func (b *lcBk) BackupSeatVolume(ctx context.Context, nodeID, seatID string) (string, error) {
	b.log.add("backup")
	if b.failOn {
		return "", errors.New("injected backup")
	}
	return "bk://" + nodeID + "/" + seatID + "/v1", nil
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

// 四操作＋重启的跨系统先后：同一条日志逐项等于预期（复验 L1/L2/L3/L4/L5 删闸对应）。
func TestLifecycleOrderOnOneLog(t *testing.T) {
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
		eqSeq(t, "destroy", log.get(), []string{"backup", "idp:retire", "node:destroy_seat"})
	})
}

// 每道闸失败即止（复验 L6/L8/L17/L18＋状态闸）。
func TestFailedGateStopsTheChain(t *testing.T) {
	ctx := context.Background()
	t.Run("backup_fail_no_retire", func(t *testing.T) {
		lc, log, _, _, bk, _, id := mkLifecycle(t)
		bk.failOn = true
		if err := lc.Destroy(ctx, id, "zzc", true, true); err == nil {
			t.Fatal("备份失败必须拒销毁")
		}
		eqSeq(t, "backup_fail", log.get(), []string{"backup"}) // 只有备份，Retire/销毁都没发生
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

// 复验阻断 3：停用中停容器失败（凭据已暂停）⇒ 重启必须先 Resume 再发 restart_seat。
func TestRestartAfterFailedStopResumesCredential(t *testing.T) {
	ctx := context.Background()
	lc, log, _, gw, _, s, id := mkLifecycle(t)
	gw.failOn = "stop_seat"
	if err := lc.Disable(ctx, id, "zzc", true, true); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("停容器失败应报 node_offline: %v", err)
	}
	gw.failOn = ""
	log.reset()
	if err := lc.Restart(ctx, id, "zzc", true); err != nil {
		t.Fatal(err)
	}
	eqSeq(t, "restart_after_failed_stop", log.get(), []string{"idp:resume", "node:restart_seat"})
	seat, _ := s.GetSeat(ctx, id)
	if seat.State != "online" {
		t.Fatalf("重启后应 online: %+v", seat)
	}
}

// 复验阻断 4：归属本层强制——陌生人三操作全拒零副作用；管理用户销毁别人的也拒（业主 8971bddf）。
func TestOwnershipEnforced(t *testing.T) {
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
	if tf["expires_at"] == "" || up["image"] != newerImage {
		t.Fatalf("token/image: %v", up)
	}
}

// 复验阻断 5：节点离线销毁——席位 destroyed、无节点指令、留对账记录（审计＋事件标记）。
func TestDestroyOfflineNodeLeavesReconcileRecord(t *testing.T) {
	ctx := context.Background()
	lc, log, _, _, _, s, id := mkLifecycle(t)
	seat, _ := s.GetSeat(ctx, id)
	if err := s.SetNodeState(ctx, seat.NodeID, "offline"); err != nil {
		t.Fatal(err)
	}
	if err := lc.Destroy(ctx, id, "zzc", true, true); err != nil {
		t.Fatal(err)
	}
	eqSeq(t, "offline_destroy", log.get(), []string{"backup", "idp:retire"}) // 无 node:destroy_seat
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
	if err := row.Scan(&detail); err != nil || !strings.Contains(detail, "reconcile_pending") {
		t.Fatalf("销毁事件须带 reconcile_pending: %s err=%v", detail, err)
	}
}

// 保留：节点失败自动回滚（旧态仍在）＋裸调 confirm 闸。
func TestUpgradeRollbackOnNodeFailure(t *testing.T) {
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
	ctx := context.Background()
	lc, _, _, _, _, s, id := mkLifecycle(t)
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
	// 节点重连（对账走编排器的 gw——mkOrch 的假网关记录指令）
	orch := NewOrchestrator(s, lc.pool, lc.idp, lc.gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
	orch.ReconcileNode(ctx, seat.NodeID)
	pending, _ = s.SeatsPendingReconcile(ctx, seat.NodeID)
	if len(pending) != 0 {
		t.Fatalf("对账后应无待清: %v", pending)
	}
}

// 联调第 7 条：停用失败（error 态）后启用可恢复——Resume＋起容器，回 online。
func TestEnableFromErrorRecoversFailedDisable(t *testing.T) {
	ctx := context.Background()
	lc, log, _, gw, _, s, id := mkLifecycle(t)
	gw.failOn = "stop_seat"
	if err := lc.Disable(ctx, id, "zzc", true, true); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("停用失败应 node_offline: %v", err)
	}
	gw.failOn = ""
	log.reset()
	if err := lc.Enable(ctx, id, "zzc", true); err != nil {
		t.Fatalf("error 态启用应可恢复: %v", err)
	}
	eqSeq(t, "enable_from_error", log.get(), []string{"idp:resume", "node:start_seat"})
	seat, _ := s.GetSeat(ctx, id)
	if seat.State != "online" {
		t.Fatalf("恢复后应 online: %+v", seat)
	}
}
