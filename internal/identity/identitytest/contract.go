// Package identitytest 是 identity.Store 的契约测试。
//
// 任何实现（内存、PG）都跑同一套：
//
//	identitytest.Run(t, func(t *testing.T) identity.Store { return newMyStore(t) })
//
// 每个子用例拿一份全新的空存储，与执行顺序无关（业主测试口径「可重现」）。
// 用例名带 spec 判据编号，便于对账。
package identitytest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// Factory 为每个子用例造一份空存储。
type Factory func(t *testing.T) identity.Store

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// Run 跑全部契约用例。
func Run(t *testing.T, newStore Factory) {
	cases := []struct {
		name string
		fn   func(t *testing.T, s identity.Store)
	}{
		{"N3_user_id_and_short_name_conflict_by_lowercase", userConflictLowercase},
		{"N3_first_admin_only_when_no_management_user", firstAdminOnlyOnce},
		{"N3_first_admin_refused_when_only_manager", firstAdminRefusedByManager},
		{"G_group_create_leader_name_code", groupCreateRules},
		{"G_group_join_leave_close", groupJoinLeaveClose},
		{"G_group_communication_creator_only_persisted", groupCommunicationPolicy},
		{"G_group_delete_creator_only_cascades", groupDeleteRules},
		{"G_group_member_rows_survive_revoke", groupMemberAfterRevoke},
		{"G_approve_group_choice_atomic", approveGroupChoice},
		{"G_hosted_group_choice_atomic", hostedGroupChoice},
		{"N3_disable_revokes_sessions", disableRevokesSessions},
		{"A11_set_user_type", setUserType},
		{"N3_get_user_by_short_name_case_insensitive", getUserByShortName},
		{"N3_list_users_sorted_by_lowercase_id", listUsers},
		{"N3_session_expiry_and_inactive_user", sessionExpiryAndInactive},
		{"N3_logout_revokes_only_current_session", logoutCurrentSession},
		{"N3_registration_code_one_shot_binds_identity", regCodeOneShot},
		{"N3_registration_code_expired_or_unknown_changes_nothing", regCodeInvalidChangesNothing},
		{"N3_identity_already_bound_rejected", identityAlreadyBound},
		{"N3_new_registration_code_voids_old", regCodeRegenVoidsOld},
		{"N3_list_active_registration_codes", regCodeListActive},
		{"N3_bind_code_binds_active_user_without_changing_state", bindCodeActiveUser},
		{"V1_A6_pending_list_scoped_to_owner", pendingScopedToOwner},
		{"V1_A7_application_expires_by_clock", applicationExpires},
		{"V1_pending_order_is_deterministic", pendingOrderDeterministic},
		{"V1_A3_A11_name_taken_case_insensitive", nameTakenCaseInsensitive},
		{"V1_A10_same_device_superseded_on_approve", sameDeviceSuperseded},
		{"V1_A2_pairing_failures_reject_at_max", pairingFailuresReject},
		{"V1_A4_A8_credential_taken_once", credentialTakenOnce},
		{"V2_A4_A12_revoke_frees_name_old_credential_stays_revoked", revokeFreesName},
		{"V2_A6_revoke_other_owner_forbidden", revokeOtherOwnerForbidden},
		{"V2_A10_revoke_idempotent", revokeIdempotent},
		{"V2_V8_hosted_not_revocable_in_console_but_retirable", revokeHosted},
		{"V6_hosted_create_and_take", hostedCreateAndTake},
		{"V8_hosted_reissue_rotates_credential", hostedReissueRotatesCredential},
		{"V6_hosted_token_expires", hostedTokenExpires},
		{"V2_A5_find_agent_by_name_prefers_active_then_latest_revoked", findAgentByName},
		{"V8_hosted_suspend_resume_keeps_credential_and_name", hostedSuspendResume},
		{"V8_suspend_records_who_and_when_resume_clears", suspendRecordsWhoWhen},
		{"V2_A5_find_agent_by_name_tie_breaks_by_id", findAgentByNameTie},
		{"G_no_group_choice_joins_nothing", noGroupJoinsNothing},
		{"G_approve_only_into_own_or_member_group", approveOnlyIntoUsableGroup},
		{"N3_reset_password_revokes_only_that_users_sessions", resetPasswordRevokesSessions},
		{"R6_promote_to_platform_admin_atomic_with_audit", promoteToPlatformAdmin},
		{"V2_revoked_before_pickup_no_credential", revokedBeforePickup},
		{"V2_list_agents_scoped_to_owner_case_insensitive", listAgentsOwnerScoped},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) { c.fn(t, newStore(t)) })
	}
}

var ctx = context.Background()

func mustUser(t *testing.T, s identity.Store, id, short string, st identity.UserState) {
	t.Helper()
	if err := s.CreateUser(ctx, identity.User{UserID: id, ShortName: short, Type: identity.UserMember, State: st, CreatedAt: t0}); err != nil {
		t.Fatalf("CreateUser(%s): %v", id, err)
	}
}

func mustApp(t *testing.T, s identity.Store, id, owner, device string, at time.Time) {
	t.Helper()
	err := s.CreateApplication(ctx, identity.Application{
		ID: id, OwnerLC: owner, PairingSalt: "salt-" + id, PairingDigest: "dig-" + id,
		PollTokenHash: "poll-" + id, DeviceFingerprint: device, ClientVersion: "0.3.0",
		SourceIP: "10.0.0.1", RequestedAt: at, ExpiresAt: at.Add(identity.ApplicationTTL), State: identity.AppPending,
	})
	if err != nil {
		t.Fatalf("CreateApplication(%s): %v", id, err)
	}
}

// newGroupFor 夹具：每个 Agent 就地建一个自己当组长的组（组码 gc-<id>、组名 gn-<id>），编组用例据此识别并滤掉夹具组。
func newGroupFor(agentID, owner string, at time.Time) *identity.Group {
	return &identity.Group{Code: "gc-" + agentID, ShortName: "gn-" + agentID, CreatorLC: owner, LeaderAgentID: agentID, CreatedAt: at, UpdatedAt: at}
}

func mustApprove(t *testing.T, s identity.Store, app, owner, agentID, name string, now time.Time) *identity.Agent {
	t.Helper()
	ag, err := s.Approve(ctx, identity.ApproveInput{AppID: app, OwnerLC: owner, AgentID: agentID, Name: name, GroupID: "gc-" + agentID, NewGroup: newGroupFor(agentID, owner, now)}, now)
	if err != nil {
		t.Fatalf("Approve(%s as %s): %v", app, name, err)
	}
	return ag
}

func wantErr(t *testing.T, what string, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("%s: err=%v, want %v", what, got, want)
	}
}

// ── N3 ─────────────────────────────────────────────────────────────────────

func userConflictLowercase(t *testing.T, s identity.Store) {
	mustUser(t, s, "zzc", "zc", identity.UserActive)
	wantErr(t, "same user_id different case", s.CreateUser(ctx, identity.User{UserID: "ZZC", ShortName: "other", Type: identity.UserMember, State: identity.UserActive}), identity.ErrConflict)
	wantErr(t, "same short_name different case", s.CreateUser(ctx, identity.User{UserID: "lbl", ShortName: "ZC", Type: identity.UserMember, State: identity.UserActive}), identity.ErrConflict)
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Fatalf("CountUsers=%d after two rejected creates, want 1", n)
	}
	u, err := s.GetUser(ctx, "ZzC")
	if err != nil || u.UserID != "zzc" || u.ShortName != "zc" {
		t.Fatalf("GetUser by mixed case = %+v, %v", u, err)
	}
}

