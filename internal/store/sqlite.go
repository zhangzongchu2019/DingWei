package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/redact"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动（无 cgo）
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// SQLite 是 Repository 的 SQLite 实现。
type SQLite struct {
	db    *sql.DB
	clock clock.Clock // 可注入时钟（测试用 Fake）；默认 clock.Real
}

// SetClock 注入时钟（测试用）。未设置时默认 clock.Real{}。
func (s *SQLite) SetClock(c clock.Clock) {
	if c != nil {
		s.clock = c
	}
}

func (s *SQLite) now() time.Time {
	if s.clock == nil {
		return time.Now()
	}
	return s.clock.Now()
}

const messageProcessingLease = 5 * time.Minute

// OpenSQLite 打开（或创建）SQLite 主库，启用 WAL + busy_timeout（§13.2）。
func OpenSQLite(path string) (*SQLite, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单写者纪律：写连接数限 1（§13.1），读用 WAL 并发。
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Close() error { return s.db.Close() }

// Ping verifies that SQLite can acquire a connection and answer within the
// caller's deadline. It is used by the lock-aware liveness endpoint.
func (s *SQLite) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Migrate 按文件名顺序执行内嵌迁移。
func (s *SQLite) Migrate(ctx context.Context) error {
	if err := s.runMigrations(ctx); err != nil {
		return err
	}
	if err := s.ensureSessionEndpointColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureBotChannelColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureSeenPersonTable(ctx); err != nil {
		return err
	}
	if err := s.ensureMemberColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureProjectColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureProjectAggregateSourceTable(ctx); err != nil {
		return err
	}
	if err := s.ensureProjectWeeklyReportTable(ctx); err != nil {
		return err
	}
	if err := s.ensureControlPlaneTables(ctx); err != nil {
		return err
	}
	return s.ensureDefaultProject(ctx)
}

// ---------------------------------------------------------------------------
// 迁移账（SA-13，乙域 terra 提出，本席实测复现后按其修法落地）
//
// 🔴 修之前的实际行为，两条，都不是理论风险：
//
//	① **逐启动全量重放**：Migrate() 每次把 migrations/ 下全部文件重跑一遍，没有任何账。
//	   于是一次性的数据迁移语句变成了【每次重启执行一次】。实测复现：
//	   0025 的 `UPDATE l1_decision_rule SET pattern=... WHERE id='l1_command_roster'`
//	   是无条件覆盖，管理员在后台改过这条规则后，**重启一次就被静默回滚**，
//	   且顺带盖上一个新的 updated_at —— 审计上看是「有人改过」，而没有任何人改过。
//	   同族的还有 0017 的回填 UPDATE、0005 的三条 DELETE（会删掉后来重建的同名服务）。
//	② **通配吞 duplicate column**：整块执行失败后逐语句重试，凡报 "duplicate column name"
//	   一律 continue。于是「库里已有同名但【不等价】的列」会被当成迁移已应用，
//	   而进程**启动成功** —— 授权类的 ADD COLUMN 没落成预期语义，上层却照常跑。
//	   🔴 启动成功被当成了「schema 如预期」。
//
// ⇒ 现在：每个迁移按内容 digest 记账，只跑一次；已应用而内容变了 ⇒ 拒绝启动；
//
//	不再吞任何错误。
//
// ⚠️ 保证范围（不夸大）：本账保证的是【经由本 runner 的路径】。
//
//	手工 psql/sqlite3 改库、外部工具建表不在其内 —— 与 repo 包同款免责。
const migrationLedgerDDL = `CREATE TABLE IF NOT EXISTS schema_migrations(
	version    TEXT PRIMARY KEY,
	digest     TEXT NOT NULL,
	applied_at TEXT NOT NULL
)`

// migrationObjState 是 legacy 库里某个迁移的「对象是否已存在」的判定结果。
type migrationObjState int

const (
	objAllMissing   migrationObjState = iota // 一个都不在 ⇒ 该迁移确实没跑过，正常执行
	objAllPresent                            // 全在 ⇒ 已跑过，补账、不执行
	objPartial                               // 一半一半 ⇒ 🔴 半迁移，拒绝启动
	objUnverifiable                          // 纯数据迁移，无 DDL 对象可查
)

func (s *SQLite) runMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, migrationLedgerDDL); err != nil {
		return fmt.Errorf("migrate: 建迁移账失败: %w", err)
	}

	applied, err := s.appliedMigrations(ctx)
	if err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// 不依赖 ReadDir 的排序实现细节：迁移顺序是语义的一部分，显式排。
	sort.Strings(names)

	// legacy 库判定：账是空的，但库里已经有业务表 ⇒ 这是一个在跑的旧库，
	// 它的迁移【早就应用过】，只是没有账。此时绝不能重跑数据语句（那正是 ① 的伤害）。
	legacy := false
	if len(applied) == 0 {
		has, err := s.hasUserTables(ctx)
		if err != nil {
			return err
		}
		legacy = has
	}
	backfilled := 0

	for _, name := range names {
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])

		if old, ok := applied[name]; ok {
			if old != digest {
				// 已应用的迁移，文件内容后来被改过。改了什么无从得知，
				// 库里是按【旧内容】迁的 —— 继续跑等于拿新账认旧事实。
				return fmt.Errorf(
					"migrate %s: 已应用的迁移内容已变更（账内 digest=%s，当前文件 digest=%s）—— 拒绝启动。"+
						"迁移文件应视为不可变；如确需修正，请新增一个迁移", name, old, digest)
			}
			continue
		}
		if name == "0044_console_spec_workflow.sql" {
			oldSchema, newSchema, err := s.consoleSpecWorkflowSchema(ctx)
			if err != nil {
				return err
			}
			if newSchema {
				if err := s.preflightConsolePrincipalDisplayNames(ctx); err != nil {
					return err
				}
				if err := s.recordMigration(ctx, name, digest); err != nil {
					return err
				}
				continue
			}
			if oldSchema {
				if err := s.applyConsoleSpecWorkflowMigration(ctx, name, string(body), digest); err != nil {
					return err
				}
				legacy = false
				continue
			}
		}
		if name == "0050_console_spec_actor_and_derive.sql" {
			state, err := s.consoleSpecActorSchema(ctx)
			if err != nil {
				return err
			}
			if state == objAllPresent {
				if err := s.recordMigration(ctx, name, digest); err != nil {
					return err
				}
				continue
			}
			if state == objPartial {
				return fmt.Errorf("migrate %s: actor/derive schema is partially present", name)
			}
			if err := s.applyForeignKeyDisabledMigration(ctx, name, string(body), digest); err != nil {
				return err
			}
			legacy = false
			continue
		}

		if legacy {
			state, err := s.migrationObjState(ctx, string(body))
			if err != nil {
				return fmt.Errorf("migrate %s: legacy 核验失败: %w", name, err)
			}
			switch state {
			case objAllPresent:
				// 受控 legacy 补账：核验过该迁移的 DDL 对象确实都在。
				// 🔴 只写账，不执行 —— 执行才是伤害本身。
				if err := s.recordMigration(ctx, name, digest); err != nil {
					return err
				}
				backfilled++
				continue
			case objUnverifiable:
				// 🔴 terra SA-13 二轮：一轮把「纯数据迁移」直接当作已应用补账 ——
				//   那是拿【无法核验】当【核验通过】。0025 是纯 UPDATE，一个账为空、
				//   且从未执行过它的旧库，会被标成已应用：既不重放、也不拒启动，
				//   于是该迁移的效果永远不会落地，而启动照样成功。
				//   ⇒ 纯数据迁移必须逐版本有 verifier；没有就拒启动，不得自动补账。
				ok, err := s.verifyDataMigration(ctx, name)
				if err != nil {
					return fmt.Errorf("migrate %s: 纯数据迁移核验失败: %w", name, err)
				}
				if ok {
					if err := s.recordMigration(ctx, name, digest); err != nil {
						return err
					}
					backfilled++
					continue
				}
				return fmt.Errorf(
					"migrate %s: 这是【纯数据迁移】，legacy 库无法自动判定它是否已执行 —— 拒绝启动。\n"+
						"  · 若已登记 verifier 而核验为「未生效」：重放数据语句可能覆盖人工改动，故不自动执行；\n"+
						"  · 若未登记 verifier：本迁移无稳定效果签名，机器判不了。\n"+
						"  ⇒ 人工核验实际数据后，用受控确认放行：%s=%s（可逗号分隔多个版本）",
					name, legacyAckEnv, name)
			case objPartial:
				return fmt.Errorf(
					"migrate %s: legacy 库处于【半迁移】状态（该迁移的对象一部分已存在、一部分缺失）—— 拒绝启动。"+
						"这正是旧实现吞 duplicate column 会掩盖的情形，须人工核验实际 schema 后再补账", name)
			}
			// objAllMissing 落到下面正常执行
		}

		if err := s.applyMigration(ctx, name, string(body), digest); err != nil {
			return err
		}
		// 🔴 一旦本轮【真执行】过一个迁移，其后的版本必然也没被应用过 ——
		//    迁移是有序的，version N 缺失就意味着 N 之后的都缺失。
		//    ⇒ 此后不再走 legacy 判定，纯数据迁移照常执行，不去要什么人工确认。
		//    （否则一个只 seed 了半张旧表的库，会在第一个纯数据迁移上被拒，
		//      而那个迁移显然从未跑过 —— 把「判不出」错用到了「明确没跑过」的情形上。）
		legacy = false
	}

	if backfilled > 0 {
		fmt.Fprintf(os.Stderr, "store: legacy 迁移补账 %d 件（已核验对象存在，未重放语句）\n", backfilled)
	}
	return nil
}

func (s *SQLite) consoleSpecActorSchema(ctx context.Context) (migrationObjState, error) {
	hasColumn := func(table, want string) (bool, error) {
		rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var cid, notNull, pk int
			var name, typ string
			var def sql.NullString
			if err := rows.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
				return false, err
			}
			if name == want {
				return true, nil
			}
		}
		return false, rows.Err()
	}
	actor, err := hasColumn("console_spec_revision", "created_by_kind")
	if err != nil {
		return objUnverifiable, err
	}
	derived, err := hasColumn("console_spec", "derived_from")
	if err != nil {
		return objUnverifiable, err
	}
	if actor && derived {
		return objAllPresent, nil
	}
	if actor || derived {
		return objPartial, nil
	}
	return objAllMissing, nil
}

func (s *SQLite) applyForeignKeyDisabledMigration(ctx context.Context, name, body, digest string) (retErr error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate %s: acquire dedicated connection: %w", name, err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return fmt.Errorf("migrate %s: disable foreign keys: %w", name, err)
	}
	defer func() {
		if restoreErr := restoreConsoleForeignKeys(context.Background(), conn, name); restoreErr != nil {
			retErr = errors.Join(retErr, restoreErr)
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate %s: open transaction: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,digest,applied_at) VALUES(?,?,?)`, name, digest, s.now()); err != nil {
		return fmt.Errorf("migrate %s: record ledger: %w", name, err)
	}
	if violations, checkErr := consoleForeignKeyViolations(ctx, tx); checkErr != nil {
		return fmt.Errorf("migrate %s: pre-commit foreign_key_check: %w", name, checkErr)
	} else if len(violations) != 0 {
		return fmt.Errorf("migrate %s: pre-commit foreign_key_check failed: %s", name, strings.Join(violations, "; "))
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if violations, checkErr := consoleForeignKeyViolations(ctx, conn); checkErr != nil {
		return fmt.Errorf("migrate %s: post-commit foreign_key_check: %w", name, checkErr)
	} else if len(violations) != 0 {
		return fmt.Errorf("migrate %s: post-commit foreign_key_check failed: %s", name, strings.Join(violations, "; "))
	}
	return nil
}

func (s *SQLite) preflightConsolePrincipalDisplayNames(ctx context.Context) error {
	return preflightConsolePrincipalDisplayNames(ctx, s.db)
}

type consoleDisplayNameQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func preflightConsolePrincipalDisplayNames(ctx context.Context, q consoleDisplayNameQuerier) error {
	// Deliberately inspect every row, including inactive identities. SQLite trim()
	// only strips ASCII space by default, so list Tab/LF/CR explicitly as well.
	rows, err := q.QueryContext(ctx, `SELECT COALESCE(display_name,''),group_concat(id),COUNT(*) FROM console_principal GROUP BY display_name HAVING trim(COALESCE(display_name,''), char(32)||char(9)||char(10)||char(13))='' OR COUNT(*)>1 ORDER BY display_name`)
	if err != nil {
		return fmt.Errorf("migrate 0044: check principal short names: %w", err)
	}
	defer rows.Close()
	var conflicts []string
	for rows.Next() {
		var name, ids string
		var count int
		if err := rows.Scan(&name, &ids, &count); err != nil {
			return err
		}
		if name == "" {
			name = "<empty>"
		} else if strings.Trim(name, " \t\n\r") == "" {
			name = "<blank-whitespace>"
		}
		conflicts = append(conflicts, fmt.Sprintf("%s(count=%d ids=%s)", name, count, ids))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("migrate 0044: console_principal display_name conflicts; owner cleanup required; no names changed: %s", strings.Join(conflicts, "; "))
	}
	return nil
}

type consoleDisplayNameRewrite struct {
	ID, OldName, NewName string
}

func rewriteLegacyOwnerDisplayNames(ctx context.Context, tx *sql.Tx) ([]consoleDisplayNameRewrite, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.id,r.owner_key FROM console_principal p
		JOIN console_principal_role r ON r.principal_id=p.id AND r.role='owner' AND r.active=1
		WHERE p.display_name='Owner' ORDER BY r.owner_key,p.id`)
	if err != nil {
		return nil, fmt.Errorf("migrate 0044: resolve legacy Owner names: %w", err)
	}
	defer rows.Close()
	owners := make(map[string]string)
	var orderedIDs []string
	for rows.Next() {
		var id, ownerKey string
		if err := rows.Scan(&id, &ownerKey); err != nil {
			return nil, err
		}
		if strings.TrimSpace(ownerKey) == "" {
			return nil, fmt.Errorf("migrate 0044: legacy display_name Owner principal %s has empty owner_key", id)
		}
		if prior, exists := owners[id]; exists && prior != ownerKey {
			return nil, fmt.Errorf("migrate 0044: legacy display_name Owner principal %s has ambiguous owner_keys %q and %q", id, prior, ownerKey)
		}
		if _, exists := owners[id]; !exists {
			orderedIDs = append(orderedIDs, id)
		}
		owners[id] = ownerKey
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_principal WHERE display_name='Owner'`).Scan(&total); err != nil {
		return nil, err
	}
	if total != len(orderedIDs) {
		return nil, fmt.Errorf("migrate 0044: %d legacy display_name Owner rows but only %d have one active owner_key; refusing silent rename", total, len(orderedIDs))
	}
	counts := make(map[string]int)
	rewrites := make([]consoleDisplayNameRewrite, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		ownerKey := owners[id]
		counts[ownerKey]++
		newName := ownerKey
		if counts[ownerKey] > 1 {
			newName = fmt.Sprintf("%s-%d", ownerKey, counts[ownerKey])
		}
		if _, err := tx.ExecContext(ctx, `UPDATE console_principal SET display_name=? WHERE id=? AND display_name='Owner'`, newName, id); err != nil {
			return nil, err
		}
		rewrites = append(rewrites, consoleDisplayNameRewrite{ID: id, OldName: "Owner", NewName: newName})
	}
	return rewrites, nil
}

func (s *SQLite) applyConsoleSpecWorkflowMigration(ctx context.Context, name, body, digest string) (retErr error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migrate %s: acquire dedicated connection: %w", name, err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return fmt.Errorf("migrate %s: disable foreign keys: %w", name, err)
	}
	defer func() {
		if restoreErr := restoreConsoleForeignKeys(context.Background(), conn, name); restoreErr != nil {
			retErr = errors.Join(retErr, restoreErr)
		}
	}()
	var enabled int
	if err = conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 0 {
		return fmt.Errorf("migrate %s: foreign_keys did not turn off (value=%d err=%v)", name, enabled, err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate %s: open transaction: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	rewrites, err := rewriteLegacyOwnerDisplayNames(ctx, tx)
	if err != nil {
		return err
	}
	if err = preflightConsolePrincipalDisplayNames(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,digest,applied_at) VALUES(?,?,?)`, name, digest, s.now()); err != nil {
		return fmt.Errorf("migrate %s: record ledger: %w", name, err)
	}
	if violations, checkErr := consoleForeignKeyViolations(ctx, tx); checkErr != nil {
		return fmt.Errorf("migrate %s: pre-commit foreign_key_check: %w", name, checkErr)
	} else if len(violations) != 0 {
		return fmt.Errorf("migrate %s: pre-commit foreign_key_check failed: %s", name, strings.Join(violations, "; "))
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if violations, checkErr := consoleForeignKeyViolations(ctx, conn); checkErr != nil {
		return fmt.Errorf("migrate %s: post-commit foreign_key_check: %w", name, checkErr)
	} else if len(violations) != 0 {
		return fmt.Errorf("migrate %s: post-commit foreign_key_check failed: %s", name, strings.Join(violations, "; "))
	}
	for _, rewrite := range rewrites {
		fmt.Fprintf(os.Stderr, "migrate 0044: principal display_name rewritten id=%s old=%q new=%q\n", rewrite.ID, rewrite.OldName, rewrite.NewName)
	}
	return nil
}

func restoreConsoleForeignKeys(ctx context.Context, conn *sql.Conn, name string) error {
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		return fmt.Errorf("migrate %s: restore foreign keys: %w", name, err)
	}
	var enabled int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
		return fmt.Errorf("migrate %s: foreign_keys did not restore (value=%d err=%v)", name, enabled, err)
	}
	return nil
}

func consoleForeignKeyViolations(ctx context.Context, q consoleDisplayNameQuerier) ([]string, error) {
	rows, err := q.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []string
	for rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var fkID int
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			return nil, err
		}
		violations = append(violations, fmt.Sprintf("table=%s rowid=%v parent=%s fkid=%d", table, rowID, parent, fkID))
	}
	return violations, rows.Err()
}

