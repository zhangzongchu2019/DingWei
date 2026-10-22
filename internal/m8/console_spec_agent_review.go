package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type specAgentRepository interface {
	StartSpecAgentRequest(context.Context, store.SpecAgentRequest, func() error, time.Time) (*store.SpecAgentRequest, error)
	SpecAgentRequest(context.Context, string) (*store.SpecAgentRequest, error)
	ListSpecAgentRequests(context.Context, string, string, string, string) ([]store.SpecAgentRequest, error)
	ReconcileSpecAgentRequests(context.Context, string, time.Time) error
	CompleteSpecAgentRequest(context.Context, string, model.ConsoleAgent, store.SpecAgentResult, func() error, time.Time) (*store.SpecAgentRequest, error)
	ConfirmSpecAgentCriteria(context.Context, string, string, int, string, func() error, time.Time) (int, error)
	CancelSpecAgentRequest(context.Context, string, string, time.Time) error
	CancelTransferSpecAgent(context.Context, string, string, string, string, string, int, string, time.Time) error
	FailSpecAgentRequest(context.Context, string, string, time.Time) error
	MarkSpecAgentSent(context.Context, string) error
	PendingSpecAgentSends(context.Context) ([]string, error)
}
type specAgentDispatchContextKey struct{}

func (h *Hub) specAgentBoot() string { return h.startedAt.UTC().Format(time.RFC3339Nano) }
func specReviewEnvelopeType(env model.Envelope) string {
	t := metaString(env.Meta, "spec_review_type")
	if specReviewType(t) {
		return t
	}
	return metaString(env.Meta, "type")
}
func specReviewType(t string) bool {
	return t == "console_spec_review_request" || t == "console_spec_criteria_request" || t == "console_spec_pm_assist_request"
}
func specAgentError(w http.ResponseWriter, e error) {
	status := 503
	code := "spec_review_unavailable"
	switch {
	case errors.Is(e, store.ErrConsoleSpecPermission), errors.Is(e, identity.ErrForbidden), errors.Is(e, identity.ErrNotFound):
		status = 403
		code = "spec_review_access_denied"
	case errors.Is(e, store.ErrSpecAgentInvalid):
		status = 400
		code = "invalid_spec_review_result"
	case errors.Is(e, store.ErrSpecAgentConflict), errors.Is(e, store.ErrConsoleSpecRevisionLocked), strings.Contains(e.Error(), "console spec agent request locked"), strings.Contains(e.Error(), "UNIQUE constraint"):
		status = 409
		code = "spec_review_conflict"
	}
	v := map[string]any{"error": code}
	if status >= 500 {
		v["outcome"] = "unknown"
		v["retryable"] = false
	}
	taskJSON(w, status, v)
}
func decodeSpecAgent(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		specAgentError(w, store.ErrSpecAgentInvalid)
		return false
	}
	var extra any
	if errors.Is(d.Decode(&extra), io.EOF) {
		return true
	}
	specAgentError(w, store.ErrSpecAgentInvalid)
	return false
}
func (h *Hub) withSpecAgent(ctx context.Context, q *store.SpecAgentRequest, fn func(model.ConsoleAgent, func() error) error) error {
	if h.Identity == nil {
		return identity.ErrForbidden
	}
	auth, ok := h.Identity.Store.(identity.SpecReviewAuthorizationStore)
	if !ok {
		return errors.New("owned Agent authorization unavailable")
	}
	return auth.WithOwnedAgentAuthorization(ctx, q.Actor, q.AgentID, func(a identity.Agent, check func() error) error {
		// The endpoint is derived from locked identity, never the callback's self-description.
		key, name := AgentKeyID(a.OwnerLC), a.NameLC()
		if (q.KeyID != "" && q.KeyID != key) || (q.SessionName != "" && q.SessionName != name) {
			return identity.ErrForbidden
		}
		return fn(model.ConsoleAgent{ID: a.ID, KeyID: key, SessionName: name, OwnerPrincipal: q.Actor, Active: true}, check)
	})
}
func (h *Hub) specAgentCapable(key, name string) (bool, bool) {
	h.mu.Lock()
	c := h.sessionClients[key][name]
	h.mu.Unlock()
	if c == nil {
		return false, false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return true, c.specReviewV1 && c.deliveryACKV1
}
func (h *Hub) HandleConsoleSpecAgentRequest(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.Repo.(specAgentRepository)
	base, bok := h.consoleSpecs()
	if !ok || !bok {
		specAgentError(w, errors.New("store unavailable"))
		return
	}
	actor := platformPrincipal(r)
	spec := r.PathValue("specID")
	if e := repo.ReconcileSpecAgentRequests(r.Context(), h.specAgentBoot(), h.now()); e != nil {
		specAgentError(w, e)
		return
	}
	current, e := base.ConsoleSpec(r.Context(), actor, spec, false)
	if e != nil || current == nil || current.CurrentEditorID != actor {
		specAgentError(w, store.ErrConsoleSpecPermission)
		return
	}
	if r.Method == http.MethodGet {
		agents, e := base.ListOwnedConsoleAgents(r.Context(), actor)
		if e != nil {
			specAgentError(w, e)
			return
		}
		online := []model.ConsoleAgent{}
		for _, a := range agents {
			if yes, cap := h.specAgentCapable(a.KeyID, a.SessionName); yes && cap {
				online = append(online, a)
			}
		}
		items, e := repo.ListSpecAgentRequests(r.Context(), "", actor, spec, "")
		if e != nil {
			specAgentError(w, e)
			return
		}
		for i := range items {
			items[i].Snapshot = nil
			if items[i].Status != "awaiting_confirmation" {
				items[i].Result = nil
			}
		}
		taskJSON(w, 200, map[string]any{"requests": items, "agents": online})
		return
	}
	var in struct {
		AgentID  string `json:"agent_id"`
		Revision int    `json:"expected_revision"`
		SHA      string `json:"content_sha256"`
		ClientID string `json:"client_request_id"`
	}
	if !decodeSpecAgent(w, r, &in) {
		return
	}
	kind := "review"
	if r.PathValue("kind") == "agent-criteria" {
		kind = "criteria"
	}
	q := store.SpecAgentRequest{ID: "specreview_" + randomHex(16), SpecID: spec, Revision: in.Revision, SHA: in.SHA, Kind: kind, Actor: actor, AgentID: in.AgentID, ClientID: in.ClientID, Boot: h.specAgentBoot()}
	var created *store.SpecAgentRequest
	e = h.withSpecAgent(r.Context(), &q, func(a model.ConsoleAgent, check func() error) error {
		yes, cap := h.specAgentCapable(a.KeyID, a.SessionName)
		if !yes {
			return errors.New("agent_offline")
		}
		if !cap {
			return errors.New("helper_upgrade_required")
		}
		var e error
		created, e = repo.StartSpecAgentRequest(r.Context(), q, check, h.now())
		return e
	})
	if e != nil {
		if e.Error() == "agent_offline" || e.Error() == "helper_upgrade_required" {
			taskJSON(w, 409, map[string]any{"error": e.Error()})
			return
		}
		specAgentError(w, e)
		return
	}
	if created.Status == "pending" {
		if e = h.sendSpecAgentRequest(r.Context(), created); e != nil {
			_ = repo.FailSpecAgentRequest(r.Context(), created.ID, e.Error(), h.now())
			taskJSON(w, 502, map[string]any{"error": "spec_review_dispatch_failed", "request_id": created.ID})
			return
		}
	}
	taskJSON(w, 202, created)
}
func (h *Hub) sendSpecAgentRequest(ctx context.Context, q *store.SpecAgentRequest) error {
	repo := h.Repo.(specAgentRepository)
	return h.withSpecAgent(ctx, q, func(a model.ConsoleAgent, check func() error) error {
		latest, e := repo.SpecAgentRequest(ctx, q.ID)
		if e != nil {
			return e
		}
		if latest.Status != "pending" || latest.Deadline <= h.now().Unix() || latest.Boot != h.specAgentBoot() {
			return store.ErrSpecAgentConflict
		}
		base, _ := h.consoleSpecs()
		spec, e := base.ConsoleSpec(ctx, q.Actor, q.SpecID, false)
		if e != nil || spec == nil || spec.CurrentEditorID != q.Actor || spec.CurrentRevision != q.Revision {
			return store.ErrConsoleSpecPermission
		}
		yes, cap := h.specAgentCapable(a.KeyID, a.SessionName)
		if !yes {
			return errors.New("agent_offline")
		}
		if !cap {
			return errors.New("helper_upgrade_required")
		}
		raw, e := json.Marshal(latest)
		if e != nil {
			return e
		}
		typ := "console_spec_" + q.Kind + "_request"
		env := model.Envelope{ID: q.ID, From: sessionAddress(systemAddressName, a.KeyID), To: sessionAddress(a.SessionName, a.KeyID), TS: h.now().Unix(), Body: string(raw), Meta: map[string]any{"type": typ, "spec_review_type": typ, "system": true, "no_mirror": true, "request_id": q.ID, "reply_policy": "no_reply"}}
		if e = check(); e != nil {
			return e
		}
		if e = h.RouteEnvelope(context.WithValue(ctx, specAgentDispatchContextKey{}, true), env); e != nil {
			return e
		}
		return repo.MarkSpecAgentSent(ctx, q.ID)
	})
}
func (h *Hub) HandleConsoleSpecAgentConfirm(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.Repo.(specAgentRepository)
	if !ok {
		specAgentError(w, errors.New("store unavailable"))
		return
	}
	if e := repo.ReconcileSpecAgentRequests(r.Context(), h.specAgentBoot(), h.now()); e != nil {
		specAgentError(w, e)
		return
	}
	q, e := repo.SpecAgentRequest(r.Context(), r.PathValue("requestID"))
	if e != nil {
		specAgentError(w, e)
		return
	}
	if q.SpecID != r.PathValue("specID") || q.Actor != platformPrincipal(r) {
		specAgentError(w, store.ErrConsoleSpecPermission)
		return
	}
	if r.PathValue("action") != "cancel" && r.PathValue("action") != "confirm" {
		http.NotFound(w, r)
		return
	}
	if r.PathValue("action") == "cancel" {
		e = repo.CancelSpecAgentRequest(r.Context(), q.ID, platformPrincipal(r), h.now())
		if e != nil {
			specAgentError(w, e)
			return
		}
		w.WriteHeader(204)
		return
	}
	var in struct {
		Revision int    `json:"expected_revision"`
		SHA      string `json:"content_sha256"`
	}
	if !decodeSpecAgent(w, r, &in) {
		return
	}
	var next int
	e = h.withSpecAgent(r.Context(), q, func(_ model.ConsoleAgent, check func() error) error {
		var e error
		next, e = repo.ConfirmSpecAgentCriteria(r.Context(), q.ID, platformPrincipal(r), in.Revision, in.SHA, check, h.now())
		return e
	})
	if e != nil {
		specAgentError(w, e)
		return
	}
	taskJSON(w, 200, map[string]any{"revision": next})
}

// Check the current spec editor and bound Agent again inside the PG authorization lock.
func (h *Hub) readSpecAgentAuthorized(ctx context.Context, q *store.SpecAgentRequest, actor string) (*store.SpecAgentRequest, error) {
	repo := h.Repo.(specAgentRepository)
	var out *store.SpecAgentRequest
	if actor != q.AgentID {
		return nil, store.ErrConsoleSpecPermission
	}
	e := h.withSpecAgent(ctx, q, func(_ model.ConsoleAgent, check func() error) error {
		current, e := repo.SpecAgentRequest(ctx, q.ID)
		if e != nil {
			return e
		}
		if current.AgentID != actor || current.Actor != q.Actor || current.Status == "cancelled" || current.Status == "failed" || current.Status == "timed_out" || current.Deadline <= h.now().Unix() {
			return store.ErrSpecAgentConflict
		}
		base, _ := h.consoleSpecs()
		spec, e := base.ConsoleSpec(ctx, q.Actor, q.SpecID, false)
		if e != nil {
			return e
		}
		if spec == nil || spec.CurrentEditorID != q.Actor || spec.CurrentRevision != q.Revision {
			return store.ErrConsoleSpecPermission
		}
		if e = check(); e != nil {
			return e
		}
		out = current
		return nil
	})
	return out, e
}
func (h *Hub) HandleAgentSpecReview(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.Repo.(specAgentRepository)
	if !ok {
		specAgentError(w, errors.New("store unavailable"))
		return
	}
	actor, _ := r.Context().Value(taskAgentContextKey{}).(string)
	if actor == "" {
		specAgentError(w, identity.ErrForbidden)
		return
	}
	if e := repo.ReconcileSpecAgentRequests(r.Context(), h.specAgentBoot(), h.now()); e != nil {
		specAgentError(w, e)
		return
	}
	if r.Method == http.MethodGet {
		if len(r.URL.Query().Get("cursor")) > 256 || len(r.URL.Query().Get("request_id")) > 256 {
			specAgentError(w, store.ErrSpecAgentInvalid)
			return
		}
		qs, e := repo.ListSpecAgentRequests(r.Context(), actor, "", "", r.URL.Query().Get("cursor"))
		if id := r.URL.Query().Get("request_id"); id != "" {
			q, err := repo.SpecAgentRequest(r.Context(), id)
			if err != nil {
				specAgentError(w, err)
				return
			}
			qs = []store.SpecAgentRequest{*q}
		}
		if e != nil {
			specAgentError(w, e)
			return
		}
		out := []store.SpecAgentRequest{}
		id := r.URL.Query().Get("request_id")
		for i := range qs {
			q := &qs[i]
			if id != "" && q.ID != id {
				continue
			}
			v, e := h.readSpecAgentAuthorized(r.Context(), q, actor)
			if e != nil {
				if errors.Is(e, store.ErrSpecAgentConflict) || errors.Is(e, store.ErrConsoleSpecPermission) || errors.Is(e, identity.ErrForbidden) {
					continue
				}
				specAgentError(w, e)
				return
			}
			if id == "" {
				v.Snapshot = nil
				v.Result = nil
			}
			out = append(out, *v)
		}
		next := ""
		if len(qs) == 100 {
			next = qs[len(qs)-1].ID
		}
		taskJSON(w, 200, map[string]any{"requests": out, "next_cursor": next})
		return
	}
	var result store.SpecAgentResult
	if !decodeSpecAgent(w, r, &result) {
		return
	}
	q, e := repo.SpecAgentRequest(r.Context(), r.PathValue("requestID"))
	if e != nil {
		specAgentError(w, e)
		return
	}
	if q.AgentID != actor {
		specAgentError(w, store.ErrConsoleSpecPermission)
		return
	}
	var out *store.SpecAgentRequest
	e = h.withSpecAgent(r.Context(), q, func(a model.ConsoleAgent, check func() error) error {
		var e error
		out, e = repo.CompleteSpecAgentRequest(r.Context(), q.ID, a, result, check, h.now())
		return e
	})
	if e != nil {
		specAgentError(w, e)
		return
	}
	taskJSON(w, 200, out)
}
func (h *Hub) handleSpecAgentReviewResult(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	typ := metaString(env.Meta, "type")
	if typ == "console_spec_pm_assist_result" {
		return h.handlePMSpecAssistResult(ctx, metaString(env.Meta, "request_id"), c, env.Body)
	}
	if typ != "console_spec_review_result" && typ != "console_spec_criteria_result" {
		return false
	}
	repo, ok := h.Repo.(specAgentRepository)
	if !ok {
		return true
	}
	q, e := repo.SpecAgentRequest(ctx, metaString(env.Meta, "request_id"))
	if e != nil || q.KeyID != c.keyID || q.SessionName != c.sessionName || q.Boot != h.specAgentBoot() || typ != "console_spec_"+q.Kind+"_result" || len(env.Body) > 128<<10 {
		return true
	}
	var result store.SpecAgentResult
	d := json.NewDecoder(bytes.NewBufferString(env.Body))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil {
		return true
	}
	var trailing any
	if !errors.Is(d.Decode(&trailing), io.EOF) {
		return true
	}
	_ = h.withSpecAgent(ctx, q, func(a model.ConsoleAgent, check func() error) error {
		_, e := repo.CompleteSpecAgentRequest(ctx, q.ID, a, result, check, h.now())
		return e
	})
	return true
}
func (h *Hub) reconcileSpecAgent(ctx context.Context) {
	repo, ok := h.Repo.(specAgentRepository)
	if !ok {
		return
	}
	if repo.ReconcileSpecAgentRequests(ctx, h.specAgentBoot(), h.now()) != nil {
		return
	}
	cursor := ""
	for {
		rows, e := repo.ListSpecAgentRequests(ctx, "", "", "", cursor)
		if e != nil {
			return
		}
		if len(rows) == 0 {
			break
		}
		cursor = rows[len(rows)-1].ID
		for i := range rows {
			q := &rows[i]
			if q.Status != "pending" && q.Status != "awaiting_confirmation" {
				continue
			}
			_, e := h.readSpecAgentAuthorized(ctx, q, q.AgentID)
			if errors.Is(e, identity.ErrForbidden) || errors.Is(e, identity.ErrNotFound) || errors.Is(e, store.ErrConsoleSpecPermission) {
				_ = repo.FailSpecAgentRequest(ctx, q.ID, "identity or editor revoked", h.now())
			}
		}
	}
	ids, e := repo.PendingSpecAgentSends(ctx)
	if e != nil {
		return
	}
	for _, id := range ids {
		q, e := repo.SpecAgentRequest(ctx, id)
		if e != nil {
			continue
		}
		if e = h.sendSpecAgentRequest(ctx, q); e != nil {
			_ = repo.FailSpecAgentRequest(ctx, id, e.Error(), h.now())
		}
	}
}
