package main

// 业主反馈（板 7a138dcf）的接线：WP_SECRET_KEY 经 newPlatformConfigFromEnv 进 cfg.codeKey，
// wireNewPlatform 交给 Hub 用的那个 Onboarding（列用户时据此取回当前有效注册码）。

import (
	"context"
	"net/http"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

func TestNewPlatformRegistrationCodeKeyWired(t *testing.T) {
	t.Setenv("DW_NEWPLAT", "1")
	t.Setenv("DW_PUBLIC_ORIGIN", "https://x")
	t.Setenv("DW_NEWPLAT_PG_DSN", "postgres://unused")
	t.Setenv("WP_SECRET_KEY", "k-wired")
	cfg, on, err := newPlatformConfigFromEnv()
	if err != nil || !on || cfg.codeKey != "k-wired" {
		t.Fatalf("config: on=%v err=%v codeKey=%q", on, err, cfg.codeKey)
	}
	hub := newplatHub(t)
	if err := wireNewPlatform(context.Background(), http.NewServeMux(), hub, cfg, identity.NewMemStore()); err != nil {
		t.Fatal(err)
	}
	if hub.Onboarding == nil || hub.Onboarding.CodeKey != "k-wired" {
		t.Fatal("the Hub's Onboarding must carry WP_SECRET_KEY as CodeKey")
	}
}
