package httpapi_test

import (
	"context"
	"testing"
)

// R-V2-0102-GROUP-FIXES B：入网审批的 new_group_name 先清洗（去不可见字符与首尾空白）再校验，落库用清洗后的值。
// 断言面＝审批 HTTP 接口＋存储里的组名；实现承诺＝identity.NormalizeGroupName。
func TestApproveNewGroupNameIsNormalized(t *testing.T) {
	e := newEnv(t)
	approve := func(agent, group string) resp {
		t.Helper()
		a := e.do("POST", "/api/enroll/applications", applyBody("zzc"), nil)
		id := a.body["application_id"].(string)
		return e.do("POST", "/api/console/enroll/applications/"+id+"/approve",
			map[string]any{"pairing_code": code, "agent_name": agent, "new_group_name": group}, owner("zzc"))
	}
	if r := approve("Zwsp", "\u200b test\u2060 "); r.status != 200 {
		t.Fatalf("零宽＋首尾空白的合法名应通过：%d %v", r.status, r.body)
	}
	gs, _ := e.store.ListGroups(context.Background())
	found := false
	for _, g := range gs {
		found = found || g.ShortName == "test"
	}
	if !found {
		t.Fatalf("落库应为清洗后的 test：%+v", gs)
	}
	if r := approve("Onlyzw", "\u200b\ufeff"); r.status != 422 || r.body["error"] != "group_name_invalid" {
		t.Fatalf("清洗后为空 ⇒ group_name_invalid：%d %v", r.status, r.body)
	}
}
