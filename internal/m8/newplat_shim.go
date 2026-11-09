package m8

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

// 新平台身份垫片（R-V2-SPEC-TASK-SHIM-IMPL-20261002164524；定案甲，板件 ANALYSIS-…161119-opus50tc2k 第三节、
// 实施方案 PLAN-R-V2-SPEC-TASK-SHIM-IMPL-…-opus50tc2k）。
//
// spec 与任务管理的处理器、存储外键都绑在旧 SQLite 的 console_principal／console_agent 上。新平台身份在 PG，
// 这里把「一个用户＋其未吊销的 Agent（＋本次请求的会话）」投影进旧表，旧处理器不改就能用：
//   - 启动对账：ReconcileNewPlatformConsole（全部用户）；
//   - 随请求：新平台 console 请求先同步当前用户（改名、恢复、升降级由此跟上）；Agent 侧请求先同步其属主；
//   - 事件即时：吊销／暂停 ⇒ 投影 Agent 立即停用；用户停用／改类型 ⇒ 立即重投影，停用时断开其 Agent。
// 🔴 同步失败一律拒绝（503），不放行、不半投影（存储层一个事务写完一个用户）。

type newPlatformShimRepository interface {
	SyncNewPlatformConsoleIdentity(context.Context, model.NewPlatformConsoleProjection) (string, error)
	DeactivateNewPlatformConsoleAgent(context.Context, string) error
	DeactivateNewPlatformConsolePrincipal(context.Context, string) error
	ListNewPlatformConsolePrincipalIDs(context.Context) ([]string, error)
}

// newPlatformShimIssuer 投影主体的 provider_issuer。
const newPlatformShimIssuer = "dingwei"

func (h *Hub) newPlatformShim() (newPlatformShimRepository, bool) {
	if h.newPlatformShimRepo != nil {
		return h.newPlatformShimRepo, true
	}
	repo, ok := h.Repo.(newPlatformShimRepository)
	return repo, ok
}

// syncNewPlatformUser 投影一个用户；sess 非空时同事务投影该会话。用户在新平台已查不到 ⇒ 停用其全部投影。
func (h *Hub) syncNewPlatformUser(ctx context.Context, userLC string, sess *model.ConsoleSession) error {
	repo, ok := h.newPlatformShim()
	if !ok || h.Identity == nil {
		return errors.New("newplat shim unavailable")
	}
	userLC = identity.Lower(userLC)
	u, err := h.Identity.Store.GetUser(ctx, userLC)
	if errors.Is(err, identity.ErrNotFound) {
		return repo.DeactivateNewPlatformConsolePrincipal(ctx, userLC)
	}
	if err != nil {
		return err
	}
	agents, err := h.Identity.Store.ListAgents(ctx, userLC) // 未吊销的（active 与 suspended）
	if err != nil {
		return err
	}
	proj := model.NewPlatformConsoleProjection{
		PrincipalID: userLC, Issuer: newPlatformShimIssuer, DisplayName: u.ShortName, OwnerKey: AgentKeyID(userLC),
		Active: u.State == identity.UserActive, Management: identity.CanManageUsers(u), Session: sess, Now: h.now(),
	}
	for _, ag := range agents {
		proj.Agents = append(proj.Agents, model.NewPlatformConsoleAgent{ID: ag.ID, SessionName: ag.NameLC(), Active: ag.State == identity.AgentActive})
	}
	_, err = repo.SyncNewPlatformConsoleIdentity(ctx, proj)
	return err
}

