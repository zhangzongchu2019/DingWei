package m8

// MCP spec grants reuse the already-authenticated Agent websocket. The
// model-facing shim never receives the raw Console page capability: Hub gives
// it once to the connected helper, which performs the Agent HTTP request.

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

const (
	specGrantRequestType  = "spec_grant_request"
	specGrantPollType     = "spec_grant_poll"
	specGrantResponseType = "spec_grant_response"
	specGrantRequestTTL   = 10 * time.Minute
)

type specGrantRequest struct {
	ID, OwnerKey, SessionName, AgentID, AgentFullName, PrincipalID string
	Action, SpecID, PageID, State                                  string
	ExpiresAt, CapabilityExpiresAt                                 time.Time
	CapabilitySessionID                                            string
	capability                                                     string // never serialized or logged
}

func validSpecGrantAction(action, specID string) (string, bool) {
	action, specID = strings.ToLower(strings.TrimSpace(action)), strings.TrimSpace(specID)
	switch action {
	case "create":
		return ConsoleAgentCapabilitySpecsNew, specID == ""
	case "update":
		return ConsoleAgentSpecCapabilityPage(specID), specID != ""
	default:
		return "", false
	}
}

func (h *Hub) specGrantResponse(c *sessionClient, requestID string, values map[string]any) model.Envelope {
	meta := map[string]any{"type": specGrantResponseType, "request_id": requestID, "system": true, "no_mirror": true}
	for k, v := range values {
		meta[k] = v
	}
	return model.Envelope{ID: randomHex(16), From: sessionAddress(systemAddressName, firstNonEmpty(cKeyID(c), "unknown")), To: sessionAddress(c.sessionName, c.keyID), TS: h.now().Unix(), Meta: meta}
}

func (h *Hub) handleSpecGrantRequest(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	typ := metaString(env.Meta, "type")
	if typ != specGrantRequestType && typ != specGrantPollType {
		return false
	}
	requestID := strings.TrimSpace(metaString(env.Meta, "request_id"))
	if c == nil || c.conn == nil || requestID == "" || len(requestID) > 128 {
		if c != nil && c.conn != nil {
			_ = c.write(ctx, h.specGrantResponse(c, requestID, map[string]any{"status": "unavailable"}))
		}
		return true
	}
	now := h.now()
	if typ == specGrantRequestType {
		pageID, ok := validSpecGrantAction(metaString(env.Meta, "action"), metaString(env.Meta, "spec_id"))
		if !ok || c.ownerKey == "" || c.sessionName == "" {
			_ = c.write(ctx, h.specGrantResponse(c, requestID, map[string]any{"status": "invalid_request"}))
			return true
		}
		ag, exists := h.newPlatformAgentAt(ctx, c.keyID, c.sessionName)
		if !exists || h.Identity == nil {
			_ = c.write(ctx, h.specGrantResponse(c, requestID, map[string]any{"status": "unavailable"}))
			return true
		}
		id, err := randomConsoleToken()
		if err != nil {
			_ = c.write(ctx, h.specGrantResponse(c, requestID, map[string]any{"status": "unavailable"}))
			return true
		}
		h.specGrantMu.Lock()
		if h.specGrantRequests == nil {
			h.specGrantRequests = map[string]*specGrantRequest{}
		}
		h.specGrantRequests[id] = &specGrantRequest{ID: id, OwnerKey: c.ownerKey, SessionName: c.sessionName, AgentID: ag.ID, AgentFullName: h.Identity.FullName(ctx, ag), PrincipalID: strings.TrimPrefix(c.keyID, "u_"), Action: metaString(env.Meta, "action"), SpecID: metaString(env.Meta, "spec_id"), PageID: pageID, State: "pending", ExpiresAt: now.Add(specGrantRequestTTL)}
		h.specGrantMu.Unlock()
		_ = c.write(ctx, h.specGrantResponse(c, requestID, map[string]any{"status": "approval_required", "grant_request_id": id, "expires_at": now.Add(specGrantRequestTTL).UTC().Format(time.RFC3339), "console_url": "/console/agents/" + url.PathEscape(h.Identity.FullName(ctx, ag)) + "/spec-grant-requests"}))
		return true
	}
	grantID := strings.TrimSpace(metaString(env.Meta, "grant_request_id"))
	h.specGrantMu.Lock()
	g := h.specGrantRequests[grantID]
	status := "not_found"
	meta := map[string]any{}
	if g != nil && g.OwnerKey == c.ownerKey && g.SessionName == c.sessionName {
		if !g.ExpiresAt.After(now) && g.State == "pending" {
			g.State = "expired"
		}
		if g.State == "granted" && !g.CapabilityExpiresAt.After(now) {
			g.State, g.capability = "expired", ""
		}
		status = g.State
		if g.State == "granted" && g.capability != "" {
			meta["capability"], meta["page_id"], meta["action"], meta["spec_id"] = g.capability, g.PageID, g.Action, g.SpecID
			g.capability = ""
			g.State = "consumed"
		}
	}
	h.specGrantMu.Unlock()
	meta["status"] = status
	_ = c.write(ctx, h.specGrantResponse(c, requestID, meta))
	return true
}

