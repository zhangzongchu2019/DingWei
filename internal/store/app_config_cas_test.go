package store

import (
	"context"
	"testing"
)

func TestCompareAndSwapAppConfig(t *testing.T) {
	db, err := OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.CompareAndSwapAppConfig(ctx, "helper_update.job.a", false, "", `{"revision":1}`); err != nil || !ok {
		t.Fatalf("initial CAS ok=%v err=%v", ok, err)
	}
	if ok, err := db.CompareAndSwapAppConfig(ctx, "helper_update.job.a", false, "", `{"revision":2}`); err != nil || ok {
		t.Fatalf("duplicate create ok=%v err=%v", ok, err)
	}
	if ok, err := db.CompareAndSwapAppConfig(ctx, "helper_update.job.a", true, `{"revision":0}`, `{"revision":2}`); err != nil || ok {
		t.Fatalf("stale CAS ok=%v err=%v", ok, err)
	}
	if ok, err := db.CompareAndSwapAppConfig(ctx, "helper_update.job.a", true, `{"revision":1}`, `{"revision":2}`); err != nil || !ok {
		t.Fatalf("matching CAS ok=%v err=%v", ok, err)
	}
}
