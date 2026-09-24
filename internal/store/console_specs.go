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
	ErrConsoleSpecNotFound       = errors.New("console spec not found")
	ErrConsoleSpecConflict       = errors.New("console spec revision conflict")
	ErrConsoleSpecGateOrder      = errors.New("console spec gate order invalid")
	ErrConsoleSpecEditDenied     = errors.New("console spec editing denied")
	ErrConsoleSpecRevisionLocked = errors.New("console spec revision locked by downstream reference")
	ErrConsoleSpecPermission     = errors.New("console spec permission denied")
	ErrConsoleSpecFinal          = errors.New("console spec is finalized")
)

type consoleSpecOwnerQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// resolveOwnerPrincipalByShortName is the single source of truth for every
// short-name based spec assignee lookup: an active identity with an active owner role.
func resolveOwnerPrincipalByShortName(ctx context.Context, q consoleSpecOwnerQuerier, shortName string) (*model.ConsolePrincipal, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT p.id,p.provider,p.provider_issuer,p.provider_subject,p.display_name,p.active
		FROM console_principal p JOIN console_principal_role r ON r.principal_id=p.id AND r.role='owner' AND r.active=1
		WHERE p.active=1 AND p.display_name=? ORDER BY p.id`, shortName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ConsolePrincipal
	for rows.Next() {
		p := new(model.ConsolePrincipal)
		var active int
		if err := rows.Scan(&p.ID, &p.Provider, &p.ProviderIssuer, &p.ProviderSubject, &p.DisplayName, &active); err != nil {
			return nil, err
		}
		p.Active = active != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != 1 {
		return nil, ErrConsoleSpecPermission
	}
	return out[0], nil
}

func (s *SQLite) ListActiveConsolePrincipals(ctx context.Context) ([]model.ConsolePrincipal, error) {
	// Production currently has one active owner_key per principal. If multiple
	// scopes appear later, leave ShortName empty so the UI visibly degrades to the
	// human name instead of silently presenting an arbitrary owner scope.
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.provider,p.provider_issuer,p.provider_subject,p.display_name,p.active,CASE WHEN COUNT(DISTINCT r.owner_key)=1 THEN MIN(r.owner_key) ELSE '' END
		FROM console_principal p JOIN console_principal_role r ON r.principal_id=p.id AND r.role='owner' AND r.active=1
		WHERE p.active=1 GROUP BY p.id,p.provider,p.provider_issuer,p.provider_subject,p.display_name,p.active ORDER BY p.display_name,p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ConsolePrincipal
	for rows.Next() {
		var p model.ConsolePrincipal
		var active int
		if err := rows.Scan(&p.ID, &p.Provider, &p.ProviderIssuer, &p.ProviderSubject, &p.DisplayName, &active, &p.ShortName); err != nil {
			return nil, err
		}
		p.Active = active != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLite) TransferConsoleSpec(ctx context.Context, specID, actor, targetShort, nextState string, now time.Time) error {
	if nextState != "editing" && nextState != "review" {
		return errors.New("invalid spec state")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	target, err := resolveOwnerPrincipalByShortName(ctx, tx, targetShort)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE console_spec SET state=?,current_editor_id=?,updated_at=?
		WHERE id=? AND current_editor_id=? AND state<>'final' AND archived=0`, nextState, target.ID, now.UTC().Format(time.RFC3339Nano), specID, actor)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return tx.Commit()
}

func (s *SQLite) FinalizeConsoleSpec(ctx context.Context, specID, actor string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec SET state='final',current_editor_id=NULL,updated_at=? WHERE id=? AND current_editor_id=? AND state='review' AND archived=0`, now.UTC().Format(time.RFC3339Nano), specID, actor)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return nil
}

func (s *SQLite) CloseConsoleSpecTaskSplit(ctx context.Context, specID, actor string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec SET task_state='closed',updated_at=? WHERE id=? AND principal_id=? AND state='final' AND task_state='splitting'`, now.UTC().Format(time.RFC3339Nano), specID, actor)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return nil
}

func specTime(v string) (time.Time, error) { return time.Parse(time.RFC3339Nano, v) }

