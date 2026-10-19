package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const consoleTaskFallbackCase = "产出了没有"

var (
	allowAnyTaskCaseMarker      bool
	allowAnyTaskCaseConfirmer   bool
	truncateTaskCaseDenominator bool
	disableTaskFallbackCase     bool
)

type consoleTaskCriterion struct {
	ID    string   `json:"id"`
	Cases []string `json:"cases"`
}

func decodeConsoleTaskCriteria(raw string) []consoleTaskCriterion {
	var direct []consoleTaskCriterion
	if json.Unmarshal([]byte(raw), &direct) == nil {
		return direct
	}
	var bundle struct {
		Criteria []consoleTaskCriterion `json:"criteria"`
	}
	_ = json.Unmarshal([]byte(raw), &bundle)
	return bundle.Criteria
}

func (s *SQLite) consoleTaskExpectedCases(ctx context.Context, taskID string) (map[string]bool, error) {
	return taskExpectedCases(ctx, s.db, taskID)
}

func taskExpectedCases(ctx context.Context, q taskQuery, taskID string) (map[string]bool, error) {
	var specRaw string
	err := q.QueryRowContext(ctx, `SELECT r.acceptance_json FROM console_spec_task t JOIN console_spec_revision r ON r.spec_id=t.spec_id AND r.revision=t.revision WHERE t.id=?`, taskID).Scan(&specRaw)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT acceptance_id FROM console_spec_task_acceptance WHERE task_id=?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	linked := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		linked[id] = true
	}
	out := map[string]bool{}
	for _, criterion := range decodeConsoleTaskCriteria(specRaw) {
		if !linked[criterion.ID] {
			continue
		}
		for _, raw := range criterion.Cases {
			name := strings.TrimSpace(raw)
			if name != "" {
				out[criterion.ID+"\x00"+name] = true
				if truncateTaskCaseDenominator {
					break
				}
			}
		}
	}
	if len(out) == 0 && !disableTaskFallbackCase {
		out["\x00"+consoleTaskFallbackCase] = true
	}
	return out, rows.Err()
}

func (s *SQLite) MarkConsoleSpecTaskCase(ctx context.Context, taskID, acceptanceID, caseName, actorAgentID string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = markTaskCase(ctx, tx, taskID, acceptanceID, caseName, actorAgentID, at); err != nil {
		return err
	}
	return tx.Commit()
}

func markTaskCase(ctx context.Context, tx *sql.Tx, taskID, acceptanceID, caseName, actorAgentID string, at time.Time) error {
	var developer sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT developer_agent_id FROM console_spec_task WHERE id=?`, taskID).Scan(&developer); err != nil {
		return err
	}
	if (!developer.Valid || developer.String != actorAgentID) && !allowAnyTaskCaseMarker {
		return ErrConsoleTaskCasePermission
	}
	expected, err := taskExpectedCases(ctx, tx, taskID)
	if err != nil {
		return err
	}
	acceptanceID, caseName = strings.TrimSpace(acceptanceID), strings.TrimSpace(caseName)
	if !expected[acceptanceID+"\x00"+caseName] {
		return ErrConsoleTaskCaseUnknown
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO console_spec_task_case_result(task_id,acceptance_id,case_name,marked_by,marked_at,confirmed_by,confirmed_at) VALUES(?,?,?,?,?,NULL,NULL)
		ON CONFLICT(task_id,acceptance_id,case_name) DO UPDATE SET marked_by=excluded.marked_by,marked_at=excluded.marked_at,confirmed_by=NULL,confirmed_at=NULL`, taskID, acceptanceID, caseName, actorAgentID, at.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLite) ConfirmConsoleSpecTaskCase(ctx context.Context, taskID, acceptanceID, caseName, actorAgentID string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = confirmTaskCase(ctx, tx, taskID, acceptanceID, caseName, actorAgentID, at); err != nil {
		return err
	}
	return tx.Commit()
}

func confirmTaskCase(ctx context.Context, tx *sql.Tx, taskID, acceptanceID, caseName, actorAgentID string, at time.Time) error {
	var developer, reviewer sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT developer_agent_id,reviewer_agent_id FROM console_spec_task WHERE id=?`, taskID).Scan(&developer, &reviewer); err != nil {
		return err
	}
	if (!reviewer.Valid || reviewer.String != actorAgentID || (developer.Valid && developer.String == actorAgentID)) && !allowAnyTaskCaseConfirmer {
		return ErrConsoleTaskCasePermission
	}
	res, err := tx.ExecContext(ctx, `UPDATE console_spec_task_case_result SET confirmed_by=?,confirmed_at=? WHERE task_id=? AND acceptance_id=? AND case_name=? AND marked_by=?`, actorAgentID, at.UTC().Format(time.RFC3339Nano), taskID, strings.TrimSpace(acceptanceID), strings.TrimSpace(caseName), developer.String)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleTaskCaseUnknown
	}
	return nil
}

func (s *SQLite) ConsoleSpecTaskCaseProgress(ctx context.Context, taskID string) (int, int, error) {
	return s.consoleSpecTaskCaseProgress(ctx, taskID, false)
}

func (s *SQLite) consoleSpecTaskCaseProgress(ctx context.Context, taskID string, countUnconfirmed bool) (int, int, error) {
	return taskCaseProgress(ctx, s.db, taskID, countUnconfirmed)
}
func taskCaseProgress(ctx context.Context, q taskQuery, taskID string, countUnconfirmed bool) (int, int, error) {
	expected, err := taskExpectedCases(ctx, q, taskID)
	if err != nil {
		return 0, 0, err
	}
	passed := 0
	query := `SELECT c.acceptance_id,c.case_name FROM console_spec_task_case_result c JOIN console_spec_task t ON t.id=c.task_id WHERE c.task_id=? AND c.marked_by=t.developer_agent_id AND c.confirmed_by=t.reviewer_agent_id AND c.confirmed_at IS NOT NULL`
	if countUnconfirmed {
		query = `SELECT acceptance_id,case_name FROM console_spec_task_case_result WHERE task_id=?`
	}
	rows, err := q.QueryContext(ctx, query, taskID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return 0, 0, err
		}
		if expected[id+"\x00"+name] {
			passed++
		}
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}
	return passed, len(expected), nil
}
