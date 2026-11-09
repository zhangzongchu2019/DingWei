package m8

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type subAgentViewRepository interface {
	ListSubAgentNodes(context.Context, string) ([]subagent.Node, error)
}

func subViewID(root, node string, generation int64) string {
	sum := sha256.Sum256([]byte(root + "\x00" + node + "\x00" + strconv.FormatInt(generation, 10)))
	return "sub-v1-" + hex.EncodeToString(sum[:])
}
func subViewSession(root, node string, generation int64) string {
	return root + "/" + subViewID("", node, generation)
}
func viewRoot(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return name
}

// Caller must authorize the root first. A child is a scope, never a principal.
func (h *Hub) subViewAgent(ctx context.Context, root consolemux.Agent, name string) (consolemux.Agent, error) {
	if name == root.FullName {
		return root, nil
	}
	route, err := subagent.Parse(name, "")
	if err != nil || len(route.Path) != 1 || viewRoot(name) != root.FullName {
		return consolemux.Agent{}, subagent.Fail("unsupported", "phase_not_enabled")
	}
	h.mu.Lock()
	c := h.sessionClients[AgentKeyID(root.Owner)][root.Session]
	h.mu.Unlock()
	capable, ready := h.subAgentCapable(c)
	if !capable {
		return consolemux.Agent{}, subagent.Fail("unsupported", "子 Agent 查看未启用")
	}
	if !ready {
		return consolemux.Agent{}, subagent.Fail("registry_unavailable", "树尚未确认")
	}
	repo, ok := h.Repo.(subAgentViewRepository)
	if !ok {
		return consolemux.Agent{}, subagent.Fail("registry_unavailable", "树存储不可用")
	}
	nodes, err := repo.ListSubAgentNodes(ctx, root.ID)
	if err != nil {
		return consolemux.Agent{}, err
	}
	for _, n := range nodes {
		if n.Name == route.Path[0] {
			return consolemux.Agent{ID: subViewID(root.ID, n.NodeID, n.Generation), RootID: root.ID, NodeID: n.NodeID, LaneGeneration: n.Generation, FullName: name, Owner: root.Owner, Session: subViewSession(root.Session, n.NodeID, n.Generation)}, nil
		}
	}
	return consolemux.Agent{}, subagent.Fail("not_found", "子 Agent 不存在")
}
func (h *Hub) projectionViewAgent(ctx context.Context, name string) (consolemux.Agent, error) {
	name = strings.ToLower(name)
	canon, a, ok := h.newPlatformCanonicalName(ctx, viewRoot(name))
	if !ok {
		return consolemux.Agent{}, errors.New("not_found")
	}
	return h.subViewAgent(ctx, consolemux.Agent{ID: a.ID, FullName: canon, Owner: a.OwnerLC, Session: a.NameLC()}, name)
}

// Root permissions are expanded only after authorization; every desired child
// remains a separate budget slot and cryptographic scope identity.
func (m *consoleMuxRegistry) authorizeViewScopes(ctx context.Context, hash string, names []string) (consolemux.Authorization, error) {
	roots := []string{}
	seen := map[string]bool{}
	for _, name := range names {
		root := viewRoot(name)
		if !seen[root] {
			roots = append(roots, root)
			seen[root] = true
		}
	}
	auth, err := m.authorizer.AuthorizeConsoleMux(ctx, hash, roots)
	if err != nil {
		return auth, err
	}
	expanded := map[string]consolemux.Agent{}
	for name, root := range auth.Agents {
		expanded[name] = root
	}
	for _, name := range names {
		root, ok := auth.Agents[viewRoot(name)]
		if !ok {
			continue
		}
		a, e := m.h.subViewAgent(ctx, root, name)
		if e != nil {
			continue
		}
		expanded[name] = a
	}
	auth.Agents = expanded
	return auth, nil
}

// Authenticated helper event routing; malformed or stale child metadata is
// consumed here and never falls through to the root projection/AMR.
func (h *Hub) subViewEvent(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	node := metaString(env.Meta, "from_node_id")
	if node == "" && env.Meta["from_generation"] == nil && env.Meta["from_full_name"] == nil {
		return false
	}
	capable, ready := h.subAgentCapable(c)
	if !capable || !ready || node == "" {
		return true
	}
	root, full, err := h.subAgentRoot(ctx, c)
	if err != nil {
		return true
	}
	route, err := subagent.Parse(metaString(env.Meta, "from_full_name"), "")
	if err != nil || viewRoot(route.FullName()) != full {
		return true
	}
	repo, ok := h.Repo.(subAgentRepository)
	if !ok {
		return true
	}
	scope, err := repo.ResolveSubAgent(ctx, root, c.connSeq, route.Path)
	if err != nil || scope.NodeID != node || scope.Generation != subAgentInt64(env.Meta, "from_generation") {
		return true
	}
	sanitized, _ := stripClientProvenance(env.Body)
	if h.SessionHistoryEnabled && h.SessionHistory != nil {
		id := env.ID
		if id == "" {
			id = randomHex(16)
		}
		cursor, e := h.SessionHistory.Append(ctx, c.ownerKey, subViewID(root, node, scope.Generation), subViewSession(c.sessionName, node, scope.Generation), id, "session_event", []byte(sanitized), h.now())
		if e != nil {
			sanitized = addHistoryGap(sanitized, "persist_error")
		} else {
			sanitized = addHistoryCursor(sanitized, cursor, id)
		}
	}
	h.publishSessionEvent(c.keyID, subViewSession(c.sessionName, node, scope.Generation), sanitized)
	return true
}

