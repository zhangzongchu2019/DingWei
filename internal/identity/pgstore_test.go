package identity_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// 审计复用既有 audit_log；正式环境由 G2 的迁移账本建。测试里建一张字段兼容的最小表。
const testAuditLogDDL = `CREATE TABLE IF NOT EXISTS audit_log (
	id bigserial PRIMARY KEY, actor text, action text, target text, before_json text, after_json text, ts timestamptz)`

// pgFactory 一个临时 PG（pgtest，随起随清）、每个契约格一个独立 schema（经连接参数 search_path 切换）⇒ 格与格互不干扰。
func pgFactory(t *testing.T) identitytest.Factory {
	t.Helper()
	dsn := pgtest.DSN(t)
	ctx := context.Background()
	admin, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	tc, err := repo.NewAccessCtx("test", "identity-pg")
	if err != nil {
		t.Fatal(err)
	}
	return func(st *testing.T) identity.Store {
		st.Helper()
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		schema := "t_" + hex.EncodeToString(b)
		if _, err := admin.Exec(ctx, tc, `CREATE SCHEMA `+schema); err != nil {
			st.Fatalf("create schema: %v", err)
		}
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		pool, err := repo.OpenPG(ctx, dsn+sep+"search_path="+schema)
		if err != nil {
			st.Fatalf("open schema pool: %v", err)
		}
		st.Cleanup(func() { _ = pool.Close() })
		for _, ddl := range append([]string{testAuditLogDDL}, splitSQL(identity.PGSchemaSQL)...) {
			if _, err := pool.Exec(ctx, tc, ddl); err != nil {
				st.Fatalf("apply schema: %v\n%s", err, ddl)
			}
		}
		return identity.NewPGStore(pool)
	}
}

// splitSQL 按分号切开迁移文件里的多条语句（文件里没有函数体与字符串内分号）。
func splitSQL(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ";") {
		var keep []string
		for _, ln := range strings.Split(part, "\n") {
			if t := strings.TrimSpace(ln); t != "" && !strings.HasPrefix(t, "--") {
				keep = append(keep, ln)
			}
		}
		if q := strings.TrimSpace(strings.Join(keep, "\n")); q != "" {
			out = append(out, q)
		}
	}
	return out
}

// 同一套契约（与内存实现完全相同的 25 格）跑在 PG 上。
func TestPGStoreContract(t *testing.T) {
	identitytest.Run(t, pgFactory(t))
}

// PG 才有的一格：并发取同一张一次性令牌，只能成功一次（行锁保证；内存实现是全局锁，测不到这一层）。
func TestPGStoreConcurrentTakeCredentialOnce(t *testing.T) {
	s := pgFactory(t)(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-race", OwnerLC: "zzc", Name: "Racer", CreatedAt: t0},
		identity.Application{ID: "app-race", PollTokenHash: "tok", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, ok, err := s.TakeCredential(ctx, "app-race", "cred-"+string(rune('a'+i)), t0.Add(time.Second))
			if err != nil {
				t.Errorf("take %d: %v", i, err)
				return
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent takes succeeded %d times, want exactly 1", wins)
	}
}

// PG 才有的一格：并发以同名批准两条申请，名字只能被批出一次（部分唯一索引保证）。
func TestPGStoreConcurrentApproveSameName(t *testing.T) {
	s := pgFactory(t)(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for _, id := range []string{"app-1", "app-2"} {
		if err := s.CreateApplication(ctx, identity.Application{ID: id, OwnerLC: "zzc", DeviceFingerprint: "dev-" + id,
			RequestedAt: t0, ExpiresAt: t0.Add(time.Hour), State: identity.AppPending}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{"app-1", "app-2"} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			_, errs[i] = s.Approve(ctx, identity.ApproveInput{AppID: id, OwnerLC: "zzc", AgentID: "ag-" + id, Name: []string{"Twin", "TWIN"}[i], GroupID: "g"}, t0)
		}(i, id)
	}
	wg.Wait()
	ok, taken := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case err == identity.ErrNameTaken:
			taken++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || taken != 1 {
		t.Fatalf("approve results ok=%d name_taken=%d, want 1 and 1", ok, taken)
	}
}
