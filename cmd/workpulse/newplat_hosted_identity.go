package main

import (
	"context"
	"errors"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// 身份侧两个适配器（组长台账 88928305：归 G3）：用 G3 的 identity.Service 实现 G4 编排器要的
// hosted.IdentityIssuer 与 hosted.IdentitySuspender。
//
// 本包不 import internal/hosted（G4 尚未合入 main；组长令各自先 merge main 再往上接、不互合分支）：
// G4 的 EnrollToken 与 IssuedAgent 都是普通结构体，这里用「底层结构体相同」的类型约束承接，
// G4 装配处实例化即可满足它的接口：
//
//	idp := newHostedIdentity[hosted.EnrollToken, hosted.IssuedAgent](svc, hosted.ErrGroupNotFound, hosted.ErrGroupClosed)
//
// 两边结构体字段一旦对不上，这一行编译不过 —— 漂移在编译期暴露，而不是跑起来才发现字段丢了。

// hostedTokenShape 与 identity.EnrollToken 底层相同的结构体（G4 的 hosted.EnrollToken）。
type hostedTokenShape interface {
	~struct {
		ApplicationID string
		PollToken     string
		ExpiresAt     time.Time
	}
}

// hostedAgentShape G4 的 hosted.IssuedAgent。
type hostedAgentShape interface {
	~struct {
		ID       string
		FullName string
	}
}

type hostedIdentity[Tok hostedTokenShape, Ag hostedAgentShape] struct {
	svc *identity.Service
	// G4 的编组两码哨兵（契约机器码 group_not_found／group_closed）。
	errGroupNotFound, errGroupClosed error
}

func newHostedIdentity[Tok hostedTokenShape, Ag hostedAgentShape](svc *identity.Service, errGroupNotFound, errGroupClosed error) *hostedIdentity[Tok, Ag] {
	return &hostedIdentity[Tok, Ag]{svc: svc, errGroupNotFound: errGroupNotFound, errGroupClosed: errGroupClosed}
}

// IssueHostedAgent 建托管 Agent 并签发一次性入网令牌。groupCode 空＝不入组（编组可缺省）；
// 给了则须「本人可用」且未关闭添加（G3 41feec50），否则返回 G4 的哨兵本身。其余错误原样透传。
func (h *hostedIdentity[Tok, Ag]) IssueHostedAgent(ctx context.Context, ownerLC, name, groupCode, by string) (Ag, Tok, error) {
	var noAg Ag
	var noTok Tok
	iss, err := h.svc.IssueHostedAgent(ctx, ownerLC, name, groupCode, by)
	if err != nil {
		return noAg, noTok, h.groupErr(err)
	}
	return Ag(struct{ ID, FullName string }{ID: iss.Agent.ID, FullName: iss.FullName}), Tok(iss.Token), nil
}

// IssueHostedAgentWithNewGroup 同上，但就地新建编组（同一事务、新 Agent 为组长；R-V2-0102-GROUP-FIXES A）。
func (h *hostedIdentity[Tok, Ag]) IssueHostedAgentWithNewGroup(ctx context.Context, ownerLC, name, newGroupName, by string) (Ag, Tok, error) {
	var noAg Ag
	var noTok Tok
	iss, err := h.svc.IssueHostedAgentWithNewGroup(ctx, ownerLC, name, newGroupName, by)
	if err != nil {
		return noAg, noTok, h.groupErr(err)
	}
	return Ag(struct{ ID, FullName string }{ID: iss.Agent.ID, FullName: iss.FullName}), Tok(iss.Token), nil
}

// AbortHostedAgent 没建成的席位回收（吊销＋删随本次新建、无其他成员的编组）。
func (h *hostedIdentity[Tok, Ag]) AbortHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	return h.svc.AbortHostedIssue(ctx, agentID, by)
}

func (h *hostedIdentity[Tok, Ag]) groupErr(err error) error {
	switch {
	case errors.Is(err, identity.ErrGroupNotFound):
		return h.errGroupNotFound
	case errors.Is(err, identity.ErrGroupClosed):
		return h.errGroupClosed
	}
	return err
}

func (h *hostedIdentity[Tok, Ag]) RetireHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	return h.svc.RetireHostedAgent(ctx, agentID, by)
}

// ListHostedAgentIDsByOwner 配额「已用」统计用：名下未吊销（active 与 suspended）的托管 Agent。
// 停用中的席位照样计配额（契约：停用期间仍计配额），所以 suspended 必须在内。
func (h *hostedIdentity[Tok, Ag]) ListHostedAgentIDsByOwner(ctx context.Context, ownerLC string) ([]string, error) {
	agents, err := h.svc.ListAgents(ctx, ownerLC)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, ag := range agents {
		if ag.Origin == identity.OriginHosted {
			ids = append(ids, ag.ID)
		}
	}
	return ids, nil
}

func (h *hostedIdentity[Tok, Ag]) SuspendHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	return h.svc.SuspendHostedAgent(ctx, agentID, by)
}

func (h *hostedIdentity[Tok, Ag]) ResumeHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	return h.svc.ResumeHostedAgent(ctx, agentID, by)
}

func (h *hostedIdentity[Tok, Ag]) ReissueHostedToken(ctx context.Context, agentID, by string) (Tok, error) {
	tok, err := h.svc.ReissueHostedToken(ctx, agentID, by)
	if err != nil {
		var noTok Tok
		return noTok, err
	}
	return Tok(*tok), nil
}
