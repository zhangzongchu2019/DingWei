package identitytest

import (
	"sort"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// listAgentsOwnerScoped 钉住 ListAgents 的属主过滤（Mem 与 PG 同一格）：
// 业主甲只看得到自己的 Agent（属主名大小写不同也算同一人），看不到业主乙的；乙同理。
// 这条过滤是 console 左栏「别人的编组成员不会出现在自己的 Agent 列表里」的事实依据
// （view-service/console-agent-tree-regression.js 的夹具据此只给自己的 Agent），此前没有任何用例钉它。
func listAgentsOwnerScoped(t *testing.T, s identity.Store) {
	mustApp(t, s, "app-1", "zzc", "dev-1", t0)
	mustApp(t, s, "app-2", "ZZC", "dev-2", t0)
	mustApp(t, s, "app-3", "lbl", "dev-3", t0)
	mustApp(t, s, "app-4", "Lbl", "dev-4", t0)
	mustApprove(t, s, "app-1", "zzc", "ag-1", "Alpha", t0)
	mustApprove(t, s, "app-2", "ZZC", "ag-2", "Beta", t0)
	mustApprove(t, s, "app-3", "lbl", "ag-3", "Gamma", t0)
	mustApprove(t, s, "app-4", "Lbl", "ag-4", "Delta", t0)

	names := func(owner string) string {
		got, err := s.ListAgents(ctx, owner)
		if err != nil {
			t.Fatalf("ListAgents(%s): %v", owner, err)
		}
		var out []string
		for _, a := range got {
			if !strings.EqualFold(a.OwnerLC, owner) {
				t.Fatalf("ListAgents(%s) returned %s owned by %q", owner, a.Name, a.OwnerLC)
			}
			out = append(out, a.Name)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for _, owner := range []string{"zzc", "ZZC", "Zzc"} {
		if got := names(owner); got != "Alpha,Beta" {
			t.Fatalf("owner %q sees [%s], want [Alpha,Beta] (own agents only, any case)", owner, got)
		}
	}
	for _, owner := range []string{"lbl", "LBL"} {
		if got := names(owner); got != "Delta,Gamma" {
			t.Fatalf("owner %q sees [%s], want [Delta,Gamma]", owner, got)
		}
	}
	if got := names("nobody"); got != "" {
		t.Fatalf("an owner with no agents sees [%s], want none", got)
	}
}