// HandleConsoleSpecGrantPage is the owner-facing Agent-management approval
// page. The guarded API below performs every read and state transition.
func (h *Hub) HandleConsoleSpecGrantPage(w http.ResponseWriter, r *http.Request) {
	ag, _, ok := h.specGrantConsoleAgent(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	path := "/api/console/agents/" + url.PathEscape(h.Identity.FullName(r.Context(), ag)) + "/spec-grant-requests"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>Agent spec approvals</title><h1>Agent spec approvals</h1><p id="status">Loading…</p><table><thead><tr><th>Action</th><th>Spec</th><th>Status</th><th>Expires</th><th>Decision</th></tr></thead><tbody id="rows"></tbody></table><script>
const api=%q, rows=document.getElementById('rows'),status=document.getElementById('status');
async function load(){const r=await fetch(api,{cache:'no-store'}),d=await r.json();if(!r.ok){status.textContent='Unable to load approvals';return}rows.replaceChildren();for(const x of d.requests||[]){const tr=document.createElement('tr');for(const v of [x.action,x.spec_id||'—',x.status,x.expires_at]){const td=document.createElement('td');td.textContent=v;tr.append(td)}const td=document.createElement('td');for(const action of ['approve','deny']){const b=document.createElement('button');b.textContent=action==='approve'?'Approve':'Deny';b.disabled=x.status!=='pending';b.onclick=async()=>{await fetch(api+'/'+encodeURIComponent(x.id)+'/'+action,{method:'POST'});load()};td.append(b)}tr.append(td);rows.append(tr)}status.textContent='';}load();
</script>`, html.EscapeString(path))
}

func (h *Hub) HandleConsoleSpecGrantRequests(w http.ResponseWriter, r *http.Request) {
	ag, principal, ok := h.specGrantConsoleAgent(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	now := h.now()
	out := []map[string]any{}
	h.specGrantMu.Lock()
	for _, g := range h.specGrantRequests {
		if g.SessionName != ag.NameLC() || g.PrincipalID != identity.Lower(principal) {
			continue
		}
		if !g.ExpiresAt.After(now) && g.State == "pending" {
			g.State = "expired"
		}
		out = append(out, map[string]any{"id": g.ID, "action": g.Action, "spec_id": g.SpecID, "status": g.State, "expires_at": g.ExpiresAt.UTC().Format(time.RFC3339)})
	}
	h.specGrantMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"requests": out})
}

func (h *Hub) HandleConsoleSpecGrantDecision(w http.ResponseWriter, r *http.Request) {
	ag, principal, ok := h.specGrantConsoleAgent(r)
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id, decision, now := strings.TrimSpace(r.PathValue("requestID")), strings.TrimSpace(r.PathValue("decision")), h.now()
	h.specGrantMu.Lock()
	g := h.specGrantRequests[id]
	if g == nil || g.SessionName != ag.NameLC() || g.PrincipalID != identity.Lower(principal) || g.State != "pending" || !g.ExpiresAt.After(now) {
		if g != nil && !g.ExpiresAt.After(now) && g.State == "pending" {
			g.State = "expired"
		}
		h.specGrantMu.Unlock()
		http.Error(w, "grant unavailable", http.StatusNotFound)
		return
	}
	if decision == "deny" {
		g.State = "denied"
		h.specGrantMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if decision != "approve" {
		h.specGrantMu.Unlock()
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	repo, repoOK := h.Repo.(consoleAuthRepository)
	sess, sessOK := h.newPlatformSessionProjection(r)
	if !repoOK || !sessOK {
		h.specGrantMu.Unlock()
		http.Error(w, "console unavailable", http.StatusServiceUnavailable)
		return
	}
	raw, err := randomConsoleToken()
	capID, idErr := randomConsoleToken()
	if err != nil || idErr != nil {
		h.specGrantMu.Unlock()
		http.Error(w, "grant unavailable", http.StatusServiceUnavailable)
		return
	}
	cap := model.ConsolePageCapability{ID: capID, SessionID: sess.ID, PrincipalID: identity.Lower(principal), AgentID: ag.ID, PageID: g.PageID, TokenHash: hashConsoleToken(raw), ExpiresAt: now.Add(newPlatformCapabilityTTL)}
	h.specGrantMu.Unlock()
	if err := repo.RotateConsolePageCapability(r.Context(), cap, AgentKeyID(identity.Lower(principal)), ag.NameLC(), now); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.specGrantMu.Lock()
	if current := h.specGrantRequests[id]; current == g && current.State == "pending" {
		current.State = "granted"
		current.capability = raw
		current.CapabilitySessionID = sess.ID
		current.CapabilityExpiresAt = cap.ExpiresAt
	}
	h.specGrantMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) specGrantConsoleAgent(r *http.Request) (*identity.Agent, string, bool) {
	if h.Identity == nil {
		return nil, "", false
	}
	sess, ok := h.newPlatformSessionProjection(r)
	if !ok {
		return nil, "", false
	}
	ag, _, err := h.Identity.ResolveFullName(r.Context(), strings.TrimSpace(r.PathValue("sessionName")))
	if err != nil || ag == nil || identity.Lower(ag.OwnerLC) != identity.Lower(sess.PrincipalID) {
		return nil, "", false
	}
	return ag, sess.PrincipalID, true
}
