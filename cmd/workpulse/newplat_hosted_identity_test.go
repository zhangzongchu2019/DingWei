package main

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 仿 G4 的 internal/hosted 两个类型与两个接口（kimi30kqtuxerh/g4-m0-m1 d2f8cbc6 orchestrator.go／lifecycle.go，逐字照抄签名）。
// 本包不 import G4；真类型上的编译核对另在合入 G4 的一次性工作树里做（见交付读数）。
type g4EnrollToken struct {
	ApplicationID string
	PollToken     string
	ExpiresAt     time.Time
}

type g4IssuedAgent struct {
	ID       string
	FullName string
}

type g4IdentityIssuer interface {
	IssueHostedAgent(ctx context.Context, ownerLC, name, groupCode, by string) (g4IssuedAgent, g4EnrollToken, error)
	RetireHostedAgent(ctx context.Context, agentID, by string) (bool, error)
	ListHostedAgentIDsByOwner(ctx context.Context, ownerLC string) ([]string, error)
}

type g4IdentitySuspender interface {
	SuspendHostedAgent(ctx context.Context, agentID, by string) (bool, error)
	ResumeHostedAgent(ctx context.Context, agentID, by string) (bool, error)
	ReissueHostedToken(ctx context.Context, agentID, by string) (g4EnrollToken, error)
}

var (
	errG4GroupNotFound = errors.New("group_not_found")
	errG4GroupClosed   = errors.New("group_closed")
)

// 同一个适配器值同时满足两个接口（G4 装配时 Orchestrator 与 Lifecycle 各拿一个）。
var (
	_ g4IdentityIssuer    = newHostedIdentity[g4EnrollToken, g4IssuedAgent](nil, nil, nil)
	_ g4IdentitySuspender = newHostedIdentity[g4EnrollToken, g4IssuedAgent](nil, nil, nil)
)

