package main

import (
	"go/parser"
	"testing"
)

func TestDDLColumns(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		want      int
	}{
		{"json", `TenantKey string ` + "`json:\"tenant_key\"`", 0},
		{"select", `SELECT tenant_key FROM np_external_principal WHERE tenant_key=$1`, 0},
		{"insert", `INSERT INTO np_external_principal(tenant_key) VALUES($1)`, 0},
		{"comments", `-- CREATE TABLE false(tenant_id TEXT);` + "\n" + `CREATE TABLE ok (id TEXT, note TEXT DEFAULT 'tenant_key'); /* tenant_id TEXT */`, 0},
		{"multiline quoted", "CREATE TABLE IF NOT EXISTS public.\"x\" (id TEXT,\n \"tenant_id\" TEXT NOT NULL, UNIQUE(tenant_id));", 1},
		{"constraints only", `CREATE TABLE x (id TEXT, UNIQUE(tenant_id), CHECK(tenant_key != ''));`, 0},
		{"alter", `ALTER TABLE public.x ADD COLUMN IF NOT EXISTS tenant_uuid UUID;`, 1},
		{"plain tenant", `CREATE TABLE x (issuer TEXT, tenant TEXT NOT NULL);`, 1},
		{"lowercase type", `create table x (tenant_key text);`, 1},
		{"nested comma", `CREATE TABLE x (name NUMERIC(10,2), tenant_id TEXT);`, 1},
		{"literal default", `CREATE TABLE x (id TEXT DEFAULT 'CREATE TABLE y(tenant_id TEXT);');`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := columns(tc.sql)
			if len(got) != tc.want {
				t.Fatalf("got %v want %d", got, tc.want)
			}
		})
	}
}
func TestGoLiteralConcatenation(t *testing.T) {
	x, e := parser.ParseExpr("`CREATE TABLE x (` + \"tenant_id TEXT);\"")
	if e != nil {
		t.Fatal(e)
	}
	s, ok := literal(x)
	if !ok || len(columns(s)) != 1 {
		t.Fatalf("literal=%q ok=%v", s, ok)
	}
}
