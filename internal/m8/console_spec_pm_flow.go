package m8

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func (h *Hub) HandlePMSpecFlowPage(w http.ResponseWriter, r *http.Request) {
	if !h.pmSpecFlowEnabled {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	page := strings.Replace(consolePMSpecFlowPageHTML, "__PM_PRINCIPAL__", template.JSEscapeString(platformPrincipal(r)), 1)
	_, _ = w.Write([]byte(page))
}

type pmSpecFlowRepository interface {
	GetPMSpecWorkingDraft(context.Context, string) (*store.PMSpecWorkingDraft, error)
	ListPMSpecWorkingDrafts(context.Context, string) ([]store.PMSpecWorkingDraft, error)
	SavePMSpecWorkingDraft(context.Context, string, store.PMSpecDraftInput, time.Time) (*store.PMSpecWorkingDraft, error)
	FreezePMSpecWorkingDraft(context.Context, string, string, store.PMSpecFreezeInput, time.Time) (int, error)
	StartPMSpecAssist(context.Context, string, string, string, string, string, int, string, time.Time) error
	PMSpecAssistRequest(context.Context, string) (*store.PMSpecAssistRequest, error)
	CompletePMSpecAssist(context.Context, string, string, json.RawMessage, time.Time) error
	FailPMSpecAssist(context.Context, string, string, time.Time) error
	MarkPMSpecAssistStale(context.Context, string, string, string, time.Time) error
	DecidePMSpecAssist(context.Context, string, string, []store.PMSpecFlowDecision, time.Time) error
	ReturnPMSpecReview(context.Context, string, string, string, time.Time) error
}

func (h *Hub) pmSpecFlowStore() (pmSpecFlowRepository, bool) {
	r, ok := h.Repo.(pmSpecFlowRepository)
	return r, ok
}

func (h *Hub) pmSpecFlowUnavailable(w http.ResponseWriter, r *http.Request) bool {
	if !h.pmSpecFlowEnabled {
		http.NotFound(w, r)
		return true
	}
	if _, ok := h.pmSpecFlowStore(); !ok {
		http.Error(w, "PM spec flow storage unavailable", http.StatusServiceUnavailable)
		return true
	}
	return false
}

func (h *Hub) HandlePMSpecWorkingDraft(w http.ResponseWriter, r *http.Request) {
	if h.pmSpecFlowUnavailable(w, r) {
		return
	}
	repo, _ := h.pmSpecFlowStore()
	actor := platformPrincipal(r)
	if r.Method == http.MethodGet {
		if r.PathValue("specID") == "" {
			items, err := repo.ListPMSpecWorkingDrafts(r.Context(), actor)
			if err != nil {
				http.Error(w, "working drafts unavailable", http.StatusServiceUnavailable)
				return
			}
			writePMSpecJSON(w, http.StatusOK, map[string]any{"drafts": items})
			return
		}
		d, err := repo.GetPMSpecWorkingDraft(r.Context(), r.PathValue("specID"))
		if err != nil {
			http.Error(w, "working draft unavailable", http.StatusServiceUnavailable)
			return
		}
		if d == nil || (d.CurrentEditorID != actor && d.PrincipalID != actor) {
			http.Error(w, "working draft not found", http.StatusNotFound)
			return
		}
		writePMSpecJSON(w, http.StatusOK, d)
		return
	}
	var in store.PMSpecDraftInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || strings.TrimSpace(in.SpecID) == "" {
		http.Error(w, "invalid working draft", http.StatusBadRequest)
		return
	}
	d, err := repo.SavePMSpecWorkingDraft(r.Context(), actor, in, h.now())
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, store.ErrPMSpecFlowDenied) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	writePMSpecJSON(w, http.StatusOK, d)
}

func (h *Hub) HandlePMSpecFlowFreeze(w http.ResponseWriter, r *http.Request) {
	if h.pmSpecFlowUnavailable(w, r) {
		return
	}
	repo, _ := h.pmSpecFlowStore()
	var in store.PMSpecFreezeInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil {
		http.Error(w, "invalid freeze request", http.StatusBadRequest)
		return
	}
	revision, err := repo.FreezePMSpecWorkingDraft(r.Context(), r.PathValue("specID"), platformPrincipal(r), in, h.now())
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, store.ErrPMSpecFlowDenied) || errors.Is(err, store.ErrConsoleSpecPermission) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	writePMSpecJSON(w, http.StatusOK, map[string]any{"spec_id": r.PathValue("specID"), "revision": revision, "action": in.Action})
}

