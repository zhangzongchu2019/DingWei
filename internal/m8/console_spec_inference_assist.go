package m8

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const specInferenceAssistRequestType = "console_spec_inference_assist_request"

func (h *Hub) HandleConsoleSpecInferenceAssist(w http.ResponseWriter, r *http.Request) {
	var releaseLegacySpec func()
	var legacySpecAllowed bool
	r, releaseLegacySpec, legacySpecAllowed = h.legacySpecMutationRequest(w, r)
	if !legacySpecAllowed {
		return
	}
	defer releaseLegacySpec()

	repo, ok := h.consoleSpecs()
	authRepo, authOK := h.Repo.(consoleAuthRepository)
	session, sessionOK := r.Context().Value(consoleSessionContextKey{}).(model.ConsoleSession)
	if !ok || !authOK || !sessionOK {
		http.Error(w, "spec Agent assist unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		AgentID string `json:"agent_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || strings.TrimSpace(body.AgentID) == "" {
		http.Error(w, "agent_id required", http.StatusBadRequest)
		return
	}
	principal, specID := platformPrincipal(r), r.PathValue("specID")
	spec, err := repo.ConsoleSpec(r.Context(), principal, specID, false)
	if err != nil || spec == nil || spec.PrincipalID != principal || spec.CurrentEditorID != principal || spec.State == "final" {
		http.Error(w, "spec is not editable by current principal", http.StatusConflict)
		return
	}
	revision, err := repo.ConsoleSpecRevision(r.Context(), principal, specID, spec.CurrentRevision, false)
	if err != nil || revision == nil {
		http.Error(w, "spec revision unavailable", http.StatusServiceUnavailable)
		return
	}
	agents, err := repo.ListOwnedConsoleAgents(r.Context(), principal)
	if err != nil {
		http.Error(w, "spec Agent list unavailable", http.StatusServiceUnavailable)
		return
	}
	var selected *model.ConsoleAgent
	for i := range agents {
		if agents[i].ID == body.AgentID {
			selected = &agents[i]
			break
		}
	}
	if selected == nil {
		http.Error(w, "selected Agent is not owned by current principal", http.StatusForbidden)
		return
	}
	if !h.consoleAgentOnline(r.Context(), selected.ID, principal) {
		http.Error(w, "所选 Agent 当前离线，无法生成推论", http.StatusConflict)
		return
	}
	capabilityRaw, err := randomConsoleToken()
	if err != nil {
		http.Error(w, "spec Agent capability unavailable", http.StatusServiceUnavailable)
		return
	}
	capabilityID, err := randomConsoleToken()
	if err != nil {
		http.Error(w, "spec Agent capability unavailable", http.StatusServiceUnavailable)
		return
	}
	now := h.now()
	pageID := "console-spec:" + specID
	capability := model.ConsolePageCapability{ID: capabilityID, SessionID: session.ID, PrincipalID: principal, AgentID: selected.ID, PageID: pageID, TokenHash: hashConsoleToken(capabilityRaw), ExpiresAt: now.Add(5 * time.Minute)}
	if err := authRepo.RotateConsolePageCapability(r.Context(), capability, selected.KeyID, selected.SessionName, now); err != nil {
		http.Error(w, "spec Agent capability rejected", http.StatusForbidden)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"spec_id": specID, "revision": revision.Revision, "content_markdown": revision.ContentMarkdown,
		"instruction": "基于正文生成一条待业主确认的推论，并 POST 到 callback；署名只由你的服务凭据与 capability 决定。",
		"callback":    "/api/agent/console/specs/" + specID + "/inferences", "capability": capabilityRaw,
	})
	env := model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, selected.KeyID), To: sessionAddress(selected.SessionName, selected.KeyID), TS: now.Unix(), Body: string(payload), Meta: map[string]any{"type": specInferenceAssistRequestType, "system": true, "no_mirror": true}}
	dispatch := h.specInferenceDispatch
	if dispatch == nil {
		dispatch = func(ctx context.Context, _ model.ConsoleAgent, env model.Envelope) error {
			return h.RouteEnvelope(ctx, env)
		}
	}
	if err := dispatch(r.Context(), *selected, env); err != nil {
		http.Error(w, "Agent 未接受生成推论请求", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"agent_id": selected.ID, "status": "requested"})
}

func (h *Hub) HandleConsoleSpecAgentInference(w http.ResponseWriter, r *http.Request) {
	var releaseLegacySpec func()
	var legacySpecAllowed bool
	r, releaseLegacySpec, legacySpecAllowed = h.legacySpecMutationRequest(w, r)
	if !legacySpecAllowed {
		return
	}
	defer releaseLegacySpec()

	repo, ok := h.consoleSpecs()
	if !ok {
		http.Error(w, "console specs unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Text           string `json:"text"`
		ClaimedAgentID string `json:"claimed_agent_id,omitempty"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || strings.TrimSpace(body.Text) == "" {
		http.Error(w, "inference text required", http.StatusBadRequest)
		return
	}
	principal, specID, actor := platformPrincipal(r), r.PathValue("specID"), authenticatedConsoleSpecActor(r)
	if actor.Kind != "agent" {
		http.Error(w, "Agent capability required", http.StatusForbidden)
		return
	}
	spec, err := repo.ConsoleSpec(r.Context(), principal, specID, false)
	if err != nil || spec == nil || spec.CurrentEditorID != principal || spec.State == "final" {
		http.Error(w, "spec is not editable", http.StatusConflict)
		return
	}
	revision, err := repo.ConsoleSpecRevision(r.Context(), principal, specID, spec.CurrentRevision, false)
	if err != nil || revision == nil {
		http.Error(w, "spec revision unavailable", http.StatusServiceUnavailable)
		return
	}
	var bundle specAcceptanceBundle
	if json.Unmarshal([]byte(revision.AcceptanceJSON), &bundle) != nil {
		http.Error(w, "spec acceptance unavailable", http.StatusServiceUnavailable)
		return
	}
	bundle.Inferences = append(bundle.Inferences, specInference{ID: "I" + strconv.Itoa(len(bundle.Inferences)+1), Text: strings.TrimSpace(body.Text), GeneratedBy: actor.ID, Status: "pending_owner_confirmation"})
	acceptance, _ := json.Marshal(bundle)
	next := *revision
	next.Revision++
	next.AcceptanceJSON = string(acceptance)
	next.CreatedBy, next.CreatedByKind = actor.ID, actor.Kind
	if _, err := repo.AppendConsoleSpecRevision(r.Context(), principal, specID, revision.Revision, next, h.now()); err != nil {
		code := http.StatusConflict
		if errors.Is(err, store.ErrConsoleSpecEditDenied) {
			code = http.StatusForbidden
		}
		http.Error(w, err.Error(), code)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
