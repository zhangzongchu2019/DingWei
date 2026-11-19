package mcpgw

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

func dcrPG(t *testing.T, full bool) (*SQLDynamicClients, *repo.Pool, []byte) {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.Pool(t)
	row, versionErr := pool.QueryRow(ctx, testAccess(t), `SELECT version()`)
	if versionErr != nil {
		t.Fatal(versionErr)
	}
	var pgVersion string
	if e := row.Scan(&pgVersion); e != nil {
		t.Fatal(e)
	}
	t.Log("DCR_PG_VERSION", pgVersion)
	key := randomKey(t)
	if full {
		if _, e := migrate.Migrate(ctx, pool, migrate.HubDomains()); e != nil {
			t.Fatal(e)
		}
	} else {
		if _, e := NewSQLDynamicClients(ctx, pool, testAccess(t), testOrigin+"/mcp", key, 0); e == nil {
			t.Fatal("missing DCR migration accepted")
		}
		b, e := os.ReadFile("../store/migrate/migrations/0115_mcpgw_dcr.sql")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, testAccess(t), string(b)); e != nil {
			t.Fatal(e)
		}
	}
	s, e := NewSQLDynamicClients(ctx, pool, testAccess(t), testOrigin+"/mcp", key, 0)
	if e != nil {
		t.Fatal(e)
	}
	return s, pool, key
}
func dcrExec(t *testing.T, p *repo.Pool, q string, args ...any) {
	t.Helper()
	if _, e := p.Exec(context.Background(), testAccess(t), q, args...); e != nil {
		t.Fatal(e)
	}
}
func dcrCount(t *testing.T, p *repo.Pool, q string, args ...any) int {
	t.Helper()
	r, e := p.QueryRow(context.Background(), testAccess(t), q, args...)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	if e = r.Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func dcrClient(t *testing.T) Client {
	t.Helper()
	c, e := (DCRPolicy{}).metadata([]byte(publicRegistration))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func dcrPrincipal(subject string) extshared.Principal {
	return extshared.Principal{Provider: "feishu", TenantKey: "team", Issuer: "app", Subject: subject}
}

func TestDCRPGPersistenceRateAndIsolation(t *testing.T) {
	s, p, key := dcrPG(t, false)
	ctx := context.Background()
	c, created, expiry, e := s.Register(ctx, "192.0.2.1", dcrClient(t))
	if e != nil {
		t.Fatal(e)
	}
	if expiry.Sub(created) != 24*time.Hour {
		t.Fatalf("unbound TTL=%v", expiry.Sub(created))
	}
	reopened, e := NewSQLDynamicClients(ctx, p, testAccess(t), s.resource, key, 0)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := reopened.Resolve(ctx, c.ID); e != nil || got.ID != c.ID {
		t.Fatal("persistent registration lost", e)
	}
	other, e := NewSQLDynamicClients(ctx, p, testAccess(t), "https://other.example/mcp", key, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Resolve(ctx, c.ID); e == nil {
		t.Fatal("cross-resource registration visible")
	}
	wrong, e := NewSQLDynamicClients(ctx, p, testAccess(t), s.resource, randomKey(t), 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = wrong.Resolve(ctx, c.ID); e == nil {
		t.Fatal("wrong-key metadata decrypted")
	}
	for i := 0; i < 5; i++ {
		if e = s.Admit(ctx, "192.0.2.1"); e != nil {
			t.Fatal(e)
		}
	}
	if e = reopened.Admit(ctx, "192.0.2.1"); !errors.Is(e, errDCRRate) {
		t.Fatal("source rate lost across store restart", e)
	}
	for i := 0; i < 14; i++ {
		if e = s.Admit(ctx, fmt.Sprintf("198.51.100.%d", i)); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.Admit(ctx, "new-source"); !errors.Is(e, errDCRRate) {
		t.Fatal("global rate not enforced", e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client WHERE source_hash='192.0.2.1'`) > 0 {
		t.Fatal("raw source persisted")
	}
	row, e := p.QueryRow(ctx, testAccess(t), `SELECT sealed FROM mcpgw_dcr_client WHERE resource=$1 AND client_id=$2`, s.resource, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	var sealed []byte
	if e = row.Scan(&sealed); e != nil {
		t.Fatal(e)
	}
	if string(sealed) == publicRegistration {
		t.Fatal("metadata not encrypted")
	}
}

func TestDCRPGLifetimesSubjectEvictionAndRenewal(t *testing.T) {
	s, p, _ := dcrPG(t, false)
	ctx := context.Background()
	principal := dcrPrincipal("one")
	c, _, _, e := s.Register(ctx, "source", dcrClient(t))
	if e != nil {
		t.Fatal(e)
	}
	dcrExec(t, p, `UPDATE mcpgw_dcr_client SET expires_ns=(extract(epoch FROM clock_timestamp())*1000000000)::bigint-1 WHERE client_id=$1`, c.ID)
	if _, e = s.Resolve(ctx, c.ID); e == nil {
		t.Fatal("unbound exact/past expiry accepted")
	}
	// An already-authorized five-minute snapshot may still complete at expiry.
	if e = s.Prune(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Redeemed(ctx, c.ID, principal); e != nil {
		t.Fatal("valid in-flight grant lost on registration expiry", e)
	}
	if _, e = s.Resolve(ctx, c.ID); e != nil {
		t.Fatal("first successful exchange did not bind", e)
	}
	dcrExec(t, p, `UPDATE mcpgw_dcr_binding SET expires_ns=expires_ns-10000000000; UPDATE mcpgw_dcr_client SET expires_ns=expires_ns-10000000000`)
	if e = s.Redeemed(ctx, c.ID, principal); e != nil {
		t.Fatal(e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_binding WHERE client_id=$1 AND expires_ns-redeemed_ns=$2`, c.ID, int64(30*24*time.Hour)) != 1 {
		t.Fatal("successful token exchange did not renew bound TTL")
	}
	for i := 0; i < 16; i++ {
		next, _, _, e := s.Register(ctx, "source", dcrClient(t))
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Redeemed(ctx, next.ID, principal); e != nil {
			t.Fatal(e)
		}
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_binding WHERE resource=$1`, s.resource) != 16 {
		t.Fatal("principal bound client limit not enforced")
	}
	if _, e = s.Resolve(ctx, c.ID); e == nil {
		t.Fatal("least recently exchanged client not evicted")
	}
	shorter, e := NewSQLDynamicClients(ctx, p, testAccess(t), s.resource, s.hmacKey, 7*24*time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	// A configuration reduction is effective on the next successful exchange.
	renewed, _, _, e := s.Register(ctx, "source", dcrClient(t))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Redeemed(ctx, renewed.ID, dcrPrincipal("shorter")); e != nil {
		t.Fatal(e)
	}
	if e = shorter.Redeemed(ctx, renewed.ID, dcrPrincipal("shorter")); e != nil {
		t.Fatal(e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client c WHERE client_id=$1 AND expires_ns=(SELECT max(b.expires_ns) FROM mcpgw_dcr_binding b WHERE b.resource=c.resource AND b.client_id=c.client_id) AND expires_ns<(extract(epoch FROM clock_timestamp())*1000000000)::bigint+$2`, renewed.ID, int64(8*24*time.Hour)) != 1 {
		t.Fatal("configured shorter bound TTL retained an obsolete long deadline")
	}
	// Public client IDs are not user identities: removing A's binding must not
	// evict B's valid association with the same public registration.
	shared, _, _, e := s.Register(ctx, "source", dcrClient(t))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Redeemed(ctx, shared.ID, dcrPrincipal("two")); e != nil {
		t.Fatal(e)
	}
	if e = s.Redeemed(ctx, shared.ID, principal); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 16; i++ {
		next, _, _, e := s.Register(ctx, "source", dcrClient(t))
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Redeemed(ctx, next.ID, principal); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.Resolve(ctx, shared.ID); e != nil {
		t.Fatal("another subject's live binding was evicted", e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client c WHERE client_id=$1 AND expires_ns=(SELECT max(b.expires_ns) FROM mcpgw_dcr_binding b WHERE b.resource=c.resource AND b.client_id=c.client_id)`, shared.ID) != 1 {
		t.Fatal("eviction retained a deadline beyond the remaining subject binding")
	}
	if e = s.Redeemed(ctx, shared.ID, extshared.Principal{}); e == nil {
		t.Fatal("untrusted/absent principal bound a client")
	}
	dcrExec(t, p, `UPDATE mcpgw_dcr_binding SET expires_ns=0; UPDATE mcpgw_dcr_client SET expires_ns=0`)
	if _, e = s.Resolve(ctx, shared.ID); e == nil {
		t.Fatal("expired bound client accepted")
	}
	if e = s.Prune(ctx); e != nil {
		t.Fatal(e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client`) != 0 {
		t.Fatal("expired bound clients leaked")
	}
}

func TestDCRPGQuotaGCAndConcurrentAdmission(t *testing.T) {
	s, p, _ := dcrPG(t, false)
	ctx := context.Background()
	for i := 0; i < 128; i++ {
		if _, _, _, e := s.Register(ctx, "one-source", dcrClient(t)); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, _, e := s.Register(ctx, "one-source", dcrClient(t)); !errors.Is(e, errDCRCapacity) {
		t.Fatal("per-source live quota missing", e)
	}
	dcrExec(t, p, `UPDATE mcpgw_dcr_client SET expires_ns=0 WHERE source_hash=$1`, s.digest("source", "one-source"))
	if _, _, _, e := s.Register(ctx, "one-source", dcrClient(t)); e != nil {
		t.Fatal("expired history permanently consumed capacity", e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client`) != 1 {
		t.Fatal("bounded expired cleanup failed")
	}
	if e := s.Prune(ctx); e != nil {
		t.Fatal(e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client`) != 1 {
		t.Fatal("GC deleted an unexpired client")
	}
	dcrExec(t, p, `INSERT INTO mcpgw_dcr_client(resource,client_id,sealed,source_hash,created_ns,expires_ns) SELECT $1,'fixture-'||i,decode('00','hex'),'fixture',0,9223372036854775807 FROM generate_series(1,4095) i`, s.resource)
	if _, _, _, e := s.Register(ctx, "new-source", dcrClient(t)); !errors.Is(e, errDCRCapacity) {
		t.Fatal("global live quota missing", e)
	}
	dcrExec(t, p, `UPDATE mcpgw_dcr_client SET expires_ns=0 WHERE client_id LIKE 'fixture-%'`)
	if _, _, _, e := s.Register(ctx, "new-source", dcrClient(t)); e != nil {
		t.Fatal("quota counted expired uncollected rows", e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client WHERE expires_ns=0`) != 4095-256 {
		t.Fatal("cleanup was not bounded to 256")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := s.Admit(ctx, "concurrent")
			if e == nil {
				successes.Add(1)
			} else if !errors.Is(e, errDCRRate) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 5 {
		t.Fatalf("concurrent quota overspent: %d", successes.Load())
	}
	// Source bucket storage has a hard cardinality bound independent of IP churn.
	dcrExec(t, p, `DELETE FROM mcpgw_dcr_bucket`)
	dcrExec(t, p, `INSERT INTO mcpgw_dcr_bucket(resource,bucket,tokens,updated_ns) SELECT $1,'source/fixture-'||i,5,(extract(epoch FROM clock_timestamp())*1000000000)::bigint FROM generate_series(1,4096) i`, s.resource)
	if e := s.Admit(ctx, "unseen"); !errors.Is(e, errDCRCapacity) {
		t.Fatal("unbounded source bucket allocation", e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_bucket WHERE bucket<>'global'`) != 4096 {
		t.Fatal("source cardinality grew past bound")
	}
}

func TestDCRPGBoundedCleanupForWidelySharedClient(t *testing.T) {
	s, p, _ := dcrPG(t, false)
	ctx := context.Background()
	c, _, _, e := s.Register(ctx, "source", dcrClient(t))
	if e != nil {
		t.Fatal(e)
	}
	dcrExec(t, p, `INSERT INTO mcpgw_dcr_binding(resource,client_id,principal_hash,redeemed_ns,expires_ns) SELECT $1,$2,'fixture-'||i,0,0 FROM generate_series(1,600) i`, s.resource, c.ID)
	dcrExec(t, p, `UPDATE mcpgw_dcr_client SET expires_ns=0,bound=true`)
	if e = s.Prune(ctx); e != nil {
		t.Fatal(e)
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_binding`) != 600-256 || dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client`) != 1 {
		t.Fatal("client GC cascaded an unbounded child deletion")
	}
	for i := 0; i < 2; i++ {
		if e = s.Prune(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_client`) != 0 || dcrCount(t, p, `SELECT count(*) FROM mcpgw_dcr_binding`) != 0 {
		t.Fatal("bounded shared-client cleanup failed to progress")
	}
}

func TestDCRPGCleanupPreservesUnexpiredRegistration(t *testing.T) {
	s, _, _ := dcrPG(t, false)
	ctx := context.Background()
	c, _, _, e := s.Register(ctx, "source", dcrClient(t))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Prune(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Resolve(ctx, c.ID); e != nil {
		t.Fatal("GC deleted a live unbound registration", e)
	}
	if e = s.Redeemed(ctx, c.ID, dcrPrincipal("live")); e != nil {
		t.Fatal(e)
	}
	if e = s.Prune(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Resolve(ctx, c.ID); e != nil {
		t.Fatal("GC deleted a live subject binding", e)
	}
}

func TestDCRFullPoolDoesNotConsumeExistingOAuthCapacity(t *testing.T) {
	s, p, key := dcrPG(t, true)
	ctx := context.Background()
	oauth, e := NewSQLStore(ctx, p, testAccess(t), s.resource, key)
	if e != nil {
		t.Fatal(e)
	}
	dcrExec(t, p, `INSERT INTO mcpgw_dcr_client(resource,client_id,sealed,source_hash,created_ns,expires_ns) SELECT $1,'fixture-'||i,decode('00','hex'),'fixture',0,9223372036854775807 FROM generate_series(1,4096) i`, s.resource)
	dcrExec(t, p, `INSERT INTO mcpgw_oauth(resource,kind,key_hash,sealed,expires_ns) SELECT $1,'attempt',lpad(i::text,64,'0'),decode('00','hex'),9223372036854775807 FROM generate_series(1,9000) i`, s.resource)
	if _, _, _, e = s.Register(ctx, "source", dcrClient(t)); !errors.Is(e, errDCRCapacity) {
		t.Fatal("fixture DCR pool was not full", e)
	}
	now := time.Now()
	if e = oauth.Put(ctx, "attempt", hash("unrelated-cimd"), []byte("test-only-payload"), now.Add(time.Minute), now); e != nil {
		t.Fatal("full DCR registry consumed the existing OAuth grant pool", e)
	}
}
