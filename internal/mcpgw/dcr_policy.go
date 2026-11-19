package mcpgw

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const dcrPrefix = "dw-dcr-"
const dcrUnboundTTL = 24 * time.Hour

// DCRPolicy is server configuration, never registration metadata.
type DCRPolicy struct {
	Enabled        bool
	BoundTTL       time.Duration
	HTTPSRedirects []string
	TrustedProxies []netip.Prefix
}

func (p *DCRPolicy) validate() error {
	if p.BoundTTL == 0 {
		p.BoundTTL = 30 * 24 * time.Hour
	}
	if p.BoundTTL < 7*24*time.Hour || p.BoundTTL > 90*24*time.Hour {
		return errors.New("mcpgw: invalid DCR bound lifetime")
	}
	for _, raw := range p.HTTPSRedirects {
		u, e := url.Parse(raw)
		if e != nil || !dcrHTTPS(u) {
			return errors.New("mcpgw: invalid DCR HTTPS allowlist")
		}
	}
	for _, prefix := range p.TrustedProxies {
		if !prefix.IsValid() {
			return errors.New("mcpgw: invalid DCR proxy prefix")
		}
	}
	return nil
}

type dcrRedirectError struct{ Kind, Scheme string }

func (e *dcrRedirectError) Error() string { return "invalid_redirect_uri" }
func filteredScheme(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.' {
			if b.Len() == 32 {
				break
			}
			b.WriteByte(byte(c))
		}
	}
	return b.String()
}
func validPort(u *url.URL) bool {
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if u.Port() == "" {
		return true
	}
	n, e := strconv.Atoi(u.Port())
	return e == nil && n > 0 && n <= 65535
}
func cleanDCRURL(u *url.URL) bool {
	return u != nil && u.Host != "" && u.User == nil && u.Fragment == "" && u.Opaque == "" && validPort(u) && !strings.ContainsAny(u.Host, "%*\\")
}
func dcrHTTPS(u *url.URL) bool {
	if !cleanDCRURL(u) || u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if _, e := netip.ParseAddr(h); e == nil {
		return false
	}
	if !strings.Contains(h, ".") || strings.HasSuffix(h, ".") || h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") {
		return false
	}
	for _, c := range h {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return false
		}
	}
	labels := strings.Split(h, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	if len(h) > 253 || labels[len(labels)-1][0] < 'a' || labels[len(labels)-1][0] > 'z' {
		return false
	}
	return true
}
func (p DCRPolicy) redirect(raw string) error {
	fail := func(k, s string) error { return &dcrRedirectError{Kind: k, Scheme: filteredScheme(s)} }
	if len(raw) == 0 || len(raw) > 2048 || strings.Contains(raw, "#") {
		return fail("malformed", "")
	}
	u, e := url.Parse(raw)
	if e != nil {
		return fail("malformed", "")
	}
	if u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return fail("custom_scheme", u.Scheme)
	}
	if !cleanDCRURL(u) {
		return fail("malformed", "")
	}
	if u.Scheme == "http" {
		h := strings.ToLower(u.Hostname())
		if h == "127.0.0.1" || h == "::1" || h == "localhost" {
			return nil
		}
		return fail("loopback_bad", "")
	}
	if u.Scheme == "https" {
		if dcrHTTPS(u) {
			for _, allowed := range p.HTTPSRedirects {
				if raw == allowed {
					return nil
				}
			}
		}
		return fail("https_unlisted", "")
	}
	if u.Scheme != "" {
		return fail("custom_scheme", u.Scheme)
	}
	return fail("malformed", "")
}

// Walk tokens before unmarshalling: encoding/json otherwise silently accepts
// duplicate security metadata. The byte bound is enforced by the HTTP caller.
func dcrJSON(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("invalid_client_metadata")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 16 {
			return errors.New("depth")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		v, composite := t.(json.Delim)
		if !composite {
			return nil
		}
		switch v {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("duplicate")
				}
				seen[key] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		default:
			return errors.New("delimiter")
		}
		_, e = d.Token()
		return e
	}
	if e := walk(0); e != nil {
		return nil, e
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(raw, &fields); e != nil || fields == nil {
		return nil, errors.New("object required")
	}
	return fields, nil
}
func (p DCRPolicy) metadata(raw []byte) (Client, error) {
	c := Client{Name: "Public MCP client", AuthMethod: "none", Grants: []string{"authorization_code"}, Responses: []string{"code"}}
	f, e := dcrJSON(raw)
	if e != nil {
		return c, errors.New("invalid_client_metadata")
	}
	for _, forbidden := range []string{"client_id", "client_secret", "client_secret_expires_at", "registration_access_token", "registration_client_uri", "jwks", "jwks_uri", "software_statement"} {
		if _, ok := f[forbidden]; ok {
			return c, errors.New("invalid_client_metadata")
		}
	}
	var method string
	if json.Unmarshal(f["token_endpoint_auth_method"], &method) != nil || method != "none" {
		return c, errors.New("invalid_client_metadata")
	}
	for _, item := range []struct{ key, value string }{{"grant_types", "authorization_code"}, {"response_types", "code"}} {
		if b, ok := f[item.key]; ok {
			var values []string
			if json.Unmarshal(b, &values) != nil || len(values) != 1 || values[0] != item.value {
				return c, errors.New("invalid_client_metadata")
			}
		}
	}
	if b, ok := f["scope"]; ok {
		var scope string
		if json.Unmarshal(b, &scope) != nil || !fixedScopes(scope) {
			return c, errors.New("invalid_client_metadata")
		}
	}
	if b, ok := f["client_name"]; ok {
		var name string
		if json.Unmarshal(b, &name) != nil || utf8.RuneCountInString(name) > 128 || name == "" {
			return c, errors.New("invalid_client_metadata")
		}
		c.Name = name
		for _, r := range c.Name {
			if unicode.IsControl(r) {
				return c, errors.New("invalid_client_metadata")
			}
		}
	}
	if json.Unmarshal(f["redirect_uris"], &c.Redirects) != nil || len(c.Redirects) < 1 || len(c.Redirects) > 4 {
		return c, errors.New("invalid_client_metadata")
	}
	seen := map[string]bool{}
	for _, uri := range c.Redirects {
		if seen[uri] {
			return c, errors.New("invalid_client_metadata")
		}
		seen[uri] = true
		if e = p.redirect(uri); e != nil {
			return c, e
		}
	}
	return c, nil
}