func disableRevokesSessions(t *testing.T, s identity.Store) {
	mustUser(t, s, "alice", "al", identity.UserActive)
	mustUser(t, s, "bob", "bo", identity.UserActive)
	for _, sess := range []identity.Session{
		{TokenHash: "h-alice-1", UserIDLC: "alice", ExpiresAt: t0.Add(time.Hour)},
		{TokenHash: "h-alice-2", UserIDLC: "alice", ExpiresAt: t0.Add(time.Hour)},
		{TokenHash: "h-bob-1", UserIDLC: "bob", ExpiresAt: t0.Add(time.Hour)},
	} {
		if err := s.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetUserState(ctx, "alice", identity.UserDisabled, t0); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"h-alice-1", "h-alice-2"} {
		_, _, err := s.ResolveSession(ctx, h, t0.Add(time.Minute))
		wantErr(t, "session of disabled user "+h, err, identity.ErrNotFound)
	}
	// 重新启用不得让旧会话复活：吊销是落在会话上的，不只是按用户状态过滤。
	if err := s.SetUserState(ctx, "alice", identity.UserActive, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.ResolveSession(ctx, "h-alice-1", t0.Add(3*time.Minute))
	wantErr(t, "old session after re-enable", err, identity.ErrNotFound)
	if _, u, err := s.ResolveSession(ctx, "h-bob-1", t0.Add(3*time.Minute)); err != nil || u.UserID != "bob" {
		t.Fatalf("other user's session affected: %+v %v", u, err)
	}
}

func sessionExpiryAndInactive(t *testing.T, s identity.Store) {
	mustUser(t, s, "carol", "ca", identity.UserActive)
	mustUser(t, s, "dave", "da", identity.UserPendingBind)
	_ = s.CreateSession(ctx, identity.Session{TokenHash: "h-carol", UserIDLC: "CAROL", ExpiresAt: t0.Add(time.Hour)})
	_ = s.CreateSession(ctx, identity.Session{TokenHash: "h-dave", UserIDLC: "dave", ExpiresAt: t0.Add(time.Hour)})
	if _, u, err := s.ResolveSession(ctx, "h-carol", t0.Add(59*time.Minute)); err != nil || u.UserID != "carol" {
		t.Fatalf("valid session: %+v %v", u, err)
	}
	_, _, err := s.ResolveSession(ctx, "h-carol", t0.Add(time.Hour))
	wantErr(t, "expired session", err, identity.ErrNotFound)
	_, _, err = s.ResolveSession(ctx, "h-dave", t0)
	wantErr(t, "session of pending_bind user", err, identity.ErrNotFound)
}

var feishuA = identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_aaaa"}
var feishuB = identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_bbbb"}

func regCodeOneShot(t *testing.T, s identity.Store) {
	mustUser(t, s, "erin", "er", identity.UserPendingBind)
	if err := s.CreateRegistrationCode(ctx, "ERIN", "code-erin", t0.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	u, err := s.RedeemRegistrationCode(ctx, "code-erin", feishuA, t0.Add(time.Hour))
	if err != nil || u.UserID != "erin" || u.State != identity.UserActive {
		t.Fatalf("redeem: %+v %v", u, err)
	}
	if got, err := s.ResolveIdentity(ctx, feishuA); err != nil || got.UserID != "erin" {
		t.Fatalf("ResolveIdentity after bind: %+v %v", got, err)
	}
	if got, err := s.GetIdentity(ctx, "ERIN", "feishu"); err != nil || *got != feishuA {
		t.Fatalf("GetIdentity after bind: %+v %v", got, err)
	}
	if _, err := s.GetIdentity(ctx, "erin", "unknown"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("GetIdentity unknown provider: %v", err)
	}
	_, err = s.RedeemRegistrationCode(ctx, "code-erin", feishuB, t0.Add(2*time.Hour))
	wantErr(t, "second redeem of same code", err, identity.ErrCodeInvalid)
	_, err = s.ResolveIdentity(ctx, feishuB)
	wantErr(t, "identity B must stay unbound", err, identity.ErrNotFound)
}

func regCodeInvalidChangesNothing(t *testing.T, s identity.Store) {
	mustUser(t, s, "frank", "fr", identity.UserPendingBind)
	_ = s.CreateRegistrationCode(ctx, "frank", "code-frank", t0.Add(24*time.Hour))
	_, err := s.RedeemRegistrationCode(ctx, "code-frank", feishuA, t0.Add(24*time.Hour))
	wantErr(t, "redeem at expiry instant", err, identity.ErrCodeInvalid)
	_, err = s.RedeemRegistrationCode(ctx, "no-such-code", feishuA, t0)
	wantErr(t, "unknown code", err, identity.ErrCodeInvalid)
	u, _ := s.GetUser(ctx, "frank")
	if u.State != identity.UserPendingBind {
		t.Fatalf("user state changed by failed redeem: %s", u.State)
	}
	if _, err := s.ResolveIdentity(ctx, feishuA); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("identity bound by failed redeem: %v", err)
	}
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Fatalf("CountUsers=%d, want 1 (no auto-create)", n)
	}
}

func identityAlreadyBound(t *testing.T, s identity.Store) {
	mustUser(t, s, "gina", "gi", identity.UserPendingBind)
	mustUser(t, s, "hank", "ha", identity.UserPendingBind)
	_ = s.CreateRegistrationCode(ctx, "gina", "code-gina", t0.Add(24*time.Hour))
	_ = s.CreateRegistrationCode(ctx, "hank", "code-hank", t0.Add(24*time.Hour))
	if _, err := s.RedeemRegistrationCode(ctx, "code-gina", feishuA, t0); err != nil {
		t.Fatal(err)
	}
	_, err := s.RedeemRegistrationCode(ctx, "code-hank", feishuA, t0)
	wantErr(t, "identity A reused for hank", err, identity.ErrIdentityTaken)
	u, _ := s.GetUser(ctx, "hank")
	if u.State != identity.UserPendingBind {
		t.Fatalf("hank state=%s, want pending_bind", u.State)
	}
	// 码在失败时没有被消耗：hank 换一个身份仍可用同一码。
	if _, err := s.RedeemRegistrationCode(ctx, "code-hank", feishuB, t0); err != nil {
		t.Fatalf("code consumed by failed redeem: %v", err)
	}
}

func regCodeRegenVoidsOld(t *testing.T, s identity.Store) {
	mustUser(t, s, "ivy", "iv", identity.UserPendingBind)
	_ = s.CreateRegistrationCode(ctx, "ivy", "code-ivy-1", t0.Add(24*time.Hour))
	_ = s.CreateRegistrationCode(ctx, "ivy", "code-ivy-2", t0.Add(24*time.Hour))
	_, err := s.RedeemRegistrationCode(ctx, "code-ivy-1", feishuA, t0)
	wantErr(t, "old code after regenerate", err, identity.ErrCodeInvalid)
	if _, err := s.RedeemRegistrationCode(ctx, "code-ivy-2", feishuA, t0); err != nil {
		t.Fatalf("new code: %v", err)
	}
}

