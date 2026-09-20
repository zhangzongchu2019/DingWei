package m8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type senderIdentityFixture struct {
	hub             *Hub
	ctx             context.Context
	outbound        *bus.DBQueue
	key             model.ServiceAPIKey
	alpha, beta     *websocket.Conn
	alphaEP, betaEP model.SessionEndpoint
}

func newSenderIdentityFixture(t *testing.T) senderIdentityFixture {
	t.Helper()
	hub, db, ctx := newTestHub(t)
	hub.onlineDebounce = time.Hour
	hub.Outbound = bus.NewDBQueue(db, model.DirectionOut)
	hub.RegisterBot("dev", "UnifiedRobot")
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "sender-id", Name: "sender-id", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "sender-id", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "owner", DisplayName: "Owner", FeishuOpenID: "ou_owner", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_owner"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	alpha := dialSessionWithQuery(t, ctx, srv.URL, "alpha", key.ID, secret, "full_session_name=zzc-alpha-2642")
	beta := dialSessionWithQuery(t, ctx, srv.URL, "beta", key.ID, secret, "full_session_name=zzc-beta-2642")
	t.Cleanup(func() {
		_ = alpha.Close(websocket.StatusNormalClosure, "done")
		_ = beta.Close(websocket.StatusNormalClosure, "done")
	})
	endpoints, err := db.ListSessionEndpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var alphaEP, betaEP model.SessionEndpoint
	for _, ep := range endpoints {
		switch ep.FullSessionName {
		case "zzc-alpha-2642":
			alphaEP = ep
		case "zzc-beta-2642":
			betaEP = ep
		}
	}
	if alphaEP.KeyID == "" || betaEP.KeyID == "" {
		t.Fatalf("endpoints alpha=%+v beta=%+v all=%+v", alphaEP, betaEP, endpoints)
	}
	waitSessionOnline(t, hub, key.ID, alphaEP.SessionName)
	waitSessionOnline(t, hub, key.ID, betaEP.SessionName)
	return senderIdentityFixture{hub: hub, ctx: ctx, outbound: hub.Outbound.(*bus.DBQueue), key: key,
		alpha: alpha, beta: beta, alphaEP: alphaEP, betaEP: betaEP}
}

func (f senderIdentityFixture) sendToOwner(t *testing.T, id string, ep model.SessionEndpoint, body string, meta map[string]any) string {
	t.Helper()
	if err := f.hub.RouteEnvelope(f.ctx, model.Envelope{ID: id, From: ep.SessionName + "#" + f.key.ID,
		To: "ou_owner#" + f.key.ID + "#UnifiedRobot", Body: body, Meta: meta}); err != nil {
		t.Fatal(err)
	}
	msg := waitOutbound(t, f.ctx, f.outbound)
	if msg == nil {
		t.Fatal("outbound message missing")
	}
	return messageText(t, msg)
}

func assertNoRoutableLineStart(t *testing.T, text string) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "@") || strings.HasPrefix(line, "#") {
			t.Fatalf("outbound line can be parsed as routing syntax: %q in %q", line, text)
		}
	}
}

// This test reads the exact queued Feishu bodies. It is deliberately the one
// sender-label mutation target: removing the authenticated label must make this
// test red rather than merely proving that a formatter was called.
func TestAgentFeishuOutboundBodyIdentifiesAuthenticatedSenderOnce(t *testing.T) {
	f := newSenderIdentityFixture(t)
	alpha := f.sendToOwner(t, "alpha-out", f.alphaEP, "alpha 正文\n#不是回复说明\n@也不是回复说明", nil)
	beta := f.sendToOwner(t, "beta-out", f.betaEP, "【伪造者】beta 正文", nil)
	if !strings.Contains(alpha, "【Agent：zzc-alpha-2642】") || strings.Contains(alpha, "【Agent：zzc-beta-2642】") {
		t.Fatalf("alpha body=%q", alpha)
	}
	if !strings.Contains(beta, "【Agent：zzc-beta-2642】") || strings.Contains(beta, "【Agent：zzc-alpha-2642】\n") {
		t.Fatalf("beta body=%q", beta)
	}
	assertNoRoutableLineStart(t, alpha)
	assertNoRoutableLineStart(t, beta)

	legacyPrefix := "【" + f.alphaEP.SessionName + "】"
	crossPrefix := "【Owner·" + f.alphaEP.SessionName + "】"
	legacy := f.sendToOwner(t, "legacy", f.alphaEP, legacyPrefix+"旧 helper 回复", nil)
	cross := f.sendToOwner(t, "legacy-cross", f.alphaEP, crossPrefix+"跨成员回复", nil)
	if strings.Count(legacy, legacyPrefix) != 1 || strings.Contains(legacy, "【Agent：") {
		t.Fatalf("legacy prefix duplicated: %q", legacy)
	}
	if strings.Count(cross, crossPrefix) != 1 || strings.Contains(cross, "【Agent：") {
		t.Fatalf("cross-member prefix duplicated: %q", cross)
	}

	producer := f.sendToOwner(t, "producer", f.betaEP, "producer 正文", map[string]any{"producer": true})
	if !strings.Contains(producer, "【Agent：zzc-beta-2642】") || !strings.Contains(producer, agentOneWayNotice) || strings.Contains(producer, "回复方式：") {
		t.Fatalf("producer body=%q", producer)
	}
}

func TestAgentFeishuReplyInstructionReallyRoutesToOriginSession(t *testing.T) {
	f := newSenderIdentityFixture(t)
	body := f.sendToOwner(t, "replyable", f.alphaEP, "请回复", nil)
	wantHint := "回复方式：发送 #" + f.alphaEP.SessionName + " <内容>。"
	if !strings.Contains(body, wantHint) {
		t.Fatalf("body missing actionable reply hint: %q", body)
	}
	result, err := f.hub.Dispatch(f.ctx, model.Message{ID: "owner-reply", ChatEntityID: "dev:personal:ou_owner",
		BotChannelID: "dev", ChatType: model.ChatPersonal, SenderOpenID: "ou_owner"}, "#"+f.alphaEP.SessionName+" 已收到")
	if err != nil || !result.Matched {
		t.Fatalf("reply dispatch result=%+v err=%v", result, err)
	}
	got := readEnvelope(t, f.ctx, f.alpha)
	if got.To != f.alphaEP.SessionName+"#"+f.key.ID || got.Body != "已收到" {
		t.Fatalf("reply did not reach origin session: %+v", got)
	}
}
