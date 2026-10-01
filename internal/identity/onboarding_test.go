package identity_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

const bootPw = "Boot-Pass-0001"

type n3 struct {
	clk   *clock.Fake
	store *identity.MemStore
	ob    *identity.Onboarding
	pwf   string
}

func newN3(t *testing.T) *n3 {
	t.Helper()
	e := &n3{clk: &clock.Fake{T: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}, store: identity.NewMemStore()}
	e.ob = identity.NewOnboarding(identity.NewService(e.store, e.clk.Now, nil))
	e.pwf = filepath.Join(t.TempDir(), "systemdefault.pw")
	if err := os.WriteFile(e.pwf, []byte(bootPw+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *n3) boot(t *testing.T) {
	t.Helper()
	if created, err := e.ob.Bootstrap(bg, e.pwf); err != nil || !created {
		t.Fatalf("bootstrap: created=%v err=%v", created, err)
	}
}

func (e *n3) admin(t *testing.T) *identity.User {
	t.Helper()
	u, err := e.store.GetUser(bg, identity.SystemDefaultUserID)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

var (
	fsA = identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_alice"}
	fsB = identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_bob"}
)

func TestN3_A1_BootstrapEmptyDB(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	if n, _ := e.store.CountUsers(bg); n != 1 {
		t.Fatalf("users=%d, want 1", n)
	}
	u := e.admin(t)
	if u.Type != identity.UserPlatformAdmin || u.State != identity.UserActive || !u.MustChangePassword || u.PasswordHash == "" || u.PasswordHash == bootPw {
		t.Fatalf("systemdefault = %+v", u)
	}
}

func TestN3_A3_BootstrapNonEmptyNoop(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	before := e.admin(t).PasswordHash
	if err := os.WriteFile(e.pwf, []byte("Another-Pass-999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := e.ob.Bootstrap(bg, e.pwf)
	if err != nil || created {
		t.Fatalf("second bootstrap: created=%v err=%v", created, err)
	}
	if n, _ := e.store.CountUsers(bg); n != 1 {
		t.Fatalf("users=%d", n)
	}
	if e.admin(t).PasswordHash != before {
		t.Fatal("password hash reset by second bootstrap")
	}
	// 非空库时连密码文件都不读：文件缺失也不报错。
	_ = os.Remove(e.pwf)
	if created, err := e.ob.Bootstrap(bg, e.pwf); err != nil || created {
		t.Fatalf("non-empty with missing file: created=%v err=%v", created, err)
	}
}

func TestN3_A4_BootstrapFailsClosed(t *testing.T) {
	for name, prep := range map[string]func(path string){
		"missing":   func(p string) { _ = os.Remove(p) },
		"mode 0644": func(p string) { _ = os.Chmod(p, 0o644) },
		"empty":     func(p string) { _ = os.WriteFile(p, []byte("\n"), 0o600) },
	} {
		e := newN3(t)
		prep(e.pwf)
		created, err := e.ob.Bootstrap(bg, e.pwf)
		if !errors.Is(err, identity.ErrBootstrapSecret) || created {
			t.Errorf("%s: created=%v err=%v", name, created, err)
		}
		if n, _ := e.store.CountUsers(bg); n != 0 {
			t.Errorf("%s: users=%d, want 0", name, n)
		}
	}
}

func TestN3_A2_MustChangePasswordThenOldFails(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	tok, u, err := e.ob.PasswordLogin(bg, "SystemDefault", bootPw, "1.1.1.1")
	if err != nil || u.UserID != identity.SystemDefaultUserID || tok == "" {
		t.Fatalf("login: %v", err)
	}
	if _, err := e.ob.ResolveSession(bg, tok); !errors.Is(err, identity.ErrMustChange) {
		t.Fatalf("session before change: %v, want ErrMustChange", err)
	}
	if err := e.ob.ChangePassword(bg, "systemdefault", bootPw, "short"); !errors.Is(err, identity.ErrWeakPassword) {
		t.Fatalf("weak: %v", err)
	}
	if err := e.ob.ChangePassword(bg, "systemdefault", bootPw, bootPw); !errors.Is(err, identity.ErrWeakPassword) {
		t.Fatalf("same as old: %v", err)
	}
	if err := e.ob.ChangePassword(bg, "systemdefault", bootPw, "New-Strong-Pass-42"); err != nil {
		t.Fatal(err)
	}
	if u, err := e.ob.ResolveSession(bg, tok); err != nil || u.UserID != identity.SystemDefaultUserID {
		t.Fatalf("session after change: %v", err)
	}
	if _, _, err := e.ob.PasswordLogin(bg, "systemdefault", bootPw, "1.1.1.1"); !errors.Is(err, identity.ErrBadCredentials) {
		t.Fatalf("old password: %v", err)
	}
	if _, _, err := e.ob.PasswordLogin(bg, "systemdefault", "New-Strong-Pass-42", "1.1.1.1"); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestN3_PasswordLoginRateLimited(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	for i := 0; i < 5; i++ {
		if _, _, err := e.ob.PasswordLogin(bg, "systemdefault", "wrong", "6.6.6.6"); !errors.Is(err, identity.ErrBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	var rl *identity.RateLimitedError
	if _, _, err := e.ob.PasswordLogin(bg, "systemdefault", bootPw, "6.6.6.6"); !errors.As(err, &rl) {
		t.Fatalf("6th attempt with right password: %v, want rate limited", err)
	}
	if _, _, err := e.ob.PasswordLogin(bg, "systemdefault", bootPw, "7.7.7.7"); err != nil {
		t.Fatalf("other IP: %v", err)
	}
	e.clk.Advance(15 * time.Minute)
	if _, _, err := e.ob.PasswordLogin(bg, "systemdefault", bootPw, "6.6.6.6"); err != nil {
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
	tok, _, _ := e.ob.PasswordLogin(bg, "systemdefault", bootPw, "1.1.1.1")
	_ = e.ob.ChangePassword(bg, "systemdefault", bootPw, "New-Strong-Pass-42")
	regCode := onboardAlice(t, e)
	e.ob.HandleRegister(bg, fsA, regCode)
	lc, _ := e.ob.IssueLoginCode("alice")
	e.ob.HandleLogin(bg, fsA, lc)
	sess, _ := e.ob.ClaimLogin(lc)
	_, _, _ = e.ob.PasswordLogin(bg, "systemdefault", "wrong", "1.1.1.1")
	seen := map[string]bool{}
	for _, ev := range e.store.AuditLog() {
		seen[ev.Action] = true
		blob := ev.Actor + "|" + ev.Action + "|" + ev.Target + "|" + ev.Detail
		for _, s := range []string{bootPw, "New-Strong-Pass-42", regCode, lc, tok, sess} {
			if s != "" && strings.Contains(blob, s) {
				t.Fatalf("audit %s leaks a secret", ev.Action)
			}
		}
	}
	for _, a := range []string{"user.bootstrap", "login.ok", "login.rejected", "password.change", "user.create", "user.registration_code", "user.bind"} {
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
