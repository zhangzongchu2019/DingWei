package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type consoleSpecRepository interface {
	CreateConsoleSpec(context.Context, model.ConsoleSpec, model.ConsoleSpecRevision, time.Time) error
	ListConsoleSpecs(context.Context, string, bool) ([]model.ConsoleSpec, error)
	ConsoleSpec(context.Context, string, string, bool) (*model.ConsoleSpec, error)
	ConsoleSpecRevision(context.Context, string, string, int, bool) (*model.ConsoleSpecRevision, error)
	AppendConsoleSpecRevision(context.Context, string, string, int, model.ConsoleSpecRevision, time.Time) (int, error)
	AppendConsoleSpecRevisionWithCapability(context.Context, string, int, string, model.ConsoleSpecRevision, time.Time) (int, error)
	CloseConsoleSpecEditing(context.Context, string, string, time.Time) error
	SubmitConsoleSpec(context.Context, string, string, time.Time) error
	ReviewConsoleSpec(context.Context, model.ConsoleSpecReview, time.Time) error
	ClaimConsoleSpec(context.Context, string, string, time.Time) error
	ArchiveConsoleSpec(context.Context, string, string, time.Time) error
	ListReadyConsoleTaskProposals(context.Context) ([]model.ConsoleTaskProposal, error)
}

func specCapabilityHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type specAcceptance struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type specInputAnchor struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Digest string `json:"digest,omitempty"`
}

type consoleSpecWriteRequest struct {
	Title            string            `json:"title"`
	ContentMarkdown  string            `json:"content_markdown"`
	Acceptance       []specAcceptance  `json:"acceptance"`
	InputAnchors     []specInputAnchor `json:"input_anchors"`
	ExpectedRevision int               `json:"expected_revision,omitempty"`
}

func validateConsoleSpecWrite(req consoleSpecWriteRequest) error {
	if strings.TrimSpace(req.Title) == "" || len(req.Title) > 200 || strings.TrimSpace(req.ContentMarkdown) == "" || len(req.Acceptance) == 0 || len(req.InputAnchors) == 0 {
		return errors.New("title, content, acceptance and input anchors are required")
	}
	seen := map[string]bool{}
	for _, item := range req.Acceptance {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Text) == "" || seen[item.ID] {
			return errors.New("acceptance ids and text must be non-empty and unique")
		}
		seen[item.ID] = true
	}
	for _, anchor := range req.InputAnchors {
		if strings.TrimSpace(anchor.Ref) == "" {
			return errors.New("input anchor ref is required")
		}
		switch anchor.Kind {
		case "requirement_pool_item", "owner_message":
		case "upstream_spec":
			if strings.TrimSpace(anchor.Digest) == "" {
				return errors.New("upstream spec anchor requires digest")
			}
		default:
			return errors.New("invalid input anchor kind")
		}
	}
	return nil
}

func decodeConsoleSpecWrite(w http.ResponseWriter, r *http.Request) (consoleSpecWriteRequest, bool) {
	var req consoleSpecWriteRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil || validateConsoleSpecWrite(req) != nil {
		http.Error(w, "invalid spec", http.StatusBadRequest)
		return consoleSpecWriteRequest{}, false
	}
	return req, true
}

func revisionFromRequest(specID, principal string, revision int, req consoleSpecWriteRequest) (model.ConsoleSpecRevision, error) {
	acceptance, err := json.Marshal(req.Acceptance)
	if err != nil {
		return model.ConsoleSpecRevision{}, err
	}
	anchors, err := json.Marshal(req.InputAnchors)
	if err != nil {
		return model.ConsoleSpecRevision{}, err
	}
	sum := sha256.Sum256([]byte(req.ContentMarkdown))
	return model.ConsoleSpecRevision{SpecID: specID, Revision: revision, ContentMarkdown: req.ContentMarkdown,
		AcceptanceJSON: string(acceptance), InputAnchorsJSON: string(anchors), ContentSHA256: hex.EncodeToString(sum[:]), CreatedBy: principal}, nil
}

func (h *Hub) consoleSpecs() (consoleSpecRepository, bool) {
	repo, ok := h.Repo.(consoleSpecRepository)
	return repo, ok
}

func (h *Hub) HandleConsoleSpecs(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	principal := platformPrincipal(r)
	management := platformRole(r) == consolePlatformRoleManagement
	if r.Method == http.MethodGet {
		items, err := repo.ListConsoleSpecs(r.Context(), principal, management)
		if err != nil {
			http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"role": platformRole(r), "specs": items})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, ok := decodeConsoleSpecWrite(w, r)
	if !ok {
		return
	}
	id := "spec-" + randomHex(12)
	editToken := randomHex(32)
	rev, err := revisionFromRequest(id, principal, 1, req)
	if err != nil {
		http.Error(w, "invalid spec", http.StatusBadRequest)
		return
	}
	spec := model.ConsoleSpec{ID: id, PrincipalID: principal, Title: strings.TrimSpace(req.Title), EditCapabilityHash: specCapabilityHash(editToken)}
	if err = repo.CreateConsoleSpec(r.Context(), spec, rev, h.now()); err != nil {
		http.Error(w, "console spec unavailable", http.StatusConflict)
		return
	}
	created, _ := repo.ConsoleSpec(r.Context(), principal, id, management)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		*model.ConsoleSpec
		EditURL string `json:"edit_url"`
	}{created, "/console/specs/" + id + "#edit=" + editToken})
}

