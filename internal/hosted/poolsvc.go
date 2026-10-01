package hosted

// T-G4-20 · 团队账号池服务层：登记（密钥只入一次不回显）· 换密 · 停用 · 按用户授权 ·
// 绑定状态查询 · 绑定/解绑 · 解密下发（最小暴露：只给绑定的那一席，每次记审计——T-G4-03）。
//
// 角色纪律：本层所有方法都要求 by 是管理用户（IsAdmin 由调用方以 httpapi.N3.Actor 的角色判定后传入——
// 普通用户调用 ⇒ ErrForbiddenRole，与 V7-C6 同源的反向格）。
// 明文纪律：任何返回值都不含凭据明文；后四位在登记/换密时算好落库，展示永不触发解密。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

var ErrForbiddenRole = errors.New("hosted: 需要管理用户")

type PoolService struct {
	st     *Store
	master []byte
}

func NewPoolService(st *Store, masterKeyPath string) (*PoolService, error) {
	mk, err := LoadMasterKey(masterKeyPath)
	if err != nil {
		return nil, err
	}
	return &PoolService{st: st, master: mk}, nil
}

func newID(prefix string) (string, error) {
	raw := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw), nil
}

func last4Of(secret string) string {
	if len(secret) <= 4 {
		return secret
	}
	return secret[len(secret)-4:]
}

func admin(by string, isAdmin bool) error {
	if !isAdmin {
		return ErrForbiddenRole
	}
	if by == "" {
		return fmt.Errorf("%w: actor 为空", ErrParams)
	}
	return nil
}

// Register 登记账号：密钥只在入参出现一次，加密入库，应答与库里都看不到明文。
func (p *PoolService) Register(ctx context.Context, by string, isAdmin bool, name, kind, cli, llm, secret string) (Account, error) {
	if err := admin(by, isAdmin); err != nil {
		return Account{}, err
	}
	if name == "" || (kind != "api_key" && kind != "token_file") || cli == "" || llm == "" || secret == "" {
		return Account{}, fmt.Errorf("%w: name/kind/cli/llm/secret 均必填", ErrParams)
	}
	if _, err := p.st.EnsureTeamKey(ctx, p.master, by); err != nil {
		return Account{}, err
	}
	enc, kv, err := p.st.EncryptAccountSecret(ctx, p.master, []byte(secret))
	if err != nil {
		return Account{}, err
	}
	id, err := newID("acct_")
	if err != nil {
		return Account{}, err
	}
	a := Account{ID: id, Name: name, Kind: kind, CLI: cli, LLM: llm, KeyVersion: kv, Last4: last4Of(secret)}
	if err := p.st.CreateAccount(ctx, a, enc, by); err != nil {
		return Account{}, err
	}
	if err := p.st.AddAudit(ctx, by, "account.register", id, nil); err != nil {
		return Account{}, err
	}
	return a, nil
}

// UpdateSecret 换密：新值加密入库（active 密钥版本）；绑定席位经 profile 更新或重启生效（设计件）。
func (p *PoolService) UpdateSecret(ctx context.Context, by string, isAdmin bool, accountID, secret string) error {
	if err := admin(by, isAdmin); err != nil {
		return err
	}
	if secret == "" {
		return fmt.Errorf("%w: secret 必填", ErrParams)
	}
	enc, kv, err := p.st.EncryptAccountSecret(ctx, p.master, []byte(secret))
	if err != nil {
		return err
	}
	if err := p.st.UpdateAccountSecret(ctx, accountID, enc, kv, last4Of(secret)); err != nil {
		return err
	}
	return p.st.AddAudit(ctx, by, "account.secret_update", accountID, nil)
}

// Disable 停用账号（绑定中的须先解绑——store 层约束）。
func (p *PoolService) Disable(ctx context.Context, by string, isAdmin bool, accountID string) error {
	if err := admin(by, isAdmin); err != nil {
		return err
	}
	if err := p.st.SetAccountState(ctx, accountID, "disabled"); err != nil {
		return err
	}
	return p.st.AddAudit(ctx, by, "account.disable", accountID, nil)
}

// Grant 按用户授权（组长定 da622016）。
func (p *PoolService) Grant(ctx context.Context, by string, isAdmin bool, accountID, userLC string) error {
	if err := admin(by, isAdmin); err != nil {
		return err
	}
	if userLC == "" {
		return fmt.Errorf("%w: user_lc 必填", ErrParams)
	}
	if err := p.st.GrantAccount(ctx, accountID, userLC, by); err != nil {
		return err
	}
	return p.st.AddAudit(ctx, by, "account.grant", accountID, jsonRaw(`{"user":"`+userLC+`"}`))
}

// Bind 绑定到席位（一席一账号——并发安全在 store 层）。
func (p *PoolService) Bind(ctx context.Context, by string, isAdmin bool, accountID, agentID string) error {
	if err := admin(by, isAdmin); err != nil {
		return err
	}
	if err := p.st.BindAccount(ctx, accountID, agentID); err != nil {
		return err
	}
	return p.st.AddAudit(ctx, by, "account.bind", accountID, jsonRaw(`{"agent":"`+agentID+`"}`))
}

// Unbind 解绑回池（销毁自动回池路径也走这里）。
func (p *PoolService) Unbind(ctx context.Context, by string, isAdmin bool, accountID string) error {
	if err := admin(by, isAdmin); err != nil {
		return err
	}
	if err := p.st.UnbindAccount(ctx, accountID); err != nil {
		return err
	}
	return p.st.AddAudit(ctx, by, "account.unbind", accountID, nil)
}

// DecryptForSeat 解密下发：只给**绑定的那一席**（最小暴露）；每次解密记审计（T-G4-03）。
// 返回的明文只允许走上"给绑定席位下发 profile"的通道（设计件第二节）。
func (p *PoolService) DecryptForSeat(ctx context.Context, by string, accountID, agentID string) (string, error) {
	a, err := p.st.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	if !a.BoundAgentID.Valid || a.BoundAgentID.String != agentID {
		return "", ErrAccountBound // 不是该席绑定的账号——最小暴露边界
	}
	plain, err := p.st.DecryptAccountSecret(ctx, p.master, accountID)
	if err != nil {
		return "", err
	}
	if err := p.st.AddAudit(ctx, by, "account.decrypt_issue", accountID, jsonRaw(`{"agent":"`+agentID+`"}`)); err != nil {
		return "", err
	}
	return string(plain), nil
}

// ListAdmin 管理视图：全部账号的元数据（无明文）。
func (p *PoolService) ListAdmin(ctx context.Context, by string, isAdmin bool) ([]Account, error) {
	if err := admin(by, isAdmin); err != nil {
		return nil, err
	}
	rows, err := p.st.db.QueryContext(ctx, `
SELECT id, name, kind, cli, llm, key_version, last4, state, bound_agent_id
FROM g4_account_pool ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Name, &a.Kind, &a.CLI, &a.LLM, &a.KeyVersion, &a.Last4, &a.State, &a.BoundAgentID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func jsonRaw(s string) []byte { return []byte(s) }

var _ = time.Now // 预留：令牌续期回写（T-G4-22）的时间戳生成
