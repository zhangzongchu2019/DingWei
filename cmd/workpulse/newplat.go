package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

// 新平台（G3）装配。DW_NEWPLAT=1 时启用：
//   - 身份与入网：/api/enroll/*、/api/console/enroll/*、/api/console/agents（替换旧实现）
//   - N3 登录：/console/login/*、/console/logout、/api/console/account/password、/api/console/platform/users*
//   - 连接：/ws/agent 只认专属凭据；旧入口 /ws/session 回 410；/ws/service 只收本机与内网来源
//
// 环境变量（不含任何值，值由部署方给）：
//
//	DW_PUBLIC_ORIGIN            对外源，例如 https://dw.cmjrt.com:55055（console 写接口校验 Origin 用；必填）
//	DW_BOOTSTRAP_PASSWORD_FILE  systemdefault 初始密码文件，须 0600（用户表为空时必填，否则拒绝启动）
//	DW_TRUST_PROXY=1            Hub 在本机 nginx 之后时开启：直连方为回环时采信 X-Real-IP
type newPlatformConfig struct {
	publicOrigin string
	passwordFile string
	trustProxy   bool
}

var errNewPlatformConfig = errors.New("DW_NEWPLAT=1 需要 DW_PUBLIC_ORIGIN")

func newPlatformConfigFromEnv() (newPlatformConfig, bool, error) {
	if strings.TrimSpace(os.Getenv("DW_NEWPLAT")) != "1" {
		return newPlatformConfig{}, false, nil
	}
	cfg := newPlatformConfig{
		publicOrigin: strings.TrimSpace(os.Getenv("DW_PUBLIC_ORIGIN")),
		passwordFile: strings.TrimSpace(os.Getenv("DW_BOOTSTRAP_PASSWORD_FILE")),
		trustProxy:   strings.TrimSpace(os.Getenv("DW_TRUST_PROXY")) == "1",
	}
	if cfg.publicOrigin == "" {
		return cfg, true, errNewPlatformConfig
	}
	return cfg, true, nil
}

// wireNewPlatform 建身份存储、自举、挂路由。自举失败（密码文件缺失或权限过宽）返回错误，调用方须拒绝启动。
func wireNewPlatform(ctx context.Context, mux *http.ServeMux, hub *m8.Hub, cfg newPlatformConfig) error {
	// 🔴 PG 实现（G2 的 R0）到位前用内存存储：重启即丢，只供内部验证，不得用于业主体验版本。
	log.Printf("newplat identity store=memory (NON-PERSISTENT until R0 PG implementation is wired)")
	store := identity.NewMemStore()
	svc := identity.NewService(store, nil, nil)
	svc.OnRevoked = hub.DisconnectAgent
	svc.OnSuspended = hub.DisconnectAgent
	svc.NameAllowed = m8.AgentNameAllowed
	ob := identity.NewOnboarding(svc)
	created, err := ob.Bootstrap(ctx, cfg.passwordFile)
	if err != nil {
		return err
	}
	if created {
		log.Printf("newplat bootstrap: systemdefault created (must change password on first login)")
	}
	hub.Identity = svc
	hub.Onboarding = ob
	hub.AgentCredentialOnly = true
	hub.PublicOrigin = cfg.publicOrigin

	n3 := &httpapi.N3{OB: ob, PublicOrigin: cfg.publicOrigin, TrustProxy: cfg.trustProxy, Secure: true}
	api := &httpapi.API{Svc: svc, Actor: n3.Actor, PublicOrigin: cfg.publicOrigin, TrustProxy: cfg.trustProxy, Online: hub.AgentOnline}
	n3.Register(mux)
	api.Register(mux)
	mux.HandleFunc("GET /ws/agent", hub.HandleAgentWS)
	return nil
}
