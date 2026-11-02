package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

type taskDispatchContextKey struct{}

func (h *Hub) taskDispatchService() *td.Service {
	if !td.Enabled() {
		return nil
	}
	repo, ok := h.Repo.(td.Repository)
	if !ok || h.Identity == nil || h.SpecReader == nil {
		return nil
	}
	return &td.Service{Repo: repo, Identity: h.Identity.Store, Reader: h.SpecReader, Now: h.now, Prepare: func(ctx context.Context, owner string) error { return h.syncNewPlatformUser(ctx, owner, nil) }, AuditFailure: func(ctx context.Context, id, reason string) {
		_ = h.Repo.WriteAudit(ctx, "system", "r10_operation_failed."+reason, id)
	}}
}
func taskDispatchError(w http.ResponseWriter, err error) {
	status := 503
	code := "r10_unavailable"
	switch {
	case errors.Is(err, td.ErrInvalid):
		status = 400
		code = td.ErrInvalid.Error()
	case errors.Is(err, td.ErrForbidden), errors.Is(err, identity.ErrForbidden), errors.Is(err, identity.ErrNotFound):
		status = 403
		code = td.ErrForbidden.Error()
	case errors.Is(err, td.ErrNotFound):
		status = 404
		code = td.ErrNotFound.Error()
	case errors.Is(err, td.ErrConflict), errors.Is(err, td.ErrClock), errors.Is(err, td.ErrNeedsSync), errors.Is(err, td.ErrComparison), errors.Is(err, td.ErrPRRequired), errors.Is(err, td.ErrCompleted):
		status = 409
		code = err.Error()
	}
	payload := map[string]any{"error": code}
	if status >= 500 {
		payload["outcome"] = "unknown"
		payload["retryable"] = false
	}
	if errors.Is(err, td.ErrPRRequired) {
		payload["hint"] = "先登记PR"
	}
	taskJSON(w, status, payload)
}
func (h *Hub) HandleTaskDispatchLock(w http.ResponseWriter, r *http.Request) {
	h.handleTaskDispatchLock(w, r, false)
}
func (h *Hub) HandleAgentTaskDispatchLock(w http.ResponseWriter, r *http.Request) {
	h.handleTaskDispatchLock(w, r, true)
}
func (h *Hub) handleTaskDispatchLock(w http.ResponseWriter, r *http.Request, agent bool) {
	if !td.Enabled() {
		http.NotFound(w, r)
		return
	}
	svc := h.taskDispatchService()
	if svc == nil {
		taskDispatchError(w, td.ErrUnavailable)
		return
	}
	actor := td.Actor{UserLC: platformPrincipal(r)}
	if agent {
		actor = td.Actor{}
		actor.AgentID, _ = r.Context().Value(taskAgentContextKey{}).(string)
	}
	if r.Method == http.MethodGet {
		k := td.Key{Provider: r.URL.Query().Get("provider"), Instance: r.URL.Query().Get("instance"), Repository: r.URL.Query().Get("repository"), ChangeDir: r.URL.Query().Get("change_dir"), TaskID: r.URL.Query().Get("task_id")}
		after := int64(0)
		if raw := r.URL.Query().Get("after_event"); raw != "" {
			n, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || n < 0 {
				taskDispatchError(w, td.ErrInvalid)
				return
			}
			after = n
		}
		out, e := svc.InspectAfter(r.Context(), actor, k, r.URL.Query().Get("operation_id"), after)
		if e != nil {
			taskDispatchError(w, e)
			return
		}
		taskJSON(w, 200, out)
		return
	}
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
		taskDispatchError(w, td.ErrInvalid)
		return
	}
	if !agent {
		origin, e := url.Parse(r.Header.Get("Origin"))
		if e != nil || origin.Host != r.Host || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || (origin.Scheme != "http" && origin.Scheme != "https") || (h.PublicOrigin != "" && r.Header.Get("Origin") != h.PublicOrigin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			taskDispatchError(w, td.ErrForbidden)
			return
		}
	}
	var req td.Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&req); e != nil {
		taskDispatchError(w, td.ErrInvalid)
		return
	}
	if e := decoder.Decode(new(any)); e != io.EOF {
		taskDispatchError(w, td.ErrInvalid)
		return
	}
	result, e := svc.Execute(r.Context(), actor, req)
	if e != nil {
		taskDispatchError(w, e)
		return
	}
	taskJSON(w, 200, result)
}

