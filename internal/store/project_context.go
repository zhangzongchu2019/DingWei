package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
)

type ContextCandidate struct {
	State    TaskFlowState
	SpecID   string
	Revision int
	SHA      string
}

func contextCandidates(ctx context.Context, q taskQuery, actor, after string, n int) ([]ContextCandidate, error) {
	if n < 1 || n > 101 {
		return nil, ErrTaskFlowInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT t.id,s.principal_id,t.group_code,t.version,t.assignment_version,COALESCE(t.developer_agent_id,''),COALESCE(t.reviewer_agent_id,''),t.needs_reassignment,s.archived,t.spec_id,t.revision,t.content_sha256 FROM console_spec_task t JOIN console_spec s ON s.id=t.spec_id WHERE (t.developer_agent_id=? OR t.reviewer_agent_id=?) AND t.id>? ORDER BY t.id LIMIT ?`, actor, actor, after, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContextCandidate{}
	for rows.Next() {
		var c ContextCandidate
		v := &c.State
		err = rows.Scan(&v.ID, &v.Owner, &v.GroupCode, &v.Version, &v.AssignmentVersion, &v.Developer, &v.Reviewer, &v.NeedsReassignment, &v.Archived, &c.SpecID, &c.Revision, &c.SHA)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *SQLite) ProjectContextCandidates(ctx context.Context, actor, after string, n int) ([]ContextCandidate, error) {
	return contextCandidates(ctx, s.db, actor, after, n)
}

type ContextTask struct {
	ID                string            `json:"task_id"`
	Role              string            `json:"role"`
	Status            string            `json:"status"`
	Version           int               `json:"version"`
	AssignmentVersion int               `json:"assignment_version"`
	Blocker           string            `json:"blocker"`
	BlockerAgent      string            `json:"blocker_agent_id,omitempty"`
	Cases             []TaskFlowCase    `json:"cases"`
	SpecID            string            `json:"spec_id"`
	Title             string            `json:"title"`
	Revision          int               `json:"revision"`
	SHA               string            `json:"content_sha256"`
	Acceptance        []json.RawMessage `json:"acceptance"`
}
type ContextSpec struct {
	TaskID     string            `json:"task_id"`
	SpecID     string            `json:"spec_id"`
	Title      string            `json:"title"`
	Revision   int               `json:"revision"`
	SHA        string            `json:"content_sha256"`
	Acceptance []json.RawMessage `json:"acceptance"`
}

type ContextSnapshot struct {
	ctx   context.Context
	tx    *sql.Tx
	actor string
}

func (s *SQLite) WithProjectContextSnapshot(ctx context.Context, actor string, candidates []ContextCandidate, fn func(*ContextSnapshot) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	snap := &ContextSnapshot{ctx, tx, actor}
	for _, old := range candidates {
		now, e := taskFlowState(ctx, tx, old.State.ID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(now, old.State) {
			return ErrTaskFlowConflict
		}
		var specID, sha string
		var revision int
		if e := tx.QueryRowContext(ctx, `SELECT spec_id,revision,content_sha256 FROM console_spec_task WHERE id=?`, old.State.ID).Scan(&specID, &revision, &sha); e != nil {
			return e
		}
		if specID != old.SpecID || revision != old.Revision || sha != old.SHA {
			return ErrTaskFlowConflict
		}
	}
	if err = fn(snap); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *ContextSnapshot) Fingerprint() (string, error) {
	h := sha256.New()
	after := ""
	for {
		page, err := contextCandidates(s.ctx, s.tx, s.actor, after, 101)
		if err != nil {
			return "", err
		}
		for _, c := range page {
			// TaskFlowState has intentionally non-JSON owner/archive fields; explicitly bind them.
			raw, _ := json.Marshal([]any{c.State.ID, c.State.Owner, c.State.Archived, c.State.GroupCode, c.State.Version, c.State.AssignmentVersion, c.State.Developer, c.State.Reviewer, c.State.NeedsReassignment, c.SpecID, c.Revision, c.SHA})
			h.Write(raw)
			h.Write([]byte{0})
			after = c.State.ID
		}
		if len(page) < 101 {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *ContextSnapshot) EndpointVisible(key, name string) (bool, error) {
	var active, hidden bool
	err := s.tx.QueryRowContext(s.ctx, `SELECT active,no_directory FROM session_endpoint WHERE key_id=? AND session_name=?`, key, name).Scan(&active, &hidden)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return active && !hidden, err
}
func (s *ContextSnapshot) Project(c ContextCandidate) (ContextTask, ContextSpec, error) {
	t := ContextTask{ID: c.State.ID, SpecID: c.SpecID, Revision: c.Revision, SHA: c.SHA, Version: c.State.Version, AssignmentVersion: c.State.AssignmentVersion, Role: "developer", Acceptance: []json.RawMessage{}}
	if c.State.Reviewer == s.actor {
		t.Role = "reviewer"
	}
	var specTitle, raw, sha string
	err := s.tx.QueryRowContext(s.ctx, `SELECT t.title,t.status,COALESCE(t.blocker,''),s.title,r.acceptance_json,r.content_sha256 FROM console_spec_task t JOIN console_spec s ON s.id=t.spec_id JOIN console_spec_revision r ON r.spec_id=t.spec_id AND r.revision=t.revision WHERE t.id=?`, c.State.ID).Scan(&t.Title, &t.Status, &t.Blocker, &specTitle, &raw, &sha)
	if err != nil {
		return t, ContextSpec{}, err
	}
	if sha != c.SHA {
		return t, ContextSpec{}, ErrTaskFlowConflict
	}
	linked := map[string]bool{}
	after := ""
	for {
		rows, e := s.tx.QueryContext(s.ctx, `SELECT acceptance_id FROM console_spec_task_acceptance WHERE task_id=? AND acceptance_id>? ORDER BY acceptance_id LIMIT 101`, c.State.ID, after)
		if e != nil {
			return t, ContextSpec{}, e
		}
		n := 0
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return t, ContextSpec{}, e
			}
			linked[id] = true
			after = id
			n++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return t, ContextSpec{}, e
		}
		if n < 101 {
			break
		}
	}
	var criteria []json.RawMessage
	if err = json.Unmarshal([]byte(raw), &criteria); err != nil {
		var bundle struct {
			Criteria []json.RawMessage `json:"criteria"`
		}
		if err = json.Unmarshal([]byte(raw), &bundle); err != nil {
			return t, ContextSpec{}, err
		}
		criteria = bundle.Criteria
	}
	for _, criterion := range criteria {
		var id struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(criterion, &id); err != nil {
			return t, ContextSpec{}, err
		}
		if linked[id.ID] {
			t.Acceptance = append(t.Acceptance, criterion)
		}
	}

	if t.Status == "blocked" {
		t.BlockerAgent = c.State.Developer
	}
	expected, e := taskExpectedCases(s.ctx, s.tx, c.State.ID)
	if e != nil {
		return t, ContextSpec{}, e
	}
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	t.Cases = []TaskFlowCase{}
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		row := TaskFlowCase{AcceptanceID: parts[0], CaseName: parts[1]}
		var marked, confirmed sql.NullString
		e = s.tx.QueryRowContext(s.ctx, `SELECT marked_by,confirmed_by FROM console_spec_task_case_result WHERE task_id=? AND acceptance_id=? AND case_name=?`, c.State.ID, row.AcceptanceID, row.CaseName).Scan(&marked, &confirmed)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return t, ContextSpec{}, e
		}
		row.Marked = marked.Valid && marked.String == c.State.Developer
		row.Confirmed = row.Marked && confirmed.Valid && confirmed.String == c.State.Reviewer
		t.Cases = append(t.Cases, row)
	}
	return t, ContextSpec{c.State.ID, c.SpecID, specTitle, c.Revision, c.SHA, t.Acceptance}, nil
}