func (s *SQLite) consoleSpecWorkflowSchema(ctx context.Context) (oldSchema, newSchema bool, err error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(console_spec)`)
	if err != nil {
		return false, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var def sql.NullString
		if err = rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
			return
		}
		if name == "claimed_by" {
			oldSchema = true
		}
		if name == "current_editor_id" {
			newSchema = true
		}
	}
	return oldSchema, newSchema, rows.Err()
}

// applyMigration 在一个事务里执行迁移并记账：要么都成，要么都不成。
// 🔴 不再吞任何错误 —— duplicate column 现在就是失败。
func (s *SQLite) applyMigration(ctx context.Context, name, body, digest string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate %s: 开事务失败: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, digest, applied_at) VALUES(?,?,?)`,
		name, digest, s.now()); err != nil {
		return fmt.Errorf("migrate %s: 记账失败: %w", name, err)
	}
	return tx.Commit()
}

// legacyAckEnv 是【受控人工确认】的入口：运维在部署件里显式列出已人工核验过的
// 纯数据迁移版本。🔴 刻意做成环境变量而非代码常量——它属于部署侧声明（架构 A§8.2：
// 环境属性由部署件声明、服务端取得），且每次使用都留在部署配置里可被审。
const legacyAckEnv = "DINGWEI_MIGRATION_LEGACY_ACK"

// dataMigrationVerifier 判定某个纯数据迁移的【效果】是否已经在库里。
//
// 🔴 判据必须是**稳定效果签名**：即「这个迁移干过一次」留下的、
//
//	不会被正常运维改动抹掉的痕迹。若一个迁移没有这样的签名（例如它只是把某个
//	可编辑字段设成某值——管理员随时可以改），**就不要硬凑一个 verifier**，
//	让它落到「拒启动 + 人工确认」那条路上。硬凑出来的 verifier 会把
//	「管理员改过」误判成「迁移没跑过」，那比没有 verifier 更坏。
type dataMigrationVerifier func(ctx context.Context, db *sql.DB) (bool, error)

var dataMigrationVerifiers = map[string]dataMigrationVerifier{
	// 0024 把旧 owner key 全量改名为 systemtaskintl。稳定签名：**旧 key 已不存在**。
	// 正常运维不会把 'system-v-task-internal' 重新写回去，故该签名稳定。
	"0024_session_naming_b.sql": func(ctx context.Context, db *sql.DB) (bool, error) {
		const old = "system-v-task-internal"
		for _, q := range []string{
			`SELECT COUNT(*) FROM service_api_key WHERE service_id=?`,
			`SELECT COUNT(*) FROM api_key_account WHERE chat_entity_id=?`,
			`SELECT COUNT(*) FROM session_endpoint WHERE owner_key=?`,
			`SELECT COUNT(*) FROM member WHERE owner_key=?`,
			`SELECT COUNT(*) FROM registered_service WHERE id=?`,
		} {
			var n int
			if err := db.QueryRowContext(ctx, q, old).Scan(&n); err != nil {
				return false, err
			}
			if n > 0 {
				return false, nil
			}
		}
		return true, nil
	},
	// 0052 retires the exact historical display name. Its stable signature is
	// the absence of that name; runtime compatibility is provided separately by
	// Hub's read alias and does not require restoring it in bot_channel.
	"0052_bot_channel_display_name.sql": func(ctx context.Context, db *sql.DB) (bool, error) {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bot_channel WHERE name='GreyTestRobot'`).Scan(&n); err != nil {
			return false, err
		}
		return n == 0, nil
	},

	// 🔴 0025_owner_links_page.sql 【故意不登记】：它只是把 l1_command_roster 的 pattern
	//    设成某个值，而那个字段管理员随时可改 —— 没有稳定效果签名。
	//    任何“verifier”都会把「管理员改过」读成「迁移没跑过」。
	//    ⇒ 让它走拒启动 + DINGWEI_MIGRATION_LEGACY_ACK 人工确认，这是诚实的那条路。
}

// verifyDataMigration 先看受控人工确认，再看 verifier。
func (s *SQLite) verifyDataMigration(ctx context.Context, name string) (bool, error) {
	for _, v := range strings.Split(os.Getenv(legacyAckEnv), ",") {
		if strings.TrimSpace(v) == name {
			fmt.Fprintf(os.Stderr, "store: 纯数据迁移 %s 由 %s 人工确认放行（仅补账，不执行）\n", name, legacyAckEnv)
			return true, nil
		}
	}
	vf, ok := dataMigrationVerifiers[name]
	if !ok {
		return false, nil // 无 verifier ⇒ 交给调用方拒启动
	}
	return vf(ctx, s.db)
}

func (s *SQLite) recordMigration(ctx context.Context, name, digest string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, digest, applied_at) VALUES(?,?,?)`,
		name, digest, s.now()); err != nil {
		return fmt.Errorf("migrate %s: 补账失败: %w", name, err)
	}
	return nil
}

func (s *SQLite) appliedMigrations(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT version, digest FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: 读迁移账失败: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var v, d string
		if err := rows.Scan(&v, &d); err != nil {
			return nil, err
		}
		out[v] = d
	}
	return out, rows.Err()
}

// hasUserTables 判断库里除迁移账与 sqlite 内部表之外，是否已有业务表。
func (s *SQLite) hasUserTables(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master
		  WHERE type='table' AND name<>'schema_migrations' AND name NOT LIKE 'sqlite_%'`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("migrate: 探测既有表失败: %w", err)
	}
	return n > 0, nil
}

// 迁移语料里【实际出现过】的三种 DDL 形态（已在 review-r1 锚上全量普查：
// CREATE TABLE 36 · ALTER TABLE ... ADD 27 · CREATE [UNIQUE] INDEX 16，
// CREATE 全部带 IF NOT EXISTS，ALTER 全部是 ADD）。
// ⚠️ 这是正则，不是 SQL 解析器；它的作用域【只在 legacy 补账这一条路径上】，
// 且判不出来时走 objPartial/objAllMissing 两条保守分支，不会静默放行。
var (
	reCreateTable = regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["'` + "`" + `]?(\w+)`)
	reCreateIndex = regexp.MustCompile(`(?i)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?["'` + "`" + `]?(\w+)`)
	reAlterAdd    = regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+["'` + "`" + `]?(\w+)["'` + "`" + `]?\s+ADD\s+(?:COLUMN\s+)?["'` + "`" + `]?(\w+)`)
)

// reData 判「本迁移是否含数据语句」。词边界必须有：`updated_at` 以 "update" 开头，
// 没有 \b 会把一堆列定义误判成 UPDATE 语句（本席写这条时先踩了一次）。
var reData = regexp.MustCompile(`(?im)^\s*(INSERT|UPDATE|DELETE)\b`)

// migrationObjState 核验某个迁移声明的 DDL 对象在当前库里是否已存在。
//
// 🔴 判据按【幂等性】分两类，不能一视同仁：
//
//	· ALTER TABLE ... ADD COLUMN —— **非幂等**，重跑即报错。故它是权威信号：
//	  全在 ⇒ 该迁移跑过；全不在 ⇒ 没跑过；一半一半 ⇒ 真半迁移，拒绝启动。
//	· CREATE TABLE/INDEX IF NOT EXISTS —— **幂等**，重跑无害。故「一部分表还没有」
//	  只说明这个旧库年代更早，不构成半迁移。⚠️ 本仓既有的 legacy 升级用例
//	  （只 seed 了一张旧 bot_channel 的库）走的正是这条，一律判 fail 会误伤。
//	· 数据语句（INSERT/UPDATE/DELETE）—— **重跑就是伤害本身**（SA-13 ①）。
//	  故只要 DDL 显示该迁移跑过，就只补账、不执行。
func (s *SQLite) migrationObjState(ctx context.Context, body string) (migrationObjState, error) {
	tally := func(present, missing int) migrationObjState {
		switch {
		case present == 0 && missing == 0:
			return objUnverifiable
		case missing == 0:
			return objAllPresent
		case present == 0:
			return objAllMissing
		default:
			return objPartial
		}
	}

	// ① 非幂等的 ALTER ADD COLUMN —— 权威信号
	aPresent, aMissing := 0, 0
	for _, m := range reAlterAdd.FindAllStringSubmatch(body, -1) {
		ok, err := s.columnExists(ctx, m[1], m[2])
		if err != nil {
			return objPartial, err
		}
		if ok {
			aPresent++
		} else {
			aMissing++
		}
	}
	if st := tally(aPresent, aMissing); st != objUnverifiable {
		return st, nil
	}

	// ② 幂等的 CREATE —— 只用来判「跑没跑过」，mixed 不算半迁移
	cPresent, cMissing := 0, 0
	for _, m := range reCreateTable.FindAllStringSubmatch(body, -1) {
		ok, err := s.objectExists(ctx, "table", m[1])
		if err != nil {
			return objPartial, err
		}
		if ok {
			cPresent++
		} else {
			cMissing++
		}
	}
	for _, m := range reCreateIndex.FindAllStringSubmatch(body, -1) {
		ok, err := s.objectExists(ctx, "index", m[1])
		if err != nil {
			return objPartial, err
		}
		if ok {
			cPresent++
		} else {
			cMissing++
		}
	}
	switch tally(cPresent, cMissing) {
	case objAllPresent:
		return objAllPresent, nil
	case objAllMissing:
		return objAllMissing, nil
	case objPartial:
		// CREATE 是幂等的，重跑安全 —— 但若本迁移还带数据语句，重跑就会重放它们。
		if reData.MatchString(body) {
			return objPartial, nil
		}
		return objAllMissing, nil // 纯 DDL：照常执行，IF NOT EXISTS 兜底
	}

	// ③ 无任何 DDL 对象 ⇒ 纯数据迁移，legacy 库一律视为已应用，只补账
	return objUnverifiable, nil
}

func (s *SQLite) objectExists(ctx context.Context, kind, name string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type=? AND name=?`, kind, name).Scan(&n)
	return n > 0, err
}

func (s *SQLite) columnExists(ctx context.Context, table, col string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, col) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *SQLite) ensureBotChannelColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(bot_channel)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if !cols["app_secret_enc"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE bot_channel ADD COLUMN app_secret_enc TEXT`); err != nil {
			return err
		}
	}
	if !cols["verification_token"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE bot_channel ADD COLUMN verification_token TEXT`); err != nil {
			return err
		}
	}
	if !cols["encrypt_key"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE bot_channel ADD COLUMN encrypt_key TEXT`); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *SQLite) ensureSeenPersonTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seen_person (
  open_id        TEXT NOT NULL,
  bot_channel_id TEXT NOT NULL,
  name           TEXT,
  source         TEXT NOT NULL,
  last_seen_at   TEXT,
  PRIMARY KEY(open_id, bot_channel_id)
)`)
	return err
}

