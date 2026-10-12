package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/consolemux"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
)

// 新平台（G3）装配。DW_NEWPLAT=1 时启用：
//   - 身份与入网：/api/enroll/*、/api/console/enroll/*、/api/console/agents（替换旧实现）
//   - N3 登录：/console/login/*、/console/logout、/api/console/account/password、/api/console/platform/users*
//   - 连接：/ws/agent 只认专属凭据；旧入口 /ws/session 回 410；/ws/service 只收本机与内网来源
//
// 环境变量（不含任何值，值由部署方给）：
//
//	DW_PUBLIC_ORIGIN            对外源，例如 https://dw.cmjrt.com:55055（console 写接口校验 Origin 用；必填）
//	DW_NEWPLAT_PG_DSN           身份存储所在 PG 的连接串（与 G2 同一个 PG 库；必填）。表由迁移账本建，缺表拒绝启动
//	DW_TRUST_PROXY=1            Hub 在本机 nginx 之后时开启：直连方为回环时采信 X-Real-IP
//
// 第一个管理用户不再由 Hub 自动建（业主裁定，台账 8971bddf：取消常驻 systemdefault 与空库首启自动建号），
// 由运维在主机上执行初始化脚本 workpulse bootstrap-admin 建（见 bootstrap_admin.go）。
type newPlatformConfig struct {
	publicOrigin string
	pgDSN        string
	trustProxy   bool
	// codeKey 注册码派生密钥（WP_SECRET_KEY，与旧平台加密管道密钥同一个环境变量）；空 ⇒ 注册码随机、列表里取不回。
	codeKey string
	// hosted 列表托管子对象的提供函数，签名即 httpapi.API.Hosted。G4 装配编排器处一行：cfg.hosted = orch.HostedItem
	// （kimi30kqtuxerh/g4-m0-m1 5a811f0d 起签名逐字相同）。为空时列表不出 hosted 子对象，页面按契约降级为只显示在线／离线。
	hosted func(ctx context.Context, agentID string) (map[string]any, bool)
}

var (
	errNewPlatformConfig = errors.New("DW_NEWPLAT=1 需要 DW_PUBLIC_ORIGIN")
	errNewPlatformPGDSN  = errors.New("新平台身份存储需要 DW_NEWPLAT_PG_DSN（Hub 与 bootstrap-admin 必须连同一个 PG 库）")
)

func newPlatformConfigFromEnv() (newPlatformConfig, bool, error) {
	if strings.TrimSpace(os.Getenv("DW_NEWPLAT")) != "1" {
		return newPlatformConfig{}, false, nil
	}
	cfg := newPlatformConfig{
		publicOrigin: strings.TrimSpace(os.Getenv("DW_PUBLIC_ORIGIN")),
		pgDSN:        strings.TrimSpace(os.Getenv("DW_NEWPLAT_PG_DSN")),
		trustProxy:   strings.TrimSpace(os.Getenv("DW_TRUST_PROXY")) == "1",
		codeKey:      os.Getenv("WP_SECRET_KEY"),
	}
	if cfg.publicOrigin == "" {
		return cfg, true, errNewPlatformConfig
	}
	if cfg.pgDSN == "" {
		return cfg, true, errNewPlatformPGDSN
	}
	return cfg, true, nil
}

// openNewPlatformStore Hub 与初始化脚本共用：连 PG（只经 internal/repo）、按迁移账本建表、再核对表都在。
// 不再有内存实现的退路 —— 初始化脚本与 Hub 是两个进程，只有落库，脚本建的管理用户 Hub 才看得见。
// pool 一并返回：G4 托管面（hosted.Store）与身份共用同一个连接池（装配④，88928305）。
func openNewPlatformStore(ctx context.Context, dsn string) (identity.Store, *repo.Pool, func(), error) {
	if dsn == "" {
		return nil, nil, nil, errNewPlatformPGDSN
	}
	pool, err := repo.OpenPG(ctx, dsn)
	if err != nil {
		return nil, nil, nil, err
	}
	// 🔴 先 Migrate 再用（组长裁定 ruling-2026-10-01T145840Z：建表归 G2 迁移账本，
	// Hub 启动与 bootstrap-admin 均先 Migrate）。账本失败即拒绝启动，不在半套表上写。
	// 🔴 只施加【Hub 域】的条目（core／identity／hosted／view／cross），🔴 不含 fanout：
	// fanout 是独立服务、独立库（裁定 R-G2-LEDGER-DOMAIN，2026-10-02T11:06:25Z）。
	// 少了这一层，0400 的五张 fanout 表会被建进生产 Hub 库。
	// 隔离核对由 migrate 内部做：库里若已记着别的域的账（例如误把 FANOUT_PG_DSN 指到 Hub 库、
	// 或反过来），拒绝启动且不施加任何条目。
	// 🔵 两步不互相替代，也不因账本到位就删掉 CheckSchema：
	//    账本管「有没有按账迁过、账内 digest 与条目正文是否一致」，
	//    CheckSchema 管「此刻库里表到底在不在」（例如迁过之后被人手工 drop）。
	rep, err := migrate.Migrate(ctx, pool, migrate.HubDomains())
	if err != nil {
		_ = pool.Close()
		return nil, nil, nil, err
	}
	if len(rep.Applied) > 0 {
		log.Printf("newplat migrate: applied %s", strings.Join(rep.Applied, ", "))
	}
	st := identity.NewPGStore(pool)
	if err := st.CheckSchema(ctx); err != nil {
		_ = pool.Close()
		return nil, nil, nil, err
	}
	return st, pool, func() { _ = pool.Close() }, nil
}

