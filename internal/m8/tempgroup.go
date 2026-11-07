package m8

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
	"net/http"
	"time"
)

func (h *Hub) HandleConsoleTempGroups(w http.ResponseWriter, r *http.Request) {
	if h.TempGroups == nil {
		http.Error(w, "临时编组未启用", http.StatusServiceUnavailable)
		return
	}
	actor := platformPrincipal(r)
	code := extshared.AGCode(r.PathValue("code"))
	w.Header().Set("Cache-Control", "no-store")
	var result any
	var err error
	switch {
	case r.Method == http.MethodGet && code == "":
		var groups []tempgroup.View
		groups, err = h.TempGroups.List(r.Context(), actor)
		for i := range groups {
			h.tempGroupPresence(r.Context(), &groups[i])
		}
		result = map[string]any{"temp_groups": groups}
	case r.Method == http.MethodGet:
		var group tempgroup.View
		group, err = h.TempGroups.Detail(r.Context(), actor, code, true)
		if err == nil {
			h.tempGroupPresence(r.Context(), &group)
		}
		result = group
	case r.Method == http.MethodPost:
		var body struct {
			ExpectedExpiresAt time.Time `json:"expected_expires_at"`
			Reason            string    `json:"reason"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || body.ExpectedExpiresAt.IsZero() || body.Reason != "owner" {
			http.Error(w, "请刷新属性页并确认解散", http.StatusBadRequest)
			return
		}
		err = h.TempGroups.Dissolve(r.Context(), actor, code, body.ExpectedExpiresAt)
		result = map[string]bool{"ok": err == nil}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, tempgroup.ErrNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, tempgroup.ErrChanged) {
			status = http.StatusConflict
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
func (h *Hub) tempGroupPresence(ctx context.Context, v *tempgroup.View) {
	external := map[string]bool{}
	if h.ExternalSeats != nil {
		if online, e := h.ExternalSeats.Online(ctx); e == nil {
			for _, a := range online {
				external[a.Agent.ID] = true
			}
		}
	}
	for i := range v.Members {
		m := &v.Members[i]
		if v.DissolvedAt == nil && time.Now().Before(v.ExpiresAt) {
			if m.Role == "external" {
				m.Online = external[m.AgentID]
			} else {
				m.Online = h.AgentOnline(m.AgentID)
			}
		}
	}
}

// 重发同一终态只刷新完整目录快照；外部侧在每次 Wait/Send/ACK 现核时立即停止。
func (h *Hub) TempGroupDissolved(ctx context.Context, _ extshared.AGCode, _ string) {
	_ = h.broadcastNewPlatformDirectories(ctx)
}
