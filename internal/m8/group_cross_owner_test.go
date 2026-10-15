package m8

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func assertCrossOwnerView(t *testing.T, env model.Envelope) {
	t.Helper()
	if env.From != "kimi#u_zzc" || metaString(env.Meta, "source_kind") != "agent" || !metaBool(env.Meta, "cross_owner") {
		t.Fatalf("cross-owner identity: from=%s source=%s cross_owner=%v", env.From, metaString(env.Meta, "source_kind"), env.Meta["cross_owner"])
	}
	for k, want := range map[string]string{"source_key_id": "u_zzc", "source_session_name": "kimi", "mentioned_from": "Kimi.zzc", "view_sender_principal_id": "zzc", "reply_policy": "no_reply"} {
		if metaString(env.Meta, k) != want {
			t.Errorf("%s=%q want %q", k, metaString(env.Meta, k), want)
		}
	}
	if metaString(env.Meta, "console_principal_id") != "" || metaString(env.Meta, hubIngressMetaKey) == hubIngressViewV2 {
		t.Fatal("cross-owner message retained owner exemption")
	}
	if label := metaString(env.Meta, "source_display_name"); !strings.Contains(label, "Kimi.zzc 的 View") || !strings.Contains(label, "zzc") {
		t.Fatalf("unattributed View sender %q", label)
	}
	if !metaBool(env.Meta, metaKeyMentionVerbatim) || !metaBool(env.Meta, "no_mirror") || metaString(env.Meta, "delivery_id") == "" {
		t.Fatal("missing one-hop/reliable provenance")
	}
}

