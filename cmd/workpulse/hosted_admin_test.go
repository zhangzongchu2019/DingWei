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

func TestHostedAdminScenarios(t *testing.T) {
	dsn := pgtest.DSN(t)
	mk := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(mk, []byte(strings.Repeat("ab", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DW_G4_MASTER_KEY_FILE", mk)
	const sec = "sk-zzhostedadmin-SECRETVALUE-123456"

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
	// S2 seed-account：口令走 stdin、stdout 不回显、库里是密文
	out, err = hostedAdminRun(t, dsn, sec+"\n", "seed-account", "--name", "zz-key", "--kind", "api_key", "--cli", "codex", "--llm", "gpt", "--secret-stdin")
	if err != nil || strings.Contains(out, sec) {
		t.Fatalf("S2 seed-account: %v out=%q", err, out)
	}
	if d := hostedAdminDump(t, dsn, `SELECT row_to_json(a)::text FROM g4_account_pool a`); strings.Contains(d, sec) || strings.Contains(d, "SECRETVALUE") {
		t.Fatalf("S2 secret stored in plaintext: %s", d)
	}
	t.Logf("S2 ok; account row: %.200s", hostedAdminDump(t, dsn, `SELECT row_to_json(a)::text FROM g4_account_pool a`))
	// S3 把口令放命令行：未注册的旗标必须被拒，错误里不得回显口令
	out, err = hostedAdminRun(t, dsn, "", "seed-account", "--name", "zz-cli", "--kind", "api_key", "--cli", "codex", "--llm", "gpt", "--secret", sec)
	if err == nil || strings.Contains(out+err.Error(), sec) {
		t.Fatalf("S3 --secret on the command line must be rejected without echo: err=%v out=%q", err, out)
	}
	// S4 缺主密钥文件：拒绝且不读 stdin 里的口令入库
	t.Setenv("DW_G4_MASTER_KEY_FILE", filepath.Join(t.TempDir(), "absent.key"))
	_, err = hostedAdminRun(t, dsn, "sk-should-not-be-stored-9999\n", "seed-account", "--name", "zz-nokey", "--kind", "api_key", "--cli", "codex", "--llm", "gpt", "--secret-stdin")
	if err == nil {
		t.Fatal("S4 missing master key must fail")
	}
	if d := hostedAdminDump(t, dsn, `SELECT row_to_json(a)::text FROM g4_account_pool a`); strings.Contains(d, "zz-nokey") {
		t.Fatalf("S4 account stored despite missing master key: %s", d)
	}
	// S5 非法 kind
	t.Setenv("DW_G4_MASTER_KEY_FILE", mk)
	_, err = hostedAdminRun(t, dsn, "x\n", "seed-account", "--name", "zz-bad", "--kind", "password", "--cli", "codex", "--llm", "gpt", "--secret-stdin")
	if err == nil {
		t.Fatal("S5 invalid kind must be rejected")
	}
	// S6 授权给用户
	out, err = hostedAdminRun(t, dsn, "{\"tokens\":{\"access_token\":\"tok-zz\"}}\n", "seed-account", "--name", "zz-sub", "--kind", "token_file", "--cli", "codex", "--llm", "gpt", "--grant-to", "b12admin", "--secret-stdin")
	if err != nil {
		t.Fatalf("S6: %v", err)
	}
	if d := hostedAdminDump(t, dsn, `SELECT row_to_json(g)::text FROM g4_account_grant g`); !strings.Contains(d, "b12admin") {
		t.Fatalf("S6 grant missing: %q", d)
	}
	// S7 空口令
	_, err = hostedAdminRun(t, dsn, "\n", "seed-account", "--name", "zz-empty", "--kind", "api_key", "--cli", "codex", "--llm", "gpt", "--secret-stdin")
	t.Logf("S7 empty stdin err=%v", err)
	if err == nil {
		t.Fatal("S7 empty secret must be rejected")
	}
}

// S8：stdin 超上限（1MiB）即拒，不静默截断；S9：register-node 的版本旗标落库（默认仅联调）。
func TestHostedAdminLimitsAndVersionFlags(t *testing.T) {
	dsn := pgtest.DSN(t)
	mk := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(mk, []byte(strings.Repeat("ab", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DW_G4_MASTER_KEY_FILE", mk)

	// S8 超限 stdin（1MiB+2）⇒ 拒，账号不入库
	huge := strings.Repeat("x", maxSecretStdinBytes+2)
	_, err := hostedAdminRun(t, dsn, huge, "seed-account", "--name", "zz-huge", "--kind", "api_key", "--cli", "codex", "--llm", "gpt", "--secret-stdin")
	if err == nil || !strings.Contains(err.Error(), "超长") {
		t.Fatalf("S8 oversized stdin must be rejected: %v", err)
	}
	if d := hostedAdminDump(t, dsn, `SELECT row_to_json(a)::text FROM g4_account_pool a`); strings.Contains(d, "zz-huge") {
		t.Fatalf("S8 oversized secret must not be stored: %s", d)
	}

	// S9 显式版本旗标落库
	if _, err := hostedAdminRun(t, dsn, "", "register-node", "--id", "nd_ver", "--name", "ver", "--docker-version", "28", "--protocol-version", "2"); err != nil {
		t.Fatalf("S9 register-node with version flags: %v", err)
	}
	d := hostedAdminDump(t, dsn, `SELECT docker_version || '/' || node_protocol_version FROM g4_node WHERE id='nd_ver'`)
	if strings.TrimSpace(d) != "28/2" {
		t.Fatalf("S9 versions must land: %q", d)
	}
}
