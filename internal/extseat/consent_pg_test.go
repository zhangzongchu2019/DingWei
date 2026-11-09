package extseat

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func TestConsentLongNameMigrationAndConcurrencyPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, old := f.issue(t)
	p := old.Principal
	short := "abcdefgh261109060000"
	if _, _, _, err := f.s.IssueToken(f.ctx, p, short, "helper.ho"); err == nil {
		t.Fatal("ZC_OLD_CHECK_ACCEPTED_LONG")
	}
	sql, err := os.ReadFile("../store/migrate/migrations/0108_extseat_short_name.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(f.ctx, f.access, string(sql)); err != nil {
		t.Fatal(err)
	}
	var oldShort string
	r, err := f.pool.QueryRow(f.ctx, f.access, `SELECT short_name FROM np_extseat WHERE agent_id=$1`, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Scan(&oldShort); err != nil || oldShort != "bot" {
		t.Fatalf("ZC_OLD_ROW_CHANGED %s %v", oldShort, err)
	}
	for _, bad := range []string{strings.Repeat("a", 21), "Bad", "bad_name", "bad-name", ""} {
		// The PG constraint must protect writes even if the application is bypassed.
		if _, err = f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat SET short_name=$1 WHERE agent_id=$2`, bad, old.ID); err == nil {
			t.Fatalf("ZC_PG_INVALID_ACCEPTED %q", bad)
		}
	}
	if _, err = f.pool.Exec(f.ctx, f.access, `SELECT setval('test_group_seq',999999999999999990,false)`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	result := make(chan Seat, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			proof := p
			if i >= 4 {
				proof.Subject = "second-person"
			}
			raw, s, _, e := f.s.IssueToken(f.ctx, proof, short, "helper.ho")
			if e == nil {
				var authenticated Seat
				authenticated, e = f.s.Authenticate(f.ctx, raw)
				if authenticated.FullName != s.FullName {
					e = fmt.Errorf("ZC_LONG_AUTH_NAME")
				}
			}
			if e != nil {
				errs <- e
			} else {
				result <- s
			}
		}(i)
	}
	wg.Wait()
	close(result)
	close(errs)
	for e := range errs {
		t.Fatalf("ZC_LONG_ISSUE %v", e)
	}
	names := map[string]bool{}
	owners := map[string]bool{}
	for s := range result {
		if len(s.FullName) != 58 || names[s.FullName] || !strings.HasPrefix(s.FullName, short+"_AG") {
			t.Fatalf("ZC_SAME_SECOND_COLLISION %s", s.FullName)
		}
		names[s.FullName] = true
		owners[s.OwnerLC] = true
		a, u, e := f.s.Identity.ResolveFullName(f.ctx, s.FullName)
		if e != nil || a.ID != s.ID || u == nil {
			t.Fatalf("ZC_LONG_RESOLVE %v", e)
		}
		if identity.ValidAgentName(s.Name) || identity.ValidShortName(short) {
			t.Fatal("ZC_ORDINARY_LIMIT_RELAXED")
		}
	}
	if len(names) != 8 || len(owners) != 2 {
		t.Fatalf("ZC_SAME_NAME_PRINCIPAL_MERGE %d %d", len(names), len(owners))
	}
	// Restore is a transaction: a failed old CHECK must not remove the new one.
	err = f.pool.InTx(f.ctx, f.access, nil, func(tx repo.Tx) error {
		_, e := tx.Exec(f.ctx, `ALTER TABLE np_extseat DROP CONSTRAINT np_extseat_short_name_check; ALTER TABLE np_extseat ADD CONSTRAINT np_extseat_short_name_check CHECK(short_name ~ '^[a-z0-9]{1,8}$')`)
		return e
	})
	if err == nil {
		t.Fatal("ZC_ROLLBACK_OLD_CHECK_ACCEPTED")
	}
	// Feature configuration lives outside storage; no long rows are rewritten.
	var count int
	row, e := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat WHERE short_name=$1`, short)
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&count); e != nil || count != 8 {
		t.Fatalf("ZC_FORWARD_ROWS_LOST %d %v", count, e)
	}
}
