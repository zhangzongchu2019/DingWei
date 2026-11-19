package m8

import (
	"context"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type recordingAgentFeishuCopies struct {
	calls  int
	owner  string
	agent  string
	enable bool
}

func (r *recordingAgentFeishuCopies) SetPreference(_ context.Context, owner, agent string, enabled bool) error {
	r.calls++
	r.owner, r.agent, r.enable = owner, agent, enabled
	return nil
}
func (*recordingAgentFeishuCopies) EnqueueAgent(context.Context, extseat.AgentFeishuCopyEvent) error {
	return nil
}

func TestAgentFeishuCopyCommandRequiresVerifiedIdentityAndDisambiguates(t *testing.T) {
	e := newAgentEnv(t)
	bindFeishuForAgentEnv(t, e, "ou_alice")
	copyService := &recordingAgentFeishuCopies{}
	e.hub.AgentFeishuCopies = copyService
	msg := feishuInboundMessage("copy-command", "Agent抄送 关")
	untrusted := msg
	untrusted.ID = "copy-command-untrusted"
	untrusted.IngressProvenance = model.IngressUntrusted
	_, handled, err := e.hub.dispatchAgentFeishuCopyCommand(e.ctx, untrusted, "Agent抄送 关")
	if err != nil || !handled || copyService.calls != 0 {
		t.Fatal("unverified sender changed persistent Agent preference")
	}
	result, err := e.hub.Dispatch(e.ctx, msg, "Agent抄送 关")
	if err != nil || !result.Matched || copyService.calls != 1 || copyService.enable || copyService.agent != e.agID || copyService.owner != "zzc" {
		t.Fatalf("single-agent command result=%+v service=%+v err=%v", result, copyService, err)
	}
	_, secondID := e.enroll("dev-2", "Kimi")
	copyService.calls = 0
	result, err = e.hub.Dispatch(e.ctx, feishuInboundMessage("copy-command-ambiguous", "Agent抄送 开"), "Agent抄送 开")
	if err != nil || !result.Matched || copyService.calls != 0 || !strings.Contains(result.Reply, "Kimi") || !strings.Contains(result.Reply, "Opus50TS") {
		t.Fatalf("ambiguous omitted-name command changed setting: result=%+v calls=%d err=%v", result, copyService.calls, err)
	}
	second, err := e.svc.Store.GetAgent(e.ctx, secondID)
	if err != nil {
		t.Fatal(err)
	}
	fullName := e.svc.FullName(e.ctx, second)
	result, err = e.hub.Dispatch(e.ctx, feishuInboundMessage("copy-command-explicit", "Agent抄送 开 "+fullName), "Agent抄送 开 "+fullName)
	if err != nil || !result.Matched || copyService.calls != 1 || !copyService.enable || copyService.agent != secondID {
		t.Fatalf("explicit full-name command result=%+v service=%+v err=%v", result, copyService, err)
	}
	copyService.calls = 0
	result, err = e.hub.Dispatch(e.ctx, feishuInboundMessage("copy-command-body", "Agent抄送正文是普通内容"), "Agent抄送正文是普通内容")
	if err != nil || copyService.calls != 0 || result.Reply == "已开启 Agent 抄送：" || result.Reply == "已关闭 Agent 抄送：" {
		t.Fatalf("ordinary message was parsed as a setting command: result=%+v calls=%d err=%v", result, copyService.calls, err)
	}
	group := msg
	group.ChatType = model.ChatGroup
	result, err = e.hub.Dispatch(e.ctx, group, "Agent抄送 开")
	if err != nil || !result.Matched || copyService.calls != 0 {
		t.Fatalf("group command changed preference: result=%+v calls=%d err=%v", result, copyService.calls, err)
	}
}
