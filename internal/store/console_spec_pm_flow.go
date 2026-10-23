package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

var (
	ErrPMSpecFlowConflict = errors.New("PM spec working draft conflict")
	ErrPMSpecFlowDenied   = errors.New("PM spec flow action denied")
	ErrPMSpecFlowStale    = errors.New("PM spec assist result is stale")
)

// denyLegacySpecAgentReview rejects legacy R8 mutations for specs enrolled in
// the PM flow. Keep this in the caller's transaction so an idempotent replay
// cannot bypass the flow boundary.
func denyLegacySpecAgentReview(ctx context.Context, tx *sql.Tx, specID string) error {
	table, err := pmSpecFlowTableExists(ctx, tx)
	if err != nil || !table {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_spec_flow WHERE spec_id=?`, specID).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return ErrPMSpecFlowDenied
	}
	return nil
}

// requireFinalizedPMSpecForSplit is opt-in: with the capability disabled,
// legacy presplit behavior is unchanged. When enabled, an enrolled PM-flow
// spec may enter splitting only through its finalized reviewed revision.
func requireFinalizedPMSpecForSplit(ctx context.Context, tx *sql.Tx, enabled bool, specID string, revision int) error {
	if !enabled {
		return nil
	}
	table, err := pmSpecFlowTableExists(ctx, tx)
	if err != nil || !table {
		return err
	}
	var state, frozenSHA string
	var frozenRevision int
	err = tx.QueryRowContext(ctx, `SELECT state,frozen_revision,frozen_sha256 FROM console_spec_flow WHERE spec_id=?`, specID).Scan(&state, &frozenRevision, &frozenSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var currentRevision int
	var currentSHA string
	if err = tx.QueryRowContext(ctx, `SELECT s.current_revision,r.content_sha256 FROM console_spec s JOIN console_spec_revision r ON r.spec_id=s.id AND r.revision=s.current_revision WHERE s.id=? AND s.archived=0`, specID).Scan(&currentRevision, &currentSHA); err != nil {
		return ErrPMSpecFlowDenied
	}
	if state != "final" || frozenRevision != revision || currentRevision != revision || frozenSHA == "" || currentSHA != frozenSHA {
		return ErrPMSpecFlowDenied
	}
	return nil
}

type PMSpecWorkingDraft struct {
	SpecID           string `json:"spec_id"`
	PrincipalID      string `json:"principal_id"`
	CurrentEditorID  string `json:"current_editor_id"`
	Title            string `json:"title"`
	ContentMarkdown  string `json:"content_markdown"`
	AcceptanceJSON   string `json:"acceptance_json"`
	InputAnchorsJSON string `json:"input_anchors_json"`
	SourceRefsJSON   string `json:"source_refs_json"`
	NormAnchorsJSON  string `json:"norm_anchors_json"`
	Version          int    `json:"version"`
	ContentSHA256    string `json:"content_sha256"`
	BaseRevision     int    `json:"base_revision"`
	State            string `json:"state"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type PMSpecDraftInput struct {
	SpecID           string `json:"spec_id"`
	Title            string `json:"title"`
	ContentMarkdown  string `json:"content_markdown"`
	AcceptanceJSON   string `json:"acceptance_json"`
	InputAnchorsJSON string `json:"input_anchors_json"`
	SourceRefsJSON   string `json:"source_refs_json"`
	NormAnchorsJSON  string `json:"norm_anchors_json"`
	ExpectedVersion  int    `json:"expected_version"`
	ExpectedSHA256   string `json:"expected_sha256"`
}

func pmDraftContentSHA(in PMSpecDraftInput) (string, error) {
	canonical := struct {
		Title            string `json:"title"`
		ContentMarkdown  string `json:"content_markdown"`
		AcceptanceJSON   string `json:"acceptance_json"`
		InputAnchorsJSON string `json:"input_anchors_json"`
		SourceRefsJSON   string `json:"source_refs_json"`
		NormAnchorsJSON  string `json:"norm_anchors_json"`
	}{in.Title, in.ContentMarkdown, in.AcceptanceJSON, in.InputAnchorsJSON, in.SourceRefsJSON, in.NormAnchorsJSON}
	b, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func normalizePMDraft(in *PMSpecDraftInput) error {
	if strings.TrimSpace(in.SpecID) == "" || len(in.SpecID) > 128 || len(in.Title) > 500 || len(in.ContentMarkdown) > 1<<20 {
		return errors.New("invalid PM spec draft")
	}
	if in.AcceptanceJSON == "" {
		in.AcceptanceJSON = `{"criteria":[]}`
	}
	if in.InputAnchorsJSON == "" {
		in.InputAnchorsJSON = `[]`
	}
	if in.SourceRefsJSON == "" {
		in.SourceRefsJSON = `[]`
	}
	if in.NormAnchorsJSON == "" {
		in.NormAnchorsJSON = `[]`
	}
	for _, raw := range []string{in.AcceptanceJSON, in.InputAnchorsJSON, in.SourceRefsJSON, in.NormAnchorsJSON} {
		if !json.Valid([]byte(raw)) {
			return errors.New("invalid PM spec structured field JSON")
		}
	}
	return nil
}

const pmDraftColumns = `spec_id,principal_id,current_editor_id,title,content_markdown,acceptance_json,input_anchors_json,source_refs_json,norm_anchors_json,version,content_sha256,base_revision,state,created_at,updated_at`

func scanPMSpecWorkingDraft(row interface{ Scan(...any) error }) (*PMSpecWorkingDraft, error) {
	d := new(PMSpecWorkingDraft)
	err := row.Scan(&d.SpecID, &d.PrincipalID, &d.CurrentEditorID, &d.Title, &d.ContentMarkdown, &d.AcceptanceJSON, &d.InputAnchorsJSON, &d.SourceRefsJSON, &d.NormAnchorsJSON, &d.Version, &d.ContentSHA256, &d.BaseRevision, &d.State, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (s *SQLite) GetPMSpecWorkingDraft(ctx context.Context, specID string) (*PMSpecWorkingDraft, error) {
	return scanPMSpecWorkingDraft(s.db.QueryRowContext(ctx, `SELECT `+pmDraftColumns+` FROM console_spec_working_draft WHERE spec_id=?`, specID))
}

func (s *SQLite) ListPMSpecWorkingDrafts(ctx context.Context, actor string) ([]PMSpecWorkingDraft, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+pmDraftColumns+` FROM console_spec_working_draft WHERE principal_id=? OR current_editor_id=? ORDER BY updated_at DESC,spec_id`, actor, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PMSpecWorkingDraft
	for rows.Next() {
		d, scanErr := scanPMSpecWorkingDraft(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if d != nil {
			out = append(out, *d)
		}
	}
	return out, rows.Err()
}

// SavePMSpecWorkingDraft is the only autosave write. On a stale CAS it commits
// an append-only conflict record containing both complete payloads before it
// returns ErrPMSpecFlowConflict.
func (s *SQLite) SavePMSpecWorkingDraft(ctx context.Context, actor string, in PMSpecDraftInput, now time.Time) (*PMSpecWorkingDraft, error) {
	if err := normalizePMDraft(&in); err != nil {
		return nil, err
	}
	sha, err := pmDraftContentSHA(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT active FROM console_principal WHERE id=?`, actor).Scan(&active); err != nil || active != 1 {
		return nil, ErrPMSpecFlowDenied
	}
	current, err := scanPMSpecWorkingDraft(tx.QueryRowContext(ctx, `SELECT `+pmDraftColumns+` FROM console_spec_working_draft WHERE spec_id=?`, in.SpecID))
	if err != nil {
		return nil, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if current == nil {
		if in.ExpectedVersion != 0 || in.ExpectedSHA256 != "" {
			return nil, ErrPMSpecFlowConflict
		}
		var owner string
		var baseRevision int
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_spec WHERE id=?`, in.SpecID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists != 0 {
			if err = tx.QueryRowContext(ctx, `SELECT principal_id,current_revision FROM console_spec WHERE id=? AND current_editor_id=? AND archived=0 AND state='editing' AND editing_closed=0`, in.SpecID, actor).Scan(&owner, &baseRevision); err != nil {
				return nil, ErrPMSpecFlowDenied
			}
		} else {
			owner = actor
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec_working_draft(spec_id,principal_id,current_editor_id,title,content_markdown,acceptance_json,input_anchors_json,source_refs_json,norm_anchors_json,version,content_sha256,base_revision,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,1,?,?,? ,?,?)`, in.SpecID, owner, actor, in.Title, in.ContentMarkdown, in.AcceptanceJSON, in.InputAnchorsJSON, in.SourceRefsJSON, in.NormAnchorsJSON, sha, baseRevision, "editing", stamp, stamp); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec_flow(spec_id,original_editor_id,review_recipient_id,state,frozen_revision,frozen_sha256,review_request_id,review_sha256,decisions_json,completed_by,completed_at,created_at,updated_at) VALUES(?,?,'','editing',0,'','','','[]','','',?,?)`, in.SpecID, actor, stamp, stamp); err != nil {
			return nil, err
		}
		if err = appendPMSpecFlowEvent(ctx, tx, in.SpecID, actor, "draft_created", 0, sha, now); err != nil {
			return nil, err
		}
	} else {
		if current.CurrentEditorID != actor || current.State != "editing" || current.Version != in.ExpectedVersion || current.ContentSHA256 != in.ExpectedSHA256 {
			currentJSON, _ := json.Marshal(current)
			submittedJSON, _ := json.Marshal(in)
			if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec_working_draft_conflict(id,spec_id,actor_id,expected_version,expected_sha256,submitted_json,current_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, newID(), in.SpecID, actor, in.ExpectedVersion, in.ExpectedSHA256, string(submittedJSON), string(currentJSON), stamp); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			return nil, ErrPMSpecFlowConflict
		}
		res, e := tx.ExecContext(ctx, `UPDATE console_spec_working_draft SET title=?,content_markdown=?,acceptance_json=?,input_anchors_json=?,source_refs_json=?,norm_anchors_json=?,version=version+1,content_sha256=?,updated_at=? WHERE spec_id=? AND current_editor_id=? AND state='editing' AND version=? AND content_sha256=?`, in.Title, in.ContentMarkdown, in.AcceptanceJSON, in.InputAnchorsJSON, in.SourceRefsJSON, in.NormAnchorsJSON, sha, stamp, in.SpecID, actor, in.ExpectedVersion, in.ExpectedSHA256)
		if e != nil {
			return nil, e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return nil, ErrPMSpecFlowConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_flow_assist SET status='stale',updated_at=? WHERE spec_id=? AND actor_id=? AND status IN ('pending','completed') AND (working_version<>? OR content_sha256<>?)`, stamp, in.SpecID, actor, in.ExpectedVersion+1, sha); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetPMSpecWorkingDraft(ctx, in.SpecID)
}

func appendPMSpecFlowEvent(ctx context.Context, tx *sql.Tx, specID, actor, action string, revision int, sha string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO console_spec_flow_event(id,spec_id,actor_id,action,revision,content_sha256,detail_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, newID(), specID, actor, action, revision, sha, `{}`, now.UTC().Format(time.RFC3339Nano))
	return err
}

func appendPMSpecFlowOperation(ctx context.Context, tx *sql.Tx, specID, actor string, in PMSpecFreezeInput, revision int, sha string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO console_spec_flow_event(id,spec_id,actor_id,action,revision,content_sha256,detail_json,operation_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, newID(), specID, actor, string(in.Action), revision, sha, mustJSON(in), in.OperationID, now.UTC().Format(time.RFC3339Nano))
	return err
}

type PMSpecFreezeAction string

const (
	PMSpecAcceptSuggestions PMSpecFreezeAction = "accept_suggestions"
	PMSpecTransferReview    PMSpecFreezeAction = "transfer_review"
	PMSpecCompleteReview    PMSpecFreezeAction = "complete_review"
)

type PMSpecFreezeInput struct {
	OperationID     string             `json:"operation_id"`
	ExpectedVersion int                `json:"expected_version"`
	ExpectedSHA256  string             `json:"expected_sha256"`
	Action          PMSpecFreezeAction `json:"action"`
	TargetPrincipal string             `json:"target_principal,omitempty"`
	AssistRequestID string             `json:"assist_request_id,omitempty"`
}

type PMSpecAssistSuggestion struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

type PMSpecAssistResult struct {
	Suggestions     []PMSpecAssistSuggestion `json:"suggestions"`
	ReviewQuestions []struct {
		ID       string `json:"id"`
		Question string `json:"question"`
	} `json:"review_questions"`
}

type PMSpecAssistRequest struct {
	RequestID      string
	SpecID         string
	ActorID        string
	AgentID        string
	WorkingVersion int
	ContentSHA256  string
	Status         string
	ResultJSON     string
	HubBoot        string
	Deadline       int64
}

func (s *SQLite) PMSpecAssistRequest(ctx context.Context, requestID string) (*PMSpecAssistRequest, error) {
	q := new(PMSpecAssistRequest)
	err := s.db.QueryRowContext(ctx, `SELECT request_id,spec_id,actor_id,agent_id,working_version,content_sha256,status,result_json,hub_boot,deadline FROM console_spec_flow_assist WHERE request_id=?`, requestID).Scan(&q.RequestID, &q.SpecID, &q.ActorID, &q.AgentID, &q.WorkingVersion, &q.ContentSHA256, &q.Status, &q.ResultJSON, &q.HubBoot, &q.Deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return q, err
}

func decodePMSpecAssistResult(raw []byte) (PMSpecAssistResult, error) {
	var result PMSpecAssistResult
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return result, ErrSpecAgentInvalid
	}
	if result.Suggestions == nil {
		result.Suggestions = []PMSpecAssistSuggestion{}
	}
	if result.ReviewQuestions == nil {
		result.ReviewQuestions = []struct {
			ID       string `json:"id"`
			Question string `json:"question"`
		}{}
	}
	seen := map[string]bool{}
	for _, item := range result.Suggestions {
		if item.ID == "" || seen[item.ID] || !json.Valid(item.Payload) {
			return result, ErrSpecAgentInvalid
		}
		seen[item.ID] = true
		if err := validatePMSpecSuggestionPayload(item.Kind, item.Payload); err != nil {
			return result, ErrSpecAgentInvalid
		}
	}
	for _, item := range result.ReviewQuestions {
		if item.ID == "" || strings.TrimSpace(item.Question) == "" || seen[item.ID] {
			return result, ErrSpecAgentInvalid
		}
		seen[item.ID] = true
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return result, ErrSpecAgentInvalid
	}
	return result, nil
}

func validatePMSpecSuggestionPayload(kind string, raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return ErrSpecAgentInvalid
	}
	read := func(key string) (string, bool) {
		var value string
		b, ok := fields[key]
		if !ok || json.Unmarshal(b, &value) != nil || strings.TrimSpace(value) == "" {
			return "", false
		}
		return value, true
	}
	switch kind {
	case "acceptance":
		if _, ok := read("text"); !ok {
			return ErrSpecAgentInvalid
		}
	case "source_ref":
		uriText, ok := read("uri")
		if !ok {
			return ErrSpecAgentInvalid
		}
		u, err := url.Parse(uriText)
		if err != nil || u.Scheme == "" || (u.Host == "" && u.Scheme != "file") {
			return ErrSpecAgentInvalid
		}
	case "norm_anchor":
		if _, ok := read("standard"); !ok {
			return ErrSpecAgentInvalid
		}
		if _, ok := read("version"); !ok {
			return ErrSpecAgentInvalid
		}
		if _, ok := read("section"); !ok {
			return ErrSpecAgentInvalid
		}
	default:
		return ErrSpecAgentInvalid
	}
	return nil
}

func readPMSpecDecisions(ctx context.Context, tx *sql.Tx, requestID, actor string) ([]PMSpecFlowDecision, error) {
	rows, err := tx.QueryContext(ctx, `SELECT item_id,outcome,reason FROM console_spec_flow_decision WHERE request_id=? AND actor_id=?`, requestID, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PMSpecFlowDecision
	for rows.Next() {
		var d PMSpecFlowDecision
		if err = rows.Scan(&d.ItemID, &d.Outcome, &d.Reason); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func validatePMSpecDecisions(result PMSpecAssistResult, decisions []PMSpecFlowDecision, requireAll bool) error {
	want := map[string]bool{}
	for _, item := range result.Suggestions {
		want[item.ID] = true
	}
	for _, item := range result.ReviewQuestions {
		want[item.ID] = true
	}
	if requireAll && len(want) != len(decisions) {
		return ErrPMSpecFlowDenied
	}
	seen := map[string]bool{}
	for _, d := range decisions {
		if !want[d.ItemID] || seen[d.ItemID] || (d.Outcome != "accept" && d.Outcome != "reject") {
			return ErrPMSpecFlowDenied
		}
		seen[d.ItemID] = true
	}
	return nil
}

func appendPMJSONItem(raw string, item json.RawMessage) (string, error) {
	var list []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &list); err != nil || list == nil {
		return "", ErrSpecAgentInvalid
	}
	list = append(list, append(json.RawMessage(nil), item...))
	out, err := json.Marshal(list)
	return string(out), err
}

func appendPMAcceptance(raw string, item json.RawMessage) (string, error) {
	var list []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &list); err == nil && list != nil {
		list = append(list, append(json.RawMessage(nil), item...))
		out, e := json.Marshal(list)
		return string(out), e
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return "", ErrSpecAgentInvalid
	}
	if existing := fields["criteria"]; len(existing) > 0 {
		if err := json.Unmarshal(existing, &list); err != nil {
			return "", ErrSpecAgentInvalid
		}
	}
	list = append(list, append(json.RawMessage(nil), item...))
	encoded, err := json.Marshal(list)
	if err != nil {
		return "", err
	}
	fields["criteria"] = encoded
	out, err := json.Marshal(fields)
	return string(out), err
}

func applyPMSpecAcceptedSuggestions(d *PMSpecWorkingDraft, result PMSpecAssistResult, decisions []PMSpecFlowDecision) (*PMSpecWorkingDraft, error) {
	accepted := map[string]bool{}
	for _, decision := range decisions {
		if decision.Outcome == "accept" {
			accepted[decision.ItemID] = true
		}
	}
	out := *d
	for _, item := range result.Suggestions {
		if !accepted[item.ID] {
			continue
		}
		var err error
		switch item.Kind {
		case "acceptance":
			out.AcceptanceJSON, err = appendPMAcceptance(out.AcceptanceJSON, item.Payload)
		case "source_ref":
			out.SourceRefsJSON, err = appendPMJSONItem(out.SourceRefsJSON, item.Payload)
		case "norm_anchor":
			out.NormAnchorsJSON, err = appendPMJSONItem(out.NormAnchorsJSON, item.Payload)
		}
		if err != nil {
			return nil, err
		}
	}
	in := PMSpecDraftInput{Title: out.Title, ContentMarkdown: out.ContentMarkdown, AcceptanceJSON: out.AcceptanceJSON, InputAnchorsJSON: out.InputAnchorsJSON, SourceRefsJSON: out.SourceRefsJSON, NormAnchorsJSON: out.NormAnchorsJSON}
	sha, err := pmDraftContentSHA(in)
	if err != nil {
		return nil, err
	}
	out.ContentSHA256 = sha
	return &out, nil
}

// FreezePMSpecWorkingDraft creates an immutable revision for exactly the three
// approved actions. It combines spec creation/revision, workflow state, and
// ownership transition in one SQLite transaction.
func (s *SQLite) FreezePMSpecWorkingDraft(ctx context.Context, specID, actor string, in PMSpecFreezeInput, now time.Time) (int, error) {
	if (in.Action != PMSpecAcceptSuggestions && in.Action != PMSpecTransferReview && in.Action != PMSpecCompleteReview) || strings.TrimSpace(in.OperationID) == "" || len(in.OperationID) > 128 {
		return 0, ErrPMSpecFlowDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	detail := mustJSON(in)
	var priorActor, priorAction, priorDetail string
	var priorRevision int
	err = tx.QueryRowContext(ctx, `SELECT actor_id,action,revision,detail_json FROM console_spec_flow_event WHERE operation_id=?`, in.OperationID).Scan(&priorActor, &priorAction, &priorRevision, &priorDetail)
	if err == nil {
		if priorActor == actor && priorAction == string(in.Action) && priorDetail == detail {
			return priorRevision, nil
		}
		return 0, ErrPMSpecFlowConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	d, err := scanPMSpecWorkingDraft(tx.QueryRowContext(ctx, `SELECT `+pmDraftColumns+` FROM console_spec_working_draft WHERE spec_id=?`, specID))
	if err != nil || d == nil || d.CurrentEditorID != actor || d.Version != in.ExpectedVersion || d.ContentSHA256 != in.ExpectedSHA256 {
		return 0, ErrPMSpecFlowConflict
	}
	var original string
	var recipient, flowState string
	if err = tx.QueryRowContext(ctx, `SELECT original_editor_id,review_recipient_id,state FROM console_spec_flow WHERE spec_id=?`, specID).Scan(&original, &recipient, &flowState); err != nil {
		return 0, ErrPMSpecFlowDenied
	}
	transferredComplete := in.Action == PMSpecCompleteReview && flowState == "review" && recipient == actor && d.State == "review"
	if !transferredComplete && (flowState != "editing" || d.State != "editing" || d.CurrentEditorID != actor) {
		return 0, ErrPMSpecFlowDenied
	}
	nowStamp := now.UTC().Format(time.RFC3339Nano)
	revision := d.BaseRevision
	if in.Action == PMSpecAcceptSuggestions {
		if in.AssistRequestID == "" {
			return 0, ErrPMSpecFlowDenied
		}
		var runVersion int
		var runSHA, runStatus, runActor, agentID, resultJSON string
		if err = tx.QueryRowContext(ctx, `SELECT working_version,content_sha256,status,actor_id,agent_id,result_json FROM console_spec_flow_assist WHERE request_id=? AND spec_id=?`, in.AssistRequestID, specID).Scan(&runVersion, &runSHA, &runStatus, &runActor, &agentID, &resultJSON); err != nil || runActor != actor || runStatus != "completed" || runVersion != d.Version || runSHA != d.ContentSHA256 {
			return 0, ErrPMSpecFlowStale
		}
		result, decodeErr := decodePMSpecAssistResult([]byte(resultJSON))
		if decodeErr != nil {
			return 0, decodeErr
		}
		decisions, decisionErr := readPMSpecDecisions(ctx, tx, in.AssistRequestID, actor)
		if decisionErr != nil {
			return 0, decisionErr
		}
		if err = validatePMSpecDecisions(result, decisions, true); err != nil {
			return 0, err
		}
		var activeAgent int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_agent WHERE id=? AND owner_principal=? AND active=1`, agentID, actor).Scan(&activeAgent); err != nil || activeAgent != 1 {
			return 0, ErrPMSpecFlowDenied
		}
		updated, applyErr := applyPMSpecAcceptedSuggestions(d, result, decisions)
		if applyErr != nil {
			return 0, applyErr
		}
		d = updated
	}
	if in.Action == PMSpecCompleteReview && (!validSpecFinalAcceptance(d.AcceptanceJSON) || legacySpecAcceptance(d.AcceptanceJSON)) {
		return 0, ErrConsoleSpecGate
	}
	if !transferredComplete && (in.Action == PMSpecAcceptSuggestions || in.Action == PMSpecCompleteReview || in.Action == PMSpecTransferReview) {
		revision++
		if err = freezePMSpecRevisionTx(ctx, tx, d, actor, revision, now); err != nil {
			return 0, err
		}
		if in.Action == PMSpecAcceptSuggestions {
			if _, err = tx.ExecContext(ctx, `UPDATE console_spec_working_draft SET acceptance_json=?,source_refs_json=?,norm_anchors_json=?,version=version+1,content_sha256=?,base_revision=?,updated_at=? WHERE spec_id=? AND version=? AND content_sha256=?`, d.AcceptanceJSON, d.SourceRefsJSON, d.NormAnchorsJSON, d.ContentSHA256, revision, nowStamp, specID, in.ExpectedVersion, in.ExpectedSHA256); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE console_spec_flow_assist SET status='stale',updated_at=? WHERE spec_id=? AND status IN ('pending','completed')`, nowStamp, specID); err != nil {
				return 0, err
			}
		} else if _, err = tx.ExecContext(ctx, `UPDATE console_spec_working_draft SET base_revision=?,updated_at=? WHERE spec_id=? AND version=? AND content_sha256=?`, revision, nowStamp, specID, d.Version, d.ContentSHA256); err != nil {
			return 0, err
		}
	} else if in.Action == PMSpecTransferReview {
		revision = d.BaseRevision
	}
	if in.Action == PMSpecAcceptSuggestions {
		if err = appendPMSpecFlowOperation(ctx, tx, specID, actor, in, revision, d.ContentSHA256, now); err != nil {
			return 0, err
		}
		if err = tx.Commit(); err != nil {
			return 0, err
		}
		return revision, nil
	}
	if in.Action == PMSpecTransferReview {
		if in.TargetPrincipal == "" || in.TargetPrincipal == actor {
			return 0, ErrConsoleSpecSelfTransfer
		}
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_principal WHERE id=? AND active=1`, in.TargetPrincipal).Scan(&active); err != nil || active != 1 {
			return 0, ErrConsoleSpecReviewerInvalid
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET state='review',current_editor_id=?,updated_at=? WHERE id=? AND current_editor_id=? AND archived=0 AND state='editing'`, in.TargetPrincipal, nowStamp, specID, actor); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_flow SET state='review',review_recipient_id=?,frozen_revision=?,frozen_sha256=?,updated_at=? WHERE spec_id=? AND state='editing'`, in.TargetPrincipal, revision, d.ContentSHA256, nowStamp, specID); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_working_draft SET state='review',current_editor_id=?,updated_at=? WHERE spec_id=? AND current_editor_id=? AND state='editing'`, in.TargetPrincipal, nowStamp, specID, actor); err != nil {
			return 0, err
		}
		if err = appendPMSpecFlowOperation(ctx, tx, specID, actor, in, revision, d.ContentSHA256, now); err != nil {
			return 0, err
		}
	} else {
		if in.AssistRequestID == "" {
			return 0, ErrPMSpecFlowDenied
		}
		var runVersion int
		var runSHA, runStatus, runActor, agentID string
		if err = tx.QueryRowContext(ctx, `SELECT working_version,content_sha256,status,actor_id,agent_id FROM console_spec_flow_assist WHERE request_id=? AND spec_id=?`, in.AssistRequestID, specID).Scan(&runVersion, &runSHA, &runStatus, &runActor, &agentID); err != nil {
			return 0, ErrPMSpecFlowDenied
		}
		if runActor != actor || runStatus != "completed" || runVersion != d.Version || runSHA != d.ContentSHA256 {
			return 0, ErrPMSpecFlowStale
		}
		var resultJSON string
		if err = tx.QueryRowContext(ctx, `SELECT result_json FROM console_spec_flow_assist WHERE request_id=?`, in.AssistRequestID).Scan(&resultJSON); err != nil {
			return 0, err
		}
		assistResult, decodeErr := decodePMSpecAssistResult([]byte(resultJSON))
		if decodeErr != nil {
			return 0, decodeErr
		}
		decisions, decisionErr := readPMSpecDecisions(ctx, tx, in.AssistRequestID, actor)
		if decisionErr != nil {
			return 0, decisionErr
		}
		if err = validatePMSpecDecisions(assistResult, decisions, true); err != nil {
			return 0, err
		}
		var activeAgent int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_agent WHERE id=? AND owner_principal=? AND active=1`, agentID, actor).Scan(&activeAgent); err != nil || activeAgent != 1 {
			return 0, ErrPMSpecFlowDenied
		}
		if transferredComplete {
			if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET current_editor_id=?,state='review',updated_at=? WHERE id=? AND current_editor_id=? AND state='review'`, original, nowStamp, specID, actor); err != nil {
				return 0, err
			}
		} else if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET state='review',updated_at=? WHERE id=? AND current_editor_id=? AND state='editing'`, nowStamp, specID, actor); err != nil {
			return 0, err
		}
		allowedFlowState := "editing"
		if transferredComplete {
			allowedFlowState = "review"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_flow SET state='review_complete',review_recipient_id='',frozen_revision=?,frozen_sha256=?,review_request_id=?,review_sha256=?,completed_by=?,completed_at=?,updated_at=? WHERE spec_id=? AND state=?`, revision, d.ContentSHA256, in.AssistRequestID, d.ContentSHA256, actor, nowStamp, nowStamp, specID, allowedFlowState); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE console_spec_working_draft SET state='review_complete',current_editor_id=?,updated_at=? WHERE spec_id=? AND current_editor_id=? AND state=?`, original, nowStamp, specID, actor, map[bool]string{true: "review", false: "editing"}[transferredComplete]); err != nil {
			return 0, err
		}
		if err = appendPMSpecFlowOperation(ctx, tx, specID, actor, in, revision, d.ContentSHA256, now); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	_ = original
	return revision, nil
}

func freezePMSpecRevisionTx(ctx context.Context, tx *sql.Tx, d *PMSpecWorkingDraft, actor string, revision int, now time.Time) error {
	var current int
	err := tx.QueryRowContext(ctx, `SELECT current_revision FROM console_spec WHERE id=?`, d.SpecID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		capHash := sha256.Sum256([]byte(newID()))
		spec := model.ConsoleSpec{ID: d.SpecID, PrincipalID: d.PrincipalID, Title: d.Title, EditCapabilityHash: hex.EncodeToString(capHash[:])}
		r := model.ConsoleSpecRevision{SpecID: d.SpecID, Revision: 1, ContentMarkdown: d.ContentMarkdown, AcceptanceJSON: d.AcceptanceJSON, InputAnchorsJSON: d.InputAnchorsJSON, SourceRefsJSON: d.SourceRefsJSON, NormAnchorsJSON: d.NormAnchorsJSON, ContentSHA256: d.ContentSHA256, CreatedBy: actor, CreatedByKind: "principal", CreatedVia: "principal"}
		return createConsoleSpecTx(ctx, tx, spec, r, now)
	}
	if err != nil {
		return err
	}
	if revision != current+1 {
		return ErrConsoleSpecConflict
	}
	r := model.ConsoleSpecRevision{SpecID: d.SpecID, Revision: revision, ContentMarkdown: d.ContentMarkdown, AcceptanceJSON: d.AcceptanceJSON, InputAnchorsJSON: d.InputAnchorsJSON, SourceRefsJSON: d.SourceRefsJSON, NormAnchorsJSON: d.NormAnchorsJSON, ContentSHA256: d.ContentSHA256, CreatedBy: actor, CreatedByKind: "principal", CreatedVia: "principal", Supersedes: current}
	if err = insertConsoleSpecRevision(ctx, tx, r, now); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, appendConsoleSpecRevisionUpdateSQL, revision, now.UTC().Format(time.RFC3339Nano), d.SpecID, actor, current)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConsoleSpecConflict
	}
	return appendConsoleSpecEvent(ctx, tx, d.SpecID, revision, actor, "pm_spec_flow_freeze", fmt.Sprintf("supersedes=%d", current), now)
}

func (s *SQLite) StartPMSpecAssist(ctx context.Context, specID, actor, agentID, requestID, boot string, expectedVersion int, expectedSHA string, now time.Time) error {
	if requestID == "" || agentID == "" {
		return ErrPMSpecFlowDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var editor string
	var version int
	var sha, state string
	if err = tx.QueryRowContext(ctx, `SELECT current_editor_id,version,content_sha256,state FROM console_spec_working_draft WHERE spec_id=?`, specID).Scan(&editor, &version, &sha, &state); err != nil || editor != actor || version != expectedVersion || sha != expectedSHA {
		return ErrPMSpecFlowConflict
	}
	var flowState, recipient string
	if err = tx.QueryRowContext(ctx, `SELECT state,review_recipient_id FROM console_spec_flow WHERE spec_id=?`, specID).Scan(&flowState, &recipient); err != nil {
		return ErrPMSpecFlowDenied
	}
	if state == "editing" && flowState != "editing" {
		return ErrPMSpecFlowDenied
	}
	if state == "review" && (flowState != "review" || recipient != actor) {
		return ErrPMSpecFlowDenied
	}
	if state != "editing" && state != "review" {
		return ErrPMSpecFlowDenied
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM console_agent WHERE id=? AND owner_principal=? AND active=1`, agentID, actor).Scan(&n); err != nil || n != 1 {
		return ErrConsoleSpecPermission
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO console_spec_flow_assist(request_id,spec_id,actor_id,agent_id,working_version,content_sha256,status,result_json,hub_boot,deadline,created_at,updated_at) VALUES(?,?,?,?,?,?,'pending','{}',?,?,?,?)`, requestID, specID, actor, agentID, version, sha, boot, now.Add(180*time.Second).Unix(), stamp, stamp)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) FailPMSpecAssist(ctx context.Context, requestID, reason string, now time.Time) error {
	if len(reason) > 512 {
		reason = reason[:512]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE console_spec_flow_assist SET status='failed',result_json=?,updated_at=? WHERE request_id=? AND status='pending'`, mustJSON(map[string]string{"error": reason}), now.UTC().Format(time.RFC3339Nano), requestID)
	return err
}

func (s *SQLite) MarkPMSpecAssistStale(ctx context.Context, requestID, actor, reason string, now time.Time) error {
	if len(reason) > 512 {
		reason = reason[:512]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE console_spec_flow_assist SET status='stale',result_json=?,updated_at=? WHERE request_id=? AND actor_id=? AND status='pending'`, mustJSON(map[string]string{"stale": reason}), now.UTC().Format(time.RFC3339Nano), requestID, actor)
	return err
}

func (s *SQLite) CompletePMSpecAssist(ctx context.Context, requestID, agentID string, result json.RawMessage, now time.Time) error {
	if len(result) > 1<<20 {
		return ErrSpecAgentInvalid
	}
	if _, err := decodePMSpecAssistResult(result); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var specID, status, sha, actor string
	var version int
	var deadline int64
	if err = tx.QueryRowContext(ctx, `SELECT spec_id,status,working_version,content_sha256,actor_id,deadline FROM console_spec_flow_assist WHERE request_id=? AND agent_id=?`, requestID, agentID).Scan(&specID, &status, &version, &sha, &actor, &deadline); err != nil {
		return ErrPMSpecFlowDenied
	}
	if now.UTC().Unix() > deadline {
		_, updateErr := tx.ExecContext(ctx, `UPDATE console_spec_flow_assist SET status='stale',result_json=?,updated_at=? WHERE request_id=? AND status='pending'`, mustJSON(map[string]string{"stale": "expired"}), now.UTC().Format(time.RFC3339Nano), requestID)
		if updateErr != nil {
			return updateErr
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return ErrPMSpecFlowStale
	}
	var currentVersion int
	var currentSHA, currentEditor, draftState string
	if err = tx.QueryRowContext(ctx, `SELECT version,content_sha256,current_editor_id,state FROM console_spec_working_draft WHERE spec_id=?`, specID).Scan(&currentVersion, &currentSHA, &currentEditor, &draftState); err != nil {
		return ErrPMSpecFlowStale
	}
	newStatus := "completed"
	if currentVersion != version || currentSHA != sha || currentEditor != actor || (draftState != "editing" && draftState != "review") {
		newStatus = "stale"
	}
	if status != "pending" {
		return ErrPMSpecFlowConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE console_spec_flow_assist SET status=?,result_json=?,updated_at=? WHERE request_id=? AND status='pending'`, newStatus, string(result), now.UTC().Format(time.RFC3339Nano), requestID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type PMSpecFlowDecision struct {
	ItemID  string `json:"item_id"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

func (s *SQLite) DecidePMSpecAssist(ctx context.Context, requestID, actor string, decisions []PMSpecFlowDecision, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var specID, sha, status string
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT spec_id,content_sha256,status,working_version FROM console_spec_flow_assist WHERE request_id=? AND actor_id=?`, requestID, actor).Scan(&specID, &sha, &status, &version); err != nil || status != "completed" {
		return ErrPMSpecFlowDenied
	}
	var currentSHA, state, editor string
	var currentVersion int
	if err = tx.QueryRowContext(ctx, `SELECT content_sha256,state,current_editor_id,version FROM console_spec_working_draft WHERE spec_id=?`, specID).Scan(&currentSHA, &state, &editor, &currentVersion); err != nil || sha != currentSHA || version != currentVersion || editor != actor || (state != "editing" && state != "review") {
		return ErrPMSpecFlowStale
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT result_json FROM console_spec_flow_assist WHERE request_id=?`, requestID).Scan(&raw); err != nil {
		return ErrSpecAgentInvalid
	}
	decoded, err := decodePMSpecAssistResult([]byte(raw))
	if err != nil {
		return err
	}
	if err = validatePMSpecDecisions(decoded, decisions, false); err != nil {
		return err
	}
	for _, d := range decisions {
		var oldOutcome, oldReason, oldActor string
		lookupErr := tx.QueryRowContext(ctx, `SELECT outcome,reason,actor_id FROM console_spec_flow_decision WHERE request_id=? AND item_id=?`, requestID, d.ItemID).Scan(&oldOutcome, &oldReason, &oldActor)
		if lookupErr == nil {
			if oldOutcome != d.Outcome || oldReason != d.Reason || oldActor != actor {
				return ErrPMSpecFlowConflict
			}
			continue
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO console_spec_flow_decision(request_id,item_id,outcome,reason,actor_id,created_at) VALUES(?,?,?,?,?,?)`, requestID, d.ItemID, d.Outcome, d.Reason, actor, now.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) ReturnPMSpecReview(ctx context.Context, specID, actor, objection string, now time.Time) error {
	if strings.TrimSpace(objection) == "" || len(objection) > 16<<10 {
		return ErrPMSpecFlowDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var original string
	var recipient, state string
	if err = tx.QueryRowContext(ctx, `SELECT original_editor_id,review_recipient_id,state FROM console_spec_flow WHERE spec_id=?`, specID).Scan(&original, &recipient, &state); err != nil || state != "review" || recipient != actor {
		return ErrPMSpecFlowDenied
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_flow SET state='editing',review_recipient_id='',updated_at=? WHERE spec_id=? AND state='review' AND review_recipient_id=?`, stamp, specID, actor); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_working_draft SET state='editing',current_editor_id=?,updated_at=? WHERE spec_id=? AND state='review' AND current_editor_id=?`, original, stamp, specID, actor); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec SET state='editing',current_editor_id=?,updated_at=? WHERE id=? AND state='review' AND current_editor_id=?`, original, stamp, specID, actor); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_flow_assist SET status='stale',updated_at=? WHERE spec_id=? AND status IN ('pending','completed')`, stamp, specID); err != nil {
		return err
	}
	if err = appendPMSpecFlowEvent(ctx, tx, specID, actor, "return_review", 0, "", now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE console_spec_flow_event SET detail_json=? WHERE rowid=last_insert_rowid()`, mustJSON(map[string]string{"objection": objection})); err != nil {
		return err
	}
	return tx.Commit()
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
