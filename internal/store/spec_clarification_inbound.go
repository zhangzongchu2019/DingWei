package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type clarificationInboundMetadata struct {
	Issuer, Subject string
	ChatType        model.ChatType
	Provenance      model.IngressProvenance
	Reply           model.FeishuReplyMetadata
}
type ClarificationInbound struct {
	Sequence                                                                     int64
	Message                                                                      model.Message
	Body, PayloadSHA, ReceivedAt, ExpiresAt, State, Reason, SpecID, ReceiptState string
	Floor                                                                        int64
}

func clarificationInboundHash(m model.Message, body string) string {
	meta := *m.FeishuReply
	meta.EventID = "" // A new event ID may redeliver the same provider message.
	return blockerHash([]any{m.BotChannelID, m.SenderOpenID, m.ChatType, meta, body})
}

func (s *SQLite) StageClarificationInbound(ctx context.Context, m model.Message, body string, now time.Time) (ClarificationInbound, error) {
	var out ClarificationInbound
	r := m.FeishuReply
	if r == nil || !r.Authenticated || m.IngressProvenance != model.IngressFeishuWSAuthenticated || m.ChatType != model.ChatPersonal || r.SenderType != "user" || r.MessageType != "text" || r.ParentID == "" || r.EventID == "" || r.MessageID == "" || r.MessageID != m.FeishuMsgID || r.Tenant == "" || r.AppID == "" || r.ChatID == "" || len(r.ContentSHA) != 64 || m.BotChannelID == "" || m.SenderOpenID == "" || !questionNonempty(body, 8192) {
		return out, ErrTaskFlowPermission
	}
	for _, id := range []string{r.EventID, r.MessageID, r.ParentID, r.RootID, r.Tenant, r.AppID, r.ChatID, m.BotChannelID, m.SenderOpenID} {
		if len(id) > 256 || strings.ContainsAny(id, "\x00\r\n") {
			return out, ErrTaskFlowInvalid
		}
	}
	hash := clarificationInboundHash(m, body)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var oldMessage, oldHash string
	err = tx.QueryRowContext(ctx, `SELECT message_id,payload_sha FROM spec_clarification_event_key WHERE issuer=? AND tenant=? AND event_id=?`, m.BotChannelID, r.Tenant, r.EventID).Scan(&oldMessage, &oldHash)
	if err == nil && (oldMessage != r.MessageID || oldHash != hash) {
		return out, ErrTaskFlowConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out, err = readClarificationInbound(ctx, tx, m.BotChannelID, r.Tenant, r.MessageID)
	if err == nil {
		if out.PayloadSHA != hash {
			return ClarificationInbound{}, ErrTaskFlowConflict
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	} else {
		metadata, _ := json.Marshal(clarificationInboundMetadata{m.BotChannelID, m.SenderOpenID, m.ChatType, m.IngressProvenance, *r})
		at, expires := now.UTC().Format(time.RFC3339Nano), now.Add(24*time.Hour).UTC().Format(time.RFC3339Nano)
		if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_inbound(issuer,tenant,message_id,event_id,payload_sha,metadata_json,body,received_at,expires_at,state) VALUES(?,?,?,?,?,?,?,?,?,'pending_match')`, m.BotChannelID, r.Tenant, r.MessageID, r.EventID, hash, string(metadata), body, at, expires); err != nil {
			return out, err
		}
		out, err = readClarificationInbound(ctx, tx, m.BotChannelID, r.Tenant, r.MessageID)
		if err != nil {
			return out, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_event_key(issuer,tenant,event_id,message_id,payload_sha) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, m.BotChannelID, r.Tenant, r.EventID, r.MessageID, hash); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func readClarificationInbound(ctx context.Context, db taskQuery, issuer, tenant, id string) (ClarificationInbound, error) {
	var out ClarificationInbound
	var metadata string
	err := db.QueryRowContext(ctx, `SELECT rowid,metadata_json,body,payload_sha,received_at,expires_at,state,reason,spec_id,floor,receipt_state FROM spec_clarification_inbound WHERE issuer=? AND tenant=? AND message_id=?`, issuer, tenant, id).Scan(&out.Sequence, &metadata, &out.Body, &out.PayloadSHA, &out.ReceivedAt, &out.ExpiresAt, &out.State, &out.Reason, &out.SpecID, &out.Floor, &out.ReceiptState)
	if err != nil {
		return out, err
	}
	var meta clarificationInboundMetadata
	if err = json.Unmarshal([]byte(metadata), &meta); err != nil {
		return out, err
	}
	out.Message = model.Message{BotChannelID: meta.Issuer, SenderOpenID: meta.Subject, ChatType: meta.ChatType, IngressProvenance: meta.Provenance, FeishuMsgID: meta.Reply.MessageID, FeishuReply: &meta.Reply}
	return out, nil
}

func (s *SQLite) RecordFeishuClarification(ctx context.Context, expected QuestionContext, n ClarificationNotice, b ClarificationBinding, a BlockerActor, in ClarificationInbound, u ClarificationUpdate, enabled bool, check func() error, now time.Time) (ClarificationResult, error) {
	var out ClarificationResult
	if !enabled || check == nil || a.UserID == "" || a.AgentID != "" || in.Message.FeishuReply == nil {
		return out, ErrTaskFlowPermission
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	r := in.Message.FeishuReply
	current, err := readClarificationInbound(ctx, tx, in.Message.BotChannelID, r.Tenant, r.MessageID)
	if err != nil {
		return out, err
	}
	if current.State == "rejected" || current.PayloadSHA != in.PayloadSHA {
		return out, ErrTaskFlowConflict
	}
	in = current
	r = in.Message.FeishuReply
	nowNotice, err := scanClarificationNotice(tx.QueryRowContext(ctx, `SELECT `+clarificationNoticeCols+` FROM spec_clarification_notice WHERE notice_id=?`, n.NoticeID))
	if err != nil {
		return out, err
	}
	expectedBinding := n.Binding
	if expectedBinding.Tenant == "" {
		// Only a tenant-less notice may learn tenant from the authenticated event.
		if r.SenderTenant != r.Tenant {
			return out, ErrTaskFlowPermission
		}
		expectedBinding.Tenant = r.Tenant
	}
	if !reflect.DeepEqual(nowNotice, n) || n.State != "sent" || n.ProviderMessageID != r.ParentID || expectedBinding != b || b.Issuer != in.Message.BotChannelID || b.Subject != in.Message.SenderOpenID || b.Tenant != r.Tenant || b.AppID != r.AppID || a.UserID != n.PMUser {
		return out, ErrTaskFlowPermission
	}
	created, e := time.Parse(time.RFC3339Nano, n.CreatedAt)
	if e != nil {
		return out, e
	}
	expires, e := time.Parse(time.RFC3339Nano, n.ExpiresAt)
	if e != nil {
		return out, e
	}
	if now.Before(created) || !now.Before(expires) {
		return out, ErrTaskFlowConflict
	}
	p, err := readClarificationPolicy(ctx, tx, n.SpecID)
	if err != nil {
		return out, err
	}
	if p.PMUser != n.PMUser || p.Version != n.PolicyVersion {
		return out, ErrTaskFlowPermission
	}
	q, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, n.QuestionID))
	if err != nil {
		return out, err
	}
	if q.Generation != n.Generation || q.Revision != n.Revision || q.SHA != n.SHA || q.SpecID != n.SpecID {
		return out, ErrTaskFlowConflict
	}
	if u.Kind != "explanation" && u.Kind != "supplement" && u.Kind != "correction" {
		return out, ErrTaskFlowPermission
	}
	u.RequestID = "feishu:" + blockerHash([]string{b.Issuer, b.Tenant, r.MessageID})
	if len(u.EntryRefs) == 0 {
		u.EntryRefs = append([]string{}, n.EntryRefs...)
	}
	parent := blockerHash([]string{b.Issuer, b.Tenant, b.AppID, n.ProviderMessageID})
	out, err = s.appendClarificationTx(ctx, tx, expected, a, n.QuestionID, u, true, check, now, clarificationOrigin{Source: "feishu", Parent: parent})
	if err != nil {
		return ClarificationResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE spec_clarification_inbound SET state='recorded',reason='',spec_id=?,floor=? WHERE issuer=? AND tenant=? AND message_id=? AND state='pending_match'`, out.Post.SpecID, out.Post.Floor, b.Issuer, b.Tenant, r.MessageID); err != nil {
		return ClarificationResult{}, err
	}
	if err = check(); err != nil {
		return ClarificationResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return ClarificationResult{}, err
	}
	return out, nil
}

func (s *SQLite) RejectClarificationInbound(ctx context.Context, in ClarificationInbound, reason string) error {
	if len(reason) > 256 || in.Message.FeishuReply == nil {
		return ErrTaskFlowInvalid
	}
	_, err := s.db.ExecContext(ctx, `UPDATE spec_clarification_inbound SET state='rejected',reason=? WHERE issuer=? AND tenant=? AND message_id=? AND state='pending_match'`, reason, in.Message.BotChannelID, in.Message.FeishuReply.Tenant, in.Message.FeishuMsgID)
	return err
}

func (s *SQLite) PendingClarificationInbound(ctx context.Context, after int64) ([]ClarificationInbound, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT issuer,tenant,message_id FROM spec_clarification_inbound WHERE state='pending_match' AND rowid>? ORDER BY rowid LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	var keys [][3]string
	for rows.Next() {
		var k [3]string
		if err = rows.Scan(&k[0], &k[1], &k[2]); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var out []ClarificationInbound
	for _, k := range keys {
		v, e := readClarificationInbound(ctx, s.db, k[0], k[1], k[2])
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *SQLite) ClaimClarificationReceipt(ctx context.Context, in ClarificationInbound) (bool, error) {
	if in.Message.FeishuReply == nil {
		return false, ErrTaskFlowInvalid
	}
	r, err := s.db.ExecContext(ctx, `UPDATE spec_clarification_inbound SET receipt_state='attempted' WHERE issuer=? AND tenant=? AND message_id=? AND receipt_state='pending' AND state IN('recorded','rejected')`, in.Message.BotChannelID, in.Message.FeishuReply.Tenant, in.Message.FeishuMsgID)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (s *SQLite) FinishClarificationReceipt(ctx context.Context, in ClarificationInbound, sent bool) error {
	if in.Message.FeishuReply == nil {
		return ErrTaskFlowInvalid
	}
	state := "unknown"
	if sent {
		state = "sent"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE spec_clarification_inbound SET receipt_state=? WHERE issuer=? AND tenant=? AND message_id=? AND receipt_state='attempted'`, state, in.Message.BotChannelID, in.Message.FeishuReply.Tenant, in.Message.FeishuMsgID)
	return err
}
