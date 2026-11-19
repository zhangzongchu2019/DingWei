package extseat

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

type ordinaryTestGroups struct{ testGroups }

func (ordinaryTestGroups) CreateTempGroup(ctx context.Context, tx repo.Tx, r CreateGroupRequest) (Group, error) {
	g, e := (testGroups{}).CreateTempGroup(ctx, tx, r)
	if e != nil {
		return g, e
	}
	seq, _ := extshared.ParseAGCode(string(g.Code))
	_, e = tx.Exec(ctx, `INSERT INTO np_temp_group(code,seq,generation,owner_lc,external_agent_id,target_agent_id,created_at,expires_at,created_by,created_by_display) VALUES($1,$2,1,$3,$4,$5,$6,$7,'fixture','fixture')`, string(g.Code), seq, r.OwnerLC, r.ExternalAgentID, r.Target.ID, r.At, g.ExpiresAt)
	if e != nil {
		return g, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO np_temp_group_member(code,agent_id,role,created_at) VALUES($1,$2,'external',$4),($1,$3,'internal',$4)`, string(g.Code), r.ExternalAgentID, r.Target.ID, r.At)
	return g, e
}
func ordinaryFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, pgtest.DSN(t))
	ddl, e := os.ReadFile("../store/migrate/migrations/0106_tempgroup.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, f.access, string(ddl)); e != nil {
		t.Fatal(e)
	}
	f.s.ExternalGroups = true
	f.s.Groups = ordinaryTestGroups{}
	return f
}
func (f *fixture) ordinary(t *testing.T, code string, members ...string) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, f.access, `INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,created_at,updated_at) VALUES($1,$1,'helperowner','helper',$2,$2)`, code, f.clock.now()); e != nil {
		t.Fatal(e)
	}
	for _, id := range members {
		if _, e := f.pool.Exec(f.ctx, f.access, `INSERT INTO np_group_member(code,agent_id,created_at) VALUES($1,$2,$3)`, code, id, f.clock.now()); e != nil {
			t.Fatal(e)
		}
	}
}
func directContext() context.Context {
	return extshared.WithRouting(context.Background(), extshared.Routing{Mode: "direct"})
}
func TestExternalGroupsOrdinaryAndExplicitIsolationPG(t *testing.T) {
	f := ordinaryFixture(t)
	_, a := f.issue(t)
	ctx := directContext()
	f.ordinary(t, "ordinary-b", "helper", a.AgentID)
	f.ordinary(t, "ordinary-a", "helper", a.AgentID)
	r, e := f.s.Send(ctx, a, "helper.ho", "ordinary", "key", false)
	if e != nil {
		t.Fatal(e)
	}
	var ref string
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT route_ref FROM np_extseat_send_key WHERE delivery_id=$1`, r.DeliveryID)
	if e = row.Scan(&ref); e != nil || ref != "ordinary-a" {
		t.Fatalf("stable ordinary priority ref=%q err=%v", ref, e)
	}
	blocked := extshared.WithRouting(f.ctx, extshared.Routing{Mode: "group", Kind: "ordinary", Ref: "missing"})
	if _, e = f.s.Send(blocked, a, "helper.ho", "escape", "blocked"); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("invalid explicit group borrowed another grant: %v", e)
	}
	if _, e = f.pool.Exec(f.ctx, f.access, `DELETE FROM np_group_member WHERE code='ordinary-a' AND agent_id=$1`, a.AgentID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Send(ctx, a, "helper.ho", "ordinary", "key", false); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("replay switched original route: %v", e)
	}
	if _, e = f.s.Send(ctx, a, "helper.ho", "new", "new"); e != nil {
		t.Fatal(e)
	}
}
func TestExternalGroupsNoPairNoGroupAndExternalPeerPG(t *testing.T) {
	f := ordinaryFixture(t)
	p := extshared.Principal{Provider: "feishu", TenantKey: "tenant", Issuer: "app", Subject: "unpaired-subject", DisplayName: "unpaired"}
	raw, a, group, e := f.s.IssueTokenWithoutPair(f.ctx, p, "fresh")
	if e != nil {
		t.Fatal(e)
	}
	if group != "" || !strings.Contains(a.Name, "_EX") || !extshared.IsExternalAgentName(a.Name) {
		t.Fatalf("unpaired identity=%+v group=%q", a, group)
	}
	_ = raw
	ctx := directContext()
	if _, e = f.s.Send(ctx, a, "helper.ho", "not yet", "deny"); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("ungrouped send escaped: %v", e)
	}
	if _, e = f.s.Check(ctx, a); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("ungrouped check escaped: %v", e)
	}
	_, b := f.issue(t)
	f.ordinary(t, "peers", a.AgentID, b.AgentID)
	receipt, e := f.s.Send(ctx, a, b.FullName, "peer payload", "peer")
	if e != nil {
		t.Fatal(e)
	}
	ds, e := f.s.Wait(ctx, b, 0)
	if e != nil || len(ds) != 1 || ds[0].ID != receipt.DeliveryID || ds[0].Body != "peer payload" {
		t.Fatalf("external peer receive=%+v err=%v", ds, e)
	}
	if e = f.s.Ack(ctx, b, receipt.DeliveryID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Wait(ctx, b, 0); e != nil {
		t.Fatal(e)
	}
	// 普通组流量不能续临时配对。
	if f.touches(t) != 0 {
		t.Fatal("ordinary traffic touched temporary pair")
	}
}
func TestExternalGroupsCrossGroupAndMirrorFailClosedPG(t *testing.T) {
	f := ordinaryFixture(t)
	_, a := f.issue(t)
	// 标记配对镜像，但使真实配对过期，镜像成员不能独立授权。
	_, e := f.pool.Exec(f.ctx, f.access, `UPDATE np_temp_group SET expires_at=$2 WHERE code=$1`, string(a.Group), f.clock.now())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Send(directContext(), a, "helper.ho", "expired mirror", "mirror"); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("expired AG mirror authorized: %v", e)
	}
	// 无共享组的有效身份仍不能通信。
	err := f.pool.InTx(f.ctx, f.access, nil, func(tx repo.Tx) error {
		_, err := identity.AuthorizeCommunicationTx(f.ctx, tx, a.AgentID, "helper", extshared.Routing{Mode: "direct"}, "tenant", f.clock.now().Add(time.Second))
		return err
	})
	if !errors.Is(err, extshared.ErrNotPermitted) {
		t.Fatalf("cross group allowed: %v", err)
	}
}

