// Package trustedorigin parses and evaluates the additional exact HTTPS
// origins accepted by browser-facing write handlers.
package trustedorigin

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Origin struct {
	Scheme string
	Host   string // lowercase DNS name or bracketed IPv6 literal
	Port   string // canonical decimal port
}

func ParseList(raw string) ([]Origin, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]Origin, 0, len(parts))
	seen := make(map[Origin]bool, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("DW_TRUSTED_ORIGINS: empty element at position %d", i+1)
		}
		o, err := Parse(part)
		if err != nil {
			return nil, fmt.Errorf("DW_TRUSTED_ORIGINS: invalid origin at position %d: %w", i+1, err)
		}
		if seen[o] {
			return nil, fmt.Errorf("DW_TRUSTED_ORIGINS: duplicate origin after normalization at position %d", i+1)
		}
		seen[o] = true
		out = append(out, o)
	}
	return out, nil
}

func Parse(raw string) (Origin, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return Origin{}, fmt.Errorf("malformed URL")
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Opaque != "" || u.User != nil || u.Host == "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return Origin{}, fmt.Errorf("expected an exact HTTPS origin without userinfo, path, query, or fragment")
	}
	host, port, err := splitHostPort(u.Host)
	if err != nil {
		return Origin{}, err
	}
	return Origin{Scheme: "https", Host: host, Port: port}, nil
}

func splitHostPort(authority string) (string, string, error) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || host == "" || port == "" {
		return "", "", fmt.Errorf("explicit host and decimal port are required")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return "", "", fmt.Errorf("port must be canonical decimal in range 1..65535")
	}
	if strings.Contains(host, "%") {
		return "", "", fmt.Errorf("IPv6 zone identifiers are not allowed")
	}
	if strings.Contains(host, ":") {
		ip := net.ParseIP(host)
		if ip == nil || !strings.HasPrefix(authority, "[") {
			return "", "", fmt.Errorf("invalid bracketed IPv6 host")
		}
		return "[" + strings.ToLower(host) + "]", port, nil
	}
	if len(host) > 253 {
		return "", "", fmt.Errorf("host is too long")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", "", fmt.Errorf("invalid ASCII DNS hostname")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return "", "", fmt.Errorf("host must be an ASCII DNS name or IPv6 literal")
			}
		}
	}
	return strings.ToLower(host), port, nil
}

// Allows evaluates the supplied per-handler legacy result when the list is
// empty. With entries present, it keeps the exact canonical PublicOrigin and
// also accepts a normalized exact tuple from the additional list.
func Allows(raw, publicOrigin string, trusted []Origin, legacyAllowed bool, headerValues int) bool {
	if len(trusted) == 0 {
		return legacyAllowed
	}
	if headerValues != 1 || strings.Contains(raw, ",") || strings.TrimSpace(raw) != raw {
		return false
	}
	if publicOrigin != "" && raw == publicOrigin {
		return true
	}
	o, err := Parse(raw)
	if err != nil {
		return false
	}
	for _, item := range trusted {
		if item == o {
			return true
		}
	}
	return false
}

// AuthorityMatches compares a parsed Origin authority with Request.Host. The
// legacy path remains byte-for-byte when trusted origins are not configured.
func AuthorityMatches(originAuthority, requestHost string, normalize bool) bool {
	if !normalize {
		return originAuthority == requestHost
	}
	left, err1 := parseAuthority(originAuthority)
	right, err2 := parseAuthority(requestHost)
	return err1 == nil && err2 == nil && left == right
}

func parseAuthority(authority string) (Origin, error) {
	// Request.Host and URL.Host are authorities, not complete origins. Port may
	// be omitted on the canonical default port, which must compare as omitted.
	if strings.HasPrefix(authority, "[") {
		return parseBracketedAuthority(authority)
	}
	if strings.Count(authority, ":") > 1 {
		return Origin{}, fmt.Errorf("invalid authority")
	}
	host := authority
	port := ""
	if i := strings.LastIndexByte(authority, ':'); i >= 0 {
		host, port = authority[:i], authority[i+1:]
		if port == "" {
			return Origin{}, fmt.Errorf("empty port")
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return Origin{}, fmt.Errorf("invalid port")
		}
	}
	if host == "" {
		return Origin{}, fmt.Errorf("empty host")
	}
	return Origin{Host: strings.ToLower(host), Port: port}, nil
}

func parseBracketedAuthority(authority string) (Origin, error) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || net.ParseIP(host) == nil || port == "" {
		return Origin{}, fmt.Errorf("invalid bracketed authority")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
		return Origin{}, fmt.Errorf("invalid port")
	}
	return Origin{Host: "[" + strings.ToLower(host) + "]", Port: port}, nil
}
