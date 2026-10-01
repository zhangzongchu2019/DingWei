package identity_test

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

func (f *fixture) hosted(t *testing.T, owner, name string) *identity.Agent {
	t.Helper()
	hi, err := f.svc.IssueHostedAgentWithNewGroup(bg, owner, name, tg(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return &hi.Agent
}

func TestValidGroupName(t *testing.T) {
	for name, want := range map[string]bool{
		"开发组": true, "a": true, "Dev1": true, "八个字八个字八个": true, "Ⅳ组": true,
		"": false, "九个字九个字九个字": false, " dev": false, "dev ": false, "de-v": false, "de v": false, "组.一": false,
	} {
		if got := identity.ValidGroupName(name); got != want {
			t.Errorf("ValidGroupName(%q) = %v, want %v", name, got, want)
		}
	}
}

// 组码：32 位、取自 64 字符字母表、每次不同。64 整除 256 —— randFrom 的拒绝采样上限若放进 byte 会溢出成 0、永远取不到字符（本用例会卡住）。
func TestCreateGroupCodeAndValidation(t *testing.T) {
	f := newFixture(t)
	z1, z2 := f.hosted(t, "zzc", "Zed"), f.hosted(t, "zzc", "Zed2")
	if _, err := f.svc.CreateGroup(bg, "zzc", z1.ID, "九个字九个字九个字"); !errors.Is(err, identity.ErrBadRequest) {
		t.Fatalf("bad name: %v", err)
	}
	// 限时：取组码卡死（拒绝采样上限溢出）时给出断言失败，而不是等 go test 总时限 panic。
	type res struct {
		g   *identity.Group
		err error
	}
	done := make(chan res, 1)
	go func() { g, err := f.svc.CreateGroup(bg, "ZZC", z1.ID, "开发组"); done <- res{g, err} }()
	var g1 *identity.Group
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		g1 = r.g
	case <-time.After(5 * time.Second):
		t.Fatal("CreateGroup did not return within 5s: group code generation is stuck (rejection-sampling limit overflow?)")
	}
	g2, err := f.svc.CreateGroup(bg, "zzc", z2.ID, "测试组")
	if err != nil {
		t.Fatal(err)
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	for _, g := range []*identity.Group{g1, g2} {
		if len(g.Code) != identity.GroupCodeLen || strings.Trim(g.Code, alphabet) != "" || g.CreatorLC != "zzc" {
			t.Fatalf("group %+v", g)
		}
	}
	if g1.Code == g2.Code {
		t.Fatal("two groups got the same code")
	}
	if _, err := f.svc.CreateGroup(bg, "lbl", z1.ID, "别人组"); !errors.Is(err, identity.ErrGroupAgentUnavailable) {
		t.Fatalf("leader not owned: %v", err)
	}
	if _, err := f.svc.CreateGroup(bg, "zzc", z1.ID, "开发组"); !errors.Is(err, identity.ErrGroupNameTaken) {
		t.Fatalf("dup name: %v", err)
	}
	if got, _ := f.svc.SuggestGroupNames(bg, "开发组", 2); strings.Join(got, ",") != "开发组1,开发组2" {
		t.Fatalf("suggestions %v", got)
	}
	if _, err := f.svc.CreateGroup(bg, "zzc", z2.ID, "八个字八个字八1"); err != nil {
		t.Fatal(err)
	}
	// 截断后接数字，避开已有的
	if got, _ := f.svc.SuggestGroupNames(bg, "八个字八个字八个", 2); strings.Join(got, ",") != "八个字八个字八2,八个字八个字八3" {
		t.Fatalf("truncated suggestions %v", got)
	}
}

// 可见：自己建的，或成员里有自己（未吊销）的 Agent；吊销的成员不展示；只有建组人 CanManage。
func TestListVisibleGroups(t *testing.T) {
	f := newFixture(t)
	z, l := f.hosted(t, "zzc", "Zed"), f.hosted(t, "lbl", "Lee")
	gz, err := f.svc.CreateGroup(bg, "zzc", z.ID, "甲组")
	if err != nil {
		t.Fatal(err)
	}
	f.clk.T = f.clk.T.Add(time.Minute) // 列表按建组时刻排序；同一时刻按组码，而组码是随机的
	if _, err := f.svc.CreateGroup(bg, "lbl", l.ID, "乙组"); err != nil {
		t.Fatal(err)
	}
	// visible 只看本用例建的组：夹具给每个托管 Agent 建的 t<n> 组一律滤掉（它们与本用例的组同刻、组码随机，位置不定）。
	visible := func(actor string) []identity.GroupInfo {
		gs, err := f.svc.ListVisibleGroups(bg, actor)
		if err != nil {
			t.Fatal(err)
		}
		var out []identity.GroupInfo
		for _, g := range gs {
			if !fixtureGroup.MatchString(g.ShortName) {
				out = append(out, g)
			}
		}
		return out
	}
	names := func(actor string) string {
		var out []string
		for _, g := range visible(actor) {
			out = append(out, g.ShortName)
		}
		return strings.Join(out, ",")
	}
	if got := names("zzc"); got != "甲组" {
		t.Fatalf("zzc sees %q", got)
	}
	f.clk.T = f.clk.T.Add(time.Minute) // 入组晚于建组：成员按入组时刻排序
	if err := f.svc.JoinGroup(bg, "lbl", gz.Code, l.ID); err != nil {
		t.Fatal(err)
	}
	if got := names("lbl"); got != "甲组,乙组" {
		t.Fatalf("lbl sees %q (member of 甲组, creator of 乙组)", got)
	}
	gs := visible("lbl")
	g := gs[0]
	if g.CanManage || g.LeaderFullName != "Zed.zzc" || g.Creator == nil || g.Creator.UserID != "zzc" || len(g.Members) != 2 ||
		g.Members[0].FullName != "Zed.zzc" || g.Members[1].FullName != "Lee.lb" {
		t.Fatalf("甲组 as seen by lbl: %+v", g)
	}
	if !gs[1].CanManage {
		t.Fatal("lbl must manage its own group")
	}
	// lbl 的 Agent 被吊销 ⇒ 甲组里不再展示它，lbl 也不再看得见甲组
	if _, err := f.svc.RetireHostedAgent(bg, l.ID, "g4"); err != nil {
		t.Fatal(err)
	}
	if got := names("lbl"); got != "乙组" {
		t.Fatalf("after revoke lbl sees %q", got)
	}
	gs = visible("zzc")
	if len(gs) != 1 || gs[0].ShortName != "甲组" || len(gs[0].Members) != 1 || gs[0].Members[0].Agent.ID != z.ID {
		t.Fatalf("revoked member still shown: %+v", gs)
	}
	if got := names("newbie"); got != "" {
		t.Fatalf("outsider sees %q", got)
	}
}

var tgSeq atomic.Int64

// tg 测试用的新组名：每次不同（组名全局唯一、1～8 个字母或数字）。批准与托管新建都必须带编组（GroupAllowed）。
func tg() string { return fmt.Sprintf("t%d", tgSeq.Add(1)) }

var fixtureGroup = regexp.MustCompile(`^t[0-9]+$`)
