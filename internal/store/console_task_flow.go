package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var (
	ErrTaskFlowPermission = errors.New("task access denied")
	ErrTaskFlowConflict   = errors.New("task version or state conflict")
	ErrTaskFlowInvalid    = errors.New("invalid task request")
)

type TaskFlowState struct {
	ID                string `json:"task_id"`
	Owner             string `json:"-"`
	Archived          bool   `json:"-"`
	GroupCode         string `json:"group_code"`
	Version           int    `json:"version"`
	AssignmentVersion int    `json:"assignment_version"`
	Developer         string `json:"developer_agent_id"`
	Reviewer          string `json:"reviewer_agent_id"`
	NeedsReassignment bool   `json:"needs_reassignment"`
}

type TaskFlowUpdate struct {
	RequestID       string `json:"request_id"`
	ExpectedVersion int    `json:"expected_version"`
	Operation       string `json:"operation"`
	Blocker         string `json:"blocker,omitempty"`
	AcceptanceID    string `json:"acceptance_id,omitempty"`
	CaseName        string `json:"case_name,omitempty"`
}

type TaskFlowItem struct {
	model.ConsoleTaskProposal
	FlowState TaskFlowState  `json:"-"`
	TaskID    string         `json:"task_id"`
	SpecTitle string         `json:"spec_title"`
	Role      string         `json:"role"`
	Cases     []TaskFlowCase `json:"cases"`
}

type TaskFlowCase struct {
	AcceptanceID string `json:"acceptance_id"`
	CaseName     string `json:"case_name"`
	Marked       bool   `json:"marked"`
	Confirmed    bool   `json:"confirmed"`
}

type taskQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func taskFlowState(ctx context.Context, q taskQuery, id string) (TaskFlowState, error) {
	var t TaskFlowState
	err := q.QueryRowContext(ctx, `SELECT t.id,s.principal_id,t.group_code,t.version,t.assignment_version,COALESCE(t.developer_agent_id,''),COALESCE(t.reviewer_agent_id,''),t.needs_reassignment,s.archived FROM console_spec_task t JOIN console_spec s ON s.id=t.spec_id WHERE t.id=?`, id).Scan(&t.ID, &t.Owner, &t.GroupCode, &t.Version, &t.AssignmentVersion, &t.Developer, &t.Reviewer, &t.NeedsReassignment, &t.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrTaskFlowPermission
	}
	return t, err
}

func (s *SQLite) ConsoleTaskFlowState(ctx context.Context, id string) (TaskFlowState, error) {
	return taskFlowState(ctx, s.db, id)
}

