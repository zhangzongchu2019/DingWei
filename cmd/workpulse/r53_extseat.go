package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/mcpgw"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/extwire"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
)

// r53 外部 Agent 接入（MCP MVP）的 main 装配入口。
// 合同 docs/design/R53-MVP-CONTRACT.md 第 6 节：TenantKey 与 Resource 由组长在 main 统一配置，
// A／B／C 三路读同一份，不各自定义、不内置默认值。默认关：DW_R53_EXTSEAT 不为 "1" 时一行不执行。
type r53Config struct {
	tenantKey    string // DW_FEISHU_TEAM_TENANT_KEY：本团队飞书 tenant
	publicOrigin string // 与新平台使用同一 DW_PUBLIC_ORIGIN 配置
	resource     string // DW_MCP_RESOURCE：MCP 受保护资源标识（暂定 https://dw123.cmjrt.com/mcp）
}

var (
	errR53NeedsNewplat  = errors.New("DW_R53_EXTSEAT=1 需要 DW_NEWPLAT=1（外部席位依赖新平台 PG）")
	errR53TenantKey     = errors.New("DW_R53_EXTSEAT=1 需要 DW_FEISHU_TEAM_TENANT_KEY")
	errR53Resource      = errors.New("DW_R53_EXTSEAT=1 需要 DW_MCP_RESOURCE（https 绝对地址，无查询串与片段）")
	errR53GroupsMissing = errors.New("r53: 临时编组消费者（D 路）尚未并入集成，拒绝启动外部席位")
)

func r53ConfigFromEnv(newplatOn bool) (r53Config, bool, error) {
	if strings.TrimSpace(os.Getenv("DW_R53_EXTSEAT")) != "1" {
		return r53Config{}, false, nil
	}
	cfg := r53Config{
		tenantKey: strings.TrimSpace(os.Getenv("DW_FEISHU_TEAM_TENANT_KEY")),
		resource:  strings.TrimSpace(os.Getenv("DW_MCP_RESOURCE")),
	}
	if !newplatOn {
		return cfg, true, errR53NeedsNewplat
	}
	if cfg.tenantKey == "" {
		return cfg, true, errR53TenantKey
	}
	if !validMCPResource(cfg.resource) {
		return cfg, true, errR53Resource
	}
	return cfg, true, nil
}

func validMCPResource(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return false
	}
	return u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery
}

// r53Dependencies 仅做装配注入；D 由调用方提供，测试可替换飞书上游与 CIMD。
// 生产不给 OAuth/Clients 时使用 C 的正式飞书客户端与 B 的安全 CIMD resolver。
type r53Dependencies struct {
	mux              *http.ServeMux
	hub              *m8.Hub
	pool             *repo.Pool
	appID, appSecret string
	oauth            feishulogin.OAuthClient
	clients          mcpgw.ClientResolver
	trustedOrigins   []trustedorigin.Origin // r52 DW_TRUSTED_ORIGINS，与 Hub 其余写入口同一份
}

// r53TrustedOrigins 把 r52 受信来源接到 B 的 Origin 守卫：名单为空时只认 PublicOrigin（与 B 缺省逐字同），
// 名单非空时按 trustedorigin.Allows 的规范化精确匹配（端口、https 都算来源身份）。
type r53TrustedOrigins struct {
	publicOrigin string
	trusted      []trustedorigin.Origin
}

func (o r53TrustedOrigins) TrustedOrigin(_ context.Context, origin string) bool {
	return trustedorigin.Allows(origin, o.publicOrigin, o.trusted, origin == o.publicOrigin, 1)
}

