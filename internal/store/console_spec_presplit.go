package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var (
	ErrConsoleSpecPresplitConflict = errors.New("console spec presplit already started")
	ErrConsoleSpecPresplitRejected = errors.New("console spec presplit callback rejected")
)

type ConsoleSpecPresplitTask struct {
	ID               string
	Title            string
	Content          string
	DefinitionOfDone string
	EstimatedHours   float64
	DependsOn        []string
}

var presplitTaskDefinitionStorage = splitTaskDefinitionOfDone

func (s *SQLite) ResolveConsoleAgentByEndpoint(ctx context.Context, keyID, sessionName string) (*model.ConsoleAgent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,key_id,session_name,owner_principal,active FROM console_agent WHERE key_id=? AND session_name=?`, keyID, sessionName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []model.ConsoleAgent
	for rows.Next() {
		var agent model.ConsoleAgent
		var active int
		if err = rows.Scan(&agent.ID, &agent.KeyID, &agent.SessionName, &agent.OwnerPrincipal, &active); err != nil {
			return nil, err
		}
		agent.Active = active != 0
		agents = append(agents, agent)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(agents) != 1 {
		return nil, nil
	}
	return &agents[0], nil
}

func (s *SQLite) StartConsoleSpecPresplit(ctx context.Context, req model.ConsoleSpecPresplitRequest, principalID string) (*model.ConsoleSpecRevision, *model.ConsoleAgent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var owner string
	if err = tx.QueryRowContext(ctx, `SELECT principal_id FROM console_spec WHERE id=? AND principal_id=? AND state='final' AND task_state='splitting'`, req.SpecID, principalID).Scan(&owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrConsoleSpecPermission
		}
		return nil, nil, err
	}
	var revision model.ConsoleSpecRevision
	var revisionCreated string
	if err = tx.QueryRowContext(ctx, `SELECT r.spec_id,r.revision,r.content_markdown,r.acceptance_json,r.input_anchors_json,r.content_sha256,r.created_by,r.created_via,COALESCE(r.supersedes_revision,0),r.created_at FROM console_spec_revision r JOIN console_spec s ON s.id=r.spec_id AND s.current_revision=r.revision WHERE s.id=?`, req.SpecID).
		Scan(&revision.SpecID, &revision.Revision, &revision.ContentMarkdown, &revision.AcceptanceJSON, &revision.InputAnchorsJSON, &revision.ContentSHA256, &revision.CreatedBy, &revision.CreatedVia, &revision.Supersedes, &revisionCreated); err != nil {
		return nil, nil, err
	}
	revision.CreatedAt, _ = time.Parse(time.RFC3339Nano, revisionCreated)
	var agent model.ConsoleAgent
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT id,key_id,session_name,owner_principal,active FROM console_agent WHERE id=? AND owner_principal=? AND active=1`, req.AgentID, owner).
		Scan(&agent.ID, &agent.KeyID, &agent.SessionName, &agent.OwnerPrincipal, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrConsoleSpecPermission
		}
		return nil, nil, err
	}
	agent.Active = active != 0
	req.Revision = revision.Revision
	req.OwnerPrincipal = owner
	_, err = tx.ExecContext(ctx, `INSERT INTO console_spec_presplit_request(id,spec_id,revision,agent_id,owner_principal,status,failure_reason,deadline_at,created_at) VALUES(?,?,?,?,?,'pending','',?,?)`,
		req.ID, req.SpecID, req.Revision, req.AgentID, owner, req.DeadlineAt.UTC().Format(time.RFC3339Nano), req.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, nil, ErrConsoleSpecPresplitConflict
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &revision, &agent, nil
}

func (s *SQLite) ConsoleSpecPresplit(ctx context.Context, specID, principalID string, now, processStartedAt time.Time) (*model.ConsoleSpecPresplitRequest, error) {
	// Any pending row older than this process is a lost callback after restart.
	_, err := s.db.ExecContext(ctx, `UPDATE console_spec_presplit_request SET status='failed',failure_reason='restart',completed_at=? WHERE spec_id=? AND owner_principal=? AND status='pending' AND created_at<?`,
		now.UTC().Format(time.RFC3339Nano), specID, principalID, processStartedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE console_spec_presplit_request SET status='failed',failure_reason='timeout',completed_at=? WHERE spec_id=? AND owner_principal=? AND status='pending' AND deadline_at<=?`,
		now.UTC().Format(time.RFC3339Nano), specID, principalID, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return scanConsoleSpecPresplit(s.db.QueryRowContext(ctx, `SELECT id,spec_id,revision,agent_id,owner_principal,status,failure_reason,deadline_at,created_at,completed_at FROM console_spec_presplit_request WHERE spec_id=? AND owner_principal=? ORDER BY created_at DESC LIMIT 1`, specID, principalID))
}

func scanConsoleSpecPresplit(row *sql.Row) (*model.ConsoleSpecPresplitRequest, error) {
	var req model.ConsoleSpecPresplitRequest
	var deadline, created string
	var completed sql.NullString
	if err := row.Scan(&req.ID, &req.SpecID, &req.Revision, &req.AgentID, &req.OwnerPrincipal, &req.Status, &req.FailureReason, &deadline, &created, &completed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	req.DeadlineAt, _ = time.Parse(time.RFC3339Nano, deadline)
	req.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if completed.Valid {
		stamp, _ := time.Parse(time.RFC3339Nano, completed.String)
		req.CompletedAt = &stamp
	}
	return &req, nil
}

func (s *SQLite) FailConsoleSpecPresplitDispatch(ctx context.Context, requestID, reason string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec_presplit_request SET status='failed',failure_reason=?,completed_at=? WHERE id=? AND status='pending'`, reason, now.UTC().Format(time.RFC3339Nano), requestID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPresplitRejected
	}
	return nil
}

