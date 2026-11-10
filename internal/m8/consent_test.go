package m8

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"strings"
	"testing"
	"time"
)

func TestConsentLongExternalRoute(t *testing.T) {
	v := newViewEnv(t)
	codes, e := v.svc.Store.ListAgentGroups(v.ctx, v.agID)
	if e != nil {
		t.Fatal(e)
	}
	ex := identity.Agent{ID: "external", Name: "abcdefgh261109060000_AG999999999999999999", OwnerLC: "zzc", Origin: identity.OriginExternal, State: identity.AgentActive}
	v.svc.Store = externalIdentityFixture{Store: v.svc.Store, external: ex, codes: codes}
	capture := &externalCapture{}
	v.hub.ExternalSeats = capture
	body := "literal https://example.invalid/file?a=b\n原始正文"
	env := model.Envelope{ID: "source-external-fixture", From: "opus50ts#u_zzc", To: "abcdefgh261109060000_AG999999999999999999.zzc#u_zzc", Body: body}
	if e = v.hub.RouteEnvelope(v.ctx, env); e != nil {
		t.Fatal(e)
	}
	if capture.calls != 1 || capture.source != v.agID || capture.target != ex.ID || capture.body != body {
		t.Fatalf("PG route lost identity/body: %+v", capture)
	}
	if v.hub.sessionClients["u_zzc"]["abcdefgh261109060000_ag999999999999999999"] != nil {
		t.Fatal("fake WebSocket client")
	}
	capture.err = extshared.ErrGroupDissolved
	env.ID = "expired-group"
	if e = v.hub.RouteEnvelope(v.ctx, env); !errors.Is(e, extshared.ErrGroupDissolved) {
		t.Fatal(e)
	}
	capture.err = nil
	env.ID = "control-external"
	env.Meta = map[string]any{"type": "terminal_input"}
	if e = v.hub.RouteEnvelope(v.ctx, env); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatal("external control allowed", e)
	}
	capture.online = []extseat.OnlineAgent{{Agent: ex, FullName: "abcdefgh261109060000_AG999999999999999999.zzc", External: true, LastSeenAt: time.Now()}}
	if !v.hub.AgentOnline(ex.ID) {
		t.Fatal("external presence not merged into Console")
	}
	capture.online = nil
	if v.hub.AgentOnline(ex.ID) {
		t.Fatal("external stale online")
	}
}

func TestConsentLongExternalDirectory(t *testing.T) {
	v := newViewEnv(t)
	v.hub.onlineDebounce = time.Hour
	codes, e := v.svc.Store.ListAgentGroups(v.ctx, v.agID)
	if e != nil {
		t.Fatal(e)
	}
	ex := identity.Agent{ID: "external", Name: "abcdefgh261109060000_AG999999999999999999", OwnerLC: "zzc", Origin: identity.OriginExternal, State: identity.AgentActive}
	v.svc.Store = externalIdentityFixture{Store: v.svc.Store, external: ex, codes: codes}
	v.hub.ExternalSeats = &externalCapture{online: []extseat.OnlineAgent{{Agent: ex, FullName: "abcdefgh261109060000_AG999999999999999999.zzc", External: true, LastSeenAt: time.Now()}}}
	conn, status, _ := v.dial(v.cred, "")
	if status != 101 {
		t.Fatal(status)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, v.hub, "u_zzc", "opus50ts")
	if e = v.hub.BroadcastOnlineDirectories(v.ctx); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(v.ctx, 3*time.Second)
	defer cancel()
	for {
		env := readEnvelopeIncludingSystem(t, ctx, conn)
		if metaString(env.Meta, "type") != "online_directory" {
			continue
		}
		if !strings.Contains(env.Body, "abcdefgh261109060000_AG999999999999999999.zzc") {
			t.Fatal("external absent from helper directory", env.Body)
		}
		break
	}
	// 读取helper provider不会把external伪装为helper presence。
	items, e := (HubHelperPresence{Hub: v.hub}).Online(v.ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range items {
		if p.External || p.Agent.ID == ex.ID {
			t.Fatal("external became helper presence")
		}
	}
}
