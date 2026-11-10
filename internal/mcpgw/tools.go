package mcpgw

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

func knownTool(name string) bool {
	for _, n := range extshared.MVPScopes {
		if n == name {
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
	switch name {
	case "list_online":
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
	case "send_message":
		var a struct {
			To          string `json:"to"`
			Body        string `json:"body"`
			ClientID    string `json:"client_message_id"`
			ExpectReply bool   `json:"expect_reply"`
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
		s, e := g.cfg.Seats.Send(ctx, seat, a.To, a.Body, a.ClientID, a.ExpectReply)
		if e != nil {
			return nil, e
		}
		return map[string]any{"delivery_id": s.DeliveryID, "generation": s.Generation, "stage": s.Stage, "created_at": s.CreatedAt, "expires_at": s.ExpiresAt}, nil
	case "wait_for_messages":
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
			out = append(out, map[string]any{"delivery_id": d.ID, "generation": d.Generation, "from": d.From, "to": d.To, "body": d.Body, "group": d.Group, "created_at": d.CreatedAt, "expires_at": d.ExpiresAt, "lease_until": d.LeaseUntil, "phase": d.Phase})
		}
		return map[string]any{"messages": out}, nil
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
func toolDefinitions() []any {
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
	return []any{
		tool("list_online", "查看本 AG 内在线席位。", schema(map[string]any{})),
		tool("send_message", "按显式全名向本 AG 成员发送；expect_reply=true 时请求自动回信，默认 false；重试沿用 client_message_id。受理不等于送达。", schema(map[string]any{"to": stringProp(), "body": stringProp(), "client_message_id": stringProp(), "expect_reply": map[string]any{"type": "boolean"}}, "to", "body", "client_message_id")),
		tool("wait_for_messages", "领取本人未确认投递；按 delivery_id 与 generation 去重后显式确认。", schema(map[string]any{"wait_seconds": map[string]any{"type": "number", "minimum": 0, "maximum": extshared.WaitMax.Seconds()}})),
		tool("ack", "确认本人投递已收到，不表示已执行。", schema(map[string]any{"delivery_id": stringProp(), "generation": map[string]any{"type": "integer", "minimum": 1}, "phase": map[string]any{"type": "string", "enum": []string{string(extshared.PhaseReceived)}}}, "delivery_id", "generation", "phase")),
	}
}
