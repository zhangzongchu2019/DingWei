package m8

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// 新平台编组接口：路径与字段与旧平台同形；成员与组长的 session_name 放 full_name，另给 full_name；
// 错误文案与状态码与旧平台同；认人与 Origin 在 RequireConsolePrincipal。
func TestNewPlatformAgentGroupsHTTP(t *testing.T) {
	v := newViewEnv(t)
	agentID := func(owner, nameLC string) string {
		ag, err := v.svc.Store.FindAgentByName(v.ctx, owner, nameLC)
		if err != nil {
			t.Fatalf("agent %s of %s: %v", nameLC, owner, err)
		}
		return ag.ID
	}
	zID, lID := agentID("zzc", "kimi"), agentID("lbl", "kimi")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/console/agent-groups", v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroups))
	mux.HandleFunc("POST /api/console/agent-groups", v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroups))
	mux.HandleFunc("DELETE /api/console/agent-groups/{code}", v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroup))
	mux.HandleFunc("POST /api/console/agent-groups/{code}/members", v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroup))
	mux.HandleFunc("DELETE /api/console/agent-groups/{code}/members/{agentID}", v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroup))
	mux.HandleFunc("POST /api/console/agent-groups/{code}/additions", v.hub.RequireConsolePrincipal(v.hub.HandleConsoleAgentGroup))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	call := func(method, path, tok, body string, origin bool) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		req.Header.Set("Content-Type", "application/json")
		if origin {
			req.Header.Set("Origin", npOrigin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(b))
	}
	type member struct {
		AgentID     string `json:"agent_id"`
		KeyID       string `json:"key_id"`
		SessionName string `json:"session_name"`
		FullName    string `json:"full_name"`
		Online      bool   `json:"online"`
	}
	type group struct {
		Code              string   `json:"code"`
		ShortName         string   `json:"short_name"`
		LeaderKeyID       string   `json:"leader_key_id"`
		LeaderSessionName string   `json:"leader_session_name"`
		AdditionsClosed   bool     `json:"additions_closed"`
		CanManage         bool     `json:"can_manage"`
		OwnerDisplayName  string   `json:"owner_display_name"`
		Members           []member `json:"members"`
	}
	type list struct {
		Principal string  `json:"principal"`
		Groups    []group `json:"groups"`
		Owned     []struct {
			ID          string `json:"id"`
			SessionName string `json:"session_name"`
			FullName    string `json:"full_name"`
			Online      bool   `json:"online"`
		} `json:"owned_agents"`
	}
	get := func(tok string) list {
		t.Helper()
		st, body := call("GET", "/api/console/agent-groups", tok, "", false)
		var l list
		if st != 200 || json.Unmarshal([]byte(body), &l) != nil {
			t.Fatalf("GET as %s: %d %s", tok, st, body)
		}
		kept := l.Groups[:0]
		for _, g := range l.Groups {
			if !fixtureGroupName.MatchString(g.ShortName) { // 夹具批准时给每个 Agent 建的 t<n> 组
				kept = append(kept, g)
			}
		}
		l.Groups = kept
		return l
	}

	l := get(v.tokZZC)
	if l.Principal != "zzc" || len(l.Groups) != 0 || len(l.Owned) != 2 {
		t.Fatalf("empty list: %+v", l)
	}
	ownedFull := map[string]string{}
	for _, o := range l.Owned {
		if o.SessionName != o.FullName || o.Online {
			t.Fatalf("owned agent view: %+v", o)
		}
		ownedFull[o.ID] = o.FullName
	}
	if ownedFull[zID] != "Kimi.zzc" {
		t.Fatalf("owned agents: %+v", l.Owned)
	}
	if st, body := call("POST", "/api/console/agent-groups", v.tokZZC, `{"agent_id":"`+zID+`","short_name":"开发组"}`, false); st != 403 || body != `{"error":"origin_mismatch"}` {
		t.Fatalf("create without Origin: %d %s", st, body)
	}
	if st, body := call("POST", "/api/console/agent-groups", v.tokZZC, `{"agent_id":"`+zID+`","short_name":"九个字九个字九个字"}`, true); st != 400 || body != `{"error":"group_name_invalid","message":"`+groupNameInvalidText+`"}` {
		t.Fatalf("bad name: %d %s", st, body)
	}
	if st, body := call("POST", "/api/console/agent-groups", v.tokZZC, `{"agent_id":"`+lID+`","short_name":"开发组"}`, true); st != 404 || body != "owned agent not found" {
		t.Fatalf("someone else's agent as leader: %d %s", st, body)
	}
	st, body := call("POST", "/api/console/agent-groups", v.tokZZC, `{"agent_id":"`+zID+`","short_name":" 开发组 "}`, true)
	var created group
	if st != 201 || json.Unmarshal([]byte(body), &created) != nil || created.ShortName != "开发组" || len(created.Code) != 32 ||
		created.LeaderKeyID != "u_zzc" || created.LeaderSessionName != "Kimi.zzc" || !created.CanManage ||
		len(created.Members) != 1 || created.Members[0].FullName != "Kimi.zzc" || created.Members[0].SessionName != "Kimi.zzc" || created.Members[0].KeyID != "u_zzc" {
		t.Fatalf("create: %d %s", st, body)
	}
	code := created.Code
	if st, body := call("POST", "/api/console/agent-groups", v.tokLBL, `{"agent_id":"`+lID+`","short_name":"开发组"}`, true); st != 409 || body != "团队名称不可用；建议名称：开发组1、开发组2" {
		t.Fatalf("dup name: %d %s", st, body)
	}
	if l := get(v.tokLBL); len(l.Groups) != 0 {
		t.Fatalf("lbl must not see zzc's group yet: %+v", l.Groups)
	}
	if st, body := call("POST", "/api/console/agent-groups/"+code+"/members", v.tokLBL, `{"agent_id":"`+lID+`"}`, true); st != 204 {
		t.Fatalf("lbl joins: %d %s", st, body)
	}
	if st, body := call("POST", "/api/console/agent-groups/"+code+"/members", v.tokLBL, `{"agent_id":"`+lID+`"}`, true); st != 409 || body != "该 Agent 已在该编组中" {
		t.Fatalf("join twice: %d %s", st, body)
	}
	if st, body := call("POST", "/api/console/agent-groups/"+code+"/members", v.tokZZC, `{"agent_id":"`+lID+`"}`, true); st != 409 || body != "该 Agent 不可用（不存在、未激活或不属于当前业主）" {
		t.Fatalf("join someone else's agent: %d %s", st, body)
	}
	// 在线：zzc 的 Kimi 连上
	c, dst, dbody := v.dial(v.zKimiCred, "")
	if c == nil {
		t.Fatalf("dial: %d %s", dst, dbody)
	}
	defer c.Close(websocket.StatusNormalClosure, "done")
	reader(c)
	waitSessionOnline(t, v.hub, "u_zzc", "kimi")
	l = get(v.tokLBL)
	if len(l.Groups) != 1 {
		t.Fatalf("lbl sees %d groups", len(l.Groups))
	}
	g := l.Groups[0]
	if g.CanManage || g.OwnerDisplayName != "zzc" || len(g.Members) != 2 ||
		g.Members[0].FullName != "Kimi.zzc" || !g.Members[0].Online ||
		g.Members[1].FullName != "kimi.lb" || g.Members[1].KeyID != "u_lbl" || g.Members[1].SessionName != "kimi.lb" || g.Members[1].Online {
		t.Fatalf("group as seen by lbl: %+v", g)
	}
	for _, c := range []struct {
		name, method, path, tok, body string
		status                        int
		want                          string
	}{
		{"member's owner deletes group", "DELETE", "/api/console/agent-groups/" + code, v.tokLBL, "", 403, "无权管理该编组"},
		{"member's owner closes additions", "POST", "/api/console/agent-groups/" + code + "/additions", v.tokLBL, `{"closed":true}`, 403, "无权管理该编组"},
		{"leader removed", "DELETE", "/api/console/agent-groups/" + code + "/members/" + zID, v.tokZZC, "", 409, "组长不可剔除"},
		{"missing group", "DELETE", "/api/console/agent-groups/nope", v.tokZZC, "", 404, "组码不存在"},
		{"closed required", "POST", "/api/console/agent-groups/" + code + "/additions", v.tokZZC, `{}`, 400, "closed required"},
		{"member leaves", "DELETE", "/api/console/agent-groups/" + code + "/members/" + lID, v.tokLBL, "", 204, ""},
		{"leave twice", "DELETE", "/api/console/agent-groups/" + code + "/members/" + lID, v.tokLBL, "", 409, "该 Agent 不在本编组中，可能列表已过期，请刷新"},
		{"creator closes", "POST", "/api/console/agent-groups/" + code + "/additions", v.tokZZC, `{"closed":true}`, 204, ""},
		{"join closed", "POST", "/api/console/agent-groups/" + code + "/members", v.tokLBL, `{"agent_id":"` + lID + `"}`, 409, "该编组已关闭添加"},
		{"creator deletes", "DELETE", "/api/console/agent-groups/" + code, v.tokZZC, "", 204, ""},
	} {
		if st, body := call(c.method, c.path, c.tok, c.body, true); st != c.status || body != c.want {
			t.Errorf("%s: %d %q, want %d %q", c.name, st, body, c.status, c.want)
		}
	}
	if l := get(v.tokZZC); len(l.Groups) != 0 {
		t.Fatalf("after delete: %+v", l.Groups)
	}
}

var tgSeq atomic.Int64

// tg 测试用的新组名：每次不同（组名全局唯一、1～8 个字母或数字）。批准与托管新建都必须带编组（GroupAllowed）。
func tg() string { return fmt.Sprintf("t%d", tgSeq.Add(1)) }

var fixtureGroupName = regexp.MustCompile(`^t[0-9]+$`)
