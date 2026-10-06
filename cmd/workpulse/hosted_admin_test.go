package main

// hosted-admin 场景测试：原格 tosonnet55tc2k 写于 R-G4-INTEGRATION-RV 复验
// （runbooks/attachments/hosted_admin_scenarios_test.go.txt，分支 tosonnet55tc2k/review-g4-integration dd1b6a1e），
// R-G4-HOSTED-ADMIN-TESTS 派工入仓：去 ZZ 前缀；新增 S8（stdin 超限拒）与 S9（版本旗标落库）。

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func hostedAdminRun(t *testing.T, dsn, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("DW_NEWPLAT_PG_DSN", dsn)
	r, w, _ := os.Pipe()
	go func() { _, _ = w.WriteString(stdin); _ = w.Close() }() // 异步写：超限格的大输入不堵管道缓冲
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	var out bytes.Buffer
	or, ow, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = ow
	done := make(chan string)
	go func() { b, _ := io.ReadAll(or); done <- string(b) }()
	err := execute(append([]string{"workpulse", "hosted-admin"}, args...), &out, slog.New(slog.NewTextHandler(io.Discard, nil)), func(*slog.Logger) error {
		t.Fatal("server must not start")
		return nil
	})
	_ = ow.Close()
	os.Stdout = oldOut
	return out.String() + <-done, err
}

func hostedAdminDump(t *testing.T, dsn, q string) string {
	t.Helper()
	ctx := context.Background()
	pool, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tc, _ := repo.NewAccessCtx("test", "zz-hosted-admin")
	rows, err := pool.Query(ctx, tc, q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		b.WriteString(s + "\n")
	}
	return b.String()
}

func hostedAdminExec(t *testing.T, dsn, q string, args ...any) {
	t.Helper()
	ctx := context.Background()
	pool, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tc, _ := repo.NewAccessCtx("test", "zz-hosted-admin")
	if _, err := pool.Exec(ctx, tc, q, args...); err != nil {
		t.Fatal(err)
	}
}

func TestHostedAdminScenarios(t *testing.T) {
	dsn := pgtest.DSN(t)
	mk := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(mk, []byte(strings.Repeat("ab", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DW_G4_MASTER_KEY_FILE", mk)

	// S1 register-node：凭据只在 stdout 出现一次，库里无明文
	out, err := hostedAdminRun(t, dsn, "", "register-node", "--id", "nd_zz", "--name", "zz")
	if err != nil || !strings.Contains(out, "节点凭据") {
		t.Fatalf("S1 register-node: %v %q", err, out)
	}
	nodeSecret := strings.TrimSpace(out[strings.LastIndex(out, ": ")+2:])
	if len(nodeSecret) < 16 {
		t.Fatalf("S1 node secret looks too short: %q", nodeSecret)
	}
	if d := hostedAdminDump(t, dsn, `SELECT row_to_json(c)::text FROM g4_node_credential c`); strings.Contains(d, nodeSecret) {
		t.Fatalf("S1 node credential stored in plaintext: %s", d)
	}
	// S2 0.2.1.101（R-V2-ACCOUNT-SEED-CLI）：seed-account 已删除——管理员不代人登记、不碰密钥
	const sec = "sk-zzhostedadmin-SECRETVALUE-123456"
	_, err = hostedAdminRun(t, dsn, sec+"\n", "seed-account", "--name", "zz-key", "--kind", "api_key", "--cli", "codex", "--llm", "gpt", "--secret-stdin")
	if err == nil || !strings.Contains(err.Error(), "未知子命令") || strings.Contains(err.Error(), sec) {
		t.Fatalf("S2 seed-account must be gone (unknown subcommand, no echo): %v", err)
	}
	if d := hostedAdminDump(t, dsn, `SELECT row_to_json(a)::text FROM g4_account_pool a`); strings.Contains(d, "zz-key") || strings.Contains(d, "SECRETVALUE") {
		t.Fatalf("S2 nothing may be stored: %s", d)
	}
	// S3 assign-owner：只给无主存量行指派；已有主拒
	hostedAdminExec(t, dsn, `INSERT INTO g4_account_pool(id,name,kind,cli,llm,secret_enc,key_version,created_by) VALUES('acct_zzlegacy','legacy','token_file','codex','gpt','\x00',1,'hosted-admin')`)
	if _, err := hostedAdminRun(t, dsn, "", "assign-owner", "--account", "acct_zzlegacy", "--owner", "ZZC"); err != nil {
		t.Fatalf("S3 assign-owner: %v", err)
	}
	if d := hostedAdminDump(t, dsn, `SELECT owner_lc FROM g4_account_pool WHERE id='acct_zzlegacy'`); strings.TrimSpace(d) != "zzc" {
		t.Fatalf("S3 owner must be zzc (lower-cased): %q", d)
	}
	if _, err := hostedAdminRun(t, dsn, "", "assign-owner", "--account", "acct_zzlegacy", "--owner", "bob"); err == nil {
		t.Fatal("S3 re-assigning an owned account must be rejected")
	}
	// S4 account-disable／enable；有存活引用不许停
	if _, err := hostedAdminRun(t, dsn, "", "account-disable", "--account", "acct_zzlegacy"); err != nil {
		t.Fatalf("S4 disable: %v", err)
	}
	if d := hostedAdminDump(t, dsn, `SELECT state FROM g4_account_pool WHERE id='acct_zzlegacy'`); strings.TrimSpace(d) != "disabled" {
		t.Fatalf("S4 state: %q", d)
	}
	if _, err := hostedAdminRun(t, dsn, "", "account-enable", "--account", "acct_zzlegacy"); err != nil {
		t.Fatalf("S4 enable: %v", err)
	}
	hostedAdminExec(t, dsn, `INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,account_ref,plan,state,created_by) VALUES('ag_zzref','nd_zz','codex','gpt','token_file','acct_zzlegacy','2c4g','online','zzc')`)
	if _, err := hostedAdminRun(t, dsn, "", "account-disable", "--account", "acct_zzlegacy"); err == nil {
		t.Fatal("S4 disabling an in-use account must be rejected")
	}
	// S5 缺 --account
	if _, err := hostedAdminRun(t, dsn, "", "account-disable"); err == nil {
		t.Fatal("S5 missing --account must be rejected")
	}
}

// S9：register-node 的版本旗标落库（默认仅联调）；L5 默认值。
func TestHostedAdminVersionFlagsAndDefaults(t *testing.T) {
	dsn := pgtest.DSN(t)
	if _, err := hostedAdminRun(t, dsn, "", "register-node", "--id", "nd_ver", "--name", "ver", "--docker-version", "28", "--protocol-version", "2"); err != nil {
		t.Fatalf("S9 register-node with version flags: %v", err)
	}
	d := hostedAdminDump(t, dsn, `SELECT docker_version || '/' || node_protocol_version FROM g4_node WHERE id='nd_ver'`)
	if strings.TrimSpace(d) != "28/2" {
		t.Fatalf("S9 versions must land: %q", d)
	}
	if _, err := hostedAdminRun(t, dsn, "", "register-node", "--id", "nd_def", "--name", "def"); err != nil {
		t.Fatalf("L5 register-node defaults: %v", err)
	}
	if d := hostedAdminDump(t, dsn, `SELECT docker_version || '/' || node_protocol_version FROM g4_node WHERE id='nd_def'`); strings.TrimSpace(d) != "29/1" {
		t.Fatalf("L5 default versions must be 29/1: %q", d)
	}
}