// 当前有效注册码（业主反馈，板 7a138dcf：用户列表一直显示到绑定、过期或重新生成）：
// 只列未用、未过期的；新码顶替旧码；只出哈希，不出明文（库里本就没有明文）。
func regCodeListActive(t *testing.T, s identity.Store) {
	mustUser(t, s, "jack", "jk", identity.UserPendingBind)
	mustUser(t, s, "kate", "kt", identity.UserPendingBind)
	exp := t0.Add(24 * time.Hour)
	_ = s.CreateRegistrationCode(ctx, "JACK", "code-jack-1", exp)
	_ = s.CreateRegistrationCode(ctx, "jack", "code-jack-2", exp.Add(time.Minute)) // 顶替 1
	_ = s.CreateRegistrationCode(ctx, "kate", "code-kate", t0.Add(time.Hour))
	digests := func(now time.Time) map[string]string {
		rows, err := s.ListActiveRegistrationCodes(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, r := range rows {
			if _, dup := m[r.UserIDLC]; dup {
				t.Fatalf("user %s listed twice", r.UserIDLC)
			}
			m[r.UserIDLC] = r.CodeDigest
		}
		return m
	}
	if got := digests(t0); got["jack"] != "code-jack-2" || got["kate"] != "code-kate" || len(got) != 2 {
		t.Fatalf("active at t0: %v, want jack→code-jack-2 kate→code-kate", got)
	}
	rows, _ := s.ListActiveRegistrationCodes(ctx, t0)
	for _, r := range rows {
		if r.UserIDLC == "jack" && !r.ExpiresAt.Equal(exp.Add(time.Minute)) {
			t.Fatalf("expires_at %v, want %v", r.ExpiresAt, exp.Add(time.Minute))
		}
	}
	// kate 的码过期 ⇒ 不列
	if got := digests(t0.Add(2 * time.Hour)); got["kate"] != "" || got["jack"] != "code-jack-2" {
		t.Fatalf("after kate's code expired: %v", got)
	}
	// jack 绑定（码已用）⇒ 不列
	if _, err := s.RedeemRegistrationCode(ctx, "code-jack-2", feishuA, t0); err != nil {
		t.Fatal(err)
	}
	if got := digests(t0); got["jack"] != "" {
		t.Fatalf("redeemed code still listed: %v", got)
	}
}

// 本人自取的飞书绑定码（组长 e483f147）：active 且尚无该渠道身份 ⇒ 只绑身份、状态不变；
// 已有该渠道身份 ⇒ ErrAlreadyBound；停用 ⇒ ErrNotPending。
func bindCodeActiveUser(t *testing.T, s identity.Store) {
	mustUser(t, s, "liam", "lm", identity.UserActive)
	mustUser(t, s, "mona", "mo", identity.UserDisabled)
	if has, err := s.UserHasIdentity(ctx, "LIAM", "feishu"); err != nil || has {
		t.Fatalf("before bind: has=%v err=%v", has, err)
	}
	_ = s.CreateRegistrationCode(ctx, "liam", "code-liam-1", t0.Add(24*time.Hour))
	u, err := s.RedeemRegistrationCode(ctx, "code-liam-1", feishuA, t0)
	if err != nil || u.State != identity.UserActive {
		t.Fatalf("bind active user: %+v %v", u, err)
	}
	if got, err := s.ResolveIdentity(ctx, feishuA); err != nil || got.UserIDLC() != "liam" {
		t.Fatalf("identity bound to %+v %v, want liam", got, err)
	}
	if got, _ := s.GetUser(ctx, "liam"); got.State != identity.UserActive {
		t.Fatalf("state changed by bind: %s", got.State)
	}
	if has, _ := s.UserHasIdentity(ctx, "liam", "feishu"); !has {
		t.Fatal("UserHasIdentity after bind")
	}
	// 已有飞书身份：第二个码拒绝（不论用哪个飞书身份来兑）
	_ = s.CreateRegistrationCode(ctx, "liam", "code-liam-2", t0.Add(24*time.Hour))
	other := identity.Identity{Provider: "feishu", Issuer: "devplatform", Subject: "ou_other"}
	_, err = s.RedeemRegistrationCode(ctx, "code-liam-2", other, t0)
	wantErr(t, "already bound", err, identity.ErrAlreadyBound)
	// 停用用户：不绑
	_ = s.CreateRegistrationCode(ctx, "mona", "code-mona", t0.Add(24*time.Hour))
	_, err = s.RedeemRegistrationCode(ctx, "code-mona", other, t0)
	wantErr(t, "disabled user", err, identity.ErrNotPending)
}

// ── V1 ─────────────────────────────────────────────────────────────────────

func pendingScopedToOwner(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-z1", "zzc", "dev-1", t0)
	mustApp(t, s, "app-z2", "ZZC", "dev-2", t0.Add(time.Minute))
	mustApp(t, s, "app-l1", "lbl", "dev-3", t0)
	got, _ := s.ListPendingApplications(ctx, "zzc", t0.Add(2*time.Minute))
	if len(got) != 2 || got[0].ID != "app-z1" || got[1].ID != "app-z2" {
		t.Fatalf("zzc pending = %v, want [app-z1 app-z2]", ids(got))
	}
	got, _ = s.ListPendingApplications(ctx, "lbl", t0.Add(2*time.Minute))
	if len(got) != 1 || got[0].ID != "app-l1" {
		t.Fatalf("lbl pending = %v, want [app-l1]", ids(got))
	}
	// 别人的申请当作不存在：不能被另一业主批准。
	_, err := s.Approve(ctx, identity.ApproveInput{AppID: "app-l1", OwnerLC: "zzc", AgentID: "ag-x", Name: "Steal"}, t0)
	wantErr(t, "approve other owner's application", err, identity.ErrNotFound)
}

func applicationExpires(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-e", "zzc", "dev-e", t0)
	if got, _ := s.ListPendingApplications(ctx, "zzc", t0.Add(identity.ApplicationTTL-time.Second)); len(got) != 1 {
		t.Fatalf("1s before expiry: %v", ids(got))
	}
	if got, _ := s.ListPendingApplications(ctx, "zzc", t0.Add(identity.ApplicationTTL)); len(got) != 0 {
		t.Fatalf("at expiry: %v, want none", ids(got))
	}
	a, _ := s.GetApplication(ctx, "app-e")
	if st := a.EffectiveState(t0.Add(identity.ApplicationTTL)); st != identity.AppExpired {
		t.Fatalf("EffectiveState=%s, want expired", st)
	}
	_, err := s.Approve(ctx, identity.ApproveInput{AppID: "app-e", OwnerLC: "zzc", AgentID: "ag-e", Name: "Late"}, t0.Add(identity.ApplicationTTL))
	wantErr(t, "approve expired", err, identity.ErrExpired)
}

func nameTakenCaseInsensitive(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-1", "zzc", "dev-1", t0)
	mustApp(t, s, "app-2", "zzc", "dev-2", t0)
	mustApp(t, s, "app-3", "lbl", "dev-3", t0)
	mustApprove(t, s, "app-1", "zzc", "ag-1", "opus50ts", t0)
	_, err := s.Approve(ctx, identity.ApproveInput{AppID: "app-2", OwnerLC: "zzc", AgentID: "ag-2", Name: "opus50TS"}, t0)
	wantErr(t, "opus50TS vs opus50ts same owner", err, identity.ErrNameTaken)
	if a, _ := s.GetApplication(ctx, "app-2"); a.State != identity.AppPending {
		t.Fatalf("app-2 state=%s after name clash, want pending (owner retries with new name)", a.State)
	}
	ag := mustApprove(t, s, "app-2", "zzc", "ag-2", "opus50tc", t0)
	if ag.Name != "opus50tc" {
		t.Fatalf("renamed approve: %+v", ag)
	}
	// 只需在本业主名下唯一：另一业主可用同名。
	mustApprove(t, s, "app-3", "lbl", "ag-3", "Opus50TS", t0)
}

func sameDeviceSuperseded(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-a", "zzc", "dev-same", t0)
	mustApp(t, s, "app-b", "zzc", "dev-same", t0.Add(time.Minute))
	mustApp(t, s, "app-c", "zzc", "dev-other", t0)
	mustApp(t, s, "app-d", "lbl", "dev-same", t0) // 另一业主同设备不受影响
	mustApprove(t, s, "app-b", "zzc", "ag-b", "one", t0.Add(2*time.Minute))
	want := map[string]identity.AppState{"app-a": identity.AppSuperseded, "app-b": identity.AppApproved, "app-c": identity.AppPending, "app-d": identity.AppPending}
	for id, st := range want {
		a, _ := s.GetApplication(ctx, id)
		if a.State != st {
			t.Fatalf("%s state=%s, want %s", id, a.State, st)
		}
	}
	if got, _ := s.ListPendingApplications(ctx, "zzc", t0.Add(3*time.Minute)); len(got) != 1 || got[0].ID != "app-c" {
		t.Fatalf("zzc pending after approve = %v, want [app-c]", ids(got))
	}
}

func pairingFailuresReject(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-p", "zzc", "dev-p", t0)
	for i, wantRemain := range []int{4, 3, 2, 1} {
		rem, rej, err := s.RecordPairingFailure(ctx, "app-p", "zzc", 5, t0)
		if err != nil || rej || rem != wantRemain {
			t.Fatalf("failure %d: remaining=%d rejected=%v err=%v, want %d false nil", i+1, rem, rej, err, wantRemain)
		}
	}
	if a, _ := s.GetApplication(ctx, "app-p"); a.State != identity.AppPending {
		t.Fatalf("after 4 failures state=%s, want pending (A2: wrong code keeps application)", a.State)
	}
	rem, rej, err := s.RecordPairingFailure(ctx, "app-p", "zzc", 5, t0)
	if err != nil || !rej || rem != 0 {
		t.Fatalf("5th failure: remaining=%d rejected=%v err=%v", rem, rej, err)
	}
	_, _, err = s.RecordPairingFailure(ctx, "app-p", "zzc", 5, t0)
	wantErr(t, "failure after rejected", err, identity.ErrNotPending)
	_, _, err = s.RecordPairingFailure(ctx, "app-p", "lbl", 5, t0)
	wantErr(t, "failure by other owner", err, identity.ErrNotFound)
}

func credentialTakenOnce(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-c", "zzc", "dev-c", t0)
	if _, ok, err := s.TakeCredential(ctx, "app-c", "cred-early", t0); ok || err != nil {
		t.Fatalf("take before approve: ok=%v err=%v, want false nil", ok, err)
	}
	mustApprove(t, s, "app-c", "zzc", "ag-c", "worker1", t0)
	ag, ok, err := s.TakeCredential(ctx, "app-c", "cred-c1", t0.Add(time.Second))
	if err != nil || !ok || ag.ID != "ag-c" || ag.CredentialHash != "cred-c1" {
		t.Fatalf("first take: %+v ok=%v err=%v", ag, ok, err)
	}
	if _, ok, err := s.TakeCredential(ctx, "app-c", "cred-c2", t0.Add(2*time.Second)); ok || err != nil {
		t.Fatalf("second take: ok=%v err=%v, want false nil", ok, err)
	}
	got, err := s.ResolveAgentByCredential(ctx, "cred-c1")
	if err != nil || got.ID != "ag-c" || got.Name != "worker1" || got.OwnerLC != "zzc" {
		t.Fatalf("resolve cred-c1: %+v %v", got, err)
	}
	_, err = s.ResolveAgentByCredential(ctx, "cred-c2")
	wantErr(t, "second credential must not exist", err, identity.ErrNotFound)
	if list, _ := s.ListAgents(ctx, "zzc"); len(list) != 1 || list[0].ID != "ag-c" {
		t.Fatalf("ListAgents = %v", list)
	}
}

// ── V2 ─────────────────────────────────────────────────────────────────────

func approveAndTake(t *testing.T, s identity.Store, app, owner, device, agentID, name, cred string, at time.Time) {
	t.Helper()
	mustApp(t, s, app, owner, device, at)
	mustApprove(t, s, app, owner, agentID, name, at)
	if _, ok, err := s.TakeCredential(ctx, app, cred, at); !ok || err != nil {
		t.Fatalf("take %s: ok=%v err=%v", app, ok, err)
	}
}

func revokeFreesName(t *testing.T, s identity.Store) {
	approveAndTake(t, s, "app-old", "zzc", "dev-1", "ag-old", "Kimi", "cred-old", t0)
	already, err := s.Revoke(ctx, "ag-old", "zzc", "zzc", t0.Add(time.Minute))
	if err != nil || already {
		t.Fatalf("revoke: already=%v err=%v", already, err)
	}
	if list, _ := s.ListAgents(ctx, "zzc"); len(list) != 0 {
		t.Fatalf("revoked agent still listed: %v", list)
	}
	if g, err := s.GetAgent(ctx, "ag-old"); err != nil || g.State != identity.AgentRevoked || g.Name != "Kimi" || g.OwnerLC != "zzc" {
		t.Fatalf("GetAgent of revoked: %+v %v", g, err)
	}
	if _, err := s.GetAgent(ctx, "ag-none"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("GetAgent unknown: %v", err)
	}
	old, err := s.ResolveAgentByCredential(ctx, "cred-old")
	if err != nil || old.State != identity.AgentRevoked || old.RevokedBy != "zzc" || !old.RevokedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("old credential must resolve to revoked row: %+v %v", old, err)
	}
	approveAndTake(t, s, "app-new", "zzc", "dev-1", "ag-new", "kimi", "cred-new", t0.Add(2*time.Minute))
	nw, _ := s.ResolveAgentByCredential(ctx, "cred-new")
	if nw.ID != "ag-new" || nw.State != identity.AgentActive || nw.GroupID != "gc-ag-new" {
		t.Fatalf("new agent: %+v", nw)
	}
	if old2, _ := s.ResolveAgentByCredential(ctx, "cred-old"); old2.ID != "ag-old" || old2.State != identity.AgentRevoked {
		t.Fatalf("old credential now resolves to %+v, want ag-old revoked", old2)
	}
}

func revokeOtherOwnerForbidden(t *testing.T, s identity.Store) {
	approveAndTake(t, s, "app-z", "zzc", "dev-z", "ag-z", "mine", "cred-z", t0)
	_, err := s.Revoke(ctx, "ag-z", "lbl", "lbl", t0)
	wantErr(t, "revoke by other owner", err, identity.ErrForbidden)
	if ag, _ := s.ResolveAgentByCredential(ctx, "cred-z"); ag.State != identity.AgentActive {
		t.Fatalf("state=%s after forbidden revoke", ag.State)
	}
	_, err = s.Revoke(ctx, "ag-none", "zzc", "zzc", t0)
	wantErr(t, "revoke unknown", err, identity.ErrNotFound)
}

