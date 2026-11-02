package m8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type stickyFaultRepo struct {
	*store.SQLite
	mu                              sync.Mutex
	sent                            []model.SessionDelivery
	failRead, failRoute, failCommit bool
	hideEvidence                    bool
}

func (r *stickyFaultRepo) GetFeishuSticky(ctx context.Context, k store.FeishuStickyKey) (*store.FeishuStickyState, error) {
	if r.failRead {
		return nil, errors.New("injected sticky read failure")
	}
	return r.SQLite.GetFeishuSticky(ctx, k)
}
func (r *stickyFaultRepo) EnqueueSessionDelivery(ctx context.Context, d model.SessionDelivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failRoute {
		return errors.New("injected route rejection")
	}
	if err := r.SQLite.EnqueueSessionDelivery(ctx, d); err != nil {
		return err
	}
	r.sent = append(r.sent, d)
	return nil
}
func (r *stickyFaultRepo) FinishFeishuSticky(ctx context.Context, o store.FeishuStickyOperation, status, target, decision string, now time.Time) error {
	if r.failCommit && status == "accepted" {
		return errors.New("injected post-route commit failure")
	}
	return r.SQLite.FinishFeishuSticky(ctx, o, status, target, decision, now)
}
func (r *stickyFaultRepo) GetSessionDelivery(ctx context.Context, id string) (*model.SessionDelivery, error) {
	r.mu.Lock()
	hide := r.hideEvidence
	r.mu.Unlock()
	if hide {
		return nil, errors.New("durable evidence not yet available")
	}
	return r.SQLite.GetSessionDelivery(ctx, id)
}
func (r *stickyFaultRepo) FindFeishuStickyDelivery(ctx context.Context, o store.FeishuStickyOperation) (string, error) {
	r.mu.Lock()
	hide := r.hideEvidence
	r.mu.Unlock()
	if hide {
		return "", nil
	}
	return r.SQLite.FindFeishuStickyDelivery(ctx, o)
}
func (r *stickyFaultRepo) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.sent) }
func (r *stickyFaultRepo) last() model.Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	var e model.Envelope
	if len(r.sent) > 0 {
		_ = json.Unmarshal([]byte(r.sent[len(r.sent)-1].Envelope), &e)
	}
	return e
}

type r14Env struct {
	*agentEnv
	repo   *stickyFaultRepo
	now    time.Time
	seq    int
	agents map[string]string
	peers  map[string]*websocket.Conn
}

func newR14Env(t *testing.T, names ...string) *r14Env {
	t.Helper()
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	r := &r14Env{agentEnv: e, repo: &stickyFaultRepo{SQLite: e.hub.Repo.(*store.SQLite)}, now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), agents: map[string]string{"opus50ts": e.agID}, peers: make(map[string]*websocket.Conn)}
	e.hub.Repo = r.repo
	e.hub.NowFunc = func() time.Time { return r.now }
	r.online(e.cred, "opus50ts")
	for i, name := range names {
		cred, id := e.enroll(fmt.Sprintf("r14-%d", i), name)
		r.agents[strings.ToLower(name)] = id
		r.online(cred, strings.ToLower(name))
	}
	return r
}
func (r *r14Env) online(cred, name string) {
	r.t.Helper()
	c, status, body := r.dial(cred, "")
	if c == nil {
		r.t.Fatalf("R14 fixture dial %d %s", status, body)
	}
	r.peers[name] = c
	r.t.Cleanup(func() { c.CloseNow() })
	waitSessionOnline(r.t, r.hub, AgentKeyID("zzc"), name)
	expected, err := r.svc.Store.FindAgentByName(r.ctx, "zzc", name)
	if err != nil {
		r.t.Fatal(err)
	}
	waitFor(r.t, time.Second, "stable R14 connection ID", func() bool {
		r.hub.mu.Lock()
		defer r.hub.mu.Unlock()
		c := r.hub.sessionClients[AgentKeyID("zzc")][name]
		return c != nil && c.metricsAgentID == expected.ID
	})
	r.hub.mu.Lock()
	client := r.hub.sessionClients[AgentKeyID("zzc")][name]
	r.hub.mu.Unlock()
	client.stateMu.Lock()
	client.deliveryACKV1 = true
	client.helperReady = true
	client.stateMu.Unlock()
}
func (r *r14Env) dispatch(text string) model.PrefixDispatchResult {
	r.t.Helper()
	r.seq++
	msg := feishuInboundMessage(fmt.Sprintf("r14-%d", r.seq), text)
	return r.dispatchMsg(msg, text)
}
func (r *r14Env) dispatchMsg(msg model.Message, text string) model.PrefixDispatchResult {
	r.t.Helper()
	res, err := r.hub.Dispatch(r.ctx, msg, text)
	if err != nil {
		r.t.Fatal(err)
	}
	return res
}
func (r *r14Env) state() *store.FeishuStickyState {
	r.t.Helper()
	s, err := r.repo.SQLite.GetFeishuSticky(r.ctx, store.FeishuStickyKey{"zzc", "devplatform"})
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}
func (r *r14Env) choose(name string) {
	r.t.Helper()
	before := r.repo.count()
	res := r.dispatch("#" + name)
	if !strings.Contains(res.Reply, "已选择") || r.repo.count() != before {
		r.t.Fatalf("S022_SELECT_ONLY %q sends=%d", res.Reply, r.repo.count()-before)
	}
}
func r14Assert(t *testing.T, ok bool, marker string, args ...any) {
	t.Helper()
	if !ok {
		t.Fatal(append([]any{marker}, args...)...)
	}
}

