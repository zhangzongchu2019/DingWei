package identity_test

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
	"strings"
	"testing"
	"time"
)

func TestR31AdminAgentPGAuditAtomicAndFailClosed(t *testing.T) {
	pool, tc := pgSchemaFactory(t)(t)
	ctx := context.Background()
	for _, ddl := range identitytest.PGTestSchema() {
		if _, err := pool.Exec(ctx, tc, ddl); err != nil {
			t.Fatal(err)
		}
	}
	s := identity.NewPGStore(pool)
	at := time.Now().UTC()
	if err := s.CreateUser(ctx, identity.User{UserID: "admin", ShortName: "ad", State: identity.UserActive, Type: identity.UserPlatformAdmin, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, tc, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at,last_source_ip) VALUES('ag-a','orphan','Unknown','unknown','selfhost','active',now(),'203.0.113.77')`); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListAdminAgents(ctx, "admin", "", 100, at)
	if err != nil || len(page.Agents) != 1 || page.Agents[0].OwnerID != "orphan" || page.Agents[0].SourceIP != "203.0.113.77" {
		t.Fatalf("admin orphan inventory: %+v %v", page, err)
	}
	row, err := pool.QueryRow(ctx, tc, `SELECT count(*),COALESCE(max(after_json::text),'') FROM audit_log WHERE action='agent.admin_list'`)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var detail string
	if err := row.Scan(&n, &detail); err != nil {
		t.Fatal(err)
	}
	if n != 1 || strings.Contains(detail, "203.0.113") {
		t.Fatalf("ADMIN_PG_READ_AUDIT_MISSING: %d %s", n, detail)
	}
	// Break only the write permission path in this isolated temporary schema.
	// Reading the inventory must not succeed if the required audit cannot commit.
	if _, err := pool.Exec(ctx, tc, `ALTER TABLE audit_log ADD CONSTRAINT reject_admin_read CHECK (action <> 'agent.admin_list') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	page, err = s.ListAdminAgents(ctx, "admin", "", 100, at)
	if err == nil || len(page.Agents) != 0 {
		t.Fatalf("ADMIN_PG_AUDIT_FAIL_OPEN: %+v %v", page, err)
	}
}
