package m8

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"strings"
	"testing"
	"time"
)

func TestNewPlatformDirectoryLeaderOnlyAndReplacement(t *testing.T) {
	v := newViewEnv(t)
	v.hub.onlineDebounce = time.Hour
	g, err := v.svc.CreateGroup(v.ctx, "zzc", v.agID, tg())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := v.svc.Store.FindAgentByName(v.ctx, "zzc", "kimi")
	b, _ := v.svc.Store.FindAgentByName(v.ctx, "lbl", "kimi")
	for _, ag := range []struct{ owner, id string }{{"zzc", a.ID}, {"lbl", b.ID}} {
		if err := v.svc.JoinGroup(v.ctx, ag.owner, g.Code, ag.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.svc.SetGroupLeaderOnly(v.ctx, "zzc", g.Code, true); err != nil {
		t.Fatal(err)
	}
	blocked := v.hub.enforceNewPlatformGrouping(v.ctx, address{Kind: addressSession, KeyID: "u_zzc", SessionName: "kimi"}, model.Envelope{}, "u_lbl", "kimi")
	if blocked == nil {
		t.Fatal("member-to-member route bypassed special group")
	}
	if err := v.hub.enforceNewPlatformGrouping(v.ctx, address{Kind: addressSession, KeyID: "u_zzc", SessionName: "opus50ts"}, model.Envelope{}, "u_lbl", "kimi"); err != nil {
		t.Fatalf("leader route blocked: %v", err)
	}
	conns := map[string]*websocket.Conn{}
	for _, peer := range []struct{ name, cred, key, session string }{{"leader", v.cred, "u_zzc", "opus50ts"}, {"a", v.zKimiCred, "u_zzc", "kimi"}, {"b", v.lKimiCr, "u_lbl", "kimi"}} {
		c, status, body := v.dial(peer.cred, "")
		if status != 101 {
			t.Fatalf("dial %s %d %s", peer.name, status, body)
		}
		defer c.Close(websocket.StatusNormalClosure, "done")
		conns[peer.name] = c
		waitSessionOnline(t, v.hub, peer.key, peer.session)
	}
	read := func(c *websocket.Conn) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		defer cancel()
		for {
			_, raw, err := c.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var env model.Envelope
			if err = json.Unmarshal(raw, &env); err != nil {
				t.Fatal(err)
			}
			if env.Meta["type"] == "online_directory" {
				if env.Meta["snapshot"] != true {
					t.Fatal("not replacement snapshot")
				}
				return env.Body
			}
		}
	}
	if err := v.hub.BroadcastOnlineDirectories(v.ctx); err != nil {
		t.Fatal(err)
	}
	leader := read(conns["leader"])
	if !strings.Contains(leader, "Kimi.zzc") || !strings.Contains(leader, "kimi.lb") {
		t.Fatalf("leader omitted cross-owner peers: %s", leader)
	}
	for _, n := range []string{"a", "b"} {
		body := read(conns[n])
		if !strings.Contains(body, "Opus50TS.zzc") || strings.Contains(body, "Kimi.zzc") || strings.Contains(body, "kimi.lb") {
			t.Fatalf("member peer leaked: %s", body)
		}
	}
	if err := v.svc.LeaveGroup(v.ctx, "lbl", g.Code, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := v.hub.BroadcastOnlineDirectories(v.ctx); err != nil {
		t.Fatal(err)
	}
	if body := read(conns["b"]); strings.Contains(body, "Opus50TS.zzc") {
		t.Fatalf("removed member retained leader: %s", body)
	}
}
