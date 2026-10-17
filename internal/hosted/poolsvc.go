package hosted

// T-G4-20 · LLM 账号池服务层。0.2.1.101（R-V2-ACCOUNT-PER-USER）：按人隔离（owner_lc）＋本人多席共用＋
// 属主本人登记／换密／停用／启用；管理用户只看元数据、停用、启用（不见密钥、不能换密）；
// 解密下发只给引用本号且同属主的席位（每次记审计——T-G4-03）。
//
// 角色纪律：*Owned 方法按属主判（他人一律 ErrNotFound）；管理方法要求 by 是管理用户（IsAdmin 由调用方判定后传入——
// 普通用户调用 ⇒ ErrForbiddenRole）。
// 明文纪律：任何返回值都不含凭据明文；后四位在登记/换密时算好落库，展示永不触发解密。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
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

// 短于 MinAPIKeyBytes 的不落（纵深：登记口已拒短 api_key；否则「后四位」会是整串密钥）。
func last4Of(secret string) string {
	if len(secret) < MinAPIKeyBytes {
		return ""
	}
	return secret[len(secret)-4:]
}

// displayLast4 按显示口径（组长 M1 返修第 16 条④）：api_key 类照旧后四位；
// token_file 类不落内容尾部（JSON 结构尾无识别意义）——返回空串，页面显示「已入池＋登记时间」。
func displayLast4(kind, secret string) string {
	if kind == "token_file" {
		return ""
	}
	return last4Of(secret)
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

// MaxSecretBytes 凭据件上限（token_file 为 JSON 文件内容；api_key 为字符串）。
const MaxSecretBytes = 64 << 10

var ErrSecretTooLarge = errors.New("hosted: secret too large")

// validateSecret 登记／换密共用的通用闸：非空、不超限、无 NUL；token_file 须为合法 JSON。错误文本不含输入内容。
// MinAPIKeyBytes api_key 最短长度（复核 tosonnet55tc2k 第 1 批提示 3：防「后四位」即整串）。
const MinAPIKeyBytes = 8

func validateSecret(kind, secret string) error {
	if secret == "" {
		return fmt.Errorf("%w: secret 必填", ErrParams)
	}
	if len(secret) > MaxSecretBytes {
		return ErrSecretTooLarge
	}
	if strings.ContainsRune(secret, 0) {
		return fmt.Errorf("%w: secret 含非法字符", ErrParams)
	}
	if kind == "api_key" && len(secret) < MinAPIKeyBytes {
		return fmt.Errorf("%w: api_key 过短", ErrParams)
	}
	if kind == "token_file" && !json.Valid([]byte(secret)) {
		return fmt.Errorf("%w: token_file 须为 JSON", ErrParams)
	}
	return nil
}

// validateCredential keeps provider-specific token-file ABIs at the pool boundary.
// Claude's initial hosted ABI is deliberately narrow: an opaque JSON object only.
// It does not claim compatibility with an unapproved vendor login-file schema and
// never includes input bytes in its error.  Codex keeps its existing JSON ABI.
func validateCredential(cli, kind, secret string) error {
	if err := validateSecret(kind, secret); err != nil {
		return err
	}
	if cli != "claude" || kind != "token_file" {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(secret), &obj); err != nil || obj == nil {
		return fmt.Errorf("%w: Claude token_file 须为 JSON 对象", ErrParams)
	}
	return nil
}

// RegisterOwned 属主本人登记（POST /api/console/llm-accounts）：归属＝owner；密钥只在入参出现一次，
// 加密入库（团队数据密钥——平台持有者可解密，业主 10-06 新裁），应答与库里都看不到明文。
// 每人上限 MaxAccountsPerOwner（停用的也计入）；本人内同名 ⇒ ErrAccountNameTaken。
// (cli,llm,kind) 是否在联动表内由调用方（编排层 combos）先判。
func (p *PoolService) RegisterOwned(ctx context.Context, owner, name, kind, cli, llm, secret string) (Account, error) {
	return p.registerOwned(ctx, owner, name, kind, cli, llm, secret, "")
}

// RegisterOwnedLogin 账号登录入池：同 RegisterOwned，邮箱随同一条 INSERT 落库（不留「名字＝邮箱、email 为空」的孤儿行）。
func (p *PoolService) RegisterOwnedLogin(ctx context.Context, owner, email, llm, cred string) (Account, error) {
	return p.registerOwned(ctx, owner, email, loginCredKind, loginCLI, llm, cred, email)
}

// AccountNameMaxLen 账号名（本人内唯一）长度上限；账号登录以邮箱作名，发起时即按它预查。
const AccountNameMaxLen = 64

func (p *PoolService) registerOwned(ctx context.Context, owner, name, kind, cli, llm, secret, email string) (Account, error) {
	if owner == "" {
		return Account{}, fmt.Errorf("%w: owner 为空", ErrParams)
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > AccountNameMaxLen || (kind != "api_key" && kind != "token_file") || cli == "" || llm == "" {
		return Account{}, fmt.Errorf("%w: name/kind/cli/llm 均必填", ErrParams)
	}
	if err := validateCredential(cli, kind, secret); err != nil {
		return Account{}, err
	}
	n, err := p.st.CountOwnedAccounts(ctx, owner)
	if err != nil {
		return Account{}, err
	}
	if n >= MaxAccountsPerOwner {
		return Account{}, ErrAccountLimit
	}
	if _, err := p.st.EnsureTeamKey(ctx, p.master, owner); err != nil {
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
	a := Account{ID: id, Name: name, Kind: kind, CLI: cli, LLM: llm, KeyVersion: kv, Last4: displayLast4(kind, secret), OwnerLC: owner, State: "available", Email: email}
	if err := p.st.CreateAccount(ctx, a, enc, owner); err != nil {
		return Account{}, err
	}
	if err := p.st.AddAudit(ctx, owner, "account.register", id, nil); err != nil {
		return Account{}, err
	}
	return a, nil
}

// UpdateSecretOwned 属主本人换密。返回本号当前在用席位数（UI 提示「有 N 个 Agent 在用，可逐个刷新」）。
// 🔴 换密不触达在跑席位：在跑席位须逐个「刷新凭据」（Lifecycle.RefreshCred）才生效。管理员无换密入口。
func (p *PoolService) UpdateSecretOwned(ctx context.Context, owner, accountID, secret string) (int, error) {
	a, err := p.st.GetOwnedAccount(ctx, owner, accountID)
	if err != nil {
		return 0, err
	}
	if err := validateCredential(a.CLI, a.Kind, secret); err != nil {
		return 0, err
	}
	enc, kv, err := p.st.EncryptAccountSecret(ctx, p.master, []byte(secret))
	if err != nil {
		return 0, err
	}
	if err := p.st.UpdateAccountSecret(ctx, accountID, enc, kv, displayLast4(a.Kind, secret)); err != nil {
		return 0, err
	}
	if err := p.st.AddAudit(ctx, owner, "account.secret_update", accountID, nil); err != nil {
		return 0, err
	}
	return p.st.AccountInUse(ctx, accountID)
}

// SetOwnedState 属主本人停用／启用。他人账号 ⇒ ErrNotFound（与不存在同）。
func (p *PoolService) SetOwnedState(ctx context.Context, owner, accountID string, disable bool) error {
	if _, err := p.st.GetOwnedAccount(ctx, owner, accountID); err != nil {
		return err
	}
	return p.setState(ctx, owner, accountID, disable)
}

// SetStateAdmin 管理用户停用／启用（组长 59bf5349：管理员可停用、可启用；不见密钥、不能换密）。
func (p *PoolService) SetStateAdmin(ctx context.Context, by string, isAdmin bool, accountID string, disable bool) error {
	if err := admin(by, isAdmin); err != nil {
		return err
	}
	if _, err := p.st.GetAccount(ctx, accountID); err != nil {
		return err
	}
	return p.setState(ctx, by, accountID, disable)
}

// Disable 宿主命令行停用（同管理口径）。
func (p *PoolService) Disable(ctx context.Context, by string, isAdmin bool, accountID string) error {
	return p.SetStateAdmin(ctx, by, isAdmin, accountID, true)
}

func (p *PoolService) setState(ctx context.Context, by, accountID string, disable bool) error {
	state, action := "available", "account.enable"
	if disable {
		state, action = "disabled", "account.disable"
	}
	if err := p.st.SetAccountState(ctx, accountID, state); err != nil {
		return err
	}
	return p.st.AddAudit(ctx, by, action, accountID, nil)
}

// AssignOwner 宿主命令行 assign-owner：只给无主存量行指派属主（已有主不可改）。
func (p *PoolService) AssignOwner(ctx context.Context, by string, isAdmin bool, accountID, ownerLC string) error {
	if err := admin(by, isAdmin); err != nil {
		return err
	}
	if err := p.st.AssignAccountOwner(ctx, accountID, ownerLC); err != nil {
		return err
	}
	return p.st.AddAudit(ctx, by, "account.assign_owner", accountID, jsonRaw(`{"owner":"`+ownerLC+`"}`))
}

// ListOwned 「我的 LLM 账号」元数据（含停用、带 in_use；无密钥）。
func (p *PoolService) ListOwned(ctx context.Context, owner string) ([]Account, error) {
	return p.st.ListOwnedAccounts(ctx, owner)
}

// Release 释放本席对账号的引用（销毁、建席失败回滚、RunJob 失败、新建超时共用）——只动本席，不动账号行与其他席位。
func (p *PoolService) Release(ctx context.Context, by, agentID string) error {
	acct, err := p.st.ReleaseSeatAccount(ctx, agentID)
	if err != nil || acct == "" {
		return err
	}
	return p.st.AddAudit(ctx, by, "account.unbind", acct, jsonRaw(`{"agent":"`+agentID+`"}`))
}

// DecryptForSeat 解密下发：只给**引用本号、且与账号同属主**的席位（最小暴露＋纵深）；每次解密记审计（T-G4-03）。
// 返回的明文只允许走「给该席下发凭据件」的通道（create_seat／refresh_cred_seat）；不进日志、不进审计 detail。
func (p *PoolService) DecryptForSeat(ctx context.Context, by string, accountID, agentID string) (string, error) {
	return p.decryptForSeat(ctx, by, accountID, agentID, "")
}

func (p *PoolService) decryptForSeat(ctx context.Context, by, accountID, agentID, reason string) (string, error) {
	ok, err := p.st.SeatMayUseAccount(ctx, agentID, accountID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrAccountBound // 不是该席引用的账号或属主不符——最小暴露边界
	}
	plain, err := p.st.DecryptAccountSecret(ctx, p.master, accountID)
	if err != nil {
		return "", err
	}
	detail := `{"agent":"` + agentID + `"}`
	if reason != "" {
		detail = `{"agent":"` + agentID + `","reason":"` + reason + `"}`
	}
	if err := p.st.AddAudit(ctx, by, "account.decrypt_issue", accountID, jsonRaw(detail)); err != nil {
		return "", err
	}
	return string(plain), nil
}

// ListAdmin 管理视图：全部账号的元数据（名称、类型、CLI／LLM、属主、在用数、状态、登记时间；无明文）。
func (p *PoolService) ListAdmin(ctx context.Context, by string, isAdmin bool) ([]Account, error) {
	if err := admin(by, isAdmin); err != nil {
		return nil, err
	}
	return p.st.ListAllAccounts(ctx)
}

func jsonRaw(s string) []byte { return []byte(s) }

var _ = time.Now // 预留：令牌续期回写（T-G4-22）的时间戳生成
