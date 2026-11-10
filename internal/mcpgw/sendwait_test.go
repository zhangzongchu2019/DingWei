package mcpgw

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"strings"
	"testing"
	"time"
)

type waitSeats struct {
	*seatsFake
	replies []extseat.Delivery
	acked   int
	since   time.Time
	waitFor time.Duration
}

func (s *waitSeats) WaitReply(ctx context.Context, a extseat.Seat, since time.Time, d time.Duration) ([]extseat.Delivery, error) {
	s.since = since
	s.waitFor = d
	return s.replies, nil
}
func (s *waitSeats) Ack(ctx context.Context, a extseat.Seat, id string, gen int) error {
	s.acked++
	return s.seatsFake.Ack(ctx, a, id, gen)
}
func (s *waitSeats) Check(context.Context, extseat.Seat) ([]extseat.InboxSummary, error) {
	return []extseat.InboxSummary{}, nil
}
func (s *waitSeats) Clear(_ context.Context, _ extseat.Seat, confirm bool) ([]string, error) {
	if !confirm {
		return nil, extshared.ErrNotPermitted
	}
	return []string{}, nil
}
func TestSendWaitDefaultsReplyTimeoutAndAliases(t *testing.T) {
	g, base, mux := fixture(t)
	s := &waitSeats{seatsFake: base}
	g.cfg.Seats = s
	for _, name := range []string{"send", "send_message"} {
		s.replies = nil
		s.acked = 0
		w := serve(mux, restRequest(name, `{"to":"target.zzc","body":"hi","client_message_id":"wait","wait_reply_seconds":1}`))
		if w.Code != 200 || !base.expectReply || !strings.Contains(w.Body.String(), `"timed_out":true`) || !strings.Contains(w.Body.String(), `"reply":null`) || !strings.Contains(w.Body.String(), "稍后调用 recv") || s.acked != 0 {
			t.Fatalf("MCP_SENDWAIT_TIMEOUT %s %d %s", name, w.Code, w.Body.String())
		}
		s.replies = []extseat.Delivery{{ID: "delivery", Generation: 2, From: "target.zzc", Body: "reply"}}
		w = serve(mux, rpcRequestFor("tools/call", name, `{"to":"target.zzc","body":"hi","client_message_id":"wait","wait_reply_seconds":1,"expect_reply":false}`))
		if w.Code != 200 || base.expectReply || s.acked != 1 || !strings.Contains(w.Body.String(), `"phase":"received"`) {
			t.Fatalf("MCP_SENDWAIT_AUTORECEIVED %s %d %s", name, w.Code, w.Body.String())
		}
	}
	for _, pair := range [][2]string{{"send", "send_message"}, {"recv", "wait_for_messages"}, {"list", "list_online"}} {
		args := `{}`
		if pair[0] == "send" {
			args = `{"to":"target.zzc","body":"hi","client_message_id":"alias"}`
		}
		if pair[0] == "recv" {
			args = `{"wait_seconds":0}`
		}
		a := serve(mux, restRequest(pair[0], args))
		b := serve(mux, restRequest(pair[1], args))
		if a.Code != 200 || a.Body.String() != b.Body.String() {
			t.Fatalf("MCP_ALIAS_EQUIVALENT %v %s/%s", pair, a.Body, b.Body)
		}
	}
	for _, name := range []string{"check", "clear"} {
		args := `{}`
		if name == "clear" {
			args = `{"confirm":true}`
		}
		w := serve(mux, restRequest(name, args))
		if w.Code != 200 {
			t.Fatalf("MCP_SHORT_SCOPE_OLD_TOKEN %s %d %s", name, w.Code, w.Body)
		}
	}
	if w := serve(mux, restRequest("clear", `{}`)); w.Code == 200 {
		t.Fatal("MCP_CLEAR_CONFIRM")
	}
	for _, v := range []string{"-1", "56", "null", "\"1\""} {
		if v == "null" {
			continue
		}
		w := serve(mux, restRequest("send", `{"to":"target.zzc","body":"hi","client_message_id":"bad","wait_reply_seconds":`+v+`}`))
		if w.Code == 200 {
			t.Fatalf("MCP_SENDWAIT_MAX %s", v)
		}
	}
	for name, scope := range map[string]string{"send": "send_message", "recv": "wait_for_messages", "check": "wait_for_messages", "list": "list_online", "clear": "ack"} {
		if !knownTool(name) || requiredScope(name) != scope {
			t.Fatalf("MCP_SHORT_SCOPE_MAPPING %s", name)
		}
	}
	for _, name := range []string{"check_messages", "execute", "unknown"} {
		if knownTool(name) {
			t.Fatalf("MCP_SHORT_SCOPE_MAPPING forbidden %s", name)
		}
		if w := serve(mux, restRequest(name, `{}`)); w.Code == 200 {
			t.Fatalf("MCP_SHORT_SCOPE_MAPPING forbidden %s", name)
		}
	}
	if extshared.ScopeString() != "list_online send_message wait_for_messages ack" {
		t.Fatal("MCP_SHORT_SCOPE_MAPPING changed scopes")
	}
	// Every exposed alias must still enforce revoked tokens.
	base.authErr = extshared.ErrTokenRevoked
	for _, name := range []string{"send", "recv", "check", "list", "clear"} {
		if w := serve(mux, restRequest(name, `{}`)); w.Code != 401 {
			t.Fatalf("MCP_SHORT_SCOPE_REVOKED %s %d", name, w.Code)
		}
	}
}
