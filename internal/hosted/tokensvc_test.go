package hosted

import (
	"context"
	"testing"
)

func TestTokenWriteBackAndUnbindPrep(t *testing.T) {
	svc, s := mkPoolSvc(t)
	ctx := context.Background()

	a, err := svc.Register(ctx, "admin", true, "team-codex", "token_file", "codex", "gpt", `{"token":"v1-old"}`)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := svc.Bind(ctx, "admin", true, a.ID, "ag_1"); err != nil {
		t.Fatalf("bind: %v", err)
	}

	// 未绑定席位上送必拒（反向）
	if err := svc.WriteBackRenewedToken(ctx, "ag_ghost", `{"token":"v2"}`, "ag_ghost"); err != ErrNoBoundAccount {
		t.Fatalf("未绑定席位回写应 ErrNoBoundAccount, 得 %v", err)
	}

	// 续期回写：池内副本变为新值（以容器内为准）、last4 更新、审计恰好一条
	if err := svc.WriteBackRenewedToken(ctx, "ag_1", `{"token":"v2-renewed"}`, "ag_1"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	plain, err := svc.DecryptForSeat(ctx, "system", a.ID, "ag_1")
	if err != nil || plain != `{"token":"v2-renewed"}` {
		t.Fatalf("回写后应解到新值: %v", err)
	}
	got, _ := s.GetAccount(ctx, a.ID)
	if got.Last4 != `ed"}` { // token_file 的内容尾部（JSON 结构符）——见设计注记
		t.Fatalf("回写后 last4: %q", got.Last4)
	}
	var n int
	if err := func() error {
		row, err := s.qrow(ctx, `SELECT COUNT(*) FROM g4_audit WHERE action='account.token_writeback' AND target=$1`, a.ID)
		if err != nil {
			return err
		}
		return row.Scan(&n)
	}(); err != nil || n != 1 {
		t.Fatalf("回写审计应恰好一条: n=%d err=%v", n, err)
	}

	// 空内容不回写（席位侧异常防护）
	if err := svc.WriteBackRenewedToken(ctx, "ag_1", "", "ag_1"); err != nil {
		t.Fatalf("空内容应静默不回写: %v", err)
	}
	if err := func() error {
		row, err := s.qrow(ctx, `SELECT COUNT(*) FROM g4_audit WHERE action='account.token_writeback' AND target=$1`, a.ID)
		if err != nil {
			return err
		}
		return row.Scan(&n)
	}(); err != nil || n != 1 {
		t.Fatalf("空内容不应新增审计: n=%d", n)
	}

	// 解绑前置：拿到绑定账号；解绑后再问必拒
	acctID, err := svc.PrepareUnbind(ctx, "ag_1")
	if err != nil || acctID != a.ID {
		t.Fatalf("PrepareUnbind: %v", err)
	}
	if err := svc.Unbind(ctx, "admin", true, a.ID); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if _, err := svc.PrepareUnbind(ctx, "ag_1"); err != ErrNoBoundAccount {
		t.Fatalf("解绑后应无绑定: %v", err)
	}
}
