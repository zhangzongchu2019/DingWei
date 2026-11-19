package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/mcpgw"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
)

// This is an old, independently built product executable operating only on a
// pgtest-owned schema. It is not a release artifact or a production rollback.
func TestDCRAdditiveMigrationOldBinaryAndForward(t *testing.T) {
	old := os.Getenv("DW_DCR_OLD_BINARY")
	if old == "" {
		t.Skip("requires independently built frozen 4a349ee5 executable via DW_DCR_OLD_BINARY")
	}
	ctx := context.Background()
	dsn := pgSchemaDSN(t, true)
	pool, e := repo.OpenPG(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if e = identity.NewPGStore(pool).CreateUser(ctx, identity.User{UserID: "admin", ShortName: "adm", Type: identity.UserPlatformAdmin, State: identity.UserActive, PasswordHash: "test-only-non-login-fixture", CreatedAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	var digest string
	row, e := pool.QueryRow(ctx, repo.SystemCtx(), `SELECT digest FROM schema_migrations WHERE version='0115_mcpgw_dcr.sql'`)
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&digest); e != nil {
		t.Fatal("DCR migration not applied", e)
	}
	if _, e = pool.Exec(ctx, repo.SystemCtx(), `INSERT INTO mcpgw_dcr_client(resource,client_id,sealed,source_hash,created_ns,expires_ns) VALUES('test','dw-dcr-fixture',decode('00','hex'),'synthetic',0,9223372036854775807)`); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(old, "bootstrap-admin", "--user-id", "another", "--short-name", "another")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "TMPDIR=" + os.Getenv("TMPDIR"), "DW_NEWPLAT_PG_DSN=" + dsn}
	out, e := cmd.CombinedOutput()
	if e == nil || !strings.Contains(string(out), "management user already exists") {
		t.Fatalf("old binary refused additive DCR ledger/schema before safe existing-admin guard: %v %s", e, out)
	}
	if _, e = migrate.Migrate(ctx, pool, migrate.HubDomains()); e != nil {
		t.Fatal("forward migration rejected unchanged ledger", e)
	}
	var after string
	row, e = pool.QueryRow(ctx, repo.SystemCtx(), `SELECT digest FROM schema_migrations WHERE version='0115_mcpgw_dcr.sql'`)
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&after); e != nil || after != digest {
		t.Fatal("old/forward path changed DCR migration digest", e)
	}
	var n int
	row, e = pool.QueryRow(ctx, repo.SystemCtx(), `SELECT count(*) FROM mcpgw_dcr_client WHERE client_id='dw-dcr-fixture'`)
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&n); e != nil || n != 1 {
		t.Fatal("old/forward path discarded additive registration state", e)
	}
	t.Run("old_hub_finishes_existing_DCR_code", func(t *testing.T) { runOldDCRGrant(t, old, dsn, pool) })
}

func runOldDCRGrant(t *testing.T, old, dsn string, pool *repo.Pool) {
	ctx := context.Background()
	resource := "https://rollback.example/mcp"
	ac := repo.SystemCtx()
	now := time.Now()
	if _, e := pool.Exec(ctx, ac, `INSERT INTO np_user(user_id,user_id_lc,short_name,short_name_lc,type,state,created_at) VALUES('owner','owner','zzc','zzc','member','active',now()); INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('target-id','owner','target','target','selfhost','active',now())`); e != nil {
		t.Fatal(e)
	}
	seats := &extseat.Service{Pool: pool, Access: ac, Identity: identity.NewService(identity.NewPGStore(pool), nil, nil), Groups: &tempgroup.Service{Pool: pool, Access: ac}, TenantKey: "team", Resource: resource}
	raw, seat, _, e := seats.IssueToken(ctx, extshared.Principal{Provider: "feishu", TenantKey: "team", Issuer: "app", Subject: "rollback-person", DisplayName: "user"}, "editor", "target.zzc")
	if e != nil {
		t.Fatal(e)
	}
	key := []byte(strings.Repeat("k", 32))
	store, e := mcpgw.NewSQLStore(ctx, pool, ac, resource, key)
	if e != nil {
		t.Fatal(e)
	}
	const verifier = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	codeHash := sha256.Sum256([]byte("test-only-existing-code"))
	grant := map[string]any{"Authorization": map[string]any{"ClientID": "dw-dcr-fixture", "Redirect": "http://127.0.0.1:43123/callback", "Challenge": challenge, "Expires": now.Add(5 * time.Minute)}, "Issued": feishulogin.CallbackResult{Token: raw, Audience: resource, Scopes: extshared.MVPScopes[:], ExpiresAt: seat.ExpiresAt}}
	payload, e := json.Marshal(grant)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Put(ctx, "code", hex.EncodeToString(codeHash[:]), payload, now.Add(5*time.Minute), now); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "oauth.key")
	if e = os.WriteFile(keyPath, key, 0600); e != nil {
		t.Fatal(e)
	}
	addr := freeNPAddr(t)
	cmd := exec.Command(old)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + os.Getenv("TMPDIR"), "WP_DATA_DIR=" + dir, "WP_DB_PATH=" + filepath.Join(dir, "hub.db"), "WP_ADDR=" + addr, "WP_INTERNAL_ADDR=" + freeNPAddr(t), "FEISHU_TRANSPORT=fake", "FEISHU_APP_ID=app", "FEISHU_APP_SECRET=test-only-synthetic-secret", "WP_CONSOLE_AUTH_ENABLED=false", "WP_SCHEDULE_TEAM_FILE=" + filepath.Join(dir, "missing.md"), "WP_SCHEDULE_PERSONAL_DIR=" + dir, "DW_NEWPLAT=1", "DW_PUBLIC_ORIGIN=https://rollback.example", "DW_NEWPLAT_PG_DSN=" + dsn, "DW_R53_EXTSEAT=1", "DW_FEISHU_TEAM_TENANT_KEY=team", "DW_MCP_RESOURCE=" + resource, "DW_MCPGW_OAUTH_KEY_FILE=" + keyPath, "DW_MCP_FEISHU_COPY_ENABLED=0"}
	logFile, e := os.Create(filepath.Join(dir, "old-hub-private.log"))
	if e != nil {
		t.Fatal(e)
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	client := &http.Client{Timeout: 2 * time.Second}
	base := "http://" + addr
	ready := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		resp, e := client.Get(base + "/healthz")
		if e == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("old Hub did not start on additive DCR schema; private test log retained in test directory")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {"test-only-existing-code"}, "client_id": {"dw-dcr-fixture"}, "redirect_uri": {"http://127.0.0.1:43123/callback"}, "code_verifier": {verifier}, "resource": {resource}}
	resp, e := client.PostForm(base+"/oauth/token", form)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var result struct {
		Token string `json:"access_token"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&result); e != nil || resp.StatusCode != 200 || result.Token != raw {
		t.Fatalf("old Hub did not finish an existing DCR grant: status=%d err=%v", resp.StatusCode, e)
	}
	resp, e = client.PostForm(base+"/oauth/token", form)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatal("old Hub replayed consumed DCR grant")
	}
}
