package mcpgw

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	ID         string   `json:"client_id"`
	Name       string   `json:"client_name"`
	Redirects  []string `json:"redirect_uris"`
	AuthMethod string   `json:"token_endpoint_auth_method"`
	Grants     []string `json:"grant_types"`
	Responses  []string `json:"response_types"`
}
type ClientResolver interface {
	Resolve(context.Context, string) (Client, error)
}

// MetadataClients 通过 HTTPS 获取 CIMD；禁止代理、跨站重定向和私网/DNS 回绑。
// 不读取环境代理或平台凭据，不给请求带任何认证。
type MetadataClients struct{}

func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !ip.IsMulticast() && !specialIP(ip)
}

var specialNetworks = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/32", "2001:db8::/32", "2002::/16"} {
		_, n, _ := net.ParseCIDR(cidr)
		out = append(out, n)
	}
	return out
}()

func specialIP(ip net.IP) bool {
	for _, n := range specialNetworks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func metadataURL(id string) bool {
	u, e := url.Parse(id)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Path != "" && u.Path != "/" && u.Fragment == "" && u.Opaque == "" && (u.Port() == "" || u.Port() == "443")
}
func (MetadataClients) Resolve(ctx context.Context, id string) (Client, error) {
	if !metadataURL(id) {
		return Client{}, errParams
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("CIMD DNS rejected")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, errors.New("CIMD address rejected")
			}
		}
		d := net.Dialer{Timeout: 5 * time.Second}
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	tr := &http.Transport{DialContext: dial, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16384}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("CIMD redirect rejected") }}
	return fetchClient(ctx, id, c)
}
func fetchClient(ctx context.Context, id string, c *http.Client) (Client, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", id, nil)
	if e != nil {
		return Client{}, errParams
	}
	req.Header.Set("Accept", "application/json")
	resp, e := c.Do(req)
	if e != nil {
		return Client{}, errParams
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Client{}, errParams
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(b) > 65536 {
		return Client{}, errParams
	}
	var client Client
	if json.Unmarshal(b, &client) != nil || validateClient(client, id) != nil {
		return Client{}, errParams
	}
	return client, nil
}
func validRedirect(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	// The localhost hostname is never a native callback identity, including
	// HTTPS spelling variants. Other HTTPS redirects retain exact matching.
	ip := net.ParseIP(u.Hostname())
	httpLoopback := ip != nil && ip.IsLoopback()
	return !strings.EqualFold(u.Hostname(), "localhost") && (u.Scheme == "https" || u.Scheme == "http" && httpLoopback)
}
func validateClient(c Client, id string) error {
	if c.ID != id || !metadataURL(id) || strings.TrimSpace(c.Name) == "" || len(c.Redirects) == 0 || (c.AuthMethod != "" && c.AuthMethod != "none") {
		return errParams
	}
	for _, r := range c.Redirects {
		if !validRedirect(r) {
			return errParams
		}
	}
	for _, v := range c.Grants {
		if v != "authorization_code" {
			return errParams
		}
	}
	for _, v := range c.Responses {
		if v != "code" {
			return errParams
		}
	}
	return nil
}
func registered(c Client, redirect string) bool {
	if !validRedirect(redirect) {
		return false
	}
	requested, _ := url.Parse(redirect)
	for _, raw := range c.Redirects {
		if raw == redirect {
			return true
		}
		allowed, err := url.Parse(raw)
		if err != nil || !validRedirect(raw) {
			continue
		}
		host := allowed.Hostname()
		if (host != "127.0.0.1" && host != "::1") || requested.Hostname() != host {
			continue
		}
		// RFC 8252 7.3: ignore only the port of the same loopback IP literal.
		// Preserve query/escaped path and all other URI components exactly.
		if allowed.Scheme != requested.Scheme || strings.SplitN(raw, ":", 2)[0] != strings.SplitN(redirect, ":", 2)[0] || allowed.EscapedPath() != requested.EscapedPath() || allowed.RawQuery != requested.RawQuery || allowed.ForceQuery != requested.ForceQuery {
			continue
		}
		if port := requested.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				continue
			}
		}
		return true
	}
	return false
}
