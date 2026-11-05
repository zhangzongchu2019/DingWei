package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// Immutable startup policy. Its zero value denies every human request; roles,
// quotas, request headers and future registrations never add allowlist members.
type hostedAccess struct {
	users  map[string]struct{}
	mode   string
	active func(*http.Request, string) bool
}

func parseHostedAccess(raw string) (hostedAccess, error) {
	var p hostedAccess
	if strings.TrimSpace(raw) == "" {
		return p, nil
	}
	p.users = make(map[string]struct{})
	for _, token := range strings.Split(raw, ",") {
		id := strings.TrimSpace(token)
		if !identity.ValidUserID(id) || identity.Lower(id) != id {
			return hostedAccess{}, errors.New("DW_G4_ACCESS_ALLOW_USER_IDS requires canonical lowercase user IDs")
		}
		p.users[id] = struct{}{}
	}
	return p, nil
}

func parseHostedAccessMode(mode, raw string) (hostedAccess, error) {
	p, err := parseHostedAccess(raw)
	if err != nil {
		return p, err
	}
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "allowlist"
	}
	if mode != "allowlist" && mode != "all_active_users" {
		return hostedAccess{}, errors.New("DW_G4_ACCESS_MODE must be allowlist or all_active_users")
	}
	p.mode = mode
	return p, nil
}

func (p hostedAccess) human(r *http.Request, actor func(*http.Request) (string, error)) (string, bool) {
	// Human authority is cookie/session-only. An Agent bearer must not inherit
	// its owner's permission, even when the request also carries an owner cookie.
	if actor == nil || len(r.Header.Values("Authorization")) != 0 {
		return "", false
	}
	id, err := actor(r)
	if err != nil {
		return "", false
	}
	if p.active != nil && !p.active(r, id) {
		return "", false
	}
	if p.mode == "all_active_users" {
		if p.active == nil {
			return "", false
		}
		return id, true
	}
	_, ok := p.users[id]
	return id, ok
}

func hostedForbidden(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
}

type hostedRouteMux interface {
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
}

// Every human route registered through this adapter is fenced before its body,
// resource lookup, origin check or business side effect. Node routes stay on the
// original registrar and retain their dedicated credential checks.
type hostedHumanMux struct {
	mux    hostedRouteMux
	access hostedAccess
	actor  func(*http.Request) (string, error)
	renew  func(http.ResponseWriter, *http.Request) error
}

func (m hostedHumanMux) HandleFunc(pattern string, next func(http.ResponseWriter, *http.Request)) {
	m.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if m.renew != nil {
			if err := m.renew(w, r); err != nil {
				hostedForbidden(w)
				return
			}
		}
		if _, ok := m.access.human(r, m.actor); !ok {
			hostedForbidden(w)
			return
		}
		next(w, r)
	})
}
