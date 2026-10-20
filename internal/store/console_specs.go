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
	ErrConsoleSpecAgentOwnership  = errors.New("只能更新由当前 Agent 创建的 spec")
	ErrConsoleSpecNotFound        = errors.New("console spec not found")
	ErrConsoleSpecConflict        = errors.New("console spec revision conflict")
	ErrConsoleSpecGateOrder       = errors.New("console spec gate order invalid")
	ErrConsoleSpecEditDenied      = errors.New("console spec editing denied")
	ErrConsoleSpecRevisionLocked  = errors.New("console spec revision locked by downstream reference")
	ErrConsoleSpecPermission      = errors.New("console spec permission denied")
	ErrConsoleSpecSelfTransfer    = errors.New("无需转给自己；请直接提交复核")
	ErrConsoleSpecFinal           = errors.New("console spec is finalized")
	ErrConsoleSpecGate            = errors.New("console spec generation gate incomplete")
	ErrConsoleSpecReviewerInvalid = errors.New("console spec review agent is missing, inactive or unknown")
	ErrConsoleSpecReviewerLookup  = errors.New("console spec review agent lookup failed")
	ErrConsoleTaskAcceptance      = errors.New("console task acceptance must reference this spec revision")
	ErrConsoleTaskDefinition      = errors.New("task proposal cannot store a task definition of done")
	ErrConsoleTaskCasePermission  = errors.New("console task case actor is not assigned to this task")
	ErrConsoleTaskCaseUnknown     = errors.New("console task case is not linked to this task")
	ErrConsoleTaskAssigneePair    = errors.New("研发与验收必须成对指定")
	ErrConsoleTaskAssigneeSame    = errors.New("研发与验收必须由不同 Agent 承担")
)

const appendConsoleSpecRevisionUpdateSQL = `UPDATE console_spec SET current_revision=?,updated_at=? WHERE id=? AND current_editor_id=? AND archived=0 AND editing_closed=0 AND current_revision=?`

type storedTaskAcceptance struct {
	ID string `json:"id"`
}

func splitTaskDefinitionOfDone(definition string) (string, string) {
	return `[]`, strings.TrimSpace(definition)
}

func legacyTaskDefinitionInAcceptance(definition string) (string, string) {
	raw, _ := json.Marshal([]map[string]string{{"id": "DOD", "text": strings.TrimSpace(definition)}})
	return string(raw), ""
}

var (
	createTaskDefinitionStorage  = splitTaskDefinitionOfDone
	updateTaskDefinitionStorage  = splitTaskDefinitionOfDone
	skipTaskAcceptanceMembership bool
	skipTaskAcceptanceValidation bool
	skipTaskAcceptanceWrite      bool
	hideTaskAcceptanceRead       bool
	treatEveryAcceptanceAsLinked bool
	skipLinkedAcceptanceLookup   bool
	allowProposalTaskDefinition  bool
	skipProposalDefinitionCheck  bool
	skipConsoleSpecTaskDelete    bool
)

func dropTaskDefinitionStorage(string) (string, string) { return `[]`, "" }

