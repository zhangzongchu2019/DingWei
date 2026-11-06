package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/mcpgw"
)

func TestMCPGatewayRegistrationFailsClosed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/agent", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	if e := registerMCPGatewayRoutes(mux, mcpgw.Config{}); e == nil {
		t.Fatal("incomplete config accepted")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "https://test/mcp", nil))
	if w.Code != 404 {
		t.Fatal("partial routes registered")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "https://test/ws/agent", nil))
	if w.Code != 204 {
		t.Fatal("helper route changed")
	}
}

func TestOptionalMCPGatewayBadKeyDisablesOnlyItsRoutes(t *testing.T) {
	var capture bytes.Buffer
	old := log.Writer()
	log.SetOutput(&capture)
	defer log.SetOutput(old)
	for _, kind := range []string{"missing", "short", "wide"} {
		t.Run(kind, func(t *testing.T) {
			capture.Reset()
			path := filepath.Join(t.TempDir(), "key")
			key := make([]byte, 32)
			if _, e := rand.Read(key); e != nil {
				t.Fatal(e)
			}
			if kind == "short" {
				key = key[:31]
			}
			if kind != "missing" {
				mode := os.FileMode(0600)
				if kind == "wide" {
					mode = 0644
				}
				if e := os.WriteFile(path, key, mode); e != nil {
					t.Fatal(e)
				}
			}
			t.Setenv("DW_MCPGW_OAUTH_KEY_FILE", path)
			mux := http.NewServeMux()
			mux.HandleFunc("/ws/agent", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
			called := false
			on := registerOptionalMCPGateway(context.Background(), mux, nil, repo.AccessCtx{}, "https://test/mcp", func(mcpgw.Store) (mcpgw.Config, error) { called = true; return mcpgw.Config{}, nil })
			if on || called {
				t.Fatal("invalid key enabled OAuth")
			}
			if strings.Count(capture.String(), "mcpgw OAuth disabled") != 1 {
				t.Fatal("missing single startup diagnostic")
			}
			for _, path := range []string{"/mcp", "/oauth/authorize", "/oauth/token"} {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest("GET", "https://test"+path, nil))
				if w.Code != 404 {
					t.Fatal("disabled routes registered")
				}
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", "https://test/ws/agent", nil))
			if w.Code != 204 {
				t.Fatal("key failure affected helper")
			}
		})
	}
}