func appendConsoleSpecEvent(ctx context.Context, tx *sql.Tx, specID string, revision int, actor, action, detail string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO console_spec_event(id,spec_id,revision,actor,action,detail,at) VALUES(?,?,?,?,?,?,?)`,
		newID(), specID, revision, actor, action, detail, at.UTC().Format(time.RFC3339Nano))
	return err
}

func insertConsoleSpecRevision(ctx context.Context, tx *sql.Tx, revision model.ConsoleSpecRevision, at time.Time) error {
	via := revision.CreatedVia
	if via == "" {
		via = "principal"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_via,supersedes_revision,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, revision.SpecID, revision.Revision, revision.ContentMarkdown, revision.AcceptanceJSON,
		revision.InputAnchorsJSON, revision.ContentSHA256, revision.CreatedBy, via, nullInt(revision.Supersedes), at.UTC().Format(time.RFC3339Nano))
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
		strings.TrimSpace(spec.EditCapabilityHash) == "" || revision.SpecID != spec.ID || revision.Revision != 1 || revision.CreatedBy != spec.PrincipalID {
		return errors.New("invalid console spec")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec(id,principal_id,title,state,current_revision,current_editor_id,archived,task_state,edit_capability_hash,editing_closed,created_at,updated_at) VALUES(?,?,?,'editing',1,?,0,'splitting',?,0,?,?)`,
		spec.ID, spec.PrincipalID, spec.Title, spec.PrincipalID, spec.EditCapabilityHash, stamp, stamp); err != nil {
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
	var editor sql.NullString
	var archived int
	var editingClosed int
	var created, updated string
	err := row.Scan(&spec.ID, &spec.PrincipalID, &spec.OwnerShortName, &spec.Title, &spec.State, &spec.CurrentRevision, &editor, &spec.CurrentEditorShort, &archived, &spec.TaskState, &spec.EditCapabilityHash, &editingClosed, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	spec.CurrentEditorID = nullableString(editor)
	spec.Archived = archived != 0
	spec.EditingClosed = editingClosed != 0
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
	// For the owner projection, the final "未知业主" fallback is unreachable under
	// today's INNER JOIN plus principal foreign key. Keep it for a future schema
	// that may use a nullable/LEFT owner relation; do not write a test for it now.
	query := `SELECT s.id,s.principal_id,COALESCE(NULLIF(o.display_name,''),o.id,'未知业主'),s.title,s.state,s.current_revision,s.current_editor_id,COALESCE(NULLIF(e.display_name,''),e.id,CASE WHEN s.state='final' THEN COALESCE(NULLIF(o.display_name,''),o.id,'未知业主') END,'未知业主'),s.archived,s.task_state,s.edit_capability_hash,s.editing_closed,s.created_at,s.updated_at FROM console_spec s JOIN console_principal o ON o.id=s.principal_id LEFT JOIN console_principal e ON e.id=s.current_editor_id WHERE s.id=?`
	// Spec 内容按业主裁定“控写、不控读”。保留 principal/management 参数以兼容
	// 既有 repo 接口，但直接链接读取不再以身份过滤；写路径仍单独按 owner/capability 判。
	return scanConsoleSpec(s.db.QueryRowContext(ctx, query, specID))
}

func (s *SQLite) ListConsoleSpecs(ctx context.Context, principalID string, management bool) ([]model.ConsoleSpec, error) {
	// As in ConsoleSpec, the owner-side "未知业主" branch is unreachable while
	// this remains an INNER JOIN protected by the principal foreign key. It is a
	// deliberate future-schema fallback; the editor LEFT JOIN fallback is live.
	query := `SELECT s.id,s.principal_id,COALESCE(NULLIF(o.display_name,''),o.id,'未知业主'),s.title,s.state,s.current_revision,s.current_editor_id,COALESCE(NULLIF(e.display_name,''),e.id,CASE WHEN s.state='final' THEN COALESCE(NULLIF(o.display_name,''),o.id,'未知业主') END,'未知业主'),s.archived,s.task_state,s.edit_capability_hash,s.editing_closed,s.created_at,s.updated_at FROM console_spec s JOIN console_principal o ON o.id=s.principal_id LEFT JOIN console_principal e ON e.id=s.current_editor_id`
	var args []any
	if !management {
		query += ` WHERE s.principal_id=? OR s.current_editor_id=?`
		args = append(args, principalID)
		args = append(args, principalID)
	}
	query += ` ORDER BY s.updated_at DESC,s.id`
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
	query := `SELECT r.spec_id,r.revision,r.content_markdown,r.acceptance_json,r.input_anchors_json,r.content_sha256,r.created_by,r.created_via,COALESCE(r.supersedes_revision,0),r.created_at
		FROM console_spec_revision r JOIN console_spec s ON s.id=r.spec_id WHERE r.spec_id=? AND r.revision=?`
	var r model.ConsoleSpecRevision
	var created string
	err := s.db.QueryRowContext(ctx, query, specID, revision).Scan(&r.SpecID, &r.Revision, &r.ContentMarkdown, &r.AcceptanceJSON, &r.InputAnchorsJSON, &r.ContentSHA256, &r.CreatedBy, &r.CreatedVia, &r.Supersedes, &created)
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
	return s.appendConsoleSpecRevision(ctx, principalID, specID, expected, revision, now, false)
}

func (s *SQLite) OpenConsoleSpecRevision(ctx context.Context, principalID, specID string, expected int, revision model.ConsoleSpecRevision, now time.Time) (int, error) {
	return s.appendConsoleSpecRevision(ctx, principalID, specID, expected, revision, now, true)
}

func (s *SQLite) appendConsoleSpecRevision(ctx context.Context, principalID, specID string, expected int, revision model.ConsoleSpecRevision, now time.Time, openNew bool) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var current int
	err = tx.QueryRowContext(ctx, `SELECT current_revision FROM console_spec WHERE id=? AND current_editor_id=? AND state<>'final' AND archived=0`, specID, principalID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		if checkErr := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_spec WHERE id=?`, specID).Scan(&exists); checkErr != nil {
			return 0, checkErr
		}
		if exists > 0 {
			return 0, ErrConsoleSpecEditDenied
		}
		return 0, ErrConsoleSpecNotFound
	}
	if err != nil {
		return 0, err
	}
	if current != expected {
		return 0, ErrConsoleSpecConflict
	}
	if !openNew {
		var locked int
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM console_task_proposal WHERE spec_id=? AND revision=?)+(SELECT COUNT(*) FROM console_spec_task WHERE spec_id=? AND revision=?)`, specID, current, specID, current).Scan(&locked); err != nil {
			return 0, err
		}
		if locked != 0 {
			return 0, ErrConsoleSpecRevisionLocked
		}
	}
	next := current + 1
	if revision.SpecID != specID || revision.Revision != next || revision.CreatedBy != principalID {
		return 0, errors.New("invalid console spec revision")
	}
	revision.Supersedes = current
	if err = insertConsoleSpecRevision(ctx, tx, revision, now); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE console_spec SET current_revision=?,updated_at=? WHERE id=? AND current_editor_id=? AND state<>'final' AND archived=0 AND editing_closed=0 AND current_revision=?`,
		next, now.UTC().Format(time.RFC3339Nano), specID, principalID, expected)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return 0, ErrConsoleSpecEditDenied
	}
	if err = appendConsoleSpecEvent(ctx, tx, specID, next, principalID, "revise", fmt.Sprintf("supersedes=%d", current), now); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

// AppendConsoleSpecRevisionWithCapability writes through a shared edit link.
// Authorization, closed-state check and revision CAS happen in one transaction.
func (s *SQLite) AppendConsoleSpecRevisionWithCapability(ctx context.Context, specID string, expected int, capabilityHash string, revision model.ConsoleSpecRevision, now time.Time) (int, error) {
	return s.appendConsoleSpecRevisionWithCapability(ctx, specID, expected, capabilityHash, revision, now, false)
}

func (s *SQLite) OpenConsoleSpecRevisionWithCapability(ctx context.Context, specID string, expected int, capabilityHash string, revision model.ConsoleSpecRevision, now time.Time) (int, error) {
	return s.appendConsoleSpecRevisionWithCapability(ctx, specID, expected, capabilityHash, revision, now, true)
}

func (s *SQLite) appendConsoleSpecRevisionWithCapability(ctx context.Context, specID string, expected int, capabilityHash string, revision model.ConsoleSpecRevision, now time.Time, openNew bool) (int, error) {
	return 0, ErrConsoleSpecEditDenied
}

func (s *SQLite) CloseConsoleSpecEditing(ctx context.Context, principalID, specID string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec SET editing_closed=1,updated_at=? WHERE id=? AND principal_id=? AND editing_closed=0`,
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

