package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// pgSchemaDSN 临时 PG 上一个新 schema 的连接串（经 search_path 切换）；applyTables 为真时建好身份存储全部表。
func pgSchemaDSN(t *testing.T, applyTables bool) string {
	t.Helper()
	dsn := pgtest.DSN(t)
	ctx := context.Background()
	tc, err := repo.NewAccessCtx("test", "bootstrap-admin")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, tc, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	sdsn := dsn + sep + "search_path=" + schema
	if applyTables {
		pool, err := repo.OpenPG(ctx, sdsn)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		for _, ddl := range identitytest.PGTestSchema() {
			if _, err := pool.Exec(ctx, tc, ddl); err != nil {
				t.Fatal(err)
			}
		}
	}
	return sdsn
}

func runCLI(t *testing.T, dsn string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("DW_NEWPLAT_PG_DSN", dsn)
	var out bytes.Buffer
	err := execute(append([]string{"workpulse"}, args...), &out, slog.New(slog.NewTextHandler(io.Discard, nil)), func(*slog.Logger) error {
		t.Fatal("server must not start for a subcommand")
		return nil
	})
	return out.String(), err
}

// 端到端：初始化脚本在 PG 上建第一个管理用户 ⇒ 再执行被拒 ⇒ Hub 连同一个库，该用户凭初始口令登录、被要求改密。
func TestBootstrapAdminEndToEnd(t *testing.T) {
	dsn := pgSchemaDSN(t, true)
	out, err := runCLI(t, dsn, "bootstrap-admin", "--user-id", "Boss", "--short-name", "bs", "--display-name", "老板")
	if err != nil {
		t.Fatalf("bootstrap-admin: %v", err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 || lines[0] != "BOOTSTRAP_ADMIN_CREATED user_id=Boss short_name=bs" || !strings.HasPrefix(lines[1], "initial_password=") {
		t.Fatalf("output: %q", out)
	}
	pw := strings.TrimPrefix(lines[1], "initial_password=")
	if len(pw) != identity.InitialPasswordLen {
		t.Fatalf("initial password %q", pw)
	}
	// 口令只出一次（tosonnet55tc2k 复验建议：多打印一次本用例原先不红）
	if n := strings.Count(out, pw); n != 1 {
		t.Fatalf("initial password printed %d times, want exactly once:\n%s", n, out)
	}
	// 库里无明文：用户表与审计表的每一列（整行转成 JSON 文本）都不得含口令（同一复验建议：明文写进 display_name 原先不红）
	pool, err := repo.OpenPG(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tc, err := repo.NewAccessCtx("test", "bootstrap-admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`SELECT row_to_json(u)::text FROM np_user u`, `SELECT row_to_json(a)::text FROM audit_log a`} {
		rows, err := pool.Query(context.Background(), tc, q)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatal(err)
			}
			n++
			if strings.Contains(row, pw) {
				t.Fatalf("plaintext initial password stored in DB (%s): %s", q, row)
			}
		}
		rows.Close()
		if n == 0 {
			t.Fatalf("%s returned no rows: the check would be vacuous", q)
		}
	}
	// 已有管理用户 ⇒ 拒绝，不输出任何口令
	out2, err := runCLI(t, dsn, "bootstrap-admin", "--user-id", "Boss2", "--short-name", "b2")
	if err == nil || !strings.Contains(err.Error(), "refused: a management user already exists") || out2 != "" {
		t.Fatalf("second run: %q %v", out2, err)
	}

	store, closeStore, err := openNewPlatformStore(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	mux := http.NewServeMux()
	if err := wireNewPlatform(context.Background(), mux, newplatHub(t), newPlatformConfig{publicOrigin: "https://x"}, store); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	login := func(user, pass string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]string{"user_id": user, "password": pass})
		req, _ := http.NewRequest("POST", srv.URL+"/console/login/password", bytes.NewReader(body))
		req.Header.Set("Origin", "https://x")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		m := map[string]any{}
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	if st, m := login("systemdefault", pw); st != 401 {
		t.Fatalf("systemdefault login: %d %v", st, m)
	}
	if st, m := login("boss", pw); st != 200 || m["must_change_password"] != true {
		t.Fatalf("admin login with initial password: %d %v", st, m)
	}
}

func TestBootstrapAdminRefusesBadInvocation(t *testing.T) {
	for _, args := range [][]string{
		{"bootstrap-admin"},
		{"bootstrap-admin", "--user-id", "Boss"},
		{"bootstrap-admin", "--short-name", "bs"},
		{"bootstrap-admin", "--user-id", "Boss", "--short-name", "bs", "extra"},
		{"bootstrap-admin", "--user-id", "Boss", "--short-name", "bs", "--password", "x"},
	} {
		if out, err := runCLI(t, "", args...); err == nil || !strings.HasPrefix(err.Error(), "usage: workpulse bootstrap-admin") || out != "" {
			t.Errorf("%v: %q %v", args, out, err)
		}
	}
	// 没给库 ⇒ 拒绝（不退回内存存储：那样建的账号 Hub 看不见）
	if _, err := runCLI(t, "", "bootstrap-admin", "--user-id", "Boss", "--short-name", "bs"); !errors.Is(err, errNewPlatformPGDSN) {
		t.Fatalf("no DSN: %v", err)
	}
	// 表没建 ⇒ 拒绝并报缺哪几张，不在半套表上写
	if _, err := runCLI(t, pgSchemaDSN(t, false), "bootstrap-admin", "--user-id", "Boss", "--short-name", "bs"); err == nil || !strings.Contains(err.Error(), "missing tables: np_user") {
		t.Fatalf("no tables: %v", err)
	}
	// 格式不对 ⇒ 拒绝，不建
	dsn := pgSchemaDSN(t, true)
	if _, err := runCLI(t, dsn, "bootstrap-admin", "--user-id", "B", "--short-name", "bs"); err == nil || !strings.Contains(err.Error(), "--user-id must be") {
		t.Fatalf("bad id: %v", err)
	}
	if out, err := runCLI(t, dsn, "bootstrap-admin", "--user-id", "Boss", "--short-name", "bs"); err != nil || !strings.HasPrefix(out, "BOOTSTRAP_ADMIN_CREATED") {
		t.Fatalf("valid after bad: %q %v", out, err)
	}
}
