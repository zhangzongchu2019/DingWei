package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

const ClarificationReplyUnavailableReason = "clarification_reply_authority_or_anchor_unconfigured"

// Derived from the configured authenticated gateway and locked PG binding;
// never supplied by a Console/Agent request or a reply body.
type ClarificationBinding struct{ Issuer, Tenant, AppID, Subject, BindingSHA string }
type ClarificationNotice struct {
	NoticeID, QuestionID, SpecID, SHA, PMUser      string
	Generation, Revision, PolicyVersion            int
	Binding                                        ClarificationBinding
	EntryRefs                                      []string
	ProviderMessageID, CreatedAt, ExpiresAt, State string
}

func (s *SQLite) PrepareClarificationNotice(ctx context.Context, expected QuestionContext, n QuestionNotice, b ClarificationBinding, check func() error, now time.Time) (ClarificationNotice, error) {
	var out ClarificationNotice
	if check == nil || b.Issuer == "" || b.Tenant == "" || b.AppID == "" || b.Subject == "" || len(b.BindingSHA) != 64 || n.Kind == "remind_60m" {
		return out, ErrTaskFlowInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	c, err := questionContext(ctx, tx, "", "", n.QuestionID)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(c, expected) {
		return out, ErrTaskFlowConflict
	}
	q, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionCols+` FROM pilot_question WHERE id=?`, n.QuestionID))
	if err != nil {
		return out, err
	}
	p, err := readClarificationPolicy(ctx, tx, q.SpecID)
	if err != nil {
		return out, err
	}
	if !questionCurrent(q, c) || p.Version < 1 || p.PMUser == "" || p.PMUser != q.PMUser || p.Version != q.PolicyVersion || n.Recipient != p.PMUser || n.Generation != q.Generation {
		return out, ErrTaskFlowPermission
	}
	var state, refs string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM pilot_question_outbox WHERE id=? AND question_id=? AND generation=? AND recipient_user=?`, n.ID, q.ID, q.Generation, p.PMUser).Scan(&state); err != nil {
		return out, err
	}
	if state != "dispatching" {
		return out, ErrTaskFlowConflict
	}
	if err = tx.QueryRowContext(ctx, `SELECT entry_refs_json FROM spec_clarification_question WHERE question_id=?`, q.ID).Scan(&refs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrTaskFlowInvalid
		}
		return out, err
	}
	if err = json.Unmarshal([]byte(refs), &out.EntryRefs); err != nil || len(out.EntryRefs) == 0 {
		return out, ErrTaskFlowInvalid
	}
	out.NoticeID, out.QuestionID, out.SpecID, out.SHA, out.PMUser = n.ID, q.ID, q.SpecID, q.SHA, p.PMUser
	out.Generation, out.Revision, out.PolicyVersion = q.Generation, q.Revision, p.Version
	out.Binding = b
	out.CreatedAt = now.UTC().Format(time.RFC3339Nano)
	out.ExpiresAt = now.Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	out.State = "prepared"
	_, err = tx.ExecContext(ctx, `INSERT INTO spec_clarification_notice(notice_id,question_id,generation,spec_id,revision,content_sha256,pm_user,policy_version,issuer,tenant,app_id,subject,binding_sha,entry_refs_json,created_at,expires_at,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, out.NoticeID, out.QuestionID, out.Generation, out.SpecID, out.Revision, out.SHA, out.PMUser, out.PolicyVersion, b.Issuer, b.Tenant, b.AppID, b.Subject, b.BindingSHA, refs, out.CreatedAt, out.ExpiresAt, out.State)
	if err != nil {
		return out, err
	}
	if err = check(); err != nil {
		return ClarificationNotice{}, err
	}
	return out, tx.Commit()
}

const clarificationNoticeCols = `notice_id,question_id,generation,spec_id,revision,content_sha256,pm_user,policy_version,issuer,tenant,app_id,subject,binding_sha,entry_refs_json,provider_message_id,created_at,expires_at,state`

func scanClarificationNotice(row rowScanner) (ClarificationNotice, error) {
	var n ClarificationNotice
	var refs string
	err := row.Scan(&n.NoticeID, &n.QuestionID, &n.Generation, &n.SpecID, &n.Revision, &n.SHA, &n.PMUser, &n.PolicyVersion, &n.Binding.Issuer, &n.Binding.Tenant, &n.Binding.AppID, &n.Binding.Subject, &n.Binding.BindingSHA, &refs, &n.ProviderMessageID, &n.CreatedAt, &n.ExpiresAt, &n.State)
	if err == nil {
		err = json.Unmarshal([]byte(refs), &n.EntryRefs)
	}
	return n, err
}

// Exact parent is the ONLY positive lookup. The weaker known-parent/root
// lookup below is exclusively a consume/reject guard, never an authorization.
func (s *SQLite) ClarificationNoticeByParent(ctx context.Context, issuer, tenant, parent string) (ClarificationNotice, error) {
	return scanClarificationNotice(s.db.QueryRowContext(ctx, `SELECT `+clarificationNoticeCols+` FROM spec_clarification_notice WHERE issuer=? AND tenant=? AND provider_message_id=? AND provider_message_id<>''`, issuer, tenant, parent))
}
func (s *SQLite) IsKnownClarificationReply(ctx context.Context, issuer, parent, root string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM spec_clarification_notice WHERE issuer=? AND provider_message_id<>'' AND ((?<>'' AND provider_message_id=?) OR (?<>'' AND provider_message_id=?))`, issuer, parent, parent, root, root).Scan(&n)
	return n > 0, err
}
func (s *SQLite) HasPreparedClarificationNotice(ctx context.Context, issuer, subject string, now time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM spec_clarification_notice WHERE issuer=? AND subject=? AND state='prepared' AND julianday(created_at)>=julianday(?)-1`, issuer, subject, now.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n > 0, err
}

func (s *SQLite) ClarificationQuestionRefs(ctx context.Context, id string) ([]string, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT entry_refs_json FROM spec_clarification_question WHERE question_id=?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	var refs []string
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		return nil, err
	}
	if len(refs) < 1 || len(refs) > 32 {
		return nil, ErrTaskFlowInvalid
	}
	return refs, nil
}