func flowAudit(ctx context.Context, tx *sql.Tx, id, actor, op string, version int, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO console_task_flow_audit(task_id,actor_id,operation,version,created_at) VALUES(?,?,?,?,?)`, id, actor, op, version, at.UTC().Format(time.RFC3339Nano))
	return err
}

func flowOutbox(ctx context.Context, tx *sql.Tx, t TaskFlowState, recipient, role, event string, at time.Time) error {
	if recipient == "" {
		return nil
	}
	key := fmt.Sprintf("%s/%d/%s/%s/%s", t.ID, t.AssignmentVersion, event, recipient, role)
	digest := sha256.Sum256([]byte(key))
	id := "task:" + hex.EncodeToString(digest[:])
	raw, _ := json.Marshal(map[string]any{"kind": "task_assigned", "task_id": t.ID, "assignment_version": t.AssignmentVersion, "role": role, "event": event, "tools": []string{"dw_task_list", "dw_task_update"}})
	_, err := tx.ExecContext(ctx, `INSERT INTO console_task_flow_outbox(id,task_id,assignment_version,event,recipient_agent_id,role,body,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(task_id,assignment_version,event,recipient_agent_id,role) DO NOTHING`, id, t.ID, t.AssignmentVersion, event, recipient, role, string(raw), at.UTC().Format(time.RFC3339Nano))
	return err
}

// The caller holds current PG membership/identity locks and supplies a final
// round-trip check. No successful task mutation is reported before COMMIT.
func (s *SQLite) UpdateConsoleTaskFlow(ctx context.Context, id, actor string, u TaskFlowUpdate, recheck func() error, at time.Time) (TaskFlowState, error) {
	var empty TaskFlowState
	if u.RequestID == "" || len(u.RequestID) > 128 || u.ExpectedVersion < 1 || len(u.Blocker) > 4096 || len(u.CaseName) > 1024 {
		return empty, ErrTaskFlowInvalid
	}
	raw, _ := json.Marshal(struct {
		ID     string
		Update TaskFlowUpdate
	}{id, u})
	sum := sha256.Sum256(raw)
	payload := hex.EncodeToString(sum[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	t, err := taskFlowState(ctx, tx, id)
	if err != nil {
		return empty, err
	}
	if t.Archived || t.GroupCode == "" || t.NeedsReassignment || (actor != t.Developer && actor != t.Reviewer) {
		return empty, ErrTaskFlowPermission
	}
	var oldHash, result string
	err = tx.QueryRowContext(ctx, `SELECT payload_sha,result_json FROM console_task_flow_request WHERE actor_id=? AND request_id=?`, actor, u.RequestID).Scan(&oldHash, &result)
	if err == nil {
		if oldHash != payload {
			return empty, ErrTaskFlowConflict
		}
		if recheck == nil {
			return empty, ErrTaskFlowPermission
		}
		if err = recheck(); err != nil {
			return empty, err
		}
		err = json.Unmarshal([]byte(result), &t)
		return t, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	if t.Version != u.ExpectedVersion {
		return empty, ErrTaskFlowConflict
	}
	var state, deps string
	if err = tx.QueryRowContext(ctx, `SELECT status,depends_on_json FROM console_spec_task WHERE id=?`, id).Scan(&state, &deps); err != nil {
		return empty, err
	}
	if state == "done" {
		return empty, ErrTaskFlowConflict
	}
	switch u.Operation {
	case "mark_case", "confirm_case":
		if u.Operation == "mark_case" {
			err = markTaskCase(ctx, tx, id, u.AcceptanceID, u.CaseName, actor, at)
		} else {
			err = confirmTaskCase(ctx, tx, id, u.AcceptanceID, u.CaseName, actor, at)
		}
		if err != nil {
			return empty, err
		}
	case "start", "resume", "block", "complete":
		if actor != t.Developer {
			return empty, ErrTaskFlowPermission
		}
		next := ""
		switch u.Operation {
		case "start":
			if state == "planned" {
				next = "in_progress"
			}
		case "resume":
			if state == "blocked" {
				next = "in_progress"
			}
		case "block":
			if state == "in_progress" && strings.TrimSpace(u.Blocker) != "" {
				next = "blocked"
			}
		case "complete":
			if state == "in_progress" || state == "blocked" {
				next = "done"
			}
		}
		if next == "" {
			return empty, ErrTaskFlowConflict
		}
		if next != "blocked" {
			if err = taskBlockersClearAt(ctx, tx, id, at); err != nil {
				return empty, err
			}
			if err = taskDependenciesDone(ctx, tx, deps); err != nil {
				return empty, err
			}
		}
		if next == "done" {
			p, n, e := taskCaseProgress(ctx, tx, id, false)
			if e != nil {
				return empty, e
			}
			if n == 0 || p != n {
				return empty, ErrConsoleTaskNotGreen
			}
		}
		blocker := ""
		if next == "blocked" {
			blocker = strings.TrimSpace(u.Blocker)
		}
		_, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET status=?,blocker=?,started_at=CASE WHEN ?='in_progress' THEN COALESCE(started_at,?) ELSE started_at END WHERE id=?`, next, blocker, next, at.UTC().Format(time.RFC3339Nano), id)
		if err != nil {
			return empty, err
		}
	default:
		return empty, ErrTaskFlowInvalid
	}
	t.Version++
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=? WHERE id=?`, t.Version, id); err != nil {
		return empty, err
	}
	if err = flowAudit(ctx, tx, id, actor, u.Operation, t.Version, at); err != nil {
		return empty, err
	}
	resultRaw, _ := json.Marshal(t)
	if _, err = tx.ExecContext(ctx, `INSERT INTO console_task_flow_request(actor_id,request_id,payload_sha,result_json,created_at) VALUES(?,?,?,?,?)`, actor, u.RequestID, payload, string(resultRaw), at.UTC().Format(time.RFC3339Nano)); err != nil {
		return empty, err
	}
	if recheck == nil {
		return empty, ErrTaskFlowPermission
	}
	if err = recheck(); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return t, nil
}

func taskDependenciesDone(ctx context.Context, q taskQuery, raw string) error {
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return ErrTaskFlowConflict
	}
	for _, id := range ids {
		var status string
		if err := q.QueryRowContext(ctx, `SELECT status FROM (`+taskUnion+`) WHERE id=?`, id).Scan(&status); err != nil || status != "done" {
			return ErrTaskFlowConflict
		}
	}
	return nil
}

func (s *SQLite) AssignConsoleTaskFlow(ctx context.Context, id, owner, group, dev, review string, expected int, recheck func() error, at time.Time, reopen ...bool) (TaskFlowState, error) {
	var empty TaskFlowState
	if group == "" || dev == "" || review == "" || dev == review {
		return empty, ErrConsoleTaskAssigneeSame
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	t, err := taskFlowState(ctx, tx, id)
	if err != nil {
		return empty, err
	}
	if t.Archived || t.Owner != owner {
		return empty, ErrTaskFlowPermission
	}
	if t.Version != expected {
		return empty, ErrTaskFlowConflict
	}
	if err = templateTaskGate(ctx, tx, id, at, dev, review); err != nil {
		return empty, err
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM console_spec_task WHERE id=?`, id).Scan(&status); err != nil {
		return empty, err
	}
	allowReopen := len(reopen) > 0 && reopen[0]
	if t.GroupCode == group && t.Developer == dev && t.Reviewer == review && !t.NeedsReassignment && !(status == "done" && allowReopen) {
		if recheck == nil {
			return empty, ErrTaskFlowPermission
		}
		return t, recheck()
	}
	if status == "done" && !allowReopen {
		return empty, ErrTaskFlowConflict
	}
	old := t
	t.GroupCode = group
	t.Developer = dev
	t.Reviewer = review
	t.Version++
	t.AssignmentVersion++
	t.NeedsReassignment = false
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET group_code=?,developer_agent_id=?,reviewer_agent_id=?,version=?,assignment_version=?,needs_reassignment=0,status=CASE WHEN status='done' THEN 'planned' ELSE status END,started_at=CASE WHEN status='done' THEN NULL ELSE started_at END WHERE id=?`, group, dev, review, t.Version, t.AssignmentVersion, id); err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM console_spec_task_case_result WHERE task_id=?`, id); err != nil {
		return empty, err
	}
	for _, v := range []struct{ id, role, event string }{{old.Developer, "developer", "unassigned"}, {old.Reviewer, "reviewer", "unassigned"}, {dev, "developer", "assigned"}, {review, "reviewer", "assigned"}} {
		if v.event == "unassigned" && ((v.role == "developer" && v.id == dev) || (v.role == "reviewer" && v.id == review)) {
			continue
		}
		if err = flowOutbox(ctx, tx, t, v.id, v.role, v.event, at); err != nil {
			return empty, err
		}
	}
	operation := "assign"
	if status == "done" {
		operation = "reopen_assign"
	}
	if err = flowAudit(ctx, tx, id, owner, operation, t.Version, at); err != nil {
		return empty, err
	}
	if recheck == nil {
		return empty, ErrTaskFlowPermission
	}
	if err = recheck(); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return t, nil
}

