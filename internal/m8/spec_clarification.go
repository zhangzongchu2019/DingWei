package m8

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type clarificationRepository interface {
	questionRepository
	ClarificationPolicy(context.Context, string) (store.ClarificationPolicy, error)
	SetClarificationPolicy(context.Context, store.QuestionContext, store.BlockerActor, store.ClarificationPolicyUpdate, func() error, time.Time) (store.ClarificationPolicy, error)
	AppendClarification(context.Context, store.QuestionContext, store.BlockerActor, string, store.ClarificationUpdate, bool, func() error, time.Time) (store.ClarificationResult, error)
	ReadClarificationThread(context.Context, store.QuestionContext, store.BlockerActor, string, int64, int, func() error) (store.ClarificationThread, error)
	FindClarificationRequest(context.Context, store.QuestionContext, store.BlockerActor, string, string, func() error) (store.ClarificationResult, bool, error)
}

type ClarificationReplyAuthority struct{ AppID, Tenant string }

func (h *Hub) clarificationRepo() (clarificationRepository, error) {
	r, ok := h.Repo.(clarificationRepository)
	if !ok || h.Identity == nil {
		return nil, errors.New("clarification store unavailable")
	}
	return r, nil
}

func decodeClarification(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(&struct{}{}) != io.EOF {
		taskError(w, store.ErrTaskFlowInvalid)
		return false
	}
	return true
}

// Identity/binding rows stay locked across the SQLite snapshot/commit. All
// clarification and policy entry points use one spec-level advisory key so a
// PM/deputy replacement cannot cross a thread write's authorization window.
func (h *Hub) withClarificationIdentity(ctx context.Context, repo clarificationRepository, c store.QuestionContext, p store.ClarificationPolicy, actor blockerCredential, extra []string, fn func(identity.ContextAuthorization, func() error) error) error {
	auth, ok := h.Identity.Store.(identity.QuestionAuthorizationStore)
	if !ok {
		return errors.New("clarification identity unavailable")
	}
	var claim *identity.ContextClaim
	if c.Task != nil && c.Task.GroupCode != "" {
		claim = &identity.ContextClaim{Owner: c.Task.Owner, Group: c.Task.GroupCode, Developer: c.Task.Developer, Reviewer: c.Task.Reviewer}
	}
	users := append([]string{c.Owner, c.Editor, p.PMUser, p.DeputyUser}, extra...)
	return auth.WithQuestionAuthorization(ctx, actor.actor.AgentID, actor.hash, actor.actor.UserID, claim, users, nil, "clarification-spec:"+c.SpecID, func(a identity.ContextAuthorization, check func() error) error {
		if !a.Users[c.Owner] {
			return identity.ErrForbidden
		}
		if actor.actor.AgentID != "" && (claim == nil || !a.Allows(*claim)) {
			return identity.ErrForbidden
		}
		current, err := repo.ClarificationPolicy(ctx, c.SpecID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, p) {
			return store.ErrTaskFlowConflict
		}
		return fn(a, check)
	})
}

