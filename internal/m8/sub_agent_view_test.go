package m8

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type subViewAuthority struct {
	root   consolemux.Agent
	denied bool
}

func (a *subViewAuthority) AuthorizeConsoleMux(_ context.Context, _ string, names []string) (consolemux.Authorization, error) {
	out := consolemux.Authorization{Principal: "zzc", Expiry: time.Now().Add(time.Hour), Agents: map[string]consolemux.Agent{}}
	for _, name := range names {
		if name == a.root.FullName && !a.denied {
			out.Agents[name] = a.root
		}
	}
	return out, nil
}
func subViewFixture(t *testing.T) (*agentEnv, *subAgentWire, *sessionClient, consolemux.Agent) {
	t.Helper()
	e := newAgentEnv(t)
	w := subWire(t, e)
	caps := w.negotiate()
	seq := int(subAgentInt64(caps.Meta, "conn_seq"))
	ack := w.report(seq, 1, 0, subNodes())
	if !metaBool(ack.Meta, "ok") {
		t.Fatal(ack)
	}
	h := e.hub
	h.mu.Lock()
	c := h.sessionClients["u_zzc"]["opus50ts"]
	h.mu.Unlock()
	root, full, err := h.subAgentRoot(e.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	return e, w, c, consolemux.Agent{ID: root, FullName: full, Owner: "zzc", Session: "opus50ts"}
}
func TestSubAgentViewScopeAndEvents(t *testing.T) {
	e, _, c, root := subViewFixture(t)
	h := e.hub
	aa, err := h.subViewAgent(e.ctx, root, "aa."+root.FullName)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := h.subViewAgent(e.ctx, root, "bb."+root.FullName)
	if err != nil {
		t.Fatal(err)
	}
	if aa.ID == root.ID || aa.ID == bb.ID || aa.Session == bb.Session || subViewID(root.ID, "node-a", 2) == aa.ID {
		t.Fatal("AA010_SCOPE_IDENTITY")
	}
	for i, a := range []consolemux.Agent{aa, bb} {
		node := []string{"node-a", "node-b"}[i]
		h.handleSessionEventEnvelopeContext(e.ctx, c, model.Envelope{Body: fmt.Sprintf(`{"kind":"assistant_text","text":"child-%d"}`, i), Meta: map[string]any{"type": "session_event", "from_node_id": node, "from_generation": 1, "from_full_name": a.FullName}})
		snap, ok := h.consoleMuxSnapshot(a)
		if !ok || snap.H != 1 || len(snap.Replay) != 1 || !strings.Contains(snap.Replay[0].Data, fmt.Sprintf("child-%d", i)) {
			t.Fatal("AA010_CHILD_EVENT_SCOPE", snap)
		}
	}
	snap, _ := h.consoleMuxSnapshot(root)
	before := snap.H
	for _, meta := range []map[string]any{
		{"from_node_id": "node-a", "from_generation": 2, "from_full_name": aa.FullName},
		{"from_node_id": "node-b", "from_generation": 1, "from_full_name": aa.FullName},
		{"from_node_id": "node-a"},
	} {
		meta["type"] = "session_event"
		h.handleSessionEventEnvelopeContext(e.ctx, c, model.Envelope{Body: `{"kind":"assistant_text","text":"FORGED"}`, Meta: meta})
	}
	snap, _ = h.consoleMuxSnapshot(root)
	aSnap, _ := h.consoleMuxSnapshot(aa)
	if snap.H != before || aSnap.H != 1 {
		t.Fatal("AA010_FORGED_EVENT_REACHED_PROJECTION")
	}
	h.handleSessionEventEnvelope(c, model.Envelope{Body: `{"kind":"assistant_text","text":"root"}`, Meta: map[string]any{"type": "session_event"}})
	snap, _ = h.consoleMuxSnapshot(root)
	if snap.H != before+1 {
		t.Fatal("AA010_ROOT_PROJECTION_PRESERVED")
	}
	if h.grantNewPlatformViewServiceInput(e.ctx, aa.FullName) {
		t.Fatal("AA010_CHILD_INHERITED_ROOT_INPUT")
	}
}
func TestSubAgentViewAuthorizationBudgetAndRevocation(t *testing.T) {
	e, _, _, root := subViewFixture(t)
	authority := &subViewAuthority{root: root}
	m := &consoleMuxRegistry{h: e.hub, authorizer: authority, feeds: map[string]*consoleMuxFeed{}}
	names := []string{root.FullName, "aa." + root.FullName, "bb." + root.FullName}
	auth, err := m.authorizeViewScopes(e.ctx, "hash", names)
	if err != nil || len(auth.Agents) != 3 {
		t.Fatal("AA010_ROOT_AUTH_EXPANSION", auth, err)
	}
	for _, a := range auth.Agents {
		if a.NodeID != "" && a.RootID != root.ID {
			t.Fatal("AA010_ROOT_SCOPE_BINDING")
		}
	}
	authority.denied = true
	denied, _ := m.authorizeViewScopes(e.ctx, "hash", names)
	if len(denied.Agents) != 0 {
		t.Fatal("AA010_UNAUTHORIZED_CHILD_ENUMERATION")
	}
	for _, n := range []int{8, 9} {
		subs := []map[string]string{}
		for i := 0; i < n; i++ {
			subs = append(subs, map[string]string{"full_name": fmt.Sprintf("a%d.opus50ts.zzc", i), "iframe_instance": strings.Repeat("a", 32)})
		}
		b, _ := json.Marshal(map[string]any{"request_id": strings.Repeat("b", 32), "subscriptions": subs})
		req := httptest.NewRequest("POST", "/", strings.NewReader(string(b)))
		_, got, _, err := muxDecode(httptest.NewRecorder(), req)
		if (n == 8 && (err != nil || len(got) != 8)) || (n == 9 && err == nil) {
			t.Fatalf("AA010_REAL_SCOPE_BUDGET n=%d err=%v", n, err)
		}
	}
	scopes := map[string]*consoleMuxScope{}
	for name, a := range auth.Agents {
		scopes[name] = &consoleMuxScope{Agent: a, Generation: strings.Repeat("c", 32)}
	}
	f := &consoleMuxFeed{Scopes: scopes, Changed: make(chan struct{}, 1)}
	m.feeds["feed"] = f
	e.hub.consoleMux = m
	t.Cleanup(func() { e.hub.consoleMux = nil })
	e.hub.revokeConsoleMuxAgent(root.ID)
	if len(f.Scopes) != 0 {
		t.Fatal("AA010_ROOT_REVOKE_WHOLE_TREE")
	}
}
func TestSubAgentViewTreeLegacyAndFeatureOff(t *testing.T) {
	e, w, c, root := subViewFixture(t)
	nodes := subNodes()
	nodes[1].State = "ended"
	nodes[1].Reason = "completed"
	if !metaBool(w.report(c.connSeq, 2, 1, nodes).Meta, "ok") {
		t.Fatal("report")
	}
	tree, err := e.hub.subViewTree(e.ctx, root.ID, root.FullName, c)
	if err != nil || len(tree.Nodes) != 2 || tree.Nodes[1].State != "ended" || tree.Nodes[1].Reason != "completed" || tree.Nodes[1].Online {
		t.Fatal("AA010_TREE_TOMBSTONE", tree, err)
	}
	items := []onlineSessionItem{{Endpoint: model.SessionEndpoint{SessionName: root.Session, FullSessionName: root.FullName}}, {Endpoint: model.SessionEndpoint{SessionName: "aa", FullSessionName: "aa." + root.FullName}}}
	text := renderNewPlatformDirectory(model.Member{OwnerKey: "zzc"}, items)
	if strings.Contains(text, "aa.") || strings.Contains(text, "bb.") || !strings.Contains(text, root.FullName) {
		t.Fatal("AA010_LEGACY_ROOT_ONLY", text)
	}
	if _, err := e.hub.subViewAgent(e.ctx, root, "bb."+root.FullName); err != nil {
		t.Fatal("AA010_ENDED_HISTORY_READABLE", err)
	}
	t.Setenv("DW_SUB_AGENTS_V1", "0")
	if _, err := e.hub.subViewAgent(e.ctx, root, "aa."+root.FullName); err == nil {
		t.Fatal("AA010_FEATURE_OFF_CHILD_REJECT")
	}
	if _, err := e.hub.subViewAgent(e.ctx, root, root.FullName); err != nil {
		t.Fatal("AA010_FEATURE_OFF_ROOT", err)
	}
}
func TestSubAgentViewSignedSubject(t *testing.T) {
	e, _, _, root := subViewFixture(t)
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "forward")
	os.WriteFile(keyFile, []byte(strings.Repeat("k", 32)), 0600)
	cfg := filepath.Join(dir, "routing.json")
	b, _ := json.Marshal(ViewRouting{Default: "v2", Targets: map[string]ViewTarget{"v2": {URL: "http://127.0.0.1:1", ViewUnlockV2: true, HubToVSSecretFile: keyFile}}})
	os.WriteFile(cfg, b, 0600)
	t.Setenv("VIEW_ROUTING_CONFIG", cfg)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)
	e.svc.Store.CreateSession(e.ctx, identity.Session{TokenHash: identity.HashSecret("test-viewer"), UserIDLC: "zzc", ExpiresAt: time.Now().Add(time.Hour)})
	req := httptest.NewRequest("GET", "/status", nil)
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "test-viewer"})
	req.Header.Set("X-DW-Mux-Subject", "forged")
	if err := e.hub.muxProjectionSubject(req, "aa."+root.FullName); err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(req.Header.Get("X-DW-Mux-Subject"))
	var sub map[string]any
	json.Unmarshal(raw, &sub)
	if sub["root_agent_id"] != root.ID || sub["node_id"] != "node-a" || sub["lane_generation"] != float64(1) || sub["agent_id"] == root.ID {
		t.Fatal("AA010_SIGNED_SCOPE_BINDING", sub)
	}
	if req.Header.Get("X-DW-Signature") != signViewServiceRequest([]byte(strings.Repeat("k", 32)), hubToVSDirection, "GET", "/status", req.Header.Get("X-DW-Timestamp"), req.Header.Get("X-DW-Nonce"), raw) {
		t.Fatal("AA010_SCOPE_SIGNATURE")
	}
	req.URL.Path = "/events"
	if err := e.hub.muxProjectionSubject(req, "aa."+root.FullName); err != nil {
		t.Fatal(err)
	}
	lease, err := e.hub.subViewLease(req, "aa."+root.FullName)
	if err != nil || !lease() {
		t.Fatal("AA010_VIEW_LEASE_POSITIVE", err)
	}
	if _, err := e.hub.subViewLease(req, "bb."+root.FullName); err == nil {
		t.Fatal("AA010_VIEW_LEASE_CROSS_SCOPE")
	}
	e.svc.Store.RevokeSession(e.ctx, identity.HashSecret("test-viewer"), time.Now())
	if lease() {
		t.Fatal("AA010_VIEW_EXPIRED_REVOKED_SUBTREE")
	}
	if err := e.hub.muxProjectionSubject(req, "missing."+root.FullName); err == nil || req.Header.Get("X-DW-Mux-Subject") != "" {
		t.Fatal("AA010_FORGED_HEADER_STRIPPED")
	}
}

