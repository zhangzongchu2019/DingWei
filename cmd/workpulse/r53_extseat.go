package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/extwire"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// r53 外部 Agent 接入（MCP MVP）的 main 装配入口。
// 合同 docs/design/R53-MVP-CONTRACT.md 第 6 节：TenantKey 与 Resource 由组长在 main 统一配置，
// A／B／C 三路读同一份，不各自定义、不内置默认值。默认关：DW_R53_EXTSEAT 不为 "1" 时一行不执行。
type r53Config struct {
	tenantKey string // DW_FEISHU_TEAM_TENANT_KEY：本团队飞书 tenant
	resource  string // DW_MCP_RESOURCE：MCP 受保护资源标识（暂定 https://dw123.cmjrt.com/mcp）
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

// wireR53 开启时先核 0105 表都在（迁移已由 openNewPlatformStore 施加；这里管「此刻表在不在」），
// 缺表即拒启动（rulings ㊹-补12 ⑦）。D 路未并入前，A 路 Service 缺 Groups，签发／续组无法成立 ⇒ fail closed。
func wireR53(ctx context.Context, cfg r53Config, checker extwire.TableChecker) error {
	if err := extwire.CheckSchema(ctx, checker); err != nil {
		return err
	}
	_ = cfg // D 路并入后在此装 extseat.Service{TenantKey: cfg.tenantKey, Resource: cfg.resource, ...}
	return errR53GroupsMissing
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
