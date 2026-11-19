package m8

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
)

func TestExternalSeatSenderReplyRoundTripPGAndHub(t *testing.T) {
	v := newViewEnv(t)
	ctx := v.ctx
	pool := pgtest.Pool(t)
	access, err := repo.NewAccessCtx("extseat-sender-e2e", "temporary PG sender attribution fixture")
	if err != nil {
		t.Fatal(err)
	}
	extseatDDL, err := os.ReadFile("../store/migrate/migrations/0105_extseat.sql")
	if err != nil {
		t.Fatal(err)
	}
	groupDDL, err := os.ReadFile("../store/migrate/migrations/0106_tempgroup.sql")
	if err != nil {
		t.Fatal(err)
	}
	policyDDL, err := os.ReadFile("../store/migrate/migrations/0110_extseat_expect_reply.sql")
	if err != nil {
		t.Fatal(err)
	}
	copyDDL, err := os.ReadFile("../store/migrate/migrations/0111_extseat_feishu_copy.sql")
	if err != nil {
		t.Fatal(err)
	}
	routeDDL, err := os.ReadFile("../store/migrate/migrations/0113_extseat_groups.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := `CREATE TABLE audit_log(id bigserial PRIMARY KEY,actor text,action text,target text,after_json text,ts timestamptz);` + identity.PGSchemaSQL + string(extseatDDL) + string(policyDDL) + string(copyDDL) + string(groupDDL) + string(routeDDL)
	if _, err = pool.Exec(ctx, access, ddl); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	target, err := v.svc.Store.FindAgentByName(ctx, "zzc", "kimi")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, access, `INSERT INTO np_user(user_id_lc,user_id,short_name,short_name_lc,display_name,type,state,created_at) VALUES('zzc','zzc','zzc','zzc','业主显示名','member','active',$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,credential_hash,created_at) VALUES($1,'zzc',$2,$3,'selfhost','active',$4,$5)`, target.ID, target.Name, strings.ToLower(target.Name), target.CredentialHash, now); err != nil {
		t.Fatal(err)
	}
	pgIdentity := identity.NewService(identity.NewPGStore(pool), func() time.Time { return now }, nil)
	groups := &tempgroup.Service{Pool: pool, Access: access, Now: func() time.Time { return now }}
	pgIdentity.CommunicationTenantKey = "tenant-e2e"
	seats := &extseat.Service{Pool: pool, Access: access, Identity: pgIdentity, Groups: groups, TenantKey: "tenant-e2e", Resource: "https://fixture.invalid/mcp", Now: func() time.Time { return now }}
	principal := extshared.Principal{Provider: "feishu", TenantKey: "tenant-e2e", Issuer: "fixture-app", Subject: "mcp-asist", DisplayName: "MCP 授权人"}
	targetFullName := v.svc.FullName(ctx, target)
	_, seat, seatGroup, err := seats.IssueToken(ctx, principal, "asist", targetFullName)
	if err != nil {
		t.Fatalf("issue external seat: %v", err)
	}
	v.hub.Identity = pgIdentity
	v.hub.ExternalSeats = seats

	receiver, status, body := v.dial(v.zKimiCred, "")
	if status != 101 {
		t.Fatalf("receiver websocket status=%d body=%s", status, body)
	}
	defer receiver.Close(websocket.StatusNormalClosure, "done")
	receiver.SetReadLimit(16 << 20)
	_ = readEnvelopeIncludingSystem(t, ctx, receiver)
	waitSessionOnline(t, v.hub, "u_zzc", "kimi")
	if err := writeEnvelope(ctx, receiver, model.Envelope{ID: "caps-extseat-e2e", To: "workpulse#u_zzc", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); err != nil {
		t.Fatal(err)
	}

	const originalBody = "请确认外部席位的收件身份。"
	if _, err = seats.Send(ctx, seat, targetFullName, originalBody, "extseat-sender-e2e-001", true); err != nil {
		t.Fatal(err)
	}
	if did, err := seats.ProjectOne(ctx, HubExtseatProjector{Hub: v.hub}); !did || err != nil {
		t.Fatalf("PG outbox projection did=%t err=%v", did, err)
	}
	delivered := readEnvelope(t, ctx, receiver)
	ackInbound := func(got model.Envelope) {
		if e := writeEnvelope(ctx, receiver, model.Envelope{To: "workpulse#u_zzc", Meta: map[string]any{"type": "delivery_ack", "delivery_id": metaString(got.Meta, "delivery_id"), "state": "processed"}}); e != nil {
			t.Fatal(e)
		}
	}
	ackInbound(delivered)
	readExpected := func(id string) model.Envelope {
		for {
			got := readEnvelope(t, ctx, receiver)
			ackInbound(got)
			if got.ID == id {
				return got
			}
		}
	}
	if !strings.Contains(delivered.Body, "对方请求自动回程：本轮回复将原样发给该外部 Agent") {
		t.Fatal("MCP_EXTERNAL_REPLY_WARNING_ENABLED")
	}
	wantHeader := strings.TrimSuffix(extshared.ExternalMessagePrefix(seat.FullName, principal.DisplayName, "", targetFullName, true), "\n")
	if delivered.Body != wantHeader+"\n"+originalBody || metaString(delivered.Meta, "origin_kind") != "extseat" || metaString(delivered.Meta, "source_display_name") != seat.FullName || !metaBool(delivered.Meta, "source_external") || metaString(delivered.Meta, metaKeyReplyPolicy) != replyPolicyExpectReply {
		t.Fatalf("MCP_EXTERNAL_REPLY_WARNING_ENABLED EXTSEAT_EXPECT_REPLY_POLICY EXTSEAT_SENDER_HEADER_FROM_LOCKED_IDENTITY projected body/meta mismatch: body=%q meta=%#v", delivered.Body, delivered.Meta)
	}

	// Run the production completed-turn processor with the persisted source
	// delivery and assistant output. Its non-candidate branch must invoke
	// reply-origin, then Hub.RouteEnvelope and extseat.
	p := testAMRProcessor()
	p.hub = v.hub
	const assistantReply = "已确认，收到来自外部 Agent 的消息。"
	p.process(amrRouteJob{key: amrTurnKey{KeyID: "u_zzc", Session: "kimi", DeliveryID: delivered.ID, TurnID: "assistant-turn-001"}, body: assistantReply})
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	replies, err := seats.Wait(waitCtx, seat, 2*time.Second)
	if err != nil || len(replies) != 1 {
		t.Fatalf("EXTSEAT_REPLY_ROUTE_E2E wait replies=%+v err=%v", replies, err)
	}
	if replies[0].From != targetFullName || replies[0].To != seat.FullName || replies[0].Body != assistantReply {
		t.Fatalf("EXTSEAT_REPLY_ROUTE_E2E reply identity/body=%+v want from=%s to=%s", replies[0], targetFullName, seat.FullName)
	}

	// Boundary matrix uses the production PG service, projector, AMR processor,
	// explicit route, and the real send.py --file CLI through a local authenticated
	// helper IPC bridge. Listener validation is exercised separately in Python.
	for _, unit := range []string{"a", "中", "😀"} {
		for _, delta := range []int{-1, 0, 1} {
			makeBody := func(n int) string { return strings.Repeat(unit, n/len(unit)) + strings.Repeat("x", n%len(unit)) }
			id := fmt.Sprintf("len-%x-%d", unit, delta)
			inBody := makeBody(extshared.MessageBodyMaxBytes + delta - len(wantHeader) - 1)
			receipt, err := seats.Send(ctx, seat, targetFullName, inBody, id, true)
			if delta > 0 {
				var large *extshared.BodyTooLargeError
				if !errors.As(err, &large) {
					t.Fatalf("MCP_LEN_OVERSIZE_ZERO_DELIVERY inbound err=%v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("MCP_LEN_EXACT_UTF8 send %v", err)
				}
				if did, e := seats.ProjectOne(ctx, HubExtseatProjector{Hub: v.hub}); !did || e != nil {
					t.Fatalf("MCP_LEN_EXACT_UTF8 project %v", e)
				}
				got := readExpected(receipt.DeliveryID)
				if got.Body != wantHeader+"\n"+inBody || len(got.Body) != extshared.MessageBodyMaxBytes+delta || got.ID != receipt.DeliveryID {
					t.Fatal("MCP_LEN_EXACT_UTF8 projection altered")
				}
			}
			for _, route := range []string{"send.py", "automatic"} {
				body := makeBody(extshared.MessageBodyMaxBytes + delta)
				if route == "send.py" {
					err = sendLengthCLI(t, body, seat.FullName+"#u_"+seat.OwnerLC, func(to, body string) error {
						return v.hub.RouteEnvelope(ctx, model.Envelope{ID: id + route, From: sessionAddress("kimi", "u_zzc"), To: to, Body: body})
					})
				} else {
					// A real persisted inbound source requesting reply-origin.
					autoReceipt, e := seats.Send(ctx, seat, targetFullName, "reply source", id+route, true)
					if e != nil {
						t.Fatal(e)
					}
					if did, e := seats.ProjectOne(ctx, HubExtseatProjector{Hub: v.hub}); !did || e != nil {
						t.Fatal(e)
					}
					source := readExpected(autoReceipt.DeliveryID)
					p.process(amrRouteJob{key: amrTurnKey{KeyID: "u_zzc", Session: "kimi", DeliveryID: source.ID, TurnID: id + route}, body: body})
				}
				if route == "send.py" && delta > 0 {
					var large *extshared.BodyTooLargeError
					if !errors.As(err, &large) {
						t.Fatalf("MCP_LEN_OVERSIZE_ZERO_DELIVERY cli err=%v", err)
					}
				} else if route == "send.py" && err != nil {
					t.Fatalf("MCP_LEN_EXACT_UTF8 cli %v", err)
				}
				got, e := seats.Wait(ctx, seat, 0)
				if delta > 0 {
					if e != nil || len(got) != 0 {
						t.Fatalf("MCP_LEN_OVERSIZE_ZERO_DELIVERY %s count=%d err=%v", route, len(got), e)
					}
				} else {
					if e != nil || len(got) != 1 || got[0].Body != body {
						t.Fatalf("MCP_LEN_EXACT_UTF8 %s count=%d err=%v", route, len(got), e)
					}
					if e = seats.Ack(ctx, seat, got[0].ID, int(got[0].Generation)); e != nil {
						t.Fatal(e)
					}
				}
			}
		}
	}

	// Omitted expect_reply persists the false default and the same completed
	// assistant turn produces no automatic return to the external inbox.
	if _, err = seats.Send(ctx, seat, targetFullName, "默认不自动回程", "extseat-sender-e2e-default"); err != nil {
		t.Fatal(err)
	}
	if did, err := seats.ProjectOne(ctx, HubExtseatProjector{Hub: v.hub}); !did || err != nil {
		t.Fatalf("default projection did=%t err=%v", did, err)
	}
	defaultDelivery := readEnvelope(t, ctx, receiver)
	if metaString(defaultDelivery.Meta, metaKeyReplyPolicy) != "" {
		t.Fatalf("default send unexpectedly requests reply: %#v", defaultDelivery.Meta)
	}
	p.process(amrRouteJob{key: amrTurnKey{KeyID: "u_zzc", Session: "kimi", DeliveryID: defaultDelivery.ID, TurnID: "assistant-turn-default"}, body: "默认回信不应送达"})
	noReplyCtx, noReplyCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	noReplies, noReplyErr := seats.Wait(noReplyCtx, seat, 0)
	noReplyCancel()
	if noReplyErr != nil || len(noReplies) != 0 {
		t.Fatalf("expect_reply default unexpectedly returned: %+v err=%v", noReplies, noReplyErr)
	}

	// Seat refusal disables generated return even if reply policy asks for it.
	t.Setenv(extshared.ExternalAutoReplyDisabledEnv, targetFullName)
	blockedReceipt, e := seats.Send(ctx, seat, targetFullName, "sensitive source", "blocked-external-return", true)
	if e != nil {
		t.Fatal(e)
	}
	if did, e := seats.ProjectOne(ctx, HubExtseatProjector{Hub: v.hub}); !did || e != nil {
		t.Fatal(e)
	}
	blockedSource := readExpected(blockedReceipt.DeliveryID)
	if !strings.Contains(blockedSource.Body, extshared.ExternalReplyDisabledWarning) || strings.Contains(blockedSource.Body, extshared.ExternalReplyWarning) {
		t.Fatal("MCP_EXTERNAL_REPLY_WARNING_DISABLED")
	}
	capture := 0
	p.routeEnvelope = func(context.Context, model.Envelope) error { capture++; return nil }
	p.process(amrRouteJob{key: amrTurnKey{KeyID: "u_zzc", Session: "kimi", DeliveryID: blockedSource.ID, TurnID: "blocked-capture"}, body: "private analysis"})
	if capture != 0 {
		t.Fatal("MCP_EXTERNAL_REPLY_SWITCH")
	}
	p.routeEnvelope = nil
	// A forged or alternate automatic-return path must pass the exit gate too.
	forged := model.Envelope{ID: "blocked-bypass", From: sessionAddress("kimi", "u_zzc"), To: seat.FullName + "#u_" + seat.OwnerLC, Body: "private analysis", Meta: map[string]any{metaKeyReplyOriginReturn: true}}
	if e = v.hub.RouteEnvelope(ctx, forged); !errors.Is(e, extshared.ErrNotPermitted) {
		t.Fatalf("MCP_EXTERNAL_REPLY_BYPASS e=%v", e)
	}
	if ds, e := seats.Wait(ctx, seat, 0); e != nil || len(ds) != 0 {
		t.Fatalf("MCP_EXTERNAL_REPLY_SWITCH count=%d e=%v", len(ds), e)
	}
	forged.ID = "explicit-manual"
	forged.Meta = nil
	forged.Body = "explicit reply"
	if e = v.hub.RouteEnvelope(ctx, forged); e != nil {
		t.Fatal(e)
	}
	manual, e := seats.Wait(ctx, seat, 0)
	if e != nil || len(manual) != 1 || manual[0].Body != "explicit reply" {
		t.Fatalf("MCP_EXTERNAL_MANUAL_REPLY_ALLOWED e=%v", e)
	}
	if e = seats.Ack(ctx, seat, manual[0].ID, int(manual[0].Generation)); e != nil {
		t.Fatal(e)
	}
	t.Setenv(extshared.ExternalAutoReplyDisabledEnv, "")

	// Two non-target Agents share an explicit ordinary group with the target.
	// The first has the same owner; the second crosses owners. Both address the
	// external seat by its exact full name and must still be denied by extseat's
	// target identity check before anything is persisted or made visible to Wait.
	peerOwner := "labs"
	peerKey := AgentKeyID(peerOwner)
	peerNow := now
	if _, err = pool.Exec(ctx, access, `INSERT INTO np_user(user_id_lc,user_id,short_name,short_name_lc,display_name,type,state,created_at) VALUES($1,$1,$1,$1,$1,'member','active',$2)`, peerOwner, peerNow); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, access, `INSERT INTO np_agent(id,owner_lc,name,name_lc,origin,state,created_at) VALUES('sender_same','zzc','sameOwner','sameowner','selfhost','active',$1),('sender_cross',$2,'crossOwner','crossowner','selfhost','active',$1)`, peerNow, peerOwner); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, access, `INSERT INTO np_group(code,short_name,creator_lc,leader_agent_id,created_at,updated_at) VALUES('reply-peers','reply-peers','zzc',$1,$2,$2)`, target.ID, peerNow); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, access, `INSERT INTO np_group_member(code,agent_id,created_at) VALUES('reply-peers',$1,$2),('reply-peers','sender_same',$2),('reply-peers','sender_cross',$2)`, target.ID, peerNow); err != nil {
		t.Fatal(err)
	}
	// The temporary group is leader-only, but its membership rows still make
	// both peers observably co-members alongside the target in the real PG.
	if _, err = pool.Exec(ctx, access, `INSERT INTO np_group_member(code,agent_id,created_at) VALUES($1,'sender_same',$2),($1,'sender_cross',$2)`, seatGroup, peerNow); err != nil {
		t.Fatal(err)
	}
	var inboxBefore int
	row, queryErr := pool.QueryRow(ctx, access, `SELECT count(*) FROM np_extseat_inbox WHERE seat_id=$1`, seat.AgentID)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if err = row.Scan(&inboxBefore); err != nil {
		t.Fatal(err)
	}
	for _, source := range []struct{ key, name, id string }{
		{AgentKeyID("zzc"), "sameowner", "same-owner-nontarget"},
		{peerKey, "crossowner", "cross-owner-same-group-nontarget"},
	} {
		env := model.Envelope{ID: "extseat-reply-deny-" + source.id, From: sessionAddress(source.name, source.key), To: seat.FullName + "#u_" + seat.OwnerLC, Body: source.id}
		if err := v.hub.RouteEnvelope(ctx, env); !errors.Is(err, extshared.ErrTargetNotInAG) {
			t.Fatalf("EXTSEAT_REPLY_ONLY_FROM_TARGET source=%s expected ErrTargetNotInAG, got %v", source.id, err)
		}
		waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		got, waitErr := seats.Wait(waitCtx, seat, 0)
		cancel()
		if waitErr != nil || len(got) != 0 {
			t.Fatalf("EXTSEAT_REPLY_ONLY_FROM_TARGET source=%s Wait=%+v err=%v", source.id, got, waitErr)
		}
	}
	// The authorized round trip above already inserted and received one row;
	// denied attempts must leave that count unchanged.
	var inboxRows int
	row, queryErr = pool.QueryRow(ctx, access, `SELECT count(*) FROM np_extseat_inbox WHERE seat_id=$1`, seat.AgentID)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if err = row.Scan(&inboxRows); err != nil || inboxRows != inboxBefore {
		t.Fatalf("EXTSEAT_REPLY_ONLY_FROM_TARGET inbox rows before=%d after=%d err=%v", inboxBefore, inboxRows, err)
	}
}

