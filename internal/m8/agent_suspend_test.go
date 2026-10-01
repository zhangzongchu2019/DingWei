package m8

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 组长裁定（托管停用）：停用 ⇒ 在线连接 4401＋credential_suspended；停用中重连 ⇒ 401 credential_suspended；
// 发往它的消息 ⇒「目标已停用」；启用后同一凭据照常连入（不重签）。
func TestHostedSuspendResumeOverWS(t *testing.T) {
	e := newAgentEnv(t) // e.cred＝自备 Agent Opus50TS，用作发送方
	hi, err := e.svc.IssueHostedAgent(e.ctx, "zzc", "Codex1", "g1", "zzc")
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.svc.Poll(e.ctx, hi.Token.ApplicationID, hi.Token.PollToken)
	if err != nil || p.Credential == "" {
		t.Fatalf("poll: %+v %v", p, err)
	}
	hc, st, body := e.dial(p.Credential, "")
	if hc == nil {
		t.Fatalf("hosted dial: %d %s", st, body)
	}
	defer hc.CloseNow()
	closed := make(chan error, 1)
	go func() {
		for {
			if _, _, err := hc.Read(context.Background()); err != nil {
				closed <- err
				return
			}
		}
	}()
	waitSessionOnline(t, e.hub, "u_zzc", "codex1")

	if _, err := e.svc.SuspendHostedAgent(e.ctx, hi.Agent.ID, "zzc"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-closed:
		var ce websocket.CloseError
		if !errorsAsClose(err, &ce) || ce.Code != StatusCredentialRevoked || ce.Reason != "credential_suspended" {
			t.Fatalf("close on suspend: %v, want 4401 credential_suspended", err)
		}
	case <-time.After(time.Second):
		t.Fatal("still connected after suspend returned")
	}
	if _, st, body := e.dial(p.Credential, ""); st != 401 || body != `{"error":"credential_suspended"}` {
		t.Fatalf("reconnect while suspended: %d %s", st, body)
	}

	sender, _, _ := e.dial(e.cred, "")
	defer sender.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, "u_zzc", "opus50ts")
	if err := writeEnvelope(e.ctx, sender, model.Envelope{ID: "m1", From: sessionAddress("opus50ts", "u_zzc"), To: sessionAddress("codex1", "u_zzc"), TS: time.Now().Unix(), Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if b := readEnvelopeOfType(t, e.ctx, sender, model.MetaTypeDeliveryFailure).Body; !strings.Contains(b, "目标已停用") || strings.Contains(b, "已吊销") {
		t.Fatalf("message to suspended target: %q", b)
	}

	if _, err := e.svc.ResumeHostedAgent(e.ctx, hi.Agent.ID, "zzc"); err != nil {
		t.Fatal(err)
	}
	hc2, st, body := e.dial(p.Credential, "")
	if hc2 == nil {
		t.Fatalf("same credential after resume: %d %s", st, body)
	}
	defer hc2.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, "u_zzc", "codex1")
	if !e.hub.AgentOnline(hi.Agent.ID) {
		t.Fatal("AgentOnline=false after resume")
	}
}

func errorsAsClose(err error, ce *websocket.CloseError) bool { return errors.As(err, ce) }