func (s *SQLite) InvalidateConsoleTaskFlow(ctx context.Context, id string, version int, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := taskFlowState(ctx, tx, id)
	if err != nil {
		return err
	}
	if t.Version != version || t.NeedsReassignment {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET needs_reassignment=1,version=version+1 WHERE id=?`, id); err != nil {
		return err
	}
	t.Version++
	if err = flowOutbox(ctx, tx, t, "owner:"+t.Owner, "owner", "needs_reassignment", at); err != nil {
		return err
	}
	if err = flowAudit(ctx, tx, id, "system", "needs_reassignment", t.Version, at); err != nil {
		return err
	}
	return tx.Commit()
}

type TaskFlowOutbox struct {
	ID, TaskID, Recipient, Body string
	AssignmentVersion           int
}

func (s *SQLite) PendingConsoleTaskNotifications(ctx context.Context) ([]TaskFlowOutbox, error) {
	all := []TaskFlowOutbox{}
	after := ""
	for {
		page, err := s.PendingConsoleTaskNotificationsAfter(ctx, after)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return all, nil
		}
		all = append(all, page...)
		after = page[len(page)-1].ID
	}
}

// Stable-key pages let callers pass permanently queued offline/owner notices
// without losing them or starving deliverable notifications behind the page.
func (s *SQLite) PendingConsoleTaskNotificationsAfter(ctx context.Context, after string) ([]TaskFlowOutbox, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,recipient_agent_id,body,assignment_version FROM console_task_flow_outbox WHERE dispatched_at IS NULL AND id > ? ORDER BY id LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskFlowOutbox{}
	for rows.Next() {
		var v TaskFlowOutbox
		if err = rows.Scan(&v.ID, &v.TaskID, &v.Recipient, &v.Body, &v.AssignmentVersion); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *SQLite) FinishConsoleTaskNotification(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE console_task_flow_outbox SET dispatched_at=? WHERE id=? AND dispatched_at IS NULL`, at.UTC().Format(time.RFC3339Nano), id)
	return err
}