func (h *Hub) HandleClarificationPolicy(w http.ResponseWriter, r *http.Request) {
	repo, err := h.clarificationRepo()
	if err != nil {
		taskError(w, err)
		return
	}
	actor, err := questionCredential(r)
	if err != nil || actor.actor.UserID == "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	if r.URL.RawQuery != "" {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := repo.QuestionAuthorizationContext(ctx, r.PathValue("specID"), "", "")
	if err != nil {
		taskError(w, err)
		return
	}
	p, err := repo.ClarificationPolicy(ctx, c.SpecID)
	if err != nil {
		taskError(w, err)
		return
	}
	if actor.actor.UserID != c.Owner && actor.actor.UserID != p.PMUser {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	var u store.ClarificationPolicyUpdate
	if r.Method == "POST" {
		if !h.SpecClarificationEnabled {
			taskJSON(w, 503, map[string]any{"error": "clarification_disabled"})
			return
		}
		if !decodeClarification(w, r, &u) {
			return
		}
		u.PMUser = strings.ToLower(strings.TrimSpace(u.PMUser))
		u.DeputyUser = strings.ToLower(strings.TrimSpace(u.DeputyUser))
	} else if r.Method != "GET" {
		taskJSON(w, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	out := p
	err = h.withClarificationIdentity(ctx, repo, c, p, actor, []string{u.PMUser, u.DeputyUser}, func(a identity.ContextAuthorization, check func() error) error {
		if r.Method == "POST" {
			if u.PMUser != "" && !a.Users[u.PMUser] || u.DeputyUser != "" && !a.Users[u.DeputyUser] {
				return store.ErrTaskFlowPermission
			}
			var e error
			out, e = repo.SetClarificationPolicy(ctx, c, actor.actor, u, check, h.now())
			return e
		}
		return check()
	})
	if err != nil {
		taskError(w, err)
		return
	}
	taskJSON(w, 200, map[string]any{"policy": out, "enabled": h.SpecClarificationEnabled, "can_select_pm": actor.actor.UserID == c.Owner, "can_select_deputy": p.PMUser != "" && actor.actor.UserID == p.PMUser, "execution_authorized": false})
}

func (h *Hub) HandleClarification(w http.ResponseWriter, r *http.Request) {
	repo, err := h.clarificationRepo()
	if err != nil {
		taskError(w, err)
		return
	}
	actor, err := questionCredential(r)
	if err != nil {
		taskError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	qid := r.PathValue("questionID")
	if qid == "" {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	c, err := repo.QuestionAuthorizationContext(ctx, "", "", qid)
	if err != nil {
		taskError(w, err)
		return
	}
	p, err := repo.ClarificationPolicy(ctx, c.SpecID)
	if err != nil {
		taskError(w, err)
		return
	}
	if r.Method == "GET" {
		if requestID := r.PathValue("requestID"); requestID != "" {
			if r.URL.RawQuery != "" {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
			var out store.ClarificationResult
			found := false
			err = h.withClarificationIdentity(ctx, repo, c, p, actor, nil, func(_ identity.ContextAuthorization, check func() error) error {
				var e error
				out, found, e = repo.FindClarificationRequest(ctx, c, actor.actor, qid, requestID, check)
				return e
			})
			if err != nil {
				taskError(w, err)
				return
			}
			if !found {
				taskJSON(w, 404, map[string]any{"error": "request_not_recorded", "retry_same_request_id_only": true})
				return
			}
			taskJSON(w, 200, out)
			return
		}
		values, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		before, limit := int64(0), 50
		for k, v := range values {
			if len(v) != 1 {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
			switch k {
			case "before":
				before, e = strconv.ParseInt(v[0], 10, 64)
				if e != nil || before < 1 {
					taskError(w, store.ErrTaskFlowInvalid)
					return
				}
			case "limit":
				limit, e = strconv.Atoi(v[0])
				if e != nil || limit < 1 || limit > 100 {
					taskError(w, store.ErrTaskFlowInvalid)
					return
				}
			default:
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
		}
		var out store.ClarificationThread
		err = h.withClarificationIdentity(ctx, repo, c, p, actor, nil, func(_ identity.ContextAuthorization, check func() error) error {
			var e error
			out, e = repo.ReadClarificationThread(ctx, c, actor.actor, qid, before, limit, check)
			return e
		})
		if err != nil {
			taskError(w, err)
			return
		}
		taskJSON(w, 200, map[string]any{"posts": out.Posts, "next_before": out.NextBefore, "context": out.Context, "enabled": h.SpecClarificationEnabled, "execution_authorized": false})
		return
	}
	if r.Method != "POST" {
		taskJSON(w, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	if r.URL.RawQuery != "" {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	if !h.SpecClarificationEnabled {
		taskJSON(w, 503, map[string]any{"error": "clarification_disabled"})
		return
	}
	var u store.ClarificationUpdate
	if !decodeClarification(w, r, &u) {
		return
	}
	var out store.ClarificationResult
	err = h.withClarificationIdentity(ctx, repo, c, p, actor, nil, func(a identity.ContextAuthorization, check func() error) error {
		// The current actor's active identity is checked by the PG store, not by
		// a body-provided user/Agent ID. Authority itself is re-read in SQLite.
		var e error
		out, e = repo.AppendClarification(ctx, c, actor.actor, qid, u, h.SpecClarificationEnabled, check, h.now())
		return e
	})
	if err != nil {
		taskError(w, err)
		return
	}
	taskJSON(w, 200, out)
}