// hasManagementUser 库中是否已有管理用户（platform_admin 或 manager，不论状态；与初始化脚本的拒绝条件一致）。
func hasManagementUser(ctx context.Context, store identity.Store) (bool, error) {
	users, err := store.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, u := range users {
		if u.Type == identity.UserPlatformAdmin || u.Type == identity.UserManager {
			return true, nil
		}
	}
	return false, nil
}

// wireNewPlatform 在给定身份存储上装配新平台、挂路由。不建任何账号；库中还没有管理用户时只提示运维执行初始化脚本。
func wireNewPlatform(ctx context.Context, mux httpapi.Mux, hub *m8.Hub, cfg newPlatformConfig, store identity.Store, pool *repo.Pool) error {
	svc := identity.NewService(store, nil, nil)
	// 吊销／暂停：先停用垫片投影的 console_agent（spec／任务 Agent 侧立即失效），再按原样 4401 断开（R-V2-SPEC-TASK-SHIM-IMPL U3）。
	svc.OnRevoked = hub.NewPlatformAgentGone
	svc.OnSuspended = hub.NewPlatformAgentGone
	// 用户停用／恢复／改类型（A11）：立即重投影；停用时断开其全部 Agent。
	svc.OnUserChanged = hub.NewPlatformUserChanged
	svc.OnCredentialIssued = hub.DisconnectAgentRotated // 轮换只断不删：旧 helper 收 credential_suspended，不删共用 /work 里的新凭据
	svc.NameAllowed = m8.AgentNameAllowed
	ob := identity.NewOnboarding(svc)
	ob.CodeKey = cfg.codeKey
	hasAdmin, err := hasManagementUser(ctx, store)
	if err != nil {
		return err
	}
	if !hasAdmin {
		log.Printf("newplat: no management user yet; on this host run: workpulse bootstrap-admin --user-id <ID> --short-name <short>")
	}
	hub.Identity = svc
	hub.Onboarding = ob
	hub.AgentCredentialOnly = true
	hub.PublicOrigin = cfg.publicOrigin
	// Shared PG is required for a global single transport slot. Memory mode keeps legacy views.
	if pg, ok := store.(*identity.PGStore); ok {
		budget := consolemux.NewBudget()
		if err := hub.ConfigureConsoleMux(pg, pg.ConsoleMuxSlots(budget), budget); err != nil {
			return err
		}
		svc.OnSessionRevoked = hub.RevokeConsoleMuxSession
		mux.HandleFunc("GET /api/console/event-context", hub.HandleConsoleMuxContext)
		mux.HandleFunc("POST /api/console/event-feeds", hub.HandleConsoleMuxCreate)
		mux.HandleFunc("GET /api/console/event-feeds/{feed_id}", hub.HandleConsoleMuxMetadata)
		mux.HandleFunc("PUT /api/console/event-feeds/{feed_id}", hub.HandleConsoleMuxReplace)
		mux.HandleFunc("DELETE /api/console/event-feeds/{feed_id}", hub.HandleConsoleMuxDelete)
		mux.HandleFunc("GET /api/console/event-feeds/{feed_id}/events", hub.HandleConsoleMuxEvents)
	}

	// 垫片启动对账（R-V2-SPEC-TASK-SHIM-IMPL）：失败只记日志不挡启动——随请求同步会补上，且同步失败的请求一律拒绝。
	if err := hub.ReconcileNewPlatformConsole(ctx); err != nil {
		log.Printf("newplat shim: startup reconcile: %v", err)
	}

	n3 := &httpapi.N3{OB: ob, PublicOrigin: cfg.publicOrigin, TrustProxy: cfg.trustProxy, Secure: true}
	// G4 托管装配（88928305）：pool 非 nil（PG 模式）才尝试；内部按 DW_G4_MASTER_KEY_FILE 开关。
	// 须在 api 构造前——cfg.hosted 注入点（orch.HostedItem）在 api 上。
	hostedOn := false
	if pool != nil {
		hostedOn, err = wireHosted(ctx, mux, hub, &cfg, pool, store, svc, n3.Actor)
		if err != nil {
			return fmt.Errorf("newplat hosted wiring: %w", err)
		}
	}
	// 页面只认真实装配结果；不能按 env、pool 非空或浏览器 404 猜测。
	hub.SetConsoleHostedPageEnabled(hostedOn)
	api := &httpapi.API{Svc: svc, Claims: ob, ClaimBots: hub.ClaimGuidance, Actor: n3.Actor, PublicOrigin: cfg.publicOrigin, TrustProxy: cfg.trustProxy, Online: hub.AgentOnline, Hosted: cfg.hosted}
	n3.Register(mux)
	api.Register(mux)
	mux.HandleFunc("GET /ws/agent", hub.HandleAgentWS)
	registerNewPlatformLoginPage(mux, hub)
	return nil
}

// newPlatformLoginPager 新平台登录页（tosonnet50ts 承办，Hub.HandleNewPlatformLoginPage）。
// 用接口断言登记：两条分支谁先合都能编译；登录页那一侧合入后自动生效（组长台账 9c647ee7 ①）。
type newPlatformLoginPager interface {
	HandleNewPlatformLoginPage(http.ResponseWriter, *http.Request)
}

func registerNewPlatformLoginPage(mux httpapi.Mux, hub any) bool {
	lp, ok := hub.(newPlatformLoginPager)
	if !ok {
		log.Printf("newplat login page not built into this binary: GET /console/login not registered")
		return false
	}
	mux.HandleFunc("GET /console/login", lp.HandleNewPlatformLoginPage)
	return true
}
