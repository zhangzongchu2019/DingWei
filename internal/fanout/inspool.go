package fanout

// 入站 sink 投递存储(设计 v2.3 §B/§D):每消息一主记录+每 sink 一状态行;
// "已接管"判定=仅看本表 WAL 提交(AcceptInbound 返回 nil 才允许向 SDK 返回成功);
// 全局终态 delivered | delivered_with_waiver | blocked_dlq(§B/T2)。

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// sink 行状态:queued|delivered|dlq|audited_waived|dropped(besteffort 到界弃)
// 主记录状态:pending|delivered|delivered_with_waiver|blocked_dlq

type InboundSinkJob struct {
	MsgKey   string
	SinkName string
	Role     string
	Body     []byte
	Attempts int
}

func (s *Spool) initInbound() error {
	// 建表是 DDL,本就无租户语义 -> 显式 SystemCtx。
	_, err := s.db.Exec(context.Background(), repo.SystemCtx(), `CREATE TABLE IF NOT EXISTS inbound_msg(
  msg_key    TEXT PRIMARY KEY,
  body       BYTEA NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS inbound_sink(
  msg_key    TEXT NOT NULL,
  sink_name  TEXT NOT NULL,
  role       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'queued',
  attempts   INTEGER NOT NULL DEFAULT 0,
  next_retry INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(msg_key, sink_name));
CREATE TABLE IF NOT EXISTS waive_audit(
  msg_key   TEXT NOT NULL,
  sink_name TEXT NOT NULL,
  actor     TEXT NOT NULL,
  reason    TEXT NOT NULL,
  at        INTEGER NOT NULL)`)
	return err
}

// AcceptInbound 接管落盘:主记录+各 sink 行单事务提交(COMMIT=fsync)。
// 幂等:同 msg_key 二投=已在账,返回 (false,nil) 不重复建行(at-least-once 吸收)。
func (s *Spool) AcceptInbound(ctx context.Context, msgKey string, body []byte, sinks []SinkTarget) (fresh bool, err error) {
	now := time.Now().UTC().Unix()
	// 事务改回调式(repo.InTx):*sql.Tx 因此不出仓储包。语义逐条对应 ——
	// fn 返回 nil 即提交(含「重复投递:幂等吸收」那条原本也是 Commit 的路径),返回错误即回滚。
	fresh = false
	err = s.db.InTx(ctx, ftx(), nil, func(tx repo.Tx) error {
		res, err := tx.Exec(ctx, `INSERT INTO inbound_msg
  (msg_key, body, status, created_at, updated_at) VALUES($1,$2,'pending',$3,$4)
  ON CONFLICT (msg_key) DO NOTHING`,
			msgKey, body, now, now)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil // 重复投递:账已在,幂等吸收 —— fresh 保持 false 并提交
		}
		for _, t := range sinks {
			if _, err := tx.Exec(ctx, `INSERT INTO inbound_sink
  (msg_key, sink_name, role, status, updated_at) VALUES($1,$2,$3,'queued',$4)`,
				msgKey, t.Name, t.Role, now); err != nil {
				return err
			}
		}
		if len(sinks) == 0 { // 无目标(如纯白名单消息无命中)=直接终态
			if _, err := tx.Exec(ctx,
				`UPDATE inbound_msg SET status='delivered', updated_at=$1 WHERE msg_key=$2`,
				now, msgKey); err != nil {
				return err
			}
		}
		fresh = true
		return nil
	})
	return fresh, err
}

