package main

import (
	"context"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
	"github.com/zhangzongchu2019/dingwei/internal/mcpgw"
)

func TestDCRConfigurationBoundLifetimeAndOptionalIsolation(t *testing.T) {
	for _, k := range []string{"DW_MCP_DCR_ENABLED", "DW_MCP_DCR_BOUND_TTL", "DW_MCP_DCR_HTTPS_REDIRECTS_JSON", "DW_MCP_DCR_TRUSTED_PROXIES"} {
		t.Setenv(k, "")
	}
	p, e := readDCRPolicy()
	if e != nil || p.Enabled || p.BoundTTL != 30*24*time.Hour {
		t.Fatal("unsafe DCR defaults", e)
	}
	for _, raw := range []string{"167h59m59.999999999s", "2160h0.000000001s", "-1h", "garbage"} {
		t.Setenv("DW_MCP_DCR_BOUND_TTL", raw)
		if _, e = readDCRPolicy(); e == nil {
			t.Fatalf("invalid DCR lifetime accepted %q", raw)
		}
	}
	for _, raw := range []string{"168h", "2160h"} {
		t.Setenv("DW_MCP_DCR_BOUND_TTL", raw)
		p, e = readDCRPolicy()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = mcpgw.NewDCR(p, nil); e != nil {
			t.Fatal(e)
		}
	}
	t.Setenv("DW_MCP_DCR_BOUND_TTL", "")
	t.Setenv("DW_MCP_DCR_ENABLED", "true")
	t.Setenv("DW_MCP_DCR_TRUSTED_PROXIES", "10.0.0.0/8,2001:db8::/32")
	p, e = readDCRPolicy()
	if e != nil || !p.Enabled || len(p.TrustedProxies) != 2 {
		t.Fatal("explicit trusted proxies not parsed", e)
	}
	t.Setenv("DW_MCP_DCR_TRUSTED_PROXIES", "not-a-cidr")
	if _, e = readDCRPolicy(); e == nil {
		t.Fatal("bad trusted proxy accepted")
	}
	t.Setenv("DW_MCP_DCR_TRUSTED_PROXIES", "")
	t.Setenv("DW_MCP_DCR_HTTPS_REDIRECTS_JSON", `["https://*.vendor.example/cb"]`)
	p, e = readDCRPolicy()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = mcpgw.NewDCR(p, nil); e == nil {
		t.Fatal("wildcard DCR callback configured")
	}
}

func TestDCRGCUsesShutdownLifecycle(t *testing.T) {
	jobs := &lifecycle.Group{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), runTasksKey{}, jobs))
	d, _ := mcpgw.NewDCR(mcpgw.DCRPolicy{}, nil)
	if !startDCRGC(ctx, d) {
		t.Fatal("GC not admitted")
	}
	cancel()
	wait, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if e := jobs.Wait(wait); e != nil {
		t.Fatal("GC leaked shutdown work", e)
	}
	jobs.Seal()
	if startDCRGC(ctx, d) {
		t.Fatal("GC admitted after background group was sealed")
	}
}

func TestDCRExactHTTPSConfigurationDoesNotSplitURICommas(t *testing.T) {
	t.Setenv("DW_MCP_DCR_ENABLED", "true")
	t.Setenv("DW_MCP_DCR_BOUND_TTL", "")
	t.Setenv("DW_MCP_DCR_TRUSTED_PROXIES", "")
	t.Setenv("DW_MCP_DCR_HTTPS_REDIRECTS_JSON", `["https://vendor.example/cb?value=a,b"]`)
	p, e := readDCRPolicy()
	if e != nil || len(p.HTTPSRedirects) != 1 || p.HTTPSRedirects[0] != "https://vendor.example/cb?value=a,b" {
		t.Fatal("exact callback URL was split or changed", e)
	}
	if _, e = mcpgw.NewDCR(p, nil); e != nil {
		t.Fatal(e)
	}
	t.Setenv("DW_MCP_DCR_HTTPS_REDIRECTS_JSON", `[1]`)
	if _, e = readDCRPolicy(); e == nil {
		t.Fatal("invalid HTTPS redirect configuration accepted")
	}
}
