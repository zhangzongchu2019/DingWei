package hosted

// T-G4-22 · 订阅类令牌件链路（台账 4308bec0；账号池设计件第一节）。
//
// 既有件复用：令牌件入池＝PoolService.Register（kind=token_file——管理者在有浏览器处
// 登录一次后把令牌文件内容登记进来，加密入库）；创建时写入容器＝席位经 V4 通道自取
// （DecryptForSeat，T-G4-32）；0.2.1.101 起同一账号可被属主本人多个席位共用（不再 CAS 独占）。
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
	// 0.2.1.101：占用以席位行 account_ref 为准（共用后一号可被多席引用，bound_agent_id 放不下）。
	// 🔴 本函数与 WriteBackRenewedToken／PrepareUnbind 目前**无生产调用点**（回写未接线）；接线后共用账号
	// 为「最后上送者胜」（审计带上送席 agent_id）。
	id, err := p.st.SeatAccountOf(ctx, agentID)
	if err != nil {
		return "", ErrNoBoundAccount
	}
	return id, nil
}
