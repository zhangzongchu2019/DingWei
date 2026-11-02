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

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type stickyFaultRepo struct {
	*store.SQLite
	mu                              sync.Mutex
	sent                            []model.SessionDelivery
	failRead, failRoute, failCommit bool
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
}

func newR14Env(t *testing.T, names ...string) *r14Env {
	t.Helper()
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	r := &r14Env{agentEnv: e, repo: &stickyFaultRepo{SQLite: e.hub.Repo.(*store.SQLite)}, now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), agents: map[string]string{"opus50ts": e.agID}}
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
	r.t.Cleanup(func() { c.CloseNow() })
	waitSessionOnline(r.t, r.hub, AgentKeyID("zzc"), name)
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
	r.svc.Store.CreateUser(r.ctx, identity.User{UserID: "bob", ShortName: "bob", State: identity.UserActive})
	r.hub.mu.Lock()
	r.hub.sessionClients[AgentKeyID("bob")] = map[string]*sessionClient{"other": {metricsAgentID: "foreign"}}
	r.hub.mu.Unlock()
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
	r.choose("Beta")
	s := r.state()
	r.dispatch("#在线")
	msg := feishuInboundMessage("group", "group plain")
	msg.ChatType = model.ChatGroup
	r.dispatchMsg(msg, "group plain")
	r14Assert(t, r.repo.count() == 0 && r.state().Generation == s.Generation, "S017_COMMAND_GROUP_CONSUMED")
	msg = feishuInboundMessage("untrusted", "plain")
	msg.IngressProvenance = model.IngressUntrusted
	r.dispatchMsg(msg, "plain")
	r14Assert(t, r.repo.count() == 0, "S017_UNTRUSTED_ROUTED")
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
	r.svc.Revoke(r.ctx, "zzc", r.agID)
	cred, id := r.enroll("replacement", "Opus50TS")
	r.online(cred, "opus50ts")
	r.dispatch("plain")
	r14Assert(t, id != r.agID && r.repo.count() == 0, "S020_RECREATED_INHERITED")
}
func TestR14S021ExactBeforePrefix(t *testing.T) {
	r := newR14Env(t, "ts3dev55", "ts3dev5520")
	r.choose("ts3dev55")
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
