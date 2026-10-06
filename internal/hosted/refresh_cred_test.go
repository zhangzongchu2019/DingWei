package hosted

// 刷新凭据（R-V2-ACCOUNT-REFRESH-CRED，并入 0.2.1.101 账号包）：C1–C13，编号对方案件第十、十三节。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// rcGw 可按指令类型回指定错误文本的假节点（模拟旧节点 unknown_command_type、写失败、重启失败、节点不通）。
type rcGw struct {
	sent    []NodeCommand
	errText string // 非空 ⇒ refresh_cred_seat 回 OK=false＋该文本
	down    bool   // ⇒ 传输错误（节点不通）
}

func (g *rcGw) Send(ctx context.Context, nodeID string, c NodeCommand) (NodeResult, error) {
	g.sent = append(g.sent, c)
	if c.Type == "refresh_cred_seat" {
		if g.down {
			return NodeResult{}, errors.New("injected: node unreachable")
		}
		if g.errText != "" {
			return NodeResult{OK: false, Error: g.errText}, nil
		}
	}
	return NodeResult{OK: true}, nil
}

func mkRefresh(t *testing.T) (*Lifecycle, *rcGw, *Store, string, string) {
	t.Helper()
	lc, _, _, _, _, s, id := mkLifecycle(t)
	gw := &rcGw{}
	lc.gw = gw
	seat, err := s.GetSeat(context.Background(), id)
	if err != nil || !seat.AccountRef.Valid {
		t.Fatalf("seat: %+v %v", seat, err)
	}
	return lc, gw, s, id, seat.AccountRef.String
}

