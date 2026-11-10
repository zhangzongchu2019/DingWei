package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
)

type subAgentRepository interface {
	BeginSubAgentConnection(context.Context, string, int) error
	SubAgentRevision(context.Context, string) (int64, error)
	ReportSubAgentTree(context.Context, string, subagent.Snapshot) (int64, error)
	ResolveSubAgent(context.Context, string, int, []string) (*subagent.Scope, error)
	CheckSubAgentRoute(context.Context, subagent.DeliveryRoute) error
	SessionDeliverySubRoute(context.Context, string) (*subagent.DeliveryRoute, error)
}
type subAgentClientKey struct{}
type subAgentDeliveryKey struct{}
type subAgentReturnTargetKey struct{}

func (h *Hub) subAgentCapable(c *sessionClient) (bool, bool) {
	if c == nil || !subAgentFeatureEnabled() {
		return false, false
	}
	h.mu.Lock()
	current := h.sessionClients[c.keyID][c.sessionName] == c
	h.mu.Unlock()
	if !current {
		return false, false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.subAgentsV1, c.subTreeReady
}
func (h *Hub) subAgentRoot(ctx context.Context, c *sessionClient) (string, string, error) {
	if h.Identity == nil || c == nil || c.metricsAgentID == "" {
		return "", "", subagent.Fail("unsupported", "dedicated root credential required")
	}
	ag, ok := h.newPlatformAgentAt(ctx, c.keyID, c.sessionName)
	if !ok || ag.ID != c.metricsAgentID || string(ag.Origin) != "selfhost" || string(ag.State) != "active" {
		return "", "", errGroupAgentNotFound
	}
	user, err := h.Identity.Store.GetUser(ctx, ag.OwnerLC)
	if err != nil {
		return "", "", err
	}
	if string(user.State) != "active" {
		return "", "", errGroupAgentNotFound
	}
	return ag.ID, ag.NameLC() + "." + user.ShortNameLC(), nil
}
func (h *Hub) negotiateSubAgents(ctx context.Context, c *sessionClient, env model.Envelope) error {
	if !subAgentFeatureEnabled() {
		return subagent.Fail("unsupported", "feature_disabled")
	}
	if !metaBool(env.Meta, "sub_agents_v1") {
		return nil
	}
	root, _, err := h.subAgentRoot(ctx, c)
	if err != nil {
		return err
	}
	if !strings.EqualFold(c.tool, "codex") || !metaBool(env.Meta, "delivery_ack_v1") && !metaBool(env.Meta, "delivery_ack_v2") {
		return subagent.Fail("unsupported", "Codex reliable helper required")
	}
	var depth int
	raw, _ := json.Marshal(env.Meta["max_supported_depth"])
	_ = json.Unmarshal(raw, &depth)
	if depth != 1 {
		return subagent.Fail("unsupported", "phase_not_enabled")
	}
	repo, ok := h.Repo.(subAgentRepository)
	if !ok {
		return subagent.Fail("registry_unavailable", "registry storage unavailable")
	}
	// Serialize against replacement publication and stale connection reports.
	c.subAgentMu.Lock()
	defer c.subAgentMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionClients[c.keyID][c.sessionName] != c {
		return subagent.Fail("generation_mismatch", "stale connection")
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.subAgentsV1 {
		return nil
	}
	if err = repo.BeginSubAgentConnection(ctx, root, c.connSeq); err != nil {
		return subagent.Fail("registry_unavailable", "registry initialization failed")
	}
	c.subAgentsV1 = true
	c.subTreeReady = false
	return nil
}
func (h *Hub) writeSubAgentCapability(c *sessionClient, env model.Envelope) {
	if !metaBool(env.Meta, "sub_agents_v1") {
		c.subAgentMu.Lock()
		defer c.subAgentMu.Unlock()
		c.stateMu.Lock()
		c.subAgentsV1 = false
		c.subTreeReady = false
		c.stateMu.Unlock()
		return
	}
	err := h.negotiateSubAgents(context.Background(), c, env)
	meta := map[string]any{"type": "sub_agent_capabilities", "sub_agents_v1": err == nil, "max_supported_depth": 1, "conn_seq": c.connSeq, "subtree_revision": 0, "resync_required": true}
	if err == nil {
		root, _, e := h.subAgentRoot(context.Background(), c)
		if e == nil {
			if repo, ok := h.Repo.(subAgentRepository); ok {
				revision, e := repo.SubAgentRevision(context.Background(), root)
				if e == nil {
					meta["subtree_revision"] = revision
				}
			}
		}
	}
	if err != nil {
		c.stateMu.Lock()
		c.subAgentsV1 = false
		c.subTreeReady = false
		c.stateMu.Unlock()
		meta["error"] = err.Error()
	}
	if c.conn != nil {
		_ = c.write(context.Background(), model.Envelope{From: sessionAddress(systemAddressName, c.keyID), To: sessionAddress(c.sessionName, c.keyID), Meta: meta})
	}
}
func (h *Hub) acceptSubAgentTree(ctx context.Context, c *sessionClient, snap subagent.Snapshot) (int64, error) {
	capable, _ := h.subAgentCapable(c)
	if !capable {
		return 0, subagent.Fail("unsupported", "connection not negotiated")
	}
	if snap.ConnSeq != c.connSeq {
		return 0, subagent.Fail("generation_mismatch", "stale connection epoch")
	}
	root, _, err := h.subAgentRoot(ctx, c)
	if err != nil {
		return 0, err
	}
	repo, ok := h.Repo.(subAgentRepository)
	if !ok {
		return 0, subagent.Fail("registry_unavailable", "registry storage unavailable")
	}
	c.subAgentMu.Lock()
	defer c.subAgentMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionClients[c.keyID][c.sessionName] != c {
		return 0, subagent.Fail("generation_mismatch", "stale connection")
	}
	rev, err := repo.ReportSubAgentTree(ctx, root, snap)
	err = subAgentStorageError(err)
	c.stateMu.Lock()
	c.subTreeReady = err == nil
	if err == nil {
		for _, n := range snap.Nodes {
			h.terminalForSession(c.keyID, subViewSession(c.sessionName, n.NodeID, n.Generation))
		}
	}
	c.subTreeRevision = rev
	c.stateMu.Unlock()
	return rev, err
}
func (h *Hub) handleSubAgentControl(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != "sub_agent_tree_report" {
		return false
	}
	raw, err := json.Marshal(env.Meta)
	var snap subagent.Snapshot
	if err == nil && len(raw) <= 256*1024 {
		err = json.Unmarshal(raw, &snap)
	} else {
		err = subagent.Fail("path_conflict", "snapshot too large")
	}
	var rev int64
	if err == nil {
		rev, err = h.acceptSubAgentTree(ctx, c, snap)
	}
	meta := map[string]any{"type": "sub_agent_tree_ack", "conn_seq": c.connSeq, "subtree_revision": rev, "ok": err == nil, "resync_required": err != nil}
	if err != nil {
		meta["error"] = err.Error()
	}
	if c.conn != nil {
		_ = c.write(ctx, model.Envelope{From: sessionAddress(systemAddressName, c.keyID), To: sessionAddress(c.sessionName, c.keyID), Meta: meta})
	}
	if err == nil {
		h.backgroundWork("owned", func() {
			h.drainSessionDeliveries(context.Background(), c)
			_ = h.broadcastNewPlatformDirectories(context.Background())
		})
	}
	return true
}
func (h *Hub) rejectSubAgentIngress(c *sessionClient, env model.Envelope) error {
	if capable, _ := h.subAgentCapable(c); !capable {
		return rejectUnnegotiatedSubAgentEnvelope(env)
	}
	return nil // fields are compared against authoritative bindings after grouping
}
func subAgentRequested(env model.Envelope) bool {
	to, err := parseAddress(env.To)
	if err == nil && to.Kind == addressSession && strings.Count(to.SessionName, ".") >= 2 {
		return true
	}
	if metaString(env.Meta, "from_node_id") != "" {
		return true
	}
	return false
}
func subAgentInt64(meta map[string]any, key string) int64 {
	var n int64
	raw, _ := json.Marshal(meta[key])
	_ = json.Unmarshal(raw, &n)
	return n
}

// routeSubAgentEnvelope authorizes root principals before exposing parser, tree,
// capability or lifecycle results. All public routing fields are reconstructed.
func (h *Hub) routeSubAgentEnvelope(ctx context.Context, env model.Envelope) error {
	to, err := parseAddress(env.To)
	if err != nil {
		return err
	}
	from, err := parseAddress(env.From)
	if err != nil {
		return err
	}
	if to.Kind != addressSession || from.Kind != addressSession || h.Identity == nil {
		return errGroupAgentNotFound
	}
	// Extract a candidate root solely for authorization; it is never a fallback
	// delivery target. Full syntax/depth validation follows authorization.
	parts := strings.Split(to.SessionName, ".")
	rootName := to.SessionName
	if len(parts) >= 2 {
		rootName = strings.Join(parts[len(parts)-2:], ".")
	}
	client, _ := ctx.Value(subAgentClientKey{}).(*sessionClient)
	if strings.Count(from.SessionName, ".") >= 2 {
		if client == nil {
			return errGroupAgentNotFound
		}
		from.SessionName = client.sessionName
		from.KeyID = client.keyID
	}
	if _, ok := h.newPlatformAgentAt(ctx, from.KeyID, from.SessionName); !ok {
		return errGroupAgentNotFound
	}
	owner := ""
	if ag, ok := h.newPlatformAgentAt(ctx, from.KeyID, from.SessionName); ok {
		if u, e := h.Identity.Store.GetUser(ctx, ag.OwnerLC); e == nil {
			owner = u.ShortNameLC()
		}
	}

	if !strings.Contains(rootName, ".") {
		rootName += "." + owner
	}
	target := h.resolveNewPlatformVisibleSession(ctx, rootName)
	if !target.found {
		return errGroupAgentNotFound
	}
	if err = h.enforceAgentGrouping(ctx, from, env, target.keyID, target.sessionName); err != nil {
		return err
	}
	route, err := subagent.Parse(to.SessionName, owner)
	if err != nil {
		return err
	}
	if len(route.Path) > 1 {
		return subagent.Fail("unsupported", "phase_not_enabled")
	}
	repo, ok := h.Repo.(subAgentRepository)
	if !ok {
		return subagent.Fail("registry_unavailable", "registry storage unavailable")
	}
	var delivery subagent.DeliveryRoute
	if len(route.Path) > 0 {
		h.mu.Lock()
		c := h.sessionClients[target.keyID][target.sessionName]
		h.mu.Unlock()
		capable, ready := h.subAgentCapable(c)
		if !capable {
			return subagent.Fail("unsupported", "connection not negotiated")
		}
		if !ready {
			return subagent.Fail("registry_unavailable", "tree not confirmed")
		}
		root, _, err := h.subAgentRoot(ctx, c)
		if err != nil {
			return err
		}
		scope, err := repo.ResolveSubAgent(ctx, root, c.connSeq, route.Path)
		if err != nil {
			return err
		}
		scope.FullName = subagent.FullName(route.Root+"."+route.Owner, scope.Path)
		delivery.To = scope
	}
	if pinned, _ := ctx.Value(subAgentReturnTargetKey{}).(*subagent.Scope); pinned != nil {
		if delivery.To == nil || delivery.To.RootID != pinned.RootID || delivery.To.NodeID != pinned.NodeID || delivery.To.Generation != pinned.Generation {
			return subagent.Fail("generation_mismatch", "persisted reply target mismatch")
		}
	}
	if metaString(env.Meta, "from_node_id") != "" || strings.Count(env.From, ".") >= 2 {
		capable, ready := h.subAgentCapable(client)
		if !capable {
			return subagent.Fail("unsupported", "trusted source connection required")
		}
		if !ready {
			return subagent.Fail("registry_unavailable", "source tree not confirmed")
		}
		root, full, err := h.subAgentRoot(ctx, client)
		if err != nil {
			return err
		}
		source, err := subagent.Parse(metaString(env.Meta, "from_full_name"), owner)
		if err != nil {
			return err
		}
		if source.Root+"."+source.Owner != full {
			return subagent.Fail("path_conflict", "source root mismatch")
		}
		scope, err := repo.ResolveSubAgent(ctx, root, client.connSeq, source.Path)
		if err != nil {
			return err
		}
		if scope.NodeID != metaString(env.Meta, "from_node_id") || scope.Generation != subAgentInt64(env.Meta, "from_generation") {
			return subagent.Fail("generation_mismatch", "source binding mismatch")
		}
		scope.FullName = subagent.FullName(full, scope.Path)
		delivery.From = scope
	}
	// Copy metadata: never mutate a caller-owned map or retain forged fields.
	meta := make(map[string]any, len(env.Meta)+8)
	for k, v := range env.Meta {
		meta[k] = v
	}
	env.Meta = meta
	expected := map[string]any{}
	if s := delivery.To; s != nil {
		expected["sub_path"] = s.Path
		expected["target_full_name"] = s.FullName
		expected["target_node_id"] = s.NodeID
		expected["lane_generation"] = s.Generation
		expected["subtree_revision"] = s.Revision
	}
	if s := delivery.From; s != nil {
		expected["from_full_name"] = s.FullName
		expected["from_node_id"] = s.NodeID
		expected["from_generation"] = s.Generation
	}
	for _, key := range []string{"sub_path", "target_full_name", "target_node_id", "lane_generation", "subtree_revision", "from_full_name", "from_node_id", "from_generation"} {
		if provided, exists := meta[key]; exists {
			want, ok := expected[key]
			a, _ := json.Marshal(provided)
			b, _ := json.Marshal(want)
			if !ok || !reflect.DeepEqual(a, b) {
				if key == "lane_generation" {
					return subagent.Fail("generation_mismatch", "target generation mismatch")
				}
				return subagent.Fail("path_conflict", "reserved route conflicts with explicit address")
			}
		}
		delete(meta, key)
	}
	for k, v := range expected {
		meta[k] = v
	}
	env.To = sessionAddress(target.sessionName, target.keyID)
	if delivery.To != nil {
		env.To = sessionAddress(delivery.To.FullName, target.keyID)
	}
	if delivery.From != nil {
		env.From = sessionAddress(delivery.From.FullName, from.KeyID)
	}
	if !reliablePrimaryEnvelope(env) {
		return subagent.Fail("unsupported", "child routing requires durable content")
	}
	env, drop := h.applyReplyOriginTermination(env)
	if drop {
		return nil
	}
	ctx = context.WithValue(ctx, subAgentDeliveryKey{}, &delivery)
	return h.routeToSession(ctx, target.keyID, target.sessionName, env, false)
}

func subAgentClaimKey(env model.Envelope) string {
	if node := metaString(env.Meta, "target_node_id"); node != "" {
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%d", env.ID, env.To, node, subAgentInt64(env.Meta, "lane_generation"), metaString(env.Meta, "from_node_id"), subAgentInt64(env.Meta, "from_generation"))
	}
	if node := metaString(env.Meta, "from_node_id"); node != "" {
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d", env.ID, env.To, node, subAgentInt64(env.Meta, "from_generation"))
	}
	return env.ID
}
func subAgentPermanent(err error) bool {
	var e *subagent.Error
	return errors.As(err, &e) && (e.Code == "sub_agent_ended" || e.Code == "sub_agent_generation_mismatch" || e.Code == "sub_agent_not_found")
}

// Revalidate persisted routing before every write, including after reconnect.
func (h *Hub) subAgentDrainCheck(ctx context.Context, c *sessionClient, d *model.SessionDelivery) error {
	repo, ok := h.Repo.(subAgentRepository)
	if !ok {
		if scopedSubAgentDelivery(d) {
			return subagent.Fail("registry_unavailable", "scope storage unavailable")
		}
		return nil
	}
	route, err := repo.SessionDeliverySubRoute(ctx, d.ID)
	if err != nil {
		return subagent.Fail("registry_unavailable", "delivery scope read failed")
	}
	if route == nil {
		if scopedSubAgentDelivery(d) {
			return subagent.Fail("registry_unavailable", "missing scope sidecar")
		}
		return nil
	}
	if route.To != nil {
		capable, ready := h.subAgentCapable(c)
		if !capable {
			return subagent.Fail("unsupported", "connection not negotiated")
		}
		if !ready {
			return subagent.Fail("registry_unavailable", "tree not confirmed")
		}
		route.To.ConnSeq = c.connSeq
	}
	// Source may be offline; its original binding must still exist and not end.
	return repo.CheckSubAgentRoute(ctx, *route)
}

// Serialize a scoped physical write against tree closure and connection replacement.
// Ordinary root deliveries keep their existing write path.
func (h *Hub) writeSubAgentDelivery(ctx context.Context, c *sessionClient, d *model.SessionDelivery, env model.Envelope) error {
	repo, ok := h.Repo.(subAgentRepository)
	if !ok {
		if scopedSubAgentDelivery(d) {
			return subagent.Fail("registry_unavailable", "scope storage unavailable")
		}
		return c.write(ctx, env)
	}
	route, err := repo.SessionDeliverySubRoute(ctx, d.ID)
	if err != nil {
		return err
	}
	if route == nil {
		if scopedSubAgentDelivery(d) {
			return subagent.Fail("registry_unavailable", "missing scope sidecar")
		}
		return c.write(ctx, env)
	}
	c.subAgentMu.Lock()
	defer c.subAgentMu.Unlock()
	h.mu.Lock()
	current := h.sessionClients[c.keyID][c.sessionName] == c
	h.mu.Unlock()
	if !current {
		return subagent.Fail("registry_unavailable", "connection replaced")
	}
	if route.To != nil {
		c.stateMu.Lock()
		ready := subAgentFeatureEnabled() && c.subAgentsV1 && c.subTreeReady
		c.stateMu.Unlock()
		if !ready {
			return subagent.Fail("registry_unavailable", "tree not confirmed")
		}
		route.To.ConnSeq = c.connSeq
	}
	if err = repo.CheckSubAgentRoute(ctx, *route); err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.write(writeCtx, env)
}

func subAgentRouteDeliveryID(base string, route subagent.DeliveryRoute) string {
	sum := sha256.Sum256([]byte(base + "\x00" + route.Identity()))
	return deliveryIDPrefix + hex.EncodeToString(sum[:])
}

func subAgentErrorCode(err error) string {
	var e *subagent.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "sub_agent_registry_unavailable"
}
func (h *Hub) subAgentACKAllowed(ctx context.Context, c *sessionClient, env model.Envelope, id string) bool {
	repo, ok := h.Repo.(subAgentRepository)
	if !ok {
		return true
	}
	route, err := repo.SessionDeliverySubRoute(ctx, id)
	if err != nil {
		return false
	}
	if route == nil || route.To == nil {
		return true
	}
	capable, ready := h.subAgentCapable(c)
	if !capable || !ready {
		return false
	}
	if metaString(env.Meta, "target_node_id") != route.To.NodeID || subAgentInt64(env.Meta, "lane_generation") != route.To.Generation {
		return false
	}
	route.To.ConnSeq = c.connSeq
	return repo.CheckSubAgentRoute(ctx, subagent.DeliveryRoute{To: route.To}) == nil
}

func scopedSubAgentDelivery(d *model.SessionDelivery) bool {
	if d == nil {
		return false
	}
	var env model.Envelope
	if json.Unmarshal([]byte(d.Envelope), &env) != nil {
		return false
	}
	return rejectUnnegotiatedSubAgentEnvelope(env) != nil
}

func subAgentFeatureEnabled() bool {
	switch strings.ToLower(os.Getenv("DW_SUB_AGENTS_V1")) {
	case "", "1", "true", "on":
		return true
	}
	return false
}
func subAgentStorageError(err error) error {
	if err == nil {
		return nil
	}
	var e *subagent.Error
	if errors.As(err, &e) {
		return err
	}
	return subagent.Fail("registry_unavailable", "registry transaction failed")
}
