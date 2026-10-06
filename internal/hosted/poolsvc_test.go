package hosted

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func mkMasterKeyPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mkPath := filepath.Join(dir, "master.key")
	if err := os.WriteFile(mkPath, []byte(hexOf(mustKey(t, 0x99))), 0o600); err != nil {
		t.Fatal(err)
	}
	return mkPath
}

func mkPoolSvc(t *testing.T) (*PoolService, *Store) {
	t.Helper()
	pool := testpool(t)
	ctx := context.Background()
	s := NewStore(pool)
	if err := s.DropSchema(ctx); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	mkPath := mkMasterKeyPath(t)
	svc, err := NewPoolService(s, mkPath)
	if err != nil {
		t.Fatalf("svc: %v", err)
	}
	return svc, s
}

func hexOf(b []byte) string {
	const hexd = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexd[v>>4]
		out[i*2+1] = hexd[v&0x0f]
	}
	return string(out)
}

// mkSeatRow 直接落一行席位（绕开编排层，便于构造引用、状态与属主组合）。node 'nd_t' 按需建。
func mkSeatRow(t *testing.T, s *Store, agentID, owner, acctID, state string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.exec(ctx, `INSERT INTO g4_node(id,name) VALUES('nd_t','nt') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	var ref any
	if acctID != "" {
		ref = acctID
	}
	if _, err := s.exec(ctx, `INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,account_ref,plan,state,created_by)
VALUES($1,'nd_t','codex','gpt','token_file',$2,'2c4g',$3,$4)`, agentID, ref, state, owner); err != nil {
		t.Fatal(err)
	}
}

func auditCount(t *testing.T, s *Store, action, target string) int {
	t.Helper()
	var n int
	row, err := s.qrow(context.Background(), `SELECT COUNT(*) FROM g4_audit WHERE action=$1 AND target=$2`, action, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// 0.2.1.101（R-V2-ACCOUNT-PER-USER）：属主本人登记／可见／解密边界／换密／停用守卫／启用；管理员只停用启用。
func TestOwnedAccountLifecycle(t *testing.T) {
	svc, s := mkPoolSvc(t)
	ctx := context.Background()

	// 登记：属主＝调用者；密钥只入一次；token_file 不落内容尾部
	a, err := svc.RegisterOwned(ctx, "zzc", "team-codex", "token_file", "codex", "gpt", `{"access_token":"sk-secret-abcd"}`)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if a.Last4 != "" || a.OwnerLC != "zzc" {
		t.Fatalf("登记结果: %+v", a)
	}
	// P2 隔离·列表：本人可见、他人不可见
	if l, _ := svc.ListOwned(ctx, "zzc"); len(l) != 1 {
		t.Fatalf("本人应见 1 个: %+v", l)
	}
	if l, _ := svc.ListOwned(ctx, "bob"); len(l) != 0 {
		t.Fatalf("他人不应见: %+v", l)
	}
	if u, _ := s.ListUsableAccounts(ctx, "bob", "codex", "gpt"); len(u) != 0 {
		t.Fatalf("他人下拉不应见: %+v", u)
	}
	if u, _ := s.ListUsableAccounts(ctx, "zzc", "codex", "gpt"); len(u) != 1 || u[0].InUse != 0 {
		t.Fatalf("本人下拉应见、in_use=0: %+v", u)
	}

	// 解密边界：引用本号且同属主的席位才给（P10 纵深：他人席位即便 account_ref 指向本号也拒）
	mkSeatRow(t, s, "ag_1", "zzc", a.ID, "online")
	mkSeatRow(t, s, "ag_2", "zzc", "", "online")
	mkSeatRow(t, s, "ag_bob", "bob", a.ID, "online")
	if _, err := svc.DecryptForSeat(ctx, "system", a.ID, "ag_2"); err != ErrAccountBound {
		t.Fatalf("未引用本号的席位解密必须拒, 得 %v", err)
	}
	if _, err := svc.DecryptForSeat(ctx, "system", a.ID, "ag_bob"); err != ErrAccountBound {
		t.Fatalf("他人席位解密必须拒（属主校验）, 得 %v", err)
	}
	plain, err := svc.DecryptForSeat(ctx, "system", a.ID, "ag_1")
	if err != nil || plain != `{"access_token":"sk-secret-abcd"}` {
		t.Fatalf("引用席解密: %v", err)
	}

	// P3 隔离·写：他人换密／停用 ⇒ ErrNotFound（与不存在同）
	if _, err := svc.UpdateSecretOwned(ctx, "bob", a.ID, `{"access_token":"x"}`); err != ErrNotFound {
		t.Fatalf("他人换密应 ErrNotFound, 得 %v", err)
	}
	if err := svc.SetOwnedState(ctx, "bob", a.ID, true); err != ErrNotFound {
		t.Fatalf("他人停用应 ErrNotFound, 得 %v", err)
	}
	// 换密：返回在用数（ag_1、ag_bob 都引用＝2），新值可解
	n, err := svc.UpdateSecretOwned(ctx, "zzc", a.ID, `{"access_token":"sk-new-wxyz"}`)
	if err != nil || n != 2 {
		t.Fatalf("换密: n=%d err=%v", n, err)
	}
	if plain, err = svc.DecryptForSeat(ctx, "system", a.ID, "ag_1"); err != nil || plain != `{"access_token":"sk-new-wxyz"}` {
		t.Fatalf("换密后应解新值: %v", err)
	}

	// 审计：register 1、decrypt 2、secret_update 1
	for action, want := range map[string]int{"account.register": 1, "account.decrypt_issue": 2, "account.secret_update": 1} {
		if got := auditCount(t, s, action, a.ID); got != want {
			t.Fatalf("审计 %s 应 %d 条, 得 %d", action, want, got)
		}
	}

	// 停用守卫：有存活引用 ⇒ ErrAccountInUse；释放后可停；停用后不可选；启用后可选
	if err := svc.SetOwnedState(ctx, "zzc", a.ID, true); err != ErrAccountInUse {
		t.Fatalf("有引用停用应 ErrAccountInUse, 得 %v", err)
	}
	for _, ag := range []string{"ag_1", "ag_bob"} {
		if err := svc.Release(ctx, "system", ag); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SetOwnedState(ctx, "zzc", a.ID, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if u, _ := s.ListUsableAccounts(ctx, "zzc", "codex", "gpt"); len(u) != 0 {
		t.Fatalf("停用后不可选: %+v", u)
	}
	if err := svc.SetOwnedState(ctx, "zzc", a.ID, false); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if u, _ := s.ListUsableAccounts(ctx, "zzc", "codex", "gpt"); len(u) != 1 {
		t.Fatalf("启用后应可选: %+v", u)
	}

	// 管理：普通用户 ⇒ ErrForbiddenRole；管理员可停用启用、列表含属主、不含密钥字段
	if err := svc.SetStateAdmin(ctx, "bob", false, a.ID, true); err != ErrForbiddenRole {
		t.Fatalf("普通用户管理停用应 ErrForbiddenRole, 得 %v", err)
	}
	if err := svc.SetStateAdmin(ctx, "admin", true, a.ID, true); err != nil {
		t.Fatalf("管理停用: %v", err)
	}
	lst, err := svc.ListAdmin(ctx, "admin", true)
	if err != nil || len(lst) != 1 || lst[0].OwnerLC != "zzc" || lst[0].State != "disabled" {
		t.Fatalf("管理列表: %+v %v", lst, err)
	}
}

// M1 返修第 16 条④：token_file 类不落内容尾部（页面显示「已入池＋登记时间」）；api_key 类照旧。
func TestTokenFileDisplayNoContentTail(t *testing.T) {
	svc, _ := mkPoolSvc(t)
	ctx := context.Background()
	a, err := svc.RegisterOwned(ctx, "zzc", "team-codex", "token_file", "codex", "gpt", `{"access_token":"abcd1234","refresh_token":"zz"}`)
	if err != nil {
		t.Fatal(err)
	}
	if a.Last4 != "" {
		t.Fatalf("token_file 类 Last4 必须为空（不落内容尾部）, 得 %q", a.Last4)
	}
	b, err := svc.RegisterOwned(ctx, "zzc", "team-ds", "api_key", "deepseek", "ds", "sk-test-5678")
	if err != nil {
		t.Fatal(err)
	}
	if b.Last4 != "5678" {
		t.Fatalf("api_key 类照旧后四位, 得 %q", b.Last4)
	}
}