func refreshParams(t *testing.T, c NodeCommand) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Params, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// C1：属主刷新 ⇒ 下发的是池内**最新**副本（先换密再刷新）；参数恰为 {seat_id, credential_file{content}}；
// 席位停 starting（detail=refresh）等 MarkOnline；解密审计带 reason=refresh；池内副本＝当前件时照常执行不报错。
func TestRefreshCredHappyPath(t *testing.T) {
	lc, gw, s, id, acct := mkRefresh(t)
	ctx := context.Background()
	if _, err := lc.pool.UpdateSecretOwned(ctx, "zzc", acct, `{"access_token":"rotated"}`); err != nil {
		t.Fatal(err)
	}
	if err := lc.RefreshCred(ctx, id, "zzc"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(gw.sent) != 1 || gw.sent[0].Type != "refresh_cred_seat" {
		t.Fatalf("节点指令: %+v", gw.sent)
	}
	m := refreshParams(t, gw.sent[0])
	if len(m) != 2 || m["seat_id"] != id {
		t.Fatalf("参数只应有 seat_id 与 credential_file: %v", m)
	}
	cf, _ := m["credential_file"].(map[string]any)
	if len(cf) != 1 || cf["content"] != `{"access_token":"rotated"}` {
		t.Fatalf("凭据件应为池内最新副本: %v", cf)
	}
	seat, _ := s.GetSeat(ctx, id)
	if seat.State != "starting" || seat.StateDetail != seatStartingRefresh {
		t.Fatalf("应停 starting/refresh 等连回: %+v", seat)
	}
	var reasoned int
	row, _ := s.qrow(ctx, `SELECT COUNT(*) FROM g4_audit WHERE action='account.decrypt_issue' AND target=$1 AND detail->>'reason'='refresh'`, acct)
	_ = row.Scan(&reasoned)
	if reasoned != 1 {
		t.Fatalf("刷新解密审计应带 reason=refresh 恰一条, 得 %d", reasoned)
	}
	// 连回（MarkOnline 同口径：starting → online）；池内副本未变再刷一次 ⇒ 照常执行
	if _, err := s.TransitionSeatState(ctx, id, "starting", "online", "", "online", nil); err != nil {
		t.Fatal(err)
	}
	if err := lc.RefreshCred(ctx, id, "zzc"); err != nil {
		t.Fatalf("池内副本＝当前件时仍应照常刷新: %v", err)
	}
	if len(gw.sent) != 2 {
		t.Fatalf("第二次刷新应再发一次指令: %d", len(gw.sent))
	}
}

// C2：非属主（含管理员）⇒ not_owner，零节点指令、零解密。
func TestRefreshCredOwnerOnly(t *testing.T) {
	lc, gw, s, id, acct := mkRefresh(t)
	ctx := context.Background()
	for _, actor := range []string{"bob", "admin"} {
		if err := lc.RefreshCred(ctx, id, actor); err != ErrNotOwner {
			t.Fatalf("%s 刷新应 ErrNotOwner, 得 %v", actor, err)
		}
	}
	if len(gw.sent) != 0 {
		t.Fatalf("不得发节点指令: %+v", gw.sent)
	}
	if n := auditCount(t, s, "account.decrypt_issue", acct); n != 1 { // 只有建席那一次
		t.Fatalf("不得新增解密审计, 得 %d", n)
	}
}

// C3：状态门——busy／disabled ⇒ invalid_state；error 可刷。
func TestRefreshCredStateGate(t *testing.T) {
	lc, gw, s, id, _ := mkRefresh(t)
	ctx := context.Background()
	for _, st := range []string{"busy", "disabled"} {
		if err := s.SetSeatState(ctx, id, st, "", "x", nil); err != nil {
			t.Fatal(err)
		}
		if err := lc.RefreshCred(ctx, id, "zzc"); err != ErrInvalidState {
			t.Fatalf("%s 态刷新应 invalid_state, 得 %v", st, err)
		}
	}
	if len(gw.sent) != 0 {
		t.Fatalf("状态门拒绝不得发指令: %+v", gw.sent)
	}
	if err := s.SetSeatState(ctx, id, "error", "start_timeout", "x", nil); err != nil {
		t.Fatal(err)
	}
	if err := lc.RefreshCred(ctx, id, "zzc"); err != nil {
		t.Fatalf("error 态应可刷新: %v", err)
	}
}

// C4：账号已停用／属主不符 ⇒ account_not_allowed，零指令。
func TestRefreshCredAccountGate(t *testing.T) {
	lc, gw, s, id, acct := mkRefresh(t)
	ctx := context.Background()
	if _, err := s.exec(ctx, `UPDATE g4_account_pool SET state='disabled' WHERE id=$1`, acct); err != nil {
		t.Fatal(err)
	}
	if err := lc.RefreshCred(ctx, id, "zzc"); err != ErrAccountNotAllowed {
		t.Fatalf("停用账号应 account_not_allowed, 得 %v", err)
	}
	if _, err := s.exec(ctx, `UPDATE g4_account_pool SET state='available', owner_lc='bob' WHERE id=$1`, acct); err != nil {
		t.Fatal(err)
	}
	if err := lc.RefreshCred(ctx, id, "zzc"); err != ErrAccountNotAllowed {
		t.Fatalf("属主不符应 account_not_allowed, 得 %v", err)
	}
	if len(gw.sent) != 0 {
		t.Fatalf("不得发指令: %+v", gw.sent)
	}
}

// C5／C6／C13：节点回报三分——不通／旧节点／写失败 ⇒ 席位回原状态；重启失败 ⇒ error/start_failed。
func TestRefreshCredNodeOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		down       bool
		errText    string
		want       error
		wantState  string
		wantDetail string
	}{
		{"node down", true, "", ErrNodeOffline, "online", ""},
		{"old node", false, "unknown_command_type", ErrNodeUnsupported, "online", ""},
		{"write failed", false, "credential_write_failed: create temp: no space", ErrCredInjectFailed, "online", ""},
		{"restart failed", false, "restart_failed: engine 500", ErrStartFailed, "error", "start_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lc, gw, s, id, _ := mkRefresh(t)
			gw.down, gw.errText = c.down, c.errText
			if err := lc.RefreshCred(context.Background(), id, "zzc"); err != c.want {
				t.Fatalf("得 %v want %v", err, c.want)
			}
			seat, _ := s.GetSeat(context.Background(), id)
			if seat.State != c.wantState || seat.StateDetail != c.wantDetail {
				t.Fatalf("席位 %s/%s want %s/%s", seat.State, seat.StateDetail, c.wantState, c.wantDetail)
			}
		})
	}
}

