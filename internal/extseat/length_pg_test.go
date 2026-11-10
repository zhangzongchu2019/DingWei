package extseat

import (
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"strings"
	"testing"
)

func exactBody(unit string, n int) string {
	return strings.Repeat(unit, n/len(unit)) + strings.Repeat("x", n%len(unit))
}
func TestMCPLengthUTF8PG(t *testing.T) {
	f := newFixture(t, pgtest.DSN(t))
	_, a := f.issue(t)
	header := extshared.ExternalSenderHeader(a.FullName, "alice", "alice", "helper.ho") + "\n"
	for _, unit := range []string{"a", "中", "😀"} {
		for _, delta := range []int{-1, 0, 1} {
			for _, direction := range []string{"send", "helper"} {
				t.Run(fmt.Sprintf("%x/%d/%s", unit, delta, direction), func(t *testing.T) {
					n := extshared.MessageBodyMaxBytes + delta
					if direction == "send" {
						n -= len(header)
					}
					body := exactBody(unit, n)
					id := fmt.Sprintf("len-%x-%d-%s", unit, delta, direction)
					before := f.touches(t)
					var err error
					var receipt SendReceipt
					if direction == "send" {
						receipt, err = f.s.Send(f.ctx, a, "helper.ho", body, id)
					} else {
						receipt, err = f.s.EnqueueFromHelper(f.ctx, "helper", a.AgentID, id, body)
					}
					if delta > 0 {
						var large *extshared.BodyTooLargeError
						if !errors.As(err, &large) || large.ActualBytes != extshared.MessageBodyMaxBytes+1 || large.LimitBytes != extshared.MessageBodyMaxBytes || f.touches(t) != before {
							t.Fatalf("MCP_LEN_OVERSIZE_ZERO_DELIVERY err=%v touches=%d/%d", err, f.touches(t), before)
						}
						var count int
						row, e := f.pool.QueryRow(f.ctx, f.access, `SELECT (SELECT count(*) FROM np_extseat_send_key WHERE client_message_id=$1)+(SELECT count(*) FROM np_extseat_inbox WHERE source_message_id=$1)`, id)
						if e != nil {
							t.Fatal(e)
						}
						if e = row.Scan(&count); e != nil || count != 0 {
							t.Fatalf("MCP_LEN_OVERSIZE_ZERO_DELIVERY rows=%d err=%v", count, e)
						}
					} else {
						if err != nil {
							t.Fatalf("MCP_LEN_EXACT_UTF8 bytes=%d err=%v", n, err)
						}
						var stored string
						table := "np_extseat_outbox"
						column := "delivery_id"
						if direction == "helper" {
							table = "np_extseat_inbox"
							column = "id"
						}
						row, e := f.pool.QueryRow(f.ctx, f.access, `SELECT body FROM `+table+` WHERE `+column+`=$1`, receipt.DeliveryID)
						if e != nil {
							t.Fatal(e)
						}
						if e = row.Scan(&stored); e != nil || stored != body {
							t.Fatalf("MCP_LEN_EXACT_UTF8 altered body err=%v", e)
						}
					}
				})
			}
		}
	}
}