func TestSubAgentViewNewHubMixedMatrix(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		t.Run(fmt.Sprint(negotiated), func(t *testing.T) {
			e := newAgentEnv(t)
			w := subWire(t, e)
			if negotiated {
				cap := w.negotiate()
				ack := w.report(int(subAgentInt64(cap.Meta, "conn_seq")), 1, 0, subNodes())
				if !metaBool(ack.Meta, "ok") {
					t.Fatal(ack)
				}
			}
			err := e.hub.RouteEnvelope(e.ctx, subEnv("matrix-child", "aa.opus50ts.zzc", "child"))
			if negotiated {
				if err != nil {
					t.Fatal("AA010_NEW_NEW_DELIVERY", err)
				}
				got := w.until(func(e model.Envelope) bool { return e.ID == "matrix-child" })
				if metaString(got.Meta, "target_node_id") != "node-a" {
					t.Fatal("AA010_NEW_NEW_SCOPE", got)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "unsupported") {
					t.Fatal("AA010_NEW_OLD_UNSUPPORTED", err)
				}
			}
			if err := e.hub.RouteEnvelope(e.ctx, subEnv("matrix-root", "opus50ts.zzc", "root")); err != nil {
				t.Fatal("AA010_MIXED_ROOT_PRESERVED", err)
			}
			w.until(func(e model.Envelope) bool { return e.ID == "matrix-root" })
		})
	}
}