func revokeIdempotent(t *testing.T, s identity.Store) {
	approveAndTake(t, s, "app-i", "zzc", "dev-i", "ag-i", "twice", "cred-i", t0)
	if already, err := s.Revoke(ctx, "ag-i", "zzc", "zzc", t0.Add(time.Minute)); already || err != nil {
		t.Fatalf("first revoke: %v %v", already, err)
	}
	if already, err := s.Revoke(ctx, "ag-i", "zzc", "someone", t0.Add(2*time.Minute)); !already || err != nil {
		t.Fatalf("second revoke: already=%v err=%v, want true nil", already, err)
	}
	ag, _ := s.ResolveAgentByCredential(ctx, "cred-i")
	if !ag.RevokedAt.Equal(t0.Add(time.Minute)) || ag.RevokedBy != "zzc" {
		t.Fatalf("second revoke overwrote fields: %+v", ag)
	}
}

func mustHosted(t *testing.T, s identity.Store, agentID, owner, name, app string, at time.Time) {
	t.Helper()
	err := s.CreateHostedAgent(ctx, identity.Agent{ID: agentID, OwnerLC: owner, Name: name, GroupID: "gc-" + agentID, CreatedAt: at},
		identity.Application{ID: app, PollTokenHash: "tok-" + app, RequestedAt: at, ExpiresAt: at.Add(time.Hour)}, newGroupFor(agentID, owner, at))
	if err != nil {
		t.Fatalf("CreateHostedAgent(%s): %v", name, err)
	}
}

func revokeHosted(t *testing.T, s identity.Store) {
	mustHosted(t, s, "ag-h", "zzc", "hosted1", "app-h", t0)
	_, err := s.Revoke(ctx, "ag-h", "zzc", "zzc", t0)
	wantErr(t, "console revoke of hosted", err, identity.ErrHosted)
	if already, err := s.RetireAgent(ctx, "ag-h", "g4", t0.Add(time.Minute)); already || err != nil {
		t.Fatalf("retire: %v %v", already, err)
	}
	if already, err := s.RetireAgent(ctx, "ag-h", "other", t0.Add(2*time.Minute)); !already || err != nil {
		t.Fatalf("second retire: already=%v err=%v", already, err)
	}
	g, _ := s.GetAgent(ctx, "ag-h")
	if g.State != identity.AgentRevoked || g.RevokedBy != "g4" || !g.RevokedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("retired agent: %+v", g)
	}
}

func hostedCreateAndTake(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-self", "zzc", "dev-1", t0)
	mustApprove(t, s, "app-self", "zzc", "ag-self", "Worker", t0)
	err := s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-h1", OwnerLC: "zzc", Name: "WORKER", GroupID: "gc-ag-h1"},
		identity.Application{ID: "app-h1", PollTokenHash: "tok-h1", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)}, newGroupFor("ag-h1", "zzc", t0))
	wantErr(t, "hosted name clashes with selfhost (case-insensitive)", err, identity.ErrNameTaken)
	if _, err := s.GetApplication(ctx, "app-h1"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("failed create must leave no application: %v", err)
	}
	mustHosted(t, s, "ag-h2", "ZZC", "Codex1", "app-h2", t0)
	a, _ := s.GetApplication(ctx, "app-h2")
	if a.State != identity.AppApproved || a.AgentID != "ag-h2" || a.OwnerLC != "zzc" {
		t.Fatalf("hosted application: %+v", a)
	}
	ag, ok, err := s.TakeCredential(ctx, "app-h2", "cred-h2", t0.Add(time.Minute))
	if err != nil || !ok || ag.Origin != identity.OriginHosted || ag.Name != "Codex1" || ag.GroupID != "gc-ag-h2" {
		t.Fatalf("take hosted: %+v %v %v", ag, ok, err)
	}
	if list, _ := s.ListAgents(ctx, "zzc"); len(list) != 2 {
		t.Fatalf("ListAgents = %d, want selfhost＋hosted", len(list))
	}
}

func hostedReissueRotatesCredential(t *testing.T, s identity.Store) {
	mustHosted(t, s, "ag-r", "zzc", "Rebuilt", "app-r1", t0)
	if _, ok, _ := s.TakeCredential(ctx, "app-r1", "cred-old", t0); !ok {
		t.Fatal("first take")
	}
	if err := s.AddHostedToken(ctx, "ag-r", identity.Application{ID: "app-r2", PollTokenHash: "tok-r2", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveAgentByCredential(ctx, "cred-old"); err != nil {
		t.Fatalf("old credential must stay valid until the new one is taken: %v", err)
	}
	if _, ok, _ := s.TakeCredential(ctx, "app-r2", "cred-new", t0.Add(time.Minute)); !ok {
		t.Fatal("take reissued")
	}
	_, err := s.ResolveAgentByCredential(ctx, "cred-old")
	wantErr(t, "old credential after rotation", err, identity.ErrNotFound)
	if g, err := s.ResolveAgentByCredential(ctx, "cred-new"); err != nil || g.ID != "ag-r" || g.State != identity.AgentActive {
		t.Fatalf("new credential: %+v %v", g, err)
	}
	mustApp(t, s, "app-s", "zzc", "dev-s", t0)
	mustApprove(t, s, "app-s", "zzc", "ag-s", "Selfie", t0)
	err = s.AddHostedToken(ctx, "ag-s", identity.Application{ID: "app-x", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)})
	wantErr(t, "reissue for selfhost agent", err, identity.ErrNotHosted)
	_, _ = s.RetireAgent(ctx, "ag-r", "g4", t0)
	err = s.AddHostedToken(ctx, "ag-r", identity.Application{ID: "app-r3", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)})
	wantErr(t, "reissue for retired agent", err, identity.ErrNotPending)
}

func hostedTokenExpires(t *testing.T, s identity.Store) {
	mustHosted(t, s, "ag-e", "zzc", "Late", "app-e", t0)
	if _, ok, err := s.TakeCredential(ctx, "app-e", "cred-e", t0.Add(time.Hour)); ok || err != nil {
		t.Fatalf("take at expiry: ok=%v err=%v, want false nil", ok, err)
	}
	if _, ok, _ := s.TakeCredential(ctx, "app-e", "cred-e", t0.Add(time.Hour-time.Second)); !ok {
		t.Fatal("take 1s before expiry")
	}
}

func revokedBeforePickup(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-r", "zzc", "dev-r", t0)
	mustApprove(t, s, "app-r", "zzc", "ag-r", "early", t0)
	if _, err := s.Revoke(ctx, "ag-r", "zzc", "zzc", t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.TakeCredential(ctx, "app-r", "cred-r", t0.Add(2*time.Second)); ok || err != nil {
		t.Fatalf("take after revoke: ok=%v err=%v, want false nil", ok, err)
	}
	_, err := s.ResolveAgentByCredential(ctx, "cred-r")
	wantErr(t, "credential of revoked-before-pickup agent", err, identity.ErrNotFound)
}

func ids(as []identity.Application) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.ID)
	}
	return out
}

// 同一时刻的申请按 application_id 升序：审核区的顺序必须确定，否则界面每次刷新都可能换序。
func pendingOrderDeterministic(t *testing.T, s identity.Store) {
	for _, id := range []string{"app-c", "app-a", "app-b"} {
		mustApp(t, s, id, "zzc", "dev-"+id, t0)
	}
	mustApp(t, s, "app-0", "zzc", "dev-0", t0.Add(time.Minute))
	for i := 0; i < 20; i++ {
		got, _ := s.ListPendingApplications(ctx, "zzc", t0.Add(2*time.Minute))
		if strings.Join(ids(got), ",") != "app-a,app-b,app-c,app-0" {
			t.Fatalf("order = %v, want [app-a app-b app-c app-0]", ids(got))
		}
	}
}

func findAgentByName(t *testing.T, s identity.Store) {
	_, err := s.FindAgentByName(ctx, "zzc", "kimi")
	wantErr(t, "never existed", err, identity.ErrNotFound)
	approveAndTake(t, s, "app-1", "zzc", "dev-1", "ag-1", "Kimi", "cred-1", t0)
	_, _ = s.Revoke(ctx, "ag-1", "zzc", "zzc", t0.Add(time.Minute))
	approveAndTake(t, s, "app-2", "zzc", "dev-2", "ag-2", "kimi", "cred-2", t0.Add(2*time.Minute))
	_, _ = s.Revoke(ctx, "ag-2", "zzc", "zzc", t0.Add(3*time.Minute))
	g, err := s.FindAgentByName(ctx, "ZZC", "KIMI")
	if err != nil || g.ID != "ag-2" || g.State != identity.AgentRevoked {
		t.Fatalf("latest revoked: %+v %v, want ag-2 revoked", g, err)
	}
	approveAndTake(t, s, "app-3", "zzc", "dev-3", "ag-3", "KIMI", "cred-3", t0.Add(4*time.Minute))
	if g, _ := s.FindAgentByName(ctx, "zzc", "kimi"); g.ID != "ag-3" || g.State != identity.AgentActive {
		t.Fatalf("active must win over revoked: %+v", g)
	}
	if _, err := s.FindAgentByName(ctx, "lbl", "kimi"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("other owner: %v", err)
	}
}

