package identity_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/identitytest"
)

func TestDeviceClaimContracts(t *testing.T) {
	for _, backend := range []string{"memory", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var factory func(*testing.T) identity.Store
			if backend == "postgres" {
				factory = pgFactory(t)
			} else {
				factory = func(*testing.T) identity.Store { return identity.NewMemStore() }
			}
			for _, scenario := range []string{"new", "existing", "takeover", "short-conflict", "disabled", "replay", "expired", "wrong-token", "concurrent", "different-claims-same-identity", "preferred-name-reuses-own-account"} {
				t.Run(scenario, func(t *testing.T) {
					st := factory(t)
					now := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
					ob := identity.NewOnboarding(identity.NewService(st, func() time.Time { return now }, nil))
					ob.CodeKey = "synthetic-claim-key"
					ctx := context.Background()
					sender := identity.Identity{Provider: "feishu", Issuer: "fixture-app", Subject: "fixture-person"}
					code := "ABCDEFGHJKMN"
					req := identity.ClaimRequest{UserID: "alice", ShortName: "al", AgentName: "desktop", CodeDigest: identity.HashSecret(code), DeviceFingerprint: strings.Repeat("a", 64), ClientVersion: "0.2.2.2", Mode: "desktop"}
					if scenario == "takeover" || scenario == "disabled" || scenario == "existing" || scenario == "preferred-name-reuses-own-account" || scenario == "short-conflict" {
						uid, short := "alice", "al"
						if scenario == "short-conflict" {
							uid = "another"
						}
						if e := st.CreateUser(ctx, identity.User{UserID: uid, ShortName: short, Type: identity.UserMember, State: identity.UserPendingBind, CreatedAt: now}); e != nil {
							t.Fatal(e)
						}
						if e := st.CreateRegistrationCode(ctx, uid, "fixture-reg", now.Add(time.Hour)); e != nil {
							t.Fatal(e)
						}
						bound := sender
						if scenario == "takeover" || scenario == "short-conflict" {
							bound.Subject = "other-person"
						}
						if _, e := st.RedeemRegistrationCode(ctx, "fixture-reg", bound, now); e != nil {
							t.Fatal(e)
						}
						if scenario == "disabled" {
							if e := st.SetUserState(ctx, "alice", identity.UserDisabled, now); e != nil {
								t.Fatal(e)
							}
						}
					}
					if scenario == "preferred-name-reuses-own-account" {
						req.UserID = "newpreferred"
						req.ShortName = "np"
					}
					res, e := ob.ApplyDeviceClaim(ctx, req, "fixture-ip")
					if e != nil {
						t.Fatal(e)
					}
					before, e := ob.PollDeviceClaim(ctx, res.ID, res.PollToken)
					if e != nil || before.State != "awaiting_feishu" {
						t.Fatalf("unowned: %+v %v", before, e)
					}
					if scenario == "wrong-token" {
						if _, e = ob.PollDeviceClaim(ctx, res.ID, "bad"); !errors.Is(e, identity.ErrNotFound) {
							t.Fatal(e)
						}
						return
					}
					if scenario == "expired" {
						now = now.Add(identity.ClaimTTL)
						if _, e = ob.PollDeviceClaim(ctx, res.ID, res.PollToken); !errors.Is(e, identity.ErrExpired) {
							t.Fatal(e)
						}
						ob.HandleDeviceClaim(ctx, sender, code)
						if users, _ := st.CountUsers(ctx); users != 0 {
							t.Fatal("expired created user")
						}
						return
					}
					if scenario == "concurrent" {
						var wg sync.WaitGroup
						for i := 0; i < 12; i++ {
							wg.Add(1)
							go func() { defer wg.Done(); ob.HandleDeviceClaim(ctx, sender, code) }()
						}
						wg.Wait()
					} else {
						ob.HandleDeviceClaim(ctx, sender, code)
					}
					if scenario == "takeover" || scenario == "short-conflict" || scenario == "disabled" {
						now = now.Add(2 * time.Second)
						p, e := ob.PollDeviceClaim(ctx, res.ID, res.PollToken)
						if e != nil || p.State != "awaiting_feishu" {
							t.Fatalf("unsafe confirmation: %+v %v", p, e)
						}
						ags, _ := st.ListAgents(ctx, "alice")
						if len(ags) != 0 {
							t.Fatal("denied created agent")
						}
						return
					}
					now = now.Add(2 * time.Second)
					p, e := ob.PollDeviceClaim(ctx, res.ID, res.PollToken)
					if e != nil || p.State != "credential_ready" || p.Credential == "" || p.FullName != "desktop.al" {
						t.Fatalf("ready: %+v %v", p, e)
					}
					if scenario == "preferred-name-reuses-own-account" && (p.UserID != "alice" || !p.OwnerReused) {
						t.Fatal("did not reuse confirmed identity")
					}
					u, e := st.ResolveIdentity(ctx, sender)
					if e != nil || u.Type != identity.UserMember || u.PasswordHash != "" {
						t.Fatal("unsafe user", e)
					}
					if scenario == "different-claims-same-identity" {
						req.UserID = "intruder"
						req.ShortName = "in"
						req.CodeDigest = identity.HashSecret("PQRSTUVWXYZ2")
						second, e := ob.ApplyDeviceClaim(ctx, req, "fixture-ip")
						if e != nil {
							t.Fatal(e)
						}
						ob.HandleDeviceClaim(ctx, sender, "PQRSTUVWXYZ2")
						cp, _ := st.(identity.ClaimStore).GetDeviceClaim(ctx, second.ID)
						if cp.State != "awaiting_feishu" {
							t.Fatal("identity attached twice")
						}
						if n, _ := st.CountUsers(ctx); n != 1 {
							t.Fatal("orphan user")
						}
					}
					if scenario == "replay" {
						ob.HandleDeviceClaim(ctx, identity.Identity{Provider: "feishu", Issuer: "fixture-app", Subject: "attacker"}, code)
					}
					now = now.Add(2 * time.Second)
					again, e := ob.PollDeviceClaim(ctx, res.ID, res.PollToken)
					if e != nil || again.Credential != p.Credential {
						t.Fatal("lost response rotated credential", e)
					}
					if e = ob.AckDeviceClaim(ctx, res.ID, "bad"); !errors.Is(e, identity.ErrNotFound) {
						t.Fatal(e)
					}
					if e = ob.AckDeviceClaim(ctx, res.ID, res.PollToken); e != nil {
						t.Fatal(e)
					}
					if e = ob.AckDeviceClaim(ctx, res.ID, res.PollToken); e != nil {
						t.Fatal("ack not idempotent", e)
					}
					now = now.Add(2 * time.Second)
					done, e := ob.PollDeviceClaim(ctx, res.ID, res.PollToken)
					if e != nil || done.State != "completed" || done.Credential != "" {
						t.Fatalf("after ack: %+v %v", done, e)
					}
					ags, _ := st.ListAgents(ctx, "alice")
					if len(ags) != 1 {
						t.Fatal("duplicate devices", len(ags))
					}
				})
			}
		})
	}
}