type subAgentTreeItem struct {
	RootID    string             `json:"root_agent_id"`
	FullName  string             `json:"full_name"`
	Online    bool               `json:"online"`
	Available bool               `json:"sub_agents_v1"`
	Nodes     []subAgentTreeNode `json:"nodes"`
}
type subAgentTreeNode struct {
	NodeID     string `json:"node_id"`
	Generation int64  `json:"generation"`
	FullName   string `json:"full_name"`
	State      string `json:"state"`
	Reason     string `json:"reason"`
	Online     bool   `json:"online"`
}

func (h *Hub) subViewTree(ctx context.Context, id, full string, c *sessionClient) (subAgentTreeItem, error) {
	capable, ready := h.subAgentCapable(c)
	item := subAgentTreeItem{RootID: id, FullName: full, Online: c != nil, Available: capable && ready, Nodes: []subAgentTreeNode{}}
	repo, ok := h.Repo.(subAgentViewRepository)
	if !ok {
		return item, nil
	}
	nodes, err := repo.ListSubAgentNodes(ctx, id)
	if err != nil {
		return item, err
	}
	for _, n := range nodes {
		item.Nodes = append(item.Nodes, subAgentTreeNode{NodeID: n.NodeID, Generation: n.Generation, FullName: n.Name + "." + strings.ToLower(full), State: n.State, Reason: n.Reason, Online: c != nil && capable && ready && n.State == "active"})
	}
	return item, nil
}
func (h *Hub) HandleConsoleSubAgentTrees(w http.ResponseWriter, r *http.Request) {
	if h.consoleMux == nil || h.Identity == nil {
		http.NotFound(w, r)
		return
	}
	auth, _, _, ok := h.consoleMux.actor(w, r, nil)
	if !ok {
		return
	}
	agents, err := h.Identity.Store.ListAgents(r.Context(), auth.Principal)
	if err != nil {
		writeAgentWSError(w, 503, "registry_unavailable")
		return
	}
	trees := []subAgentTreeItem{}
	for _, a := range agents {
		full := h.Identity.FullName(r.Context(), &a)
		h.mu.Lock()
		c := h.sessionClients[AgentKeyID(a.OwnerLC)][a.NameLC()]
		h.mu.Unlock()
		item, err := h.subViewTree(r.Context(), a.ID, full, c)
		if err != nil {
			writeAgentWSError(w, 503, "registry_unavailable")
			return
		}
		trees = append(trees, item)
	}
	muxJSON(w, 200, map[string]any{"version": 1, "trees": trees})
}

// Standalone child SSE checks the same fresh root authority as the mux. The
// signed viewer binding is never accepted from a bare session/header claim.
func (h *Hub) subViewLease(r *http.Request, session string) (func() bool, error) {
	if session == viewRoot(session) {
		return func() bool { return true }, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-DW-Mux-Subject"))
	if err != nil || len(raw) > 2048 {
		return nil, errors.New("scope ticket required")
	}
	var subject struct {
		AgentID  string `json:"agent_id"`
		FullName string `json:"full_name"`
		Viewer   string `json:"viewer_session_hash"`
	}
	if json.Unmarshal(raw, &subject) != nil || subject.FullName != session || subject.Viewer == "" {
		return nil, errors.New("scope ticket mismatch")
	}
	a, err := h.projectionViewAgent(r.Context(), session)
	if err != nil || subject.AgentID != a.ID {
		return nil, errors.New("scope unavailable")
	}
	targetName, target := loadViewRouting().resolve(viewRoot(session), a.Owner)
	key, err := os.ReadFile(target.HubToVSSecretFile)
	if err != nil || len(bytes.TrimSpace(key)) < 32 {
		return nil, errors.New("scope authority unavailable")
	}
	u, err := url.Parse(target.URL)
	if err != nil {
		return nil, err
	}
	timestamp := r.Header.Get("X-DW-Timestamp")
	stamp, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(stamp, 0)).Abs() > time.Minute {
		return nil, errors.New("expired scope ticket")
	}
	want := signViewServiceRequest(bytes.TrimSpace(key), hubToVSDirection, "GET", strings.TrimRight(u.Path, "/")+"/events", timestamp, r.Header.Get("X-DW-Nonce"), raw)
	if r.Header.Get("X-DW-VS-Target") != targetName || !hmac.Equal([]byte(want), []byte(r.Header.Get("X-DW-Signature"))) {
		return nil, errors.New("invalid scope signature")
	}
	valid := func() bool {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		_, user, e := h.Identity.Store.ResolveSession(ctx, subject.Viewer, h.Identity.Now())
		if e != nil || user.UserIDLC() != a.Owner {
			return false
		}
		current, e := h.projectionViewAgent(ctx, session)
		return e == nil && current.ID == subject.AgentID
	}
	if !valid() {
		return nil, errors.New("scope revoked")
	}
	return valid, nil
}

// Resolve observation routing from the durable sidecar, never event JSON.
func (h *Hub) subViewObservation(key, session, id string, source bool) (string, string) {
	repo, ok := h.Repo.(subAgentRepository)
	if !ok {
		return key, session
	}
	route, err := repo.SessionDeliverySubRoute(context.Background(), id)
	if err != nil {
		return "", ""
	}
	if route != nil {
		scope := route.To
		if source {
			scope = route.From
		}
		if scope != nil {
			_, a, ok := h.newPlatformCanonicalName(context.Background(), viewRoot(scope.FullName))
			if !ok {
				return "", ""
			}
			return AgentKeyID(a.OwnerLC), subViewSession(a.NameLC(), scope.NodeID, scope.Generation)
		}
	}
	if strings.Count(session, ".") > 1 {
		a, err := h.projectionViewAgent(context.Background(), session)
		if err != nil {
			return "", ""
		}
		return AgentKeyID(a.Owner), a.Session
	}
	return key, session
}
