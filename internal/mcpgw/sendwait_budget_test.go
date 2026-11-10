package mcpgw

import (
	"context"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type slowReplySeats struct{ *waitSeats }

func (s slowReplySeats) Send(ctx context.Context, a extseat.Seat, to, body, id string, expect ...bool) (extseat.SendReceipt, error) {
	// Deterministic acceptance overhead, before the requested wait window starts.
	select {
	case <-ctx.Done():
		return extseat.SendReceipt{}, ctx.Err()
	case <-time.After(30 * time.Millisecond):
	}
	return s.waitSeats.Send(ctx, a, to, body, id, expect...)
}
func (s slowReplySeats) WaitReply(ctx context.Context, a extseat.Seat, since time.Time, max time.Duration) ([]extseat.Delivery, error) {
	timer := time.NewTimer(max)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return []extseat.Delivery{}, nil
	}
}

// Local HTTP deadline probe; documents Codex's official default separately.
// This does not claim a desktop client or live nginx was exercised.
func TestSendWaitHTTPDeadlineBudget(t *testing.T) {
	g, base, mux := fixture(t)
	g.cfg.SendReplyMax = extshared.SendReplyMax
	g.cfg.Seats = slowReplySeats{&waitSeats{seatsFake: base}}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for _, budget := range []time.Duration{50 * time.Second, 60 * time.Second} {
		t.Run(budget.String(), func(t *testing.T) {
			req, e := http.NewRequest("POST", srv.URL+"/api/external/send", strings.NewReader(`{"to":"target.zzc","body":"budget","client_message_id":"budget","wait_reply_seconds":55}`))
			if e != nil {
				t.Fatal(e)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer local-token")
			start := time.Now()
			response, e := (&http.Client{Timeout: budget}).Do(req)
			if budget == 50*time.Second {
				var timeout net.Error
				if !errors.As(e, &timeout) || !timeout.Timeout() {
					if response != nil {
						response.Body.Close()
					}
					t.Fatalf("MCP_SENDWAIT_CLIENT_BUDGET expected deadline e=%v", e)
				}
			} else {
				if e != nil {
					t.Fatalf("MCP_SENDWAIT_CLIENT_BUDGET e=%v", e)
				}
				defer response.Body.Close()
				b, e := io.ReadAll(response.Body)
				if e != nil || response.StatusCode != 200 || !strings.Contains(string(b), `"timed_out":true`) {
					t.Fatalf("MCP_SENDWAIT_CLIENT_BUDGET status=%d e=%v body=%s", response.StatusCode, e, b)
				}
			}
			if time.Since(start) < 49*time.Second {
				t.Fatal("MCP_SENDWAIT_CLIENT_BUDGET probe did not exercise real configured wait window")
			}
		})
	}
}