func TestSubAgentViewMentionRootsOnly(t *testing.T) {
	e, _, _, root := subViewFixture(t)
	_, other := e.enroll("mention-peer", "Other")
	group, err := e.svc.CreateGroup(e.ctx, "zzc", e.agID, "scope_mentions")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.JoinGroup(e.ctx, "zzc", group.Code, other); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		req := httptest.NewRequest("GET", "/view/"+name+"/mention-candidates", nil)
		req.SetPathValue("sessionName", name)
		out := httptest.NewRecorder()
		e.hub.HandleViewMentionCandidates(out, req)
		if out.Code != 200 {
			t.Fatal("AA010_CHILD_MENTION_ROOT_AUTHORITY", out.Code)
		}
		return out.Body.String()
	}
	main, child := read(root.FullName), read("aa."+root.FullName)
	if main != child || !strings.Contains(strings.ToLower(child), "other.zzc") || strings.Contains(child, "aa.") || strings.Contains(child, "bb.") {
		t.Fatal("AA010_MENTION_ROOT_ONLY", main, child)
	}
}

func TestSubAgentViewSidecarObservation(t *testing.T) {
	e, w, _, root := subViewFixture(t)
	aa, err := e.hub.subViewAgent(e.ctx, root, "aa."+root.FullName)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.hub.RouteEnvelope(e.ctx, subEnv("view-sidecar", "aa."+root.FullName, "child input")); err != nil {
		t.Fatal(err)
	}
	w.until(func(e model.Envelope) bool { return e.ID == "view-sidecar" })
	count := func(a consolemux.Agent) int {
		snap, _ := e.hub.consoleMuxSnapshot(a)
		n := 0
		for _, ev := range snap.Replay {
			var v struct {
				Kind     string `json:"kind"`
				Delivery struct {
					Role string `json:"role"`
				} `json:"delivery"`
			}
			json.Unmarshal([]byte(ev.Data), &v)
			if v.Kind == "input_posted" && v.Delivery.Role == "primary" {
				n++
			}
		}
		return n
	}
	if count(root) != 0 || count(aa) != 1 {
		t.Fatal("AA010_INPUT_SIDECAR_SCOPE", count(root), count(aa))
	}
}