// ReconcileNewPlatformConsole 启动对账：投影全部新平台用户；已投影而新平台里已查不到的主体一律停用（不删）。
// 某个用户失败不挡其余用户，错误合并返回。
func (h *Hub) ReconcileNewPlatformConsole(ctx context.Context) error {
	repo, ok := h.newPlatformShim()
	if !ok || h.Identity == nil {
		return errors.New("newplat shim unavailable")
	}
	users, err := h.Identity.Store.ListUsers(ctx)
	if err != nil {
		return err
	}
	var errs []error
	seen := map[string]bool{}
	for _, u := range users {
		seen[u.UserIDLC()] = true
		if err := h.syncNewPlatformUser(ctx, u.UserIDLC(), nil); err != nil {
			errs = append(errs, err)
		}
	}
	projected, err := repo.ListNewPlatformConsolePrincipalIDs(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, id := range projected {
		if !seen[id] {
			if err := repo.DeactivateNewPlatformConsolePrincipal(ctx, id); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// NewPlatformAgentGone identity.Service.OnRevoked／OnSuspended：投影 Agent 立即停用，再按原样断开在线连接。
func (h *Hub) NewPlatformAgentGone(agentID string) {
	h.newPlatformAgentUnavailable(agentID, true)
}

// NewPlatformAgentSuspended drops the active projection immediately but preserves history;
// a reversible suspension is not an identity revocation.
func (h *Hub) NewPlatformAgentSuspended(agentID string) {
	h.newPlatformAgentUnavailable(agentID, false)
}

func (h *Hub) newPlatformAgentUnavailable(agentID string, purgeHistory bool) {
	h.revokeConsoleMuxAgent(agentID)
	if purgeHistory && h.SessionHistory != nil && h.Identity != nil {
		if agent, err := h.Identity.Store.GetAgent(context.Background(), agentID); err == nil && agent != nil {
			if err := h.SessionHistory.RequestAgentDeletion(context.Background(), AgentKeyID(agent.OwnerLC), agent.ID, h.now()); err != nil {
				log.Printf("session history deletion enqueue failed agent=%q error_type=%T", agentID, err)
			}
		}
	}
	if repo, ok := h.newPlatformShim(); ok {
		if err := repo.DeactivateNewPlatformConsoleAgent(context.Background(), agentID); err != nil {
			log.Printf("newplat shim: deactivate agent %s: %v", agentID, err)
		}
	}
	h.DisconnectAgent(agentID)
}

// NewPlatformUserChanged identity.Service.OnUserChanged（A11 停用／恢复／改类型）：立即重投影；
// 用户已不是 active ⇒ 以 4401（credential_suspended：客户端不删凭据，等恢复）断开其全部在线 Agent。
func (h *Hub) NewPlatformUserChanged(userLC string) {
	h.revokeConsoleMuxUser(userLC)
	ctx := context.Background()
	if err := h.syncNewPlatformUser(ctx, userLC, nil); err != nil {
		log.Printf("newplat shim: sync user %s: %v", userLC, err)
	}
	if h.Identity == nil {
		return
	}
	u, err := h.Identity.Store.GetUser(ctx, userLC)
	if err != nil || u.State == identity.UserActive {
		return
	}
	agents, err := h.Identity.Store.ListAgents(ctx, userLC)
	if err != nil {
		return
	}
	for _, ag := range agents {
		if h.SessionHistory != nil {
			if err := h.SessionHistory.RequestAgentDeletion(ctx, AgentKeyID(ag.OwnerLC), ag.ID, h.now()); err != nil {
				log.Printf("session history deletion enqueue failed agent=%q error_type=%T", ag.ID, err)
			}
		}
		h.disconnectAgentSession(AgentKeyID(ag.OwnerLC), ag.NameLC(), "credential_suspended")
	}
}

// newPlatformSessionProjection 由新平台会话 cookie 得出投影会话（id／token_hash＝np:＋会话令牌哈希，到期同新平台会话）。
// 会话无效 ⇒ (nil, false)。
func (h *Hub) newPlatformSessionProjection(w http.ResponseWriter, r *http.Request) (*model.ConsoleSession, bool) {
	c, err := r.Cookie(httpapi.SessionCookie)
	if err != nil || c.Value == "" {
		return nil, false
	}
	if _, err := h.resolveNewPlatformHTTPSession(w, r, c.Value); err != nil {
		return nil, false
	}
	hash := identity.HashSecret(c.Value)
	s, u, err := h.Identity.Store.ResolveSession(r.Context(), hash, h.Identity.Now())
	if err != nil {
		return nil, false
	}
	id := store.NewPlatformSessionPrefix + hash
	return &model.ConsoleSession{ID: id, PrincipalID: u.UserIDLC(), TokenHash: id, ExpiresAt: s.ExpiresAt}, true
}

// newPlatformSessionValid 投影会话对应的新平台会话此刻仍有效、且属于 principal（Agent 侧每次现查：
// 人被停用、口令被重置 ⇒ 新平台会话已吊销 ⇒ 先前签的能力令牌随之失效，不等投影的 expires_at）。
func (h *Hub) newPlatformSessionValid(ctx context.Context, sessionID, principal string) bool {
	hash, ok := strings.CutPrefix(sessionID, store.NewPlatformSessionPrefix)
	if !ok || hash == "" {
		return false
	}
	_, u, err := h.Identity.Store.ResolveSession(ctx, hash, h.Identity.Now())
	return err == nil && u.UserIDLC() == principal && u.State == identity.UserActive
}

// newPlatformConsoleContext 新平台 console 请求认人之后：同步当前用户（连同本次会话），放入 principal 与投影会话。
// 同步失败 ⇒ 503，不放行。
func (h *Hub) newPlatformConsoleContext(w http.ResponseWriter, r *http.Request, userLC string) (*http.Request, bool) {
	sess, ok := h.newPlatformSessionProjection(w, r)
	if !ok || sess.PrincipalID != userLC {
		writeAgentWSError(w, http.StatusUnauthorized, "unauthenticated")
		return nil, false
	}
	if err := h.syncNewPlatformUser(r.Context(), userLC, sess); err != nil {
		log.Printf("newplat shim: sync user %s: %v", userLC, err)
		writeAgentWSError(w, http.StatusServiceUnavailable, "console_unavailable")
		return nil, false
	}
	ctx := context.WithValue(r.Context(), consolePrincipalContextKey{}, userLC)
	ctx = context.WithValue(ctx, consoleSessionContextKey{}, *sess)
	return r.WithContext(ctx), true
}
