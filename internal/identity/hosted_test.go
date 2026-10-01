package identity_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 托管全链路：签发 → 容器用一次性令牌经现有轮询接口取凭据 → 凭据认人（V6）。
func TestHostedIssueThenContainerTakesCredential(t *testing.T) {
	f := newFixture(t)
	hi, err := f.svc.IssueHostedAgentWithNewGroup(bg, "ZZC", "Codex1", tg(), "zzc")
	if err != nil {
		t.Fatal(err)
	}
	if hi.Agent.Origin != identity.OriginHosted || hi.FullName != "Codex1.zzc" || len(hi.Token.PollToken) != 64 ||
		!hi.Token.ExpiresAt.Equal(f.clk.T.Add(identity.HostedTokenTTL)) {
		t.Fatalf("issue: %+v", hi)
	}
	p, err := f.svc.Poll(bg, hi.Token.ApplicationID, hi.Token.PollToken)
	if err != nil || p.State != identity.AppApproved || !strings.HasPrefix(p.Credential, identity.CredentialPrefix) || p.AgentFullName != "Codex1.zzc" {
		t.Fatalf("container poll: %+v %v", p, err)
	}
	ag, _, err := f.svc.AuthenticateAgent(bg, p.Credential)
	if err != nil || ag.ID != hi.Agent.ID || ag.Origin != identity.OriginHosted {
		t.Fatalf("authenticate: %+v %v", ag, err)
	}
	f.clk.Advance(identity.PollMinInterval)
	if p2, _ := f.svc.Poll(bg, hi.Token.ApplicationID, hi.Token.PollToken); p2.Credential != "" {
		t.Fatal("one-time token yielded a second credential")
	}
	for _, e := range f.store.AuditLog() {
		if strings.Contains(e.Detail+e.Target+e.Actor, hi.Token.PollToken) || strings.Contains(e.Detail+e.Target+e.Actor, p.Credential) {
			t.Fatalf("audit %s leaks a secret", e.Action)
		}
	}
}

func TestHostedIssueValidation(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		owner, name, group string
		want               error
	}{
		{"zzc", "bad.name", "g", identity.ErrNameInvalid},
		{"nosuch", "Codex1", "g", identity.ErrUnknownOwner},
		{"newbie", "Codex1", "g", identity.ErrUnknownOwner},  // 未绑定的用户不能拥有托管席位
		{"zzc", "Codex1", "nope", identity.ErrGroupNotFound}, // GroupAllowed：已有组须存在
	}
	for _, c := range cases {
		if _, err := f.svc.IssueHostedAgent(bg, c.owner, c.name, c.group, "admin"); !errors.Is(err, c.want) {
			t.Errorf("%+v: err=%v", c, err)
		}
	}
	// 编组不是必须的（业主令 15:34:17Z）：不给组码即不入组
	hi, err := f.svc.IssueHostedAgent(bg, "zzc", "Loner", "", "admin")
	if err != nil || hi.Agent.GroupID != "" {
		t.Fatalf("hosted without group: %+v %v", hi, err)
	}
	if codes, _ := f.store.ListAgentGroups(bg, hi.Agent.ID); len(codes) != 0 {
		t.Fatalf("hosted without group joined %v", codes)
	}
	if _, err := f.svc.IssueHostedAgentWithNewGroup(bg, "zzc", "Codex1", tg(), "zzc"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.IssueHostedAgentWithNewGroup(bg, "zzc", "CODEX1", tg(), "zzc"); !errors.Is(err, identity.ErrNameTaken) {
		t.Fatalf("duplicate hosted name: %v", err)
	}
}

// 容器重建／升级（V8）：重签 ⇒ 新凭据取走前旧凭据仍有效；取走后旧凭据作废、新凭据认得同一个席位。
func TestHostedReissueRotation(t *testing.T) {
	f := newFixture(t)
	hi, _ := f.svc.IssueHostedAgentWithNewGroup(bg, "zzc", "Codex1", tg(), "zzc")
	old, _ := f.svc.Poll(bg, hi.Token.ApplicationID, hi.Token.PollToken)
	tok, err := f.svc.ReissueHostedToken(bg, hi.Agent.ID, "g4")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.AuthenticateAgent(bg, old.Credential); err != nil {
		t.Fatalf("old credential before pickup: %v", err)
	}
	nw, err := f.svc.Poll(bg, tok.ApplicationID, tok.PollToken)
	if err != nil || nw.Credential == "" || nw.Credential == old.Credential {
		t.Fatalf("reissued poll: %+v %v", nw, err)
	}
	if _, _, err := f.svc.AuthenticateAgent(bg, old.Credential); !errors.Is(err, identity.ErrCredentialInvalid) {
		t.Fatalf("old credential after rotation: %v", err)
	}
	if ag, _, err := f.svc.AuthenticateAgent(bg, nw.Credential); err != nil || ag.ID != hi.Agent.ID {
		t.Fatalf("new credential: %+v %v", ag, err)
	}
	// 令牌过期后取不到
	tok2, _ := f.svc.ReissueHostedToken(bg, hi.Agent.ID, "g4")
	f.clk.Advance(identity.HostedTokenTTL)
	if p, _ := f.svc.Poll(bg, tok2.ApplicationID, tok2.PollToken); p.Credential != "" {
		t.Fatal("expired hosted token still yields a credential")
	}
}

