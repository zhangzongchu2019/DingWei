// Package migrate 是新平台 PG 库的【迁移账本】（G2 owns internal/store）。
//
// 依据：组长裁定 ruling-2026-10-01T145840Z（板 ba71a98c）——「新平台必须 PG、去掉内存退路、
// 缺表即拒并报缺哪几张；建表归 G2 迁移账本；Hub 启动与 bootstrap-admin 均先 Migrate 再用」。
// 口径页（背景件，🔴 与本包代码冲突时以代码为准）：
// runbooks/DESIGN-G2-MIGRATION-LEDGER-20261001-qwen38maxkhovvob4.md
//
// 语义逐条对照 SQLite 侧既有实现（internal/store/sqlite.go:106-140 那套是两次事故换来的），
// 只保留适用 PG 的部分：
//
//	· 按内容 digest 记账、只跑一次；已应用而条目内容变了 ⇒ 拒绝（Hub 起不来），
//	  要修正就【新增一条】，不改旧的（sqlite.go:186-193 的理由原文：库里是按旧内容迁的，
//	  继续跑等于拿新账认旧事实）。
//	· 顺序 ＝ 名字字典序，【显式 sort】，不依赖 ReadDir/Glob 的排序实现细节（sqlite.go:163-164）。
//	· 每条一个事务，DDL 与账同事务提交（sqlite.go:584-600）。PG 的 DDL 是事务性的
//	  ⇒ 单条不会半迁移；这条在 TestMigrateEntries_FailedEntryLeavesNoTrace 里实测钉住。
//	· 不吞任何错。sqlite.go:118-127 记着修之前的两条实测缺陷：① 逐启动全量重放，把管理员
//	  改过的规则静默回滚、还顺带盖一个新 updated_at；② 通配吞 duplicate column，让
//	  【启动成功】冒充「schema 如预期」。本包不给这两条任何形状。
//
// 🔴 明确【不移植】的（免得各组照 SQLite 抄）：legacy 补账那一整套——对象探测
// （objAllPresent/objPartial）、纯数据迁移 verifier、DINGWEI_MIGRATION_LEGACY_ACK 人工确认。
// 那是为「账为空但库里已有业务表」的旧 SQLite 库准备的；PG 侧是绿地＋有基线快照，不存在这个状态。
// 将来若要把旧平台 SQLite 数据导进 PG，走 G2 的导入件，不由各域写 UPDATE 兜。
//
// ⚠️ 保证范围（不夸大，与 repo 包同款免责）：只保证【经由本 runner 的路径】。
// 手工 psql 改库、外部工具建表不在其内。身份域启动时另有一道 CheckSchema（缺表即拒），
// 那是【库的真实状态】的核对，与本账本互补：账本管「有没有按账迁过」，CheckSchema 管「表现在在不在」。
//
// 🔴 本 runner 【不做跨进程互斥】（写明不假装）：两个进程同时对同一个库跑 Migrate 时，
// 后到的那个会在记账时撞 schema_migrations 主键而【拒绝启动】——fail-closed，不会静默半迁移，
// 但也不会自动让路。运维顺序是「先 workpulse bootstrap-admin，再起 Hub」（两者共用同一个
// openNewPlatformStore，见 cmd/workpulse/newplat.go）。真要支持并发启动，得连「单连接持有
// pg_advisory_xact_lock」一起解决——repo.Pool 按判据二不交连接，故那不是加一行能了事的。
package migrate

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// 账本条目：G2 代管的冻结 SQL 文件（各域基线与后续增量）。
//
//go:embed migrations/*.sql
var embeddedFS embed.FS

// embeddedGlob 是内嵌条目的取法。🔴 只认 .sql：目录里放别的（README、临时备份）不会被当成迁移。
const embeddedGlob = "migrations/*.sql"