type TaskCandidates struct {
	RequestID string                    `json:"request_id"`
	Version   int                       `json:"version"`
	Revision  int                       `json:"revision"`
	Confirmed bool                      `json:"confirmed"`
	Tasks     []ConsoleSpecPresplitTask `json:"tasks"`
}

func validateTaskCandidates(tasks []ConsoleSpecPresplitTask) error {
	if len(tasks) == 0 || len(tasks) > 200 {
		return ErrTaskFlowInvalid
	}
	byID := map[string]ConsoleSpecPresplitTask{}
	for _, t := range tasks {
		if t.ID == "" || byID[t.ID].ID != "" || strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Content) == "" || t.EstimatedHours < 0 {
			return ErrTaskFlowInvalid
		}
		if (t.DeveloperAgentID == "") != (t.ReviewerAgentID == "") {
			return ErrConsoleTaskAssigneePair
		}
		if t.DeveloperAgentID != "" && (t.DeveloperAgentID == t.ReviewerAgentID || t.GroupCode == "") {
			return ErrConsoleTaskAssigneeSame
		}
		byID[t.ID] = t
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return ErrTaskFlowConflict
		}
		if done[id] {
			return nil
		}
		t, ok := byID[id]
		if !ok {
			return ErrTaskFlowInvalid
		}
		visiting[id] = true
		seen := map[string]bool{}
		for _, dep := range t.DependsOn {
			if dep == id || seen[dep] {
				return ErrTaskFlowInvalid
			}
			seen[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		done[id] = true
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) ConsoleTaskCandidates(ctx context.Context, spec, owner string) (*TaskCandidates, error) {
	var c TaskCandidates
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT c.request_id,c.version,r.revision,c.tasks_json,c.confirmed_at IS NOT NULL FROM console_task_candidate c JOIN console_spec_presplit_request r ON r.id=c.request_id JOIN console_spec s ON s.id=r.spec_id WHERE r.spec_id=? AND s.principal_id=? ORDER BY r.created_at DESC LIMIT 1`, spec, owner).Scan(&c.RequestID, &c.Version, &c.Revision, &raw, &c.Confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal([]byte(raw), &c.Tasks)
	return &c, err
}

func (s *SQLite) ConfirmConsoleTaskCandidates(ctx context.Context, request, owner string, revision, version int, tasks []ConsoleSpecPresplitTask, recheck func() error, at time.Time) error {
	if err := validateTaskCandidates(tasks); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var spec, hash, acceptance, anchors, original string
	var currentRevision, currentVersion int
	var confirmed bool
	err = tx.QueryRowContext(ctx, `SELECT r.spec_id,s.current_revision,v.content_sha256,v.acceptance_json,v.input_anchors_json,c.tasks_json,c.version,c.confirmed_at IS NOT NULL FROM console_task_candidate c JOIN console_spec_presplit_request r ON r.id=c.request_id JOIN console_spec s ON s.id=r.spec_id JOIN console_spec_revision v ON v.spec_id=s.id AND v.revision=r.revision WHERE c.request_id=? AND s.principal_id=? AND s.state='final' AND s.task_state='splitting' AND s.archived=0 AND r.status='completed' AND r.revision=?`, request, owner, revision).Scan(&spec, &currentRevision, &hash, &acceptance, &anchors, &original, &currentVersion, &confirmed)
	if err != nil {
		return ErrTaskFlowPermission
	}
	if revision != currentRevision || version != currentVersion {
		return ErrTaskFlowConflict
	}
	if err = templateSpecGate(ctx, tx, spec, revision, at); err != nil {
		return err
	}
	var old []ConsoleSpecPresplitTask
	if json.Unmarshal([]byte(original), &old) != nil || len(old) != len(tasks) {
		return ErrTaskFlowConflict
	}
	allowed := map[string]bool{}
	for _, t := range old {
		allowed[t.ID] = true
	}
	for _, t := range tasks {
		if !allowed[t.ID] {
			return ErrTaskFlowConflict
		}
	}
	raw, _ := json.Marshal(tasks)
	if confirmed {
		if original != string(raw) {
			return ErrTaskFlowConflict
		}
		if recheck == nil {
			return ErrTaskFlowPermission
		}
		return recheck()
	}
	for _, task := range tasks {
		deps, _ := json.Marshal(task.DependsOn)
		a, dod := presplitTaskDefinitionStorage(task.DefinitionOfDone)
		var dev, review any
		if task.DeveloperAgentID != "" {
			dev = task.DeveloperAgentID
			review = task.ReviewerAgentID
		}
		assignment := 0
		if dev != nil {
			assignment = 1
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec_task(id,spec_id,revision,content_sha256,title,task_content,status,estimated_hours,depends_on_json,acceptance_json,input_anchors_json,definition_of_done,created_by,created_at,developer_agent_id,reviewer_agent_id,group_code,assignment_version) VALUES(?,?,?,?,?,?,'planned',?,?,?,?,?,?,?,?,?,?,?)`, task.ID, spec, revision, hash, task.Title, task.Content, task.EstimatedHours, string(deps), a, anchors, dod, owner, at.UTC().Format(time.RFC3339Nano), dev, review, task.GroupCode, assignment); err != nil {
			return err
		}
		if dev != nil {
			if err = templateTaskGate(ctx, tx, task.ID, at, task.DeveloperAgentID, task.ReviewerAgentID); err != nil {
				return err
			}
		}
		t := TaskFlowState{ID: task.ID, Owner: owner, Version: 1, AssignmentVersion: assignment, GroupCode: task.GroupCode, Developer: task.DeveloperAgentID, Reviewer: task.ReviewerAgentID}
		for _, v := range []struct{ id, role string }{{t.Developer, "developer"}, {t.Reviewer, "reviewer"}} {
			if err = flowOutbox(ctx, tx, t, v.id, v.role, "assigned", at); err != nil {
				return err
			}
		}
		if err = flowAudit(ctx, tx, task.ID, owner, "confirm_candidates", 1, at); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_task_candidate SET tasks_json=?,confirmed_at=?,confirmed_by=? WHERE request_id=?`, string(raw), at.UTC().Format(time.RFC3339Nano), owner, request); err != nil {
		return err
	}
	if recheck == nil {
		return ErrTaskFlowPermission
	}
	if err = recheck(); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) ListConsoleTaskFlow(ctx context.Context, agent string) ([]TaskFlowItem, error) {
	tasks, err := s.ListConsoleTasksForAgent(ctx, agent)
	if err != nil {
		return nil, err
	}
	return s.consoleTaskFlowItems(ctx, agent, tasks)
}

// Selection and projection are separate SQLite reads. Revalidate the current
// actor instead of treating the initial selection as continuing authorization.
func (s *SQLite) consoleTaskFlowItems(ctx context.Context, agent string, tasks []model.ConsoleTaskProposal) ([]TaskFlowItem, error) {
	var err error
	out := make([]TaskFlowItem, 0, len(tasks))
	for _, task := range tasks {
		state, e := s.ConsoleTaskFlowState(ctx, task.ID)
		if e != nil {
			return nil, e
		}
		if state.Archived || state.GroupCode == "" || (agent != state.Developer && agent != state.Reviewer) {
			continue
		}
		var title string
		if err = s.db.QueryRowContext(ctx, `SELECT title FROM console_spec WHERE id=?`, task.SpecID).Scan(&title); err != nil {
			return nil, err
		}
		role := "developer"
		if agent == state.Reviewer {
			role = "reviewer"
		}
		cases, e := taskExpectedCases(ctx, s.db, task.ID)
		if e != nil {
			return nil, e
		}
		records := make([]TaskFlowCase, 0, len(cases))
		for key := range cases {
			parts := strings.SplitN(key, "\x00", 2)
			c := TaskFlowCase{AcceptanceID: parts[0], CaseName: parts[1]}
			var marked, confirmed sql.NullString
			e = s.db.QueryRowContext(ctx, `SELECT marked_by,confirmed_by FROM console_spec_task_case_result WHERE task_id=? AND acceptance_id=? AND case_name=?`, task.ID, c.AcceptanceID, c.CaseName).Scan(&marked, &confirmed)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			c.Marked = marked.Valid && marked.String == state.Developer
			c.Confirmed = c.Marked && confirmed.Valid && confirmed.String == state.Reviewer
			records = append(records, c)
		}
		sort.Slice(records, func(i, j int) bool {
			if records[i].AcceptanceID == records[j].AcceptanceID {
				return records[i].CaseName < records[j].CaseName
			}
			return records[i].AcceptanceID < records[j].AcceptanceID
		})
		task.Version = state.Version
		task.GroupCode = state.GroupCode
		task.NeedsReassignment = state.NeedsReassignment
		task.PassedCases = 0
		task.TotalCases = len(records)
		for _, c := range records {
			if c.Confirmed {
				task.PassedCases++
			}
		}
		out = append(out, TaskFlowItem{ConsoleTaskProposal: task, FlowState: state, TaskID: state.ID, SpecTitle: title, Role: role, Cases: records})
	}
	return out, nil
}

func (s *SQLite) ConsoleTaskFlowStates(ctx context.Context) ([]TaskFlowState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,s.principal_id,t.group_code,t.version,t.assignment_version,COALESCE(t.developer_agent_id,''),COALESCE(t.reviewer_agent_id,''),t.needs_reassignment FROM console_spec_task t JOIN console_spec s ON s.id=t.spec_id WHERE t.group_code<>'' AND s.archived=0 ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskFlowState{}
	for rows.Next() {
		var t TaskFlowState
		if err = rows.Scan(&t.ID, &t.Owner, &t.GroupCode, &t.Version, &t.AssignmentVersion, &t.Developer, &t.Reviewer, &t.NeedsReassignment); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLite) SupersedeConsoleTaskNotification(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE console_task_flow_outbox SET dispatched_at=?,terminal_reason='superseded' WHERE id=? AND dispatched_at IS NULL`, at.UTC().Format(time.RFC3339Nano), id)
	return err
}
