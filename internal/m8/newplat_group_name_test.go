package m8

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// R-V2-0102-GROUP-FIXES B：添加编组页（新平台 POST /api/console/agent-groups）先清洗再校验、落库用清洗值；
// 三种 400 为带码的中文。断言面＝HTTP 状态与正文逐字、存储里的组名；实现承诺＝newplat_groups.go POST 分支。
func TestNewPlatformGroupCreateNormalizesAndExplainsInChinese(t *testing.T) {
	v := newViewEnv(t)
	ag, err := v.svc.Store.FindAgentByName(v.ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/console/agent-groups", v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroups))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	post := func(body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+"/api/console/agent-groups", strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: v.tokZZC})
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", npOrigin)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(b))
	}
	stored := func(name string) bool {
		gs, _ := v.svc.Store.ListGroups(context.Background())
		for _, g := range gs {
			if g.ShortName == name {
				return true
			}
		}
		return false
	}
	// 零宽、BOM、首尾空白 ⇒ 201，落库为清洗后的名
	if st, body := post(`{"agent_id":"` + ag.ID + `","short_name":"\u200btest\u2060 "}`); st != 201 || !strings.Contains(body, `"short_name":"test"`) {
		t.Fatalf("zero-width test: %d %s", st, body)
	}
	if !stored("test") || stored("test\u200b") {
		t.Fatal("落库应为 test")
	}
	invalid := `{"error":"group_name_invalid","message":"` + groupNameInvalidText + `"}`
	for _, name := range []string{"te st", "test-1", "九个字九个字九个字", "\u200b"} {
		if st, body := post(`{"agent_id":"` + ag.ID + `","short_name":"` + name + `"}`); st != 400 || body != invalid {
			t.Fatalf("%q: %d %s", name, st, body)
		}
	}
	if st, body := post(`{"agent_id":"","short_name":"ok1"}`); st != 400 || body != `{"error":"group_leader_required","message":"请选择组长"}` {
		t.Fatalf("no leader: %d %s", st, body)
	}
	if st, body := post(`{not json`); st != 400 || body != `{"error":"bad_request","message":"请求格式不正确，请刷新页面后重试"}` {
		t.Fatalf("bad json: %d %s", st, body)
	}
}

// 提示逐字一致：Hub 的 groupNameInvalidText 与页面 GROUP_NAME_RULE_TEXT 是同一句；页面里这句只定义一次，
// CONSOLE_ERRORS、groupChoice、添加编组预检三处都引用该常量（不各写一份，防漂移）。
func TestGroupNameRuleTextIsOneSentenceEverywhere(t *testing.T) {
	page := consolePageHTML
	if !strings.Contains(page, "var GROUP_NAME_RULE_TEXT='"+groupNameInvalidText+"';") {
		t.Fatal("页面常量与 Hub 文案不一致")
	}
	if n := strings.Count(page, groupNameInvalidText); n != 1 {
		t.Fatalf("这句在页面里应只出现 1 次（常量定义处），实得 %d", n)
	}
	for _, use := range []string{
		"group_name_invalid:GROUP_NAME_RULE_TEXT,",
		"if(!NEW_GROUP_RE.test(n))return {error:GROUP_NAME_RULE_TEXT};",
		"if(!NEW_GROUP_RE.test(name)){groupFeedback('groupNameFeedback',GROUP_NAME_RULE_TEXT);return;}",
	} {
		if strings.Count(page, use) != 1 {
			t.Fatalf("缺引用：%s", use)
		}
	}
}
