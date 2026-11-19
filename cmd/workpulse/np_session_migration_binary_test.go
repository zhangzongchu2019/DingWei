package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"golang.org/x/crypto/bcrypt"
)

// Opt-in proof with independently built product binaries. It starts from the exact
// eight-entry Hub ledger, lets the current binary apply pending Hub migrations, then starts the
// certified R50 binary against that same PG schema and checks an authenticated API.
func TestNPSessionPG8To9AndR50BinaryCompatibility(t *testing.T) {
	current, old := os.Getenv("R51_NP_CURRENT_BINARY"), os.Getenv("R51_NP_R50_BINARY")
	if current == "" || old == "" {
		t.Skip("set independently built current and certified R50 binary paths")
	}
	ctx := context.Background()
	dsn := pgSchemaDSN(t, false)
	pool, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	entries, err := migrate.LedgerInDomains(migrate.HubDomains())
	if err != nil {
		t.Fatal(err)
	}
	var oldEntries []migrate.Entry
	for _, e := range entries {
		// r50 现网账＝8 条：剔掉 r51 的 0104 与 r53 的 0105／0107（r53 集成起当前二进制会一并施加）。
		if e.Name != "0104_np_session_created_at.sql" && e.Name != "0105_extseat.sql" && e.Name != "0106_tempgroup.sql" && e.Name != "0107_mcpgw_oauth.sql" && e.Name != "0108_extseat_short_name.sql" && e.Name != "0110_extseat_expect_reply.sql" && e.Name != "0111_extseat_feishu_copy.sql" && e.Name != "0112_np_agent_display_name.sql" && e.Name != "0113_extseat_groups.sql" && e.Name != "0114_agent_feishu_summary_copy.sql" && e.Name != "0115_mcpgw_dcr.sql" {
			oldEntries = append(oldEntries, e)
		}
	}
	if len(oldEntries) != 8 {
		t.Fatalf("PG8 fixture entries=%d", len(oldEntries))
	}
	if _, err := migrate.MigrateEntries(ctx, pool, oldEntries, nil); err != nil {
		t.Fatal(err)
	}
	var count int
	row, err := pool.QueryRow(ctx, repo.SystemCtx(), `SELECT count(*) FROM schema_migrations`)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&count); err != nil || count != 8 {
		t.Fatalf("PG8 ledger count=%d err=%v", count, err)
	}
	store := identity.NewPGStore(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("R50-Renewal-Check-42"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUser(ctx, identity.User{UserID: "renew-admin", ShortName: "renew", Type: identity.UserPlatformAdmin, State: identity.UserActive, PasswordHash: string(passwordHash), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	const token = "np-r50-compat-synthetic-session"
	if _, err := pool.Exec(ctx, repo.SystemCtx(), `INSERT INTO np_session(token_hash,user_id_lc,expires_at) VALUES($1,$2,$3)`, identity.HashSecret(token), "renew-admin", now.Add(12*time.Hour)); err != nil {
		t.Fatal(err)
	}
	before, err := pool.Query(ctx, repo.SystemCtx(), `SELECT version,digest FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	var previous []string
	for before.Next() {
		var a, b string
		if err := before.Scan(&a, &b); err != nil {
			t.Fatal(err)
		}
		previous = append(previous, a+":"+b)
	}
	before.Close()
	cmd := exec.Command(current, "bootstrap-admin", "--user-id", "secondadmin", "--short-name", "sec")
	cmd.Env = append(os.Environ(), "DW_NEWPLAT_PG_DSN="+dsn)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "management user already exists") {
		t.Fatalf("current binary did not migrate PG8 then reach existing-admin guard: err=%v out=%s", err, out)
	}
	row, err = pool.QueryRow(ctx, repo.SystemCtx(), `SELECT count(*) FROM schema_migrations`)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&count); err != nil || count != 19 {
		t.Fatalf("PG19 ledger count=%d err=%v (want 0104_np_session_created_at, 0105_extseat, 0106_tempgroup, 0107_mcpgw_oauth, 0108_extseat_short_name, 0110_extseat_expect_reply, 0111_extseat_feishu_copy, 0112_np_agent_display_name, 0113_extseat_groups, 0114_agent_feishu_summary_copy, 0115_mcpgw_dcr applied)", count, err)
	}
	var created, applied time.Time
	row, err = pool.QueryRow(ctx, repo.SystemCtx(), `SELECT s.created_at,m.applied_at FROM np_session s CROSS JOIN schema_migrations m WHERE s.token_hash=$1 AND m.version='0104_np_session_created_at.sql'`, identity.HashSecret(token))
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&created, &applied); err != nil {
		t.Fatal(err)
	}
	if created.Before(applied.Add(-time.Second)) || created.After(applied.Add(time.Second)) {
		t.Fatalf("existing session created_at=%s migration applied_at=%s", created, applied)
	}
	for i, want := range previous {
		version, expectedDigest, _ := strings.Cut(want, ":")
		var got string
		row, err := pool.QueryRow(ctx, repo.SystemCtx(), `SELECT digest FROM schema_migrations WHERE version=$1`, version)
		if err != nil {
			t.Fatal(err)
		}
		if err := row.Scan(&got); err != nil || got != expectedDigest {
			t.Fatalf("old ledger row %d (%s) changed: got=%q want=%q err=%v", i, version, got, expectedDigest, err)
		}
	}
	pool.Close()

	addr := freeNPAddr(t)
	runDir := t.TempDir()
	cmd = exec.Command(old)
	cmd.Dir = runDir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + runDir, "TZ=UTC", "WP_DATA_DIR=" + runDir,
		"WP_DB_PATH=" + filepath.Join(runDir, "hub.db"), "WP_ADDR=" + addr, "WP_INTERNAL_ADDR=" + freeNPAddr(t),
		"FEISHU_TRANSPORT=fake", "WP_CONSOLE_AUTH_ENABLED=false", "WP_SCHEDULE_TEAM_FILE=" + filepath.Join(runDir, "missing.md"),
		"WP_SCHEDULE_PERSONAL_DIR=" + runDir, "DW_NEWPLAT=1", "DW_PUBLIC_ORIGIN=http://" + addr, "DW_NEWPLAT_PG_DSN=" + dsn}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	base := "http://" + addr
	deadline := time.Now().Add(20 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/healthz")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("certified R50 binary did not become healthy against PG13")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	loginBody := strings.NewReader(`{"user_id":"renew-admin","password":"R50-Renewal-Check-42"}`)
	loginReq, _ := http.NewRequest(http.MethodPost, base+"/console/login/password", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Origin", base)
	loginRes, err := client.Do(loginReq)
	if err != nil {
		t.Fatalf("certified R50 password login: %v", err)
	}
	defer loginRes.Body.Close()
	if loginRes.StatusCode != http.StatusOK {
		t.Fatalf("certified R50 password login status=%d", loginRes.StatusCode)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range loginRes.Cookies() {
		if cookie.Name == "dw_np_session" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("certified R50 password login did not issue a session cookie")
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/api/console/platform/users", nil)
	req.AddCookie(sessionCookie)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("certified R50 authenticated API: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("certified R50 authenticated API status=%d", res.StatusCode)
	}
	t.Logf("PG8->PG%d: prior_entries=%d unchanged=true total_entries=%d; existing_session_created_at~migration_time=true; certified_R50_password_login=%d authenticated_API=%d", count, len(previous), count, loginRes.StatusCode, res.StatusCode)
}

func freeNPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}