func hostedIdentityFixture(t *testing.T) (*identity.Service, identity.Store, *hostedIdentity[g4EnrollToken, g4IssuedAgent]) {
	t.Helper()
	st := identity.NewMemStore()
	t0 := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)
	svc := identity.NewService(st, func() time.Time { return t0 }, nil) // 定时钟：令牌到期时刻可精确比对
	ctx := context.Background()
	for _, u := range []struct{ id, short string }{{"Own", "ow"}, {"Other", "ot"}} {
		if err := st.CreateUser(ctx, identity.User{UserID: u.id, ShortName: u.short, Type: identity.UserMember, State: identity.UserActive, CreatedAt: svc.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	return svc, st, newHostedIdentity[g4EnrollToken, g4IssuedAgent](svc, errG4GroupNotFound, errG4GroupClosed)
}

func TestHostedIdentityIssue(t *testing.T) {
	ctx := context.Background()
	svc, st, idp := hostedIdentityFixture(t)

	ag, tok, err := idp.IssueHostedAgent(ctx, "Own", "Walker", "", "g4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ag.ID, "ag_") || ag.FullName != "Walker.ow" {
		t.Errorf("issued %+v, want ag_… and Walker.ow", ag)
	}
	got, err := st.GetAgent(ctx, ag.ID)
	if err != nil || got.Origin != identity.OriginHosted || got.OwnerLC != "own" || got.GroupID != "" {
		t.Fatalf("stored agent %+v %v, want hosted, owner own, no group", got, err)
	}
	// 令牌原样转交：凭它走与自备同一条轮询路取凭据（不是只看字段非空）。
	if tok.ApplicationID == "" || tok.PollToken == "" || !tok.ExpiresAt.Equal(svc.Now().Add(identity.HostedTokenTTL)) {
		t.Fatalf("token %+v", tok)
	}
	if st, err := svc.Poll(ctx, tok.ApplicationID, tok.PollToken); err != nil || st.State != identity.AppApproved || st.Credential == "" {
		t.Fatalf("poll with issued token: %+v %v", st, err)
	}

	// 编组两码译成 G4 的哨兵本身（G4 用 errors.Is 认，页面只认机器码）。
	if _, err := svc.IssueHostedAgentWithNewGroup(ctx, "Own", "Mine", "mine", "g4"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IssueHostedAgentWithNewGroup(ctx, "Other", "Theirs", "theirs", "g4"); err != nil {
		t.Fatal(err)
	}
	codeOf := func(name string) string {
		groups, err := st.ListGroups(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.ShortName == name {
				return g.Code
			}
		}
		t.Fatalf("group %s not found", name)
		return ""
	}
	for _, c := range []struct {
		name, code string
		want       error
	}{
		{"NoSuch", "zzz", errG4GroupNotFound},
		{"Foreign", codeOf("theirs"), errG4GroupNotFound}, // 别人的组 ＝ 不存在（本人可用，41feec50）
	} {
		if _, _, err := idp.IssueHostedAgent(ctx, "Own", c.name, c.code, "g4"); err != c.want {
			t.Errorf("%s: err %v, want the G4 sentinel %v itself", c.name, err, c.want)
		}
	}
	if err := st.SetGroupAdditionsClosed(ctx, codeOf("mine"), "own", true, svc.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idp.IssueHostedAgent(ctx, "Own", "Closed", codeOf("mine"), "g4"); err != errG4GroupClosed {
		t.Errorf("closed own group: err %v, want %v", err, errG4GroupClosed)
	}
	// 名称两码译成契约机器码（联调第 3 条：错误字段不许是 Go 文本；kimi30kqtuxerh 修订，
	// 已报 opus50tc2k——「其余原样透传」口径因此收窄为「编组两码＋名称两码之外原样透传」）。
	if _, _, err := idp.IssueHostedAgent(ctx, "Own", "walker", "", "g4"); err == nil || err.Error() != "name_taken" {
		t.Errorf("name taken: %v, want 契约机器码 name_taken", err)
	}
	if _, _, err := idp.IssueHostedAgent(ctx, "Nobody", "Ghost", "", "g4"); !errors.Is(err, identity.ErrUnknownOwner) {
		t.Errorf("unknown owner: %v, want identity.ErrUnknownOwner passed through", err)
	}
}

// 配额「已用」：名下未吊销的托管 Agent，停用中的也算；自备、已销毁、别人的都不算。
func TestHostedIdentityListByOwner(t *testing.T) {
	ctx := context.Background()
	svc, st, idp := hostedIdentityFixture(t)
	issue := func(owner, name string) string {
		ag, _, err := idp.IssueHostedAgent(ctx, owner, name, "", "g4")
		if err != nil {
			t.Fatal(err)
		}
		return ag.ID
	}
	running, paused, gone := issue("Own", "Run"), issue("Own", "Pause"), issue("Own", "Gone")
	_ = issue("Other", "Foreign")
	if _, err := idp.SuspendHostedAgent(ctx, paused, "g4"); err != nil {
		t.Fatal(err)
	}
	if _, err := idp.RetireHostedAgent(ctx, gone, "g4"); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateApplication(ctx, identity.Application{ID: "app-self", OwnerLC: "own", DeviceFingerprint: "dev", RequestedAt: svc.Now(), ExpiresAt: svc.Now().Add(time.Hour), State: identity.AppPending}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Approve(ctx, identity.ApproveInput{AppID: "app-self", OwnerLC: "own", AgentID: "ag-self", Name: "Selfie"}, svc.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := idp.ListHostedAgentIDsByOwner(ctx, "OWN")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{running, paused}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hosted ids %v, want %v (running＋suspended only)", got, want)
	}
	if got, err := idp.ListHostedAgentIDsByOwner(ctx, "nobody"); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no agents: %#v %v, want empty non-nil slice", got, err)
	}
}

// 停用／启用／重签／销毁：转到 identity.Service，already 语义原样。
func TestHostedIdentityLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, st, idp := hostedIdentityFixture(t)
	ag, first, err := idp.IssueHostedAgent(ctx, "Own", "Life", "", "g4")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		name        string
		call        func() (bool, error)
		wantAlready bool
		wantState   identity.AgentState
	}{
		{"suspend", func() (bool, error) { return idp.SuspendHostedAgent(ctx, ag.ID, "g4") }, false, identity.AgentSuspended},
		{"suspend again", func() (bool, error) { return idp.SuspendHostedAgent(ctx, ag.ID, "g4") }, true, identity.AgentSuspended},
		{"resume", func() (bool, error) { return idp.ResumeHostedAgent(ctx, ag.ID, "g4") }, false, identity.AgentActive},
		{"resume again", func() (bool, error) { return idp.ResumeHostedAgent(ctx, ag.ID, "g4") }, true, identity.AgentActive},
	} {
		already, err := step.call()
		got, gerr := st.GetAgent(ctx, ag.ID)
		if err != nil || gerr != nil || already != step.wantAlready || got.State != step.wantState {
			t.Fatalf("%s: already=%v err=%v state=%v (%v), want already=%v state=%v", step.name, already, err, got.State, gerr, step.wantAlready, step.wantState)
		}
	}
	tok, err := idp.ReissueHostedToken(ctx, ag.ID, "g4")
	if err != nil || tok.ApplicationID == "" || tok.ApplicationID == first.ApplicationID || tok.PollToken == first.PollToken {
		t.Fatalf("reissue: %+v %v, want a fresh token", tok, err)
	}
	if st, err := svc.Poll(ctx, tok.ApplicationID, tok.PollToken); err != nil || st.Credential == "" {
		t.Fatalf("poll with reissued token: %+v %v", st, err)
	}
	if already, err := idp.RetireHostedAgent(ctx, ag.ID, "g4"); err != nil || already {
		t.Fatalf("retire: already=%v err=%v", already, err)
	}
	if already, err := idp.RetireHostedAgent(ctx, ag.ID, "g4"); err != nil || !already {
		t.Fatalf("retire again: already=%v err=%v, want already", already, err)
	}
	if _, err := idp.ReissueHostedToken(ctx, "ag_nosuch", "g4"); err == nil {
		t.Errorf("reissue for unknown agent must fail")
	}
}
