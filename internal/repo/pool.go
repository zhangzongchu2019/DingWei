package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql 驱动
	_ "modernc.org/sqlite"             // sqlite database/sql 驱动
)

// Pool 持有裸库句柄。
//
// 🔴 db 字段未导出,且本包【没有任何函数返回 *sql.DB / *sql.Tx】——
//
//	这是 §22.2c #1 的两半:「不导出」与「任何函数不返回它」。
//	判据二(scripts/repo-boundary-gate.sh)对本包同样生效,允许清单不豁免它:
//	仓储包自己开一个 DB() 访问器,正是「句柄出了仓储包」本身。
type Pool struct {
	db *sql.DB
}

// OpenPG 是本仓【唯一】允许出现 sql.Open 的地方之一(另见同包其它 Open*)。
//
// opts 与 OpenSQLite 同一套(Option／PoolTuner),故收编与并发语义的表达能力两侧一致。
//
// 🔴 为什么 PG 侧也必须能调池参数(G3 身份层复验 🟡③,组长裁定 ruling-2026-10-01T152239Z 六③):
//
//	database/sql 的默认是 MaxIdleConns=2、MaxOpenConns=0(无上限)。
//	「无上限」不等于「并发用例就能造出并发」:空闲连接只有 2 条时,第 3 个及以后的 goroutine
//	各自要【现建一条连接】(TCP＋认证,毫秒级),而它要撞的临界窗口
//	(SELECT … FOR UPDATE 到同事务里那句 UPDATE 之间)是微秒级 ⇒ 抖动比窗口大三个量级。
//	实测:撤掉身份层 TakeCredential 里那句 SELECT … FOR UPDATE 的行锁(在 G3 的
//	g3-identity-pg-r1 叠上,本分支底座还没有那个文件,故不写行号),原样 -count=30 与
//	加起跑屏障 -count=10 共 40 轮 0 红 —— 🔴 那不能读成「锁没用」,而是两边根本没
//	同时待在窗口里(于是那次变异与没变异等价,复验把它记成覆盖缺口而不是判据失效)。
//	⇒ 要造真并发,调用方得能把池【预热】成 N 条常驻连接:MaxOpenConns(N)＋MaxIdleConns(N),
//	  跑之前先用 N 条查询把它们建出来,再放起跑屏障。
//	🔴 本函数只给这个能力,【不代为预热】:预热几条、什么时候预热是调用方的语义,
//	  仓储包代做就等于替所有调用方决定并发形状(与 Option 那条「不代为拼装 dsn」同理)。
func OpenPG(ctx context.Context, dsn string, opts ...Option) (*Pool, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	for _, o := range opts {
		o(tuner{db: db})
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Pool{db: db}, nil
}

// Close 关闭池。
func (p *Pool) Close() error { return p.db.Close() }

// Ping 健康检查:本就无租户语义,故要求显式 SystemCtx,不接受零值。
func (p *Pool) Ping(ctx context.Context, t AccessCtx) error {
	if err := t.check(); err != nil {
		return err
	}
	return p.db.PingContext(ctx)
}

// Exec / Query / QueryRow 是受控查询面。
//
// ⚠️ 现阶段(SP7a)它们【只强制上下文存在】,并不注入租户过滤条件 ——
//
//	因为领域表里还没有租户列(领域 schema 归 SP7b,由各领域 spec 自交,不作统一前置)。
//	注入位就在这里:租户列一旦落地,过滤在本函数内单点注入,调用方不改。
//	🔴 不得因为「调用没报错」就认为跨租户读已被拦住 —— 现在拦不住,本注释不掩盖这一点。
func (p *Pool) Exec(ctx context.Context, t AccessCtx, query string, args ...any) (sql.Result, error) {
	if err := t.check(); err != nil {
		return nil, err
	}
	return p.db.ExecContext(ctx, query, args...)
}

func (p *Pool) Query(ctx context.Context, t AccessCtx, query string, args ...any) (*sql.Rows, error) {
	if err := t.check(); err != nil {
		return nil, err
	}
	return p.db.QueryContext(ctx, query, args...)
}

// QueryRow 返回 (*sql.Row, error) 而非裸 *sql.Row:
// sql.Row 的错误只能在 Scan 时暴露,缺上下文这类【拒绝】不该被推迟到 Scan,
// 否则调用方很容易写成「拒绝＝一次扫不出行」,即把 deny 降格成空结果集(§22.2a #2a 禁止)。
func (p *Pool) QueryRow(ctx context.Context, t AccessCtx, query string, args ...any) (*sql.Row, error) {
	if err := t.check(); err != nil {
		return nil, err
	}
	return p.db.QueryRowContext(ctx, query, args...), nil
}

// Tx 事务包装:同样不把 *sql.Tx 交出去。
type Tx struct {
	tx *sql.Tx
	t  AccessCtx
}

func (x Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return x.tx.ExecContext(ctx, query, args...)
}

func (x Tx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return x.tx.QueryContext(ctx, query, args...)
}

func (x Tx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return x.tx.QueryRowContext(ctx, query, args...)
}

// InTx 以回调形态提供事务 —— 回调式而非「返回一个 Tx 让你自己 Commit」,
// 是为了让 *sql.Tx 无从逃出本包(判据二)。fn 返回错误即回滚。
func (p *Pool) InTx(ctx context.Context, t AccessCtx, opts *sql.TxOptions, fn func(Tx) error) error {
	if err := t.check(); err != nil {
		return err
	}
	tx, err := p.db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	if err := fn(Tx{tx: tx, t: t}); err != nil {
		// 回调可用 *CommitErr 要求「提交后仍把错误交回」——见 CommitErr 注释。
		var ce *CommitErr
		if errors.As(err, &ce) {
			if cerr := tx.Commit(); cerr != nil {
				return &CommitFailed{Err: cerr}
			}
			return ce.Err
		}
		_ = tx.Rollback()
		return err
	}
	if cerr := tx.Commit(); cerr != nil {
		return &CommitFailed{Err: cerr}
	}
	return nil
}

// CommitErr 让回调表达「**提交这个事务,但把 Err 交回调用方**」。
//
// 🔴 为什么必须有它:datad 的 AssignGeneration 里有三处「审计行写入 + 提交 + 返回业务错误」——
//
//	审计属 fail-closed 要求(拒绝也必须留痕),所以那三处的 commit 不能因为「有错」就变成 rollback。
//	若把事务改成回调式却不提供这个表达,重写就会**静悄悄地把审计行丢掉**,
//	而调用方看到的错误一模一样 —— 这正是 §4.5 警告的「以重写为由丢弃既有语义」。
//	⇒ 机制先到位,再动那段代码。
type CommitErr struct{ Err error }

func (c *CommitErr) Error() string { return c.Err.Error() }
func (c *CommitErr) Unwrap() error { return c.Err }

// CommitFailed 表示提交本身失败(区别于「业务错误」),便于调用方按 fail-closed 单独处置。
type CommitFailed struct{ Err error }

func (c *CommitFailed) Error() string { return "commit failed: " + c.Err.Error() }
func (c *CommitFailed) Unwrap() error { return c.Err }

// Option 是建池选项。
//
// 🔴 存在的理由：被收编的包各自带着【已经用事故换来的】连接层语义，收编不得把它们抹掉。
//
//	例：fanout/spool 有 `SetMaxOpenConns(1)`，原注释写「面板自死锁教训:单连接串行,杜绝游标互等」。
//	若仓储包只提供一个无参 Open，收编就会静悄悄丢掉这条 —— 那正是 §4.5 说的
//	「不得以【重写】为由丢弃任一既有并发语义」。⇒ 先让仓储包能表达它，再动那些包。
type Option func(PoolTuner)

// PoolTuner 是 Option 能碰到的【全部】能力面：只调池参数,不交出裸句柄。
//
// 🔴 甲域 sol CR #6:上一版写的是 `type Option func(*sql.DB)`。
//
//	那不是普通的「句柄作入参」(调用方 -> 包内,句柄没出去);方向是反的 ——
//	仓储包把裸句柄【交给包外提供的回调】,调用方一句 `func(d *sql.DB){ stolen = d }`
//	就把句柄搬出了包。判据二按「导出函数/字段/接口方法【返回】句柄」判,
//	对这条回调外泄输出 0,且主闸照样打印「持平 ✓」—— 又一处「闸没起作用但看得见 ✓」。
//	现在传进 Option 的具体类型是本包【未导出】的 tuner,不是 *sql.DB,
//	故包外 `func(t repo.PoolTuner){ d := t.(*sql.DB) }` 断言必失败:
//	这条收口靠类型系统成立,不靠自觉。
//	保证范围:不含 reflect / unsafe(与判据二的既定作用域一致,不互相冒充)。
type PoolTuner interface {
	SetMaxOpenConns(n int)
	SetMaxIdleConns(n int)
	SetConnMaxLifetime(d time.Duration)
	SetConnMaxIdleTime(d time.Duration)
}

// tuner 未导出:包外拿到的是它的接口值,断不回 *sql.DB。
type tuner struct{ db *sql.DB }

func (t tuner) SetMaxOpenConns(n int)              { t.db.SetMaxOpenConns(n) }
func (t tuner) SetMaxIdleConns(n int)              { t.db.SetMaxIdleConns(n) }
func (t tuner) SetConnMaxLifetime(d time.Duration) { t.db.SetConnMaxLifetime(d) }
func (t tuner) SetConnMaxIdleTime(d time.Duration) { t.db.SetConnMaxIdleTime(d) }

// MaxOpenConns 固定池上限。n=1 即单连接串行。
func MaxOpenConns(n int) Option { return func(t PoolTuner) { t.SetMaxOpenConns(n) } }

// MaxIdleConns 固定【空闲】连接上限。
//
// 🔴 不设就是 database/sql 的默认 2,不是 0(见 OpenPG 注:这正是并发用例造不出并发的成因)。
// 要 N 条连接同时在场,MaxOpenConns 与 MaxIdleConns 得【一起】抬到 N:只抬前者,
// 用完的连接会被关掉,下一轮又得现建。
func MaxIdleConns(n int) Option { return func(t PoolTuner) { t.SetMaxIdleConns(n) } }

// ConnMaxLifetime 一条连接的最长寿命(到点即关,零值＝不限)。
func ConnMaxLifetime(d time.Duration) Option {
	return func(t PoolTuner) { t.SetConnMaxLifetime(d) }
}

// ConnMaxIdleTime 一条连接最长空闲多久(到点即关,零值＝不限)。
//
// 🔴 这两个是【收编用】的表达能力,不是本仓自己新发明的策略:PoolTuner 早就列了这四个方法
// (被收编的包各自带着用事故换来的连接层语义,收编不得抹掉),而此前只有 MaxOpenConns 导出了
// 构造器 ⇒ 另三个能力【写得出接口、调不到】。补齐它们不放宽任何判据:Option 拿到的仍是
// 本包未导出的 tuner,断不回 *sql.DB(反测见 pool_option_test.go)。
func ConnMaxIdleTime(d time.Duration) Option {
	return func(t PoolTuner) { t.SetConnMaxIdleTime(d) }
}

// OpenSQLite 打开 SQLite 池。dsn 由调用方给全（含 pragma），本包不代为拼装 ——
// pragma 属被收编包的既有行为，代拼即改语义。
func OpenSQLite(ctx context.Context, dsn string, opts ...Option) (*Pool, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	for _, o := range opts {
		o(tuner{db: db})
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Pool{db: db}, nil
}
