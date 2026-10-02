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
	// active 用户必带激活痕迹（口令或飞书身份）：生产里 active 只来自注册码兑换（带飞书身份）或初始化脚本（带口令），
	// 「恢复」据此推回停用前状态（R1）。夹具给口令哈希。
	for _, u := range []identity.User{
		{UserID: "mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive, PasswordHash: "x"},
		{UserID: "mem", ShortName: "me", Type: identity.UserMember, State: identity.UserActive, PasswordHash: "x"},
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
	if _, err := e.ob.SetUserState(bg, e.admin(t), "MEM", identity.UserDisabled); err != nil {
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
	if _, err := e.ob.SetUserState(bg, e.admin(t), "mem", identity.UserActive); err != nil {
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
		{"non-manager disables", errOf(e.ob.SetUserState(bg, mem, "mgr", identity.UserDisabled)), identity.ErrNotAllowed},
		{"non-manager retypes", e.ob.SetUserType(bg, mem, "mem", identity.UserManager), identity.ErrNotAllowed},
		{"disable self", errOf(e.ob.SetUserState(bg, admin, adminID, identity.UserDisabled)), identity.ErrNotAllowed},
		{"retype platform_admin", e.ob.SetUserType(bg, a11User(t, e, "mgr"), adminID, identity.UserMember), identity.ErrNotAllowed},
		{"bad state", errOf(e.ob.SetUserState(bg, admin, "mem", identity.UserPendingBind)), identity.ErrBadRequest},
		{"bad type", e.ob.SetUserType(bg, admin, "mem", identity.UserPlatformAdmin), identity.ErrBadRequest},
		{"activate non-disabled", errOf(e.ob.SetUserState(bg, admin, "mem", identity.UserActive)), nil}, // 已是 active：无变化、不报错
		{"unknown user", errOf(e.ob.SetUserState(bg, admin, "nobody", identity.UserDisabled)), identity.ErrNotFound},
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
	if _, err := e.ob.SetUserState(bg, admin, "pend", identity.UserActive); !errors.Is(err, identity.ErrBadRequest) {
		t.Fatalf("activate pending_bind: err=%v, want ErrBadRequest", err)
	}
}

// 最后一个可管理用户：调用者本身可管理且不能动自己，所以按调用时的快照看，平台上总还剩调用者。
// 护栏防的是并发互停——两个管理者各拿着改动前的会话快照同时停对方：串行后第二个必须被拒，否则平台上一个管理者都不剩。
// 夹具：平台管理者不可经服务停用（R2），故先在存储层把他停掉，只留 mgr 与 mgr2 两个可管理用户。
func TestA11_LastManagerKept(t *testing.T) {
	e, _ := a11Env(t)
	if err := e.store.CreateUser(bg, identity.User{UserID: "mgr2", ShortName: "m2", Type: identity.UserManager, State: identity.UserActive, PasswordHash: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetUserState(bg, adminID, identity.UserDisabled, e.clk.Now()); err != nil {
		t.Fatal(err)
	}
	mgr, mgr2 := a11User(t, e, "mgr"), a11User(t, e, "mgr2") // 两份快照都取在任何改动之前
	if _, err := e.ob.SetUserState(bg, mgr, "mgr2", identity.UserDisabled); err != nil {
		t.Fatalf("mgr disables mgr2: %v", err)
	}
	if _, err := e.ob.SetUserState(bg, mgr2, "mgr", identity.UserDisabled); !errors.Is(err, identity.ErrLastManager) {
		t.Fatalf("mgr2 (stale snapshot) disables the last manager: err=%v, want ErrLastManager", err)
	}
	if err := e.ob.SetUserType(bg, mgr2, "mgr", identity.UserMember); !errors.Is(err, identity.ErrLastManager) {
		t.Fatalf("mgr2 (stale snapshot) demotes the last manager: err=%v, want ErrLastManager", err)
	}
	if u := a11User(t, e, "mgr"); u.State != identity.UserActive || u.Type != identity.UserManager {
		t.Fatalf("last manager changed despite refusal: %+v", u)
	}
	// 还有别的可管理用户时照常：mgr 恢复 mgr2 后，mgr2 的旧快照去降级 mgr ⇒ 允许（mgr2 还在）。
	if _, err := e.ob.SetUserState(bg, mgr, "mgr2", identity.UserActive); err != nil {
		t.Fatal(err)
	}
	if err := e.ob.SetUserType(bg, mgr2, "mgr", identity.UserMember); err != nil {
		t.Fatalf("demote mgr while mgr2 still manages: %v", err)
	}
}

func errOf(_ identity.UserState, err error) error { return err }

// R1（组长 17:56:12Z）：恢复只回到停用前状态。待绑定 → 停用 → 恢复 ⇒ 仍待绑定，且注册码流程可用（重新出码、飞书兑换后才 active）；
// 已绑飞书的 active 用户 → 停用 → 恢复 ⇒ active；有口令无飞书的管理用户同样回 active。
func TestA11_RestoreReturnsToPreDisableState(t *testing.T) {
	e, changed := a11Env(t)
	admin := e.admin(t)
	nu, code, _, err := e.ob.CreateUser(bg, admin, identity.CreateUserRequest{UserID: "pend", ShortName: "pe", Type: identity.UserMember})
	if err != nil || nu.State != identity.UserPendingBind {
		t.Fatalf("create pending user: %+v %v", nu, err)
	}
	if st, err := e.ob.SetUserState(bg, admin, "pend", identity.UserDisabled); err != nil || st != identity.UserDisabled {
		t.Fatalf("disable pending: %s %v", st, err)
	}
	if r := e.ob.HandleRegister(bg, fsA, code); a11User(t, e, "pend").State != identity.UserDisabled {
		t.Fatalf("disabled user activated by registration code: %s", r)
	}
	st, err := e.ob.SetUserState(bg, admin, "pend", identity.UserActive)
	if err != nil || st != identity.UserPendingBind {
		t.Fatalf("restore pending user: state=%s err=%v, want pending_bind", st, err)
	}
	if u := a11User(t, e, "pend"); u.State != identity.UserPendingBind {
		t.Fatalf("stored state=%s, want pending_bind", u.State)
	}
	code2, _, err := e.ob.RegenerateRegistrationCode(bg, admin, "pend")
	if err != nil {
		t.Fatalf("registration code after restore: %v", err)
	}
	e.ob.HandleRegister(bg, fsA, code2)
	if u := a11User(t, e, "pend"); u.State != identity.UserActive {
		t.Fatalf("after feishu registration: %s, want active", u.State)
	}
	// 已绑飞书的 active 用户：停用 → 恢复 ⇒ active。
	if _, err := e.ob.SetUserState(bg, admin, "pend", identity.UserDisabled); err != nil {
		t.Fatal(err)
	}
	if st, err := e.ob.SetUserState(bg, admin, "pend", identity.UserActive); err != nil || st != identity.UserActive {
		t.Fatalf("restore bound user: %s %v, want active", st, err)
	}
	// 有口令、无飞书的管理用户（如初始化脚本建的那类）：恢复 ⇒ active。
	if err := e.store.SetPassword(bg, "mgr", "hash", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ob.SetUserState(bg, admin, "mgr", identity.UserDisabled); err != nil {
		t.Fatal(err)
	}
	if st, err := e.ob.SetUserState(bg, admin, "mgr", identity.UserActive); err != nil || st != identity.UserActive {
		t.Fatalf("restore password user: %s %v, want active", st, err)
	}
	if len(*changed) != 6 {
		t.Fatalf("OnUserChanged calls=%d, want 6", len(*changed))
	}
}

// R2（组长 17:56:12Z）：改状态与改类型同一护栏——经理不得停用或恢复平台管理者，也不得改其类型。
func TestA11_PlatformAdminUntouchableByManager(t *testing.T) {
	e, changed := a11Env(t)
	mgr := a11User(t, e, "mgr")
	if _, err := e.ob.SetUserState(bg, mgr, adminID, identity.UserDisabled); !errors.Is(err, identity.ErrNotAllowed) {
		t.Fatalf("manager disables platform admin: err=%v, want ErrNotAllowed", err)
	}
	if err := e.store.SetUserState(bg, adminID, identity.UserDisabled, e.clk.Now()); err != nil { // 造出「平台管理者已停用」
		t.Fatal(err)
	}
	if _, err := e.ob.SetUserState(bg, mgr, adminID, identity.UserActive); !errors.Is(err, identity.ErrNotAllowed) {
		t.Fatalf("manager restores platform admin: err=%v, want ErrNotAllowed", err)
	}
	if err := e.ob.SetUserType(bg, mgr, adminID, identity.UserMember); !errors.Is(err, identity.ErrNotAllowed) {
		t.Fatalf("manager retypes platform admin: err=%v, want ErrNotAllowed", err)
	}
	if u := e.admin(t); u.State != identity.UserDisabled || u.Type != identity.UserPlatformAdmin {
		t.Fatalf("platform admin changed: %+v", u)
	}
	if len(*changed) != 0 {
		t.Fatalf("hook called on refused changes: %v", *changed)
	}
}
