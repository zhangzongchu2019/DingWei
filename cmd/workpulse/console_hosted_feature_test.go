package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// R-V2-FEATURE-HOSTED-ON-20261004004703: the page feature bit follows the
// real wireNewPlatform → wireHosted result. It is not inferred from config,
// pool presence, or a browser-side 404 probe.
func TestConsoleHostedFeatureFollowsRealAssembly(t *testing.T) {
	t.Run("registered routes render true", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		masterKey := filepath.Join(t.TempDir(), "master.key")
		if err := os.WriteFile(masterKey, []byte(strings.Repeat("k", 32)), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("DW_G4_MASTER_KEY_FILE", masterKey)

		mux := http.NewServeMux()
		hub := newplatHub(t)
		if err := wireNewPlatform(ctx, mux, hub, newPlatformConfig{publicOrigin: "https://x"}, identity.NewMemStore(), pgtest.Pool(t)); err != nil {
			t.Fatalf("wire new platform: %v", err)
		}
		assertHostedRouteState(t, mux, true)
		assertConsoleHostedFeature(t, hub, true)
	})

	t.Run("configured but not assembled renders false", func(t *testing.T) {
		// Even a configured key must not enable the page when the hosted assembly
		// is skipped (nil pool). This kills config/env-based inference.
		t.Setenv("DW_G4_MASTER_KEY_FILE", filepath.Join(t.TempDir(), "configured-but-unused.key"))
		mux := http.NewServeMux()
		hub := newplatHub(t)
		if err := wireNewPlatform(context.Background(), mux, hub, newPlatformConfig{publicOrigin: "https://x"}, identity.NewMemStore(), nil); err != nil {
			t.Fatalf("wire new platform: %v", err)
		}
		assertHostedRouteState(t, mux, false)
		assertConsoleHostedFeature(t, hub, false)
	})

	t.Run("old platform renders false", func(t *testing.T) {
		assertConsoleHostedFeature(t, newplatHub(t), false)
	})

	t.Run("failed assembly never renders true", func(t *testing.T) {
		t.Setenv("DW_G4_MASTER_KEY_FILE", filepath.Join(t.TempDir(), "missing.key"))
		mux := http.NewServeMux()
		hub := newplatHub(t)
		err := wireNewPlatform(context.Background(), mux, hub, newPlatformConfig{publicOrigin: "https://x"}, identity.NewMemStore(), pgtest.Pool(t))
		if err == nil {
			t.Fatal("unreadable hosted master key must refuse assembly")
		}
		assertHostedRouteState(t, mux, false)
		assertConsoleHostedFeature(t, hub, false)
	})
}

func assertHostedRouteState(t *testing.T, mux *http.ServeMux, want bool) {
	t.Helper()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/console/hosted/options", nil))
	if want && rr.Code == http.StatusNotFound {
		t.Fatal("hosted feature true but hosted options route is not registered")
	}
	if !want && rr.Code != http.StatusNotFound {
		t.Fatalf("hosted feature false but hosted options route returned %d, want 404", rr.Code)
	}
}

func assertConsoleHostedFeature(t *testing.T, hub *m8.Hub, want bool) {
	t.Helper()
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	needle := "var FEATURE_HOSTED='false'==='true';"
	if want {
		needle = "var FEATURE_HOSTED='true'==='true';"
	}
	body := rr.Body.String()
	if !strings.Contains(body, needle) {
		t.Fatalf("console page missing %q", needle)
	}
	if strings.Contains(body, "{{FEATURE_HOSTED}}") {
		t.Fatal("console page retained FEATURE_HOSTED placeholder")
	}
}