func (s *SQLite) ReopenConsoleSpecEditing(ctx context.Context, principalID, specID string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec SET editing_closed=0,updated_at=? WHERE id=? AND principal_id=? AND editing_closed=1`,
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

func (s *SQLite) ArchiveConsoleSpec(ctx context.Context, specID, actor string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT current_revision FROM console_spec WHERE id=? AND archived=0`, specID).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		return ErrConsoleSpecNotFound
	} else if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET archived=1,updated_at=? WHERE id=?`, now.UTC().Format(time.RFC3339Nano), specID); err != nil {
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
	err := row.Scan(&p.ID, &p.SchemaVersion, &p.SpecID, &p.Revision, &p.ContentSHA256, &p.Title, &p.TaskContent, &p.Status, &p.EstimatedHours, &p.DependsOnJSON, &p.AcceptanceJSON, &p.InputAnchorsJSON, &p.ReviewID, &p.ReviewedBy, &reviewed, &created)
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

const proposalColumns = `p.id,p.schema_version,p.spec_id,p.revision,p.content_sha256,p.title,p.task_content,p.status,p.estimated_hours,p.depends_on_json,p.acceptance_json,p.input_anchors_json,p.review_id,p.reviewed_by,p.reviewed_at,p.created_at`
const taskUnion = `SELECT ` + proposalColumns + ` FROM console_task_proposal p UNION ALL
	SELECT t.id,1,t.spec_id,t.revision,t.content_sha256,t.title,t.task_content,t.status,t.estimated_hours,t.depends_on_json,t.acceptance_json,t.input_anchors_json,'manual:'||t.id,t.created_by,t.created_at,t.created_at FROM console_spec_task t`

func (s *SQLite) ListReadyConsoleTaskProposals(ctx context.Context) ([]model.ConsoleTaskProposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+proposalColumns+` FROM console_task_proposal p JOIN console_spec s ON s.id=p.spec_id AND s.current_revision=p.revision WHERE s.state='final' AND s.task_state='closed' ORDER BY p.created_at,p.id`)
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
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM (`+taskUnion+`) q WHERE q.spec_id=? ORDER BY q.revision,q.created_at,q.id`, specID)
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

