package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
)

// enqueueDeliveryFailureNoticeTx commits with the original terminal transition.
// Only previously ACK-capable session senders are eligible for durable notices.
// Only the persisted target session label is rendered, after strict sanitization.
// Original bodies, raw addresses/keys, arbitrary last_error and credentials are never copied.
func enqueueDeliveryFailureNoticeTx(ctx context.Context, tx *sql.Tx, d model.SessionDelivery, reason string, now time.Time) (bool, error) {
	if !model.DeliveryFailureReasonAllowed(reason) || d.Provenance == model.FailureNoticeProvenance {
		return false, nil
	}
	var original model.Envelope
	if json.Unmarshal([]byte(d.Envelope), &original) != nil {
		return false, nil
	}
	body := "【系统投递失败通知】\n发往" + safeFailureNoticeTarget(d.SessionName) + "的原消息未能完成投递；原因：" + failureNoticeReasonText(reason) + "（原因码：" + reason + "）。本通知不要求回复。"
	if reason == "queue_full" {
		body = "发送未进入目标收件箱：目标 Agent 忙且收件箱已满；本条未提交，请缩短排队或稍后重新发送。"
	}
	from := strings.Split(strings.TrimSpace(original.From), "#")
	if reason == "queue_full" && len(from) == 3 && (strings.HasPrefix(from[0], "ou_") || strings.HasPrefix(from[0], "oc_") || strings.HasPrefix(from[0], "on_")) && from[1] != "" && from[2] != "" {
		// Feishu senders use the existing durable outbound message queue. Keep
		// the same deterministic notice ID so a replayed ACK cannot enqueue twice.
		var botChannelID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM bot_channel WHERE name=? AND active=1 AND can_send=1`, from[2]).Scan(&botChannelID); err != nil {
			return false, err
		}
		chatType, entityKind := model.ChatPersonal, "personal"
		if strings.HasPrefix(from[0], "oc_") {
			chatType, entityKind = model.ChatGroup, "group"
		}
		content, err := json.Marshal(map[string]string{"text": body})
		if err != nil {
			return false, err
		}
		id := model.FailureNoticeDeliveryID(d.ID)
		stamp := now.UTC().Format(time.RFC3339)
		result, err := tx.ExecContext(ctx, `INSERT INTO message
 (id,chat_entity_id,direction,bot_channel_id,feishu_msg_id,chat_type,sender_open_id,ingress_provenance,feishu_reply_json,content_json,status,attempts,error,created_at,processed_at)
 VALUES(?,?,?, ?,?,?, '', 'untrusted', '',?,'queued',0,'',?,NULL) ON CONFLICT(id) DO NOTHING`,
			id, botChannelID+":"+entityKind+":"+from[0], string(model.DirectionOut), botChannelID, id, string(chatType), string(content), stamp)
		if err != nil {
			return false, err
		}
		n, err := result.RowsAffected()
		return n > 0, err
	}
	if d.SourceType != "session_inbound" && !strings.HasPrefix(d.SourceType, "session_inbound/subagent/") && !(reason == "queue_full" && (d.SourceType == "feishu_inbound" || d.SourceType == "agent_route")) {
		return false, nil
	}
	source := from
	if len(source) != 2 || source[0] == "" || source[1] == "" || source[0] == "workpulse" {
		if reason == "queue_full" {
			return false, errors.New("queue_full sender address is not durably routable")
		}
		return false, nil
	}
	var route subagent.DeliveryRoute
	var routeJSON string
	err := tx.QueryRowContext(ctx, `SELECT route_json FROM session_delivery_sub_route WHERE delivery_id=?`, d.ID).Scan(&routeJSON)
	if err == nil {
		if err = json.Unmarshal([]byte(routeJSON), &route); err != nil {
			return false, err
		}
		if route.From != nil {
			parsed, e := subagent.Parse(route.From.FullName, "")
			if e != nil {
				return false, e
			}
			source[0] = parsed.Root
		}
	} else if err != sql.ErrNoRows {
		return false, err
	}
	var ack int
	err = tx.QueryRowContext(ctx, `SELECT delivery_ack_v1 FROM session_endpoint WHERE key_id=? AND session_name=?`, source[1], source[0]).Scan(&ack)
	if err == sql.ErrNoRows {
		if reason == "queue_full" {
			return false, errors.New("queue_full sender endpoint is missing")
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ack != 1 && reason != "queue_full" {
		return false, nil
	}
	id := model.FailureNoticeDeliveryID(d.ID)
	notice := model.Envelope{ID: id, From: "workpulse#" + source[1], To: original.From, TS: now.Unix(),
		Body: body,
		Meta: map[string]any{"type": "delivery", "delivery_id": id, "delivery_version": 1, "ack_required": true,
			"source_message_id": d.ID, "failed_message_id": original.ID, "failed_delivery_id": d.ID, "reason_code": reason,
			"kind": "reply", "reply_policy": "no_reply", "conversation_terminal": true, "suppress_reply_origin": true, "no_mirror": true}}
	var returnRoute *subagent.DeliveryRoute
	if scope := route.From; scope != nil {
		notice.Meta["sub_path"] = scope.Path
		notice.Meta["target_full_name"] = scope.FullName
		notice.Meta["target_node_id"] = scope.NodeID
		notice.Meta["lane_generation"] = scope.Generation
		notice.Meta["subtree_revision"] = scope.Revision
		returnRoute = &subagent.DeliveryRoute{To: scope}
	}
	raw, err := json.Marshal(notice)
	if err != nil {
		return false, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	r, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO session_delivery
 (id,key_id,session_name,envelope_json,source_message_id,source_type,provenance,state,attempts,next_attempt_at,last_error,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,'queued',0,?,'',?,?)`, id, source[1], source[0], string(raw), d.ID, "failure_notice", model.FailureNoticeProvenance, stamp, stamp, stamp)
	if err != nil {
		return false, err
	}
	if returnRoute != nil {
		raw, e := json.Marshal(returnRoute)
		if e != nil {
			return false, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO session_delivery_sub_route(delivery_id,route_json) VALUES(?,?)`, id, string(raw)); e != nil {
			return false, e
		}
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// Do not parse an address supplied in the envelope: account keys are not display names.
// Reject rather than partially quote unsafe names (commands, control/bidi or markup).
func safeFailureNoticeTarget(session string) string {
	if session == "" || len(session) > 192 || len([]rune(session)) > 64 {
		return "目标 Agent（名称无法安全显示）"
	}
	for _, r := range session {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '-' && r != '_' {
			return "目标 Agent（名称无法安全显示）"
		}
	}
	return "Agent「" + session + "」"
}

func failureNoticeReasonText(reason string) string {
	switch reason {
	case "outbound_permission_denied":
		return "发送或接收身份、当前通信权限已变化"
	case "sub_agent_not_found":
		return "子 Agent 不存在"
	case "sub_agent_ended":
		return "子 Agent 已结束"
	case "sub_agent_generation_mismatch":
		return "子 Agent 代次不符"
	case "retry_exhausted":
		return "重试次数已用尽，仍未确认完成投递"
	case "delivery_ack_unsupported":
		return "目标不支持投递确认，无法确认完成投递"
	case "delivery_capability_unconfirmed_expired":
		return "等待目标确认投递能力已超时"
	case "delivery_expired":
		return "投递等待已超时"
	case "queue_full":
		return "目标收件箱已满，消息未提交"
	}
	return "无法确认完成投递"
}

func collectFailureRows(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]model.SessionDelivery, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.SessionDelivery
	for rows.Next() {
		d, err := scanSessionDelivery(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *d)
	}
	return result, rows.Err()
}

// SweepSessionDeliveryFailures includes every expirable nonterminal state,
// even if neither endpoint ever reconnects. Backfill is limited to explicit
// three stored reasons and the last 30 minutes of terminal-transition time.
// Historical retry_exhausted is always excluded, even when explicitly stored.
// No old timestamp or ambiguous arbitrary retry error is guessed into scope.
func (s *SQLite) SweepSessionDeliveryFailures(ctx context.Context, now time.Time) ([]model.SessionDelivery, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	cutoff := now.Add(-30 * time.Minute).UTC().Format(time.RFC3339Nano)
	rows, err := collectFailureRows(ctx, tx, `UPDATE session_delivery SET state='dead',lease_until=NULL,updated_at=?,
 last_error=CASE WHEN state='capability_pending' AND provenance='capability_pending_ack_v1' THEN 'delivery_capability_unconfirmed_expired' ELSE 'delivery_expired' END
 WHERE id IN (SELECT id FROM session_delivery WHERE COALESCE(ack_protocol,0)<>2 AND state NOT IN ('processed','dead','cancelled','legacy_received') AND (attempts>=12 OR created_at<=?) ORDER BY created_at,id LIMIT 1000)
 RETURNING `+sessionDeliveryColumns, stamp, cutoff)
	if err != nil {
		return nil, err
	}
	for _, d := range rows {
		if _, err := enqueueDeliveryFailureNoticeTx(ctx, tx, d, d.LastError, now); err != nil {
			return nil, err
		}
	}
	recent, err := collectFailureRows(ctx, tx, sessionDeliverySelect+` WHERE state='dead' AND updated_at>=? AND updated_at<=? AND last_error IN ('delivery_ack_unsupported','delivery_capability_unconfirmed_expired','delivery_expired')
 AND (source_type='session_inbound' OR source_type LIKE 'session_inbound/subagent/%') AND EXISTS (SELECT 1 FROM session_endpoint ep WHERE ep.delivery_ack_v1=1 AND ep.session_name<>'workpulse' AND (ep.session_name || '#' || ep.key_id)=json_extract(CASE WHEN json_valid(envelope_json) THEN envelope_json ELSE '{}' END,'$.from'))
 AND NOT EXISTS (SELECT 1 FROM session_delivery n WHERE n.source_type='failure_notice' AND n.source_message_id=session_delivery.id) ORDER BY updated_at,id LIMIT 1000`, cutoff, stamp)
	// Dedup is the deterministic primary key in the INSERT, not an in-memory flag.
	if err != nil {
		return nil, err
	}
	for _, d := range recent {
		created, err := enqueueDeliveryFailureNoticeTx(ctx, tx, d, d.LastError, now)
		if err != nil {
			return nil, err
		}
		if created {
			rows = append(rows, d)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rows, nil
}
