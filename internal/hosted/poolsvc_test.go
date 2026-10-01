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

func TestPoolServiceLifecycle(t *testing.T) {
	svc, s := mkPoolSvc(t)
	ctx := context.Background()

	// 普通用户一律拒（反向，V7-C6 同源）
	if _, err := svc.Register(ctx, "user1", false, "team-codex", "token_file", "codex", "gpt", "sk-secret-abcd"); err != ErrForbiddenRole {
		t.Fatalf("普通用户登记应 ErrForbiddenRole, 得 %v", err)
	}

	// 登记：密钥只入一次；返回值与列表都无明文、有后四位
	a, err := svc.Register(ctx, "admin", true, "team-codex", "token_file", "codex", "gpt", "sk-secret-abcd")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if a.Last4 != "" { // token_file 类不落内容尾部（第 16 条④；api_key 类才显示后四位）
		t.Fatalf("token_file last4 应为空, 得 %q", a.Last4)
	}
	lst, err := svc.ListAdmin(ctx, "admin", true)
	if err != nil || len(lst) != 1 {
		t.Fatalf("list: %v", err)
	}
	if lst[0].Last4 != "" {
		t.Fatal("token_file 列表不应带内容尾部")
	}

	// 授权 → 用户下拉可见；绑定 → 不再可选；解密只给绑定席
	if err := svc.Grant(ctx, "admin", true, a.ID, "zzc"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	usable, err := s.ListUsableAccounts(ctx, "zzc", "codex", "gpt")
	if err != nil || len(usable) != 1 {
		t.Fatalf("授权后应可见: %+v", usable)
	}
	if err := svc.Bind(ctx, "admin", true, a.ID, "ag_1"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if _, err := svc.DecryptForSeat(ctx, "system", a.ID, "ag_2"); err != ErrAccountBound {
		t.Fatalf("非绑定席解密必须拒（最小暴露反向）, 得 %v", err)
	}
	plain, err := svc.DecryptForSeat(ctx, "system", a.ID, "ag_1")
	if err != nil || plain != "sk-secret-abcd" {
		t.Fatalf("绑定席解密: %v", err)
	}

	// 换密：旧值解密路径已换、新值可解
	if err := svc.UpdateSecret(ctx, "admin", true, a.ID, "sk-new-wxyz"); err != nil {
		t.Fatalf("update: %v", err)
	}
	plain, err = svc.DecryptForSeat(ctx, "system", a.ID, "ag_1")
	if err != nil || plain != "sk-new-wxyz" {
		t.Fatalf("换密后应解新值: %v", err)
	}
	got, _ := s.GetAccount(ctx, a.ID)
	if got.Last4 != "" { // token_file 类换密后同样不落内容尾部
		t.Fatalf("换密后 token_file last4 应为空, 得 %q", got.Last4)
	}

	// 审计：register/grant/bind/decrypt×2/secret_update 各自恰好一条（T-G4-03）
	expect := map[string]int{
		"account.register": 1, "account.grant": 1, "account.bind": 1,
		"account.decrypt_issue": 2, "account.secret_update": 1,
	}
	for action, want := range expect {
		var n int
		if err := func() error {
 	row, err := s.qrow(ctx, `SELECT COUNT(*) FROM g4_audit WHERE action=$1 AND target=$2`, action, a.ID)
 	if err != nil {
 		return err
 	}
 	return row.Scan(&n)
 }(); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Fatalf("审计 %s 应 %d 条, 得 %d", action, want, n)
		}
	}

	// 绑定中停用必拒；解绑 → 回池可停
	if err := svc.Disable(ctx, "admin", true, a.ID); err == nil {
		t.Fatal("绑定中停用必须拒")
	}
	if err := svc.Unbind(ctx, "admin", true, a.ID); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if err := svc.Disable(ctx, "admin", true, a.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	usable, err = s.ListUsableAccounts(ctx, "zzc", "codex", "gpt")
	if err != nil || len(usable) != 0 {
		t.Fatalf("停用后不可选: %+v", usable)
	}
}

// M1 返修第 16 条④：token_file 类不落内容尾部（页面显示「已入池＋登记时间」）；api_key 类照旧。
func TestTokenFileDisplayNoContentTail(t *testing.T) {
	svc, _ := mkPoolSvc(t)
	ctx := context.Background()
	a, err := svc.Register(ctx, "admin", true, "team-codex", "token_file", "codex", "gpt", `{"access_token":"abcd1234","refresh_token":"zz"}`)
	if err != nil {
		t.Fatal(err)
	}
	if a.Last4 != "" {
		t.Fatalf("token_file 类 Last4 必须为空（不落内容尾部）, 得 %q", a.Last4)
	}
	b, err := svc.Register(ctx, "admin", true, "team-ds", "api_key", "deepseek", "ds", "sk-test-5678")
	if err != nil {
		t.Fatal(err)
	}
	if b.Last4 != "5678" {
		t.Fatalf("api_key 类照旧后四位, 得 %q", b.Last4)
	}
}
