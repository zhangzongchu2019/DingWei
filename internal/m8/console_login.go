package m8

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 统一登录（T-113 第 6 格）。
//
// 🔴 设计裁定：**复用既有的飞书解锁身份证明，不另造 OAuth**。
// 现有 view 解锁已经是一条完整、在用的"证明你是谁"的链路：页面给 code，
// 用户从飞书发出带该 code 的消息，Hub 经飞书信道收到 ⇒ 因而知道 SenderOpenID。
// 业主要的不是去掉飞书证明，是把它从【每页一次】变成【一次】。
//
// 三条硬门（缺一即变成洞）：
//  ① 未命中 principal 一律拒绝，**不得自动建** —— 否则任何飞书用户发一条就登进来了；
//  ② code 一次性、短 TTL，**与页面写锁 token 不共用命名空间** ——
//     复用会把"谁能输入"和"你是谁"搅在一起；
//  ③ 绑定的是**发出那条消息的 SenderOpenID**，不是页面自称的身份 ——
//     **页面不可信，信道可信。**

const (
	consoleLoginVerb   = "#console"
	consoleLoginTTL    = 5 * time.Minute
	consoleSessionTTL  = 12 * time.Hour
	consoleLoginCodeNB = 4 // 8 个十六进制字符
)

var errConsoleLoginNotFound = errors.New("console login code not found or expired")

// consoleLoginTicket 是一次登录尝试。它只在内存里活 consoleLoginTTL。
type consoleLoginTicket struct {
	code      string
	createdAt time.Time
	// 下面两个字段在飞书侧确认后才被填上；未确认时为空。
	sessionToken string // 明文,只交给轮询到的那一个浏览器,交完即抹
	principalID  string
	confirmedAt  time.Time
}

type consoleLoginStore struct {
	mu      sync.Mutex
	tickets map[string]*consoleLoginTicket
}

func newConsoleLoginStore() *consoleLoginStore {
	return &consoleLoginStore{tickets: map[string]*consoleLoginTicket{}}
}

// issue 造一张新票。🔴 code 与页面写锁的 code 池分开（独立 map、独立前缀）。
func (s *consoleLoginStore) issue(now time.Time) (string, error) {
	b := make([]byte, consoleLoginCodeNB)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := "c" + hex.EncodeToString(b) // 前缀 c:与 viewer code 形态不同,一眼可分
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	s.tickets[code] = &consoleLoginTicket{code: code, createdAt: now}
	return code, nil
}

func (s *consoleLoginStore) sweepLocked(now time.Time) {
	for k, t := range s.tickets {
		if now.Sub(t.createdAt) > consoleLoginTTL {
			delete(s.tickets, k)
		}
	}
}

// confirm 由飞书侧调用：把票据标记为已确认,并存下待取的会话令牌。
func (s *consoleLoginStore) confirm(now time.Time, code, principalID, sessionToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	t, ok := s.tickets[code]
	if !ok {
		return errConsoleLoginNotFound
	}
	if t.confirmedAt.IsZero() {
		t.principalID = principalID
		t.sessionToken = sessionToken
		t.confirmedAt = now
	}
	return nil
}

// claim 由浏览器轮询调用。🔴 一次性:取走即从表中删除,
// 防同一 code 被重复兑换成多个会话。
func (s *consoleLoginStore) claim(now time.Time, code string) (principalID, sessionToken string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	t, exists := s.tickets[code]
	if !exists || t.confirmedAt.IsZero() {
		return "", "", false
	}
	delete(s.tickets, code)
	return t.principalID, t.sessionToken, true
}

// parseConsoleLoginCommand 解析 `#console <code>`。
// 🔴 只接受恰好两段,避免把带附言的消息误判成登录指令。
func parseConsoleLoginCommand(text string) (code string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 2 || fields[0] != consoleLoginVerb {
		return "", false
	}
	c := fields[1]
	if len(c) != 1+consoleLoginCodeNB*2 || !strings.HasPrefix(c, "c") {
		return "", false
	}
	if _, err := hex.DecodeString(c[1:]); err != nil {
		return "", false
	}
	return c, true
}

type consoleLoginRepository interface {
	ResolveConsolePrincipal(ctx context.Context, provider, issuer, subject string) (*model.ConsolePrincipal, error)
	CreateConsoleSession(ctx context.Context, session model.ConsoleSession, now time.Time) error
}

