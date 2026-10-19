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

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var ErrSpecAgentConflict = errors.New("spec agent request conflict")
var ErrSpecAgentInvalid = errors.New("invalid spec agent result")

type SpecAgentRequest struct {
	ID             string          `json:"request_id"`
	SpecID         string          `json:"spec_id"`
	Revision       int             `json:"revision"`
	Kind           string          `json:"kind"`
	Actor          string          `json:"actor"`
	AgentID        string          `json:"agent_id"`
	KeyID          string          `json:"-"`
	SessionName    string          `json:"-"`
	SHA            string          `json:"content_sha256"`
	ClientID       string          `json:"-"`
	Status         string          `json:"status"`
	Deadline       int64           `json:"deadline"`
	Boot           string          `json:"-"`
	Snapshot       json.RawMessage `json:"snapshot"`
	Result         json.RawMessage `json:"result,omitempty"`
	Digest         string          `json:"-"`
	Failure        string          `json:"failure,omitempty"`
	ResultRevision int             `json:"result_revision,omitempty"`
}
type SpecAgentSnapshot struct {
	Content    string          `json:"content_markdown"`
	Acceptance json.RawMessage `json:"acceptance"`
	Anchors    json.RawMessage `json:"input_anchors"`
}
type SpecAgentIssue struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
type SpecAgentCriterion struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	OwnerRef    string   `json:"owner_ref"`
	Cases       []string `json:"cases"`
	Source      string   `json:"source,omitempty"`
	GeneratedBy string   `json:"generated_by,omitempty"`
	CheckedBy   string   `json:"checked_by,omitempty"`
	CheckedAt   string   `json:"checked_at,omitempty"`
}
type SpecAgentResult struct {
	Revision int                  `json:"revision"`
	SHA      string               `json:"content_sha256"`
	Issues   []SpecAgentIssue     `json:"issues,omitempty"`
	Criteria []SpecAgentCriterion `json:"criteria,omitempty"`
}

const specAgentCols = `id,spec_id,revision,kind,actor,agent_id,key_id,session_name,snapshot_sha,client_id,status,deadline,boot,request_json,result_json,result_digest,failure,result_revision`

