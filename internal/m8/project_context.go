package m8

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

const contextSchema = "hub-project-context-v1"
const contextMaxBytes = 256 << 10

var errContextCursor = errors.New("invalid context cursor")
var errContextCursorActor = errors.New("context cursor actor")
var errContextChanged = errors.New("context changed")
var errContextLarge = errors.New("context entry too large")

type projectContextRepo interface {
	ProjectContextCandidates(context.Context, string, string, int) ([]store.ContextCandidate, error)
	WithProjectContextSnapshot(context.Context, string, []store.ContextCandidate, func(*store.ContextSnapshot) error) error
}
type contextCursor struct {
	Actor        string `json:"a"`
	Scope        string `json:"s"`
	Limit        int    `json:"l"`
	Auth         string `json:"h"`
	Expires      int64  `json:"e"`
	TaskAfter    string `json:"t"`
	SessionAfter string `json:"p"`
	TasksDone    bool   `json:"td"`
	SessionsDone bool   `json:"pd"`
}
type contextAgent struct {
	ID     string   `json:"agent_id"`
	Name   string   `json:"full_name"`
	Groups []string `json:"groups"`
}
type contextPage[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next_cursor"`
}

func (h *Hub) contextKey() ([]byte, error) {
	h.projectContextCursorOnce.Do(func() { _, h.projectContextCursorErr = rand.Read(h.projectContextCursorKey[:]) })
	return h.projectContextCursorKey[:], h.projectContextCursorErr
}
func (h *Hub) cursorAEAD() (cipher.AEAD, error) {
	key, err := h.contextKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (h *Hub) encodeContextCursor(c contextCursor) (string, error) {
	aead, err := h.cursorAEAD()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, raw, []byte(contextSchema))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}
func (h *Hub) decodeContextCursor(raw string, actor, scope string, limit int) (contextCursor, error) {
	var c contextCursor
	if len(raw) > 4096 {
		return c, errContextCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, errContextCursor
	}
	aead, err := h.cursorAEAD()
	if err != nil {
		return c, err
	}
	if len(b) < aead.NonceSize() {
		return c, errContextCursor
	}
	plain, err := aead.Open(nil, b[:aead.NonceSize()], b[aead.NonceSize():], []byte(contextSchema))
	if err != nil || json.Unmarshal(plain, &c) != nil {
		return c, errContextCursor
	}
	if c.Actor != actor {
		return c, errContextCursorActor
	}
	if c.Scope != scope || c.Limit != limit || c.Expires <= h.now().Unix() || c.Expires > h.now().Add(5*time.Minute).Unix() {
		return c, errContextCursor
	}
	return c, nil
}
func contextError(w http.ResponseWriter, err error) {
	status, code := 503, "project_context_unavailable"
	switch {
	case errors.Is(err, errContextCursor), errors.Is(err, store.ErrTaskFlowInvalid):
		status, code = 400, "invalid_context_request"
	case errors.Is(err, errContextCursorActor), errors.Is(err, identity.ErrForbidden):
		status, code = 403, "context_access_denied"
	case errors.Is(err, errContextChanged), errors.Is(err, identity.ErrConflict), errors.Is(err, store.ErrTaskFlowConflict):
		status, code = 409, "context_changed"
	case errors.Is(err, errContextLarge):
		status, code = 413, "context_entry_too_large"
	}
	taskJSON(w, status, map[string]any{"error": code})
}

