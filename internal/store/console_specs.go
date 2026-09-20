package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var (
	ErrConsoleSpecNotFound  = errors.New("console spec not found")
	ErrConsoleSpecConflict  = errors.New("console spec revision conflict")
	ErrConsoleSpecGateOrder = errors.New("console spec gate order invalid")
)

func specTime(v string) (time.Time, error) { return time.Parse(time.RFC3339Nano, v) }

func appendConsoleSpecEvent(ctx context.Context, tx *sql.Tx, specID string, revision int, actor, action, detail string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO console_spec_event(id,spec_id,revision,actor,action,detail,at) VALUES(?,?,?,?,?,?,?)`,
		newID(), specID, revision, actor, action, detail, at.UTC().Format(time.RFC3339Nano))
	return err
}

func insertConsoleSpecRevision(ctx context.Context, tx *sql.Tx, revision model.ConsoleSpecRevision, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,supersedes_revision,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, revision.SpecID, revision.Revision, revision.ContentMarkdown, revision.AcceptanceJSON,
		revision.InputAnchorsJSON, revision.ContentSHA256, revision.CreatedBy, nullInt(revision.Supersedes), at.UTC().Format(time.RFC3339Nano))
	return err
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func (s *SQLite) CreateConsoleSpec(ctx context.Context, spec model.ConsoleSpec, revision model.ConsoleSpecRevision, now time.Time) error {
	if strings.TrimSpace(spec.ID) == "" || strings.TrimSpace(spec.PrincipalID) == "" || strings.TrimSpace(spec.Title) == "" ||
		revision.SpecID != spec.ID || revision.Revision != 1 || revision.CreatedBy != spec.PrincipalID {
		return errors.New("invalid console spec")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec(id,principal_id,title,state,current_revision,created_at,updated_at) VALUES(?,?,?,'draft',1,?,?)`,
		spec.ID, spec.PrincipalID, spec.Title, stamp, stamp); err != nil {
		return err
	}
	if err = insertConsoleSpecRevision(ctx, tx, revision, now); err != nil {
		return err
	}
	if err = appendConsoleSpecEvent(ctx, tx, spec.ID, 1, spec.PrincipalID, "create", "", now); err != nil {
		return err
	}
	return tx.Commit()
}