// C7／C11／C13（job）：refresh_cred 走 A1 job——旧节点 ⇒ job failed/node_unsupported；
// 同席位有异 op 活跃 job ⇒ invalid_state；同 op 返回原 job。
func TestRefreshCredAsJob(t *testing.T) {
	lc, gw, s, id, _ := mkRefresh(t)
	ctx := context.Background()
	gw.errText = "unknown_command_type"
	jid, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "refresh_cred"})
	if err != nil {
		t.Fatal(err)
	}
	lc.RunJob(ctx, jid)
	j, _ := lc.getJob(ctx, jid)
	if j.Stage != "failed" || j.Reason != "node_unsupported" {
		t.Fatalf("旧节点 job 应 failed/node_unsupported: %+v", j)
	}
	if seat, _ := s.GetSeat(ctx, id); seat.State != "online" {
		t.Fatalf("旧节点情形席位应回 online: %+v", seat)
	}
	// 成功：job 留 running 直到连回；期间发 restart（异 op）⇒ invalid_state；再发 refresh（同 op）⇒ 原 job
	gw.errText = ""
	jid2, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "refresh_cred"})
	if err != nil {
		t.Fatal(err)
	}
	lc.RunJob(ctx, jid2)
	if j, _ := lc.getJob(ctx, jid2); j.Stage != "running" {
		t.Fatalf("刷新成功后 job 应 running 等连回: %+v", j)
	}
	if _, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "restart"}); err != ErrInvalidState {
		t.Fatalf("刷新进行中发重启应 invalid_state, 得 %v", err)
	}
	if again, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "refresh_cred"}); err != nil || again != jid2 {
		t.Fatalf("同 op 应返回原 job: %q %v", again, err)
	}
	// 连回后读进度 ⇒ completed
	if _, err := s.TransitionSeatState(ctx, id, "starting", "online", "", "online", nil); err != nil {
		t.Fatal(err)
	}
	if v, err := lc.JobProgressFor(ctx, jid2, "zzc"); err != nil || v.Stage != "completed" {
		t.Fatalf("连回后进度应 completed: %+v %v", v, err)
	}
}

// C10：Hub 重启时 RecoverJobs 收尾 running 的 refresh_cred：席位 online ⇒ completed；否则 failed/internal_error。
func TestRefreshCredRecoverJobs(t *testing.T) {
	lc, _, s, id, _ := mkRefresh(t)
	ctx := context.Background()
	jid, err := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "refresh_cred"})
	if err != nil {
		t.Fatal(err)
	}
	lc.RunJob(ctx, jid) // 席位停 starting
	if err := lc.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if j, _ := lc.getJob(ctx, jid); j.Stage != "failed" || j.Reason != "internal_error" {
		t.Fatalf("席位非 online ⇒ failed/internal_error: %+v", j)
	}
	if _, err := s.TransitionSeatState(ctx, id, "starting", "online", "", "online", nil); err != nil {
		t.Fatal(err)
	}
	jid2, _ := lc.EnqueueJob(ctx, LifecycleJobRequest{AgentID: id, Actor: "zzc", Op: "refresh_cred"})
	lc.RunJob(ctx, jid2)
	if _, err := s.TransitionSeatState(ctx, id, "starting", "online", "", "online", nil); err != nil {
		t.Fatal(err)
	}
	if err := lc.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if j, _ := lc.getJob(ctx, jid2); j.Stage != "completed" {
		t.Fatalf("席位 online ⇒ completed: %+v", j)
	}
}
