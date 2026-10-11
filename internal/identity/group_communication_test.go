package identity_test

import (
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"testing"
)

func TestGroupLeaderOnlyCommunicationAndBypass(t *testing.T) {
	f := newFixture(t)
	leader, a, b := f.hosted(t, "zzc", "Lead"), f.hosted(t, "zzc", "Alpha"), f.hosted(t, "lbl", "Beta")
	g, err := f.svc.CreateGroup(bg, "zzc", leader.ID, tg())
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []*identity.Agent{a, b} {
		if err := f.svc.JoinGroup(bg, x.OwnerLC, g.Code, x.ID); err != nil {
			t.Fatal(err)
		}
	}
	check := func(x, y *identity.Agent, want bool) {
		t.Helper()
		got, err := f.svc.AgentsCanCommunicate(bg, x, y)
		if err != nil || got != want {
			t.Fatalf("%s -> %s: %v %v want %v", x.Name, y.Name, got, err, want)
		}
	}
	check(a, b, true)
	if err := f.svc.SetGroupLeaderOnly(bg, "lbl", g.Code, true); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("noncreator mode change: %v", err)
	}
	if err := f.svc.SetGroupLeaderOnly(bg, "zzc", g.Code, true); err != nil {
		t.Fatal(err)
	}
	check(leader, a, true)
	check(a, leader, true)
	check(a, b, false)
	check(b, a, false)
	bypass, err := f.svc.CreateGroup(bg, "zzc", a.ID, tg())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.JoinGroup(bg, "lbl", bypass.Code, b.ID); err != nil {
		t.Fatal(err)
	}
	check(a, b, false)
	cs, err := f.svc.MentionCandidates(bg, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Agent.ID == b.ID {
			t.Fatal("restricted peer leaked in mentions")
		}
	}
	if err := f.svc.SetGroupLeaderOnly(bg, "zzc", g.Code, false); err != nil {
		t.Fatal(err)
	}
	check(a, b, true)
	outsider := f.hosted(t, "zzc", "Outside")
	check(a, outsider, false)
	outsider.State = identity.AgentSuspended
	check(leader, outsider, false)
}
