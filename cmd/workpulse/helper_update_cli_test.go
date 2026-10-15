package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestHelperUpdateRegisterIsOperatorOnlyCLI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hub.db")
	t.Setenv("WP_DB_PATH", dbPath)
	sha := strings.Repeat("a", 64)
	var out bytes.Buffer
	if err := runHelperUpdateRegister(context.Background(), []string{
		"--operator", "ops", "--sha256", sha, "--lock-sha256", strings.Repeat("b", 64),
		"--version", "0.2.3.1", "--url", "https://files.example/helper.tgz", "--file-service-origin", "https://files.example",
	}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "HELPER_UPDATE_CATALOG_REGISTERED") {
		t.Fatalf("output=%q", out.String())
	}
	db, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.ListAppConfig(context.Background())
	if err != nil || len(rows) != 1 || rows[0].Key != "helper_update.catalog."+sha {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if err := runHelperUpdateRegister(context.Background(), []string{"--sha256", sha}, &out); err == nil {
		t.Fatal("missing operator accepted")
	}
	if err := runHelperUpdateRegister(context.Background(), []string{
		"--operator", "ops", "--sha256", strings.Repeat("c", 64), "--lock-sha256", strings.Repeat("d", 64),
		"--version", "0.2.3.1", "--url", "https://files.example/helper.tgz",
	}, &out); err == nil {
		t.Fatal("catalog without registered file-service origin accepted")
	}
}
