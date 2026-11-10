package subagent

import (
	"strings"
	"testing"
)

func TestRouteParser(t *testing.T) {
	for _, tc := range []struct {
		input string
		root  string
		depth int
	}{
		{"agent", "agent", 0}, {"agent.user", "agent", 0}, {"r2.agent.user", "agent", 1}, {"r21.r2.agent.user", "agent", 2}, {"r211.r21.r2.agent.user", "agent", 3}, {"short_AG3.user", "short_ag3", 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := Parse(tc.input, "user")
			if err != nil || got.Root != tc.root || len(got.Path) != tc.depth {
				t.Fatalf("AA001_PARSE %+v %v", got, err)
			}
			if tc.depth == 3 && strings.Join(got.Path, ",") != "r2,r21,r211" {
				t.Fatal("AA001_PATH_ORDER", got)
			}
			if got.FullName() != strings.ToLower(tc.input) && tc.input != "agent" {
				t.Fatal("AA001_ROUNDTRIP", got)
			}
		})
	}
	for _, name := range []string{"agent.", ".agent.user", "r-.agent.user", "r2.ag/ent.user", "r2.аgent.user", "r2.short_AG3.user", strings.Repeat("a", 33) + ".agent.user"} {
		if _, err := Parse(name, "user"); err == nil {
			t.Fatalf("AA001_MALFORMED %s", name)
		}
	}
	if _, err := Parse("aa.bb.cc.dd.agent.user", "user"); err == nil || !strings.Contains(err.Error(), "depth_exceeded") {
		t.Fatalf("AA002_DEPTH_EXCEEDED %v", err)
	}
}
func TestSnapshotCanonicalDigest(t *testing.T) {
	a := Node{NodeID: "node-a", Name: "aa", Aliases: []string{"zz", "bb"}, Generation: 1, State: "active"}
	b := Node{NodeID: "node-b", Name: "cc", Generation: 1, State: "active"}
	s := Snapshot{Revision: 1, Nodes: []Node{a, b}}
	first, err := s.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	a.Aliases = []string{"bb", "zz"}
	s.Nodes = []Node{b, a}
	s.ConnSeq = 99
	s.BaseRevision = 88
	second, err := s.CanonicalDigest()
	if err != nil || first != second {
		t.Fatal("AA009_CANONICAL_DIGEST", first, second, err)
	}
	s.Nodes[0].Aliases = []string{"aa"}
	if _, err = s.CanonicalDigest(); err == nil {
		t.Fatal("AA003_ALIAS_CONFLICT")
	}
}
