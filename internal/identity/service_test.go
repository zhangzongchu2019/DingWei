package identity_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

var bg = context.Background()

const (
	saltHex = "000102030405060708090a0b0c0d0e0f"
	code    = "123456"
	// sha256(盐字节 ＋ "123456")，由 Python 独立算出；与客户端 dbfc9f14 的算法一致。
	// 若有人改成「盐的 hex 串 ＋ 码」，结果会是 de2924652cf0…，本向量即红。
	codeDigest = "99c90773ea9ce66a355bd6a2a6e55122c3caf5a164785c90b1d58c2d63bb8680"
)

type fixture struct {
	clk     *clock.Fake
	store   *identity.MemStore
	svc     *identity.Service
	revoked []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{clk: &clock.Fake{T: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}, store: identity.NewMemStore()}
	f.svc = identity.NewService(f.store, f.clk.Now, nil)
	f.svc.OnRevoked = func(id string) { f.revoked = append(f.revoked, id) }
	for _, u := range []identity.User{
		{UserID: "zzc", ShortName: "zzc", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "lbl", ShortName: "lb", Type: identity.UserMember, State: identity.UserActive},
		{UserID: "newbie", ShortName: "nb", Type: identity.UserMember, State: identity.UserPendingBind},
	} {
		if err := f.store.CreateUser(bg, u); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *fixture) apply(t *testing.T, owner, device, ip string) *identity.ApplyResult {
	t.Helper()
	r, err := f.svc.Apply(bg, identity.ApplyRequest{OwnerUserID: owner, PairingSalt: saltHex, PairingDigest: codeDigest,
		DeviceFingerprint: device, ClientVersion: "0.3.0", SourceIP: ip})
	if err != nil {
		t.Fatalf("Apply(%s,%s): %v", owner, device, err)
	}
	return r
}

func TestPairingDigestVector(t *testing.T) {
	got, err := identity.PairingDigest(saltHex, code)
	if err != nil || got != codeDigest {
		t.Fatalf("PairingDigest=%s err=%v, want %s", got, err, codeDigest)
	}
}

func TestApplyValidationAndOwner(t *testing.T) {
	f := newFixture(t)
	good := identity.ApplyRequest{OwnerUserID: "zzc", PairingSalt: saltHex, PairingDigest: codeDigest, DeviceFingerprint: "d", ClientVersion: "v", SourceIP: "1.1.1.1"}
	bad := map[string]func(r *identity.ApplyRequest){
		"owner with dot":    func(r *identity.ApplyRequest) { r.OwnerUserID = "z.zc" },
		"owner too short":   func(r *identity.ApplyRequest) { r.OwnerUserID = "z" },
		"salt not 16 bytes": func(r *identity.ApplyRequest) { r.PairingSalt = "0001" },
		"digest not hex":    func(r *identity.ApplyRequest) { r.PairingDigest = strings.Repeat("z", 64) },
		"empty fingerprint": func(r *identity.ApplyRequest) { r.DeviceFingerprint = "" },
	}
	for name, mut := range bad {
		r := good
		mut(&r)
		if _, err := f.svc.Apply(bg, r); !errors.Is(err, identity.ErrBadRequest) {
			t.Errorf("%s: err=%v, want ErrBadRequest", name, err)
		}
	}
	for _, owner := range []string{"nosuch", "newbie"} { // 不存在；存在但未绑定（非 active）
		r := good
		r.OwnerUserID = owner
		if _, err := f.svc.Apply(bg, r); !errors.Is(err, identity.ErrUnknownOwner) {
			t.Errorf("owner %s: err=%v, want ErrUnknownOwner", owner, err)
		}
	}
	r := good
	r.OwnerUserID = "ZZC" // 大小写不同仍归到 zzc
	res, err := f.svc.Apply(bg, r)
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := f.svc.ListPending(bg, "zzc"); len(list) != 1 || list[0].ID != res.ApplicationID || list[0].SourceIP != "1.1.1.1" {
		t.Fatalf("pending for zzc = %+v", list)
	}
	if !res.ExpiresAt.Equal(f.clk.T.Add(24 * time.Hour)) {
		t.Fatalf("ExpiresAt=%s", res.ExpiresAt)
	}
}

func TestApplyRateLimitPerIP(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < identity.ApplyPerIPPerHour; i++ {
		f.apply(t, "zzc", "dev", "9.9.9.9")
		f.clk.Advance(time.Minute)
	}
	_, err := f.svc.Apply(bg, identity.ApplyRequest{OwnerUserID: "zzc", PairingSalt: saltHex, PairingDigest: codeDigest, DeviceFingerprint: "d", SourceIP: "9.9.9.9"})
	var rl *identity.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("11th apply from same IP: err=%v, want RateLimitedError", err)
	}
	f.apply(t, "zzc", "dev", "8.8.8.8") // 别的 IP 不受影响
	f.clk.Advance(rl.RetryAfter)
	f.apply(t, "zzc", "dev", "9.9.9.9") // 窗口滑过后恢复
}

