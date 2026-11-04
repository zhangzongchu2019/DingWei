package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/r9pm"
)

// R9PMKey is the immutable OpenSpec decision point identity.
type R9PMKey struct {
	Provider        string `json:"provider"`
	Instance        string `json:"instance"`
	RepositoryID    string `json:"repository_id"`
	ChangeDirectory string `json:"change_directory"`
	PointID         string `json:"point_id"`
}
type R9PMResponsibility struct {
	R9PMKey
	PMUserIDLC        string `json:"pm_user_id_lc,omitempty"`
	MappingGeneration int64  `json:"mapping_generation"`
	SetByUserIDLC     string `json:"set_by_user_id_lc"`
	UpdatedAtUTC      string `json:"updated_at_utc"`
	DeletedAtUTC      string `json:"deleted_at_utc,omitempty"`
}

var ErrR9PMNotFound = errors.New("R9 PM mapping not found")

func (s *SQLite) GetR9PMResponsibility(ctx context.Context, k R9PMKey) (*R9PMResponsibility, error) {
	var v R9PMResponsibility
	var pm sql.NullString
	v.R9PMKey = k
	err := s.db.QueryRowContext(ctx, `SELECT pm_user_id_lc,mapping_generation,set_by_user_id_lc,updated_at_utc,deleted_at_utc FROM r9_pm_responsibility WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=?`, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID).Scan(&pm, &v.MappingGeneration, &v.SetByUserIDLC, &v.UpdatedAtUTC, &v.DeletedAtUTC)
	v.PMUserIDLC = pm.String
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrR9PMNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *SQLite) SetR9PMResponsibility(ctx context.Context, k R9PMKey, user, actor string) (int64, error) {
	return s.mutateR9PMResponsibility(ctx, k, user, actor, false, "", "")
}
func (s *SQLite) DeleteR9PMResponsibility(ctx context.Context, k R9PMKey, actor string) (int64, error) {
	return s.mutateR9PMResponsibility(ctx, k, "", actor, true, "", "")
}
func (s *SQLite) SetR9PMResponsibilityAndReissue(ctx context.Context, k R9PMKey, user, actor, issuer, subject string) (int64, error) {
	return s.mutateR9PMResponsibility(ctx, k, user, actor, false, issuer, subject)
}

