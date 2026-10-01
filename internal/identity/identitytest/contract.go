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
		{"N3_disable_revokes_sessions", disableRevokesSessions},
		{"N3_get_user_by_short_name_case_insensitive", getUserByShortName},
		{"N3_list_users_sorted_by_lowercase_id", listUsers},
		{"N3_session_expiry_and_inactive_user", sessionExpiryAndInactive},
		{"N3_registration_code_one_shot_binds_identity", regCodeOneShot},
		{"N3_registration_code_expired_or_unknown_changes_nothing", regCodeInvalidChangesNothing},
		{"N3_identity_already_bound_rejected", identityAlreadyBound},
		{"N3_new_registration_code_voids_old", regCodeRegenVoidsOld},
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
		{"V2_revoked_before_pickup_no_credential", revokedBeforePickup},
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

func mustApprove(t *testing.T, s identity.Store, app, owner, agentID, name string, now time.Time) *identity.Agent {
	t.Helper()
	ag, err := s.Approve(ctx, identity.ApproveInput{AppID: app, OwnerLC: owner, AgentID: agentID, Name: name, GroupID: "g-" + agentID}, now)
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
	if nw.ID != "ag-new" || nw.State != identity.AgentActive || nw.GroupID != "g-ag-new" {
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
	err := s.CreateHostedAgent(ctx, identity.Agent{ID: agentID, OwnerLC: owner, Name: name, GroupID: "g-" + agentID, CreatedAt: at},
		identity.Application{ID: app, PollTokenHash: "tok-" + app, RequestedAt: at, ExpiresAt: at.Add(time.Hour)})
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
	err := s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-h1", OwnerLC: "zzc", Name: "WORKER"},
		identity.Application{ID: "app-h1", PollTokenHash: "tok-h1", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)})
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
	if err != nil || !ok || ag.Origin != identity.OriginHosted || ag.Name != "Codex1" || ag.GroupID != "g-ag-h2" {
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
	err = s.CreateHostedAgent(ctx, identity.Agent{ID: "ag-p2", OwnerLC: "zzc", Name: "PAUSY"},
		identity.Application{ID: "app-p2", RequestedAt: t0, ExpiresAt: t0.Add(time.Hour)})
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
