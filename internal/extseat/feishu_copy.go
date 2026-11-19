package extseat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// PGFeishuCopyQueue 在主消息事务中原子记录飞书只读副本。
type PGFeishuCopyQueue struct {
	Pool   *repo.Pool
	Access repo.AccessCtx
}

type FeishuCopyReceipt struct{ BotChannelID, MessageID string }
type FeishuCopySender func(context.Context, FeishuCopyEvent) (FeishuCopyReceipt, error)

const (
	FeishuCopyMaxBytes        = 4096
	FeishuCopyExcerptMaxBytes = 512
	FeishuCopyRetryTTL        = 24 * time.Hour
	FeishuCopyAttemptLimit    = 20
)

var (
	copyCredentialPattern = regexp.MustCompile(`(?i)(access[_ -]?token|refresh[_ -]?token|authorization|bearer|client[_ -]?secret|password|api[_ -]?key)\s*[:= ]\s*[^\s,;]+`)
	copyOpenIDPattern     = regexp.MustCompile(`\bou_[A-Za-z0-9_-]{6,}\b`)
	copyHiddenPromptLine  = regexp.MustCompile(`(?i)^\s*(system|developer|hidden)\s+(prompt|instructions?)\s*:`)
)

// FeishuCopyText 只渲染固定字段和经过清洗、限长的摘录；不发送原文。
func FeishuCopyText(event FeishuCopyEvent) string {
	sender, recipient := event.ExternalName, event.TargetName
	if event.Direction == FeishuCopyInternalToExternal {
		sender, recipient = event.TargetName, event.ExternalName
	}
	group := event.Group
	if group == "" {
		group = "未登记"
	}
	when := event.CreatedAt.UTC().Format(time.RFC3339)
	if event.CreatedAt.IsZero() {
		when = "时间未知"
	}
	body := safeFeishuExcerpt(event.Body)
	text := fmt.Sprintf("【Agent 消息摘要】\n发送方：%s\n接收方：%s\n编组：%s\n时间：%s\n摘录：%s\n【只读提示】回复不会转发到 Agent。",
		sender, recipient, group, when, body)
	if len(text) > FeishuCopyMaxBytes {
		text = trimUTF8(text, FeishuCopyMaxBytes)
	}
	return text
}

func safeFeishuExcerpt(body string) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || copyHiddenPromptLine.MatchString(line) {
			continue
		}
		line = copyCredentialPattern.ReplaceAllString(line, "$1=[已隐藏]")
		line = copyOpenIDPattern.ReplaceAllString(line, "[身份标识已隐藏]")
		lines = append(lines, line)
	}
	out := strings.Join(lines, " ")
	out = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return -1
		}
		return r
	}, out)
	if len(out) > FeishuCopyExcerptMaxBytes {
		out = trimUTF8(out, FeishuCopyExcerptMaxBytes) + "…"
	}
	return out
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
	var origin string
	var preference sql.NullBool
	if err := tx.QueryRow(ctx, `SELECT origin,feishu_copy_enabled FROM np_agent WHERE id=$1 AND state='active'`, event.SeatID).Scan(&origin, &preference); err != nil {
		return err
	}
	if preference.Valid && !preference.Bool {
		return nil
	}
	if !preference.Valid && origin != string(identity.OriginExternal) {
		return nil
	}
	excerpt := safeFeishuExcerpt(event.Body)
	expires := event.CreatedAt.Add(FeishuCopyRetryTTL)
	_, err := tx.Exec(ctx, `INSERT INTO np_extseat_feishu_copy(event_id,seat_id,principal_issuer,principal_subject,external_name,target_name,owner_display_name,owner_short_name,direction,body,created_at,expires_at,next_attempt_at,group_label)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$11,$13) ON CONFLICT(event_id) DO NOTHING`, event.ID, event.SeatID, event.Issuer, event.Subject, event.ExternalName, event.TargetName, event.OwnerDisplayName, event.OwnerShortName, event.Direction, excerpt, event.CreatedAt, expires, event.Group)
	if err != nil {
		return err
	}
	var seat, issuer, subject, external, target, display, short, direction, body, group string
	err = tx.QueryRow(ctx, `SELECT seat_id,principal_issuer,principal_subject,external_name,target_name,owner_display_name,owner_short_name,direction,body,group_label FROM np_extseat_feishu_copy WHERE event_id=$1`, event.ID).Scan(&seat, &issuer, &subject, &external, &target, &display, &short, &direction, &body, &group)
	if err != nil {
		return err
	}
	if seat != event.SeatID || issuer != event.Issuer || subject != event.Subject || external != event.ExternalName || target != event.TargetName || display != event.OwnerDisplayName || short != event.OwnerShortName || direction != event.Direction || body != excerpt || group != event.Group {
		return errors.Join(extshared.ErrIdempotencyKey, errors.New("feishu copy event conflicts with existing event"))
	}
	return nil
}

