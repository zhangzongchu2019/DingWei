package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

func TestR53ConfigFromEnv(t *testing.T) {
	const res = "https://dw123.cmjrt.com/mcp"
	cases := []struct {
		name     string
		on       string
		tenant   string
		resource string
		newplat  bool
		wantOn   bool
		wantErr  error
	}{
		{name: "off_by_default", on: "", tenant: "tk", resource: res, newplat: true},
		{name: "off_when_not_1", on: "true", tenant: "tk", resource: res, newplat: true},
		{name: "needs_newplat", on: "1", tenant: "tk", resource: res, newplat: false, wantOn: true, wantErr: errR53NeedsNewplat},
		{name: "needs_tenant", on: "1", tenant: " ", resource: res, newplat: true, wantOn: true, wantErr: errR53TenantKey},
		{name: "needs_resource", on: "1", tenant: "tk", resource: "", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "resource_http_rejected", on: "1", tenant: "tk", resource: "http://dw123.cmjrt.com/mcp", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "resource_relative_rejected", on: "1", tenant: "tk", resource: "/mcp", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "resource_query_rejected", on: "1", tenant: "tk", resource: res + "?a=1", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "resource_fragment_rejected", on: "1", tenant: "tk", resource: res + "\x23x", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "ok", on: "1", tenant: "tk-2d31", resource: res, newplat: true, wantOn: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("DW_R53_EXTSEAT", c.on)
			t.Setenv("DW_FEISHU_TEAM_TENANT_KEY", c.tenant)
			t.Setenv("DW_MCP_RESOURCE", c.resource)
			cfg, on, err := r53ConfigFromEnv(c.newplat)
			if on != c.wantOn || !errors.Is(err, c.wantErr) {
				t.Fatalf("on=%v err=%v want on=%v err=%v", on, err, c.wantOn, c.wantErr)
			}
			if c.name == "ok" && (cfg.tenantKey != "tk-2d31" || cfg.resource != res) {
				t.Fatalf("cfg=%+v", cfg)
			}
		})
	}
}

// 开启时：0105 表齐 ⇒ 过表核对、停在「D 路未并入」；删一张表 ⇒ 报出缺的那张表名。
func TestWireR53SchemaCheckPG(t *testing.T) {
	dsn := pgSchemaDSN(t, false)
	ctx := context.Background()
	_, pool, closeStore, err := openNewPlatformStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	cfg := r53Config{tenantKey: "tk", resource: "https://dw123.cmjrt.com/mcp"}
	if err := wireR53(ctx, cfg, pgTableChecker{pool: pool}); !errors.Is(err, errR53GroupsMissing) {
		t.Fatalf("all tables present: err=%v want errR53GroupsMissing", err)
	}
	if _, err := pool.Exec(ctx, repo.SystemCtx(), `DROP TABLE np_extseat_outbox`); err != nil {
		t.Fatal(err)
	}
	err = wireR53(ctx, cfg, pgTableChecker{pool: pool})
	if err == nil || errors.Is(err, errR53GroupsMissing) || !strings.Contains(err.Error(), "np_extseat_outbox") {
		t.Fatalf("dropped table: err=%v want missing np_extseat_outbox", err)
	}
}
