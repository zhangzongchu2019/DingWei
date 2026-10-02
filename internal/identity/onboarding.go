package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// N3：初始化第一个管理用户、开户、飞书注册码绑定、以用户 ID 登录。spec：SPEC-THIN-SH-N3-bootstrap-onboarding-DRAFT.md。
// 业主裁定（台账 8971bddf）：取消常驻 systemdefault 与「空库首启自动建号」，改由初始化脚本（workpulse bootstrap-admin）建第一个管理用户。

const (
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
	// InitialPasswordLen 初始化脚本给出的一次性初始口令长度（同一字母表，约 99 位熵；首登强制改密）。
	InitialPasswordLen = 20
)

var (
	// ErrAdminExists 库中已有管理用户（platform_admin 或 manager，不论状态）：初始化脚本拒绝执行。
	ErrAdminExists = errors.New("identity: a management user already exists")
	// ErrNoPasswordLogin 该用户不用口令登录（飞书登录的普通用户没有口令可忘）：reset-password 拒绝，免得凭空给他开口令登录。
	ErrNoPasswordLogin = errors.New("identity: user does not log in with a password")
	ErrBadCredentials  = errors.New("identity: bad credentials")
	ErrWeakPassword    = errors.New("identity: password too weak")
	ErrNotAllowed      = errors.New("identity: not allowed for this role")
	ErrMustChange      = errors.New("identity: must change password first")
)

// Onboarding N3 的业务层。与入网的 Service 共用存储、时钟、随机源。
type Onboarding struct {
	*Service

	// CodeKey 注册码派生密钥（Hub 的 WP_SECRET_KEY）。非空 ⇒ 注册码由它、用户与到期时刻 HMAC 派生，
	// 管理用户在用户列表里可随时取回「当前有效」的码（业主反馈，板 7a138dcf）；库里照旧只存哈希。
	// 为空 ⇒ 随机码，取不回（页面退回「重新生成」）。
	CodeKey string

	omu        sync.Mutex
	umu        sync.Mutex             // 停用／改类型串行（SetUserState、SetUserType）；与 omu 分开，OnUserChanged 在其下调用
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

// BootstrapAdmin 初始化脚本（workpulse bootstrap-admin，运维在主机上执行）建第一个管理用户：
// 用户 ID 与简称由执行者给；类型 platform_admin、直接 active、必须改密；返回一次性初始口令（明文只在返回值里出现这一次）。
// 库中已有管理用户 ⇒ ErrAdminExists，什么也不改；用户 ID 或简称格式不对 ⇒ ErrBadRequest；与已有用户冲突 ⇒ ErrConflict。
func (o *Onboarding) BootstrapAdmin(ctx context.Context, userID, shortName, displayName string) (string, error) {
	if displayName == "" {
		displayName = userID
	}
	if !ValidUserID(userID) || !ValidShortName(shortName) || len(displayName) > 64 {
		return "", ErrBadRequest
	}
	pw, err := randFrom(o.Rand, codeAlphabet, InitialPasswordLen)
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	u := User{
		UserID: userID, ShortName: shortName, DisplayName: displayName,
		Type: UserPlatformAdmin, State: UserActive, PasswordHash: string(hash), MustChangePassword: true, CreatedAt: o.Now(),
	}
	if err := o.Store.CreateFirstAdmin(ctx, u); err != nil {
		return "", err
	}
	o.audit(ctx, "bootstrap-admin", "user.bootstrap_admin", u.UserIDLC(), "")
	return pw, nil
}

// ResetPassword 运维重置口令（组长令，workpulse reset-password，主机上执行）：生成一次性新口令（与初始化脚本同字母表与长度），
// 置必须改密，吊销该用户全部会话，写一笔审计（不含口令）。用户不存在 ⇒ ErrNotFound；用户本无口令登录 ⇒ ErrNoPasswordLogin。
// 返回的新口令明文只在返回值里出现这一次。
func (o *Onboarding) ResetPassword(ctx context.Context, userID string) (string, error) {
	u, err := o.Store.GetUser(ctx, userID)
	if err != nil {
		return "", ErrNotFound
	}
	if u.PasswordHash == "" {
		return "", ErrNoPasswordLogin
	}
	pw, err := randFrom(o.Rand, codeAlphabet, InitialPasswordLen)
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	if err := o.Store.ResetPassword(ctx, u.UserIDLC(), string(hash), o.Now()); err != nil {
		return "", err
	}
	o.audit(ctx, "reset-password", "user.password_reset_by_ops", u.UserIDLC(), "")
	return pw, nil
}

// PasswordLogin 用户 ID＋密码登录（本版只有初始化脚本建的管理用户有密码）。成功返回会话令牌（只此一次）与用户。
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

// IssueBindCode 本人自取的飞书绑定码（组长取甲，台账 e483f147）：已登录、active、尚无飞书身份的用户，
// 给自己出一个一次性码，在飞书里发注册指令兑换 —— 只绑身份、不改状态。只能给 actor 自己出（没有替别人出的入口）；
// 已有飞书身份 ⇒ ErrAlreadyBound。码与注册码同表同法（同样 24 小时、新码顶替旧码、派生可取回）。
// 🔴 再认证（组长 01be74c0，复验方登记②）：会话被盗者可借此把自己的飞书身份绑到受害账号上做持久化 ⇒
// 须带当前口令；口令错 ⇒ ErrBadCredentials，并与口令登录共用同一失败限速（来源＋用户 ID 为键，15 分钟 5 次）。
func (o *Onboarding) IssueBindCode(ctx context.Context, actor *User, password, sourceIP string) (string, time.Time, error) {
	if actor == nil || actor.State != UserActive {
		return "", time.Time{}, ErrNotAllowed
	}
	key := sourceIP + "|" + actor.UserIDLC()
	now := o.Now()
	if err := o.checkPasswordFails(key, now); err != nil {
		return "", time.Time{}, err
	}
	u, err := o.Store.GetUser(ctx, actor.UserIDLC())
	if err != nil || u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		o.recordPasswordFail(key, now)
		o.audit(ctx, actor.UserIDLC(), "bind_code.rejected", actor.UserIDLC(), "reason=bad_credentials")
		return "", time.Time{}, ErrBadCredentials
	}
	has, err := o.Store.UserHasIdentity(ctx, actor.UserIDLC(), "feishu")
	if err != nil {
		return "", time.Time{}, err
	}
	if has {
		return "", time.Time{}, ErrAlreadyBound
	}
	return o.issueRegistrationCode(ctx, actor, actor.UserIDLC())
}

