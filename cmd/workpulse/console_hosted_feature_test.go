package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

// R-V2-FEATURE-HOSTED-ON-20261004004703: the page feature bit is a consequence of
// mounting the real hosted routes, not a front-end constant and not a 404 probe.
func TestConsoleHostedFeatureFollowsRouteRegistration(t *testing.T) {
	t.Run("registered routes render true", func(t *testing.T) {
		mux := http.NewServeMux()
		hub := &m8.Hub{}
		registerHostedRoutesAndEnablePage(mux, hub, hostedDeps{})

		_, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, "/api/console/hosted/options", nil))
		if pattern != "GET /api/console/hosted/options" {
			t.Fatalf("hosted route was not actually registered: pattern=%q", pattern)
		}
		assertConsoleHostedFeature(t, hub, true)
	})

	t.Run("unregistered routes render false", func(t *testing.T) {
		mux := http.NewServeMux()
		hub := &m8.Hub{}
		_, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, "/api/console/hosted/options", nil))
		if pattern != "" {
			t.Fatalf("hosted route unexpectedly registered: pattern=%q", pattern)
		}
		assertConsoleHostedFeature(t, hub, false)
	})
}

func assertConsoleHostedFeature(t *testing.T, hub *m8.Hub, want bool) {
	t.Helper()
	rr := httptest.NewRecorder()
	hub.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
	needle := "var FEATURE_HOSTED='false'==='true';"
	if want {
		needle = "var FEATURE_HOSTED='true'==='true';"
	}
	if !strings.Contains(rr.Body.String(), needle) {
		t.Fatalf("console page missing %q", needle)
	}
}
