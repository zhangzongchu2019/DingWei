package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func checkClarificationTransportKeys(ctx context.Context, tx *sql.Tx, m model.Message) error {
	r := m.FeishuReply
	if r == nil || !r.Authenticated || m.IngressProvenance != model.IngressFeishuWSAuthenticated {
		return nil
	}
	// Incomplete old events remain non-authoritative at the later recognizer.
	if m.BotChannelID == "" || r.Tenant == "" || r.EventID == "" || r.MessageID == "" {
		return nil
	}
	if r.MessageID != m.FeishuMsgID {
		return ErrTaskFlowConflict
	}
	hash := clarificationInboundHash(m, m.Content)
	for _, key := range [][2]string{{"event", r.EventID}, {"message", r.MessageID}} {
		var id, sha string
		err := tx.QueryRowContext(ctx, `SELECT message_id,payload_sha FROM spec_clarification_transport_key WHERE issuer=? AND tenant=? AND key_kind=? AND provider_id=?`, m.BotChannelID, r.Tenant, key[0], key[1]).Scan(&id, &sha)
		if err == nil {
			if id != r.MessageID || sha != hash {
				return ErrTaskFlowConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_transport_key(issuer,tenant,key_kind,provider_id,message_id,payload_sha) VALUES(?,?,?,?,?,?)`, m.BotChannelID, r.Tenant, key[0], key[1], r.MessageID, hash); err != nil {
			return err
		}
	}
	return nil
}