func hostedSuspendResume(t *testing.T, s identity.Store) {
	mustHosted(t, s, "ag-p", "zzc", "Pausy", "app-p", t0)
	if _, ok, _ := s.TakeCredential(ctx, "app-p", "cred-p", t0); !ok {
		t.Fatal("take")
	}
	if already, err := s.SetAgentSuspended(ctx, "ag-p", true, "g4", t0.Add(time.Minute)); already || err != nil {
		t.Fatalf("suspend: %v %v", already, err)
	}
	if already, err := s.SetAgentSuspended(ctx, "ag-p", true, "g4", t0.Add(2*time.Minute)); !already || err != nil {
		t.Fatalf("second suspend: already=%v err=%v", already, err)
	}
	g, err := s.ResolveAgentByCredential(ctx, "cred-p")
	if err != nil || g.State != identity.AgentSuspended || g.CredentialHash != "cred-p" {
		t.Fatalf("suspended agent by credential: %+v %v (credential must be kept)", g, err)
	}
	// 暂停中仍占名字、仍在列表里
	err = s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-p2", OwnerLC: "zzc", Name: "PAUSY", GroupID: "gc-ag-p2"},
		identity.Application{ID: "app-p2", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)}, newGroupFor("ag-p2", "zzc", t0))
	wantErr(t, "name of suspended agent reused", err, identity.ErrNameTaken)
	if list, _ := s.ListAgents(ctx, "zzc"); len(list) != 1 || list[0].ID != "ag-p" || list[0].State != identity.AgentSuspended {
		t.Fatalf("ListAgents while suspended: %+v", list)
	}
	if f, _ := s.FindAgentByName(ctx, "zzc", "pausy"); f.ID != "ag-p" || f.State != identity.AgentSuspended {
		t.Fatalf("FindAgentByName while suspended: %+v", f)
	}
	// 暂停中不能领新凭据（重签被拒）
	err = s.AddHostedToken(ctx, "ag-p", identity.Application{ID: "app-p3", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)})
	wantErr(t, "reissue while suspended", err, identity.ErrNotPending)
	if already, err := s.SetAgentSuspended(ctx, "ag-p", false, "g4", t0.Add(3*time.Minute)); already || err != nil {
		t.Fatalf("resume: %v %v", already, err)
	}
	if g, _ := s.ResolveAgentByCredential(ctx, "cred-p"); g.State != identity.AgentActive || g.CredentialHash != "cred-p" {
		t.Fatalf("after resume the same credential must work: %+v", g)
	}
	if already, _ := s.SetAgentSuspended(ctx, "ag-p", false, "g4", t0); !already {
		t.Fatal("second resume must be already=true")
	}
	// 暂停中可直接销毁；销毁后不能再暂停／恢复
	_, _ = s.SetAgentSuspended(ctx, "ag-p", true, "g4", t0.Add(4*time.Minute))
	if already, err := s.RetireAgent(ctx, "ag-p", "g4", t0.Add(5*time.Minute)); already || err != nil {
		t.Fatalf("retire from suspended: %v %v", already, err)
	}
	_, err = s.SetAgentSuspended(ctx, "ag-p", false, "g4", t0)
	wantErr(t, "resume after retire", err, identity.ErrNotPending)
	// 自备席位不能暂停
	mustApp(t, s, "app-s", "zzc", "dev-s", t0)
	mustApprove(t, s, "app-s", "zzc", "ag-s", "Selfie", t0)
	_, err = s.SetAgentSuspended(ctx, "ag-s", true, "g4", t0)
	wantErr(t, "suspend selfhost", err, identity.ErrNotHosted)
	_, err = s.SetAgentSuspended(ctx, "ag-none", true, "g4", t0)
	wantErr(t, "suspend unknown", err, identity.ErrNotFound)
}

func getUserByShortName(t *testing.T, s identity.Store) {
	mustUser(t, s, "Alice", "Al", identity.UserActive)
	mustUser(t, s, "bob", "bb", identity.UserActive)
	if u, err := s.GetUserByShortName(ctx, "AL"); err != nil || u.UserID != "Alice" {
		t.Fatalf("short AL: %+v %v", u, err)
	}
	if u, err := s.GetUserByShortName(ctx, "bb"); err != nil || u.UserID != "bob" {
		t.Fatalf("short bb: %+v %v", u, err)
	}
	_, err := s.GetUserByShortName(ctx, "alice") // 用户 ID 不是简称
	wantErr(t, "user id used as short name", err, identity.ErrNotFound)
}

func listUsers(t *testing.T, s identity.Store) {
	if got, err := s.ListUsers(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty store: %v %v", got, err)
	}
	mustUser(t, s, "carol", "ca", identity.UserActive)
	mustUser(t, s, "Alice", "al", identity.UserPendingBind)
	mustUser(t, s, "bob", "bo", identity.UserActive)
	got, err := s.ListUsers(ctx)
	if err != nil || len(got) != 3 {
		t.Fatalf("ListUsers: %v %v", got, err)
	}
	var ids []string
	for _, u := range got {
		ids = append(ids, u.UserID+"/"+string(u.State))
	}
	if strings.Join(ids, ",") != "Alice/pending_bind,bob/active,carol/active" {
		t.Fatalf("order/fields = %v", ids)
	}
}

// 初始化脚本建第一个管理用户（台账 8971bddf）：库中已有 platform_admin 或 manager（不论状态）⇒ ErrAdminExists 且不插入；
// 只有普通用户时可建；用户 ID／简称冲突 ⇒ ErrConflict。
func firstAdminOnlyOnce(t *testing.T, s identity.Store) {
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	admin := func(id, short string) identity.User {
		return identity.User{UserID: id, ShortName: short, Type: identity.UserPlatformAdmin, State: identity.UserActive,
			PasswordHash: "h-" + id, MustChangePassword: true, CreatedAt: t0}
	}
	mustUser(t, s, "member1", "m1", identity.UserActive)
	wantErr(t, "user id taken by member", s.CreateFirstAdmin(ctx, admin("MEMBER1", "zz")), identity.ErrConflict)
	wantErr(t, "short name taken by member", s.CreateFirstAdmin(ctx, admin("fresh", "M1")), identity.ErrConflict)
	if err := s.CreateFirstAdmin(ctx, admin("Boss", "bs")); err != nil {
		t.Fatalf("first admin with only members present: %v", err)
	}
	u, err := s.GetUser(ctx, "boss")
	if err != nil || u.Type != identity.UserPlatformAdmin || u.State != identity.UserActive || !u.MustChangePassword || u.PasswordHash != "h-Boss" {
		t.Fatalf("stored admin: %+v %v", u, err)
	}
	wantErr(t, "second admin", s.CreateFirstAdmin(ctx, admin("Boss2", "b2")), identity.ErrAdminExists)
	if _, err := s.GetUser(ctx, "boss2"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("refused admin was inserted: %v", err)
	}
	// 已有的管理用户被停用，或只有 manager：仍算「已有管理用户」
	if err := s.SetUserState(ctx, "boss", identity.UserDisabled, t0); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "admin disabled", s.CreateFirstAdmin(ctx, admin("Boss3", "b3")), identity.ErrAdminExists)
	if n, _ := s.CountUsers(ctx); n != 2 {
		t.Fatalf("CountUsers=%d, want 2", n)
	}
}

// 库里只有一个 manager（尚未绑定）也算「已有管理用户」。
func firstAdminRefusedByManager(t *testing.T, s identity.Store) {
	if err := s.CreateUser(ctx, identity.User{UserID: "mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserPendingBind, CreatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	err := s.CreateFirstAdmin(ctx, identity.User{UserID: "Boss", ShortName: "bs", Type: identity.UserPlatformAdmin, State: identity.UserActive, CreatedAt: t0})
	wantErr(t, "manager exists", err, identity.ErrAdminExists)
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Fatalf("CountUsers=%d, want 1", n)
	}
}

// ── 编组（组长裁定 132246Z）────────────────────────────────────────────────

func groupFixture(t *testing.T, s identity.Store) {
	t.Helper()
	mustUser(t, s, "zzc", "zz", identity.UserActive)
	mustUser(t, s, "lbl", "lb", identity.UserActive)
	mustUser(t, s, "mmm", "mm", identity.UserActive)
	mustHosted(t, s, "ag-z", "zzc", "Zed", "app-z", t0)
	mustHosted(t, s, "ag-z2", "zzc", "Zed2", "app-z2", t0)
	mustHosted(t, s, "ag-l", "lbl", "Lee", "app-l", t0)
	mustHosted(t, s, "ag-m", "mmm", "Moe", "app-m", t0)
}

func mkGroup(code, name, creator, leader string, at time.Time) identity.Group {
	return identity.Group{Code: code, ShortName: name, CreatorLC: creator, LeaderAgentID: leader, CreatedAt: at, UpdatedAt: at}
}

func memberIDs(t *testing.T, s identity.Store, code string) string {
	t.Helper()
	ms, err := s.ListGroupMembers(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.AgentID)
	}
	return strings.Join(ids, ",")
}

// 建组：组长须属建组人且未吊销；组名全局唯一、区分大小写；组码唯一；组长自动入组；列表顺序确定。
func groupCreateRules(t *testing.T, s identity.Store) {
	groupFixture(t, s)
	mustHosted(t, s, "ag-r", "zzc", "Rev", "app-r", t0)
	if _, err := s.RetireAgent(ctx, "ag-r", "test", t0); err != nil {
		t.Fatal(err)
	}
	for what, leader := range map[string]string{"other owner's agent": "ag-l", "revoked agent": "ag-r", "no such agent": "ag-none"} {
		wantErr(t, what, s.CreateGroup(ctx, mkGroup("c0", "开发组", "zzc", leader, t0)), identity.ErrGroupAgentUnavailable)
	}
	if err := s.CreateGroup(ctx, mkGroup("c1", "开发组", "ZZC", "ag-z", t0.Add(time.Minute))); err != nil {
		t.Fatalf("create: %v", err)
	}
	g, err := s.GetGroup(ctx, "c1")
	if err != nil || g.ShortName != "开发组" || g.CreatorLC != "zzc" || g.LeaderAgentID != "ag-z" || g.AdditionsClosed || !g.CreatedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("GetGroup: %+v %v", g, err)
	}
	if got := memberIDs(t, s, "c1"); got != "ag-z" {
		t.Fatalf("leader must be the first member: %q", got)
	}
	if codes, _ := s.ListAgentGroups(ctx, "ag-z"); strings.Join(notFixture(codes), ",") != "c1" {
		t.Fatalf("ListAgentGroups: %v", codes)
	}
	wantErr(t, "same name, other user", s.CreateGroup(ctx, mkGroup("c2", "开发组", "lbl", "ag-l", t0)), identity.ErrGroupNameTaken)
	wantErr(t, "same code", s.CreateGroup(ctx, mkGroup("c1", "别的", "lbl", "ag-l", t0)), identity.ErrConflict)
	if _, err := s.GetGroup(ctx, "c2"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("refused group was created: %v", err)
	}
	// 区分大小写（与旧平台同）
	if err := s.CreateGroup(ctx, mkGroup("zz", "Dev", "zzc", "ag-z2", t0)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateGroup(ctx, mkGroup("aa", "dev", "lbl", "ag-l", t0)); err != nil {
		t.Fatalf("names differing only in case must both be allowed: %v", err)
	}
	gs, err := s.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, g := range gs {
		order = append(order, g.Code)
	}
	if strings.Join(notFixture(order), ",") != "aa,zz,c1" {
		t.Fatalf("ListGroups order = %v, want aa,zz,c1 (created_at, then code)", order)
	}
}

