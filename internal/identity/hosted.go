package identity

import (
	"context"
	"errors"
	"time"
)

// 托管席位的专属凭据签发（V6／V8；业主 2026-10-01 改序「托管先行」，组长令 G3 提供给 G4 的 Hub 内部接口）。
//
// 不走配对码：Hub 内部直接建 origin=hosted 的 Agent，并发一张「一次性入网令牌」＝ 一条已批准申请的
// (application_id, poll_token)。节点代理把它随 profile 带进容器；容器里的客户端用现有的轮询接口
// GET /api/enroll/applications/{application_id}（Bearer poll_token）取回专属凭据 —— 与自备同一条路，
// 凭据在首次取回时才生成、库里只存哈希，不经 Docker 环境变量与镜像层（V6 B7）。

// HostedTokenTTL 一次性入网令牌的有效期：覆盖拉镜像与容器启动；过期由 G4 重签。
const HostedTokenTTL = 2 * time.Hour

// EnrollToken 一次性入网令牌。PollToken 只在此出现一次。
type EnrollToken struct {
	ApplicationID string
	PollToken     string
	ExpiresAt     time.Time
}

// HostedIssue 新建托管席位的结果。
type HostedIssue struct {
	Agent    Agent
	FullName string
	Token    EnrollToken
}

func (s *Service) newEnrollToken(now time.Time) (Application, EnrollToken, error) {
	id, err := s.randHex(16)
	if err != nil {
		return Application{}, EnrollToken{}, err
	}
	tok, err := s.randHex(32)
	if err != nil {
		return Application{}, EnrollToken{}, err
	}
	exp := now.Add(HostedTokenTTL)
	app := Application{ID: id, PollTokenHash: HashSecret(tok), RequestedAt: now, ExpiresAt: exp, State: AppApproved}
	return app, EnrollToken{ApplicationID: id, PollToken: tok, ExpiresAt: exp}, nil
}

// IssueHostedAgent 建托管席位并签发一次性入网令牌。ownerLC＝该席归属用户；by＝操作者（审计用）。
// 配额校验（V7）由调用方在此之前完成。
func (s *Service) IssueHostedAgent(ctx context.Context, ownerLC, name, groupCode, by string) (*HostedIssue, error) {
	return s.issueHosted(ctx, ownerLC, name, GroupChoice{Code: groupCode}, by)
}

// IssueHostedAgentWithNewGroup 同 IssueHostedAgent，但就地新建编组、以新托管 Agent 为组长。
func (s *Service) IssueHostedAgentWithNewGroup(ctx context.Context, ownerLC, name, newGroupName, by string) (*HostedIssue, error) {
	return s.issueHosted(ctx, ownerLC, name, GroupChoice{NewName: newGroupName}, by)
}

func (s *Service) issueHosted(ctx context.Context, ownerLC, name string, group GroupChoice, by string) (*HostedIssue, error) {
	if !ValidAgentName(name) || (s.NameAllowed != nil && !s.NameAllowed(name)) {
		return nil, ErrNameInvalid
	}
	owner := Lower(ownerLC)
	if group.Code == "" && group.NewName == "" {
		return nil, ErrGroupRequired
	}
	u, err := s.Store.GetUser(ctx, owner)
	if errors.Is(err, ErrNotFound) || (err == nil && u.State != UserActive) {
		return nil, ErrUnknownOwner
	}
	if err != nil {
		return nil, err
	}
	now := s.Now()
	agentID, err := s.randHex(16)
	if err != nil {
		return nil, err
	}
	app, tok, err := s.newEnrollToken(now)
	if err != nil {
		return nil, err
	}
	groupCode, newGroup, err := s.resolveGroupChoice(group, owner, "ag_"+agentID, now)
	if err != nil {
		return nil, err
	}
	ag := Agent{ID: "ag_" + agentID, OwnerLC: owner, Name: name, GroupID: groupCode, Origin: OriginHosted, State: AgentActive, CreatedAt: now}
	if err := s.Store.CreateHostedAgent(ctx, ag, app, newGroup); err != nil {
		return nil, err
	}
	s.audit(ctx, by, "agent.hosted_issue", ag.ID, "owner="+owner)
	return &HostedIssue{Agent: ag, FullName: ag.Name + "." + u.ShortName, Token: tok}, nil
}

// ReissueHostedToken 给已有的托管席位重签一次性入网令牌（容器重建、升级）。
// 新凭据被取走的那一刻，旧凭据作废（凭据轮换）；在那之前旧凭据仍可用，旧容器不会被提前踢下线。
func (s *Service) ReissueHostedToken(ctx context.Context, agentID, by string) (*EnrollToken, error) {
	now := s.Now()
	app, tok, err := s.newEnrollToken(now)
	if err != nil {
		return nil, err
	}
	if err := s.Store.AddHostedToken(ctx, agentID, app); err != nil {
		return nil, err
	}
	s.audit(ctx, by, "agent.hosted_token_reissue", agentID, "")
	return &tok, nil
}

// RetireHostedAgent 销毁托管席位时吊销其凭据并断开连接（V8 D5）。重复调用 already=true、不重复审计与断开。
func (s *Service) RetireHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	ag, err := s.Store.GetAgent(ctx, agentID)
	if err != nil {
		return false, err
	}
	if ag.Origin != OriginHosted {
		return false, ErrNotHosted
	}
	already, err := s.Store.RetireAgent(ctx, agentID, by, s.Now())
	if err != nil || already {
		return already, err
	}
	s.audit(ctx, by, "agent.hosted_retire", agentID, "")
	if s.OnRevoked != nil {
		s.OnRevoked(agentID)
	}
	return false, nil
}

// SuspendHostedAgent 停用托管席位（组长裁定 2026-10-01：停用期间凭据拒连）。
// 先暂停凭据、再由调用方停容器；在线连接经 OnSuspended 以 4401（credential_suspended）断开。
// 凭据不作废：ResumeHostedAgent 后原凭据照常可用，客户端收到暂停时不得删本地凭据。重复调用 already=true。
func (s *Service) SuspendHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	already, err := s.Store.SetAgentSuspended(ctx, agentID, true, by, s.Now())
	if err != nil || already {
		return already, err
	}
	s.audit(ctx, by, "agent.hosted_suspend", agentID, "")
	if s.OnSuspended != nil {
		s.OnSuspended(agentID)
	}
	return false, nil
}

// ResumeHostedAgent 启用：恢复凭据（不重新签发），再由调用方起容器。重复调用 already=true。
func (s *Service) ResumeHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	already, err := s.Store.SetAgentSuspended(ctx, agentID, false, by, s.Now())
	if err != nil || already {
		return already, err
	}
	s.audit(ctx, by, "agent.hosted_resume", agentID, "")
	return false, nil
}
