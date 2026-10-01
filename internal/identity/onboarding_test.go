package identity_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// adminID 初始化脚本建的第一个管理用户（台账 8971bddf：取消 systemdefault）。
const adminID, adminShort = "Admin", "ad"

type n3 struct {
	clk   *clock.Fake
	store *identity.MemStore
	ob    *identity.Onboarding
	pw    string // 初始化脚本给出的一次性初始口令
}

func newN3(t *testing.T) *n3 {
	t.Helper()
	e := &n3{clk: &clock.Fake{T: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}, store: identity.NewMemStore()}
	e.ob = identity.NewOnboarding(identity.NewService(e.store, e.clk.Now, nil))
	return e
}

func (e *n3) boot(t *testing.T) {
	t.Helper()
	pw, err := e.ob.BootstrapAdmin(bg, adminID, adminShort, "")
	if err != nil {
		t.Fatalf("bootstrap-admin: %v", err)
	}
	e.pw = pw
}

func (e *n3) admin(t *testing.T) *identity.User {
	t.Helper()
	u, err := e.store.GetUser(bg, adminID)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

var (
	fsA = identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_alice"}
	fsB = identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_bob"}
)

func TestN3_A1_BootstrapAdmin(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	if n, _ := e.store.CountUsers(bg); n != 1 {
		t.Fatalf("users=%d, want 1", n)
	}
	u := e.admin(t)
	if u.UserID != adminID || u.ShortName != adminShort || u.DisplayName != adminID || u.Type != identity.UserPlatformAdmin ||
		u.State != identity.UserActive || !u.MustChangePassword || u.PasswordHash == "" || u.PasswordHash == e.pw {
		t.Fatalf("admin = %+v", u)
	}
	if len(e.pw) != identity.InitialPasswordLen || len(e.pw) < identity.MinPasswordLen || strings.Trim(e.pw, "ABCDEFGHJKMNPQRSTUVWXYZ23456789") != "" {
		t.Fatalf("initial password %q", e.pw)
	}
	// 不再有常驻 systemdefault
	if _, err := e.store.GetUser(bg, "systemdefault"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("systemdefault must not exist: %v", err)
	}
	// 口令是随机的：另一个空库再执行一次，得到不同的口令
	e2 := newN3(t)
	e2.boot(t)
	if e2.pw == e.pw {
		t.Fatal("two bootstraps produced the same initial password")
	}
}

// 库中已有管理用户（platform_admin 或 manager，不论状态）⇒ 拒绝，什么也不改；只有普通用户时允许。
func TestN3_A3_BootstrapAdminRefusedWhenAdminExists(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	before := e.admin(t).PasswordHash
	if pw, err := e.ob.BootstrapAdmin(bg, "Other", "ot", ""); !errors.Is(err, identity.ErrAdminExists) || pw != "" {
		t.Fatalf("second bootstrap: %q %v", pw, err)
	}
	if n, _ := e.store.CountUsers(bg); n != 1 || e.admin(t).PasswordHash != before {
		t.Fatalf("second bootstrap changed state: users=%d", n)
	}
	// 再以同一用户 ID 执行（想借此重置口令）也一样拒
	if _, err := e.ob.BootstrapAdmin(bg, adminID, adminShort, ""); !errors.Is(err, identity.ErrAdminExists) || e.admin(t).PasswordHash != before {
		t.Fatalf("re-bootstrap same admin: %v", err)
	}
	for name, existing := range map[string]identity.User{
		"manager":        {UserID: "Mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserPendingBind},
		"disabled admin": {UserID: "Old", ShortName: "od", Type: identity.UserPlatformAdmin, State: identity.UserDisabled},
	} {
		e := newN3(t)
		if err := e.store.CreateUser(bg, existing); err != nil {
			t.Fatal(err)
		}
		if _, err := e.ob.BootstrapAdmin(bg, adminID, adminShort, ""); !errors.Is(err, identity.ErrAdminExists) {
			t.Errorf("%s exists: %v, want ErrAdminExists", name, err)
		}
	}
	e = newN3(t)
	if err := e.store.CreateUser(bg, identity.User{UserID: "Mem", ShortName: "me", Type: identity.UserMember, State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	e.boot(t)
}

func TestN3_A4_BootstrapAdminValidation(t *testing.T) {
	for _, c := range []struct{ id, short, display string }{
		{"a", "ad", ""}, {"has space", "ad", ""}, {strings.Repeat("a", 33), "ad", ""},
		{adminID, "x", ""}, {adminID, "ad!", ""}, {adminID, strings.Repeat("s", 17), ""},
		{adminID, adminShort, strings.Repeat("名", 30)},
	} {
		e := newN3(t)
		if _, err := e.ob.BootstrapAdmin(bg, c.id, c.short, c.display); !errors.Is(err, identity.ErrBadRequest) {
			t.Errorf("%q/%q/%d: %v, want ErrBadRequest", c.id, c.short, len(c.display), err)
		}
		if n, _ := e.store.CountUsers(bg); n != 0 {
			t.Errorf("%q/%q: users=%d, want 0", c.id, c.short, n)
		}
	}
	// 与已有普通用户的用户 ID 或简称冲突 ⇒ ErrConflict
	e := newN3(t)
	_ = e.store.CreateUser(bg, identity.User{UserID: "Mem", ShortName: "me", Type: identity.UserMember, State: identity.UserActive})
	for _, c := range [][2]string{{"MEM", "zz"}, {"Fresh", "ME"}} {
		if _, err := e.ob.BootstrapAdmin(bg, c[0], c[1], ""); !errors.Is(err, identity.ErrConflict) {
			t.Errorf("%v: %v, want ErrConflict", c, err)
		}
	}
	if pw, err := e.ob.BootstrapAdmin(bg, "Boss", "bs", "老板"); err != nil || pw == "" {
		t.Fatalf("valid: %v", err)
	}
	if u, _ := e.store.GetUser(bg, "boss"); u.DisplayName != "老板" {
		t.Fatalf("display name: %+v", u)
	}
}

func TestN3_A2_MustChangePasswordThenOldFails(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	tok, u, err := e.ob.PasswordLogin(bg, "ADMIN", e.pw, "1.1.1.1")
	if err != nil || u.UserID != adminID || tok == "" {
		t.Fatalf("login: %v", err)
	}
	if _, err := e.ob.ResolveSession(bg, tok); !errors.Is(err, identity.ErrMustChange) {
		t.Fatalf("session before change: %v, want ErrMustChange", err)
	}
	if err := e.ob.ChangePassword(bg, "admin", e.pw, "short"); !errors.Is(err, identity.ErrWeakPassword) {
		t.Fatalf("weak: %v", err)
	}
	if err := e.ob.ChangePassword(bg, "admin", e.pw, e.pw); !errors.Is(err, identity.ErrWeakPassword) {
		t.Fatalf("same as old: %v", err)
	}
	if err := e.ob.ChangePassword(bg, "admin", e.pw, "New-Strong-Pass-42"); err != nil {
		t.Fatal(err)
	}
	if u, err := e.ob.ResolveSession(bg, tok); err != nil || u.UserID != adminID {
		t.Fatalf("session after change: %v", err)
	}
	if _, _, err := e.ob.PasswordLogin(bg, "admin", e.pw, "1.1.1.1"); !errors.Is(err, identity.ErrBadCredentials) {
		t.Fatalf("old password: %v", err)
	}
	if _, _, err := e.ob.PasswordLogin(bg, "admin", "New-Strong-Pass-42", "1.1.1.1"); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestN3_PasswordLoginRateLimited(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	for i := 0; i < 5; i++ {
		if _, _, err := e.ob.PasswordLogin(bg, "admin", "wrong", "6.6.6.6"); !errors.Is(err, identity.ErrBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	var rl *identity.RateLimitedError
	if _, _, err := e.ob.PasswordLogin(bg, "admin", e.pw, "6.6.6.6"); !errors.As(err, &rl) {
		t.Fatalf("6th attempt with right password: %v, want rate limited", err)
	}
	if _, _, err := e.ob.PasswordLogin(bg, "admin", e.pw, "7.7.7.7"); err != nil {
		t.Fatalf("other IP: %v", err)
	}
	e.clk.Advance(15 * time.Minute)
	if _, _, err := e.ob.PasswordLogin(bg, "admin", e.pw, "6.6.6.6"); err != nil {
		t.Fatalf("after window: %v", err)
	}
}

// onboardAlice 开户 alice 并用飞书身份 A 绑定，返回注册码。
func onboardAlice(t *testing.T, e *n3) string {
	t.Helper()
	_, code, exp, err := e.ob.CreateUser(bg, e.admin(t), identity.CreateUserRequest{DisplayName: "Alice", UserID: "Alice", ShortName: "al", Type: identity.UserMember})
	if err != nil || len(code) != 8 || !exp.Equal(e.clk.T.Add(24*time.Hour)) {
		t.Fatalf("create alice: code=%q exp=%s err=%v", code, exp, err)
	}
	return code
}

func TestN3_A6_A7_CreateUserRulesAndRoles(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	onboardAlice(t, e)
	_, _, _, err := e.ob.CreateUser(bg, e.admin(t), identity.CreateUserRequest{UserID: "ALICE", ShortName: "zz", Type: identity.UserMember})
	if !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("A6 ALICE vs Alice: %v", err)
	}
	_, _, _, err = e.ob.CreateUser(bg, e.admin(t), identity.CreateUserRequest{UserID: "carol", ShortName: "AL", Type: identity.UserMember})
	if !errors.Is(err, identity.ErrConflict) {
		t.Fatalf("A6 short AL vs al: %v", err)
	}
	for _, bad := range []identity.CreateUserRequest{
		{UserID: "c.rol", ShortName: "cr", Type: identity.UserMember},
		{UserID: "carol", ShortName: strings.Repeat("c", 17), Type: identity.UserMember},
		{UserID: "carol", ShortName: "cr", Type: identity.UserPlatformAdmin},
	} {
		if _, _, _, err := e.ob.CreateUser(bg, e.admin(t), bad); !errors.Is(err, identity.ErrBadRequest) {
			t.Errorf("bad %+v: %v", bad, err)
		}
	}
	member, _ := e.store.GetUser(bg, "alice")
	member.State = identity.UserActive
	if _, _, _, err := e.ob.CreateUser(bg, member, identity.CreateUserRequest{UserID: "dave", ShortName: "dv", Type: identity.UserMember}); !errors.Is(err, identity.ErrNotAllowed) {
		t.Fatalf("A7 member creates user: %v", err)
	}
}

func TestN3_A8_A9_A10_RegisterBinding(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	code := onboardAlice(t, e)
	if r := e.ob.HandleRegister(bg, fsA, "  "+strings.ToLower(code)+" "); !strings.Contains(r, "绑定成功") || !strings.Contains(r, "Alice") {
		t.Fatalf("A8 register: %q", r)
	}
	if u, _ := e.store.GetUser(bg, "alice"); u.State != identity.UserActive {
		t.Fatalf("alice state=%s", u.State)
	}
	if r := e.ob.HandleRegister(bg, fsB, code); !strings.Contains(r, "无效") {
		t.Fatalf("A8 code reuse: %q", r)
	}
	if r := e.ob.HandleRegister(bg, fsB, "ZZZZZZZZ"); !strings.Contains(r, "无效") {
		t.Fatalf("A9 unknown code: %q", r)
	}
	if n, _ := e.store.CountUsers(bg); n != 2 {
		t.Fatalf("A9 users=%d, want 2 (no auto-create)", n)
	}
	_, code2, _, _ := e.ob.CreateUser(bg, e.admin(t), identity.CreateUserRequest{UserID: "bob", ShortName: "bb", Type: identity.UserMember})
	if r := e.ob.HandleRegister(bg, fsA, code2); !strings.Contains(r, "已绑定到其它账号") {
		t.Fatalf("A10 identity reuse: %q", r)
	}
	if u, _ := e.store.GetUser(bg, "bob"); u.State != identity.UserPendingBind {
		t.Fatalf("A10 bob state=%s", u.State)
	}
	_, code3, _, _ := e.ob.CreateUser(bg, e.admin(t), identity.CreateUserRequest{UserID: "carol", ShortName: "cr", Type: identity.UserMember})
	e.clk.Advance(identity.RegistrationCodeTTL)
	if r := e.ob.HandleRegister(bg, fsB, code3); !strings.Contains(r, "过期") {
		t.Fatalf("A9 expired code: %q", r)
	}
	if r := e.ob.HandleRegister(bg, identity.Identity{Provider: "feishu"}, code2); !strings.Contains(r, "无法确认发送者") {
		t.Fatalf("no sender: %q", r)
	}
}

func TestN3_A11_A12_LoginByUserID(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	e.ob.HandleRegister(bg, fsA, onboardAlice(t, e))
	_, codeB, _, _ := e.ob.CreateUser(bg, e.admin(t), identity.CreateUserRequest{UserID: "bob", ShortName: "bb", Type: identity.UserMember})
	e.ob.HandleRegister(bg, fsB, codeB)

	lc, err := e.ob.IssueLoginCode("alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.ob.ClaimLogin(lc); ok {
		t.Fatal("claim before confirm")
	}
	if r := e.ob.HandleLogin(bg, fsB, lc); !strings.Contains(r, "未绑定到该用户") {
		t.Fatalf("A11 bob confirms alice's code: %q", r)
	}
	if _, ok := e.ob.ClaimLogin(lc); ok {
		t.Fatal("A11 claim after foreign confirm")
	}
	if r := e.ob.HandleLogin(bg, fsA, lc); !strings.Contains(r, "以 Alice 登录") {
		t.Fatalf("A11 alice confirms: %q", r)
	}
	tok, ok := e.ob.ClaimLogin(lc)
	if !ok {
		t.Fatal("claim after confirm")
	}
	if u, err := e.ob.ResolveSession(bg, tok); err != nil || u.UserID != "Alice" {
		t.Fatalf("session: %+v %v", u, err)
	}
	if _, ok := e.ob.ClaimLogin(lc); ok {
		t.Fatal("A12 second claim")
	}
	if r := e.ob.HandleLogin(bg, fsA, lc); !strings.Contains(r, "已过期或已被使用") {
		t.Fatalf("A12 reuse after claim: %q", r)
	}
	lc2, _ := e.ob.IssueLoginCode("alice")
	e.clk.Advance(identity.LoginCodeTTL)
	if r := e.ob.HandleLogin(bg, fsA, lc2); !strings.Contains(r, "已过期") {
		t.Fatalf("A12 expired: %q", r)
	}
	// 用户 ID 不存在也出码（不泄露），只是确认时一定失败。
	lc3, err := e.ob.IssueLoginCode("ghost")
	if err != nil || lc3 == "" {
		t.Fatalf("ghost code: %v", err)
	}
	if r := e.ob.HandleLogin(bg, fsA, lc3); !strings.Contains(r, "未绑定到该用户") {
		t.Fatalf("ghost confirm: %q", r)
	}
}

func TestN3_A13_DisableKillsSession(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	e.ob.HandleRegister(bg, fsA, onboardAlice(t, e))
	lc, _ := e.ob.IssueLoginCode("alice")
	e.ob.HandleLogin(bg, fsA, lc)
	tok, _ := e.ob.ClaimLogin(lc)
	if err := e.store.SetUserState(bg, "alice", identity.UserDisabled, e.clk.T); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ob.ResolveSession(bg, tok); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("session after disable: %v", err)
	}
}

func TestN3_A5_A14_AuditHasNoSecretsAndCoversActions(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	tok, _, _ := e.ob.PasswordLogin(bg, "admin", e.pw, "1.1.1.1")
	_ = e.ob.ChangePassword(bg, "admin", e.pw, "New-Strong-Pass-42")
	regCode := onboardAlice(t, e)
	e.ob.HandleRegister(bg, fsA, regCode)
	lc, _ := e.ob.IssueLoginCode("alice")
	e.ob.HandleLogin(bg, fsA, lc)
	sess, _ := e.ob.ClaimLogin(lc)
	_, _, _ = e.ob.PasswordLogin(bg, "admin", "wrong", "1.1.1.1")
	seen := map[string]bool{}
	for _, ev := range e.store.AuditLog() {
		seen[ev.Action] = true
		blob := ev.Actor + "|" + ev.Action + "|" + ev.Target + "|" + ev.Detail
		for _, s := range []string{e.pw, "New-Strong-Pass-42", regCode, lc, tok, sess} {
			if s != "" && strings.Contains(blob, s) {
				t.Fatalf("audit %s leaks a secret", ev.Action)
			}
		}
	}
	for _, a := range []string{"user.bootstrap_admin", "login.ok", "login.rejected", "password.change", "user.create", "user.registration_code", "user.bind"} {
		if !seen[a] {
			t.Errorf("A14: no audit for %s", a)
		}
	}
	// 阳性对照：同一扫描对真正带了明文的审计必须报出来，证明扫描本身有牙。
	_ = e.store.AppendAudit(bg, identity.AuditEvent{Action: "planted", Detail: "code=" + regCode})
	leaked := false
	for _, ev := range e.store.AuditLog() {
		if strings.Contains(ev.Detail, regCode) {
			leaked = true
		}
	}
	if !leaked {
		t.Fatal("positive control: scan failed to see a planted secret")
	}
}
