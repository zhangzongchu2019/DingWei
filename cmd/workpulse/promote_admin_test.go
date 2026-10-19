package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// R6 端到端：promote-admin 在临时 PG 上把 active 的 manager 提升为 platform_admin（按 user_id 或 short_name），
// 同一事务写一笔审计（含原类型）；已是 platform_admin、停用、待绑定、不存在、歧义、用法错一律拒绝且不改任何状态；不动口令与会话。
func TestPromoteAdminEndToEnd(t *testing.T) {
	ctx := context.Background()
	dsn := pgSchemaDSN(t, true)
	out, err := runCLI(t, dsn, "bootstrap-admin", "--user-id", "Boss", "--short-name", "bs")
	if err != nil {
		t.Fatal(err)
	}
	initial := strings.TrimPrefix(strings.Split(out, "\n")[1], "initial_password=")
	store, _, closeStore, err := openNewPlatformStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	ob := identity.NewOnboarding(identity.NewService(store, nil, nil))
	tok, _, err := ob.PasswordLogin(ctx, "boss", initial, "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []identity.User{
		{UserID: "Mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "Mgr2", ShortName: "m2", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "Off", ShortName: "of", Type: identity.UserManager, State: identity.UserDisabled},
		{UserID: "Pend", ShortName: "pd", Type: identity.UserMember, State: identity.UserPendingBind},
		// 歧义：Amb 的 user_id 恰是 Clash 的 short_name
		{UserID: "Amb", ShortName: "ab", Type: identity.UserMember, State: identity.UserActive},
		{UserID: "Clash", ShortName: "amb", Type: identity.UserMember, State: identity.UserActive},
	} {
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tc, _ := repo.NewAccessCtx("test", "promote-admin")
	snapshot := func() string {
		t.Helper()
		var b strings.Builder
		for _, q := range []string{
			`SELECT coalesce(string_agg(row_to_json(u)::text, E'\n' ORDER BY user_id_lc), '') FROM np_user u`,
			`SELECT coalesce(string_agg(row_to_json(s)::text, E'\n' ORDER BY token_hash), '') FROM np_session s`,
			`SELECT count(*)::text FROM audit_log`,
		} {
			var s string
			row, err := pool.QueryRow(ctx, tc, q)
			if err != nil {
				t.Fatal(err)
			}
			if err := row.Scan(&s); err != nil {
				t.Fatal(err)
			}
			b.WriteString(s + "\n")
		}
		return b.String()
	}
	before := snapshot()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"promote-admin"}, "usage: workpulse promote-admin"},
		{[]string{"promote-admin", "--user", "nobody"}, "no such user"},
		{[]string{"promote-admin", "--user", "boss"}, "already platform_admin"},
		{[]string{"promote-admin", "--user", "bs"}, "already platform_admin"},
		{[]string{"promote-admin", "--user", "Off"}, "not active"},
		{[]string{"promote-admin", "--user", "pd"}, "not active"},
		{[]string{"promote-admin", "--user", "amb"}, "pass the exact user_id"},
	} {
		if out, err := runCLI(t, dsn, c.args...); err == nil || !strings.Contains(err.Error(), c.want) || out != "" {
			t.Fatalf("%v: out=%q err=%v, want refusal containing %q", c.args, out, err, c.want)
		}
	}
	if after := snapshot(); after != before {
		t.Fatalf("a refused promote-admin changed state:\nbefore=%s\nafter=%s", before, after)
	}

	sessions := func() string {
		t.Helper()
		row, err := pool.QueryRow(ctx, tc, `SELECT coalesce(string_agg(row_to_json(s)::text, E'\n' ORDER BY token_hash), '') FROM np_session s`)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if err := row.Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	sessBefore := sessions()
	_, resolveBefore := ob.ResolveSession(ctx, tok)
	// 按 user_id（大小写不敏感）
	out, err = runCLI(t, dsn, "promote-admin", "--user", "MGR")
	if err != nil || out != "PROMOTED_PLATFORM_ADMIN user_id=Mgr short_name=mg old_type=manager\n" {
		t.Fatalf("promote by user_id: %q %v", out, err)
	}
	// 按 short_name
	out, err = runCLI(t, dsn, "promote-admin", "--user", "m2")
	if err != nil || out != "PROMOTED_PLATFORM_ADMIN user_id=Mgr2 short_name=m2 old_type=manager\n" {
		t.Fatalf("promote by short_name: %q %v", out, err)
	}
	for _, id := range []string{"mgr", "mgr2"} {
		u, err := store.GetUser(ctx, id)
		if err != nil || u.Type != identity.UserPlatformAdmin || u.State != identity.UserActive {
			t.Fatalf("%s after promote: %+v %v", id, u, err)
		}
	}
	if out, err := runCLI(t, dsn, "promote-admin", "--user", "mgr"); err == nil || !strings.Contains(err.Error(), "already platform_admin") || out != "" {
		t.Fatalf("second promote: %q %v", out, err)
	}
	// 审计：恰两笔，actor／action／target／原类型
	rows, err := pool.Query(ctx, tc, `SELECT actor, action, target, after_json::jsonb ->> 'detail' FROM audit_log WHERE action = 'user.promoted_platform_admin_by_ops' ORDER BY target`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var a, act, tgt, aj string
		if err := rows.Scan(&a, &act, &tgt, &aj); err != nil {
			t.Fatal(err)
		}
		got = append(got, a+"|"+act+"|"+tgt+"|"+aj)
	}
	rows.Close()
	want := []string{
		`promote-admin|user.promoted_platform_admin_by_ops|mgr|old_type=manager new_type=platform_admin`,
		`promote-admin|user.promoted_platform_admin_by_ops|mgr2|old_type=manager new_type=platform_admin`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("audit rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// 不动口令与会话：管理员会话仍有效，口令照旧可登录
	if sessions() != sessBefore || sessBefore == "" {
		t.Fatalf("sessions changed by promote-admin:\nbefore=%s\nafter=%s", sessBefore, sessions())
	}
	if _, err := ob.ResolveSession(ctx, tok); !errors.Is(err, resolveBefore) && err != resolveBefore {
		t.Fatalf("session resolution changed by promote-admin: before=%v after=%v", resolveBefore, err)
	}
	if _, _, err := ob.PasswordLogin(ctx, "boss", initial, "1.1.1.2"); err != nil {
		t.Fatalf("password changed by promote-admin: %v", err)
	}
	// 停用的仍是 manager
	if u, _ := store.GetUser(ctx, "off"); u.Type != identity.UserManager || u.State != identity.UserDisabled {
		t.Fatalf("disabled user changed: %+v", u)
	}
}