func (s *SQLite) mutateR9PMResponsibility(ctx context.Context, k R9PMKey, user, actor string, deleted bool, issuer, subject string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	nowTime := s.now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	type staleRequest struct {
		id, commit, sha, summary, creator string
		generation                        int64
	}
	var stale *staleRequest
	if !deleted {
		var v staleRequest
		e := tx.QueryRowContext(ctx, `SELECT request_id,commit_sha,content_sha256,summary_json,created_by_user_id_lc,generation FROM r9_pm_request WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=? AND status IN ('open','timed_out','unknown') ORDER BY generation DESC LIMIT 1`, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID).Scan(&v.id, &v.commit, &v.sha, &v.summary, &v.creator, &v.generation)
		if e == nil {
			stale = &v
			if issuer == "" || subject == "" {
				return 0, errors.New("cannot reissue R9 request without private Feishu identity")
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return 0, e
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_responsibility(provider,instance,repository_id,change_directory,point_id,pm_user_id_lc,mapping_generation,set_by_user_id_lc,updated_at_utc,deleted_at_utc) VALUES(?,?,?,?,?,?,1,?,?,?) ON CONFLICT(provider,instance,repository_id,change_directory,point_id) DO UPDATE SET pm_user_id_lc=excluded.pm_user_id_lc,mapping_generation=r9_pm_responsibility.mapping_generation+1,set_by_user_id_lc=excluded.set_by_user_id_lc,updated_at_utc=excluded.updated_at_utc,deleted_at_utc=excluded.deleted_at_utc`, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID, nullIfEmpty(user), actor, now, func() string {
		if deleted {
			return now
		}
		return ""
	}())
	if err != nil {
		return 0, err
	}
	var gen int64
	if err = tx.QueryRowContext(ctx, `SELECT mapping_generation FROM r9_pm_responsibility WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=?`, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID).Scan(&gen); err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE r9_pm_request SET status='superseded' WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=? AND status IN ('open','timed_out','unknown')`, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE r9_pm_delivery SET state='closed',reason='mapping changed' WHERE request_id IN (SELECT request_id FROM r9_pm_request WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=? AND status='superseded') AND state='pending'`, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID)
	if err != nil {
		return 0, err
	}
	if stale != nil {
		var content struct {
			Question       string   `json:"question"`
			Options        []string `json:"options"`
			Recommendation string   `json:"recommendation"`
			Source         string   `json:"source"`
		}
		if e := json.Unmarshal([]byte(stale.summary), &content); e != nil || content.Question == "" || len(content.Options) < 2 || content.Recommendation == "" {
			return 0, errors.New("cannot reissue malformed R9 decision summary")
		}
		point := r9pm.Point{ID: k.PointID, Question: content.Question, Options: content.Options, Recommendation: content.Recommendation}
		contentSHA := point.ContentSHA256(user)
		var nextGen int64
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0)+1 FROM r9_pm_request WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=?`, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID).Scan(&nextGen); e != nil {
			return 0, e
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", stale.id, gen)))
		newID := hex.EncodeToString(sum[:])
		deadline := nowTime.Add(24 * time.Hour).Format(time.RFC3339Nano)
		if _, e := tx.ExecContext(ctx, `INSERT INTO r9_pm_request(request_id,provider,instance,repository_id,change_directory,commit_sha,point_id,content_sha256,pm_user_id_lc,mapping_generation,generation,status,created_at_utc,deadline_at_utc,next_reminder_at_utc,summary_json,created_by_user_id_lc) VALUES(?,?,?,?,?,?,?,?,?,?,?,'open',?,?,?,?,?)`, newID, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, stale.commit, k.PointID, contentSHA, user, gen, nextGen, now, deadline, deadline, stale.summary, stale.creator); e != nil {
			return 0, e
		}
		for _, d := range []struct{ ch, recipient, iss string }{{"console", user, ""}, {"feishu", subject, issuer}} {
			op := newID + ":" + d.ch + ":initial"
			if _, e := tx.ExecContext(ctx, `INSERT INTO r9_pm_delivery(operation_id,request_id,generation,channel,kind,period_index,recipient_id,issuer,state,created_at_utc) VALUES(?,?,?,?,'initial',0,?,?,'pending',?)`, op, newID, nextGen, d.ch, d.recipient, d.iss, now); e != nil {
				return 0, e
			}
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO r9_pm_audit(actor_user_id_lc,provider,instance,repository_id,change_directory,point_id,action,pm_user_id_lc,mapping_generation,at_utc,result) VALUES(?,?,?,?,?,?,'request',?,?,?,'mapping_reissued')`, actor, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID, user, gen, now); e != nil {
			return 0, e
		}
	}
	action := "set"
	if deleted {
		action = "delete"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_audit(actor_user_id_lc,provider,instance,repository_id,change_directory,point_id,action,pm_user_id_lc,mapping_generation,at_utc,result) VALUES(?,?,?,?,?,?,?,?,?,?, 'ok')`, actor, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, k.PointID, action, user, gen, now)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return gen, nil
}
func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

type R9PMCreateInput struct {
	RequestID string
	R9PMKey
	CommitSHA, ContentSHA256, PMUserIDLC, ConsoleRecipient, FeishuRecipient, FeishuIssuer, SummaryJSON, CreatedByUserIDLC string
}
type R9PMRequest struct {
	RequestID         string `json:"request_id"`
	Generation        int64  `json:"generation"`
	MappingGeneration int64  `json:"mapping_generation"`
	PMUserIDLC        string `json:"pm_user_id_lc"`
	Status            string `json:"status"`
	CreatedAtUTC      string `json:"created_at_utc"`
	DeadlineAtUTC     string `json:"deadline_at_utc"`
}

// CreateR9PMRequest commits a request and its two initial channel outboxes together.
// A configured mapping is re-read in the same transaction, so callers cannot use a stale PM.
func (s *SQLite) CreateR9PMRequest(ctx context.Context, in R9PMCreateInput) (R9PMRequest, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return R9PMRequest{}, false, err
	}
	defer tx.Rollback()
	var pm sql.NullString
	var mg int64
	err = tx.QueryRowContext(ctx, `SELECT pm_user_id_lc,mapping_generation FROM r9_pm_responsibility WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=?`, in.Provider, in.Instance, in.RepositoryID, in.ChangeDirectory, in.PointID).Scan(&pm, &mg)
	if errors.Is(err, sql.ErrNoRows) || !pm.Valid || pm.String == "" {
		return R9PMRequest{}, false, ErrR9PMNotFound
	}
	if err != nil {
		return R9PMRequest{}, false, err
	}
	if in.PMUserIDLC != "" && in.PMUserIDLC != pm.String {
		return R9PMRequest{}, false, errors.New("R9 PM mapping changed")
	}
	in.PMUserIDLC = pm.String
	var existing R9PMRequest
	err = tx.QueryRowContext(ctx, `SELECT request_id,generation,mapping_generation,pm_user_id_lc,status,created_at_utc,deadline_at_utc FROM r9_pm_request WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND commit_sha=? AND point_id=? AND content_sha256=? AND pm_user_id_lc=? AND (status IN ('open','timed_out','unknown') OR created_at_utc>=?) ORDER BY generation DESC LIMIT 1`, in.Provider, in.Instance, in.RepositoryID, in.ChangeDirectory, in.CommitSHA, in.PointID, in.ContentSHA256, in.PMUserIDLC, s.now().UTC().Add(-24*time.Hour).Format(time.RFC3339Nano)).Scan(&existing.RequestID, &existing.Generation, &existing.MappingGeneration, &existing.PMUserIDLC, &existing.Status, &existing.CreatedAtUTC, &existing.DeadlineAtUTC)
	if err == nil {
		if e := tx.Commit(); e != nil {
			return R9PMRequest{}, false, e
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return R9PMRequest{}, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE r9_pm_request SET status='superseded',next_reminder_at_utc='' WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=? AND status IN ('open','timed_out','unknown')`, in.Provider, in.Instance, in.RepositoryID, in.ChangeDirectory, in.PointID)
	if err != nil {
		return R9PMRequest{}, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE r9_pm_delivery SET state='closed',reason='request content superseded' WHERE request_id IN (SELECT request_id FROM r9_pm_request WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=? AND status='superseded') AND state='pending'`, in.Provider, in.Instance, in.RepositoryID, in.ChangeDirectory, in.PointID)
	if err != nil {
		return R9PMRequest{}, false, err
	}
	var gen int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0)+1 FROM r9_pm_request WHERE provider=? AND instance=? AND repository_id=? AND change_directory=? AND point_id=?`, in.Provider, in.Instance, in.RepositoryID, in.ChangeDirectory, in.PointID).Scan(&gen)
	if err != nil {
		return R9PMRequest{}, false, err
	}
	now := s.now().UTC()
	created := now.Format(time.RFC3339Nano)
	deadline := now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_request(request_id,provider,instance,repository_id,change_directory,commit_sha,point_id,content_sha256,pm_user_id_lc,mapping_generation,generation,status,created_at_utc,deadline_at_utc,next_reminder_at_utc,summary_json,created_by_user_id_lc) VALUES(?,?,?,?,?,?,?,?,?,?,?,'open',?,?,?,?,?)`, in.RequestID, in.Provider, in.Instance, in.RepositoryID, in.ChangeDirectory, in.CommitSHA, in.PointID, in.ContentSHA256, in.PMUserIDLC, mg, gen, created, deadline, deadline, in.SummaryJSON, in.CreatedByUserIDLC)
	if err != nil {
		return R9PMRequest{}, false, err
	}
	for _, d := range []struct{ ch, recipient string }{{"console", in.ConsoleRecipient}, {"feishu", in.FeishuRecipient}} {
		op := in.RequestID + ":" + d.ch + ":initial"
		if _, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_delivery(operation_id,request_id,generation,channel,kind,period_index,recipient_id,issuer,state,created_at_utc) VALUES(?,?,?,?,'initial',0,?,?,'pending',?)`, op, in.RequestID, gen, d.ch, d.recipient, func() string {
			if d.ch == "feishu" {
				return in.FeishuIssuer
			}
			return ""
		}(), created); err != nil {
			return R9PMRequest{}, false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_audit(actor_user_id_lc,provider,instance,repository_id,change_directory,point_id,action,pm_user_id_lc,mapping_generation,at_utc,result) VALUES(?,?,?,?,?,?,'request',?,?,?,'created')`, "system:r9", in.Provider, in.Instance, in.RepositoryID, in.ChangeDirectory, in.PointID, in.PMUserIDLC, mg, created); err != nil {
		return R9PMRequest{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return R9PMRequest{}, false, err
	}
	return R9PMRequest{in.RequestID, gen, mg, in.PMUserIDLC, "open", created, deadline}, true, nil
}

type R9PMResponseInput struct {
	ResponseID, RequestID, PrincipalUserIDLC, Channel, Decision, Comment, Issuer, Tenant, MessageID, EventID, PayloadSHA256, OperationID string
	Generation                                                                                                                           int64
}

// AcceptR9PMResponse atomically arbitrates the one accepted response and closes both channels.
func (s *SQLite) AcceptR9PMResponse(ctx context.Context, in R9PMResponseInput) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var pm, status string
	var gen int64
	err = tx.QueryRowContext(ctx, `SELECT pm_user_id_lc,status,generation FROM r9_pm_request WHERE request_id=?`, in.RequestID).Scan(&pm, &status, &gen)
	if err != nil {
		return false, err
	}
	if pm != in.PrincipalUserIDLC {
		return false, errors.New("R9 response authority rejected")
	}
	var seq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(received_seq),0)+1 FROM r9_pm_response`).Scan(&seq); err != nil {
		return false, err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	late := gen != in.Generation || (status != "open" && status != "timed_out")
	if in.Channel == "feishu" {
		if in.Issuer == "" || in.Tenant == "" || in.MessageID == "" || in.PayloadSHA256 == "" {
			return false, errors.New("R9 Feishu event identity incomplete")
		}
		keys := []struct{ kind, id string }{{"message", in.MessageID}}
		if in.EventID != "" {
			keys = append(keys, struct{ kind, id string }{"event", in.EventID})
		}
		for _, key := range keys {
			_, e := tx.ExecContext(ctx, `INSERT INTO r9_pm_inbound_key(issuer,tenant,key_kind,event_or_message_id,payload_sha256,request_id,generation,received_at_utc,state) VALUES(?,?,?,?,?,?,?,?,?)`, in.Issuer, in.Tenant, key.kind, key.id, in.PayloadSHA256, in.RequestID, in.Generation, now, func() string {
				if late {
					return "late"
				}
				return "accepted"
			}())
			if e != nil {
				if strings.Contains(strings.ToLower(e.Error()), "unique") {
					var prior string
					q := tx.QueryRowContext(ctx, `SELECT payload_sha256 FROM r9_pm_inbound_key WHERE issuer=? AND tenant=? AND key_kind=? AND event_or_message_id=?`, in.Issuer, in.Tenant, key.kind, key.id).Scan(&prior)
					if q == nil && prior == in.PayloadSHA256 {
						return false, nil
					}
					return false, errors.New("R9 duplicate event payload conflict")
				}
				return false, e
			}
		}
	}
	if gen != in.Generation || (status != "open" && status != "timed_out") {
		_, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_response(response_id,request_id,generation,principal_user_id_lc,channel,decision,comment,issuer,tenant,message_id,operation_id,received_seq,received_at_utc,state,accepted_slot) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'late',0)`, in.ResponseID, in.RequestID, in.Generation, in.PrincipalUserIDLC, in.Channel, in.Decision, in.Comment, in.Issuer, in.Tenant, in.MessageID, in.OperationID, seq, now)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return false, nil
			}
			return false, err
		}
		if err = tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_response(response_id,request_id,generation,principal_user_id_lc,channel,decision,comment,issuer,tenant,message_id,operation_id,received_seq,received_at_utc,state,accepted_slot) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'accepted',1)`, in.ResponseID, in.RequestID, in.Generation, in.PrincipalUserIDLC, in.Channel, in.Decision, in.Comment, in.Issuer, in.Tenant, in.MessageID, in.OperationID, seq, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return false, nil
		}
		return false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE r9_pm_request SET status='answered',answered_at_utc=?,answer=?,next_reminder_at_utc='' WHERE request_id=? AND generation=? AND status IN ('open','timed_out')`, now, in.Decision, in.RequestID, in.Generation)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE r9_pm_delivery SET state='closed',reason='request answered' WHERE request_id=? AND generation=? AND state='pending'`, in.RequestID, in.Generation); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_audit(actor_user_id_lc,provider,instance,repository_id,change_directory,point_id,action,pm_user_id_lc,mapping_generation,at_utc,result) SELECT ?,provider,instance,repository_id,change_directory,point_id,'response',?,mapping_generation,?,'accepted' FROM r9_pm_request WHERE request_id=?`, in.PrincipalUserIDLC, in.PrincipalUserIDLC, now, in.RequestID); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

