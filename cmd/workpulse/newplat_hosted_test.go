package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// fakeHostedItem 仿 G4 的 hosted.HostedListItem（契约 v2 第二节；本包不 import G4，只认 json 标签）。
// 三个值互不相同、类型各异，钉的是「键名照抄标签、值与类型原样」，不是碰巧相等。
type fakeHostedItem struct {
	NodeName   string `json:"node_name"`
	State      string `json:"state"`
	Upgradable bool   `json:"upgradable"`
}

func TestHostedListHook(t *testing.T) {
	ctx := context.Background()
	var calls [][]string
	overlay := func(_ context.Context, ids []string) (map[string]fakeHostedItem, error) {
		calls = append(calls, append([]string(nil), ids...))
		switch ids[0] {
		case "ag-boom":
			return nil, errors.New("seat store down")
		case "ag-h":
			return map[string]fakeHostedItem{"ag-h": {NodeName: "node-106", State: "busy", Upgradable: true}}, nil
		}
		return map[string]fakeHostedItem{}, nil
	}
	hook := hostedListHook(overlay)

	got, ok := hook(ctx, "ag-h")
	want := map[string]any{"node_name": "node-106", "state": "busy", "upgradable": true}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("hosted row: ok=%v %#v, want %#v", ok, got, want)
	}
	if got, ok := hook(ctx, "ag-gone"); ok || got != nil {
		t.Errorf("absent from overlay (未落席位／已销毁) must yield no sub-object: ok=%v %#v", ok, got)
	}
	if got, ok := hook(ctx, "ag-boom"); ok || got != nil {
		t.Errorf("overlay error must degrade to no sub-object: ok=%v %#v", ok, got)
	}
	if !reflect.DeepEqual(calls, [][]string{{"ag-h"}, {"ag-gone"}, {"ag-boom"}}) {
		t.Errorf("overlay calls %v, want exactly one id per call", calls)
	}
}

// 接线：cfg.hosted 经 wireNewPlatform 真到了 GET /api/console/agents —— 托管行带 hosted 子对象，自备行不调、不带；
// 叠加里缺席的托管行不带。
func TestWireNewPlatformHostedOverlay(t *testing.T) {
	ctx := context.Background()
	hub := newplatHub(t)
	st := identity.NewMemStore()
	var asked []string
	overlay := func(_ context.Context, ids []string) (map[string]fakeHostedItem, error) {
		asked = append(asked, ids...)
		if ids[0] == "ag-hosted" {
			return map[string]fakeHostedItem{"ag-hosted": {NodeName: "node-68", State: "running", Upgradable: false}}, nil
		}
		return nil, nil
	}
	mux := http.NewServeMux()
	if err := wireNewPlatform(ctx, mux, hub, newPlatformConfig{publicOrigin: "https://x", hosted: hostedListHook(overlay)}, st); err != nil {
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