// dispatchConsoleLogin 处理来自飞书的 `#console <code>`。
//
// 🔴 本函数是三条硬门里第 ①③ 条的落点：
//
//	· 身份取自 msg.SenderOpenID（信道给的），**不取正文里任何自称**;
//	· ResolveConsolePrincipal 未命中 ⇒ 拒绝,**不建 principal、不建会话**。
func (h *Hub) dispatchConsoleLogin(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	code, ok := parseConsoleLoginCommand(text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	repo, ok := h.Repo.(consoleLoginRepository)
	if !ok {
		return model.PrefixDispatchResult{Matched: true, Reply: "控制台登录暂不可用。"}, true, nil
	}
	subject := strings.TrimSpace(msg.SenderOpenID)
	if subject == "" {
		// 🔴 信道没给出发送者 ⇒ 无法证明"你是谁" ⇒ 拒绝。不猜、不回落。
		return model.PrefixDispatchResult{Matched: true, Reply: "无法确认发送者身份，登录被拒绝。"}, true, nil
	}
	issuer := strings.TrimSpace(msg.BotChannelID)
	principal, err := repo.ResolveConsolePrincipal(ctx, "feishu", issuer, subject)
	if err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "控制台登录暂不可用。"}, true, nil
	}
	if principal == nil || !principal.Active {
		// 🔴 硬门①：未命中一律拒绝,**不自动建 principal**。
		// 否则任何飞书用户发一条 `#console <code>` 就能登进来。
		return model.PrefixDispatchResult{
			Matched: true,
			Reply:   "你尚未被授予控制台访问权限，登录被拒绝。",
		}, true, nil
	}

	token, err := randomConsoleToken()
	if err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "控制台登录暂不可用。"}, true, nil
	}
	now := time.Now()
	sess := model.ConsoleSession{
		ID:          newConsoleSessionID(),
		PrincipalID: principal.ID,
		TokenHash:   hashConsoleToken(token),
		ExpiresAt:   now.Add(consoleSessionTTL),
	}
	if err := repo.CreateConsoleSession(ctx, sess, now); err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "控制台登录暂不可用。"}, true, nil
	}
	if err := h.consoleLogins().confirm(now, code, principal.ID, token); err != nil {
		// code 已过期/已被取走 ⇒ 明说,不静默成功。
		return model.PrefixDispatchResult{
			Matched: true,
			Reply:   "登录码已过期或已被使用，请在页面上重新获取。",
		}, true, nil
	}
	return model.PrefixDispatchResult{
		Matched: true,
		Reply:   fmt.Sprintf("控制台登录已确认，请回到页面。%.0f 小时后需重新登录。", consoleSessionTTL.Hours()),
	}, true, nil
}

func newConsoleSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "cs-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return "cs-" + hex.EncodeToString(b)
}

// consoleLogins 懒初始化票据表。用 sync.Once 而不是在 New 里建,
// 是为了不改动构造签名(那条今天刚被 coder56solzzc 收紧成类型级不变量,不宜再动)。
func (h *Hub) consoleLogins() *consoleLoginStore {
	h.consoleLoginOnce.Do(func() {
		if h.consoleLoginStore == nil {
			h.consoleLoginStore = newConsoleLoginStore()
		}
	})
	return h.consoleLoginStore
}

// HandleConsoleLoginPage is intentionally public: it creates a short-lived
// login challenge but grants no access. Identity proof happens only through
// the authenticated Feishu ingress handled by dispatchConsoleLogin.
func (h *Hub) HandleConsoleLoginPage(w http.ResponseWriter, r *http.Request) {
	code, err := h.consoleLogins().issue(time.Now())
	if err != nil {
		http.Error(w, "console login unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>DingWei Console Login</title><main><h1>登录 Agent Console</h1><p>请在飞书发送：</p><code>#console %s</code><p id=s>等待飞书确认…</p></main><script>const c=%q;const poll=async()=>{const r=await fetch('/console/login/status?code='+encodeURIComponent(c),{cache:'no-store'});if(r.status===200){location.replace('/console');return}if(r.status===202){setTimeout(poll,1000);return}document.querySelector('#s').textContent='登录码已过期，请刷新页面重试。'};poll()</script>`, html.EscapeString(code), code)
}

func (h *Hub) HandleConsoleLoginStatus(w http.ResponseWriter, r *http.Request) {
	code, ok := parseConsoleLoginCommand("#console " + strings.TrimSpace(r.URL.Query().Get("code")))
	if !ok {
		http.Error(w, "invalid login code", http.StatusBadRequest)
		return
	}
	_, token, claimed := h.consoleLogins().claim(time.Now(), code)
	w.Header().Set("Cache-Control", "no-store")
	if !claimed {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"confirmed":false}`))
		return
	}
	// 与续期共用同一个构造器:属性不一致在结构上不可能发生(见 newConsoleSessionCookie)。
	http.SetCookie(w, newConsoleSessionCookie(token))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"confirmed":true}`))
}
