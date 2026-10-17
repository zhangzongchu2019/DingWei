package hosted

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

type QuotaNotice struct {
	NodeID, EventID, Recipient, SeatID, Reason, WriterKind string
	MeasuredAt                                             time.Time
}

func (n QuotaNotice) DeliveryID() string {
	h := sha256.Sum256([]byte(n.NodeID + ":" + n.EventID + ":" + n.Recipient))
	return "quota-notice-" + hex.EncodeToString(h[:])
}
func (n QuotaNotice) Text() string {
	reason := map[string]string{
		"usage_warning": "用量接近告警线", "disk_warning": "磁盘用量接近告警线", "disk_limit": "用量达到上限，工作已暂停或正在停止，数据保留",
		"usage_unknown": "用量读数持续未知，工作已暂停或正在停止，数据保留", "private_state_unavailable": "私有状态不可用，禁止继续写入",
		"unclean_restart": "异常退出后的保护性暂停", "node_space_emergency": "节点可用空间低于紧急线，工作已暂停或正在停止，数据保留",
		"node_inode_emergency": "节点可用 inode 低于紧急线，工作已暂停或正在停止，数据保留", "node_space_unknown": "节点余量持续未知，工作已暂停或正在停止，数据保留",
	}[n.Reason]
	return fmt.Sprintf("【DingWei 托管磁盘通知】节点 %s，席位 %s：%s。无硬磁盘配额；请查看只读监控中的实时用量、阈值与停止状态。prodops 负责监控并通知业主，只有业主本人能解除 hold，不会自动解除或启动。事件 %s，采样时间 %s；无需回复。", n.NodeID, n.SeatID, reason, n.EventID, n.MeasuredAt.UTC().Format(time.RFC3339))
}

// One bounded batch per tick. Delivery means durable Hub enqueue, not human read.
// Stable per-recipient IDs make a crash between enqueue and receipt idempotent.
// Failed recipients stay pending independently; no raw error is persisted.
func (s *Store) DeliverQuotaNotices(ctx context.Context, deliver func(context.Context, QuotaNotice) error) error {
	rows, err := s.query(ctx, `SELECT node_id,event_id,recipient,seat_id,reason,writer_kind,measured_at FROM g4_quota_notice WHERE delivered_at IS NULL AND next_attempt_at<=now() ORDER BY measured_at,node_id,event_id,recipient LIMIT 32`)
	if err != nil {
		return err
	}
	var notices []QuotaNotice
	for rows.Next() {
		var n QuotaNotice
		if err = rows.Scan(&n.NodeID, &n.EventID, &n.Recipient, &n.SeatID, &n.Reason, &n.WriterKind, &n.MeasuredAt); err != nil {
			rows.Close()
			return err
		}
		notices = append(notices, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, n := range notices {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !knownQuotaReason(n.Reason) || !nodeSeatID.MatchString(n.SeatID) || !quotaEventIDPattern.MatchString(n.EventID) {
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = deliver(dctx, n)
		cancel()
		if err != nil {
			if _, updateErr := s.exec(ctx, `UPDATE g4_quota_notice SET next_attempt_at=now()+interval '30 seconds' WHERE node_id=$1 AND event_id=$2 AND recipient=$3 AND delivered_at IS NULL`, n.NodeID, n.EventID, n.Recipient); updateErr != nil {
				return updateErr
			}
			continue
		}
		if _, err = s.exec(ctx, `UPDATE g4_quota_notice SET delivered_at=now() WHERE node_id=$1 AND event_id=$2 AND recipient=$3 AND delivered_at IS NULL`, n.NodeID, n.EventID, n.Recipient); err != nil {
			return err
		}
	}
	return nil
}