func scanConsoleSpec(row rowScanner) (*model.ConsoleSpec, error) {
	var spec model.ConsoleSpec
	var claimed sql.NullString
	var created, updated string
	err := row.Scan(&spec.ID, &spec.PrincipalID, &spec.Title, &spec.State, &spec.CurrentRevision, &claimed, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	spec.ClaimedBy = nullableString(claimed)
	spec.CreatedAt, err = specTime(created)
	if err != nil {
		return nil, err
	}
	spec.UpdatedAt, err = specTime(updated)
	if err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *SQLite) ConsoleSpec(ctx context.Context, principalID, specID string, management bool) (*model.ConsoleSpec, error) {
	query := `SELECT id,principal_id,title,state,current_revision,claimed_by,created_at,updated_at FROM console_spec WHERE id=?`
	args := []any{specID}
	if !management {
		query += ` AND principal_id=?`
		args = append(args, principalID)
	}
	return scanConsoleSpec(s.db.QueryRowContext(ctx, query, args...))
}

func (s *SQLite) ListConsoleSpecs(ctx context.Context, principalID string, management bool) ([]model.ConsoleSpec, error) {
	query := `SELECT id,principal_id,title,state,current_revision,claimed_by,created_at,updated_at FROM console_spec`
	var args []any
	if !management {
		query += ` WHERE principal_id=?`
		args = append(args, principalID)
	}
	query += ` ORDER BY updated_at DESC,id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleSpec
	for rows.Next() {
		spec, err := scanConsoleSpec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *spec)
	}
	return out, rows.Err()
}

func (s *SQLite) ConsoleSpecRevision(ctx context.Context, principalID, specID string, revision int, management bool) (*model.ConsoleSpecRevision, error) {
	query := `SELECT r.spec_id,r.revision,r.content_markdown,r.acceptance_json,r.input_anchors_json,r.content_sha256,r.created_by,COALESCE(r.supersedes_revision,0),r.created_at
		FROM console_spec_revision r JOIN console_spec s ON s.id=r.spec_id WHERE r.spec_id=? AND r.revision=?`
	args := []any{specID, revision}
	if !management {
		query += ` AND s.principal_id=?`
		args = append(args, principalID)
	}
	var r model.ConsoleSpecRevision
	var created string
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&r.SpecID, &r.Revision, &r.ContentMarkdown, &r.AcceptanceJSON, &r.InputAnchorsJSON, &r.ContentSHA256, &r.CreatedBy, &r.Supersedes, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt, err = specTime(created)
	return &r, err
}

func (s *SQLite) AppendConsoleSpecRevision(ctx context.Context, principalID, specID string, expected int, revision model.ConsoleSpecRevision, now time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var current int
	err = tx.QueryRowContext(ctx, `SELECT current_revision FROM console_spec WHERE id=? AND principal_id=?`, specID, principalID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrConsoleSpecNotFound
	}
	if err != nil {
		return 0, err
	}
	if current != expected {
		return 0, ErrConsoleSpecConflict
	}
	next := current + 1
	if revision.SpecID != specID || revision.Revision != next || revision.CreatedBy != principalID {
		return 0, errors.New("invalid console spec revision")
	}
	revision.Supersedes = current
	if err = insertConsoleSpecRevision(ctx, tx, revision, now); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE console_spec SET current_revision=?,state='needs_improvement',claimed_by=NULL,updated_at=? WHERE id=? AND principal_id=? AND current_revision=?`,
		next, now.UTC().Format(time.RFC3339Nano), specID, principalID, expected)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return 0, ErrConsoleSpecConflict
	}
	if err = appendConsoleSpecEvent(ctx, tx, specID, next, principalID, "revise", fmt.Sprintf("supersedes=%d", current), now); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *SQLite) SubmitConsoleSpec(ctx context.Context, principalID, specID string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec SET state='hard_gate',updated_at=? WHERE id=? AND principal_id=? AND state IN ('draft','needs_improvement')`,
		now.UTC().Format(time.RFC3339Nano), specID, principalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecNotFound
	}
	return nil
}

func (s *SQLite) ReviewConsoleSpec(ctx context.Context, review model.ConsoleSpecReview, now time.Time) error {
	if (review.Stage != "hard_gate" && review.Stage != "human_review") || (review.Verdict != "pass" && review.Verdict != "fail") || review.ID == "" || review.ReviewedBy == "" {
		return errors.New("invalid console spec review")
	}
	if review.Verdict == "fail" {
		var reasonCodes []string
		if err := json.Unmarshal([]byte(review.ReasonCodes), &reasonCodes); err != nil || len(reasonCodes) == 0 {
			return errors.New("failed review requires reason codes")
		}
		for _, code := range reasonCodes {
			if strings.TrimSpace(code) == "" {
				return errors.New("failed review requires non-empty reason codes")
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	var state, title string
	err = tx.QueryRowContext(ctx, `SELECT current_revision,state,title FROM console_spec WHERE id=?`, review.SpecID).Scan(&current, &state, &title)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConsoleSpecNotFound
	}
	if err != nil {
		return err
	}
	if current != review.Revision {
		return ErrConsoleSpecGateOrder
	}
	if review.Stage == "hard_gate" && state != "hard_gate" && state != "human_review" {
		return ErrConsoleSpecGateOrder
	}
	if review.Stage == "human_review" {
		var hardPass int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_spec_review WHERE spec_id=? AND revision=? AND stage='hard_gate' AND verdict='pass'`, review.SpecID, review.Revision).Scan(&hardPass); err != nil {
			return err
		}
		if hardPass == 0 || (state != "human_review" && state != "claimable") {
			return ErrConsoleSpecGateOrder
		}
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO console_spec_review(id,spec_id,revision,stage,verdict,reason_codes_json,detail,reviewed_by,reviewed_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		review.ID, review.SpecID, review.Revision, review.Stage, review.Verdict, firstNonEmptyStore(review.ReasonCodes, "[]"), review.Detail, review.ReviewedBy, stamp)
	if err != nil {
		return err
	}
	nextState := "needs_improvement"
	if review.Verdict == "pass" && review.Stage == "hard_gate" {
		nextState = "human_review"
	}
	if review.Verdict == "pass" && review.Stage == "human_review" {
		nextState = "claimable"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET state=?,updated_at=? WHERE id=?`, nextState, stamp, review.SpecID); err != nil {
		return err
	}
	if review.Stage == "human_review" && review.Verdict == "pass" {
		var contentHash, acceptance, anchors string
		if err = tx.QueryRowContext(ctx, `SELECT content_sha256,acceptance_json,input_anchors_json FROM console_spec_revision WHERE spec_id=? AND revision=?`, review.SpecID, review.Revision).Scan(&contentHash, &acceptance, &anchors); err != nil {
			return err
		}
		proposalID := fmt.Sprintf("proposal:%s:%d", review.SpecID, review.Revision)
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO console_task_proposal(id,schema_version,spec_id,revision,content_sha256,title,acceptance_json,input_anchors_json,review_id,reviewed_by,reviewed_at,created_at) VALUES(?,1,?,?,?,?,?,?,?,?,?,?)`,
			proposalID, review.SpecID, review.Revision, contentHash, title, acceptance, anchors, review.ID, review.ReviewedBy, stamp, stamp)
		if err != nil {
			return err
		}
	}
	if err = appendConsoleSpecEvent(ctx, tx, review.SpecID, review.Revision, review.ReviewedBy, "review."+review.Stage+"."+review.Verdict, review.ReasonCodes, now); err != nil {
		return err
	}
	return tx.Commit()
}

