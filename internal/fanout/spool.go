package fanout

// 出站托管 spool(设计 v2.3 §C/§D):PG 持久队列,202 仅在事务提交
// (synchronous_commit=on)之后;幂等键=(caller_id,request_id)唯一+body_sha256;状态机
// queued→sending→sent|dlq。入站 sink 投递(段2)复用本存储。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

var ErrBodyConflict = errors.New("request_id exists with different body hash")

// PGSchemaSQL 本包五表的建表语句（幂等）。迁移账本据此登记冻结副本；OpenSpool 也用它建表，
// 测试用它在新库上建形状 —— 三处同一份字节，故「源 ↔ 账本」漂移能被双轨格判出来。
// 🔴 表结构只改 pgschema.sql，🔴 不要把 DDL 再写回 Go 字符串里（那正是本件收口之前的形状：
// 五表分散在 spool.go／inspool.go／admin.go 三处，任一处改了账本都看不见）。
//
//go:embed pgschema.sql
var PGSchemaSQL string

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
func ftx() repo.AccessCtx {
	return repo.PendingCtx("fanout", "SP7b", "fanout 出站/入站表尚无双轴列,待领域 schema 落地")
}

// OpenSpool 在调用方注入的 PG 池上初始化 spool。池的创建与生命周期属于装配层；
// 本包不接收 DSN，也不保留 SQLite 兼容入口。
func OpenSpool(pool *repo.Pool) (*Spool, error) {
	if pool == nil {
		return nil, errors.New("fanout spool: nil PG pool")
	}
	ctx := context.Background()
	// 建表是 DDL，本就无租户语义 -> 显式 SystemCtx，不用 ftx。
	// 五表一次建齐（原先 outbound 在这里、入站三表在 initInbound、selfcheck 在 SelfCheck 里懒建）：
	// 全部 CREATE TABLE IF NOT EXISTS ⇒ 幂等，重复 OpenSpool 与「账本已建过」两种先前状态都吃得下。
	// 🔴 无参多语句走简单协议（口径页 §五；先例＝identity.PGSchemaSQL、hosted.ApplySchema）。
	if _, err := pool.Exec(ctx, repo.SystemCtx(), PGSchemaSQL); err != nil {
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
  VALUES($1,$2,$3,$4,$5,'queued',0,$6,$7)`, id, callerID, requestID, bodyHash, body, now, now)
	if err == nil {
		return id, false, nil
	}
	// 冲突路:读回既有行判 hash
	var haveHash string
	row, qErr := s.db.QueryRow(ctx, ftx(),
		`SELECT body_hash FROM outbound WHERE caller_id=$1 AND request_id=$2`, callerID, requestID)
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
  FROM outbound WHERE status IN('queued','sending') AND next_retry<=$1 ORDER BY created_at LIMIT 1`,
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
		`UPDATE outbound SET status='sending', updated_at=$1 WHERE outbound_id=$2`,
		now.UTC().Unix(), j.OutboundID)
	return j, err
}

func (s *Spool) MarkSent(ctx context.Context, outboundID, larkMsgID string) error {
	_, err := s.db.Exec(ctx, ftx(), `UPDATE outbound SET status='sent', lark_msg_id=$1, last_error='',
  updated_at=$2 WHERE outbound_id=$3`, larkMsgID, time.Now().UTC().Unix(), outboundID)
	return err
}

// MarkRetry 失败退避;attempts 达上限=入 DLQ(terminal,告警面可见)。
func (s *Spool) MarkRetry(ctx context.Context, outboundID, sendErr string, attempts, maxAttempts int, backoff time.Duration) (dlq bool, err error) {
	now := time.Now().UTC()
	if attempts+1 >= maxAttempts {
		_, err = s.db.Exec(ctx, ftx(), `UPDATE outbound SET status='dlq', attempts=$1, last_error=$2,
  updated_at=$3 WHERE outbound_id=$4`, attempts+1, sendErr, now.Unix(), outboundID)
		return true, err
	}
	_, err = s.db.Exec(ctx, ftx(), `UPDATE outbound SET status='queued', attempts=$1, last_error=$2,
  next_retry=$3, updated_at=$4 WHERE outbound_id=$5`,
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
  FROM outbound WHERE outbound_id=$1 AND caller_id=$2`, outboundID, callerID)
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
		`SELECT COUNT(*) FROM outbound WHERE status=$1`, status)
	if qErr != nil {
		return 0, qErr
	}
	err := row.Scan(&n)
	return n, err
}
