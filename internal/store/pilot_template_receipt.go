package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"
)

// A receipt accepts a responsibility in this immutable plan. It never marks a
// test as executed, a task as done, a checklist as PASS or business delivery as
// signed off. Withdrawals append history and make the live contract incomplete.
func (s *SQLite) UpdateTemplateResponsibility(ctx context.Context, expected TemplateReferences, a BlockerActor, u TemplateOperation, snapshot TemplatePrincipalSnapshot, check func() error, now time.Time) (TemplateView, error) {
	var out TemplateView
	if check == nil || a.Key() == "" || !questionNonempty(u.RequestID, 128) || len(u.EntryIDs) == 0 || len(u.EntryIDs) > 100 || len(u.Reason) > 2048 || u.Decision != "accepted" && u.Decision != "withdrawn" {
		return out, ErrTaskFlowInvalid
	}
	if u.Decision == "withdrawn" && templateUnknown(u.Reason) {
		return out, ErrTaskFlowInvalid
	}
	if a.AgentID != "" && (u.Role == "signoff" || u.Role == "full_review") {
		return out, ErrTaskFlowPermission
	}
	if u.Role == "full_review" {
		if a.UserID == "" || !u.ConfirmHuman || templateUnknown(u.Reason) {
			return out, ErrTaskFlowPermission
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
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
	if !templateActorAllowed(a, r) || !snapshot.Active[a.Key()] || r.State == "cancelled" {
		return out, ErrTaskFlowPermission
	}
	payload := templateDigest([]any{"responsibility", r.ID, u})
	replay, err := templateRequest(ctx, tx, a.Key(), u.RequestID, payload, &out)
	if err != nil {
		return out, err
	}
	if replay {
		return out, check()
	}
	view, err := templateLoadView(ctx, tx, r.ID, a, &snapshot, now, false)
	if err != nil {
		return out, err
	}
	if u.ExpectedVersion != r.Version || u.PlanSHA != view.PlanSHA {
		return out, ErrTaskFlowConflict
	}
	// Consent may be collected before other people answer, but never against an
	// incomplete/unknown six-field contract or an unresolved source/parent.
	if u.Decision == "accepted" {
		for _, m := range view.Missing {
			if len(m.Path) < 6 || m.Path[:6] != "roles." {
				return out, ErrTaskFlowConflict
			}
		}
	}
	seen := map[string]bool{}
	for _, entry := range u.EntryIDs {
		if seen[entry] {
			return out, ErrTaskFlowInvalid
		}
		seen[entry] = true
		found := false
		for _, role := range view.Roles {
			if role.EntryID == entry && role.Role == u.Role && role.Principal == a.Key() {
				found = true
			}
		}
		if !found {
			return out, ErrTaskFlowPermission
		}
		var prior string
		e := tx.QueryRowContext(ctx, `SELECT decision FROM pilot_template_receipt WHERE draft_id=? AND draft_version=? AND entry_id=? AND role=? AND principal_id=? ORDER BY id DESC LIMIT 1`, r.ID, r.Version, entry, u.Role, a.Key()).Scan(&prior)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		if prior == u.Decision {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_template_receipt(draft_id,draft_version,entry_id,role,principal_id,decision,reason,created_at) VALUES(?,?,?,?,?,?,?,?)`, r.ID, r.Version, entry, u.Role, a.Key(), u.Decision, u.Reason, templateNow(now)); err != nil {
			return out, err
		}
	}
	out, err = templateLoadView(ctx, tx, r.ID, a, &snapshot, now, a.UserID == r.Owner)
	if err != nil {
		return out, err
	}
	if err = templateAudit(ctx, tx, r.ID, out.SpecID, a.Key(), "responsibility_"+u.Decision, map[string]any{"entries": u.EntryIDs, "role": u.Role, "plan_sha256": u.PlanSHA, "reason": u.Reason, "not_business_signoff": true}, now); err != nil {
		return out, err
	}
	if err = saveTemplateRequest(ctx, tx, a.Key(), u.RequestID, payload, out, now); err != nil {
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