func TestDeviceClaimPGFailureRollsBack(t *testing.T) {
	pool, tc := pgSchemaFactory(t)(t)
	ctx := context.Background()
	for _, ddl := range identitytest.PGTestSchema() {
		if _, e := pool.Exec(ctx, tc, ddl); e != nil {
			t.Fatal(e)
		}
	}
	// Force the final audit step to fail after the user, identity, agent and claim writes.
	if _, e := pool.Exec(ctx, tc, `CREATE FUNCTION reject_claim_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$`); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, tc, `CREATE TRIGGER reject_claim_audit BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION reject_claim_audit()`); e != nil {
		t.Fatal(e)
	}
	st := identity.NewPGStore(pool)
	ob := identity.NewOnboarding(identity.NewService(st, nil, nil))
	ob.CodeKey = "fixture-key"
	result, e := ob.ApplyDeviceClaim(ctx, identity.ClaimRequest{UserID: "alice", ShortName: "al", AgentName: "desktop", CodeDigest: identity.HashSecret("ABCDEFGHJKMN"), DeviceFingerprint: strings.Repeat("a", 64), ClientVersion: "0.2.2.2", Mode: "desktop"}, "fixture-ip")
	if e != nil {
		t.Fatal(e)
	}
	ob.HandleDeviceClaim(ctx, identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "person"}, "ABCDEFGHJKMN")
	if n, e := st.CountUsers(ctx); e != nil || n != 0 {
		t.Fatal("partial user after failure", n, e)
	}
	if agents, e := st.ListAgents(ctx, "alice"); e != nil || len(agents) != 0 {
		t.Fatal("partial agent after failure", e)
	}
	claim, e := st.GetDeviceClaim(ctx, result.ID)
	if e != nil || claim.State != "awaiting_feishu" || claim.AgentID != "" {
		t.Fatal("claim consumed on rollback", e)
	}
}