// 入组、退出、关闭添加：组长不可剔除；成员属主或建组人才可剔除；关闭后不能再入；只有建组人能开关。
func groupJoinLeaveClose(t *testing.T, s identity.Store) {
	groupFixture(t, s)
	if err := s.CreateGroup(ctx, mkGroup("c1", "开发组", "zzc", "ag-z", t0)); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(time.Minute)
	wantErr(t, "join missing group", s.JoinGroup(ctx, "nope", "ag-l", "lbl", t1), identity.ErrNotFound)
	wantErr(t, "join someone else's agent", s.JoinGroup(ctx, "c1", "ag-l", "zzc", t1), identity.ErrGroupAgentUnavailable)
	if err := s.JoinGroup(ctx, "c1", "ag-l", "LBL", t1); err != nil {
		t.Fatalf("join own agent: %v", err)
	}
	wantErr(t, "join twice", s.JoinGroup(ctx, "c1", "ag-l", "lbl", t1), identity.ErrGroupMemberExists)
	if err := s.JoinGroup(ctx, "c1", "ag-m", "mmm", t1); err != nil {
		t.Fatal(err)
	}
	if got := memberIDs(t, s, "c1"); got != "ag-z,ag-l,ag-m" {
		t.Fatalf("members %q, want ag-z,ag-l,ag-m (joined_at, then agent_id)", got)
	}
	wantErr(t, "leader removed by creator", s.LeaveGroup(ctx, "c1", "ag-z", "zzc"), identity.ErrGroupLeaderProtected)
	wantErr(t, "third party removes a member", s.LeaveGroup(ctx, "c1", "ag-l", "mmm"), identity.ErrForbidden)
	wantErr(t, "leave missing group", s.LeaveGroup(ctx, "nope", "ag-l", "lbl"), identity.ErrNotFound)
	if err := s.LeaveGroup(ctx, "c1", "ag-l", "lbl"); err != nil {
		t.Fatalf("owner leaves: %v", err)
	}
	wantErr(t, "leave twice", s.LeaveGroup(ctx, "c1", "ag-l", "lbl"), identity.ErrGroupMemberNotFound)
	if err := s.LeaveGroup(ctx, "c1", "ag-m", "zzc"); err != nil {
		t.Fatalf("creator removes a member: %v", err)
	}
	if got := memberIDs(t, s, "c1"); got != "ag-z" {
		t.Fatalf("members after leaves: %q", got)
	}
	t2 := t0.Add(2 * time.Minute)
	wantErr(t, "non-creator closes", s.SetGroupAdditionsClosed(ctx, "c1", "lbl", true, t2), identity.ErrForbidden)
	wantErr(t, "close missing group", s.SetGroupAdditionsClosed(ctx, "nope", "zzc", true, t2), identity.ErrNotFound)
	if err := s.SetGroupAdditionsClosed(ctx, "c1", "ZZC", true, t2); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.GetGroup(ctx, "c1"); !g.AdditionsClosed || !g.UpdatedAt.Equal(t2) {
		t.Fatalf("after close: %+v", g)
	}
	wantErr(t, "join closed group", s.JoinGroup(ctx, "c1", "ag-l", "lbl", t2), identity.ErrGroupClosed)
	if err := s.SetGroupAdditionsClosed(ctx, "c1", "zzc", false, t2); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinGroup(ctx, "c1", "ag-l", "lbl", t2); err != nil {
		t.Fatalf("join after reopen: %v", err)
	}
}

// 删组：建组人才可；成员行随之删除；组名可再用。
func groupDeleteRules(t *testing.T, s identity.Store) {
	groupFixture(t, s)
	if err := s.CreateGroup(ctx, mkGroup("c1", "开发组", "zzc", "ag-z", t0)); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinGroup(ctx, "c1", "ag-l", "lbl", t0); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "member's owner deletes", s.DeleteGroup(ctx, "c1", "lbl"), identity.ErrForbidden)
	wantErr(t, "delete missing", s.DeleteGroup(ctx, "nope", "zzc"), identity.ErrNotFound)
	if err := s.DeleteGroup(ctx, "c1", "ZZC"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetGroup(ctx, "c1"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("group still there: %v", err)
	}
	if got := memberIDs(t, s, "c1"); got != "" {
		t.Fatalf("member rows survived delete: %q", got)
	}
	for _, ag := range []string{"ag-z", "ag-l"} {
		if codes, _ := s.ListAgentGroups(ctx, ag); len(notFixture(codes)) != 0 {
			t.Fatalf("%s still in groups %v", ag, codes)
		}
	}
	if err := s.CreateGroup(ctx, mkGroup("c2", "开发组", "lbl", "ag-l", t0)); err != nil {
		t.Fatalf("name must be reusable after delete: %v", err)
	}
}

// 吊销不删成员行（留作历史，由业务层不予展示）；吊销后的 Agent 不能再入组、不能当组长。
func groupMemberAfterRevoke(t *testing.T, s identity.Store) {
	groupFixture(t, s)
	if err := s.CreateGroup(ctx, mkGroup("c1", "开发组", "zzc", "ag-z", t0)); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinGroup(ctx, "c1", "ag-l", "lbl", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetireAgent(ctx, "ag-l", "test", t0); err != nil {
		t.Fatal(err)
	}
	if got := memberIDs(t, s, "c1"); got != "ag-z,ag-l" {
		t.Fatalf("store must keep the revoked member row: %q", got)
	}
	if err := s.CreateGroup(ctx, mkGroup("c2", "二组", "zzc", "ag-z2", t0)); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "revoked agent joins", s.JoinGroup(ctx, "c2", "ag-l", "lbl", t0), identity.ErrGroupAgentUnavailable)
}

// notFixture 滤掉夹具给每个 Agent 建的 gc-<id> 组。
func notFixture(codes []string) []string {
	var out []string
	for _, c := range codes {
		if !strings.HasPrefix(c, "gc-") {
			out = append(out, c)
		}
	}
	return out
}

// 批准时的编组（GroupAllowed）：入已有组须存在且未关闭添加；新组同一事务建、以新 Agent 为组长；任一失败整个批准不生效。
func approveGroupChoice(t *testing.T, s identity.Store) {
	mustHosted(t, s, "ag-lead", "zzc", "Lead", "app-lead", t0)
	if err := s.CreateGroup(ctx, mkGroup("c1", "一组", "zzc", "ag-lead", t0)); err != nil {
		t.Fatal(err)
	}
	mustApp(t, s, "app-1", "zzc", "dev-1", t0)
	in := func(group string, ng *identity.Group) identity.ApproveInput {
		return identity.ApproveInput{AppID: "app-1", OwnerLC: "zzc", AgentID: "ag-1", Name: "Worker", GroupID: group, NewGroup: ng}
	}
	nothingChanged := func(what string) {
		t.Helper()
		if a, _ := s.GetApplication(ctx, "app-1"); a.State != identity.AppPending {
			t.Fatalf("%s: application left %s, want pending", what, a.State)
		}
		if _, err := s.GetAgent(ctx, "ag-1"); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("%s: agent created anyway: %v", what, err)
		}
	}
	_, err := s.Approve(ctx, in("nope", nil), t0)
	wantErr(t, "missing group", err, identity.ErrGroupNotFound)
	nothingChanged("missing group")
	if err := s.SetGroupAdditionsClosed(ctx, "c1", "zzc", true, t0); err != nil {
		t.Fatal(err)
	}
	_, err = s.Approve(ctx, in("c1", nil), t0)
	wantErr(t, "closed group", err, identity.ErrGroupClosed)
	nothingChanged("closed group")
	_, err = s.Approve(ctx, in("gx", &identity.Group{Code: "gx", ShortName: "一组", CreatorLC: "zzc", CreatedAt: t0, UpdatedAt: t0}), t0)
	wantErr(t, "new group name taken", err, identity.ErrGroupNameTaken)
	nothingChanged("new group name taken")
	if _, err := s.GetGroup(ctx, "gx"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("refused new group was created: %v", err)
	}
	// 事务中途失败（新组本身没问题，但 Agent 名已被占）⇒ 不留空组、不留 Agent（组长台账 6f3b6edd 反向第二条）
	_, err = s.Approve(ctx, identity.ApproveInput{AppID: "app-1", OwnerLC: "zzc", AgentID: "ag-1", Name: "LEAD", GroupID: "gy",
		NewGroup: &identity.Group{Code: "gy", ShortName: "空组", CreatorLC: "zzc", CreatedAt: t0, UpdatedAt: t0}}, t0)
	wantErr(t, "agent name taken with new group", err, identity.ErrNameTaken)
	nothingChanged("agent name taken with new group")
	if _, err := s.GetGroup(ctx, "gy"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("empty group left behind after failed approve: %v", err)
	}
	if err := s.SetGroupAdditionsClosed(ctx, "c1", "zzc", false, t0); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(time.Minute)
	ag, err := s.Approve(ctx, in("c1", nil), t1)
	if err != nil || ag.GroupID != "c1" {
		t.Fatalf("approve into c1: %+v %v", ag, err)
	}
	if got := memberIDs(t, s, "c1"); got != "ag-lead,ag-1" {
		t.Fatalf("c1 members %q", got)
	}
	// 新组：另一份申请，就地建组、以新 Agent 为组长
	mustApp(t, s, "app-2", "zzc", "dev-2", t0)
	ag2, err := s.Approve(ctx, identity.ApproveInput{AppID: "app-2", OwnerLC: "zzc", AgentID: "ag-2", Name: "Second", GroupID: "g2",
		NewGroup: &identity.Group{Code: "g2", ShortName: "二组", CreatorLC: "ZZC", CreatedAt: t1, UpdatedAt: t1}}, t1)
	if err != nil || ag2.GroupID != "g2" {
		t.Fatalf("approve with new group: %+v %v", ag2, err)
	}
	g, err := s.GetGroup(ctx, "g2")
	if err != nil || g.LeaderAgentID != "ag-2" || g.CreatorLC != "zzc" || g.ShortName != "二组" || g.AdditionsClosed {
		t.Fatalf("new group: %+v %v", g, err)
	}
	if got := memberIDs(t, s, "g2"); got != "ag-2" {
		t.Fatalf("g2 members %q", got)
	}
}

