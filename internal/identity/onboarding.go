package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// N3：系统自举、开户、飞书注册码绑定、以用户 ID 登录。spec：SPEC-THIN-SH-N3-bootstrap-onboarding-DRAFT.md。

const (
	SystemDefaultUserID = "systemdefault"
	// SystemDefaultShortName systemdefault 的简称；spec 未定，暂取 sys（简称上限 16 位，systemdefault 本身超长）。
	SystemDefaultShortName = "sys"

	RegistrationCodeTTL = 24 * time.Hour // 台账 c5981300
	LoginCodeTTL        = 5 * time.Minute
	SessionTTL          = 12 * time.Hour // 与旧平台 consoleSessionTTL 一致
	MinPasswordLen      = 12
	// 同一来源对同一用户 ID 的密码登录失败：15 分钟内最多 5 次。
	passwordFailWindow = 15 * time.Minute
	passwordFailMax    = 5
	// 注册码与登录码的字母表：去掉易混的 0/O、1/I/L。
	codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLen      = 8
)

var (
	ErrBootstrapSecret = errors.New("identity: bootstrap password file missing or not 0600")
	ErrBadCredentials  = errors.New("identity: bad credentials")
	ErrWeakPassword    = errors.New("identity: password too weak")
	ErrNotAllowed      = errors.New("identity: not allowed for this role")
	ErrMustChange      = errors.New("identity: must change password first")
)

// Onboarding N3 的业务层。与入网的 Service 共用存储、时钟、随机源。
type Onboarding struct {
	*Service

	omu        sync.Mutex
	pwFails    map[string][]time.Time // 来源 IP＋用户 ID → 失败时刻
	loginCodes map[string]*loginTicket
}

type loginTicket struct {
	userLC    string
	expiresAt time.Time
	token     string // 飞书确认后填入，浏览器取走即删
}

// NewOnboarding 基于已有 Service 建 N3 业务层。
func NewOnboarding(s *Service) *Onboarding {
	return &Onboarding{Service: s, pwFails: map[string][]time.Time{}, loginCodes: map[string]*loginTicket{}}
}

// Bootstrap Hub 启动时调用（N3 第二节 1）。用户表为空才建 systemdefault；非空直接返回 false，什么也不改。
// 用户表为空而密码文件缺失、为空或权限宽于 0600 ⇒ 返回 ErrBootstrapSecret，调用方须拒绝启动（失败即关闭）。
func (o *Onboarding) Bootstrap(ctx context.Context, passwordFile string) (bool, error) {
	n, err := o.Store.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	fi, err := os.Stat(passwordFile)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return false, ErrBootstrapSecret
	}
	raw, err := os.ReadFile(passwordFile)
	if err != nil {
		return false, ErrBootstrapSecret
	}
	pw := strings.TrimRight(string(raw), "\r\n")
	if pw == "" {
		return false, ErrBootstrapSecret
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return false, err
	}
	u := User{
		UserID: SystemDefaultUserID, ShortName: SystemDefaultShortName, DisplayName: SystemDefaultUserID,
		Type: UserPlatformAdmin, State: UserActive, PasswordHash: string(hash), MustChangePassword: true, CreatedAt: o.Now(),
	}
	if err := o.Store.CreateUser(ctx, u); err != nil {
		return false, err
	}
	o.audit(ctx, "system", "user.bootstrap", SystemDefaultUserID, "")
	return true, nil
}

// PasswordLogin 用户 ID＋密码登录（本版只有 systemdefault 有密码）。成功返回会话令牌（只此一次）与用户。
func (o *Onboarding) PasswordLogin(ctx context.Context, userID, password, sourceIP string) (string, *User, error) {
	key := sourceIP + "|" + Lower(userID)
	now := o.Now()
	if err := o.checkPasswordFails(key, now); err != nil {
		return "", nil, err
	}
	u, err := o.Store.GetUser(ctx, userID)
	if err != nil || u.State != UserActive || u.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		o.recordPasswordFail(key, now)
		o.audit(ctx, "client@"+sourceIP, "login.rejected", Lower(userID), "method=password")
		return "", nil, ErrBadCredentials
	}
	token, err := o.newSession(ctx, u.UserIDLC())
	if err != nil {
		return "", nil, err
	}
	o.audit(ctx, u.UserIDLC(), "login.ok", u.UserIDLC(), "method=password")
	return token, u, nil
}

