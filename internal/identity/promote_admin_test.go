package identity_test

import (
	"errors"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// R6：Onboarding.PromoteToPlatformAdmin（内存实现）——按 user_id／short_name 解析、歧义拒绝、状态与类型护栏、审计一笔含原类型。
func TestPromoteToPlatformAdminMemStore(t *testing.T) {
	e := newN3(t)
	e.boot(t)
	for _, u := range []identity.User{
		{UserID: "Mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "Off", ShortName: "of", Type: identity.UserManager, State: identity.UserDisabled},
		{UserID: "Amb", ShortName: "ab", Type: identity.UserMember, State: identity.UserActive},
		{UserID: "Clash", ShortName: "amb", Type: identity.UserMember, State: identity.UserActive},
	} {
		if err := e.store.CreateUser(bg, u); err != nil {
			t.Fatal(err)
		}
	}
	auditBefore := len(e.store.AuditLog())
	for user, want := range map[string]error{
		"nobody": identity.ErrNotFound, adminID: identity.ErrAlreadyPlatformAdmin, "off": identity.ErrUserNotActive,
		"amb": identity.ErrConflict, " ": identity.ErrBadRequest,
	} {
		if _, _, err := e.ob.PromoteToPlatformAdmin(bg, user); !errors.Is(err, want) {
			t.Fatalf("%q: %v, want %v", user, err, want)
		}
	}
	if n := len(e.store.AuditLog()); n != auditBefore {
		t.Fatalf("refusals wrote %d audit rows", n-auditBefore)
	}
	u, old, err := e.ob.PromoteToPlatformAdmin(bg, "MG")
	if err != nil || u.UserID != "Mgr" || u.Type != identity.UserPlatformAdmin || old != identity.UserManager {
		t.Fatalf("promote: %+v %q %v", u, old, err)
	}
	if got, _ := e.store.GetUser(bg, "mgr"); got.Type != identity.UserPlatformAdmin || got.State != identity.UserActive {
		t.Fatalf("stored: %+v", got)
	}
	log := e.store.AuditLog()
	if len(log) != auditBefore+1 {
		t.Fatalf("audit rows: %d", len(log)-auditBefore)
	}
	if a := log[len(log)-1]; a.Actor != "promote-admin" || a.Action != "user.promoted_platform_admin_by_ops" || a.Target != "mgr" ||
		a.Detail != "old_type=manager new_type=platform_admin" {
		t.Fatalf("audit: %+v", a)
	}
}
