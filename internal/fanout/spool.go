package fanout

// 出站托管 spool(设计 v2.3 §C/§D):WAL 持久队列,202 仅在事务提交(synchronous=FULL,
// 提交即 fsync)之后;幂等键=(caller_id,request_id)唯一+body_sha256;状态机
// queued→sending→sent|dlq。入站 sink 投递(段2)复用本存储。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

var ErrBodyConflict = errors.New("request_id exists with different body hash")

type OutboundJob struct {
	OutboundID string
	CallerID   string
	RequestID  string
	BodyHash   string
	Body       []byte
	Status     string // queued|sending|sent|dlq
	Attempts   int
	NextRetry  time.Time
	LarkMsgID  string
	LastError  string
}

type Spool struct {
	db *repo.Pool
}

// ftx 是 fanout 【尚未租户化】的统一占位上下文（同 datad 的 dtx）。
//
// 🔴 不要换成 repo.SystemCtx()：SystemCtx 表示「本就与租户无关」，
//
//	而出站队列里装的是租户数据，它只是【还没有租户列】可过滤（领域 schema 归 SP7b）。
//	混用会用「与租户无关」冒充「已做隔离」。保留占位是为了让这笔债可清点——由 specs/tools/pending-debt 按效果发现并计数，不按名字判（SA-12b）。
func ftx() repo.TenantCtx {
	return repo.PendingTenantCtx("fanout", "SP7b", "fanout 出站/入站表尚无租户列,待 SP7b 领域 schema 落地")
}

// OpenSpool 打开/初始化 spool 库。synchronous=FULL:COMMIT 返回=已 fsync,
// 202 的耐久承诺(§C2)由此成立。
func OpenSpool(path string) (*Spool, error) {
	ctx := context.Background()
	// 🔴 SetMaxOpenConns(1) 必须保留：原注释「面板自死锁教训:单连接串行,杜绝游标互等」。
	//    收编不得顺手抹掉用事故换来的并发语义（§4.5）。dsn 里的 pragma 亦原样保留。
	pool, err := repo.OpenSQLite(ctx,
		"file:"+path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)",
		repo.MaxOpenConns(1))
	if err != nil {
		return nil, err
	}
	// 建表是 DDL，本就无租户语义 -> 显式 SystemCtx，不用 ftx。
	if _, err := pool.Exec(ctx, repo.SystemCtx(), `CREATE TABLE IF NOT EXISTS outbound(
  outbound_id TEXT PRIMARY KEY,
  caller_id   TEXT NOT NULL,
  request_id  TEXT NOT NULL,
  body_hash   TEXT NOT NULL,
  body        BLOB NOT NULL,
  status      TEXT NOT NULL DEFAULT 'queued',
  attempts    INTEGER NOT NULL DEFAULT 0,
  next_retry  INTEGER NOT NULL DEFAULT 0,
  lark_msg_id TEXT NOT NULL DEFAULT '',
  last_error  TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  UNIQUE(caller_id, request_id))`); err != nil {
		_ = pool.Close()
		return nil, err
	}
	return &Spool{db: pool}, nil
}

func (s *Spool) Close() error { return s.db.Close() }

// OutboundIDFor 确定性 outbound_id:重启/重放恒同,审计三点关联稳定。
func OutboundIDFor(callerID, requestID string) string {
	sum := sha256.Sum256([]byte("t17ob\x1f" + callerID + "\x1f" + requestID))
	return "ob_" + hex.EncodeToString(sum[:12])
}

// Accept 幂等收件:新键=插入(队列态)返回 (id,false,nil);同键同 hash=返回既有
// (id,true,nil);同键异 hash=ErrBodyConflict(409)。COMMIT 返回即已落盘。
func (s *Spool) Accept(ctx context.Context, callerID, requestID string, body []byte) (string, bool, error) {
	bodyHash := hex.EncodeToString(func() []byte { h := sha256.Sum256(body); return h[:] }())
	id := OutboundIDFor(callerID, requestID)
	now := time.Now().UTC().Unix()
	_, err := s.db.Exec(ctx, ftx(), `INSERT INTO outbound
  (outbound_id, caller_id, request_id, body_hash, body, status, next_retry, created_at, updated_at)
  VALUES(?,?,?,?,?,'queued',0,?,?)`, id, callerID, requestID, bodyHash, body, now, now)
	if err == nil {
		return id, false, nil
	}
	// 冲突路:读回既有行判 hash
	var haveHash string
	row, qErr := s.db.QueryRow(ctx, ftx(),
		`SELECT body_hash FROM outbound WHERE caller_id=? AND request_id=?`, callerID, requestID)
	if qErr != nil {
		return "", false, fmt.Errorf("accept: %w (lookup: %v)", err, qErr)
	}
	if scanErr := row.Scan(&haveHash); scanErr != nil {
		return "", false, fmt.Errorf("accept: %w (lookup: %v)", err, scanErr)
	}
	if haveHash != bodyHash {
		return "", false, ErrBodyConflict
	}
	return id, true, nil
}

