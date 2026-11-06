package m8

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type externalIdentityFixture struct {
	identity.Store
	external identity.Agent
	codes    []string
}

func (s externalIdentityFixture) FindAgentByName(ctx context.Context, owner, name string) (*identity.Agent, error) {
	if owner == s.external.OwnerLC && strings.EqualFold(name, s.external.Name) {
		a := s.external
		return &a, nil
	}
	return s.Store.FindAgentByName(ctx, owner, name)
}
func (s externalIdentityFixture) GetAgent(ctx context.Context, id string) (*identity.Agent, error) {
	if id == s.external.ID {
		a := s.external
		return &a, nil
	}
	return s.Store.GetAgent(ctx, id)
}
func (s externalIdentityFixture) ListAgentGroups(ctx context.Context, id string) ([]string, error) {
	if id == s.external.ID {
		return s.codes, nil
	}
	return s.Store.ListAgentGroups(ctx, id)
}

type externalCapture struct {
	source, target, body string
	online               []extseat.OnlineAgent
	err                  error
	calls                int
}

func (c *externalCapture) EnqueueFromHelper(ctx context.Context, source, target, id, body string) (extseat.SendReceipt, error) {
	c.calls++
	c.source = source
	c.target = target
	c.body = body
	return extseat.SendReceipt{DeliveryID: "dw-ext_fixture", Stage: extshared.StageAccepted}, c.err
}
func (c *externalCapture) Online(context.Context) ([]extseat.OnlineAgent, error) {
	return c.online, c.err
}
func TestExternalRouteUsesPGAdapterWithoutWS(t *testing.T) {
	v := newViewEnv(t)
	codes, e := v.svc.Store.ListAgentGroups(v.ctx, v.agID)
	if e != nil {
		t.Fatal(e)
	}
	ex := identity.Agent{ID: "external", Name: "bot_AG1", OwnerLC: "zzc", Origin: identity.OriginExternal, State: identity.AgentActive}
	v.svc.Store = externalIdentityFixture{Store: v.svc.Store, external: ex, codes: codes}
	capture := &externalCapture{}
	v.hub.ExternalSeats = capture
	body := "literal https://example.invalid/file?a=b\n原始正文"
	env := model.Envelope{ID: "source-external-fixture", From: "opus50ts#u_zzc", To: "bot_AG1.zzc#u_zzc", Body: body}
	if e = v.hub.RouteEnvelope(v.ctx, env); e != nil {
		t.Fatal(e)
	}
	if capture.calls != 1 || capture.source != v.agID || capture.target != ex.ID || capture.body != body {
		t.Fatalf("PG route lost identity/body: %+v", capture)
	}
	if v.hub.sessionClients["u_zzc"]["bot_ag1"] != nil {
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
	capture.online = []extseat.OnlineAgent{{Agent: ex, FullName: "bot_AG1.zzc", External: true, LastSeenAt: time.Now()}}
	if !v.hub.AgentOnline(ex.ID) {
		t.Fatal("external presence not merged into Console")
	}
	capture.online = nil
	if v.hub.AgentOnline(ex.ID) {
		t.Fatal("external stale online")
	}
}
func TestExternalProjectionIdempotentPreservesHelperQueue(t *testing.T) {
	v := newViewEnv(t)
	codes, _ := v.svc.Store.ListAgentGroups(v.ctx, v.agID)
	ex := identity.Agent{ID: "external", Name: "bot_AG1", OwnerLC: "zzc", Origin: identity.OriginExternal, State: identity.AgentActive}
	v.svc.Store = externalIdentityFixture{Store: v.svc.Store, external: ex, codes: codes}
	target, e := v.svc.Store.GetAgent(v.ctx, v.agID)
	if e != nil {
		t.Fatal(e)
	}
	p := HubExtseatProjector{Hub: v.hub}
	d := extseat.Delivery{ID: "dw-ext_projection", From: "bot_AG1.zzc", To: "Opus50TS.zzc", Body: "literal link https://example.invalid/test", Generation: 1, CreatedAt: time.Now().UTC()}
	if e = p.Project(v.ctx, d, *target); e != nil {
		t.Fatal(e)
	}
	if e = p.Project(v.ctx, d, *target); e != nil {
		t.Fatal(e)
	}
	got, e := v.hub.Repo.GetSessionDelivery(v.ctx, d.ID)
	if e != nil || got == nil {
		t.Fatal(e)
	}
	if got.AckProtocol != 0 || got.State != "queued" || got.SourceType != "extseat_inbound" || got.Attempts != 0 {
		t.Fatalf("helper semantics changed: %+v", got)
	}
	if !strings.Contains(got.Envelope, d.Body) {
		t.Fatal("body lost")
	}
	d.Body = "different"
	if e = p.Project(v.ctx, d, *target); !errors.Is(e, extshared.ErrIdempotencyKey) {
		t.Fatal("projection conflict", e)
	}
}

func TestExternalDirectoryMergesByExistingGroupVisibility(t *testing.T) {
	v := newViewEnv(t)
	v.hub.onlineDebounce = time.Hour
	codes, e := v.svc.Store.ListAgentGroups(v.ctx, v.agID)
	if e != nil {
		t.Fatal(e)
	}
	ex := identity.Agent{ID: "external", Name: "bot_AG1", OwnerLC: "zzc", Origin: identity.OriginExternal, State: identity.AgentActive}
	v.svc.Store = externalIdentityFixture{Store: v.svc.Store, external: ex, codes: codes}
	v.hub.ExternalSeats = &externalCapture{online: []extseat.OnlineAgent{{Agent: ex, FullName: "bot_AG1.zzc", External: true, LastSeenAt: time.Now()}}}
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
		if !strings.Contains(env.Body, "bot_AG1.zzc") {
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
