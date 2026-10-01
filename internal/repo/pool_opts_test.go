package repo

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// tunerRecorder 记下每个 Option 到底动了哪个池参数。
//
// 🔴 为什么要有这一格:四个构造器长得几乎一样,复制粘贴把 SetMaxIdleConns 写成 SetMaxOpenConns
// 编译照过、行为全错(池上限被当成空闲上限设),而这种错在集成用例里只会表现成「并发数不对」,
// 离真因差好几层。故在这里按【效果】钉:每个构造器恰好动一个方法、动的是它名字里那个。
type tunerRecorder struct {
	calls          []string
	open, idle     int
	life, idleTime time.Duration
}

func (r *tunerRecorder) SetMaxOpenConns(n int) {
	r.open = n
	r.calls = append(r.calls, "MaxOpenConns")
}
func (r *tunerRecorder) SetMaxIdleConns(n int) {
	r.idle = n
	r.calls = append(r.calls, "MaxIdleConns")
}
func (r *tunerRecorder) SetConnMaxLifetime(d time.Duration) {
	r.life = d
	r.calls = append(r.calls, "ConnMaxLifetime")
}
func (r *tunerRecorder) SetConnMaxIdleTime(d time.Duration) {
	r.idleTime = d
	r.calls = append(r.calls, "ConnMaxIdleTime")
}

func TestOptionConstructorsHitExactlyOneTunerMethod(t *testing.T) {
	var rec tunerRecorder
	for _, o := range []Option{
		MaxOpenConns(3),
		MaxIdleConns(5),
		ConnMaxLifetime(2 * time.Minute),
		ConnMaxIdleTime(30 * time.Second),
	} {
		o(&rec)
	}
	if got, want := rec.calls, []string{"MaxOpenConns", "MaxIdleConns", "ConnMaxLifetime", "ConnMaxIdleTime"}; len(got) != len(want) {
		t.Fatalf("Option 动了 %v,要每个构造器恰好动一个方法 %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("第 %d 个构造器动的是 %s,要 %s(串位＝池上限被当成空闲上限设)", i, got[i], want[i])
			}
		}
	}
	if rec.open != 3 || rec.idle != 5 {
		t.Errorf("数值串位: open=%d(要 3) idle=%d(要 5)", rec.open, rec.idle)
	}
	if rec.life != 2*time.Minute || rec.idleTime != 30*time.Second {
		t.Errorf("时长串位: life=%v(要 2m) idleTime=%v(要 30s)", rec.life, rec.idleTime)
	}
}

// TestPoolIdleConnsLandOnPool 用 SQLite 池钉「MaxIdleConns 真落到池上」,并顺手把
// database/sql 的【默认值】测出来 —— 那个默认正是 G3 身份层复验 🟡③ 的成因侧证据:
// 4 条连接并发用完之后,不设 MaxIdleConns 只剩 2 条空闲,第 3 条起每轮都得现建连接。
//
// 🔴 不需要 PG:池参数是 database/sql 层的,与驱动无关;要 PG 才能钉的那半
// (opts 真到了 OpenPG 建的池上、N 条事务能同时在场)在 pool_pg_opts_test.go。
func TestPoolIdleConnsLandOnPool(t *testing.T) {
	ctx := context.Background()
	tc, err := NewAccessCtx("test", "repo-opts")
	if err != nil {
		t.Fatalf("构造访问上下文: %v", err)
	}

	for _, tc2 := range []struct {
		what     string
		opts     []Option
		wantIdle int
	}{
		{"抬到 4", []Option{MaxOpenConns(4), MaxIdleConns(4)}, 4},
		{"压到 1", []Option{MaxOpenConns(4), MaxIdleConns(1)}, 1},
		{"不设(默认)", nil, 2}, // 🔴 database/sql 的默认 MaxIdleConns＝2,不是 0、也不是「无上限」
	} {
		t.Run(tc2.what, func(t *testing.T) {
			dsn := "file:" + filepath.Join(t.TempDir(), "idle.db")
			p, err := OpenSQLite(ctx, dsn, tc2.opts...)
			if err != nil {
				t.Fatalf("OpenSQLite: %v", err)
			}
			defer func() { _ = p.Close() }()

			// 4 条事务【同时在场】(事务把连接钉住,提交前不还池),故必然用掉 4 条连接。
			const n = 4
			var wg sync.WaitGroup
			start := make(chan struct{})
			errs := make([]error, n)
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					errs[i] = p.InTx(ctx, tc, nil, func(tx Tx) error {
						var one int
						if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil {
							return err
						}
						time.Sleep(80 * time.Millisecond) // 撑开窗口,确保四条真同时在
						return nil
					})
				}(i)
			}
			close(start)
			wg.Wait()
			for i, e := range errs {
				if e != nil {
					t.Fatalf("第 %d 条事务: %v", i, e)
				}
			}

			if got := p.db.Stats().Idle; got != tc2.wantIdle {
				t.Errorf("并发用完后空闲连接 = %d,要 %d(%s)", got, tc2.wantIdle, tc2.what)
			}
		})
	}
}
