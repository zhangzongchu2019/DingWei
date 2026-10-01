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

// 升级（G4 调用顺序：Reissue ⇒ 新容器取走凭据 ⇒ 撤旧容器）：新凭据被取走那一刻旧凭据作废，
// 旧容器那条仍连着的连接也必须同时断开（4401）；否则新容器连入会撞「同一 Agent 已在线」的 409，
// 且一条拿作废凭据建立的连接会一直挂到旧容器被撤为止。
func TestHostedReissueTakeKicksOldConnection(t *testing.T) {
	e := newAgentEnv(t)
	hi, err := e.svc.IssueHostedAgent(e.ctx, "zzc", "Codex2", "g1", "zzc")
	if err != nil {
		t.Fatal(err)
	}
	p1, err := e.svc.Poll(e.ctx, hi.Token.ApplicationID, hi.Token.PollToken)
	if err != nil || p1.Credential == "" {
		t.Fatalf("poll 1: %+v %v", p1, err)
	}
	oldc, st, body := e.dial(p1.Credential, "")
	if oldc == nil {
		t.Fatalf("old dial: %d %s", st, body)
	}
	defer oldc.CloseNow()
	closed := make(chan error, 1)
	go func() {
		for {
			if _, _, err := oldc.Read(context.Background()); err != nil {
				closed <- err
				return
			}
		}
	}()
	waitSessionOnline(t, e.hub, "u_zzc", "codex2")

	tok, err := e.svc.ReissueHostedToken(e.ctx, hi.Agent.ID, "g4")
	if err != nil {
		t.Fatal(err)
	}
	// 只重签、新容器还没取走：旧连接照常在线（升级失败可回退）
	select {
	case err := <-closed:
		t.Fatalf("old connection closed on reissue alone: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	p2, err := e.svc.Poll(e.ctx, tok.ApplicationID, tok.PollToken)
	if err != nil || p2.Credential == "" || p2.Credential == p1.Credential {
		t.Fatalf("poll 2: %+v %v", p2, err)
	}
	select {
	case err := <-closed:
		var ce websocket.CloseError
		if !errorsAsClose(err, &ce) || ce.Code != StatusCredentialRevoked || ce.Reason != "credential_revoked" {
			t.Fatalf("old connection close: %v, want 4401 credential_revoked", err)
		}
	case <-time.After(time.Second):
		t.Fatal("old connection still up after the new credential was taken")
	}
	newc, st, body := e.dial(p2.Credential, "")
	if newc == nil {
		t.Fatalf("new container dial: %d %s", st, body)
	}
	defer newc.Close(websocket.StatusNormalClosure, "done")
	waitSessionOnline(t, e.hub, "u_zzc", "codex2")
	if _, st, body := e.dial(p1.Credential, ""); st != 401 || body != `{"error":"credential_invalid"}` {
		t.Fatalf("old credential after rotation: %d %s", st, body)
	}
}