// 销毁（V8 D5）：吊销、断开钩子只触发一次、console 吊销托管仍被拒、自备席位不可走此路。
func TestHostedRetire(t *testing.T) {
	f := newFixture(t)
	hi, _ := f.svc.IssueHostedAgentWithNewGroup(bg, "zzc", "Codex1", tg(), "zzc")
	p, _ := f.svc.Poll(bg, hi.Token.ApplicationID, hi.Token.PollToken)
	if _, err := f.svc.Revoke(bg, "zzc", hi.Agent.ID); !errors.Is(err, identity.ErrHosted) {
		t.Fatalf("console revoke of hosted: %v", err)
	}
	if already, err := f.svc.RetireHostedAgent(bg, hi.Agent.ID, "g4"); already || err != nil || len(f.revoked) != 1 || f.revoked[0] != hi.Agent.ID {
		t.Fatalf("retire: %v %v hook=%v", already, err, f.revoked)
	}
	if already, _ := f.svc.RetireHostedAgent(bg, hi.Agent.ID, "g4"); !already || len(f.revoked) != 1 {
		t.Fatalf("second retire: already=%v hook=%v", already, f.revoked)
	}
	if _, _, err := f.svc.AuthenticateAgent(bg, p.Credential); !errors.Is(err, identity.ErrCredentialRevoked) {
		t.Fatalf("credential after retire: %v", err)
	}
	if _, err := f.svc.ReissueHostedToken(bg, hi.Agent.ID, "g4"); !errors.Is(err, identity.ErrNotPending) {
		t.Fatalf("reissue after retire: %v", err)
	}
	r := f.apply(t, "zzc", "dev-9", "1.1.1.1")
	self, _, _ := f.svc.ApproveWithNewGroup(bg, "zzc", r.ApplicationID, code, "Selfie", tg())
	if _, err := f.svc.RetireHostedAgent(bg, self.ID, "g4"); !errors.Is(err, identity.ErrNotHosted) {
		t.Fatalf("retire selfhost via hosted path: %v", err)
	}
	if _, err := f.svc.ReissueHostedToken(bg, self.ID, "g4"); !errors.Is(err, identity.ErrNotHosted) {
		t.Fatalf("reissue selfhost: %v", err)
	}
	_ = time.Second
}

// 组长裁定：停用期间凭据拒连（credential_suspended），启用后原凭据照常可用；断开钩子只在真正暂停时触发一次。
func TestHostedSuspendResume(t *testing.T) {
	f := newFixture(t)
	var suspended []string
	f.svc.OnSuspended = func(id string) { suspended = append(suspended, id) }
	hi, _ := f.svc.IssueHostedAgentWithNewGroup(bg, "zzc", "Codex1", tg(), "zzc")
	p, _ := f.svc.Poll(bg, hi.Token.ApplicationID, hi.Token.PollToken)
	if already, err := f.svc.SuspendHostedAgent(bg, hi.Agent.ID, "g4"); already || err != nil || len(suspended) != 1 {
		t.Fatalf("suspend: %v %v hook=%v", already, err, suspended)
	}
	if already, _ := f.svc.SuspendHostedAgent(bg, hi.Agent.ID, "g4"); !already || len(suspended) != 1 {
		t.Fatalf("second suspend: already=%v hook=%v", already, suspended)
	}
	if _, _, err := f.svc.AuthenticateAgent(bg, p.Credential); !errors.Is(err, identity.ErrCredentialSuspended) {
		t.Fatalf("auth while suspended: %v, want ErrCredentialSuspended", err)
	}
	if len(f.revoked) != 0 {
		t.Fatalf("suspend must not fire the revoke hook: %v", f.revoked)
	}
	if already, err := f.svc.ResumeHostedAgent(bg, hi.Agent.ID, "g4"); already || err != nil {
		t.Fatalf("resume: %v %v", already, err)
	}
	if ag, _, err := f.svc.AuthenticateAgent(bg, p.Credential); err != nil || ag.ID != hi.Agent.ID {
		t.Fatalf("auth after resume (same credential, no reissue): %+v %v", ag, err)
	}
	n := map[string]int{}
	for _, e := range f.store.AuditLog() {
		n[e.Action]++
	}
	if n["agent.hosted_suspend"] != 1 || n["agent.hosted_resume"] != 1 {
		t.Fatalf("audits: %v", n)
	}
}
