package m8

import (
	"context"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 并入自复验件 REVIEW-FEISHU-TRUSTED-INGRESS-tosonnet55tc2k-20261001.md 附录（组长令，板 3d272254）。
// 原注：独立复验（tosonnet55tc2k）：不可信入站对三条指令（含 ＃lock）的拒绝必须与发信人是否绑定、码是否有效【无关】——
// 否则不可信来源能当探针（探测 open_id 是否已绑定、注册码／登录码是否有效）。
func TestZZ_UntrustedIngressRefusalIsUniform(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	svc := identity.NewService(identity.NewMemStore(), nil, nil)
	ob := identity.NewOnboarding(svc)
	if _, err := ob.BootstrapAdmin(ctx, "Root", "rt", ""); err != nil {
		t.Fatal(err)
	}
	hub.Onboarding = ob
	hub.Identity = svc
	admin, _ := svc.Store.GetUser(ctx, "root")
	_, regCode, _, _ := ob.CreateUser(ctx, admin, identity.CreateUserRequest{UserID: "alice", ShortName: "al", Type: identity.UserMember})
	// （不需要绑定：不可信入站应当对已绑定与未绑定发信人答复完全一致）
	provs := []model.IngressProvenance{"", "untrusted", "WEBHOOK_VERIFIED", " webhook_verified", "feishu_ws_authenticated ", "trusted", "webhook_verified\n"}
	for _, p := range provs {
		for _, sender := range []string{"ou_bound", "ou_nobody"} {
			m := msgFrom(sender, "devplatform")
			m.IngressProvenance = p
			for _, text := range []string{"#register ABCD2345", "#console ABCD2345", "#register " + regCode, "#unlock PGCODE01", "#lock PGCODE01"} {
				var reply string
				var handled bool
				var err error
				if text[:5] == "#unlo" || text[:5] == "#lock" {
					var res model.PrefixDispatchResult
					res, handled, err = hub.dispatchTerminalInputCommand(context.Background(), m, text)
					reply = res.Reply
				} else {
					var res model.PrefixDispatchResult
					res, handled, err = hub.dispatchConsoleLogin(context.Background(), m, text)
					reply = res.Reply
				}
				if err != nil || !handled || reply != untrustedIngressReply {
					t.Errorf("provenance=%q sender=%s %q: handled=%v err=%v reply=%q (want the uniform refusal)", p, sender, text, handled, err, reply)
				}
			}
		}
	}
}