func wireR53(ctx context.Context, cfg r53Config, checker extwire.TableChecker, groups extseat.Groups, d r53Dependencies) error {
	// 关闭态不读配置、不查库、不登记路由，与基线启动行为相同。
	if strings.TrimSpace(os.Getenv("DW_R53_EXTSEAT")) != "1" {
		return nil
	}
	if cfg.resource != cfg.publicOrigin+"/mcp" || !validR53Origin(cfg.publicOrigin) {
		return errors.New("r53: Resource 必须等于 PublicOrigin + /mcp（DW_PUBLIC_ORIGIN）")
	}
	if err := extwire.CheckSchema(ctx, checker); err != nil {
		return err
	}
	if groups == nil {
		return errR53GroupsMissing
	}
	if d.hub == nil || d.hub.Identity == nil || d.pool == nil || d.mux == nil || cfg.tenantKey == "" {
		return errors.New("r53: incomplete Hub dependencies or TenantKey")
	}
	seats := &extseat.Service{Pool: d.pool, Access: repo.SystemCtx(), Identity: d.hub.Identity,
		Helpers: m8.HubHelperPresence{Hub: d.hub}, Groups: groups, TenantKey: cfg.tenantKey, Resource: cfg.resource,
		OnBackgroundError: func(error) { log.Print("r53: external seat background operation failed") }}
	enabled := registerOptionalMCPGateway(ctx, d.mux, d.pool, repo.SystemCtx(), cfg.resource, func(store mcpgw.Store) (mcpgw.Config, error) {
		oauth := d.oauth
		redirect := cfg.publicOrigin + "/mcp/oauth/feishu/callback"
		if oauth == nil {
			var err error
			oauth, err = feishulogin.NewFeishuClient(feishulogin.OAuthConfig{AppID: d.appID, AppSecret: d.appSecret, RedirectURI: redirect})
			if err != nil {
				return mcpgw.Config{}, err
			}
		}
		defaults, err := extwire.ParseConsentTargets(os.Getenv("DW_MCP_CONSENT_TARGETS_JSON"))
		if err != nil {
			return mcpgw.Config{}, err
		}
		defaults.Identity, defaults.Pool, defaults.Access = d.hub.Identity, d.pool, repo.SystemCtx()
		login, err := feishulogin.NewHandler(feishulogin.Config{AppID: d.appID, TeamTenantKey: cfg.tenantKey, RedirectURI: redirect,
			Defaults: defaults, OAuth: oauth, States: mcpgw.LoginStates{Store: store}, Tokens: extwire.Issuer{Seats: seats, Resource: cfg.resource}})
		if err != nil {
			return mcpgw.Config{}, err
		}
		clients := d.clients
		if clients == nil {
			clients = mcpgw.MetadataClients{}
		}
		return mcpgw.Config{PublicOrigin: cfg.publicOrigin, Resource: cfg.resource, Seats: seats, Login: login, Clients: clients,
			Origins: r53TrustedOrigins{publicOrigin: cfg.publicOrigin, trusted: d.trustedOrigins}}, nil
	})
	if enabled {
		d.hub.ExternalSeats = seats
		if tg, ok := groups.(*tempgroup.Service); ok {
			tg.Access = seats.Access
			d.hub.TempGroups = tg
			tg.Finalize = seats.SettleDissolvedGroup
			tg.OnBackgroundError = func(error) { log.Print("r53: temporary group background operation failed") }
			if !startRunTask(ctx, "r53_tempgroup_worker", func() { _ = tg.Run(ctx) }) {
				return errors.New("r53: Hub is shutting down")
			}
		}

		done := func() {}
		if jobs, ok := ctx.Value(runTasksKey{}).(*lifecycle.Group); ok {
			var admitted bool
			done, admitted = jobs.Enter("r53_extseat_worker")
			if !admitted {
				return errors.New("r53: Hub is shutting down")
			}
		}
		go func() {
			defer done()
			if err := seats.Run(ctx, m8.HubExtseatProjector{Hub: d.hub}); err != nil && ctx.Err() == nil {
				log.Print("r53: external seat worker stopped")
			}
		}()
	}
	return nil
}

func validR53Origin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == ""
}

// pgTableChecker 用新平台连接池实现 extwire.TableChecker（写法同 identity.PGStore 的 CheckSchema）。
// 核表在不在属于无租户语义的系统操作，故用 SystemCtx。
type pgTableChecker struct{ pool *repo.Pool }

func (c pgTableChecker) TableExists(ctx context.Context, name string) (bool, error) {
	if c.pool == nil {
		return false, fmt.Errorf("r53: no pg pool")
	}
	row, err := c.pool.QueryRow(ctx, repo.SystemCtx(), `SELECT to_regclass($1) IS NOT NULL`, name)
	if err != nil {
		return false, err
	}
	var ok bool
	if err := row.Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}
