package repo_test

import (
	"context"
	"sync"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// 本文件是【外部测试包】(repo_test),因为它要用 internal/store/pgtest 起临时 PG,
// 而 pgtest 自己 import internal/repo —— 内部测试包那么写就是导入环。
//
// 钉的是 SQLite 那格钉不了的另一半:opts 真的到了【OpenPG 建出来的池】上,
// 而且「N 条事务同时在场」这件事在 PG 上可证 —— 那正是 G3 身份层两条并发格
// (TestPGStoreConcurrentTakeCredentialOnce／…ApproveSameName)缺的能力(复验 🟡③)。

// TestOpenPGOptionsReachThePool 两半各钉一条,缺任何一半都会让「加了 opts」成假:
//
//	① 上限真落到 PG 池上:把池压到 2 条,4 条并发事务就只能落在 ≤2 个后端进程上;
//	② 抬到 4 条并允许 4 条空闲:4 条并发事务各占一个后端 ⇒ 4 个不同的 pg_backend_pid。
func TestOpenPGOptionsReachThePool(t *testing.T) {
	dsn := pgtest.DSN(t)
	ctx := context.Background()
	tc, err := repo.NewAccessCtx("test", "repo-pg-opts")
	if err != nil {
		t.Fatalf("构造访问上下文: %v", err)
	}

	t.Run("压到 2 条", func(t *testing.T) {
		pool, err := repo.OpenPG(ctx, dsn, repo.MaxOpenConns(2))
		if err != nil {
			t.Fatalf("OpenPG: %v", err)
		}
		defer func() { _ = pool.Close() }()
		pids := concurrentBackends(t, ctx, pool, tc, 4)
		if len(pids) == 0 {
			t.Fatalf("一个后端都没取到")
		}
		if len(pids) > 2 {
			t.Errorf("池上限 2 而 4 条并发事务落在 %d 个后端上 —— MaxOpenConns 没落到 PG 池上", len(pids))
		}
	})

	t.Run("抬到 4 条", func(t *testing.T) {
		pool, err := repo.OpenPG(ctx, dsn, repo.MaxOpenConns(4), repo.MaxIdleConns(4))
		if err != nil {
			t.Fatalf("OpenPG: %v", err)
		}
		defer func() { _ = pool.Close() }()
		pids := concurrentBackends(t, ctx, pool, tc, 4)
		if len(pids) != 4 {
			t.Errorf("4 条并发事务只落在 %d 个后端上,要 4 个各不相同 —— 预热后本该同时在场", len(pids))
		}
	})
}

// concurrentBackends 放 n 条事务同时跑,每条在自己的连接上取 pg_backend_pid,返回去重后的集合。
//
// 🔴 「同时在场」是被构造出来的,不是碰运气:起跑屏障让 n 条一起进,事务把连接钉住
// (提交前不还池),里面再睡 holdEach 把窗口撑到远大于调度抖动。
// 🔴 池上限小于 n 时,多出来的那几条会阻塞在取连接上 —— 那正是①要的现象,不是缺陷。
func concurrentBackends(t *testing.T, ctx context.Context, pool *repo.Pool, tc repo.AccessCtx, n int) map[int]bool {
	t.Helper()
	const holdEach = `SELECT pg_sleep(0.3)`

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		pids  = map[int]bool{}
		errs  = make([]error, n)
		start = make(chan struct{})
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = pool.InTx(ctx, tc, nil, func(tx repo.Tx) error {
				var pid int
				if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return err
				}
				mu.Lock()
				pids[pid] = true
				mu.Unlock()
				_, err := tx.Exec(ctx, holdEach)
				return err
			})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 条并发事务: %v", i, err)
		}
	}
	return pids
}
