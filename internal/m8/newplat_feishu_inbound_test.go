package m8

import (
	"bytes"
	"encoding/json"
	"log"
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
	content, _ := json.Marshal(map[string]string{"text": text})
	return model.Message{ID: id, FeishuMsgID: id, ChatEntityID: "devplatform:personal:ou_alice", BotChannelID: "devplatform", ChatType: model.ChatPersonal, SenderOpenID: "ou_alice", IngressProvenance: model.IngressFeishuWSAuthenticated, Content: string(content)}
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

	for i, tc := range []struct{ text, want string }{{"#opus50ts hello-selector", "hello-selector"}, {"#Opus50ts.zzc hello-full-name", "hello-full-name"}, {"hello-default", "hello-default"}} {
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
	if got := e.hub.resolveNewPlatformFeishuSession(e.ctx, "zzc", "Opus50ts.zzc"); got.status != ownerSessionNoMatch {
		t.Fatalf("revoked Agent full_name remained routable: %+v", got)
	}
	res, handled, err := e.hub.dispatchSelector(e.ctx, feishuInboundMessage("revoked", "#opus50ts blocked"), "#opus50ts blocked")
	if err != nil || !handled || !strings.Contains(res.Reply, "未找到你名下") {
		t.Fatalf("revoked selector: handled=%v reply=%q err=%v", handled, res.Reply, err)
	}
}

func TestNewPlatformFullNameRejectsSuspendedAgentWithStaleSocket(t *testing.T) {
	e := newAgentEnv(t)
	hi, err := e.svc.IssueHostedAgentWithNewGroup(e.ctx, "zzc", "Pausy", tg(), "zzc")
	if err != nil {
		t.Fatal(err)
	}
	e.hub.mu.Lock()
	e.hub.sessionClients[AgentKeyID("zzc")] = map[string]*sessionClient{"pausy": {keyID: AgentKeyID("zzc"), sessionName: "pausy"}}
	e.hub.mu.Unlock()
	if _, err := e.svc.Store.SetAgentSuspended(e.ctx, hi.Agent.ID, true, "zzc", e.svc.Now()); err != nil {
		t.Fatal(err)
	}
	if got := e.hub.resolveNewPlatformFeishuSession(e.ctx, "zzc", "Pausy.zzc"); got.status != ownerSessionNoMatch {
		t.Fatalf("suspended Agent full_name remained routable through stale socket: %+v", got)
	}
	if got := e.hub.resolveOwnerSessionByShort(e.ctx, "zzc", "Pausy.zzc", ""); got.status != ownerSessionNoMatch {
		t.Fatalf("suspended Agent full_name remained Agent-routable: %+v", got)
	}
}

func TestNewPlatformFeishuFullNameOwnerMismatchLooksMissing(t *testing.T) {
	v := newViewEnv(t)
	bindFeishuForAgentEnv(t, v.agentEnv, "ou_alice")
	v.hub.mu.Lock()
	v.hub.sessionClients[AgentKeyID("lbl")] = map[string]*sessionClient{"kimi": {keyID: AgentKeyID("lbl"), sessionName: "kimi"}}
	v.hub.mu.Unlock()
	text := "#Kimi.lb must-not-cross-owner"
	res, handled, err := v.hub.dispatchSelector(v.ctx, feishuInboundMessage("foreign-full-name", text), text)
	want := groupAgentNotFoundText
	if err != nil || !handled || res.Reply != want {
		t.Fatalf("foreign full_name did not use missing-target response: handled=%v reply=%q want=%q err=%v", handled, res.Reply, want, err)
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
	if err != nil || !strings.Contains(res.Reply, "请选择一个 Agent") || !strings.Contains(strings.ToLower(res.Reply), "kimi") || !strings.Contains(strings.ToLower(res.Reply), "opus50ts") {
		t.Fatalf("multiple sessions: reply=%q err=%v", res.Reply, err)
	}
}

func TestNewPlatformFeishuDefaultCountsOnlySenderOwnedSessions(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	if err := e.svc.Store.CreateUser(e.ctx, identity.User{UserID: "lbl", ShortName: "lb", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	otherCred := e.enrollForOwner("lbl", "dev-default-other", "Other")
	own, _, _ := e.dial(e.cred, "")
	other, _, _ := e.dial(otherCred, "")
	if own == nil || other == nil {
		t.Fatal("agent websocket did not connect")
	}
	defer own.CloseNow()
	defer other.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	waitSessionOnline(t, e.hub, AgentKeyID("lbl"), "other")
	res, err := e.hub.Dispatch(e.ctx, feishuInboundMessage("owned-unique", "owned-only"), "owned-only")
	if err != nil || res.Reply != "" {
		t.Fatalf("default with foreign online Agent: %+v err=%v", res, err)
	}
	if got := e.readEnvUntil(own, envelopeReadTimeout, func(env model.Envelope) bool { return env.Body == "owned-only" }); got == nil {
		t.Fatal("sender's unique Agent did not receive default message")
	}
	if leaked := e.readEnvUntil(other, 250*time.Millisecond, func(env model.Envelope) bool { return env.Body == "owned-only" }); leaked != nil {
		t.Fatalf("default message leaked across owner: %+v", leaked)
	}
}

func TestNewPlatformFeishuDefaultRejectsUnboundSender(t *testing.T) {
	e := newAgentEnv(t)
	msg := feishuInboundMessage("unbound-default", "plain-unbound")
	msg.SenderOpenID = "ou_unknown"
	msg.ChatEntityID = "devplatform:personal:ou_unknown"
	var logs bytes.Buffer
	oldLogWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldLogWriter) })
	res, handled, err := e.hub.dispatchDefaultPersonalSession(e.ctx, msg, "plain-unbound")
	if err != nil || !handled || !strings.Contains(res.Reply, "未绑定或已停用") {
		t.Fatalf("unbound default sender was not rejected: %+v handled=%v err=%v", res, handled, err)
	}
	if !strings.Contains(logs.String(), "reason=sender_not_active_member") {
		t.Fatalf("unbound default rejection lacked structured reason: %q", logs.String())
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
	mismatch := "@lb#Opus50ts.zzc owner-mismatch"
	res, handled, err = v.hub.dispatchMemberMention(v.ctx, feishuInboundMessage("member-owner-mismatch", mismatch), mismatch)
	if err != nil || !handled || !strings.Contains(res.Reply, "未找到该成员的在线 Agent") {
		t.Fatalf("member full_name owner mismatch leaked target: handled=%v reply=%q err=%v", handled, res.Reply, err)
	}
}

func TestNewPlatformFeishuMemberMentionDeliversWithinGroup(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	if err := e.svc.Store.CreateUser(e.ctx, identity.User{UserID: "lbl", ShortName: "lb", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	targetCred := e.enrollForOwner("lbl", "dev-lbl", "Kimi")
	target, err := e.svc.Store.FindAgentByName(e.ctx, "lbl", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	codes, err := e.svc.Store.ListAgentGroups(e.ctx, e.agID)
	if err != nil || len(codes) == 0 {
		t.Fatalf("source groups: %v %v", codes, err)
	}
	if err := e.svc.JoinGroup(e.ctx, "lbl", codes[0], target.ID); err != nil {
		t.Fatal(err)
	}
	source, _, _ := e.dial(e.cred, "")
	targetConn, _, _ := e.dial(targetCred, "")
	if source == nil || targetConn == nil {
		t.Fatal("agent websocket did not connect")
	}
	defer source.CloseNow()
	defer targetConn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	waitSessionOnline(t, e.hub, AgentKeyID("lbl"), "kimi")
	for i, tc := range []struct{ selector, body string }{{"@lb#kimi", "hello-member-short"}, {"@lb#Kimi.lb", "hello-member-full"}} {
		text := tc.selector + " " + tc.body
		msg := feishuInboundMessage("member-positive-"+string(rune('a'+i)), text)
		res, err := e.hub.Dispatch(e.ctx, msg, text)
		if err != nil || res.Reply != "" {
			t.Fatalf("same-group member dispatch %q: %+v err=%v", tc.selector, res, err)
		}
		got := e.readEnvUntil(targetConn, envelopeReadTimeout, func(env model.Envelope) bool { return env.Body == tc.body })
		if got == nil || got.To != sessionAddress("kimi", AgentKeyID("lbl")) {
			t.Fatalf("same-group member delivery %q: %+v", tc.selector, got)
		}
	}
}

func TestNewPlatformFeishuDisabledUserCannotUseStaleLegacyMapping(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	e.hub.mu.Lock()
	e.hub.sessionClients[AgentKeyID("zzc")] = map[string]*sessionClient{"opus50ts": {keyID: AgentKeyID("zzc"), sessionName: "opus50ts"}}
	e.hub.mu.Unlock()
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
	checks := []struct {
		name string
		call func() (model.PrefixDispatchResult, bool, error)
	}{
		{"default", func() (model.PrefixDispatchResult, bool, error) {
			return e.hub.dispatchDefaultPersonalSession(e.ctx, feishuInboundMessage("disabled-default", "plain"), "plain")
		}},
		{"member", func() (model.PrefixDispatchResult, bool, error) {
			return e.hub.dispatchMemberMention(e.ctx, feishuInboundMessage("disabled-member", "@lb#kimi blocked"), "@lb#kimi blocked")
		}},
		{"mirror", func() (model.PrefixDispatchResult, bool, error) {
			return e.hub.dispatchMirrorCommand(e.ctx, feishuInboundMessage("disabled-mirror", "mirror on opus50ts"), "mirror on opus50ts")
		}},
		{"sync", func() (model.PrefixDispatchResult, bool, error) {
			return e.hub.dispatchFeishuSyncCommand(e.ctx, feishuInboundMessage("disabled-sync", "#sync opus50ts"), "#sync opus50ts")
		}},
	}
	for _, tc := range checks {
		res, handled, err := tc.call()
		if err != nil || !handled || !strings.Contains(res.Reply, "账号已停用") {
			t.Errorf("disabled %s fell through: handled=%v reply=%q err=%v", tc.name, handled, res.Reply, err)
		}
	}
	if got := e.hub.ownerFeishuOpenID(e.ctx, AgentKeyID("zzc")); got != "" {
		t.Fatalf("disabled owner's Feishu identity remained a fallback: %q", got)
	}
	if got := e.hub.ownerKeyForKey(e.ctx, AgentKeyID("zzc")); got != "" {
		t.Fatalf("disabled user's virtual key still resolved an owner: %q", got)
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
	msg = feishuInboundMessage("h17-unsync", "#unsync Opus50ts.zzc")
	unsync, err := e.hub.Dispatch(e.ctx, msg, "#unsync Opus50ts.zzc")
	if err != nil || !unsync.Matched || !strings.Contains(unsync.Reply, "已停止同步 opus50ts") {
		t.Fatalf("H17 unsync did not resolve new-platform owner: %+v err=%v", unsync, err)
	}
	msg = feishuInboundMessage("mirror-new-identity", "mirror on opus50ts")
	mirror, handled, err := e.hub.dispatchMirrorCommand(e.ctx, msg, "mirror on opus50ts")
	if err != nil || !handled || !strings.Contains(mirror.Reply, "镜像已开启") {
		t.Fatalf("mirror did not resolve new-platform owner: %+v handled=%v err=%v", mirror, handled, err)
	}
	msg = feishuInboundMessage("h17-sync-full", "#sync Opus50ts.zzc")
	sync, err = e.hub.Dispatch(e.ctx, msg, "#sync Opus50ts.zzc")
	if err != nil || !sync.Matched || !strings.Contains(sync.Reply, "已开启同步 opus50ts") {
		t.Fatalf("full_name sync did not normalize: %+v err=%v", sync, err)
	}
	msg = feishuInboundMessage("h17-unsync-full", "#unsync opus50ts")
	unsync, err = e.hub.Dispatch(e.ctx, msg, "#unsync opus50ts")
	if err != nil || !unsync.Matched || !strings.Contains(unsync.Reply, "已停止同步 opus50ts") {
		t.Fatalf("full_name unsync did not normalize: %+v err=%v", unsync, err)
	}
	msg = feishuInboundMessage("mirror-full-name", "mirror off Opus50ts.zzc")
	mirror, handled, err = e.hub.dispatchMirrorCommand(e.ctx, msg, "mirror off Opus50ts.zzc")
	if err != nil || !handled || !strings.Contains(mirror.Reply, "镜像已关闭") {
		t.Fatalf("full_name mirror did not normalize: %+v handled=%v err=%v", mirror, handled, err)
	}
}

func TestNewPlatformFeishuGroupIntentAndUnboundGate(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	conn, _, _ := e.dial(e.cred, "")
	if conn == nil {
		t.Fatal("agent websocket did not connect")
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	groupMsg := func(id, sender, text string) model.Message {
		return model.Message{ID: id, ChatEntityID: "devplatform:group:oc_team", BotChannelID: "devplatform", ChatType: model.ChatGroup, SenderOpenID: sender, IngressProvenance: model.IngressFeishuWSAuthenticated, Content: `{"text":"` + text + `"}`}
	}
	res, err := e.hub.Dispatch(e.ctx, groupMsg("group-explicit", "ou_alice", "#opus50ts group-task"), "#opus50ts group-task")
	if err != nil || res.Reply != "" {
		t.Fatalf("explicit group dispatch: %+v err=%v", res, err)
	}
	if got := e.readEnvUntil(conn, envelopeReadTimeout, func(env model.Envelope) bool { return env.Body == "group-task" }); got == nil {
		t.Fatal("explicit group selector was not delivered")
	}
	quiet, err := e.hub.Dispatch(e.ctx, groupMsg("group-idle", "ou_alice", "ordinary group chat"), "ordinary group chat")
	if err != nil || quiet.Matched || quiet.Reply != "" {
		t.Fatalf("group idle chat was not silent: %+v err=%v", quiet, err)
	}
	unbound, err := e.hub.Dispatch(e.ctx, groupMsg("group-unbound", "ou_unknown", "#opus50ts forbidden"), "#opus50ts forbidden")
	if err != nil || !unbound.Matched || !strings.Contains(unbound.Reply, "未绑定或已停用") {
		t.Fatalf("unbound group sender was not rejected: %+v err=%v", unbound, err)
	}
}

func TestNewPlatformFeishuLiveAgentRevocationStopsInbound(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	conn, _, _ := e.dial(e.cred, "")
	if conn == nil {
		t.Fatal("agent websocket did not connect")
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	closed := reader(conn)
	if _, err := e.svc.Revoke(e.ctx, "zzc", e.agID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(6 * time.Second):
		t.Fatal("revoked live websocket did not close")
	}
	waitFor(t, 6*time.Second, "revoked Agent removed from live session bucket", func() bool {
		return !e.hub.sessionOnline(AgentKeyID("zzc"), "opus50ts")
	})
	res, handled, err := e.hub.dispatchSelector(e.ctx, feishuInboundMessage("revoked-live", "#opus50ts blocked"), "#opus50ts blocked")
	if err != nil || !handled || !strings.Contains(res.Reply, "未找到你名下在线且可用") {
		t.Fatalf("live-revoked Agent remained routable: %+v handled=%v err=%v", res, handled, err)
	}
}
