// Command console-hosted-page-fixture renders the production console handler with the
// server-side hosted feature enabled. It exists only for the Playwright regression gate,
// so that the browser fixture cannot mutate the inspected JavaScript flag.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
)

func main() {
	hub := &m8.Hub{}
	if os.Getenv("DW_CONSOLE_TEMP_GROUPS_FIXTURE") == "1" {
		hub.TempGroups = &tempgroup.Service{}
	}
	if os.Getenv("DW_CONSOLE_NAME_CHECK_FIXTURE") == "1" {
		hub.Identity = identity.NewService(identity.NewMemStore(), nil, nil)
	}
	// Synthetic browser-only grant; production injects the real N3 session fence.
	hub.SetConsoleHostedAccessCheck(func(*http.Request) bool { return os.Getenv("DW_CONSOLE_HOSTED_ACCESS_FIXTURE") != "0" })
	hub.SetConsoleHostedPageEnabled(os.Getenv("DW_CONSOLE_HOSTED_FIXTURE") != "0")
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	if rr.Code != http.StatusOK {
		panic(fmt.Sprintf("console status %d", rr.Code))
	}
	fmt.Print(rr.Body.String())
}
