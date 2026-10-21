package m8

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (h *Hub) HandleSpecClarifications(w http.ResponseWriter, r *http.Request) {
	repo, e := h.clarificationRepo()
	if e != nil {
		taskError(w, e)
		return
	}
	projection, ok := h.Repo.(interface {
		ReadSpecClarifications(context.Context, store.QuestionContext, int, int64, int, func() error) (store.ContextClarifications, error)
	})
	if !ok {
		taskJSON(w, 503, map[string]any{"error": "clarification_projection_unavailable"})
		return
	}
	actor, e := questionCredential(r)
	if e != nil || actor.actor.UserID == "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	revision, limit := 0, 50
	before := int64(0)
	for k, v := range values {
		if len(v) != 1 {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		switch k {
		case "revision":
			revision, e = strconv.Atoi(v[0])
			if e != nil || revision < 1 {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	c, e := repo.QuestionAuthorizationContext(ctx, r.PathValue("specID"), "", "")
	if e != nil {
		taskError(w, e)
		return
	}
	if revision == 0 {
		revision = c.Revision
	}
	p, e := repo.ClarificationPolicy(ctx, c.SpecID)
	if e != nil {
		taskError(w, e)
		return
	}
	var out store.ContextClarifications
	e = h.withClarificationIdentity(ctx, repo, c, p, actor, nil, func(_ identity.ContextAuthorization, check func() error) error {
		var err error
		out, err = projection.ReadSpecClarifications(ctx, c, revision, before, limit, check)
		return err
	})
	if e != nil {
		taskError(w, e)
		return
	}
	taskJSON(w, 200, map[string]any{"posts": out.Posts, "next_before": out.NextBefore, "revision": revision, "data_only": true, "execution_authorized": false})
}
