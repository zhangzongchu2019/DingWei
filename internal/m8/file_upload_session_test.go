package m8

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// R-V2-AGENT-FILE-UPLOAD：真 Hub 会话链路（不带 target_bot 的普通 helper 连接）上取上传票，
// 回信须带票所绑的规范会话名；该名与连接时请求的本地名不同（Hub 规范化为 <属主>-<本地名>-<key 尾>），
// 夹具故意取互不相同的值，以免「回了本地名」或「回了属主」也能通过。
func TestAgentUploadTicketOverSessionWSCarriesBoundSession(t *testing.T) {
	hub, db, ctx := newTestHub(t)
	issuer := &recordingFileIssuer{}
	hub.fileIssuer = issuer
	if err := db.UpsertMember(ctx, model.Member{OwnerKey: "u1", DisplayName: "UserOne", FeishuOpenID: "ou_u1", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := hub.UpsertService(ctx, model.RegisteredService{ID: "svc1", Name: "svc1", DeliveryType: "ws", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secret, key, err := hub.IssueAPIKey(ctx, "svc1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.BindAccount(ctx, key.ID, "dev:personal:ou_u1"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/session/{sessionName}", hub.HandleSessionWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	bound := "u1-uploader-" + keyTail(key.ID)
	conn := dialSession(t, ctx, srv.URL, "uploader", key.ID, secret)
	defer conn.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, hub, key.ID, bound)

	if err := writeEnvelope(ctx, conn, model.Envelope{Meta: map[string]any{
		"type": fileCapabilityRequestType, "request_id": "req-upload-1", "operation": "upload", "max_bytes": 4096,
		"session_name": "forged-session", "owner_key": "forged-owner",
	}}); err != nil {
		t.Fatal(err)
	}
	got := readEnvelopeOfType(t, ctx, conn, fileCapabilityResponseType)
	if !metaBool(got.Meta, "ok") || metaString(got.Meta, "request_id") != "req-upload-1" || metaString(got.Meta, "token") != "opaque-ticket" {
		t.Fatalf("upload ticket response=%+v", got.Meta)
	}
	if session := metaString(got.Meta, "session"); session != bound {
		t.Fatalf("response session=%q want bound session %q", session, bound)
	}
	if issuer.calls != 1 || issuer.req.SessionName != bound || issuer.req.OwnerKey != "u1" || issuer.req.MaxBytes != 4096 {
		t.Fatalf("issuer request=%+v calls=%d", issuer.req, issuer.calls)
	}
}
