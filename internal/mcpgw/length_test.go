package mcpgw

import (
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"strings"
	"testing"
)

func TestMCPLengthEscapedHTTP(t *testing.T) {
	_, s, mux := fixture(t)
	for _, unit := range []string{"a", "中", "😀", "\x00"} {
		for _, delta := range []int{-1, 0, 1} {
			n := extshared.MessageBodyMaxBytes + delta
			body := strings.Repeat(unit, n/len(unit)) + strings.Repeat("x", n%len(unit))
			args, _ := json.Marshal(map[string]any{"to": "target.zzc", "body": body, "client_message_id": "boundary"})
			for _, protocol := range []string{"rest", "2026", "2025"} {
				r := restRequest("send_message", string(args))
				if protocol != "rest" {
					r = rpcRequestFor("tools/call", "send_message", string(args))
				}
				if protocol == "2025" {
					r = requestCompat("tools/call", `{"name":"send_message","arguments":`+string(args)+`}`)
				}
				before := s.calls
				w := serve(mux, r)
				if delta > 0 {
					if w.Code != 413 || s.calls != before || !strings.Contains(w.Body.String(), "body_too_large") || !strings.Contains(w.Body.String(), "actual_bytes") || !strings.Contains(w.Body.String(), "1048576") {
						t.Fatalf("MCP_LEN_OVERSIZE_ZERO_DELIVERY protocol=%s status=%d response=%s", protocol, w.Code, w.Body.String())
					}
				} else if w.Code != 200 || s.calls != before+1 {
					t.Fatalf("MCP_LEN_EXACT_UTF8 protocol=%s bytes=%d status=%d %s", protocol, n, w.Code, w.Body.String())
				}
			}
		}
	}
}