func (h *Hub) HandlePMSpecAssistAction(w http.ResponseWriter, r *http.Request) {
	if h.pmSpecFlowUnavailable(w, r) {
		return
	}
	repo, _ := h.pmSpecFlowStore()
	actor, specID := platformPrincipal(r), r.PathValue("specID")
	switch r.PathValue("action") {
	case "get":
		q, err := repo.PMSpecAssistRequest(r.Context(), r.PathValue("requestID"))
		if err != nil || q == nil || q.ActorID != actor {
			http.NotFound(w, r)
			return
		}
		draft, err := repo.GetPMSpecWorkingDraft(r.Context(), specID)
		if err != nil || draft == nil || draft.CurrentEditorID != actor {
			http.NotFound(w, r)
			return
		}
		if q.Status == "pending" {
			reason := ""
			switch {
			case q.HubBoot != h.specAgentBoot():
				reason = "Hub restarted"
			case h.now().UTC().Unix() >= q.Deadline:
				reason = "request expired"
			case draft.Version != q.WorkingVersion || draft.ContentSHA256 != q.ContentSHA256:
				reason = "working draft changed"
			}
			if reason != "" {
				if err = repo.MarkPMSpecAssistStale(r.Context(), q.RequestID, actor, reason, h.now()); err != nil {
					http.Error(w, "assist status unavailable", http.StatusServiceUnavailable)
					return
				}
				q.Status = "stale"
			}
		}
		writePMSpecJSON(w, http.StatusOK, map[string]any{"request_id": q.RequestID, "status": q.Status, "working_version": q.WorkingVersion, "content_sha256": q.ContentSHA256, "result": json.RawMessage(q.ResultJSON)})
	case "start":
		var in struct {
			AgentID string `json:"agent_id"`
			Version int    `json:"working_version"`
			SHA     string `json:"content_sha256"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.AgentID == "" || in.Version < 1 || in.SHA == "" {
			http.Error(w, "invalid assist request", 400)
			return
		}
		requestID := "pmspecassist_" + randomHex(16)
		draft, err := repo.GetPMSpecWorkingDraft(r.Context(), specID)
		if err != nil || draft == nil || draft.Version != in.Version || draft.ContentSHA256 != in.SHA {
			http.Error(w, "working draft changed", http.StatusConflict)
			return
		}
		q := store.SpecAgentRequest{ID: requestID, SpecID: specID, Actor: actor, AgentID: in.AgentID}
		var dispatchErr error
		dispatchErr = h.withSpecAgent(r.Context(), &q, func(a model.ConsoleAgent, check func() error) error {
			clientFound, capable := h.specAgentCapable(a.KeyID, a.SessionName)
			if !clientFound || !capable {
				return errors.New("spec_agent_upgrade_required")
			}
			h.mu.Lock()
			client := h.sessionClients[a.KeyID][a.SessionName]
			h.mu.Unlock()
			if client == nil {
				return errors.New("agent_offline")
			}
			client.stateMu.Lock()
			pmCapable := client.pmSpecFlowV1
			client.stateMu.Unlock()
			if !pmCapable {
				return errors.New("pm_spec_flow_upgrade_required")
			}
			if err := check(); err != nil {
				return err
			}
			if err := repo.StartPMSpecAssist(r.Context(), specID, actor, in.AgentID, requestID, h.specAgentBoot(), in.Version, in.SHA, h.now()); err != nil {
				return err
			}
			payload := map[string]any{"request_id": requestID, "spec_id": specID, "working_version": draft.Version, "content_sha256": draft.ContentSHA256, "title": draft.Title, "content_markdown": draft.ContentMarkdown, "acceptance": json.RawMessage(draft.AcceptanceJSON), "source_refs": json.RawMessage(draft.SourceRefsJSON), "norm_anchors": json.RawMessage(draft.NormAnchorsJSON), "instruction": "Return structured suggestions and review questions only; do not edit or finalize."}
			raw, err := json.Marshal(payload)
			if err != nil {
				_ = repo.FailPMSpecAssist(r.Context(), requestID, "encode failed", h.now())
				return err
			}
			env := model.Envelope{ID: requestID, From: sessionAddress(systemAddressName, a.KeyID), To: sessionAddress(a.SessionName, a.KeyID), TS: h.now().Unix(), Body: string(raw), Meta: map[string]any{"type": "console_spec_pm_assist_request", "spec_review_type": "console_spec_pm_assist_request", "system": true, "no_mirror": true, "request_id": requestID, "reply_policy": "no_reply"}}
			if err = h.RouteEnvelope(context.WithValue(r.Context(), specAgentDispatchContextKey{}, true), env); err != nil {
				_ = repo.FailPMSpecAssist(r.Context(), requestID, "dispatch failed", h.now())
				return err
			}
			return nil
		})
		if err := dispatchErr; err != nil {
			status := http.StatusConflict
			if err.Error() == "agent_offline" || err.Error() == "pm_spec_flow_upgrade_required" || err.Error() == "spec_agent_upgrade_required" {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		writePMSpecJSON(w, http.StatusAccepted, map[string]any{"request_id": requestID, "status": "pending"})
	case "decide":
		var in struct {
			Decisions []store.PMSpecFlowDecision `json:"decisions"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil {
			http.Error(w, "invalid decisions", 400)
			return
		}
		if err := repo.DecidePMSpecAssist(r.Context(), r.PathValue("requestID"), actor, in.Decisions, h.now()); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "return":
		var in struct {
			Objection string `json:"objection"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 20<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil {
			http.Error(w, "objection required", 400)
			return
		}
		if err := repo.ReturnPMSpecReview(r.Context(), specID, actor, in.Objection, h.now()); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (h *Hub) handlePMSpecAssistResult(ctx context.Context, requestID string, c *sessionClient, body string) bool {
	repo, ok := h.pmSpecFlowStore()
	if !ok {
		return true
	}
	q, err := repo.PMSpecAssistRequest(ctx, requestID)
	if err != nil || q == nil || q.AgentID == "" || q.HubBoot != h.specAgentBoot() || q.Deadline < h.now().Unix() || q.Status != "pending" {
		return true
	}
	specReq := store.SpecAgentRequest{ID: q.RequestID, SpecID: q.SpecID, Actor: q.ActorID, AgentID: q.AgentID}
	_ = h.withSpecAgent(ctx, &specReq, func(a model.ConsoleAgent, check func() error) error {
		if err := check(); err != nil {
			return err
		}
		if a.ID != q.AgentID || a.KeyID != c.keyID || a.SessionName != c.sessionName {
			return errors.New("assist agent mismatch")
		}
		return repo.CompletePMSpecAssist(ctx, q.RequestID, a.ID, json.RawMessage(body), h.now())
	})
	return true
}

func (h *Hub) HandleAgentPMSpecAssistResult(w http.ResponseWriter, r *http.Request) {
	if h.pmSpecFlowUnavailable(w, r) {
		return
	}
	repo, _ := h.pmSpecFlowStore()
	agentID, _ := r.Context().Value(taskAgentContextKey{}).(string)
	if agentID == "" {
		taskJSON(w, 401, map[string]string{"error": "agent_auth_required"})
		return
	}
	var in struct {
		Result json.RawMessage `json:"result"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil {
		http.Error(w, "invalid result", 400)
		return
	}
	q, err := repo.PMSpecAssistRequest(r.Context(), r.PathValue("requestID"))
	if err != nil || q == nil || q.AgentID != agentID || q.HubBoot != h.specAgentBoot() || q.Deadline < h.now().Unix() {
		http.Error(w, "request unavailable", http.StatusConflict)
		return
	}
	specReq := store.SpecAgentRequest{ID: q.RequestID, SpecID: q.SpecID, Actor: q.ActorID, AgentID: q.AgentID}
	err = h.withSpecAgent(r.Context(), &specReq, func(a model.ConsoleAgent, check func() error) error {
		if err := check(); err != nil {
			return err
		}
		return repo.CompletePMSpecAssist(r.Context(), q.RequestID, a.ID, in.Result, h.now())
	})
	if err != nil {
		http.Error(w, "result rejected", http.StatusConflict)
		return
	}
	writePMSpecJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (h *Hub) HandleAgentPMSpecAssistRequest(w http.ResponseWriter, r *http.Request) {
	if h.pmSpecFlowUnavailable(w, r) {
		return
	}
	repo, _ := h.pmSpecFlowStore()
	agentID, _ := r.Context().Value(taskAgentContextKey{}).(string)
	q, err := repo.PMSpecAssistRequest(r.Context(), r.PathValue("requestID"))
	if err != nil || q == nil || q.AgentID != agentID || q.Status != "pending" || q.HubBoot != h.specAgentBoot() || q.Deadline < h.now().Unix() {
		http.NotFound(w, r)
		return
	}
	specReq := store.SpecAgentRequest{ID: q.RequestID, SpecID: q.SpecID, Actor: q.ActorID, AgentID: q.AgentID}
	var draft *store.PMSpecWorkingDraft
	err = h.withSpecAgent(r.Context(), &specReq, func(a model.ConsoleAgent, check func() error) error {
		if a.ID != agentID {
			return errors.New("assist identity mismatch")
		}
		if err := check(); err != nil {
			return err
		}
		var e error
		draft, e = repo.GetPMSpecWorkingDraft(r.Context(), q.SpecID)
		if e != nil {
			return e
		}
		if draft == nil || draft.CurrentEditorID != q.ActorID || draft.Version != q.WorkingVersion || draft.ContentSHA256 != q.ContentSHA256 || (draft.State != "editing" && draft.State != "review") {
			return store.ErrPMSpecFlowStale
		}
		return nil
	})
	if err != nil || draft == nil {
		http.NotFound(w, r)
		return
	}
	writePMSpecJSON(w, http.StatusOK, map[string]any{"request_id": q.RequestID, "spec_id": q.SpecID, "working_version": draft.Version, "content_sha256": draft.ContentSHA256, "title": draft.Title, "content_markdown": draft.ContentMarkdown, "acceptance": json.RawMessage(draft.AcceptanceJSON), "source_refs": json.RawMessage(draft.SourceRefsJSON), "norm_anchors": json.RawMessage(draft.NormAnchorsJSON)})
}

func writePMSpecJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
