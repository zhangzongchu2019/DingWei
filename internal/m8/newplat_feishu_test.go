package m8

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

func TestNewPlatformFeishuRegisterAndLogin(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	svc := identity.NewService(identity.NewMemStore(), nil, nil)
	ob := identity.NewOnboarding(svc)
	pwf := filepath.Join(t.TempDir(), "pw")
	_ = os.WriteFile(pwf, []byte("Boot-Pass-0001"), 0o600)
	if _, err := ob.Bootstrap(ctx, pwf); err != nil {
		t.Fatal(err)
	}
	hub.Onboarding = ob
	admin, _ := svc.Store.GetUser(ctx, identity.SystemDefaultUserID)
	_, regCode, _, err := ob.CreateUser(ctx, admin, identity.CreateUserRequest{UserID: "alice", ShortName: "al", Type: identity.UserMember})
	if err != nil {
		t.Fatal(err)
	}
	send := func(sender, text string) (string, bool) {
		res, handled, err := hub.dispatchConsoleLogin(context.Background(), msgFrom(sender, "devplatform"), text)
		if err != nil {
			t.Fatal(err)
		}
		return res.Reply, handled
	}
	// 附言、错格式一律不当指令（不吞别的消息）
	for _, txt := range []string{"#register", "#register " + regCode + " 谢谢", "#registerX " + regCode, "#register abc"} {
		if _, handled := send("ou_alice", txt); handled {
			t.Errorf("%q should not be handled", txt)
		}
	}
	if r, h := send("ou_alice", "#register "+strings.ToLower(regCode)); !h || !strings.Contains(r, "绑定成功") {
		t.Fatalf("register: %v %q", h, r)
	}
	if u, err := svc.Store.ResolveIdentity(ctx, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_alice"}); err != nil || u.UserID != "alice" {
		t.Fatalf("binding uses channel sender+bot: %+v %v", u, err)
	}
	lc, _ := ob.IssueLoginCode("alice")
	if r, _ := send("ou_mallory", "#console "+lc); !strings.Contains(r, "未绑定到该用户") {
		t.Fatalf("other sender: %q", r)
	}
	if r, _ := send("ou_alice", "#console "+lc); !strings.Contains(r, "以 alice 登录") {
		t.Fatalf("login: %q", r)
	}
	if tok, ok := ob.ClaimLogin(lc); !ok || tok == "" {
		t.Fatal("browser cannot claim session after feishu confirm")
	}
}
