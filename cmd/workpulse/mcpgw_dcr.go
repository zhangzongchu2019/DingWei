package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/lifecycle"
	"github.com/zhangzongchu2019/dingwei/internal/mcpgw"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

func readDCRPolicy() (mcpgw.DCRPolicy, error) {
	p := mcpgw.DCRPolicy{BoundTTL: 30 * 24 * time.Hour}
	if raw := os.Getenv("DW_MCP_DCR_ENABLED"); raw != "" {
		v, e := strconv.ParseBool(raw)
		if e != nil {
			return p, errors.New("invalid DCR enabled flag")
		}
		p.Enabled = v
	}
	if raw := os.Getenv("DW_MCP_DCR_BOUND_TTL"); raw != "" {
		v, e := time.ParseDuration(raw)
		if e != nil || v < 7*24*time.Hour || v > 90*24*time.Hour {
			return p, errors.New("invalid DCR bound TTL")
		}
		p.BoundTTL = v
	}
	if raw := os.Getenv("DW_MCP_DCR_HTTPS_REDIRECTS_JSON"); raw != "" {
		if len(raw) > 16384 || json.Unmarshal([]byte(raw), &p.HTTPSRedirects) != nil {
			return p, errors.New("invalid DCR HTTPS redirect list")
		}
	}
	if raw := os.Getenv("DW_MCP_DCR_TRUSTED_PROXIES"); raw != "" {
		for _, v := range strings.Split(raw, ",") {
			prefix, e := netip.ParsePrefix(v)
			if e != nil {
				return p, errors.New("invalid DCR trusted proxy")
			}
			p.TrustedProxies = append(p.TrustedProxies, prefix)
		}
	}
	return p, nil
}

func startDCRGC(ctx context.Context, d *mcpgw.DCR) bool {
	done := func() {}
	if jobs, ok := ctx.Value(runTasksKey{}).(*lifecycle.Group); ok {
		var admitted bool
		done, admitted = jobs.Enter("mcpgw_dcr_gc")
		if !admitted {
			d.Store = nil
			return false
		}
	}
	go func() { defer done(); d.RunGC(ctx) }()
	return true
}

// Invalid DCR settings only disable this optional capability, never CIMD.
func configuredDCR(ctx context.Context, pool *repo.Pool, access repo.AccessCtx, resource string, key []byte) *mcpgw.DCR {
	p, e := readDCRPolicy()
	if e != nil {
		log.Print("mcpgw DCR unavailable: invalid configuration")
		return &mcpgw.DCR{Policy: mcpgw.DCRPolicy{Enabled: true}}
	}
	d, e := mcpgw.NewDCR(p, nil)
	if e != nil {
		log.Print("mcpgw DCR unavailable: invalid redirect policy")
		return &mcpgw.DCR{Policy: mcpgw.DCRPolicy{Enabled: p.Enabled}}
	}
	store, e := mcpgw.NewSQLDynamicClients(ctx, pool, access, resource, key, p.BoundTTL)
	if e != nil {
		if p.Enabled {
			log.Print("mcpgw DCR unavailable: missing schema or database")
		}
		return d
	}
	d.Store = store
	return d
}
