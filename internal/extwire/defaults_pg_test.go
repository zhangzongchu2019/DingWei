package extwire

import (
	"context"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
)

func TestConsentRecentTrustedPrincipalPG(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	ac := repo.SystemCtx()
	if _, e := migrate.Migrate(ctx, pool, migrate.HubDomains()); e != nil {
		t.Fatal(e)
	}
	_, e := pool.Exec(ctx, ac, `INSERT INTO np_user(user_id,user_id_lc,short_name,short_name_lc,type,state,created_at) VALUES('owner','owner','zzc','zzc','member','active',now()); INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('a','owner','Helper','helper','selfhost','active',now()),('b','owner','Other','other','selfhost','active',now())`)
	if e != nil {
		t.Fatal(e)
	}
	id := identity.NewService(identity.NewPGStore(pool), nil, nil)
	d, _ := ParseConsentTargets(`{"t":["Helper.zzc","Other.zzc"]}`)
	d.Pool = pool
	d.Access = ac
	d.Identity = id
	p := extshared.Principal{Provider: "feishu", TenantKey: "t", Issuer: "app", Subject: "one", DisplayName: "same"}
	if got, e := d.Recent(ctx, p); e != nil || got != "" {
		t.Fatalf("ZC_RECENT_EMPTY %s %v", got, e)
	}
	s := &extseat.Service{Pool: pool, Access: ac, Identity: id, Groups: &tempgroup.Service{Pool: pool, Access: ac}, TenantKey: "t", Resource: "https://example.invalid/mcp"}
	for _, target := range []string{"Helper.zzc", "Other.zzc"} {
		if _, _, _, e = s.IssueToken(ctx, p, "same261109060000", target); e != nil {
			t.Fatal(e)
		}
	}
	if got, e := d.Recent(ctx, p); e != nil || got != "Other.zzc" {
		t.Fatalf("ZC_RECENT_LATEST %s %v", got, e)
	}
	for _, field := range []string{"subject", "tenant", "issuer", "provider"} {
		other := p
		switch field {
		case "subject":
			other.Subject = "two"
		case "tenant":
			other.TenantKey = "x"
		case "issuer":
			other.Issuer = "x"
		case "provider":
			other.Provider = "x"
		}
		if got, e := d.Recent(ctx, other); e != nil || got != "" {
			t.Fatalf("ZC_RECENT_SCOPE_%s %s %v", field, got, e)
		}
	}
	// Config shutdown never changes pre-existing names or persisted identities.
	off, _ := ParseConsentTargets("")
	if len(off.Allowed("t")) != 0 {
		t.Fatal("ZC_OFF_NOT_EMPTY")
	}
	row, e := pool.QueryRow(ctx, ac, `SELECT count(*) FROM np_extseat WHERE short_name='same261109060000'`)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	if e = row.Scan(&n); e != nil || n != 2 {
		t.Fatalf("ZC_OFF_REWROTE_LONG_ROWS %d %v", n, e)
	}
}
