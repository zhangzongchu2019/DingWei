package m8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// 内嵌 store.Repository(为 nil)以满足接口;只覆写本条路径用到的两个方法。
// 🔴 若被测代码走到别的方法,会 panic —— 那正是我们想要的:
//
//	它会把"路径走错了"变成一次响亮的失败,而不是静默返回零值。
type fakeLoginRepo struct {
	store.Repository
	principal *model.ConsolePrincipal
	created   []model.ConsoleSession
	resolveIn [3]string
}

func (f *fakeLoginRepo) ResolveConsolePrincipal(_ context.Context, provider, issuer, subject string) (*model.ConsolePrincipal, error) {
	f.resolveIn = [3]string{provider, issuer, subject}
	return f.principal, nil
}

func (f *fakeLoginRepo) CreateConsoleSession(_ context.Context, s model.ConsoleSession, _ time.Time) error {
	f.created = append(f.created, s)
	return nil
}

func newLoginHub(p *model.ConsolePrincipal) (*Hub, *fakeLoginRepo) {
	repo := &fakeLoginRepo{principal: p}
	return &Hub{Repo: repo}, repo
}

func msgFrom(sender, channel string) model.Message {
	return model.Message{SenderOpenID: sender, BotChannelID: channel}
}

func TestConsoleLoginHappyPathCreatesSessionAndConfirmsTicket(t *testing.T) {
	h, repo := newLoginHub(&model.ConsolePrincipal{ID: "owner-a", Active: true})
	code, err := h.consoleLogins().issue(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	res, handled, err := h.dispatchConsoleLogin(context.Background(),
		msgFrom("ou_owner", "greyTestRobot"), "#console "+code)
	if err != nil || !handled || !res.Matched {
		t.Fatalf("handled=%v res=%+v err=%v", handled, res, err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("应恰好建一个会话, got %d", len(repo.created))
	}
	// 🔴 身份必须取自信道给的 SenderOpenID
	if repo.resolveIn != [3]string{"feishu", "greyTestRobot", "ou_owner"} {
		t.Fatalf("解析身份用错了输入: %v", repo.resolveIn)
	}
	pid, token, ok := h.consoleLogins().claim(time.Now(), code)
	if !ok || pid != "owner-a" || token == "" {
		t.Fatalf("票据未被确认: ok=%v pid=%q token空=%v", ok, pid, token == "")
	}
	if repo.created[0].TokenHash != hashConsoleToken(token) {
		t.Fatal("下发的令牌与入库的哈希对不上")
	}
}

// 🔴 最要紧的一条:未命中 principal ⇒ 拒绝,且**一个会话都不许建**。
// 缺这条 ⇒ 任何飞书用户发一条 `#console <code>` 就能登进来。
func TestConsoleLoginUnknownSenderIsRefusedAndCreatesNothing(t *testing.T) {
	h, repo := newLoginHub(nil) // 查无此人
	code, _ := h.consoleLogins().issue(time.Now())
	res, handled, _ := h.dispatchConsoleLogin(context.Background(),
		msgFrom("ou_stranger", "greyTestRobot"), "#console "+code)
	if !handled || !res.Matched {
		t.Fatal("应当被处理并给出拒绝回复")
	}
	if !strings.Contains(res.Reply, "拒绝") {
		t.Fatalf("回复未说明被拒: %q", res.Reply)
	}
	if len(repo.created) != 0 {
		t.Fatalf("🔴 未命中却建了 %d 个会话", len(repo.created))
	}
	if _, _, ok := h.consoleLogins().claim(time.Now(), code); ok {
		t.Fatal("🔴 未命中却把票据标成已确认 —— 浏览器会据此登入")
	}
}

func TestConsoleLoginInactivePrincipalIsRefused(t *testing.T) {
	h, repo := newLoginHub(&model.ConsolePrincipal{ID: "owner-a", Active: false})
	code, _ := h.consoleLogins().issue(time.Now())
	res, _, _ := h.dispatchConsoleLogin(context.Background(),
		msgFrom("ou_owner", "greyTestRobot"), "#console "+code)
	if !strings.Contains(res.Reply, "拒绝") || len(repo.created) != 0 {
		t.Fatalf("停用的 principal 必须被拒且不建会话: reply=%q created=%d", res.Reply, len(repo.created))
	}
}

// 🔴 信道没给出发送者 ⇒ 无法证明"你是谁" ⇒ 必须拒绝,不得回落到任何默认身份。
func TestConsoleLoginEmptySenderIsRefused(t *testing.T) {
	h, repo := newLoginHub(&model.ConsolePrincipal{ID: "owner-a", Active: true})
	code, _ := h.consoleLogins().issue(time.Now())
	res, _, _ := h.dispatchConsoleLogin(context.Background(),
		msgFrom("", "greyTestRobot"), "#console "+code)
	if !strings.Contains(res.Reply, "拒绝") || len(repo.created) != 0 {
		t.Fatalf("空发送者必须被拒: reply=%q created=%d", res.Reply, len(repo.created))
	}
}

// 🔴 一次性:同一个 code 不得兑换出第二个会话。
func TestConsoleLoginCodeIsSingleUse(t *testing.T) {
	h, _ := newLoginHub(&model.ConsolePrincipal{ID: "owner-a", Active: true})
	code, _ := h.consoleLogins().issue(time.Now())
	_, _, _ = h.dispatchConsoleLogin(context.Background(), msgFrom("ou_owner", "c"), "#console "+code)
	if _, _, ok := h.consoleLogins().claim(time.Now(), code); !ok {
		t.Fatal("第一次取应当成功")
	}
	if _, _, ok := h.consoleLogins().claim(time.Now(), code); ok {
		t.Fatal("🔴 同一个 code 被取了第二次")
	}
}

// 过期的 code ⇒ 必须明说,不得静默当作成功。
func TestConsoleLoginExpiredCodeSaysSoInsteadOfSilentlySucceeding(t *testing.T) {
	h, _ := newLoginHub(&model.ConsolePrincipal{ID: "owner-a", Active: true})
	past := time.Now().Add(-2 * consoleLoginTTL)
	code, _ := h.consoleLogins().issue(past)
	res, _, _ := h.dispatchConsoleLogin(context.Background(),
		msgFrom("ou_owner", "greyTestRobot"), "#console "+code)
	if !strings.Contains(res.Reply, "过期") {
		t.Fatalf("过期必须明说: %q", res.Reply)
	}
}

func TestParseConsoleLoginCommand(t *testing.T) {
	h, _ := newLoginHub(nil)
	good, _ := h.consoleLogins().issue(time.Now())
	if c, ok := parseConsoleLoginCommand("#console " + good); !ok || c != good {
		t.Fatalf("正常形态未被识别: %q", good)
	}
	for _, bad := range []string{
		"#console",                           // 缺 code
		"#console " + good + " 附言",           // 🔴 带附言的消息不得被当成登录指令
		"#unlock " + good,                    // 动词不对
		"#console zzz",                       // 非法 code
		"#console " + strings.Repeat("a", 9), // 长度不对且无前缀
	} {
		if _, ok := parseConsoleLoginCommand(bad); ok {
			t.Fatalf("不该被识别为登录指令: %q", bad)
		}
	}
}

// 🔴 命名空间隔离:页面写锁那种形态的 code 不得被当成 console 登录码。
// 复用会把"谁能输入"和"你是谁"搅在一起。
func TestViewerStyleCodeIsNotAConsoleLoginCode(t *testing.T) {
	if _, ok := parseConsoleLoginCommand("#console zzc-opus50ts-2642-Ab3dEf90"); ok {
		t.Fatal("🔴 viewer 形态的 code 被当成了 console 登录码")
	}
}

func TestConsoleLoginHTTPPageToCookie(t *testing.T) {
	h, _ := newLoginHub(&model.ConsolePrincipal{ID: "owner-a", Active: true})
	pageReq := httptest.NewRequest(http.MethodGet, "/console/login", nil)
	pageRes := httptest.NewRecorder()
	h.HandleConsoleLoginPage(pageRes, pageReq)
	if pageRes.Code != 200 {
		t.Fatalf("page code=%d", pageRes.Code)
	}
	match := regexp.MustCompile(`#console (c[0-9a-f]{8})`).FindStringSubmatch(pageRes.Body.String())
	if len(match) != 2 {
		t.Fatalf("login code absent: %s", pageRes.Body.String())
	}
	code := match[1]
	pending := httptest.NewRecorder()
	h.HandleConsoleLoginStatus(pending, httptest.NewRequest(http.MethodGet, "/console/login/status?code="+code, nil))
	if pending.Code != http.StatusAccepted {
		t.Fatalf("pending=%d", pending.Code)
	}
	if _, _, err := h.dispatchConsoleLogin(context.Background(), msgFrom("ou_owner", "app"), "#console "+code); err != nil {
		t.Fatal(err)
	}
	confirmed := httptest.NewRecorder()
	h.HandleConsoleLoginStatus(confirmed, httptest.NewRequest(http.MethodGet, "/console/login/status?code="+code, nil))
	if confirmed.Code != 200 {
		t.Fatalf("confirmed=%d", confirmed.Code)
	}
	cookies := confirmed.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	c := cookies[0]
	if c.Name != consoleSessionCookie || c.Value == "" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("unsafe cookie=%+v", c)
	}
	replay := httptest.NewRecorder()
	h.HandleConsoleLoginStatus(replay, httptest.NewRequest(http.MethodGet, "/console/login/status?code="+code, nil))
	if replay.Code != http.StatusAccepted || len(replay.Result().Cookies()) != 0 {
		t.Fatalf("replay code=%d cookies=%v", replay.Code, replay.Result().Cookies())
	}
}

func TestConsoleLoginIsWiredIntoRealL1Dispatch(t *testing.T) {
	h, repo := newLoginHub(&model.ConsolePrincipal{ID: "owner-a", Active: true})
	code, _ := h.consoleLogins().issue(time.Now())
	res, intent, _, err := h.dispatchL1(context.Background(), msgFrom("ou_owner", "app"), "#console "+code)
	if err != nil || intent != "command.console_login" || !res.Matched || len(repo.created) != 1 {
		t.Fatalf("intent=%q res=%+v created=%d err=%v", intent, res, len(repo.created), err)
	}
	if _, _, ok := h.consoleLogins().claim(time.Now(), code); !ok {
		t.Fatal("真实派发路径未确认票据")
	}
}