func scanSpecAgent(row interface{ Scan(...any) error }) (*SpecAgentRequest, error) {
	q := new(SpecAgentRequest)
	var snapshot, result string
	err := row.Scan(&q.ID, &q.SpecID, &q.Revision, &q.Kind, &q.Actor, &q.AgentID, &q.KeyID, &q.SessionName, &q.SHA, &q.ClientID, &q.Status, &q.Deadline, &q.Boot, &snapshot, &result, &q.Digest, &q.Failure, &q.ResultRevision)
	if err != nil {
		return nil, err
	}
	q.Snapshot = json.RawMessage(snapshot)
	if result != "" {
		q.Result = json.RawMessage(result)
	}
	return q, nil
}
func expireSpecAgent(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, e := tx.ExecContext(ctx, `UPDATE console_spec_agent_request SET status='timed_out',failure='deadline expired',updated_at=? WHERE status IN ('pending','awaiting_confirmation') AND deadline<=?`, now.UTC().Format(time.RFC3339Nano), now.Unix())
	return e
}
func (s *SQLite) ReconcileSpecAgentRequests(ctx context.Context, boot string, now time.Time) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = expireSpecAgent(ctx, tx, now); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE console_spec_agent_request SET status='failed',failure='Hub restarted',updated_at=? WHERE status IN ('pending','awaiting_confirmation') AND boot<>?`, now.UTC().Format(time.RFC3339Nano), boot); e != nil {
		return e
	}
	return tx.Commit()
}
func specAgentEditable(ctx context.Context, tx *sql.Tx, spec, actor string, revision int, sha string) error {
	var n int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=s.current_revision WHERE s.id=? AND s.current_editor_id=? AND s.current_revision=? AND r.content_sha256=? AND s.state IN ('editing','review') AND s.archived=0 AND s.editing_closed=0`, spec, actor, revision, sha).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrSpecAgentConflict
	}
	e = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM console_task_proposal WHERE spec_id=? AND revision=?)+(SELECT count(*) FROM console_spec_task WHERE spec_id=? AND revision=?)`, spec, revision, spec, revision).Scan(&n)
	if e != nil {
		return e
	}
	if n != 0 {
		return ErrConsoleSpecRevisionLocked
	}
	return nil
}
func (s *SQLite) StartSpecAgentRequest(ctx context.Context, q SpecAgentRequest, check func() error, now time.Time) (*SpecAgentRequest, error) {
	if q.ID == "" || q.ClientID == "" || len(q.ClientID) > 128 || (q.Kind != "review" && q.Kind != "criteria") {
		return nil, ErrSpecAgentInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = expireSpecAgent(ctx, tx, now); e != nil {
		return nil, e
	}
	old, e := scanSpecAgent(tx.QueryRowContext(ctx, `SELECT `+specAgentCols+` FROM console_spec_agent_request WHERE actor=? AND client_id=?`, q.Actor, q.ClientID))
	if e == nil {
		if old.SpecID != q.SpecID || old.AgentID != q.AgentID || old.Kind != q.Kind || old.Revision != q.Revision || old.SHA != q.SHA {
			return nil, ErrSpecAgentConflict
		}
		if check != nil {
			if e = check(); e != nil {
				return nil, e
			}
		}
		return old, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e = specAgentEditable(ctx, tx, q.SpecID, q.Actor, q.Revision, q.SHA); e != nil {
		return nil, e
	}
	var agent model.ConsoleAgent
	var active int
	e = tx.QueryRowContext(ctx, `SELECT id,key_id,session_name,owner_principal,active FROM console_agent WHERE id=? AND owner_principal=? AND active=1`, q.AgentID, q.Actor).Scan(&agent.ID, &agent.KeyID, &agent.SessionName, &agent.OwnerPrincipal, &active)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrConsoleSpecPermission
	}
	if e != nil {
		return nil, e
	}
	q.KeyID, q.SessionName = agent.KeyID, agent.SessionName
	var content, acceptance, anchors string
	if e = tx.QueryRowContext(ctx, `SELECT content_markdown,acceptance_json,input_anchors_json FROM console_spec_revision WHERE spec_id=? AND revision=?`, q.SpecID, q.Revision).Scan(&content, &acceptance, &anchors); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(SpecAgentSnapshot{content, json.RawMessage(acceptance), json.RawMessage(anchors)})
	if e != nil {
		return nil, e
	}
	if len(raw) > 1<<20 {
		return nil, ErrSpecAgentInvalid
	}
	q.Snapshot = raw
	// State is changed before creating the lease, so all existing writes remain guarded.
	if q.Kind == "review" {
		if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO console_spec_agent_fresh(spec_id,revision) VALUES(?,?)`, q.SpecID, q.Revision); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE console_spec_agent_gate SET human_confirmed=0 WHERE spec_id=? AND revision=?`, q.SpecID, q.Revision); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE console_spec SET state='review' WHERE id=?`, q.SpecID); e != nil {
			return nil, e
		}
	}
	q.Status = "pending"
	q.Deadline = now.Add(180 * time.Second).Unix()
	stamp := now.UTC().Format(time.RFC3339Nano)
	_, e = tx.ExecContext(ctx, `INSERT INTO console_spec_agent_request (`+specAgentCols+`,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, q.ID, q.SpecID, q.Revision, q.Kind, q.Actor, q.AgentID, q.KeyID, q.SessionName, q.SHA, q.ClientID, q.Status, q.Deadline, q.Boot, string(q.Snapshot), "", "", "", 0, stamp, stamp)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO console_spec_agent_outbox(request_id) VALUES(?)`, q.ID); e != nil {
		return nil, e
	}
	if e = appendConsoleSpecEvent(ctx, tx, q.SpecID, q.Revision, q.Actor, "agent_"+q.Kind+"_requested", q.ID, now); e != nil {
		return nil, e
	}
	if check != nil {
		if e = check(); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &q, nil
}
func (s *SQLite) SpecAgentRequest(ctx context.Context, id string) (*SpecAgentRequest, error) {
	return scanSpecAgent(s.db.QueryRowContext(ctx, `SELECT `+specAgentCols+` FROM console_spec_agent_request WHERE id=?`, id))
}
func (s *SQLite) ListSpecAgentRequests(ctx context.Context, agent, actor, spec, cursor string) ([]SpecAgentRequest, error) {
	order := "id"
	if actor != "" && spec != "" {
		order = "CASE WHEN status IN ('pending','awaiting_confirmation') THEN 0 ELSE 1 END,created_at DESC,id DESC"
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+specAgentCols+` FROM console_spec_agent_request WHERE (?='' OR agent_id=?) AND (?='' OR actor=?) AND (?='' OR spec_id=?) AND id>? ORDER BY `+order+` LIMIT 100`, agent, agent, actor, actor, spec, spec, cursor)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SpecAgentRequest{}
	for rows.Next() {
		q, e := scanSpecAgent(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *q)
	}
	return out, rows.Err()
}
func sameSpecAgent(q *SpecAgentRequest, a model.ConsoleAgent) bool {
	return a.Active && a.ID == q.AgentID && a.OwnerPrincipal == q.Actor && a.KeyID == q.KeyID && a.SessionName == q.SessionName
}
func validateSpecAgentResult(q *SpecAgentRequest, r SpecAgentResult) (string, error) {
	if r.Revision != q.Revision || r.SHA != q.SHA {
		return "", ErrSpecAgentConflict
	}
	var snap SpecAgentSnapshot
	if json.Unmarshal(q.Snapshot, &snap) != nil {
		return "", ErrSpecAgentInvalid
	}
	if q.Kind == "review" {
		if r.Criteria != nil || r.Issues == nil || len(r.Issues) > 100 {
			return "", ErrSpecAgentInvalid
		}
		seen := map[string]bool{}
		issues := []storedSpecGateIssue{}
		for _, i := range r.Issues {
			if strings.TrimSpace(i.ID) == "" || strings.TrimSpace(i.Text) == "" || seen[i.ID] {
				return "", ErrSpecAgentInvalid
			}
			seen[i.ID] = true
			issues = append(issues, storedSpecGateIssue{ID: i.ID, Text: i.Text, Outcome: "pending"})
		}
		b, e := json.Marshal(issues)
		return string(b), e
	}
	if r.Issues != nil || len(r.Criteria) == 0 || len(r.Criteria) > 100 {
		return "", ErrSpecAgentInvalid
	}
	var prior []SpecAgentCriterion
	if strings.HasPrefix(strings.TrimSpace(string(snap.Acceptance)), "[") {
		if json.Unmarshal(snap.Acceptance, &prior) != nil {
			return "", ErrSpecAgentInvalid
		}
	} else {
		var b struct {
			Criteria []SpecAgentCriterion `json:"criteria"`
		}
		if json.Unmarshal(snap.Acceptance, &b) != nil {
			return "", ErrSpecAgentInvalid
		}
		prior = b.Criteria
	}
	index := map[string]int{}
	for i, c := range prior {
		index[c.ID] = i
	}
	seen := map[string]bool{}
	for _, c := range r.Criteria {
		if c.ID == "" || strings.TrimSpace(c.Text) == "" || strings.TrimSpace(c.OwnerRef) == "" || !strings.Contains(snap.Content, c.OwnerRef) || len(c.Cases) == 0 || len(c.Cases) > 100 || seen[c.ID] || c.CheckedBy != "" || c.CheckedAt != "" || c.GeneratedBy != "" || c.Source != "" {
			return "", ErrSpecAgentInvalid
		}
		seen[c.ID] = true
		for _, v := range c.Cases {
			if strings.TrimSpace(v) == "" {
				return "", ErrSpecAgentInvalid
			}
		}
		c.Source = "ai"
		c.GeneratedBy = q.AgentID
		if i, ok := index[c.ID]; ok {
			prior[i] = c
		} else {
			index[c.ID] = len(prior)
			prior = append(prior, c)
		}
	}
	if len(prior) > 100 {
		return "", ErrSpecAgentInvalid
	}
	bundle := map[string]json.RawMessage{}
	if !strings.HasPrefix(strings.TrimSpace(string(snap.Acceptance)), "[") {
		if json.Unmarshal(snap.Acceptance, &bundle) != nil {
			return "", ErrSpecAgentInvalid
		}
	}
	encoded, e := json.Marshal(prior)
	if e != nil {
		return "", e
	}
	bundle["criteria"] = encoded
	b, e := json.Marshal(bundle)
	return string(b), e
}
func (s *SQLite) CompleteSpecAgentRequest(ctx context.Context, id string, a model.ConsoleAgent, r SpecAgentResult, check func() error, now time.Time) (*SpecAgentRequest, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = expireSpecAgent(ctx, tx, now); e != nil {
		return nil, e
	}
	q, e := scanSpecAgent(tx.QueryRowContext(ctx, `SELECT `+specAgentCols+` FROM console_spec_agent_request WHERE id=?`, id))
	if e != nil {
		return nil, e
	}
	if !sameSpecAgent(q, a) {
		return nil, ErrConsoleSpecPermission
	}
	payload, e := validateSpecAgentResult(q, r)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256([]byte(payload))
	digest := hex.EncodeToString(sum[:])
	if q.Status != "pending" {
		if (q.Status == "completed" || q.Status == "awaiting_confirmation") && q.Digest == digest {
			if e = specAgentEditable(ctx, tx, q.SpecID, q.Actor, q.Revision, q.SHA); e != nil {
				return nil, e
			}
			if check != nil {
				if e = check(); e != nil {
					return nil, e
				}
			}
			return q, nil
		}
		return nil, ErrSpecAgentConflict
	}
	if e = specAgentEditable(ctx, tx, q.SpecID, q.Actor, q.Revision, q.SHA); e != nil {
		return nil, e
	}
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_agent WHERE id=? AND key_id=? AND session_name=? AND owner_principal=? AND active=1`, a.ID, a.KeyID, a.SessionName, a.OwnerPrincipal).Scan(&n); e != nil {
		return nil, e
	}
	if n != 1 {
		return nil, ErrConsoleSpecPermission
	}
	q.Status = "completed"
	if q.Kind == "criteria" {
		q.Status = "awaiting_confirmation"
		q.Deadline = now.Add(15 * time.Minute).Unix()
	}
	q.Result = json.RawMessage(payload)
	q.Digest = digest
	if _, e = tx.ExecContext(ctx, `UPDATE console_spec_agent_request SET status=?,deadline=?,result_json=?,result_digest=?,updated_at=? WHERE id=? AND status='pending'`, q.Status, q.Deadline, payload, digest, now.UTC().Format(time.RFC3339Nano), id); e != nil {
		return nil, e
	}
	if q.Kind == "review" {
		_, e = tx.ExecContext(ctx, `INSERT INTO console_spec_gate_review(id,spec_id,revision,review_agent_id,operator_principal,issues_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(spec_id,revision) DO UPDATE SET review_agent_id=excluded.review_agent_id,operator_principal=excluded.operator_principal,issues_json=excluded.issues_json,updated_at=excluded.updated_at`, newID(), q.SpecID, q.Revision, a.ID, q.Actor, payload, now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
		if e != nil {
			return nil, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO console_spec_agent_gate(spec_id,revision,request_id,agent_id,issues_json,human_confirmed) VALUES(?,?,?,?,?,0) ON CONFLICT(spec_id,revision) DO UPDATE SET request_id=excluded.request_id,agent_id=excluded.agent_id,issues_json=excluded.issues_json,human_confirmed=0`, q.SpecID, q.Revision, q.ID, a.ID, payload)
		if e != nil {
			return nil, e
		}
	}
	if e = appendConsoleSpecEvent(ctx, tx, q.SpecID, q.Revision, a.ID, "agent_"+q.Kind+"_received", q.ID, now); e != nil {
		return nil, e
	}
	if check != nil {
		if e = check(); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return q, nil
}
func (s *SQLite) FailSpecAgentRequest(ctx context.Context, id, reason string, now time.Time) error {
	_, e := s.db.ExecContext(ctx, `UPDATE console_spec_agent_request SET status='failed',failure=?,updated_at=? WHERE id=? AND status IN ('pending','awaiting_confirmation')`, reason, now.UTC().Format(time.RFC3339Nano), id)
	return e
}
func (s *SQLite) MarkSpecAgentSent(ctx context.Context, id string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE console_spec_agent_outbox SET sent=1 WHERE request_id=?`, id)
	return e
}
func (s *SQLite) PendingSpecAgentSends(ctx context.Context) ([]string, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT o.request_id FROM console_spec_agent_outbox o JOIN console_spec_agent_request q ON q.id=o.request_id WHERE o.sent=0 AND q.status='pending' ORDER BY o.request_id LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Confirm only the bounded candidate; caller holds current PG identity locks.
func (s *SQLite) ConfirmSpecAgentCriteria(ctx context.Context, id, actor string, revision int, sha string, check func() error, now time.Time) (int, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if e = expireSpecAgent(ctx, tx, now); e != nil {
		return 0, e
	}
	q, e := scanSpecAgent(tx.QueryRowContext(ctx, `SELECT `+specAgentCols+` FROM console_spec_agent_request WHERE id=?`, id))
	if e != nil {
		return 0, e
	}
	if q.Actor != actor || q.Kind != "criteria" || q.Revision != revision || q.SHA != sha {
		return 0, ErrConsoleSpecPermission
	}
	if q.Status == "completed" && q.ResultRevision > 0 {
		var editor string
		if e = tx.QueryRowContext(ctx, `SELECT current_editor_id FROM console_spec WHERE id=?`, q.SpecID).Scan(&editor); e != nil {
			return 0, e
		}
		if editor != actor {
			return 0, ErrConsoleSpecPermission
		}
		if check != nil {
			if e = check(); e != nil {
				return 0, e
			}
		}
		return q.ResultRevision, nil
	}
	if q.Status != "awaiting_confirmation" {
		return 0, ErrSpecAgentConflict
	}
	if e = specAgentEditable(ctx, tx, q.SpecID, actor, revision, sha); e != nil {
		return 0, e
	}
	var snap SpecAgentSnapshot
	if json.Unmarshal(q.Snapshot, &snap) != nil {
		return 0, ErrSpecAgentInvalid
	}
	var b struct {
		Criteria []SpecAgentCriterion `json:"criteria"`
	}
	if json.Unmarshal(q.Result, &b) != nil || len(b.Criteria) == 0 {
		return 0, ErrSpecAgentInvalid
	}
	for i := range b.Criteria {
		c := &b.Criteria[i]
		if strings.TrimSpace(c.OwnerRef) == "" || !strings.Contains(snap.Content, c.OwnerRef) || len(c.Cases) == 0 {
			return 0, ErrSpecAgentInvalid
		}
		c.CheckedBy = actor
		c.CheckedAt = now.UTC().Format(time.RFC3339Nano)
	}
	bundle := map[string]json.RawMessage{}
	if json.Unmarshal(q.Result, &bundle) != nil {
		return 0, ErrSpecAgentInvalid
	}
	encoded, e := json.Marshal(b.Criteria)
	if e != nil {
		return 0, e
	}
	bundle["criteria"] = encoded
	acceptance, e := json.Marshal(bundle)
	if e != nil {
		return 0, e
	}
	next := revision + 1
	if _, e = tx.ExecContext(ctx, `UPDATE console_spec_agent_request SET status='completed',result_revision=?,updated_at=? WHERE id=?`, next, now.UTC().Format(time.RFC3339Nano), id); e != nil {
		return 0, e
	}
	rev := model.ConsoleSpecRevision{SpecID: q.SpecID, Revision: next, ContentMarkdown: snap.Content, AcceptanceJSON: string(acceptance), InputAnchorsJSON: string(snap.Anchors), ContentSHA256: q.SHA, CreatedBy: actor, CreatedByKind: "principal", CreatedVia: "principal", Supersedes: revision}
	if e = insertConsoleSpecRevision(ctx, tx, rev, now); e != nil {
		return 0, e
	}
	res, e := tx.ExecContext(ctx, `UPDATE console_spec SET current_revision=?,state='editing',updated_at=? WHERE id=? AND current_editor_id=? AND current_revision=? AND state IN ('editing','review') AND archived=0 AND editing_closed=0`, next, now.UTC().Format(time.RFC3339Nano), q.SpecID, actor, revision)
	if e != nil {
		return 0, e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return 0, e
	}
	if n != 1 {
		return 0, ErrSpecAgentConflict
	}
	if _, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO console_spec_agent_fresh(spec_id,revision) VALUES(?,?)`, q.SpecID, next); e != nil {
		return 0, e
	}
	if e = appendConsoleSpecEvent(ctx, tx, q.SpecID, next, actor, "agent_criteria_confirm", id, now); e != nil {
		return 0, e
	}
	if check != nil {
		if e = check(); e != nil {
			return 0, e
		}
	}
	if e = tx.Commit(); e != nil {
		return 0, e
	}
	return next, nil
}