func (o *Onboarding) checkPasswordFails(key string, now time.Time) error {
	o.omu.Lock()
	defer o.omu.Unlock()
	cut := now.Add(-passwordFailWindow)
	kept := o.pwFails[key][:0]
	for _, t := range o.pwFails[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	o.pwFails[key] = kept
	if len(kept) >= passwordFailMax {
		return &RateLimitedError{RetryAfter: kept[0].Add(passwordFailWindow).Sub(now)}
	}
	return nil
}

func (o *Onboarding) recordPasswordFail(key string, now time.Time) {
	o.omu.Lock()
	defer o.omu.Unlock()
	o.pwFails[key] = append(o.pwFails[key], now)
}

func (o *Onboarding) newSession(ctx context.Context, userLC string) (string, error) {
	tok, err := o.randHex(32)
	if err != nil {
		return "", err
	}
	if err := o.Store.CreateSession(ctx, Session{TokenHash: HashSecret(tok), UserIDLC: userLC, ExpiresAt: o.Now().Add(SessionTTL)}); err != nil {
		return "", err
	}
	return tok, nil
}

// ResolveSession 按会话令牌认出当前用户。must_change_password 为真时返回 ErrMustChange（同时返回用户，供改密页使用）。
func (o *Onboarding) ResolveSession(ctx context.Context, token string) (*User, error) {
	_, u, err := o.Store.ResolveSession(ctx, HashSecret(token), o.Now())
	if err != nil {
		return nil, ErrNotFound
	}
	if u.MustChangePassword {
		return u, ErrMustChange
	}
	return u, nil
}

// ChangePassword 改密（N3 A2）。改后 must_change_password 清零，旧密码失效。
func (o *Onboarding) ChangePassword(ctx context.Context, userLC, oldPw, newPw string) error {
	u, err := o.Store.GetUser(ctx, userLC)
	if err != nil || u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPw)) != nil {
		return ErrBadCredentials
	}
	if len(newPw) < MinPasswordLen || subtle.ConstantTimeCompare([]byte(oldPw), []byte(newPw)) == 1 {
		return ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := o.Store.SetPassword(ctx, u.UserIDLC(), string(hash), false); err != nil {
		return err
	}
	o.audit(ctx, u.UserIDLC(), "password.change", u.UserIDLC(), "")
	return nil
}

// CanManageUsers 平台管理者与管理用户可开户（N3 A7）。
func CanManageUsers(u *User) bool {
	return u != nil && u.State == UserActive && (u.Type == UserPlatformAdmin || u.Type == UserManager)
}

// CreateUserRequest 管理者开户的输入（N3 第二节 3）。
type CreateUserRequest struct {
	DisplayName string
	UserID      string
	ShortName   string
	Type        UserType // manager ｜ member
}

// CreateUser 开户：建 pending_bind 账号并生成一次性注册码（明文只在返回值里出现这一次）。
func (o *Onboarding) CreateUser(ctx context.Context, actor *User, r CreateUserRequest) (*User, string, time.Time, error) {
	if !CanManageUsers(actor) {
		return nil, "", time.Time{}, ErrNotAllowed
	}
	if !ValidUserID(r.UserID) || !ValidShortName(r.ShortName) || (r.Type != UserManager && r.Type != UserMember) ||
		len(r.DisplayName) > 64 {
		return nil, "", time.Time{}, ErrBadRequest
	}
	u := User{UserID: r.UserID, ShortName: r.ShortName, DisplayName: r.DisplayName, Type: r.Type, State: UserPendingBind, CreatedAt: o.Now()}
	if err := o.Store.CreateUser(ctx, u); err != nil {
		return nil, "", time.Time{}, err
	}
	o.audit(ctx, actor.UserIDLC(), "user.create", u.UserIDLC(), "type="+string(r.Type))
	code, exp, err := o.issueRegistrationCode(ctx, actor, u.UserIDLC())
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return &u, code, exp, nil
}

