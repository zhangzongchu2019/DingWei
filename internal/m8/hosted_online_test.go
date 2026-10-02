package m8

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// 板 ac33b1cb：托管 Agent 以专属凭据连入、会话登记成功后回调 OnHostedAgentOnline（G4 接 MarkOnline）；
// 自备 Agent 不回调；同一凭据重复连入被拒（409 already_connected）时不再回调。
func TestHostedAgentConnectFiresOnline(t *testing.T) {
	e := newAgentEnv(t)
	got := make(chan string, 4)
	e.hub.OnHostedAgentOnline = func(_ context.Context, id string) { got <- id }
	noCallback := func(why string) {
		t.Helper()
		select {
		case id := <-got:
			t.Fatalf("%s: unexpected OnHostedAgentOnline(%s)", why, id)
		case <-time.After(300 * time.Millisecond):
		}
	}

	self, st, body := e.dial(e.cred, "")
	if self == nil {
		t.Fatalf("self-hosted dial: %d %s", st, body)
	}
	defer self.Close(websocket.StatusNormalClosure, "done")
	reader(self)
	noCallback("self-hosted agent")

	iss, err := e.svc.IssueHostedAgent(e.ctx, "zzc", "Hosted1", "", "g4")
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.svc.Poll(e.ctx, iss.Token.ApplicationID, iss.Token.PollToken)
	if err != nil || p.Credential == "" {
		t.Fatalf("poll: %+v %v", p, err)
	}
	hosted, st, body := e.dial(p.Credential, "")
	if hosted == nil {
		t.Fatalf("hosted dial: %d %s", st, body)
	}
	defer hosted.Close(websocket.StatusNormalClosure, "done")
	reader(hosted)
	select {
	case id := <-got:
		if id != iss.Agent.ID {
			t.Fatalf("callback for %s, want %s", id, iss.Agent.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hosted agent connected but OnHostedAgentOnline never fired")
	}

	if dup, st, _ := e.dial(p.Credential, ""); dup != nil || st != 409 {
		t.Fatalf("duplicate hosted dial should be refused with 409, got %d", st)
	}
	noCallback("refused duplicate connection")
}
