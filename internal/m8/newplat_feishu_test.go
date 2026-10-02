package m8

import (
	"context"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// trustedMsgFrom 来自可信入站的飞书消息（长连接已认证；webhook 已验签同样可信）。msgFrom 不带来源 ⇒ 不可信。
func trustedMsgFrom(sender, channel string, p model.IngressProvenance) model.Message {
	m := msgFrom(sender, channel)
	m.IngressProvenance = p
	return m
}

func TestNewPlatformFeishuRegisterAndLogin(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	svc := identity.NewService(identity.NewMemStore(), nil, nil)
	ob := identity.NewOnboarding(svc)
	if _, err := ob.BootstrapAdmin(ctx, "Root", "rt", ""); err != nil {
		t.Fatal(err)
	}
	hub.Onboarding = ob
	admin, _ := svc.Store.GetUser(ctx, "root")
	_, regCode, _, err := ob.CreateUser(ctx, admin, identity.CreateUserRequest{UserID: "alice", ShortName: "al", Type: identity.UserMember})
	if err != nil {
		t.Fatal(err)
	}
	sendAs := func(m model.Message, text string) (string, bool) {
		res, handled, err := hub.dispatchConsoleLogin(context.Background(), m, text)
		if err != nil {
			t.Fatal(err)
		}
		return res.Reply, handled
	}
	send := func(sender, text string) (string, bool) {
		return sendAs(trustedMsgFrom(sender, "devplatform", model.IngressFeishuWSAuthenticated), text)
	}
	untrusted := func(sender, text string) (string, bool) { return sendAs(msgFrom(sender, "devplatform"), text) }
	// 附言、错格式一律不当指令（不吞别的消息）
	for _, txt := range []string{"#register", "#register " + regCode + " 谢谢", "#registerX " + regCode, "#register abc", "你好"} {
		if _, handled := send("ou_alice", txt); handled {
			t.Errorf("%q should not be handled", txt)
		}
		// 不可信入站的非指令消息同样不吞：可信性只拦这三条指令，不拦别的消息
		if _, handled := untrusted("ou_alice", txt); handled {
			t.Errorf("untrusted %q should not be handled", txt)
		}
	}
	// 不可信入站（未签名 webhook）：拒，且不绑定（板 6c90284e）
	if r, h := untrusted("ou_alice", "#register "+regCode); !h || r != untrustedIngressReply {
		t.Fatalf("untrusted register: %v %q", h, r)
	}
	if _, err := svc.Store.ResolveIdentity(ctx, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_alice"}); err == nil {
		t.Fatal("untrusted register must not bind")
	}
	// webhook 已验签也是可信入站
	if r, h := sendAs(trustedMsgFrom("ou_alice", "devplatform", model.IngressWebhookVerified), "#register "+strings.ToLower(regCode)); !h || !strings.Contains(r, "绑定成功") {
		t.Fatalf("register: %v %q", h, r)
	}
	if u, err := svc.Store.ResolveIdentity(ctx, identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_alice"}); err != nil || u.UserID != "alice" {
		t.Fatalf("binding uses channel sender+bot: %+v %v", u, err)
	}
	// 不可信入站的 #console：拒，浏览器领不到会话
	lc0, _ := ob.IssueLoginCode("alice")
	if r, h := untrusted("ou_alice", "#console "+lc0); !h || r != untrustedIngressReply {
		t.Fatalf("untrusted login: %v %q", h, r)
	}
	if tok, ok := ob.ClaimLogin(lc0); ok || tok != "" {
		t.Fatal("untrusted #console must not confirm the login")
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
