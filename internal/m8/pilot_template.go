package m8

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"io"
	"net/http"
	"strings"
	"time"
)

type templateRepository interface {
	TemplateSource(context.Context, string, string) (store.TemplateSource, error)
	TemplateSources(context.Context, string, string) ([]store.TemplateSource, error)
	SaveTemplateSource(context.Context, string, string, store.TemplateSource, func() error, time.Time) (store.TemplateSource, error)
	TemplateParents(context.Context, string, string) ([]map[string]any, error)
	TemplateReferences(context.Context, string) (store.TemplateReferences, error)
	TemplateCandidateIDs(context.Context, store.BlockerActor, string) ([]string, error)
	ReadTemplateDraft(context.Context, store.TemplateReferences, store.BlockerActor, store.TemplatePrincipalSnapshot, func() error, time.Time) (store.TemplateView, error)
	SaveTemplateDraft(context.Context, string, string, store.TemplateOperation, store.TemplatePrincipalSnapshot, func() error, time.Time) (store.TemplateView, error)
	UpdateTemplateResponsibility(context.Context, store.TemplateReferences, store.BlockerActor, store.TemplateOperation, store.TemplatePrincipalSnapshot, func() error, time.Time) (store.TemplateView, error)
	ConfirmTemplateDraft(context.Context, store.TemplateReferences, string, store.TemplateOperation, store.TemplatePrincipalSnapshot, func() error, time.Time) (store.TemplateView, error)
}

func (h *Hub) templateRepo() (templateRepository, error) {
	v, ok := h.Repo.(templateRepository)
	if !ok || h.Identity == nil {
		return nil, errors.New("template store unavailable")
	}
	return v, nil
}
func templateDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 768<<10))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		taskError(w, store.ErrTaskFlowInvalid)
		return false
	}
	return true
}
func templateSnapshot(a identity.ContextAuthorization) store.TemplatePrincipalSnapshot {
	s := store.TemplatePrincipalSnapshot{Active: map[string]bool{}, AgentOwners: map[string]string{}}
	for id, ok := range a.Users {
		s.Active["user:"+id] = ok
	}
	for id, x := range a.Agents {
		s.Active["agent:"+id] = a.Active(id)
		s.AgentOwners[id] = x.OwnerLC
	}
	return s
}
func (h *Hub) withTemplateIdentity(ctx context.Context, c blockerCredential, ref store.TemplateReferences, doc *store.TemplateDocument, fn func(store.TemplatePrincipalSnapshot, func() error) error) error {
	auth, ok := h.Identity.Store.(identity.TemplateAuthorizationStore)
	if !ok {
		return identity.ErrForbidden
	}
	users := []string{ref.Owner, c.actor.UserID}
	agents := []string{}
	docs := []store.TemplateDocument{ref.Document}
	if doc != nil {
		docs = append(docs, *doc)
	}
	for _, d := range docs {
		for _, entry := range d.Entries {
			for _, p := range []string{entry.Fields.Executor, entry.Fields.Reviewer, entry.Fields.Signoff, d.Defaults.Executor, d.Defaults.Reviewer, d.Defaults.Signoff, d.Full.ReviewPrincipal} {
				if strings.HasPrefix(p, "user:") {
					users = append(users, strings.TrimPrefix(p, "user:"))
				} else if strings.HasPrefix(p, "agent:") {
					agents = append(agents, strings.TrimPrefix(p, "agent:"))
				}
			}
		}
	}
	return auth.WithTemplateAuthorization(ctx, c.actor.AgentID, c.hash, c.actor.UserID, users, agents, ref.ID, func(a identity.ContextAuthorization, check func() error) error {
		if ref.Owner != "" && !a.Users[identity.Lower(ref.Owner)] {
			return identity.ErrForbidden
		}
		return fn(templateSnapshot(a), check)
	})
}
func templateAfter(r *http.Request) (string, error) {
	q := r.URL.Query()
	for k, v := range q {
		if k != "after" || len(v) != 1 || len(v[0]) > 256 {
			return "", store.ErrTaskFlowInvalid
		}
	}
	return q.Get("after"), nil
}

