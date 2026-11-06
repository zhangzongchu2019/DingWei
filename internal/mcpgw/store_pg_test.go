package mcpgw

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestPGStore(t *testing.T) {
	dsn := pgtest.DSN(t)
	ctx := context.Background()
	access := testAccess(t)
	admin, e := repo.OpenPG(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := fmt.Sprintf("mcpgw_%d", time.Now().UnixNano())
	if _, e = admin.Exec(ctx, access, `CREATE SCHEMA `+schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec(ctx, access, `DROP SCHEMA `+schema+` CASCADE`)
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	pool, e := repo.OpenPG(ctx, dsn+sep+"search_path="+schema)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	key := randomKey(t)
	if _, e = NewSQLStore(ctx, pool, access, testOrigin+"/mcp", key); e == nil {
		t.Fatal("missing migration accepted")
	}
	ddl, e := os.ReadFile("../store/migrate/migrations/0107_mcpgw_oauth.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, access, string(ddl)); e != nil {
		t.Fatal(e)
	}
	store, e := NewSQLStore(ctx, pool, access, testOrigin+"/mcp", key)
	if e != nil {
		t.Fatal(e)
	}
	t.Run("code_exchange_concurrency", func(t *testing.T) {
		_, _, mux := oauthFixtureWithStore(t, store)
		code := mintCode(t, mux)
		var wg sync.WaitGroup
		var wins atomic.Int32
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := redeem(mux, tokenForm(code))
				if w.Code == 200 {
					wins.Add(1)
				} else if w.Code != 400 {
					t.Errorf("unexpected status %d", w.Code)
				}
			}()
		}
		wg.Wait()
		if wins.Load() != 1 {
			t.Fatalf("PG code exchanged %d times", wins.Load())
		}
		if w := redeem(mux, tokenForm(code)); w.Code != 400 {
			t.Fatal("PG code replay")
		}
	})
	t.Run("expiry_cleanup_resource_isolation_restart", func(t *testing.T) {
		now := time.Now()
		id := hash("pending")
		payload := []byte("encrypted-token")
		if e := store.Put(ctx, "code", id, payload, now.Add(time.Minute), now); e != nil {
			t.Fatal(e)
		}
		separate, e := NewSQLStore(ctx, pool, access, "https://other.example/mcp", key)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = separate.Get(ctx, "code", id, now); e == nil {
			t.Fatal("cross-resource state visible")
		}
		second, e := repo.OpenPG(ctx, dsn+sep+"search_path="+schema)
		if e != nil {
			t.Fatal(e)
		}
		defer second.Close()
		reopened, e := NewSQLStore(ctx, second, access, testOrigin+"/mcp", key)
		if e != nil {
			t.Fatal(e)
		}
		b, e := reopened.Get(ctx, "code", id, now)
		if e != nil || string(b) != string(payload) {
			t.Fatal("reopened PG cannot read")
		}
		if _, e = reopened.Take(ctx, "code", id, now.Add(time.Minute)); e == nil {
			t.Fatal("exact expiry allowed")
		}
		if e = store.Put(ctx, "attempt", hash("expired"), payload, now.Add(time.Minute), now); e != nil {
			t.Fatal(e)
		}
		if e = store.Put(ctx, "attempt", hash("new"), payload, now.Add(3*time.Minute), now.Add(2*time.Minute)); e != nil {
			t.Fatal(e)
		}
		row, e := pool.QueryRow(ctx, access, `SELECT count(*) FROM mcpgw_oauth WHERE resource=$1 AND expires_ns<=$2`, testOrigin+"/mcp", now.Add(2*time.Minute).UnixNano())
		if e != nil {
			t.Fatal(e)
		}
		var count int
		if e = row.Scan(&count); e != nil || count != 0 {
			t.Fatal("expired rows not cleaned")
		}
		row, e = pool.QueryRow(ctx, access, `SELECT count(*) FROM pg_indexes WHERE schemaname=$1 AND indexname='mcpgw_oauth_expiry'`, schema)
		if e != nil {
			t.Fatal(e)
		}
		if e = row.Scan(&count); e != nil || count != 1 {
			t.Fatal("expiry index missing")
		}
	})
}
