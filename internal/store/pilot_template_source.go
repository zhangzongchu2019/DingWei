package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func templateRequest(ctx context.Context, tx *sql.Tx, actor, request, payload string, out any) (bool, error) {
	var hash, raw string
	err := tx.QueryRowContext(ctx, `SELECT payload_sha,result_json FROM pilot_template_request WHERE actor_id=? AND request_id=?`, actor, request).Scan(&hash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if hash != payload {
		return false, ErrTaskFlowConflict
	}
	return true, json.Unmarshal([]byte(raw), out)
}
func saveTemplateRequest(ctx context.Context, tx *sql.Tx, actor, request, payload string, out any, now time.Time) error {
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO pilot_template_request(actor_id,request_id,payload_sha,result_json,created_at) VALUES(?,?,?,?,?)`, actor, request, payload, string(raw), templateNow(now))
	return err
}
func scanTemplateSource(row rowScanner) (TemplateSource, error) {
	var s TemplateSource
	err := row.Scan(&s.ID, &s.Owner, &s.URL, &s.DocumentKind, &s.DocumentID, &s.Revision, &s.Kind, &s.State, &s.Content, &s.SHA, &s.CreatedAt)
	return s, err
}

const templateSourceCols = `id,owner_principal,source_url,document_kind,document_id,source_revision,source_kind,state,content_markdown,content_sha256,created_at`

func (s *SQLite) TemplateSource(ctx context.Context, owner, id string) (TemplateSource, error) {
	v, err := scanTemplateSource(s.db.QueryRowContext(ctx, `SELECT `+templateSourceCols+` FROM pilot_spec_source WHERE id=? AND owner_principal=?`, id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrTaskFlowPermission
	}
	return v, err
}

// Source kind is selected by the server: a manual export never pretends that a
// connector has independently read or authenticated a provider revision.
func (s *SQLite) SaveTemplateSource(ctx context.Context, owner, request string, input TemplateSource, check func() error, now time.Time) (TemplateSource, error) {
	var out TemplateSource
	if owner == "" || !questionNonempty(request, 128) || check == nil || len(input.Content) > 512<<10 || len(input.Revision) > 256 {
		return out, ErrTaskFlowInvalid
	}
	canonical, kind, id, err := ParseTemplateSourceURL(input.URL)
	if err != nil {
		return out, err
	}
	input.URL = canonical
	input.DocumentKind = kind
	input.DocumentID = id
	input.Owner = owner
	if input.Kind == "pending_connector" {
		if input.Content != "" {
			return out, ErrTaskFlowInvalid
		}
		input.State = "pending_config"
		input.SHA = ""
	} else if input.Kind == "manual_export" || input.Kind == "trusted_connector" {
		if strings.TrimSpace(input.Content) == "" || templateUnknown(input.Revision) {
			return out, ErrTaskFlowInvalid
		}
		input.State = "ready"
		input.SHA = templateContentSHA(input.Content)
	} else {
		return out, ErrTaskFlowInvalid
	}
	payload := templateDigest([]any{"source", owner, input.URL, input.Kind, input.Revision, input.SHA})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	replay, err := templateRequest(ctx, tx, "user:"+owner, request, payload, &out)
	if err != nil {
		return out, err
	}
	if replay {
		return out, check()
	}
	if input.State == "ready" {
		out, err = scanTemplateSource(tx.QueryRowContext(ctx, `SELECT `+templateSourceCols+` FROM pilot_spec_source WHERE owner_principal=? AND source_url=? AND source_revision=? AND content_sha256=? AND source_kind=?`, owner, input.URL, input.Revision, input.SHA, input.Kind))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
	}
	if out.ID == "" {
		out = input
		out.ID = "tps:" + templateDigest([]string{owner, request})
		out.CreatedAt = templateNow(now)
		if _, err = tx.ExecContext(ctx, `INSERT INTO pilot_spec_source(id,owner_principal,request_id,request_sha,source_url,document_kind,document_id,source_revision,source_kind,state,content_markdown,content_sha256,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, out.ID, owner, request, payload, out.URL, out.DocumentKind, out.DocumentID, out.Revision, out.Kind, out.State, out.Content, out.SHA, out.CreatedAt); err != nil {
			return out, err
		}
	}
	if err = templateAudit(ctx, tx, "", "", "user:"+owner, "source_snapshot", map[string]any{"source_id": out.ID, "source_kind": out.Kind, "content_sha256": out.SHA}, now); err != nil {
		return out, err
	}
	if err = saveTemplateRequest(ctx, tx, "user:"+owner, request, payload, out, now); err != nil {
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
func (s *SQLite) TemplateSources(ctx context.Context, owner, after string) ([]TemplateSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+templateSourceCols+` FROM pilot_spec_source WHERE owner_principal=? AND id>? ORDER BY id LIMIT 100`, owner, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TemplateSource{}
	for rows.Next() {
		v, err := scanTemplateSource(rows)
		if err != nil {
			return nil, err
		}
		v.Content = ""
		out = append(out, v)
	}
	return out, rows.Err()
}
