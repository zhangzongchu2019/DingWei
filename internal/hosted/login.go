package hosted

// 账号登录（0.2.1.101，R-V2-ACCOUNT-LOGIN-FLOW；设计件 runbooks/DESIGN-R-V2-ACCOUNT-LOGIN-FLOW-opus50tc2k.md
// 第十二、十三节为实现依据）：只做 codex 设备码。平台在任一在线节点起一次性登录容器，把「链接＋一次性码」
// 只给发起人本人；用户在自己浏览器完成后，节点**主动上传**凭证（auth.json）到 /api/node/login/{id}，
// 本服务校验后直接走 RegisterOwned／UpdateSecretOwned 加密入池。
//
// 🔴 凭证不入登录会话表、不进审计 detail、不进日志、不回浏览器；链接与码只在进行中保留，结束即清空。
// 🔴 上传绑会话：只认会话记录的执行节点（节点密钥鉴权到该节点）、状态 waiting_user、未过期、一次性。

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	LoginTTL          = 15 * time.Minute // = codex 一次性码有效期
	LoginCooldown     = 30 * time.Second // 每人发起冷却（组长裁定）
	LoginPerNodeMax   = 2                // 每节点并发（组长裁定）
	LoginTotalMax     = 4                // 全平台总并发（组长裁定）
	LoginUploadMax    = MaxSecretBytes + 4<<10 + 1<<10
	loginCLI          = "codex"
	loginCredKind     = "token_file"
	loginEmailMaxLen  = 254
	loginStateStart   = "starting"
	loginStateWaiting = "waiting_user"
)

var (
	ErrLoginInProgress  = errors.New("login_in_progress")  // 本人已有进行中的登录
	ErrLoginRateLimited = errors.New("login_rate_limited") // 冷却未过
	ErrLoginBusy        = errors.New("login_busy")         // 全平台或节点并发已满／无在线节点
	ErrEmailInvalid     = errors.New("email_invalid")
	ErrLoginNotActive   = errors.New("login_not_active") // 上传时会话已结束／过期
	ErrLoginStartFailed = errors.New("login_start_failed")
	ErrLoginUnavailable = errors.New("login_unavailable") // 无 codex 登记镜像

	loginReasonPattern = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
	loginCodePattern   = regexp.MustCompile(`^[A-Z0-9]{4,}-[A-Z0-9]{4,}$`)
)

// LoginView 发起人本人可见的会话形状（URL／UserCode 只在进行中非空）。
type LoginView struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	LLM       string    `json:"llm"`
	Email     string    `json:"email"`
	URL       string    `json:"url,omitempty"`
	UserCode  string    `json:"user_code,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	AccountID string    `json:"account_id,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

type loginRow struct {
	LoginView
	OwnerLC, NodeID string
	TargetAccount   sql.NullString
}

// LoginService 账号登录编排。
type LoginService struct {
	st      *Store
	pool    *PoolService
	gw      NodeGateway
	images  ImageCatalog
	comboOK func(cli, llm, kind string) bool
	origin  string // Hub 公网源（拼节点上传地址）
	now     func() time.Time
	mu      sync.Mutex // 同一 Hub 进程内串行化上传收尾（一次性）
}

func NewLoginService(st *Store, pool *PoolService, gw NodeGateway, images ImageCatalog, comboOK func(cli, llm, kind string) bool, publicOrigin string) *LoginService {
	return &LoginService{st: st, pool: pool, gw: gw, images: images, comboOK: comboOK, origin: strings.TrimRight(publicOrigin, "/"), now: time.Now}
}