// 托管新建的编组：语义同批准；失败时连申请（一次性令牌）也不留。
func hostedGroupChoice(t *testing.T, s identity.Store) {
	mustHosted(t, s, "ag-lead", "zzc", "Lead", "app-lead", t0)
	if err := s.CreateGroup(ctx, mkGroup("c1", "一组", "zzc", "ag-lead", t0)); err != nil {
		t.Fatal(err)
	}
	create := func(id, app, group string, ng *identity.Group) error {
		return s.CreateHostedAgent(ctx, identity.Agent{ID: id, OwnerLC: "zzc", Name: "H" + id[3:], GroupID: group, CreatedAt: t0},
			identity.Application{ID: app, PollTokenHash: "tok-" + app, RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)}, ng)
	}
	nothing := func(what, id, app string) {
		t.Helper()
		if _, err := s.GetAgent(ctx, id); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("%s: agent created anyway: %v", what, err)
		}
		if _, err := s.GetApplication(ctx, app); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("%s: application left behind: %v", what, err)
		}
	}
	wantErr(t, "hosted into missing group", create("ag-a", "app-a", "nope", nil), identity.ErrGroupNotFound)
	nothing("missing group", "ag-a", "app-a")
	if err := s.SetGroupAdditionsClosed(ctx, "c1", "zzc", true, t0); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "hosted into closed group", create("ag-b", "app-b", "c1", nil), identity.ErrGroupClosed)
	nothing("closed group", "ag-b", "app-b")
	wantErr(t, "hosted new group name taken", create("ag-c", "app-c", "gx", &identity.Group{Code: "gx", ShortName: "一组", CreatorLC: "zzc", CreatedAt: t0, UpdatedAt: t0}), identity.ErrGroupNameTaken)
	nothing("new group name taken", "ag-c", "app-c")
	// 事务中途失败（Agent 名已被占：夹具里组长 Agent 名为 Lead）⇒ 不留空组、不留 Agent、不留申请
	err := s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-f", OwnerLC: "zzc", Name: "LEAD", GroupID: "gy", CreatedAt: t0},
		identity.Application{ID: "app-f", PollTokenHash: "tok-f", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)},
		&identity.Group{Code: "gy", ShortName: "空组", CreatorLC: "zzc", CreatedAt: t0, UpdatedAt: t0})
	wantErr(t, "hosted agent name taken with new group", err, identity.ErrNameTaken)
	nothing("agent name taken with new group", "ag-f", "app-f")
	if _, err := s.GetGroup(ctx, "gy"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("empty group left behind after failed hosted create: %v", err)
	}
	if err := s.SetGroupAdditionsClosed(ctx, "c1", "zzc", false, t0); err != nil {
		t.Fatal(err)
	}
	if err := create("ag-d", "app-d", "c1", nil); err != nil {
		t.Fatalf("hosted into c1: %v", err)
	}
	if err := create("ag-e", "app-e", "g2", &identity.Group{Code: "g2", ShortName: "二组", CreatorLC: "zzc", CreatedAt: t0, UpdatedAt: t0}); err != nil {
		t.Fatalf("hosted with new group: %v", err)
	}
	if got := memberIDs(t, s, "c1"); got != "ag-d,ag-lead" && got != "ag-lead,ag-d" {
		t.Fatalf("c1 members %q", got)
	}
	if g, err := s.GetGroup(ctx, "g2"); err != nil || g.LeaderAgentID != "ag-e" || memberIDs(t, s, "g2") != "ag-e" {
		t.Fatalf("new hosted group: %+v %v", g, err)
	}
}

// 暂停记下谁停的、何时停的；已是暂停时再停不覆盖；恢复即清空（qwen 复验 🟡①：by／now 曾是两个实现里的死参数）。
// 每一步用互不相同的操作者与时刻，写错字段或写成别一步的值都会红。
func suspendRecordsWhoWhen(t *testing.T, s identity.Store) {
	mustHosted(t, s, "ag-s", "zzc", "Sleepy", "app-s", t0)
	t1, t2, t3, t4 := t0.Add(time.Minute), t0.Add(2*time.Minute), t0.Add(3*time.Minute), t0.Add(4*time.Minute)
	get := func() *identity.Agent {
		t.Helper()
		g, err := s.GetAgent(ctx, "ag-s")
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	if g := get(); !g.SuspendedAt.IsZero() || g.SuspendedBy != "" {
		t.Fatalf("fresh agent has suspend fields: %+v", g)
	}
	if _, err := s.SetAgentSuspended(ctx, "ag-s", true, "g4", t1); err != nil {
		t.Fatal(err)
	}
	if g := get(); !g.SuspendedAt.Equal(t1) || g.SuspendedBy != "g4" {
		t.Fatalf("after suspend: at=%v by=%q, want %v g4", g.SuspendedAt, g.SuspendedBy, t1)
	}
	if already, err := s.SetAgentSuspended(ctx, "ag-s", true, "other", t2); !already || err != nil {
		t.Fatalf("second suspend: %v %v", already, err)
	}
	if g := get(); !g.SuspendedAt.Equal(t1) || g.SuspendedBy != "g4" {
		t.Fatalf("no-op suspend overwrote fields: at=%v by=%q", g.SuspendedAt, g.SuspendedBy)
	}
	if _, err := s.SetAgentSuspended(ctx, "ag-s", false, "resumer", t3); err != nil {
		t.Fatal(err)
	}
	if g := get(); !g.SuspendedAt.IsZero() || g.SuspendedBy != "" || g.State != identity.AgentActive {
		t.Fatalf("after resume: %+v", g)
	}
	if _, err := s.SetAgentSuspended(ctx, "ag-s", true, "ops", t4); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.ResolveAgentByCredential(ctx, "cred-none"); g != nil {
		t.Fatalf("unexpected credential hit: %+v", g)
	}
	if list, _ := s.ListAgents(ctx, "zzc"); len(list) != 1 || !list[0].SuspendedAt.Equal(t4) || list[0].SuspendedBy != "ops" {
		t.Fatalf("ListAgents must carry suspend fields: %+v", list)
	}
}

// 两条同名 Agent 在同一时刻被吊销（平票）：FindAgentByName 取 agent_id 大的，两个实现一致（qwen 复验 🔵1）。
// 建两组：先建 id 大的、再建 id 小的，排除「按插入序」碰巧对上。
func findAgentByNameTie(t *testing.T, s identity.Store) {
	at := t0.Add(time.Hour)
	for _, id := range []string{"ag-z9", "ag-a1"} {
		mustHosted(t, s, id, "zzc", "Twin", "app-"+id, t0)
		if _, err := s.RetireAgent(ctx, id, "by-"+id, at); err != nil {
			t.Fatal(err)
		}
	}
	// 连查 20 次：内存实现遍历 map 的顺序每次随机，平票若不确定，20 次全对的概率只有 2^-20
	for i := 0; i < 20; i++ {
		g, err := s.FindAgentByName(ctx, "zzc", "twin")
		if err != nil || g.ID != "ag-z9" || g.RevokedBy != "by-ag-z9" {
			t.Fatalf("tie (call %d): %+v %v, want ag-z9", i, g, err)
		}
	}
}

// 业主令（15:34:17Z，板 8582acc4）：编组不是必须的 —— 批准与托管新建都可不给组，不入任何组、不建组。
func noGroupJoinsNothing(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-1", "zzc", "dev-1", t0)
	ag, err := s.Approve(ctx, identity.ApproveInput{AppID: "app-1", OwnerLC: "zzc", AgentID: "ag-1", Name: "Loner"}, t0)
	if err != nil || ag.GroupID != "" {
		t.Fatalf("approve without group: %+v %v", ag, err)
	}
	if err := s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-h", OwnerLC: "zzc", Name: "Solo", CreatedAt: t0},
		identity.Application{ID: "app-h", PollTokenHash: "tok-h", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)}, nil); err != nil {
		t.Fatalf("hosted without group: %v", err)
	}
	for _, id := range []string{"ag-1", "ag-h"} {
		if codes, _ := s.ListAgentGroups(ctx, id); len(codes) != 0 {
			t.Fatalf("%s joined %v", id, codes)
		}
		if g, _ := s.GetAgent(ctx, id); g == nil || g.GroupID != "" {
			t.Fatalf("%s group code: %+v", id, g)
		}
	}
	if gs, _ := s.ListGroups(ctx); len(gs) != 0 {
		t.Fatalf("a group was created: %+v", gs)
	}
}

