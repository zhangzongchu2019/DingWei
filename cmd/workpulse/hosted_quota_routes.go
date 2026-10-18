package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

type hostedQuotaRoutes struct {
	access             hostedAccess
	store              *hosted.Store
	gw                 hosted.NodeGateway
	identity           *identity.Service
	actor              func(*http.Request) (string, error)
	owner, ops, origin string
}

// No role aliases: Console's ordinary "owner" role, managers, and Agents owned
// by the platform owner must not acquire the human owner's release permission.
func (d hostedQuotaRoutes) humanOwner(r *http.Request) (string, bool) {
	if _, ok := d.access.human(r, d.actor); !ok {
		return "", false
	}
	if d.identity == nil || d.owner == "" || r.Header.Get("Authorization") != "" || d.actor == nil {
		return "", false
	}
	actor, err := d.actor(r)
	if err != nil || actor != d.owner {
		return "", false
	}
	u, err := d.identity.Store.GetUser(r.Context(), actor)
	return actor, err == nil && u.State == identity.UserActive && u.Type == identity.UserPlatformAdmin && !u.MustChangePassword
}
func (d hostedQuotaRoutes) canMonitor(r *http.Request) bool {
	if _, ok := d.humanOwner(r); ok {
		return true
	}
	if d.identity == nil || d.ops == "" || r.Method != http.MethodGet {
		return false
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || len(auth) > 4096 {
		return false
	}
	ag, full, err := d.identity.AuthenticateAgent(r.Context(), strings.TrimPrefix(auth, "Bearer "))
	return err == nil && ag != nil && full == d.ops
}
func (d hostedQuotaRoutes) register(mux httpapi.Mux) {
	human := hostedHumanMux{mux: mux, access: d.access, actor: d.actor}
	mux.HandleFunc("GET /api/console/hosted/quota-monitor", d.monitor)
	human.HandleFunc("POST /api/console/hosted/nodes/{nodeID}/holds/{seatID}/release", d.release)
	human.HandleFunc("GET /console/hosted-quota", d.page)
}
func (d hostedQuotaRoutes) monitor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !d.canMonitor(r) {
		writeJSON(w, 403, map[string]string{"error": "forbidden"})
		return
	}
	v, err := d.store.QuotaMonitor(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "internal_error"})
		return
	}
	_, canRelease := d.humanOwner(r)
	writeJSON(w, 200, map[string]any{"nodes": v, "can_release": canRelease})
}
func (d hostedQuotaRoutes) release(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := d.humanOwner(r)
	if !ok {
		// Do not echo or persist bearer/cookie/body/URL text. Authenticated denied
		// actors are recorded by their resolved identity, never a supplied name.
		denied := "unauthenticated"
		if d.actor != nil && r.Header.Get("Authorization") == "" {
			if user, err := d.actor(r); err == nil {
				denied = user
			}
		}
		if d.store != nil {
			_ = d.store.AddAudit(r.Context(), denied, "node.soft_quota.release_denied", "soft_hold_release", json.RawMessage(`{}`))
		}
		writeJSON(w, 403, map[string]string{"error": "forbidden"})
		return
	}
	if d.origin == "" || r.Header.Get("Origin") != d.origin {
		writeJSON(w, 403, map[string]string{"error": "origin_mismatch"})
		return
	}
	var p struct {
		HoldID  string `json:"hold_id"`
		Confirm bool   `json:"confirm"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	dec.DisallowUnknownFields()
	if dec.Decode(&p) != nil || dec.Decode(&struct{}{}) != io.EOF || !p.Confirm {
		writeJSON(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := d.store.ReleaseSoftHold(ctx, d.gw, actor, r.PathValue("nodeID"), r.PathValue("seatID"), p.HoldID); err != nil {
		writeJSON(w, 409, map[string]string{"error": "hold_release_refused", "notice": "未确认解除；请刷新监控核对，不自动重发"})
		return
	}
	writeJSON(w, 200, map[string]string{"state": "released_stopped", "notice": "hold 已解除，席位保持停止；需要另行启动"})
}

func (d hostedQuotaRoutes) page(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.humanOwner(r); !ok {
		http.Error(w, "forbidden", 403)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, hostedQuotaPage)
}

const hostedQuotaPage = `<!doctype html><meta charset="utf-8"><title>托管磁盘监控</title>
<h1>托管磁盘监控</h1><p>无硬磁盘配额。只有业主能解除 hold；解除不启动席位。prodops 仅监控并通知业主。</p>
<button id="refresh">刷新</button><p id="status"></p><div id="nodes"></div>
<script>
const status=document.getElementById('status'),nodes=document.getElementById('nodes');
function text(parent,tag,value){const e=document.createElement(tag);e.textContent=value;parent.append(e);return e}
async function refresh(){try{const r=await fetch('/api/console/hosted/quota-monitor',{cache:'no-store'});if(!r.ok)throw Error();const v=await r.json();nodes.replaceChildren();for(const n of v.nodes){const box=text(nodes,'section','');text(box,'h2',n.node_id+' / '+n.quota_mode+' / '+(n.fresh?'读数新鲜':'读数未知或过期'));text(box,'pre',JSON.stringify(n.thresholds,null,2));for(const [id,s] of Object.entries(n.seats)){const row=text(box,'div',id);text(row,'pre',JSON.stringify(s,null,2));if(v.can_release&&n.fresh&&n.hold_release_version===1&&s.hold&&s.hold_id&&s.stop_state==='stopped'){const b=text(row,'button','解除 hold，保持停止');b.onclick=async()=>{if(!confirm('仅解除当前 hold，不启动席位。确认继续？'))return;b.disabled=true;try{const r=await fetch('/api/console/hosted/nodes/'+encodeURIComponent(n.node_id)+'/holds/'+encodeURIComponent(id)+'/release',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({hold_id:s.hold_id,confirm:true})});const result=await r.json();status.textContent=result.notice||result.error}catch(e){status.textContent='结果未知；请手动刷新核对，不自动重发'}}}}}}catch(e){status.textContent='无法读取监控，请核对登录和权限'}}
document.getElementById('refresh').onclick=refresh;refresh();
</script>`

func runQuotaNotices(ctx context.Context, st *hosted.Store, hub *m8.Hub, owner, ops string) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		batch, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = st.DeliverQuotaNotices(batch, func(c context.Context, n hosted.QuotaNotice) error {
			return hub.QueueHostedQuotaNotice(c, owner, ops, n.Recipient, n.DeliveryID(), n.Text())
		})
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
