package m8

import (
	"context"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestFeishuDeviceClaimRequiresTrustedPrivateIdentity(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	store := identity.NewMemStore()
	ob := identity.NewOnboarding(identity.NewService(store, nil, nil))
	ob.CodeKey = "fixture-key"
	hub.Onboarding = ob
	_, e := ob.ApplyDeviceClaim(ctx, identity.ClaimRequest{UserID: "alice", ShortName: "al", AgentName: "pmagent", CodeDigest: identity.HashSecret("ABCDEFGHJKMN"), DeviceFingerprint: strings.Repeat("a", 64), ClientVersion: "0.2.2.2", Mode: "desktop"}, "fixture-ip")
	if e != nil {
		t.Fatal(e)
	}
	message := trustedMsgFrom("person", "fixture-app", model.IngressFeishuWSAuthenticated)
	message.ChatType = model.ChatPersonal
	bad := message
	bad.IngressProvenance = model.IngressProvenance("")
	for _, m := range []model.Message{bad, func() model.Message { v := message; v.ChatType = model.ChatGroup; return v }()} {
		_, handled, e := hub.dispatchNewPlatformCommand(context.Background(), m, "#pair ABCDEFGHJKMN")
		if e != nil || !handled {
			t.Fatal(e)
		}
		if n, _ := store.CountUsers(ctx); n != 0 {
			t.Fatal("untrusted/group claim created user")
		}
	}
	res, handled, e := hub.dispatchNewPlatformCommand(ctx, message, "#pair ABCDEFGHJKMN")
	if e != nil || !handled || !strings.Contains(res.Reply, "配对成功") {
		t.Fatalf("trusted claim %v %v", handled, e)
	}
	if n, _ := store.CountUsers(ctx); n != 1 {
		t.Fatal("no self-service account")
	}
}
