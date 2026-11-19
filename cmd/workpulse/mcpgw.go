package main

import (
	"context"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/zhangzongchu2019/dingwei/internal/mcpgw"
)

// registerMCPGatewayRoutes 由集成装配传入 A、C、统一 PublicOrigin/Resource、
// CIMD 与持久加密 OAuth Store；缺依赖拒绝注册，不以环境变量猜配置或启动后台实例。
// 本路不装配 D，不替换现有 helper 路径。
func registerMCPGatewayRoutes(mux *http.ServeMux, cfg mcpgw.Config) error {
	gateway, err := mcpgw.New(cfg)
	if err != nil {
		return err
	}
	gateway.Register(mux)
	return nil
}

// registerOptionalMCPGateway 密钥或本路配置无效只关闭本路，不阻止 Hub 其他功能。
// build 在 Store 可用后构造 C Handler（可用 mcpgw.LoginStates），避免循环依赖。
func registerOptionalMCPGateway(ctx context.Context, mux *http.ServeMux, pool *repo.Pool, access repo.AccessCtx, resource string, build func(mcpgw.Store) (mcpgw.Config, error)) bool {
	fail := func() bool {
		log.Printf("mcpgw OAuth disabled: invalid key file, missing migration, or incomplete configuration")
		return false
	}
	key, e := mcpgw.ReadOAuthKeyFile(os.Getenv("DW_MCPGW_OAUTH_KEY_FILE"))
	if e != nil {
		return fail()
	}
	defer clear(key)
	store, e := mcpgw.NewSQLStore(ctx, pool, access, resource, key)
	if e != nil || build == nil {
		return fail()
	}
	cfg, e := build(store)
	if e != nil {
		return fail()
	}
	cfg.Store = store
	cfg.DCR = configuredDCR(ctx, pool, access, resource, key)
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if e = registerMCPGatewayRoutes(mux, cfg); e != nil {
		return fail()
	}
	if cfg.DCR.Store != nil && cfg.DCR.Policy.Enabled {
		startDCRGC(ctx, cfg.DCR)
	}
	return true
}
