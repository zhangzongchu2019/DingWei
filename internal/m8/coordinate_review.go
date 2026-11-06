package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	cr "github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
)

type coordinateReviewDispatchKey struct{}
type coordinateReviewRouteKey struct{}

func coordinateReviewEnvelope(e model.Envelope) bool {
	return metaString(e.Meta, "coordinate_review_type") == "coordinate_review_request"
}
func coordinateError(w http.ResponseWriter, e error) {
	status := 503
	code := "coordinate_review_unavailable"
	switch {
	case errors.Is(e, cr.ErrInvalid), errors.Is(e, specread.ErrInvalid):
		status = 400
		code = "invalid_coordinate_review"
	case errors.Is(e, cr.ErrForbidden), errors.Is(e, specread.ErrForbidden):
		status = 403
		code = "coordinate_review_denied"
	case errors.Is(e, cr.ErrConflict):
		status = 409
		code = "coordinate_review_conflict"
	}
	taskJSON(w, status, map[string]any{"error": code, "retryable": false})
}
func coordinateDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		coordinateError(w, cr.ErrInvalid)
		return false
	}
	return true
}
func (h *Hub) coordinateMutation(w http.ResponseWriter, r *http.Request, agent bool) bool {
	if r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
		coordinateError(w, cr.ErrInvalid)
		return false
	}
	if agent {
		return true
	}
	o, e := url.Parse(r.Header.Get("Origin"))
	if e != nil || !trustedorigin.AuthorityMatches(o.Host, r.Host, len(h.TrustedOrigins) > 0) || o.User != nil || o.Path != "" || o.RawQuery != "" || o.Fragment != "" || (o.Scheme != "https" && o.Scheme != "http") || !h.originAllowed(r.Header.Get("Origin"), h.PublicOrigin == "" || r.Header.Get("Origin") == h.PublicOrigin, len(r.Header.Values("Origin"))) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		coordinateError(w, cr.ErrForbidden)
		return false
	}
	return true
}
func (h *Hub) HandleCoordinateReviews(w http.ResponseWriter, r *http.Request) {
	h.coordinateReviewsHTTP(w, r, false)
}
func (h *Hub) HandleAgentCoordinateReviews(w http.ResponseWriter, r *http.Request) {
	h.coordinateReviewsHTTP(w, r, true)
}
func (h *Hub) coordinateReviewsHTTP(w http.ResponseWriter, r *http.Request, agent bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !h.coordinateReviewReady() {
		taskJSON(w, 503, map[string]any{"error": "coordinate_review_not_enabled", "retryable": false})
		return
	}
	actor := platformPrincipal(r)
	if agent {
		v, _ := r.Context().Value(taskAgentContextKey{}).(string)
		if v == "" {
			coordinateError(w, cr.ErrForbidden)
			return
		}
		actor = cr.AgentActor(v)
	}
	if actor == "" {
		coordinateError(w, cr.ErrForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	id, action := r.PathValue("requestID"), r.PathValue("action")
	if r.Method == "GET" {
		if id != "" {
			q, e := h.CoordinateReviews.Get(ctx, actor, id)
			if e != nil {
				coordinateError(w, e)
				return
			}
			taskJSON(w, 200, map[string]any{"request": q})
			return
		}
		query, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil {
			coordinateError(w, cr.ErrInvalid)
			return
		}
		for k, v := range query {
			if k != "after" || len(v) != 1 || len(v[0]) > 128 {
				coordinateError(w, cr.ErrInvalid)
				return
			}
		}
		ids, e := h.CoordinateReviews.Store.CoordinateReviewIDs(ctx, query.Get("after"), 100)
		if e != nil {
			coordinateError(w, e)
			return
		}
		items := []cr.Record{}
		for _, id := range ids {
			q, e := h.CoordinateReviews.Get(ctx, actor, id)
			if e == nil {
				items = append(items, q)
			}
		}
		next := ""
		if len(ids) == 100 {
			next = ids[len(ids)-1]
		}
		taskJSON(w, 200, map[string]any{"requests": items, "next": next})
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if !h.coordinateMutation(w, r, agent) {
		return
	}
	var q cr.Record
	var e error
	if id == "" && !agent {
		var in cr.Create
		if !coordinateDecode(w, r, &in) {
			return
		}
		q, e = h.CoordinateReviews.Create(ctx, actor, in)
	} else {
		switch action {
		case "open", "cancel", "timeline":
			var in struct{}
			if !coordinateDecode(w, r, &in) {
				return
			}
			if action == "open" && agent {
				q, e = h.CoordinateReviews.Open(ctx, actor, id)
			} else if action == "cancel" && !agent {
				q, e = h.CoordinateReviews.Cancel(ctx, actor, id)
			} else if action == "timeline" && agent {
				q, e = h.CoordinateReviews.Timeline(ctx, actor, id)
			} else {
				e = cr.ErrForbidden
			}
		case "result":
			if !agent {
				coordinateError(w, cr.ErrForbidden)
				return
			}
			var in cr.Result
			if !coordinateDecode(w, r, &in) {
				return
			}
			if in.Reviewer != "" || !in.At.IsZero() {
				coordinateError(w, cr.ErrInvalid)
				return
			}
			q, e = h.CoordinateReviews.Submit(ctx, actor, id, in)
		case "compare":
			if agent {
				coordinateError(w, cr.ErrForbidden)
				return
			}
			var in cr.Coordinate
			if !coordinateDecode(w, r, &in) {
				return
			}
			q, e = h.CoordinateReviews.Compare(ctx, actor, id, in)
		case "read":
			if !agent {
				coordinateError(w, cr.ErrForbidden)
				return
			}
			var in struct {
				Path string `json:"path"`
			}
			if !coordinateDecode(w, r, &in) {
				return
			}
			out, x := h.CoordinateReviews.Read(ctx, actor, id, in.Path)
			if x != nil {
				coordinateError(w, x)
				return
			}
			taskJSON(w, 200, map[string]any{"document": out})
			return
		default:
			e = cr.ErrInvalid
		}
	}
	if e != nil {
		coordinateError(w, e)
		return
	}
	taskJSON(w, 200, map[string]any{"request": q})
}
func (h *Hub) HandleCoordinateReviewAgentBinding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.coordinateReviewReady() {
		coordinateError(w, cr.ErrUnavailable)
		return
	}
	repo := h.Repo.(coordinateReviewRepository)
	if r.Method == "GET" {
		id := r.URL.Query().Get("agent_id")
		if _, e := h.Identity.Store.GetAgent(r.Context(), id); e != nil {
			coordinateError(w, cr.ErrForbidden)
			return
		}
		b, e := repo.CoordinateReviewAgentBinding(r.Context(), id)
		if e != nil {
			coordinateError(w, e)
			return
		}
		taskJSON(w, 200, map[string]any{"binding": b})
		return
	}
	if r.Method != "PUT" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	if !h.coordinateMutation(w, r, false) {
		return
	}
	var in struct {
		AgentID   string `json:"agent_id"`
		AccountID string `json:"account_id"`
		Login     string `json:"login"`
	}
	if !coordinateDecode(w, r, &in) {
		return
	}
	if _, e := h.Identity.Store.GetAgent(r.Context(), in.AgentID); e != nil {
		coordinateError(w, cr.ErrForbidden)
		return
	}
	var b *specread.Binding
	if r.Method == "PUT" {
		b = &specread.Binding{Principal: cr.AgentActor(in.AgentID), Provider: "github", Instance: "github.com", AccountID: in.AccountID, Login: in.Login}
		if !b.Valid() {
			coordinateError(w, cr.ErrInvalid)
			return
		}
	}
	release, err := repo.BeginCoordinateReview(r.Context())
	if err != nil {
		coordinateError(w, cr.ErrUnavailable)
		return
	}
	defer release()
	h.coordinateReviewAuthMu.Lock()
	defer h.coordinateReviewAuthMu.Unlock()
	if e := repo.SetCoordinateReviewAgentBinding(r.Context(), platformPrincipal(r), in.AgentID, b); e != nil {
		coordinateError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (h *Hub) sendCoordinateReview(ctx context.Context, r cr.Record, current cr.Coordinate, route string) error {
	h.mu.Lock()
	var client *sessionClient
	name, keyPart, found := strings.Cut(route, "#")
	if found {
		client = h.sessionClients[keyPart][name]
	}
	h.mu.Unlock()
	if client == nil {
		return cr.ErrUnavailable
	}
	client.stateMu.Lock()
	capable := client.coordinateReviewV1 && client.deliveryACKV1
	client.stateMu.Unlock()
	if !capable {
		return cr.ErrUnavailable
	}
	body, _ := json.Marshal(current)
	event := "assigned"
	id := r.ID
	if current != r.Coordinate {
		event = "stale"
		id += "_" + current.Commit
	}
	// Route identity is independently checked in reviewAccess before this callback.
	_, key, ok := strings.Cut(route, "#")
	if !ok {
		return cr.ErrUnavailable
	}
	env := model.Envelope{ID: id, From: sessionAddress(systemAddressName, key), To: route, Body: string(body), Meta: map[string]any{"system": true, "no_mirror": true, "reply_policy": "no_reply", "coordinate_review_type": "coordinate_review_request", "coordinate_review_event": event, "request_id": r.ID}}
	return h.RouteEnvelope(context.WithValue(ctx, coordinateReviewDispatchKey{}, true), env)
}
func (h *Hub) coordinateReviewDeliveryAllowed(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if !h.coordinateReviewReady() {
		return false
	}
	c.stateMu.Lock()
	capable := c.coordinateReviewV1 && c.deliveryACKV1
	c.stateMu.Unlock()
	if !capable {
		return false
	}
	id := metaString(env.Meta, "request_id")
	r, e := h.CoordinateReviews.Store.CoordinateReview(ctx, id)
	if e != nil || c.metricsAgentID != r.AgentID {
		return false
	}
	route := sessionAddress(c.sessionName, c.keyID)
	ctx = context.WithValue(ctx, coordinateReviewRouteKey{}, route)
	current, e := h.CoordinateReviews.Get(ctx, cr.AgentActor(r.AgentID), id)
	if e != nil || !current.Cancelled.IsZero() {
		return false
	}
	var body cr.Coordinate
	if json.Unmarshal([]byte(env.Body), &body) != nil {
		return false
	}
	if metaString(env.Meta, "coordinate_review_event") == "assigned" {
		return current.Pending() && body == current.Coordinate
	}
	if metaString(env.Meta, "coordinate_review_event") != "stale" {
		return false
	}
	for _, a := range current.Alerts {
		if a.Current == body {
			return true
		}
	}
	return false
}

// Single Hub-lifetime worker; a keyset page avoids starvation beyond 100 records.
func (h *Hub) processCoordinateReviews(ctx context.Context) {
	if !h.coordinateReviewReady() {
		return
	}
	h.coordinateReviewWorkerMu.Lock()
	defer h.coordinateReviewWorkerMu.Unlock()
	ids, e := h.CoordinateReviews.Store.CoordinateReviewIDs(ctx, h.coordinateReviewCursor, 100)
	if e != nil {
		return
	}
	if len(ids) == 0 {
		h.coordinateReviewCursor = ""
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		h.coordinateReviewCursor = id
		r, e := h.CoordinateReviews.Store.CoordinateReview(ctx, id)
		if e != nil {
			continue
		}
		a, e := h.Identity.Store.GetAgent(ctx, r.AgentID)
		if e != nil {
			continue
		}
		route := sessionAddress(a.NameLC(), AgentKeyID(a.OwnerLC))
		work, cancel := context.WithTimeout(context.WithValue(ctx, coordinateReviewRouteKey{}, route), 15*time.Second)
		_ = h.CoordinateReviews.Track(work, id)
		_ = h.CoordinateReviews.Dispatch(work, id, func(q cr.Record) error { return h.sendCoordinateReview(work, q, q.Coordinate, route) })
		_ = h.CoordinateReviews.DispatchAlert(work, id, func(q cr.Record, c cr.Coordinate) error { return h.sendCoordinateReview(work, q, c, route) })
		_ = h.CoordinateReviews.Remind(work, id, func(q cr.Record, slot int, issuer, subject string) (string, error) {
			body, _ := json.Marshal(q.Coordinate)
			return h.CoordinateReviewSender(work, issuer, subject, fmt.Sprintf("r6:%s:%d", q.ID, slot), string(body))
		})
		cancel()
	}
}