// SetUserState 停用／恢复用户（R-V2-SPEC-TASK-SHIM-IMPL A11，组长 16:55:17Z／16:55:51Z／17:56:12Z）。返回操作后的实际状态。
// 只许可管理用户调用；不能动自己；平台管理者不可停用或恢复（与改类型同一护栏，R2）；
// 停用不得让平台上不再有 active 的可管理用户（ErrLastManager）。
// 请求只接受 disabled（任何状态均可停用）与 active（＝「恢复」，只从 disabled 起）。
// 🔴 恢复只回到停用前的状态（R1）：库里不存停用前状态，按激活痕迹推回——已有飞书身份或有口令（初始化脚本建的管理用户）
// ⇒ active；两者都无 ⇒ 停用前必是待绑定 ⇒ 回 pending_bind，激活仍只走飞书注册码。
// 停用时存储层同一事务吊销该用户全部会话；成功后同步调 OnUserChanged。
func (o *Onboarding) SetUserState(ctx context.Context, actor *User, userID string, state UserState) (UserState, error) {
	if !CanManageUsers(actor) {
		return "", ErrNotAllowed
	}
	if state != UserDisabled && state != UserActive {
		return "", ErrBadRequest
	}
	o.umu.Lock() // 「最后一个可管理用户」的判定与写入串行，免得两个管理者同时互相停用
	defer o.umu.Unlock()
	target, err := o.userChangeTarget(ctx, actor, userID)
	if err != nil {
		return "", err
	}
	if target.Type == UserPlatformAdmin {
		return "", ErrNotAllowed
	}
	if state == UserDisabled && target.State == UserDisabled {
		return UserDisabled, nil
	}
	if state == UserActive {
		if target.State != UserDisabled {
			if target.State == UserActive {
				return UserActive, nil // 已是 active：无变化
			}
			return "", ErrBadRequest // 待绑定不能经「恢复」激活
		}
		if state, err = o.restoredState(ctx, target); err != nil {
			return "", err
		}
	}
	if state == UserDisabled {
		if err := o.keepOneManager(ctx, target); err != nil {
			return "", err
		}
	}
	if err := o.Store.SetUserState(ctx, target.UserIDLC(), state, o.Now()); err != nil {
		return "", err
	}
	o.audit(ctx, actor.UserIDLC(), "user.state", target.UserIDLC(), "state="+string(state))
	if o.OnUserChanged != nil {
		o.OnUserChanged(target.UserIDLC())
	}
	return state, nil
}