// RegenerateRegistrationCode 重新生成注册码（旧码作废）。
func (o *Onboarding) RegenerateRegistrationCode(ctx context.Context, actor *User, userID string) (string, time.Time, error) {
	if !CanManageUsers(actor) {
		return "", time.Time{}, ErrNotAllowed
	}
	u, err := o.Store.GetUser(ctx, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if u.State != UserPendingBind {
		return "", time.Time{}, ErrNotPending
	}
	return o.issueRegistrationCode(ctx, actor, u.UserIDLC())
}

func (o *Onboarding) issueRegistrationCode(ctx context.Context, actor *User, userLC string) (string, time.Time, error) {
	code, err := o.randCode()
	if err != nil {
		return "", time.Time{}, err
	}
	exp := o.Now().Add(RegistrationCodeTTL)
	if err := o.Store.CreateRegistrationCode(ctx, userLC, HashSecret(code), exp); err != nil {
		return "", time.Time{}, err
	}
	o.audit(ctx, actor.UserIDLC(), "user.registration_code", userLC, "")
	return code, exp, nil
}

func (o *Onboarding) randCode() (string, error) {
	// 拒绝采样：只收 < 248（31 的 8 倍）的字节，避免取模偏差。
	limit := byte(256 / len(codeAlphabet) * len(codeAlphabet))
	out := make([]byte, 0, codeLen)
	b := make([]byte, 1)
	for len(out) < codeLen {
		if _, err := io.ReadFull(o.Rand, b); err != nil {
			return "", err
		}
		if b[0] < limit {
			out = append(out, codeAlphabet[int(b[0])%len(codeAlphabet)])
		}
	}
	return string(out), nil
}

// NormalizeCode 用户在飞书里输入的码：去空白、转大写。
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// HandleRegister 飞书「注册指令＋码」（N3 第二节 4）。身份取自信道给出的发信人，不取正文自称。
// 返回给机器人的一句回复；任何失败都不建账号、不改状态。
func (o *Onboarding) HandleRegister(ctx context.Context, sender Identity, code string) string {
	if sender.Subject == "" {
		return "无法确认发送者身份，注册被拒绝。"
	}
	u, err := o.Store.RedeemRegistrationCode(ctx, HashSecret(NormalizeCode(code)), sender, o.Now())
	switch {
	case err == nil:
		o.audit(ctx, u.UserIDLC(), "user.bind", u.UserIDLC(), "provider="+sender.Provider)
		return fmt.Sprintf("绑定成功：此飞书身份已绑定到用户 %s，现在可以登录 console。", u.UserID)
	case errors.Is(err, ErrIdentityTaken):
		o.audit(ctx, "feishu", "user.bind_rejected", "", "reason=identity_taken")
		return "此飞书身份已绑定到其它账号，注册被拒绝。"
	default:
		o.audit(ctx, "feishu", "user.bind_rejected", "", "reason=code_invalid")
		return "注册码无效、已使用或已过期，请联系管理者重新生成。"
	}
}

// IssueLoginCode 登录页输入用户 ID 后出一次性登录码（N3 第二节 5）。
// 用户 ID 不存在也照样出码（不泄露用户是否存在）；确认时才判定。
func (o *Onboarding) IssueLoginCode(userID string) (string, error) {
	if !ValidUserID(userID) {
		return "", ErrBadRequest
	}
	code, err := o.randCode()
	if err != nil {
		return "", err
	}
	o.omu.Lock()
	defer o.omu.Unlock()
	o.sweepLoginLocked(o.Now())
	o.loginCodes[code] = &loginTicket{userLC: Lower(userID), expiresAt: o.Now().Add(LoginCodeTTL)}
	return code, nil
}

func (o *Onboarding) sweepLoginLocked(now time.Time) {
	for c, t := range o.loginCodes {
		if !now.Before(t.expiresAt) {
			delete(o.loginCodes, c)
		}
	}
}

// HandleLogin 飞书「登录指令＋码」：发信身份必须绑定在登录码对应的那个用户 ID 上（N3 A11）。
func (o *Onboarding) HandleLogin(ctx context.Context, sender Identity, code string) string {
	code = NormalizeCode(code)
	now := o.Now()
	o.omu.Lock()
	t := o.loginCodes[code]
	valid := t != nil && now.Before(t.expiresAt) && t.token == ""
	o.omu.Unlock()
	if !valid {
		return "登录码已过期或已被使用，请在页面上重新获取。"
	}
	u, err := o.Store.ResolveIdentity(ctx, sender)
	if err != nil || u.State != UserActive || u.UserIDLC() != t.userLC {
		o.audit(ctx, "feishu", "login.rejected", t.userLC, "method=feishu")
		return "此飞书身份未绑定到该用户 ID，登录被拒绝。"
	}
	tok, err := o.newSession(ctx, u.UserIDLC())
	if err != nil {
		return "登录暂不可用。"
	}
	o.omu.Lock()
	if cur := o.loginCodes[code]; cur == t && cur.token == "" {
		cur.token = tok
	}
	o.omu.Unlock()
	o.audit(ctx, u.UserIDLC(), "login.ok", u.UserIDLC(), "method=feishu")
	return fmt.Sprintf("已确认以 %s 登录，请回到页面。%.0f 小时后需重新登录。", u.UserID, SessionTTL.Hours())
}

// ClaimLogin 浏览器轮询取会话令牌；一次性：取走即删（N3 A12）。
func (o *Onboarding) ClaimLogin(code string) (string, bool) {
	code = NormalizeCode(code)
	o.omu.Lock()
	defer o.omu.Unlock()
	t := o.loginCodes[code]
	if t == nil || t.token == "" || !o.Now().Before(t.expiresAt) {
		return "", false
	}
	delete(o.loginCodes, code)
	return t.token, true
}