func (h *Hub) HandleConsoleSpec(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	principal := platformPrincipal(r)
	management := platformRole(r) == consolePlatformRoleManagement
	id := r.PathValue("specID")
	spec, err := repo.ConsoleSpec(r.Context(), principal, id, management)
	if err != nil {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	if spec == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		rev, err := repo.ConsoleSpecRevision(r.Context(), principal, id, spec.CurrentRevision, management)
		if err != nil {
			http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"spec": spec, "revision": rev})
		return
	}
	if r.Method != http.MethodPatch || management {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	req, ok := decodeConsoleSpecWrite(w, r)
	if !ok {
		return
	}
	if req.ExpectedRevision <= 0 {
		http.Error(w, "expected_revision required", http.StatusBadRequest)
		return
	}
	rev, err := revisionFromRequest(id, principal, req.ExpectedRevision+1, req)
	if err != nil {
		http.Error(w, "invalid spec", http.StatusBadRequest)
		return
	}
	capability := strings.TrimSpace(r.Header.Get("X-DW-Spec-Edit-Capability"))
	var next int
	if capability != "" {
		next, err = repo.AppendConsoleSpecRevisionWithCapability(r.Context(), id, req.ExpectedRevision, specCapabilityHash(capability), rev, h.now())
	} else if principal != "" {
		next, err = repo.AppendConsoleSpecRevision(r.Context(), principal, id, req.ExpectedRevision, rev, h.now())
	} else {
		err = store.ErrConsoleSpecEditDenied
	}
	if errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, store.ErrConsoleSpecConflict) {
		http.Error(w, "revision conflict", http.StatusConflict)
		return
	}
	if errors.Is(err, store.ErrConsoleSpecEditDenied) {
		http.Error(w, "editing closed or invalid capability", http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "console spec unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"revision": next})
}

func (h *Hub) HandleConsoleSpecCloseEditing(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := repo.CloseConsoleSpecEditing(r.Context(), platformPrincipal(r), r.PathValue("specID"), h.now()); errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecSubmit(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	if err := repo.SubmitConsoleSpec(r.Context(), platformPrincipal(r), r.PathValue("specID"), h.now()); errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "cannot submit spec", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecReview(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	var req struct {
		Revision    int      `json:"revision"`
		Stage       string   `json:"stage"`
		Verdict     string   `json:"verdict"`
		ReasonCodes []string `json:"reason_codes"`
		Detail      string   `json:"detail"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil {
		http.Error(w, "invalid review", 400)
		return
	}
	reasons, _ := json.Marshal(req.ReasonCodes)
	review := model.ConsoleSpecReview{ID: "review-" + randomHex(12), SpecID: r.PathValue("specID"), Revision: req.Revision, Stage: req.Stage, Verdict: req.Verdict, ReasonCodes: string(reasons), Detail: req.Detail, ReviewedBy: platformPrincipal(r)}
	if err := repo.ReviewConsoleSpec(r.Context(), review, h.now()); errors.Is(err, store.ErrConsoleSpecGateOrder) {
		http.Error(w, "gate order invalid", http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, "invalid review", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecClaim(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	if err := repo.ClaimConsoleSpec(r.Context(), r.PathValue("specID"), platformPrincipal(r), h.now()); err != nil {
		http.Error(w, "spec not claimable", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleSpecArchive(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	if err := repo.ArchiveConsoleSpec(r.Context(), r.PathValue("specID"), platformPrincipal(r), h.now()); errors.Is(err, store.ErrConsoleSpecNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "cannot archive spec", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) HandleConsoleTaskProposalsReady(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	items, err := repo.ListReadyConsoleTaskProposals(r.Context())
	if err != nil {
		http.Error(w, "console specs unavailable", 503)
		return
	}
	type sourceV1 struct {
		SpecID        string `json:"spec_id"`
		Revision      int    `json:"revision"`
		ContentSHA256 string `json:"content_sha256"`
	}
	type reviewV1 struct {
		ReviewID   string    `json:"review_id"`
		ReviewedBy string    `json:"reviewed_by"`
		ReviewedAt time.Time `json:"reviewed_at"`
	}
	type proposalV1 struct {
		SchemaVersion int             `json:"schema_version"`
		ProposalID    string          `json:"proposal_id"`
		Source        sourceV1        `json:"source"`
		Title         string          `json:"title"`
		Acceptance    json.RawMessage `json:"acceptance"`
		InputAnchors  json.RawMessage `json:"input_anchors"`
		HumanReview   reviewV1        `json:"human_review"`
		CreatedAt     time.Time       `json:"created_at"`
	}
	out := make([]proposalV1, 0, len(items))
	for _, item := range items {
		out = append(out, proposalV1{SchemaVersion: item.SchemaVersion, ProposalID: item.ID, Source: sourceV1{item.SpecID, item.Revision, item.ContentSHA256}, Title: item.Title, Acceptance: json.RawMessage(item.AcceptanceJSON), InputAnchors: json.RawMessage(item.InputAnchorsJSON), HumanReview: reviewV1{item.ReviewID, item.ReviewedBy, item.ReviewedAt}, CreatedAt: item.CreatedAt})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"proposals": out})
}