type stickyIdentityAlias struct{ identity.Store }

func (s stickyIdentityAlias) ResolveIdentity(ctx context.Context, i identity.Identity) (*identity.User, error) {
	if i.Issuer == "another-bot" {
		i.Issuer = "devplatform"
	}
	return s.Store.ResolveIdentity(ctx, i)
}
func TestR14S001KeyIsolation(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("Beta")
	r.svc.Store = stickyIdentityAlias{r.svc.Store}
	before := r.repo.count()
	msg := feishuInboundMessage("bot2", "plain")
	msg.BotChannelID = "another-bot"
	r.dispatchMsg(msg, "plain")
	r14Assert(t, r.repo.count() == before, "S001_CHANNEL_LEAK")
	if err := r.svc.Store.CreateUser(r.ctx, identity.User{UserID: "bob", ShortName: "bob", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	digest := "bob-bind"
	r.svc.Store.CreateRegistrationCode(r.ctx, "bob", digest, time.Now().Add(time.Hour))
	r.svc.Store.RedeemRegistrationCode(r.ctx, digest, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_bob"}, time.Now())
	msg = feishuInboundMessage("bob", "plain")
	msg.SenderOpenID = "ou_bob"
	r.dispatchMsg(msg, "plain")
	r14Assert(t, r.repo.count() == before, "S001_OWNER_LEAK")
}
func TestR14S002IdentityFailure(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("Beta")
	r.svc.Store.SetUserState(r.ctx, "zzc", identity.UserDisabled, r.now)
	res := r.dispatch("plain")
	r14Assert(t, r.repo.count() == 0 && strings.Contains(res.Reply, "停用"), "S002_INACTIVE_ROUTED")
}
func TestR14S003OtherOwnerExcluded(t *testing.T) {
	r := newR14Env(t)
	if err := r.svc.Store.CreateUser(r.ctx, identity.User{UserID: "bob", ShortName: "bob", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	cred := r.enrollForOwner("bob", "other-default", "Other")
	conn, status, body := r.dial(cred, "")
	if conn == nil {
		t.Fatal(status, body)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, r.hub, AgentKeyID("bob"), "other")
	other, _ := r.svc.Store.FindAgentByName(r.ctx, "bob", "other")
	groups, _ := r.svc.Store.ListAgentGroups(r.ctx, r.agID)
	if err := r.svc.JoinGroup(r.ctx, "bob", groups[0], other.ID); err != nil {
		t.Fatal(err)
	}
	r.dispatch("plain")
	r14Assert(t, r.repo.count() == 1 && r.repo.last().To == sessionAddress("opus50ts", AgentKeyID("zzc")), "S003_OTHER_OWNER_TARGET")
}

func TestR14S004CurrentStateAndOffline(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("Beta")
	r.svc.Store.RetireAgent(r.ctx, r.agents["beta"], "zzc", r.now)
	r.dispatch("plain")
	r14Assert(t, r.repo.count() == 0, "S004_STALE_SOCKET_USED")
}
func TestR14S005ExplicitSwitchAndRouteFailure(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("opus50ts")
	before := r.state()
	r.repo.failRoute = true
	r.dispatch("#Beta business")
	after := r.state()
	r14Assert(t, after.AgentID == before.AgentID && after.Generation == before.Generation && after.ExpiresAt.Equal(before.ExpiresAt), "S005_REJECTED_SWITCH_COMMITTED", after)
	r.repo.failRoute = false
	r.dispatch("#Beta business2")
	r14Assert(t, r.repo.count() == 1 && r.state().AgentID == r.agents["beta"], "S005_SWITCH_NOT_ACCEPTED")
}
func TestR14S006ExplicitInvalidNoFallback(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("opus50ts")
	before := r.state()
	res := r.dispatch("#Missing body")
	r14Assert(t, r.repo.count() == 0 && res.Reply != "" && r.state().Generation == before.Generation, "S006_FELL_BACK")
}
func TestR14S007TTLAndClockRollback(t *testing.T) {
	for _, tc := range []struct {
		label string
		delta time.Duration
		want  int
	}{{"inside", 30*time.Minute - time.Nanosecond, 1}, {"boundary", 30 * time.Minute, 0}, {"rollback", -time.Nanosecond, 0}} {
		t.Run(tc.label, func(t *testing.T) {
			r := newR14Env(t, "Beta")
			r.choose("Beta")
			r.now = r.now.Add(tc.delta)
			r.dispatch("plain")
			r14Assert(t, r.repo.count() == tc.want, "S007_TTL_BOUNDARY", tc, r.repo.count())
		})
	}
}
func TestR14S008ReplayAndAgentReplyCannotOverwrite(t *testing.T) {
	r := newR14Env(t, "Beta")
	msg := feishuInboundMessage("old-A", "#opus50ts")
	r.dispatchMsg(msg, "#opus50ts")
	r.choose("Beta")
	s := r.state()
	r.dispatchMsg(msg, "#opus50ts")
	r14Assert(t, r.state().AgentID == r.agents["beta"] && r.state().Generation == s.Generation, "S008_OLD_REPLAY_OVERWROTE")
}
func TestR14S009UnavailableDoesNotPickSoleOther(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("opus50ts")
	r.hub.mu.Lock()
	delete(r.hub.sessionClients[AgentKeyID("zzc")], "opus50ts")
	r.hub.mu.Unlock()
	res := r.dispatch("lost-business")
	r14Assert(t, r.repo.count() == 0 && strings.Contains(res.Reply, "不可用"), "S009_SILENT_OTHER_TARGET")
	r.choose("Beta")
	r14Assert(t, r.repo.count() == 0, "S009_CACHED_BODY_REPLAY")
}
func TestR14S010StateReadFailsClosed(t *testing.T) {
	r := newR14Env(t)
	r.repo.failRead = true
	res := r.dispatch("plain")
	r14Assert(t, r.repo.count() == 0 && strings.Contains(res.Reply, "暂不可确定"), "S010_DB_FAILURE_FALLBACK")
}
func TestR14S011CandidateOrdering(t *testing.T) {
	r := newR14Env(t, "Alpha", "Beta")
	r.choose("Beta")
	before := r.state()
	r.now = r.now.Add(30 * time.Minute)
	res := r.dispatch("expired")
	want := "请选择一个 Agent：\n#Beta.zzc\n#Alpha.zzc\n#Opus50TS.zzc"
	r14Assert(t, strings.HasSuffix(res.Reply, want) && r.repo.count() == 0 && r.state().Generation == before.Generation, "S011_CANDIDATE_ORDER_FORMAT", res.Reply)
}
func TestR14S012CandidatePasteNoRoute(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("Beta")
	s := r.state()
	res := r.dispatch("#Opus50TS.zzc\r\n#Beta.zzc")
	r14Assert(t, r.repo.count() == 0 && r.state().Generation == s.Generation && strings.Contains(res.Reply, "请选择一行"), "S012_PASTED_BLOCK_ROUTED", res)
}
func TestR14S013SingleAgentConvenience(t *testing.T) {
	r := newR14Env(t)
	r.dispatch("plain")
	r14Assert(t, r.repo.count() == 1 && r.state() != nil, "S013_SINGLE_AGENT_LOST")
}
func TestR14S014RegisteredOfflineDoesNotCount(t *testing.T) {
	r := newR14Env(t)
	r.enroll("registered-offline", "Offline")
	r.dispatch("plain")
	r14Assert(t, r.repo.count() == 1 && r.state().AgentID == r.agID, "S014_REGISTERED_COUNT_USED")
}
func TestR14S015RestartPreservesExpiry(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("Beta")
	expiry := r.state().ExpiresAt
	r.now = r.now.Add(30 * time.Minute)
	old := r.hub
	h := New(r.repo, ConsoleAuthDisabled())
	defer h.Close()
	h.Identity = r.svc
	h.NowFunc = func() time.Time { return r.now }
	old.mu.Lock()
	for key, clients := range old.sessionClients {
		h.sessionClients[key] = make(map[string]*sessionClient)
		for name, client := range clients {
			h.sessionClients[key][name] = client
		}
	}
	old.mu.Unlock()
	r.hub = h
	res := r.dispatch("after-restart")
	r14Assert(t, r.repo.count() == 0 && strings.Contains(res.Reply, "过期") && r.state().ExpiresAt.Equal(expiry), "S015_RESTART_EXTENDED")
}
func TestR14S016ConcurrentSameSourceOneDelivery(t *testing.T) {
	r := newR14Env(t, "Beta")
	msg := feishuInboundMessage("same-concurrent-source", "#Beta concurrent")
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := r.hub.dispatchSelector(r.ctx, msg, "#Beta concurrent")
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	r14Assert(t, r.repo.count() == 1 && r.state().Generation == 1, "S016_DUPLICATE_ROUTE", r.repo.count(), r.state())
}
func TestR14S017CommandsAndGroupBypass(t *testing.T) {
	r := newR14Env(t, "Beta")
	res, handled, err := r.hub.dispatchSelector(r.ctx, feishuInboundMessage("seed-S017", "#Beta"), "#Beta")
	if err != nil || !handled || !strings.Contains(res.Reply, "已选择") {
		t.Fatal("S017_FIXTURE", res, err)
	}
	s := r.state()
	r.dispatch("#在线")
	r.dispatch("#unsync opus50ts")
	msg := feishuInboundMessage("group", "group plain")
	msg.ChatType = model.ChatGroup
	r.dispatchMsg(msg, "group plain")
	r14Assert(t, r.repo.count() == 0 && r.state().Generation == s.Generation, "S017_COMMAND_GROUP_CONSUMED")
	msg = feishuInboundMessage("untrusted", "plain")
	msg.IngressProvenance = model.IngressUntrusted
	r.dispatchMsg(msg, "plain")
	r14Assert(t, r.repo.count() == 0, "S017_UNTRUSTED_ROUTED")
	svc := sessionServiceID(AgentKeyID("zzc"), "opus50ts")
	if err := r.repo.UpsertRegisteredService(r.ctx, model.RegisteredService{ID: svc, Name: "local-prefix", DeliveryType: "session", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.hub.BindAccount(r.ctx, AgentKeyID("zzc"), "devplatform:personal:ou_alice"); err != nil {
		t.Fatal(err)
	}
	if err := r.repo.InsertRoutingRule(r.ctx, model.RoutingRule{ID: "r14-existing-prefix", ServiceID: svc, MatchType: "prefix", MatchExpr: "pfx:", StripPrefix: true, Enabled: true, AccountScopeJSON: `["devplatform:personal:ou_alice"]`}); err != nil {
		t.Fatal(err)
	}
	r.dispatch("pfx: configured-route")
	r14Assert(t, r.repo.count() == 1 && r.repo.last().To == sessionAddress("opus50ts", AgentKeyID("zzc")) && r.state().AgentID == s.AgentID && r.state().Generation == s.Generation, "S017_CONFIGURED_PREFIX_SWALLOWED", r.repo.last(), r.state())
	other := feishuInboundMessage("other-channel", "other channel")
	other.SenderOpenID = ""
	other.BotChannelID = "sessionhelper"
	_, handled, err = r.hub.dispatchFeishuSticky(r.ctx, other, "other channel", false)
	r14Assert(t, err == nil && !handled && r.state().Generation == s.Generation, "S017_OTHER_CHANNEL_ENTERED_STICKY")
}

func TestR14S018TextCandidateIsolation(t *testing.T) {
	r := newR14Env(t, "Beta")
	res := r.dispatch("plain")
	r14Assert(t, strings.Contains(res.Reply, "\n#Beta.zzc\n#Opus50TS.zzc") && r.repo.count() == 0, "S018_CANDIDATE_NOT_PLAIN_TEXT")
	r.dispatch(res.Reply)
	r14Assert(t, r.repo.count() == 0, "S018_SYSTEM_REPLY_ENTERED_AGENT")
}
func TestR14S019CommitFailureVisibleAndBlocked(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.repo.failCommit = true
	res := r.dispatch("#Beta business")
	r14Assert(t, r.repo.count() == 1 && res.Reply == stickyUnconfirmed, "S019_HIDDEN_COMMIT_FAILURE", res)
	r.repo.failCommit = false
	r.dispatch("plain")
	r14Assert(t, r.repo.count() == 1, "S019_UNKNOWN_AUTO_REPLAY")
	r.now = r.now.Add(11 * time.Minute)
	r.dispatch("plain2")
	r14Assert(t, r.repo.count() == 1, "S019_UNKNOWN_EXPIRED_TO_DEFAULT")
}
func TestR14S020SameNameNewStableID(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("opus50ts")
	before := r.state()
	r.svc.Revoke(r.ctx, "zzc", r.agID)
	cred, id := r.enroll("replacement", "Opus50TS")
	r.online(cred, "opus50ts")
	r.dispatch("plain")
	r14Assert(t, id != r.agID && r.repo.count() == 0 && r.state().AgentID == before.AgentID && r.state().Generation == before.Generation, "S020_RECREATED_INHERITED")
}
func TestR14S021ExactBeforePrefix(t *testing.T) {
	r := newR14Env(t, "ts3dev55", "ts3dev5520")
	res := r.dispatch("#ts3dev55")
	r14Assert(t, strings.Contains(res.Reply, "已选择") && r.state() != nil, "S021_EXACT_NOT_PRIORITIZED", res)
	r14Assert(t, r.state().AgentID == r.agents["ts3dev55"], "S021_EXACT_NOT_PRIORITIZED")
	s := r.state()
	r.dispatch("#ts3dev body")
	r14Assert(t, r.repo.count() == 0 && r.state().Generation == s.Generation, "S021_AMBIGUOUS_PREFIX_GUESSED")
}
func TestR14S022SelectorPositionAndSelectOnly(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("Bet")
	r14Assert(t, r.state().AgentID == r.agents["beta"], "S022_UNIQUE_PREFIX_NOT_SELECTED")
	r.choose("Opus50TS.zzc")
	r.dispatch("body #Beta")
	r14Assert(t, r.repo.count() == 1 && r.repo.last().Body == "body #Beta" && r.state().AgentID == r.agID, "S022_MIDDLE_SELECTOR_SWITCHED")
	msg := feishuInboundMessage("missing-provider-id", "plain")
	msg.FeishuMsgID = ""
	before := r.repo.count()
	s := r.state()
	r.dispatchMsg(msg, "plain")
	r14Assert(t, r.repo.count() == before && r.state().Generation == s.Generation, "S022_NO_SOURCE_ID_STICKY")
}

func TestR14S020QueuedTargetIDCannotBeReused(t *testing.T) {
	r := newR14Env(t, "Beta")
	old, err := r.svc.Store.GetAgent(r.ctx, r.agID)
	if err != nil {
		t.Fatal(err)
	}
	env := model.Envelope{ID: "old-r14-queue", From: feishuAddress("ou_alice", AgentKeyID("zzc"), "devplatform"), To: sessionAddress("opus50ts", AgentKeyID("zzc")), Body: "queued-before-revoke", Meta: map[string]any{"r14_target_agent_id": old.ID, "r14_principal_lc": "zzc"}}
	env.Meta["bot_channel_id"] = "devplatform"
	env.Meta["chat_type"] = "personal"
	r.hub.annotateSourceContext(r.ctx, env.Meta, feishuInboundMessage("queued-old-target", "queued-before-revoke"))
	if _, err = r.svc.Revoke(r.ctx, "zzc", old.ID); err != nil {
		t.Fatal(err)
	}
	cred, id := r.enroll("queued-replacement", "Opus50TS")
	r.online(cred, "opus50ts")
	before := r.repo.count()
	err = r.hub.RouteEnvelope(r.ctx, env)
	r14Assert(t, err != nil && r.repo.count() == before && id != old.ID, "S020_QUEUED_NAME_REBOUND", err, r.repo.count())
}

func TestR14S022CrossOwnerFullNameUsesExistingAuthorization(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.choose("Beta")
	before := r.state()
	if err := r.svc.Store.CreateUser(r.ctx, identity.User{UserID: "lbl", ShortName: "lb", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	cred := r.enrollForOwner("lbl", "cross-full", "Foreign")
	conn, status, body := r.dial(cred, "")
	if conn == nil {
		t.Fatal(status, body)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, r.hub, AgentKeyID("lbl"), "foreign")
	target, err := r.svc.Store.FindAgentByName(r.ctx, "lbl", "foreign")
	if err != nil {
		t.Fatal(err)
	}
	res := r.dispatch("#Foreign.lb forbidden")
	r14Assert(t, res.Reply == groupAgentNotFoundText && r.state().Generation == before.Generation, "S022_CROSS_OWNER_AUTH_SKIPPED", res)
	groups, err := r.svc.Store.ListAgentGroups(r.ctx, r.agID)
	if err != nil || len(groups) == 0 {
		t.Fatal(groups, err)
	}
	if err = r.svc.JoinGroup(r.ctx, "lbl", groups[0], target.ID); err != nil {
		t.Fatal(err)
	}
	res = r.dispatch("#Foreign.lb permitted")
	got := r.readEnvUntil(conn, time.Second, func(e model.Envelope) bool { return e.Body == "permitted" })
	r14Assert(t, res.Reply == "" && got != nil && r.state().AgentID == before.AgentID && r.state().Generation == before.Generation, "S022_CROSS_OWNER_NOT_EXPLICIT_OR_CHANGED_DEFAULT", res, got)
}

type stickyIdentityFailure struct{ identity.Store }

func (s stickyIdentityFailure) ResolveIdentity(context.Context, identity.Identity) (*identity.User, error) {
	return nil, errors.New("injected identity lookup failure")
}
func TestR14S002IdentityLookupError(t *testing.T) {
	r := newR14Env(t)
	r.choose("opus50ts")
	s := r.state()
	r.svc.Store = stickyIdentityFailure{r.svc.Store}
	res := r.dispatch("plain")
	r14Assert(t, r.repo.count() == 0 && res.Reply != "" && r.state().Generation == s.Generation, "S002_IDENTITY_ERROR_FALLBACK")
}

func TestR14CleanupRevocationAndBoundedCursor(t *testing.T) {
	r := newR14Env(t)
	now := r.now
	// More than one page of valid keys must not starve a later revoked key.
	for i := 0; i < store.FeishuStickyCleanupBatch+1; i++ {
		key := store.FeishuStickyKey{"zzc", fmt.Sprintf("bot-%03d", i)}
		o, _, err := r.repo.BeginFeishuSticky(r.ctx, key, "seed", "select-only", now)
		if err != nil {
			t.Fatal(err)
		}
		target := r.agID
		if i == store.FeishuStickyCleanupBatch {
			target = "missing-revoked-agent"
		}
		if err = r.repo.FinishFeishuSticky(r.ctx, o, "accepted", target, "seed", now); err != nil {
			t.Fatal(err)
		}
	}
	r.hub.cleanupFeishuSticky(r.ctx, now)
	key := store.FeishuStickyKey{"zzc", fmt.Sprintf("bot-%03d", store.FeishuStickyCleanupBatch)}
	first, err := r.repo.GetFeishuSticky(r.ctx, key)
	if err != nil || first == nil {
		t.Fatal("CLEANUP_SCAN_NOT_BOUNDED", err)
	}
	r.hub.cleanupFeishuSticky(r.ctx, now)
	second, err := r.repo.GetFeishuSticky(r.ctx, key)
	if err != nil || second != nil {
		t.Fatal("CLEANUP_CURSOR_STARVED_REVOKED", second, err)
	}
}

func TestR14PendingMaintenanceJoinsLifetime(t *testing.T) {
	r := newR14Env(t)
	key := store.FeishuStickyKey{"zzc", "devplatform"}
	o, _, err := r.repo.BeginFeishuSticky(r.ctx, key, "orphan", "explicit", r.now)
	if err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(11 * time.Minute)
	ctx, cancel := context.WithCancel(r.ctx)
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { defer close(done); r.hub.runFeishuStickyMaintenance(ctx, ticks) }()
	ticks <- r.now
	waitFor(t, time.Second, "R14 pending maintenance", func() bool {
		v, _, err := r.repo.BeginFeishuSticky(r.ctx, key, o.SourceMessageID, "explicit", r.now)
		return err == nil && v.Status == "unknown"
	})
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("R14_PENDING_WORKER_NOT_JOINED")
	}
}

func TestR14EnqueueBeforeCheckpointHasReconciliationEvidence(t *testing.T) {
	r := newR14Env(t, "Beta")
	k := store.FeishuStickyKey{"zzc", "devplatform"}
	op, fresh, err := r.repo.BeginFeishuSticky(r.ctx, k, "crash-window", "explicit", r.now)
	if err != nil || !fresh {
		t.Fatal(err)
	}
	op.SourceEnvelopeID = "source-crash-window"
	if err = r.repo.RecordFeishuStickyIntent(r.ctx, op, r.agents["beta"], "explicit", ""); err != nil {
		t.Fatal(err)
	}
	env := model.Envelope{ID: op.SourceEnvelopeID, To: sessionAddress("beta", AgentKeyID("zzc")), From: feishuAddress("ou_alice", AgentKeyID("zzc"), "devplatform"), Body: "local crash-window fixture", Meta: map[string]any{"r14_target_agent_id": r.agents["beta"], "r14_principal_lc": "zzc"}}
	env.Meta["bot_channel_id"] = "devplatform"
	env.Meta["chat_type"] = "personal"
	r.hub.annotateSourceContext(r.ctx, env.Meta, feishuInboundMessage("source-crash-window", "local crash-window fixture"))
	if err = r.hub.RouteEnvelope(r.ctx, env); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss before persisting the returned delivery ID.
	recovered, fresh, err := r.repo.BeginFeishuSticky(r.ctx, k, "crash-window", "explicit", r.now)
	if err != nil || fresh || recovered.DeliveryID != "" {
		t.Fatal(recovered, err)
	}
	id, err := r.repo.FindFeishuStickyDelivery(r.ctx, recovered)
	if err != nil || id == "" {
		t.Fatal("ENQUEUE_CHECKPOINT_EVIDENCE_LOST", id, err)
	}
	d, err := r.repo.GetSessionDelivery(r.ctx, id)
	if err != nil || !stickyDeliveryEvidence(recovered, d) {
		t.Fatal("ENQUEUE_CHECKPOINT_EVIDENCE_MISMATCH", d, err)
	}
}

func TestR14S019DirectUnknownNeverBlindlyReplays(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.hub.mu.Lock()
	client := r.hub.sessionClients[AgentKeyID("zzc")]["beta"]
	r.hub.mu.Unlock()
	client.stateMu.Lock()
	client.deliveryACKV1 = false
	client.stateMu.Unlock()
	r.repo.failCommit = true
	res := r.dispatch("#Beta direct-once")
	got := r.readEnvUntil(r.peers["beta"], time.Second, func(e model.Envelope) bool { return e.Body == "direct-once" })
	r14Assert(t, got != nil && res.Reply == stickyUnconfirmed, "S019_DIRECT_FAILURE_HIDDEN", got, res)
	r.repo.failCommit = false
	res = r.dispatch("no-prefix")
	r14Assert(t, res.Reply == stickyUnconfirmed && r.state() == nil, "S019_DIRECT_UNKNOWN_DEFAULTED", res)
	r.now = r.now.Add(11 * time.Minute)
	res = r.dispatch("still-unknown")
	r14Assert(t, res.Reply == stickyUnconfirmed && r.state() == nil, "S019_DIRECT_UNKNOWN_EXPIRED", res)
	// Explicit selection is still available, but cannot pretend to reconcile the
	// uncertain direct write; implicit routing stays paused until real evidence.
	r.choose("opus50ts")
	res = r.dispatch("still-explicit-only")
	r14Assert(t, res.Reply == stickyUnconfirmed, "S019_EXPLICIT_CLEARED_UNRECONCILED")
}

func TestR14LateReconciliationPreservesNewerBinding(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.repo.failCommit = true
	msg := feishuInboundMessage("late-A", "#opus50ts old-A")
	res := r.dispatchMsg(msg, "#opus50ts old-A")
	r14Assert(t, res.Reply == stickyUnconfirmed && r.repo.count() == 1, "LATE_RECONCILE_FIXTURE_A", res)
	r.repo.failCommit = false
	r.repo.mu.Lock()
	r.repo.hideEvidence = true
	r.repo.mu.Unlock()
	r.now = r.now.Add(11 * time.Minute)
	r.choose("Beta")
	selected := r.state()
	before := r.repo.count()
	r.repo.mu.Lock()
	r.repo.hideEvidence = false
	r.repo.mu.Unlock()
	r.now = r.now.Add(time.Minute)
	// A pure candidate paste reconciles but does not renew B as a business action.
	r.dispatch("#Opus50TS.zzc\n#Beta.zzc")
	after := r.state()
	op, fresh, err := r.repo.BeginFeishuSticky(r.ctx, store.FeishuStickyKey{"zzc", "devplatform"}, "late-A", "explicit", r.now)
	r14Assert(t, err == nil && !fresh && op.Status == "resolved" && op.DeliveryID != "" && op.CompletedAt.Equal(r.now), "LATE_RECONCILE_NOT_CLOSED", op, err)
	r14Assert(t, after.AgentID == selected.AgentID && after.Generation == selected.Generation && after.SelectedAt.Equal(selected.SelectedAt) && after.ExpiresAt.Equal(selected.ExpiresAt) && r.repo.count() == before, "LATE_RECONCILE_OVERWROTE_BINDING", selected, after)
}

type stickyReplyQueue chan model.Message

func (q stickyReplyQueue) Enqueue(ctx context.Context, m model.Message) error {
	select {
	case q <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestR14S008ActualAgentLateReplyDoesNotRenew(t *testing.T) {
	r := newR14Env(t, "Beta")
	r.dispatch("#opus50ts original-A")
	r.now = r.now.Add(time.Minute)
	r.choose("Beta")
	before := r.state()
	q := make(stickyReplyQueue, 2)
	r.hub.Outbound = q
	if err := r.repo.UpsertBotChannel(r.ctx, model.BotChannel{ID: "devplatform", Name: "devplatform", CanSend: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	r.now = r.now.Add(time.Minute)
	r.sendEnv(r.peers["opus50ts"], model.Envelope{ID: "late-agent-A", From: sessionAddress("opus50ts", AgentKeyID("zzc")), To: feishuAddress("ou_alice", AgentKeyID("zzc"), "devplatform"), Body: "late Agent A reply in local fixture"})
	waitFor(t, time.Second, "local fake Feishu outbound", func() bool { return len(q) > 0 })
	after := r.state()
	r14Assert(t, after.AgentID == before.AgentID && after.Generation == before.Generation && after.SelectedAt.Equal(before.SelectedAt) && after.ExpiresAt.Equal(before.ExpiresAt), "S008_AGENT_REPLY_RENEWED_OR_OVERWROTE", before, after)
}
