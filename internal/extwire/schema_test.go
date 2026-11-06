package extwire

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type fakeTables map[string]bool

func (f fakeTables) TableExists(_ context.Context, name string) (bool, error) {
	if name == "boom" {
		return false, errors.New("db down")
	}
	return f[name], nil
}

func TestCheckSchemaAllPresent(t *testing.T) {
	all := fakeTables{}
	for _, n := range RequiredTables {
		all[n] = true
	}
	if err := CheckSchema(context.Background(), all); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSchemaReportsEveryMissingTable(t *testing.T) {
	some := fakeTables{}
	for i, n := range RequiredTables {
		if i%2 == 0 {
			some[n] = true
		}
	}
	err := CheckSchema(context.Background(), some)
	if err == nil {
		t.Fatal("missing tables must fail closed")
	}
	for i, n := range RequiredTables {
		if i%2 == 1 && !strings.Contains(err.Error(), n) {
			t.Fatalf("error must name missing table %s: %v", n, err)
		}
	}
	if CheckSchema(context.Background(), nil) == nil {
		t.Fatal("nil checker must fail closed")
	}
}

// RequiredTables 必须恰好等于 0105（A 路）与 0107（B 路）迁移里 CREATE TABLE 的表，次序同文件次序。
// 从迁移原文取表名，免得迁移加表而这里漏改（D 路 0106 合入时把文件加进 files）。
func TestRequiredTablesMatchMigrations(t *testing.T) {
	files := []string{"0105_extseat.sql", "0107_mcpgw_oauth.sql"}
	re := regexp.MustCompile(`(?m)^CREATE TABLE ([a-z_][a-z0-9_]*)`)
	var want []string
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join("..", "store", "migrate", "migrations", f))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			want = append(want, m[1])
		}
	}
	if len(want) != 7 {
		t.Fatalf("parsed %d tables from %v, want 7 (0105 six + 0107 one): %v", len(want), files, want)
	}
	if strings.Join(RequiredTables, ",") != strings.Join(want, ",") {
		t.Fatalf("RequiredTables %v drifted from migrations %v", RequiredTables, want)
	}
}
