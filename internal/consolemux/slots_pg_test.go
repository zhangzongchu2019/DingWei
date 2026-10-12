package consolemux

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestPGAuthorityConcurrentOccupancyAndStaleCleanup(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.DSN(t)
	access, err := repo.NewAccessCtx("test", "console-mux")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	seed, err := NewOwner("principal", "https://example.invalid", "one", "console")
	if err != nil {
		t.Fatal(err)
	}
	schema := "mux_" + seed.Nonce
	if _, err := admin.Exec(ctx, access, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, access, "DROP SCHEMA "+schema+" CASCADE")
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	pool, err := repo.OpenPG(ctx, dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ddl, err := os.ReadFile("../store/migrate/migrations/0300_console_transport_slot.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, access, string(ddl)); err != nil {
		t.Fatal(err)
	}
	stores := []*PGSlots{NewPGSlots(pool, access, NewBudget()), NewPGSlots(pool, access, NewBudget())}
	start := make(chan struct{})
	winners := make(chan Owner, 12)
	faults := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o, e := NewOwner("principal", "https://example.invalid", "test-session", "console")
			if e != nil {
				faults <- e
				return
			}
			<-start
			o, e = stores[i%2].Acquire(ctx, o)
			if e == nil {
				winners <- o
			} else if !errors.Is(e, ErrInUse) {
				faults <- e
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(winners)
	close(faults)
	for e := range faults {
		t.Fatal(e)
	}
	var old Owner
	count := 0
	for o := range winners {
		old = o
		count++
	}
	if count != 1 {
		t.Fatalf("two authorities admitted %d owners", count)
	}
	if _, err := stores[0].Renew(ctx, old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, access, "UPDATE console_transport_slot SET expires_at=clock_timestamp()-interval '1 millisecond'"); err != nil {
		t.Fatal(err)
	}
	if _, err := stores[1].Renew(ctx, old); !errors.Is(err, ErrLost) {
		t.Fatal("expired owner renewed", err)
	}
	next, _ := NewOwner("principal", "https://example.invalid", "other-session", "other-console")
	next, err = stores[1].Acquire(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if next.Generation <= old.Generation {
		t.Fatal("fence reused")
	}
	if err := stores[0].Release(ctx, old); err != nil {
		t.Fatal(err)
	}
	if _, err := stores[1].Check(ctx, next); err != nil {
		t.Fatal("stale cleanup removed successor", err)
	}
	tampered := next
	tampered.Nonce = old.Nonce
	if _, err := stores[0].Renew(ctx, tampered); !errors.Is(err, ErrLost) {
		t.Fatal("wrong nonce renewed", err)
	}
}
