package identity_test

// 业主反馈（板 7a138dcf）：管理用户在用户列表里一直看得到待绑定用户「当前有效」的注册码，
// 直到绑定、过期或重新生成。库里只存哈希：码由 CodeKey、用户、到期时刻派生，取回时重算、哈希对上才给。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

func regcodeEnv(t *testing.T, key string) (*identity.Onboarding, *clock.Fake, *identity.User) {
	t.Helper()
	ob, clk, admin, _ := regcodeEnvPw(t, key)
	return ob, clk, admin
}

// regcodeEnvPw 同 regcodeEnv，另返回初始化脚本给 Root 的一次性口令（自取绑定码要再认证）。
func regcodeEnvPw(t *testing.T, key string) (*identity.Onboarding, *clock.Fake, *identity.User, string) {
	t.Helper()
	clk := &clock.Fake{T: time.Date(2026, 10, 2, 1, 0, 0, 123456789, time.UTC)} // 带纳秒：钉住「截到微秒」
	svc := identity.NewService(identity.NewMemStore(), clk.Now, nil)
	ob := identity.NewOnboarding(svc)
	ob.CodeKey = key
	ctx := context.Background()
	pw, err := ob.BootstrapAdmin(ctx, "Root", "rt", "")
	if err != nil {
		t.Fatal(err)
	}
	admin, _ := svc.Store.GetUser(ctx, "root")
	return ob, clk, admin, pw
}

func TestRegistrationCodeRecoverable(t *testing.T) {
	ctx := context.Background()
	ob, clk, admin := regcodeEnv(t, "k-secret-1")
	_, code, exp, err := ob.CreateUser(ctx, admin, identity.CreateUserRequest{UserID: "Alice", ShortName: "al", Type: identity.UserMember})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ob.RecoverableRegistrationCodes(ctx)
	if err != nil || got["alice"].Code != code || !got["alice"].ExpiresAt.Equal(exp) {
		t.Fatalf("recoverable %+v %v, want code %s exp %v", got, err, code, exp)
	}
	if exp.Nanosecond()%1000 != 0 {
		t.Fatalf("expiry must be truncated to microseconds (PG precision): %v", exp)
	}
	// 重新生成 ⇒ 只给新码
	code2, _, err := ob.RegenerateRegistrationCode(ctx, admin, "alice")
	if err != nil || code2 == code {
		t.Fatalf("regenerate: %q %v", code2, err)
	}
	if got, _ := ob.RecoverableRegistrationCodes(ctx); got["alice"].Code != code2 {
		t.Fatalf("after regenerate: %+v, want %s", got, code2)
	}
	// 过期 ⇒ 不给
	clk.T = clk.T.Add(identity.RegistrationCodeTTL + time.Second)
	if got, _ := ob.RecoverableRegistrationCodes(ctx); len(got) != 0 {
		t.Fatalf("expired code still recoverable: %+v", got)
	}
	// 新码、绑定 ⇒ 不给
	code3, _, _ := ob.RegenerateRegistrationCode(ctx, admin, "alice")
	if r := ob.HandleRegister(ctx, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_a"}, code3); !strings.Contains(r, "绑定成功") {
		t.Fatalf("register: %s", r)
	}
	if got, _ := ob.RecoverableRegistrationCodes(ctx); len(got) != 0 {
		t.Fatalf("redeemed code still recoverable: %+v", got)
	}
}

// 取不回就不给：未配密钥、换过密钥、没密钥时代发的随机码。
func TestRegistrationCodeNotRecoverableWithoutTheKey(t *testing.T) {
	ctx := context.Background()
	ob, _, admin := regcodeEnv(t, "")
	if _, _, _, err := ob.CreateUser(ctx, admin, identity.CreateUserRequest{UserID: "bob", ShortName: "bb", Type: identity.UserMember}); err != nil {
		t.Fatal(err)
	}
	if got, _ := ob.RecoverableRegistrationCodes(ctx); len(got) != 0 {
		t.Fatalf("no key: %+v", got)
	}
	ob.CodeKey = "k-secret-1" // 后来才配上密钥：之前发的随机码取不回
	if got, _ := ob.RecoverableRegistrationCodes(ctx); len(got) != 0 {
		t.Fatalf("random code issued before the key must not be recoverable: %+v", got)
	}
	if _, _, err := ob.RegenerateRegistrationCode(ctx, admin, "bob"); err != nil {
		t.Fatal(err)
	}
	ob.CodeKey = "k-secret-2" // 换了密钥
	if got, _ := ob.RecoverableRegistrationCodes(ctx); len(got) != 0 {
		t.Fatalf("code derived under another key must not be recoverable: %+v", got)
	}
}

