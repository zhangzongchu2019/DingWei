package hosted

// 账号登录 Hub 侧（R-V2-ACCOUNT-LOGIN-FLOW，设计件第十二、十三节）：会话状态机、每人 1 个、冷却 30 秒、
// 每节点 2／全平台 4、上传绑会话节点、一次性、到期清扫与启动收尾、入池（新登记／重新登录）、凭证不落会话表与审计。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	ldURL  = "https://auth.example.test/codex/device"
	ldCode = "ABCD-12345"
)

type ldGw struct {
	mu      sync.Mutex
	sent    []NodeCommand
	nodes   []string
	errText string
	down    bool
	payload string
}

func (g *ldGw) Send(ctx context.Context, nodeID string, c NodeCommand) (NodeResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sent = append(g.sent, c)
	g.nodes = append(g.nodes, nodeID)
	if c.Type == "login_start" {
		if g.down {
			return NodeResult{}, errors.New("injected: node unreachable")
		}
		if g.errText != "" {
			return NodeResult{OK: false, Error: g.errText}, nil
		}
		p := g.payload
		if p == "" {
			p = `{"url":"` + ldURL + `","user_code":"` + ldCode + `"}`
		}
		return NodeResult{OK: true, Payload: json.RawMessage(p)}, nil
	}
	return NodeResult{OK: true}, nil
}

func (g *ldGw) types() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, c := range g.sent {
		out = append(out, c.Type)
	}
	return out
}

type ldRig struct {
	l       *LoginService
	gw      *ldGw
	s       *Store
	svc     *PoolService
	secrets map[string]string
	clock   time.Time
}

func (r *ldRig) advance(d time.Duration) { r.clock = r.clock.Add(d) }

