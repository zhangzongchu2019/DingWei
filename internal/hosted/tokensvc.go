package hosted

// T-G4-22 · 订阅类令牌件链路（台账 4308bec0；账号池设计件第一节）。
//
// 既有件复用：令牌件入池＝PoolService.Register（kind=token_file——管理者在有浏览器处
// 登录一次后把令牌文件内容登记进来，加密入库）；创建时写入容器＝席位经 V4 通道自取
// （DecryptForSeat，T-G4-32）；一账号一时刻只绑一席＝BindAccount 的 CAS（T-G4-21）。
//
// 本件补的一环：🔴 令牌续期回写——令牌会在容器内自动续期并改写文件，续期后的令牌
// 以容器内为准 ⇒ 席位周期上送最新令牌件，本层以 active 团队密钥重新加密回写池内副本
// （记审计）；解绑/换席前编排层先要求一次回写（防副本滞后，见 PrepareUnbind）。

import (
	"context"
	"errors"
)

var ErrNoBoundAccount = errors.New("hosted: 该席位无绑定账号")

// WriteBackRenewedToken 席位上送续期后的令牌件：重新加密回写池副本（以容器内为准），审计一条。
// by 传席位身份（审计可溯到席）。
func (p *PoolService) WriteBackRenewedToken(ctx context.Context, agentID, content, by string) error {
	if content == "" {
		return nil // 空内容不回写（席位侧异常不上送坏件）
	}
	acctID, err := p.boundAccountOf(ctx, agentID)
	if err != nil {
		return err
	}
	enc, kv, err := p.st.EncryptAccountSecret(ctx, p.master, []byte(content))
	if err != nil {
		return err
	}
	if err := p.st.UpdateAccountSecret(ctx, acctID, enc, kv, last4Of(content)); err != nil {
		return err
	}
	return p.st.AddAudit(ctx, by, "account.token_writeback", acctID, jsonRaw(`{"agent":"`+agentID+`"}`))
}

// PrepareUnbind 解绑/换席前置：返回席位绑定的账号 ID（编排层据此先向席位拉一次最新令牌件
// 并调 WriteBackRenewedToken，再解绑——防池内副本滞后于容器内续期）。
func (p *PoolService) PrepareUnbind(ctx context.Context, agentID string) (string, error) {
	return p.boundAccountOf(ctx, agentID)
}

func (p *PoolService) boundAccountOf(ctx context.Context, agentID string) (string, error) {
	var id string
	err := p.st.db.QueryRowContext(ctx, `
SELECT id FROM g4_account_pool WHERE bound_agent_id=$1 AND state='bound'`, agentID).Scan(&id)
	if err != nil {
		return "", ErrNoBoundAccount
	}
	return id, nil
}
