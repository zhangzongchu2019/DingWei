package m8

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func groupReplyTurn(t *testing.T, e *vmEnv, inbound model.Envelope, target string, body string) {
	t.Helper()
	conn := e.yConn
	if target == "quill" {
		conn = e.qConn
	}
	id := metaString(inbound.Meta, "delivery_id")
	if id == "" {
		t.Fatal("forward has no durable delivery identity")
	}
	emitTurnEvent(t, e.ctx, conn, textEv(id, "reply-turn", body, nil))
	emitTurnEvent(t, e.ctx, conn, doneEv(id, "reply-turn", nil))
}

func TestGroupReplyAMR(t *testing.T) {
	for _, mode := range []string{"full-name", "member-selector", "same-owner"} {
		t.Run(mode, func(t *testing.T) {
			e := newVMEnv(t)
			target, name := "gamma", "@Gamma.lb"
			peer := e.y
			if mode == "member-selector" {
				name = "@lb#gamma"
			}
			if mode == "same-owner" {
				target, name, peer = "quill", "@Quill.zzc", e.q
			}
			body := name + " reply-mainline"
			emitTurnEvent(t, e.ctx, e.xConn, textEv("original-source-turn", "ask", body, nil))
			emitTurnEvent(t, e.ctx, e.xConn, doneEv("original-source-turn", "ask", nil))
			got := peer.next(t, body)
			if replyPolicyOf(got) != "expect_reply" {
				t.Fatalf("AMR policy=%q", replyPolicyOf(got))
			}
			if originAddrOf(got) != "beta#u_zzc" {
				t.Fatalf("origin=%q From=%q", originAddrOf(got), got.From)
			}
			// The answer deliberately contains a valid mention: it must return verbatim,
			// never start a fresh transfer to that mentioned Agent.
			answer := "@Quill.zzc response-for-origin"
			groupReplyTurn(t, e, got, target, answer)
			reply := e.x.next(t, answer)
			if reply.To != "beta#u_zzc" || reply.From != sessionAddress(target, map[bool]string{true: "u_zzc", false: "u_lbl"}[target == "quill"]) || kindOf(reply) != kindReply || replyPolicyOf(reply) != "no_reply" || !metaBool(reply.Meta, metaKeyMentionVerbatim) {
				t.Fatalf("wrong return: to=%s from=%s kind=%s policy=%s", reply.To, reply.From, kindOf(reply), replyPolicyOf(reply))
			}
			e.q.none(t, answer, 100*time.Millisecond)
			// The originating Agent's next output is not a new request or a ping-pong.
			id := metaString(reply.Meta, "delivery_id")
			emitTurnEvent(t, e.ctx, e.xConn, textEv(id, "reply-to-reply", "@Gamma.lb no-third-hop", nil))
			emitTurnEvent(t, e.ctx, e.xConn, doneEv(id, "reply-to-reply", nil))
			e.y.none(t, "@Gamma.lb no-third-hop", 200*time.Millisecond)
			e.x.none(t, answer, 100*time.Millisecond)
		})
	}
}

