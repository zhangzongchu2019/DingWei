package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var (
	ErrConsoleTaskStartIncomplete = errors.New("开工信息 7 项缺一不可")
	ErrConsoleTaskStartUnassigned = errors.New("每条任务必须同时指定不同的研发与验收 Agent")
	ErrConsoleTaskNotGreen        = errors.New("任务尚未全绿")
	ErrConsoleTaskOwnerReview     = errors.New("组长尚未完成验收检查")
)

func validTaskDriverStart(v model.ConsoleTaskDriverStart) bool {
	return strings.TrimSpace(v.SpecID) != "" && v.Revision > 0 && strings.TrimSpace(v.ContentSHA256) != "" &&
		strings.TrimSpace(v.RepositoryName) != "" && strings.TrimSpace(v.BranchName) != "" &&
		strings.TrimSpace(v.Schedule) != "" && strings.TrimSpace(v.TaskEndpoint) != "" &&
		strings.TrimSpace(v.TokenInstructions) != "" && strings.TrimSpace(v.AnnouncedBy) != ""
}

func (s *SQLite) StartConsoleTaskDriver(ctx context.Context, start model.ConsoleTaskDriverStart) error {
	if !validTaskDriverStart(start) {
		return ErrConsoleTaskStartIncomplete
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tasks, invalid int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN developer_agent_id IS NULL OR reviewer_agent_id IS NULL OR developer_agent_id=reviewer_agent_id THEN 1 ELSE 0 END),0)
		FROM console_spec_task WHERE spec_id=? AND revision=?`, start.SpecID, start.Revision).Scan(&tasks, &invalid)
	if err != nil {
		return err
	}
	if tasks == 0 || invalid != 0 {
		return ErrConsoleTaskStartUnassigned
	}
	var actualSHA, principal, state string
	err = tx.QueryRowContext(ctx, `SELECT r.content_sha256,s.principal_id,s.state FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=? WHERE s.id=?`, start.Revision, start.SpecID).Scan(&actualSHA, &principal, &state)
	if err != nil {
		return err
	}
	if state != "final" || principal != start.AnnouncedBy || actualSHA != start.ContentSHA256 {
		return ErrConsoleSpecPermission
	}
	var unresolved int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_task_blocker WHERE spec_id=? AND revision=? AND state NOT IN('resolved','cancelled')`, start.SpecID, start.Revision).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved != 0 {
		return ErrTaskFlowConflict
	}
	rows, e := tx.QueryContext(ctx, `SELECT id FROM console_spec_task WHERE spec_id=? AND revision=?`, start.SpecID, start.Revision)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if e = templateTaskGate(ctx, tx, id, start.AnnouncedAt); e != nil {
			return e
		}
	}
	stamp := start.AnnouncedAt.UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO console_task_driver_start(spec_id,revision,content_sha256,repository_name,branch_name,schedule,task_endpoint,token_instructions,announced_by,announced_at)
		VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(spec_id,revision) DO UPDATE SET content_sha256=excluded.content_sha256,repository_name=excluded.repository_name,branch_name=excluded.branch_name,schedule=excluded.schedule,task_endpoint=excluded.task_endpoint,token_instructions=excluded.token_instructions,announced_by=excluded.announced_by,announced_at=excluded.announced_at`,
		start.SpecID, start.Revision, start.ContentSHA256, start.RepositoryName, start.BranchName, start.Schedule, start.TaskEndpoint, start.TokenInstructions, start.AnnouncedBy, stamp)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) ListConsoleTasksForAgent(ctx context.Context, agentID string) ([]model.ConsoleTaskProposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,1,t.spec_id,t.revision,t.content_sha256,t.title,t.task_content,t.status,t.estimated_hours,t.depends_on_json,
		COALESCE((SELECT json_group_array(json_object('id',a.acceptance_id,'text',COALESCE(json_extract(j.value,'$.text'),''))) FROM console_spec_task_acceptance a JOIN console_spec_revision rr ON rr.spec_id=t.spec_id AND rr.revision=t.revision JOIN json_each(rr.acceptance_json) j ON json_extract(j.value,'$.id')=a.acceptance_id WHERE a.task_id=t.id),'[]'),
		t.input_anchors_json,t.definition_of_done,t.developer_agent_id,COALESCE(da.session_name,''),t.reviewer_agent_id,COALESCE(ra.session_name,''),
		'manual:'||t.id,t.created_by,t.created_at,t.created_at,t.started_at,t.planned_finish_at,t.blocker,t.resource_coordination
		FROM console_spec_task t LEFT JOIN console_agent da ON da.id=t.developer_agent_id LEFT JOIN console_agent ra ON ra.id=t.reviewer_agent_id
		WHERE t.developer_agent_id=? OR t.reviewer_agent_id=? ORDER BY t.created_at,t.id`, agentID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleTaskProposal
	for rows.Next() {
		p, scanErr := scanProposal(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *SQLite) RecordConsoleTaskPause(ctx context.Context, specID string, revision int, agentID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO console_task_pause_report(spec_id,revision,agent_id,reported_at) VALUES(?,?,?,?) ON CONFLICT(spec_id,revision,agent_id) DO UPDATE SET reported_at=excluded.reported_at`, specID, revision, agentID, at.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLite) ConsoleTaskPauseReported(ctx context.Context, specID string, revision int, agentID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_task_pause_report WHERE spec_id=? AND revision=? AND agent_id=?`, specID, revision, agentID).Scan(&n)
	return n == 1, err
}

