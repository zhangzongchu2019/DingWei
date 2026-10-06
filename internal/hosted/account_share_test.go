package hosted

// 0.2.1.101 账号包（R-V2-ACCOUNT-PER-USER／ACCOUNT-SHARE）：按人隔离、本人共用、停用守卫与建席串行化、
// 迁移回填与幂等、回退复位 SQL、刷新凭据的超时口径。编号对方案件 PLAN-R-V2-ACCOUNT-PER-USER-opus50tc2k.md。

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func mkAcct(t *testing.T, svc *PoolService, owner, name string) Account {
	t.Helper()
	a, err := svc.RegisterOwned(context.Background(), owner, name, "token_file", "codex", "gpt", `{"access_token":"t-`+name+`"}`)
	if err != nil {
		t.Fatalf("register %s/%s: %v", owner, name, err)
	}
	return a
}

func seatWith(id, owner, acct string) Seat {
	return Seat{AgentID: id, NodeID: "nd_t", CLI: "codex", LLM: "gpt", CredKind: "token_file",
		AccountRef: sql.NullString{String: acct, Valid: acct != ""}, Plan: "2c4g", ImageRef: "hub/base@sha256:x", CreatedBy: owner}
}

func ensureNode(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.exec(context.Background(), `INSERT INTO g4_node(id,name) VALUES('nd_t','nt') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
}

// P5：两人同名都可建；本人重名拒（不泄露他人名字）；旧的全局 UNIQUE 确已去掉。
func TestAccountNameUniquePerOwner(t *testing.T) {
	svc, _ := mkPoolSvc(t)
	mkAcct(t, svc, "zzc", "my-key")
	mkAcct(t, svc, "bob", "my-key")
	if _, err := svc.RegisterOwned(context.Background(), "zzc", "my-key", "api_key", "codex", "gpt", "sk-1234"); err != ErrAccountNameTaken {
		t.Fatalf("本人重名应 ErrAccountNameTaken, 得 %v", err)
	}
}

// P13：每人上限 20（停用的也计入）；他人不受影响。
func TestAccountLimitPerOwner(t *testing.T) {
	svc, _ := mkPoolSvc(t)
	ctx := context.Background()
	var last Account
	for i := 0; i < MaxAccountsPerOwner; i++ {
		last = mkAcct(t, svc, "zzc", "k"+string(rune('a'+i)))
	}
	if err := svc.SetOwnedState(ctx, "zzc", last.ID, true); err != nil { // 停用不释放名额
		t.Fatal(err)
	}
	if _, err := svc.RegisterOwned(ctx, "zzc", "one-more", "api_key", "codex", "gpt", "sk-1234"); err != ErrAccountLimit {
		t.Fatalf("第 21 个应 ErrAccountLimit, 得 %v", err)
	}
	if l, _ := svc.ListOwned(ctx, "zzc"); len(l) != MaxAccountsPerOwner {
		t.Fatalf("库内仍应 %d 个, 得 %d", MaxAccountsPerOwner, len(l))
	}
	mkAcct(t, svc, "bob", "bob-1") // 他人不受影响
}

// P7：凭据校验——token_file 须 JSON、超 64KiB 拒、空拒；错误文本不回显输入。
func TestAccountSecretValidation(t *testing.T) {
	svc, _ := mkPoolSvc(t)
	ctx := context.Background()
	const marker = "SECRET-MARKER-should-not-echo"
	_, err := svc.RegisterOwned(ctx, "zzc", "bad-json", "token_file", "codex", "gpt", "not json "+marker)
	if !errors.Is(err, ErrParams) || strings.Contains(err.Error(), marker) {
		t.Fatalf("token_file 非 JSON 应 ErrParams 且不回显, 得 %v", err)
	}
	big := `{"x":"` + strings.Repeat("a", MaxSecretBytes) + `"}`
	if _, err := svc.RegisterOwned(ctx, "zzc", "too-big", "token_file", "codex", "gpt", big); err != ErrSecretTooLarge {
		t.Fatalf("超限应 ErrSecretTooLarge, 得 %v", err)
	}
	if _, err := svc.RegisterOwned(ctx, "zzc", "empty", "api_key", "codex", "gpt", ""); !errors.Is(err, ErrParams) {
		t.Fatalf("空密钥应 ErrParams, 得 %v", err)
	}
	a := mkAcct(t, svc, "zzc", "ok")
	if _, err := svc.UpdateSecretOwned(ctx, "zzc", a.ID, "not json"); !errors.Is(err, ErrParams) {
		t.Fatalf("换密同样校验 JSON, 得 %v", err)
	}
}

// T14（S14）：同账号并发建两席都成功（不得有唯一约束把共用挡掉）；in_use＝2。
func TestConcurrentCreateSameAccount(t *testing.T) {
	svc, s := mkPoolSvc(t)
	ensureNode(t, s)
	a := mkAcct(t, svc, "zzc", "shared")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{"ag_c1", "ag_c2"} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			errs[i] = s.CreateSeat(context.Background(), seatWith(id, "zzc", a.ID))
		}(i, id)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发建席 %d: %v", i, err)
		}
	}
	if n, _ := s.AccountInUse(context.Background(), a.ID); n != 2 {
		t.Fatalf("in_use 应 2, 得 %d", n)
	}
}

// T2／T9／T8：释放一席不误伤另一席；并发释放两席后可停用；历史 destroyed 行（account_ref 未清）不计数、不挡停用。
func TestReleaseAndDestroyedNotCounted(t *testing.T) {
	svc, s := mkPoolSvc(t)
	ctx := context.Background()
	ensureNode(t, s)
	a := mkAcct(t, svc, "zzc", "shared")
	for _, id := range []string{"ag_r1", "ag_r2"} {
		if err := s.CreateSeat(ctx, seatWith(id, "zzc", a.ID)); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Release(ctx, "system", "ag_r1"); err != nil {
		t.Fatal(err)
	}
	if ref, err := s.SeatAccountOf(ctx, "ag_r2"); err != nil || ref != a.ID {
		t.Fatalf("另一席引用不得被误伤: %q %v", ref, err)
	}
	if _, err := svc.DecryptForSeat(ctx, "system", a.ID, "ag_r2"); err != nil {
		t.Fatalf("另一席仍可解密: %v", err)
	}
	if _, err := svc.DecryptForSeat(ctx, "system", a.ID, "ag_r1"); err != ErrAccountBound {
		t.Fatalf("已释放席不可再解密: %v", err)
	}
	// 并发释放 ag_r2（与重复释放 ag_r1）——只动各自
	var wg sync.WaitGroup
	for _, id := range []string{"ag_r1", "ag_r2"} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); _ = svc.Release(ctx, "system", id) }(id)
	}
	wg.Wait()
	if n, _ := s.AccountInUse(ctx, a.ID); n != 0 {
		t.Fatalf("全释放后 in_use 应 0, 得 %d", n)
	}
	// 历史形态：destroyed 但 account_ref 仍指向本号 ⇒ 不计数、不挡停用
	mkSeatRow(t, s, "ag_old", "zzc", a.ID, "destroyed")
	if n, _ := s.AccountInUse(ctx, a.ID); n != 0 {
		t.Fatalf("destroyed 行不应计数, 得 %d", n)
	}
	if err := svc.SetOwnedState(ctx, "zzc", a.ID, true); err != nil {
		t.Fatalf("仅 destroyed 引用时应可停用: %v", err)
	}
}

// T11（确定性交错，两种先后）：建席持 FOR SHARE 未插行 ⇔ 停用持 FOR UPDATE 未数引用。
// 任一先后都必须恰一成功，不得出现「已停用账号被新席引用」。撤锁 ⇒ 交错下必红（R8）。
func TestCreateVsDisableSerialized(t *testing.T) {
	t.Run("create first", func(t *testing.T) {
		svc, s := mkPoolSvc(t)
		ctx := context.Background()
		ensureNode(t, s)
		a := mkAcct(t, svc, "zzc", "race")
		locked, release := make(chan struct{}), make(chan struct{})
		s.afterSeatAccountLock = func() { close(locked); <-release }
		createErr := make(chan error, 1)
		go func() { createErr <- s.CreateSeat(ctx, seatWith("ag_x", "zzc", a.ID)) }()
		<-locked
		s.afterSeatAccountLock = nil
		disableErr := make(chan error, 1)
		go func() { disableErr <- svc.SetOwnedState(ctx, "zzc", a.ID, true) }()
		select {
		case err := <-disableErr:
			t.Fatalf("停用在建席持锁期间不得完成（应等锁）, 得 %v", err)
		case <-time.After(300 * time.Millisecond):
		}
		close(release)
		if err := <-createErr; err != nil {
			t.Fatalf("建席应成功: %v", err)
		}
		if err := <-disableErr; err != ErrAccountInUse {
			t.Fatalf("停用应数到新引用而拒, 得 %v", err)
		}
		assertNoDisabledWithRef(t, s, a.ID)
	})
	t.Run("disable first", func(t *testing.T) {
		svc, s := mkPoolSvc(t)
		ctx := context.Background()
		ensureNode(t, s)
		a := mkAcct(t, svc, "zzc", "race")
		locked, release := make(chan struct{}), make(chan struct{})
		s.beforeDisableCount = func() { close(locked); <-release }
		disableErr := make(chan error, 1)
		go func() { disableErr <- svc.SetOwnedState(ctx, "zzc", a.ID, true) }()
		<-locked
		s.beforeDisableCount = nil
		createErr := make(chan error, 1)
		go func() { createErr <- s.CreateSeat(ctx, seatWith("ag_y", "zzc", a.ID)) }()
		select {
		case err := <-createErr:
			t.Fatalf("建席在停用持锁期间不得完成（应等锁）, 得 %v", err)
		case <-time.After(300 * time.Millisecond):
		}
		close(release)
		if err := <-disableErr; err != nil {
			t.Fatalf("停用应成功: %v", err)
		}
		if err := <-createErr; err != ErrAccountNotAllowed {
			t.Fatalf("建席应见已停用而拒, 得 %v", err)
		}
		assertNoDisabledWithRef(t, s, a.ID)
	})
}

func assertNoDisabledWithRef(t *testing.T, s *Store, acctID string) {
	t.Helper()
	a, err := s.GetAccount(context.Background(), acctID)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := s.AccountInUse(context.Background(), acctID)
	if a.State == "disabled" && n > 0 {
		t.Fatalf("出现「已停用账号被 %d 个席位引用」", n)
	}
}

// 9.5：条件插入失败区分原因——主键冲突原样返回存储错误，不映射成 account_not_allowed。
func TestCreateSeatPKConflictIsNotAccountError(t *testing.T) {
	svc, s := mkPoolSvc(t)
	ensureNode(t, s)
	a := mkAcct(t, svc, "zzc", "pk")
	if err := s.CreateSeat(context.Background(), seatWith("ag_dup", "zzc", a.ID)); err != nil {
		t.Fatal(err)
	}
	err := s.CreateSeat(context.Background(), seatWith("ag_dup", "zzc", a.ID))
	if err == nil || errors.Is(err, ErrAccountNotAllowed) {
		t.Fatalf("主键冲突应为存储错误、不是 account_not_allowed, 得 %v", err)
	}
}

// P8／P8'／P9：存量回填（恰一授权 ⇒ 归该用户；多授权／无授权保持无主；已有属主不被覆盖）、ApplySchema 幂等、
// job op 的 CHECK 收 refresh_cred、仍拒未知 op。
func TestSchemaBackfillAndJobOp(t *testing.T) {
	_, s := mkPoolSvc(t)
	ctx := context.Background()
	ins := func(id, owner string) {
		if _, err := s.exec(ctx, `INSERT INTO g4_account_pool(id,name,kind,cli,llm,secret_enc,key_version,created_by,owner_lc)
VALUES($1,$1,'api_key','codex','gpt','\x00',1,'x',$2)`, id, owner); err != nil {
			t.Fatal(err)
		}
	}
	grant := func(id, u string) {
		if _, err := s.exec(ctx, `INSERT INTO g4_account_grant(account_id,user_lc,granted_by) VALUES($1,$2,'x')`, id, u); err != nil {
			t.Fatal(err)
		}
	}
	ins("acct_one", "")
	grant("acct_one", "zzc")
	ins("acct_two", "")
	grant("acct_two", "zzc")
	grant("acct_two", "bob")
	ins("acct_none", "")
	ins("acct_owned", "carol")
	grant("acct_owned", "zzc")
	for i := 0; i < 2; i++ { // 幂等
		if err := s.ApplySchema(ctx); err != nil {
			t.Fatalf("apply #%d: %v", i, err)
		}
	}
	want := map[string]string{"acct_one": "zzc", "acct_two": "", "acct_none": "", "acct_owned": "carol"}
	for id, w := range want {
		a, err := s.GetAccount(ctx, id)
		if err != nil || a.OwnerLC != w {
			t.Fatalf("%s owner=%q want %q (%v)", id, a.OwnerLC, w, err)
		}
	}
	// 无主行对任何人不可见
	if l, _ := s.ListUsableAccounts(ctx, "zzc", "", ""); len(l) != 1 || l[0].ID != "acct_one" {
		t.Fatalf("zzc 只应见 acct_one: %+v", l)
	}
	ensureNode(t, s)
	mkSeatRow(t, s, "ag_j", "zzc", "", "online")
	if _, err := s.exec(ctx, `INSERT INTO g4_lifecycle_job(id,agent_id,created_by,op) VALUES('job_r','ag_j','zzc','refresh_cred')`); err != nil {
		t.Fatalf("refresh_cred 应可入 job 表: %v", err)
	}
	if _, err := s.exec(ctx, `INSERT INTO g4_lifecycle_job(id,agent_id,created_by,op,stage) VALUES('job_u','ag_j','zzc','bogus','failed')`); err == nil {
		t.Fatal("未知 op 仍须被 CHECK 拒")
	}
}

// T15／R10：回退复位 SQL（换件单回退节照抄）——只把「bound 且无存活引用」的行放回 available。
const rollbackResetSQL = `UPDATE g4_account_pool SET state='available', bound_agent_id=NULL
 WHERE state='bound'
   AND NOT EXISTS (SELECT 1 FROM g4_hosted_seat s
                    WHERE s.account_ref = g4_account_pool.id AND s.state <> 'destroyed')`

func TestRollbackResetSQL(t *testing.T) {
	_, s := mkPoolSvc(t)
	ctx := context.Background()
	ensureNode(t, s)
	for _, id := range []string{"acct_free", "acct_used", "acct_destroyedref"} {
		if _, err := s.exec(ctx, `INSERT INTO g4_account_pool(id,name,kind,cli,llm,secret_enc,key_version,created_by,owner_lc,state,bound_agent_id)
VALUES($1,$1,'api_key','codex','gpt','\x00',1,'x','zzc','bound','ag_legacy')`, id); err != nil {
			t.Fatal(err)
		}
	}
	mkSeatRow(t, s, "ag_live", "zzc", "acct_used", "online")
	mkSeatRow(t, s, "ag_gone", "zzc", "acct_destroyedref", "destroyed")
	if _, err := s.exec(ctx, rollbackResetSQL); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"acct_free": "available", "acct_used": "bound", "acct_destroyedref": "available"} {
		a, _ := s.GetAccount(ctx, id)
		if a.State != want {
			t.Fatalf("%s state=%s want %s", id, a.State, want)
		}
	}
}

// 刷新凭据的 starting（detail=refresh）超时按既有席位处置：只记 error/start_timeout，不吊销、不释放引用。
// 撤掉 existingSeatStarting 对 refresh 的识别 ⇒ 被当新建回收（Retire＋释放）⇒ 红。
func TestRefreshStartingTimeoutIsExistingSeat(t *testing.T) {
	o, idp, _, s := mkOrch(t)
	ctx := context.Background()
	acct := acctOf(t, o.pool)
	ensureNode(t, s)
	if err := s.CreateSeat(ctx, seatWith("ag_rf", "zzc", acct)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeatState(ctx, "ag_rf", "starting", seatStartingRefresh, "refresh_cred", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.exec(ctx, `UPDATE g4_hosted_seat SET updated_at = now() - interval '10 minutes' WHERE agent_id='ag_rf'`); err != nil {
		t.Fatal(err)
	}
	retiredBefore := len(idp.retired)
	o.SweepTimeouts(ctx, time.Now())
	seat, _ := s.GetSeat(ctx, "ag_rf")
	if seat.State != "error" || seat.StateDetail != "start_timeout" {
		t.Fatalf("刷新超时应 error/start_timeout: %+v", seat)
	}
	if len(idp.retired) != retiredBefore {
		t.Fatalf("既有席位不得被吊销: %v", idp.retired)
	}
	if ref, err := s.SeatAccountOf(ctx, "ag_rf"); err != nil || ref != acct {
		t.Fatalf("既有席位不得释放账号引用: %q %v", ref, err)
	}
}

// T13：存量形态（旧版独占留下的 state='bound'、bound_agent_id 指向旧席）仍可被本人第二席共用——
// 可选只排除 disabled，不认 bound；R2（下拉恢复只认 available）⇒ 本格红。
func TestLegacyBoundAccountSharable(t *testing.T) {
	_, s := mkPoolSvc(t)
	ctx := context.Background()
	ensureNode(t, s)
	if _, err := s.exec(ctx, `INSERT INTO g4_account_pool(id,name,kind,cli,llm,secret_enc,key_version,created_by,owner_lc,state,bound_agent_id)
VALUES('acct_legacy','legacy','token_file','codex','gpt','\x00',1,'x','zzc','bound','ag_old')`); err != nil {
		t.Fatal(err)
	}
	mkSeatRow(t, s, "ag_old", "zzc", "acct_legacy", "online")
	u, err := s.ListUsableAccounts(ctx, "zzc", "codex", "gpt")
	if err != nil || len(u) != 1 || u[0].InUse != 1 {
		t.Fatalf("存量 bound 号应仍可选、in_use=1: %+v %v", u, err)
	}
	if err := s.CreateSeat(ctx, seatWith("ag_new", "zzc", "acct_legacy")); err != nil {
		t.Fatalf("存量 bound 号建第二席: %v", err)
	}
	if n, _ := s.AccountInUse(ctx, "acct_legacy"); n != 2 {
		t.Fatalf("in_use 应 2, 得 %d", n)
	}
}
