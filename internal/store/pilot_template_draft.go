package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

func templateReferences(ctx context.Context, q taskQuery, id string) (TemplateReferences, error) {
	var r TemplateReferences
	var raw string
	err := q.QueryRowContext(ctx, `SELECT d.id,d.owner_principal,d.current_version,d.state,r.document_json FROM pilot_template_draft d JOIN pilot_template_revision r ON r.draft_id=d.id AND r.version=d.current_version WHERE d.id=?`, id).Scan(&r.ID, &r.Owner, &r.Version, &r.State, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrTaskFlowPermission
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(raw), &r.Document)
	return r, err
}
func (s *SQLite) TemplateReferences(ctx context.Context, id string) (TemplateReferences, error) {
	return templateReferences(ctx, s.db, id)
}
func templateActorAllowed(a BlockerActor, r TemplateReferences) bool {
	if a.UserID != "" && a.UserID == r.Owner {
		return true
	}
	for _, role := range templateRoles(r.Document) {
		if role.Principal == a.Key() {
			return true
		}
	}
	return false
}
func templateCrossReferences(ctx context.Context, q taskQuery, owner string, d TemplateDocument) []TemplateMissing {
	missing := []TemplateMissing{}
	add := func(p, r string) { missing = append(missing, TemplateMissing{p, r}) }
	if d.Parent != nil {
		var actualOwner, sha, parentState string
		var current, archived int
		var raw string
		err := q.QueryRowContext(ctx, `SELECT s.principal_id,s.current_revision,s.archived,s.state,r.content_sha256,t.document_json FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=s.current_revision JOIN pilot_template_binding b ON b.spec_id=s.id AND b.revision=s.current_revision JOIN pilot_template_revision t ON t.draft_id=b.draft_id AND t.version=b.draft_version WHERE s.id=?`, d.Parent.SpecID).Scan(&actualOwner, &current, &archived, &parentState, &sha, &raw)
		var parent TemplateDocument
		if err != nil || json.Unmarshal([]byte(raw), &parent) != nil || actualOwner != owner || archived != 0 || parentState != "final" || current != d.Parent.Revision || sha != d.Parent.SHA || parent.Kind != "parent" {
			add("parent", "owned_current_template_parent_revision_sha_required")
		} else if parent.RequestedMode == "full" && d.RequestedMode != "full" {
			add("requested_mode", "full_parent_cannot_be_downgraded_by_child")
		}
	}
	target := d.TargetSpecID
	if target == "" {
		target = d.PredecessorSpecID
	}
	if target != "" {
		var actualOwner, editor, state string
		var current, archived int
		err := q.QueryRowContext(ctx, `SELECT principal_id,COALESCE(current_editor_id,''),state,current_revision,archived FROM console_spec WHERE id=?`, target).Scan(&actualOwner, &editor, &state, &current, &archived)
		if err != nil || actualOwner != owner || archived != 0 || current != d.ExpectedRevision {
			add("target_spec_id", "owned_current_revision_required")
		} else if d.PredecessorSpecID != "" && state != "final" {
			add("predecessor_spec_id", "final_predecessor_required")
		} else if d.TargetSpecID != "" && (state == "final" || editor != owner) {
			add("target_spec_id", "current_editor_required_final_must_use_successor")
		}
		var oldRaw string
		err = q.QueryRowContext(ctx, `SELECT r.document_json FROM pilot_template_binding b JOIN pilot_template_revision r ON r.draft_id=b.draft_id AND r.version=b.draft_version WHERE b.spec_id=? ORDER BY b.revision DESC LIMIT 1`, target).Scan(&oldRaw)
		if err == nil {
			var old TemplateDocument
			if json.Unmarshal([]byte(oldRaw), &old) == nil && old.RequestedMode == "full" && d.RequestedMode == "light" && (templateUnknown(d.DowngradeReason) || templateUnknown(d.RiskClearanceReference)) {
				add("downgrade_reason", "owner_new_revision_risk_clearance_required")
			}
		}
	}
	return missing
}
func templateLoadView(ctx context.Context, q taskQuery, id string, a BlockerActor, snapshot *TemplatePrincipalSnapshot, now time.Time, historyContent bool) (TemplateView, error) {
	var out TemplateView
	ref, err := templateReferences(ctx, q, id)
	if err != nil {
		return out, err
	}
	if !templateActorAllowed(a, ref) {
		return out, ErrTaskFlowPermission
	}
	out.ID = id
	out.Owner = ref.Owner
	out.Version = ref.Version
	out.State = ref.State
	out.Document = ref.Document
	if err = q.QueryRowContext(ctx, `SELECT plan_sha256,content_markdown,content_sha256,mode FROM pilot_template_revision WHERE draft_id=? AND version=?`, id, ref.Version).Scan(&out.PlanSHA, &out.ContentMarkdown, &out.ContentSHA, &out.Mode); err != nil {
		return out, err
	}
	if err = q.QueryRowContext(ctx, `SELECT confirmed_spec_id,confirmed_revision FROM pilot_template_draft WHERE id=?`, id).Scan(&out.SpecID, &out.SpecRevision); err != nil {
		return out, err
	}
	out.Source, err = scanTemplateSource(q.QueryRowContext(ctx, `SELECT `+templateSourceCols+` FROM pilot_spec_source WHERE id=? AND owner_principal=?`, ref.Document.SourceID, ref.Owner))
	if err != nil {
		return out, err
	}
	out.Missing = templateValidate(out.Document, out.Source, now)
	crossDoc := out.Document
	if ref.State == "confirmed" {
		crossDoc.TargetSpecID = ""
		crossDoc.PredecessorSpecID = ""
		crossDoc.ExpectedRevision = 0
	}
	out.Missing = append(out.Missing, templateCrossReferences(ctx, q, ref.Owner, crossDoc)...)
	out.Roles = templateRoles(out.Document)
	for i := range out.Roles {
		role := &out.Roles[i]
		var decision, at string
		err = q.QueryRowContext(ctx, `SELECT decision,created_at FROM pilot_template_receipt WHERE draft_id=? AND draft_version=? AND entry_id=? AND role=? AND principal_id=? ORDER BY id DESC LIMIT 1`, id, ref.Version, role.EntryID, role.Role, role.Principal).Scan(&decision, &at)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		role.Accepted = decision == "accepted"
		if role.Accepted {
			role.AcceptedAt = at
		} else {
			out.Missing = append(out.Missing, TemplateMissing{fmt.Sprintf("roles.%s.%s", role.EntryID, role.Role), "actual_principal_acceptance_required"})
		}
		if snapshot != nil && ref.State == "pending" && !snapshot.Active[role.Principal] {
			out.Missing = append(out.Missing, TemplateMissing{fmt.Sprintf("roles.%s.%s", role.EntryID, role.Role), "currently_active_platform_principal_required"})
		}
	}
	if a.UserID == ref.Owner && out.SpecID != "" {
		impact := &TemplateImpact{Successor: out.SpecID, TaskIDs: []string{}, Limit: 100}
		e := q.QueryRowContext(ctx, `SELECT i.predecessor_spec_id,i.predecessor_revision FROM pilot_template_impact i JOIN console_spec p ON p.id=i.predecessor_spec_id WHERE i.successor_spec_id=? AND i.confirmed_by=? AND p.principal_id=? ORDER BY i.created_at LIMIT 1`, out.SpecID, ref.Owner, ref.Owner).Scan(&impact.Predecessor, &impact.Revision)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		if e == nil {
			if e = q.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_task WHERE spec_id=? AND revision=?`, impact.Predecessor, impact.Revision).Scan(&impact.TaskCount); e != nil {
				return out, e
			}
			rows, e := q.QueryContext(ctx, `SELECT id FROM console_spec_task WHERE spec_id=? AND revision=? ORDER BY id LIMIT 100`, impact.Predecessor, impact.Revision)
			if e != nil {
				return out, e
			}
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					rows.Close()
					return out, e
				}
				impact.TaskIDs = append(impact.TaskIDs, id)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return out, e
			}
			out.Impact = impact
		}
	}
	out.CanConfirm = a.UserID == ref.Owner && ref.State == "pending" && len(out.Missing) == 0
	if a.UserID != ref.Owner || !historyContent {
		out.Source.Content = ""
		out.ContentMarkdown = ""
	}
	return out, nil
}
func (s *SQLite) ReadTemplateDraft(ctx context.Context, expected TemplateReferences, a BlockerActor, snapshot TemplatePrincipalSnapshot, check func() error, now time.Time) (TemplateView, error) {
	var out TemplateView
	if check == nil {
		return out, ErrTaskFlowPermission
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	r, err := templateReferences(ctx, tx, expected.ID)
	if err != nil {
		return out, err
	}
	if !reflect.DeepEqual(r, expected) {
		return out, ErrTaskFlowConflict
	}
	out, err = templateLoadView(ctx, tx, r.ID, a, &snapshot, now, true)
	if err != nil {
		return out, err
	}
	if err = check(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
func (s *SQLite) SaveTemplateDraft(ctx context.Context, owner, id string, u TemplateOperation, snapshot TemplatePrincipalSnapshot, check func() error, now time.Time) (TemplateView, error) {
	var out TemplateView
	if owner == "" || check == nil || !questionNonempty(u.RequestID, 128) || u.Document == nil {
		return out, ErrTaskFlowInvalid
	}
	d := *u.Document
	if d.TemplateVersion != "" && d.TemplateVersion != PilotTemplateVersion {
		return out, ErrTaskFlowInvalid
	}
	d = templateExpand(d)
	raw, _ := json.Marshal(d)
	if len(raw) > 512<<10 {
		return out, ErrTaskFlowInvalid
	}
	payload := templateDigest([]any{"draft", id, u})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	// A replay is still scoped to current owner and rights before cached bytes.
	if id != "" {
		r, e := templateReferences(ctx, tx, id)
		if e != nil || r.Owner != owner {
			return out, ErrTaskFlowPermission
		}
	}
	replay, err := templateRequest(ctx, tx, "user:"+owner, u.RequestID, payload, &out)
	if err != nil {
		return out, err
	}
	if replay {
		return out, check()
	}
	source, err := scanTemplateSource(tx.QueryRowContext(ctx, `SELECT `+templateSourceCols+` FROM pilot_spec_source WHERE id=? AND owner_principal=?`, d.SourceID, owner))
	if err != nil {
		return out, ErrTaskFlowPermission
	}
	version := 1
	if id == "" {
		if u.ExpectedVersion != 0 {
			return out, ErrTaskFlowConflict
		}
		id = "tpd:" + templateDigest([]string{owner, u.RequestID})
		if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_template_draft(id,owner_principal,current_version,state,created_at,updated_at) VALUES(?,?,1,'pending',?,?)`, id, owner, templateNow(now), templateNow(now)); err != nil {
			return out, err
		}
	} else {
		r, e := templateReferences(ctx, tx, id)
		if e != nil {
			return out, e
		}
		if r.Owner != owner || r.State != "pending" || r.Version != u.ExpectedVersion {
			return out, ErrTaskFlowConflict
		}
		version = r.Version + 1
		if _, err = tx.ExecContext(ctx, `UPDATE pilot_template_draft SET current_version=?,updated_at=? WHERE id=? AND current_version=? AND state='pending'`, version, templateNow(now), id, u.ExpectedVersion); err != nil {
			return out, err
		}
	}
	body, contentSHA := templateRenderedContent(d, source)
	if len(body) > 1536<<10 {
		return out, ErrTaskFlowInvalid
	}
	planSHA := templateDigest(d)
	acceptance := templateAcceptance(d, owner, now)
	if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_template_revision(draft_id,version,source_id,document_json,plan_sha256,content_markdown,acceptance_json,content_sha256,mode,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, version, d.SourceID, string(raw), planSHA, body, acceptance, contentSHA, d.RequestedMode, templateNow(now)); err != nil {
		return out, err
	}
	for _, role := range templateRoles(d) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_template_role(draft_id,draft_version,entry_id,role,principal_id) VALUES(?,?,?,?,?)`, id, version, role.EntryID, role.Role, role.Principal); err != nil {
			return out, err
		}
	}
	out, err = templateLoadView(ctx, tx, id, BlockerActor{UserID: owner}, &snapshot, now, true)
	if err != nil {
		return out, err
	}
	if err = templateAudit(ctx, tx, id, "", "user:"+owner, "draft_revision", map[string]any{"version": version, "plan_sha256": planSHA, "missing": out.Missing}, now); err != nil {
		return out, err
	}
	if err = saveTemplateRequest(ctx, tx, "user:"+owner, u.RequestID, payload, out, now); err != nil {
		return out, err
	}
	if err = check(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
func templateAcceptance(d TemplateDocument, actor string, now time.Time) string {
	criteria := []map[string]any{}
	for _, e := range d.Entries {
		criteria = append(criteria, map[string]any{"id": e.ID, "text": e.Fields.Acceptance, "owner_ref": e.OwnerRef, "source": "owner", "checked_by": actor, "checked_at": templateNow(now), "cases": []string{e.ID}})
	}
	raw, _ := json.Marshal(map[string]any{"criteria": criteria})
	return string(raw)
}
func (s *SQLite) TemplateCandidateIDs(ctx context.Context, a BlockerActor, after string) ([]string, error) {
	if a.Key() == "" {
		return nil, ErrTaskFlowPermission
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id FROM pilot_template_draft d WHERE d.id>? AND ((?<>'' AND d.owner_principal=?) OR EXISTS(SELECT 1 FROM pilot_template_role r WHERE r.draft_id=d.id AND r.draft_version=d.current_version AND r.principal_id=?)) ORDER BY d.id LIMIT 100`, after, a.UserID, a.UserID, a.Key())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Parents are owned, current, final template snapshots. The child stores the
// exact revision and SHA, and those are rechecked inside its confirm transaction.
func (s *SQLite) TemplateParents(ctx context.Context, owner, after string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.title,s.current_revision,r.content_sha256 FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=s.current_revision JOIN pilot_template_binding b ON b.spec_id=s.id AND b.revision=s.current_revision JOIN pilot_template_revision t ON t.draft_id=b.draft_id AND t.version=b.draft_version WHERE s.principal_id=? AND s.id>? AND s.state='final' AND s.archived=0 AND json_extract(t.document_json,'$.kind')='parent' ORDER BY s.id LIMIT 100`, owner, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, sha string
		var n int
		if err = rows.Scan(&id, &title, &n, &sha); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"spec_id": id, "title": title, "revision": n, "content_sha256": sha})
	}
	return out, rows.Err()
}
