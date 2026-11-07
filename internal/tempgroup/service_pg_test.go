package tempgroup

import (
	"context"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"os"
	"sync"
	"testing"
	"time"
)

type fixture struct {
	ctx   context.Context
	p     *repo.Pool
	s     *Service
	seats *extseat.Service
	now   time.Time
	t     *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	p := pgtest.Pool(t)
	ctx := context.Background()
	access, err := repo.NewAccessCtx("tempgroup-test", "isolated PG fixture")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 2, 0, 0, 0, time.UTC)
	a, e := os.ReadFile("../store/migrate/migrations/0105_extseat.sql")
	if e != nil {
		t.Fatal(e)
	}
	d, e := os.ReadFile("../store/migrate/migrations/0106_tempgroup.sql")
	if e != nil {
		t.Fatal(e)
	}
	ddl := `CREATE TABLE audit_log(id bigserial PRIMARY KEY,actor text,action text,target text,after_json text,ts timestamptz);` + identity.PGSchemaSQL + string(a) + string(d)
	if _, e = p.Exec(ctx, access, ddl); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, access, `INSERT INTO np_user(user_id_lc,user_id,short_name,short_name_lc,type,state,created_at) VALUES('alice','alice','alice','alice','member','active',$1),('bob','bob','bob','bob','member','active',$1),('admin','admin','admin','admin','platform_admin','active',$1)`, now); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('ha','alice','ha','ha','selfhost','active',$1),('hb','bob','hb','hb','selfhost','active',$1)`, now); e != nil {
		t.Fatal(e)
	}
	f := &fixture{ctx: ctx, p: p, now: now, t: t}
	f.s = &Service{Pool: p, Access: access, Now: func() time.Time { return f.now }}
	f.seats = &extseat.Service{Pool: p, Access: access, Identity: identity.NewService(identity.NewPGStore(p), func() time.Time { return f.now }, nil), Groups: f.s, TenantKey: "tenant", Resource: "https://example.invalid/mcp", Now: func() time.Time { return f.now }}
	return f
}
func (f *fixture) issue(target string) extseat.Seat {
	f.t.Helper()
	_, a, _, e := f.seats.IssueToken(f.ctx, extshared.Principal{Provider: "feishu", TenantKey: "tenant", Issuer: "app", Subject: "external", DisplayName: "external"}, "bot", target)
	if e != nil {
		f.t.Fatal(e)
	}
	return a
}
func (f *fixture) scalar(q string, args ...any) int {
	f.t.Helper()
	r, e := f.p.QueryRow(f.ctx, f.s.Access, q, args...)
	if e != nil {
		f.t.Fatal(e)
	}
	var n int
	if e = r.Scan(&n); e != nil {
		f.t.Fatal(e)
	}
	return n
}
func TestRealGroupsPG(t *testing.T) {
	t.Run("MirrorReachabilityAndAgentIDRecheck", func(t *testing.T) {
		f := newFixture(t)
		a := f.issue("ha.alice")
		if f.scalar(`SELECT count(*) FROM np_group_member WHERE code=$1`, string(a.Group)) != 2 {
			t.Fatal("mirror members missing")
		}
		ext, e := f.seats.Identity.Store.GetAgent(f.ctx, a.AgentID)
		if e != nil {
			t.Fatal(e)
		}
		internal, e := f.seats.Identity.Store.GetAgent(f.ctx, "ha")
		if e != nil {
			t.Fatal(e)
		}
		allowed, e := f.seats.Identity.AgentsCanCommunicate(f.ctx, internal, ext)
		if e != nil || !allowed {
			t.Fatalf("mirror reachability: %v %v", allowed, e)
		}
		e = f.p.InTx(f.ctx, f.s.Access, nil, func(tx repo.Tx) error {
			g, e := f.s.RecheckGroup(f.ctx, tx, extseat.Seat{AgentID: a.AgentID}, f.now)
			if e == nil && (g.Code != a.Group || g.Target.ID != "ha" || g.Generation != 1) {
				t.Fatal("authoritative reverse lookup mismatch")
			}
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		for _, bad := range []extseat.Seat{{AgentID: a.AgentID, Group: "AG999"}, {AgentID: a.AgentID, Generation: 2}, {AgentID: "ha"}} {
			e = f.p.InTx(f.ctx, f.s.Access, nil, func(tx repo.Tx) error { _, e := f.s.RecheckGroup(f.ctx, tx, bad, f.now); return e })
			if !errors.Is(e, extshared.ErrGroupDissolved) {
				t.Fatalf("bad assertion accepted: %v", e)
			}
		}
	})
	t.Run("SequenceBurnsRollbackAndConcurrentUnique", func(t *testing.T) {
		f := newFixture(t)
		var burned extshared.AGCode
		rollback := errors.New("rollback")
		e := f.p.InTx(f.ctx, f.s.Access, nil, func(tx repo.Tx) error {
			_, e := f.s.CreateTempGroup(f.ctx, tx, extseat.CreateGroupRequest{ExternalAgentID: "burn", OwnerLC: "alice", Target: identity.Agent{ID: "ha"}, At: f.now, CreateExternal: func(g extseat.Group) error { burned = g.Code; return rollback }})
			return e
		})
		if !errors.Is(e, rollback) {
			t.Fatal(e)
		}
		a := f.issue("ha.alice")
		b, _ := extshared.ParseAGCode(string(burned))
		n, _ := extshared.ParseAGCode(string(a.Group))
		if n <= b {
			t.Fatal("sequence rollback reused number")
		}
		var wg sync.WaitGroup
		out := make(chan extshared.AGCode, 8)
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, a, _, e := f.seats.IssueToken(f.ctx, extshared.Principal{Provider: "feishu", TenantKey: "tenant", Issuer: "app", Subject: fmt.Sprintf("p%d", i), DisplayName: "external"}, "bot", "ha.alice")
				if e != nil {
					errs <- e
					return
				}
				out <- a.Group
			}(i)
		}
		wg.Wait()
		close(out)
		close(errs)
		for e := range errs {
			t.Error(e)
		}
		seen := map[extshared.AGCode]bool{}
		for code := range out {
			if seen[code] {
				t.Fatal("duplicate AG")
			}
			seen[code] = true
		}
		if len(seen) != 8 {
			t.Fatal("concurrent issuance incomplete")
		}
		if f.scalar(`SELECT count(*) FROM np_temp_group_member WHERE role='internal' AND agent_id='ha'`) != 9 {
			t.Fatal("internal peer must belong to multiple groups")
		}
	})
	t.Run("TouchIdempotencyGenerationAndExactExpiry", func(t *testing.T) {
		f := newFixture(t)
		a := f.issue("ha.alice")
		f.now = f.now.Add(time.Hour)
		touch := func(key string, kind extshared.GroupUseKind) error {
			return f.p.InTx(f.ctx, f.s.Access, nil, func(tx repo.Tx) error {
				return f.s.TouchGroup(f.ctx, tx, extseat.Group{Code: a.Group, Generation: 1}, key, kind, f.now)
			})
		}
		if e := touch("helper:send1", extshared.UseAcceptedSend); e != nil {
			t.Fatal(e)
		}
		v, e := f.s.Detail(f.ctx, "alice", a.Group, false)
		if e != nil {
			t.Fatal(e)
		}
		expiry := v.ExpiresAt
		f.now = f.now.Add(time.Hour)
		if e = touch("helper:send1", extshared.UseAcceptedSend); e != nil {
			t.Fatal(e)
		}
		v, e = f.s.Detail(f.ctx, "alice", a.Group, false)
		if e != nil || !v.ExpiresAt.Equal(expiry) || v.Generation != 1 {
			t.Fatal("duplicate renewed or changed generation")
		}
		if e = touch("helper:send1", extshared.UseFirstReceivedAck); !errors.Is(e, extshared.ErrIdempotencyKey) {
			t.Fatal("kind conflict accepted", e)
		}
		if e = touch("poll", extshared.GroupUseKind("poll")); !errors.Is(e, extshared.ErrNotPermitted) {
			t.Fatal("poll renewed")
		}
		f.now = expiry
		if e = f.seats.Recheck(f.ctx, a); !errors.Is(e, extshared.ErrGroupDissolved) {
			t.Fatal("exact expiry accepted", e)
		}
		if e = touch("late", extshared.UseAcceptedSend); !errors.Is(e, extshared.ErrGroupDissolved) {
			t.Fatal("late touch revived")
		}
		if e = f.s.Expire(f.ctx); e != nil {
			t.Fatal(e)
		}
		if f.scalar(`SELECT count(*) FROM np_group WHERE code=$1`, string(a.Group)) != 0 {
			t.Fatal("expired mirror retained")
		}
		v, e = f.s.Detail(f.ctx, "alice", a.Group, false)
		if e != nil || v.Generation != 1 || v.DissolveReason != "expired" {
			t.Fatal("expiry reason/generation", e)
		}
	})
	t.Run("ConsoleOnlyInternalOwnerAndDissolveFence", func(t *testing.T) {
		f := newFixture(t)
		a := f.issue("ha.alice")
		f.issue("hb.bob")
		for _, actor := range []string{"alice", "bob", "admin", a.OwnerLC} {
			list, e := f.s.List(f.ctx, actor)
			if e != nil {
				t.Fatal(e)
			}
			want := 0
			if actor == "alice" || actor == "bob" {
				want = 1
			}
			if len(list) != want {
				t.Fatalf("login-owner filter %s: got %d want %d", actor, len(list), want)
			}
		}
		v, e := f.s.Detail(f.ctx, "alice", a.Group, false)
		if e != nil || len(v.Members) != 2 || v.CreatedByDisplay == "" {
			t.Fatal("details", e)
		}
		if e = f.s.Dissolve(f.ctx, "bob", a.Group, v.ExpiresAt); !errors.Is(e, ErrNotFound) {
			t.Fatal("unrelated dissolved", e)
		}
		if e = f.s.Dissolve(f.ctx, "alice", a.Group, v.ExpiresAt.Add(time.Second)); !errors.Is(e, ErrChanged) {
			t.Fatal("stale confirmation accepted", e)
		}
		if e = f.s.Dissolve(f.ctx, "admin", a.Group, v.ExpiresAt); e != nil {
			t.Fatal(e)
		}
		if e = f.s.Dissolve(f.ctx, "alice", a.Group, v.ExpiresAt); !errors.Is(e, ErrChanged) {
			t.Fatal("duplicate dissolve", e)
		}
		if f.scalar(`SELECT count(*) FROM np_group`) != 1 {
			t.Fatal("other group affected")
		}
		if e = f.seats.Recheck(f.ctx, a); !errors.Is(e, extshared.ErrGroupDissolved) {
			t.Fatal("dissolved seat accepted", e)
		}
		notifier := &recordNotifier{}
		f.s.Notifier = notifier
		for i := 0; i < 2; i++ {
			if e = f.s.NotifyPending(f.ctx); e != nil {
				t.Fatal(e)
			}
		}
		if notifier.n != 1 {
			t.Fatal("notification not idempotent", notifier.n)
		}
	})
}

type recordNotifier struct{ n int }

func (n *recordNotifier) TempGroupDissolved(context.Context, extshared.AGCode, string) { n.n++ }

func TestDissolveSettlesBothDirectionsPG(t *testing.T) {
	f := newFixture(t)
	a := f.issue("ha.alice")
	if _, e := f.seats.Send(f.ctx, a, "ha.alice", "out", "client-1"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.seats.EnqueueFromHelper(f.ctx, "ha", a.AgentID, "helper-1", "in"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.seats.EnqueueFromHelper(f.ctx, "ha", a.AgentID, "helper-2", "acked"); e != nil {
		t.Fatal(e)
	}
	deliveries, e := f.seats.Wait(f.ctx, a, 0)
	if e != nil {
		t.Fatal(e)
	}
	if len(deliveries) != 1 {
		t.Fatal("need one actual leased inbox delivery")
	}
	if e = f.seats.Ack(f.ctx, a, deliveries[0].ID, int(deliveries[0].Generation)); e != nil {
		t.Fatal(e)
	}
	second, e := f.seats.Wait(f.ctx, a, 0)
	if e != nil || len(second) != 1 {
		t.Fatal("need second leased delivery", e)
	}
	v, e := f.s.Detail(f.ctx, "alice", a.Group, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Dissolve(f.ctx, "alice", a.Group, v.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	f.s.Notifier = &recordNotifier{}
	f.s.Finalize = f.seats.SettleDissolvedGroup
	if e = f.s.NotifyPending(f.ctx); e != nil {
		t.Fatal(e)
	}
	if f.scalar(`SELECT count(*) FROM np_extseat_inbox WHERE state='received'`) != 1 || f.scalar(`SELECT count(*) FROM np_extseat_inbox WHERE state='dead' AND dead_reason='group_unavailable'`) != 1 || f.scalar(`SELECT count(*) FROM np_extseat_outbox WHERE state='dead' AND dead_reason='group_unavailable'`) != 1 {
		t.Fatal("inflight settlement must preserve received and stop queued/leased both directions")
	}
}

func TestNotifyFailureRetriesAndOrdinaryGroupIsolationPG(t *testing.T) {
	f := newFixture(t)
	a := f.issue("ha.alice")
	if _, e := f.p.Exec(f.ctx, f.s.Access, `INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,created_at,updated_at) VALUES('ordinary','ordinary','alice','ha',$1,$1)`, f.now); e != nil {
		t.Fatal(e)
	}
	v, e := f.s.Detail(f.ctx, "alice", a.Group, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Dissolve(f.ctx, "alice", a.Group, v.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	n := &recordNotifier{}
	f.s.Notifier = n
	failure := errors.New("retry")
	f.s.Finalize = func(context.Context, extshared.AGCode) error { return failure }
	if e = f.s.NotifyPending(f.ctx); !errors.Is(e, failure) {
		t.Fatal("finalization failure hidden", e)
	}
	if n.n != 0 || f.scalar(`SELECT count(*) FROM np_temp_group WHERE notified_at IS NOT NULL`) != 0 {
		t.Fatal("failed settlement acknowledged notification")
	}
	f.s.Finalize = f.seats.SettleDissolvedGroup
	if e = f.s.NotifyPending(f.ctx); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(4 * 24 * time.Hour)
	if e = f.s.Expire(f.ctx); e != nil {
		t.Fatal(e)
	}
	if f.scalar(`SELECT count(*) FROM np_group WHERE code='ordinary'`) != 1 {
		t.Fatal("ordinary group affected")
	}
}

func TestExternalMemberOwnerMayDissolveWithoutSidebarExemptionPG(t *testing.T) {
	f := newFixture(t)
	a := f.issue("ha.alice")
	list, e := f.s.List(f.ctx, a.OwnerLC)
	if e != nil || len(list) != 0 {
		t.Fatal("external creator must not acquire sidebar exemption", e)
	}
	v, e := f.s.Detail(f.ctx, a.OwnerLC, a.Group, true)
	if e != nil || !v.CanDissolve {
		t.Fatal("related external member owner may view/dissolve", e)
	}
	if e = f.s.Dissolve(f.ctx, a.OwnerLC, a.Group, v.ExpiresAt); e != nil {
		t.Fatal("external member owner rejected", e)
	}
}
