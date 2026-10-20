package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func templateModelRevision(id string, n int, body, acceptance, sha, actor string, source TemplateSource) model.ConsoleSpecRevision {
	anchors, _ := json.Marshal([]map[string]string{{"kind": "pilot_source_snapshot", "ref": source.ID, "sha256": source.SHA, "source_url": source.URL, "source_kind": source.Kind}})
	return model.ConsoleSpecRevision{SpecID: id, Revision: n, ContentMarkdown: body, AcceptanceJSON: acceptance, InputAnchorsJSON: string(anchors), ContentSHA256: sha, CreatedBy: actor, CreatedByKind: "principal", CreatedVia: "principal"}
}
func (s *SQLite) ConfirmTemplateDraft(ctx context.Context, expected TemplateReferences, owner string, u TemplateOperation, snapshot TemplatePrincipalSnapshot, check func() error, now time.Time) (TemplateView, error) {
	var out TemplateView
	if check == nil || owner == "" || !u.ConfirmHuman || !questionNonempty(u.RequestID, 128) {
		return out, ErrTaskFlowPermission
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
	if r.Owner != owner {
		return out, ErrTaskFlowPermission
	}
	if !reflect.DeepEqual(r, expected) {
		return out, ErrTaskFlowConflict
	}
	payload := templateDigest([]any{"confirm", r.ID, u})
	replay, err := templateRequest(ctx, tx, "user:"+owner, u.RequestID, payload, &out)
	if err != nil {
		return out, err
	}
	if replay {
		return out, check()
	}
	if r.State != "pending" || r.Version != u.ExpectedVersion {
		return out, ErrTaskFlowConflict
	}
	view, err := templateLoadView(ctx, tx, r.ID, BlockerActor{UserID: owner}, &snapshot, now, true)
	if err != nil {
		return out, err
	}
	if view.PlanSHA != u.PlanSHA || !view.CanConfirm {
		return out, ErrTaskFlowConflict
	}
	// Do not derive a share/edit secret from the public draft/plan identity.
	// Principal editing needs no share token; this random unreachable capability
	// leaves the legacy capability column unguessable without exposing a token.
	var entropy [32]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		return out, err
	}
	capDigest := sha256.Sum256(entropy[:])
	capHash := hex.EncodeToString(capDigest[:])
	acceptance := templateAcceptance(view.Document, owner, now)
	specID, revision := "tpspec:"+templateDigest([]any{r.ID, r.Version}), 1
	d := view.Document
	if d.TargetSpecID != "" {
		specID = d.TargetSpecID
		revision = d.ExpectedRevision + 1
		rev := templateModelRevision(specID, revision, view.ContentMarkdown, acceptance, view.ContentSHA, owner, view.Source)
		if _, err = appendConsoleSpecRevisionTx(ctx, tx, owner, specID, d.ExpectedRevision, rev, now, false, false); err != nil {
			return out, err
		}
	} else if d.PredecessorSpecID != "" {
		var old model.ConsoleSpecRevision
		err = tx.QueryRowContext(ctx, `SELECT content_markdown,acceptance_json,input_anchors_json,content_sha256 FROM console_spec_revision WHERE spec_id=? AND revision=?`, d.PredecessorSpecID, d.ExpectedRevision).Scan(&old.ContentMarkdown, &old.AcceptanceJSON, &old.InputAnchorsJSON, &old.ContentSHA256)
		if err != nil {
			return out, err
		}
		old.SpecID = specID
		old.Revision = 1
		old.CreatedBy = owner
		old.CreatedByKind = "principal"
		old.CreatedVia = "principal"
		spec := model.ConsoleSpec{ID: specID, PrincipalID: owner, Title: d.Title, EditCapabilityHash: capHash, DerivedFrom: d.PredecessorSpecID}
		if err = deriveConsoleSpecTx(ctx, tx, owner, d.PredecessorSpecID, spec, old, now); err != nil {
			return out, err
		}
		revision = 2
		rev := templateModelRevision(specID, revision, view.ContentMarkdown, acceptance, view.ContentSHA, owner, view.Source)
		if _, err = appendConsoleSpecRevisionTx(ctx, tx, owner, specID, 1, rev, now, false, false); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_template_impact(predecessor_spec_id,predecessor_revision,successor_spec_id,reason,confirmed_by,created_at) VALUES(?,?,?,?,?,?)`, d.PredecessorSpecID, d.ExpectedRevision, specID, d.ChangeReason, owner, templateNow(now)); err != nil {
			return out, err
		}
	} else {
		spec := model.ConsoleSpec{ID: specID, PrincipalID: owner, Title: d.Title, EditCapabilityHash: capHash}
		rev := templateModelRevision(specID, 1, view.ContentMarkdown, acceptance, view.ContentSHA, owner, view.Source)
		if err = createConsoleSpecTx(ctx, tx, spec, rev, now); err != nil {
			return out, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_template_enrollment(spec_id,owner_principal,created_at) VALUES(?,?,?) ON CONFLICT(spec_id) DO NOTHING`, specID, owner, templateNow(now)); err != nil {
		return out, err
	}
	family := 1
	if d.PredecessorSpecID != "" {
		var old int
		err = tx.QueryRowContext(ctx, `SELECT family_revision FROM pilot_template_binding WHERE spec_id=? AND revision=?`, d.PredecessorSpecID, d.ExpectedRevision).Scan(&old)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		family = old + 1
		if family < 2 {
			family = 2
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_template_binding(spec_id,revision,draft_id,draft_version,content_sha256,family_revision,created_at) VALUES(?,?,?,?,?,?,?)`, specID, revision, r.ID, r.Version, view.ContentSHA, family, templateNow(now)); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pilot_template_draft SET state='confirmed',confirmed_spec_id=?,confirmed_revision=?,updated_at=? WHERE id=? AND current_version=? AND state='pending'`, specID, revision, templateNow(now), r.ID, r.Version); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO console_spec_agent_fresh(spec_id,revision) VALUES(?,?)`, specID, revision); err != nil {
		return out, err
	}
	if err = templateAudit(ctx, tx, r.ID, specID, "user:"+owner, "human_confirmed_draft", map[string]any{"revision": revision, "plan_sha256": view.PlanSHA, "not_finalized": true, "not_execution_authorization": true, "predecessor": d.PredecessorSpecID}, now); err != nil {
		return out, err
	}
	out, err = templateLoadView(ctx, tx, r.ID, BlockerActor{UserID: owner}, &snapshot, now, true)
	if err != nil {
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