// The bridge owns only test IPC state; never connects to an online helper.
func sendLengthCLI(t *testing.T, body, to string, route func(string, string) error) error {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "len-cli-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "ipc.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	file := filepath.Join(dir, "body.txt")
	if err = os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			result <- e
			return
		}
		defer c.Close()
		line, e := bufio.NewReader(c).ReadBytes('\n')
		if e != nil {
			result <- e
			return
		}
		var msg struct{ To, Body string }
		if e = json.Unmarshal(line, &msg); e != nil {
			result <- e
			return
		}
		e = route(msg.To, msg.Body)
		result <- e
		_ = json.NewEncoder(c).Encode(map[string]any{"ok": e == nil, "error": fmt.Sprint(e)})
	}()
	script, e := os.ReadFile("../../tools/sessionhelper/sessionhelper/send_dingwei.py")
	if e != nil {
		t.Fatal(e)
	}
	cli := filepath.Join(dir, "send.py")
	if e = os.WriteFile(cli, script, 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("python3", cli, to, "--file", file)
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "SH_") && !strings.HasPrefix(v, "DINGWEI_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "SH_MCP_IPC_SOCK="+socket)
	output, runErr := cmd.CombinedOutput()
	if len(body) > extshared.MessageBodyMaxBytes {
		ln.Close()
		<-result
		if runErr == nil || !strings.Contains(string(output), "body_too_large") {
			t.Fatalf("MCP_LEN_OVERSIZE_ZERO_DELIVERY CLI local rejection %v %s", runErr, output)
		}
		return &extshared.BodyTooLargeError{ActualBytes: len(body), LimitBytes: extshared.MessageBodyMaxBytes}
	}
	e = <-result
	if e == nil {
		// This legacy test bridge reports only IPC ok, deliberately without a
		// durable outbound receipt. The new CLI must stay unknown even though
		// the PG round-trip below independently verifies the external row.
		// Do not fabricate a new-protocol receipt for this external-only path.
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(output), `"outcome": "unknown"`) || strings.Contains(string(output), "sent to=") {
			t.Fatalf("MCP_LEN_EXACT_UTF8 legacy IPC bridge falsely claimed acceptance: %v %s", runErr, output)
		}
	}
	return e
}
