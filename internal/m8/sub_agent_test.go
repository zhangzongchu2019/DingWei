package m8

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
	"strings"
	"testing"
	"time"
)

type subAgentWire struct {
	t      *testing.T
	conn   *websocket.Conn
	events chan model.Envelope
}

func subWire(t *testing.T, e *agentEnv) *subAgentWire {
	t.Helper()
	c, st, body := e.dial(e.cred, "tool=codex&version=0.2.3.14")
	if c == nil {
		t.Fatal(st, body)
	}
	w := &subAgentWire{t, c, make(chan model.Envelope, 128)}
	go func() {
		defer close(w.events)
		for {
			var env model.Envelope
			if wsjson.Read(context.Background(), c, &env) != nil {
				return
			}
			w.events <- env
		}
	}()
	t.Cleanup(func() { c.CloseNow() })
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	return w
}
func (w *subAgentWire) send(env model.Envelope) {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, w.conn, env); err != nil {
		w.t.Fatal(err)
	}
}
func (w *subAgentWire) until(pred func(model.Envelope) bool) model.Envelope {
	w.t.Helper()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case env, ok := <-w.events:
			if !ok {
				w.t.Fatal("wire closed")
			}
			if pred(env) {
				return env
			}
		case <-timer.C:
			w.t.Fatal("timed out waiting for frame")
		}
	}
}
func (w *subAgentWire) typ(typ string) model.Envelope {
	return w.until(func(env model.Envelope) bool { return metaString(env.Meta, "type") == typ })
}
func (w *subAgentWire) negotiate() model.Envelope {
	w.send(model.Envelope{Meta: map[string]any{"type": "session_capabilities", "sub_agents_v1": true, "max_supported_depth": 1, "subtree_revision": 0, "delivery_ack_v1": true}})
	ack := w.typ("sub_agent_capabilities")
	if !metaBool(ack.Meta, "sub_agents_v1") {
		w.t.Fatal("AA003_NEGOTIATION", ack)
	}
	return ack
}
func (w *subAgentWire) report(seq int, revision, base int64, nodes []subagent.Node) model.Envelope {
	w.send(model.Envelope{Meta: map[string]any{"type": "sub_agent_tree_report", "conn_seq": seq, "subtree_revision": revision, "base_revision": base, "nodes": nodes}})
	return w.typ("sub_agent_tree_ack")
}
func subNodes() []subagent.Node {
	return []subagent.Node{{NodeID: "node-a", Name: "aa", Aliases: []string{"alias"}, Generation: 1, State: "active"}, {NodeID: "node-b", Name: "bb", Generation: 1, State: "active"}}
}
func subEnv(id, target, body string) model.Envelope {
	return model.Envelope{ID: id, From: "opus50ts#u_zzc", To: target + "#u_zzc", Body: body}
}
func subErr(t *testing.T, err error, code, label string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "sub_agent_"+code) {
		t.Fatalf("%s err=%v", label, err)
	}
}
func TestSubAgentHubNegotiationRoutesAndSiblingIdentity(t *testing.T) {
	e := newAgentEnv(t)
	w := subWire(t, e)
	h := e.hub
	db := h.Repo.(*store.SQLite)
	subErr(t, h.RouteEnvelope(e.ctx, subEnv("unsupported", "aa.opus50ts.zzc", "no")), "unsupported", "AA003_UNNEGOTIATED")
	cap := w.negotiate()
	seq := int(subAgentInt64(cap.Meta, "conn_seq"))
	subErr(t, h.RouteEnvelope(e.ctx, subEnv("recovering", "aa.opus50ts.zzc", "no")), "registry_unavailable", "AA003_RECOVERING")
	ack := w.report(seq, 1, 0, subNodes())
	if !metaBool(ack.Meta, "ok") {
		t.Fatal("AA003_REGISTER", ack)
	}
	if !metaBool(w.report(seq, 1, 0, subNodes()).Meta, "ok") {
		t.Fatal("AA009_IDEMPOTENT_ACK")
	}
	for _, target := range []string{"aa", "bb"} {
		env := subEnv("same-envelope", target+".opus50ts.zzc", "unchanged\n中文\x00body")
		if err := h.RouteEnvelope(e.ctx, env); err != nil {
			t.Fatal("AA009_SIBLINGS_ROUTE_DELIVERIES", err)
		}
	}
	ids := map[string]bool{}
	for i := 0; i < 2; i++ {
		got := w.until(func(env model.Envelope) bool { return env.ID == "same-envelope" })
		node := metaString(got.Meta, "target_node_id")
		id := metaString(got.Meta, "delivery_id")
		ids[id] = true
		if got.Body != "unchanged\n中文\x00body" || node == "" || subAgentInt64(got.Meta, "lane_generation") != 1 || !strings.Contains(got.To, ".opus50ts.zzc#") {
			t.Fatal("AA003_BODY_AND_TARGET", got)
		}
		route, err := db.SessionDeliverySubRoute(e.ctx, id)
		if err != nil || route == nil || route.To.NodeID != node {
			t.Fatal("AA009_PERSISTED_SCOPE", route, err)
		}
	}
	if len(ids) != 2 {
		t.Fatal("AA009_SIBLINGS_DISTINCT_DELIVERIES", ids)
	}
	// Alias resolves to the same opaque node and canonical full name.
	if err := h.RouteEnvelope(e.ctx, subEnv("alias", "alias.opus50ts.zzc", "alias-body")); err != nil {
		t.Fatal(err)
	}
	got := w.until(func(env model.Envelope) bool { return env.ID == "alias" })
	if metaString(got.Meta, "target_node_id") != "node-a" || metaString(got.Meta, "target_full_name") != "aa.opus50ts.zzc" {
		t.Fatal("AA003_ALIAS_CANONICAL", got)
	}
	subErr(t, h.RouteEnvelope(e.ctx, subEnv("missing", "xx.opus50ts.zzc", "no")), "not_found", "AA003_NOT_FOUND")
	subErr(t, h.RouteEnvelope(e.ctx, subEnv("phase", "xx.aa.opus50ts.zzc", "no")), "unsupported", "AA002_PHASE_NOT_ENABLED")
	subErr(t, h.RouteEnvelope(e.ctx, subEnv("depth", "xx.yy.zz.aa.opus50ts.zzc", "no")), "depth_exceeded", "AA002_DEPTH_EXCEEDED")
	nodes := subNodes()
	nodes[0].State = "ended"
	nodes[0].Reason = "completed"
	if !metaBool(w.report(seq, 2, 1, nodes).Meta, "ok") {
		t.Fatal("end report failed")
	}
	subErr(t, h.RouteEnvelope(e.ctx, subEnv("ended", "alias.opus50ts.zzc", "no")), "ended", "AA003_ENDED_DISTINCT")
	bad := w.report(seq, 3, 2, subNodes())
	if metaBool(bad.Meta, "ok") || !metaBool(bad.Meta, "resync_required") {
		t.Fatal("AA009_ENDED_CANNOT_REVIVE", bad)
	}
}
func TestSubAgentHubTrustedSourceReplyAndSpoof(t *testing.T) {
	e := newAgentEnv(t)
	w := subWire(t, e)
	seq := int(subAgentInt64(w.negotiate().Meta, "conn_seq"))
	if !metaBool(w.report(seq, 1, 0, subNodes()).Meta, "ok") {
		t.Fatal("register")
	}
	env := subEnv("child-origin", "opus50ts.zzc", "child to root")
	env.Meta = map[string]any{"from_node_id": "node-a", "from_generation": 1, "from_full_name": "aa.opus50ts.zzc"}
	w.send(env)
	got := w.until(func(env model.Envelope) bool { return env.ID == "child-origin" })
	if got.From != "aa.opus50ts.zzc#u_zzc" || originAddrOf(got) != got.From {
		t.Fatal("AA004_CHILD_ORIGIN", got)
	}
	route, err := e.hub.Repo.(*store.SQLite).SessionDeliverySubRoute(e.ctx, metaString(got.Meta, "delivery_id"))
	if err != nil || route == nil || route.From.NodeID != "node-a" {
		t.Fatal("AA009_SOURCE_SCOPE", route, err)
	}
	reply := subEnv("reply-child", "aa.opus50ts.zzc", "root reply")
	w.send(reply)
	got = w.until(func(env model.Envelope) bool { return env.ID == "reply-child" })
	if metaString(got.Meta, "target_node_id") != "node-a" {
		t.Fatal("AA004_REPLY_TO_SAME_CHILD", got)
	}
	// Direct Hub ingress cannot mint the trusted connection context.
	subErr(t, e.hub.RouteEnvelope(e.ctx, env), "unsupported", "AA004_UNTRUSTED_SOURCE_FIELDS")
	env.ID = "spoof"
	env.Meta["from_node_id"] = "node-b"
	w.send(env)
	failure := w.until(func(env model.Envelope) bool { return strings.Contains(env.Body, "generation_mismatch") })
	if !strings.Contains(failure.Body, "generation_mismatch") {
		t.Fatal("AA004_ORIGIN_SPOOF", failure)
	}
	env = subEnv("conflict", "aa.opus50ts.zzc", "bad-route")
	env.Meta = map[string]any{"sub_path": []string{"bb"}}
	w.send(env)
	failure = w.until(func(env model.Envelope) bool { return strings.Contains(env.Body, "path_conflict") })
	if !strings.Contains(failure.Body, "path_conflict") {
		t.Fatal("AA003_PATH_CONFLICT", failure)
	}
}
func TestSubAgentHubReconnectRequiresNegotiationAndSnapshot(t *testing.T) {
	e := newAgentEnv(t)
	first := subWire(t, e)
	seq := int(subAgentInt64(first.negotiate().Meta, "conn_seq"))
	if !metaBool(first.report(seq, 1, 0, subNodes()).Meta, "ok") {
		t.Fatal("register")
	}
	h := e.hub
	h.mu.Lock()
	old := h.sessionClients["u_zzc"]["opus50ts"]
	h.mu.Unlock()
	first.conn.CloseNow()
	deadline := time.Now().Add(3 * time.Second)
	for e.hub.sessionOnline("u_zzc", "opus50ts") {
		if time.Now().After(deadline) {
			t.Fatal("disconnect timeout")
		}
		time.Sleep(time.Millisecond)
	}
	second := subWire(t, e)
	subErr(t, h.RouteEnvelope(e.ctx, subEnv("unnegotiated-again", "aa.opus50ts.zzc", "no")), "unsupported", "AA003_RECONNECT_CAPABILITY_NOT_INHERITED")
	cap := second.negotiate()
	newseq := int(subAgentInt64(cap.Meta, "conn_seq"))
	if subAgentInt64(cap.Meta, "subtree_revision") != 1 {
		t.Fatal("AA009_CAPABILITY_REVISION", cap)
	}
	if newseq <= seq {
		t.Fatal("conn sequence did not advance")
	}
	subErr(t, h.RouteEnvelope(e.ctx, subEnv("recovering-again", "aa.opus50ts.zzc", "no")), "registry_unavailable", "AA003_RECONNECT_TREE_NOT_INHERITED")
	_, err := h.acceptSubAgentTree(e.ctx, old, subagent.Snapshot{ConnSeq: seq, Revision: 2, BaseRevision: 1, Nodes: subNodes()})
	if err == nil {
		t.Fatal("AA009_STALE_CONNECTION_REPORT")
	}
	ack := second.report(newseq, 1, 0, subNodes())
	if !metaBool(ack.Meta, "ok") {
		t.Fatal("AA009_RECONNECT_SNAPSHOT", ack)
	}
	if err := h.RouteEnvelope(e.ctx, subEnv("after-reconnect", "aa.opus50ts.zzc", "yes")); err != nil {
		t.Fatal(err)
	}
	got := second.until(func(env model.Envelope) bool { return env.ID == "after-reconnect" })
	if metaString(got.Meta, "target_node_id") != "node-a" {
		t.Fatal(got)
	}
}
func TestSubAgentHubAuthBeforeAddressAndTree(t *testing.T) {
	e := newAgentEnv(t)
	w := subWire(t, e)
	seq := int(subAgentInt64(w.negotiate().Meta, "conn_seq"))
	if !metaBool(w.report(seq, 1, 0, subNodes()).Meta, "ok") {
		t.Fatal("register")
	}
	_, otherID := e.enroll("other-group", "Other")
	_ = otherID
	for _, target := range []string{"aa.opus50ts.zzc", "xx.opus50ts.zzc", "xx.yy.zz.aa.opus50ts.zzc", ".opus50ts.zzc"} {
		env := subEnv("forbidden-"+target, target, "no")
		env.From = "other#u_zzc"
		err := e.hub.RouteEnvelope(e.ctx, env)
		if err == nil || strings.Contains(err.Error(), "sub_agent_") {
			t.Fatalf("AA004_AUTH_BEFORE_ADDRESS target=%s err=%v", target, err)
		}
	}
	if _, _, err := e.svc.ResolveFullName(e.ctx, "aa.opus50ts.zzc"); err == nil {
		t.Fatal("AA004_PRINCIPAL_BOUNDARY")
	}
}
func TestSubAgentIdentityIncludesNodeAndGeneration(t *testing.T) {
	a := &subagent.Scope{RootID: "root", NodeID: "a", Generation: 1}
	b := *a
	b.NodeID = "b"
	next := *a
	next.Generation = 2
	if subAgentRouteDeliveryID("same", subagent.DeliveryRoute{To: a}) == subAgentRouteDeliveryID("same", subagent.DeliveryRoute{To: &b}) {
		t.Fatal("AA009_ID_INCLUDES_NODE")
	}
	if subAgentRouteDeliveryID("same", subagent.DeliveryRoute{To: a}) == subAgentRouteDeliveryID("same", subagent.DeliveryRoute{To: &next}) {
		t.Fatal("AA009_ID_INCLUDES_GENERATION")
	}
	if subAgentRouteDeliveryID("same", subagent.DeliveryRoute{From: a, To: &b}) == subAgentRouteDeliveryID("same", subagent.DeliveryRoute{From: &b, To: &b}) {
		t.Fatal("AA009_ID_INCLUDES_SOURCE")
	}
	first := model.Envelope{ID: "same", To: "bb.root.user#key", Meta: map[string]any{"target_node_id": "b", "lane_generation": 1, "from_node_id": "a", "from_generation": 1}}
	second := first
	second.Meta = map[string]any{"target_node_id": "b", "lane_generation": 1, "from_node_id": "b", "from_generation": 1}
	if subAgentClaimKey(first) == subAgentClaimKey(second) {
		t.Fatal("AA009_CLAIM_INCLUDES_BOTH_ENDS")
	}
	// Explicit JSON confirms no native threadId leaves helper-local storage.
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), "thread") {
		t.Fatal("native thread leak")
	}
}

