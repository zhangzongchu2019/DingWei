package main

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"path/filepath"
	"testing"
)

func TestR11FreezeStartupConfig(t *testing.T) {
	ctx := context.Background()
	for _, raw := range []string{"", "0", "false", "1", "true", "garbage"} {
		name := raw
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			db, e := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			if e = db.Migrate(ctx); e != nil {
				t.Fatal(e)
			}
			e = configureLegacySpecFreeze(ctx, db, raw)
			if raw == "garbage" {
				if e == nil {
					t.Fatal("invalid config accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			frozen, e := db.LegacySpecsFrozen(ctx)
			if e != nil || frozen != (raw == "true" || raw == "1") {
				t.Fatal(frozen, e)
			}
			if e = configureLegacySpecFreeze(ctx, db, "true"); e != nil {
				t.Fatal(e)
			}
			if e = configureLegacySpecFreeze(ctx, db, "false"); e != nil {
				t.Fatal(e)
			}
			if frozen, e = db.LegacySpecsFrozen(ctx); e != nil || !frozen {
				t.Fatal("false reopened frozen DB", e)
			}
		})
	}
}
