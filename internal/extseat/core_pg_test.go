package extseat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// D路消费者的事务假实现；事务真实落PG，未依赖D的生产实现。
type testGroups struct{}

type failingFeishuCopyQueue struct{}

func (failingFeishuCopyQueue) Enqueue(context.Context, repo.Tx, FeishuCopyEvent) error {
	return errors.New("synthetic copy storage failure")
}

func (testGroups) CreateTempGroup(ctx context.Context, tx repo.Tx, r CreateGroupRequest) (Group, error) {
	var n int64
	if e := tx.QueryRow(ctx, `SELECT nextval('test_group_seq')`).Scan(&n); e != nil {
		return Group{}, e
	}
	code, _ := extshared.FormatAGCode(n)
	g := Group{Code: code, Generation: 1, Target: r.Target, ExpiresAt: r.At.Add(extshared.GroupTTL)}
	if e := r.CreateExternal(g); e != nil {
		return g, e
	}
	_, e := tx.Exec(ctx, `INSERT INTO test_groups(code,generation,target,external,expires_at) VALUES($1,1,$2,$3,$4)`, string(code), r.Target.ID, r.ExternalAgentID, g.ExpiresAt)
	if e != nil {
		return g, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,created_at,updated_at) VALUES($1,$1,$2,$3,$4,$4)`, string(code), r.OwnerLC, r.Target.ID, r.At); e != nil {
		return g, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO np_group_member(code,agent_id,created_at) VALUES($1,$2,$4),($1,$3,$4)`, string(code), r.Target.ID, r.ExternalAgentID, r.At)
	return g, e
}
func (testGroups) RecheckGroup(ctx context.Context, tx repo.Tx, a Seat, now time.Time) (Group, error) {
	g := Group{Code: a.Group, Generation: a.Generation}
	var target string
	var active bool
	e := tx.QueryRow(ctx, `SELECT target,expires_at,active FROM test_groups WHERE code=$1 AND generation=$2 AND external=$3 FOR UPDATE`, string(a.Group), a.Generation, a.AgentID).Scan(&target, &g.ExpiresAt, &active)
	if errors.Is(e, sql.ErrNoRows) || !active || !now.Before(g.ExpiresAt) {
		return g, extshared.ErrGroupDissolved
	}
	if e != nil {
		return g, e
	}
	e = tx.QueryRow(ctx, `SELECT id,owner_lc,name,origin,state FROM np_agent WHERE id=$1`, target).Scan(&g.Target.ID, &g.Target.OwnerLC, &g.Target.Name, &g.Target.Origin, &g.Target.State)
	if e != nil {
		return g, e
	}
	if g.Target.State != identity.AgentActive {
		return g, extshared.ErrTargetNotInAG
	}
	return g, nil
}
func (testGroups) ReadGroup(ctx context.Context, tx repo.Tx, a Seat, now time.Time) (Group, error) {
	g := Group{Code: a.Group, Generation: a.Generation}
	var target string
	var active bool
	e := tx.QueryRow(ctx, `SELECT target,expires_at,active FROM test_groups WHERE code=$1 AND generation=$2 AND external=$3`, string(a.Group), a.Generation, a.AgentID).Scan(&target, &g.ExpiresAt, &active)
	if errors.Is(e, sql.ErrNoRows) || !active || !now.Before(g.ExpiresAt) {
		return g, extshared.ErrGroupDissolved
	}
	if e != nil {
		return g, e
	}
	e = tx.QueryRow(ctx, `SELECT id,owner_lc,name,origin,state FROM np_agent WHERE id=$1`, target).Scan(&g.Target.ID, &g.Target.OwnerLC, &g.Target.Name, &g.Target.Origin, &g.Target.State)
	if e != nil {
		return g, e
	}
	if g.Target.State != identity.AgentActive {
		return g, extshared.ErrTargetNotInAG
	}
	return g, nil
}
func (testGroups) TouchGroup(ctx context.Context, tx repo.Tx, g Group, key string, kind extshared.GroupUseKind, at time.Time) error {
	if kind != extshared.UseAcceptedSend && kind != extshared.UseFirstReceivedAck {
		return extshared.ErrNotPermitted
	}
	r, e := tx.Exec(ctx, `INSERT INTO test_touches(event,kind) VALUES($1,$2) ON CONFLICT DO NOTHING`, key, string(kind))
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return nil
	}
	r, e = tx.Exec(ctx, `UPDATE test_groups SET expires_at=GREATEST(expires_at,$2) WHERE code=$1 AND active AND expires_at>$3`, string(g.Code), at.Add(extshared.GroupTTL), at)
	if e != nil {
		return e
	}
	n, _ = r.RowsAffected()
	if n != 1 {
		return extshared.ErrGroupDissolved
	}
	return nil
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