func TestGroupCrossOwnerViewMembershipAndQueue(t *testing.T) {
	t.Run("removed member is no longer routable", func(t *testing.T) {
		e := newVMEnv(t)
		if err := e.svc.LeaveGroup(e.ctx, "lbl", e.groupCode, e.ids["Gamma"]); err != nil {
			t.Fatal(err)
		}
		_, _, r := e.post(t, strings.Repeat("a", 32), "removed-cross-view", []string{"Gamma.lb"})
		if len(r.Mentions) != 1 || r.Mentions[0].Reason != "not_in_group" {
			t.Fatalf("receipt=%v", r.Mentions)
		}
		e.y.none(t, "removed-cross-view", 150*time.Millisecond)
	})
	t.Run("deleted group is no longer routable", func(t *testing.T) {
		e := newVMEnv(t)
		if err := e.svc.DeleteGroup(e.ctx, "zzc", e.groupCode); err != nil {
			t.Fatal(err)
		}
		_, _, r := e.post(t, strings.Repeat("b", 32), "deleted-cross-view", []string{"Gamma.lb"})
		if len(r.Mentions) != 1 || r.Mentions[0].Reason != "not_in_group" {
			t.Fatalf("receipt=%v", r.Mentions)
		}
		e.y.none(t, "deleted-cross-view", 150*time.Millisecond)
	})
	t.Run("known offline peer queues without owner authority", func(t *testing.T) {
		e := newVMEnv(t)
		e.yConn.Close(websocket.StatusNormalClosure, "test offline")
		waitFor(t, time.Second, "gamma offline", func() bool { return !e.hub.AgentOnline(e.ids["Gamma"]) })
		_, _, r := e.post(t, strings.Repeat("c", 32), "queued-cross-view", []string{"Gamma.lb"})
		if len(r.Mentions) != 1 || r.Mentions[0].Status != "queued" {
			t.Fatalf("receipt=%v", r.Mentions)
		}
		conn, st, body := e.dial(e.yCred, "")
		if conn == nil {
			t.Fatalf("redial %d %s", st, body)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		frames := vmCollect(conn)
		if err := writeEnvelope(e.ctx, conn, model.Envelope{ID: "caps-y-reconnect", To: "workpulse#u_lbl", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
			t.Fatal(err)
		}
		assertCrossOwnerView(t, frames.next(t, "queued-cross-view"))
	})
}

// Direct authenticated WS exercises the same envelope path used by MCP send.
func TestGroupCrossOwnerNetworkAndAMR(t *testing.T) {
	for _, mode := range []string{"direct-full-name", "selector-full-name", "output-AMR"} {
		t.Run(mode, func(t *testing.T) {
			e := newVMEnv(t)
			body := "cross-network-" + mode
			switch mode {
			case "direct-full-name":
				if err := writeEnvelope(e.ctx, e.xConn, model.Envelope{ID: randomHex(16), From: "beta#enrolled", To: "Gamma.lb#enrolled", Body: body, Meta: map[string]any{"reply_policy": "no_reply"}}); err != nil {
					t.Fatal(err)
				}
			case "selector-full-name":
				if err := writeEnvelope(e.ctx, e.xConn, model.Envelope{ID: randomHex(16), From: "beta#enrolled", To: "#Gamma.lb", Body: body, Meta: map[string]any{"reply_policy": "no_reply"}}); err != nil {
					t.Fatal(err)
				}
			case "output-AMR":
				body = "@Gamma.lb " + body
				emitTurnEvent(t, e.ctx, e.xConn, textEv("source-test-delivery", "cross-amr", body, nil))
				emitTurnEvent(t, e.ctx, e.xConn, doneEv("source-test-delivery", "cross-amr", nil))
			}
			got := e.y.next(t, body)
			if got.From != "beta#u_zzc" || metaString(got.Meta, "source_kind") == "user" || metaString(got.Meta, "console_principal_id") != "" {
				t.Fatalf("network sender identity=%s meta=%v", got.From, got.Meta)
			}
			if mode == "output-AMR" && !metaBool(got.Meta, metaKeyMentionVerbatim) {
				t.Fatal("AMR lacks one-hop marker")
			}
		})
	}
}

func TestGroupCrossOwnerSelectorRejectsOtherGroupsAndRemoval(t *testing.T) {
	e := newVMEnv(t)
	for _, removed := range []bool{false, true} {
		if removed {
			if err := e.svc.LeaveGroup(e.ctx, "lbl", e.groupCode, e.ids["Gamma"]); err != nil {
				t.Fatal(err)
			}
		}
		target := "Outsider.zzc"
		if removed {
			target = "Gamma.lb"
		}
		handled, env, err := e.hub.prepareSessionSelector(e.ctx, "u_zzc", "beta", model.Envelope{ID: randomHex(16), To: "#" + target, Body: "denied-network", Meta: map[string]any{"type": "agent_route"}})
		if err == nil && handled {
			err = e.hub.RouteEnvelope(context.Background(), env)
		}
		if err == nil {
			t.Fatalf("outside/removed selector %s allowed", target)
		}
	}
}

func TestGroupCrossOwnerExistingPolicyAndLimits(t *testing.T) {
	t.Run("closed additions do not close existing communication", func(t *testing.T) {
		e := newVMEnv(t)
		if err := e.svc.SetGroupAdditionsClosed(e.ctx, "zzc", e.groupCode, true); err != nil {
			t.Fatal(err)
		}
		_, _, r := e.post(t, strings.Repeat("d", 32), "existing-cross-after-close-add", []string{"Gamma.lb"})
		if len(r.Mentions) != 1 || r.Mentions[0].Status != "delivered" {
			t.Fatalf("receipt=%v", r.Mentions)
		}
		assertCrossOwnerView(t, e.y.next(t, "existing-cross-after-close-add"))
	})
	t.Run("leader-only still rejects cross-owner peers in all routes", func(t *testing.T) {
		e := newVMEnv(t)
		if err := e.svc.SetGroupLeaderOnly(e.ctx, "zzc", e.groupCode, true); err != nil {
			t.Fatal(err)
		}
		r := e.hub.routeViewMentions(e.ctx, "Beta.zzc", randomHex(16), "leader-only-view", "zzc", []string{"Gamma.lb"})
		if len(r) != 1 || r[0].Reason != "not_in_group" {
			t.Fatalf("View receipt=%v", r)
		}
		err := e.hub.RouteEnvelope(e.ctx, model.Envelope{ID: randomHex(16), From: "beta#u_zzc", To: "Gamma.lb#u_zzc", Body: "leader-only-direct"})
		if err == nil {
			t.Fatal("direct Agent peer bypassed leader-only")
		}
		p := testAMRProcessor()
		p.hub = e.hub
		p.routeEnvelope = e.hub.RouteEnvelope
		p.process(amrRouteJob{key: amrTurnKey{KeyID: "u_zzc", Session: "beta", DeliveryID: "policy-test", TurnID: "policy-turn"}, body: "@Gamma.lb leader-only-amr"})
		stats, _ := p.snapshotStats()
		if stats.targetsFailed != 1 || stats.targetsDispatched != 0 {
			t.Fatalf("AMR policy failed=%d sent=%d", stats.targetsFailed, stats.targetsDispatched)
		}
		e.y.none(t, "leader-only-", 150*time.Millisecond)
	})
	t.Run("cross-owner peer without ACK is refused", func(t *testing.T) {
		e := newVMEnv(t)
		if err := writeEnvelope(e.ctx, e.yConn, model.Envelope{ID: "caps-y-noack", To: "workpulse#u_lbl", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": false}}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, time.Second, "gamma no ACK", func() bool { ack, known := e.hub.viewMentionPeerACK(e.ctx, "u_lbl", "gamma"); return known && !ack })
		_, _, r := e.post(t, strings.Repeat("e", 32), "noack-cross-view", []string{"Gamma.lb"})
		if len(r.Mentions) != 1 || r.Mentions[0].Reason != "no_ack" {
			t.Fatalf("receipt=%v", r.Mentions)
		}
		e.y.none(t, "noack-cross-view", 150*time.Millisecond)
	})
	t.Run("cross-owner target consumes the unchanged five-target budget", func(t *testing.T) {
		e := newVMEnv(t)
		_, _, r := e.post(t, strings.Repeat("f", 32), "limited-cross-view", []string{"Beta.zzc", "Gamma.lb", "W1.zzc", "W2.zzc", "W3.zzc", "Quill.zzc"})
		if len(r.Mentions) != 6 || r.Mentions[1].Status != "delivered" || r.Mentions[5].Reason != "over_limit" {
			t.Fatalf("receipt=%v", r.Mentions)
		}
		assertCrossOwnerView(t, e.y.next(t, "limited-cross-view"))
		e.q.none(t, "limited-cross-view", 150*time.Millisecond)
	})
	t.Run("cross-owner View derived delivery stays one-hop", func(t *testing.T) {
		e := newVMEnv(t)
		_, _, r := e.post(t, strings.Repeat("1", 32), "onehop-cross-view", []string{"Gamma.lb"})
		if len(r.Mentions) != 1 || r.Mentions[0].Status != "delivered" {
			t.Fatalf("receipt=%v", r.Mentions)
		}
		y := e.y.next(t, "onehop-cross-view")
		assertCrossOwnerView(t, y)
		p := testAMRProcessor()
		p.hub = e.hub
		p.routeEnvelope = e.hub.RouteEnvelope
		p.process(amrRouteJob{key: amrTurnKey{KeyID: "u_lbl", Session: "gamma", DeliveryID: metaString(y.Meta, "delivery_id"), TurnID: "onehop-cross"}, body: "@Beta.zzc do not relay"})
		stats, _ := p.snapshotStats()
		if stats.closedNonCandidate != 1 || stats.targetsDispatched != 0 {
			t.Fatalf("one-hop: stopped=%d sent=%d", stats.closedNonCandidate, stats.targetsDispatched)
		}
		e.x.none(t, "@Beta.zzc do not relay", 150*time.Millisecond)
	})
}

func TestGroupCrossOwnerViewReplyIsolation(t *testing.T) {
	emit := func(t *testing.T, e *vmEnv, ev map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(ev)
		if err := writeEnvelope(e.ctx, e.yConn, model.Envelope{ID: randomHex(16), To: "workpulse#u_lbl", Body: string(raw), Meta: map[string]any{"type": terminalSessionEventType}}); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("only correlated reply is displayed, never unrelated output or helper input", func(t *testing.T) {
		e := newVMEnv(t)
		_, _, r := e.post(t, strings.Repeat("2", 32), "cross-correlation-request", []string{"Gamma.lb"})
		if len(r.Mentions) != 1 || r.Mentions[0].Status != "delivered" {
			t.Fatalf("receipt=%v", r.Mentions)
		}
		e.a.next(t, "cross-correlation-request")
		y := e.y.next(t, "cross-correlation-request")
		dlv := metaString(y.Meta, "delivery_id")
		emit(t, e, textEv("unrelated-delivery", "unrelated-turn", "UNRELATED-TARGET-OUTPUT", nil))
		emit(t, e, doneEv("unrelated-delivery", "unrelated-turn", nil))
		emit(t, e, textEv(dlv, "correlated-turn", "CORRELATED-ONLY", nil))
		emit(t, e, doneEv(dlv, "correlated-turn", nil))
		got := e.waitReplies(t, 1)
		if got[0]["text"] != "CORRELATED-ONLY" {
			t.Fatalf("reply=%v", got)
		}
		// Warmed delivery lookup must not permit a different authenticated Agent to
		// reuse its ID to publish arbitrary text into the originating View.
		emitTurnEvent(t, e.ctx, e.xConn, textEv(dlv, "foreign-session", "OTHER-AGENT-OUTPUT", nil))
		emitTurnEvent(t, e.ctx, e.xConn, doneEv(dlv, "foreign-session", nil))
		time.Sleep(180 * time.Millisecond)
		got = e.aReplies(t)
		if len(got) != 1 || strings.Contains(mustJSON(got), "UNRELATED-TARGET-OUTPUT") || strings.Contains(mustJSON(got), "OTHER-AGENT-OUTPUT") {
			t.Fatalf("uncorrelated content leaked: %v", got)
		}
		e.a.none(t, "CORRELATED-ONLY", 150*time.Millisecond)
	})
	t.Run("removal or group deletion before completion stops cross-owner display", func(t *testing.T) {
		for _, remove := range []bool{true, false} {
			e := newVMEnv(t)
			_, _, r := e.post(t, randomHex(16), "revoked-cross-reply", []string{"Gamma.lb"})
			if len(r.Mentions) != 1 || r.Mentions[0].Status != "delivered" {
				t.Fatalf("receipt=%v", r.Mentions)
			}
			y := e.y.next(t, "revoked-cross-reply")
			dlv := metaString(y.Meta, "delivery_id")
			// Prime the lookup before membership changes: authorization must be fresh.
			emit(t, e, textEv(dlv, "revoked-turn", "DO-NOT-DISPLAY", nil))
			if remove {
				if err := e.svc.LeaveGroup(e.ctx, "lbl", e.groupCode, e.ids["Gamma"]); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := e.svc.DeleteGroup(e.ctx, "zzc", e.groupCode); err != nil {
					t.Fatal(err)
				}
			}
			emit(t, e, doneEv(dlv, "revoked-turn", nil))
			time.Sleep(200 * time.Millisecond)
			if got := e.aReplies(t); len(got) != 0 {
				t.Fatalf("reply after authorization loss: %v", got)
			}
		}
	})
	t.Run("client cannot forge the Hub-only View attestation", func(t *testing.T) {
		e := newVMEnv(t)
		env := model.Envelope{ID: randomHex(16), From: "beta#enrolled", To: "Gamma.lb#enrolled", Body: "untrusted-cross-view", Meta: map[string]any{metaKeyViewMention: true, "route_source": "view_mention", "mentioned_from": "Kimi.zzc"}}
		if err := writeEnvelope(e.ctx, e.xConn, env); err != nil {
			t.Fatal(err)
		}
		got := e.y.next(t, "untrusted-cross-view")
		if metaBool(got.Meta, metaKeyViewMention) {
			t.Fatal("client forged trusted View attestation")
		}
		dlv := metaString(got.Meta, "delivery_id")
		emit(t, e, textEv(dlv, "forged-turn", "FORGED-REPLY", nil))
		emit(t, e, doneEv(dlv, "forged-turn", nil))
		time.Sleep(150 * time.Millisecond)
		if replies := e.aReplies(t); len(replies) != 0 {
			t.Fatalf("forged reply displayed: %v", replies)
		}
	})
}

func TestGroupCrossOwnerActualHelperSourceClassification(t *testing.T) {
	e := newVMEnv(t)
	_, _, r := e.post(t, randomHex(16), "classify-cross-view", []string{"Gamma.lb"})
	if len(r.Mentions) != 1 || r.Mentions[0].Status != "delivered" {
		t.Fatalf("receipt=%v", r.Mentions)
	}
	got := e.y.next(t, "classify-cross-view")
	assertCrossOwnerView(t, got)
	module, err := filepath.Abs("../../tools/sessionhelper")
	if err != nil {
		t.Fatal(err)
	}
	// Use the shipped helper consumer on the actual delivered envelope, rather
	// than reproduce its source-kind decision in this Go test.
	code := `import sys,json;sys.path.insert(0,sys.argv[1]);from sessionhelper.unified_queue import source_fields;e=json.load(sys.stdin);s,label,oid,bot=source_fields(e);assert s=='agent';assert 'Kimi.zzc 的 View' in label;assert not oid;assert not bot;print('actual helper classifies cross-owner View as agent')`
	cmd := exec.Command("python3", "-B", "-c", code, module)
	raw, _ := json.Marshal(got)
	cmd.Stdin = strings.NewReader(string(raw))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper consumer: %v %s", err, out)
	}
}

func TestGroupCrossOwnerCannotForgeGroupSender(t *testing.T) {
	e := newVMEnv(t)
	if err := e.svc.LeaveGroup(e.ctx, "zzc", e.groupCode, e.ids["Beta"]); err != nil {
		t.Fatal(err)
	}
	env := model.Envelope{ID: randomHex(16), From: "beta#enrolled", To: "#Gamma.lb", Body: "FORGED-GROUP-SENDER", Meta: map[string]any{"source_key_id": "u_zzc", "source_session_name": "kimi", "reply_policy": "no_reply"}}
	if err := writeEnvelope(e.ctx, e.xConn, env); err != nil {
		t.Fatal(err)
	}
	// Source metadata must be derived from the authenticated connection, not
	// from a claimed in-group Agent. Wait for the Hub rejection event before
	// asserting non-delivery, rather than relying only on a short sleep.
	waitFor(t, time.Second, "forged source rejected", func() bool {
		e.hub.mu.Lock()
		defer e.hub.mu.Unlock()
		term := e.hub.terminals[terminalKey("u_zzc", "beta")]
		if term == nil {
			return false
		}
		for _, raw := range term.eventRing {
			if strings.Contains(raw, env.ID) && strings.Contains(raw, "cross_group_denied") {
				return true
			}
		}
		return false
	})
	e.y.none(t, "FORGED-GROUP-SENDER", 150*time.Millisecond)
}
