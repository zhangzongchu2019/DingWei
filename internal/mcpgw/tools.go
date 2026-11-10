package mcpgw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

// A missing mapping is denied; token scope strings and PG validation stay fixed.
func requiredScope(name string) string {
	switch name {
	case "send", "send_message":
		return "send_message"
	case "recv", "check", "wait_for_messages":
		return "wait_for_messages"
	case "list", "list_online":
		return "list_online"
	case "clear", "ack":
		return "ack"
	}
	return ""
}
func knownTool(name string) bool {
	scope := requiredScope(name)
	for _, s := range extshared.MVPScopes {
		if scope == s {
			return true
		}
	}
	return false
}
func parseArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return errParams
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errParams
	}
	var tail any
	if e := d.Decode(&tail); e != io.EOF {
		return errParams
	}
	return nil
}
func (g *Gateway) call(ctx context.Context, seat extseat.Seat, name string, raw json.RawMessage) (any, error) {
	if !knownTool(name) {
		return nil, errMethod
	}
	switch name {
	case "list", "list_online":
		if e := parseArgs(raw, &struct{}{}); e != nil {
			return nil, e
		}
		agents, e := g.cfg.Seats.ListOnline(ctx, seat)
		if e != nil {
			return nil, e
		}
		out := make([]map[string]any, 0, len(agents))
		for _, a := range agents {
			out = append(out, map[string]any{"full_name": a.FullName, "last_seen_at": a.LastSeenAt, "external": a.External})
		}
		return map[string]any{"agents": out}, nil
	case "send", "send_message":
		var a struct {
			To          string   `json:"to"`
			Body        string   `json:"body"`
			ClientID    string   `json:"client_message_id"`
			ExpectReply *bool    `json:"expect_reply"`
			WaitSeconds *float64 `json:"wait_reply_seconds"`
		}
		if e := parseArgs(raw, &a); e != nil {
			return nil, e
		}
		if a.To == "" || a.Body == "" || a.ClientID == "" || len(a.ClientID) > 128 {
			return nil, errParams
		}
		if e := extshared.CheckMessageBody(a.Body); e != nil {
			return nil, e
		}
		wait := time.Duration(0)
		expect := a.WaitSeconds != nil
		if a.ExpectReply != nil {
			expect = *a.ExpectReply
		}
		if a.WaitSeconds != nil {
			if *a.WaitSeconds < 0 || *a.WaitSeconds > g.cfg.SendReplyMax.Seconds() {
				return nil, errParams
			}
			wait = time.Duration(*a.WaitSeconds * float64(time.Second))
		}
		var waiter interface {
			WaitReply(context.Context, extseat.Seat, time.Time, time.Duration) ([]extseat.Delivery, error)
		}
		if wait > 0 {
			var ok bool
			waiter, ok = g.cfg.Seats.(interface {
				WaitReply(context.Context, extseat.Seat, time.Time, time.Duration) ([]extseat.Delivery, error)
			})
			if !ok {
				return nil, extshared.ErrNotPermitted
			}
		}
		s, e := g.cfg.Seats.Send(ctx, seat, a.To, a.Body, a.ClientID, expect)
		if e != nil {
			return nil, e
		}
		out := map[string]any{"delivery_id": s.DeliveryID, "generation": s.Generation, "stage": s.Stage, "created_at": s.CreatedAt, "expires_at": s.ExpiresAt}
		if wait > 0 {
			ds, e := waiter.WaitReply(ctx, seat, s.CreatedAt, wait)
			if e != nil {
				return nil, e
			}
			if len(ds) > 0 {
				d := ds[0]
				if e = g.cfg.Seats.Ack(ctx, seat, d.ID, int(d.Generation)); e != nil {
					return nil, e
				}
				d.Phase = extshared.PhaseReceived
				out["reply"] = deliveryFields(d)
				out["timed_out"] = false
			} else {
				out["reply"] = nil
				out["timed_out"] = true
				out["hint"] = "对方暂未回复，消息已送达。稍后调用 recv 领取回复（或先用 check 查看是否有新消息）。"
			}
		}
		return out, nil
	case "recv", "wait_for_messages":
		var a struct {
			Seconds *float64 `json:"wait_seconds"`
		}
		if e := parseArgs(raw, &a); e != nil {
			return nil, e
		}
		wait := extshared.WaitMax
		if a.Seconds != nil {
			if *a.Seconds < 0 || *a.Seconds > extshared.WaitMax.Seconds() {
				return nil, errParams
			}
			wait = time.Duration(*a.Seconds * float64(time.Second))
		}
		ds, e := g.cfg.Seats.Wait(ctx, seat, wait)
		if e != nil {
			return nil, e
		}
		out := make([]map[string]any, 0, len(ds))
		for _, d := range ds {
			out = append(out, deliveryFields(d))
		}
		return map[string]any{"messages": out}, nil
	case "check":
		if e := parseArgs(raw, &struct{}{}); e != nil {
			return nil, e
		}
		checker, ok := g.cfg.Seats.(interface {
			Check(context.Context, extseat.Seat) ([]extseat.InboxSummary, error)
		})
		if !ok {
			return nil, extshared.ErrNotPermitted
		}
		ds, e := checker.Check(ctx, seat)
		if e != nil {
			return nil, e
		}
		return map[string]any{"count": len(ds), "messages": ds}, nil
	case "clear":
		var a struct {
			Confirm bool `json:"confirm"`
		}
		if e := parseArgs(raw, &a); e != nil {
			return nil, e
		}
		if !a.Confirm {
			return nil, errParams
		}
		clearer, ok := g.cfg.Seats.(interface {
			Clear(context.Context, extseat.Seat, bool) ([]string, error)
		})
		if !ok {
			return nil, extshared.ErrNotPermitted
		}
		ids, e := clearer.Clear(ctx, seat, a.Confirm)
		if e != nil {
			return nil, e
		}
		return map[string]any{"count": len(ids), "delivery_ids": ids}, nil
	case "ack":
		var a struct {
			ID         string `json:"delivery_id"`
			Generation int    `json:"generation"`
			Phase      string `json:"phase"`
		}
		if e := parseArgs(raw, &a); e != nil {
			return nil, e
		}
		if a.ID == "" || len(a.ID) > 256 || a.Generation < 1 || a.Phase != string(extshared.PhaseReceived) {
			return nil, errParams
		}
		if e := g.cfg.Seats.Ack(ctx, seat, a.ID, a.Generation); e != nil {
			return nil, e
		}
		return map[string]any{"delivery_id": a.ID, "generation": a.Generation, "phase": extshared.PhaseReceived}, nil
	default:
		return nil, errMethod
	}
}
func deliveryFields(d extseat.Delivery) map[string]any {
	return map[string]any{"delivery_id": d.ID, "generation": d.Generation, "from": d.From, "to": d.To, "body": d.Body, "group": d.Group, "created_at": d.CreatedAt, "expires_at": d.ExpiresAt, "lease_until": d.LeaseUntil, "phase": d.Phase}
}
func toolDefinitions(maxWait ...time.Duration) []any {
	cap := extshared.SendReplyMax
	if len(maxWait) > 0 {
		cap = maxWait[0]
	}
	stringProp := func() any { return map[string]any{"type": "string"} }
	schema := func(props map[string]any, required ...string) any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	tool := func(name, description string, s any) any {
		return map[string]any{"name": name, "description": description, "inputSchema": s}
	}
	old := []any{
		tool("list_online", "查看本 AG 内在线席位。", schema(map[string]any{})),
		tool("send_message", "按全名向本AG成员发送；wait_reply_seconds可在本次调用等首条新回信并自动received，缺省0；有等待参数时expect_reply缺省true，显式false尊重；无等待参数时缺省false。超时给recv/check提示，重试沿用client_message_id；受理不等于送达。", schema(map[string]any{"to": stringProp(), "body": stringProp(), "client_message_id": stringProp(), "expect_reply": map[string]any{"type": "boolean"}, "wait_reply_seconds": map[string]any{"type": "number", "minimum": 0, "maximum": cap.Seconds()}}, "to", "body", "client_message_id")),
		tool("wait_for_messages", "领取本人未确认投递；按 delivery_id 与 generation 去重后显式确认。", schema(map[string]any{"wait_seconds": map[string]any{"type": "number", "minimum": 0, "maximum": extshared.WaitMax.Seconds()}})),
		tool("ack", "确认本人投递已收到，不表示已执行。", schema(map[string]any{"delivery_id": stringProp(), "generation": map[string]any{"type": "integer", "minimum": 1}, "phase": map[string]any{"type": "string", "enum": []string{string(extshared.PhaseReceived)}}}, "delivery_id", "generation", "phase")),
	}
	primary := []any{}
	for _, pair := range [][2]string{{"send", "send_message"}, {"recv", "wait_for_messages"}, {"list", "list_online"}} {
		for _, entry := range old {
			d := entry.(map[string]any)
			if d["name"] == pair[1] {
				copy := map[string]any{}
				for k, v := range d {
					copy[k] = v
				}
				copy["name"] = pair[0]
				copy["description"] = d["description"]
				if pair[0] == "send" {
					copy["description"] = fmt.Sprintf("%s；同步等待上限%g秒（在客户端默认60秒超时内留出响应预算）。", d["description"], cap.Seconds())
				}
				d["description"] = fmt.Sprintf("已更名为 %s，旧名仍可用。%s", pair[0], copy["description"])
				primary = append(primary, copy)
			}
		}
	}
	primary = append(primary, tool("check", "立即只查待收数量与摘要；不领取、不改租约、不续组。", schema(map[string]any{})), tool("clear", "将本席已到达queued与leased消息标为received；必须confirm:true，不影响发出方向。", schema(map[string]any{"confirm": map[string]any{"type": "boolean"}}, "confirm")))
	return append(primary, old...)
}