// 派生码形状：8 位、只用注册码字母表；不同用户、不同到期时刻得不同码。
func TestRegistrationCodeDerivedShape(t *testing.T) {
	ctx := context.Background()
	ob, _, admin := regcodeEnv(t, "k-secret-1")
	seen := map[string]bool{}
	for _, id := range []string{"cara", "dave", "erin", "fred"} {
		_, code, _, err := ob.CreateUser(ctx, admin, identity.CreateUserRequest{UserID: id, ShortName: id[:2] + "x", Type: identity.UserMember})
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 8 || strings.Trim(code, "ABCDEFGHJKMNPQRSTUVWXYZ23456789") != "" {
			t.Fatalf("code %q not 8 chars from the alphabet", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code %s", code)
		}
		seen[code] = true
	}
}

// 在 PG 上走一遍：库里到期时刻只到微秒，派生用的到期时刻必须同精度，否则取回时重算不出同一个码。
func TestRegistrationCodeRecoverableOnPG(t *testing.T) {
	ctx := context.Background()
	st := pgFactory(t)(t)
	clk := &clock.Fake{T: time.Date(2026, 10, 2, 1, 0, 0, 123456789, time.UTC)}
	svc := identity.NewService(st, clk.Now, nil)
	ob := identity.NewOnboarding(svc)
	ob.CodeKey = "k-secret-1"
	if _, err := ob.BootstrapAdmin(ctx, "Root", "rt", ""); err != nil {
		t.Fatal(err)
	}
	admin, _ := st.GetUser(ctx, "root")
	_, code, exp, err := ob.CreateUser(ctx, admin, identity.CreateUserRequest{UserID: "Alice", ShortName: "al", Type: identity.UserMember})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ob.RecoverableRegistrationCodes(ctx)
	if err != nil || got["alice"].Code != code || !got["alice"].ExpiresAt.Equal(exp) {
		t.Fatalf("PG round trip: %+v %v, want %s %v", got, err, code, exp)
	}
}

// 本人自取飞书绑定码（组长 e483f147）：口令建的 active 管理员给自己出码 ⇒ 兑换只绑身份，状态、改密标记、会话都不变；
// 已有飞书身份 ⇒ 不再出码；待绑定用户不走这条（他们有管理员发的注册码）。
func TestBindCodeForActivePasswordUser(t *testing.T) {
	ctx := context.Background()
	ob, _, admin, pw := regcodeEnvPw(t, "k-secret-1")
	if err := ob.Store.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("tok-root"), UserIDLC: "root",
		ExpiresAt: ob.Now().Add(identity.SessionTTL)}); err != nil {
		t.Fatal(err)
	}
	code, _, err := ob.IssueBindCode(ctx, admin, pw, "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if r := ob.HandleRegister(ctx, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_root"}, code); !strings.Contains(r, "绑定成功") {
		t.Fatalf("redeem: %s", r)
	}
	after, _ := ob.Store.GetUser(ctx, "root")
	if after.State != identity.UserActive || after.MustChangePassword != admin.MustChangePassword || after.Type != admin.Type {
		t.Fatalf("bind changed the user: before %+v after %+v", admin, after)
	}
	if _, u, err := ob.Store.ResolveSession(ctx, identity.HashSecret("tok-root"), ob.Now()); err != nil || u.UserIDLC() != "root" {
		t.Fatalf("existing session must survive the bind: %+v %v", u, err)
	}
	if _, _, err := ob.IssueBindCode(ctx, after, pw, "10.0.0.1"); err != identity.ErrAlreadyBound {
		t.Fatalf("second bind code: %v, want ErrAlreadyBound", err)
	}
	pending := &identity.User{UserID: "zed", State: identity.UserPendingBind}
	if _, _, err := ob.IssueBindCode(ctx, pending, pw, "10.0.0.1"); err != identity.ErrNotAllowed {
		t.Fatalf("pending user: %v, want ErrNotAllowed", err)
	}
}

// 组长 01be74c0（复验方登记②）：自取绑定码须再认证。缺口令、错口令 ⇒ ErrBadCredentials 且不出码；
// 失败计入口令登录的同一限速（同来源＋同用户）：5 次后连正确口令也 rate_limited，口令登录同样被限。
func TestBindCodeRequiresCurrentPassword(t *testing.T) {
	ctx := context.Background()
	ob, _, admin, pw := regcodeEnvPw(t, "k-secret-1")
	for _, bad := range []string{"", "wrong-password"} {
		if code, _, err := ob.IssueBindCode(ctx, admin, bad, "10.0.0.1"); err != identity.ErrBadCredentials || code != "" {
			t.Fatalf("password %q: code=%q err=%v, want ErrBadCredentials and no code", bad, code, err)
		}
	}
	if codes, _ := ob.RecoverableRegistrationCodes(ctx); len(codes) != 0 {
		t.Fatalf("a refused bind-code request must not issue anything: %v", codes)
	}
	// 另一来源不受这两次失败影响（键是来源＋用户），正确口令照常出码
	if _, _, err := ob.IssueBindCode(ctx, admin, pw, "10.0.0.2"); err != nil {
		t.Fatalf("right password from another source: %v", err)
	}
	for i := 0; i < 3; i++ { // 同来源再错 3 次 ⇒ 共 5 次
		if _, _, err := ob.IssueBindCode(ctx, admin, "wrong-password", "10.0.0.1"); err != identity.ErrBadCredentials {
			t.Fatalf("fail %d: %v", i+3, err)
		}
	}
	var rl *identity.RateLimitedError
	if _, _, err := ob.IssueBindCode(ctx, admin, pw, "10.0.0.1"); !errors.As(err, &rl) {
		t.Fatalf("after 5 failures even the right password must be rate limited: %v", err)
	}
	if _, _, err := ob.PasswordLogin(ctx, "Root", pw, "10.0.0.1"); !errors.As(err, &rl) {
		t.Fatalf("bind-code failures must count toward the password-login limit: %v", err)
	}
}