func (s *SQLite) ListConsoleTaskPauseReports(ctx context.Context, specID string, revision int) ([]model.ConsoleTaskPauseReport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT spec_id,revision,agent_id,reported_at FROM console_task_pause_report WHERE spec_id=? AND revision=? ORDER BY reported_at,agent_id`, specID, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsoleTaskPauseReport
	for rows.Next() {
		var v model.ConsoleTaskPauseReport
		var stamp string
		if err := rows.Scan(&v.SpecID, &v.Revision, &v.AgentID, &stamp); err != nil {
			return nil, err
		}
		v.ReportedAt, _ = time.Parse(time.RFC3339Nano, stamp)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *SQLite) consoleSpecTasksGreen(ctx context.Context, specID string, revision int) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM console_spec_task WHERE spec_id=? AND revision=?`, specID, revision)
	if err != nil {
		return false, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, id := range ids {
		passed, total, err := s.ConsoleSpecTaskCaseProgress(ctx, id)
		if err != nil || total < 1 || passed != total {
			return false, err
		}
	}
	return len(ids) > 0, nil
}

func (s *SQLite) ReviewConsoleTaskDriver(ctx context.Context, specID string, revision int, principal string, at time.Time) error {
	green, err := s.consoleSpecTasksGreen(ctx, specID, revision)
	if err != nil {
		return err
	}
	if !green {
		return ErrConsoleTaskNotGreen
	}
	res, err := s.db.ExecContext(ctx, `UPDATE console_task_driver_start SET owner_reviewed_at=? WHERE spec_id=? AND revision=? AND announced_by=?`, at.UTC().Format(time.RFC3339Nano), specID, revision, principal)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return nil
}

func (s *SQLite) RequestConsoleTaskDeployment(ctx context.Context, specID string, revision int, principal string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_task_driver_start SET deployment_requested_at=? WHERE spec_id=? AND revision=? AND announced_by=? AND owner_reviewed_at IS NOT NULL`, at.UTC().Format(time.RFC3339Nano), specID, revision, principal)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleTaskOwnerReview
	}
	return nil
}

func (s *SQLite) ConsoleTaskDriverStart(ctx context.Context, specID string, revision int) (*model.ConsoleTaskDriverStart, error) {
	var v model.ConsoleTaskDriverStart
	var announced string
	var reviewed, deployed sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT spec_id,revision,content_sha256,repository_name,branch_name,schedule,task_endpoint,token_instructions,announced_by,announced_at,owner_reviewed_at,deployment_requested_at FROM console_task_driver_start WHERE spec_id=? AND revision=?`, specID, revision).Scan(&v.SpecID, &v.Revision, &v.ContentSHA256, &v.RepositoryName, &v.BranchName, &v.Schedule, &v.TaskEndpoint, &v.TokenInstructions, &v.AnnouncedBy, &announced, &reviewed, &deployed)
	if err != nil {
		return nil, err
	}
	v.AnnouncedAt, _ = time.Parse(time.RFC3339Nano, announced)
	if reviewed.Valid {
		t, _ := time.Parse(time.RFC3339Nano, reviewed.String)
		v.OwnerReviewedAt = &t
	}
	if deployed.Valid {
		t, _ := time.Parse(time.RFC3339Nano, deployed.String)
		v.DeploymentRequestedAt = &t
	}
	return &v, nil
}
