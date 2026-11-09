package m8

import (
	"context"
	"errors"
	"os"
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
	ddl := `CREATE TABLE audit_log(id bigserial PRIMARY KEY,actor text,action text,target text,after_json text,ts timestamptz);` + identity.PGSchemaSQL + string(extseatDDL) + string(policyDDL) + string(groupDDL)
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
	wantHeader := externalSeatSenderHeader(seat.FullName, principal.DisplayName, "", targetFullName)
	if delivered.Body != wantHeader+"\n"+originalBody || metaString(delivered.Meta, "origin_kind") != "extseat" || metaString(delivered.Meta, "source_display_name") != seat.FullName || !metaBool(delivered.Meta, "source_external") || metaString(delivered.Meta, metaKeyReplyPolicy) != replyPolicyExpectReply {
		t.Fatalf("EXTSEAT_EXPECT_REPLY_POLICY EXTSEAT_SENDER_HEADER_FROM_LOCKED_IDENTITY projected body/meta mismatch: body=%q meta=%#v", delivered.Body, delivered.Meta)
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