type R9PMReplyTarget struct {
	RequestID, PMUserIDLC, Status, Provider, Instance, RepositoryID, ChangeDirectory, CommitSHA string
	Generation                                                                                  int64
}

func (s *SQLite) R9PMReplyTarget(ctx context.Context, providerMessageID string) (*R9PMReplyTarget, error) {
	var v R9PMReplyTarget
	err := s.db.QueryRowContext(ctx, `SELECT r.request_id,r.pm_user_id_lc,r.status,r.generation,r.provider,r.instance,r.repository_id,r.change_directory,r.commit_sha FROM r9_pm_delivery d JOIN r9_pm_request r ON r.request_id=d.request_id AND r.generation=d.generation WHERE d.provider_message_id=? AND d.channel='feishu' AND d.state='sent'`, providerMessageID).Scan(&v.RequestID, &v.PMUserIDLC, &v.Status, &v.Generation, &v.Provider, &v.Instance, &v.RepositoryID, &v.ChangeDirectory, &v.CommitSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrR9PMNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// RunR9PMScheduler advances due request cycles and inserts one current reminder slot,
// skipping missed intervals instead of generating a reminder backlog after downtime.
func (s *SQLite) RunR9PMScheduler(ctx context.Context, now time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT request_id,generation,created_at_utc,deadline_at_utc,next_reminder_at_utc FROM r9_pm_request WHERE status IN ('open','timed_out') AND next_reminder_at_utc<>'' AND next_reminder_at_utc<=? ORDER BY next_reminder_at_utc,request_id LIMIT 500`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	type due struct {
		id                      string
		generation              int64
		created, deadline, next string
	}
	var list []due
	for rows.Next() {
		var d due
		if err = rows.Scan(&d.id, &d.generation, &d.created, &d.deadline, &d.next); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, d := range list {
		deadline, e := time.Parse(time.RFC3339Nano, d.deadline)
		if e != nil {
			return 0, e
		}
		elapsed := now.UTC().Sub(deadline)
		period := int64(0)
		if elapsed > 0 {
			period = int64(elapsed / (6 * time.Hour))
		}
		previousPeriod := int64(-1)
		if d.next != d.deadline {
			if previousDue, e := time.Parse(time.RFC3339Nano, d.next); e == nil {
				previousPeriod = int64(previousDue.Sub(deadline)/(6*time.Hour)) - 1
			}
		}
		missed := period - previousPeriod - 1
		if missed < 0 {
			missed = 0
		}
		next := deadline.Add(time.Duration(period+1) * 6 * time.Hour).Format(time.RFC3339Nano)
		at := now.UTC().Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx, `UPDATE r9_pm_request SET status='timed_out',next_reminder_at_utc=?,missed_periods=missed_periods+? WHERE request_id=? AND generation=? AND status IN ('open','timed_out')`, next, missed, d.id, d.generation)
		if err != nil {
			return 0, err
		}
		for _, channel := range []string{"console", "feishu"} {
			op := fmt.Sprintf("%s:%s:reminder:%d", d.id, channel, period)
			_, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_delivery(operation_id,request_id,generation,channel,kind,period_index,recipient_id,issuer,state,created_at_utc) SELECT ?,r.request_id,r.generation,?,'reminder',?,CASE WHEN ?='console' THEN r.pm_user_id_lc ELSE COALESCE((SELECT recipient_id FROM r9_pm_delivery WHERE request_id=r.request_id AND generation=r.generation AND channel='feishu' AND kind='initial'), '') END,CASE WHEN ?='feishu' THEN COALESCE((SELECT issuer FROM r9_pm_delivery WHERE request_id=r.request_id AND generation=r.generation AND channel='feishu' AND kind='initial'), '') ELSE '' END,'pending',? FROM r9_pm_request r WHERE r.request_id=? AND r.generation=? AND r.status='timed_out' ON CONFLICT(request_id,generation,channel,kind,period_index) DO NOTHING`, op, channel, period, channel, channel, at, d.id, d.generation)
			if err != nil {
				return 0, err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO r9_pm_audit(actor_user_id_lc,provider,instance,repository_id,change_directory,point_id,action,pm_user_id_lc,mapping_generation,at_utc,result) SELECT 'system:r9',provider,instance,repository_id,change_directory,point_id,'timeout',pm_user_id_lc,mapping_generation,?,'reminder' FROM r9_pm_request WHERE request_id=?`, at, d.id)
		if err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(list), nil
}

type R9PMDelivery struct {
	OperationID, RequestID, Channel, RecipientID, Issuer, Kind, SummaryJSON, CommitSHA, PointID, ContentSHA256 string
	Generation, PeriodIndex                                                                                    int64
}

func (s *SQLite) ListR9PMFeishuDeliveries(ctx context.Context, limit int) ([]R9PMDelivery, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.operation_id,d.request_id,d.generation,d.period_index,d.recipient_id,d.issuer,d.kind,r.summary_json,r.commit_sha,r.point_id,r.content_sha256 FROM r9_pm_delivery d JOIN r9_pm_request r ON r.request_id=d.request_id AND r.generation=d.generation WHERE d.channel='feishu' AND d.state='pending' AND r.status IN ('open','timed_out') ORDER BY d.created_at_utc,d.operation_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []R9PMDelivery
	for rows.Next() {
		var d R9PMDelivery
		if err = rows.Scan(&d.OperationID, &d.RequestID, &d.Generation, &d.PeriodIndex, &d.RecipientID, &d.Issuer, &d.Kind, &d.SummaryJSON, &d.CommitSHA, &d.PointID, &d.ContentSHA256); err != nil {
			return nil, err
		}
		d.Channel = "feishu"
		out = append(out, d)
	}
	return out, rows.Err()
}

// StartR9PMDeliveryAttempt changes pending to unknown before the external call. A lost receipt
// therefore cannot be mistaken for success or retried under a new operation ID.
func (s *SQLite) StartR9PMDeliveryAttempt(ctx context.Context, operationID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE r9_pm_delivery SET state='unknown',attempts=attempts+1,attempted_at_utc=? WHERE operation_id=? AND channel='feishu' AND state='pending'`, s.now().UTC().Format(time.RFC3339Nano), operationID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
func (s *SQLite) CompleteR9PMDelivery(ctx context.Context, operationID, messageID string) error {
	if messageID == "" {
		return errors.New("R9 delivery receipt is unknown")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE r9_pm_delivery SET state='sent',provider_message_id=?,receipt_at_utc=?,reason='' WHERE operation_id=? AND state='unknown'`, messageID, s.now().UTC().Format(time.RFC3339Nano), operationID)
	return err
}

type R9PMConsoleTask struct {
	RequestID         string `json:"request_id"`
	Provider          string `json:"provider"`
	Instance          string `json:"instance"`
	RepositoryID      string `json:"repository_id"`
	ChangeDirectory   string `json:"change_directory"`
	CommitSHA         string `json:"commit_sha"`
	PointID           string `json:"point_id"`
	ContentSHA256     string `json:"content_sha256"`
	Status            string `json:"status"`
	MRWritebackStatus string `json:"mr_writeback_status"`
	CreatedAtUTC      string `json:"created_at_utc"`
	DeadlineAtUTC     string `json:"deadline_at_utc"`
	SummaryJSON       string `json:"summary_json"`
	Generation        int64  `json:"generation"`
}

func (s *SQLite) ListR9PMConsoleTasks(ctx context.Context, principal string) ([]R9PMConsoleTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.request_id,r.provider,r.instance,r.repository_id,r.change_directory,r.commit_sha,r.point_id,r.content_sha256,r.status,r.mr_writeback_status,r.created_at_utc,r.deadline_at_utc,r.summary_json,r.generation FROM r9_pm_request r JOIN r9_pm_delivery d ON d.request_id=r.request_id AND d.generation=r.generation AND d.channel='console' WHERE r.pm_user_id_lc=? AND r.status IN ('open','timed_out') AND d.state IN ('pending','sent','unknown') ORDER BY r.created_at_utc,r.request_id LIMIT 100`, principal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []R9PMConsoleTask
	for rows.Next() {
		var v R9PMConsoleTask
		if err = rows.Scan(&v.RequestID, &v.Provider, &v.Instance, &v.RepositoryID, &v.ChangeDirectory, &v.CommitSHA, &v.PointID, &v.ContentSHA256, &v.Status, &v.MRWritebackStatus, &v.CreatedAtUTC, &v.DeadlineAtUTC, &v.SummaryJSON, &v.Generation); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *SQLite) GetR9PMRequestAuthority(ctx context.Context, requestID string) (*R9PMReplyTarget, error) {
	var v R9PMReplyTarget
	err := s.db.QueryRowContext(ctx, `SELECT request_id,pm_user_id_lc,status,generation,provider,instance,repository_id,change_directory,commit_sha FROM r9_pm_request WHERE request_id=?`, requestID).Scan(&v.RequestID, &v.PMUserIDLC, &v.Status, &v.Generation, &v.Provider, &v.Instance, &v.RepositoryID, &v.ChangeDirectory, &v.CommitSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrR9PMNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}