func (s *SQLite) ensureSessionEndpointColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(session_endpoint)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["mirror_enabled"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN mirror_enabled INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !cols["mirror_to"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN mirror_to TEXT`); err != nil {
			return err
		}
	}
	if !cols["client_ip"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN client_ip TEXT`); err != nil {
			return err
		}
	}
	if !cols["tool"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN tool TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["model"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN model TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["version"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN version TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["full_session_name"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN full_session_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["owner_key"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN owner_key TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["producer"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN producer INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !cols["target_group"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN target_group TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["no_directory"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN no_directory INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !cols["no_directory_admin"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN no_directory_admin INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !cols["no_directory_reported"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN no_directory_reported INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE session_endpoint SET no_directory_reported=no_directory WHERE no_directory=1`); err != nil {
			return err
		}
	}
	if !cols["conn_seq"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE session_endpoint ADD COLUMN conn_seq INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS ux_session_endpoint_owner_active_name
ON session_endpoint(owner_key, session_name)
WHERE active=1 AND owner_key <> ''`); err != nil {
		return err
	}
	return nil
}

func (s *SQLite) ensureMemberColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(member)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["dm_optout"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE member ADD COLUMN dm_optout INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) ensureProjectColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(project)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["parent_id"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE project ADD COLUMN parent_id TEXT NOT NULL DEFAULT 'proj:default'`); err != nil {
			return err
		}
	}
	if !cols["owner_key"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE project ADD COLUMN owner_key TEXT`); err != nil {
			return err
		}
	}
	if !cols["product_manager_key"] {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE project ADD COLUMN product_manager_key TEXT`); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_project_parent ON project(parent_id)`)
	return err
}

func (s *SQLite) ensureProjectAggregateSourceTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS project_aggregate_source (
  aggregate_project_id TEXT NOT NULL,
  source_project_id    TEXT NOT NULL,
  created_at           TEXT,
  PRIMARY KEY(aggregate_project_id, source_project_id)
)`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_project_aggregate_source_source ON project_aggregate_source(source_project_id)`)
	return err
}

func (s *SQLite) ensureProjectWeeklyReportTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS project_weekly_report (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  week       TEXT NOT NULL,
  content    TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'final',
  approved_at TEXT,
  vetoed_at TEXT,
  published_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT,
  UNIQUE(project_id, week)
)`)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(project_weekly_report)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, stmt := range []struct {
		col string
		sql string
	}{
		{"status", `ALTER TABLE project_weekly_report ADD COLUMN status TEXT NOT NULL DEFAULT 'final'`},
		{"approved_at", `ALTER TABLE project_weekly_report ADD COLUMN approved_at TEXT`},
		{"vetoed_at", `ALTER TABLE project_weekly_report ADD COLUMN vetoed_at TEXT`},
		{"published_at", `ALTER TABLE project_weekly_report ADD COLUMN published_at TEXT`},
		{"updated_at", `ALTER TABLE project_weekly_report ADD COLUMN updated_at TEXT`},
	} {
		if !cols[stmt.col] {
			if _, err := s.db.ExecContext(ctx, stmt.sql); err != nil {
				return err
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE project_weekly_report SET status='final' WHERE status IS NULL OR status=''`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE project_weekly_report SET updated_at=created_at WHERE updated_at IS NULL OR updated_at=''`); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_project_weekly_report_project_week ON project_weekly_report(project_id, week)`)
	return err
}

func (s *SQLite) ensureControlPlaneTables(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS control_task (
  id             TEXT PRIMARY KEY,
  parent_id      TEXT,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  source         TEXT NOT NULL,
  source_addr    TEXT NOT NULL,
  owner_key      TEXT NOT NULL,
  bot_channel_id TEXT,
  raw_input      TEXT NOT NULL,
  intent         TEXT,
  layer          TEXT,
  target         TEXT,
  result         TEXT,
  status         TEXT NOT NULL,
  priority       INTEGER NOT NULL DEFAULT 0,
  attempts       INTEGER NOT NULL DEFAULT 0,
  max_attempts   INTEGER NOT NULL DEFAULT 3,
  error          TEXT,
  lease_owner    TEXT,
  lease_until    TEXT,
  expire_at      TEXT
)`,
		`CREATE INDEX IF NOT EXISTS idx_ct_status_prio ON control_task(status, priority DESC, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_ct_parent ON control_task(parent_id)`,
		`CREATE TABLE IF NOT EXISTS l1_decision_rule (
  id          TEXT PRIMARY KEY,
  seq         INTEGER NOT NULL UNIQUE,
  match_type  TEXT NOT NULL,
  pattern     TEXT NOT NULL,
  intent      TEXT NOT NULL,
  action      TEXT NOT NULL,
  exit_queue  INTEGER NOT NULL DEFAULT 0,
  enabled     INTEGER NOT NULL DEFAULT 1,
  description TEXT,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS idx_l1_decision_rule_enabled_seq ON l1_decision_rule(enabled, seq)`,
		`CREATE TABLE IF NOT EXISTS control_task_l2_metric (
  task_id     TEXT PRIMARY KEY,
  duration_ms INTEGER NOT NULL,
  success     INTEGER NOT NULL,
  error       TEXT,
  created_at  TEXT NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS idx_ctl2_metric_created ON control_task_l2_metric(created_at)`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return s.seedL1DecisionRules(ctx)
}

func (s *SQLite) seedL1DecisionRules(ctx context.Context) error {
	now := s.now().UTC().Format(time.RFC3339)
	rules := []model.L1DecisionRule{
		{ID: "l1_command_terminal_input", Seq: 1, MatchType: "prefix_any", Pattern: "#unlock |#lock", Intent: "command.unlock", Action: "grantTerminalInput/revoke", ExitQueue: true, Enabled: true, Description: "Terminal input unlock/lock commands"},
		{ID: "l1_command_roster", Seq: 2, MatchType: "prefix_any", Pattern: "#在线|#roster|#清单", Intent: "command.roster", Action: "onlineDirectory", ExitQueue: true, Enabled: true, Description: "Online roster command"},
		{ID: "l1_command_apply_key", Seq: 3, MatchType: "prefix", Pattern: "#申请 ", Intent: "command.apply_key", Action: "applyKeyFlow", ExitQueue: true, Enabled: true, Description: "Session key application command"},
		{ID: "l1_command_mirror", Seq: 4, MatchType: "prefix_any", Pattern: "#mirror on|#mirror off|mirror on|mirror off", Intent: "command.mirror", Action: "setMirror", ExitQueue: true, Enabled: true, Description: "Mirror on/off command"},
		{ID: "l1_route_session", Seq: 5, MatchType: "regex", Pattern: "^#[^[:space:]]+[[:space:]]+.+", Intent: "route.session", Action: "routeToSession", ExitQueue: true, Enabled: true, Description: "Same-owner #session routing"},
		{ID: "l1_route_cross", Seq: 6, MatchType: "regex", Pattern: "^@[^[:space:]#]+#[^[:space:]]+[[:space:]]+.+", Intent: "route.cross", Action: "routeToSession", ExitQueue: true, Enabled: true, Description: "Cross-member @member#session routing"},
		{ID: "l1_route_default_single", Seq: 7, MatchType: "default", Pattern: "personal_single_online_session", Intent: "route.default", Action: "routeToOnlySession", ExitQueue: true, Enabled: true, Description: "DM without prefix and one online session"},
		{ID: "l1_nl_dispatch", Seq: 8, MatchType: "default", Pattern: "personal_multiple_online_sessions", Intent: "nl_dispatch", Action: "handoffL2", ExitQueue: false, Enabled: true, Description: "DM without prefix and multiple online sessions"},
		{ID: "l1_decompose", Seq: 9, MatchType: "keyword_any", Pattern: "让团队|大家|所有人", Intent: "decompose", Action: "handoffL2", ExitQueue: false, Enabled: true, Description: "Multi-target team semantics"},
		{ID: "l1_unknown", Seq: 10, MatchType: "fallback", Pattern: "*", Intent: "unknown", Action: "handoffL2", ExitQueue: false, Enabled: true, Description: "Fallback to L2 clarification"},
	}
	for _, rule := range rules {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO l1_decision_rule(id, seq, match_type, pattern, intent, action, exit_queue, enabled, description, created_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			rule.ID, rule.Seq, rule.MatchType, rule.Pattern, rule.Intent, rule.Action, boolInt(rule.ExitQueue), boolInt(rule.Enabled), rule.Description, now, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) ensureDefaultProject(ctx context.Context) error {
	notifyChat := os.Getenv("WP_SCHEDULE_NOTIFY_CHAT")
	notifyBot := os.Getenv("WP_SCHEDULE_NOTIFY_BOT")
	if notifyBot == "" {
		notifyBot = os.Getenv("FEISHU_BOT_CHANNEL_ID")
	}
	if notifyBot == "" {
		notifyBot = "unifiedrobot"
	}
	if err := s.UpsertProject(ctx, model.Project{
		ID:           "proj:default",
		Name:         "Default Project",
		ParentID:     "",
		NotifyChatID: notifyChat,
		NotifyBotID:  notifyBot,
		Active:       true,
	}); err != nil {
		return err
	}
	return s.ensureSystemProducerIdentity(ctx)
}

const systemProducerOwner = "systemtaskintl"

func (s *SQLite) ensureSystemProducerIdentity(ctx context.Context) error {
	if err := s.UpsertMember(ctx, model.Member{
		OwnerKey:     systemProducerOwner,
		DisplayName:  "SYSTEM-V-TASK-INTERNAL",
		FeishuOpenID: "",
		Role:         model.RoleSystem,
		Active:       true,
	}); err != nil {
		return err
	}
	if err := s.UpsertRegisteredService(ctx, model.RegisteredService{
		ID:           systemProducerOwner,
		Name:         "SYSTEM-V-TASK-INTERNAL",
		Description:  "Virtual system task producer for group-only outputs",
		DeliveryType: "session",
		ReplyMode:    "none",
		Enabled:      true,
	}); err != nil {
		return err
	}
	if err := s.UpsertSystemService(ctx, model.SystemService{
		Name:        "sec-ops",
		Description: "SYSTEM-V-TASK-INTERNAL security operations control plane",
		Delivery:    "session",
		Active:      true,
	}); err != nil {
		return err
	}
	if err := s.UpsertSystemRoute(ctx, model.SystemRoute{
		Keyword:     "#系统安全",
		ServiceName: "sec-ops",
		Action:      "fanout",
		Priority:    0,
		Active:      true,
	}); err != nil {
		return err
	}
	keys, err := s.ListServiceAPIKeys(ctx, systemProducerOwner)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key.Active {
			if err := s.BindAPIKeyAccount(ctx, key.ID, systemProducerOwner); err != nil {
				return err
			}
			return nil
		}
	}
	secret := strings.TrimSpace(os.Getenv("WP_SYSTEM_V_TASK_SECRET"))
	if secret == "" {
		secret = "unusable-" + newID()
	}
	sum := sha256.Sum256([]byte(secret))
	key := model.ServiceAPIKey{
		ID:        "FB-system-v-task-internal",
		ServiceID: systemProducerOwner,
		KeyHash:   hex.EncodeToString(sum[:]),
		Label:     "SYSTEM-V-TASK-INTERNAL",
		Active:    true,
		CreatedAt: s.now().UTC(),
	}
	if err := s.InsertServiceAPIKey(ctx, key); err != nil && !strings.Contains(strings.ToLower(err.Error()), "unique") {
		return err
	}
	return s.BindAPIKeyAccount(ctx, key.ID, systemProducerOwner)
}

func (s *SQLite) AdminCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_user`).Scan(&n)
	return n, err
}

func (s *SQLite) CreateAdmin(ctx context.Context, u model.AdminUser) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_user(id, username, password_hash, active, created_at) VALUES(?,?,?,1,?)`,
		u.ID, u.Username, u.PasswordHash, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) GetAdmin(ctx context.Context, username string) (*model.AdminUser, error) {
	var u model.AdminUser
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, active FROM admin_user WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &active)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Active = active == 1
	return &u, nil
}

func (s *SQLite) UpsertChatEntity(ctx context.Context, e model.ChatEntity) error {
	active := boolInt(e.Active)
	if e.ID == "" {
		e.ID = fmt.Sprintf("%s:%s", e.BotChannelID, e.FeishuID)
	}
	if strings.TrimSpace(e.BoundOwner) == "" {
		e.BoundOwner = resolveChatEntityOwner(ctx, s.db, e.FeishuID)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO chat_entity(id, bot_channel_id, type, feishu_id, display_name, bound_owner, active)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(bot_channel_id, feishu_id) DO UPDATE SET type=excluded.type, display_name=excluded.display_name,
		   bound_owner=CASE WHEN excluded.bound_owner!='' THEN excluded.bound_owner ELSE chat_entity.bound_owner END,
		   active=excluded.active`,
		e.ID, e.BotChannelID, string(e.Type), e.FeishuID, e.DisplayName, e.BoundOwner, active)
	return err
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func resolveChatEntityOwner(ctx context.Context, q rowQuerier, feishuOpenID string) string {
	if strings.TrimSpace(feishuOpenID) == "" {
		return ""
	}
	var ownerKey string
	if err := q.QueryRowContext(ctx,
		`SELECT owner_key FROM member WHERE feishu_open_id=? AND active=1 ORDER BY owner_key LIMIT 1`,
		feishuOpenID).Scan(&ownerKey); err != nil {
		return ""
	}
	return ownerKey
}

func (s *SQLite) UpsertProject(ctx context.Context, p model.Project) error {
	now := s.now().UTC().Format(time.RFC3339)
	active := boolInt(p.Active)
	if strings.TrimSpace(p.NotifyBotID) == "" {
		p.NotifyBotID = "unifiedrobot"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO project(id, name, parent_id, owner_key, product_manager_key, notify_chat_id, notify_bot_id, transcript_dirs, evidence_cron, evidence_tz, active, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, parent_id=excluded.parent_id, owner_key=excluded.owner_key, product_manager_key=excluded.product_manager_key, notify_chat_id=excluded.notify_chat_id, notify_bot_id=excluded.notify_bot_id,
		   transcript_dirs=excluded.transcript_dirs, evidence_cron=excluded.evidence_cron, evidence_tz=excluded.evidence_tz,
		   active=excluded.active, updated_at=excluded.updated_at`,
		p.ID, p.Name, p.ParentID, p.OwnerKey, p.ProductManagerKey, p.NotifyChatID, p.NotifyBotID, p.TranscriptDirs, p.EvidenceCron, p.EvidenceTZ, active, now, now)
	return err
}

func (s *SQLite) GetProject(ctx context.Context, id string) (*model.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, projectSelectSQL()+` WHERE id=?`, id))
}

func (s *SQLite) GetProjectByGroupChat(ctx context.Context, chatID string) (*model.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, projectSelectSQL()+` WHERE notify_chat_id=? AND active=1 ORDER BY id LIMIT 1`, chatID))
}

func (s *SQLite) ListProjects(ctx context.Context, activeOnly bool) ([]model.Project, error) {
	query := projectSelectSQL()
	if activeOnly {
		query += ` WHERE active=1`
	}
	query += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		p, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) scanProject(row rowScanner) (*model.Project, error) {
	p, err := scanProjectRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanProjectRow(row rowScanner) (model.Project, error) {
	var p model.Project
	var active int
	var created, updated sql.NullString
	var ownerKey, productManagerKey sql.NullString
	err := row.Scan(&p.ID, &p.Name, &p.ParentID, &ownerKey, &productManagerKey, &p.NotifyChatID, &p.NotifyBotID, &p.TranscriptDirs, &p.EvidenceCron, &p.EvidenceTZ, &active, &created, &updated)
	if err != nil {
		return p, err
	}
	p.Active = active == 1
	if ownerKey.Valid {
		p.OwnerKey = ownerKey.String
	}
	if productManagerKey.Valid {
		p.ProductManagerKey = productManagerKey.String
	}
	if created.Valid {
		p.CreatedAt, _ = time.Parse(time.RFC3339, created.String)
	}
	if updated.Valid {
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updated.String)
	}
	return p, nil
}

func projectSelectSQL() string {
	return `SELECT id, name, parent_id, COALESCE(owner_key, ''), COALESCE(product_manager_key, ''), notify_chat_id, notify_bot_id, transcript_dirs, evidence_cron, evidence_tz, active, created_at, updated_at FROM project`
}

func (s *SQLite) AppendScheduleDoc(ctx context.Context, d model.ScheduleDoc) (model.ScheduleDoc, error) {
	if d.ID == "" {
		d.ID = newID()
	}
	if d.Kind == "" {
		d.Kind = "team"
	}
	if d.ProjectID == "" {
		d.ProjectID = "proj:default"
	}
	if d.Source == "" {
		d.Source = "unknown"
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = s.now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	var latest sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(version) FROM schedule_doc WHERE project_id=? AND kind=? AND owner_key=?`, d.ProjectID, d.Kind, d.OwnerKey).Scan(&latest); err != nil {
		return d, err
	}
	if d.Version <= 0 {
		d.Version = 1
		if latest.Valid {
			d.Version = int(latest.Int64) + 1
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO schedule_doc(id, project_id, kind, owner_key, version, content, source, created_by, created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.Kind, d.OwnerKey, d.Version, d.Content, d.Source, d.CreatedBy, d.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return d, err
	}
	return d, tx.Commit()
}

func (s *SQLite) LatestScheduleDoc(ctx context.Context, projectID, kind, ownerKey string) (*model.ScheduleDoc, error) {
	return scanScheduleDoc(s.db.QueryRowContext(ctx,
		`SELECT id, project_id, kind, owner_key, version, content, source, created_by, created_at
		 FROM schedule_doc WHERE project_id=? AND kind=? AND owner_key=? ORDER BY version DESC LIMIT 1`,
		projectID, kind, ownerKey))
}

func (s *SQLite) ListScheduleDocVersions(ctx context.Context, projectID, kind, ownerKey string) ([]model.ScheduleDoc, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, kind, owner_key, version, content, source, created_by, created_at
		 FROM schedule_doc WHERE project_id=? AND kind=? AND owner_key=? ORDER BY version DESC`,
		projectID, kind, ownerKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ScheduleDoc
	for rows.Next() {
		d, err := scanScheduleDocRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanScheduleDoc(row rowScanner) (*model.ScheduleDoc, error) {
	d, err := scanScheduleDocRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func scanScheduleDocRow(row rowScanner) (model.ScheduleDoc, error) {
	var d model.ScheduleDoc
	var created string
	var createdBy sql.NullString
	err := row.Scan(&d.ID, &d.ProjectID, &d.Kind, &d.OwnerKey, &d.Version, &d.Content, &d.Source, &createdBy, &created)
	if err != nil {
		return d, err
	}
	if createdBy.Valid {
		d.CreatedBy = createdBy.String
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return d, nil
}

func (s *SQLite) AssignProjectMember(ctx context.Context, projectID, ownerKey string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO project_member(project_id, owner_key) VALUES(?,?)`, projectID, ownerKey)
	return err
}

func (s *SQLite) UnassignProjectMember(ctx context.Context, projectID, ownerKey string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_member WHERE project_id=? AND owner_key=?`, projectID, ownerKey)
	return err
}

func (s *SQLite) ListProjectMembers(ctx context.Context, projectID string) ([]model.Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, pm.owner_key, m.display_name, m.feishu_open_id, m.role, m.evidence_optout, m.dm_optout, m.active
		 FROM project_member pm
		 LEFT JOIN member m ON m.owner_key=pm.owner_key
		 WHERE pm.project_id=?
		 ORDER BY COALESCE(m.display_name, pm.owner_key), pm.owner_key`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Member
	for rows.Next() {
		var m model.Member
		var id, display, feishu, role sql.NullString
		var optOut, dmOptOut, active sql.NullInt64
		if err := rows.Scan(&id, &m.OwnerKey, &display, &feishu, &role, &optOut, &dmOptOut, &active); err != nil {
			return nil, err
		}
		if id.Valid {
			m.ID = id.String
		}
		if display.Valid {
			m.DisplayName = display.String
		}
		if feishu.Valid {
			m.FeishuOpenID = feishu.String
		}
		if role.Valid {
			m.Role = model.Role(role.String)
		} else {
			m.Role = model.RoleMember
		}
		m.EvidenceOptOut = optOut.Valid && optOut.Int64 == 1
		m.DMOptOut = dmOptOut.Valid && dmOptOut.Int64 == 1
		m.Active = !active.Valid || active.Int64 == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLite) SetProjectAggregateSources(ctx context.Context, aggregateProjectID string, sourceProjectIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_aggregate_source WHERE aggregate_project_id=?`, aggregateProjectID); err != nil {
		return err
	}
	now := s.now().UTC().Format(time.RFC3339)
	seen := map[string]bool{}
	for _, sourceID := range sourceProjectIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" || sourceID == aggregateProjectID || seen[sourceID] {
			continue
		}
		seen[sourceID] = true
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO project_aggregate_source(aggregate_project_id, source_project_id, created_at) VALUES(?,?,?)`,
			aggregateProjectID, sourceID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) ListProjectAggregateSources(ctx context.Context, aggregateProjectID string) ([]model.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.parent_id, COALESCE(p.owner_key, ''), COALESCE(p.product_manager_key, ''), p.notify_chat_id, p.notify_bot_id, p.transcript_dirs, p.evidence_cron, p.evidence_tz, p.active, p.created_at, p.updated_at
		   FROM project_aggregate_source pas
		   JOIN project p ON p.id=pas.source_project_id
		  WHERE pas.aggregate_project_id=?
		  ORDER BY p.id`, aggregateProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		p, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) ListAggregateProjectIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT aggregate_project_id FROM project_aggregate_source ORDER BY aggregate_project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertProjectWeeklyReport(ctx context.Context, r model.ProjectWeeklyReport) error {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = s.now().UTC()
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = s.now().UTC()
	}
	if strings.TrimSpace(r.Status) == "" {
		r.Status = "final"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO project_weekly_report(id, project_id, week, content, status, approved_at, vetoed_at, published_at, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(project_id, week) DO UPDATE SET content=excluded.content, status=excluded.status, approved_at=excluded.approved_at, vetoed_at=excluded.vetoed_at, published_at=excluded.published_at, updated_at=excluded.updated_at`,
		r.ID, r.ProjectID, r.Week, r.Content, r.Status, nullableTime(r.ApprovedAt), nullableTime(r.VetoedAt), nullableTime(r.PublishedAt), r.CreatedAt.UTC().Format(time.RFC3339), r.UpdatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) GetProjectWeeklyReport(ctx context.Context, projectID, week string) (*model.ProjectWeeklyReport, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, project_id, week, content, COALESCE(status, 'final'), approved_at, vetoed_at, published_at, created_at, updated_at FROM project_weekly_report WHERE project_id=? AND week=?`, projectID, week)
	var r model.ProjectWeeklyReport
	var approved, vetoed, published, updated sql.NullString
	var created string
	if err := row.Scan(&r.ID, &r.ProjectID, &r.Week, &r.Content, &r.Status, &approved, &vetoed, &published, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if updated.Valid {
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updated.String)
	}
	r.ApprovedAt = parseNullableTime(approved)
	r.VetoedAt = parseNullableTime(vetoed)
	r.PublishedAt = parseNullableTime(published)
	return &r, nil
}

func nullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func parseNullableTime(v sql.NullString) *time.Time {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return nil
	}
	return &t
}

func (s *SQLite) GetChatEntity(ctx context.Context, botChannelID, feishuID string) (*model.ChatEntity, error) {
	var e model.ChatEntity
	var typ string
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, bot_channel_id, type, feishu_id, display_name, bound_owner, active
		   FROM chat_entity WHERE bot_channel_id=? AND feishu_id=?`,
		botChannelID, feishuID).
		Scan(&e.ID, &e.BotChannelID, &typ, &e.FeishuID, &e.DisplayName, &e.BoundOwner, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Type = model.ChatType(typ)
	e.Active = active == 1
	return &e, nil
}

func (s *SQLite) ListChatEntities(ctx context.Context, typ model.ChatType) ([]model.ChatEntity, error) {
	query := `SELECT id, bot_channel_id, type, feishu_id, display_name, bound_owner, active FROM chat_entity`
	var args []any
	if typ != "" {
		query += ` WHERE type=?`
		args = append(args, string(typ))
	}
	query += ` ORDER BY bot_channel_id, display_name, feishu_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ChatEntity
	for rows.Next() {
		var e model.ChatEntity
		var rowType string
		var active int
		if err := rows.Scan(&e.ID, &e.BotChannelID, &rowType, &e.FeishuID, &e.DisplayName, &e.BoundOwner, &active); err != nil {
			return nil, err
		}
		e.Type = model.ChatType(rowType)
		e.Active = active == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertBotChannel(ctx context.Context, c model.BotChannel) error {
	secret := any(nil)
	if c.AppSecretEnc != "" {
		secret = c.AppSecretEnc
	}
	token := any(nil)
	if c.VerificationToken != "" {
		token = c.VerificationToken
	}
	encryptKey := any(nil)
	if c.EncryptKey != "" {
		encryptKey = c.EncryptKey
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bot_channel(id, name, app_id, app_secret_enc, verification_token, encrypt_key, purpose, can_send, can_receive, active)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, app_id=excluded.app_id, app_secret_enc=COALESCE(excluded.app_secret_enc, bot_channel.app_secret_enc), verification_token=COALESCE(excluded.verification_token, bot_channel.verification_token), encrypt_key=COALESCE(excluded.encrypt_key, bot_channel.encrypt_key), purpose=excluded.purpose, can_send=excluded.can_send, can_receive=excluded.can_receive, active=excluded.active`,
		c.ID, c.Name, c.AppID, secret, token, encryptKey, c.Purpose, boolInt(c.CanSend), boolInt(c.CanReceive), boolInt(c.Active))
	return err
}

func (s *SQLite) ListBotChannels(ctx context.Context) ([]model.BotChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, app_id, app_secret_enc, COALESCE(verification_token, ''), COALESCE(encrypt_key, ''), purpose, can_send, can_receive, active FROM bot_channel ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BotChannel
	for rows.Next() {
		var c model.BotChannel
		var secret sql.NullString
		var token, encryptKey string
		var canSend, canReceive, active int
		if err := rows.Scan(&c.ID, &c.Name, &c.AppID, &secret, &token, &encryptKey, &c.Purpose, &canSend, &canReceive, &active); err != nil {
			return nil, err
		}
		if secret.Valid {
			c.AppSecretEnc = secret.String
			c.AppSecretSet = secret.String != ""
		}
		c.VerificationToken = token
		c.VerificationTokenSet = token != ""
		c.EncryptKey = encryptKey
		c.EncryptKeySet = encryptKey != ""
		c.CanSend = canSend == 1
		c.CanReceive = canReceive == 1
		c.Active = active == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertSeenPerson(ctx context.Context, p model.SeenPerson) error {
	if p.LastSeenAt.IsZero() {
		p.LastSeenAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO seen_person(open_id, bot_channel_id, name, source, last_seen_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(open_id, bot_channel_id) DO UPDATE SET name=COALESCE(NULLIF(excluded.name,''), seen_person.name), source=excluded.source, last_seen_at=excluded.last_seen_at`,
		p.OpenID, p.BotChannelID, p.Name, p.Source, p.LastSeenAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ListSeenPersons(ctx context.Context) ([]model.SeenPerson, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sp.open_id, sp.bot_channel_id, sp.name, sp.source, sp.last_seen_at,
		        EXISTS(SELECT 1 FROM member m WHERE m.feishu_open_id=sp.open_id AND m.active=1)
		   FROM seen_person sp
		  ORDER BY sp.name, sp.open_id, sp.bot_channel_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SeenPerson
	for rows.Next() {
		var p model.SeenPerson
		var seen string
		var isMember int
		if err := rows.Scan(&p.OpenID, &p.BotChannelID, &p.Name, &p.Source, &seen, &isMember); err != nil {
			return nil, err
		}
		p.LastSeenAt, _ = time.Parse(time.RFC3339, seen)
		p.IsMember = isMember == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertSeenPersonGroup(ctx context.Context, p model.SeenPersonGroup) error {
	if p.LastSeenAt.IsZero() {
		p.LastSeenAt = s.now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO seen_person_group(open_id, bot_channel_id, group_chat_id, group_name, last_seen_at)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(open_id, bot_channel_id, group_chat_id) DO UPDATE SET
		   group_name=COALESCE(NULLIF(excluded.group_name,''), seen_person_group.group_name),
		   last_seen_at=excluded.last_seen_at`,
		p.OpenID, p.BotChannelID, p.GroupChatID, p.GroupName, p.LastSeenAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ListSeenPersonsByGroup(ctx context.Context, groupChatID string) ([]model.SeenPersonGroup, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT open_id, bot_channel_id, group_chat_id, group_name, last_seen_at
		   FROM seen_person_group
		  WHERE group_chat_id=?
		  ORDER BY group_name, open_id, bot_channel_id`, groupChatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SeenPersonGroup
	for rows.Next() {
		var p model.SeenPersonGroup
		var groupName sql.NullString
		var seen string
		if err := rows.Scan(&p.OpenID, &p.BotChannelID, &p.GroupChatID, &groupName, &seen); err != nil {
			return nil, err
		}
		if groupName.Valid {
			p.GroupName = groupName.String
		}
		p.LastSeenAt, _ = time.Parse(time.RFC3339, seen)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteBotChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bot_channel WHERE id=?`, id)
	return err
}

func (s *SQLite) EnqueueMessage(ctx context.Context, m model.Message) error {
	m = s.prepareMessageForInsert(m)
	var feishuMsgID any
	if m.FeishuMsgID != "" {
		feishuMsgID = m.FeishuMsgID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO message(id, chat_entity_id, direction, bot_channel_id, feishu_msg_id, chat_type, sender_open_id, ingress_provenance, content_json, status, attempts, error, created_at, processed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)`,
		m.ID, m.ChatEntityID, string(m.Direction), m.BotChannelID, feishuMsgID, string(m.ChatType), m.SenderOpenID, string(m.IngressProvenance), m.Content, m.Status, m.Attempts, m.Err, m.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) BatchEnqueueMessages(ctx context.Context, messages []model.Message) error {
	if len(messages) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO message(id, chat_entity_id, direction, bot_channel_id, feishu_msg_id, chat_type, sender_open_id, ingress_provenance, content_json, status, attempts, error, created_at, processed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, m := range messages {
		m = s.prepareMessageForInsert(m)
		var feishuMsgID any
		if m.FeishuMsgID != "" {
			feishuMsgID = m.FeishuMsgID
		}
		if _, err := stmt.ExecContext(ctx, m.ID, m.ChatEntityID, string(m.Direction), m.BotChannelID, feishuMsgID, string(m.ChatType), m.SenderOpenID, string(m.IngressProvenance), m.Content, m.Status, m.Attempts, m.Err, m.CreatedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) prepareMessageForInsert(m model.Message) model.Message {
	if m.ID == "" {
		m.ID = newID()
	}
	if m.Status == "" {
		m.Status = "queued"
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	m.IngressProvenance = model.NormalizeIngressProvenance(m.IngressProvenance)
	m.Content = redact.Content(m.Content)
	return m
}

func (s *SQLite) ClaimNextMessage(ctx context.Context, direction model.Direction) (*model.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	cutoff := time.Now().UTC().Add(-messageProcessingLease).Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`UPDATE message SET status='queued' WHERE direction=? AND status='processing' AND processed_at < ?`,
		string(direction), cutoff); err != nil {
		return nil, err
	}

	var id string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM message WHERE direction=? AND status='queued' ORDER BY created_at, id LIMIT 1`,
		string(direction)).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`UPDATE message SET status='processing', attempts=attempts+1, processed_at=? WHERE id=? AND status='queued'`,
		now, id); err != nil {
		return nil, err
	}
	m, err := scanMessageRow(tx.QueryRowContext(ctx,
		`SELECT id, chat_entity_id, direction, bot_channel_id, feishu_msg_id, chat_type, sender_open_id, ingress_provenance, content_json, status, attempts, error, created_at, processed_at FROM message WHERE id=?`,
		id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *SQLite) AckMessage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE message SET status='done', error=NULL, processed_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *SQLite) EnqueueSessionDelivery(ctx context.Context, d model.SessionDelivery) error {
	if d.ID == "" {
		d.ID = newID()
	}
	if d.SourceType == "" {
		d.SourceType = "feishu_inbound"
	}
	if d.State == "" {
		d.State = "queued"
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if d.NextAttemptAt.IsZero() {
		d.NextAttemptAt = d.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO session_delivery
		(id,key_id,session_name,envelope_json,source_message_id,source_type,provenance,state,attempts,next_attempt_at,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, d.ID, d.KeyID, d.SessionName, d.Envelope, nullString(d.SourceMessageID), d.SourceType, nullString(d.Provenance), d.State, d.Attempts, d.NextAttemptAt.UTC().Format(time.RFC3339Nano), d.LastError, d.CreatedAt.UTC().Format(time.RFC3339Nano), d.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func scanSessionDelivery(row rowScanner) (*model.SessionDelivery, error) {
	var d model.SessionDelivery
	var next, created string
	var provenance, lease, delivered, processed sql.NullString
	if err := row.Scan(&d.ID, &d.KeyID, &d.SessionName, &d.Envelope, &d.SourceMessageID, &d.SourceType, &provenance, &d.State, &d.Attempts, &next, &lease, &d.LastError, &created, &delivered, &processed); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if provenance.Valid {
		d.Provenance = provenance.String
	}
	d.NextAttemptAt, _ = time.Parse(time.RFC3339Nano, next)
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if lease.Valid {
		d.LeaseUntil, _ = time.Parse(time.RFC3339Nano, lease.String)
	}
	if delivered.Valid {
		value, _ := time.Parse(time.RFC3339Nano, delivered.String)
		d.DeliveredAt = &value
	}
	if processed.Valid {
		value, _ := time.Parse(time.RFC3339Nano, processed.String)
		d.ProcessedAt = &value
	}
	return &d, nil
}

const sessionDeliverySelect = `SELECT id,key_id,session_name,envelope_json,COALESCE(source_message_id,''),source_type,provenance,state,attempts,next_attempt_at,lease_until,COALESCE(last_error,''),created_at,delivered_at,processed_at FROM session_delivery`

func (s *SQLite) ExpireSessionDeliveries(ctx context.Context, keyID, sessionName string, now time.Time) ([]model.SessionDelivery, error) {
	nowText := now.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `UPDATE session_delivery
		SET state='dead',last_error='delivery_expired',lease_until=NULL,updated_at=?
		WHERE key_id=? AND session_name=? AND state NOT IN ('processed','dead','cancelled') AND (attempts>=12 OR created_at<=?)
		RETURNING id,key_id,session_name,envelope_json,COALESCE(source_message_id,''),source_type,provenance,state,attempts,next_attempt_at,lease_until,COALESCE(last_error,''),created_at,delivered_at,processed_at`,
		nowText, keyID, sessionName, now.Add(-30*time.Minute).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var expired []model.SessionDelivery
	for rows.Next() {
		d, err := scanSessionDelivery(rows)
		if err != nil {
			return nil, err
		}
		expired = append(expired, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return expired, nil
}

func (s *SQLite) ClaimSessionDelivery(ctx context.Context, keyID, sessionName string, now time.Time) (*model.SessionDelivery, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	nowText := now.UTC().Format(time.RFC3339Nano)
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM session_delivery d
		WHERE key_id=? AND session_name=? AND state IN ('queued','sent','delivered') AND next_attempt_at<=?
		AND NOT EXISTS (SELECT 1 FROM session_delivery active WHERE active.key_id=d.key_id AND active.session_name=d.session_name AND active.state IN ('sent','delivered') AND active.next_attempt_at>?)
		ORDER BY CASE WHEN state='queued' AND attempts=0 THEN 0 ELSE 1 END,created_at,id LIMIT 1`, keyID, sessionName, nowText, nowText).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var attempts int
	if err = tx.QueryRowContext(ctx, `SELECT attempts FROM session_delivery WHERE id=?`, id).Scan(&attempts); err != nil {
		return nil, err
	}
	delays := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute}
	delay := 2 * time.Minute
	if attempts < len(delays) {
		delay = delays[attempts]
	}
	lease := now.Add(delay).UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `UPDATE session_delivery SET state='sent',attempts=attempts+1,next_attempt_at=?,lease_until=?,updated_at=? WHERE id=? AND state IN ('queued','sent','delivered')`, lease, lease, nowText, id); err != nil {
		return nil, err
	}
	d, err := scanSessionDelivery(tx.QueryRowContext(ctx, sessionDeliverySelect+` WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *SQLite) UpdateSessionDelivery(ctx context.Context, id, state, errText string, nextAttemptAt time.Time) error {
	if nextAttemptAt.IsZero() {
		nextAttemptAt = time.Now().UTC()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE session_delivery SET state=?,last_error=?,next_attempt_at=?,lease_until=NULL,updated_at=?,
		delivered_at=CASE WHEN ?='delivered' THEN COALESCE(delivered_at,?) ELSE delivered_at END,
		processed_at=CASE WHEN ?='processed' THEN COALESCE(processed_at,?) ELSE processed_at END WHERE id=? AND state NOT IN ('processed','dead','cancelled')`,
		state, errText, nextAttemptAt.UTC().Format(time.RFC3339Nano), now, state, now, state, now, id)
	return err
}

func (s *SQLite) UpdateSessionDeliveryBusy(ctx context.Context, id, errText string, nextAttemptAt time.Time) error {
	if nextAttemptAt.IsZero() {
		nextAttemptAt = time.Now().UTC()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE session_delivery
		SET state='queued',last_error=?,next_attempt_at=?,lease_until=NULL,updated_at=?,
		    attempts=CASE WHEN attempts>0 THEN attempts-1 ELSE attempts END
		WHERE id=? AND state NOT IN ('processed','dead','cancelled')`,
		errText, nextAttemptAt.UTC().Format(time.RFC3339Nano), now, id)
	return err
}

// ReleaseSessionDeliveryLeases makes in-flight work immediately claimable after
// the delivery-ACK WebSocket that owned the lease disappears. The delivery id
// stays unchanged, so the helper inbox remains the idempotency authority.
func (s *SQLite) ReleaseSessionDeliveryLeases(ctx context.Context, keyID, sessionName, reason string, now time.Time) (int64, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE session_delivery
		SET state='queued',last_error=?,next_attempt_at=?,lease_until=NULL,updated_at=?,
		    attempts=CASE WHEN attempts>0 THEN attempts-1 ELSE attempts END
		WHERE key_id=? AND session_name=? AND state IN ('sent','delivered')`,
		reason, nowText, nowText, keyID, sessionName)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLite) GetSessionDelivery(ctx context.Context, id string) (*model.SessionDelivery, error) {
	return scanSessionDelivery(s.db.QueryRowContext(ctx, sessionDeliverySelect+` WHERE id=?`, id))
}

// CancelPendingSessionDelivery is the injection-before cancellation CAS. Only a
// delivery that has not left the Hub queue may transition to cancelled; a late
// cancel racing a claim or processed ACK therefore fails without regressing state.
func (s *SQLite) CancelPendingSessionDelivery(ctx context.Context, messageID, deliveryID, keyID, sessionName string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE session_delivery
		SET state='cancelled',last_error='cancelled_by_authorized_view',lease_until=NULL,updated_at=?
		WHERE id=? AND source_message_id=? AND key_id=? AND session_name=? AND state='queued'`,
		now, deliveryID, messageID, keyID, sessionName)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

// ConfirmSessionDeliveryCancelled records the helper's authoritative removal
// acknowledgement. processed/dead records remain immutable under a late ACK.
func (s *SQLite) ConfirmSessionDeliveryCancelled(ctx context.Context, messageID, deliveryID, keyID, sessionName string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE session_delivery
		SET state='cancelled',last_error='cancelled_by_authorized_view',lease_until=NULL,updated_at=?
		WHERE id=? AND source_message_id=? AND key_id=? AND session_name=? AND state IN ('sent','delivered')`,
		now, deliveryID, messageID, keyID, sessionName)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (s *SQLite) FailMessage(ctx context.Context, id, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE message
		   SET status=CASE WHEN attempts >= 3 THEN 'dead' ELSE 'queued' END,
		       error=?,
		       processed_at=?
		 WHERE id=?`,
		reason, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *SQLite) RecoverProcessing(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE message SET status='queued' WHERE status='processing'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLite) RecentMessages(ctx context.Context, filter model.MessageFilter) ([]model.Message, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	query := `SELECT id, chat_entity_id, direction, bot_channel_id, feishu_msg_id, chat_type, sender_open_id, ingress_provenance, content_json, status, attempts, error, created_at, processed_at FROM message WHERE 1=1`
	args := []any{}
	if filter.BotChannelID != "" {
		query += ` AND bot_channel_id=?`
		args = append(args, filter.BotChannelID)
	}
	if filter.ChatEntityID != "" {
		query += ` AND chat_entity_id=?`
		args = append(args, filter.ChatEntityID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Message
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteCollectMessagesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM message WHERE direction=? AND created_at < ?`,
		string(model.DirectionCollect), cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLite) MessageStats(ctx context.Context) (model.MessageStats, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM message GROUP BY status`)
	if err != nil {
		return model.MessageStats{}, err
	}
	defer rows.Close()
	var st model.MessageStats
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return model.MessageStats{}, err
		}
		st.Total += n
		switch status {
		case "queued":
			st.Queued = n
		case "processing":
			st.Processing = n
		case "done":
			st.Done = n
		case "dead":
			st.Dead = n
		}
	}
	return st, rows.Err()
}

func (s *SQLite) EnqueueControlTask(ctx context.Context, task model.ControlTask) (model.ControlTask, bool, error) {
	if task.ID == "" {
		task.ID = newID()
	}
	now := s.now().UTC()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	if task.Status == "" {
		task.Status = "queued"
	}
	if task.MaxAttempts <= 0 {
		task.MaxAttempts = 3
	}
	if task.ExpireAt == nil {
		expire := now.Add(5 * time.Minute)
		task.ExpireAt = &expire
	}
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO control_task(id, parent_id, created_at, updated_at, source, source_addr, owner_key, bot_channel_id, raw_input, intent, layer, target, result, status, priority, attempts, max_attempts, error, lease_owner, lease_until, expire_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		task.ID, nullString(task.ParentID), task.CreatedAt.UTC().Format(time.RFC3339), task.UpdatedAt.UTC().Format(time.RFC3339), task.Source, task.SourceAddr, task.OwnerKey, nullString(task.BotChannelID), task.RawInput, nullString(task.Intent), nullString(task.Layer), nullString(task.Target), nullString(task.Result), task.Status, task.Priority, task.Attempts, task.MaxAttempts, nullString(task.Error), nullString(task.LeaseOwner), timePtrString(task.LeaseUntil), timePtrString(task.ExpireAt))
	if err != nil {
		return model.ControlTask{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.ControlTask{}, false, err
	}
	if affected == 0 {
		existing, err := s.GetControlTask(ctx, task.ID)
		if err != nil {
			return model.ControlTask{}, false, err
		}
		if existing == nil {
			return model.ControlTask{}, false, sql.ErrNoRows
		}
		return *existing, false, nil
	}
	return task, true, nil
}

func (s *SQLite) GetControlTask(ctx context.Context, id string) (*model.ControlTask, error) {
	task, err := scanControlTaskRow(s.db.QueryRowContext(ctx, `SELECT id, parent_id, created_at, updated_at, source, source_addr, owner_key, bot_channel_id, raw_input, intent, layer, target, result, status, priority, attempts, max_attempts, error, lease_owner, lease_until, expire_at FROM control_task WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return task, err
}

func (s *SQLite) listControlTasks(ctx context.Context, where string, args ...any) ([]model.ControlTask, error) {
	query := `SELECT id, parent_id, created_at, updated_at, source, source_addr, owner_key, bot_channel_id, raw_input, intent, layer, target, result, status, priority, attempts, max_attempts, error, lease_owner, lease_until, expire_at FROM control_task `
	query += where
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ControlTask
	for rows.Next() {
		task, err := scanControlTaskRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	return out, rows.Err()
}

func (s *SQLite) UpdateControlTaskAfterL1(ctx context.Context, id, intent, layer, target, result, status, errText string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE control_task
SET intent=?, layer=?, target=?, result=?, status=?, error=?, updated_at=?, lease_owner=NULL, lease_until=NULL
WHERE id=?`,
		nullString(intent), nullString(layer), nullString(target), nullString(result), status, nullString(errText), s.now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *SQLite) RetryControlTask(ctx context.Context, id, errText string) (*model.ControlTask, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE control_task
SET attempts=attempts+1,
    status=CASE WHEN attempts+1 >= max_attempts THEN 'failed' ELSE 'queued' END,
    error=?,
    updated_at=?,
    lease_owner=NULL,
    lease_until=NULL
WHERE id=?`,
		errText, s.now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return nil, err
	}
	return s.GetControlTask(ctx, id)
}

func (s *SQLite) ReapExpiredControlTasks(ctx context.Context, now time.Time) ([]model.ControlTask, error) {
	nowText := now.UTC().Format(time.RFC3339)
	expired, err := s.listControlTasks(ctx, `WHERE expire_at IS NOT NULL
  AND expire_at <= ?
  AND status NOT IN ('done','failed','expired')`, nowText)
	if err != nil {
		return nil, err
	}
	if len(expired) == 0 {
		return nil, nil
	}
	_, err = s.db.ExecContext(ctx, `UPDATE control_task
SET status='expired', error=COALESCE(NULLIF(error,''), 'task expired'), updated_at=?, lease_owner=NULL, lease_until=NULL
WHERE expire_at IS NOT NULL
  AND expire_at <= ?
  AND status NOT IN ('done','failed','expired')`,
		nowText, nowText)
	if err != nil {
		return nil, err
	}
	for i := range expired {
		expired[i].Status = "expired"
		if expired[i].Error == "" {
			expired[i].Error = "task expired"
		}
		expired[i].UpdatedAt = now.UTC()
	}
	return expired, nil
}

func (s *SQLite) ClaimNextL2ControlTask(ctx context.Context, workerID string, leaseUntil time.Time, now time.Time) (*model.ControlTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	nowText := now.UTC().Format(time.RFC3339)
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM control_task
WHERE status='llm_pending'
  AND attempts < max_attempts
  AND (lease_owner IS NULL OR lease_until IS NULL OR lease_until <= ?)
ORDER BY priority DESC, created_at
LIMIT 1`, nowText).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE control_task
SET lease_owner=?, lease_until=?, updated_at=?
WHERE id=?
  AND status='llm_pending'
  AND attempts < max_attempts
  AND (lease_owner IS NULL OR lease_until IS NULL OR lease_until <= ?)`,
		workerID, leaseUntil.UTC().Format(time.RFC3339), nowText, id, nowText)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, nil
	}
	task, err := scanControlTaskRow(tx.QueryRowContext(ctx, `SELECT id, parent_id, created_at, updated_at, source, source_addr, owner_key, bot_channel_id, raw_input, intent, layer, target, result, status, priority, attempts, max_attempts, error, lease_owner, lease_until, expire_at FROM control_task WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *SQLite) RetryL2ControlTask(ctx context.Context, id, errText string) (*model.ControlTask, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE control_task
SET attempts=attempts+1,
    status=CASE WHEN attempts+1 >= max_attempts THEN 'failed' ELSE 'llm_pending' END,
    error=?,
    updated_at=?,
    lease_owner=NULL,
    lease_until=NULL
WHERE id=? AND status='llm_pending'`,
		errText, s.now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return nil, err
	}
	return s.GetControlTask(ctx, id)
}

func (s *SQLite) RetryAggregatingControlTask(ctx context.Context, id, errText string) (*model.ControlTask, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE control_task
SET attempts=attempts+1,
    status=CASE WHEN attempts+1 >= max_attempts THEN 'failed' ELSE 'awaiting_children' END,
    error=?,
    updated_at=?,
    lease_owner=NULL,
    lease_until=NULL
WHERE id=? AND status='aggregating'`,
		errText, s.now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return nil, err
	}
	return s.GetControlTask(ctx, id)
}

func (s *SQLite) CompleteControlTaskL2(ctx context.Context, id, intent, target, result string, duration time.Duration) error {
	now := s.now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE control_task
SET intent=?, layer='L2', target=?, result=?, status='done', error=NULL, updated_at=?, lease_owner=NULL, lease_until=NULL
WHERE id=?`,
		nullString(intent), nullString(target), nullString(result), now, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO control_task_l2_metric(task_id, duration_ms, success, error, created_at)
VALUES(?,?,?,?,?)
ON CONFLICT(task_id) DO UPDATE SET duration_ms=excluded.duration_ms, success=excluded.success, error=excluded.error, created_at=excluded.created_at`,
		id, duration.Milliseconds(), 1, nil, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) RecordControlTaskL2Failure(ctx context.Context, id, errText string, duration time.Duration) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO control_task_l2_metric(task_id, duration_ms, success, error, created_at)
VALUES(?,?,?,?,?)
ON CONFLICT(task_id) DO UPDATE SET duration_ms=excluded.duration_ms, success=excluded.success, error=excluded.error, created_at=excluded.created_at`,
		id, duration.Milliseconds(), 0, nullString(errText), s.now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) CreateControlSubtasks(ctx context.Context, parent model.ControlTask, children []model.ControlTask) error {
	now := s.now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE control_task
SET intent='decompose', layer='L2', target=?, status='awaiting_children', result=NULL, error=NULL, updated_at=?, lease_owner=NULL, lease_until=NULL
WHERE id=?`,
		parent.Target, now, parent.ID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO control_task(id, parent_id, created_at, updated_at, source, source_addr, owner_key, bot_channel_id, raw_input, intent, layer, target, result, status, priority, attempts, max_attempts, error, lease_owner, lease_until, expire_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, child := range children {
		if child.ID == "" {
			child.ID = newID()
		}
		if child.CreatedAt.IsZero() {
			child.CreatedAt = s.now().UTC()
		}
		child.UpdatedAt = s.now().UTC()
		if child.Status == "" {
			child.Status = "awaiting_result"
		}
		if child.MaxAttempts <= 0 {
			child.MaxAttempts = 3
		}
		if _, err := stmt.ExecContext(ctx,
			child.ID, nullString(child.ParentID), child.CreatedAt.UTC().Format(time.RFC3339), child.UpdatedAt.UTC().Format(time.RFC3339), child.Source, child.SourceAddr, child.OwnerKey, nullString(child.BotChannelID), child.RawInput, nullString(child.Intent), nullString(child.Layer), nullString(child.Target), nullString(child.Result), child.Status, child.Priority, child.Attempts, child.MaxAttempts, nullString(child.Error), nullString(child.LeaseOwner), timePtrString(child.LeaseUntil), timePtrString(child.ExpireAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) CompleteControlSubtask(ctx context.Context, id, result, status, errText string) (*model.ControlTask, error) {
	if status == "" {
		status = "done"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE control_task
SET result=?, status=?, error=?, updated_at=?, lease_owner=NULL, lease_until=NULL
WHERE id=? AND parent_id IS NOT NULL`,
		nullString(result), status, nullString(errText), s.now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return nil, err
	}
	return s.GetControlTask(ctx, id)
}

func (s *SQLite) ListControlSubtasks(ctx context.Context, parentID string) ([]model.ControlTask, error) {
	return s.listControlTasks(ctx, `WHERE parent_id=? ORDER BY created_at, id`, parentID)
}

func (s *SQLite) TryClaimControlParentForAggregation(ctx context.Context, parentID, workerID string, leaseUntil time.Time, now time.Time) (*model.ControlTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var openChildren int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_task WHERE parent_id=? AND status NOT IN ('done','failed','expired')`, parentID).Scan(&openChildren); err != nil {
		return nil, err
	}
	if openChildren != 0 {
		return nil, nil
	}
	nowText := now.UTC().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `UPDATE control_task
SET status='aggregating', lease_owner=?, lease_until=?, updated_at=?
WHERE id=? AND status='awaiting_children'`,
		workerID, leaseUntil.UTC().Format(time.RFC3339), nowText, parentID)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, nil
	}
	parent, err := scanControlTaskRow(tx.QueryRowContext(ctx, `SELECT id, parent_id, created_at, updated_at, source, source_addr, owner_key, bot_channel_id, raw_input, intent, layer, target, result, status, priority, attempts, max_attempts, error, lease_owner, lease_until, expire_at FROM control_task WHERE id=?`, parentID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return parent, nil
}

func (s *SQLite) ClaimNextAggregateControlTask(ctx context.Context, workerID string, leaseUntil time.Time, now time.Time) (*model.ControlTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT p.id
FROM control_task p
WHERE (p.status='awaiting_children' OR (p.status='aggregating' AND (p.lease_owner IS NULL OR p.lease_until IS NULL OR p.lease_until <= ?)))
  AND EXISTS (SELECT 1 FROM control_task c WHERE c.parent_id=p.id)
  AND NOT EXISTS (SELECT 1 FROM control_task c WHERE c.parent_id=p.id AND c.status NOT IN ('done','failed','expired'))
ORDER BY p.priority DESC, p.created_at
LIMIT 1`, now.UTC().Format(time.RFC3339)).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	nowText := now.UTC().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `UPDATE control_task
SET status='aggregating', lease_owner=?, lease_until=?, updated_at=?
WHERE id=?
  AND (status='awaiting_children' OR (status='aggregating' AND (lease_owner IS NULL OR lease_until IS NULL OR lease_until <= ?)))`,
		workerID, leaseUntil.UTC().Format(time.RFC3339), nowText, id, nowText)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, nil
	}
	parent, err := scanControlTaskRow(tx.QueryRowContext(ctx, `SELECT id, parent_id, created_at, updated_at, source, source_addr, owner_key, bot_channel_id, raw_input, intent, layer, target, result, status, priority, attempts, max_attempts, error, lease_owner, lease_until, expire_at FROM control_task WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return parent, nil
}

func (s *SQLite) ControlTaskStats(ctx context.Context) (model.ControlTaskStats, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM control_task GROUP BY status`)
	if err != nil {
		return model.ControlTaskStats{}, err
	}
	defer rows.Close()
	stats := model.ControlTaskStats{Status: map[string]int{}}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return model.ControlTaskStats{}, err
		}
		stats.Status[status] = n
		stats.Total += n
		switch status {
		case "queued", "llm_pending", "dispatched", "awaiting_result", "awaiting_children", "aggregating":
			stats.Depth += n
		}
	}
	if err := rows.Err(); err != nil {
		return model.ControlTaskStats{}, err
	}
	if stats.Total > 0 {
		stats.FailedRate = float64(stats.Status["failed"]) / float64(stats.Total)
		stats.ExpiredRate = float64(stats.Status["expired"]) / float64(stats.Total)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_task WHERE status IN ('llm_pending','aggregating') AND lease_owner IS NOT NULL AND lease_until > ?`, s.now().UTC().Format(time.RFC3339)).Scan(&stats.L2InFlight); err != nil {
		return model.ControlTaskStats{}, err
	}
	l2Rows, err := s.db.QueryContext(ctx, `SELECT duration_ms, success FROM control_task_l2_metric ORDER BY duration_ms`)
	if err != nil {
		return model.ControlTaskStats{}, err
	}
	defer l2Rows.Close()
	var durations []int64
	var total, failures int
	for l2Rows.Next() {
		var duration int64
		var success int
		if err := l2Rows.Scan(&duration, &success); err != nil {
			return model.ControlTaskStats{}, err
		}
		durations = append(durations, duration)
		total++
		if success == 0 {
			failures++
		}
	}
	if err := l2Rows.Err(); err != nil {
		return model.ControlTaskStats{}, err
	}
	if total > 0 {
		stats.L2FailureRate = float64(failures) / float64(total)
		stats.L2P50MS = percentileDuration(durations, 0.50)
		stats.L2P95MS = percentileDuration(durations, 0.95)
	}
	return stats, nil
}

func (s *SQLite) ListRecentControlTaskL2Metrics(ctx context.Context, limit int) ([]model.ControlTaskL2Metric, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT task_id, duration_ms, success, error, created_at
FROM control_task_l2_metric
ORDER BY created_at DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ControlTaskL2Metric
	for rows.Next() {
		var item model.ControlTaskL2Metric
		var success int
		var errText sql.NullString
		var created string
		if err := rows.Scan(&item.TaskID, &item.DurationMS, &success, &errText, &created); err != nil {
			return nil, err
		}
		item.Success = success != 0
		if errText.Valid {
			item.Error = errText.String
		}
		if t, err := time.Parse(time.RFC3339, created); err == nil {
			item.CreatedAt = t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func percentileDuration(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (s *SQLite) ListL1DecisionRules(ctx context.Context) ([]model.L1DecisionRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, seq, match_type, pattern, intent, action, exit_queue, enabled, description, created_at, updated_at FROM l1_decision_rule WHERE enabled=1 ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.L1DecisionRule
	for rows.Next() {
		rule, err := scanL1DecisionRuleRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

func (s *SQLite) ListSchedules(ctx context.Context, ownerKey string) ([]model.Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_key, start_date, end_date, task, status, priority FROM schedule WHERE owner_key=? ORDER BY start_date`, ownerKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Schedule
	for rows.Next() {
		var s model.Schedule
		if err := rows.Scan(&s.ID, &s.OwnerKey, &s.StartDate, &s.EndDate, &s.Task, &s.Status, &s.Priority); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertSchedule(ctx context.Context, sc model.Schedule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO schedule(id, owner_key, start_date, end_date, task, status, priority, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(owner_key, start_date, task) DO UPDATE SET end_date=excluded.end_date, status=excluded.status, priority=excluded.priority, updated_at=excluded.updated_at`,
		sc.ID, sc.OwnerKey, sc.StartDate, sc.EndDate, sc.Task, sc.Status, sc.Priority, now, now)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessageRow(row rowScanner) (*model.Message, error) {
	var m model.Message
	var direction, chatType, created string
	var feishuMsgID, errText, processed sql.NullString
	var provenance string
	err := row.Scan(&m.ID, &m.ChatEntityID, &direction, &m.BotChannelID, &feishuMsgID, &chatType, &m.SenderOpenID, &provenance, &m.Content, &m.Status, &m.Attempts, &errText, &created, &processed)
	if err != nil {
		return nil, err
	}
	m.Direction = model.Direction(direction)
	m.ChatType = model.ChatType(chatType)
	m.IngressProvenance = model.NormalizeIngressProvenance(model.IngressProvenance(provenance))
	if feishuMsgID.Valid {
		m.FeishuMsgID = feishuMsgID.String
	}
	if errText.Valid {
		m.Err = errText.String
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if processed.Valid {
		if t, err := time.Parse(time.RFC3339, processed.String); err == nil {
			m.ProcessedAt = &t
		}
	}
	return &m, nil
}

func scanControlTaskRow(row rowScanner) (*model.ControlTask, error) {
	var task model.ControlTask
	var parentID, botChannelID, intent, layer, target, result, errText, leaseOwner, leaseUntil, expireAt sql.NullString
	var created, updated string
	err := row.Scan(&task.ID, &parentID, &created, &updated, &task.Source, &task.SourceAddr, &task.OwnerKey, &botChannelID, &task.RawInput, &intent, &layer, &target, &result, &task.Status, &task.Priority, &task.Attempts, &task.MaxAttempts, &errText, &leaseOwner, &leaseUntil, &expireAt)
	if err != nil {
		return nil, err
	}
	task.ParentID = nullableString(parentID)
	task.BotChannelID = nullableString(botChannelID)
	task.Intent = nullableString(intent)
	task.Layer = nullableString(layer)
	task.Target = nullableString(target)
	task.Result = nullableString(result)
	task.Error = nullableString(errText)
	task.LeaseOwner = nullableString(leaseOwner)
	task.CreatedAt, _ = time.Parse(time.RFC3339, created)
	task.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	task.LeaseUntil = parseTimePtr(leaseUntil)
	task.ExpireAt = parseTimePtr(expireAt)
	return &task, nil
}

func scanL1DecisionRuleRow(row rowScanner) (model.L1DecisionRule, error) {
	var rule model.L1DecisionRule
	var exitQueue, enabled int
	var desc, created, updated sql.NullString
	err := row.Scan(&rule.ID, &rule.Seq, &rule.MatchType, &rule.Pattern, &rule.Intent, &rule.Action, &exitQueue, &enabled, &desc, &created, &updated)
	if err != nil {
		return rule, err
	}
	rule.ExitQueue = exitQueue == 1
	rule.Enabled = enabled == 1
	rule.Description = nullableString(desc)
	if created.Valid {
		rule.CreatedAt, _ = time.Parse(time.RFC3339, created.String)
	}
	if updated.Valid {
		rule.UpdatedAt, _ = time.Parse(time.RFC3339, updated.String)
	}
	return rule, nil
}

func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func nullableString(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

func parseTimePtr(v sql.NullString) *time.Time {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return nil
	}
	return &t
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *SQLite) DeleteScheduleByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedule WHERE id=?`, id)
	return err
}

func (s *SQLite) UpsertMember(ctx context.Context, m model.Member) error {
	if m.ID == "" {
		m.ID = newID()
	}
	role := string(m.Role)
	if role == "" {
		role = string(model.RoleMember)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO member(id, owner_key, display_name, feishu_open_id, role, evidence_optout, dm_optout, active)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(owner_key) DO UPDATE SET display_name=excluded.display_name, feishu_open_id=excluded.feishu_open_id, role=excluded.role, evidence_optout=excluded.evidence_optout, dm_optout=excluded.dm_optout, active=excluded.active`,
		m.ID, m.OwnerKey, m.DisplayName, m.FeishuOpenID, role, boolInt(m.EvidenceOptOut), boolInt(m.DMOptOut), boolInt(m.Active))
	return err
}

func (s *SQLite) GetMemberByOwnerKey(ctx context.Context, ownerKey string) (*model.Member, error) {
	var m model.Member
	var role string
	var optOut, dmOptOut, active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_key, display_name, feishu_open_id, role, evidence_optout, dm_optout, active FROM member WHERE owner_key=?`,
		ownerKey).Scan(&m.ID, &m.OwnerKey, &m.DisplayName, &m.FeishuOpenID, &role, &optOut, &dmOptOut, &active)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Role = model.Role(role)
	m.EvidenceOptOut = optOut == 1
	m.DMOptOut = dmOptOut == 1
	m.Active = active == 1
	return &m, nil
}

func (s *SQLite) ListMembers(ctx context.Context) ([]model.Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_key, display_name, feishu_open_id, role, evidence_optout, dm_optout, active FROM member WHERE active=1 ORDER BY display_name, owner_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Member
	for rows.Next() {
		var m model.Member
		var role string
		var optOut, dmOptOut, active int
		if err := rows.Scan(&m.ID, &m.OwnerKey, &m.DisplayName, &m.FeishuOpenID, &role, &optOut, &dmOptOut, &active); err != nil {
			return nil, err
		}
		m.Role = model.Role(role)
		m.EvidenceOptOut = optOut == 1
		m.DMOptOut = dmOptOut == 1
		m.Active = active == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLite) AddProgress(ctx context.Context, p model.Progress) error {
	if p.ID == "" {
		p.ID = newID()
	}
	if p.ReportedAt.IsZero() {
		p.ReportedAt = time.Now().UTC()
	}
	if p.Source == "" {
		p.Source = "self"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO progress(id, owner_key, task_key, note, percent, reported_at, source) VALUES(?,?,?,?,?,?,?)`,
		p.ID, p.OwnerKey, p.TaskKey, p.Note, p.Percent, p.ReportedAt.UTC().Format(time.RFC3339Nano), p.Source)
	return err
}

func (s *SQLite) LatestProgress(ctx context.Context, ownerKey string) ([]model.Progress, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.owner_key, p.task_key, p.note, p.percent, p.reported_at, p.source
		   FROM progress p
		  WHERE p.owner_key=?
		    AND NOT EXISTS (
		      SELECT 1 FROM progress q
		       WHERE q.owner_key=p.owner_key AND q.task_key=p.task_key
		         AND (q.reported_at > p.reported_at OR (q.reported_at=p.reported_at AND q.rowid > p.rowid))
		    )
		  ORDER BY p.reported_at DESC, p.id DESC`,
		ownerKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Progress
	for rows.Next() {
		var p model.Progress
		var reported string
		if err := rows.Scan(&p.ID, &p.OwnerKey, &p.TaskKey, &p.Note, &p.Percent, &reported, &p.Source); err != nil {
			return nil, err
		}
		p.ReportedAt, _ = time.Parse(time.RFC3339Nano, reported)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) ListProgressBetween(ctx context.Context, ownerKey string, from, to time.Time) ([]model.Progress, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_key, task_key, note, percent, reported_at, source
		   FROM progress
		  WHERE owner_key=? AND reported_at>=? AND reported_at<?
		  ORDER BY reported_at ASC, id ASC`,
		ownerKey, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Progress
	for rows.Next() {
		var p model.Progress
		var reported string
		if err := rows.Scan(&p.ID, &p.OwnerKey, &p.TaskKey, &p.Note, &p.Percent, &reported, &p.Source); err != nil {
			return nil, err
		}
		p.ReportedAt, _ = time.Parse(time.RFC3339Nano, reported)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) ReportRisk(ctx context.Context, r model.Risk) error {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if r.Status == "" {
		r.Status = "open"
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE risk SET reporters_json=? WHERE status='open' AND owner_key=? AND content=?`,
		r.ReportersJSON, r.OwnerKey, r.Content)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO risk(id, owner_key, content, status, related_task_key, reporters_json, created_at) VALUES(?,?,?,?,?,?,?)`,
		r.ID, r.OwnerKey, r.Content, r.Status, r.RelatedTaskKey, r.ReportersJSON, r.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ResolveRisks(ctx context.Context, ownerKey, keyword string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE risk SET status='resolved', resolved_at=? WHERE status='open' AND owner_key=? AND content LIKE ?`,
		time.Now().UTC().Format(time.RFC3339), ownerKey, "%"+keyword+"%")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLite) ListOpenRisks(ctx context.Context, ownerKey string) ([]model.Risk, error) {
	query := `SELECT id, owner_key, content, status, related_task_key, reporters_json, created_at, resolved_at FROM risk WHERE status='open'`
	args := []any{}
	if ownerKey != "" {
		query += ` AND owner_key=?`
		args = append(args, ownerKey)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Risk
	for rows.Next() {
		var r model.Risk
		var owner, related, reporters sql.NullString
		var created string
		var resolved sql.NullString
		if err := rows.Scan(&r.ID, &owner, &r.Content, &r.Status, &related, &reporters, &created, &resolved); err != nil {
			return nil, err
		}
		if owner.Valid {
			r.OwnerKey = owner.String
		}
		if related.Valid {
			r.RelatedTaskKey = related.String
		}
		if reporters.Valid {
			r.ReportersJSON = reporters.String
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if resolved.Valid {
			if t, err := time.Parse(time.RFC3339, resolved.String); err == nil {
				r.ResolvedAt = &t
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLite) AddAIEvidence(ctx context.Context, e model.AIEvidence) error {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ai_evidence(id, owner_key, session_id, session_source, work_item, artifact, files_json, action_type, occurred_at, mapped_task_key, confidence, raw_excerpt_hash)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.OwnerKey, e.SessionID, e.SessionSource, e.WorkItem, e.Artifact, e.FilesJSON, e.ActionType, e.OccurredAt.UTC().Format(time.RFC3339Nano), e.MappedTaskKey, e.Confidence, e.RawExcerptHash)
	return err
}

func (s *SQLite) ListAIEvidence(ctx context.Context, ownerKey, taskKey string) ([]model.AIEvidence, error) {
	query := `SELECT id, owner_key, session_id, session_source, work_item, artifact, files_json, action_type, occurred_at, mapped_task_key, confidence, raw_excerpt_hash FROM ai_evidence WHERE owner_key=?`
	args := []any{ownerKey}
	if taskKey != "" {
		query += ` AND mapped_task_key=?`
		args = append(args, taskKey)
	}
	query += ` ORDER BY occurred_at DESC, id DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AIEvidence
	for rows.Next() {
		var e model.AIEvidence
		var occurred string
		if err := rows.Scan(&e.ID, &e.OwnerKey, &e.SessionID, &e.SessionSource, &e.WorkItem, &e.Artifact, &e.FilesJSON, &e.ActionType, &occurred, &e.MappedTaskKey, &e.Confidence, &e.RawExcerptHash); err != nil {
			return nil, err
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLite) ListAIEvidenceBetween(ctx context.Context, from, to time.Time) ([]model.AIEvidence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_key, session_id, session_source, work_item, artifact, files_json, action_type, occurred_at, mapped_task_key, confidence, raw_excerpt_hash
		   FROM ai_evidence
		  WHERE occurred_at>=? AND occurred_at<?
		  ORDER BY occurred_at ASC, id ASC`,
		from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AIEvidence
	for rows.Next() {
		var e model.AIEvidence
		var occurred string
		if err := rows.Scan(&e.ID, &e.OwnerKey, &e.SessionID, &e.SessionSource, &e.WorkItem, &e.Artifact, &e.FilesJSON, &e.ActionType, &occurred, &e.MappedTaskKey, &e.Confidence, &e.RawExcerptHash); err != nil {
			return nil, err
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLite) MapAIEvidenceToTask(ctx context.Context, evidenceID, ownerKey, taskKey string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ai_evidence SET mapped_task_key=? WHERE id=? AND owner_key=?`,
		taskKey, evidenceID, ownerKey)
	return err
}

func (s *SQLite) UpsertReconciliation(ctx context.Context, r model.Reconciliation) error {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.ComputedAt.IsZero() {
		r.ComputedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM reconciliation WHERE owner_key=? AND task_key=?`, r.OwnerKey, r.TaskKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO reconciliation(id, owner_key, task_key, self_status, evidence_count, verdict, computed_at, detail_json)
		 VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.OwnerKey, r.TaskKey, r.SelfStatus, r.EvidenceCount, r.Verdict, r.ComputedAt.UTC().Format(time.RFC3339Nano), r.DetailJSON); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) ListReconciliation(ctx context.Context, ownerKey string) ([]model.Reconciliation, error) {
	query := `SELECT id, owner_key, task_key, self_status, evidence_count, verdict, computed_at, detail_json FROM reconciliation`
	args := []any{}
	if ownerKey != "" {
		query += ` WHERE owner_key=?`
		args = append(args, ownerKey)
	}
	query += ` ORDER BY computed_at DESC, owner_key, task_key`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Reconciliation
	for rows.Next() {
		var r model.Reconciliation
		var computed string
		if err := rows.Scan(&r.ID, &r.OwnerKey, &r.TaskKey, &r.SelfStatus, &r.EvidenceCount, &r.Verdict, &computed, &r.DetailJSON); err != nil {
			return nil, err
		}
		r.ComputedAt, _ = time.Parse(time.RFC3339Nano, computed)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutPending 写入待确认变更：先把该 actor 的旧 pending 置 cancelled，再插入新条（§15.2 同时只一个）。
func (s *SQLite) PutPending(ctx context.Context, actor, payloadJSON string, expiresAt time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE pending_update SET status='cancelled' WHERE open_id=? AND status='pending'`, actor); err != nil {
		return "", err
	}
	id := newID()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO pending_update(id, open_id, payload_json, status, created_at, expires_at) VALUES(?,?,?,'pending',?,?)`,
		id, actor, payloadJSON, time.Now().UTC().Format(time.RFC3339), expiresAt.UTC().Format(time.RFC3339)); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *SQLite) GetPending(ctx context.Context, actor string) (*model.Pending, error) {
	var p model.Pending
	var created, expires string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, open_id, payload_json, status, created_at, expires_at FROM pending_update WHERE open_id=? AND status='pending'`, actor).
		Scan(&p.ID, &p.Actor, &p.PayloadJSON, &p.Status, &created, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if t, e := time.Parse(time.RFC3339, expires); e == nil {
		// 过期则视为无待确认（§15.2 TTL）。用可注入时钟（默认 Real），测试可注入 Fake。
		if s.now().After(t) {
			_ = s.SetPendingStatus(ctx, p.ID, "expired")
			return nil, nil
		}
		p.ExpiresAt = &t
	}
	return &p, nil
}

func (s *SQLite) SetPendingStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_update SET status=? WHERE id=?`, status, id)
	return err
}

func (s *SQLite) WriteAudit(ctx context.Context, actor, action, target string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log(id, actor, action, target, ts) VALUES(?,?,?,?,?)`,
		newID(), actor, action, target, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ListRecentAudit(ctx context.Context, actionPrefix string, limit int) ([]model.AuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	actionPrefix = strings.TrimSpace(actionPrefix)
	query := `SELECT id, COALESCE(actor, ''), action, COALESCE(target, ''), ts FROM audit_log`
	var args []any
	if actionPrefix != "" {
		query += ` WHERE action LIKE ?`
		args = append(args, actionPrefix+"%")
	}
	query += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AuditLog
	for rows.Next() {
		var item model.AuditLog
		var ts string
		if err := rows.Scan(&item.ID, &item.Actor, &item.Action, &item.Target, &ts); err != nil {
			return nil, err
		}
		item.TS, _ = time.Parse(time.RFC3339, ts)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLite) UpsertAppConfig(ctx context.Context, key, valueJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_config(key, value_json, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at`,
		key, valueJSON, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLite) ListAppConfig(ctx context.Context) ([]model.AppConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value_json, updated_at FROM app_config ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AppConfig
	for rows.Next() {
		var c model.AppConfig
		var updated sql.NullString
		if err := rows.Scan(&c.Key, &c.ValueJSON, &updated); err != nil {
			return nil, err
		}
		if updated.Valid {
			c.UpdatedAt, _ = time.Parse(time.RFC3339, updated.String)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteAppConfig(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_config WHERE key=?`, key)
	return err
}

func (s *SQLite) CleanupBefore(ctx context.Context, cutoff time.Time, confirm bool) (model.CleanupResult, error) {
	cut := cutoff.UTC().Format(time.RFC3339Nano)
	count := func(query string) (int, error) {
		var n int
		err := s.db.QueryRowContext(ctx, query, cut).Scan(&n)
		return n, err
	}
	result := model.CleanupResult{}
	var err error
	if result.Messages, err = count(`SELECT COUNT(*) FROM message WHERE created_at < ?`); err != nil {
		return result, err
	}
	if result.AIEvidence, err = count(`SELECT COUNT(*) FROM ai_evidence WHERE occurred_at < ?`); err != nil {
		return result, err
	}
	if result.Reconciliations, err = count(`SELECT COUNT(*) FROM reconciliation WHERE computed_at < ?`); err != nil {
		return result, err
	}
	if !confirm {
		return result, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM message WHERE created_at < ?`, cut); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_evidence WHERE occurred_at < ?`, cut); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reconciliation WHERE computed_at < ?`, cut); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (s *SQLite) RecordNotification(ctx context.Context, kind, ownerKey, targetKey, remindDate string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO notification_log(id, kind, owner_key, target_key, remind_date, created_at) VALUES(?,?,?,?,?,?)`,
		newID(), kind, ownerKey, targetKey, remindDate, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLite) UpsertSessionEndpoint(ctx context.Context, ep model.SessionEndpoint) error {
	if ep.LastSeenAt.IsZero() {
		ep.LastSeenAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_endpoint(key_id, session_name, full_session_name, owner_key, last_seen_at, active, mirror_enabled, mirror_to, client_ip, tool, model, version, producer, target_group, no_directory, no_directory_admin, no_directory_reported)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(key_id, session_name) DO UPDATE SET
		   last_seen_at=excluded.last_seen_at,
		   active=excluded.active,
		   full_session_name=COALESCE(NULLIF(excluded.full_session_name,''), session_endpoint.full_session_name),
		   owner_key=COALESCE(NULLIF(excluded.owner_key,''), session_endpoint.owner_key),
		   client_ip=COALESCE(NULLIF(excluded.client_ip,''), session_endpoint.client_ip),
		   tool=COALESCE(NULLIF(excluded.tool,''), session_endpoint.tool),
		   model=COALESCE(NULLIF(excluded.model,''), session_endpoint.model),
		   version=CASE WHEN excluded.active=0 THEN session_endpoint.version ELSE excluded.version END,
		   producer=excluded.producer,
		   target_group=COALESCE(NULLIF(excluded.target_group,''), session_endpoint.target_group),
		   no_directory_reported=CASE WHEN excluded.active=0 THEN session_endpoint.no_directory_reported ELSE excluded.no_directory_reported END,
		   no_directory=CASE
		     WHEN excluded.active=0 THEN session_endpoint.no_directory
		     WHEN session_endpoint.no_directory_admin=1 OR excluded.no_directory_reported=1 THEN 1
		     ELSE 0
		   END,
		   mirror_enabled=CASE WHEN excluded.mirror_to<>'' THEN excluded.mirror_enabled ELSE session_endpoint.mirror_enabled END,
		   mirror_to=COALESCE(NULLIF(excluded.mirror_to,''), session_endpoint.mirror_to)`,
		ep.KeyID, ep.SessionName, ep.FullSessionName, ep.OwnerKey, ep.LastSeenAt.UTC().Format(time.RFC3339), boolInt(ep.Active), boolInt(ep.MirrorEnabled), ep.MirrorTo, ep.ClientIP, ep.Tool, ep.Model, ep.Version, boolInt(ep.Producer), ep.TargetGroup, boolInt(ep.NoDirectory), boolInt(ep.NoDirectoryAdmin), boolInt(ep.NoDirectory))
	return err
}

func (s *SQLite) IncrementSessionEndpointConnSeq(ctx context.Context, keyID, sessionName string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE session_endpoint SET conn_seq=conn_seq+1 WHERE key_id=? AND session_name=?`, keyID, sessionName); err != nil {
		return 0, err
	}
	var seq int
	if err := tx.QueryRowContext(ctx, `SELECT conn_seq FROM session_endpoint WHERE key_id=? AND session_name=?`, keyID, sessionName).Scan(&seq); err != nil {
		return 0, err
	}
	return seq, tx.Commit()
}

func (s *SQLite) TouchSessionEndpoint(ctx context.Context, keyID, sessionName string, lastSeenAt time.Time) error {
	if lastSeenAt.IsZero() {
		lastSeenAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE session_endpoint SET last_seen_at=? WHERE key_id=? AND session_name=?`, lastSeenAt.UTC().Format(time.RFC3339), keyID, sessionName)
	return err
}

func (s *SQLite) ListSessionEndpoints(ctx context.Context) ([]model.SessionEndpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key_id, session_name, COALESCE(full_session_name, ''), COALESCE(owner_key, ''), last_seen_at, active, mirror_enabled, COALESCE(mirror_to, ''), COALESCE(client_ip, ''), COALESCE(tool, ''), COALESCE(model, ''), COALESCE(version, ''), COALESCE(producer, 0), COALESCE(target_group, ''), COALESCE(no_directory, 0), COALESCE(no_directory_admin, 0), COALESCE(conn_seq, 0), COALESCE(delivery_ack_v1, 0)
		   FROM session_endpoint ORDER BY key_id, session_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SessionEndpoint
	for rows.Next() {
		var ep model.SessionEndpoint
		var lastSeen string
		var active, mirrorEnabled, producer, noDirectory, noDirectoryAdmin, deliveryACKV1 int
		if err := rows.Scan(&ep.KeyID, &ep.SessionName, &ep.FullSessionName, &ep.OwnerKey, &lastSeen, &active, &mirrorEnabled, &ep.MirrorTo, &ep.ClientIP, &ep.Tool, &ep.Model, &ep.Version, &producer, &ep.TargetGroup, &noDirectory, &noDirectoryAdmin, &ep.ConnSeq, &deliveryACKV1); err != nil {
			return nil, err
		}
		ep.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeen)
		ep.Active = active == 1
		ep.MirrorEnabled = mirrorEnabled == 1
		ep.Producer = producer == 1
		ep.NoDirectory = noDirectory == 1
		ep.NoDirectoryAdmin = noDirectoryAdmin == 1
		ep.DeliveryACKV1 = deliveryACKV1 == 1
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *SQLite) SetSessionEndpointDeliveryACK(ctx context.Context, keyID, sessionName string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE session_endpoint SET delivery_ack_v1=? WHERE key_id=? AND session_name=?`,
		boolInt(enabled), keyID, sessionName)
	return err
}

func (s *SQLite) SessionEndpointDeliveryACK(ctx context.Context, keyID, sessionName string) (bool, bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx,
		`SELECT delivery_ack_v1 FROM session_endpoint WHERE key_id=? AND session_name=?`,
		keyID, sessionName).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return enabled == 1, true, nil
}

var agentGroupCodePattern = regexp.MustCompile(`^(?:[A-Za-z0-9+/]{32}|[A-Za-z0-9_-]{32})$`)

func validateAgentGroupCode(code string) error {
	if !agentGroupCodePattern.MatchString(code) {
		return errors.New("group code must be exactly 32 base64 characters")
	}
	return nil
}

func (s *SQLite) UpsertAgentGroup(ctx context.Context, group model.AgentGroup) error {
	if err := validateAgentGroupCode(group.Code); err != nil {
		return err
	}
	if group.LeaderKeyID == "" || group.LeaderSessionName == "" || group.LeaderOwnerKey == "" {
		return errors.New("group leader endpoint and owner are required")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_group(code,leader_key_id,leader_session_name,leader_owner_key,created_at,updated_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(code) DO UPDATE SET leader_key_id=excluded.leader_key_id,
		leader_session_name=excluded.leader_session_name,leader_owner_key=excluded.leader_owner_key,updated_at=excluded.updated_at`,
		group.Code, group.LeaderKeyID, group.LeaderSessionName, group.LeaderOwnerKey, now, now); err != nil {
		return err
	}
	// A leader is mechanically a member of the group; this also makes the
	// one-leader invariant useful immediately after first creation.
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_group_member(code,key_id,session_name,owner_key,created_at)
		VALUES(?,?,?,?,?) ON CONFLICT(code,key_id,session_name) DO UPDATE SET owner_key=excluded.owner_key`,
		group.Code, group.LeaderKeyID, group.LeaderSessionName, group.LeaderOwnerKey, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_group_state SET ever_activated=1 WHERE singleton=1`); err != nil {
		return err
	}
	return tx.Commit()
}

var ErrAgentGroupShortNameConflict = errors.New("agent group short name conflict")
var ErrAgentGroupCodeConflict = errors.New("agent group code conflict")
var ErrAgentGroupMemberExists = errors.New("agent group member already exists")
var ErrAgentGroupAdditionsClosed = errors.New("agent group additions closed")
var ErrAgentGroupAccessDenied = errors.New("agent group access denied")
var ErrAgentGroupLeaderProtected = errors.New("agent group leader protected")
var ErrAgentGroupAgentUnavailable = errors.New("agent group agent unavailable")
var ErrAgentGroupMemberNotFound = errors.New("agent group member not found")

func (s *SQLite) agentGroupExists(ctx context.Context, code string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_group WHERE code=?)`, code).Scan(&exists)
	return exists != 0, err
}

// agentGroupHasCreator distinguishes user-created groups from legacy rows.
// Legacy rows deliberately retain the established non-disclosure 404 contract.
func (s *SQLite) agentGroupHasCreator(ctx context.Context, code string) (bool, error) {
	var creator sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT creator_principal FROM agent_group WHERE code=?`, code).Scan(&creator)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return creator.Valid && creator.String != "", err
}

func (s *SQLite) CreateConsoleAgentGroup(ctx context.Context, group model.AgentGroup) error {
	if err := validateAgentGroupCode(group.Code); err != nil {
		return err
	}
	if !validConsoleAgentGroupName(group.ShortName) || group.CreatorPrincipal == "" || group.LeaderKeyID == "" || group.LeaderSessionName == "" || group.LeaderOwnerKey == "" {
		return errors.New("complete console group identity required")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_group(code,short_name,creator_principal,additions_closed,leader_key_id,leader_session_name,leader_owner_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		group.Code, group.ShortName, group.CreatorPrincipal, boolInt(group.AdditionsClosed), group.LeaderKeyID, group.LeaderSessionName, group.LeaderOwnerKey, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "agent_group.short_name") {
			return ErrAgentGroupShortNameConflict
		}
		if strings.Contains(err.Error(), "agent_group.code") {
			return ErrAgentGroupCodeConflict
		}
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_group_member(code,key_id,session_name,owner_key,created_at) VALUES(?,?,?,?,?)`, group.Code, group.LeaderKeyID, group.LeaderSessionName, group.LeaderOwnerKey, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_group_state SET ever_activated=1 WHERE singleton=1`); err != nil {
		return err
	}
	return tx.Commit()
}

func validConsoleAgentGroupName(name string) bool {
	if strings.TrimSpace(name) != name || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 8 {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

func (s *SQLite) DeleteConsoleAgentGroup(ctx context.Context, code, principalID string, management bool) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM agent_group WHERE code=? AND (creator_principal=? OR (creator_principal IS NULL AND EXISTS(
		SELECT 1 FROM console_agent a WHERE a.key_id=agent_group.leader_key_id AND a.session_name=agent_group.leader_session_name AND a.owner_principal=? AND a.active=1)) OR
		(?=1 AND creator_principal IS NULL AND NOT EXISTS(SELECT 1 FROM console_agent a WHERE a.key_id=agent_group.leader_key_id AND a.session_name=agent_group.leader_session_name AND a.active=1)))`, code, principalID, principalID, boolInt(management))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		exists, lookupErr := s.agentGroupExists(ctx, code)
		if lookupErr != nil {
			return lookupErr
		}
		hasCreator, creatorErr := s.agentGroupHasCreator(ctx, code)
		if creatorErr != nil {
			return creatorErr
		}
		if exists && hasCreator {
			return ErrAgentGroupAccessDenied
		}
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) SetConsoleAgentGroupAdditionsClosed(ctx context.Context, code, principalID string, closed, management bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_group SET additions_closed=?,creator_principal=COALESCE(creator_principal,?),updated_at=? WHERE code=? AND (creator_principal=? OR (creator_principal IS NULL AND EXISTS(
		SELECT 1 FROM console_agent a WHERE a.key_id=agent_group.leader_key_id AND a.session_name=agent_group.leader_session_name AND a.owner_principal=? AND a.active=1)) OR
		(?=1 AND creator_principal IS NULL AND NOT EXISTS(SELECT 1 FROM console_agent a WHERE a.key_id=agent_group.leader_key_id AND a.session_name=agent_group.leader_session_name AND a.active=1)))`, boolInt(closed), principalID, s.now().UTC().Format(time.RFC3339Nano), code, principalID, principalID, boolInt(management))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		exists, lookupErr := s.agentGroupExists(ctx, code)
		if lookupErr != nil {
			return lookupErr
		}
		hasCreator, creatorErr := s.agentGroupHasCreator(ctx, code)
		if creatorErr != nil {
			return creatorErr
		}
		if exists && hasCreator {
			return ErrAgentGroupAccessDenied
		}
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) AssignOwnedConsoleAgentGroup(ctx context.Context, code, principalID, agentID string) error {
	res, err := s.db.ExecContext(ctx, `INSERT INTO agent_group_member(code,key_id,session_name,owner_key,created_at)
		SELECT g.code,a.key_id,a.session_name,e.owner_key,? FROM agent_group g JOIN console_agent a ON a.id=?
		JOIN session_endpoint e ON e.key_id=a.key_id AND e.session_name=a.session_name
		WHERE g.code=? AND g.creator_principal IS NOT NULL AND a.owner_principal=? AND a.active=1 AND g.additions_closed=0
		ON CONFLICT(code,key_id,session_name) DO NOTHING`,
		s.now().UTC().Format(time.RFC3339Nano), agentID, code, principalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		var additionsClosed int
		var creator sql.NullString
		lookupErr := s.db.QueryRowContext(ctx, `SELECT additions_closed,creator_principal FROM agent_group WHERE code=?`, code).Scan(&additionsClosed, &creator)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		if lookupErr != nil {
			return lookupErr
		}
		if !creator.Valid || creator.String == "" {
			return sql.ErrNoRows
		}
		if additionsClosed != 0 {
			return ErrAgentGroupAdditionsClosed
		}
		var exists int
		lookupErr = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_group_member m JOIN console_agent a ON a.key_id=m.key_id AND a.session_name=m.session_name WHERE m.code=? AND a.id=?)`, code, agentID).Scan(&exists)
		if lookupErr != nil {
			return lookupErr
		}
		if exists != 0 {
			return ErrAgentGroupMemberExists
		}
		lookupErr = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM console_agent WHERE id=? AND owner_principal=? AND active=1)`, agentID, principalID).Scan(&exists)
		if lookupErr != nil {
			return lookupErr
		}
		if exists == 0 {
			return ErrAgentGroupAgentUnavailable
		}
		// The group and the caller-owned active Agent both existed, yet the
		// INSERT selected no row. This covers internal referential drift such as
		// a missing session_endpoint; it intentionally remains a non-disclosing
		// 404 until a reproducible repair path is available.
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) UnassignConsoleAgentGroup(ctx context.Context, code, principalID, agentID string, management bool) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM agent_group_member WHERE code=? AND (key_id,session_name) IN (
		SELECT a.key_id,a.session_name FROM console_agent a JOIN agent_group g ON g.code=?
		WHERE a.id=? AND (a.owner_principal=? OR g.creator_principal=? OR (g.creator_principal IS NULL AND EXISTS(
			SELECT 1 FROM console_agent leader WHERE leader.key_id=g.leader_key_id AND leader.session_name=g.leader_session_name AND leader.owner_principal=? AND leader.active=1)) OR
			(?=1 AND g.creator_principal IS NULL AND NOT EXISTS(SELECT 1 FROM console_agent leader WHERE leader.key_id=g.leader_key_id AND leader.session_name=g.leader_session_name AND leader.active=1)))
	) AND NOT (key_id=(SELECT leader_key_id FROM agent_group WHERE code=?) AND session_name=(SELECT leader_session_name FROM agent_group WHERE code=?))`,
		code, code, agentID, principalID, principalID, principalID, boolInt(management), code, code)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		exists, lookupErr := s.agentGroupExists(ctx, code)
		if lookupErr != nil {
			return lookupErr
		}
		if !exists {
			return sql.ErrNoRows
		}
		hasCreator, creatorErr := s.agentGroupHasCreator(ctx, code)
		if creatorErr != nil {
			return creatorErr
		}
		if !hasCreator {
			return sql.ErrNoRows
		}
		var member, leader int
		lookupErr = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_group_member m JOIN console_agent a ON a.key_id=m.key_id AND a.session_name=m.session_name WHERE m.code=? AND a.id=?)`, code, agentID).Scan(&member)
		if lookupErr != nil {
			return lookupErr
		}
		if member == 0 {
			return ErrAgentGroupMemberNotFound
		}
		lookupErr = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_group g JOIN console_agent a ON a.id=? WHERE g.code=? AND g.leader_key_id=a.key_id AND g.leader_session_name=a.session_name)`, agentID, code).Scan(&leader)
		if lookupErr != nil {
			return lookupErr
		}
		if leader != 0 {
			return ErrAgentGroupLeaderProtected
		}
		return ErrAgentGroupAccessDenied
	}
	return nil
}

func (s *SQLite) AgentGroupingActivated(ctx context.Context) (bool, error) {
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT ever_activated FROM agent_group_state WHERE singleton=1`).Scan(&active)
	return active != 0, err
}

func (s *SQLite) AssignAgentGroup(ctx context.Context, member model.AgentGroupMember) error {
	if err := validateAgentGroupCode(member.Code); err != nil {
		return err
	}
	if member.KeyID == "" || member.SessionName == "" || member.OwnerKey == "" {
		return errors.New("group member endpoint and owner are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_group_member(code,key_id,session_name,owner_key,created_at)
		VALUES(?,?,?,?,?) ON CONFLICT(code,key_id,session_name) DO UPDATE SET owner_key=excluded.owner_key`,
		member.Code, member.KeyID, member.SessionName, member.OwnerKey, s.now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLite) UnassignAgentGroup(ctx context.Context, code, keyID, sessionName string) error {
	if err := validateAgentGroupCode(code); err != nil {
		return err
	}
	var leaderKeyID, leaderSessionName string
	if err := s.db.QueryRowContext(ctx, `SELECT leader_key_id,leader_session_name FROM agent_group WHERE code=?`, code).Scan(&leaderKeyID, &leaderSessionName); err != nil {
		return err
	}
	if leaderKeyID == keyID && leaderSessionName == sessionName {
		return errors.New("group leader cannot be removed; appoint another leader first")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_group_member WHERE code=? AND key_id=? AND session_name=?`, code, keyID, sessionName)
	return err
}

func (s *SQLite) ListAgentGroups(ctx context.Context) ([]model.AgentGroup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.code,COALESCE(g.short_name,''),COALESCE(g.creator_principal,''),g.additions_closed,g.leader_key_id,g.leader_session_name,g.leader_owner_key,
		EXISTS(SELECT 1 FROM console_agent a WHERE a.key_id=g.leader_key_id AND a.session_name=g.leader_session_name AND a.active=1),g.created_at,g.updated_at FROM agent_group g ORDER BY g.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AgentGroup
	for rows.Next() {
		var g model.AgentGroup
		var createdAt, updatedAt string
		var closed, mapped int
		if err := rows.Scan(&g.Code, &g.ShortName, &g.CreatorPrincipal, &closed, &g.LeaderKeyID, &g.LeaderSessionName, &g.LeaderOwnerKey, &mapped, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		g.AdditionsClosed = closed != 0
		g.LeaderConsoleMapped = mapped != 0
		g.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		g.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *SQLite) ListAgentGroupMembers(ctx context.Context, code string) ([]model.AgentGroupMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.code,COALESCE(a.id,''),m.key_id,m.session_name,m.owner_key,m.created_at FROM agent_group_member m LEFT JOIN console_agent a ON a.key_id=m.key_id AND a.session_name=m.session_name WHERE m.code=? ORDER BY m.owner_key,m.session_name,m.key_id`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AgentGroupMember
	for rows.Next() {
		var m model.AgentGroupMember
		var createdAt string
		if err := rows.Scan(&m.Code, &m.AgentID, &m.KeyID, &m.SessionName, &m.OwnerKey, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLite) ListAgentGroupsForEndpoint(ctx context.Context, keyID, sessionName string) ([]model.AgentGroup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.code,COALESCE(g.short_name,''),COALESCE(g.creator_principal,''),g.additions_closed,g.leader_key_id,g.leader_session_name,g.leader_owner_key,
		EXISTS(SELECT 1 FROM console_agent a WHERE a.key_id=g.leader_key_id AND a.session_name=g.leader_session_name AND a.active=1),g.created_at,g.updated_at
		FROM agent_group g JOIN agent_group_member m ON m.code=g.code WHERE m.key_id=? AND m.session_name=? ORDER BY g.code`, keyID, sessionName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AgentGroup
	for rows.Next() {
		var g model.AgentGroup
		var createdAt, updatedAt string
		var closed, mapped int
		if err := rows.Scan(&g.Code, &g.ShortName, &g.CreatorPrincipal, &closed, &g.LeaderKeyID, &g.LeaderSessionName, &g.LeaderOwnerKey, &mapped, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		g.AdditionsClosed = closed != 0
		g.LeaderConsoleMapped = mapped != 0
		g.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		g.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *SQLite) SetSessionMirror(ctx context.Context, keyID, sessionName string, enabled bool, mirrorTo string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_endpoint(key_id, session_name, full_session_name, owner_key, last_seen_at, active, mirror_enabled, mirror_to, client_ip, tool, model, producer, target_group, no_directory, no_directory_admin, no_directory_reported)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(key_id, session_name) DO UPDATE SET mirror_enabled=excluded.mirror_enabled, mirror_to=excluded.mirror_to`,
		keyID, sessionName, "", "", time.Now().UTC().Format(time.RFC3339), 0, boolInt(enabled), mirrorTo, "", "", "", 0, "", 0, 0, 0)
	return err
}

func (s *SQLite) SetSessionNoDirectory(ctx context.Context, keyID, sessionName string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_endpoint(key_id, session_name, full_session_name, owner_key, last_seen_at, active, mirror_enabled, mirror_to, client_ip, tool, model, producer, target_group, no_directory, no_directory_admin, no_directory_reported)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(key_id, session_name) DO UPDATE SET
		   no_directory_admin=excluded.no_directory_admin,
		   no_directory=CASE WHEN excluded.no_directory_admin=1 OR session_endpoint.no_directory_reported=1 THEN 1 ELSE 0 END`,
		keyID, sessionName, "", "", time.Now().UTC().Format(time.RFC3339), 0, 0, "", "", "", "", 0, "", boolInt(enabled), boolInt(enabled), 0)
	return err
}

func (s *SQLite) UpsertRegisteredService(ctx context.Context, svc model.RegisteredService) error {
	if svc.ID == "" {
		svc.ID = newID()
	}
	if svc.DeliveryType == "" {
		svc.DeliveryType = "ws"
	}
	if svc.ReplyMode == "" {
		svc.ReplyMode = "sync"
	}
	if svc.Priority == 0 {
		svc.Priority = 100
	}
	if svc.TimeoutMs == 0 {
		svc.TimeoutMs = 5000
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO registered_service(id, name, description, delivery_type, endpoint, secret_ref, reply_mode, priority, timeout_ms, retry, enabled, health_status, last_heartbeat)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, delivery_type=excluded.delivery_type, endpoint=excluded.endpoint, secret_ref=excluded.secret_ref, reply_mode=excluded.reply_mode, priority=excluded.priority, timeout_ms=excluded.timeout_ms, retry=excluded.retry, enabled=excluded.enabled, health_status=excluded.health_status, last_heartbeat=excluded.last_heartbeat`,
		svc.ID, svc.Name, svc.Description, svc.DeliveryType, svc.Endpoint, svc.SecretRef, svc.ReplyMode, svc.Priority, svc.TimeoutMs, svc.Retry, boolInt(svc.Enabled), svc.HealthStatus, timePtrString(svc.LastHeartbeatAt))
	return err
}

func (s *SQLite) ListRegisteredServices(ctx context.Context) ([]model.RegisteredService, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, delivery_type, endpoint, secret_ref, reply_mode, priority, timeout_ms, retry, enabled, health_status, last_heartbeat
		   FROM registered_service ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RegisteredService
	for rows.Next() {
		var svc model.RegisteredService
		var enabled int
		var last sql.NullString
		if err := rows.Scan(&svc.ID, &svc.Name, &svc.Description, &svc.DeliveryType, &svc.Endpoint, &svc.SecretRef, &svc.ReplyMode, &svc.Priority, &svc.TimeoutMs, &svc.Retry, &enabled, &svc.HealthStatus, &last); err != nil {
			return nil, err
		}
		svc.Enabled = enabled == 1
		if last.Valid {
			if t, err := time.Parse(time.RFC3339, last.String); err == nil {
				svc.LastHeartbeatAt = &t
			}
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

func (s *SQLite) DeleteRegisteredService(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM registered_service WHERE id=?`, id)
	return err
}

func (s *SQLite) InsertServiceAPIKey(ctx context.Context, key model.ServiceAPIKey) error {
	if key.ID == "" {
		key.ID = newID()
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO service_api_key(id, service_id, key_hash, label, active, created_at, revoked_at) VALUES(?,?,?,?,?,?,?)`,
		key.ID, key.ServiceID, key.KeyHash, key.Label, boolInt(key.Active), key.CreatedAt.UTC().Format(time.RFC3339), timePtrString(key.RevokedAt))
	return err
}

func (s *SQLite) ListServiceAPIKeys(ctx context.Context, serviceID string) ([]model.ServiceAPIKey, error) {
	query := `SELECT id, service_id, key_hash, label, active, created_at, revoked_at FROM service_api_key`
	args := []any{}
	if serviceID != "" {
		query += ` WHERE service_id=?`
		args = append(args, serviceID)
	}
	query += ` ORDER BY created_at DESC, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ServiceAPIKey
	for rows.Next() {
		var key model.ServiceAPIKey
		var created string
		var revoked sql.NullString
		var active int
		if err := rows.Scan(&key.ID, &key.ServiceID, &key.KeyHash, &key.Label, &active, &created, &revoked); err != nil {
			return nil, err
		}
		key.Active = active == 1
		key.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if revoked.Valid {
			if t, err := time.Parse(time.RFC3339, revoked.String); err == nil {
				key.RevokedAt = &t
			}
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (s *SQLite) ResolveServiceAPIKey(ctx context.Context, keyHash string) (*model.ServiceAPIKey, error) {
	var key model.ServiceAPIKey
	var created string
	var revoked sql.NullString
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, service_id, key_hash, label, active, created_at, revoked_at FROM service_api_key WHERE key_hash=? AND active=1`,
		keyHash).Scan(&key.ID, &key.ServiceID, &key.KeyHash, &key.Label, &active, &created, &revoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	key.Active = active == 1
	key.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if revoked.Valid {
		if t, err := time.Parse(time.RFC3339, revoked.String); err == nil {
			key.RevokedAt = &t
		}
	}
	return &key, nil
}

func (s *SQLite) ResolveServiceAPISecret(ctx context.Context, keyID, secretHash string) (*model.ServiceAPIKey, error) {
	var key model.ServiceAPIKey
	var created string
	var revoked sql.NullString
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, service_id, key_hash, label, active, created_at, revoked_at FROM service_api_key WHERE id=? AND key_hash=? AND active=1`,
		keyID, secretHash).Scan(&key.ID, &key.ServiceID, &key.KeyHash, &key.Label, &active, &created, &revoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	key.Active = active == 1
	key.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if revoked.Valid {
		if t, err := time.Parse(time.RFC3339, revoked.String); err == nil {
			key.RevokedAt = &t
		}
	}
	return &key, nil
}

func (s *SQLite) RevokeServiceAPIKey(ctx context.Context, keyID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE service_api_key SET active=0, revoked_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), keyID)
	return err
}

func (s *SQLite) BindAPIKeyAccount(ctx context.Context, keyID, chatEntityID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO api_key_account(api_key_id, chat_entity_id) VALUES(?,?)`,
		keyID, chatEntityID)
	return err
}

func (s *SQLite) ListAPIKeyAccounts(ctx context.Context, keyID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT chat_entity_id FROM api_key_account WHERE api_key_id=? ORDER BY chat_entity_id`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStrings(rows)
}

func (s *SQLite) ListAPIKeyAccountsForKeys(ctx context.Context, keyIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(keyIDs))
	if len(keyIDs) == 0 {
		return out, nil
	}
	seen := make(map[string]bool, len(keyIDs))
	args := make([]any, 0, len(keyIDs))
	placeholders := make([]string, 0, len(keyIDs))
	for _, keyID := range keyIDs {
		keyID = strings.TrimSpace(keyID)
		if keyID == "" || seen[keyID] {
			continue
		}
		seen[keyID] = true
		args = append(args, keyID)
		placeholders = append(placeholders, "?")
		out[keyID] = nil
	}
	if len(args) == 0 {
		return out, nil
	}
	query := `SELECT api_key_id, chat_entity_id FROM api_key_account WHERE api_key_id IN (` + strings.Join(placeholders, ",") + `) ORDER BY api_key_id, chat_entity_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var keyID, account string
		if err := rows.Scan(&keyID, &account); err != nil {
			return nil, err
		}
		out[keyID] = append(out[keyID], account)
	}
	return out, rows.Err()
}

func (s *SQLite) ListServiceBoundAccounts(ctx context.Context, serviceID string) ([]string, error) {
	if keyID, _, ok := sessionServiceParts(serviceID); ok {
		return s.ListAPIKeyAccounts(ctx, keyID)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT aka.chat_entity_id
		   FROM api_key_account aka
		   JOIN service_api_key k ON k.id=aka.api_key_id
		  WHERE k.service_id=? AND k.active=1
		  ORDER BY aka.chat_entity_id`,
		serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStrings(rows)
}

func (s *SQLite) CreateKeyApplication(ctx context.Context, app model.KeyApplication) (model.KeyApplication, error) {
	if app.ID == "" {
		app.ID = newID()
	}
	if app.Status == "" {
		app.Status = "pending"
	}
	if app.CreatedAt.IsZero() {
		app.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO key_application(
  id, applicant_open_id, applicant_account, applicant_bot_id, applicant_bot_name, description,
  status, approver_open_id, service_id, key_id, reject_reason, created_at, reviewed_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		app.ID, app.ApplicantOpenID, app.ApplicantAccount, app.ApplicantBotID, app.ApplicantBotName, app.Description,
		app.Status, nullString(app.ApproverOpenID), nullString(app.ServiceID), nullString(app.KeyID), nullString(app.RejectReason),
		app.CreatedAt.UTC().Format(time.RFC3339), timePtrString(app.ReviewedAt))
	return app, err
}

func (s *SQLite) GetKeyApplication(ctx context.Context, id string) (*model.KeyApplication, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, applicant_open_id, applicant_account, applicant_bot_id, applicant_bot_name, description,
  status, approver_open_id, service_id, key_id, reject_reason, created_at, reviewed_at
  FROM key_application WHERE id=?`, id)
	app, err := scanKeyApplication(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &app, nil
}

func (s *SQLite) ApproveKeyApplication(ctx context.Context, id, approverOpenID, serviceID, keyID string, reviewedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE key_application
SET status='approved', approver_open_id=?, service_id=?, key_id=?, reviewed_at=?
WHERE id=? AND status='pending'`,
		approverOpenID, serviceID, keyID, reviewedAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) ApproveKeyApplicationWithGrant(ctx context.Context, id, approverOpenID string, svc model.RegisteredService, key model.ServiceAPIKey, account string, entity model.ChatEntity, grant model.Message, reviewedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if svc.ID == "" || key.ID == "" || key.KeyHash == "" || account == "" || grant.ID == "" {
		return fmt.Errorf("approve key application requires service, key, account and grant message")
	}
	if svc.DeliveryType == "" {
		svc.DeliveryType = "ws"
	}
	if svc.ReplyMode == "" {
		svc.ReplyMode = "sync"
	}
	if svc.Priority == 0 {
		svc.Priority = 100
	}
	if svc.TimeoutMs == 0 {
		svc.TimeoutMs = 5000
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	if grant.Direction == "" {
		grant.Direction = model.DirectionOut
	}
	grant = s.prepareMessageForInsert(grant)

	res, err := tx.ExecContext(ctx, `UPDATE key_application
SET status='approved', approver_open_id=?, service_id=?, key_id=?, reviewed_at=?
WHERE id=? AND status='pending'`,
		approverOpenID, svc.ID, key.ID, reviewedAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO registered_service(id, name, description, delivery_type, endpoint, secret_ref, reply_mode, priority, timeout_ms, retry, enabled, health_status, last_heartbeat)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, delivery_type=excluded.delivery_type, endpoint=excluded.endpoint, secret_ref=excluded.secret_ref, reply_mode=excluded.reply_mode, priority=excluded.priority, timeout_ms=excluded.timeout_ms, retry=excluded.retry, enabled=excluded.enabled, health_status=excluded.health_status, last_heartbeat=excluded.last_heartbeat`,
		svc.ID, svc.Name, svc.Description, svc.DeliveryType, svc.Endpoint, svc.SecretRef, svc.ReplyMode, svc.Priority, svc.TimeoutMs, svc.Retry, boolInt(svc.Enabled), svc.HealthStatus, timePtrString(svc.LastHeartbeatAt)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO service_api_key(id, service_id, key_hash, label, active, created_at, revoked_at) VALUES(?,?,?,?,?,?,?)`,
		key.ID, key.ServiceID, key.KeyHash, key.Label, boolInt(key.Active), key.CreatedAt.UTC().Format(time.RFC3339), timePtrString(key.RevokedAt)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO api_key_account(api_key_id, chat_entity_id) VALUES(?,?)`,
		key.ID, account); err != nil {
		return err
	}
	if entity.ID != "" {
		if entity.Type == "" {
			entity.Type = model.ChatPersonal
		}
		if strings.TrimSpace(entity.BoundOwner) == "" {
			entity.BoundOwner = resolveChatEntityOwner(ctx, tx, entity.FeishuID)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO chat_entity(id, bot_channel_id, type, feishu_id, display_name, bound_owner, active)
			 VALUES(?,?,?,?,?,?,?)
			 ON CONFLICT(id) DO UPDATE SET bot_channel_id=excluded.bot_channel_id, type=excluded.type, feishu_id=excluded.feishu_id,
			   bound_owner=CASE WHEN excluded.bound_owner!='' THEN excluded.bound_owner ELSE chat_entity.bound_owner END,
			   active=excluded.active`,
			entity.ID, entity.BotChannelID, string(entity.Type), entity.FeishuID, entity.DisplayName, entity.BoundOwner, boolInt(entity.Active)); err != nil {
			return err
		}
	}
	var feishuMsgID any
	if grant.FeishuMsgID != "" {
		feishuMsgID = grant.FeishuMsgID
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO message(id, chat_entity_id, direction, bot_channel_id, feishu_msg_id, chat_type, sender_open_id, ingress_provenance, content_json, status, attempts, error, created_at, processed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)`,
		grant.ID, grant.ChatEntityID, string(grant.Direction), grant.BotChannelID, feishuMsgID, string(grant.ChatType), grant.SenderOpenID, string(model.NormalizeIngressProvenance(grant.IngressProvenance)), grant.Content, grant.Status, grant.Attempts, grant.Err, grant.CreatedAt.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) RejectKeyApplication(ctx context.Context, id, approverOpenID, reason string, reviewedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE key_application
SET status='rejected', approver_open_id=?, reject_reason=?, reviewed_at=?
WHERE id=? AND status='pending'`,
		approverOpenID, reason, reviewedAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func sessionServiceParts(serviceID string) (keyID string, sessionName string, ok bool) {
	if !strings.HasPrefix(serviceID, "session:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(serviceID, "session:")
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *SQLite) InsertRoutingRule(ctx context.Context, rule model.RoutingRule) error {
	if rule.ID == "" {
		rule.ID = newID()
	}
	if rule.MatchType == "" {
		rule.MatchType = "prefix"
	}
	if rule.Combine == "" {
		rule.Combine = "or"
	}
	if rule.Priority == 0 {
		rule.Priority = 100
	}
	if rule.AccountScopeJSON != "" {
		var scoped []string
		if err := json.Unmarshal([]byte(rule.AccountScopeJSON), &scoped); err != nil {
			return err
		}
		bound, err := s.ListServiceBoundAccounts(ctx, rule.ServiceID)
		if err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, id := range bound {
			allowed[id] = true
		}
		for _, id := range scoped {
			if !allowed[id] {
				return fmt.Errorf("account scope %s is not bound to service", id)
			}
		}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO routing_rule(id, service_id, match_type, match_expr, combine, priority, scope_entity_type, account_scope_json, case_sensitive, strip_prefix, enabled)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		rule.ID, rule.ServiceID, rule.MatchType, rule.MatchExpr, rule.Combine, rule.Priority, rule.ScopeEntityType, rule.AccountScopeJSON, boolInt(rule.CaseSensitive), boolInt(rule.StripPrefix), boolInt(rule.Enabled))
	return err
}

func (s *SQLite) DeleteRoutingRule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM routing_rule WHERE id=?`, id)
	return err
}

func (s *SQLite) ListAllPrefixRoutes(ctx context.Context) ([]model.PrefixRoute, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.service_id, r.match_type, r.match_expr, r.combine, r.priority, r.scope_entity_type, r.account_scope_json, r.case_sensitive, r.strip_prefix, r.enabled,
		        s.id, s.name, s.description, s.delivery_type, s.endpoint, s.secret_ref, s.reply_mode, s.priority, s.timeout_ms, s.retry, s.enabled, s.health_status, s.last_heartbeat
		   FROM routing_rule r
		   JOIN registered_service s ON s.id=r.service_id
		  WHERE r.enabled=1 AND s.enabled=1 AND r.match_type='prefix'
		  ORDER BY r.priority ASC, r.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []model.PrefixRoute
	for rows.Next() {
		var route model.PrefixRoute
		var rCase, rStrip, rEnabled, sEnabled int
		var lastHeartbeat sql.NullString
		if err := rows.Scan(
			&route.Rule.ID, &route.Rule.ServiceID, &route.Rule.MatchType, &route.Rule.MatchExpr, &route.Rule.Combine, &route.Rule.Priority, &route.Rule.ScopeEntityType, &route.Rule.AccountScopeJSON, &rCase, &rStrip, &rEnabled,
			&route.Service.ID, &route.Service.Name, &route.Service.Description, &route.Service.DeliveryType, &route.Service.Endpoint, &route.Service.SecretRef, &route.Service.ReplyMode, &route.Service.Priority, &route.Service.TimeoutMs, &route.Service.Retry, &sEnabled, &route.Service.HealthStatus, &lastHeartbeat,
		); err != nil {
			return nil, err
		}
		route.Rule.CaseSensitive = rCase == 1
		route.Rule.StripPrefix = rStrip == 1
		route.Rule.Enabled = rEnabled == 1
		route.Service.Enabled = sEnabled == 1
		if lastHeartbeat.Valid {
			if t, err := time.Parse(time.RFC3339, lastHeartbeat.String); err == nil {
				route.Service.LastHeartbeatAt = &t
			}
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return routes, nil
}

func (s *SQLite) ListPrefixRoutes(ctx context.Context, chatEntityID string) ([]model.PrefixRoute, error) {
	routes, err := s.ListAllPrefixRoutes(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.PrefixRoute
	boundCache := map[string][]string{}
	for _, route := range routes {
		bound, ok := boundCache[route.Service.ID]
		if !ok {
			var err error
			bound, err = s.ListServiceBoundAccounts(ctx, route.Service.ID)
			if err != nil {
				return nil, err
			}
			boundCache[route.Service.ID] = bound
		}
		if routeAllowedForEntity(route.Rule, chatEntityID, bound) {
			out = append(out, route)
		}
	}
	return out, nil
}

func routeAllowedForEntity(rule model.RoutingRule, chatEntityID string, bound []string) bool {
	boundSet := map[string]bool{}
	for _, id := range bound {
		boundSet[id] = true
	}
	if rule.AccountScopeJSON == "" {
		return boundSet[chatEntityID]
	}
	var scoped []string
	if err := json.Unmarshal([]byte(rule.AccountScopeJSON), &scoped); err != nil {
		return false
	}
	for _, id := range scoped {
		if id == chatEntityID && boundSet[id] {
			return true
		}
	}
	return false
}

func (s *SQLite) UpsertSystemService(ctx context.Context, svc model.SystemService) error {
	if svc.Delivery == "" {
		svc.Delivery = "internal"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_service(name, description, delivery, endpoint, active)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET description=excluded.description, delivery=excluded.delivery, endpoint=excluded.endpoint, active=excluded.active`,
		svc.Name, svc.Description, svc.Delivery, svc.Endpoint, boolInt(svc.Active))
	return err
}

func (s *SQLite) UpsertSystemRoute(ctx context.Context, route model.SystemRoute) error {
	if route.Priority == 0 {
		route.Priority = 10
	}
	if route.Action == "" {
		route.Action = "auto"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_route(keyword, service_name, action, priority, active)
		 VALUES(?,?,?,?,?)
		 ON CONFLICT(keyword) DO UPDATE SET service_name=excluded.service_name, action=excluded.action, priority=excluded.priority, active=excluded.active`,
		route.Keyword, route.ServiceName, route.Action, route.Priority, boolInt(route.Active))
	return err
}

func (s *SQLite) DeleteSystemRoute(ctx context.Context, keyword string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM system_route WHERE keyword=?`, keyword)
	return err
}

func (s *SQLite) ListSystemRoutes(ctx context.Context) ([]model.SystemRoute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT keyword, service_name, action, priority, active FROM system_route WHERE active=1 ORDER BY priority ASC, keyword ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SystemRoute
	for rows.Next() {
		var route model.SystemRoute
		var active int
		if err := rows.Scan(&route.Keyword, &route.ServiceName, &route.Action, &route.Priority, &active); err != nil {
			return nil, err
		}
		if route.Action == "" {
			route.Action = "auto"
		}
		route.Active = active == 1
		out = append(out, route)
	}
	return out, rows.Err()
}

func (s *SQLite) GetSystemService(ctx context.Context, name string) (*model.SystemService, error) {
	var svc model.SystemService
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT name, description, delivery, endpoint, active FROM system_service WHERE name=? AND active=1`, name).
		Scan(&svc.Name, &svc.Description, &svc.Delivery, &svc.Endpoint, &active)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	svc.Active = active == 1
	return &svc, nil
}

func timePtrString(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func scanKeyApplication(row rowScanner) (model.KeyApplication, error) {
	var app model.KeyApplication
	var approver, serviceID, keyID, rejectReason, reviewed sql.NullString
	var created string
	err := row.Scan(&app.ID, &app.ApplicantOpenID, &app.ApplicantAccount, &app.ApplicantBotID, &app.ApplicantBotName, &app.Description,
		&app.Status, &approver, &serviceID, &keyID, &rejectReason, &created, &reviewed)
	if err != nil {
		return app, err
	}
	app.ApproverOpenID = nullableString(approver)
	app.ServiceID = nullableString(serviceID)
	app.KeyID = nullableString(keyID)
	app.RejectReason = nullableString(rejectReason)
	app.CreatedAt, _ = time.Parse(time.RFC3339, created)
	app.ReviewedAt = parseTimePtr(reviewed)
	return app, nil
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