// restoredState 停用用户恢复后应回到的状态（见 SetUserState 的 R1 说明）。
func (o *Onboarding) restoredState(ctx context.Context, u *User) (UserState, error) {
	if u.PasswordHash != "" {
		return UserActive, nil
	}
	bound, err := o.Store.UserHasIdentity(ctx, u.UserIDLC(), "feishu")
	if err != nil {
		return "", err
	}
	if bound {
		return UserActive, nil
	}
	return UserPendingBind, nil
}

// SetUserType 改用户类型（manager／member）。护栏同 SetUserState；平台管理者（初始化脚本建的）类型不可改。
func (o *Onboarding) SetUserType(ctx context.Context, actor *User, userID string, t UserType) error {
	if !CanManageUsers(actor) {
		return ErrNotAllowed
	}
	if t != UserManager && t != UserMember {
		return ErrBadRequest
	}
	o.umu.Lock()
	defer o.umu.Unlock()
	target, err := o.userChangeTarget(ctx, actor, userID)
	if err != nil {
		return err
	}
	if target.Type == UserPlatformAdmin {
		return ErrNotAllowed
	}
	if target.Type == t {
		return nil
	}
	if t == UserMember {
		if err := o.keepOneManager(ctx, target); err != nil {
			return err
		}
	}
	if err := o.Store.SetUserType(ctx, target.UserIDLC(), t); err != nil {
		return err
	}
	o.audit(ctx, actor.UserIDLC(), "user.type", target.UserIDLC(), "type="+string(t))
	if o.OnUserChanged != nil {
		o.OnUserChanged(target.UserIDLC())
	}
	return nil
}

// userChangeTarget 取被改的用户；不能是 actor 自己（ErrNotAllowed）。
func (o *Onboarding) userChangeTarget(ctx context.Context, actor *User, userID string) (*User, error) {
	target, err := o.Store.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if target.UserIDLC() == actor.UserIDLC() {
		return nil, ErrNotAllowed
	}
	return target, nil
}

// keepOneManager target 失去管理能力后，平台上须仍有别的 active 可管理用户。调用方须持 umu。
func (o *Onboarding) keepOneManager(ctx context.Context, target *User) error {
	if !CanManageUsers(target) {
		return nil
	}
	users, err := o.Store.ListUsers(ctx)
	if err != nil {
		return err
	}
	for i := range users {
		if users[i].UserIDLC() != target.UserIDLC() && CanManageUsers(&users[i]) {
			return nil
		}
	}
	return ErrLastManager
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
	// 到期时刻截到微秒（PG timestamptz 的精度），派生与取回用的是同一个值。
	exp := o.Now().Add(RegistrationCodeTTL).UTC().Truncate(time.Microsecond)
	var code string
	var err error
	if o.CodeKey != "" {
		// 新码必须与现有有效码不同（重新生成即作废旧码）：同一微秒内再生成会派生出同一个码，故顺延 1 微秒直到不撞。
		active, err := o.Store.ListActiveRegistrationCodes(ctx, o.Now())
		if err != nil {
			return "", time.Time{}, err
		}
		taken := map[string]bool{}
		for _, r := range active {
			taken[r.CodeDigest] = true
		}
		for code = deriveRegistrationCode(o.CodeKey, userLC, exp); taken[HashSecret(code)]; code = deriveRegistrationCode(o.CodeKey, userLC, exp) {
			exp = exp.Add(time.Microsecond)
		}
	} else if code, err = o.randCode(); err != nil {
		return "", time.Time{}, err
	}
	if err := o.Store.CreateRegistrationCode(ctx, userLC, HashSecret(code), exp); err != nil {
		return "", time.Time{}, err
	}
	o.audit(ctx, actor.UserIDLC(), "user.registration_code", userLC, "")
	return code, exp, nil
}

