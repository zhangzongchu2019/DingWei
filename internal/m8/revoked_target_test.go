package m8

import (
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// V2 A5：发往已吊销 Agent 的消息 ⇒ 发送方收到「目标已吊销」；从未存在、在用但离线的目标不得被说成已吊销。
func TestDeliveryToRevokedTargetSaysRevoked(t *testing.T) {
	e := newAgentEnv(t) // 发送方 Opus50TS（e.cred）
	_, kimiID := e.enroll("dev-k", "Kimi")
	_, _ = e.enroll("dev-q", "Qwen") // 在用但从不连入 ⇒ 离线
	if _, err := e.svc.Revoke(e.ctx, "zzc", kimiID); err != nil {
		t.Fatal(err)
	}
	conn, st, body := e.dial(e.cred, "")
	if conn == nil {
		t.Fatalf("dial: %d %s", st, body)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	from := sessionAddress("opus50ts", "u_zzc")
	send := func(to, text string) string {
		t.Helper()
		if err := writeEnvelope(e.ctx, conn, model.Envelope{ID: "m-" + to, From: from, To: to, TS: time.Now().Unix(), Body: text}); err != nil {
			t.Fatal(err)
		}
		return readEnvelopeOfType(t, e.ctx, conn, model.MetaTypeDeliveryFailure).Body
	}
	if b := send(sessionAddress("kimi", "u_zzc"), "hello kimi"); !strings.Contains(b, "目标已吊销") || !strings.Contains(b, "Kimi") {
		t.Fatalf("revoked target: %q", b)
	}
	if b := send(sessionAddress("ghost", "u_zzc"), "hello ghost"); strings.Contains(b, "已吊销") {
		t.Fatalf("never-existed target reported as revoked: %q", b)
	}
	if b := send(sessionAddress("qwen", "u_zzc"), "hello qwen"); strings.Contains(b, "已吊销") {
		t.Fatalf("active-but-offline target reported as revoked: %q", b)
	}
}