type fixture struct {
	s      *Service
	clock  *testClock
	pool   *repo.Pool
	access repo.AccessCtx
	ctx    context.Context
}

func newFixture(t *testing.T, dsn string) *fixture {
	t.Helper()
	ctx := context.Background()
	admin, e := repo.OpenPG(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { admin.Close() })
	schema := fmt.Sprintf("ext_%d", time.Now().UnixNano())
	ac, accessErr := repo.NewAccessCtx("test", "extseat-pg")
	if accessErr != nil {
		t.Fatal(accessErr)
	}
	if _, e = admin.Exec(ctx, ac, `CREATE SCHEMA `+schema); e != nil {
		t.Fatal(e)
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	pool, e := repo.OpenPG(ctx, dsn+sep+"search_path="+schema)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pool.Close(); _, _ = admin.Exec(ctx, ac, `DROP SCHEMA `+schema+` CASCADE`) })
	sqlText, e := os.ReadFile("../store/migrate/migrations/0105_extseat.sql")
	if e != nil {
		t.Fatal(e)
	}
	policyDDL, e := os.ReadFile("../store/migrate/migrations/0110_extseat_expect_reply.sql")
	if e != nil {
		t.Fatal(e)
	}
	copyDDL, e := os.ReadFile("../store/migrate/migrations/0111_extseat_feishu_copy.sql")
	if e != nil {
		t.Fatal(e)
	}
	copyUpgradeDDL, e := os.ReadFile("../store/migrate/migrations/0114_agent_feishu_summary_copy.sql")
	if e != nil {
		t.Fatal(e)
	}
	ddl := `CREATE TABLE audit_log(id bigserial PRIMARY KEY,actor text,action text,target text,after_json text,ts timestamptz);` + identity.PGSchemaSQL + string(sqlText) + string(policyDDL) + string(copyDDL) + string(copyUpgradeDDL) + `CREATE SEQUENCE test_group_seq;CREATE TABLE test_groups(code text PRIMARY KEY,generation bigint,target text,external text,expires_at timestamptz,active boolean NOT NULL DEFAULT true);CREATE TABLE test_touches(event text PRIMARY KEY,kind text);`
	if _, e = pool.Exec(ctx, ac, ddl); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 11, 6, 5, 0, 0, 0, time.UTC)
	clock := &testClock{t: now}
	_, e = pool.Exec(ctx, ac, `INSERT INTO np_user(user_id_lc,user_id,short_name,short_name_lc,type,state,created_at) VALUES('helperowner','helperowner','ho','ho','member','active',$1);`, now)
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, ac, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('helper','helperowner','helper','helper','selfhost','active',$1)`, now)
	if e != nil {
		t.Fatal(e)
	}
	s := &Service{Pool: pool, Access: ac, Identity: identity.NewService(identity.NewPGStore(pool), clock.now, nil), Groups: testGroups{}, TenantKey: "tenant", Resource: "https://resource.example.invalid/mcp", Now: clock.now}
	return &fixture{s: s, clock: clock, pool: pool, access: ac, ctx: ctx}
}
func (f *fixture) issue(t *testing.T) (string, Seat) {
	t.Helper()
	raw, a, _, e := f.s.IssueToken(f.ctx, extshared.Principal{Provider: "feishu", TenantKey: "tenant", Issuer: "app", Subject: "open", DisplayName: "alice"}, "bot", "helper.ho")
	if e != nil {
		t.Fatal(e)
	}
	return raw, a
}
func (f *fixture) touches(t *testing.T) int {
	t.Helper()
	row, e := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM test_touches`)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	if e = row.Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func (f *fixture) keepGroup(t *testing.T) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, f.access, `UPDATE test_groups SET expires_at=$1`, f.clock.now().Add(90*24*time.Hour)); e != nil {
		t.Fatal(e)
	}
}
func TestCorePG(t *testing.T) {
	dsn := pgtest.DSN(t)
	t.Run("RenewalPersistsAcrossRestart", func(t *testing.T) {
		f := newFixture(t, dsn)
		raw, a := f.issue(t)
		f.keepGroup(t)
		f.clock.add(13 * 24 * time.Hour)
		fresh, e := f.s.Authenticate(f.ctx, raw)
		if e != nil {
			t.Fatal(e)
		}
		if !fresh.ExpiresAt.Equal(f.clock.now().Add(extshared.TokenTTL)) {
			t.Fatal("renewal expiry")
		}
		// 丢弃服务实例，在原14天之后读取真实PG，而非内存expiry。
		restarted := *f.s
		f.clock.add(2 * 24 * time.Hour)
		if _, e = restarted.Authenticate(f.ctx, raw); e != nil {
			t.Fatalf("persisted renewal lost across restart: %v", e)
		}
		if e = f.s.Recheck(f.ctx, a); e != nil {
			t.Fatal(e)
		}
		var expiry time.Time
		row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT expires_at FROM np_extseat_token WHERE id=$1`, a.TokenID)
		if e = row.Scan(&expiry); e != nil {
			t.Fatal(e)
		}
		f.clock.add(time.Hour)
		if e = f.s.Recheck(f.ctx, a); e != nil {
			t.Fatal(e)
		}
		var after time.Time
		row, _ = f.pool.QueryRow(f.ctx, f.access, `SELECT expires_at FROM np_extseat_token WHERE id=$1`, a.TokenID)
		row.Scan(&after)
		if !after.Equal(expiry) {
			t.Fatal("Recheck renewed")
		}
	})
	t.Run("RevokedCannotRevive", func(t *testing.T) {
		f := newFixture(t, dsn)
		raw, a := f.issue(t)
		if e := f.s.Revoke(f.ctx, a.ID); e != nil {
			t.Fatal(e)
		}
		if _, e := f.s.Authenticate(f.ctx, raw); !errors.Is(e, extshared.ErrTokenRevoked) {
			t.Fatalf("revoked token revived: %v", e)
		}
		if e := f.s.Recheck(f.ctx, a); !errors.Is(e, extshared.ErrTokenRevoked) {
			t.Fatalf("stale auth survived revocation: %v", e)
		}
	})
	t.Run("ExpiryEqualityAndClockRollback", func(t *testing.T) {
		f := newFixture(t, dsn)
		raw, _ := f.issue(t)
		f.keepGroup(t)
		f.clock.add(extshared.TokenTTL)
		if _, e := f.s.Authenticate(f.ctx, raw); !errors.Is(e, extshared.ErrTokenExpired) {
			t.Fatalf("equal expiry: %v", e)
		}
		f.clock.add(-extshared.TokenTTL - time.Second)
		if _, e := f.s.Authenticate(f.ctx, raw); !errors.Is(e, extshared.ErrNotPermitted) {
			t.Fatalf("rollback accepted: %v", e)
		}
	})
	t.Run("IdempotencyAndAtomicRollback", func(t *testing.T) {
		f := newFixture(t, dsn)
		_, a := f.issue(t)
		r, e := f.s.Send(f.ctx, a, "helper.ho", "one", "key")
		if e != nil || r.Stage != extshared.StageAccepted {
			t.Fatal(r, e)
		}
		r2, e := f.s.Send(f.ctx, a, "helper.ho", "one", "key")
		if e != nil || r2.DeliveryID != r.DeliveryID || f.touches(t) != 1 {
			t.Fatal("send duplicate", r2, e)
		}
		if _, e = f.s.Send(f.ctx, a, "helper.ho", "different", "key"); !errors.Is(e, extshared.ErrIdempotencyKey) {
			t.Fatalf("same key different request accepted: %v", e)
		}
		if _, e = f.s.Send(f.ctx, a, "helper.ho", "one", "key", true); !errors.Is(e, extshared.ErrIdempotencyKey) {
			t.Fatalf("same key changed expect_reply accepted: %v", e)
		}
		if _, e = f.s.Send(f.ctx, a, "other.ho", "one", "new"); !errors.Is(e, extshared.ErrTargetNotInAG) {
			t.Fatal(e)
		}
		_, e = f.pool.Exec(f.ctx, f.access, `CREATE FUNCTION reject_touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic touch failure'; END $$;CREATE TRIGGER reject_touch BEFORE INSERT ON test_touches FOR EACH ROW EXECUTE FUNCTION reject_touch();`)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.s.Send(f.ctx, a, "helper.ho", "two", "fail"); e == nil {
			t.Fatal("accepted failed group touch")
		}
		var n int
		row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_send_key WHERE client_message_id='fail'`)
		row.Scan(&n)
		if n != 0 {
			t.Fatal("send transaction did not roll back")
		}
	})
	t.Run("FeishuCopyEnqueueFailureIsolatedFromPrimarySend", func(t *testing.T) {
		f := newFixture(t, dsn)
		_, a := f.issue(t)
		f.s.FeishuCopies = failingFeishuCopyQueue{}
		copyFailures := 0
		f.s.OnFeishuCopyEnqueueError = func(error) { copyFailures++ }
		receipt, err := f.s.Send(f.ctx, a, "helper.ho", "完整主消息正文", "copy-failure-isolated")
		if err != nil || receipt.Stage != extshared.StageAccepted || copyFailures != 1 {
			t.Fatalf("MCP_COPY_FAILURE_ISOLATION receipt=%+v failures=%d err=%v", receipt, copyFailures, err)
		}
		row, err := f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_outbox WHERE delivery_id=$1`, receipt.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := row.Scan(&n); err != nil || n != 1 {
			t.Fatalf("MCP_COPY_FAILURE_ISOLATION primary outbox count=%d err=%v", n, err)
		}
		receipt, err = f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, "reply-copy-failure-isolated", "完整目标回复")
		if err != nil || receipt.Stage != extshared.StageAccepted || copyFailures != 2 {
			t.Fatalf("MCP_COPY_FAILURE_ISOLATION internal reply receipt=%+v failures=%d err=%v", receipt, copyFailures, err)
		}
		row, err = f.pool.QueryRow(f.ctx, f.access, `SELECT count(*) FROM np_extseat_inbox WHERE id=$1`, receipt.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		if err := row.Scan(&n); err != nil || n != 1 {
			t.Fatalf("MCP_COPY_FAILURE_ISOLATION primary inbox count=%d err=%v", n, err)
		}
	})
	t.Run("PollingNeverTouchesGroup", func(t *testing.T) {
		f := newFixture(t, dsn)
		raw, a := f.issue(t)
		if _, e := f.s.Authenticate(f.ctx, raw); e != nil {
			t.Fatal(e)
		}
		if _, e := f.s.Wait(f.ctx, a, 0); e != nil {
			t.Fatal(e)
		}
		if _, e := f.s.ListOnline(f.ctx, a); e != nil {
			t.Fatal(e)
		}
		if f.touches(t) != 0 {
			t.Fatal("poll/list unexpectedly touched group")
		}
		online, e := f.s.Online(f.ctx)
		if e != nil || len(online) != 1 {
			t.Fatal(online, e)
		}
		f.clock.add(extshared.PresenceTimeout)
		online, e = f.s.Online(f.ctx)
		if e != nil || len(online) != 0 {
			t.Fatal("presence boundary", online, e)
		}
	})
	t.Run("InboxLeaseRedeliveryAndFirstReceipt", func(t *testing.T) {
		f := newFixture(t, dsn)
		_, a := f.issue(t)
		r, e := f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, "message", "body")
		if e != nil {
			t.Fatal(e)
		}
		r2, e := f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, "message", "body")
		if e != nil || r2.DeliveryID != r.DeliveryID || f.touches(t) != 1 {
			t.Fatal(r2, e)
		}
		if _, e = f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, "message", "different"); !errors.Is(e, extshared.ErrIdempotencyKey) {
			t.Fatal(e)
		}
		if e = f.s.Ack(f.ctx, a, r.DeliveryID, 1); e == nil {
			t.Fatal("unclaimed ACK")
		}
		ds, e := f.s.Wait(f.ctx, a, 0)
		if e != nil || len(ds) != 1 {
			t.Fatal(ds, e)
		}
		ds, e = f.s.Wait(f.ctx, a, 0)
		if e != nil || len(ds) != 0 {
			t.Fatal("leased delivery duplicated", ds, e)
		}
		f.clock.add(5 * time.Second)
		ds, e = f.s.Wait(f.ctx, a, 0)
		if e != nil || len(ds) != 1 || ds[0].ID != r.DeliveryID || ds[0].Generation != 1 {
			t.Fatal("redelivery identity", ds, e)
		}
		if e = f.s.Ack(f.ctx, a, r.DeliveryID, 2); e == nil {
			t.Fatal("wrong generation ACK")
		}
		if e = f.s.Ack(f.ctx, a, r.DeliveryID, 1); e != nil {
			t.Fatal(e)
		}
		if e = f.s.Ack(f.ctx, a, r.DeliveryID, 1); e != nil {
			t.Fatal(e)
		}
		if f.touches(t) != 2 {
			t.Fatal("duplicate receipt touched group")
		}
		state, _, e := f.s.Receipt(f.ctx, a, r.DeliveryID)
		if e != nil || state != string(extshared.PhaseReceived) {
			t.Fatal(state, e)
		}
	})
	t.Run("MessageExpiryAndRevokedWait", func(t *testing.T) {
		f := newFixture(t, dsn)
		_, a := f.issue(t)
		r, e := f.s.EnqueueFromHelper(f.ctx, "helper", a.ID, "expiry", "body")
		if e != nil {
			t.Fatal(e)
		}
		f.clock.add(extshared.MessageTTL)
		if e = f.s.Expire(f.ctx); e != nil {
			t.Fatal(e)
		}
		state, reason, e := f.s.Receipt(f.ctx, a, r.DeliveryID)
		if e != nil || state != "dead" || reason != "message_expired" {
			t.Fatal(state, reason, e)
		}
		done := make(chan error, 1)
		go func() { _, e := f.s.Wait(f.ctx, a, time.Second); done <- e }()
		time.Sleep(25 * time.Millisecond)
		if e = f.s.Revoke(f.ctx, a.ID); e != nil {
			t.Fatal(e)
		}
		select {
		case e := <-done:
			if !errors.Is(e, extshared.ErrTokenRevoked) {
				t.Fatal(e)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("revoked wait not interrupted")
		}
	})
	t.Run("TenantAndUnforgeableSnapshot", func(t *testing.T) {
		f := newFixture(t, dsn)
		raw, a := f.issue(t)
		if _, e := f.s.Authenticate(f.ctx, raw+"x"); !errors.Is(e, extshared.ErrUnauthenticated) {
			t.Fatal(e)
		}
		a.proof = ""
		if e := f.s.Recheck(f.ctx, a); !errors.Is(e, extshared.ErrUnauthenticated) {
			t.Fatal("forged snapshot", e)
		}
		_, _, _, e := f.s.IssueToken(f.ctx, extshared.Principal{Provider: "feishu", TenantKey: "wrong", Issuer: "app", Subject: "open", DisplayName: "alice"}, "bot", "helper.ho")
		if !errors.Is(e, extshared.ErrTenantRejected) {
			t.Fatal(e)
		}
		if identity.ValidAgentName("bot_AG1") {
			t.Fatal("ordinary helper names widened")
		}
	})
}

func TestIssueTokenMissingTargetClassificationPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, _, _, err := f.s.IssueToken(f.ctx, extshared.Principal{Provider: "feishu", TenantKey: "tenant", Issuer: "app", Subject: "open", DisplayName: "alice"}, "bot", "missing.zzc")
	if !errors.Is(err, extshared.ErrTargetNotInAG) || !errors.Is(err, extshared.ErrTargetMissing) {
		t.Fatalf("TARGET_MISSING_CLASSIFICATION err=%v", err)
	}
}

type recordingProjector struct {
	mu   sync.Mutex
	ids  []string
	fail bool
}

func (p *recordingProjector) Project(ctx context.Context, d Delivery, target identity.Agent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ids = append(p.ids, d.ID)
	if p.fail {
		return errors.New("synthetic sqlite unavailable")
	}
	return nil
}
func TestOutboxAndIsolationPG(t *testing.T) {
	dsn := pgtest.DSN(t)
	t.Run("ProjectionRetryStableID", func(t *testing.T) {
		f := newFixture(t, dsn)
		_, a := f.issue(t)
		r, e := f.s.Send(f.ctx, a, "helper.ho", "literal https://example.invalid/a?b=c", "client")
		if e != nil {
			t.Fatal(e)
		}
		p := &recordingProjector{fail: true}
		did, e := f.s.ProjectOne(f.ctx, p)
		if !did || e == nil {
			t.Fatal("failed projection accepted")
		}
		p.fail = false
		did, e = f.s.ProjectOne(f.ctx, p)
		if e != nil || !did {
			t.Fatal(did, e)
		}
		did, e = f.s.ProjectOne(f.ctx, p)
		if e != nil || did {
			t.Fatal("projected twice")
		}
		if len(p.ids) != 2 || p.ids[0] != r.DeliveryID || p.ids[1] != r.DeliveryID {
			t.Fatal("projection regenerated delivery", p.ids)
		}
		if f.touches(t) != 1 {
			t.Fatal("projection touched group")
		}
	})
	t.Run("QueueLimitAndGroupBoundary", func(t *testing.T) {
		f := newFixture(t, dsn)
		_, a := f.issue(t)
		f.s.QueueLimit = 1
		if _, e := f.s.Send(f.ctx, a, "helper.ho", "first", "one"); e != nil {
			t.Fatal(e)
		}
		if _, e := f.s.Send(f.ctx, a, "helper.ho", "second", "two"); !errors.Is(e, extshared.ErrQueueFull) {
			t.Fatal(e)
		}
		if _, e := f.s.EnqueueFromHelper(f.ctx, "other", a.ID, "outside", "body"); !errors.Is(e, extshared.ErrTargetNotInAG) {
			t.Fatal(e)
		}
		f.clock.add(extshared.GroupTTL)
		if _, e := f.s.Wait(f.ctx, a, 0); !errors.Is(e, extshared.ErrGroupDissolved) {
			t.Fatal("expired group revived", e)
		}
	})
	t.Run("WrongSeatCannotAck", func(t *testing.T) {
		f := newFixture(t, dsn)
		_, a := f.issue(t)
		_, b := f.issue(t)
		r, e := f.s.EnqueueFromHelper(f.ctx, "helper", a.ID, "private", "secret fixture")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.s.Wait(f.ctx, a, 0); e != nil {
			t.Fatal(e)
		}
		if e = f.s.Ack(f.ctx, b, r.DeliveryID, 1); !errors.Is(e, extshared.ErrNotPermitted) {
			t.Fatal("cross seat ACK", e)
		}
		if _, _, e = f.s.Receipt(f.ctx, b, r.DeliveryID); !errors.Is(e, extshared.ErrNotPermitted) {
			t.Fatal("cross seat receipt", e)
		}
		if _, _, e = f.s.Identity.ResolveFullName(f.ctx, a.FullName); e != nil {
			t.Fatal("external full name", e)
		}
	})
	t.Run("SameKeyConcurrentSend", func(t *testing.T) {
		f := newFixture(t, dsn)
		_, a := f.issue(t)
		ids := make(chan string, 8)
		errs := make(chan error, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, e := f.s.Send(f.ctx, a, "helper.ho", "same", "concurrent")
				ids <- r.DeliveryID
				errs <- e
			}()
		}
		wg.Wait()
		close(ids)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		id := ""
		for x := range ids {
			if id == "" {
				id = x
			}
			if id != x {
				t.Fatal("multiple delivery IDs")
			}
		}
		if f.touches(t) != 1 {
			t.Fatal("concurrent repeated group touch")
		}
	})
}

func TestSecretsAndReceiptRollbackPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	raw, a := f.issue(t)
	var stored string
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT row_to_json(t)::text FROM np_extseat_token t WHERE id=$1`, a.TokenID)
	if e := row.Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(stored, raw) || !strings.Contains(stored, identity.HashSecret(raw)) {
		t.Fatal("token storage is not digest-only")
	}
	row, _ = f.pool.QueryRow(f.ctx, f.access, `SELECT coalesce(string_agg(after_json,''),'') FROM audit_log`)
	if e := row.Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(stored, raw) {
		t.Fatal("audit exposed bearer")
	}
	if _, e := f.pool.Exec(f.ctx, f.access, `UPDATE np_agent SET credential_hash=$2 WHERE id=$1`, a.ID, identity.HashSecret("fixture-external-credential")); e != nil {
		t.Fatal(e)
	}
	if _, _, e := f.s.Identity.AuthenticateAgent(f.ctx, "fixture-external-credential"); !errors.Is(e, identity.ErrCredentialInvalid) {
		t.Fatal("external used helper credential auth", e)
	}
	r, e := f.s.EnqueueFromHelper(f.ctx, "helper", a.ID, "rollback-receipt", "body")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Wait(f.ctx, a, 0); e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(f.ctx, f.access, `CREATE FUNCTION fail_ack_touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='first_received_ack' THEN RAISE EXCEPTION 'synthetic ACK touch failure'; END IF; RETURN NEW; END $$;CREATE TRIGGER fail_ack_touch BEFORE INSERT ON test_touches FOR EACH ROW EXECUTE FUNCTION fail_ack_touch();`)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Ack(f.ctx, a, r.DeliveryID, 1); e == nil {
		t.Fatal("ACK succeeded despite failed group touch")
	}
	state, _, e := f.s.Receipt(f.ctx, a, r.DeliveryID)
	if e != nil || state != "leased" {
		t.Fatal("receipt transaction did not roll back", state, e)
	}
	f.clock.add(time.Minute)
	if _, e = f.s.Wait(f.ctx, a, 0); e != nil {
		t.Fatal(e)
	}
	f.clock.add(-time.Second)
	if e = f.s.Recheck(f.ctx, a); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatal("presence clock rollback accepted", e)
	}
}

func TestTokenAudiencePG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	raw, a := f.issue(t)
	other := *f.s
	other.Resource = "https://another.example.invalid/mcp"
	if _, e := other.Authenticate(f.ctx, raw); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatal("wrong audience accepted", e)
	}
	if e := other.Recheck(f.ctx, a); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatal("snapshot crossed resource", e)
	}
	var audience, scopes string
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT audience,scopes FROM np_extseat_token WHERE id=$1`, a.TokenID)
	if e := row.Scan(&audience, &scopes); e != nil {
		t.Fatal(e)
	}
	if audience != f.s.Resource || scopes != "list_online send_message wait_for_messages ack" {
		t.Fatal("audience/scope not persisted")
	}
	other.Resource = ""
	if _, e := other.Authenticate(f.ctx, raw); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatal("empty resource accepted", e)
	}
	if _, _, _, e := other.IssueToken(f.ctx, a.Principal, "bot", "helper.ho"); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatal("empty resource issued token", e)
	}
	if _, e := f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_token SET scopes='list_online' WHERE id=$1`, a.TokenID); e == nil {
		t.Fatal("subset scopes persisted")
	}
	// 仅在隔离fixture内移除DB约束，独立验证应用层也拒绝不完整授权。
	if _, e := f.pool.Exec(f.ctx, f.access, `ALTER TABLE np_extseat_token DROP CONSTRAINT np_extseat_token_scopes_check`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(f.ctx, f.access, `UPDATE np_extseat_token SET scopes='list_online' WHERE id=$1`, a.TokenID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.Authenticate(f.ctx, raw); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatal("subset scopes authenticated", e)
	}
	if e := f.s.Recheck(f.ctx, a); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatal("subset scopes snapshot accepted", e)
	}
}

type transientProjector struct {
	mu    sync.Mutex
	calls int
	ids   []string
}

func (p *transientProjector) Project(ctx context.Context, d Delivery, target identity.Agent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.ids = append(p.ids, d.ID)
	if p.calls == 1 {
		return errors.New("synthetic transient projection failure")
	}
	return nil
}
func TestBackgroundRetriesOutboxPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, a := f.issue(t)
	r, e := f.s.Send(f.ctx, a, "helper.ho", "body", "worker")
	if e != nil {
		t.Fatal(e)
	}
	p := &transientProjector{}
	ctx, cancel := context.WithTimeout(f.ctx, 1500*time.Millisecond)
	defer cancel()
	if e = f.s.Run(ctx, p); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.ids) != 2 || p.ids[0] != r.DeliveryID || p.ids[1] != r.DeliveryID {
		t.Fatal("worker abandoned or regenerated failed outbox", p.ids)
	}
	var state string
	row, _ := f.pool.QueryRow(f.ctx, f.access, `SELECT state FROM np_extseat_outbox WHERE delivery_id=$1`, r.DeliveryID)
	row.Scan(&state)
	if state != "projected" {
		t.Fatal(state)
	}
}

type advancingGroups struct {
	Groups
	clock *testClock
}

func (g advancingGroups) RecheckGroup(ctx context.Context, tx repo.Tx, a Seat, at time.Time) (Group, error) {
	result, e := g.Groups.RecheckGroup(ctx, tx, a, at)
	g.clock.add(extshared.TokenTTL)
	return result, e
}
func TestWaitingAuthenticationCannotReviveExpiredPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	raw, _ := f.issue(t)
	f.keepGroup(t)
	f.s.Groups = advancingGroups{Groups: f.s.Groups, clock: f.clock}
	if _, e := f.s.Authenticate(f.ctx, raw); !errors.Is(e, extshared.ErrTokenExpired) {
		t.Fatal("authentication lock wait revived expired token", e)
	}
}

// 真正阻塞在令牌行锁上，释放锁时已到期，不能沿用请求开始时间续期。
func TestTokenRowLockWaitRechecksClockPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	raw, a := f.issue(t)
	f.keepGroup(t)
	result := make(chan error, 1)
	e := f.pool.InTx(f.ctx, f.access, nil, func(tx repo.Tx) error {
		var id string
		if e := tx.QueryRow(f.ctx, `SELECT id FROM np_extseat_token WHERE id=$1 FOR UPDATE`, a.TokenID).Scan(&id); e != nil {
			return e
		}
		go func() { _, e := f.s.Authenticate(f.ctx, raw); result <- e }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var waiting bool
			row, e := f.pool.QueryRow(f.ctx, f.access, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'SELECT t.id,t.token_hash%')`)
			if e != nil {
				return e
			}
			if e = row.Scan(&waiting); e != nil {
				return e
			}
			if waiting {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("authentication never waited on token lock")
			}
			time.Sleep(10 * time.Millisecond)
		}
		f.clock.add(extshared.TokenTTL)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-result:
		if !errors.Is(e, extshared.ErrTokenExpired) {
			t.Fatal("token row lock wait revived expired token", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("authentication remained blocked after token lock release")
	}
}