// The source import has no arbitrary URL fetch or borrowed-cookie path. Until a
// separately configured, authorized connector exists, link-only imports are
// explicitly pending_config; a provenance-labelled manual export remains usable.
func (h *Hub) HandleTemplateSource(w http.ResponseWriter, r *http.Request) {
	repo, e := h.templateRepo()
	if e != nil {
		taskError(w, e)
		return
	}
	c, e := questionCredential(r)
	if e != nil || c.actor.UserID == "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var input struct {
		RequestID string `json:"request_id"`
		URL       string `json:"source_url"`
		Revision  string `json:"source_revision"`
		Content   string `json:"content_markdown"`
		Mode      string `json:"mode"`
	}
	if r.Method == "POST" && !templateDecode(w, r, &input) {
		return
	}
	var out any
	e = h.withTemplateIdentity(ctx, c, store.TemplateReferences{Owner: c.actor.UserID}, nil, func(_ store.TemplatePrincipalSnapshot, check func() error) error {
		var err error
		if r.Method == "GET" {
			after, x := templateAfter(r)
			if x != nil {
				return x
			}
			if id := r.PathValue("sourceID"); id != "" {
				out, err = repo.TemplateSource(ctx, c.actor.UserID, id)
			} else {
				out, err = repo.TemplateSources(ctx, c.actor.UserID, after)
			}
			if err == nil {
				err = check()
			}
			return err
		}
		if r.Method != "POST" {
			return store.ErrTaskFlowInvalid
		}
		kind := "pending_connector"
		if input.Mode == "manual_export" {
			kind = "manual_export"
		} else if input.Mode != "connector" {
			return store.ErrTaskFlowInvalid
		}
		if kind == "pending_connector" && (input.Content != "" || input.Revision != "") {
			return store.ErrTaskFlowInvalid
		}
		out, err = repo.SaveTemplateSource(ctx, c.actor.UserID, input.RequestID, store.TemplateSource{URL: input.URL, Revision: input.Revision, Content: input.Content, Kind: kind}, check, h.now())
		return err
	})
	if e != nil {
		taskError(w, e)
		return
	}
	taskJSON(w, 200, map[string]any{"source": out, "execution_authorized": false})
}
func (h *Hub) HandleTemplateDraft(w http.ResponseWriter, r *http.Request) {
	repo, e := h.templateRepo()
	if e != nil {
		taskError(w, e)
		return
	}
	c, e := questionCredential(r)
	if e != nil {
		taskError(w, e)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	id := r.PathValue("draftID")
	if r.Method == "GET" && id == "" {
		after, x := templateAfter(r)
		if x != nil {
			taskError(w, x)
			return
		}
		ids, x := repo.TemplateCandidateIDs(ctx, c.actor, after)
		if x != nil {
			taskError(w, x)
			return
		}
		out := []store.TemplateView{}
		for _, key := range ids {
			ref, x := repo.TemplateReferences(ctx, key)
			if x != nil {
				taskError(w, x)
				return
			}
			var v store.TemplateView
			x = h.withTemplateIdentity(ctx, c, ref, nil, func(s store.TemplatePrincipalSnapshot, check func() error) error {
				var e error
				v, e = repo.ReadTemplateDraft(ctx, ref, c.actor, s, check, h.now())
				return e
			})
			if x != nil {
				if errors.Is(x, store.ErrTaskFlowPermission) || errors.Is(x, identity.ErrForbidden) {
					continue
				}
				taskError(w, x)
				return
			}
			// Lists are summaries. Repeating up to 100 full 512KiB contracts
			// would exceed the helper's bounded read and disclose unnecessary
			// details before the caller selects a single invitation.
			v.Summary = true
			v.MissingCount = len(v.Missing)
			v.Document = store.TemplateDocument{Title: v.Document.Title, Kind: v.Document.Kind, TemplateVersion: v.Document.TemplateVersion}
			v.ContentMarkdown = ""
			v.Source = store.TemplateSource{}
			v.Missing = nil
			v.Roles = nil
			v.Impact = nil
			out = append(out, v)
		}
		next := ""
		if len(ids) == 100 {
			next = ids[len(ids)-1]
		}
		taskJSON(w, 200, map[string]any{"drafts": out, "next_after": next, "execution_authorized": false})
		return
	}
	var u store.TemplateOperation
	if r.Method == "POST" {
		if !templateDecode(w, r, &u) {
			return
		}
		if u.Operation != "save" && u.Operation != "responsibility" && u.Operation != "confirm" {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		if c.actor.AgentID != "" && (u.Operation != "responsibility" || u.ConfirmHuman || u.Role != "executor" && u.Role != "reviewer") {
			taskError(w, store.ErrTaskFlowPermission)
			return
		}
		if u.Operation != "save" && u.Document != nil {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
	}
	ref := store.TemplateReferences{Owner: c.actor.UserID}
	if id != "" {
		ref, e = repo.TemplateReferences(ctx, id)
		if e != nil {
			taskError(w, e)
			return
		}
	}
	var out store.TemplateView
	e = h.withTemplateIdentity(ctx, c, ref, u.Document, func(s store.TemplatePrincipalSnapshot, check func() error) error {
		var err error
		switch {
		case r.Method == "GET":
			if len(r.URL.Query()) != 0 {
				return store.ErrTaskFlowInvalid
			}
			if _, x := templateAfter(r); x != nil {
				return x
			}
			out, err = repo.ReadTemplateDraft(ctx, ref, c.actor, s, check, h.now())
		case u.Operation == "save":
			if c.actor.UserID == "" {
				return store.ErrTaskFlowPermission
			}
			out, err = repo.SaveTemplateDraft(ctx, c.actor.UserID, id, u, s, check, h.now())
		case u.Operation == "responsibility":
			out, err = repo.UpdateTemplateResponsibility(ctx, ref, c.actor, u, s, check, h.now())
		case u.Operation == "confirm":
			if c.actor.UserID == "" {
				return store.ErrTaskFlowPermission
			}
			out, err = repo.ConfirmTemplateDraft(ctx, ref, c.actor.UserID, u, s, check, h.now())
		default:
			return store.ErrTaskFlowInvalid
		}
		return err
	})
	if e != nil {
		taskError(w, e)
		return
	}
	taskJSON(w, 200, map[string]any{"draft": out, "artifacts": store.TemplateArtifacts(out), "execution_authorized": false})
}
func (h *Hub) HandleTemplateOptions(w http.ResponseWriter, r *http.Request) {
	c, e := questionCredential(r)
	if e != nil || c.actor.UserID == "" || h.Identity == nil {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	users, e := h.Identity.Store.ListUsers(r.Context())
	if e != nil {
		taskError(w, e)
		return
	}
	contacts := []map[string]string{}
	for _, u := range users {
		if u.State == identity.UserActive {
			contacts = append(contacts, map[string]string{"id": "user:" + u.UserIDLC(), "name": u.ShortName})
			if len(contacts) >= 500 {
				break
			}
		}
	}
	agents, e := h.Identity.Store.ListAgents(r.Context(), c.actor.UserID)
	if e != nil {
		taskError(w, e)
		return
	}
	owned := []map[string]string{}
	for _, a := range agents {
		if a.State == identity.AgentActive {
			owned = append(owned, map[string]string{"id": "agent:" + a.ID, "name": a.Name})
			if len(owned) >= 500 {
				break
			}
		}
	}
	e = h.withTemplateIdentity(r.Context(), c, store.TemplateReferences{Owner: c.actor.UserID}, nil, func(_ store.TemplatePrincipalSnapshot, check func() error) error { return check() })
	if e != nil {
		taskError(w, e)
		return
	}
	taskJSON(w, 200, map[string]any{"actor_principal": "user:" + c.actor.UserID, "users": contacts, "agents": owned, "limit": 500, "sample": store.StockTemplateSample(), "template_version": store.PilotTemplateVersion, "connector_state": "pending_config", "execution_authorized": false})
}
func (h *Hub) HandleTemplateParents(w http.ResponseWriter, r *http.Request) {
	repo, e := h.templateRepo()
	if e != nil {
		taskError(w, e)
		return
	}
	c, e := questionCredential(r)
	if e != nil || c.actor.UserID == "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	after, e := templateAfter(r)
	if e != nil {
		taskError(w, e)
		return
	}
	var parents []map[string]any
	e = h.withTemplateIdentity(r.Context(), c, store.TemplateReferences{Owner: c.actor.UserID}, nil, func(_ store.TemplatePrincipalSnapshot, check func() error) error {
		var x error
		parents, x = repo.TemplateParents(r.Context(), c.actor.UserID, after)
		if x != nil {
			return x
		}
		return check()
	})
	if e != nil {
		taskError(w, e)
		return
	}
	next := ""
	if len(parents) == 100 {
		next, _ = parents[99]["spec_id"].(string)
	}
	taskJSON(w, 200, map[string]any{"parents": parents, "next_after": next, "execution_authorized": false})
}
