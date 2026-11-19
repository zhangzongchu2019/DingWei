package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

func (a *API) adminAgents(w http.ResponseWriter, r *http.Request) {
	// A bearer must never inherit a coincident human cookie's administrator role.
	if len(r.Header.Values("Authorization")) != 0 {
		writeErr(w, 403, "forbidden", nil)
		return
	}
	if c, err := r.Cookie(SessionCookie); err != nil || c.Value == "" {
		writeErr(w, 401, "unauthenticated", nil)
		return
	}
	a.console(a.listAdminAgents)(w, r)
}
func (a *API) listAdminAgents(w http.ResponseWriter, r *http.Request, actor string) {
	u, err := a.Svc.Store.GetUser(r.Context(), actor)
	if err != nil || u == nil || u.State != identity.UserActive || u.Type != identity.UserPlatformAdmin {
		writeErr(w, 403, "forbidden", nil)
		return
	}
	query := r.URL.Query()
	limit := 100
	if len(query["limit"]) > 1 || len(query["after"]) > 1 {
		writeErr(w, 400, "bad_request", nil)
		return
	}
	if values, ok := query["limit"]; ok {
		limit, err = strconv.Atoi(values[0])
		if err != nil {
			writeErr(w, 400, "bad_request", nil)
			return
		}
	}
	after := query.Get("after")
	if limit < 1 || limit > identity.AdminAgentPageMax || len(after) > 128 || strings.ContainsRune(after, 0) {
		writeErr(w, 400, "bad_request", nil)
		return
	}
	st, ok := a.Svc.Store.(identity.AdminAgentStore)
	if !ok {
		writeErr(w, 503, "unavailable", nil)
		return
	}
	page, err := st.ListAdminAgents(r.Context(), actor, after, limit, a.Svc.Now())
	if err != nil {
		mapErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(page.Agents))
	for _, row := range page.Agents {
		online := false
		if a.Online != nil {
			online = a.Online(row.ID)
		}
		fullName := row.Name
		if row.OwnerShortName != "" {
			fullName += "." + row.OwnerShortName
		}
		item := map[string]any{"agent_id": row.ID, "owner": map[string]string{"user_id": row.OwnerID, "short_name": row.OwnerShortName, "display_name": row.OwnerDisplayName}, "full_name": fullName, "agent_type": string(row.Origin), "state": string(row.State), "online": online, "host": ""}
		if row.Origin == identity.OriginExternal && extshared.ExternalDisplayNamesEnabled() {
			item["display_name"] = row.DisplayName
			item["display_label"] = extshared.ExternalDisplayLabel(row.DisplayName, fullName)
		}
		if row.Origin == identity.OriginSelfhost {
			item["host"] = row.SourceIP
			if !row.SourceAt.IsZero() {
				item["last_source_at"] = row.SourceAt.UTC().Format(time.RFC3339Nano)
			}
		} else if row.Origin == identity.OriginHosted {
			item["host"] = row.HostedNodeName
		}
		out = append(out, item)
	}
	writeJSON(w, 200, map[string]any{"agents": out, "next_after": page.NextAfter, "limit": limit})
}