// ledgerDDL 是账本表。🔴 与 SQLite 侧【同名同形】（internal/store/sqlite.go 的 migrationLedgerDDL），
// 语义可逐条对照；只 applied_at 按 PG 换类型（TEXT → timestamptz）。
// 同名不是为了好看：两本账同名同形，运维在两种库上查同一张表得到同一种答案。
const ledgerDDL = `CREATE TABLE IF NOT EXISTS schema_migrations(
	version    text        PRIMARY KEY,
	digest     text        NOT NULL,
	applied_at timestamptz NOT NULL
)`

// Entry 是一条已登记的迁移。
//
// Name ＝ 账本主键（＝ 文件名，含 .sql 后缀）；Body ＝ 文件全文，digest 按【字节】算。
// 🔴 Name 一旦在长命库上记过账就【不可改、不可复用】：改号 ＝ 改主键 ＝ 库里那条账成了孤儿，
// 而同一段 DDL 会以新号再跑一次。长命库出现之前允许改号（口径页 §五）。
type Entry struct{ Name, Body string }

// Report 是本次 Migrate 实际做了什么。
//
// 为什么要交回它：「跑过了」与「跑过且什么都没改」是两件不同的事，合成一个数就分不清
// 【首次建库】和【重启空转】——启动日志里看不出这次到底动了库没有，等于没有日志。
type Report struct {
	Applied []string // 本次真执行的条目名（按应用顺序）
	Skipped []string // 账上已有且内容未变的条目名（按账本顺序）
}

// String 给启动日志用的一行读数。
func (r Report) String() string {
	return fmt.Sprintf("applied=%d skipped=%d", len(r.Applied), len(r.Skipped))
}

var (
	mu         sync.Mutex
	registered []Entry

	embedOnce    sync.Once
	embedEntries []Entry
	embedErr     error
)

