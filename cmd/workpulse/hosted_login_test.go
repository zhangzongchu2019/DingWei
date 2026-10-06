package main

// 账号登录 HTTP 面（R-V2-ACCOUNT-LOGIN-FLOW）：发起／查询／取消的鉴权与 Origin、他人 404、邮箱错不回显；
// 节点上传端点：只认会话节点的密钥、限长 413、一次性 409；入池后列表方式与邮箱只给属主，管理视图不给邮箱。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

type loginRig struct {
	*acctRig
	secret string
}

func mkLoginRig(t *testing.T) *loginRig {
	t.Helper()
	deps, st, _, gw, pool := mkHostedDeps(t)
	ctx := context.Background()
	sec, err := st.RegisterNode(ctx, hosted.Node{ID: "nd_rt", Name: "rt", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetNodeOnline(ctx, "nd_rt", json.RawMessage(`{"cpu_total":16,"mem_free_mb":16384,"disk_free_mb":102400}`)); err != nil {
		t.Fatal(err)
	}
	deps.login = hosted.NewLoginService(st, pool, gw, fakeImagesRT{}, deps.orch.AccountComboKnown, testRouteOrigin)
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	return &loginRig{acctRig: &acctRig{t: t, mux: mux, st: st, gw: gw, pool: pool}, secret: sec}
}

func (r *loginRig) upload(id, secret string, body []byte) int {
	q := httptest.NewRequest("POST", "/api/node/login/"+id, bytes.NewReader(body))
	if secret != "" {
		q.Header.Set("Authorization", "Bearer "+secret)
	}
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, q)
	return rec.Code
}

func (r *loginRig) start(actor, body string) (int, map[string]any) {
	code, raw := r.req(actor, "POST", "/api/console/llm-account-logins", body, "")
	var m map[string]any
	json.Unmarshal([]byte(raw), &m)
	return code, m
}

const credMark = "RT-HTTP-LEAK-MARKER"

func loginCred(email string) []byte {
	claims, _ := json.Marshal(map[string]string{"email": email})
	idt := "h." + base64.RawURLEncoding.EncodeToString(claims) + ".s"
	auth, _ := json.Marshal(map[string]any{"tokens": map[string]string{"id_token": idt, "refresh_token": credMark}})
	b, _ := json.Marshal(map[string]string{"status": "ok", "credential": string(auth)})
	return b
}

// 全流程：发起 201（链接＋码）→ 他人查 404 → 无 Origin 发起／取消 403 → 节点上传（他人密钥 401、正确 204、再传 409）
// → 属主列表见 method=account_login＋email、名称＝邮箱 → 管理列表不见邮箱、名称以「（账号登录）」代称；所有应答不含凭证。
func TestLoginHTTPFlow(t *testing.T) {
	r := mkLoginRig(t)
	if code, _ := r.req("zzc", "POST", "/api/console/llm-account-logins", `{"llm":"gpt","email":"me@example.com"}`, "-"); code != 403 {
		t.Fatalf("无 Origin 发起应 403, 得 %d", code)
	}
	code, v := r.start("zzc", `{"llm":"gpt","email":"me@example.com"}`)
	if code != 201 || v["state"] != "waiting_user" || v["url"] != "https://auth.example.test/device" || v["user_code"] != "WXYZ-98765" {
		t.Fatalf("发起: %d %v", code, v)
	}
	id, _ := v["id"].(string)
	if c, _ := r.req("bob", "GET", "/api/console/llm-account-logins/"+id, "", ""); c != 404 {
		t.Fatalf("他人查应 404, 得 %d", c)
	}
	if c, _ := r.req("bob", "POST", "/api/console/llm-account-logins/"+id+"/cancel", "", ""); c != 404 {
		t.Fatalf("他人取消应 404, 得 %d", c)
	}
	if c, _ := r.req("zzc", "POST", "/api/console/llm-account-logins/"+id+"/cancel", "", "-"); c != 403 {
		t.Fatalf("无 Origin 取消应 403, 得 %d", c)
	}
	if c := r.upload(id, "", loginCred("me@example.com")); c != 401 {
		t.Fatalf("无节点密钥应 401, 得 %d", c)
	}
	if c := r.upload(id, "wrong-secret", loginCred("me@example.com")); c != 401 {
		t.Fatalf("错密钥应 401, 得 %d", c)
	}
	if c := r.upload("0123456789abcdef0123456789abcdef", r.secret, loginCred("me@example.com")); c != 404 {
		t.Fatalf("不存在会话应 404, 得 %d", c)
	}
	if c := r.upload(id, r.secret, loginCred("me@example.com")); c != 204 {
		t.Fatalf("正确上传应 204, 得 %d", c)
	}
	if c := r.upload(id, r.secret, loginCred("me@example.com")); c != 409 {
		t.Fatalf("再传应 409（一次性）, 得 %d", c)
	}
	c, raw := r.req("zzc", "GET", "/api/console/llm-account-logins/"+id, "", "")
	if c != 200 || !strings.Contains(raw, `"state":"done"`) || strings.Contains(raw, "WXYZ-98765") {
		t.Fatalf("完成后查: %d %s", c, raw)
	}
	mine := r.list("zzc", "/api/console/llm-accounts")
	var got map[string]any
	for _, a := range mine {
		if a["name"] == "me@example.com" {
			got = a
		}
	}
	if got == nil || got["method"] != "account_login" || got["email"] != "me@example.com" {
		t.Fatalf("属主列表: %v", mine)
	}
	admin := r.list("admin", "/api/console/admin/llm-accounts")
	for _, a := range admin {
		if a["id"] == got["id"] && (a["name"] != "（账号登录）" || a["email"] != nil) {
			t.Fatalf("管理视图不得给邮箱: %v", a)
		}
	}
	_, adminRaw := r.req("admin", "GET", "/api/console/admin/llm-accounts", "", "")
	if strings.Contains(adminRaw, "me@example.com") {
		t.Fatal("管理视图出现了邮箱")
	}
	for _, p := range []string{"/api/console/llm-accounts", "/api/console/admin/llm-accounts", "/api/console/llm-account-logins/" + id} {
		_, body := r.req("zzc", "GET", p, "", "")
		if strings.Contains(body, credMark) {
			t.Fatalf("%s 应答含凭证", p)
		}
	}
}

// 邮箱错 400 email_invalid 且不回显输入；冷却 429；他节点密钥 401；超长上传 413 且会话 failed。
func TestLoginHTTPGuards(t *testing.T) {
	r := mkLoginRig(t)
	code, raw := r.req("zzc", "POST", "/api/console/llm-account-logins", `{"llm":"gpt","email":"bad email ECHO-MARK"}`, "")
	if code != 400 || !strings.Contains(raw, "email_invalid") || strings.Contains(raw, "ECHO-MARK") {
		t.Fatalf("邮箱错: %d %s", code, raw)
	}
	_, v := r.start("zzc", `{"llm":"gpt","email":"me@example.com"}`)
	id, _ := v["id"].(string)
	if c, raw := r.req("zzc", "POST", "/api/console/llm-account-logins/"+id+"/cancel", "", ""); c != 200 || !strings.Contains(raw, "cancelled") {
		t.Fatalf("取消: %d %s", c, raw)
	}
	if c, raw := r.req("zzc", "POST", "/api/console/llm-account-logins", `{"llm":"gpt","email":"me@example.com"}`, ""); c != 429 || !strings.Contains(raw, "login_rate_limited") {
		t.Fatalf("冷却内应 429, 得 %d %s", c, raw)
	}
	_, v2 := r.start("bob", `{"llm":"gpt","email":"bob@example.com"}`)
	id2, _ := v2["id"].(string)
	other, err := r.st.RegisterNode(context.Background(), hosted.Node{ID: "nd_other", Name: "other", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if c := r.upload(id2, other, loginCred("bob@example.com")); c != 401 {
		t.Fatalf("他节点密钥应 401, 得 %d", c)
	}
	big := bytes.Repeat([]byte("a"), hosted.LoginUploadMax+1)
	if c := r.upload(id2, r.secret, big); c != 413 {
		t.Fatalf("超长应 413, 得 %d", c)
	}
	if _, raw := r.req("bob", "GET", "/api/console/llm-account-logins/"+id2, "", ""); !strings.Contains(raw, `"state":"failed"`) || !strings.Contains(raw, "too_large") {
		t.Fatalf("超长后会话应 failed/too_large: %s", raw)
	}
}
