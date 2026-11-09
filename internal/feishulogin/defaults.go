package feishulogin

import (
	"context"
	"strings"
	"time"

	"github.com/mozillazg/go-pinyin"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
)

// DefaultTargets is server configuration plus authoritative identity lookups.
// Allowed must return a copy; it never enumerates online sessions. Recent is a
// preference only and can be consulted only after the subject is authenticated.
type DefaultTargets interface {
	Allowed(tenant string) []string
	Valid(context.Context, string) bool
	Recent(context.Context, extshared.Principal) (string, error)
}

func letters(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if c >= 'a' && c <= 'z' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// AutomaticShortName uses the trusted raw profile, not client-supplied display
// names. go-pinyin's frozen first pronunciation gives deterministic initials.
func AutomaticShortName(p UserInfo, now time.Time) string {
	name := letters(p.EnName)
	if name == "" {
		a := pinyin.NewArgs()
		a.Style = pinyin.FirstLetter
		name = letters(strings.Join(pinyin.LazyPinyin(p.Name, a), ""))
	}
	if name == "" {
		name = "pm"
	}
	if len(name) > 8 {
		name = name[:8]
	}
	return name + now.UTC().Format("060102150405")
}

func (h *Handler) allowedTarget(ctx context.Context, target string) bool {
	if h.defaults == nil || !validTargetAgentFullName(target) {
		return false
	}
	for _, name := range h.defaults.Allowed(h.teamTenantKey) {
		if name == target {
			return h.defaults.Valid(ctx, target)
		}
	}
	return false
}
func (h *Handler) validTargets(ctx context.Context) []string {
	var out []string
	if h.defaults != nil {
		for _, name := range h.defaults.Allowed(h.teamTenantKey) {
			if h.allowedTarget(ctx, name) {
				out = append(out, name)
			}
		}
	}
	return out
}
func (h *Handler) prepareSelection(ctx context.Context, a LoginAttempt, targets []string) (CallbackResult, error) {
	if len(targets) == 0 {
		return CallbackResult{}, ErrInvalidStart
	}
	recent, err := h.defaults.Recent(ctx, a.Request.Principal)
	if err != nil {
		return CallbackResult{}, ErrInvalidStart
	}
	preferred := ""
	for _, target := range targets {
		if strings.EqualFold(recent, target) {
			preferred = target
		}
	}
	token, err := h.newState()
	if err != nil {
		return CallbackResult{}, ErrInvalidState
	}
	a.StateHash = hashState(token)
	a.Purpose = StatePurposeSelection
	// Preserve original expiry and the one generated name across the selection.
	if !h.now().Before(a.ExpiresAt) {
		return CallbackResult{}, ErrStateExpired
	}
	if err = h.states.Create(ctx, a); err != nil {
		return CallbackResult{}, err
	}
	return CallbackResult{AttemptID: a.Request.AttemptID, Selection: token, Targets: targets, Preferred: preferred}, nil
}
func (h *Handler) selectTarget(ctx context.Context, req CallbackRequest) (CallbackResult, error) {
	a, err := h.states.Consume(ctx, hashState(req.Selection), h.now())
	if err != nil {
		return CallbackResult{}, err
	}
	if a.Purpose != StatePurposeSelection || a.AppID != h.appID || a.RedirectURI != h.redirectURI || !a.Request.Automatic || a.Request.Principal.TenantKey != h.teamTenantKey || a.Request.Principal.Subject == "" || !extshared.ValidShortName(a.Request.ShortName) || !h.allowedTarget(ctx, req.Target) {
		return CallbackResult{}, ErrInvalidStart
	}
	a.Request.TargetAgent = req.Target
	return h.issue(ctx, a)
}