func firstNonEmptyStore(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *SQLite) ClaimConsoleSpec(ctx context.Context, specID, actor string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT current_revision FROM console_spec WHERE id=? AND state='claimable'`, specID).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		return ErrConsoleSpecGateOrder
	} else if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET state='in_progress',claimed_by=?,updated_at=? WHERE id=?`, actor, now.UTC().Format(time.RFC3339Nano), specID); err != nil {
		return err
	}
	if err = appendConsoleSpecEvent(ctx, tx, specID, revision, actor, "claim", "", now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) ArchiveConsoleSpec(ctx context.Context, specID, actor string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT current_revision FROM console_spec WHERE id=? AND state!='archived'`, specID).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		return ErrConsoleSpecNotFound
	} else if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET state='archived',claimed_by=NULL,updated_at=? WHERE id=?`, now.UTC().Format(time.RFC3339Nano), specID); err != nil {
		return err
	}
	if err = appendConsoleSpecEvent(ctx, tx, specID, revision, actor, "archive", "", now); err != nil {
		return err
	}
	return tx.Commit()
}

func scanProposal(row rowScanner) (*model.ConsoleTaskProposal, error) {
	var p model.ConsoleTaskProposal
	var reviewed, created string
	err := row.Scan(&p.ID, &p.SchemaVersion, &p.SpecID, &p.Revision, &p.ContentSHA256, &p.Title, &p.AcceptanceJSON, &p.InputAnchorsJSON, &p.ReviewID, &p.ReviewedBy, &reviewed, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.ReviewedAt, err = specTime(reviewed)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, err = specTime(created)
	return &p, err
}

const proposalColumns = `p.id,p.schema_version,p.spec_id,p.revision,p.content_sha256,p.title,p.acceptance_json,p.input_anchors_json,p.review_id,p.reviewed_by,p.reviewed_at,p.created_at`

func (s *SQLite) ListReadyConsoleTaskProposals(ctx context.Context) ([]model.ConsoleTaskProposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+proposalColumns+` FROM console_task_proposal p JOIN console_spec s ON s.id=p.spec_id AND s.current_revision=p.revision WHERE s.state IN ('claimable','in_progress') ORDER BY p.created_at,p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleTaskProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *SQLite) ListConsoleTaskProposals(ctx context.Context, specID string) ([]model.ConsoleTaskProposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+proposalColumns+` FROM console_task_proposal p WHERE p.spec_id=? ORDER BY p.revision`, specID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleTaskProposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *SQLite) ListConsoleSpecEvents(ctx context.Context, specID string) ([]model.ConsoleSpecEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,spec_id,revision,actor,action,detail,at FROM console_spec_event WHERE spec_id=? ORDER BY at,id`, specID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleSpecEvent
	for rows.Next() {
		var e model.ConsoleSpecEvent
		var at string
		if err := rows.Scan(&e.ID, &e.SpecID, &e.Revision, &e.Actor, &e.Action, &e.Detail, &at); err != nil {
			return nil, err
		}
		e.At, err = specTime(at)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
