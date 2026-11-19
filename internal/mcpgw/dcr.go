package mcpgw

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

type DCR struct {
	Policy DCRPolicy
	Store  DynamicStore
	slots  chan struct{}
}

func NewDCR(policy DCRPolicy, store DynamicStore) (*DCR, error) {
	if e := policy.validate(); e != nil {
		return nil, e
	}
	policy.HTTPSRedirects = append([]string(nil), policy.HTTPSRedirects...)
	policy.TrustedProxies = append([]netip.Prefix(nil), policy.TrustedProxies...)
	return &DCR{Policy: policy, Store: store, slots: make(chan struct{}, 32)}, nil
}
func (d *DCR) ready() bool { return d != nil && d.Policy.Enabled && d.Store != nil }

// RunGC belongs to the caller's shutdown context; no detached per-request work.
func (d *DCR) RunGC(ctx context.Context) {
	if d == nil || d.Store == nil {
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = d.Store.Prune(ctx)
		}
	}
}
func (p DCRPolicy) trustedPeer(ip netip.Addr) bool {
	for _, prefix := range p.TrustedProxies {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
func (p DCRPolicy) source(r *http.Request) (string, error) {
	h, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return "", errRecord
	}
	ip, e := netip.ParseAddr(h)
	if e != nil || ip.Zone() != "" {
		return "", errRecord
	}
	ip = ip.Unmap()
	if p.trustedPeer(ip) {
		v := r.Header.Values("X-Forwarded-For")
		if len(v) > 1 {
			return "", errRecord
		}
		if len(v) == 1 {
			if len(v[0]) > 1024 {
				return "", errRecord
			}
			parts := strings.Split(v[0], ",")
			if len(parts) > 8 {
				return "", errRecord
			}
			// Validate the whole bounded chain before using any member.
			chain := make([]netip.Addr, len(parts))
			for i, part := range parts {
				a, e := netip.ParseAddr(strings.TrimSpace(part))
				if e != nil || a.Zone() != "" {
					return "", errRecord
				}
				chain[i] = a.Unmap()
			}
			for i := len(chain) - 1; i >= 0; i-- {
				if !p.trustedPeer(ip) {
					break
				}
				ip = chain[i]
			}
		}
	}
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String(), nil
	}
	return ip.String(), nil
}
func (g *Gateway) dcrFailure(w http.ResponseWriter, e error) {
	if errors.Is(e, errDCRRate) {
		w.Header().Set("Retry-After", "60")
		oauthError(w, 429, "rate_limited")
		return
	}
	oauthError(w, 503, "temporarily_unavailable")
}
func (g *Gateway) dcrRedirectRejected(e error) {
	var bad *dcrRedirectError
	if errors.As(e, &bad) && g.cfg.Logger != nil {
		g.cfg.Logger.Info("mcpgw DCR redirect rejected", "reason", bad.Kind, "scheme", bad.Scheme)
	}
}
func (g *Gateway) registerClient(w http.ResponseWriter, r *http.Request) {
	// Registration is a short request. Prevent net/http's post-handler drain
	// from waiting on an unread attacker body after any early rejection.
	r.Close = true
	w.Header().Set("Connection", "close")
	d := g.cfg.DCR
	if d == nil || !d.Policy.Enabled {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if !d.ready() {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	select {
	case d.slots <- struct{}{}:
		defer func() { <-d.slots }()
	default:
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	source, e := d.Policy.source(r)
	// Invalid proxy chains still spend a global/source allowance, under a fixed
	// sentinel (not an attacker-controlled bucket) rather than bypassing limits.
	if e != nil {
		if a := d.Store.Admit(r.Context(), "invalid-source"); a != nil {
			g.dcrFailure(w, a)
			return
		}
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	if e = d.Store.Admit(r.Context(), source); e != nil {
		g.dcrFailure(w, e)
		return
	}
	g.guard(func(w http.ResponseWriter, r *http.Request) {
		if !method(w, r, "POST") {
			return
		}
		media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if e != nil || media != "application/json" {
			oauthError(w, 415, "invalid_client_metadata")
			return
		}
		control := http.NewResponseController(w)
		if control.SetReadDeadline(time.Now().Add(3*time.Second)) != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			var large *http.MaxBytesError
			if errors.As(e, &large) {
				oauthError(w, 413, "invalid_client_metadata")
			} else {
				oauthError(w, 400, "invalid_client_metadata")
			}
			return
		}
		_ = control.SetReadDeadline(time.Time{})
		c, e := d.Policy.metadata(raw)
		if e != nil {
			var bad *dcrRedirectError
			if errors.As(e, &bad) {
				g.dcrRedirectRejected(e)
				oauthError(w, 400, "invalid_redirect_uri")
			} else {
				oauthError(w, 400, "invalid_client_metadata")
			}
			return
		}
		c, created, expires, e := d.Store.Register(r.Context(), source, c)
		if e != nil {
			g.dcrFailure(w, e)
			return
		}
		writeJSON(w, 201, map[string]any{"client_id": c.ID, "client_id_issued_at": created.Unix(), "client_id_expires_at": expires.Unix(), "client_name": c.Name, "redirect_uris": c.Redirects, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code"}, "response_types": []string{"code"}, "scope": extshared.ScopeString()})
	})(w, r)
}