// Cancellation and handoff share the same transaction; invalid targets do not consume the request.
func (s *SQLite) CancelTransferSpecAgent(ctx context.Context, spec, actor, target, state, id string, revision int, sha string, now time.Time) error {
	if state != "editing" && state != "review" {
		return ErrSpecAgentInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	recipient, e := resolveOwnerPrincipalByShortName(ctx, tx, target)
	if e != nil {
		return e
	}
	if recipient.ID == actor {
		return ErrConsoleSpecSelfTransfer
	}
	q, e := scanSpecAgent(tx.QueryRowContext(ctx, `SELECT `+specAgentCols+` FROM console_spec_agent_request WHERE id=?`, id))
	if e != nil {
		return e
	}
	if q.SpecID != spec || q.Actor != actor || q.Revision != revision || q.SHA != sha || (q.Status != "pending" && q.Status != "awaiting_confirmation") {
		return ErrSpecAgentConflict
	}
	if e = specAgentEditable(ctx, tx, spec, actor, revision, sha); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE console_spec_agent_request SET status='cancelled',failure='transferred',updated_at=? WHERE id=?`, now.UTC().Format(time.RFC3339Nano), id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM console_spec_agent_outbox WHERE request_id=?`, id); e != nil {
		return e
	}
	res, e := tx.ExecContext(ctx, `UPDATE console_spec SET current_editor_id=?,state=?,updated_at=? WHERE id=? AND current_editor_id=? AND current_revision=?`, recipient.ID, state, now.UTC().Format(time.RFC3339Nano), spec, actor, revision)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrSpecAgentConflict
	}
	if e = appendConsoleSpecEvent(ctx, tx, spec, revision, actor, "agent_cancel_transfer", fmt.Sprintf("%s to=%s", id, recipient.ID), now); e != nil {
		return e
	}
	return tx.Commit()
}