// Authentication is separate from the credential/state recheck under PG locks.
func (h *Hub) HandleProjectContext(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		taskJSON(w, 405, map[string]any{"error": "method_not_allowed"})
		return
	}
	for _, key := range []string{"X-Agent-ID", "X-Owner-ID", "X-User-ID", "X-Group-Code", "X-DW-Agent-ID", "X-API-Key", "X-Service-Key", "X-DW-Console-Principal", "X-DW-Console-Capability-Verified"} {
		if len(r.Header.Values(key)) > 0 {
			taskJSON(w, 400, map[string]any{"error": "identity_selector_rejected"})
			return
		}
	}
	auths := r.Header.Values("Authorization")
	auth := strings.Fields(r.Header.Get("Authorization"))
	if h.Identity == nil || len(auths) != 1 || len(auth) != 2 || !strings.EqualFold(auth[0], "Bearer") || len(r.Header.Values("Cookie")) != 0 {
		taskJSON(w, 401, map[string]any{"error": "agent_auth_required"})
		return
	}
	actor, _, err := h.Identity.AuthenticateAgent(r.Context(), auth[1])
	if err != nil {
		if errors.Is(err, identity.ErrCredentialInvalid) || errors.Is(err, identity.ErrCredentialRevoked) || errors.Is(err, identity.ErrCredentialSuspended) {
			taskJSON(w, 403, map[string]any{"error": "agent_inactive_or_invalid"})
		} else {
			contextError(w, err)
		}
		return
	}
	if actor == nil {
		contextError(w, errors.New("identity unavailable"))
		return
	}
	q, err := parseContextQuery(r)
	if err != nil {
		contextError(w, err)
		return
	}
	cursor := contextCursor{Actor: actor.ID, Scope: q.scope, Limit: q.limit, Expires: h.now().Add(5 * time.Minute).Unix()}
	if q.cursor != "" {
		cursor, err = h.decodeContextCursor(q.cursor, actor.ID, q.scope, q.limit)
		if err != nil {
			contextError(w, err)
			return
		}
	}
	repository, ok := h.Repo.(projectContextRepo)
	if !ok {
		contextError(w, errors.New("context store unavailable"))
		return
	}
	authorization, ok := h.Identity.Store.(identity.ProjectContextAuthorizationStore)
	if !ok {
		contextError(w, errors.New("context identity unavailable"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	candidates := []store.ContextCandidate{}
	wantsTasks := q.scope != "sessions"
	if wantsTasks && !cursor.TasksDone {
		candidates, err = repository.ProjectContextCandidates(ctx, actor.ID, cursor.TaskAfter, q.limit+1)
		if err != nil {
			contextError(w, err)
			return
		}
	}
	var output []byte
	err = authorization.WithProjectContextAuthorization(ctx, actor.ID, identity.HashSecret(auth[1]), nil, func(a identity.ContextAuthorization, check func() error) error {
		return repository.WithProjectContextSnapshot(ctx, actor.ID, candidates, func(snapshot *store.ContextSnapshot) error {
			observedAt := h.now().UTC()
			fingerprint, e := snapshot.Fingerprint()
			if e != nil {
				return e
			}
			// Bind all current authorizations, task versions and private-directory flags.
			digest := sha256.New()
			digest.Write([]byte(a.Digest()))
			digest.Write([]byte(fingerprint))
			ids := make([]string, 0, len(a.Agents))
			for id := range a.Agents {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			visibility := map[string]bool{}
			for _, id := range ids {
				peer := a.Agents[id]
				visible, e := snapshot.EndpointVisible(AgentKeyID(peer.OwnerLC), peer.NameLC())
				if e != nil {
					return e
				}
				visibility[id] = visible
				raw, _ := json.Marshal([]any{id, visible})
				digest.Write(raw)
			}
			currentDigest := hex.EncodeToString(digest.Sum(nil))
			if cursor.Auth != "" && cursor.Auth != currentDigest {
				return errContextChanged
			}
			cursor.Auth = currentDigest
			tasks := []store.ContextTask{}
			specs := []store.ContextSpec{}
			sessions := []contextAgent{}
			next := cursor
			if wantsTasks && !cursor.TasksDone {
				next.TasksDone = len(candidates) <= q.limit
				for i, c := range candidates {
					if i == q.limit {
						break
					}
					state := c.State
					if !state.Archived && !state.NeedsReassignment && (actor.ID == state.Developer || actor.ID == state.Reviewer) && a.Allows(identity.ContextClaim{Owner: state.Owner, Group: state.GroupCode, Developer: state.Developer, Reviewer: state.Reviewer}) {
						task, spec, e := snapshot.Project(c)
						if e != nil {
							return e
						}
						tasks = append(tasks, task)
						specs = append(specs, spec)
					}
					next.TaskAfter = state.ID
				}
			}
			if q.scope == "sessions" || q.scope == "all" {
				if !cursor.SessionsDone {
					h.mu.Lock()
					online := map[string]string{}
					for key, clients := range h.sessionClients {
						for name, client := range clients {
							if client != nil {
								online[key+"\x00"+name] = client.metricsAgentID
							}
						}
					}
					h.mu.Unlock()
					n := 0
					next.SessionsDone = true
					for _, id := range ids {
						if id <= cursor.SessionAfter {
							continue
						}
						if n == q.limit {
							next.SessionsDone = false
							break
						}
						peer := a.Agents[id]
						n++
						next.SessionAfter = id
						if !a.Active(id) || !visibility[id] || online[AgentKeyID(peer.OwnerLC)+"\x00"+peer.NameLC()] != id {
							continue
						}
						names := []string{}
						for code, g := range a.Groups {
							if a.Member(code, actor.ID) && a.Member(code, id) {
								names = append(names, g.ShortName)
							}
						}
						if len(names) == 0 {
							continue
						}
						sort.Strings(names)
						sessions = append(sessions, contextAgent{id, peer.Name + "." + a.Names[peer.OwnerLC], names})
					}
				}
			} else {
				next.SessionsDone = true
			}
			if !wantsTasks {
				next.TasksDone = true
			}
			// Shrink only at complete task/session item boundaries; cursors never skip data.
			for {
				more := !next.TasksDone || !next.SessionsDone
				token := ""
				if more {
					token, e = h.encodeContextCursor(next)
					if e != nil {
						return e
					}
				}
				result := map[string]any{"meta": map[string]any{"schema": contextSchema, "source": "current_hub", "generated_at": h.now().UTC().Format(time.RFC3339Nano), "observed_at": observedAt.Format(time.RFC3339Nano), "scope": q.scope, "complete": !more, "next_cursor": token}}
				if q.scope == "tasks" || q.scope == "all" {
					result["tasks"] = contextPage[store.ContextTask]{tasks, token}
				}
				if q.scope == "sessions" || q.scope == "all" {
					result["sessions"] = contextPage[contextAgent]{sessions, token}
				}
				if q.scope == "config" || q.scope == "all" {
					result["config"] = map[string]any{"context_schema": contextSchema, "capabilities": []string{"project_context_v1"}, "page_limit": 100, "task_states": []string{"planned", "in_progress", "blocked", "done"}, "human_signoff_required": true, "task_done_is_human_signoff": false, "case_confirmation": "assigned_reviewer_only", "specs": contextPage[store.ContextSpec]{specs, token}}
				}
				output, e = json.Marshal(result)
				if e != nil {
					return e
				}
				if len(output) <= contextMaxBytes {
					break
				}
				if len(tasks)+len(sessions) <= 1 {
					return errContextLarge
				}
				if len(tasks) > 0 {
					tasks = tasks[:len(tasks)-1]
					specs = specs[:len(specs)-1]
					next.TasksDone = false
					next.TaskAfter = cursor.TaskAfter
					if len(tasks) > 0 {
						next.TaskAfter = tasks[len(tasks)-1].ID
					}
				} else {
					sessions = sessions[:len(sessions)-1]
					next.SessionsDone = false
					next.SessionAfter = cursor.SessionAfter
					if len(sessions) > 0 {
						next.SessionAfter = sessions[len(sessions)-1].ID
					}
				}
			}
			return check()
		})
	})
	if err != nil {
		contextError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(output)
}

type contextQuery struct {
	scope, cursor string
	limit         int
}

func parseContextQuery(r *http.Request) (contextQuery, error) {
	q := contextQuery{scope: r.URL.Query().Get("scope"), cursor: r.URL.Query().Get("cursor"), limit: 50}
	parsed, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, store.ErrTaskFlowInvalid
	}
	for k, vs := range parsed {
		if len(vs) != 1 || (k != "scope" && k != "limit" && k != "cursor") {
			return q, store.ErrTaskFlowInvalid
		}
	}
	if q.scope != "tasks" && q.scope != "sessions" && q.scope != "config" && q.scope != "all" {
		return q, store.ErrTaskFlowInvalid
	}
	if values, present := parsed["limit"]; present {
		n, e := strconv.Atoi(values[0])
		if e != nil || n < 1 || n > 100 {
			return q, store.ErrTaskFlowInvalid
		}
		q.limit = n
	}
	return q, nil
}