// ValidEmail 账号邮箱只做格式与长度校验（作为独立 argv／显示名用，永不经 shell）。
func ValidEmail(e string) bool {
	if len(e) == 0 || len(e) > loginEmailMaxLen || strings.Count(e, "@") != 1 {
		return false
	}
	for _, r := range e {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	local, domain, _ := strings.Cut(e, "@")
	return local != "" && domain != "" && strings.Contains(domain, ".")
}

func newLoginID() (string, error) {
	raw := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// Start 发起登录。reloginID 非空＝对本人已有 codex token_file 账号重新登录（成功才换凭据，失败旧凭据不动）。
func (l *LoginService) Start(ctx context.Context, owner, llm, email, reloginID string) (LoginView, error) {
	if owner == "" {
		return LoginView{}, ErrParams
	}
	email = strings.TrimSpace(email)
	var target sql.NullString
	if reloginID != "" {
		a, err := l.st.GetOwnedAccount(ctx, owner, reloginID)
		if err != nil {
			return LoginView{}, err // 他人／不存在一律 ErrNotFound
		}
		if a.Kind != loginCredKind || a.CLI != loginCLI {
			return LoginView{}, ErrComboIncompatible
		}
		llm = a.LLM
		if email == "" {
			email = a.Email
		}
		target = sql.NullString{String: a.ID, Valid: true}
	}
	if !ValidEmail(email) {
		return LoginView{}, ErrEmailInvalid
	}
	if llm == "" || !l.comboOK(loginCLI, llm, loginCredKind) {
		return LoginView{}, ErrComboIncompatible
	}
	if !target.Valid { // 新登记：发起时即预查入池会被拒的条件（名字＝邮箱：长度与本人内重名、账号数上限），免得用户在浏览器登完才失败
		if len(email) > AccountNameMaxLen {
			return LoginView{}, ErrEmailInvalid
		}
		if taken, err := l.st.OwnedAccountNameTaken(ctx, owner, email); err != nil {
			return LoginView{}, err
		} else if taken {
			return LoginView{}, ErrAccountNameTaken
		}
		if n, err := l.st.CountOwnedAccounts(ctx, owner); err == nil && n >= MaxAccountsPerOwner {
			return LoginView{}, ErrAccountLimit
		}
	}
	image, ok := l.images.Latest(loginCLI)
	if !ok {
		return LoginView{}, ErrLoginUnavailable
	}
	now := l.now()
	// 本人已到期但清扫尚未轮到的会话先收尾（否则部分唯一索引会把新发起误判为「进行中」）
	if rows, err := l.st.activeLogins(ctx); err == nil {
		for _, r := range rows {
			if r.OwnerLC == owner && !now.Before(r.ExpiresAt) {
				l.expireSerialized(ctx, r.ID)
			}
		}
	}
	if last, err := l.st.lastLoginStart(ctx, owner); err == nil && now.Sub(last) < LoginCooldown {
		return LoginView{}, ErrLoginRateLimited
	}
	node, err := l.pickLoginNode(ctx)
	if err != nil {
		return LoginView{}, err
	}
	id, err := newLoginID()
	if err != nil {
		return LoginView{}, err
	}
	row := loginRow{LoginView: LoginView{ID: id, State: loginStateStart, LLM: llm, Email: email, ExpiresAt: now.Add(LoginTTL)},
		OwnerLC: owner, NodeID: node, TargetAccount: target}
	if err := l.st.insertLogin(ctx, row, now); err != nil {
		return LoginView{}, err // 本人已有进行中（部分唯一索引）⇒ ErrLoginInProgress
	}
	_ = l.st.AddAudit(ctx, owner, "account.login_start", id, jsonRaw(`{"node":`+jsonStr(node)+`,"relogin":`+fmt.Sprint(target.Valid)+`}`))

	params, _ := json.Marshal(map[string]string{"login_id": id, "image": image, "upload_url": l.origin + "/api/node/login/" + id})
	res, err := l.gw.Send(ctx, node, NodeCommand{Type: "login_start", Params: params})
	// 命令已发出之后的任何失败（含网关超时、请求取消）：节点上可能已起容器 ⇒ 一律通知节点撤（幂等），不占并发名额到 15 分钟
	switch {
	case err != nil:
		l.fail(ctx, id, owner, "node_offline")
		l.cancelOnNode(node, id)
		return LoginView{}, ErrNodeOffline
	case !res.OK:
		l.cancelOnNode(node, id)
		switch {
		case strings.HasPrefix(res.Error, "unknown_command_type"):
			l.fail(ctx, id, owner, "node_unsupported")
			return LoginView{}, ErrNodeUnsupported
		case strings.HasPrefix(res.Error, "login_busy"):
			l.fail(ctx, id, owner, "login_busy")
			return LoginView{}, ErrLoginBusy
		default:
			l.fail(ctx, id, owner, "login_start_failed")
			return LoginView{}, ErrLoginStartFailed
		}
	}
	var out struct {
		URL      string `json:"url"`
		UserCode string `json:"user_code"`
	}
	if json.Unmarshal(res.Payload, &out) != nil || !strings.HasPrefix(out.URL, "https://") || !loginCodePattern.MatchString(out.UserCode) {
		l.fail(ctx, id, owner, "login_start_failed")
		l.cancelOnNode(node, id)
		return LoginView{}, ErrLoginStartFailed
	}
	if err := l.st.setLoginWaiting(ctx, id, out.URL, out.UserCode); err != nil {
		l.fail(ctx, id, owner, "login_start_failed")
		l.cancelOnNode(node, id)
		return LoginView{}, err
	}
	return l.Get(ctx, owner, id)
}

// pickLoginNode 任一在线节点：全平台进行中 < LoginTotalMax，取进行中最少且 < LoginPerNodeMax 的节点。
func (l *LoginService) pickLoginNode(ctx context.Context) (string, error) {
	nodes, err := l.st.ListNodes(ctx)
	if err != nil {
		return "", err
	}
	perNode, total, err := l.st.activeLoginsByNode(ctx)
	if err != nil {
		return "", err
	}
	if total >= LoginTotalMax {
		return "", ErrLoginBusy
	}
	best, bestN := "", LoginPerNodeMax
	for _, n := range nodes {
		if n.State != "online" {
			continue
		}
		if c := perNode[n.ID]; c < bestN {
			best, bestN = n.ID, c
		}
	}
	if best == "" {
		return "", ErrLoginBusy
	}
	return best, nil
}

// Get 发起人本人查进度；他人与不存在一律 ErrNotFound。到期未结束的就地置 expired。
func (l *LoginService) Get(ctx context.Context, owner, id string) (LoginView, error) {
	r, err := l.st.getLogin(ctx, id)
	if err != nil || r.OwnerLC != owner {
		return LoginView{}, ErrNotFound
	}
	if loginActive(r.State) && !l.now().Before(r.ExpiresAt) {
		l.expireSerialized(ctx, id)
		r, _ = l.st.getLogin(ctx, id)
	}
	return r.LoginView, nil
}

// Cancel 发起人取消：进行中 ⇒ cancelled（清链接与码）并通知节点删容器；已结束 ⇒ 原样返回（幂等）。
func (l *LoginService) Cancel(ctx context.Context, owner, id string) (LoginView, error) {
	r, err := l.st.getLogin(ctx, id)
	if err != nil || r.OwnerLC != owner {
		return LoginView{}, ErrNotFound
	}
	l.mu.Lock() // 与上传收尾（Complete）串行：不出现「已入池却显示已取消」
	moved, _ := l.st.finishLogin(ctx, id, "cancelled", "cancelled", "")
	l.mu.Unlock()
	if moved {
		_ = l.st.AddAudit(ctx, owner, "account.login_cancel", id, nil)
		l.cancelOnNode(r.NodeID, id)
	}
	return l.Get(ctx, owner, id)
}

// Complete 节点上传登录结果。调用前 handler 已按会话节点做节点密钥鉴权（见 AuthorizeUpload）。
// body：{"status":"ok","credential":"<auth.json>"} 或 {"status":"failed","reason":"<码>"}。
func (l *LoginService) Complete(ctx context.Context, id string, body []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, err := l.st.getLogin(ctx, id)
	if err != nil {
		return ErrNotFound
	}
	if loginActive(r.State) && !l.now().Before(r.ExpiresAt) {
		l.expire(ctx, r) // 过期后到的上传：就地收尾，不入池
		return ErrLoginNotActive
	}
	if r.State != loginStateWaiting {
		return ErrLoginNotActive
	}
	var in struct {
		Status     string `json:"status"`
		Credential string `json:"credential"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		l.fail(ctx, id, r.OwnerLC, "upload_malformed")
		return ErrParams
	}
	if in.Status != "ok" {
		reason := in.Reason
		if !loginReasonPattern.MatchString(reason) {
			reason = "login_failed"
		}
		l.fail(ctx, id, r.OwnerLC, reason)
		return nil
	}
	cred := in.Credential
	in.Credential = ""
	if !json.Valid([]byte(cred)) {
		l.fail(ctx, id, r.OwnerLC, "credential_not_json")
		return ErrParams
	}
	acctID, note, err := l.intoPool(ctx, r, cred)
	cred = ""
	if err != nil {
		l.fail(ctx, id, r.OwnerLC, poolFailReason(err))
		return nil
	}
	if moved, _ := l.st.finishLogin(ctx, id, "done", note, acctID); moved {
		_ = l.st.AddAudit(ctx, r.OwnerLC, "account.login_complete", id, jsonRaw(`{"account":`+jsonStr(acctID)+`}`))
	}
	return nil
}

// AuthorizeUpload 上传鉴权：会话存在、节点密钥鉴权到**会话记录的执行节点**（他节点的密钥一律拒）。
func (l *LoginService) AuthorizeUpload(ctx context.Context, id, secret string) error {
	r, err := l.st.getLogin(ctx, id)
	if err != nil {
		return ErrNotFound
	}
	if secret == "" {
		return ErrNodeAuth
	}
	return l.st.AuthenticateNode(ctx, r.NodeID, secret)
}

// FailUpload 上传体超限等：会话直接失败（不入池）。
func (l *LoginService) FailUpload(ctx context.Context, id, reason string) {
	if r, err := l.st.getLogin(ctx, id); err == nil {
		l.fail(ctx, id, r.OwnerLC, reason)
	}
}

// intoPool 新登记：名称＝邮箱、记 email 列；重新登录：只换凭据（成功才换，失败不动旧的）。
// codex 的 id_token 邮箱只作核对：解析不出照用户输入；不符仍入池、note＝email_mismatch 供页面提示。
func (l *LoginService) intoPool(ctx context.Context, r loginRow, cred string) (string, string, error) {
	note := ""
	if got := idTokenEmail(cred); got != "" && !strings.EqualFold(got, r.Email) {
		note = "email_mismatch"
	}
	if r.TargetAccount.Valid {
		if _, err := l.pool.UpdateSecretOwned(ctx, r.OwnerLC, r.TargetAccount.String, cred); err != nil {
			return "", "", err
		}
		return r.TargetAccount.String, note, nil
	}
	a, err := l.pool.RegisterOwnedLogin(ctx, r.OwnerLC, r.Email, r.LLM, cred) // 邮箱随同一条 INSERT 落库
	if err != nil {
		return "", "", err
	}
	return a.ID, note, nil
}

func poolFailReason(err error) string {
	switch {
	case errors.Is(err, ErrAccountNameTaken):
		return "account_name_taken"
	case errors.Is(err, ErrAccountLimit):
		return "account_limit"
	case errors.Is(err, ErrNotFound):
		return "account_not_found"
	case errors.Is(err, ErrSecretTooLarge):
		return "too_large"
	default:
		return "pool_failed"
	}
}

// idTokenEmail 只解码（不验签）auth.json 里 tokens.id_token 的 email 声明；任何不符合预期的形状 ⇒ ""。
func idTokenEmail(authJSON string) string {
	var d struct {
		Tokens struct {
			IDToken string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal([]byte(authJSON), &d) != nil {
		return ""
	}
	parts := strings.Split(d.Tokens.IDToken, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var c struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	return c.Email
}

func loginActive(state string) bool { return state == loginStateStart || state == loginStateWaiting }

func (l *LoginService) fail(ctx context.Context, id, owner, reason string) {
	if moved, _ := l.st.finishLogin(ctx, id, "failed", reason, ""); moved {
		_ = l.st.AddAudit(ctx, owner, "account.login_failed", id, jsonRaw(`{"reason":`+jsonStr(reason)+`}`))
	}
}

// expireSerialized 到期收尾与上传收尾（Complete 持 l.mu）串行，并在锁内重读：
// 上传刚把会话收成 done 的，不会再被清扫改成 expired（复核 tosonnet55tc2k 建议 7）。
func (l *LoginService) expireSerialized(ctx context.Context, id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, err := l.st.getLogin(ctx, id)
	if err != nil || !loginActive(r.State) || l.now().Before(r.ExpiresAt) {
		return
	}
	l.expire(ctx, r)
}

// expire 调用方须持 l.mu（或在 Complete 内）。
func (l *LoginService) expire(ctx context.Context, r loginRow) {
	if moved, _ := l.st.finishLogin(ctx, r.ID, "expired", "expired", ""); moved {
		_ = l.st.AddAudit(ctx, r.OwnerLC, "account.login_expired", r.ID, nil)
		l.cancelOnNode(r.NodeID, r.ID)
	}
}

// cancelOnNode 通知节点删容器（尽力而为：节点不在线则由节点启动清扫兜底）。
func (l *LoginService) cancelOnNode(node, id string) {
	params, _ := json.Marshal(map[string]string{"login_id": id})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = l.gw.Send(ctx, node, NodeCommand{Type: "login_cancel", Params: params})
	}()
}

// Sweep 到期清扫（周期调用）：进行中且已过 expires_at ⇒ expired＋通知节点。
func (l *LoginService) Sweep(ctx context.Context) error {
	rows, err := l.st.activeLogins(ctx)
	if err != nil {
		return err
	}
	now := l.now()
	for _, r := range rows {
		if !now.Before(r.ExpiresAt) {
			l.expireSerialized(ctx, r.ID)
		}
	}
	return nil
}

// RecoverOnStart Hub 启动：上次进程留下的进行中会话一律 expired（节点侧容器由 cancel／节点清扫收尾）。
func (l *LoginService) RecoverOnStart(ctx context.Context) error {
	rows, err := l.st.activeLogins(ctx)
	if err != nil {
		return err
	}
	for _, r := range rows {
		l.mu.Lock()
		l.expire(ctx, r)
		l.mu.Unlock()
	}
	return nil
}

// ---------- store ----------

const loginCols = `id, owner_lc, cli, llm, email, account_id, node_id, state, url, user_code, reason, result_account_id, expires_at`

func scanLogin(sc interface{ Scan(...any) error }) (loginRow, error) {
	var r loginRow
	var cli string
	var result sql.NullString
	err := sc.Scan(&r.ID, &r.OwnerLC, &cli, &r.LLM, &r.Email, &r.TargetAccount, &r.NodeID, &r.State, &r.URL, &r.UserCode, &r.Reason, &result, &r.ExpiresAt)
	r.AccountID = result.String
	return r, err
}

func (s *Store) insertLogin(ctx context.Context, r loginRow, now time.Time) error {
	_, err := s.exec(ctx, `
INSERT INTO g4_account_login (id, owner_lc, cli, llm, email, account_id, node_id, state, created_at, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.ID, r.OwnerLC, loginCLI, r.LLM, r.Email, r.TargetAccount, r.NodeID, r.State, now, r.ExpiresAt)
	if err != nil && isUniqueViolation(err, "g4_account_login_one_active") {
		return ErrLoginInProgress
	}
	return err
}

func (s *Store) getLogin(ctx context.Context, id string) (loginRow, error) {
	row, err := s.qrow(ctx, `SELECT `+loginCols+` FROM g4_account_login WHERE id=$1`, id)
	if err != nil {
		return loginRow{}, err
	}
	r, err := scanLogin(row)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) setLoginWaiting(ctx context.Context, id, url, code string) error {
	_, err := s.exec(ctx, `UPDATE g4_account_login SET state='waiting_user', url=$2, user_code=$3 WHERE id=$1 AND state='starting'`, id, url, code)
	return err
}

// finishLogin 只从进行中迁到终态（条件更新＝一次性）；同时清空链接与码。
func (s *Store) finishLogin(ctx context.Context, id, state, reason, accountID string) (bool, error) {
	var acct any
	if accountID != "" {
		acct = accountID
	}
	res, err := s.exec(ctx, `
UPDATE g4_account_login SET state=$2, reason=$3, result_account_id=$4, url='', user_code='', finished_at=now()
WHERE id=$1 AND state IN ('starting','waiting_user')`, id, state, reason, acct)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) lastLoginStart(ctx context.Context, owner string) (time.Time, error) {
	var t sql.NullTime
	row, err := s.qrow(ctx, `SELECT max(created_at) FROM g4_account_login WHERE owner_lc=$1`, owner)
	if err != nil {
		return time.Time{}, err
	}
	if err := row.Scan(&t); err != nil || !t.Valid {
		return time.Time{}, ErrNotFound
	}
	return t.Time, nil
}

func (s *Store) activeLoginsByNode(ctx context.Context) (map[string]int, int, error) {
	rows, err := s.query(ctx, `SELECT node_id, count(*) FROM g4_account_login WHERE state IN ('starting','waiting_user') GROUP BY node_id`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, total := map[string]int{}, 0
	for rows.Next() {
		var n string
		var c int
		if err := rows.Scan(&n, &c); err != nil {
			return nil, 0, err
		}
		out[n] = c
		total += c
	}
	return out, total, rows.Err()
}

func (s *Store) activeLogins(ctx context.Context) ([]loginRow, error) {
	rows, err := s.query(ctx, `SELECT `+loginCols+` FROM g4_account_login WHERE state IN ('starting','waiting_user')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []loginRow
	for rows.Next() {
		r, err := scanLogin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