func TestGroupReplyMembership(t *testing.T) {
	for _, revoke := range []string{"removed", "deleted", "leader-only"} {
		t.Run(revoke, func(t *testing.T) {
			e := newVMEnv(t)
			ask := "pending-cross-owner-reply"
			if err := e.hub.RouteEnvelope(e.ctx, model.Envelope{ID: randomHex(16), From: "beta#u_zzc", To: "Gamma.lb#u_zzc", Body: ask, Meta: map[string]any{"reply_policy": "expect_reply"}}); err != nil {
				t.Fatal(err)
			}
			got := e.y.next(t, ask)
			var err error
			switch revoke {
			case "removed":
				err = e.svc.LeaveGroup(e.ctx, "lbl", e.groupCode, e.ids["Gamma"])
			case "deleted":
				err = e.svc.DeleteGroup(e.ctx, "zzc", e.groupCode)
			case "leader-only":
				err = e.svc.SetGroupLeaderOnly(e.ctx, "zzc", e.groupCode, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			groupReplyTurn(t, e, got, "gamma", "membership-blocked-response")
			e.x.none(t, "membership-blocked-response", 300*time.Millisecond)
			waitFor(t, time.Second, "visible failed return", func() bool {
				e.hub.amr.mu.Lock()
				defer e.hub.amr.mu.Unlock()
				return e.hub.amr.stats.replyOriginUnavailable > 0
			})
		})
	}
}

func TestGroupReplyMCP(t *testing.T) {
	python := os.Getenv("DW_SPEC_E2E_PYTHON")
	if python == "" {
		t.Skip("requires locked helper Python; set DW_SPEC_E2E_PYTHON")
	}
	e := newVMEnv(t)
	// Replace the synthetic Kimi socket with an actual enrolled helper and MCP
	// subprocess. The existing bridge is generic tools/call, with private logs.
	e.hub.mu.Lock()
	c := e.hub.sessionClients["u_zzc"]["kimi"]
	e.hub.mu.Unlock()
	c.conn.CloseNow()
	waitFor(t, time.Second, "source disconnected", func() bool { return !e.hub.sessionOnline("u_zzc", "kimi") })
	b := startSpecMCPBridgeAs(t, &shimEnv{srv: e.srv}, python, e.zKimiCred, "Kimi", "u_zzc")
	defer b.stop()
	packet := b.packet("send_message", map[string]any{"to": "Gamma.lb", "body": "mcp-cross-owner-question", "reply_policy": "expect_reply"})
	wire, _ := json.Marshal(packet)
	if strings.Contains(string(wire), `"isError":true`) {
		t.Fatal("actual MCP send failed")
	}
	got := e.y.next(t, "mcp-cross-owner-question")
	if replyPolicyOf(got) != "expect_reply" || originAddrOf(got) != "kimi#u_zzc" {
		t.Fatal("actual MCP lost policy/origin")
	}
	groupReplyTurn(t, e, got, "gamma", "mcp-returned-to-source")
	// Real helper's desktop inbox must expose the return to the initiating MCP.
	var inbox string
	waitFor(t, 3*time.Second, "MCP return in actual helper inbox", func() bool {
		result := groupMCPResult(t, b.packet("dw_inbox", map[string]any{"operation": "list"}))
		messages, _ := result["messages"].([]any)
		for _, item := range messages {
			m, _ := item.(map[string]any)
			read := groupMCPResult(t, b.packet("dw_inbox", map[string]any{"operation": "read", "message_id": m["id"]}))
			raw, _ := json.Marshal(read)
			inbox = string(raw)
			if strings.Contains(inbox, "mcp-returned-to-source") {
				return true
			}
		}
		return false
	})
	if strings.Contains(inbox, "no-third-hop") {
		t.Fatal("unexpected forwarding")
	}
	for _, policy := range []string{"", "no_reply"} {
		args := map[string]any{"to": "Gamma.lb", "body": "mcp-no-return-" + policy}
		if policy != "" {
			args["reply_policy"] = policy
		}
		b.packet("send_message", args)
		inbound := e.y.next(t, "mcp-no-return-"+policy)
		e.hub.amr.mu.Lock()
		before := e.hub.amr.stats.replyOriginSkipped
		e.hub.amr.mu.Unlock()
		groupReplyTurn(t, e, inbound, "gamma", "must-not-return-"+policy)
		waitFor(t, time.Second, "MCP no-policy/no-reply skipped", func() bool {
			e.hub.amr.mu.Lock()
			defer e.hub.amr.mu.Unlock()
			return e.hub.amr.stats.replyOriginSkipped > before
		})
		result := groupMCPResult(t, b.packet("dw_inbox", map[string]any{"operation": "list"}))
		messages, _ := result["messages"].([]any)
		for _, item := range messages {
			m, _ := item.(map[string]any)
			read := groupMCPResult(t, b.packet("dw_inbox", map[string]any{"operation": "read", "message_id": m["id"]}))
			raw, _ := json.Marshal(read)
			if strings.Contains(string(raw), "must-not-return-") {
				t.Fatal("MCP default/explicit no_reply unexpectedly returned")
			}
		}
	}

}

// Generic desktop tools return their IPC object directly, unlike spec tools'
// nested result envelope. Decode the real MCP tools/call content accordingly.
func groupMCPResult(t *testing.T, packet map[string]any) map[string]any {
	t.Helper()
	result, ok := packet["result"].(map[string]any)
	if !ok || result["isError"] == true {
		t.Fatal("MCP desktop call failed")
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatal("MCP desktop content missing")
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	var frame map[string]any
	if json.Unmarshal([]byte(text), &frame) != nil || frame["ok"] != true {
		t.Fatal("desktop IPC result failed")
	}
	return frame
}

func TestGroupReplyTrueSourceAndDeliveryBinding(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := replyOriginFixture(t, h, ctx, "dlv-true-source", "source-message", "ant#k1", map[string]any{
		"source_key_id": "k2", "source_session_name": "real-agent", "origin_addr": "ant#k1", "reply_policy": "expect_reply",
	})
	p, routed := replyOriginProcessor(h)
	p.replyToOrigin(amrRouteJob{key: key, body: "real-source-return"})
	if len(*routed) != 1 || (*routed)[0].To != "real-agent#k2" {
		t.Fatal("return did not restore the true source")
	}
	env, _ := h.applyReplyOriginTermination(model.Envelope{ID: randomHex(16), From: "ant#k1", Meta: map[string]any{"source_key_id": "k2", "source_session_name": "real-agent"}})
	if originAddrOf(env) != "real-agent#k2" {
		t.Fatal("origin capture trusted rewritten From")
	}
	key.KeyID = "other-key"
	p.replyToOrigin(amrRouteJob{key: key, body: "foreign-delivery"})
	if len(*routed) != 1 {
		t.Fatal("foreign session borrowed another delivery origin")
	}
}

func TestGroupReplyChannelOneHop(t *testing.T) {
	h, keyID, _ := channelTestHub(t)
	for _, kind := range []string{kindContent, kindReply} {
		id := "dlv-channel-" + kind
		inbound := model.Envelope{ID: id, From: sessionAddress("ant", keyID), To: sessionAddress("bee", keyID), Meta: map[string]any{metaKeyMentionVerbatim: true, metaKeyKind: kind, "reply_policy": "expect_reply"}}
		raw, _ := json.Marshal(inbound)
		if err := h.Repo.EnqueueSessionDelivery(context.Background(), model.SessionDelivery{ID: id, KeyID: keyID, SessionName: "bee", Envelope: string(raw), SourceMessageID: id}); err != nil {
			t.Fatal(err)
		}
		p, routed := channelProcessor(h, keyID)
		p.process(amrRouteJob{key: amrTurnKey{KeyID: keyID, Session: "bee", DeliveryID: id, TurnID: "channel-quote"}, body: "@feishu#ou_target quoted channel"})
		if kind == kindReply {
			if len(*routed) != 0 {
				t.Fatal("reply forwarded a channel mention")
			}
		} else if len(*routed) != 1 || (*routed)[0].To != sessionAddress("ant", keyID) {
			t.Fatal("quoted channel did not return only to origin")
		}
	}
}

func TestGroupReplyMarkerCannotBeSpoofed(t *testing.T) {
	e := newVMEnv(t)
	if err := writeEnvelope(e.ctx, e.xConn, model.Envelope{ID: randomHex(16), From: "beta#enrolled", To: "Gamma.lb#enrolled", Body: "spoof-return-marker", Meta: map[string]any{metaKeyReplyOriginReturn: true, "route_source": replyToOriginRouteSource, metaKeyKind: kindReply}}); err != nil {
		t.Fatal(err)
	}
	got := e.y.next(t, "spoof-return-marker")
	if metaBool(got.Meta, metaKeyReplyOriginReturn) {
		t.Fatal("client supplied a Hub-only return marker")
	}
}
