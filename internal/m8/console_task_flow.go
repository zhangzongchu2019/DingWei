package m8

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type taskFlowRepository interface {
	ConsoleTaskFlowState(context.Context, string) (store.TaskFlowState, error)
	ListConsoleTaskFlow(context.Context, string) ([]store.TaskFlowItem, error)
	UpdateConsoleTaskFlow(context.Context, string, string, store.TaskFlowUpdate, func() error, time.Time) (store.TaskFlowState, error)
	AssignConsoleTaskFlow(context.Context, string, string, string, string, string, int, func() error, time.Time) (store.TaskFlowState, error)
	InvalidateConsoleTaskFlow(context.Context, string, int, time.Time) error
	ConsoleTaskCandidates(context.Context, string, string) (*store.TaskCandidates, error)
	ConfirmConsoleTaskCandidates(context.Context, string, string, int, int, []store.ConsoleSpecPresplitTask, func() error, time.Time) error
	PendingConsoleTaskNotifications(context.Context) ([]store.TaskFlowOutbox, error)
	FinishConsoleTaskNotification(context.Context, string, time.Time) error
	SupersedeConsoleTaskNotification(context.Context, string, time.Time) error
}

func (h *Hub) taskFlowRepo() (taskFlowRepository, bool) {
	r, ok := h.Repo.(taskFlowRepository)
	return r, ok
}

type taskAgentContextKey struct{}
type taskNotificationContextKey struct{}

