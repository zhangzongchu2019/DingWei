package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
)

func r31Store(t *testing.T) *Store {
	t.Helper()
	s := NewStore(testpool(t))
	if err := s.ApplySchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.SetNodeAuthorizer(identity.AuthorizeHostedNodeTx)
	ids := identity.NewPGStore(s.pool)
	for _, name := range []string{"admin", "creator", "member", "outsider"} {
		typ := identity.UserMember
		if name == "admin" {
			typ = identity.UserPlatformAdmin
		}
		if err := ids.CreateUser(context.Background(), identity.User{UserID: name, ShortName: name, Type: typ, State: identity.UserActive, PasswordHash: "fixture", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}
func r31Register(t *testing.T, s *Store, id, scope string, groups ...string) string {
	t.Helper()
	secret, err := s.RegisterManagedNode(context.Background(), NodeRegistration{ID: id, Name: id, QuotaMode: "hard", QuotaPolicyRevision: strings.Repeat("a", 64), Access: NodeAccess{Scope: scope, Groups: groups}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return secret
}
func r31Frame(snap seatcapacity.Snapshot, managed map[string]Plan) json.RawMessage {
	resources := []seatcapacity.Resources{}
	seats := map[string]any{}
	for id, plan := range managed {
		resources = append(resources, capacityPlan(plan))
		seats[id] = map[string]any{"writer_kind": ""}
	}
	v := seatcapacity.Evaluate(seatcapacity.DefaultPolicy(), snap, resources, time.Now(), time.Minute)
	b, _ := json.Marshal(map[string]any{"quota_mode": "hard", "quota_policy_revision": strings.Repeat("a", 64), "resource_limits_version": 1, "cpu_total": 64, "mem_free_mb": 65536, "disk_free_mb": 1048576, "seat_capacity": v, "soft_seats": seats})
	return b
}
func r31Online(t *testing.T, s *Store, id string, snap seatcapacity.Snapshot) {
	t.Helper()
	if err := s.SetNodeOnline(context.Background(), id, r31Frame(snap, nil)); err != nil {
		t.Fatal(err)
	}
}
func r31Has(t *testing.T, s *Store, user, node string, want bool) {
	t.Helper()
	n, err := s.GetNode(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.CanAccessNode(context.Background(), user, n)
	if err != nil || ok != want {
		t.Fatalf("access %s/%s got=%v want=%v err=%v", user, node, ok, want, err)
	}
}

func TestR31NodeRegistrationCredentialAndAtomicAudit(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	secret := r31Register(t, s, "nd_admin", "admin_only")
	var hash string
	row, _ := s.qrow(ctx, `SELECT secret_hash FROM g4_node_credential WHERE node_id='nd_admin'`)
	if err := row.Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == secret || hash != hashNodeSecret(secret) {
		t.Fatal("plaintext or invalid credential hash")
	}
	if err := s.AuthenticateNode(ctx, "nd_admin", secret); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterManagedNode(ctx, NodeRegistration{ID: "nd_admin", Name: "duplicate", QuotaPolicyRevision: strings.Repeat("a", 64), Access: NodeAccess{Scope: "public"}}, "admin"); !errors.Is(err, ErrNodeExists) {
		t.Fatalf("duplicate silently rotated: %v", err)
	}
	if err := s.AuthenticateNode(ctx, "nd_admin", secret); err != nil {
		t.Fatal("duplicate changed credential")
	}
	r31Has(t, s, "member", "nd_admin", false)
	r31Has(t, s, "admin", "nd_admin", true)
	if _, err := s.exec(ctx, `CREATE FUNCTION r31_fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture audit failure'; END $$; CREATE TRIGGER r31_fail_audit BEFORE INSERT ON g4_audit FOR EACH ROW EXECUTE FUNCTION r31_fail_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterManagedNode(ctx, NodeRegistration{ID: "nd_rollback", Name: "rollback", QuotaPolicyRevision: strings.Repeat("a", 64), Access: NodeAccess{Scope: "public"}}, "admin"); err == nil {
		t.Fatal("registration ignored audit failure")
	}
	if _, err := s.GetNode(ctx, "nd_rollback"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed audit left node: %v", err)
	}
}

func TestR31NodeGroupAccessAndRevocation(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	if _, err := s.exec(ctx, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at)VALUES('ag_member','member','MemberAgent','memberagent','selfhost','active',now()); INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,created_at,updated_at)VALUES('gggggggggggggggggggggggggggggggg','GroupOne','creator','ag_member',now(),now()); INSERT INTO np_group_member(code,agent_id,created_at)VALUES('gggggggggggggggggggggggggggggggg','ag_member',now())`); err != nil {
		t.Fatal(err)
	}
	r31Register(t, s, "nd_restricted", "restricted", "gggggggggggggggggggggggggggggggg")
	r31Has(t, s, "creator", "nd_restricted", true)
	r31Has(t, s, "member", "nd_restricted", true)
	r31Has(t, s, "outsider", "nd_restricted", false)
	for _, state := range []string{"suspended", "revoked", "active"} {
		if _, err := s.exec(ctx, `UPDATE np_agent SET state=$1 WHERE id='ag_member'`, state); err != nil {
			t.Fatal(err)
		}
		r31Has(t, s, "member", "nd_restricted", state == "active")
	}
	if _, err := s.exec(ctx, `UPDATE np_group SET additions_closed=true WHERE code='gggggggggggggggggggggggggggggggg'`); err != nil {
		t.Fatal(err)
	}
	r31Has(t, s, "member", "nd_restricted", true)
	if _, err := s.exec(ctx, `DELETE FROM np_group_member WHERE code='gggggggggggggggggggggggggggggggg'`); err != nil {
		t.Fatal(err)
	}
	r31Has(t, s, "member", "nd_restricted", false)
	if _, err := s.exec(ctx, `DELETE FROM np_group WHERE code='gggggggggggggggggggggggggggggggg'`); err != nil {
		t.Fatal(err)
	}
	r31Has(t, s, "creator", "nd_restricted", false)
	r31Register(t, s, "nd_public", "public")
	r31Has(t, s, "outsider", "nd_public", true)
	if _, err := s.exec(ctx, `UPDATE np_user SET state='disabled' WHERE user_id='outsider'`); err != nil {
		t.Fatal(err)
	}
	r31Has(t, s, "outsider", "nd_public", false)
}

func TestR31HardCapacityAllDimensionsAndLegacyUnknown(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	r31Register(t, s, "nd_good", "public")
	r31Register(t, s, "nd_bad", "public")
	snap := seatcapacity.Snapshot{Known: true, At: time.Now(), Total: seatcapacity.Resources{CPU: 16000, Memory: 30 << 30, Disk: 560 << 30}, Reserved: seatcapacity.Resources{CPU: 2000, Memory: 3 << 30, Disk: 80 << 30}}
	r31Online(t, s, "nd_good", snap)
	n, _ := s.GetNode(ctx, "nd_good")
	v, err := s.nodeSeatCapacity(ctx, n)
	if err != nil || v == nil || v.AvailableSlots != 12 {
		t.Fatalf("owner example: %+v %v", v, err)
	}
	o := &Orchestrator{st: s}
	plan, _ := PlanByID("1c1.5g40")
	for _, dim := range []string{"cpu", "memory", "disk", "unknown"} {
		bad := netCapacitySnapshot()
		switch dim {
		case "cpu":
			bad.Total.CPU = 999
		case "memory":
			bad.Total.Memory = (1536 << 20) - 1
		case "disk":
			bad.Total.Disk = (40 << 30) - 1
		case "unknown":
			bad.Known = false
		}
		r31Online(t, s, "nd_bad", bad)
		got, err := o.pickNodeForOwner(ctx, "member", "auto", "", plan, "")
		if err != nil || got.ID != "nd_good" {
			t.Fatalf("did not skip %s: %s %v", dim, got.ID, err)
		}
		_, err = o.pickNodeForOwner(ctx, "member", "nd_bad", "", plan, "")
		if !errors.Is(err, ErrNoNodeCapacity) {
			t.Fatalf("bad explicit node accepted %s: %v", dim, err)
		}
	}
	if err = s.SetNodeOnline(ctx, "nd_bad", json.RawMessage(`{"quota_mode":"hard","quota_policy_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","mem_free_mb":65536}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = o.pickNodeForOwner(ctx, "member", "nd_bad", "", plan, ""); !errors.Is(err, ErrNoNodeCapacity) {
		t.Fatal("old hard frame bypassed capacity")
	}
	if err = s.SetNodeOnline(ctx, "nd_bad", json.RawMessage(`{"mem_free_mb":65536}`)); err != nil {
		t.Fatal(err)
	}
	_, err = o.pickNodeForOwner(ctx, "member", "nd_bad", "", plan, "")
	requireCapacityReasons(t, err, "load_unknown")
}

func TestR31NodeDisableSurvivesHeartbeatAndBlocksFinalAdmission(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	r31Register(t, s, "nd_one", "public")
	snap := netCapacitySnapshot()
	r31Online(t, s, "nd_one", snap)
	seat := Seat{AgentID: "ag_first", NodeID: "nd_one", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "1c1.5g40", CreatedBy: "member"}
	if err := s.CreateAdmittedSeat(ctx, seat); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetNodeRegistration(ctx, "nd_one", "admin")
	if err != nil {
		t.Fatal(err)
	}
	r.SchedulingState = "disabled"
	if err = s.UpdateManagedNode(ctx, r.ID, "admin", r); err != nil {
		t.Fatal(err)
	}
	r31Online(t, s, "nd_one", snap)
	n, err := s.GetNode(ctx, "nd_one")
	if err != nil || n.SchedulingState != "disabled" {
		t.Fatalf("heartbeat undid disable: %+v %v", n, err)
	}
	seat.AgentID = "ag_second"
	if err = s.CreateAdmittedSeat(ctx, seat); err == nil {
		t.Fatal("disabled node admitted seat")
	}
	first, err := s.GetSeat(ctx, "ag_first")
	if err != nil || first.State != "queued" {
		t.Fatal("disable changed existing seat")
	}
	// A seat admitted before disable is existing work. Disable must not cancel
	// it at the queue runner; only new admission is blocked.
	gw := &fakeGateway{}
	o := NewOrchestrator(s, nil, &fakeIdentity{}, gw, fakeCombos{}, fakeCatalog{})
	o.RunJob(ctx, "ag_first")
	if len(gw.sent) == 0 || gw.sent[0].Type != "pull_image" {
		t.Fatal("disable canceled an existing admitted queue")
	}
}

func TestR31NodeOptionsHideModelsAndQueuedRevocation(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	r31Register(t, s, "nd_public", "public")
	r31Register(t, s, "nd_secret", "admin_only")
	for id, model := range map[string]string{"nd_public": "visible-model", "nd_secret": "hidden-model"} {
		var m map[string]any
		if err := json.Unmarshal(r31Frame(netCapacitySnapshot(), nil), &m); err != nil {
			t.Fatal(err)
		}
		m["models"] = []string{model}
		raw, _ := json.Marshal(m)
		if err := s.SetNodeOnline(ctx, id, raw); err != nil {
			t.Fatal(err)
		}
	}
	gw := &fakeGateway{}
	o := NewOrchestrator(s, nil, &fakeIdentity{}, gw, fakeCombos{}, fakeCatalog{})
	v, err := o.Options(ctx, "member")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Nodes) != 1 || v.Nodes[0].ID != "nd_public" || len(v.Models) != 1 || v.Models[0] != "visible-model" {
		t.Fatalf("hidden node/model disclosure: %+v", v)
	}
	seat := Seat{AgentID: "ag_queued", NodeID: "nd_public", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "1c1.5g40", CreatedBy: "member"}
	if err = s.CreateAdmittedSeat(ctx, seat); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetNodeRegistration(ctx, "nd_public", "admin")
	if err != nil {
		t.Fatal(err)
	}
	r.Access = NodeAccess{Scope: "admin_only"}
	if err = s.UpdateManagedNode(ctx, r.ID, "admin", r); err != nil {
		t.Fatal(err)
	}
	o.RunJob(ctx, seat.AgentID)
	if len(gw.sent) != 0 {
		t.Fatal("revoked queue sent node commands")
	}
	got, err := s.GetSeat(ctx, seat.AgentID)
	if err != nil || got.State != "error" {
		t.Fatalf("revoked job not failed: %+v %v", got, err)
	}
}

func TestR31AdmissionAndNodeRevocationCommitOrder(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	r31Register(t, s, "nd_race", "public")
	r31Online(t, s, "nd_race", netCapacitySnapshot())
	reached, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s.afterNodeAdmission = func() { close(reached); <-release }
	created := make(chan error, 1)
	go func() {
		created <- s.CreateAdmittedSeat(ctx, Seat{AgentID: "ag_before", NodeID: "nd_race", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "1c1.5g40", CreatedBy: "member"})
	}()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("admission not reached")
	}
	r, err := s.GetNodeRegistration(ctx, "nd_race", "admin")
	if err != nil {
		t.Fatal(err)
	}
	r.Access = NodeAccess{Scope: "admin_only"}
	revoked := make(chan error, 1)
	go func() { revoked <- s.UpdateManagedNode(ctx, r.ID, "admin", r) }()
	select {
	case err := <-revoked:
		close(release)
		t.Fatalf("revoke crossed held admission: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err = <-created; err != nil {
		t.Fatal(err)
	}
	if err = <-revoked; err != nil {
		t.Fatal(err)
	}
	s.afterNodeAdmission = nil
	if _, err = s.GetSeat(ctx, "ag_before"); err != nil {
		t.Fatal("pre-revocation admission lost")
	}
	if err = s.CreateAdmittedSeat(ctx, Seat{AgentID: "ag_after", NodeID: "nd_race", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "1c1.5g40", CreatedBy: "member"}); !errors.Is(err, ErrForbiddenRole) {
		t.Fatalf("post-revocation admitted: %v", err)
	}
}

func TestR31HardConcurrentReservationsDoNotOversell(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	r31Register(t, s, "nd_slots", "public")
	r31Online(t, s, "nd_slots", seatcapacity.Snapshot{Known: true, At: time.Now(), Total: seatcapacity.Resources{CPU: 2000, Memory: 3 << 30, Disk: 80 << 30}})
	results := make(chan error, 10)
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			<-start
			results <- s.CreateAdmittedSeat(ctx, Seat{AgentID: fmt.Sprintf("ag_parallel_%d", i), NodeID: "nd_slots", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "1c1.5g40", CreatedBy: "member"})
		}(i)
	}
	close(start)
	passed := 0
	for i := 0; i < 10; i++ {
		err := <-results
		if err == nil {
			passed++
		} else if !errors.Is(err, ErrNoNodeCapacity) {
			t.Fatal(err)
		}
	}
	if passed != 2 {
		t.Fatalf("expected two seats, admitted %d", passed)
	}
}

func TestR31AdmissionUsesOwnerNotAdministratorAuditActor(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	r31Register(t, s, "nd_private", "admin_only")
	r31Online(t, s, "nd_private", netCapacitySnapshot())
	err := s.CreateAdmittedSeat(ctx, Seat{AgentID: "ag_owner", OwnerLC: "member", CreatedBy: "admin", NodeID: "nd_private", CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "1c1.5g40"})
	if !errors.Is(err, ErrForbiddenRole) {
		t.Fatalf("audit actor granted owner's access: %v", err)
	}
}

func TestR31HostedApplySchemaUsesUnifiedLedger(t *testing.T) {
	s := NewStore(testpool(t))
	ctx := context.Background()
	if err := s.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	row, err := s.qrow(ctx, `SELECT count(*) FROM schema_migrations WHERE version IN ('0200_hosted_r30_baseline.sql','0201_hosted_node_access_r31.sql')`)
	if err == nil {
		err = row.Scan(&count)
	}
	if err != nil || count != 2 {
		t.Fatalf("hosted startup bypassed unified migration ledger: rows=%d err=%v", count, err)
	}
}

func TestR31StatePatchCannotRestoreConcurrentRevocation(t *testing.T) {
	s := r31Store(t)
	ctx := context.Background()
	r31Register(t, s, "nd_patch", "public")
	ac, err := repo.NewAccessCtx("test", "patch-order")
	if err != nil {
		t.Fatal(err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	revokeDone := make(chan error, 1)
	go func() {
		revokeDone <- s.pool.InTx(ctx, ac, nil, func(tx repo.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE g4_node SET access_scope='restricted',address='203.0.113.50',address_source='node' WHERE id='nd_patch'`); err != nil {
				return err
			}
			if err := saveAccessTx(ctx, tx, "nd_patch", NodeAccess{Scope: "restricted", Users: []string{"creator"}}); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-revokeDone:
		t.Fatalf("revoke failed before lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("revoke lock timeout")
	}
	state := "disabled"
	done := make(chan error, 1)
	go func() { done <- s.PatchManagedNode(ctx, "nd_patch", "admin", NodePatch{SchedulingState: &state}) }()
	select {
	case err := <-done:
		close(release)
		t.Fatalf("state patch crossed uncommitted revoke: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	close(release)
	if err := <-revokeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	r, err := s.GetNodeRegistration(ctx, "nd_patch", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if r.Access.Scope != "restricted" || len(r.Access.Users) != 1 || r.Access.Users[0] != "creator" || r.SchedulingState != "disabled" {
		t.Fatal("state patch restored revoked access")
	}
	var source string
	row, err := s.qrow(ctx, `SELECT address_source FROM g4_node WHERE id='nd_patch'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "node" {
		t.Fatal("state patch promoted reported address to administrator ownership")
	}
}
