package identity_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// pgFactory 一个临时 PG（pgtest，随起随清）、每个契约格一个独立 schema（经连接参数 search_path 切换）⇒ 格与格互不干扰。
func pgFactory(t *testing.T) identitytest.Factory {
	t.Helper()
	newSchema := pgSchemaFactory(t)
	return func(st *testing.T) identity.Store {
		st.Helper()
		pool, tc := newSchema(st)
		for _, ddl := range identitytest.PGTestSchema() {
			if _, err := pool.Exec(context.Background(), tc, ddl); err != nil {
				st.Fatalf("apply schema: %v\n%s", err, ddl)
			}
		}
		return identity.NewPGStore(pool)
	}
}

// pgSchemaFactory 每次调用给一个空的独立 schema 上的连接池（未建表）。
func pgSchemaFactory(t *testing.T) func(*testing.T) (*repo.Pool, repo.AccessCtx) {
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
	return func(st *testing.T) (*repo.Pool, repo.AccessCtx) {
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
		return pool, tc
	}
}

// CheckSchema：本包不自行建表；缺表时 Hub 与初始化脚本据此拒绝运行，并报出缺哪几张。
func TestPGStoreCheckSchema(t *testing.T) {
	pool, tc := pgSchemaFactory(t)(t)
	ctx := context.Background()
	s := identity.NewPGStore(pool)
	err := s.CheckSchema(ctx)
	if err == nil || !strings.Contains(err.Error(), "np_user, np_identity, np_registration_code, np_session, np_agent, np_enroll_application, np_group, np_group_member, audit_log") {
		t.Fatalf("empty schema: %v", err)
	}
	for _, ddl := range identitytest.PGTestSchema() {
		if _, err := pool.Exec(ctx, tc, ddl); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CheckSchema(ctx); err != nil {
		t.Fatalf("full schema: %v", err)
	}
	if _, err := pool.Exec(ctx, tc, `DROP TABLE audit_log`); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckSchema(ctx); err == nil || !strings.HasSuffix(err.Error(), "missing tables: audit_log") {
		t.Fatalf("without audit_log: %v", err)
	}
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

// PG 才有的一格：两个初始化脚本同时执行（各给不同的用户 ID），只能建出一个管理用户（表锁保证；
// 只靠「先查后插」会两个都查到空、都插进去）。
func TestPGStoreConcurrentCreateFirstAdminOnce(t *testing.T) {
	s := pgFactory(t)(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, refused := 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "admin" + string(rune('a'+i))
			err := s.CreateFirstAdmin(ctx, identity.User{UserID: id, ShortName: id, Type: identity.UserPlatformAdmin, State: identity.UserActive, CreatedAt: t0})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, identity.ErrAdminExists):
				refused++
			default:
				t.Errorf("admin %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 || refused != n-1 {
		t.Fatalf("concurrent first-admin: wins=%d refused=%d, want 1 and %d", wins, refused, n-1)
	}
	if c, _ := s.CountUsers(ctx); c != 1 {
		t.Fatalf("users=%d, want 1", c)
	}
}

// 表锁的确定性判据（并发那一格未必撞得上竞态窗口）：另一事务插入了 manager、尚未提交时，
// CreateFirstAdmin 必须等它提交，再看到它、拒绝；不加锁就会读不到未提交的行，直接插入第二个管理用户。
func TestPGStoreFirstAdminWaitsForUncommittedInsert(t *testing.T) {
	pool, tc := pgSchemaFactory(t)(t)
	ctx := context.Background()
	for _, ddl := range identitytest.PGTestSchema() {
		if _, err := pool.Exec(ctx, tc, ddl); err != nil {
			t.Fatal(err)
		}
	}
	s := identity.NewPGStore(pool)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	done := make(chan error, 1)
	returnedEarly := false
	err := pool.InTx(ctx, tc, nil, func(tx repo.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO np_user (user_id_lc, user_id, short_name, short_name_lc, type, state, created_at)
			VALUES ('mgr', 'mgr', 'mg', 'mg', 'manager', 'pending_bind', $1)`, t0); err != nil {
			return err
		}
		go func() {
			done <- s.CreateFirstAdmin(ctx, identity.User{UserID: "Boss", ShortName: "bs", Type: identity.UserPlatformAdmin, State: identity.UserActive, CreatedAt: t0})
		}()
		select {
		case err := <-done:
			returnedEarly = true
			t.Errorf("CreateFirstAdmin returned (%v) while another transaction's insert was uncommitted; it must wait on the table lock", err)
		case <-time.After(500 * time.Millisecond):
		}
		return nil // 提交
	})
	if err != nil {
		t.Fatal(err)
	}
	if returnedEarly {
		return
	}
	select {
	case err := <-done:
		if !errors.Is(err, identity.ErrAdminExists) {
			t.Fatalf("after the manager committed: %v, want ErrAdminExists", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CreateFirstAdmin still blocked after commit")
	}
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Fatalf("users=%d, want 1", n)
	}
}