func TestExternalGroupsControlledMembershipAndRollbackPG(t *testing.T) {
	f := ordinaryFixture(t)
	_, a := f.issue(t)
	_, e := f.pool.Exec(f.ctx, f.access, `INSERT INTO np_user(user_id_lc,user_id,short_name,short_name_lc,type,state,created_at) VALUES('admin','admin','ad','ad','platform_admin','active',$1),('stranger','stranger','st','st','member','active',$1)`, f.clock.now())
	if e != nil {
		t.Fatal(e)
	}
	f.s.Identity.ExternalGroups = true
	f.s.Identity.CommunicationTenantKey = "tenant"
	f.ordinary(t, "controlled", "helper")
	// 外部 Agent 全名不能作为 Console actor 自行入组；认证主体与 Agent 身份不同。
	if e = f.s.Identity.JoinGroup(f.ctx, a.FullName, "controlled", a.AgentID); e == nil {
		t.Fatal("external Agent self-invite granted membership")
	}
	if e = f.s.Identity.JoinGroup(f.ctx, "stranger", "controlled", a.AgentID); e == nil {
		t.Fatal("unrelated member added external Agent")
	}
	if e = f.s.Identity.JoinGroup(f.ctx, "admin", "controlled", a.AgentID); e != nil {
		t.Fatal(e)
	}
	f.s.ExternalGroups = false
	f.s.Identity.ExternalGroups = false
	if _, e = f.s.Send(directContext(), a, "helper.ho", "retained", "retained"); e != nil {
		t.Fatalf("rollback revoked existing membership: %v", e)
	}
	if e = f.s.Identity.JoinGroup(f.ctx, "admin", "controlled", a.AgentID); e == nil {
		t.Fatal("rollback allowed new external join")
	}
	if _, _, _, e = f.s.IssueTokenWithoutPair(f.ctx, a.Principal, "new"); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("rollback allowed new unpaired onboarding: %v", e)
	}
	var n int
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_group_member WHERE code='controlled' AND agent_id=$1`, a.AgentID)
	if e = row.Scan(&n); e != nil || n != 1 {
		t.Fatalf("rollback removed membership: %d %v", n, e)
	}
}

func TestExternalGroupsReauthenticationAndLegacySelectionPG(t *testing.T) {
	f := ordinaryFixture(t)
	p := extshared.Principal{Provider: "feishu", TenantKey: "tenant", Issuer: "app", Subject: "same-user", DisplayName: "same"}
	raw, a, _, e := f.s.IssueTokenWithoutPair(f.ctx, p, "stable")
	if e != nil {
		t.Fatal(e)
	}
	raw2, b, _, e := f.s.IssueTokenWithoutPair(f.ctx, p, "stable")
	if e != nil {
		t.Fatal(e)
	}
	if a.AgentID != b.AgentID || a.FullName != b.FullName || raw == raw2 {
		t.Fatal("reauthentication replaced stable identity or reused bearer")
	}
	if _, e = f.s.Authenticate(f.ctx, raw2); e != nil {
		t.Fatalf("ungrouped identity discovery denied: %v", e)
	}
	if _, e = f.s.Send(f.ctx, b, "helper.ho", "implicit", "old"); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("missing selector opened generic direct: %v", e)
	}
	f.ordinary(t, "ordinary", "helper", b.AgentID)
	if _, e = f.s.Send(directContext(), b, "helper.ho", "explicit direct", "direct"); e != nil {
		t.Fatal(e)
	}
	f.clock.add(extshared.TokenTTL)
	if _, e = f.s.Authenticate(f.ctx, raw2); !errors.Is(e, extshared.ErrTokenExpired) {
		t.Fatalf("token expiry ignored: %v", e)
	}
}

func TestExternalGroupsReadOnlyCheckAndBoundClaimPG(t *testing.T) {
	f := ordinaryFixture(t)
	_, a := f.issue(t)
	ctx := directContext()
	f.ordinary(t, "inbound-a", "helper", a.AgentID)
	f.ordinary(t, "inbound-b", "helper", a.AgentID)
	r, e := f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, "inbound", "private")
	if e != nil {
		t.Fatal(e)
	}
	before := f.inboxBytes(t)
	got, e := f.s.Check(ctx, a)
	if e != nil || len(got) != 1 || got[0].BodyBytes != 7 || f.inboxBytes(t) != before {
		t.Fatalf("read-only check=%+v %v", got, e)
	}
	if _, e = f.pool.Exec(f.ctx, f.access, `DELETE FROM np_group_member WHERE code='inbound-a' AND agent_id=$1`, a.AgentID); e != nil {
		t.Fatal(e)
	}
	ds, e := f.s.Wait(ctx, a, 0)
	if e != nil || len(ds) != 0 {
		t.Fatalf("bound inbox borrowed other group: %+v %v", ds, e)
	}
	explicit := extshared.WithRouting(f.ctx, extshared.Routing{Mode: "group", Kind: "ordinary", Ref: "inbound-b"})
	if _, e = f.s.Send(explicit, a, "helper.ho", "reply", "reply"); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Ack(ctx, a, r.DeliveryID, 1); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("lost binding ACK authorized: %v", e)
	}
}

func TestExternalGroupsProjectionRechecksOriginalRoutePG(t *testing.T) {
	f := ordinaryFixture(t)
	_, a := f.issue(t)
	f.ordinary(t, "route-a", "helper", a.AgentID)
	f.ordinary(t, "route-b", "helper", a.AgentID)
	ctx := directContext()
	r, e := f.s.Send(ctx, a, "helper.ho", "queued", "queued")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, f.access, `DELETE FROM np_group_member WHERE code='route-a' AND agent_id=$1`, a.AgentID); e != nil {
		t.Fatal(e)
	}
	projector := &recordingProjector{}
	did, e := f.s.ProjectOne(f.ctx, projector)
	if e != nil || !did || len(projector.ids) != 0 {
		t.Fatalf("projection borrowed another route did=%v ids=%v err=%v", did, projector.ids, e)
	}
	var state, reason string
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT state,dead_reason FROM np_extseat_outbox WHERE delivery_id=$1`, r.DeliveryID)
	if e = row.Scan(&state, &reason); e != nil || state != "dead" || reason != "group_unavailable" {
		t.Fatalf("lost route not settled: %q %q %v", state, reason, e)
	}
	r, e = f.s.Send(ctx, a, "helper.ho", "live", "live")
	if e != nil {
		t.Fatal(e)
	}
	did, e = f.s.ProjectOne(f.ctx, projector)
	if e != nil || !did || len(projector.ids) != 1 || projector.ids[0] != r.DeliveryID {
		t.Fatalf("ordinary projection id lost: %v %v %v", did, projector.ids, e)
	}
}
func TestExternalGroupsTenantAndLeaderOnlyRemainHardBoundariesPG(t *testing.T) {
	f := ordinaryFixture(t)
	_, a := f.issue(t)
	raw, peer, _, e := f.s.IssueTokenWithoutPair(f.ctx, extshared.Principal{Provider: "feishu", TenantKey: "tenant", Issuer: "app", Subject: "peer-two", DisplayName: "peer"}, "peer")
	if e != nil {
		t.Fatal(e)
	}
	_ = raw
	f.ordinary(t, "shared", a.AgentID, peer.AgentID)
	if _, e = f.pool.Exec(f.ctx, f.access, `UPDATE np_group SET leader_only=true WHERE code='shared'`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Send(directContext(), a, peer.FullName, "no member to member", "leader-only"); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("leader-only restriction bypassed: %v", e)
	}
	if _, e = f.pool.Exec(f.ctx, f.access, `UPDATE np_group SET leader_only=false WHERE code='shared'`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, f.access, `UPDATE np_external_principal SET tenant_key='foreign' WHERE subject='peer-two'`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Send(directContext(), a, peer.FullName, "group is not tenant admission", "tenant"); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("ordinary group admitted foreign tenant: %v", e)
	}
}
func TestExternalGroupsPGEnvironment(t *testing.T) {
	f := ordinaryFixture(t)
	var version string
	row, e := f.pool.QueryRow(f.ctx, f.access, `SHOW server_version`)
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&version); e != nil {
		t.Fatal(e)
	}
	t.Logf("REAL_PG_SERVER_VERSION=%s", version)
}

func TestExternalGroupsConcurrentSameIDRecoversOriginalReceiptPG(t *testing.T) {
	f := ordinaryFixture(t)
	_, a := f.issue(t)
	f.ordinary(t, "stable", "helper", a.AgentID)
	var wg sync.WaitGroup
	results := make(chan SendReceipt, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.s.Send(directContext(), a, "helper.ho", "same", "concurrent")
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	id := ""
	for r := range results {
		if id == "" {
			id = r.DeliveryID
		}
		if id != r.DeliveryID {
			t.Fatal("same ID generated another delivery")
		}
	}
	var n int
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_outbox WHERE seat_id=$1`, a.AgentID)
	if e := row.Scan(&n); e != nil || n != 1 {
		t.Fatalf("duplicate outbox %d %v", n, e)
	}
}
