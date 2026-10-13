package m8

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
)

// Resolve after actor authorization; this test never creates a listener or
// connects to a live authority. Both control handlers serialize this same error.
func TestMuxScopedUnavailableControl(t *testing.T) {
	h, _, _ := newTestHub(t)
	config := filepath.Join(t.TempDir(), "routing.json")
	routing := ViewRouting{Default: "v2", Targets: map[string]ViewTarget{"v2": {URL: "https://unused.invalid", ViewUnlockV2: true, HubToVSSecretFile: "unused-forward", VSToHubSecretFile: "unused-reverse"}}}
	raw, _ := json.Marshal(routing)
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", config)
	resetViewRoutingCacheForTest()
	t.Cleanup(resetViewRoutingCacheForTest)
	live := consolemux.Agent{ID: "live-id", FullName: "live.owner", Owner: "owner", Session: "live"}
	offline := consolemux.Agent{ID: "offline-id", FullName: "offline.owner", Owner: "owner", Session: "offline"}
	auth := consolemux.Authorization{Principal: "owner", Agents: map[string]consolemux.Agent{live.FullName: live, offline.FullName: offline}}
	h.publishSessionEvent(AgentKeyID("owner"), "live", `{"kind":"test"}`)
	m := &consoleMuxRegistry{h: h}
	instance := "11111111111111111111111111111111"
	good := consoleMuxDesired{FullName: live.FullName, Instance: instance}
	initial, code, err := m.resolveScopes(auth, consoleMuxControl{Subscriptions: []consoleMuxDesired{good}}, nil)
	if err != nil || code != 200 {
		t.Fatalf("live: %d %v", code, err)
	}
	for _, tc := range []struct{ name, cause string }{{"unknown.owner", "not_found"}, {offline.FullName, "agent_offline"}} {
		for _, phase := range []string{"POST", "PUT"} {
			t.Run(phase+"/"+tc.cause, func(t *testing.T) {
				var old map[string]*consoleMuxScope
				if phase == "PUT" {
					old = initial
				}
				_, code, err := m.resolveScopes(auth, consoleMuxControl{Subscriptions: []consoleMuxDesired{good, {FullName: tc.name, Instance: instance}}}, old)
				var scoped *muxScopeControlError
				if code != 404 || !errors.As(err, &scoped) || scoped.name != tc.name || scoped.Error() != tc.cause {
					t.Fatalf("wrong scope: %d %v", code, err)
				}
				w := httptest.NewRecorder()
				muxWriteScopeError(w, code, err)
				var body map[string]string
				if json.Unmarshal(w.Body.Bytes(), &body) != nil || w.Code != 404 || body["error"] != tc.cause || body["full_name"] != tc.name || len(body) != 2 {
					t.Fatalf("wrong wire: %s", w.Body.String())
				}
				if len(initial) != 1 || initial[live.FullName].Agent != live {
					t.Fatal("failure mutated previous scope")
				}
			})
		}
	}
	if _, ok := h.consoleMuxSnapshot(offline); ok {
		t.Fatal("missing terminal was fabricated")
	}
	if _, code, err := m.resolveScopes(auth, consoleMuxControl{Subscriptions: []consoleMuxDesired{good}}, initial); code != 200 || err != nil {
		t.Fatalf("remaining live scope: %d %v", code, err)
	}
	w := httptest.NewRecorder()
	muxWriteScopeError(w, 404, errors.New("not_found"))
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["full_name"]; ok {
		t.Fatal("feed-missing error acquired scope")
	}
}