// NextDueInbound 取一件到期 sink 投递对(独立游标:不同 sink/消息互不 HOL)。
func (s *Spool) NextDueInbound(ctx context.Context, now time.Time) (*InboundSinkJob, error) {
	row, qErr := s.db.QueryRow(ctx, ftx(), `SELECT k.msg_key, k.sink_name, k.role, m.body, k.attempts
  FROM inbound_sink k JOIN inbound_msg m ON m.msg_key=k.msg_key
  WHERE k.status='queued' AND k.next_retry<=$1 ORDER BY k.updated_at LIMIT 1`, now.UTC().Unix())
	if qErr != nil {
		return nil, qErr
	}
	j := &InboundSinkJob{}
	if err := row.Scan(&j.MsgKey, &j.SinkName, &j.Role, &j.Body, &j.Attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// 乐观置忙:next_retry 前挪防同轮重取(单 worker 下等效)
	_, err := s.db.Exec(ctx, ftx(), `UPDATE inbound_sink SET next_retry=$1, updated_at=$2
  WHERE msg_key=$3 AND sink_name=$4`, now.Add(time.Minute).UTC().Unix(), now.UTC().Unix(),
		j.MsgKey, j.SinkName)
	return j, err
}

func (s *Spool) MarkSinkDelivered(ctx context.Context, msgKey, sinkName string) error {
	if _, err := s.db.Exec(ctx, ftx(), `UPDATE inbound_sink SET status='delivered', last_error='',
  updated_at=$1 WHERE msg_key=$2 AND sink_name=$3`,
		time.Now().UTC().Unix(), msgKey, sinkName); err != nil {
		return err
	}
	return s.recomputeGlobal(ctx, msgKey)
}

// MarkSinkFailed 失败处置:authoritative 到界=dlq(→blocked);besteffort 到界=dropped(弃,不入告警级)。
// poison=true(4xx 毒件)立即终态,不重试。
func (s *Spool) MarkSinkFailed(ctx context.Context, j *InboundSinkJob, sendErr string, maxAttempts int, backoff time.Duration, poison bool) (terminal string, err error) {
	now := time.Now().UTC()
	att := j.Attempts + 1
	if poison || att >= maxAttempts {
		terminal = "dlq"
		if j.Role != "authoritative" {
			terminal = "dropped"
		}
		_, err = s.db.Exec(ctx, ftx(), `UPDATE inbound_sink SET status=$1, attempts=$2, last_error=$3,
  updated_at=$4 WHERE msg_key=$5 AND sink_name=$6`,
			terminal, att, sendErr, now.Unix(), j.MsgKey, j.SinkName)
		if err != nil {
			return "", err
		}
		return terminal, s.recomputeGlobal(ctx, j.MsgKey)
	}
	_, err = s.db.Exec(ctx, ftx(), `UPDATE inbound_sink SET status='queued', attempts=$1, last_error=$2,
  next_retry=$3, updated_at=$4 WHERE msg_key=$5 AND sink_name=$6`,
		att, sendErr, now.Add(backoff).Unix(), now.Unix(), j.MsgKey, j.SinkName)
	return "", err
}

// WaiveSink T2:仅 admin 经管理 API 调用;dlq→audited_waived+审计行+全局重算。
func (s *Spool) WaiveSink(ctx context.Context, msgKey, sinkName, actor, reason string) error {
	res, err := s.db.Exec(ctx, ftx(), `UPDATE inbound_sink SET status='audited_waived', updated_at=$1
  WHERE msg_key=$2 AND sink_name=$3 AND status='dlq'`,
		time.Now().UTC().Unix(), msgKey, sinkName)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("no dlq row to waive")
	}
	if _, err := s.db.Exec(ctx, ftx(), `INSERT INTO waive_audit(msg_key,sink_name,actor,reason,at)
  VALUES($1,$2,$3,$4,$5)`, msgKey, sinkName, actor, reason, time.Now().UTC().Unix()); err != nil {
		return err
	}
	return s.recomputeGlobal(ctx, msgKey)
}

// recomputeGlobal §B/T2 全局终态:delivered=全部 auth delivered;
// delivered_with_waiver=全部 auth∈{delivered,audited_waived}且≥1 waived;
// blocked_dlq=存在 auth dlq 未 waive;否则 pending。besteffort 不参与。
func (s *Spool) recomputeGlobal(ctx context.Context, msgKey string) error {
	rows, err := s.db.Query(ctx, ftx(),
		`SELECT status FROM inbound_sink WHERE msg_key=$1 AND role='authoritative'`, msgKey)
	if err != nil {
		return err
	}
	var total, delivered, waived, dlq int
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			rows.Close()
			return err
		}
		total++
		switch st {
		case "delivered":
			delivered++
		case "audited_waived":
			waived++
		case "dlq":
			dlq++
		}
	}
	rows.Close()
	global := "pending"
	switch {
	case dlq > 0:
		global = "blocked_dlq"
	case total > 0 && delivered == total:
		global = "delivered"
	case total > 0 && delivered+waived == total && waived > 0:
		global = "delivered_with_waiver"
	}
	_, err = s.db.Exec(ctx, ftx(), `UPDATE inbound_msg SET status=$1, updated_at=$2 WHERE msg_key=$3`,
		global, time.Now().UTC().Unix(), msgKey)
	return err
}

func (s *Spool) InboundGlobalStatus(ctx context.Context, msgKey string) (string, error) {
	var st string
	row, qErr := s.db.QueryRow(ctx, ftx(),
		`SELECT status FROM inbound_msg WHERE msg_key=$1`, msgKey)
	if qErr != nil {
		return "", qErr
	}
	err := row.Scan(&st)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return st, err
}

// CountBlocked healthz degraded 判据(§B:blocked_dlq>0=degraded)。
func (s *Spool) CountBlocked(ctx context.Context) (int64, error) {
	var n int64
	row, qErr := s.db.QueryRow(ctx, ftx(),
		`SELECT COUNT(*) FROM inbound_msg WHERE status='blocked_dlq'`)
	if qErr != nil {
		return 0, qErr
	}
	err := row.Scan(&n)
	return n, err
}
