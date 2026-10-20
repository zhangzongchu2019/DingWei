package m8

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type blockerRepository interface {
	ConsoleTaskFlowState(context.Context, string) (store.TaskFlowState, error)
	UpdateTaskBlocker(context.Context, store.TaskFlowState, store.BlockerActor, string, store.BlockerUpdate, func() error, time.Time) (store.BlockerResult, error)
	ReadTaskBlockers(context.Context, store.TaskFlowState, store.BlockerActor, string, string, int, func() error, ...int) ([]store.TaskBlocker, []store.BlockerInput, error)
	PendingBlockerWakes(context.Context, string) ([]store.BlockerWake, error)
	FinishBlockerWake(context.Context, string, string, time.Time) error
	BlockerWakeCurrent(context.Context, store.BlockerWake) (bool, error)
	ReconcileTaskBlockers(context.Context, store.TaskFlowState, func() error, time.Time) error
	BlockerTasksAfter(context.Context, string) ([]store.TaskFlowState, error)
}
type blockerCredential struct {
	actor store.BlockerActor
	hash  string
}
type blockerCredentialKey struct{}
type blockerDispatchKey struct{}

func (h *Hub) RequireAgentBlocker(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(r.Header.Values("Authorization")) != 1 || len(r.Header.Values("Cookie")) != 0 || h.Identity == nil {
			taskJSON(w, 401, map[string]any{"error": "agent_auth_required"})
			return
		}
		fields := strings.Fields(r.Header.Get("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			taskJSON(w, 401, map[string]any{"error": "agent_auth_required"})
			return
		}
		a, _, err := h.Identity.AuthenticateAgent(r.Context(), fields[1])
		if err != nil || a == nil {
			taskJSON(w, 403, map[string]any{"error": "agent_inactive_or_invalid"})
			return
		}
		c := blockerCredential{actor: store.BlockerActor{AgentID: a.ID}, hash: identity.HashSecret(fields[1])}
		next(w, r.WithContext(context.WithValue(r.Context(), blockerCredentialKey{}, c)))
	}
}
func (h *Hub) withBlockerIdentity(ctx context.Context, t store.TaskFlowState, c blockerCredential, fn func(func() error) error) error {
	if h.Identity == nil || t.Archived {
		return store.ErrTaskFlowPermission
	}
	s, ok := h.Identity.Store.(identity.BlockerAuthorizationStore)
	if !ok {
		return errors.New("blocker authorization unavailable")
	}
	claim := identity.ContextClaim{Owner: t.Owner, Group: t.GroupCode, Developer: t.Developer, Reviewer: t.Reviewer}
	return s.WithBlockerAuthorization(ctx, c.actor.AgentID, c.hash, c.actor.UserID, claim, func(a identity.ContextAuthorization, check func() error) error {
		if !a.Allows(claim) {
			return identity.ErrForbidden
		}
		return fn(check)
	})
}
func (h *Hub) HandleTaskBlockers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	repo, ok := h.Repo.(blockerRepository)
	if !ok || h.Identity == nil {
		taskError(w, errors.New("blocker store unavailable"))
		return
	}
	c, agent := r.Context().Value(blockerCredentialKey{}).(blockerCredential)
	if !agent {
		if len(r.Header.Values("Authorization")) != 0 {
			taskError(w, store.ErrTaskFlowPermission)
			return
		}
		c.actor.UserID = platformPrincipal(r)
	}
	if c.actor.Key() == "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	t, err := repo.ConsoleTaskFlowState(ctx, r.PathValue("taskID"))
	if err != nil {
		taskError(w, err)
		return
	}
	bid := r.PathValue("blockerID")
	if r.Method == "GET" {
		q, err := parseBlockerRead(r)
		if err != nil {
			taskError(w, err)
			return
		}
		var cards []store.TaskBlocker
		var inputs []store.BlockerInput
		err = h.withBlockerIdentity(ctx, t, c, func(check func() error) error {
			var e error
			cards, inputs, e = repo.ReadTaskBlockers(ctx, t, c.actor, bid, q.cursor, q.limit+1, check, q.before)
			return e
		})
		if err != nil {
			taskError(w, err)
			return
		}
		next := ""
		if len(cards) > q.limit {
			cards = cards[:q.limit]
			next = cards[len(cards)-1].ID
		}
		inputNext := 0
		if len(inputs) > 100 {
			inputs = inputs[:100]
			inputNext = inputs[len(inputs)-1].InputVersion
		}
		for {
			body, e := json.Marshal(map[string]any{"blockers": cards, "inputs": inputs, "inputs_next_before": inputNext, "inputs_complete": inputNext == 0, "next_cursor": next, "task_version": t.Version, "execution_authorized": false})
			if e != nil {
				taskError(w, e)
				return
			}
			if len(body) <= 1<<20 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				_, _ = w.Write(body)
				break
			}
			if len(cards) > 1 {
				cards = cards[:len(cards)-1]
				next = cards[len(cards)-1].ID
			} else if len(inputs) > 1 {
				inputs = inputs[:len(inputs)-1]
				inputNext = inputs[len(inputs)-1].InputVersion
			} else {
				taskJSON(w, 413, map[string]any{"error": "blocker_entry_too_large"})
				return
			}
		}
		return
	}
	if r.Method != "POST" {
		taskJSON(w, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	var u store.BlockerUpdate
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&u) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	if bid == "" && u.Operation != "create" || bid != "" && u.Operation == "create" {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	// This source is an authenticated principal, not an external webhook claim.
	if u.Operation == "input" {
		if u.Source != "" && u.Source != c.actor.Key() {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		u.Source = c.actor.Key()
	}
	if u.WaitingPrincipal != "" {
		if strings.HasPrefix(u.WaitingPrincipal, "agent:") {
			a, e := h.Identity.Store.GetAgent(ctx, strings.TrimPrefix(u.WaitingPrincipal, "agent:"))
			if e != nil || a == nil || a.State != identity.AgentActive {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
		} else if strings.HasPrefix(u.WaitingPrincipal, "user:") {
			id := strings.TrimPrefix(u.WaitingPrincipal, "user:")
			person, e := h.Identity.Store.GetUser(ctx, id)
			if e != nil || person == nil || person.State != identity.UserActive || person.UserIDLC() != id {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
		} else {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
	}
	var out store.BlockerResult
	err = h.withBlockerIdentity(ctx, t, c, func(check func() error) error {
		var e error
		out, e = repo.UpdateTaskBlocker(ctx, t, c.actor, bid, u, check, h.now())
		return e
	})
	if err != nil {
		taskError(w, err)
		return
	}
	taskJSON(w, 200, out)
	// Delivery is durable and asynchronous; POST acknowledgement is not execution.
	go h.dispatchBlockerWakes(context.Background())
}

type blockerRead struct {
	cursor string
	limit  int
	before int
}

func parseBlockerRead(r *http.Request) (blockerRead, error) {
	q := blockerRead{limit: 50}
	parsed, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, store.ErrTaskFlowInvalid
	}
	for k, values := range parsed {
		if len(values) != 1 {
			return q, store.ErrTaskFlowInvalid
		}
		switch k {
		case "cursor":
			q.cursor = values[0]
		case "input_before":
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 1 {
				return q, store.ErrTaskFlowInvalid
			}
			q.before = n
		case "limit":
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 1 || n > 100 {
				return q, store.ErrTaskFlowInvalid
			}
			q.limit = n
		default:
			return q, store.ErrTaskFlowInvalid
		}
	}
	return q, nil
}

func (h *Hub) dispatchBlockerWakes(ctx context.Context) {
	if !h.blockerDispatchMu.TryLock() {
		return
	}
	defer h.blockerDispatchMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	repo, ok := h.Repo.(blockerRepository)
	if !ok || h.Identity == nil {
		return
	}
	after := ""
	for ctx.Err() == nil {
		items, err := repo.PendingBlockerWakes(ctx, after)
		if err != nil || len(items) == 0 {
			return
		}
		after = items[len(items)-1].ID
		for _, item := range items {
			current, e := repo.ConsoleTaskFlowState(ctx, item.TaskID)
			if e != nil {
				continue
			}
			if current.NeedsReassignment || current.Archived || current.AssignmentVersion != item.AssignmentVersion || current.Developer != item.Recipient {
				_ = repo.FinishBlockerWake(ctx, item.ID, "assignment_invalidated", h.now())
				continue
			}
			peer, e := h.Identity.Store.GetAgent(ctx, item.Recipient)
			if e != nil || peer == nil {
				continue
			}
			key, name := AgentKeyID(peer.OwnerLC), peer.NameLC()
			h.mu.Lock()
			client := h.sessionClients[key][name]
			h.mu.Unlock()
			if client == nil || client.metricsAgentID != peer.ID {
				continue
			}
			client.stateMu.Lock()
			capable := client.blockerNoticeV1 && client.deliveryACKV1
			automatic := client.blockerWakeV1
			client.stateMu.Unlock()
			if !capable {
				continue
			}
			err = h.withTaskIdentity(ctx, current, func(_ []identity.Agent, check func() error) error {
				state, e := repo.ConsoleTaskFlowState(ctx, item.TaskID)
				if e != nil {
					return e
				}
				if state != current {
					return store.ErrTaskFlowConflict
				}
				valid, e := repo.BlockerWakeCurrent(ctx, item)
				if e != nil {
					return e
				}
				if !valid {
					_ = repo.FinishBlockerWake(ctx, item.ID, "superseded", h.now())
					return nil
				}
				if e = check(); e != nil {
					return e
				}
				meta := map[string]any{"kind": "task_blocker_ready", "task_flow_internal": true, "reply_policy": "no_reply", "conversation_terminal": true, "signal_id": item.ID, "blocker_id": item.BlockerID, "task_id": item.TaskID, "generation": item.Generation, "input_version": item.InputVersion, "assignment_version": item.AssignmentVersion, "automatic_recheck": automatic}
				body, _ := json.Marshal(map[string]any{"task_id": item.TaskID, "blocker_id": item.BlockerID, "input_version": item.InputVersion, "notice": "有新输入，仅授权重核；不授权恢复或业务动作"})
				env := model.Envelope{ID: item.ID, From: sessionAddress(systemAddressName, key), To: sessionAddress(name, key), TS: h.now().Unix(), Body: string(body), Meta: meta}
				if e = h.RouteEnvelope(context.WithValue(ctx, blockerDispatchKey{}, true), env); e != nil {
					return e
				}
				return repo.FinishBlockerWake(ctx, item.ID, "reliable_delivery_enqueued", h.now())
			})
			if err != nil {
				h.invalidateTaskIfDenied(ctx, current, err)
			}
		}
	}
}
func (h *Hub) reconcileBlockers(ctx context.Context) {
	repo, ok := h.Repo.(blockerRepository)
	if !ok || h.Identity == nil {
		return
	}
	after := ""
	for ctx.Err() == nil {
		tasks, err := repo.BlockerTasksAfter(ctx, after)
		if err != nil || len(tasks) == 0 {
			return
		}
		after = tasks[len(tasks)-1].ID
		for _, t := range tasks {
			e := h.withTaskIdentity(ctx, t, func(_ []identity.Agent, check func() error) error {
				return repo.ReconcileTaskBlockers(ctx, t, check, h.now())
			})
			if e != nil {
				h.invalidateTaskIfDenied(ctx, t, e)
			}
		}
	}
}
