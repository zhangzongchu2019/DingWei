package extseat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// PGFeishuCopyQueue 在主消息事务中原子记录飞书只读副本。
type PGFeishuCopyQueue struct {
	Pool   *repo.Pool
	Access repo.AccessCtx
}

type FeishuCopyReceipt struct{ BotChannelID, MessageID string }
type FeishuCopySender func(context.Context, FeishuCopyEvent) (FeishuCopyReceipt, error)

const FeishuCopyMaxBytes = 20000

// FeishuCopyText 构造只读副本；长度限制只应用于该副本，调用方仍持久化原始正文。
func FeishuCopyText(event FeishuCopyEvent) string {
	label := "外部 MCP Agent 消息"
	if event.Direction == FeishuCopyInternalToExternal {
		label = "内部目标回复"
	}
	header := extshared.ExternalSenderHeader(event.ExternalName, event.OwnerDisplayName, event.OwnerShortName, event.TargetName)
	prefix := header + "\n【只读提醒】飞书回复不会转发到 MCP。\n【" + label + "】\n"
	const suffix = "\n……（飞书抄送已截断；原消息未截断）"
	if len(prefix)+len(suffix) >= FeishuCopyMaxBytes {
		prefix = trimUTF8(prefix, FeishuCopyMaxBytes-len(suffix))
	}
	remaining := FeishuCopyMaxBytes - len(prefix)
	if len(event.Body) <= remaining {
		return prefix + event.Body
	}
	bodyBudget := remaining - len(suffix)
	if bodyBudget < 0 {
		bodyBudget = 0
	}
	var out strings.Builder
	out.WriteString(prefix)
	out.WriteString(trimUTF8(event.Body, bodyBudget))
	out.WriteString(suffix)
	return out.String()
}

func trimUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && !utf8.RuneStart(value[maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}

func (q PGFeishuCopyQueue) Enqueue(ctx context.Context, tx repo.Tx, event FeishuCopyEvent) error {
	if event.ID == "" || event.SeatID == "" || event.Issuer == "" || event.Subject == "" || event.ExternalName == "" || event.TargetName == "" || event.Body == "" || (event.Direction != FeishuCopyExternalToInternal && event.Direction != FeishuCopyInternalToExternal) {
		return extshared.ErrNotPermitted
	}
	_, err := tx.Exec(ctx, `INSERT INTO np_extseat_feishu_copy(event_id,seat_id,principal_issuer,principal_subject,external_name,target_name,owner_display_name,owner_short_name,direction,body,created_at,expires_at,next_attempt_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$11) ON CONFLICT(event_id) DO NOTHING`, event.ID, event.SeatID, event.Issuer, event.Subject, event.ExternalName, event.TargetName, event.OwnerDisplayName, event.OwnerShortName, event.Direction, event.Body, event.CreatedAt, event.ExpiresAt)
	if err != nil {
		return err
	}
	var seat, issuer, subject, external, target, display, short, direction, body string
	err = tx.QueryRow(ctx, `SELECT seat_id,principal_issuer,principal_subject,external_name,target_name,owner_display_name,owner_short_name,direction,body FROM np_extseat_feishu_copy WHERE event_id=$1`, event.ID).Scan(&seat, &issuer, &subject, &external, &target, &display, &short, &direction, &body)
	if err != nil {
		return err
	}
	if seat != event.SeatID || issuer != event.Issuer || subject != event.Subject || external != event.ExternalName || target != event.TargetName || display != event.OwnerDisplayName || short != event.OwnerShortName || direction != event.Direction || body != event.Body {
		return errors.Join(extshared.ErrIdempotencyKey, errors.New("feishu copy event conflicts with existing event"))
	}
	return nil
}

func (q PGFeishuCopyQueue) ProjectOne(ctx context.Context, send FeishuCopySender) (bool, error) {
	if q.Pool == nil || send == nil {
		return false, extshared.ErrNotPermitted
	}
	var event FeishuCopyEvent
	var attempts int
	claimed := false
	now := time.Now().UTC()
	err := q.Pool.InTx(ctx, q.Access, nil, func(tx repo.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE np_extseat_feishu_copy SET state='dead',lease_until=NULL,last_error='message_expired' WHERE state IN ('queued','sending') AND expires_at<=$1`, now); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT event_id,seat_id,principal_issuer,principal_subject,external_name,target_name,owner_display_name,owner_short_name,direction,body,created_at,expires_at,attempts
			FROM np_extseat_feishu_copy WHERE expires_at>$1 AND ((state='queued' AND next_attempt_at<=$1) OR (state='sending' AND lease_until<=$1)) ORDER BY created_at,event_id LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(&event.ID, &event.SeatID, &event.Issuer, &event.Subject, &event.ExternalName, &event.TargetName, &event.OwnerDisplayName, &event.OwnerShortName, &event.Direction, &event.Body, &event.CreatedAt, &event.ExpiresAt, &attempts)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		claimed = true
		attempts++
		_, err = tx.Exec(ctx, `UPDATE np_extseat_feishu_copy SET state='sending',attempts=$2,lease_until=$3 WHERE event_id=$1`, event.ID, attempts, now.Add(20*time.Second))
		return err
	})
	if err != nil || !claimed {
		return claimed, err
	}
	copyCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	receipt, sendErr := send(copyCtx, event)
	cancel()
	if sendErr == nil && (receipt.BotChannelID == "" || receipt.MessageID == "") {
		sendErr = errors.New("feishu copy sender returned incomplete receipt")
	}
	if sendErr == nil {
		_, err = q.Pool.Exec(ctx, q.Access, `UPDATE np_extseat_feishu_copy SET state='sent',bot_channel_id=$2,feishu_message_id=$3,lease_until=NULL,last_error='' WHERE event_id=$1 AND state='sending'`, event.ID, receipt.BotChannelID, receipt.MessageID)
		return true, err
	}
	delay := time.Second << min(attempts-1, 8)
	state := "queued"
	if attempts >= 20 || !time.Now().Before(event.ExpiresAt) {
		state = "dead"
		delay = 0
	}
	_, err = q.Pool.Exec(ctx, q.Access, `UPDATE np_extseat_feishu_copy SET state=$2,next_attempt_at=$3,lease_until=NULL,last_error='send_failed' WHERE event_id=$1 AND state='sending'`, event.ID, state, time.Now().UTC().Add(delay))
	return true, err
}

func (q PGFeishuCopyQueue) Run(ctx context.Context, send FeishuCopySender, onError func(error)) error {
	if send == nil {
		return extshared.ErrNotPermitted
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		for i := 0; i < 32; i++ {
			did, err := q.ProjectOne(ctx, send)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if onError != nil {
					onError(err)
				}
				break
			}
			if !did {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (q PGFeishuCopyQueue) IsCopyReply(ctx context.Context, botChannelID, parentMessageID string) (bool, error) {
	if q.Pool == nil || botChannelID == "" || parentMessageID == "" {
		return false, nil
	}
	row, err := q.Pool.QueryRow(ctx, q.Access, `SELECT 1 FROM np_extseat_feishu_copy WHERE bot_channel_id=$1 AND feishu_message_id=$2 AND state='sent'`, botChannelID, parentMessageID)
	if err != nil {
		return false, err
	}
	var one int
	err = row.Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