// The lifetime starts only after Identity and R5 have been wired. Dependency
// pointers are captured before launching the worker, avoiding a startup race.
func (h *Hub) StartTaskDispatchLockMonitor(ctx context.Context) {
	if !td.Enabled() {
		return
	}
	svc := h.taskDispatchService()
	if svc == nil {
		return
	}
	// Polling remote repositories may consume its full deadline. Routing has a
	// separate owned loop so a slow poll cannot delay already committed notices.
	h.backgroundWork("owned", func() {
		poll := time.NewTicker(time.Minute)
		defer poll.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.backgroundContext().Done():
				return
			case <-poll.C:
				_ = svc.Tick(ctx)
			}
		}
	})
	h.backgroundWork("owned", func() {
		_ = svc.RecoverNotices(ctx)
		notice := time.NewTicker(time.Second)
		defer notice.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.backgroundContext().Done():
				return
			case <-notice.C:
				h.dispatchTaskLockNotices(ctx, svc)
			}
		}
	})
}
func (h *Hub) dispatchTaskLockNotices(ctx context.Context, svc *td.Service) {
	if !td.Enabled() {
		return
	}
	var after time.Time
	afterID := ""
	for ctx.Err() == nil {
		notices, e := svc.Repo.TaskDispatchNoticesAfter(ctx, "pending", 1000, after, afterID)
		if e != nil || len(notices) == 0 {
			return
		}
		last := notices[len(notices)-1]
		after = last.CreatedAt
		afterID = last.ID
		for _, n := range notices {
			l, e := svc.Repo.TaskDispatchLock(ctx, n.Key)
			if e != nil {
				continue
			}
			if l == nil || l.Generation != n.Generation || ((n.Type == "grant" || n.Type == "handoff") && !l.Effective(h.now())) {
				_, _ = svc.Repo.UpdateTaskDispatchNotice(ctx, n.ID, "pending", "superseded", "", "generation_changed", h.now())
				continue
			}
			if _, e = svc.AuthorizeNotice(ctx, n); e != nil {
				_, _ = svc.Repo.UpdateTaskDispatchNotice(ctx, n.ID, "pending", "unknown", "", "recipient_access_unknown", h.now())
				continue
			}
			if n.RecipientKind == "user" {
				_, _ = svc.Repo.UpdateTaskDispatchNotice(ctx, n.ID, "pending", "available", "", "console_notice_available", h.now())
				continue
			}
			a, e := svc.Identity.GetAgent(ctx, n.RecipientID)
			if e != nil || a == nil || a.OwnerLC != n.RecipientOwnerLC || a.State != identity.AgentActive {
				continue
			}
			key, name := AgentKeyID(a.OwnerLC), a.NameLC()
			h.mu.Lock()
			client := h.sessionClients[key][name]
			h.mu.Unlock()
			if client == nil {
				continue
			}
			client.stateMu.Lock()
			capable := client.taskFlowV1 && client.deliveryACKV1
			client.stateMu.Unlock()
			if !capable {
				continue
			}
			claimed, e := svc.Repo.UpdateTaskDispatchNotice(ctx, n.ID, "pending", "attempting", "", "", h.now())
			if e != nil || !claimed {
				continue
			}
			recovery := ""
			if l.Handoff.ExecutionCommit != "" {
				recovery = "；旧执行恢复状态=" + l.Handoff.RecoveryState
			}
			pr := "PR已登记"
			if l.PRDimension == "pr_not_opened" {
				pr = "未开PR"
			}
			body := fmt.Sprintf("任务锁提醒：%s；仓库 %s:%s:%s；%s/%s；固定C=%s；ref=%s；PR head=%s；PR state=%s；比较状态=%s；策略=%s；generation=%d；%s。请通过任务锁接口重核当前资格与锁状态。", n.Type, n.Key.Provider, n.Key.Instance, n.Key.Repository, n.Key.ChangeDir, n.Key.TaskID, l.CommitSHA, l.ObservedCommitSHA, l.PRHeadSHA, l.PRRemoteState, l.ComparisonState, td.ComparisonPolicyVersion(), n.Generation, pr+recovery)
			env := model.Envelope{ID: n.SourceEnvelopeID, From: sessionAddress(systemAddressName, key), To: sessionAddress(name, key), TS: h.now().Unix(), Body: body, Meta: map[string]any{"kind": "r10_task_notice", "r10_notice_id": n.ID, "r10_key": n.Key, "reply_policy": "no_reply", "conversation_terminal": true}}
			e = h.RouteEnvelope(context.WithValue(ctx, taskDispatchContextKey{}, n.ID), env)
			state, reason := "queued", "hub_queued_not_opened"
			if e != nil {
				state = "unknown"
				reason = "route_outcome_unknown"
			}
			_, _ = svc.Repo.UpdateTaskDispatchNotice(ctx, n.ID, "attempting", state, n.SourceEnvelopeID, reason, h.now())
		}
	}
}

// Untagged envelopes return before any R10 service/table/identity access.
func (h *Hub) taskDispatchEnvelopeAllowed(ctx context.Context, env model.Envelope) error {
	if metaString(env.Meta, "kind") != "r10_task_notice" && !strings.HasPrefix(metaString(env.Meta, "kind"), "r10_") {
		return nil
	}
	if !td.Enabled() {
		return td.ErrForbidden
	}
	id := metaString(env.Meta, "r10_notice_id")
	trusted, _ := ctx.Value(taskDispatchContextKey{}).(string)
	if id == "" || trusted != id {
		return td.ErrForbidden
	}
	svc := h.taskDispatchService()
	if svc == nil {
		return td.ErrUnavailable
	}
	n, e := svc.Repo.TaskDispatchNotice(ctx, id)
	if e != nil || n == nil || n.State != "attempting" || n.SourceEnvelopeID != env.ID {
		return td.ErrForbidden
	}
	l, e := svc.Repo.TaskDispatchLock(ctx, n.Key)
	if e != nil || l == nil || l.Generation != n.Generation {
		return td.ErrConflict
	}
	a, e := svc.Identity.GetAgent(ctx, n.RecipientID)
	if e != nil || a == nil || a.State != identity.AgentActive || a.OwnerLC != n.RecipientOwnerLC {
		return td.ErrForbidden
	}
	if env.To != sessionAddress(a.NameLC(), AgentKeyID(a.OwnerLC)) {
		return td.ErrForbidden
	}
	if _, e = svc.AuthorizeNotice(ctx, *n); e != nil {
		return e
	}
	return nil
}
