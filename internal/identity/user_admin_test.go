package identity_test

import (
	"errors"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// A11 停用／改角色（R-V2-SPEC-TASK-SHIM-IMPL，组长 16:55:17Z／16:55:51Z）：Onboarding.SetUserState／SetUserType 的护栏与同步钩子。

// a11Env 管理员（platform_admin）＋经理 mgr＋普通用户 mem，三人都 active；钩子记录被改的用户。
func a11Env(t *testing.T) (*n3, *[]string) {
	t.Helper()
	e := newN3(t)
	e.boot(t)
	for _, u := range []identity.User{
		{UserID: "mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "mem", ShortName: "me", Type: identity.UserMember, State: identity.UserActive},
	} {
		if err := e.store.CreateUser(bg, u); err != nil {
			t.Fatal(err)
		}
	}
	var changed []string
	e.ob.OnUserChanged = func(userLC string) { changed = append(changed, userLC) }
	return e, &changed
}

func a11User(t *testing.T, e *n3, id string) *identity.User {
	t.Helper()
	u, err := e.store.GetUser(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestA11_DisableRevokesSessionsAndCallsHook(t *testing.T) {
	e, changed := a11Env(t)
	if err := e.store.CreateSession(bg, identity.Session{TokenHash: identity.HashSecret("tok-mem"), UserIDLC: "mem", ExpiresAt: e.clk.Now().Add(identity.SessionTTL)}); err != nil {
		t.Fatal(err)
	}
	if err := e.ob.SetUserState(bg, e.admin(t), "MEM", identity.UserDisabled); err != nil {
		t.Fatal(err)
	}
	if u := a11User(t, e, "mem"); u.State != identity.UserDisabled {
		t.Fatalf("state=%s, want disabled", u.State)
	}
	if _, err := e.ob.ResolveSession(bg, "tok-mem"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("session after disable: err=%v, want ErrNotFound", err)
	}
	if len(*changed) != 1 || (*changed)[0] != "mem" {
		t.Fatalf("OnUserChanged calls=%v, want [mem]", *changed)
	}
	// 恢复只从 disabled 回 active，同样触发钩子。
	if err := e.ob.SetUserState(bg, e.admin(t), "mem", identity.UserActive); err != nil {
		t.Fatal(err)
	}
	if u := a11User(t, e, "mem"); u.State != identity.UserActive || len(*changed) != 2 {
		t.Fatalf("after re-enable: state=%s calls=%v", u.State, *changed)
	}
}

func TestA11_SetUserTypeChangesRoleAndCallsHook(t *testing.T) {
	e, changed := a11Env(t)
	if err := e.ob.SetUserType(bg, e.admin(t), "mem", identity.UserManager); err != nil {
		t.Fatal(err)
	}
	if u := a11User(t, e, "mem"); u.Type != identity.UserManager || !identity.CanManageUsers(u) {
		t.Fatalf("type=%s, want manager", u.Type)
	}
	if err := e.ob.SetUserType(bg, e.admin(t), "mgr", identity.UserMember); err != nil {
		t.Fatal(err)
	}
	if u := a11User(t, e, "mgr"); u.Type != identity.UserMember || identity.CanManageUsers(u) {
		t.Fatalf("type=%s, want member", u.Type)
	}
	if len(*changed) != 2 || (*changed)[0] != "mem" || (*changed)[1] != "mgr" {
		t.Fatalf("OnUserChanged calls=%v, want [mem mgr]", *changed)
	}
}

func TestA11_Guards(t *testing.T) {
	e, changed := a11Env(t)
	admin := e.admin(t)
	mem := a11User(t, e, "mem")
	for _, c := range []struct {
		name string
		err  error
		want error
	}{
		{"non-manager disables", e.ob.SetUserState(bg, mem, "mgr", identity.UserDisabled), identity.ErrNotAllowed},
		{"non-manager retypes", e.ob.SetUserType(bg, mem, "mem", identity.UserManager), identity.ErrNotAllowed},
		{"disable self", e.ob.SetUserState(bg, admin, adminID, identity.UserDisabled), identity.ErrNotAllowed},
		{"retype platform_admin", e.ob.SetUserType(bg, a11User(t, e, "mgr"), adminID, identity.UserMember), identity.ErrNotAllowed},
		{"bad state", e.ob.SetUserState(bg, admin, "mem", identity.UserPendingBind), identity.ErrBadRequest},
		{"bad type", e.ob.SetUserType(bg, admin, "mem", identity.UserPlatformAdmin), identity.ErrBadRequest},
		{"activate non-disabled", e.ob.SetUserState(bg, admin, "mem", identity.UserActive), nil}, // 已是 active：无变化、不报错
		{"unknown user", e.ob.SetUserState(bg, admin, "nobody", identity.UserDisabled), identity.ErrNotFound},
	} {
		if !errors.Is(c.err, c.want) || (c.want == nil && c.err != nil) {
			t.Errorf("%s: err=%v, want %v", c.name, c.err, c.want)
		}
	}
	if len(*changed) != 0 {
		t.Fatalf("hook called on refused/no-op changes: %v", *changed)
	}
	// 待绑定用户不能被「恢复」成 active（激活只走飞书注册码）。
	if err := e.store.CreateUser(bg, identity.User{UserID: "pend", ShortName: "pe", Type: identity.UserMember, State: identity.UserPendingBind}); err != nil {
		t.Fatal(err)
	}
	if err := e.ob.SetUserState(bg, admin, "pend", identity.UserActive); !errors.Is(err, identity.ErrBadRequest) {
		t.Fatalf("activate pending_bind: err=%v, want ErrBadRequest", err)
	}
}

// 最后一个可管理用户：调用者本身可管理且不能动自己，所以按调用时的快照看，平台上总还剩调用者。
// 护栏防的是并发互停——两个管理者各拿着改动前的会话快照同时停对方：串行后第二个必须被拒，否则平台上一个管理者都不剩。
func TestA11_LastManagerKept(t *testing.T) {
	e, _ := a11Env(t)
	admin, mgr := e.admin(t), a11User(t, e, "mgr") // 两份快照都取在任何改动之前；可管理用户只有这两个
	if err := e.ob.SetUserState(bg, admin, "mgr", identity.UserDisabled); err != nil {
		t.Fatalf("admin disables mgr: %v", err)
	}
	if err := e.ob.SetUserState(bg, mgr, adminID, identity.UserDisabled); !errors.Is(err, identity.ErrLastManager) {
		t.Fatalf("mgr (stale snapshot) disables the last manager: err=%v, want ErrLastManager", err)
	}
	if err := e.ob.SetUserState(bg, admin, "mgr", identity.UserActive); err != nil {
		t.Fatal(err)
	}
	// 降级同理：mgr 被降为普通用户后，只剩 admin 可管理。
	if err := e.ob.SetUserType(bg, admin, "mgr", identity.UserMember); err != nil {
		t.Fatalf("admin demotes mgr: %v", err)
	}
	if err := e.ob.SetUserState(bg, mgr, adminID, identity.UserDisabled); !errors.Is(err, identity.ErrLastManager) {
		t.Fatalf("after demotion, mgr (stale) disables last manager: err=%v, want ErrLastManager", err)
	}
	if u := e.admin(t); u.State != identity.UserActive {
		t.Fatalf("last manager disabled despite refusal: %+v", u)
	}
	// 还有别的可管理用户时照常：mem 提为经理后，mgr 的旧快照停 admin ⇒ 允许（mem 还在）。
	if err := e.ob.SetUserType(bg, admin, "mem", identity.UserManager); err != nil {
		t.Fatal(err)
	}
	if err := e.ob.SetUserState(bg, mgr, adminID, identity.UserDisabled); err != nil {
		t.Fatalf("disable admin while mem still manages: %v", err)
	}
}
