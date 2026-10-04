// Command console-hosted-page-fixture renders the production console handler with the
// server-side hosted feature enabled. It exists only for the Playwright regression gate,
// so that the browser fixture cannot mutate the inspected JavaScript flag.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

func main() {
	hub := &m8.Hub{}
	hub.EnableConsoleHostedPage()
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	if rr.Code != http.StatusOK {
		panic(fmt.Sprintf("console status %d", rr.Code))
	}
	fmt.Print(rr.Body.String())
}
