package m8

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func seedNoticeRoute(t *testing.T, h *Hub, key amrTurnKey, sourceType, provenance string, meta map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(model.Envelope{ID: "message-" + key.DeliveryID, From: "ant#" + key.KeyID, To: key.Session + "#" + key.KeyID, Body: "notification", Meta: meta})
	if err := h.Repo.EnqueueSessionDelivery(context.Background(), model.SessionDelivery{ID: key.DeliveryID, KeyID: key.KeyID, SessionName: key.Session, SourceMessageID: "message-" + key.DeliveryID, SourceType: sourceType, Provenance: provenance, Envelope: string(raw)}); err != nil {
		t.Fatal(err)
	}
}
func TestFailureNoticeTrustedContextStopsAgentAndDirectReply(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := amrTurnKey{KeyID: "k1", Session: "bee", DeliveryID: "notice-context", TurnID: "t1"}
	// No kind/no_reply/client marker: only the private repository record is proof.
	seedNoticeRoute(t, h, key, "failure_notice", model.FailureNoticeProvenance, nil)
	p, routed := replyOriginProcessor(h)
	p.stats.queued = 1
	p.process(amrRouteJob{key: key, body: "@ant quoted Agent mention"})
	if p.stats.closedNonCandidate != 1 || p.stats.notificationsSent != 0 || len(*routed) != 0 {
		t.Fatal("notice reached Agent parser or generated feedback")
	}
	p.replyToOrigin(amrRouteJob{key: key, body: "normal completion"})
	if len(*routed) != 0 || p.stats.replyOriginSkipped != 1 {
		t.Fatal("notice returned to origin")
	}
	routeCtx, err := h.failureNoticeRouteContext(ctx, key)
	if err != nil || !failureNoticeRouteStopped(routeCtx) {
		t.Fatal("trusted context not set")
	}
	for _, wrong := range []amrTurnKey{{KeyID: "other", Session: key.Session, DeliveryID: key.DeliveryID}, {KeyID: key.KeyID, Session: "other", DeliveryID: key.DeliveryID}} {
		c, err := h.failureNoticeRouteContext(ctx, wrong)
		if err != nil || failureNoticeRouteStopped(c) {
			t.Fatal("foreign notice claimed as local context")
		}
	}
}
func TestFailureNoticeTrustedContextStopsRealChannelOutbound(t *testing.T) {
	h, keyID, ctx := channelTestHub(t)
	h.Outbound = bus.NewDBQueue(h.Repo, model.DirectionOut)
	key := channelJob(keyID)
	seedNoticeRoute(t, h, key, "failure_notice", model.FailureNoticeProvenance, nil)
	p := testAMRProcessor()
	p.hub = h
	p.stats.queued = 1
	p.process(amrRouteJob{key: key, body: "@飞书#ou_owner quoted channel mention"})
	got, err := h.Repo.ClaimNextMessage(ctx, model.DirectionOut)
	if err != nil || got != nil || p.stats.closedNonCandidate != 1 || p.stats.notificationsSent != 0 {
		t.Fatalf("notice reached channel/feedback: %+v %v", got, err)
	}
}
func TestFailureNoticeClientClaimCannotCreateContextOrSuppressNormalReply(t *testing.T) {
	h, _, ctx := newTestHub(t)
	key := amrTurnKey{KeyID: "k1", Session: "bee", DeliveryID: model.FailureNoticeDeliveryID("forged"), TurnID: "t1"}
	meta := map[string]any{hubFailureNoticeMetaKey: true, "kind": "content", "reply_policy": "expect_reply"}
	seedNoticeRoute(t, h, key, "session_inbound", `{"claimed":"hub_delivery_failure_notice_v1"}`, meta)
	c, err := h.failureNoticeRouteContext(ctx, key)
	if err != nil || failureNoticeRouteStopped(c) {
		t.Fatal("client marker or ID shape became authority")
	}
	p, routed := replyOriginProcessor(h)
	p.replyToOrigin(amrRouteJob{key: key, body: "real business response"})
	if len(*routed) != 1 {
		t.Fatal("forged notice marker suppressed normal business reply")
	}
	wire := sessionClientEnvelope(model.Envelope{Meta: meta})
	if _, ok := wire.Meta[hubFailureNoticeMetaKey]; ok {
		t.Fatal("Hub-private marker crossed client wire")
	}
	if meta[hubFailureNoticeMetaKey] != true || wire.Meta["reply_policy"] != "expect_reply" {
		t.Fatal("stripping mutated original/ordinary fields")
	}
}
func TestFailureNoticeRollbackKindReplyStillStopsAMRAndReturn(t *testing.T) {
	h, _, _ := newTestHub(t)
	key := amrTurnKey{KeyID: "k1", Session: "bee", DeliveryID: "rollback-notice", TurnID: "t1"}
	// Model rollback-era Hub context: no new persisted provenance, existing reply semantics only.
	seedNoticeRoute(t, h, key, "session_inbound", "", map[string]any{"kind": "reply", "reply_policy": "no_reply", "conversation_terminal": true})
	p, routed := replyOriginProcessor(h)
	p.stats.queued = 1
	p.process(amrRouteJob{key: key, body: "@ant quoted response"})
	if len(*routed) != 0 || p.stats.closedNonCandidate != 1 || p.stats.notificationsSent != 0 {
		t.Fatal("rollback reply semantics allowed AMR/return")
	}
}

func TestFailureNoticeIPCBodyStopsBeforeSharedParser(t *testing.T) {
	for _, field := range []string{"source_delivery_id", "in_reply_to"} {
		t.Run(field, func(t *testing.T) {
			h, _, ctx := newTestHub(t)
			p := testAMRProcessor()
			p.hub = h
			replaceAMR(h, p)
			attachAMRNoticeClient(t, h, ctx, "ipc-key", "ipc-source")
			c := h.sessionClients["ipc-key"]["ipc-source"]
			key := amrTurnKey{KeyID: c.keyID, Session: c.sessionName, DeliveryID: "ipc-notice"}
			seedNoticeRoute(t, h, key, "failure_notice", model.FailureNoticeProvenance, nil)
			original := model.Envelope{ID: "outbound", From: "ipc-source#ipc-key", To: "primary#ipc-key", Body: "正文提到 @张宗楚 但不是合法地址", Meta: map[string]any{field: key.DeliveryID}}
			h.routeIPCSendBodyAMR(ctx, c, original)
			if p.stats.notificationsSent != 0 {
				t.Fatal("notice-triggered IPC body reached shared parser")
			}
			original.Meta[field] = "not-a-notice"
			h.routeIPCSendBodyAMR(ctx, c, original)
			if p.stats.notificationsSent != 1 {
				t.Fatal("ordinary IPC body parser/feedback was disabled")
			}
		})
	}
}