// SetPreference 按已验证的调用方 owner 写单个 Agent 的持久偏好。
func (q PGFeishuCopyQueue) SetPreference(ctx context.Context, ownerLC, agentID string, enabled bool) error {
	if q.Pool == nil || ownerLC == "" || agentID == "" {
		return extshared.ErrNotPermitted
	}
	return q.Pool.InTx(ctx, q.Access, nil, func(tx repo.Tx) error {
		r, err := tx.Exec(ctx, `UPDATE np_agent SET feishu_copy_enabled=$3 WHERE id=$1 AND owner_lc=$2 AND state='active'`, agentID, identity.Lower(ownerLC), enabled)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return extshared.ErrNotPermitted
		}
		if !enabled {
			_, err = tx.Exec(ctx, `UPDATE np_agent_feishu_copy_outbox SET state='dead',lease_until=NULL,last_error='preference_disabled' WHERE agent_id=$1 AND state IN ('queued','sending')`, agentID)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE np_extseat_feishu_copy SET state='dead',lease_until=NULL,last_error='preference_disabled' WHERE seat_id=$1 AND state IN ('queued','sending')`, agentID)
		}
		return err
	})
}

// EnqueueAgent 只从内部 Agent 的已接受投递路径调用。收件身份由 owner_lc→np_user→np_identity
// 在同一事务中解析；缺失或多身份时 fail closed，不猜收件人。
func (q PGFeishuCopyQueue) EnqueueAgent(ctx context.Context, event AgentFeishuCopyEvent) error {
	if q.Pool == nil || event.ID == "" || event.AgentID == "" || event.SourceName == "" || event.TargetName == "" || event.CreatedAt.IsZero() {
		return extshared.ErrNotPermitted
	}
	return q.Pool.InTx(ctx, q.Access, nil, func(tx repo.Tx) error {
		var enabled bool
		var issuer, subject, displayName, shortName, origin string
		var identityCount int
		err := tx.QueryRow(ctx, `SELECT a.origin,COALESCE(a.feishu_copy_enabled,a.origin='external'),u.display_name,u.short_name,count(i.subject),COALESCE(min(i.issuer),''),COALESCE(min(i.subject),'')
			FROM np_agent a JOIN np_user u ON u.user_id_lc=a.owner_lc
			LEFT JOIN np_identity i ON i.user_id_lc=u.user_id_lc AND i.provider='feishu'
			WHERE a.id=$1 AND a.state='active' AND u.state='active' GROUP BY a.id,a.origin,a.feishu_copy_enabled,u.display_name,u.short_name`, event.AgentID).
			Scan(&origin, &enabled, &displayName, &shortName, &identityCount, &issuer, &subject)
		if err != nil {
			return err
		}
		if origin == string(identity.OriginExternal) || !enabled {
			return nil
		}
		if identityCount != 1 || issuer == "" || subject == "" {
			return extshared.ErrNotPermitted
		}
		excerpt := safeFeishuExcerpt(event.Body)
		expires := event.CreatedAt.Add(FeishuCopyRetryTTL)
		_, err = tx.Exec(ctx, `INSERT INTO np_agent_feishu_copy_outbox(event_id,agent_id,recipient_issuer,recipient_subject,source_name,target_name,group_label,excerpt,created_at,expires_at,next_attempt_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$9) ON CONFLICT(event_id) DO NOTHING`, event.ID, event.AgentID, issuer, subject, event.SourceName, event.TargetName, event.Group, excerpt, event.CreatedAt, expires)
		if err != nil {
			return err
		}
		var gotAgent, gotIssuer, gotSubject, gotSource, gotTarget, gotGroup, gotExcerpt string
		err = tx.QueryRow(ctx, `SELECT agent_id,recipient_issuer,recipient_subject,source_name,target_name,group_label,excerpt FROM np_agent_feishu_copy_outbox WHERE event_id=$1`, event.ID).
			Scan(&gotAgent, &gotIssuer, &gotSubject, &gotSource, &gotTarget, &gotGroup, &gotExcerpt)
		if err != nil {
			return err
		}
		if gotAgent != event.AgentID || gotIssuer != issuer || gotSubject != subject || gotSource != event.SourceName || gotTarget != event.TargetName || gotGroup != event.Group || gotExcerpt != excerpt {
			return errors.Join(extshared.ErrIdempotencyKey, errors.New("agent Feishu copy event conflicts with existing event"))
		}
		return nil
	})
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
		if _, err := tx.Exec(ctx, `UPDATE np_extseat_feishu_copy SET state='dead',lease_until=NULL,last_error='retry_limit_reached' WHERE state IN ('queued','sending') AND attempts>=$1`, FeishuCopyAttemptLimit); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE np_extseat_feishu_copy c SET state='dead',lease_until=NULL,last_error='preference_disabled' FROM np_agent a WHERE a.id=c.seat_id AND c.state IN ('queued','sending') AND (a.feishu_copy_enabled=false OR (a.feishu_copy_enabled IS NULL AND a.origin<>'external'))`); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT c.event_id,c.seat_id,c.principal_issuer,c.principal_subject,c.external_name,c.target_name,c.owner_display_name,c.owner_short_name,c.direction,c.body,c.created_at,c.expires_at,c.attempts,c.group_label
			FROM np_extseat_feishu_copy c JOIN np_agent a ON a.id=c.seat_id WHERE c.expires_at>$1 AND COALESCE(a.feishu_copy_enabled,a.origin='external') AND ((c.state='queued' AND c.next_attempt_at<=$1) OR (c.state='sending' AND c.lease_until<=$1)) ORDER BY c.created_at,c.event_id LIMIT 1 FOR UPDATE OF c SKIP LOCKED`, now).Scan(&event.ID, &event.SeatID, &event.Issuer, &event.Subject, &event.ExternalName, &event.TargetName, &event.OwnerDisplayName, &event.OwnerShortName, &event.Direction, &event.Body, &event.CreatedAt, &event.ExpiresAt, &attempts, &event.Group)
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
	if attempts >= FeishuCopyAttemptLimit || !time.Now().Before(event.ExpiresAt) {
		state = "dead"
		delay = 0
	}
	_, err = q.Pool.Exec(ctx, q.Access, `UPDATE np_extseat_feishu_copy SET state=$2,next_attempt_at=$3,lease_until=NULL,last_error='send_failed' WHERE event_id=$1 AND state='sending'`, event.ID, state, time.Now().UTC().Add(delay))
	return true, err
}

func (q PGFeishuCopyQueue) ProjectAgentOne(ctx context.Context, send FeishuCopySender) (bool, error) {
	if q.Pool == nil || send == nil {
		return false, extshared.ErrNotPermitted
	}
	var event FeishuCopyEvent
	var attempts int
	claimed := false
	now := time.Now().UTC()
	err := q.Pool.InTx(ctx, q.Access, nil, func(tx repo.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE np_agent_feishu_copy_outbox SET state='dead',lease_until=NULL,last_error='copy_expired' WHERE state IN ('queued','sending') AND expires_at<=$1`, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE np_agent_feishu_copy_outbox SET state='dead',lease_until=NULL,last_error='retry_limit_reached' WHERE state IN ('queued','sending') AND attempts>=$1`, FeishuCopyAttemptLimit); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE np_agent_feishu_copy_outbox c SET state='dead',lease_until=NULL,last_error='recipient_identity_changed' FROM np_agent a
			WHERE a.id=c.agent_id AND c.state IN ('queued','sending') AND (a.state<>'active' OR NOT EXISTS (
				SELECT 1 FROM np_user u WHERE u.user_id_lc=a.owner_lc AND u.state='active'
				AND (SELECT count(*) FROM np_identity i WHERE i.user_id_lc=u.user_id_lc AND i.provider='feishu')=1
				AND EXISTS (SELECT 1 FROM np_identity i WHERE i.user_id_lc=u.user_id_lc AND i.provider='feishu' AND i.issuer=c.recipient_issuer AND i.subject=c.recipient_subject)
			))`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE np_agent_feishu_copy_outbox c SET state='dead',lease_until=NULL,last_error='preference_disabled' FROM np_agent a WHERE a.id=c.agent_id AND c.state IN ('queued','sending') AND NOT COALESCE(a.feishu_copy_enabled,a.origin='external')`); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT c.event_id,c.agent_id,c.recipient_issuer,c.recipient_subject,c.source_name,c.target_name,c.group_label,c.excerpt,c.created_at,c.expires_at,c.attempts
			FROM np_agent_feishu_copy_outbox c JOIN np_agent a ON a.id=c.agent_id WHERE c.expires_at>$1 AND COALESCE(a.feishu_copy_enabled,a.origin='external') AND ((c.state='queued' AND c.next_attempt_at<=$1) OR (c.state='sending' AND c.lease_until<=$1)) ORDER BY c.created_at,c.event_id LIMIT 1 FOR UPDATE OF c SKIP LOCKED`, now).
			Scan(&event.ID, &event.AgentID, &event.Issuer, &event.Subject, &event.ExternalName, &event.TargetName, &event.Group, &event.Body, &event.CreatedAt, &event.ExpiresAt, &attempts)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		event.Direction = FeishuCopyInternalAgent
		claimed = true
		attempts++
		_, err = tx.Exec(ctx, `UPDATE np_agent_feishu_copy_outbox SET state='sending',attempts=$2,lease_until=$3 WHERE event_id=$1`, event.ID, attempts, now.Add(20*time.Second))
		return err
	})
	if err != nil || !claimed {
		return claimed, err
	}
	copyCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	receipt, sendErr := send(copyCtx, event)
	cancel()
	if sendErr == nil && (receipt.BotChannelID == "" || receipt.MessageID == "") {
		sendErr = errors.New("Feishu copy sender returned incomplete receipt")
	}
	if sendErr == nil {
		_, err = q.Pool.Exec(ctx, q.Access, `UPDATE np_agent_feishu_copy_outbox SET state='sent',bot_channel_id=$2,feishu_message_id=$3,lease_until=NULL,last_error='' WHERE event_id=$1 AND state='sending'`, event.ID, receipt.BotChannelID, receipt.MessageID)
		return true, err
	}
	delay := time.Second << min(attempts-1, 8)
	state := "queued"
	if attempts >= FeishuCopyAttemptLimit || !time.Now().Before(event.ExpiresAt) {
		state = "dead"
		delay = 0
	}
	_, err = q.Pool.Exec(ctx, q.Access, `UPDATE np_agent_feishu_copy_outbox SET state=$2,next_attempt_at=$3,lease_until=NULL,last_error='send_failed' WHERE event_id=$1 AND state='sending'`, event.ID, state, time.Now().UTC().Add(delay))
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
				did, err = q.ProjectAgentOne(ctx, send)
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
	row, err := q.Pool.QueryRow(ctx, q.Access, `SELECT 1 FROM np_extseat_feishu_copy WHERE bot_channel_id=$1 AND feishu_message_id=$2 AND state='sent' UNION ALL SELECT 1 FROM np_agent_feishu_copy_outbox WHERE bot_channel_id=$1 AND feishu_message_id=$2 AND state='sent' LIMIT 1`, botChannelID, parentMessageID)
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