func (s *SQLite) ListAllConsoleTaskProposals(ctx context.Context) ([]model.ConsoleTaskProposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM (`+taskUnion+`) q ORDER BY q.created_at,q.id`)
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

func (s *SQLite) CreateConsoleTaskProposal(ctx context.Context, proposal model.ConsoleTaskProposal, principalID string, management bool) error {
	if proposal.ID == "" || proposal.SpecID == "" || proposal.Title == "" || proposal.TaskContent == "" || proposal.EstimatedHours < 0 {
		return errors.New("invalid task plan")
	}
	var dependsOn []string
	if err := json.Unmarshal([]byte(proposal.DependsOnJSON), &dependsOn); err != nil {
		return errors.New("invalid task dependencies")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var title, contentHash, acceptance, anchors string
	if err = tx.QueryRowContext(ctx, `SELECT s.title,r.content_sha256,r.acceptance_json,r.input_anchors_json
		FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=s.current_revision
		WHERE s.id=? AND s.principal_id=? AND s.state='final' AND s.task_state='splitting'`, proposal.SpecID, principalID).Scan(&title, &contentHash, &acceptance, &anchors); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConsoleSpecPermission
		}
		return err
	}
	if proposal.AcceptanceJSON != "" {
		acceptance = proposal.AcceptanceJSON
	}
	seen := map[string]bool{}
	for _, dep := range dependsOn {
		if dep == "" || dep == proposal.ID || seen[dep] {
			return errors.New("invalid task dependency")
		}
		seen[dep] = true
		var ignored string
		if err = tx.QueryRowContext(ctx, `SELECT spec_id FROM (`+taskUnion+`) q WHERE id=?`, dep).Scan(&ignored); err != nil {
			return errors.New("dependency task not found or not frozen")
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,depends_on_json FROM (`+taskUnion+`) q`)
	if err != nil {
		return err
	}
	graph := map[string][]string{proposal.ID: append([]string(nil), dependsOn...)}
	for rows.Next() {
		var id, raw string
		var deps []string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal([]byte(raw), &deps)
		graph[id] = deps
	}
	if err = rows.Close(); err != nil {
		return err
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if done[id] {
			return false
		}
		visiting[id] = true
		for _, dep := range graph[id] {
			if visit(dep) {
				return true
			}
		}
		visiting[id] = false
		done[id] = true
		return false
	}
	for id := range graph {
		if visit(id) {
			return errors.New("task dependency cycle")
		}
	}
	stamp := proposal.CreatedAt.UTC().Format(time.RFC3339Nano)
	if proposal.ReviewedAt.IsZero() {
		proposal.ReviewedAt = proposal.CreatedAt
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO console_spec_task(id,spec_id,revision,content_sha256,title,task_content,status,estimated_hours,depends_on_json,acceptance_json,input_anchors_json,created_by,created_at)
		SELECT ?,s.id,s.current_revision,?,?,?,?,?,?,?,?,?,? FROM console_spec s WHERE s.id=?`, proposal.ID, contentHash,
		proposal.Title, proposal.TaskContent, proposal.Status, proposal.EstimatedHours, proposal.DependsOnJSON, acceptance, anchors,
		proposal.ReviewedBy, stamp, proposal.SpecID)
	if err != nil {
		return err
	}
	_ = title
	return tx.Commit()
}

func (s *SQLite) UpdateConsoleTaskProposalPlan(ctx context.Context, proposalID, principalID string, management bool, content, status string, hours float64, dependsOn []string, acceptanceJSON string) error {
	if proposalID == "" || hours < 0 {
		return errors.New("invalid task plan")
	}
	allowed := map[string]bool{"proposed": true, "planned": true, "in_progress": true, "blocked": true, "done": true}
	if !allowed[status] {
		return errors.New("invalid task status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var specID string
	if err = tx.QueryRowContext(ctx, `SELECT q.spec_id FROM (`+taskUnion+`) q JOIN console_spec s ON s.id=q.spec_id WHERE q.id=? AND s.principal_id=? AND s.state='final' AND s.task_state='splitting'`, proposalID, principalID).Scan(&specID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConsoleSpecPermission
		}
		return err
	}
	seen := map[string]bool{}
	for _, dep := range dependsOn {
		if dep == proposalID {
			return errors.New("task cannot depend on itself")
		}
		if dep == "" || seen[dep] {
			return errors.New("invalid duplicate dependency")
		}
		seen[dep] = true
		var depSpec string
		if err = tx.QueryRowContext(ctx, `SELECT spec_id FROM (`+taskUnion+`) q WHERE id=?`, dep).Scan(&depSpec); err != nil {
			return errors.New("dependency task not found or not frozen")
		}
		// A cross-spec target is valid only because a proposal row is itself the
		// immutable downstream reference that freezes that exact revision.
		_ = depSpec
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,depends_on_json FROM (`+taskUnion+`) q`)
	if err != nil {
		return err
	}
	graph := map[string][]string{}
	for rows.Next() {
		var id, raw string
		var deps []string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal([]byte(raw), &deps)
		graph[id] = deps
	}
	rows.Close()
	graph[proposalID] = append([]string(nil), dependsOn...)
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if done[id] {
			return false
		}
		visiting[id] = true
		for _, dep := range graph[id] {
			if visit(dep) {
				return true
			}
		}
		visiting[id] = false
		done[id] = true
		return false
	}
	for id := range graph {
		if visit(id) {
			return errors.New("task dependency cycle")
		}
	}
	raw, _ := json.Marshal(dependsOn)
	if acceptanceJSON == "" {
		acceptanceJSON = "[]"
	}
	res, err := tx.ExecContext(ctx, `UPDATE console_task_proposal SET task_content=?,status=?,estimated_hours=?,depends_on_json=?,acceptance_json=? WHERE id=?`, content, status, hours, string(raw), acceptanceJSON, proposalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		res, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET task_content=?,status=?,estimated_hours=?,depends_on_json=?,acceptance_json=? WHERE id=?`, content, status, hours, string(raw), acceptanceJSON, proposalID)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		if n != 1 {
			return sql.ErrNoRows
		}
	}
	return tx.Commit()
}

func (s *SQLite) DeleteConsoleSpecTask(ctx context.Context, taskID, principalID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM console_spec_task WHERE id=? AND EXISTS(SELECT 1 FROM console_spec s WHERE s.id=console_spec_task.spec_id AND s.principal_id=? AND s.state='final' AND s.task_state='splitting')`, taskID, principalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return nil
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