// Human annotation may resolve issues; it cannot replace the authenticated provenance.
func annotateSpecAgentGate(ctx context.Context, tx *sql.Tx, spec string, revision int, review model.ConsoleSpecGateReview) error {
	var agent, raw string
	e := tx.QueryRowContext(ctx, `SELECT agent_id,issues_json FROM console_spec_agent_gate WHERE spec_id=? AND revision=?`, spec, revision).Scan(&agent, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if review.ReviewAgentID != agent {
		return ErrSpecAgentInvalid
	}
	var original, annotated []storedSpecGateIssue
	if json.Unmarshal([]byte(raw), &original) != nil || json.Unmarshal([]byte(review.IssuesJSON), &annotated) != nil || len(original) != len(annotated) {
		return ErrSpecAgentInvalid
	}
	for i, o := range original {
		a := annotated[i]
		if a.ID != o.ID || a.Text != o.Text {
			return ErrSpecAgentInvalid
		}
		if (a.Outcome != "true_problem" && a.Outcome != "false_positive") || strings.TrimSpace(a.Response) == "" {
			return ErrConsoleSpecGate
		}
	}
	_, e = tx.ExecContext(ctx, `UPDATE console_spec_agent_gate SET human_confirmed=1 WHERE spec_id=? AND revision=?`, spec, revision)
	return e
}
func (s *SQLite) CancelSpecAgentRequest(ctx context.Context, id, actor string, now time.Time) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	q, e := scanSpecAgent(tx.QueryRowContext(ctx, `SELECT `+specAgentCols+` FROM console_spec_agent_request WHERE id=?`, id))
	if e != nil {
		return e
	}
	if q.Actor != actor {
		return ErrConsoleSpecPermission
	}
	if e = specAgentEditable(ctx, tx, q.SpecID, actor, q.Revision, q.SHA); e != nil {
		return e
	}
	if q.Status != "pending" && q.Status != "awaiting_confirmation" {
		return ErrSpecAgentConflict
	}
	if _, e = tx.ExecContext(ctx, `UPDATE console_spec_agent_request SET status='cancelled',failure='cancelled by editor',updated_at=? WHERE id=?`, now.UTC().Format(time.RFC3339Nano), id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM console_spec_agent_outbox WHERE request_id=?`, id); e != nil {
		return e
	}
	return tx.Commit()
}

func confirmedEmptySpecAgentGate(ctx context.Context, tx *sql.Tx, spec string, revision int, raw string) (bool, error) {
	var issues []storedSpecGateIssue
	if json.Unmarshal([]byte(raw), &issues) != nil || issues == nil || len(issues) != 0 {
		return false, nil
	}
	var n int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_agent_gate WHERE spec_id=? AND revision=? AND issues_json='[]' AND human_confirmed=1`, spec, revision).Scan(&n)
	return n == 1, e
}