// 「本人可用」（组长令，收紧 GroupAllowed）：批准与托管新建只能入本人建的组、或本人已有未吊销成员在内的组；
// 别人的组与不存在同一答复 ErrGroupNotFound（不暴露组存在），且什么也不改。
func approveOnlyIntoUsableGroup(t *testing.T, s identity.Store) {
	mustHosted(t, s, "ag-lbl", "lbl", "Lee", "app-lbl", t0)
	if err := s.CreateGroup(ctx, mkGroup("lg", "李组", "lbl", "ag-lbl", t0)); err != nil {
		t.Fatal(err)
	}
	mustApp(t, s, "app-1", "zzc", "dev-1", t0)
	in := identity.ApproveInput{AppID: "app-1", OwnerLC: "zzc", AgentID: "ag-1", Name: "Worker", GroupID: "lg"}
	_, err := s.Approve(ctx, in, t0)
	wantErr(t, "approve into someone else's group", err, identity.ErrGroupNotFound)
	if a, _ := s.GetApplication(ctx, "app-1"); a.State != identity.AppPending {
		t.Fatalf("application left %s", a.State)
	}
	if _, err := s.GetAgent(ctx, "ag-1"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("agent created anyway: %v", err)
	}
	err = s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-h", OwnerLC: "zzc", Name: "Hosted", GroupID: "lg", CreatedAt: t0},
		identity.Application{ID: "app-h", PollTokenHash: "tok-h", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)}, nil)
	wantErr(t, "hosted into someone else's group", err, identity.ErrGroupNotFound)
	if got := memberIDs(t, s, "lg"); got != "ag-lbl" {
		t.Fatalf("someone else's group gained members: %q", got)
	}
	// 本人已有成员在内 ⇒ 可用（成员经 JoinGroup 入组；JoinGroup 的收紧另待组长裁）
	mustHosted(t, s, "ag-z", "zzc", "Zed", "app-z", t0)
	if err := s.JoinGroup(ctx, "lg", "ag-z", "zzc", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(ctx, in, t0.Add(time.Minute)); err != nil {
		t.Fatalf("approve into a group where I already have a member: %v", err)
	}
	// 成员被吊销后不再算「本人已有成员」
	mustApp(t, s, "app-2", "zzc", "dev-2", t0)
	if _, err := s.RetireAgent(ctx, "ag-z", "g4", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetireAgent(ctx, "ag-1", "g4", t0); err != nil {
		t.Fatal(err)
	}
	_, err = s.Approve(ctx, identity.ApproveInput{AppID: "app-2", OwnerLC: "zzc", AgentID: "ag-2", Name: "Second", GroupID: "lg"}, t0)
	wantErr(t, "only revoked members left", err, identity.ErrGroupNotFound)
}

// 运维重置口令（组长令，reset-password）：一个事务里换哈希、置必须改密、吊销该用户全部会话；别的用户会话不动；用户不存在 ⇒ ErrNotFound。
func resetPasswordRevokesSessions(t *testing.T, s identity.Store) {
	mustUser(t, s, "alice", "al", identity.UserActive)
	mustUser(t, s, "bob", "bo", identity.UserActive)
	exp := t0.Add(time.Hour)
	for _, se := range []identity.Session{{TokenHash: "s-a1", UserIDLC: "alice", ExpiresAt: exp}, {TokenHash: "s-a2", UserIDLC: "alice", ExpiresAt: exp}, {TokenHash: "s-b1", UserIDLC: "bob", ExpiresAt: exp}} {
		if err := s.CreateSession(ctx, se); err != nil {
			t.Fatal(err)
		}
	}
	wantErr(t, "reset missing user", s.ResetPassword(ctx, "nobody", "h-x", t0), identity.ErrNotFound)
	if err := s.ResetPassword(ctx, "ALICE", "h-new", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	u, _ := s.GetUser(ctx, "alice")
	if u.PasswordHash != "h-new" || !u.MustChangePassword {
		t.Fatalf("after reset: %+v", u)
	}
	for _, tok := range []string{"s-a1", "s-a2"} {
		if _, _, err := s.ResolveSession(ctx, tok, t0.Add(2*time.Minute)); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("alice session %s survived reset: %v", tok, err)
		}
	}
	if _, _, err := s.ResolveSession(ctx, "s-b1", t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("bob's session must be untouched: %v", err)
	}
	if b, _ := s.GetUser(ctx, "bob"); b.MustChangePassword {
		t.Fatal("bob marked must-change by alice's reset")
	}
}

// AuditCounter 可选：能数审计行的存储实现它（内存实现直接数，PG 契约入口用测试包装查 audit_log），
// 契约据此核「成功恰写一行、拒绝一行不写」。未实现则这部分跳过并记日志，不当通过。
type AuditCounter interface {
	CountAudit(ctx context.Context, action, target string) (int, error)
}

const promoteAction = "user.promoted_platform_admin_by_ops"

func auditCount(t *testing.T, s identity.Store, target string) (int, bool) {
	t.Helper()
	ac, ok := s.(AuditCounter)
	if !ok {
		t.Logf("store %T does not implement AuditCounter; audit rows not checked", s)
		return 0, false
	}
	n, err := ac.CountAudit(ctx, promoteAction, target)
	if err != nil {
		t.Fatalf("CountAudit: %v", err)
	}
	return n, true
}

// R6 promote-admin：active 用户一事务内升 platform_admin 并写恰一行审计、返回原类型；
// 不存在／非 active／已是管理员 ⇒ 对应错误，类型不变、审计不增。其他用户不动。
func promoteToPlatformAdmin(t *testing.T, s identity.Store) {
	mustUser(t, s, "gina", "gi", identity.UserActive)
	mustUser(t, s, "hank", "ha", identity.UserDisabled)
	mustUser(t, s, "ivan", "iv", identity.UserActive)
	if err := s.SetUserType(ctx, "gina", identity.UserManager); err != nil {
		t.Fatal(err)
	}
	old, err := s.PromoteToPlatformAdmin(ctx, "GINA", "promote-admin", promoteAction, t0)
	if err != nil || old != identity.UserManager {
		t.Fatalf("promote gina: old=%q err=%v", old, err)
	}
	if u, _ := s.GetUser(ctx, "gina"); u.Type != identity.UserPlatformAdmin || u.State != identity.UserActive || u.ShortName != "gi" {
		t.Fatalf("after promote: %+v", u)
	}
	if n, ok := auditCount(t, s, "gina"); ok && n != 1 {
		t.Fatalf("audit rows for gina after promote = %d, want 1", n)
	}
	if u, _ := s.GetUser(ctx, "ivan"); u.Type != identity.UserMember {
		t.Fatalf("other user changed: %+v", u)
	}
	for _, c := range []struct {
		name, id string
		want     error
		typ      identity.UserType
	}{
		{"missing", "nobody", identity.ErrNotFound, ""},
		{"disabled", "hank", identity.ErrUserNotActive, identity.UserMember},
		{"already admin", "gina", identity.ErrAlreadyPlatformAdmin, identity.UserPlatformAdmin},
	} {
		before, _ := auditCount(t, s, c.id)
		_, err := s.PromoteToPlatformAdmin(ctx, c.id, "promote-admin", promoteAction, t0.Add(time.Minute))
		wantErr(t, "promote "+c.name, err, c.want)
		if c.typ != "" {
			if u, _ := s.GetUser(ctx, c.id); u.Type != c.typ {
				t.Fatalf("promote %s changed type: %+v", c.name, u)
			}
		}
		if after, ok := auditCount(t, s, c.id); ok && after != before {
			t.Fatalf("promote %s wrote audit: %d -> %d", c.name, before, after)
		}
	}
}

// setUserType A11 改角色：只改该用户的类型，其他字段与其他用户不动；不存在 ⇒ ErrNotFound。
func setUserType(t *testing.T, s identity.Store) {
	mustUser(t, s, "erin", "er", identity.UserActive)
	mustUser(t, s, "finn", "fi", identity.UserActive)
	if err := s.SetUserType(ctx, "ERIN", identity.UserManager); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUser(ctx, "erin")
	if err != nil || u.Type != identity.UserManager || u.State != identity.UserActive || u.ShortName != "er" {
		t.Fatalf("after SetUserType(manager): %+v %v", u, err)
	}
	if o, _ := s.GetUser(ctx, "finn"); o.Type != identity.UserMember {
		t.Fatalf("other user's type changed: %+v", o)
	}
	if err := s.SetUserType(ctx, "erin", identity.UserMember); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetUser(ctx, "erin"); u.Type != identity.UserMember {
		t.Fatalf("after SetUserType(member): %+v", u)
	}
	wantErr(t, "SetUserType unknown user", s.SetUserType(ctx, "nobody", identity.UserManager), identity.ErrNotFound)
}

func groupCommunicationPolicy(t *testing.T, s identity.Store) {
	groupFixture(t, s)
	if err := s.CreateGroup(ctx, mkGroup("policy", "Policy", "zzc", "ag-z", t0)); err != nil {
		t.Fatal(err)
	}
	g, err := s.GetGroup(ctx, "policy")
	if err != nil || g.LeaderOnly {
		t.Fatalf("ordinary default: %+v %v", g, err)
	}
	wantErr(t, "noncreator", s.SetGroupLeaderOnly(ctx, "policy", "lbl", true, t0), identity.ErrForbidden)
	if err := s.SetGroupLeaderOnly(ctx, "policy", "zzc", true, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	g, err = s.GetGroup(ctx, "policy")
	if err != nil || !g.LeaderOnly || !g.UpdatedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("persisted: %+v %v", g, err)
	}
	if err := s.SetGroupLeaderOnly(ctx, "policy", "zzc", false, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	g, err = s.GetGroup(ctx, "policy")
	if err != nil || g.LeaderOnly {
		t.Fatalf("restore ordinary: %+v %v", g, err)
	}
}
