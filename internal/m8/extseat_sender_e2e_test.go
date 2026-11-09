package m8

import (
	"context"
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
	ddl := `CREATE TABLE audit_log(id bigserial PRIMARY KEY,actor text,action text,target text,after_json text,ts timestamptz);` + identity.PGSchemaSQL + string(extseatDDL) + string(groupDDL)
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
	_, seat, _, err := seats.IssueToken(ctx, principal, "asist", targetFullName)
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
	if _, err = seats.Send(ctx, seat, targetFullName, originalBody, "extseat-sender-e2e-001"); err != nil {
		t.Fatal(err)
	}
	if did, err := seats.ProjectOne(ctx, HubExtseatProjector{Hub: v.hub}); !did || err != nil {
		t.Fatalf("PG outbox projection did=%t err=%v", did, err)
	}
	delivered := readEnvelope(t, ctx, receiver)
	wantHeader := externalSeatSenderHeader(seat.FullName, principal.DisplayName, "", targetFullName)
	if delivered.Body != wantHeader+"\n"+originalBody || metaString(delivered.Meta, "origin_kind") != "extseat" || metaString(delivered.Meta, "source_display_name") != seat.FullName || !metaBool(delivered.Meta, "source_external") {
		t.Fatalf("EXTSEAT_SENDER_HEADER_FROM_LOCKED_IDENTITY projected body/meta mismatch: body=%q meta=%#v", delivered.Body, delivered.Meta)
	}

	// MCP addressing carries the full name as the session component plus the authenticated owner's virtual key.
	reply := model.Envelope{ID: "extseat-sender-reply-e2e-001", From: "kimi#u_zzc", To: seat.FullName + "#u_" + seat.OwnerLC, Body: "已确认，收到来自外部 Agent 的消息。"}
	if err := v.hub.RouteEnvelope(ctx, reply); err != nil {
		t.Fatalf("EXTSEAT_REPLY_ROUTE_E2E routeExternalSeat rejected reply: %v", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	replies, err := seats.Wait(waitCtx, seat, 2*time.Second)
	if err != nil || len(replies) != 1 {
		t.Fatalf("EXTSEAT_REPLY_ROUTE_E2E wait replies=%+v err=%v", replies, err)
	}
	if replies[0].From != targetFullName || replies[0].To != seat.FullName || replies[0].Body != reply.Body {
		t.Fatalf("EXTSEAT_REPLY_ROUTE_E2E reply identity/body=%+v want from=%s to=%s", replies[0], targetFullName, seat.FullName)
	}
}