func (o *Onboarding) randCode() (string, error) { return randFrom(o.Rand, codeAlphabet, codeLen) }

// deriveRegistrationCode 由密钥、用户、到期时刻派生注册码：HMAC-SHA256 的字节按拒绝采样映到 codeAlphabet
// （只收小于字母表长度整数倍的字节，无取模偏差）。不知密钥则不可预测；同一组输入恒得同一个码，故可取回。
func deriveRegistrationCode(key, userLC string, exp time.Time) string {
	limit := 256 - 256%len(codeAlphabet)
	out := make([]byte, 0, codeLen)
	for counter := 0; len(out) < codeLen; counter++ {
		mac := hmac.New(sha256.New, []byte(key))
		fmt.Fprintf(mac, "dingwei-regcode-v1|%s|%s|%d", Lower(userLC), exp.UTC().Format(time.RFC3339Nano), counter)
		for _, b := range mac.Sum(nil) {
			if int(b) < limit && len(out) < codeLen {
				out = append(out, codeAlphabet[int(b)%len(codeAlphabet)])
			}
		}
	}
	return string(out)
}

// RegistrationCodeView 管理用户在用户列表里看到的「当前有效」注册码。
type RegistrationCodeView struct {
	Code      string
	ExpiresAt time.Time
}

// RecoverableRegistrationCodes 待绑定用户当前有效的注册码（未用、未过期、未被重新生成顶替），按小写用户 ID。
// 只给派生得出且哈希对得上的：未配 CodeKey、换过密钥、随机码时代发的码一律不给（取不回就不给，不猜）。
// 调用方负责只对管理用户展示（httpapi 的用户列表本就只对管理用户开放）。
func (o *Onboarding) RecoverableRegistrationCodes(ctx context.Context) (map[string]RegistrationCodeView, error) {
	out := map[string]RegistrationCodeView{}
	if o.CodeKey == "" {
		return out, nil
	}
	rows, err := o.Store.ListActiveRegistrationCodes(ctx, o.Now())
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		code := deriveRegistrationCode(o.CodeKey, r.UserIDLC, r.ExpiresAt)
		if subtle.ConstantTimeCompare([]byte(HashSecret(code)), []byte(r.CodeDigest)) == 1 {
			out[r.UserIDLC] = RegistrationCodeView{Code: code, ExpiresAt: r.ExpiresAt.UTC()}
		}
	}
	return out, nil
}

// randFrom 从字母表等概率取 n 个字符。拒绝采样：只收小于字母表长度整数倍的字节，避免取模偏差。
func randFrom(rnd io.Reader, alphabet string, n int) (string, error) {
	// 上限用 int 算：字母表长度整除 256 时（如 64 位组码字母表）上限正是 256，放进 byte 会溢出成 0、永远取不到字符。
	limit := 256 / len(alphabet) * len(alphabet)
	out := make([]byte, 0, n)
	b := make([]byte, 1)
	for len(out) < n {
		if _, err := io.ReadFull(rnd, b); err != nil {
			return "", err
		}
		if int(b[0]) < limit {
			out = append(out, alphabet[int(b[0])%len(alphabet)])
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
	case errors.Is(err, ErrAlreadyBound):
		o.audit(ctx, "feishu", "user.bind_rejected", "", "reason=already_bound")
		return "该用户已绑定飞书，无需再绑定。"
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