func taskAcceptanceIDs(raw string) ([]string, error) {
	var entries []storedTaskAcceptance
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "[") {
		if json.Unmarshal([]byte(trimmed), &entries) != nil {
			return nil, ErrConsoleTaskAcceptance
		}
	} else {
		var bundle struct {
			Criteria []storedTaskAcceptance `json:"criteria"`
		}
		if json.Unmarshal([]byte(trimmed), &bundle) != nil {
			return nil, ErrConsoleTaskAcceptance
		}
		entries = bundle.Criteria
	}
	if len(entries) == 0 {
		return nil, ErrConsoleTaskAcceptance
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			return nil, ErrConsoleTaskAcceptance
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func taskAcceptanceMembership(taskIDs, specIDs []string) error {
	allowed := make(map[string]bool, len(specIDs))
	for _, id := range specIDs {
		allowed[id] = true
	}
	for _, id := range taskIDs {
		if !allowed[id] {
			return ErrConsoleTaskAcceptance
		}
	}
	return nil
}

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
	if target.ID == actor {
		return ErrConsoleSpecSelfTransfer
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

// SubmitConsoleSpecReview enters review without inventing a transfer. The
// current editor remains unchanged so a legitimate one-person project can run
// Agent review and finalize under the existing gates.
func (s *SQLite) SubmitConsoleSpecReview(ctx context.Context, specID, actor string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec SET state='review',updated_at=? WHERE id=? AND current_editor_id=? AND state='editing' AND archived=0`, now.UTC().Format(time.RFC3339Nano), specID, actor)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return nil
}

type storedSpecCriterion struct {
	OwnerRef  string   `json:"owner_ref"`
	Source    string   `json:"source"`
	CheckedBy string   `json:"checked_by"`
	CheckedAt string   `json:"checked_at"`
	Cases     []string `json:"cases"`
}

type storedSpecAcceptanceBundle struct {
	Criteria []storedSpecCriterion `json:"criteria"`
}

type storedSpecGateIssue struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Response string `json:"response"`
	Outcome  string `json:"outcome"`
}

func validSpecFinalAcceptance(raw string) bool {
	var legacy []json.RawMessage
	if json.Unmarshal([]byte(raw), &legacy) == nil {
		// Pre-S-GEN revisions used a bare array and are explicitly out of backfill
		// scope. New writes always persist the object bundle below.
		return len(legacy) > 0
	}
	var bundle storedSpecAcceptanceBundle
	if json.Unmarshal([]byte(raw), &bundle) != nil || len(bundle.Criteria) == 0 {
		return false
	}
	for _, item := range bundle.Criteria {
		if strings.TrimSpace(item.OwnerRef) == "" || (item.Source != "owner" && item.Source != "ai") || strings.TrimSpace(item.CheckedBy) == "" || strings.TrimSpace(item.CheckedAt) == "" || len(item.Cases) == 0 {
			return false
		}
		if _, err := time.Parse(time.RFC3339, item.CheckedAt); err != nil {
			return false
		}
		for _, testCase := range item.Cases {
			if strings.TrimSpace(testCase) == "" {
				return false
			}
		}
	}
	return true
}

func legacySpecAcceptance(raw string) bool {
	var legacy []json.RawMessage
	return json.Unmarshal([]byte(raw), &legacy) == nil
}

func validCompletedSpecGateReview(raw string) bool {
	var issues []storedSpecGateIssue
	if json.Unmarshal([]byte(raw), &issues) != nil || len(issues) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, issue := range issues {
		if strings.TrimSpace(issue.ID) == "" || strings.TrimSpace(issue.Text) == "" || strings.TrimSpace(issue.Response) == "" || (issue.Outcome != "true_problem" && issue.Outcome != "false_positive") || seen[issue.ID] {
			return false
		}
		seen[issue.ID] = true
	}
	return true
}

func (s *SQLite) SaveConsoleSpecGateReview(ctx context.Context, specID, actor string, review model.ConsoleSpecGateReview, now time.Time) error {
	if strings.TrimSpace(review.ReviewAgentID) == "" {
		return ErrConsoleSpecReviewerInvalid
	}
	if strings.TrimSpace(review.IssuesJSON) == "" {
		return ErrConsoleSpecGate
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT current_revision FROM console_spec WHERE id=? AND current_editor_id=? AND state='review' AND archived=0`, specID, actor).Scan(&revision); err != nil {
		return ErrConsoleSpecPermission
	}
	// 🔴 业主 2026-09-25 裁定「删」：原先此处比 `owner_principal == actor`（即"复核 Agent 归谁"），
	// 而 Q7 要挡的是"复核者是不是操作者本人" —— 后者所需的事实在 spec 路由上并未被记下来
	// （该路由挂 RequireConsolePrincipal，context 只有 principal＋session，ConsoleSession 无 Agent 字段）。
	// ⇒ 本批降为【记录 ＋ 不卡死】：**不再按归属人拒绝**；仅校验复核 Agent 存在且启用。
	//    Q7 真语义与「操作者 Agent 身份落库」另开一项（动登录通路会移动基线，不在本批）。
	var reviewerExists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM console_agent WHERE id=? AND active=1`, review.ReviewAgentID).Scan(&reviewerExists); errors.Is(err, sql.ErrNoRows) {
		return ErrConsoleSpecReviewerInvalid
	} else if err != nil {
		return fmt.Errorf("%w: %v", ErrConsoleSpecReviewerLookup, err)
	}
	var issues []storedSpecGateIssue
	if json.Unmarshal([]byte(review.IssuesJSON), &issues) != nil {
		return ErrConsoleSpecGate
	}
	seen := map[string]bool{}
	for _, issue := range issues {
		if strings.TrimSpace(issue.ID) == "" || strings.TrimSpace(issue.Text) == "" || seen[issue.ID] || (issue.Outcome != "pending" && issue.Outcome != "true_problem" && issue.Outcome != "false_positive") {
			return ErrConsoleSpecGate
		}
		seen[issue.ID] = true
	}
	if err = annotateSpecAgentGate(ctx, tx, specID, revision, review); err != nil {
		return err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	reviewID := review.ID
	if reviewID == "" {
		reviewID = newID()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO console_spec_gate_review(id,spec_id,revision,review_agent_id,operator_principal,issues_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(spec_id,revision) DO UPDATE SET review_agent_id=excluded.review_agent_id,operator_principal=excluded.operator_principal,issues_json=excluded.issues_json,updated_at=excluded.updated_at`,
		reviewID, specID, revision, review.ReviewAgentID, actor, review.IssuesJSON, stamp, stamp)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) ConsoleSpecGateReview(ctx context.Context, specID string, revision int) (*model.ConsoleSpecGateReview, error) {
	var review model.ConsoleSpecGateReview
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,spec_id,revision,review_agent_id,operator_principal,issues_json,created_at,updated_at FROM console_spec_gate_review WHERE spec_id=? AND revision=?`, specID, revision).
		Scan(&review.ID, &review.SpecID, &review.Revision, &review.ReviewAgentID, &review.OperatorPrincipal, &review.IssuesJSON, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	review.CreatedAt, err = specTime(created)
	if err != nil {
		return nil, err
	}
	review.UpdatedAt, err = specTime(updated)
	if err != nil {
		return nil, err
	}
	return &review, nil
}

func (s *SQLite) FinalizeConsoleSpec(ctx context.Context, specID, actor string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int
	var acceptance string
	if err = tx.QueryRowContext(ctx, `SELECT s.current_revision,r.acceptance_json FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=s.current_revision WHERE s.id=? AND s.current_editor_id=? AND s.state='review' AND s.archived=0`, specID, actor).Scan(&revision, &acceptance); err != nil {
		return ErrConsoleSpecPermission
	}
	legacy := legacySpecAcceptance(acceptance)
	if !validSpecFinalAcceptance(acceptance) {
		return ErrConsoleSpecGate
	}
	if !legacy {
		var issues string
		if err = tx.QueryRowContext(ctx, `SELECT issues_json FROM console_spec_gate_review WHERE spec_id=? AND revision=? AND operator_principal=?`, specID, revision, actor).Scan(&issues); err != nil {
			return ErrConsoleSpecGate
		}
		if !validCompletedSpecGateReview(issues) {
			allowed, e := confirmedEmptySpecAgentGate(ctx, tx, specID, revision, issues)
			if e != nil {
				return e
			}
			if !allowed {
				return ErrConsoleSpecGate
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE console_spec SET state='final',current_editor_id=NULL,updated_at=? WHERE id=? AND current_editor_id=? AND state='review' AND archived=0`, now.UTC().Format(time.RFC3339Nano), specID, actor)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return tx.Commit()
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