func (s *SQLite) RejectConsoleSpecPresplitResult(ctx context.Context, requestID string, agent model.ConsoleAgent, reason string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec_presplit_request SET status='failed',failure_reason=?,completed_at=? WHERE id=? AND status='pending' AND agent_id=? AND owner_principal=? AND EXISTS(SELECT 1 FROM console_agent a WHERE a.id=? AND a.key_id=? AND a.session_name=? AND a.owner_principal=console_spec_presplit_request.owner_principal AND a.active=1)`,
		reason, now.UTC().Format(time.RFC3339Nano), requestID, agent.ID, agent.OwnerPrincipal, agent.ID, agent.KeyID, agent.SessionName)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPresplitRejected
	}
	return nil
}

func (s *SQLite) CompleteConsoleSpecPresplit(ctx context.Context, requestID string, agent model.ConsoleAgent, tasks []ConsoleSpecPresplitTask, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var req model.ConsoleSpecPresplitRequest
	var deadline string
	if err = tx.QueryRowContext(ctx, `SELECT id,spec_id,revision,agent_id,owner_principal,status,deadline_at FROM console_spec_presplit_request WHERE id=?`, requestID).
		Scan(&req.ID, &req.SpecID, &req.Revision, &req.AgentID, &req.OwnerPrincipal, &req.Status, &deadline); err != nil {
		return ErrConsoleSpecPresplitRejected
	}
	req.DeadlineAt, _ = time.Parse(time.RFC3339Nano, deadline)
	if req.Status != "pending" {
		return ErrConsoleSpecPresplitRejected
	}
	if !now.Before(req.DeadlineAt) {
		_, _ = tx.ExecContext(ctx, `UPDATE console_spec_presplit_request SET status='failed',failure_reason='timeout',completed_at=? WHERE id=? AND status='pending'`, now.UTC().Format(time.RFC3339Nano), requestID)
		_ = tx.Commit()
		return ErrConsoleSpecPresplitRejected
	}
	var currentOwner string
	activeErr := tx.QueryRowContext(ctx, `SELECT owner_principal FROM console_agent WHERE id=? AND key_id=? AND session_name=? AND active=1`, agent.ID, agent.KeyID, agent.SessionName).Scan(&currentOwner)
	active := activeErr == nil && currentOwner == agent.OwnerPrincipal
	authorizedAgent := active && agent.ID == req.AgentID && agent.OwnerPrincipal == req.OwnerPrincipal
	if !authorizedAgent {
		if agent.ID == req.AgentID && (!active || agent.OwnerPrincipal != req.OwnerPrincipal) {
			_, _ = tx.ExecContext(ctx, `UPDATE console_spec_presplit_request SET status='failed',failure_reason='agent authorization changed',completed_at=? WHERE id=? AND status='pending'`, now.UTC().Format(time.RFC3339Nano), requestID)
			_ = tx.Commit()
		}
		return ErrConsoleSpecPresplitRejected
	}
	var creator, hash, acceptance, anchors, taskState string
	if err = tx.QueryRowContext(ctx, `SELECT s.principal_id,s.task_state,r.content_sha256,r.acceptance_json,r.input_anchors_json FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=? WHERE s.id=?`, req.Revision, req.SpecID).
		Scan(&creator, &taskState, &hash, &acceptance, &anchors); err != nil || creator != req.OwnerPrincipal || taskState != "splitting" {
		return ErrConsoleSpecPresplitRejected
	}
	for _, task := range tasks {
		deps, _ := json.Marshal(task.DependsOn)
		acceptanceJSON, definitionOfDone := presplitTaskDefinitionStorage(task.DefinitionOfDone)
		if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec_task(id,spec_id,revision,content_sha256,title,task_content,status,estimated_hours,depends_on_json,acceptance_json,input_anchors_json,definition_of_done,created_by,created_at) VALUES(?,?,?,?,?,?,'planned',?,?,?,?,?,?,?)`,
			task.ID, req.SpecID, req.Revision, hash, task.Title, task.Content, task.EstimatedHours, string(deps), acceptanceJSON, anchors, definitionOfDone, agent.OwnerPrincipal, now.UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("persist presplit task: %w", err)
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE console_spec_presplit_request SET status='completed',completed_at=? WHERE id=? AND status='pending'`, now.UTC().Format(time.RFC3339Nano), requestID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPresplitRejected
	}
	return tx.Commit()
}
