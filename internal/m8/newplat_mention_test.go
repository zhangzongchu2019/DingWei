package m8

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

// V11（组长 9f0cd6ab）：候选＝所在全部编组成员的并集，去掉自己与已吊销的，只列 Agent；
// insert：并集（含自己）内短名唯一 ⇒ 短名，否则全名；在线优先，再按短名。
// 两条反向（派工点名）：总给短名 ⇒ kimi.lb 那格红；去掉编组过滤 ⇒ Outsider 那格红。
func TestNewPlatformMentionCandidates(t *testing.T) {
	v := newViewEnv(t) // zzc：Opus50TS、Kimi；lbl：kimi
	ctx := v.ctx
	id := func(owner, nameLC string) string {
		ag, err := v.svc.Store.FindAgentByName(ctx, owner, nameLC)
		if err != nil {
			t.Fatalf("%s/%s: %v", owner, nameLC, err)
		}
		return ag.ID
	}
	_, betaID := v.enrollFor("zzc", "dev-b", "Beta")
	_, gammaID := v.enrollFor("lbl", "dev-g", "Gamma")
	_, deltaID := v.enrollFor("lbl", "dev-d", "Delta")
	_, outID := v.enrollFor("zzc", "dev-o", "Outsider")
	kimiZ, kimiL, opus := id("zzc", "kimi"), id("lbl", "kimi"), id("zzc", "opus50ts")
	g1, err := v.svc.CreateGroup(ctx, "zzc", kimiZ, "一组")
	if err != nil {
		t.Fatal(err)
	}
	g2, err := v.svc.CreateGroup(ctx, "zzc", opus, "二组")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.svc.CreateGroup(ctx, "zzc", outID, "外组"); err != nil { // 与 Kimi.zzc 不同组
		t.Fatal(err)
	}
	for _, j := range []struct{ owner, code, agent string }{
		{"lbl", g1.Code, kimiL}, {"zzc", g1.Code, betaID},
		{"zzc", g2.Code, kimiZ}, {"lbl", g2.Code, gammaID}, {"lbl", g2.Code, deltaID},
		{"zzc", g2.Code, betaID}, // Beta 同在两组：并集里只出现一次
	} {
		if err := v.svc.JoinGroup(ctx, j.owner, j.code, j.agent); err != nil {
			t.Fatalf("join %+v: %v", j, err)
		}
	}
	if _, err := v.svc.Revoke(ctx, "lbl", deltaID); err != nil { // 吊销的不列
		t.Fatal(err)
	}
	// Opus50TS 在线，握手报 tool=Codex ⇒ cli=codex、排最前
	c, st, body := v.dial(v.cred, "tool=Codex")
	if c == nil {
		t.Fatalf("dial: %d %s", st, body)
	}
	defer c.Close(websocket.StatusNormalClosure, "done")
	reader(c)
	waitSessionOnline(t, v.hub, "u_zzc", "opus50ts")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /view/{sessionName}/mention-candidates", v.hub.RequireConsoleAgent("view", v.hub.HandleViewMentionCandidates))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	get := func(path, tok string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if tok != "" {
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(b))
	}
	st, body = get("/view/KIMI.ZZC/mention-candidates", v.tokZZC)
	var resp struct {
		Candidates []struct {
			FullName string `json:"full_name"`
			Name     string `json:"name"`
			CLI      string `json:"cli"`
			Online   bool   `json:"online"`
			Insert   string `json:"insert"`
		} `json:"candidates"`
	}
	if st != 200 || json.Unmarshal([]byte(body), &resp) != nil {
		t.Fatalf("candidates: %d %s", st, body)
	}
	var got []string
	for _, c := range resp.Candidates {
		got = append(got, c.FullName+"|"+c.Name+"|"+c.CLI+"|"+map[bool]string{true: "on", false: "off"}[c.Online]+"|"+c.Insert)
	}
	want := []string{
		"Opus50TS.zzc|Opus50TS|codex|on|Opus50TS", // 在线优先
		"Beta.zzc|Beta||off|Beta",                 // 两组都有，只出现一次
		"Gamma.lb|Gamma||off|Gamma",
		"kimi.lb|kimi||off|kimi.lb", // 与自己（Kimi.zzc）同短名 ⇒ 插全名
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("candidates:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if strings.Contains(body, "Outsider") || strings.Contains(body, "Delta") || strings.Contains(body, "Kimi.zzc") {
		t.Fatalf("must not list non-group, revoked or self: %s", body)
	}
	// 认人同 view 页：非归属 404、无会话 401
	if st, body := get("/view/Kimi.zzc/mention-candidates", v.tokLBL); st != 404 || body != `{"error":"not_found"}` {
		t.Fatalf("other user: %d %s", st, body)
	}
	if st, _ := get("/view/Kimi.zzc/mention-candidates", ""); st != 401 {
		t.Fatalf("no session: %d", st)
	}
	// 自己那组里只有自己 ⇒ 空列表（不是 null）
	if st, body := get("/view/Outsider.zzc/mention-candidates", v.tokZZC); st != 200 || body != `{"candidates":[]}` {
		t.Fatalf("lonely agent: %d %s", st, body)
	}
	// 未入组的 Agent（业主令 15:34:17Z：编组不是必须的）⇒ 候选为空
	digest, _ := identity.PairingDigest("000102030405060708090a0b0c0d0e0f", "123456")
	app, err := v.svc.Apply(ctx, identity.ApplyRequest{OwnerUserID: "zzc", PairingSalt: "000102030405060708090a0b0c0d0e0f", PairingDigest: digest, DeviceFingerprint: "dev-ng", SourceIP: "127.0.0.9"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.svc.Approve(ctx, "zzc", app.ApplicationID, "123456", "NoGroup", ""); err != nil {
		t.Fatal(err)
	}
	if st, body := get("/view/NoGroup.zzc/mention-candidates", v.tokZZC); st != 200 || body != `{"candidates":[]}` {
		t.Fatalf("ungrouped agent: %d %s", st, body)
	}
}
