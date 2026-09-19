package store

import (
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestAgentGroupCodeMembershipAndSingleLeader(t *testing.T) {
	db, ctx := newTestSQLite(t)
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: "short", LeaderKeyID: "k1", LeaderSessionName: "s1", LeaderOwnerKey: "o1"}); err == nil {
		t.Fatal("short non-base64 group code accepted")
	}
	code := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: code, LeaderKeyID: "k1", LeaderSessionName: "s1", LeaderOwnerKey: "o1"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignAgentGroup(ctx, model.AgentGroupMember{Code: code, KeyID: "k2", SessionName: "s2", OwnerKey: "o2"}); err != nil {
		t.Fatal(err)
	}
	members, err := db.ListAgentGroupMembers(ctx, code)
	if err != nil || len(members) != 2 {
		t.Fatalf("members=%+v err=%v", members, err)
	}
	if err := db.UnassignAgentGroup(ctx, code, "k1", "s1"); err == nil {
		t.Fatal("leader removal accepted without appointing a replacement")
	}
	if err := db.UpsertAgentGroup(ctx, model.AgentGroup{Code: code, LeaderKeyID: "k2", LeaderSessionName: "s2", LeaderOwnerKey: "o2"}); err != nil {
		t.Fatal(err)
	}
	groups, err := db.ListAgentGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].LeaderKeyID != "k2" || groups[0].LeaderSessionName != "s2" {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	if err := db.UnassignAgentGroup(ctx, code, "k1", "s1"); err != nil {
		t.Fatalf("old leader remained protected after replacement: %v", err)
	}
}
