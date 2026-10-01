package m8

import (
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// amrRunGoroutines 数当前进程里正在跑 (*amrProcessor).run 的 goroutine（扫全部栈，不受其它 goroutine 干扰）。
func amrRunGoroutines() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), "(*amrProcessor).run(")
		}
		buf = make([]byte, 2*len(buf))
	}
}

func waitAMRRunGoroutines(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := amrRunGoroutines()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("(*amrProcessor).run goroutines = %d, want %d", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 根治验证：Hub.Close 必须收掉 amr 心跳 goroutine；幂等。
// 此前 run() 无退出分支，每建一个 Hub 永久泄漏一个，随机打红日志卫生用例（台账 4496f6c5 小单乙）。
func TestHubCloseStopsAMRGoroutine(t *testing.T) {
	before := amrRunGoroutines()
	hub := New(nil, ConsoleAuthDisabled())
	waitAMRRunGoroutines(t, before+1)
	hub.Close()
	waitAMRRunGoroutines(t, before)
	hub.Close() // 幂等：第二次不得 panic（close 已关闭的通道会 panic）
	var nilHub *Hub
	nilHub.Close() // nil 安全
}

// 测试里有直接用字面量构造 amrProcessor 的（stop 为空）：Close 不得因此 panic。
// 首版 close() 未判空，m8 整包 -shuffle 跑出 TestNoDisplayGateOnAMRChokePoint「close of nil channel」—— 本用例钉住。
func TestHubCloseToleratesLiteralAMRProcessor(t *testing.T) {
	hub := &Hub{amr: &amrProcessor{}}
	hub.Close()
	hub.Close()
}

// replaceAMR 测试里把 Hub 的 amr 换成测试用处理器时，先收掉原来那个（New 里已起了它的 goroutine）。
// 直接 h.amr = p 会让原处理器永久泄漏：Close 只收得到换上去的那个（复验后插桩定位，6 处）。
func replaceAMR(h *Hub, p *amrProcessor) {
	if h.amr != nil {
		h.amr.close()
	}
	h.amr = p
}

// Close 返回后，run() 必须已经退出（同步收尾），不是只发了信号。
func TestHubCloseWaitsForRunToExit(t *testing.T) {
	before := amrRunGoroutines()
	hub := New(nil, ConsoleAuthDisabled())
	waitAMRRunGoroutines(t, before+1)
	hub.Close()
	if got := amrRunGoroutines(); got != before {
		t.Fatalf("right after Close: run goroutines = %d, want %d (Close must wait for run to return)", got, before)
	}
}

// 换掉 amr 时原处理器被收掉（replaceAMR 的约定）。
func TestReplaceAMRStopsOriginal(t *testing.T) {
	before := amrRunGoroutines()
	hub := New(nil, ConsoleAuthDisabled())
	t.Cleanup(hub.Close)
	waitAMRRunGoroutines(t, before+1)
	replaceAMR(hub, &amrProcessor{})
	if got := amrRunGoroutines(); got != before {
		t.Fatalf("after replaceAMR: run goroutines = %d, want %d", got, before)
	}
}

// Close 必须等在线会话的处理 goroutine 收尾完（释放投递租约等）再返回：
// 否则它们会拖到下一条用例里写已关闭的库、往全局 log 打「session delivery lease release failed」
// （复验 -count=8 -shuffle 时，日志卫生用例因此 8 轮红 7 轮，前一条用例正是开着 WS 会话的那条）。
func TestHubCloseWaitsForSessionHandlers(t *testing.T) {
	hub, _, ctx := newTestHub(t)
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc-close", Name: "svc-close", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc-close", "person")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:closer"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	conn := dialSessionWithHeaderRaw(t, ctx, srv.URL, "closer", key.ID, secret, nil)
	defer conn.CloseNow()
	waitSessionOnline(t, hub, key.ID, "closer")
	hub.Close() // 未先关客户端：Close 自己断连并等收尾
	if hub.sessionOnline(key.ID, "closer") {
		t.Fatal("session still registered after Close returned")
	}
	if n := goroutinesIn("(*Hub).serveSession("); n != 0 {
		t.Fatalf("%d serveSession goroutines still running after Close", n)
	}
}

func goroutinesIn(frame string) int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), frame)
		}
		buf = make([]byte, 2*len(buf))
	}
}