func (h *Hub) RequireAgentTaskFlow(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.Fields(r.Header.Get("Authorization"))
		if h.Identity == nil || len(auth) != 2 || !strings.EqualFold(auth[0], "Bearer") || r.Header.Get("Cookie") != "" {
			writeAgentWSError(w, 401, "agent_auth_required")
			return
		}
		a, _, err := h.Identity.AuthenticateAgent(r.Context(), auth[1])
		if err != nil || a == nil || a.State != identity.AgentActive {
			writeAgentWSError(w, 403, "agent_inactive_or_invalid")
			return
		}
		// AuthenticateAgent also checks the current owner. Group access is separately
		// rechecked under identity locks for every task read or mutation.
		next(w, r.WithContext(context.WithValue(r.Context(), taskAgentContextKey{}, a.ID)))
	}
}
func taskJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func taskError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	code := "task_unavailable"
	switch {
	case errors.Is(err, store.ErrTaskFlowPermission), errors.Is(err, store.ErrConsoleSpecPermission), errors.Is(err, store.ErrConsoleTaskCasePermission), errors.Is(err, identity.ErrForbidden), errors.Is(err, identity.ErrNotFound), errors.Is(err, identity.ErrGroupNotFound):
		status = 403
		code = "task_access_denied"
	case errors.Is(err, store.ErrTaskFlowInvalid), errors.Is(err, store.ErrConsoleTaskCaseUnknown):
		status = 400
		code = "invalid_task_request"
	case errors.Is(err, store.ErrTaskFlowConflict), errors.Is(err, store.ErrConsoleTaskNotGreen), errors.Is(err, store.ErrConsoleTaskAssigneeSame), errors.Is(err, store.ErrConsoleTaskAssigneePair):
		status = 409
		code = "task_conflict"
	}
	taskJSON(w, status, map[string]any{"error": code})
}
func (h *Hub) withTaskIdentity(ctx context.Context, t store.TaskFlowState, fn func([]identity.Agent, func() error) error) error {
	if h.Identity == nil {
		return identity.ErrForbidden
	}
	repo, ok := h.Identity.Store.(identity.TaskAuthorizationStore)
	if !ok {
		return errors.New("task identity store unavailable")
	}
	return repo.WithTaskAuthorization(ctx, t.Owner, t.GroupCode, []string{t.Developer, t.Reviewer}, fn)
}
func (h *Hub) invalidateTaskIfDenied(ctx context.Context, t store.TaskFlowState, err error) {
	if errors.Is(err, identity.ErrForbidden) || errors.Is(err, identity.ErrNotFound) || errors.Is(err, identity.ErrGroupNotFound) {
		if repo, ok := h.taskFlowRepo(); ok {
			_ = repo.InvalidateConsoleTaskFlow(ctx, t.ID, t.Version, h.now())
		}
	}
}
func (h *Hub) HandleAgentTaskFlow(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.taskFlowRepo()
	if !ok {
		taskError(w, errors.New("task store unavailable"))
		return
	}
	actor, _ := r.Context().Value(taskAgentContextKey{}).(string)
	if actor == "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	if r.Method == "GET" {
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > 100 {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
			limit = n
		}
		list, err := repo.ListConsoleTaskFlow(r.Context(), actor)
		if err != nil {
			taskError(w, err)
			return
		}
		sort.Slice(list, func(i, j int) bool { return list[i].FlowState.ID < list[j].FlowState.ID })
		out := []store.TaskFlowItem{}
		next := ""
		cursor := r.URL.Query().Get("cursor")
		for _, item := range list {
			if item.FlowState.ID <= cursor {
				continue
			}
			if spec := r.URL.Query().Get("spec_id"); spec != "" && item.SpecID != spec {
				continue
			}
			if state := r.URL.Query().Get("status"); state != "" && item.Status != state {
				continue
			}
			err = h.withTaskIdentity(r.Context(), item.FlowState, func(_ []identity.Agent, check func() error) error {
				current, e := repo.ConsoleTaskFlowState(r.Context(), item.FlowState.ID)
				if e != nil {
					return e
				}
				if current.Version != item.Version {
					return store.ErrTaskFlowConflict
				}
				if current.NeedsReassignment {
					return store.ErrTaskFlowPermission
				}
				return check()
			})
			if err != nil {
				h.invalidateTaskIfDenied(r.Context(), item.FlowState, err)
				if errors.Is(err, identity.ErrForbidden) || errors.Is(err, identity.ErrNotFound) || errors.Is(err, identity.ErrGroupNotFound) || errors.Is(err, store.ErrTaskFlowPermission) {
					continue
				}
				taskError(w, err)
				return
			}
			if len(out) == limit {
				next = out[len(out)-1].FlowState.ID
				break
			}
			out = append(out, item)
		}
		taskJSON(w, 200, map[string]any{"tasks": out, "next_cursor": next})
		return
	}
	t, err := repo.ConsoleTaskFlowState(r.Context(), r.PathValue("taskID"))
	if err != nil {
		taskError(w, err)
		return
	}
	if actor != t.Developer && actor != t.Reviewer {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	var u store.TaskFlowUpdate
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&u) != nil {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	if action := r.PathValue("caseAction"); action != "" {
		want := map[string]string{"mark": "mark_case", "confirm": "confirm_case"}[action]
		if want == "" || (u.Operation != "" && u.Operation != want) {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		u.Operation = want
	}
	var result store.TaskFlowState
	err = h.withTaskIdentity(r.Context(), t, func(_ []identity.Agent, check func() error) error {
		var e error
		result, e = repo.UpdateConsoleTaskFlow(r.Context(), t.ID, actor, u, check, h.now())
		return e
	})
	if err != nil {
		h.invalidateTaskIfDenied(r.Context(), t, err)
		taskError(w, err)
		return
	}
	taskJSON(w, 200, result)
}

func (h *Hub) resolveTaskPair(ctx context.Context, dev, review string) (*identity.Agent, *identity.Agent, error) {
	if h.Identity == nil {
		return nil, nil, identity.ErrForbidden
	}
	d, _, err := h.Identity.ResolveFullName(ctx, dev)
	if err != nil || d == nil {
		return nil, nil, identity.ErrForbidden
	}
	v, _, err := h.Identity.ResolveFullName(ctx, review)
	if err != nil || v == nil || d.ID == v.ID {
		return nil, nil, store.ErrConsoleTaskAssigneeSame
	}
	// Same owner is explicitly allowed by the leader's final R32 ruling.
	for _, a := range []*identity.Agent{d, v} {
		if err = h.syncNewPlatformUser(ctx, a.OwnerLC, nil); err != nil {
			return nil, nil, err
		}
	}
	return d, v, nil
}

func (h *Hub) withTaskCandidates(ctx context.Context, owner, group string, tasks []store.ConsoleSpecPresplitTask, fn func([]store.ConsoleSpecPresplitTask, func() error) error) error {
	ids := []string{}
	resolved := append([]store.ConsoleSpecPresplitTask(nil), tasks...)
	for i, t := range resolved {
		if t.DeveloperAgent == "" && t.ReviewerAgent == "" && t.DeveloperAgentID == "" && t.ReviewerAgentID == "" {
			resolved[i].GroupCode = ""
			continue
		}
		if t.DeveloperAgent == "" || t.ReviewerAgent == "" {
			return store.ErrConsoleTaskAssigneePair
		}
		d, v, err := h.resolveTaskPair(ctx, t.DeveloperAgent, t.ReviewerAgent)
		if err != nil {
			return err
		}
		resolved[i].DeveloperAgentID = d.ID
		resolved[i].ReviewerAgentID = v.ID
		resolved[i].GroupCode = group
		ids = append(ids, d.ID, v.ID)
	}
	if len(ids) == 0 {
		return fn(resolved, func() error {
			if h.Identity == nil {
				return ctx.Err()
			}
			u, e := h.Identity.Store.GetUser(ctx, owner)
			if e != nil {
				return e
			}
			if u.State != identity.UserActive {
				return identity.ErrForbidden
			}
			return nil
		})
	}
	auth, ok := h.Identity.Store.(identity.TaskAuthorizationStore)
	if !ok {
		return errors.New("task identity store unavailable")
	}
	return auth.WithTaskAuthorization(ctx, owner, group, ids, func(_ []identity.Agent, check func() error) error { return fn(resolved, check) })
}

func (h *Hub) HandleConsoleTaskCandidateConfirm(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.taskFlowRepo()
	if !ok {
		taskError(w, errors.New("task store unavailable"))
		return
	}
	owner := platformPrincipal(r)
	c, err := repo.ConsoleTaskCandidates(r.Context(), r.PathValue("specID"), owner)
	if err != nil {
		taskError(w, err)
		return
	}
	if c == nil || c.RequestID != r.PathValue("requestID") {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	var body struct {
		ExpectedRevision int                             `json:"expected_revision"`
		ExpectedVersion  int                             `json:"expected_version"`
		GroupCode        string                          `json:"group_code"`
		Tasks            []store.ConsoleSpecPresplitTask `json:"tasks"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	if body.Tasks == nil {
		body.Tasks = c.Tasks
	}
	err = h.withTaskCandidates(r.Context(), owner, body.GroupCode, body.Tasks, func(tasks []store.ConsoleSpecPresplitTask, check func() error) error {
		return repo.ConfirmConsoleTaskCandidates(r.Context(), c.RequestID, owner, body.ExpectedRevision, body.ExpectedVersion, tasks, check, h.now())
	})
	if err != nil {
		taskError(w, err)
		return
	}
	taskJSON(w, 200, map[string]any{"confirmed": true})
	h.dispatchTaskNotifications(r.Context())
}

func (h *Hub) assignTaskFlow(w http.ResponseWriter, r *http.Request, id, owner, group, dev, review string, version int) {
	repo, ok := h.taskFlowRepo()
	if !ok || h.Identity == nil {
		taskError(w, errors.New("task store unavailable"))
		return
	}
	auth, ok := h.Identity.Store.(identity.TaskAuthorizationStore)
	if !ok {
		taskError(w, errors.New("task identity store unavailable"))
		return
	}
	for _, id := range []string{dev, review} {
		a, err := h.Identity.Store.GetAgent(r.Context(), id)
		if err != nil {
			taskError(w, err)
			return
		}
		if err = h.syncNewPlatformUser(r.Context(), a.OwnerLC, nil); err != nil {
			taskError(w, err)
			return
		}
	}
	var result store.TaskFlowState
	err := auth.WithTaskAuthorization(r.Context(), owner, group, []string{dev, review}, func(_ []identity.Agent, check func() error) error {
		var e error
		result, e = repo.AssignConsoleTaskFlow(r.Context(), id, owner, group, dev, review, version, check, h.now())
		return e
	})
	if err != nil {
		taskError(w, err)
		return
	}
	taskJSON(w, 200, result)
	h.dispatchTaskNotifications(r.Context())
}

// Only negotiated task-flow peers receive task notifications. Legacy helpers
// keep their normal ACK/injection behavior and receive no unknown task payload.
func (h *Hub) dispatchTaskNotifications(ctx context.Context) {
	if !h.taskFlowDispatchMu.TryLock() {
		return
	}
	defer h.taskFlowDispatchMu.Unlock()
	repo, ok := h.taskFlowRepo()
	if !ok || h.Identity == nil {
		return
	}
	items, err := repo.PendingConsoleTaskNotifications(ctx)
	if err != nil {
		return
	}
	for _, item := range items {
		current, e := repo.ConsoleTaskFlowState(ctx, item.TaskID)
		if e != nil {
			continue
		}
		if current.AssignmentVersion != item.AssignmentVersion {
			_ = repo.SupersedeConsoleTaskNotification(ctx, item.ID, h.now())
			continue
		}
		if strings.HasPrefix(item.Recipient, "owner:") {
			continue
		} // Console exposes owner notices durably.
		a, err := h.Identity.Store.GetAgent(ctx, item.Recipient)
		if err != nil || a == nil || a.State != identity.AgentActive {
			continue
		}
		key, name := AgentKeyID(a.OwnerLC), a.NameLC()
		h.mu.Lock()
		c := h.sessionClients[key][name]
		h.mu.Unlock()
		if c == nil {
			continue
		}
		c.stateMu.Lock()
		capable := c.taskFlowV1 && c.deliveryACKV1
		c.stateMu.Unlock()
		if !capable {
			continue
		}
		env := model.Envelope{ID: item.ID, From: sessionAddress(systemAddressName, key), To: sessionAddress(name, key), TS: h.now().Unix(), Body: item.Body, Meta: map[string]any{"kind": "task_assigned", "reply_policy": "no_reply", "conversation_terminal": true, "task_flow_internal": true}}
		if err = h.RouteEnvelope(context.WithValue(ctx, taskNotificationContextKey{}, true), env); err != nil {
			continue
		}
		_ = repo.FinishConsoleTaskNotification(ctx, item.ID, h.now())
	}
}

// StartTaskFlow follows the Hub lifetime; no detached loop is started by tests
// or construction. Every tick retries outbox and reconciles invalid membership.
func (h *Hub) StartTaskFlow(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			h.reconcileTaskFlow(ctx)
			h.dispatchTaskNotifications(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (h *Hub) reconcileTaskFlow(ctx context.Context) {
	repo, ok := h.Repo.(interface {
		ConsoleTaskFlowStates(context.Context) ([]store.TaskFlowState, error)
	})
	if !ok || h.Identity == nil {
		return
	}
	tasks, err := repo.ConsoleTaskFlowStates(ctx)
	if err != nil {
		return
	}
	for _, t := range tasks {
		if t.NeedsReassignment {
			continue
		}
		err = h.withTaskIdentity(ctx, t, func(_ []identity.Agent, check func() error) error { return check() })
		if err != nil {
			h.invalidateTaskIfDenied(ctx, t, err)
		}
	}
}

type taskFlowAgentOption struct {
	ID          string   `json:"id"`
	SessionName string   `json:"session_name"`
	GroupCodes  []string `json:"group_codes"`
	Online      bool     `json:"online"`
}
type taskFlowGroupOption struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (h *Hub) taskFlowOptions(ctx context.Context, owner string) ([]taskFlowGroupOption, []taskFlowAgentOption, error) {
	groups := []taskFlowGroupOption{}
	agents := []taskFlowAgentOption{}
	if h.Identity == nil {
		return groups, agents, nil
	}
	visible, err := h.Identity.ListVisibleGroups(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]*taskFlowAgentOption{}
	for _, g := range visible {
		groups = append(groups, taskFlowGroupOption{g.Code, g.ShortName})
		for _, m := range g.Members {
			a := m.Agent
			if a.State != identity.AgentActive {
				continue
			}
			u, e := h.Identity.Store.GetUser(ctx, a.OwnerLC)
			if e != nil {
				return nil, nil, e
			}
			if u.State != identity.UserActive {
				continue
			}
			option := byID[a.ID]
			if option == nil {
				option = &taskFlowAgentOption{ID: a.ID, SessionName: h.Identity.FullName(ctx, &a), GroupCodes: []string{}, Online: h.sessionOnline(AgentKeyID(a.OwnerLC), a.NameLC())}
				byID[a.ID] = option
			}
			option.GroupCodes = append(option.GroupCodes, g.Code)
		}
	}
	for _, a := range byID {
		agents = append(agents, *a)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].SessionName < agents[j].SessionName })
	return groups, agents, nil
}
