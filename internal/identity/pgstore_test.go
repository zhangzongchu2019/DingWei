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

// pgSchemaFactory 每次调用给一个空的独立 schema 上的连接池（未建表）；opts 原样交给该池的 repo.OpenPG。
func pgSchemaFactory(t *testing.T, opts ...repo.Option) func(*testing.T) (*repo.Pool, repo.AccessCtx) {
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
		pool, err := repo.OpenPG(ctx, dsn+sep+"search_path="+schema, opts...)
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
	if err == nil || !strings.Contains(err.Error(), "np_user, np_identity, np_registration_code, np_session, np_agent, np_enroll_application, np_group, np_group_member, audit_log, np_device_claim") {
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

// 两条「PG 才有的并发格」共用的真并发装置（qwen 复验 🟡③：原写法 20 个协程撤掉行锁也一次都没红，
// 因为池是冷的、建连接远慢于事务本身 ⇒ 事务在库侧事实串行）。三件的分量不一样（按实测，不按设想）：
//
//	① 池预热【承重】：MaxOpenConns(n)＋MaxIdleConns(n)，先让 n 条连接同时在场再还池 ⇒ 起跑时不用现建连接。
//	   删掉它就退回 🟡③ 那一形：qwen 复验 S4b（退回无预热的写法）撤行锁 20 轮全绿。
//	   空闲连接默认只留 2 条（database/sql 默认 MaxIdleConns），撞上竞态的事务数也就只有 2 左右。
//	② 起跑屏障【放大】：n 个协程等同一个信号一起发起；
//	③ 撑开窗口【放大】：本格的临时 schema 上给 np_agent 挂一个触发器，每次插入／更新后在同一事务里睡 holdAgentWrite。
//	   产品代码一字不动；触发器只拉长「写了 np_agent 到提交」这段，不改任何结果。
//	②③ 把撤锁后「多拿凭据的事务数」从 2 左右放大到 n。单独去掉其一本格仍红（qwen 复验 S2 撤窗口、S4d 撤屏障，
//	各 10／10）；叠加撤到冷池（S4、S4c）各 20／20 仍红，但赢家降为 2。都留着：离 flaky 更远。
//
// 每格的反向对照（撤掉承重件必须红）写在各格注释里，读数见交付件与复验件 REVIEW-G3-PGREALCONC。
const holdAgentWrite = "0.2" // 秒；远大于调度抖动与连接取用（毫秒级），又不至于让 20 条串行时太慢

func concurrentPGStore(t *testing.T, n int) identity.Store {
	t.Helper()
	pool, tc := pgSchemaFactory(t, repo.MaxOpenConns(n), repo.MaxIdleConns(n))(t)
	ctx := context.Background()
	for _, ddl := range identitytest.PGTestSchema() {
		if _, err := pool.Exec(ctx, tc, ddl); err != nil {
			t.Fatalf("apply schema: %v\n%s", err, ddl)
		}
	}
	for _, ddl := range []string{
		`CREATE FUNCTION hold_agent_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(` + holdAgentWrite + `); RETURN NULL; END $$`,
		`CREATE TRIGGER hold_agent_write AFTER INSERT OR UPDATE ON np_agent FOR EACH ROW EXECUTE FUNCTION hold_agent_write()`,
	} {
		if _, err := pool.Exec(ctx, tc, ddl); err != nil {
			t.Fatalf("widen window: %v\n%s", err, ddl)
		}
	}
	// 预热：n 条事务同时在场（各自钉住一条连接），取到 n 个不同的后端才算热；还池后因 MaxIdleConns(n) 全部留作空闲。
	pids := map[int]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := pool.InTx(ctx, tc, nil, func(tx repo.Tx) error {
				var pid int
				if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return err
				}
				mu.Lock()
				pids[pid] = true
				mu.Unlock()
				_, err := tx.Exec(ctx, `SELECT pg_sleep(0.1)`)
				return err
			}); err != nil {
				t.Errorf("warm pool: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(pids) != n {
		t.Fatalf("warm pool: %d backends, want %d", len(pids), n)
	}
	return identity.NewPGStore(pool)
}

// PG 才有的一格：并发取同一张一次性令牌，只能成功一次（承重件：TakeCredential 对申请行的 FOR UPDATE；
// 内存实现是全局锁，测不到这一层）。
// 反向：撤掉申请行的 FOR UPDATE ⇒ 其余事务在第一条提交前都读到「未领取」，随后排队在 Agent 行锁上、逐个领取成功 ⇒ 必红。
func TestPGStoreConcurrentTakeCredentialOnce(t *testing.T) {
	const n = 20
	s := concurrentPGStore(t, n)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-race", OwnerLC: "zzc", Name: "Racer", GroupID: "g-race", CreatedAt: t0},
		identity.Application{ID: "app-race", PollTokenHash: "tok", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)},
		&identity.Group{Code: "g-race", ShortName: "race", CreatorLC: "zzc", CreatedAt: t0, UpdatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
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
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent takes succeeded %d times, want exactly 1", wins)
	}
}

// PG 才有的一格：并发以同名（只差大小写）批准多条申请，名字只能被批出一次，其余一律 ErrNameTaken
// （承重件：部分唯一索引 np_agent_owner_name_current；后到的插入在索引上等先到的提交，再撞约束）。
// 反向：把唯一索引换成「插入前先查同名」⇒ 顺序执行的契约格照绿（先查查得到），本格必红（窗口里大家都查不到）。
func TestPGStoreConcurrentApproveSameName(t *testing.T) {
	const n = 6
	s := concurrentPGStore(t, n)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	names := []string{"Twin", "TWIN", "twin", "tWIN", "TwIn", "twiN"}
	for i := 0; i < n; i++ {
		id := "app-" + string(rune('a'+i))
		if err := s.CreateApplication(ctx, identity.Application{ID: id, OwnerLC: "zzc", DeviceFingerprint: "dev-" + id,
			RequestedAt: t0, ExpiresAt: t0.Add(time.Hour), State: identity.AppPending}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "app-" + string(rune('a'+i))
			<-start
			_, errs[i] = s.Approve(ctx, identity.ApproveInput{AppID: id, OwnerLC: "zzc", AgentID: "ag-" + id, Name: names[i], GroupID: "g-" + id,
				NewGroup: &identity.Group{Code: "g-" + id, ShortName: "n" + string(rune('a'+i)), CreatorLC: "zzc", CreatedAt: t0, UpdatedAt: t0}}, t0)
		}(i)
	}
	close(start)
	wg.Wait()
	ok, taken := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, identity.ErrNameTaken):
			taken++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || taken != n-1 {
		t.Fatalf("approve results ok=%d name_taken=%d, want 1 and %d", ok, taken, n-1)
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
