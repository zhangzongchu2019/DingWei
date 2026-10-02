package m8

import (
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func bindFeishuForAgentEnv(t *testing.T, e *agentEnv, subject string) {
	t.Helper()
	digest := "test-registration-digest"
	if err := e.svc.Store.CreateRegistrationCode(e.ctx, "zzc", digest, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Store.RedeemRegistrationCode(e.ctx, digest, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: subject}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func feishuInboundMessage(id, text string) model.Message {
	return model.Message{ID: id, ChatEntityID: "devplatform:personal:ou_alice", BotChannelID: "devplatform", ChatType: model.ChatPersonal, SenderOpenID: "ou_alice", IngressProvenance: model.IngressFeishuWSAuthenticated, Content: `{"text":"` + text + `"}`}
}

func TestNewPlatformFeishuInboundSelectorAndDefaultUseIdentityBucket(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	if got := e.hub.ownerFeishuOpenID(e.ctx, AgentKeyID("zzc")); got != "ou_alice" {
		t.Fatalf("new identity owner fallback=%q", got)
	}
	conn, st, body := e.dial(e.cred, "")
	if conn == nil {
		t.Fatalf("dial: %d %s", st, body)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")

	for i, tc := range []struct{ text, want string }{{"#opus50ts hello-selector", "hello-selector"}, {"hello-default", "hello-default"}} {
		res, err := e.hub.Dispatch(e.ctx, feishuInboundMessage("np-feishu-"+string(rune('a'+i)), tc.text), tc.text)
		if err != nil || res.Reply != "" {
			t.Fatalf("dispatch %q: reply=%q err=%v", tc.text, res.Reply, err)
		}
		got := e.readEnvUntil(conn, 2*time.Second, func(env model.Envelope) bool { return env.Body == tc.want })
		if got == nil {
			t.Fatalf("%q was not delivered", tc.want)
		}
		if got.From != feishuAddress("ou_alice", AgentKeyID("zzc"), "devplatform") {
			t.Fatalf("from=%q", got.From)
		}
	}
}

func TestNewPlatformFeishuInboundIsolationAndRevocation(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	e.hub.mu.Lock()
	e.hub.sessionClients[AgentKeyID("zzc")] = map[string]*sessionClient{"opus50ts": {keyID: AgentKeyID("zzc"), sessionName: "opus50ts"}}
	e.hub.sessionClients[AgentKeyID("other")] = map[string]*sessionClient{"opus50ts": {keyID: AgentKeyID("other"), sessionName: "opus50ts"}}
	e.hub.mu.Unlock()
	resolved := e.hub.resolveNewPlatformFeishuSession(e.ctx, "zzc", "opus50ts")
	if resolved.status != ownerSessionUniqueHit || resolved.keyID != AgentKeyID("zzc") {
		t.Fatalf("cross-user same-name session leaked into lookup: %+v", resolved)
	}
	if _, err := e.svc.Revoke(e.ctx, "zzc", e.agID); err != nil {
		t.Fatal(err)
	}
	if got := e.hub.newPlatformOnlineSessions(e.ctx, "zzc"); len(got) != 0 {
		t.Fatalf("revoked Agent remained routable: %v", got)
	}
	res, handled, err := e.hub.dispatchSelector(e.ctx, feishuInboundMessage("revoked", "#opus50ts blocked"), "#opus50ts blocked")
	if err != nil || !handled || !strings.Contains(res.Reply, "未找到你名下") {
		t.Fatalf("revoked selector: handled=%v reply=%q err=%v", handled, res.Reply, err)
	}
}

func TestNewPlatformFeishuInboundMultipleSessionsRequireSelector(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	cred2, _ := e.enroll("dev-2", "Kimi")
	a, _, _ := e.dial(e.cred, "")
	b, _, _ := e.dial(cred2, "")
	if a == nil || b == nil {
		t.Fatal("agent websocket did not connect")
	}
	defer a.Close(websocket.StatusNormalClosure, "done")
	defer b.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "kimi")
	res, err := e.hub.Dispatch(e.ctx, feishuInboundMessage("multiple", "do-not-guess"), "do-not-guess")
	if err != nil || !strings.Contains(res.Reply, "多个在线会话") || !strings.Contains(res.Reply, "kimi") || !strings.Contains(res.Reply, "opus50ts") {
		t.Fatalf("multiple sessions: reply=%q err=%v", res.Reply, err)
	}
}

func TestNewPlatformFeishuMemberMentionRejectsCrossGroup(t *testing.T) {
	v := newViewEnv(t)
	digest := "feishu-zzc-digest"
	if err := v.svc.Store.CreateRegistrationCode(v.ctx, "zzc", digest, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.svc.Store.RedeemRegistrationCode(v.ctx, digest, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_alice"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	target, err := v.svc.Store.FindAgentByName(v.ctx, "lbl", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	v.hub.mu.Lock()
	v.hub.sessionClients[AgentKeyID("lbl")] = map[string]*sessionClient{"kimi": {keyID: AgentKeyID("lbl"), sessionName: "kimi"}}
	v.hub.mu.Unlock()
	msg := feishuInboundMessage("cross-group", "@lb#kimi blocked")
	res, handled, err := v.hub.dispatchMemberMention(v.ctx, msg, "@lb#kimi blocked")
	if err != nil || !handled || res.Reply != groupAgentNotFoundText {
		t.Fatalf("cross-group mention target=%s: handled=%v reply=%q err=%v", target.ID, handled, res.Reply, err)
	}
}

func TestNewPlatformFeishuDisabledUserCannotUseStaleLegacyMapping(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	if err := e.svc.Store.SetUserState(e.ctx, "zzc", identity.UserDisabled, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Even if a migration left legacy rows behind, the resolved disabled new
	// identity must stop before any legacy fallback can route or control it.
	if err := e.hub.Repo.UpsertMember(e.ctx, model.Member{OwnerKey: "zzc", FeishuOpenID: "ou_alice", Active: true}); err != nil {
		t.Fatal(err)
	}
	res, handled, err := e.hub.dispatchSelector(e.ctx, feishuInboundMessage("disabled", "#opus50ts blocked"), "#opus50ts blocked")
	if err != nil || !handled || !strings.Contains(res.Reply, "账号已停用") {
		t.Fatalf("disabled identity fell through: handled=%v reply=%q err=%v", handled, res.Reply, err)
	}
}

// H17: Feishu roster and sync commands must resolve the bound np_user owner;
// neither command may depend on legacy member/api_key_account rows.
func TestH17NewPlatformFeishuRosterAndSyncUseIdentityOwner(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	conn, st, body := e.dial(e.cred, "")
	if conn == nil {
		t.Fatalf("dial: %d %s", st, body)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	msg := feishuInboundMessage("h17-roster", "#roster")
	roster, err := e.hub.Dispatch(e.ctx, msg, "#roster")
	if err != nil || !roster.Matched || !strings.Contains(roster.Reply, "opus50ts") {
		t.Fatalf("H17 roster did not list new-platform Agent: %+v err=%v", roster, err)
	}
	msg = feishuInboundMessage("h17-sync", "#sync opus50ts")
	sync, err := e.hub.Dispatch(e.ctx, msg, "#sync opus50ts")
	if err != nil || !sync.Matched || !strings.Contains(sync.Reply, "已开启同步 opus50ts") {
		t.Fatalf("H17 sync did not resolve new-platform owner: %+v err=%v", sync, err)
	}
}
