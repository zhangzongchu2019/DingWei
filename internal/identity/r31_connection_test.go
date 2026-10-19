package identity

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"testing"
	"time"
)

func TestR31ConnectionPrivacyPGAndMemory(t *testing.T) {
	ctx := context.Background()
	mem := NewMemStore()
	pool := pgtest.Pool(t)
	if _, err := migrate.Migrate(ctx, pool, migrate.HubDomains()); err != nil {
		t.Fatal(err)
	}
	pg := NewPGStore(pool)
	tc, err := repo.NewAccessCtx("test", "r31-connection")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		store   Store
		seed    func()
		disable func()
	}{
		{"memory", mem, func() {
			mem.agents["ag_one"] = &Agent{ID: "ag_one", OwnerLC: "owner", Origin: OriginSelfhost, State: AgentActive}
			mem.agents["ag_hosted"] = &Agent{ID: "ag_hosted", OwnerLC: "owner", Origin: OriginHosted, State: AgentActive}
		}, func() { mem.users["owner"].State = UserDisabled }},
		{"pg", pg, func() {
			if _, err := pool.Exec(ctx, tc, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('ag_one','owner','One','one','selfhost','active',now()),('ag_hosted','owner','Hosted','hosted','hosted','active',now())`); err != nil {
				t.Fatal(err)
			}
		}, func() {
			if _, err := pool.Exec(ctx, tc, `UPDATE np_user SET state='disabled' WHERE user_id='owner'`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"owner", "admin", "other"} {
				typ := UserMember
				if name == "admin" {
					typ = UserPlatformAdmin
				}
				if err := tc.store.CreateUser(ctx, User{UserID: name, ShortName: name, State: UserActive, Type: typ, PasswordHash: "fixture", CreatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			tc.seed()
			at := time.Now().UTC().Truncate(time.Microsecond)
			if err := tc.store.RecordAgentConnection(ctx, "ag_one", "2001:db8::1", at); err != nil {
				t.Fatal(err)
			}
			if err := tc.store.RecordAgentConnection(ctx, "ag_one", "198.51.100.2", at.Add(-time.Second)); err != nil {
				t.Fatal(err)
			}
			for _, reader := range []string{"owner", "admin"} {
				got, err := tc.store.ReadAgentConnection(ctx, reader, "ag_one")
				if err != nil || got.IP != "2001:db8::1" || !got.At.Equal(at) {
					t.Fatalf("reader=%s got=%+v err=%v", reader, got, err)
				}
			}
			for _, reader := range []string{"other", "missing"} {
				got, err := tc.store.ReadAgentConnection(ctx, reader, "ag_one")
				if err == nil || got.IP != "" {
					t.Fatalf("IP disclosed to %s: %+v %v", reader, got, err)
				}
			}
			if err := tc.store.RecordAgentConnection(ctx, "ag_one", "evil.example", at.Add(time.Second)); err == nil {
				t.Fatal("invalid source accepted")
			}
			if got, err := tc.store.ReadAgentConnection(ctx, "owner", "ag_hosted"); err == nil || got.IP != "" {
				t.Fatal("hosted source IP disclosed")
			}
			tc.disable()
			if got, err := tc.store.ReadAgentConnection(ctx, "owner", "ag_one"); err == nil || got.IP != "" {
				t.Fatal("disabled owner read IP")
			}
		})
	}
}
