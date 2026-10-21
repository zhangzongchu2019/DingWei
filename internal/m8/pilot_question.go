package m8

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type questionRepository interface {
	QuestionAuthorizationContext(context.Context, string, string, string) (store.QuestionContext, error)
	QuestionPolicy(context.Context, string) (store.QuestionPolicy, error)
	SetQuestionPolicy(context.Context, store.QuestionContext, store.BlockerActor, store.QuestionUpdate, func() error, time.Time) (store.QuestionPolicy, error)
	UpdateQuestion(context.Context, store.QuestionContext, store.BlockerActor, string, store.QuestionUpdate, func() error, time.Time) (store.QuestionResult, error)
	QuestionCandidateIDs(context.Context, store.BlockerActor, string, string, int) ([]string, error)
	ReadQuestion(context.Context, store.QuestionContext, store.BlockerActor, string, int64, func() error, ...int64) (store.PilotQuestion, []store.QuestionAnswer, []store.QuestionChange, error)
	ReadQuestionSummary(context.Context, store.QuestionContext, store.BlockerActor, string, func() error) (store.PilotQuestion, error)
	OpenQuestionIDs(context.Context, string) ([]string, error)
	QueueQuestionReminder(context.Context, store.QuestionContext, string, func() error, time.Time) error
	PendingQuestionNotices(context.Context, string) ([]store.QuestionNotice, error)
	PrepareQuestionNotice(context.Context, store.QuestionContext, store.QuestionNotice, bool, func() error, time.Time, ...string) (store.PilotQuestion, bool, error)
	FinishQuestionNotice(context.Context, string, string, bool, time.Time) error
	QuestionReferences(context.Context, string) (store.PilotQuestion, error)
}

// Worker-only recovery never treats an uncertain provider attempt as unsent.
type unsentQuestionRepository interface {
	DeferUnsentQuestionNotice(context.Context, store.QuestionNotice, string, bool, time.Time) error
}
type QuestionSender func(context.Context, string, string, string, string) (string, error)