// NextDue 取一件到期待发件并置 sending(单 worker 语义)。无件=nil。
func (s *Spool) NextDue(ctx context.Context, now time.Time) (*OutboundJob, error) {
	row, qErr := s.db.QueryRow(ctx, ftx(), `SELECT outbound_id, caller_id, request_id, body, attempts
  FROM outbound WHERE status IN('queued','sending') AND next_retry<=? ORDER BY created_at LIMIT 1`,
		now.UTC().Unix())
	if qErr != nil {
		return nil, qErr
	}
	j := &OutboundJob{}
	if err := row.Scan(&j.OutboundID, &j.CallerID, &j.RequestID, &j.Body, &j.Attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_, err := s.db.Exec(ctx, ftx(),
		`UPDATE outbound SET status='sending', updated_at=? WHERE outbound_id=?`,
		now.UTC().Unix(), j.OutboundID)
	return j, err
}

func (s *Spool) MarkSent(ctx context.Context, outboundID, larkMsgID string) error {
	_, err := s.db.Exec(ctx, ftx(), `UPDATE outbound SET status='sent', lark_msg_id=?, last_error='',
  updated_at=? WHERE outbound_id=?`, larkMsgID, time.Now().UTC().Unix(), outboundID)
	return err
}

// MarkRetry 失败退避;attempts 达上限=入 DLQ(terminal,告警面可见)。
func (s *Spool) MarkRetry(ctx context.Context, outboundID, sendErr string, attempts, maxAttempts int, backoff time.Duration) (dlq bool, err error) {
	now := time.Now().UTC()
	if attempts+1 >= maxAttempts {
		_, err = s.db.Exec(ctx, ftx(), `UPDATE outbound SET status='dlq', attempts=?, last_error=?,
  updated_at=? WHERE outbound_id=?`, attempts+1, sendErr, now.Unix(), outboundID)
		return true, err
	}
	_, err = s.db.Exec(ctx, ftx(), `UPDATE outbound SET status='queued', attempts=?, last_error=?,
  next_retry=?, updated_at=? WHERE outbound_id=?`,
		attempts+1, sendErr, now.Add(backoff).Unix(), now.Unix(), outboundID)
	return false, err
}

// RecoverSending 启动恢复:进程崩溃残留的 sending 归队(at-least-once,Hub 幂等吸收)。
func (s *Spool) RecoverSending(ctx context.Context) (int64, error) {
	res, err := s.db.Exec(ctx, ftx(),
		`UPDATE outbound SET status='queued' WHERE status='sending'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Status K3:仅本 caller 可查。
func (s *Spool) Status(ctx context.Context, callerID, outboundID string) (*OutboundJob, error) {
	row, qErr := s.db.QueryRow(ctx, ftx(), `SELECT outbound_id, status, attempts, lark_msg_id, last_error
  FROM outbound WHERE outbound_id=? AND caller_id=?`, outboundID, callerID)
	if qErr != nil {
		return nil, qErr
	}
	j := &OutboundJob{}
	if err := row.Scan(&j.OutboundID, &j.Status, &j.Attempts, &j.LarkMsgID, &j.LastError); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return j, nil
}

func (s *Spool) CountByStatus(ctx context.Context, status string) (int64, error) {
	var n int64
	row, qErr := s.db.QueryRow(ctx, ftx(),
		`SELECT COUNT(*) FROM outbound WHERE status=?`, status)
	if qErr != nil {
		return 0, qErr
	}
	err := row.Scan(&n)
	return n, err
}