func normalizeConsoleSpecActor(revision *model.ConsoleSpecRevision) {
	if revision.CreatedByKind == "" {
		revision.CreatedByKind = "principal"
	}
}

func validateConsoleSpecActor(ctx context.Context, tx *sql.Tx, revision model.ConsoleSpecRevision) error {
	if strings.TrimSpace(revision.CreatedBy) == "" || (revision.CreatedByKind != "principal" && revision.CreatedByKind != "agent") {
		return errors.New("invalid console spec actor")
	}
	query := `SELECT COUNT(*) FROM console_principal WHERE id=? AND active=1`
	if revision.CreatedByKind == "agent" {
		query = `SELECT COUNT(*) FROM console_agent WHERE id=? AND active=1`
	}
	var n int
	if err := tx.QueryRowContext(ctx, query, revision.CreatedBy).Scan(&n); err != nil || n != 1 {
		return errors.New("invalid console spec actor")
	}
	return nil
}

func insertConsoleSpecRevision(ctx context.Context, tx *sql.Tx, revision model.ConsoleSpecRevision, at time.Time) error {
	normalizeConsoleSpecActor(&revision)
	if err := validateConsoleSpecActor(ctx, tx, revision); err != nil {
		return err
	}
	via := revision.CreatedVia
	if via == "" {
		via = "principal"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO console_spec_revision(spec_id,revision,content_markdown,acceptance_json,input_anchors_json,content_sha256,created_by,created_by_kind,created_via,supersedes_revision,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, revision.SpecID, revision.Revision, revision.ContentMarkdown, revision.AcceptanceJSON,
		revision.InputAnchorsJSON, revision.ContentSHA256, revision.CreatedBy, revision.CreatedByKind, via, nullInt(revision.Supersedes), at.UTC().Format(time.RFC3339Nano))
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
		strings.TrimSpace(spec.EditCapabilityHash) == "" || revision.SpecID != spec.ID || revision.Revision != 1 {
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
	action := "create"
	if revision.CreatedByKind == "agent" {
		action = "agent_create"
	}
	if err = appendConsoleSpecEvent(ctx, tx, spec.ID, 1, revision.CreatedBy, action, "", now); err != nil {
		return err
	}
	return tx.Commit()
}

func scanConsoleSpec(row rowScanner) (*model.ConsoleSpec, error) {
	var spec model.ConsoleSpec
	var editor sql.NullString
	var derived sql.NullString
	var archived int
	var editingClosed int
	var created, updated string
	err := row.Scan(&spec.ID, &spec.PrincipalID, &spec.OwnerShortName, &spec.Title, &spec.State, &spec.CurrentRevision, &editor, &spec.CurrentEditorShort, &archived, &spec.TaskState, &spec.EditCapabilityHash, &editingClosed, &derived, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	spec.CurrentEditorID = nullableString(editor)
	spec.Archived = archived != 0
	spec.EditingClosed = editingClosed != 0
	spec.DerivedFrom = nullableString(derived)
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
	query := `SELECT s.id,s.principal_id,COALESCE(NULLIF(o.display_name,''),o.id),s.title,s.state,s.current_revision,s.current_editor_id,COALESCE(NULLIF(e.display_name,''),e.id,''),s.archived,s.task_state,s.edit_capability_hash,s.editing_closed,s.derived_from,s.created_at,s.updated_at FROM console_spec s JOIN console_principal o ON o.id=s.principal_id LEFT JOIN console_principal e ON e.id=s.current_editor_id WHERE s.id=?`
	// Spec 内容按业主裁定“控写、不控读”。保留 principal/management 参数以兼容
	// 既有 repo 接口，但直接链接读取不再以身份过滤；写路径仍单独按 owner/capability 判。
	return scanConsoleSpec(s.db.QueryRowContext(ctx, query, specID))
}

func (s *SQLite) ListConsoleSpecs(ctx context.Context, principalID string, management bool) ([]model.ConsoleSpec, error) {
	query := `SELECT s.id,s.principal_id,COALESCE(NULLIF(o.display_name,''),o.id),s.title,s.state,s.current_revision,s.current_editor_id,COALESCE(NULLIF(e.display_name,''),e.id,''),s.archived,s.task_state,s.edit_capability_hash,s.editing_closed,s.derived_from,s.created_at,s.updated_at FROM console_spec s JOIN console_principal o ON o.id=s.principal_id LEFT JOIN console_principal e ON e.id=s.current_editor_id`
	var args []any
	if !management {
		query += ` WHERE s.principal_id=? OR s.current_editor_id=?`
		args = append(args, principalID)
		args = append(args, principalID)
	}
	query += ` ORDER BY s.created_at DESC,s.id`
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

func (s *SQLite) ConsoleSpecAttribution(ctx context.Context, specID string) (model.ConsoleSpecAttribution, error) {
	var out model.ConsoleSpecAttribution
	var creatorID, creatorKind string
	if err := s.db.QueryRowContext(ctx, `SELECT created_by,created_by_kind FROM console_spec_revision WHERE spec_id=? AND revision=1`, specID).Scan(&creatorID, &creatorKind); err != nil {
		return out, err
	}
	actorLabel := func(kind, id string) string {
		if kind == "unknown" {
			return "无法还原"
		}
		var label string
		if kind == "agent" {
			_ = s.db.QueryRowContext(ctx, `SELECT session_name FROM console_agent WHERE id=?`, id).Scan(&label)
		} else {
			_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(display_name,''),id) FROM console_principal WHERE id=?`, id).Scan(&label)
		}
		if label == "" {
			return id
		}
		return label
	}
	var created, lastEdited, lastID, lastKind string
	if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM console_spec_revision WHERE spec_id=? AND revision=1`, specID).Scan(&created); err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT created_by,created_by_kind,created_at FROM console_spec_revision WHERE spec_id=? ORDER BY revision DESC LIMIT 1`, specID).Scan(&lastID, &lastKind, &lastEdited); err != nil {
		return out, err
	}
	out.CreatedAt, _ = specTime(created)
	out.LastEditedAt, _ = specTime(lastEdited)
	out.LastEditor = actorLabel(lastKind, lastID)
	out.CreatorKind = creatorKind
	out.Creator = actorLabel(creatorKind, creatorID)
	rows, err := s.db.QueryContext(ctx, `
		SELECT created_by_kind,created_by FROM console_spec_revision WHERE spec_id=? AND revision>1
		UNION SELECT 'agent',review_agent_id FROM console_spec_gate_review WHERE spec_id=?
		UNION SELECT 'principal',operator_principal FROM console_spec_gate_review WHERE spec_id=?
		UNION SELECT 'principal',reviewed_by FROM console_spec_review WHERE spec_id=?`, specID, specID, specID, specID)
	if err != nil {
		return out, err
	}
	seen := map[string]bool{creatorKind + "\x00" + creatorID: true}
	type actorRef struct{ kind, id string }
	var participants []actorRef
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return out, err
		}
		key := kind + "\x00" + id
		if seen[key] || kind == "unknown" || id == "" {
			continue
		}
		seen[key] = true
		participants = append(participants, actorRef{kind, id})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	for _, participant := range participants {
		out.Participants = append(out.Participants, actorLabel(participant.kind, participant.id))
	}
	return out, nil
}

func (s *SQLite) DeriveConsoleSpec(ctx context.Context, principalID, sourceID string, spec model.ConsoleSpec, revision model.ConsoleSpecRevision, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var sourceRevision int
	var sourceHash string
	if err = tx.QueryRowContext(ctx, `SELECT s.current_revision,r.content_sha256 FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=s.current_revision WHERE s.id=? AND s.principal_id=? AND s.state='final' AND s.archived=0`, sourceID, principalID).Scan(&sourceRevision, &sourceHash); err != nil {
		return ErrConsoleSpecPermission
	}
	if spec.ID == "" || spec.PrincipalID != principalID || spec.DerivedFrom != sourceID || revision.SpecID != spec.ID || revision.Revision != 1 || revision.ContentSHA256 != sourceHash {
		return errors.New("invalid derived console spec")
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec(id,principal_id,title,state,current_revision,current_editor_id,archived,task_state,edit_capability_hash,editing_closed,derived_from,created_at,updated_at) VALUES(?,?,?,'editing',1,?,0,'splitting',?,0,?,?,?)`,
		spec.ID, principalID, spec.Title, principalID, spec.EditCapabilityHash, sourceID, stamp, stamp); err != nil {
		return err
	}
	if err = insertConsoleSpecRevision(ctx, tx, revision, now); err != nil {
		return err
	}
	if err = appendConsoleSpecEvent(ctx, tx, spec.ID, 1, revision.CreatedBy, "derive", fmt.Sprintf("source=%s revision=%d", sourceID, sourceRevision), now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) ConsoleSpecRevision(ctx context.Context, principalID, specID string, revision int, management bool) (*model.ConsoleSpecRevision, error) {
	query := `SELECT r.spec_id,r.revision,r.content_markdown,r.acceptance_json,r.input_anchors_json,r.content_sha256,r.created_by,r.created_by_kind,r.created_via,COALESCE(r.supersedes_revision,0),r.created_at
		FROM console_spec_revision r JOIN console_spec s ON s.id=r.spec_id WHERE r.spec_id=? AND r.revision=?`
	var r model.ConsoleSpecRevision
	var created string
	err := s.db.QueryRowContext(ctx, query, specID, revision).Scan(&r.SpecID, &r.Revision, &r.ContentMarkdown, &r.AcceptanceJSON, &r.InputAnchorsJSON, &r.ContentSHA256, &r.CreatedBy, &r.CreatedByKind, &r.CreatedVia, &r.Supersedes, &created)
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
	return s.appendConsoleSpecRevision(ctx, principalID, specID, expected, revision, now, false, false)
}

func (s *SQLite) OpenConsoleSpecRevision(ctx context.Context, principalID, specID string, expected int, revision model.ConsoleSpecRevision, now time.Time) (int, error) {
	return s.appendConsoleSpecRevision(ctx, principalID, specID, expected, revision, now, true, false)
}

// AppendConsoleSpecRevisionByAgent is only for authenticated direct full-spec
// updates. Explicitly requested inference assistance retains its separate gate.
func (s *SQLite) AppendConsoleSpecRevisionByAgent(ctx context.Context, principalID, specID string, expected int, revision model.ConsoleSpecRevision, now time.Time) (int, error) {
	return s.appendConsoleSpecRevision(ctx, principalID, specID, expected, revision, now, false, true)
}

func (s *SQLite) appendConsoleSpecRevision(ctx context.Context, principalID, specID string, expected int, revision model.ConsoleSpecRevision, now time.Time, openNew, agentSelfOnly bool) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var current, archived, editingClosed int
	var state string
	var editor sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT current_revision,state,current_editor_id,archived,editing_closed FROM console_spec WHERE id=?`, specID).Scan(&current, &state, &editor, &archived, &editingClosed)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrConsoleSpecNotFound
	}
	if err != nil {
		return 0, err
	}
	// Agent self-ownership is checked against immutable revision 1 inside this
	// SAME transaction, before revision insertion or optimistic update. Current
	// owner/editor/latest revision are not proof that this Agent created it.
	if agentSelfOnly {
		if revision.CreatedByKind != "agent" || revision.CreatedBy == "" {
			return 0, ErrConsoleSpecAgentOwnership
		}
		var creator, kind string
		err = tx.QueryRowContext(ctx, `SELECT created_by,created_by_kind FROM console_spec_revision WHERE spec_id=? AND revision=1`, specID).Scan(&creator, &kind)
		if err != nil {
			return 0, err
		}
		if kind != "agent" || creator != revision.CreatedBy {
			return 0, ErrConsoleSpecAgentOwnership
		}
	}
	// Final is a distinct, auditable reason. Do not fold it into the compound
	// editor/archived/closed predicate: final rows also have no current editor,
	// which previously let removal of the final guard stay observationally green.
	// The test bypasses the table CHECK only to isolate this decision; production still enforces final => current_editor_id IS NULL as a second guard.
	if state == "final" {
		return 0, ErrConsoleSpecFinal
	}
	if !editor.Valid || editor.String != principalID || archived != 0 || editingClosed != 0 {
		return 0, ErrConsoleSpecEditDenied
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
	if revision.SpecID != specID || revision.Revision != next {
		return 0, errors.New("invalid console spec revision")
	}
	revision.Supersedes = current
	if err = insertConsoleSpecRevision(ctx, tx, revision, now); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, appendConsoleSpecRevisionUpdateSQL,
		next, now.UTC().Format(time.RFC3339Nano), specID, principalID, expected)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return 0, ErrConsoleSpecEditDenied
	}
	action := "revise"
	if agentSelfOnly {
		action = "agent_update"
	}
	if err = appendConsoleSpecEvent(ctx, tx, specID, next, revision.CreatedBy, action, fmt.Sprintf("supersedes=%d", current), now); err != nil {
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

// SetConsoleSpecsArchived checks the entire batch and writes its archive events
// in one transaction. No row is changed when any ID/permission is invalid.
func (s *SQLite) SetConsoleSpecsArchived(ctx context.Context, ids []string, actor string, mayDeleteOthers, archived bool, now time.Time) error {
	if len(ids) == 0 || len(ids) > 100 || actor == "" {
		return ErrConsoleSpecPermission
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	type row struct {
		id            string
		revision, old int
	}
	items := make([]row, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) != id || id == "" || seen[id] {
			return ErrConsoleSpecNotFound
		}
		seen[id] = true
		item := row{id: id}
		var owner string
		err = tx.QueryRowContext(ctx, `SELECT principal_id,current_revision,archived FROM console_spec WHERE id=?`, id).Scan(&owner, &item.revision, &item.old)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConsoleSpecNotFound
		}
		if err != nil {
			return err
		}
		if owner != actor && !mayDeleteOthers {
			return ErrConsoleSpecPermission
		}
		items = append(items, item)
	}
	value, action := 0, "unarchive"
	if archived {
		value, action = 1, "archive"
	}
	for _, item := range items {
		if item.old == value {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET archived=?,updated_at=? WHERE id=?`, value, now.UTC().Format(time.RFC3339Nano), item.id); err != nil {
			return err
		}
		if err = appendConsoleSpecEvent(ctx, tx, item.id, item.revision, actor, action, "", now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) ArchiveConsoleSpec(ctx context.Context, specID, actor string, now time.Time) error {
	return s.SetConsoleSpecsArchived(ctx, []string{specID}, actor, false, true, now)
}

func scanProposal(row rowScanner) (*model.ConsoleTaskProposal, error) {
	var p model.ConsoleTaskProposal
	var developerID, developerName, reviewerID, reviewerName, definitionOfDone sql.NullString
	var reviewed, created string
	var started, planned, blocker, resources sql.NullString
	err := row.Scan(&p.ID, &p.SchemaVersion, &p.SpecID, &p.Revision, &p.ContentSHA256, &p.Title, &p.TaskContent, &p.Status, &p.EstimatedHours, &p.DependsOnJSON, &p.AcceptanceJSON, &p.InputAnchorsJSON,
		&definitionOfDone, &developerID, &developerName, &reviewerID, &reviewerName, &p.ReviewID, &p.ReviewedBy, &reviewed, &created, &started, &planned, &blocker, &resources)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.DeveloperAgentID, p.DeveloperAgent = developerID.String, developerName.String
	p.ReviewerAgentID, p.ReviewerAgent = reviewerID.String, reviewerName.String
	p.DefinitionOfDone = definitionOfDone.String
	p.ReviewedAt, err = specTime(reviewed)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, err = specTime(created)
	if err != nil {
		return nil, err
	}
	if started.Valid {
		t, e := specTime(started.String)
		if e != nil {
			return nil, e
		}
		p.StartedAt = &t
	}
	if planned.Valid {
		t, e := specTime(planned.String)
		if e != nil {
			return nil, e
		}
		p.PlannedFinishAt = &t
	}
	if blocker.Valid {
		v := blocker.String
		p.Blocker = &v
	}
	if resources.Valid {
		v := resources.String
		p.ResourceCoordination = &v
	}
	return &p, err
}

const proposalColumns = `p.id,p.schema_version,p.spec_id,p.revision,p.content_sha256,p.title,p.task_content,p.status,p.estimated_hours,p.depends_on_json,'[]',p.input_anchors_json,'',NULL,NULL,NULL,NULL,p.review_id,p.reviewed_by,p.reviewed_at,p.created_at,NULL,NULL,NULL,NULL`
const taskUnion = `SELECT ` + proposalColumns + ` FROM console_task_proposal p UNION ALL
	SELECT t.id,1,t.spec_id,t.revision,t.content_sha256,t.title,t.task_content,t.status,t.estimated_hours,t.depends_on_json,
		COALESCE((SELECT json_group_array(json_object('id',a.acceptance_id,'text',COALESCE(json_extract(j.value,'$.text'),''))) FROM console_spec_task_acceptance a JOIN console_spec_revision rr ON rr.spec_id=t.spec_id AND rr.revision=t.revision JOIN json_each(rr.acceptance_json) j ON json_extract(j.value,'$.id')=a.acceptance_id WHERE a.task_id=t.id),'[]'),
		t.input_anchors_json,t.definition_of_done,
		t.developer_agent_id,(SELECT session_name FROM console_agent WHERE id=t.developer_agent_id),t.reviewer_agent_id,(SELECT session_name FROM console_agent WHERE id=t.reviewer_agent_id),
		'manual:'||t.id,t.created_by,t.created_at,t.created_at,t.started_at,t.planned_finish_at,t.blocker,t.resource_coordination FROM console_spec_task t`

func nullableTaskTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC().Format(time.RFC3339Nano)
}
func nullableTaskText(v *string) any {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	return s
}

func (s *SQLite) UpdateConsoleSpecTaskMetadata(ctx context.Context, taskID, principalID string, startedAt, plannedFinishAt *time.Time, blocker, resourceCoordination *string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec_task SET started_at=?,planned_finish_at=?,blocker=?,resource_coordination=?
		WHERE id=? AND EXISTS(SELECT 1 FROM console_spec s WHERE s.id=console_spec_task.spec_id AND s.principal_id=? AND s.state='final' AND s.task_state='splitting')`,
		nullableTaskTime(startedAt), nullableTaskTime(plannedFinishAt), nullableTaskText(blocker), nullableTaskText(resourceCoordination), taskID, principalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return nil
}

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
	var title, contentHash, anchors string
	if err = tx.QueryRowContext(ctx, `SELECT s.title,r.content_sha256,r.input_anchors_json
		FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=s.current_revision
		WHERE s.id=? AND s.principal_id=? AND s.state='final' AND s.task_state='splitting'`, proposal.SpecID, principalID).Scan(&title, &contentHash, &anchors); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConsoleSpecPermission
		}
		return err
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
	acceptanceJSON, definitionOfDone := createTaskDefinitionStorage(proposal.DefinitionOfDone)
	_, err = tx.ExecContext(ctx, `INSERT INTO console_spec_task(id,spec_id,revision,content_sha256,title,task_content,status,estimated_hours,depends_on_json,acceptance_json,input_anchors_json,definition_of_done,created_by,created_at)
		SELECT ?,s.id,s.current_revision,?,?,?,?,?,?,?,?,?,?,? FROM console_spec s WHERE s.id=?`, proposal.ID, contentHash,
		proposal.Title, proposal.TaskContent, proposal.Status, proposal.EstimatedHours, proposal.DependsOnJSON, acceptanceJSON, anchors, definitionOfDone,
		proposal.ReviewedBy, stamp, proposal.SpecID)
	if err != nil {
		return err
	}
	_ = title
	return tx.Commit()
}

func (s *SQLite) UpdateConsoleTaskProposalPlan(ctx context.Context, proposalID, principalID string, management bool, content, status string, hours float64, dependsOn []string, definitionOfDone string) error {
	return s.UpdateConsoleTaskProposalPlanAuthorized(ctx, proposalID, principalID, management, content, status, hours, dependsOn, definitionOfDone, nil)
}
func (s *SQLite) UpdateConsoleTaskProposalPlanAuthorized(ctx context.Context, proposalID, principalID string, management bool, content, status string, hours float64, dependsOn []string, definitionOfDone string, recheck func() error) error {
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
	if err = tx.QueryRowContext(ctx, `SELECT q.spec_id FROM (`+taskUnion+`) q
		JOIN console_spec s ON s.id=q.spec_id JOIN console_spec_revision r ON r.spec_id=q.spec_id AND r.revision=q.revision
		WHERE q.id=? AND s.principal_id=? AND s.state='final' AND s.task_state='splitting'`, proposalID, principalID).Scan(&specID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConsoleSpecPermission
		}
		return err
	}
	// This legacy API updates either a per-spec-revision proposal row or a real
	// task row. DoD is task-level data, so K17 covers only console_spec_task;
	// accepting it on console_task_proposal would recreate the semantic mixing
	// that put DoD in acceptance_json in the first place.
	var proposalRow int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM console_task_proposal WHERE id=?`, proposalID).Scan(&proposalRow); err != nil {
		return err
	}
	if proposalRow != 0 && strings.TrimSpace(definitionOfDone) != "" && !allowProposalTaskDefinition && !skipProposalDefinitionCheck {
		return ErrConsoleTaskDefinition
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
	var flowGroup, currentState string
	flowErr := tx.QueryRowContext(ctx, `SELECT group_code,status FROM console_spec_task WHERE id=?`, proposalID).Scan(&flowGroup, &currentState)
	if flowErr != nil && !errors.Is(flowErr, sql.ErrNoRows) {
		return flowErr
	}
	if flowGroup != "" {
		if status == "proposed" {
			return ErrTaskFlowConflict
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM console_spec_task_case_result WHERE task_id=? AND EXISTS(SELECT 1 FROM console_spec_task WHERE id=? AND (task_content<>? OR definition_of_done<>?))`, proposalID, proposalID, content, definitionOfDone); err != nil {
			return err
		}
		if status == "in_progress" || status == "done" {
			if err = taskBlockersClear(ctx, tx, proposalID); err != nil {
				return err
			}
			rawDeps, _ := json.Marshal(dependsOn)
			if err = taskDependenciesDone(ctx, tx, string(rawDeps)); err != nil {
				return err
			}
		}
		if status == "done" {
			p, n, e := taskCaseProgress(ctx, tx, proposalID, false)
			if e != nil {
				return e
			}
			if n == 0 || p != n {
				return ErrConsoleTaskNotGreen
			}
		}
		if currentState == "done" && status != "done" && status != "planned" {
			return ErrTaskFlowConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=version+1 WHERE id=?`, proposalID); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(dependsOn)
	acceptanceJSON, storedDefinition := updateTaskDefinitionStorage(definitionOfDone)
	query := `UPDATE console_task_proposal SET task_content=?,status=?,estimated_hours=?,depends_on_json=? WHERE id=?`
	args := []any{content, status, hours, string(raw), proposalID}
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		query = `UPDATE console_spec_task SET task_content=?,status=?,estimated_hours=?,depends_on_json=?,acceptance_json=?,definition_of_done=? WHERE id=?`
		args = []any{content, status, hours, string(raw), acceptanceJSON, storedDefinition, proposalID}
		res, err = tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		if n != 1 {
			return sql.ErrNoRows
		}
	}
	if flowGroup != "" {
		var version int
		if err = tx.QueryRowContext(ctx, `SELECT version FROM console_spec_task WHERE id=?`, proposalID).Scan(&version); err != nil {
			return err
		}
		if err = flowAudit(ctx, tx, proposalID, principalID, "owner_plan", version, time.Now()); err != nil {
			return err
		}
		if recheck == nil {
			return ErrTaskFlowPermission
		}
		if err = recheck(); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) UpdateConsoleSpecTaskAssignees(ctx context.Context, taskID, principalID, developerAgentID, reviewerAgentID string) error {
	developerAgentID = strings.TrimSpace(developerAgentID)
	reviewerAgentID = strings.TrimSpace(reviewerAgentID)
	if (developerAgentID == "") != (reviewerAgentID == "") {
		return ErrConsoleTaskAssigneePair
	}
	if developerAgentID != "" && developerAgentID == reviewerAgentID {
		return ErrConsoleTaskAssigneeSame
	}
	var flowGroup string
	if e := s.db.QueryRowContext(ctx, `SELECT group_code FROM console_spec_task WHERE id=?`, taskID).Scan(&flowGroup); e == nil && flowGroup != "" {
		return ErrTaskFlowConflict
	}
	res, err := s.db.ExecContext(ctx, `UPDATE console_spec_task
		SET developer_agent_id=NULLIF(?,''),reviewer_agent_id=NULLIF(?,'')
		WHERE id=? AND EXISTS(SELECT 1 FROM console_spec s WHERE s.id=console_spec_task.spec_id AND s.principal_id=? AND s.state='final' AND s.task_state='splitting')`,
		developerAgentID, reviewerAgentID, taskID, principalID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecPermission
	}
	return nil
}

// acceptance_id cannot have a database foreign key because spec acceptance
// criteria live inside console_spec_revision.acceptance_json. Validate every
// association against the task's frozen spec revision before writing it.
func (s *SQLite) SetConsoleSpecTaskAcceptances(ctx context.Context, taskID, principalID string, acceptanceIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var specRaw string
	if err = tx.QueryRowContext(ctx, `SELECT r.acceptance_json FROM console_spec_task t
		JOIN console_spec s ON s.id=t.spec_id JOIN console_spec_revision r ON r.spec_id=t.spec_id AND r.revision=t.revision
		WHERE t.id=? AND s.principal_id=? AND s.state='final' AND s.task_state='splitting'`, taskID, principalID).Scan(&specRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConsoleSpecPermission
		}
		return err
	}
	specIDs, err := taskAcceptanceIDs(specRaw)
	if err != nil {
		return err
	}
	allowed := make(map[string]bool, len(specIDs))
	for _, id := range specIDs {
		allowed[id] = true
	}
	seen := make(map[string]bool, len(acceptanceIDs))
	for _, rawID := range acceptanceIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] || (!skipTaskAcceptanceValidation && !skipTaskAcceptanceMembership && !allowed[id]) {
			return ErrConsoleTaskAcceptance
		}
		seen[id] = true
	}
	if skipTaskAcceptanceWrite {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM console_spec_task_acceptance WHERE task_id=?`, taskID); err != nil {
		return err
	}
	for _, rawID := range acceptanceIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec_task_acceptance(task_id,acceptance_id) VALUES(?,?)`, taskID, strings.TrimSpace(rawID)); err != nil {
			return err
		}
	}
	var flowGroup, status string
	if err = tx.QueryRowContext(ctx, `SELECT group_code,status FROM console_spec_task WHERE id=?`, taskID).Scan(&flowGroup, &status); err != nil {
		return err
	}
	if flowGroup != "" {
		if status == "done" {
			return ErrTaskFlowConflict
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM console_spec_task_case_result WHERE task_id=?`, taskID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_task SET version=version+1 WHERE id=?`, taskID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) ListConsoleSpecTaskAcceptanceIDs(ctx context.Context, taskID string) ([]string, error) {
	if hideTaskAcceptanceRead {
		return []string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT acceptance_id FROM console_spec_task_acceptance WHERE task_id=? ORDER BY acceptance_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *SQLite) ListUnlinkedConsoleSpecAcceptanceIDs(ctx context.Context, specID string, revision int) ([]string, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT acceptance_json FROM console_spec_revision WHERE spec_id=? AND revision=?`, specID, revision).Scan(&raw); err != nil {
		return nil, err
	}
	ids, err := taskAcceptanceIDs(raw)
	if err != nil {
		return nil, err
	}
	if treatEveryAcceptanceAsLinked {
		return []string{}, nil
	}
	if skipLinkedAcceptanceLookup {
		return ids, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT a.acceptance_id FROM console_spec_task_acceptance a
		JOIN console_spec_task t ON t.id=a.task_id WHERE t.spec_id=? AND t.revision=?`, specID, revision)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	missing := make([]string, 0)
	for _, id := range ids {
		if !linked[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

func (s *SQLite) DeleteConsoleSpecTask(ctx context.Context, taskID, principalID string) error {
	if skipConsoleSpecTaskDelete {
		return nil
	}
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
