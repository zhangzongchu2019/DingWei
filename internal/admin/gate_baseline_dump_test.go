package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// 🔴 门 A **基线字节** dumper —— 它不是判据本身,是**跨构建对照**的取数装置。
//
// 用途:把「旧 admin 原有行为不因本次搬迁而改变」做成**实测**证据,
// 而不是停在「adminBasePrefix 在门 A 上恒为 /admin」那句**读码推论**上。
// (本组的规矩:读码得出的结论与实测得出的结论必须分开,且推论不当验收依据。)
//
//	DW_ADMIN_BASELINE_OUT=/tmp/doorA-base.txt \
//	  go test ./internal/admin/ -run TestGateDumpDoorABaseline -count=1
//
// 在**改动前的基线**(如 `bcd5f337`)与本分支各跑一次,`diff` 两个输出 ⇒ **必须逐字节全等**。
//
// 🔵 本文件**只用改动前就存在的 API**(`New`／`Mount`／`fakeFileBrowser`),
// 所以它可以**原样拷进基线树**跑 —— 这正是它能当对照装置的前提:两边跑的是同一份取数代码。
// 🔴 因此:**不要**在这里引用任何 Nova-260921-A 新增的符号(consoleDoorRoutes／MountConsole/…),
// 否则它在基线树上编译不过,对照就做不成。
//
// 🔴 凭证纪律(派工第四节③,无例外):`/admin/api-keys` 的**正文不进**本文件。
// 只记它的状态码与 Content-Type;它的前缀正确性由 TestG4DoorPagesLinkInsideTheirOwnDoor
// 用**布尔结果**单独验(不打印正文)。
func TestGateDumpDoorABaseline(t *testing.T) {
	out := os.Getenv("DW_ADMIN_BASELINE_OUT")
	if out == "" {
		t.Skip("设 DW_ADMIN_BASELINE_OUT 才跑(跨构建基线字节对照用)")
	}
	ctx := context.Background()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "baseline.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	srv := New(db)
	srv.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	srv.SecretKey = "baseline-secret-key"
	srv.Files = &fakeFileBrowser{}
	srv.sessions["tok-baseline"] = "admin"
	mux := http.NewServeMux()
	srv.Mount(mux)

	const fileID = "0123456789abcdef0123456789abcdef"
	admin := []*http.Cookie{{Name: "wp_admin", Value: "tok-baseline"}}

	var buf strings.Builder
	dump := func(label, method, path string, cookies []*http.Cookie, form url.Values, body bool) {
		var reader *strings.Reader
		if form != nil {
			reader = strings.NewReader(form.Encode())
		}
		var req *http.Request
		if reader != nil {
			req = httptest.NewRequest(method, path, reader)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		buf.WriteString("=== " + label + " " + method + " " + path + "\n")
		buf.WriteString("status: " + strconv.Itoa(rec.Code) + "\n")
		for _, h := range []string{"Content-Type", "Location", "Content-Disposition", "Cache-Control"} {
			buf.WriteString("header " + h + ": " + rec.Header().Get(h) + "\n")
		}
		if body {
			buf.WriteString("body:\n" + rec.Body.String() + "\n")
		} else {
			buf.WriteString("body: <redacted: credential material>\n")
		}
		buf.WriteString("\n")
	}

	// ── 顺序固定 ⇒ 两次实跑可逐字节比 ──
	// 🔴 先做完所有 GET 再做 POST:POST 会写审计行,而 /admin/provision 页会把审计渲染进去。
	for _, path := range []string{
		"/admin", "/admin/files", "/admin/files/" + fileID + "/content",
		"/admin/provision", "/admin/bot-channels", "/admin/members", "/admin/config", "/admin/sessions",
	} {
		dump("AUTHED", http.MethodGet, path, admin, nil, true)
	}
	dump("AUTHED", http.MethodGet, "/admin/api-keys", admin, nil, false) // 🔴 凭证纪律:不记正文
	for _, path := range []string{"/admin", "/admin/files", "/admin/provision", "/admin/api-keys", "/admin/bot-channels", "/admin/members"} {
		dump("ANON", http.MethodGet, path, nil, nil, true)
	}
	dump("AUTHED", http.MethodPost, "/admin/api-keys", admin, url.Values{}, true)
	dump("AUTHED", http.MethodPost, "/admin/bot-channels", admin,
		url.Values{"action": {"delete"}, "id": {"__baseline_probe__"}}, true)
	// 🔴 门 A 上被 requireAuthCap 包住的 8 条路由要**全覆盖**,否则"字节全等"这句话
	// 只覆盖了其中 7 条 —— 少一条就是少一条,不能含糊过去。
	dump("AUTHED", http.MethodPost, "/admin/provision", admin, url.Values{}, true)
	dump("ANON", http.MethodPost, "/admin/provision", nil, url.Values{}, true)
	dump("ANON", http.MethodPost, "/admin/api-keys", nil, url.Values{}, true)

	if err := os.WriteFile(out, []byte(buf.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%d bytes)", out, buf.Len())
}
