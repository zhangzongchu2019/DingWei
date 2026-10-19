package main

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// Explicit opt-in: build the immutable R30 base independently, then pass only
// its local executable path. Every command below uses a pgtest-owned database.
// This checks old startup/read compatibility, not safe live hosted rollback.
func TestR31OldBinarySchemaReadCompatibility(t *testing.T) {
	binary := os.Getenv("R31_R30_BINARY")
	if binary == "" {
		t.Skip("requires independently built R30 binary via R31_R30_BINARY")
	}
	ctx := context.Background()
	dsn := pgSchemaDSN(t, true)
	p, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ac, err := repo.NewAccessCtx("test", "r31-old-binary")
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.NewPGStore(p).CreateUser(ctx, identity.User{UserID: "admin", ShortName: "adm", Type: identity.UserPlatformAdmin, State: identity.UserActive, PasswordHash: "fixture", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, ac, ` INSERT INTO g4_node(id,name,address,address_source,access_scope,scheduling_state)VALUES('nd_compat','compat','203.0.113.8','admin','restricted','disabled');
 INSERT INTO g4_node_access_grant(node_id,subject_kind,subject_id)VALUES('nd_compat','user','owner');
 INSERT INTO g4_account_pool(id,name,kind,cli,llm,secret_enc,key_version,owner_lc,created_by)VALUES('acct_compat','compat','token_file','codex','gpt',decode('0102','hex'),1,'owner','owner');`); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "master.key")
	if err = os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = append(os.Environ(), "DW_NEWPLAT_PG_DSN="+dsn, "DW_G4_MASTER_KEY_FILE="+key)
		raw, err := cmd.CombinedOutput()
		return string(raw), err
	}
	if out, err := run("bootstrap-admin", "--user-id", "another", "--short-name", "another"); err == nil || !strings.Contains(out, "a management user already exists") {
		t.Fatalf("old identity startup did not reach safe existing-admin read: %s", out)
	}
	if out, err := run("hosted-admin", "account-disable", "--account", "acct_compat"); err != nil || !strings.Contains(out, "account-disable 完成") {
		t.Fatalf("old hosted startup/read failed: %s", out)
	}
	var scope, state, address, owner, secret, acctState string
	row, err := p.QueryRow(ctx, ac, `SELECT n.access_scope,n.scheduling_state,n.address,a.owner_lc,encode(a.secret_enc,'hex'),a.state FROM g4_node n CROSS JOIN g4_account_pool a WHERE n.id='nd_compat' AND a.id='acct_compat'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&scope, &state, &address, &owner, &secret, &acctState); err != nil {
		t.Fatal(err)
	}
	if scope != "restricted" || state != "disabled" || address != "203.0.113.8" || owner != "owner" || secret != "0102" || acctState != "disabled" {
		t.Fatal("old binary altered unrelated R31 data")
	}
	// Returning to R31 must still accept the unchanged digests and grants.
	if err = hosted.NewStore(p).ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
}
