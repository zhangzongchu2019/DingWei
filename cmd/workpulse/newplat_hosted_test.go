package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// 接线：cfg.hosted 经 wireNewPlatform 真到了 GET /api/console/agents —— 托管行带 hosted 子对象（原样，不改名），
// 自备行不调、不带；提供函数答 false 的托管行不带。
func TestWireNewPlatformHostedOverlay(t *testing.T) {
	ctx := context.Background()
	hub := newplatHub(t)
	st := identity.NewMemStore()
	// 仿 G4 的 Orchestrator.HostedItem：只认 ag-hosted；其余（未落席位、非托管）答 false。三个值互不相同、类型各异。
	var asked []string
	hostedItem := func(_ context.Context, agentID string) (map[string]any, bool) {
		asked = append(asked, agentID)
		if agentID == "ag-hosted" {
			return map[string]any{"node_name": "node-68", "state": "running", "upgradable": false}, true
		}
		return nil, false
	}
	mux := http.NewServeMux()
	if err := wireNewPlatform(ctx, mux, hub, newPlatformConfig{publicOrigin: "https://x", hosted: hostedItem, hostedAccess: func(*http.Request) bool { return true }}, st, nil); err != nil {
		t.Fatal(err)
	}
	t0 := hub.Identity.Now()
	if err := st.CreateUser(ctx, identity.User{UserID: "Own", ShortName: "ow", Type: identity.UserMember, State: identity.UserActive, CreatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("tok-own"), UserIDLC: "own", ExpiresAt: t0.Add(identity.SessionTTL)}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ag-hosted", "ag-seatless"} {
		if err := st.CreateHostedAgent(ctx, identity.Agent{ID: id, OwnerLC: "own", Name: "H" + id[3:7], CreatedAt: t0},
			identity.Application{ID: "app-" + id, PollTokenHash: "p-" + id, RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.CreateApplication(ctx, identity.Application{ID: "app-self", OwnerLC: "own", DeviceFingerprint: "dev", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour), State: identity.AppPending}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Approve(ctx, identity.ApproveInput{AppID: "app-self", OwnerLC: "own", AgentID: "ag-self", Name: "Selfie"}, t0); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/console/agents", nil)
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "tok-own"})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Agents []map[string]any `json:"agents"`
	}
	if res.StatusCode != 200 {
		t.Fatalf("list: %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	rows := map[string]map[string]any{}
	for _, a := range body.Agents {
		rows[a["agent_id"].(string)] = a
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d %v, want 3", len(rows), body.Agents)
	}
	want := map[string]any{"node_name": "node-68", "state": "running", "upgradable": false}
	if got := rows["ag-hosted"]["hosted"]; !reflect.DeepEqual(got, want) {
		t.Errorf("hosted row sub-object %#v, want %#v", got, want)
	}
	for _, id := range []string{"ag-seatless", "ag-self"} {
		if _, has := rows[id]["hosted"]; has {
			t.Errorf("%s must carry no hosted sub-object: %v", id, rows[id])
		}
	}
	if rows["ag-hosted"]["state"] != "active" {
		t.Errorf("top-level state stays G3's identity state: %v", rows["ag-hosted"]["state"])
	}
	for _, id := range asked {
		if id == "ag-self" {
			t.Errorf("overlay asked about self-hosted agent %s", id)
		}
	}
}