// Register 显式登记一条迁移（不靠 init 顺序：谁装配谁调，调完再 Migrate）。
//
// 用途：某域要把自己的冻结基线／增量条目交进账本，而不经 G2 的 migrations 目录。
// 🔴 撞名即 panic —— 登记期就炸，不留到运行期两个域互相盖账。
// panic 而不是返回 error 的理由：登记发生在装配期（进程刚起、还没服务任何请求），这时炸掉
// 是最便宜的时机；等运行期才发现「两个域用了同一个号」，就要在库上收拾了。
// 🔴 与【内嵌条目】撞名不在这里判（那要读 embed，可能失败，不该用 panic 表达 I/O 错），
// 由 Ledger() 返回错误——同样是 fail-closed，只是换成错误而不是 panic。
func Register(e Entry) {
	if strings.TrimSpace(e.Name) == "" {
		panic("migrate: Register 收到没有名字的条目（账本主键不得为空）")
	}
	if strings.TrimSpace(e.Body) == "" {
		panic("migrate: Register " + e.Name + "：正文为空（空条目会被记账却不改库）")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, old := range registered {
		if old.Name == e.Name {
			panic("migrate: 条目 " + e.Name + " 被登记两次 —— 号段由 G2 分配（口径页 §五），撞号即两个域互相盖账")
		}
	}
	registered = append(registered, e)
}

// Ledger 返回将要应用的全部条目：内嵌 migrations/*.sql ∪ Register 登记的，按 Name 字典序。
//
// 顺序是【语义】的一部分（一条迁移只能依赖比它早的版本），故这里显式排；
// MigrateEntries 里还会再排一次——调用方自己拼的切片同样不许依赖给的顺序。
func Ledger() ([]Entry, error) {
	emb, err := loadEmbedded()
	if err != nil {
		return nil, err
	}
	mu.Lock()
	reg := append([]Entry(nil), registered...)
	mu.Unlock()

	out := make([]Entry, 0, len(emb)+len(reg))
	src := make(map[string]string, len(emb)+len(reg))
	for _, e := range emb {
		out = append(out, e)
		src[e.Name] = "内嵌 " + embeddedGlob
	}
	for _, e := range reg {
		if where, ok := src[e.Name]; ok {
			return nil, fmt.Errorf("migrate: 条目 %s 撞名（%s 与 Register 登记）—— 号段由 G2 分配（口径页 §五）", e.Name, where)
		}
		out = append(out, e)
		src[e.Name] = "Register 登记"
	}
	sortEntries(out)
	return out, nil
}

// loadEmbedded 读内嵌条目（只读一次；embed.FS 在进程生命期里不会变）。
func loadEmbedded() ([]Entry, error) {
	embedOnce.Do(func() {
		names, err := fs.Glob(embeddedFS, embeddedGlob)
		if err != nil {
			embedErr = fmt.Errorf("migrate: 取内嵌账本条目失败: %w", err)
			return
		}
		if len(names) == 0 {
			// 🔴 空账本不是「没事可做」，是【件没打进二进制】：go:embed 匹配不到文件时编译就失败，
			// 能走到这里说明目录被清空或改了后缀。静默返回空会让 Hub 起来却没有一张表，
			// 然后由身份域的 CheckSchema 报「缺 6 张表」——离真因差一层，故在这里就判死。
			embedErr = errors.New("migrate: 内嵌账本一条也没有（" + embeddedGlob + " 匹配为空）—— 账本件没打进二进制")
			return
		}
		embedEntries = make([]Entry, 0, len(names))
		for _, name := range names {
			body, err := embeddedFS.ReadFile(name)
			if err != nil {
				embedErr = fmt.Errorf("migrate: 读内嵌条目 %s 失败: %w", name, err)
				return
			}
			embedEntries = append(embedEntries, Entry{Name: strings.TrimPrefix(name, "migrations/"), Body: string(body)})
		}
		sortEntries(embedEntries)
	})
	return embedEntries, embedErr
}

// Migrate 把账本（Ledger()）应用到 pool，时钟用 clock.Real。
//
// 这是各域与装配点【唯一】要调的入口：Hub 与 bootstrap-admin 在 repo.OpenPG 之后、
// 建存储之前调一次（cmd/workpulse/newplat.go 的 openNewPlatformStore）。
// DDL 无租户语义 ⇒ 内部用 repo.SystemCtx()（评审面，与 internal/datad/pgstore.go:123 同型）。
func Migrate(ctx context.Context, pool *repo.Pool) (Report, error) {
	entries, err := Ledger()
	if err != nil {
		return Report{}, err
	}
	return MigrateEntries(ctx, pool, entries, clock.Real{})
}

// MigrateEntries 应用给定条目（不读全局账本）。时钟可注入 ⇒ applied_at 可被用例断言。
//
// clk 为 nil 时用 clock.Real（不给「零时刻」留后门：见下面对 now.IsZero() 的拒绝）。
// 🔴 出错时返回的 Report 反映【出错之前】已做的事，不是全空——那正是排障要的信息
// （哪条成了、卡在哪条），丢掉它等于让人去库里反推。
func MigrateEntries(ctx context.Context, pool *repo.Pool, entries []Entry, clk clock.Clock) (Report, error) {
	if pool == nil {
		return Report{}, errors.New("migrate: 缺连接池（建库只许在 internal/repo，故这里不接受库句柄）")
	}
	if clk == nil {
		clk = clock.Real{}
	}

	sorted := append([]Entry(nil), entries...)
	sortEntries(sorted)

	// 🔴 先全验、再动手：验不过就一条都不应用，不留半套表＋半套账。
	seen := make(map[string]bool, len(sorted))
	for _, e := range sorted {
		switch {
		case strings.TrimSpace(e.Name) == "":
			return Report{}, errors.New("migrate: 有条目缺名字（账本主键不得为空）")
		case !strings.HasSuffix(e.Name, ".sql"):
			return Report{}, fmt.Errorf("migrate: 条目 %s 的名字不以 .sql 结尾 —— 账本主键＝文件名（口径页 §五），"+
				"少个后缀就会与同一段 DDL 的另一个号并存，等于同一件事记两本账", e.Name)
		case strings.TrimSpace(e.Body) == "":
			return Report{}, fmt.Errorf("migrate %s: 条目正文为空 —— 空迁移会被记账却不改库，"+
				"等于把「已应用」发给了一个什么都没做的版本", e.Name)
		case seen[e.Name]:
			return Report{}, fmt.Errorf("migrate: 条目 %s 在同一批里出现两次 —— 账本主键不得重复", e.Name)
		}
		seen[e.Name] = true
	}

	// 全包唯一一处 SystemCtx：建表与迁移本就无租户语义（repo 包注释要求每处都被问一句
	// 「这个操作真的与租户无关吗」——答案是「是」，且它只用于 DDL 与账本表，不碰任何业务表）。
	t := repo.SystemCtx()
	if _, err := pool.Exec(ctx, t, ledgerDDL); err != nil {
		return Report{}, fmt.Errorf("migrate: 建迁移账失败: %w", err)
	}
	applied, err := appliedLedger(ctx, pool, t)
	if err != nil {
		return Report{}, err
	}

	var rep Report
	for _, e := range sorted {
		digest := Digest(e.Body)
		if old, ok := applied[e.Name]; ok {
			if old != digest {
				return rep, fmt.Errorf("migrate %s: 已应用的迁移内容已变更（账内 digest=%s，当前 digest=%s）—— 拒绝启动。"+
					"迁移条目应视为不可变；如确需修正，请【新增一条】", e.Name, old, digest)
			}
			rep.Skipped = append(rep.Skipped, e.Name)
			continue
		}
		if err := applyOne(ctx, pool, t, e, digest, clk); err != nil {
			return rep, err
		}
		rep.Applied = append(rep.Applied, e.Name)
	}
	return rep, nil
}

// applyOne 应用一条：DDL 与账同一个事务提交。
func applyOne(ctx context.Context, pool *repo.Pool, t repo.AccessCtx, e Entry, digest string, clk clock.Clock) error {
	now := clk.Now()
	if now.IsZero() {
		return fmt.Errorf("migrate %s: 时钟给出零时刻 —— applied_at 是「库什么时候真的应用了这条」的唯一记录，"+
			"拒绝盖零时刻（那会让审计上看不出先后）", e.Name)
	}
	return pool.InTx(ctx, t, nil, func(tx repo.Tx) error {
		// 🔴 一次 Exec 里放多条语句，靠的是【无参数】：pgx/v5@v5.7.1/conn.go:500-503
		// 「Always use simple protocol when there are no arguments」——只有简单协议允许多语句。
		// 先例＝datad 的 SchemaDDL 与 hosted 的 ApplySchema（均已实测）。
		// ⇒ 带参数的语句【不得】混进同一个 Body：拆成独立条目，或走 repo.Pool.InTx 自己写。
		if _, err := tx.Exec(ctx, e.Body); err != nil {
			return fmt.Errorf("migrate %s: %w", e.Name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations(version, digest, applied_at) VALUES($1,$2,$3)`,
			e.Name, digest, now.UTC()); err != nil {
			return fmt.Errorf("migrate %s: 记账失败: %w", e.Name, err)
		}
		return nil
	})
}

// appliedLedger 读账：version → digest。
func appliedLedger(ctx context.Context, pool *repo.Pool, t repo.AccessCtx) (map[string]string, error) {
	rows, err := pool.Query(ctx, t, `SELECT version, digest FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: 读迁移账失败: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var v, d string
		if err := rows.Scan(&v, &d); err != nil {
			return nil, fmt.Errorf("migrate: 读迁移账扫描失败: %w", err)
		}
		out[v] = d
	}
	// 🔴 rows.Err() 不查＝把「遍历中断」当成「账就这么多」，那正是「启动成功冒充 schema 如预期」的同族。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate: 读迁移账遍历失败: %w", err)
	}
	return out, nil
}

// Digest 是账本主键之外的那一半身份：正文的 sha256（hex）。与 SQLite 侧同一算法，
// 故同一段 SQL 在两本账里的 digest 相同——对照时不必换算。
func Digest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// sortEntries 显式排序（顺序是语义，不依赖任何库的排序实现细节）。
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
}