func TestApplyPendingCapPerOwner(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < identity.MaxPendingPerOwner; i++ {
		f.apply(t, "zzc", "dev", "10.0.0."+string(rune('a'+i))) // 每条换 IP，只测 owner 上限
	}
	_, err := f.svc.Apply(bg, identity.ApplyRequest{OwnerUserID: "zzc", PairingSalt: saltHex, PairingDigest: codeDigest, DeviceFingerprint: "d", SourceIP: "10.1.1.1"})
	var rl *identity.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("21st pending: err=%v, want RateLimitedError", err)
	}
	f.apply(t, "lbl", "dev", "10.1.1.1") // 别的 owner 不受影响
}

func TestPollTokenAndRate(t *testing.T) {
	f := newFixture(t)
	r := f.apply(t, "zzc", "dev", "1.1.1.1")
	if _, err := f.svc.Poll(bg, r.ApplicationID, r.PollToken+"x"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("wrong token: %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Poll(bg, "nosuchapp", r.PollToken); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("unknown app: %v, want ErrNotFound", err)
	}
	p, err := f.svc.Poll(bg, r.ApplicationID, r.PollToken)
	if err != nil || p.State != identity.AppPending || p.Credential != "" {
		t.Fatalf("first poll: %+v %v", p, err)
	}
	var rl *identity.RateLimitedError
	if _, err := f.svc.Poll(bg, r.ApplicationID, r.PollToken); !errors.As(err, &rl) {
		t.Fatalf("immediate second poll: %v, want rate limited", err)
	}
	f.clk.Advance(identity.PollMinInterval)
	if _, err := f.svc.Poll(bg, r.ApplicationID, r.PollToken); err != nil {
		t.Fatalf("poll after interval: %v", err)
	}
	f.clk.Advance(24 * time.Hour)
	if p, _ := f.svc.Poll(bg, r.ApplicationID, r.PollToken); p.State != identity.AppExpired {
		t.Fatalf("after 24h state=%s, want expired (V1 A7)", p.State)
	}
}

func TestApproveFlowCredentialOnce(t *testing.T) {
	f := newFixture(t)
	r := f.apply(t, "zzc", "dev-1", "1.1.1.1")
	ag, full, err := f.svc.Approve(bg, "zzc", r.ApplicationID, code, "Opus50ts", "grp-1")
	if err != nil || ag.Name != "Opus50ts" || ag.GroupID != "grp-1" || full != "Opus50ts.zzc" {
		t.Fatalf("approve: %+v %q %v", ag, full, err)
	}
	f.clk.Advance(identity.PollMinInterval)
	p, err := f.svc.Poll(bg, r.ApplicationID, r.PollToken)
	if err != nil || p.State != identity.AppApproved || !strings.HasPrefix(p.Credential, identity.CredentialPrefix) ||
		len(p.Credential) != len(identity.CredentialPrefix)+64 || p.AgentFullName != "Opus50ts.zzc" {
		t.Fatalf("poll after approve: %+v %v", p, err)
	}
	got, full2, err := f.svc.AuthenticateAgent(bg, p.Credential)
	if err != nil || got.ID != ag.ID || full2 != "Opus50ts.zzc" {
		t.Fatalf("authenticate: %+v %q %v", got, full2, err)
	}
	f.clk.Advance(identity.PollMinInterval)
	p2, err := f.svc.Poll(bg, r.ApplicationID, r.PollToken)
	if err != nil || p2.State != identity.AppApproved || p2.Credential != "" {
		t.Fatalf("second poll must not return credential: %+v %v", p2, err)
	}
	// V1 A8 / N3 A5 的这一侧：审计里不含凭据、轮询令牌、配对码的明文。
	for _, e := range f.store.AuditLog() {
		blob := e.Actor + e.Action + e.Target + e.Detail
		for _, secret := range []string{p.Credential, r.PollToken, code} {
			if strings.Contains(blob, secret) {
				t.Fatalf("audit %q leaks a secret", e.Action)
			}
		}
	}
	if _, _, err := f.svc.AuthenticateAgent(bg, identity.CredentialPrefix+strings.Repeat("0", 64)); !errors.Is(err, identity.ErrCredentialInvalid) {
		t.Fatalf("unknown credential: %v, want ErrCredentialInvalid", err)
	}
}

func TestApprovePairingMismatchKeepsThenRejects(t *testing.T) {
	f := newFixture(t)
	r := f.apply(t, "zzc", "dev-1", "1.1.1.1")
	for i := 1; i < identity.MaxPairingFailures; i++ {
		_, _, err := f.svc.Approve(bg, "zzc", r.ApplicationID, "654321", "Name1", "g")
		var pm *identity.PairingMismatchError
		if !errors.As(err, &pm) || pm.Rejected || pm.Remaining != identity.MaxPairingFailures-i {
			t.Fatalf("wrong code #%d: %v", i, err)
		}
	}
	if list, _ := f.svc.ListPending(bg, "zzc"); len(list) != 1 {
		t.Fatalf("V1 A2: application must stay pending after wrong codes, pending=%d", len(list))
	}
	_, _, err := f.svc.Approve(bg, "zzc", r.ApplicationID, "000000", "Name1", "g")
	var pm *identity.PairingMismatchError
	if !errors.As(err, &pm) || !pm.Rejected {
		t.Fatalf("5th wrong code: %v, want rejected", err)
	}
	if _, _, err := f.svc.Approve(bg, "zzc", r.ApplicationID, code, "Name1", "g"); !errors.Is(err, identity.ErrNotPending) {
		t.Fatalf("correct code after auto-reject: %v, want ErrNotPending", err)
	}
	f.clk.Advance(identity.PollMinInterval)
	if p, _ := f.svc.Poll(bg, r.ApplicationID, r.PollToken); p.State != identity.AppRejected {
		t.Fatalf("client sees %s, want rejected", p.State)
	}
}

func TestApproveNameRulesAndOwnership(t *testing.T) {
	f := newFixture(t)
	r1 := f.apply(t, "zzc", "dev-1", "1.1.1.1")
	r2 := f.apply(t, "zzc", "dev-2", "1.1.1.1")
	for _, bad := range []string{"a", "opus.ts", "opus_ts", "名字", strings.Repeat("a", 33)} {
		if _, _, err := f.svc.Approve(bg, "zzc", r1.ApplicationID, code, bad, "g"); !errors.Is(err, identity.ErrNameInvalid) {
			t.Errorf("name %q: %v, want ErrNameInvalid", bad, err)
		}
	}
	if _, _, err := f.svc.Approve(bg, "lbl", r1.ApplicationID, code, "Thief", "g"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("other owner approves: %v, want ErrNotFound", err)
	}
	if _, _, err := f.svc.Approve(bg, "zzc", r1.ApplicationID, code, "opus50ts", "g"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Approve(bg, "zzc", r2.ApplicationID, code, "OPUS50TS", "g"); !errors.Is(err, identity.ErrNameTaken) {
		t.Fatalf("V1 A11: %v, want ErrNameTaken", err)
	}
}

func TestRevokeHookAuditAndAuth(t *testing.T) {
	f := newFixture(t)
	r := f.apply(t, "zzc", "dev-1", "1.1.1.1")
	ag, _, _ := f.svc.Approve(bg, "zzc", r.ApplicationID, code, "kimi", "g")
	f.clk.Advance(identity.PollMinInterval)
	p, _ := f.svc.Poll(bg, r.ApplicationID, r.PollToken)
	if _, err := f.svc.Revoke(bg, "lbl", ag.ID); !errors.Is(err, identity.ErrForbidden) || len(f.revoked) != 0 {
		t.Fatalf("V2 A6: err=%v hook=%v", err, f.revoked)
	}
	if already, err := f.svc.Revoke(bg, "zzc", ag.ID); already || err != nil || len(f.revoked) != 1 || f.revoked[0] != ag.ID {
		t.Fatalf("revoke: already=%v err=%v hook=%v", already, err, f.revoked)
	}
	if already, err := f.svc.Revoke(bg, "zzc", ag.ID); !already || err != nil || len(f.revoked) != 1 {
		t.Fatalf("V2 A10 second revoke: already=%v err=%v hook=%v", already, err, f.revoked)
	}
	if _, _, err := f.svc.AuthenticateAgent(bg, p.Credential); !errors.Is(err, identity.ErrCredentialRevoked) {
		t.Fatalf("V2 A2: %v, want ErrCredentialRevoked", err)
	}
	n := 0
	for _, e := range f.store.AuditLog() {
		if e.Action == "agent.revoke" {
			n++
			if e.Actor != "zzc" || e.Target != ag.ID {
				t.Fatalf("revoke audit = %+v", e)
			}
		}
	}
	if n != 1 {
		t.Fatalf("agent.revoke audits=%d, want 1", n)
	}
}

func TestRandomnessSourceUsed(t *testing.T) {
	// 可重现：注入固定随机源时，申请号与令牌确定；防止有人改用 math/rand 或时间戳拼号。
	f := newFixture(t)
	f.svc.Rand = bytes.NewReader(bytes.Repeat([]byte{0xab}, 64))
	r := f.apply(t, "zzc", "dev", "1.1.1.1")
	if r.ApplicationID != strings.Repeat("ab", 16) || r.PollToken != strings.Repeat("ab", 32) {
		t.Fatalf("ids not drawn from injected source: %s %s", r.ApplicationID, r.PollToken)
	}
}