type subAgentMissingRouteRepo struct{ *store.SQLite }

func (r subAgentMissingRouteRepo) SessionDeliverySubRoute(context.Context, string) (*subagent.DeliveryRoute, error) {
	return nil, nil
}
func TestSubAgentHubDrainAndACKFailClosed(t *testing.T) {
	h, c, _ := diskEnv(t)
	ctx := context.Background()
	c.tool = "codex"
	c.connSeq = 1
	c.deliveryACKV1 = true
	caps := model.Envelope{Meta: map[string]any{"sub_agents_v1": true, "max_supported_depth": 1, "delivery_ack_v1": true}}
	if err := h.negotiateSubAgents(ctx, c, caps); err != nil {
		t.Fatal(err)
	}
	if _, err := h.acceptSubAgentTree(ctx, c, subagent.Snapshot{ConnSeq: 1, Revision: 1, Nodes: subNodes()}); err != nil {
		t.Fatal(err)
	}
	db := h.Repo.(*store.SQLite)
	scope, err := db.ResolveSubAgent(ctx, c.metricsAgentID, 1, []string{"aa"})
	if err != nil {
		t.Fatal(err)
	}
	scope.FullName = "aa.diskagent.zzc"
	env := model.Envelope{ID: "queued-child", To: "aa.diskagent.zzc#u_zzc", From: "diskagent#u_zzc", Body: "do not send to parent", Meta: map[string]any{"target_node_id": "node-a", "lane_generation": 1, "sub_path": []string{"aa"}}}
	raw, _ := json.Marshal(env)
	d := model.SessionDelivery{ID: "pending-scope", KeyID: c.keyID, SessionName: c.sessionName, Envelope: string(raw), SourceType: "session_inbound", SourceMessageID: env.ID, SubRoute: &subagent.DeliveryRoute{To: scope}}
	if err := db.EnqueueSessionDelivery(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := h.subAgentDrainCheck(ctx, c, &d); err != nil {
		t.Fatal("AA009_DRAIN_POSITIVE", err)
	}
	good := model.Envelope{Meta: map[string]any{"target_node_id": "node-a", "lane_generation": 1}}
	if !h.subAgentACKAllowed(ctx, c, good, d.ID) {
		t.Fatal("AA009_SCOPED_ACK_POSITIVE")
	}
	for _, bad := range []model.Envelope{{Meta: map[string]any{}}, {Meta: map[string]any{"target_node_id": "node-b", "lane_generation": 1}}, {Meta: map[string]any{"target_node_id": "node-a", "lane_generation": 2}}} {
		if h.subAgentACKAllowed(ctx, c, bad, d.ID) {
			t.Fatal("AA009_CROSS_LANE_ACK")
		}
	}
	h.Repo = subAgentMissingRouteRepo{db}
	subErr(t, h.subAgentDrainCheck(ctx, c, &d), "registry_unavailable", "AA009_MISSING_SIDECAR")
	h.Repo = db
	c.stateMu.Lock()
	c.subAgentsV1 = false
	c.stateMu.Unlock()
	subErr(t, h.subAgentDrainCheck(ctx, c, &d), "unsupported", "AA009_UNNEGOTIATED_REPLAY")
	// Check final write guard independently of the initial drain guard.
	subErr(t, h.writeSubAgentDelivery(ctx, c, &d, env), "registry_unavailable", "AA009_FINAL_WRITE_GUARD")
	c.stateMu.Lock()
	c.subAgentsV1 = true
	c.stateMu.Unlock()
	nodes := subNodes()
	nodes[0].State = "draining"
	nodes[0].Reason = "parent_closed"
	if _, err := h.acceptSubAgentTree(ctx, c, subagent.Snapshot{ConnSeq: 1, Revision: 2, BaseRevision: 1, Nodes: nodes}); err != nil {
		t.Fatal(err)
	}
	subErr(t, h.subAgentDrainCheck(ctx, c, &d), "ended", "AA009_CLOSED_AFTER_QUEUE")
	subErr(t, h.writeSubAgentDelivery(ctx, c, &d, env), "ended", "AA009_CLOSED_AT_FINAL_WRITE")
	if h.subAgentACKAllowed(ctx, c, good, d.ID) {
		t.Fatal("AA009_ENDED_ACK_REJECTED")
	}
}
func TestSubAgentHubCapabilityAndEpochNegatives(t *testing.T) {
	for _, mode := range []string{"old-helper", "wrong-depth", "no-ack", "no-credential", "stale"} {
		t.Run(mode, func(t *testing.T) {
			h, c, _ := diskEnv(t)
			c.tool = "codex"
			c.connSeq = 1
			caps := model.Envelope{Meta: map[string]any{"sub_agents_v1": true, "max_supported_depth": 1, "delivery_ack_v1": true}}
			switch mode {
			case "old-helper":
				c.tool = "claude"
			case "wrong-depth":
				caps.Meta["max_supported_depth"] = 2
			case "no-ack":
				caps.Meta["delivery_ack_v1"] = false
			case "no-credential":
				c.metricsAgentID = ""
			case "stale":
				h.sessionClients[c.keyID][c.sessionName] = &sessionClient{}
			}
			if err := h.negotiateSubAgents(context.Background(), c, caps); err == nil {
				t.Fatal("AA003_CAPABILITY_BOUNDARY", mode)
			}
		})
	}
	h, c, _ := diskEnv(t)
	c.tool = "codex"
	c.connSeq = 1
	caps := model.Envelope{Meta: map[string]any{"sub_agents_v1": true, "max_supported_depth": 1, "delivery_ack_v1": true}}
	if err := h.negotiateSubAgents(context.Background(), c, caps); err != nil {
		t.Fatal(err)
	}
	_, err := h.acceptSubAgentTree(context.Background(), c, subagent.Snapshot{ConnSeq: 999, Revision: 1, Nodes: subNodes()})
	subErr(t, err, "generation_mismatch", "AA009_REPORT_EPOCH_MISMATCH")
	h.writeSubAgentCapability(c, model.Envelope{Meta: map[string]any{}})
	if capable, _ := h.subAgentCapable(c); capable {
		t.Fatal("AA003_CAPABILITY_WITHDRAWAL")
	}
}

func TestSubAgentHubFeatureOffPreservesRoot(t *testing.T) {
	h, c, _ := diskEnv(t)
	ctx := context.Background()
	c.tool = "codex"
	c.connSeq = 1
	caps := model.Envelope{Meta: map[string]any{"sub_agents_v1": true, "max_supported_depth": 1, "delivery_ack_v1": true}}
	if err := h.negotiateSubAgents(ctx, c, caps); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DW_SUB_AGENTS_V1", "0")
	if capable, _ := h.subAgentCapable(c); capable {
		t.Fatal("AA009_FEATURE_OFF")
	}
	subErr(t, h.negotiateSubAgents(ctx, c, caps), "unsupported", "AA009_FEATURE_OFF_NEGOTIATION")
	if err := h.rejectSubAgentIngress(c, model.Envelope{}); err != nil {
		t.Fatal("AA009_FEATURE_OFF_ROOT_UNCHANGED", err)
	}
	subErr(t, h.rejectSubAgentIngress(c, model.Envelope{Meta: map[string]any{"sub_path": []string{"aa"}}}), "unsupported", "AA009_FEATURE_OFF_CHILD_REFUSED")
}

func TestSubAgentHubAutomaticReplyUsesPersistedSource(t *testing.T) {
	e := newAgentEnv(t)
	w := subWire(t, e)
	seq := int(subAgentInt64(w.negotiate().Meta, "conn_seq"))
	if !metaBool(w.report(seq, 1, 0, subNodes()).Meta, "ok") {
		t.Fatal("register")
	}
	send := func(id, policy string) model.Envelope {
		env := subEnv(id, "opus50ts.zzc", "child question")
		env.Meta = map[string]any{"from_node_id": "node-a", "from_generation": 1, "from_full_name": "aa.opus50ts.zzc"}
		if policy != "" {
			env.Meta["reply_policy"] = policy
		}
		w.send(env)
		return w.until(func(env model.Envelope) bool { return env.ID == id })
	}
	noPolicy := send("default-policy", "")
	p := e.hub.amr
	p.mu.Lock()
	before := p.stats.replyOriginSkipped
	p.mu.Unlock()
	p.replyToOrigin(amrRouteJob{key: amrTurnKey{KeyID: "u_zzc", Session: "opus50ts", DeliveryID: metaString(noPolicy.Meta, "delivery_id"), TurnID: "no-policy"}, body: "must not automatically send"})
	p.mu.Lock()
	skipped := p.stats.replyOriginSkipped
	p.mu.Unlock()
	if skipped != before+1 {
		t.Fatal("AA004_CHILD_DEFAULT_REPLY_POLICY")
	}
	inbound := send("explicit-policy", "expect_reply")
	p.replyToOrigin(amrRouteJob{key: amrTurnKey{KeyID: "u_zzc", Session: "opus50ts", DeliveryID: metaString(inbound.Meta, "delivery_id"), TurnID: "explicit-policy"}, body: "automatic return"})
	got := w.until(func(env model.Envelope) bool { return env.Body == "automatic return" })
	if metaString(got.Meta, "target_node_id") != "node-a" || got.To != "aa.opus50ts.zzc#u_zzc" {
		t.Fatal("AA004_AUTOMATIC_REPLY_TO_SOURCE_NODE", got)
	}
	pinned := &subagent.Scope{RootID: e.agID, NodeID: "node-a", Generation: 99}
	ctx := context.WithValue(e.ctx, subAgentReturnTargetKey{}, pinned)
	subErr(t, e.hub.RouteEnvelope(ctx, subEnv("old-return", "aa.opus50ts.zzc", "no")), "generation_mismatch", "AA009_REPLY_GENERATION_PIN")
}