func mkLogin(t *testing.T) *ldRig {
	t.Helper()
	svc, s := mkPoolSvc(t)
	ctx := context.Background()
	r := &ldRig{gw: &ldGw{}, s: s, svc: svc, secrets: map[string]string{}, clock: time.Now()}
	for _, id := range []string{"nd_a", "nd_b"} {
		sec, err := s.RegisterNode(ctx, Node{ID: id, Name: id, Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin")
		if err != nil {
			t.Fatal(err)
		}
		r.secrets[id] = sec
		if err := s.SetNodeOnline(ctx, id, json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	combos := fakeCombos{m: map[string]bool{"codex|gpt|token_file": true}}
	r.l = NewLoginService(s, svc, r.gw, fakeCatalog{}, func(cli, llm, kind string) bool { _, ok := combos.Lookup(cli, llm, kind); return ok }, "https://hub.test")
	r.l.now = func() time.Time { return r.clock }
	return r
}

// authJSON 造一份带 id_token（只含 email 声明）的 auth.json；refresh 令牌带标记串供泄漏扫描。
func authJSON(email string) string {
	claims, _ := json.Marshal(map[string]string{"email": email})
	idt := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	b, _ := json.Marshal(map[string]any{"tokens": map[string]string{"id_token": idt, "refresh_token": "RT-LEAK-MARKER-9"}})
	return string(b)
}

func uploadOK(cred string) []byte {
	b, _ := json.Marshal(map[string]string{"status": "ok", "credential": cred})
	return b
}

func (r *ldRig) row(t *testing.T, id string) loginRow {
	t.Helper()
	row, err := r.s.getLogin(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// H1：发起 ⇒ 链接与码只给发起人；节点参数里是本会话上传端点、无邮箱；id 128 位十六进制；审计无邮箱、链接、码。
func TestLoginStartHappy(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if v.State != "waiting_user" || v.URL != ldURL || v.UserCode != ldCode || v.Email != "me@example.com" {
		t.Fatalf("view: %+v", v)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(v.ID) {
		t.Fatalf("login id 须 32 位十六进制: %q", v.ID)
	}
	var p map[string]string
	json.Unmarshal(r.gw.sent[0].Params, &p)
	if p["upload_url"] != "https://hub.test/api/node/login/"+v.ID || p["login_id"] != v.ID || len(p) != 3 {
		t.Fatalf("节点参数: %v", p)
	}
	if strings.Contains(string(r.gw.sent[0].Params), "example.com") {
		t.Fatal("节点参数不得含邮箱")
	}
	var detail string
	row, _ := r.s.qrow(ctx, `SELECT coalesce(string_agg(detail::text, ' '),'') FROM g4_audit WHERE target=$1`, v.ID)
	row.Scan(&detail)
	for _, s := range []string{"example.com", ldURL, ldCode} {
		if strings.Contains(detail, s) {
			t.Fatalf("审计不得含 %q: %s", s, detail)
		}
	}
	// 他人查／取消 ⇒ ErrNotFound（同不存在）
	if _, err := r.l.Get(ctx, "bob", v.ID); err != ErrNotFound {
		t.Fatalf("他人查应 ErrNotFound, 得 %v", err)
	}
	if _, err := r.l.Cancel(ctx, "bob", v.ID); err != ErrNotFound {
		t.Fatalf("他人取消应 ErrNotFound, 得 %v", err)
	}
	if row := r.row(t, v.ID); row.State != "waiting_user" || row.URL != ldURL {
		t.Fatalf("他人取消不得改动本人会话: %+v", row)
	}
}

// H2b：本人会话已到期但无人上传、清扫未轮到 ⇒ 本人再发起照常成功（不被部分唯一索引误判「进行中」），旧会话收尾为 expired。
func TestLoginStartExpiresOwnStaleSession(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	r.advance(LoginTTL + time.Second)
	v2, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	if err != nil {
		t.Fatalf("旧会话已到期，应可再发起: %v", err)
	}
	if row := r.row(t, v.ID); row.State != "expired" || row.URL != "" {
		t.Fatalf("旧会话应收尾为 expired: %+v", row)
	}
	if v2.ID == v.ID || v2.State != "waiting_user" {
		t.Fatalf("新会话: %+v", v2)
	}
}

// H2／H3：每人 1 个进行中（库层唯一）；冷却 30 秒；取消后清链接与码并通知节点。
func TestLoginOnePerUserAndCooldown(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	r.advance(31 * time.Second)
	if _, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", ""); err != ErrLoginInProgress {
		t.Fatalf("进行中再发起应 ErrLoginInProgress, 得 %v", err)
	}
	c, err := r.l.Cancel(ctx, "zzc", v.ID)
	if err != nil || c.State != "cancelled" || c.URL != "" || c.UserCode != "" {
		t.Fatalf("取消后应 cancelled 且清链接与码: %+v %v", c, err)
	}
	if again, err := r.l.Cancel(ctx, "zzc", v.ID); err != nil || again.State != "cancelled" {
		t.Fatalf("重复取消应幂等: %+v %v", again, err)
	}
	waitTypes(t, r.gw, "login_cancel")
	if _, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", ""); err != nil {
		t.Fatalf("冷却已过（31s 前发起）应可再发起: %v", err)
	}
	r.advance(10 * time.Second)
	if _, err := r.l.Start(ctx, "bob", "gpt", "bob@example.com", ""); err != nil {
		t.Fatalf("他人不受本人冷却影响: %v", err)
	}
	// 本人刚发起 10 秒 ⇒ 取消后立即再发起 ⇒ 冷却
	vs, _ := r.s.activeLogins(ctx)
	for _, x := range vs {
		if x.OwnerLC == "zzc" {
			r.l.Cancel(ctx, "zzc", x.ID)
		}
	}
	if _, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", ""); err != ErrLoginRateLimited {
		t.Fatalf("冷却内再发起应 ErrLoginRateLimited, 得 %v", err)
	}
}

func waitTypes(t *testing.T, g *ldGw, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, ty := range g.types() {
			if ty == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("未发 %s: %v", want, g.types())
}

// H4：每节点 2、全平台 4（两节点）——第 5 个 ⇒ ErrLoginBusy；分布在两节点各 2。
func TestLoginConcurrencyCaps(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	for i, u := range []string{"u1", "u2", "u3", "u4"} {
		if _, err := r.l.Start(ctx, u, "gpt", u+"@example.com", ""); err != nil {
			t.Fatalf("第 %d 个: %v", i+1, err)
		}
	}
	if _, err := r.l.Start(ctx, "u5", "gpt", "u5@example.com", ""); err != ErrLoginBusy {
		t.Fatalf("第 5 个应 ErrLoginBusy, 得 %v", err)
	}
	per, total, _ := r.s.activeLoginsByNode(ctx)
	if total != 4 || per["nd_a"] != 2 || per["nd_b"] != 2 {
		t.Fatalf("分布: %v total=%d", per, total)
	}
}

// H5：邮箱只做格式与长度校验。
func TestLoginEmailValidation(t *testing.T) {
	r := mkLogin(t)
	for _, e := range []string{"", "noat", "a@b", "a@@b.com", "a b@c.com", "a@b.com\x00", "a\tb@c.com", strings.Repeat("a", 250) + "@b.com", "@b.com"} {
		if _, err := r.l.Start(context.Background(), "zzc", "gpt", e, ""); err != ErrEmailInvalid {
			t.Fatalf("%q 应 ErrEmailInvalid, 得 %v", e, err)
		}
	}
	if _, err := r.l.Start(context.Background(), "zzc", "nope", "me@example.com", ""); err != ErrComboIncompatible {
		t.Fatalf("组合不在表应 ErrComboIncompatible, 得 %v", err)
	}
}

// H6／H7：上传 ok ⇒ 新登记（名称＝邮箱、记 email、token_file）、会话 done、清链接与码；凭证不进会话表与审计；
// id_token 邮箱不符 ⇒ 仍入池、reason=email_mismatch。
func TestLoginCompleteRegisters(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, _ := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	cred := authJSON("ME@example.com")
	if err := r.l.Complete(ctx, v.ID, uploadOK(cred)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, _ := r.l.Get(ctx, "zzc", v.ID)
	if got.State != "done" || got.AccountID == "" || got.URL != "" || got.UserCode != "" || got.Reason != "" {
		t.Fatalf("done 视图: %+v", got)
	}
	a, err := r.s.GetOwnedAccount(ctx, "zzc", got.AccountID)
	if err != nil || a.Name != "me@example.com" || a.Email != "me@example.com" || a.Kind != "token_file" || a.CLI != "codex" || a.LLM != "gpt" {
		t.Fatalf("入池账号: %+v %v", a, err)
	}
	mkSeatRow(t, r.s, "ag_l1", "zzc", a.ID, "online")
	if plain, err := r.svc.DecryptForSeat(ctx, "system", a.ID, "ag_l1"); err != nil || plain != cred {
		t.Fatalf("池内副本应为上传的凭证: %v", err)
	}
	var leak int
	row, _ := r.s.qrow(ctx, `SELECT (SELECT count(*) FROM g4_account_login WHERE row_to_json(g4_account_login)::text LIKE '%RT-LEAK-MARKER%')
	                              + (SELECT count(*) FROM g4_audit WHERE detail::text LIKE '%RT-LEAK-MARKER%')`)
	row.Scan(&leak)
	if leak != 0 {
		t.Fatal("凭证不得进会话表或审计")
	}

	r.advance(31 * time.Second)
	v2, _ := r.l.Start(ctx, "zzc", "gpt", "other@example.com", "")
	if err := r.l.Complete(ctx, v2.ID, uploadOK(authJSON("someone@else.com"))); err != nil {
		t.Fatal(err)
	}
	if g2, _ := r.l.Get(ctx, "zzc", v2.ID); g2.State != "done" || g2.Reason != "email_mismatch" {
		t.Fatalf("邮箱不符应 done＋email_mismatch: %+v", g2)
	}
}

// H8：一次性——同一会话再传 ⇒ ErrLoginNotActive、不重复入池；过期后到的上传 ⇒ 拒、不入池；
// H9：失败上传 ⇒ failed，原因码按白名单格式（否则一律 login_failed）；坏 JSON 凭证 ⇒ failed。
func TestLoginCompleteOneShotExpiryAndFailures(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, _ := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	r.l.Complete(ctx, v.ID, uploadOK(authJSON("me@example.com")))
	if err := r.l.Complete(ctx, v.ID, uploadOK(authJSON("me@example.com"))); err != ErrLoginNotActive {
		t.Fatalf("重复上传应 ErrLoginNotActive, 得 %v", err)
	}
	if n, _ := r.s.CountOwnedAccounts(ctx, "zzc"); n != 1 {
		t.Fatalf("不得重复入池: %d", n)
	}

	r.advance(31 * time.Second)
	v2, _ := r.l.Start(ctx, "zzc", "gpt", "late@example.com", "")
	r.advance(LoginTTL + time.Second)
	if err := r.l.Complete(ctx, v2.ID, uploadOK(authJSON("late@example.com"))); err != ErrLoginNotActive {
		t.Fatalf("过期后上传应拒, 得 %v", err)
	}
	if n, _ := r.s.CountOwnedAccounts(ctx, "zzc"); n != 1 {
		t.Fatal("过期上传不得入池")
	}

	cases := map[string]string{
		`{"status":"failed","reason":"login_exit_1"}`:         "login_exit_1",
		`{"status":"failed","reason":"Bad Reason <script>"}`:  "login_failed",
		`{"status":"ok","credential":"NOT-JSON-CRED-MARKER"}`: "credential_not_json",
	}
	for body, want := range cases {
		r.advance(31 * time.Second)
		vv, err := r.l.Start(ctx, "zzc", "gpt", "f@example.com", "")
		if err != nil {
			t.Fatal(err)
		}
		_ = r.l.Complete(ctx, vv.ID, []byte(body))
		if g, _ := r.l.Get(ctx, "zzc", vv.ID); g.State != "failed" || g.Reason != want {
			t.Fatalf("%s ⇒ %+v, 期望 failed/%s", body, g, want)
		}
	}
}

// H10：重新登录——只换本人账号凭据；失败不动旧凭据；他人账号 ⇒ ErrNotFound；api_key 账号 ⇒ 组合不符。
func TestLoginRelogin(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	old := `{"tokens":{"refresh_token":"OLD"}}`
	a, err := r.svc.RegisterOwned(ctx, "zzc", "me@example.com", "token_file", "codex", "gpt", old)
	if err != nil {
		t.Fatal(err)
	}
	mkSeatRow(t, r.s, "ag_rl", "zzc", a.ID, "online")
	if _, err := r.l.Start(ctx, "bob", "", "", a.ID); err != ErrNotFound {
		t.Fatalf("他人账号重新登录应 ErrNotFound, 得 %v", err)
	}
	k, _ := r.svc.RegisterOwned(ctx, "zzc", "key1", "api_key", "codex", "gpt", "sk-12345678")
	if _, err := r.l.Start(ctx, "zzc", "", "me@example.com", k.ID); err != ErrComboIncompatible {
		t.Fatalf("api_key 账号不可账号登录, 得 %v", err)
	}
	v, err := r.l.Start(ctx, "zzc", "", "me@example.com", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.l.Complete(ctx, v.ID, []byte(`{"status":"failed","reason":"login_timeout"}`))
	if plain, _ := r.svc.DecryptForSeat(ctx, "system", a.ID, "ag_rl"); plain != old {
		t.Fatal("重新登录失败不得动旧凭据")
	}
	r.advance(31 * time.Second)
	v2, _ := r.l.Start(ctx, "zzc", "", "me@example.com", a.ID)
	fresh := authJSON("me@example.com")
	if err := r.l.Complete(ctx, v2.ID, uploadOK(fresh)); err != nil {
		t.Fatal(err)
	}
	if plain, _ := r.svc.DecryptForSeat(ctx, "system", a.ID, "ag_rl"); plain != fresh {
		t.Fatal("重新登录成功应换为新凭据")
	}
	if n, _ := r.s.CountOwnedAccounts(ctx, "zzc"); n != 2 {
		t.Fatalf("重新登录不得新增账号: %d", n)
	}
	if g, _ := r.l.Get(ctx, "zzc", v2.ID); g.AccountID != a.ID {
		t.Fatalf("结果账号应为原账号: %+v", g)
	}
}

// H11：节点回报——旧节点 ⇒ node_unsupported；不通 ⇒ node_offline；载荷不合（无码）⇒ login_start_failed 并通知节点撤。
func TestLoginStartNodeOutcomes(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, errText, payload string
		down                   bool
		want                   error
		reason                 string
	}{
		{"old_node", "unknown_command_type", "", false, ErrNodeUnsupported, "node_unsupported"},
		{"offline", "", "", true, ErrNodeOffline, "node_offline"},
		{"busy", "login_busy", "", false, ErrLoginBusy, "login_busy"},
		{"bad_payload", "", `{"url":"http://plain.test","user_code":"x"}`, false, ErrLoginStartFailed, "login_start_failed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := mkLogin(t)
			r.gw.errText, r.gw.down, r.gw.payload = c.errText, c.down, c.payload
			if _, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", ""); err != c.want {
				t.Fatalf("应 %v, 得 %v", c.want, err)
			}
			rows, _ := r.s.query(ctx, `SELECT state, reason, url, user_code FROM g4_account_login`)
			defer rows.Close()
			rows.Next()
			var st, rs, u, uc string
			rows.Scan(&st, &rs, &u, &uc)
			if st != "failed" || rs != c.reason || u != "" || uc != "" {
				t.Fatalf("会话应 failed/%s 且无链接码: %s/%s %q %q", c.reason, st, rs, u, uc)
			}
			waitTypes(t, r.gw, "login_cancel") // 命令发出后的任何失败（含网关超时）都通知节点撤（复核必改 2）
		})
	}
}

// H12：到期清扫与启动收尾——进行中到期 ⇒ expired、清链接与码、通知节点撤；Hub 重启 ⇒ 进行中一律 expired。
func TestLoginSweepAndRecover(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, _ := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	r.advance(LoginTTL - time.Second)
	r.l.Sweep(ctx)
	if g, _ := r.l.Get(ctx, "zzc", v.ID); g.State != "waiting_user" {
		t.Fatalf("未到期不得清: %+v", g)
	}
	r.advance(2 * time.Second)
	if err := r.l.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	row := r.row(t, v.ID)
	if row.State != "expired" || row.URL != "" || row.UserCode != "" {
		t.Fatalf("到期应 expired 并清空: %+v", row)
	}
	waitTypes(t, r.gw, "login_cancel")

	v2, _ := r.l.Start(ctx, "bob", "gpt", "bob@example.com", "")
	if err := r.l.RecoverOnStart(ctx); err != nil {
		t.Fatal(err)
	}
	if row := r.row(t, v2.ID); row.State != "expired" {
		t.Fatalf("启动收尾应 expired: %+v", row)
	}
}

// H13：上传鉴权绑会话节点——会话节点的密钥过；他节点的密钥、空密钥、错密钥一律拒；会话不存在 ⇒ ErrNotFound。
func TestLoginUploadBoundToSessionNode(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, _ := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	node := r.row(t, v.ID).NodeID
	other := "nd_a"
	if node == "nd_a" {
		other = "nd_b"
	}
	if err := r.l.AuthorizeUpload(ctx, v.ID, r.secrets[node]); err != nil {
		t.Fatalf("会话节点密钥应过: %v", err)
	}
	for name, sec := range map[string]string{"other_node": r.secrets[other], "empty": "", "wrong": "x"} {
		if err := r.l.AuthorizeUpload(ctx, v.ID, sec); err == nil {
			t.Fatalf("%s 应拒", name)
		}
	}
	if err := r.l.AuthorizeUpload(ctx, "0123456789abcdef0123456789abcdef", r.secrets[node]); err != ErrNotFound {
		t.Fatalf("不存在会话应 ErrNotFound, 得 %v", err)
	}
}

// H14：账号数上限在发起时先挡（新登记）；重新登录不受此挡。
func TestLoginAccountLimitPrecheck(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	for i := 0; i < MaxAccountsPerOwner; i++ {
		if _, err := r.svc.RegisterOwned(ctx, "zzc", "k"+string(rune('a'+i)), "api_key", "codex", "gpt", "sk-12345678"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", ""); err != ErrAccountLimit {
		t.Fatalf("已满应 ErrAccountLimit, 得 %v", err)
	}
}

// idTokenEmail：只解码取 email；形状不对一律空串。
func TestIDTokenEmail(t *testing.T) {
	if got := idTokenEmail(authJSON("x@y.com")); got != "x@y.com" {
		t.Fatalf("%q", got)
	}
	for _, bad := range []string{"", "{}", `{"tokens":{"id_token":"a.b"}}`, `{"tokens":{"id_token":"a.!!!.c"}}`, "not json"} {
		if got := idTokenEmail(bad); got != "" {
			t.Fatalf("%q ⇒ %q", bad, got)
		}
	}
}

// H4b：全平台上限单独咬——三个在线节点（每节点上限合计 6）时，第 5 个仍 ErrLoginBusy。
func TestLoginTotalCapBindsWithThreeNodes(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	if _, err := r.s.RegisterNode(ctx, Node{ID: "nd_c", Name: "nd_c", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := r.s.SetNodeOnline(ctx, "nd_c", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"u1", "u2", "u3", "u4"} {
		if _, err := r.l.Start(ctx, u, "gpt", u+"@example.com", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.l.Start(ctx, "u5", "gpt", "u5@example.com", ""); err != ErrLoginBusy {
		t.Fatalf("三节点时第 5 个应被全平台上限挡住, 得 %v", err)
	}
}

// H4c：每节点上限单独咬——只有一个在线节点时，第 3 个 ErrLoginBusy。
func TestLoginPerNodeCapBindsWithOneNode(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	if _, err := r.s.exec(ctx, `UPDATE g4_node SET state='offline' WHERE id='nd_b'`); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"u1", "u2"} {
		if _, err := r.l.Start(ctx, u, "gpt", u+"@example.com", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.l.Start(ctx, "u3", "gpt", "u3@example.com", ""); err != ErrLoginBusy {
		t.Fatalf("单节点时第 3 个应被每节点上限挡住, 得 %v", err)
	}
	if _, total, _ := r.s.activeLoginsByNode(ctx); total != 2 {
		t.Fatalf("应只有 2 个进行中: %d", total)
	}
}

// 复核必改 3：入池会被拒的条件在发起时即拒（用户不必先去浏览器登完）——邮箱超过账号名长度、本人已有同名账号；
// 邮箱随同一条 INSERT 落库（入池即带 email，不存在「名字＝邮箱、email 为空」的中间态）。
func TestLoginPrechecksAndEmailInSameInsert(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	long := strings.Repeat("a", AccountNameMaxLen-len("@example.com")+1) + "@example.com"
	if _, err := r.l.Start(ctx, "zzc", "gpt", long, ""); err != ErrEmailInvalid {
		t.Fatalf("超过账号名长度的邮箱应在发起时拒, 得 %v", err)
	}
	if _, err := r.svc.RegisterOwned(ctx, "zzc", "dup@example.com", "token_file", "codex", "gpt", `{"t":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.l.Start(ctx, "zzc", "gpt", "dup@example.com", ""); err != ErrAccountNameTaken {
		t.Fatalf("同名应在发起时拒, 得 %v", err)
	}
	if len(r.gw.types()) != 0 {
		t.Fatalf("预查不过不得起节点容器: %v", r.gw.types())
	}
	a, err := r.svc.RegisterOwnedLogin(ctx, "zzc", "one@example.com", "gpt", authJSON("one@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	var email string
	row, _ := r.s.qrow(ctx, `SELECT email FROM g4_account_pool WHERE id=$1`, a.ID)
	row.Scan(&email)
	if email != "one@example.com" || a.Name != "one@example.com" || a.Kind != "token_file" || a.CLI != "codex" {
		t.Fatalf("登录入池须一条 INSERT 带 email: %q %+v", email, a)
	}
}

// 复核建议 7：上传已收成 done 的会话，之后到期清扫与取消都不得改动它。
func TestLoginDoneNotOverwrittenBySweepOrCancel(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, _ := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	if err := r.l.Complete(ctx, v.ID, uploadOK(authJSON("me@example.com"))); err != nil {
		t.Fatal(err)
	}
	r.advance(LoginTTL + time.Minute)
	r.l.Sweep(ctx)
	r.l.Cancel(ctx, "zzc", v.ID)
	if row := r.row(t, v.ID); row.State != "done" || row.AccountID == "" {
		t.Fatalf("done 不得被清扫或取消改写: %+v", row)
	}
}

// 组长 LOGIN-IMPL-R1 建议 7：上传先原子认领（waiting_user→completing）再入池——
// completing 期间清扫、取消都不碰它；同会话再上传认领不到 ⇒ 拒、不重复入池；过期会话认领不到；Hub 重启把 completing 收为 failed/interrupted。
func TestLoginClaimCompletingIsAtomic(t *testing.T) {
	r := mkLogin(t)
	ctx := context.Background()
	v, _ := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
	if ok, err := r.s.claimLoginCompleting(ctx, v.ID, r.clock); err != nil || !ok {
		t.Fatalf("认领: %v %v", ok, err)
	}
	if ok, _ := r.s.claimLoginCompleting(ctx, v.ID, r.clock); ok {
		t.Fatal("同一会话不得被认领两次")
	}
	r.advance(LoginTTL + time.Minute)
	r.l.Sweep(ctx)
	r.l.Cancel(ctx, "zzc", v.ID)
	if row := r.row(t, v.ID); row.State != "completing" || row.URL != "" || row.UserCode != "" {
		t.Fatalf("completing 不得被清扫或取消改写，且已清链接与码: %+v", row)
	}
	if err := r.l.Complete(ctx, v.ID, uploadOK(authJSON("me@example.com"))); err != ErrLoginNotActive {
		t.Fatalf("completing 时再上传应拒, 得 %v", err)
	}
	if n, _ := r.s.CountOwnedAccounts(ctx, "zzc"); n != 0 {
		t.Fatalf("认领不到不得入池: %d", n)
	}
	if _, err := r.l.Start(ctx, "zzc", "gpt", "x@example.com", ""); err != ErrLoginInProgress {
		t.Fatalf("completing 仍算进行中（每人 1 个）, 得 %v", err)
	}
	if err := r.l.RecoverOnStart(ctx); err != nil {
		t.Fatal(err)
	}
	if row := r.row(t, v.ID); row.State != "failed" || row.Reason != "interrupted" {
		t.Fatalf("重启应把 completing 收为 failed/interrupted: %+v", row)
	}

	// 过期会话认领不到
	r.advance(31 * time.Second)
	v2, err := r.l.Start(ctx, "zzc", "gpt", "late@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.s.claimLoginCompleting(ctx, v2.ID, r.clock.Add(LoginTTL+time.Second)); ok {
		t.Fatal("过期会话不得被认领")
	}
}

// 组长 LOGIN-IMPL-R2 第 3 条（复核 M3）：会话一旦结束（done／failed／expired／cancelled），库里链接与码一律清空。
func TestLoginTerminalStatesClearLinkAndCode(t *testing.T) {
	ctx := context.Background()
	end := map[string]func(r *ldRig, id string){
		"done": func(r *ldRig, id string) { r.l.Complete(ctx, id, uploadOK(authJSON("me@example.com"))) },
		"failed": func(r *ldRig, id string) {
			r.l.Complete(ctx, id, []byte(`{"status":"failed","reason":"login_exit_1"}`))
		},
		"expired":   func(r *ldRig, id string) { r.advance(LoginTTL + time.Second); r.l.Sweep(ctx) },
		"cancelled": func(r *ldRig, id string) { r.l.Cancel(ctx, "zzc", id) },
	}
	for want, do := range end {
		t.Run(want, func(t *testing.T) {
			r := mkLogin(t)
			v, err := r.l.Start(ctx, "zzc", "gpt", "me@example.com", "")
			if err != nil {
				t.Fatal(err)
			}
			if row := r.row(t, v.ID); row.URL != ldURL || row.UserCode != ldCode {
				t.Fatalf("进行中应有链接与码: %+v", row)
			}
			do(r, v.ID)
			row := r.row(t, v.ID)
			if row.State != want || row.URL != "" || row.UserCode != "" {
				t.Fatalf("结束为 %s 后链接与码须清空: %+v", want, row)
			}
			var leak int
			q, _ := r.s.qrow(ctx, `SELECT count(*) FROM g4_account_login WHERE id=$1 AND (url LIKE '%auth.example%' OR user_code=$2)`, v.ID, ldCode)
			q.Scan(&leak)
			if leak != 0 {
				t.Fatal("库里仍留链接或码")
			}
		})
	}
}
