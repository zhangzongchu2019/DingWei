package identity_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

func TestDeviceClaimCancellation(t *testing.T) {
	for _, backend := range []string{"memory", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			for _, mode := range []string{"pending", "expired", "issued", "completed", "race"} {
				t.Run(mode, func(t *testing.T) {
					var store identity.Store = identity.NewMemStore()
					if backend == "postgres" {
						store = pgFactory(t)(t)
					}
					ctx := context.Background()
					now := time.Now()
					ob := identity.NewOnboarding(identity.NewService(store, func() time.Time { return now }, nil))
					ob.CodeKey = "synthetic-cancel-key"
					code := "ABCDEFGHJKMN"
					r, err := ob.ApplyDeviceClaim(ctx, identity.ClaimRequest{UserID: "alice", ShortName: "al", AgentName: "desktop", CodeDigest: identity.HashSecret(code), DeviceFingerprint: strings.Repeat("a", 64), ClientVersion: "0.2.2.2", Mode: "desktop"}, "fixture-ip")
					if err != nil {
						t.Fatal(err)
					}
					st := store.(identity.ClaimStore)
					if err := ob.CancelDeviceClaim(ctx, r.ID, "wrong"); !errors.Is(err, identity.ErrNotFound) {
						t.Fatal("wrong token could cancel")
					}
					sender := identity.Identity{Provider: "feishu", Issuer: "fixture", Subject: "person"}
					if mode == "issued" || mode == "completed" {
						ob.HandleDeviceClaim(ctx, sender, code)
					}
					if mode == "completed" {
						if err := ob.AckDeviceClaim(ctx, r.ID, r.PollToken); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "expired" {
						now = now.Add(identity.ClaimTTL)
					}
					if mode == "race" {
						var wg sync.WaitGroup
						wg.Add(2)
						go func() { defer wg.Done(); ob.HandleDeviceClaim(ctx, sender, code) }()
						go func() { defer wg.Done(); _ = ob.CancelDeviceClaim(ctx, r.ID, r.PollToken) }()
						wg.Wait()
						c, err := st.GetDeviceClaim(ctx, r.ID)
						if err != nil {
							t.Fatal(err)
						}
						users, err := store.CountUsers(ctx)
						if err != nil {
							t.Fatal(err)
						}
						agents, err := store.ListAgents(ctx, "alice")
						if err != nil {
							t.Fatal(err)
						}
						if c.State == "cancelled" {
							if users != 0 || len(agents) != 0 || c.AgentID != "" || c.CredentialHash != "" {
								t.Fatal("cancelled claim created identity/agent")
							}
						} else if c.State == "credential_ready" {
							if users != 1 || len(agents) != 1 || c.AgentID != agents[0].ID {
								t.Fatal("claimed race partially committed")
							}
							if err := ob.CancelDeviceClaim(ctx, r.ID, r.PollToken); !errors.Is(err, identity.ErrNotAllowed) {
								t.Fatal("issued race could cancel")
							}
						} else {
							t.Fatal("unexpected concurrent outcome")
						}
						return
					}
					err = ob.CancelDeviceClaim(ctx, r.ID, r.PollToken)
					if mode == "issued" || mode == "completed" {
						if !errors.Is(err, identity.ErrNotAllowed) {
							t.Fatal("issued credential was cancelled")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := ob.CancelDeviceClaim(ctx, r.ID, r.PollToken); err != nil {
						t.Fatal("cancel not idempotent")
					}
					ob.HandleDeviceClaim(ctx, sender, code)
					c, err := st.GetDeviceClaim(ctx, r.ID)
					if err != nil || c.State != "cancelled" || c.AgentID != "" {
						t.Fatal("cancelled code replay consumed claim")
					}
					users, err := store.CountUsers(ctx)
					if err != nil || users != 0 {
						t.Fatal("cancel created user")
					}
				})
			}
		})
	}
}