type lockedIdentityProjector struct{ f *fixture }

func (p lockedIdentityProjector) Project(ctx context.Context, d Delivery, target identity.Agent) error {
	if d.SourceAgent.ID == "" || d.SourceAgent.Origin != identity.OriginExternal || target.ID != "helper" {
		return errors.New("projection missing locked identity snapshots")
	}
	for _, id := range []string{d.SourceAgent.ID, target.ID} {
		e := p.f.pool.InTx(ctx, p.f.access, nil, func(tx repo.Tx) error {
			if _, e := tx.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); e != nil {
				return e
			}
			_, e := tx.Exec(ctx, `UPDATE np_agent SET state='revoked' WHERE id=$1`, id)
			if e != nil {
				return e
			}
			return errors.New("projection identity update was not locked")
		})
		if e == nil || !strings.Contains(e.Error(), "lock timeout") {
			return fmt.Errorf("projection identity not transactionally locked: %s: %v", id, e)
		}
	}
	return nil
}
func TestProjectionHoldsBothIdentityLocksPG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, a := f.issue(t)
	if _, e := f.s.Send(f.ctx, a, "helper.ho", "body", "locked-identities"); e != nil {
		t.Fatal(e)
	}
	if did, e := f.s.ProjectOne(f.ctx, lockedIdentityProjector{f}); !did || e != nil {
		t.Fatal(did, e)
	}
}