func (h *Hub) questionRepo() (questionRepository, error) {
	r, ok := h.Repo.(questionRepository)
	if !ok || h.Identity == nil {
		return nil, errors.New("question store unavailable")
	}
	return r, nil
}
func questionCredential(r *http.Request) (blockerCredential, error) {
	if c, ok := r.Context().Value(blockerCredentialKey{}).(blockerCredential); ok {
		return c, nil
	}
	if len(r.Header.Values("Authorization")) != 0 {
		return blockerCredential{}, store.ErrTaskFlowPermission
	}
	c := blockerCredential{actor: store.BlockerActor{UserID: platformPrincipal(r)}}
	if c.actor.UserID == "" {
		return c, store.ErrTaskFlowPermission
	}
	return c, nil
}
func (h *Hub) withQuestionIdentity(ctx context.Context, c store.QuestionContext, actor blockerCredential, q store.PilotQuestion, extra []string, ownerReadOrCancel bool, fn func(identity.ContextAuthorization, func() error) error) error {
	auth, ok := h.Identity.Store.(identity.QuestionAuthorizationStore)
	if !ok {
		return errors.New("question identity unavailable")
	}
	if c.Archived {
		return store.ErrTaskFlowPermission
	}
	var claim *identity.ContextClaim
	if c.Task != nil && c.Task.GroupCode != "" {
		claim = &identity.ContextClaim{Owner: c.Task.Owner, Group: c.Task.GroupCode, Developer: c.Task.Developer, Reviewer: c.Task.Reviewer}
	}
	users := []string{c.Owner, c.Editor, q.PMUser, q.LeaderUser}
	if strings.HasPrefix(q.Responsible, "user:") {
		users = append(users, strings.TrimPrefix(q.Responsible, "user:"))
	}
	agents, mustActive := []string{}, []string{}
	if strings.HasPrefix(q.Responsible, "agent:") {
		agents = append(agents, strings.TrimPrefix(q.Responsible, "agent:"))
	}
	for _, id := range extra {
		if strings.HasPrefix(id, "agent:") {
			agents = append(agents, strings.TrimPrefix(id, "agent:"))
			mustActive = append(mustActive, strings.TrimPrefix(id, "agent:"))
		} else {
			users = append(users, id)
		}
	}
	return auth.WithQuestionAuthorization(ctx, actor.actor.AgentID, actor.hash, actor.actor.UserID, claim, users, agents, "clarification-spec:"+c.SpecID, func(a identity.ContextAuthorization, check func() error) error {
		if !a.Users[identity.Lower(c.Owner)] {
			return identity.ErrForbidden
		}
		if claim != nil && !a.Allows(*claim) && !(ownerReadOrCancel && actor.actor.UserID == c.Owner) {
			return identity.ErrForbidden
		}
		for _, id := range mustActive {
			if !a.Active(id) {
				return identity.ErrForbidden
			}
		}
		return fn(a, check)
	})
}
func decodeQuestion(w http.ResponseWriter, r *http.Request, u *store.QuestionUpdate) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	if d.Decode(u) != nil || d.Decode(&struct{}{}) != io.EOF {
		taskError(w, store.ErrTaskFlowInvalid)
		return false
	}
	u.PMUser = strings.ToLower(strings.TrimSpace(u.PMUser))
	u.LeaderUser = strings.ToLower(strings.TrimSpace(u.LeaderUser))
	return true
}
func (h *Hub) HandleQuestionPolicy(w http.ResponseWriter, r *http.Request) {
	repo, err := h.questionRepo()
	if err != nil {
		taskError(w, err)
		return
	}
	actor, err := questionCredential(r)
	if err != nil || actor.actor.UserID == "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := repo.QuestionAuthorizationContext(ctx, r.PathValue("specID"), "", "")
	if err != nil {
		taskError(w, err)
		return
	}
	if c.Owner != actor.actor.UserID {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	var u store.QuestionUpdate
	if r.Method == "POST" {
		if !decodeQuestion(w, r, &u) {
			return
		}
		if u.Operation != "" && u.Operation != "configure_policy" {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		u.Operation = "configure_policy"
	} else if r.Method != "GET" {
		taskJSON(w, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	var out store.QuestionPolicy
	err = h.withQuestionIdentity(ctx, c, actor, store.PilotQuestion{}, []string{u.PMUser, u.LeaderUser}, false, func(a identity.ContextAuthorization, check func() error) error {
		if r.Method == "POST" {
			if u.PMUser != "" && !a.Users[u.PMUser] || u.LeaderUser != "" && !a.Users[u.LeaderUser] {
				return store.ErrTaskFlowInvalid
			}
			var e error
			out, e = repo.SetQuestionPolicy(ctx, c, actor.actor, u, check, h.now())
			return e
		}
		current, e := repo.QuestionAuthorizationContext(ctx, c.SpecID, "", "")
		if e != nil {
			return e
		}
		if current.Owner != c.Owner || current.Archived {
			return store.ErrTaskFlowPermission
		}
		out, e = repo.QuestionPolicy(ctx, c.SpecID)
		if e != nil {
			return e
		}
		return check()
	})
	if err != nil {
		taskError(w, err)
		return
	}
	taskJSON(w, 200, map[string]any{"policy": out, "applies": "new_questions_only", "existing_questions_require_explicit_reassignment": true, "execution_authorized": false})
}
func (h *Hub) HandleQuestion(w http.ResponseWriter, r *http.Request) {
	repo, err := h.questionRepo()
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
	if r.Method == "GET" {
		if qid == "" {
			h.listQuestions(ctx, w, r, repo, actor)
			return
		}
		values, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		var before, changeBefore int64
		for key, v := range values {
			if (key != "before" && key != "change_before") || len(v) != 1 {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
			n, x := strconv.ParseInt(v[0], 10, 64)
			e = x
			if key == "before" {
				before = n
			} else {
				changeBefore = n
			}
			if e != nil || n < 1 {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
		}
		c, e := repo.QuestionAuthorizationContext(ctx, "", "", qid)
		if e != nil {
			taskError(w, e)
			return
		}
		ref, e := repo.QuestionReferences(ctx, qid)
		if e != nil {
			taskError(w, e)
			return
		}
		var q store.PilotQuestion
		var answers []store.QuestionAnswer
		var changes []store.QuestionChange
		e = h.withQuestionIdentity(ctx, c, actor, ref, nil, true, func(_ identity.ContextAuthorization, check func() error) error {
			var x error
			q, answers, changes, x = repo.ReadQuestion(ctx, c, actor.actor, qid, before, check, changeBefore)
			return x
		})
		if e != nil {
			taskError(w, e)
			return
		}
		next := int64(0)
		if len(answers) > 100 {
			answers = answers[:100]
			next = answers[len(answers)-1].Sequence
		}
		changeNext := int64(0)
		if len(changes) > 100 {
			changes = changes[:100]
			changeNext = changes[len(changes)-1].Sequence
		}
		for {
			body, e := json.Marshal(map[string]any{"question": q, "answers": answers, "changes": changes, "answers_next_before": next, "answers_complete": next == 0, "changes_next_before": changeNext, "changes_complete": changeNext == 0, "execution_authorized": false})
			if e != nil {
				taskError(w, e)
				return
			}
			if len(body) <= 1<<20 {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write(body)
				return
			}
			if len(changes) > len(answers) && len(changes) > 1 {
				changes = changes[:len(changes)-1]
				changeNext = changes[len(changes)-1].Sequence
			} else if len(answers) > 1 {
				answers = answers[:len(answers)-1]
				next = answers[len(answers)-1].Sequence
			} else {
				taskJSON(w, 413, map[string]any{"error": "question_entry_too_large"})
				return
			}
		}
	}
	if r.Method != "POST" {
		taskJSON(w, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	var u store.QuestionUpdate
	if !decodeQuestion(w, r, &u) {
		return
	}
	if actor.actor.AgentID != "" {
		if u.PMUser != "" || u.LeaderUser != "" || u.Responsible != "" || u.Reason != "" || u.ChangeID != "" || u.NewRevision != 0 || u.NewSpecID != "" {
			returnQuestionDenied(w)
			return
		}
		if qid != "" {
			if u.Operation != "" && u.Operation != "input" {
				taskError(w, store.ErrTaskFlowPermission)
				return
			}
			u.Operation = "input"
		} else {
			if u.Operation != "" && u.Operation != "create" {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
			u.Operation = "create"
			task := r.PathValue("taskID")
			if task == "" || u.TaskID != "" && u.TaskID != task {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
			u.TaskID = task
		}
	} else if qid == "" {
		if u.Operation != "" && u.Operation != "create" {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		u.Operation = "create"
	}
	c, err := repo.QuestionAuthorizationContext(ctx, r.PathValue("specID"), u.TaskID, qid)
	if err != nil {
		taskError(w, err)
		return
	}
	ref := store.PilotQuestion{}
	if qid != "" {
		ref, err = repo.QuestionReferences(ctx, qid)
		if err != nil {
			taskError(w, err)
			return
		}
	}
	// Client-selected recipients are stable platform principals. The actor is
	// always the authenticated session/bearer; no external callback is accepted.
	if strings.HasPrefix(u.Responsible, "agent:") {
		if c.Task == nil {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		a, e := h.Identity.Store.GetAgent(ctx, strings.TrimPrefix(u.Responsible, "agent:"))
		if e != nil || a == nil || a.State != identity.AgentActive {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		owner, e := h.Identity.Store.GetUser(ctx, a.OwnerLC)
		if e != nil || owner == nil || owner.State != identity.UserActive {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
	}
	extra := []string{u.PMUser, u.LeaderUser}
	if strings.HasPrefix(u.Responsible, "agent:") {
		extra = append(extra, u.Responsible)
	}
	if strings.HasPrefix(u.Responsible, "user:") {
		extra = append(extra, strings.TrimPrefix(u.Responsible, "user:"))
	}
	var out store.QuestionResult
	err = h.withQuestionIdentity(ctx, c, actor, ref, extra, (u.Operation == "cancel" || u.Operation == "reject_change" || u.Operation == "confirm_change") && actor.actor.UserID == c.Owner, func(a identity.ContextAuthorization, check func() error) error {
		for _, id := range extra {
			if id != "" && !strings.HasPrefix(id, "agent:") && !a.Users[id] {
				return store.ErrTaskFlowInvalid
			}
		}
		var e error
		out, e = repo.UpdateQuestion(ctx, c, actor.actor, qid, u, check, h.now())
		return e
	})
	if err != nil {
		taskError(w, err)
		return
	}
	taskJSON(w, 200, out)
}
func questionListScope(spec, task string) string {
	b, _ := json.Marshal([]string{spec, task})
	return fmt.Sprintf("pilot-questions:%x", sha256.Sum256(b))
}
func (h *Hub) listQuestions(ctx context.Context, w http.ResponseWriter, r *http.Request, repo questionRepository, actor blockerCredential) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	limit := 50
	spec, task, cursor := "", "", ""
	for key, v := range values {
		if len(v) != 1 {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		switch key {
		case "limit":
			limit, err = strconv.Atoi(v[0])
			if err != nil || limit < 1 || limit > 100 {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
		case "spec_id":
			spec = v[0]
		case "task_id":
			task = v[0]
		case "cursor":
			cursor = v[0]
		default:
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
	}
	if len(spec) > 256 || len(task) > 256 {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	scope := questionListScope(spec, task)
	after := ""
	if cursor != "" {
		c, e := h.decodeContextCursor(cursor, actor.actor.Key(), scope, limit)
		if e != nil {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		after = c.TaskAfter
	}
	out := []store.PilotQuestion{}
	more := false
	examined := 0
	for len(out) < limit+1 && examined < 1000 {
		ids, e := repo.QuestionCandidateIDs(ctx, actor.actor, spec, after, 101)
		if e != nil {
			taskError(w, e)
			return
		}
		if len(ids) == 0 {
			more = false
			break
		}
		more = len(ids) == 101
		for _, id := range ids {
			after = id
			examined++
			c, e := repo.QuestionAuthorizationContext(ctx, "", "", id)
			if e != nil {
				taskError(w, e)
				return
			}
			if task != "" && c.TaskID != task {
				continue
			}
			ref, e := repo.QuestionReferences(ctx, id)
			if e != nil {
				taskError(w, e)
				return
			}
			var q store.PilotQuestion
			e = h.withQuestionIdentity(ctx, c, actor, ref, nil, true, func(_ identity.ContextAuthorization, check func() error) error {
				var x error
				q, x = repo.ReadQuestionSummary(ctx, c, actor.actor, id, check)
				return x
			})
			if e != nil {
				if errors.Is(e, store.ErrTaskFlowPermission) || errors.Is(e, store.ErrTaskFlowConflict) || errors.Is(e, identity.ErrForbidden) || errors.Is(e, identity.ErrNotFound) {
					continue
				}
				taskError(w, e)
				return
			}
			out = append(out, q)
			if len(out) > limit {
				more = true
				break
			}
		}
		if len(ids) < 101 {
			more = false
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
		after = out[len(out)-1].ID
		more = true
	}
	if examined >= 1000 {
		more = true
	}
	for {
		next := ""
		if more {
			next, err = h.encodeContextCursor(contextCursor{Actor: actor.actor.Key(), Scope: scope, Limit: limit, TaskAfter: after, Expires: h.now().Add(15 * time.Minute).Unix()})
			if err != nil {
				taskError(w, err)
				return
			}
		}
		body, e := json.Marshal(map[string]any{"questions": out, "next_cursor": next, "execution_authorized": false})
		if e != nil {
			taskError(w, e)
			return
		}
		if len(body) <= 1<<20 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(body)
			return
		}
		if len(out) <= 1 {
			taskJSON(w, 413, map[string]any{"error": "question_entry_too_large"})
			return
		}
		out = out[:len(out)-1]
		after = out[len(out)-1].ID
		more = true
	}
}

func returnQuestionDenied(w http.ResponseWriter) { taskError(w, store.ErrTaskFlowPermission) }

func (h *Hub) HandleQuestionCreateContext(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		taskJSON(w, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	repo, err := h.questionRepo()
	if err != nil {
		taskError(w, err)
		return
	}
	actor, err := questionCredential(r)
	if err != nil || actor.actor.UserID == "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	task, cursor := "", ""
	limit := 50
	explicit := map[string]bool{}
	for k, v := range values {
		if k != "allow_user" && len(v) != 1 {
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
		switch k {
		case "task_id":
			task = v[0]
		case "limit":
			limit, e = strconv.Atoi(v[0])
			if e != nil || limit < 1 || limit > 100 {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
		case "cursor":
			cursor = v[0]
		case "allow_user":
			if len(v) > 20 {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
			for _, id := range v {
				id = identity.Lower(strings.TrimSpace(id))
				if id == "" || len(id) > 128 || strings.ContainsAny(id, " :/\\") {
					taskError(w, store.ErrTaskFlowInvalid)
					return
				}
				explicit[id] = true
			}
		default:
			taskError(w, store.ErrTaskFlowInvalid)
			return
		}
	}
	if len(task) > 256 || len(cursor) > 4096 {
		taskError(w, store.ErrTaskFlowInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := repo.QuestionAuthorizationContext(ctx, r.PathValue("specID"), task, "")
	if err != nil {
		taskError(w, err)
		return
	}
	if (c.Task == nil && actor.actor.UserID != c.Owner && actor.actor.UserID != c.Editor) || (c.Task != nil && actor.actor.UserID != c.Task.Owner) {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	err = h.withQuestionIdentity(ctx, c, actor, store.PilotQuestion{}, nil, false, func(_ identity.ContextAuthorization, check func() error) error { return check() })
	if err != nil {
		taskError(w, err)
		return
	}
	contacts := []map[string]string{}
	next := ""
	if actor.actor.UserID == c.Owner {
		auth, ok := h.Identity.Store.(identity.QuestionContactAuthorizationStore)
		if !ok {
			taskError(w, errors.New("question contacts authorization unavailable"))
			return
		}
		groups := map[string]bool{}
		if c.Task != nil {
			groups[c.Task.GroupCode] = true
		} else {
			owned, x := h.Identity.Store.ListAgents(ctx, c.Owner)
			if x != nil {
				taskError(w, x)
				return
			}
			for _, ag := range owned {
				if ag.State != identity.AgentActive {
					continue
				}
				gs, x := h.Identity.Store.ListAgentGroups(ctx, ag.ID)
				if x != nil {
					taskError(w, x)
					return
				}
				for _, g := range gs {
					groups[g] = true
				}
			}
			all, x := h.Identity.Store.ListGroups(ctx)
			if x != nil {
				taskError(w, x)
				return
			}
			for _, g := range all {
				if g.CreatorLC == c.Owner {
					groups[g.Code] = true
				}
			}
		}
		policy, x := repo.QuestionPolicy(ctx, c.SpecID)
		if x != nil {
			taskError(w, x)
			return
		}
		allow := map[string]bool{c.Owner: true}
		for id := range explicit {
			allow[id] = true
		}
		for _, id := range []string{policy.PMUser, policy.LeaderUser} {
			if id != "" {
				allow[id] = true
			}
		}
		gs := []string{}
		for g := range groups {
			if g != "" {
				gs = append(gs, g)
			}
		}
		sort.Strings(gs)
		us := []string{}
		for id := range allow {
			us = append(us, id)
		}
		sort.Strings(us)
		raw, _ := json.Marshal([]any{c.SpecID, task, us})
		scope := fmt.Sprintf("question-contacts:%x", sha256.Sum256(raw))
		after, oldDigest := "", ""
		if cursor != "" {
			page, x := h.decodeContextCursor(cursor, actor.actor.Key(), scope, limit)
			if x != nil {
				taskError(w, store.ErrTaskFlowInvalid)
				return
			}
			after = page.SessionAfter
			oldDigest = page.Auth
		}
		err = auth.WithQuestionContacts(ctx, c.Owner, gs, us, func(a identity.ContextAuthorization, check func() error) error {
			if c.Task != nil && c.Task.GroupCode != "" && !a.Allows(identity.ContextClaim{Owner: c.Task.Owner, Group: c.Task.GroupCode, Developer: c.Task.Developer, Reviewer: c.Task.Reviewer}) {
				return identity.ErrForbidden
			}
			now, x := repo.QuestionAuthorizationContext(ctx, c.SpecID, task, "")
			if x != nil {
				return x
			}
			if !reflect.DeepEqual(now, c) {
				return store.ErrTaskFlowConflict
			}
			allowed := map[string]bool{}
			for id := range allow {
				if a.Users[id] {
					allowed[id] = true
				}
			}
			for g, group := range a.Groups {
				ownerIn := group.CreatorLC == c.Owner
				for id := range a.Members[g] {
					if a.Agents[id].OwnerLC == c.Owner && a.Active(id) {
						ownerIn = true
					}
				}
				if !ownerIn {
					continue
				}
				for id := range a.Members[g] {
					if a.Active(id) {
						allowed[a.Agents[id].OwnerLC] = true
					}
				}
			}
			digest := a.Digest()
			if oldDigest != "" && oldDigest != digest {
				return errContextChanged
			}
			ids := []string{}
			for id := range allowed {
				if id > after {
					ids = append(ids, id)
				}
			}
			sort.Strings(ids)
			for i, id := range ids {
				if i == limit {
					var x error
					next, x = h.encodeContextCursor(contextCursor{Actor: actor.actor.Key(), Scope: scope, Limit: limit, Auth: digest, SessionAfter: ids[i-1], Expires: h.now().Add(5 * time.Minute).Unix()})
					if x != nil {
						return x
					}
					break
				}
				contacts = append(contacts, map[string]string{"id": id, "display_name": a.Names[id]})
			}
			if x = check(); x != nil {
				return x
			}
			now, x = repo.QuestionAuthorizationContext(ctx, c.SpecID, task, "")
			if x != nil {
				return x
			}
			if !reflect.DeepEqual(now, c) {
				return store.ErrTaskFlowConflict
			}
			return nil
		})
		if err != nil {
			contextError(w, err)
			return
		}
	} else if len(explicit) > 0 || cursor != "" {
		taskError(w, store.ErrTaskFlowPermission)
		return
	}
	version := c.Revision
	if c.Task != nil {
		version = c.Task.Version
	}
	anchorsRepo, ok := h.Repo.(interface {
		ClarificationCreateAnchors(context.Context, store.QuestionContext, func() error) ([]string, bool, error)
	})
	if !ok {
		taskJSON(w, 503, map[string]any{"error": "anchored_question_unavailable"})
		return
	}
	var refs []string
	var truncated bool
	err = h.withQuestionIdentity(ctx, c, actor, store.PilotQuestion{}, nil, false, func(_ identity.ContextAuthorization, check func() error) error {
		var e error
		refs, truncated, e = anchorsRepo.ClarificationCreateAnchors(ctx, c, check)
		return e
	})
	if err != nil {
		taskError(w, err)
		return
	}
	taskJSON(w, 200, map[string]any{"spec_id": c.SpecID, "task_id": task, "expected_version": version, "entry_refs": refs, "anchors_truncated": truncated, "can_configure_policy": actor.actor.UserID == c.Owner, "contacts": contacts, "contact_limit": limit, "contacts_next_cursor": next, "execution_authorized": false})
}
